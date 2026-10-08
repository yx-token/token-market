// fake-sub2api 是开发与演示用的 sub2api 替身：在应用了 sub2api 真实迁移的数据库上实现本系统用到的
// Admin API、用户 API 与网关（网关不访问任何上游，按 internal/testutil/fakesub2api 的规则作答并记账）。
// 不能用于生产。
//
// 用法：
//
//	fake-sub2api --dsn postgres://.../fake_sub2api --sub2api-repo ../sub2api [--ref v0.2.14] [--listen 127.0.0.1:18081] [--admin-key dev-admin-key]
//
// 数据库为空时先执行 sub2api 的全部迁移；本系统的 sub2api.replica_dsn 指向同一个库。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/DueGin/token-market/internal/compat"
	"github.com/DueGin/token-market/internal/testutil/fakesub2api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := flag.String("dsn", "", "替身使用的数据库（空库会自动执行 sub2api 迁移）")
	repo := flag.String("sub2api-repo", "", "sub2api 本地 git 仓库，用于读取迁移")
	ref := flag.String("ref", "v0.2.14", "sub2api 版本的 git ref")
	listen := flag.String("listen", "127.0.0.1:18081", "监听地址")
	adminKey := flag.String("admin-key", "dev-admin-key", "Admin API Key")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*dsn, *repo, *ref, *listen, *adminKey, log); err != nil {
		log.Error("fake-sub2api exited", "err", err)
		os.Exit(1)
	}
}

// pongRe 与题库约定：探测题要求回答"PONG<n>"之类的口令时，替身按口令作答，其他问题回答 ok。
var pongRe = regexp.MustCompile(`PONG\d+`)

func run(dsn, repo, ref, listen, adminKey string, log *slog.Logger) error {
	if dsn == "" {
		return errors.New("--dsn is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	var migrated bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&migrated); err != nil {
		return err
	}
	if !migrated {
		if repo == "" {
			return errors.New("database is empty: --sub2api-repo is required to apply sub2api migrations")
		}
		set, err := compat.LoadGitMigrations(ctx, repo, ref)
		if err != nil {
			return err
		}
		log.Info("applying sub2api migrations", "ref", ref, "count", len(set.Names))
		if err := compat.ApplySub2APIMigrations(ctx, conn, set); err != nil {
			return err
		}
	}
	_ = conn.Close(ctx)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	f := fakesub2api.NewUnstarted(pool, adminKey)
	f.SetResponder(func(_ int64, prompt string) string {
		if code := pongRe.FindString(prompt); code != "" {
			return "The code is " + code
		}
		return "ok"
	})
	srv := &http.Server{Addr: listen, Handler: f.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	fmt.Fprintf(os.Stderr, "fake sub2api listening on http://%s (admin key %q) — for development only\n", listen, adminKey)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
