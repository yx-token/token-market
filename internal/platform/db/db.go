// Package db 封装本系统数据库（market PG）的连接与迁移。
package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 建立连接池并做一次连通性检查。
func Open(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// migrationLockKey 是迁移用的 advisory lock 键，避免多个进程同时迁移。
const migrationLockKey int64 = 0x746d5f6d6967 // "tm_mig"

// Migrate 按文件名顺序执行 fsys 中尚未执行的 *.sql，每个文件一个事务。
// 已执行文件的内容被修改时返回错误，避免生产库与代码静默不一致。
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockKey)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS market_schema_migrations (
		filename   text PRIMARY KEY,
		checksum   text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}

	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	var applied []string
	for _, name := range names {
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, err
		}
		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])

		var existing string
		err = conn.QueryRow(ctx, "SELECT checksum FROM market_schema_migrations WHERE filename = $1", name).Scan(&existing)
		switch {
		case err == nil:
			if existing != checksum {
				return applied, fmt.Errorf("migration %s was modified after being applied (db=%s file=%s)", name, existing, checksum)
			}
			continue
		case !errors.Is(err, pgx.ErrNoRows):
			return applied, err
		}

		if strings.TrimSpace(string(content)) == "" {
			return applied, fmt.Errorf("migration %s is empty", name)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO market_schema_migrations (filename, checksum) VALUES ($1, $2)", name, checksum); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit %s: %w", name, err)
		}
		applied = append(applied, name)
	}
	return applied, nil
}
