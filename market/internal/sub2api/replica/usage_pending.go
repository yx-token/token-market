package replica

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ReadSnapshot keeps balances and all usage pages on one source snapshot.
func (r *Reader) ReadSnapshot(ctx context.Context, fn func(*Reader) error) error {
	beginner, ok := r.db.(interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("replica: reader does not support snapshot transactions")
	}
	tx, err := beginner.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("replica: begin read snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(&Reader{db: tx, queries: r.queries}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type UsageCost struct {
	ID         int64
	ActualCost float64
}

// UsageCostsByUser returns lightweight pages for an exact source/ledger match.
func (r *Reader) UsageCostsByUser(ctx context.Context, userID, afterID int64, limit int) ([]UsageCost, error) {
	rows, err := r.db.Query(ctx, r.queries["usage_costs_by_user"], userID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("replica usage_costs_by_user: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (UsageCost, error) {
		var cost UsageCost
		err := row.Scan(&cost.ID, &cost.ActualCost)
		return cost, err
	})
}
