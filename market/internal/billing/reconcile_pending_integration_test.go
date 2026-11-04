//go:build integration

package billing

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
)

func TestBalanceReconciliationMissingSourceUsersDoNotAbortOtherWallets(t *testing.T) {
	for _, tc := range []struct {
		name, removeSQL string
	}{
		{"missing", `DELETE FROM users WHERE id IN (101,201)`},
		{"soft_deleted", `UPDATE users SET deleted_at=now() WHERE id IN (101,201)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStoreCreditFixture(t)
			ctx := context.Background()
			creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,actual_cost numeric(20,10));
				INSERT INTO users(id,balance,status) VALUES(102,9,'active')`)
			creditExec(t, f.replicaDB, tc.removeSQL)
			var otherBuyerID int64
			if err := f.db.QueryRow(ctx, `INSERT INTO users(email,password_hash)
				VALUES('other-reconcile-buyer@example.test','fixture') RETURNING id`).Scan(&otherBuyerID); err != nil {
				t.Fatal(err)
			}
			creditExec(t, f.db, `INSERT INTO shadow_identities(kind,owner_id,status,sub2api_user_id,email,password_enc)
				VALUES('buyer',$1,'active',102,'other-reconcile-buyer@shadow.test','\x01')`, otherBuyerID)
			tx, err := f.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := Post(ctx, tx, "recharge", "other-reconcile-buyer", "", nil, []Entry{
				{Kind: KindPlatformCash, Amount: "10"},
				{Kind: KindBuyerCash, OwnerID: otherBuyerID, Amount: "-10"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			op, err := ops.Enqueue(ctx, f.db, ops.NewOp{Type: opshandlers.TypeUserBalanceAdd, Target: "user:201",
				Payload: opshandlers.BalancePayload{UserID: 201, Amount: 70}, IdempotencyKey: "missing-source-funding"})
			if err != nil {
				t.Fatal(err)
			}
			creditExec(t, f.db, `UPDATE sub2api_ops SET status='done' WHERE id=$1`, op)
			creditExec(t, f.db, `UPDATE store_credit_packs SET funding_op_id=$2 WHERE id=$1`, f.packID, op)
			r := &Reconciler{DB: f.db, Replica: f.funds.Replica, Settings: f.funds.Settings, Log: slog.Default()}
			if n, err := r.RunBalances(ctx); err != nil || n != 3 {
				t.Fatalf("missing source identities must not stop other buyer or credit reconciliation: n=%d err=%v", n, err)
			}
			for _, wallet := range []struct {
				key                  string
				actual, expectedDiff float64
			}{
				{fmt.Sprintf("balance:%d:", f.buyerID), 0, -70},
				{fmt.Sprintf("balance:%d:", otherBuyerID), 9, -1},
				{fmt.Sprintf("credit-balance:%d:", f.packID), 0, -70},
			} {
				var actual, diff float64
				if err := f.db.QueryRow(ctx, `SELECT (detail->>'sub2api_balance')::float8,amount::float8
					FROM billing_anomalies WHERE dedupe_key LIKE $1`, wallet.key+"%").Scan(&actual, &diff); err != nil || actual != wallet.actual || diff != wallet.expectedDiff {
					t.Fatalf("wallet %s anomaly: actual=%v diff=%v err=%v", wallet.key, actual, diff, err)
				}
			}
			if n, err := r.RunBalances(ctx); err != nil || n != 0 {
				t.Fatalf("missing-source anomalies must remain idempotent: n=%d err=%v", n, err)
			}
		})
	}
}

func TestBalanceReconciliationChecksUnpostedIDsOnBothSidesOfCursor(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,actual_cost numeric(20,10));
		INSERT INTO usage_logs VALUES(1,101,2),(2,201,3.5);
		UPDATE users SET balance=CASE WHEN id=101 THEN 68 ELSE 66.5 END`)
	creditExec(t, f.db, `UPDATE ingest_cursors SET last_id=5000 WHERE source='usage_logs'`)
	op, err := ops.Enqueue(ctx, f.db, ops.NewOp{Type: opshandlers.TypeUserBalanceAdd, Target: "user:201",
		Payload: opshandlers.BalancePayload{UserID: 201, Amount: 70}, IdempotencyKey: "reconcile-pending-funding"})
	if err != nil {
		t.Fatal(err)
	}
	creditExec(t, f.db, `UPDATE sub2api_ops SET status='done' WHERE id=$1`, op)
	creditExec(t, f.db, `UPDATE store_credit_packs SET funding_op_id=$2 WHERE id=$1`, f.packID, op)

	// A one-connection pool also checks that verification does not acquire a
	// second market connection while holding its read transaction.
	poolCfg := f.db.Config()
	poolCfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	r := &Reconciler{DB: single, Replica: f.funds.Replica, Settings: f.funds.Settings, Log: slog.Default()}
	check := func() {
		t.Helper()
		if n, err := r.RunBalances(ctx); err != nil || n != 0 {
			t.Fatalf("legitimate pending or posted usage must not report a mismatch: n=%d err=%v", n, err)
		}
	}
	check() // Both late commits are below the forward cursor, awaiting replay.

	f.addUsage(t, 2, "3.5", "2.8")
	f.post(t, 1)
	creditExec(t, f.db, `INSERT INTO usage_records
		(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,sub2api_api_key_id,model,
		total_cost,actual_cost,buyer_user_id,channel)
		VALUES(1,now(),301,101,501,'gpt-6-astra',2,2,$1,'direct')`, f.buyerID)
	f.post(t, 1)
	check()

	creditExec(t, f.replicaDB, `INSERT INTO usage_logs VALUES(5001,101,1); UPDATE users SET balance=67 WHERE id=101`)
	creditExec(t, f.db, `INSERT INTO usage_records
		(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,sub2api_api_key_id,model,
		total_cost,actual_cost,buyer_user_id,channel)
		VALUES(5001,now(),301,101,501,'gpt-6-astra',1,1,$1,'direct')`, f.buyerID)
	f.post(t, 1)
	check() // Usage above the cursor may already have been posted.

	creditExec(t, f.replicaDB, `DELETE FROM usage_logs WHERE id=1`)
	check() // Source retention must not subtract all historical market usage.
	creditExec(t, f.replicaDB, `UPDATE users SET balance=68 WHERE id=101`)
	if n, err := r.RunBalances(ctx); err != nil || n != 1 {
		t.Fatalf("a confirmed unexplained balance difference must still be reported: n=%d err=%v", n, err)
	}
}

func TestBalanceReconciliationPaginatesPendingCosts(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx := context.Background()
	creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,actual_cost numeric(20,10));
		INSERT INTO usage_logs SELECT id,101,0.01 FROM generate_series(1,2001) id;
		UPDATE users SET balance=49.99 WHERE id=101`)
	creditExec(t, f.db, `UPDATE ingest_cursors SET last_id=5000 WHERE source='usage_logs'`)
	r := &Reconciler{DB: f.db, Replica: f.funds.Replica, Settings: f.funds.Settings, Log: slog.Default()}
	if n, err := r.RunBalances(ctx); err != nil || n != 0 {
		t.Fatalf("every pending source cost must be counted across pages: n=%d err=%v", n, err)
	}
}

