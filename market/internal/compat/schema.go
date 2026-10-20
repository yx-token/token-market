package compat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
)

// ApplySub2APIMigrations 在一个空数据库上按 sub2api 迁移执行器的规则执行迁移：
// 普通文件整个文件一个事务；*_notx.sql 逐条语句非事务执行（CONCURRENTLY 索引）。
// 只用于兼容检查的临时库，不用于生产。
func ApplySub2APIMigrations(ctx context.Context, conn *pgx.Conn, set *MigrationSet) error {
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		checksum   TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return err
	}
	for _, name := range set.Names {
		content := string(set.Contents[name])
		checksum := Checksum(set.Contents[name])
		if strings.HasSuffix(name, "_notx.sql") {
			for i, st := range SplitStatements(content) {
				if _, err := conn.Exec(ctx, st.Text); err != nil {
					return fmt.Errorf("apply %s (statement %d): %w", name, i+1, err)
				}
			}
			if _, err := conn.Exec(ctx, "INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", name, checksum); err != nil {
				return err
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(content) != "" {
			if _, err := tx.Exec(ctx, content); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("apply %s: %w", name, err)
			}
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", name, checksum); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ErrScratchNotEmpty：查询检查必须使用空库，防止误把生产库当作临时库。
var ErrScratchNotEmpty = errors.New("scratch database is not empty; refusing to apply migrations")

// CheckQueries 把目标版本的迁移执行到空的临时库，再对每条契约查询执行 EXPLAIN，
// 返回失败的查询（查询名 → 错误）。这是 s2a-compat 结构比对阶段的轻量版本，
// 最终以启动目标版本 sub2api 镜像的完整检查为准。
func CheckQueries(ctx context.Context, scratchDSN string, target *MigrationSet) ([]Finding, error) {
	conn, err := pgx.Connect(ctx, scratchDSN)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var tables int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()`).Scan(&tables); err != nil {
		return nil, err
	}
	if tables > 0 {
		return nil, ErrScratchNotEmpty
	}
	if err := ApplySub2APIMigrations(ctx, conn, target); err != nil {
		return nil, err
	}

	results := replica.ExplainAll(ctx, conn)
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	var findings []Finding
	for _, name := range names {
		if err := results[name]; err != nil {
			findings = append(findings, Finding{
				Severity: SeverityConflict,
				Rule:     "query_invalid",
				Detail:   fmt.Sprintf("契约查询 %s 在目标版本表结构上执行失败：%v", name, err),
			})
		}
	}
	return findings, nil
}
