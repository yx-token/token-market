package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
)

const reconcileUsageBatch = 2000

const buyerExpectedBalanceSQL = `SELECT
	COALESCE((SELECT -sum(e.amount) FROM ledger_accounts a JOIN ledger_entries e ON e.account_id=a.id
		WHERE a.owner_type='buyer' AND a.owner_id=$1 AND a.kind='buyer_cash'),0)::float8
	+ COALESCE((SELECT sum(remaining) FROM quota_packs WHERE buyer_id=$1 AND status='active'),0)::float8`

const creditExpectedBalanceSQL = `SELECT CASE WHEN refund_op_id IS NOT NULL
	AND (SELECT status FROM sub2api_ops WHERE id=refund_op_id)='done'
	THEN 0 ELSE remaining_balance_units END::float8 FROM store_credit_packs WHERE id=$1`

const balanceInflightSQL = `SELECT COALESCE(sum(CASE WHEN op_type='user.balance_add'
	THEN (payload->>'amount')::numeric ELSE -(payload->>'amount')::numeric END),0)::float8
	FROM sub2api_ops WHERE target=$1 AND status IN ('pending','sending','failed')
	AND op_type IN ('user.balance_add','user.balance_subtract') AND idempotency_key NOT LIKE $2`

type verifiedBalance struct {
	actual, expected, inflight, pending float64
}

func (b verifiedBalance) diff() float64 {
	return b.actual + b.inflight - (b.expected - b.pending)
}

// verifyBalance is the slow path for a suspected mismatch. The forward cursor
// can skip late commits, so only exact posted IDs establish settled usage.
// Keep market reads on one snapshot without blocking concurrent ingestion.
func (r *Reconciler) verifyBalance(ctx context.Context, subUserID, ownerID int64, credit bool) (verifiedBalance, error) {
	var out verifiedBalance
	tx, err := r.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	expectedSQL, excludedOps := buyerExpectedBalanceSQL, "tm-refund-%"
	if credit {
		expectedSQL, excludedOps = creditExpectedBalanceSQL, "tm-credit-refund-debit-%"
	}
	if err := tx.QueryRow(ctx, expectedSQL, ownerID).Scan(&out.expected); err != nil {
		return out, err
	}
	if err := tx.QueryRow(ctx, balanceInflightSQL, fmt.Sprintf("user:%d", subUserID), excludedOps).Scan(&out.inflight); err != nil {
		return out, err
	}
	err = r.Replica.ReadSnapshot(ctx, func(source *replica.Reader) error {
		balances, err := source.UserBalances(ctx, []int64{subUserID})
		if err != nil {
			return err
		}
		// Keep deleted/missing source users local to this anomaly; one deleted
		// shadow identity must not abort reconciliation for every other buyer.
		if len(balances) == 1 {
			out.actual = balances[0].Balance
		}
		out.pending, err = unpostedUsageCost(ctx, tx, source, subUserID)
		return err
	})
	return out, err
}

func unpostedUsageCost(ctx context.Context, db DB, source *replica.Reader, userID int64) (float64, error) {
	var total, correction float64
	var after int64
	for {
		costs, err := source.UsageCostsByUser(ctx, userID, after, reconcileUsageBatch)
		if err != nil {
			return 0, err
		}
		if len(costs) == 0 {
			return total, nil
		}
		ids := make([]int64, len(costs))
		for i, cost := range costs {
			ids[i] = cost.ID
		}
		rows, err := db.Query(ctx, `SELECT sub2api_usage_id FROM usage_records
			WHERE sub2api_usage_id=ANY($1) AND ledger_txn_id IS NOT NULL`, ids)
		if err != nil {
			return 0, err
		}
		postedIDs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return 0, err
		}
		posted := make(map[int64]bool, len(postedIDs))
		for _, id := range postedIDs {
			posted[id] = true
		}
		for _, cost := range costs {
			if !posted[cost.ID] {
				// Compensated summation keeps large pending pages within the
				// same floating-point tolerance as SQL's numeric aggregate.
				y := cost.ActualCost - correction
				next := total + y
				correction = (next - total) - y
				total = next
			}
		}
		after = costs[len(costs)-1].ID
		if len(costs) < reconcileUsageBatch {
			return total, nil
		}
	}
}
