// market-api：控制面 HTTP 接口。M0 只提供健康检查与 sub2api 集成状态，业务接口随后续里程碑加入。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（可选，敏感项建议用环境变量）")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*configPath, log); err != nil {
		log.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Database.DSN == "" {
		return errors.New("database.dsn is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.Database.DSN, cfg.Database.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           routes(pool),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("api listening", "addr", cfg.HTTP.Addr)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func routes(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// 管理后台鉴权随 M1 的用户体系加入；在此之前该接口只应在内网暴露。
	mux.HandleFunc("GET /api/admin/sub2api/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := sub2apiStatus(r.Context(), pool)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	return mux
}

type statusResponse struct {
	Version     *string        `json:"sub2api_version"`
	Fingerprint *string        `json:"migration_fingerprint"`
	GuardState  string         `json:"guard_state"`
	Reason      *string        `json:"reason"`
	CheckedAt   *time.Time     `json:"checked_at"`
	Ops         map[string]int `json:"ops_by_status"`
}

func sub2apiStatus(ctx context.Context, pool *pgxpool.Pool) (*statusResponse, error) {
	var s statusResponse
	err := pool.QueryRow(ctx, `SELECT sub2api_version, migration_fingerprint, guard_state, reason, checked_at FROM compat_state WHERE id = 1`).
		Scan(&s.Version, &s.Fingerprint, &s.GuardState, &s.Reason, &s.CheckedAt)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT status, count(*) FROM sub2api_ops WHERE status <> 'done' GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s.Ops = map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		s.Ops[status] = n
	}
	return &s, rows.Err()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
