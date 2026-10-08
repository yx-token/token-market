// Package testutil 提供集成测试用的临时数据库。
//
// 设置 TM_TEST_DATABASE_DSN 指向一个可以建库的 PostgreSQL（例如本地 docker），
// 每个测试会创建独立的临时库并在结束时删除；未设置时跳过集成测试。
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewDatabase 创建一个空的临时库，返回其 DSN 与连接池。
func NewDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	base := os.Getenv("TM_TEST_DATABASE_DSN")
	if base == "" {
		t.Skip("TM_TEST_DATABASE_DSN not set")
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "tm_t_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	dsn := u.String()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return dsn, pool
}
