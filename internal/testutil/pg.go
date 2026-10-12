// Package testutil 提供集成测试用的临时数据库。
//
// 设置 TM_TEST_DATABASE_DSN 指向一个可以建库的 PostgreSQL（例如本地 docker），
// 每个测试会创建独立的临时库并在结束时删除；未设置时跳过集成测试。
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 给慢速 CI 中的迁移和 PostgreSQL 文件复制留出余量；单次操作失败不无限等待。
const databaseTimeout = 3 * time.Minute

// NewDatabase 创建一个空的临时库，返回其 DSN 与连接池。
// 迁移/兼容测试保留此路径，以验证从空库执行迁移的行为。
func NewDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	return newTestDatabase(t, "", nil)
}

func testDatabaseBase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TM_TEST_DATABASE_DSN")
	if base == "" {
		t.Skip("TM_TEST_DATABASE_DSN not set")
	}
	return base
}

func newTestDatabase(t *testing.T, key string, initialize templateInitializer) (string, *pgxpool.Pool) {
	t.Helper()
	base := testDatabaseBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	var name, dsn string
	var err error
	if initialize == nil {
		name, dsn, err = createDatabase(ctx, base, "", "tm_t_")
	} else {
		name, dsn, err = processTemplates.clone(ctx, base, key, initialize)
	}
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	// CREATE 成功后立刻注册，后续连接失败也会清理新库。
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), databaseTimeout)
		defer cleanupCancel()
		if err := dropDatabase(cleanupCtx, base, name); err != nil {
			t.Errorf("drop test database %s: %v", name, err)
		}
	})
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return dsn, pool
}

func databaseDSN(base, name string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse dsn: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("test database DSN must be a postgres URL")
	}
	u.Path = "/" + name
	return u.String(), nil
}

func createDatabase(ctx context.Context, base, template, prefix string) (string, string, error) {
	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		return "", "", fmt.Errorf("database name: %w", err)
	}
	name := prefix + hex.EncodeToString(suffix)
	dsn, err := databaseDSN(base, name)
	if err != nil {
		return "", "", err
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", "", fmt.Errorf("connect admin db: %w", err)
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	query := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		query += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}
	if _, err := admin.Exec(ctx, query); err != nil {
		return "", "", cleanupFailedCreate(base, name, err)
	}
	return name, dsn, nil
}

func dropDatabase(ctx context.Context, base, name string) error {
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

// CREATE 的客户端错误不能证明服务器没有提交（例如确认响应丢失）。用新时限
// 清理本次随机候选名；42P04 表示候选名已经存在，绝不能删除原有数据库。
func cleanupFailedCreate(base, name string, createErr error) error {
	var pgErr *pgconn.PgError
	if errors.As(createErr, &pgErr) && pgErr.Code == "42P04" {
		return fmt.Errorf("create database: %w", createErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	if err := dropDatabase(ctx, base, name); err != nil {
		return errors.Join(fmt.Errorf("create database: %w", createErr), fmt.Errorf("cleanup failed database %s: %w", name, err))
	}
	return fmt.Errorf("create database: %w", createErr)
}