func TestBalanceVerificationSnapshotDoesNotBlockConcurrentPosting(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,actual_cost numeric(20,10));
		INSERT INTO usage_logs VALUES(1,101,2); UPDATE users SET balance=68 WHERE id=101`)
	creditExec(t, f.db, `INSERT INTO usage_records
		(sub2api_usage_id,created_at,sub2api_account_id,sub2api_user_id,sub2api_api_key_id,model,
		total_cost,actual_cost,buyer_user_id,channel)
		VALUES(1,now(),301,101,501,'gpt-6-astra',2,2,$1,'direct')`, f.buyerID)
	blocker, err := f.replicaDB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{DB: f.db, Replica: f.funds.Replica}
	type result struct {
		balance verifiedBalance
		err     error
	}
	completed := make(chan result, 1)
	go func() {
		balance, err := r.verifyBalance(ctx, 101, f.buyerID, false)
		completed <- result{balance, err}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := f.replicaDB.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND query LIKE '%SELECT id, balance, status%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("balance verification did not reach its source read")
		case <-ticker.C:
		}
	}
	// The market snapshot already contains the old ledger. Commit usage while
	// verification is paused between its ledger and source reads.
	postCtx, postCancel := context.WithTimeout(ctx, 2*time.Second)
	defer postCancel()
	tx, err := f.db.Begin(postCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if n, err := postUsage(postCtx, tx, 0); err != nil || n != 1 {
		t.Fatalf("post concurrent usage: n=%d err=%v", n, err)
	}
	if err := tx.Commit(postCtx); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-completed
	if got.err != nil || got.balance.expected != 70 || got.balance.pending != 2 || got.balance.diff() != 0 {
		t.Fatalf("verification must keep its original ledger and claim snapshot: balance=%+v err=%v", got.balance, got.err)
	}
}

func TestBalanceSourceSnapshotKeepsBalanceAndUsagePagesConsistent(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx := context.Background()
	creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint PRIMARY KEY,user_id bigint,actual_cost numeric(20,10));
		INSERT INTO usage_logs VALUES(1,101,1)`)
	err := f.funds.Replica.ReadSnapshot(ctx, func(source *replica.Reader) error {
		balances, err := source.UserBalances(ctx, []int64{101})
		if err != nil || len(balances) != 1 || balances[0].Balance != 70 {
			t.Fatalf("initial source balance: %+v %v", balances, err)
		}
		creditExec(t, f.replicaDB, `UPDATE users SET balance=69 WHERE id=101;
			INSERT INTO usage_logs VALUES(2,101,1)`)
		costs, err := source.UsageCostsByUser(ctx, 101, 0, 2000)
		if err != nil || len(costs) != 1 || costs[0].ID != 1 {
			t.Fatalf("later usage pages must retain the balance snapshot: %+v %v", costs, err)
		}
		balances, err = source.UserBalances(ctx, []int64{101})
		if err != nil || len(balances) != 1 || balances[0].Balance != 70 {
			t.Fatalf("source snapshot changed after concurrent commit: %+v %v", balances, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	balances, err := f.funds.Replica.UserBalances(ctx, []int64{101})
	if err != nil || len(balances) != 1 || balances[0].Balance != 69 {
		t.Fatalf("new queries must observe the later source commit: %+v %v", balances, err)
	}
}
