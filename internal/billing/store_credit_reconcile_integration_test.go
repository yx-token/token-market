//go:build integration

package billing

import (
	"context"
	"log/slog"
	"testing"

	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
)

func TestStoreCreditWalletReconciliationIncludesInflightRemainder(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx := context.Background()
	creditExec(t, f.replicaDB, `CREATE TABLE usage_logs(id bigint,user_id bigint,actual_cost numeric(20,10))`)
	op, err := ops.Enqueue(ctx, f.db, ops.NewOp{Type: opshandlers.TypeUserBalanceAdd, Target: "user:201", Payload: opshandlers.BalancePayload{UserID: 201, Amount: 70}, IdempotencyKey: "reconcile-funding-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	creditExec(t, f.db, "UPDATE sub2api_ops SET status='done' WHERE id=$1", op)
	creditExec(t, f.db, "UPDATE store_credit_packs SET funding_op_id=$2 WHERE id=$1", f.packID, op)
	r := &Reconciler{DB: f.db, Replica: f.funds.Replica, Settings: f.funds.Settings, Log: slog.Default()}
	check := func() {
		t.Helper()
		n, err := r.RunBalances(ctx)
		if err != nil || n != 0 {
			t.Fatalf("balance reconciliation found %d anomalies: %v", n, err)
		}
	}
	check()
	f.addUsage(t, 1, "3.5", "2.8")
	f.post(t, 1)
	creditExec(t, f.replicaDB, "UPDATE users SET balance=66.5 WHERE id=201")
	check()
	f.addUsage(t, 2, "68", "54.4")
	f.post(t, 1)
	creditExec(t, f.replicaDB, "UPDATE users SET balance=-1.5 WHERE id=201")
	check() // 隔离钱包负尾差、普通现金未扣：两边 outbox 都应作为在途差异。
	creditExec(t, f.db, "UPDATE sub2api_ops SET status='done' WHERE idempotency_key IN ('tm-credit-cash-2','tm-credit-restore-2')")
	creditExec(t, f.replicaDB, "UPDATE users SET balance=CASE WHEN id=101 THEN 68.5 ELSE 0 END")
	check()
	creditExec(t, f.replicaDB, "UPDATE users SET balance=2 WHERE id=201")
	n, err := r.RunBalances(ctx)
	if err != nil || n != 1 {
		t.Fatalf("unexplained isolated funds should be reported once: n=%d err=%v", n, err)
	}
	var packID int64
	if err := f.db.QueryRow(ctx, `SELECT (detail->>'credit_pack_id')::bigint FROM billing_anomalies WHERE dedupe_key LIKE 'credit-balance:%'`).Scan(&packID); err != nil || packID != f.packID {
		t.Fatalf("missing isolated wallet identity: %d %v", packID, err)
	}
}

func TestStoreCreditCashDebtCannotBeRefundedAndRetriesAfterRecharge(t *testing.T) {
	f := newStoreCreditFixture(t)
	ctx := context.Background()
	f.addUsage(t, 1, "71.5", "57.2")
	f.post(t, 1)
	// 远端余额尚未补扣时，仍只能退已记账的现金，不可绕开 ¥1.5 欠款。
	fen, err := f.funds.refundableFen(ctx, f.buyerID, 70)
	if err != nil || fen != 6850 {
		t.Fatalf("inflight debt enlarged refundable cash: %d %v", fen, err)
	}
	creditExec(t, f.db, "UPDATE sub2api_ops SET status='dead',attempts=max_attempts WHERE idempotency_key='tm-credit-cash-1'")
	creditExec(t, f.replicaDB, "UPDATE users SET balance=0 WHERE id=101")
	if err := f.funds.retryCreditRemainders(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := f.db.QueryRow(ctx, "SELECT status FROM sub2api_ops WHERE idempotency_key='tm-credit-cash-1'").Scan(&status); err != nil || status != "dead" {
		t.Fatalf("unfunded remainder retried: %s %v", status, err)
	}
	creditExec(t, f.replicaDB, "UPDATE users SET balance=2 WHERE id=101")
	for range 2 {
		if err := f.funds.retryCreditRemainders(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var attempts, count int
	if err := f.db.QueryRow(ctx, "SELECT status,attempts FROM sub2api_ops WHERE idempotency_key='tm-credit-cash-1'").Scan(&status, &attempts); err != nil || status != "pending" || attempts != 0 {
		t.Fatalf("recharge did not rearm original operation: %s/%d %v", status, attempts, err)
	}
	if err := f.db.QueryRow(ctx, "SELECT count(*) FROM sub2api_ops WHERE idempotency_key=$1", "tm-credit-cash-1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry created duplicate cash deduction: %d %v", count, err)
	}
	f.assertBalances(t, 68.5, 0, 25.2, 6.3)
}
