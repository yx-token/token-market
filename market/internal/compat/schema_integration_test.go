//go:build integration

package compat_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/contract"
	"github.com/yx-token/idlicat/market/internal/compat"
	"github.com/yx-token/idlicat/market/internal/sub2api/compatguard"
	"github.com/yx-token/idlicat/market/internal/sub2api/replica"
	"github.com/yx-token/idlicat/market/internal/testutil"
	"github.com/jackc/pgx/v5"
)

// 在真实的 sub2api 表结构上验证全部契约查询，并验证只读访问层可以扫描结果。
// 需要 TM_TEST_DATABASE_DSN 与 TM_SUB2API_REPO。
func TestContractQueriesAgainstRealSub2APISchema(t *testing.T) {
	repo := os.Getenv("TM_SUB2API_REPO")
	if repo == "" {
		t.Skip("TM_SUB2API_REPO not set")
	}
	dsn, pool := testutil.NewDatabase(t)
	ctx := context.Background()

	target, err := compat.LoadGitMigrations(ctx, repo, "v0.2.14")
	if err != nil {
		t.Fatal(err)
	}
	findings, err := compat.CheckQueries(ctx, dsn, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Errorf("%s: %s", f.Rule, f.Detail)
	}

	// 临时库已不再为空，再次检查必须拒绝
	if _, err := compat.CheckQueries(ctx, dsn, target); err != compat.ErrScratchNotEmpty {
		t.Fatalf("expected ErrScratchNotEmpty, got %v", err)
	}

	r, err := replica.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	names, err := r.AppliedMigrationNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(target.Names) {
		t.Fatalf("applied %d migrations, target has %d", len(names), len(target.Names))
	}
	spec, err := contract.Load()
	if err != nil {
		t.Fatal(err)
	}
	if res := compatguard.Evaluate(spec, "0.2.14", names); res.State != compatguard.StateNormal {
		t.Fatalf("fresh v0.2.14 schema should pass the runtime guard: %+v", res)
	}
	if _, err := r.TMAccounts(ctx); err != nil {
		t.Fatalf("TMAccounts: %v", err)
	}
	if _, err := r.TMGroups(ctx); err != nil {
		t.Fatalf("TMGroups: %v", err)
	}
	if _, err := r.TMAccountGroups(ctx); err != nil {
		t.Fatalf("TMAccountGroups: %v", err)
	}
	if _, err := r.UserBalances(ctx, []int64{1, 2}); err != nil {
		t.Fatalf("UserBalances: %v", err)
	}

	// 写入一组最小数据，验证各列类型能被正确扫描（numeric → float64、jsonb → map、可空列 → 指针）
	var userID, accountID, groupID, keyID int64
	mustScan(t, pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, balance) VALUES ('b1@shadow.local', 'x', 12.5) RETURNING id`), &userID)
	mustScan(t, pool.QueryRow(ctx, `INSERT INTO accounts (name, platform, type, extra, rate_multiplier) VALUES ('tm-u-1', 'anthropic', 'oauth', '{"max_sessions": 2}', 0.3) RETURNING id`), &accountID)
	mustScan(t, pool.QueryRow(ctx, `INSERT INTO groups (name, platform, rate_multiplier, is_exclusive) VALUES ('tm-l-1', 'anthropic', 0.36, true) RETURNING id`), &groupID)
	if _, err := pool.Exec(ctx, `INSERT INTO account_groups (account_id, group_id) VALUES ($1, $2)`, accountID, groupID); err != nil {
		t.Fatal(err)
	}
	mustScan(t, pool.QueryRow(ctx, `INSERT INTO api_keys (key, name, user_id, group_id) VALUES ('tm_internal_0123456789', 'tm-ik-1', $1, $2) RETURNING id`, userID, groupID), &keyID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, account_id, group_id, model, request_id,
		                        input_tokens, output_tokens, total_cost, actual_cost, account_rate_multiplier, first_token_ms)
		VALUES ($1, $2, $3, $4, 'claude-sonnet-4', 'client:abc', 100, 20, 1.0, 0.36, 0.3, 850)`,
		userID, keyID, accountID, groupID); err != nil {
		t.Fatal(err)
	}

	logs, err := r.UsageLogsAfter(ctx, 0, time.Now().Add(time.Minute), 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("UsageLogsAfter: %v %d", err, len(logs))
	}
	u := logs[0]
	if u.RequestID == nil || *u.RequestID != "client:abc" || u.ActualCost != 0.36 ||
		u.AccountRateMultiplier == nil || *u.AccountRateMultiplier != 0.3 || u.FirstTokenMs == nil || *u.FirstTokenMs != 850 {
		t.Fatalf("usage log scanned wrong: %+v", u)
	}
	accounts, err := r.TMAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].RateMultiplier != 0.3 || accounts[0].Extra["max_sessions"] != float64(2) {
		t.Fatalf("TMAccounts: %v %+v", err, accounts)
	}
	groups, err := r.TMGroups(ctx)
	if err != nil || len(groups) != 1 || !groups[0].IsExclusive || groups[0].RateMultiplier != 0.36 {
		t.Fatalf("TMGroups: %v %+v", err, groups)
	}
	members, err := r.TMAccountGroups(ctx)
	if err != nil || len(members) != 1 || members[0].GroupID != groupID {
		t.Fatalf("TMAccountGroups: %v %+v", err, members)
	}
	balances, err := r.UserBalances(ctx, []int64{userID})
	if err != nil || len(balances) != 1 || balances[0].Balance != 12.5 {
		t.Fatalf("UserBalances: %v %+v", err, balances)
	}
}

func mustScan(t *testing.T, row pgx.Row, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}
