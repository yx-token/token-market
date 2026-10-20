//go:build integration

package billing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func unfreezeDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	return pool
}

func seedUnfreezeSettlement(t *testing.T, pool *pgxpool.Pool, sellerID int64, hour time.Time, payout string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO settlement_lines
		(upstream_id, seller_id, hour, channel, request_count, total_cost, payout, buyer_paid)
		VALUES ($1, $1, $2, 'direct', 1, $3, $3, $3)`, sellerID, hour, payout); err != nil {
		t.Fatal(err)
	}
	if _, err := Post(ctx, tx, "adjust", fmt.Sprintf("unfreeze-fixture:%d", sellerID), "", nil, []Entry{
		{Kind: KindPlatformCash, Amount: payout},
		{Kind: KindSellerPending, OwnerID: sellerID, Amount: "-" + payout},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func assertUnfreezeBalance(t *testing.T, pool *pgxpool.Pool, sellerID int64, pending, available float64) {
	t.Helper()
	balances, err := Balances(context.Background(), pool, "seller", sellerID)
	if err != nil {
		t.Fatal(err)
	}
	if balances[KindSellerPending] != pending || balances[KindSellerAvailable] != available {
		t.Fatalf("seller %d balance = %+v, want pending=%v available=%v", sellerID, balances, pending, available)
	}
}

func TestUnfreezeHoursWaitsForWholeHourMaturity(t *testing.T) {
	pool := unfreezeDB(t)
	ctx := context.Background()
	hour := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for i, freezeHours := range []int{0, 1, 24, 25} {
		t.Run(fmt.Sprintf("%d hours", freezeHours), func(t *testing.T) {
			sellerID := int64(100 + i)
			seedUnfreezeSettlement(t, pool, sellerID, hour, "1.10")
			maturesAt := hour.Add(time.Duration(freezeHours+1) * time.Hour)
			if n, err := UnfreezeHours(ctx, pool, freezeHours, maturesAt.Add(-time.Nanosecond)); err != nil || n != 0 {
				t.Fatalf("hour must remain frozen until its end plus freeze period: n=%d err=%v", n, err)
			}
			assertUnfreezeBalance(t, pool, sellerID, 1.10, 0)
			if n, err := UnfreezeHours(ctx, pool, freezeHours, maturesAt); err != nil || n != 1 {
				t.Fatalf("exact maturity should release: n=%d err=%v", n, err)
			}
			assertUnfreezeBalance(t, pool, sellerID, 0, 1.10)
			if n, err := UnfreezeHours(ctx, pool, freezeHours, maturesAt); err != nil || n != 0 {
				t.Fatalf("repeat should be idempotent: n=%d err=%v", n, err)
			}
		})
	}
}

func TestUnfreezeUsesUpdatedFreezeHoursAndKeepsDaysCompatibility(t *testing.T) {
	pool := unfreezeDB(t)
	ctx := context.Background()
	hour := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	seedUnfreezeSettlement(t, pool, 201, hour, "4")
	now := hour.Add(4 * time.Hour)
	st, err := settings.Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := UnfreezeHours(ctx, pool, st.FreezeHours, now); err != nil || n != 0 {
		t.Fatalf("default 24 hours must not release after 4 hours: n=%d err=%v", n, err)
	}
	if _, err := settings.Update(ctx, pool, []byte(`{"freeze_hours":2}`)); err != nil {
		t.Fatal(err)
	}
	st, err = settings.Load(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := UnfreezeHours(ctx, pool, st.FreezeHours, now); err != nil || n != 1 {
		t.Fatalf("updated hours should take effect immediately: n=%d err=%v", n, err)
	}
	assertUnfreezeBalance(t, pool, 201, 0, 4)
	seedUnfreezeSettlement(t, pool, 202, hour, "3")
	if n, err := Unfreeze(ctx, pool, 1, hour.Add(24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("legacy days still wait for bucket end: n=%d err=%v", n, err)
	}
	if n, err := Unfreeze(ctx, pool, 1, hour.Add(25*time.Hour)); err != nil || n != 1 {
		t.Fatalf("legacy days convert to hours: n=%d err=%v", n, err)
	}
	assertUnfreezeBalance(t, pool, 202, 0, 3)
}

func TestUnfreezeConcurrentClaimsAndLateUsage(t *testing.T) {
	pool := unfreezeDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hour := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	now := hour.Add(26 * time.Hour)
	seedUnfreezeSettlement(t, pool, 301, hour, "10")
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT 1 FROM settlement_lines WHERE seller_id=301 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		n   int
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			n, err := UnfreezeHours(ctx, pool, 24, now)
			results <- result{n: n, err: err}
		}()
	}
	// 两个事务都已经到达相同结算行的锁，确定性覆盖并发争领。
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND query LIKE '%settlement_lines%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent unfreeze transactions did not reach row lock")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var totalReleased int
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		totalReleased += r.n
	}
	if totalReleased != 1 {
		t.Fatalf("exactly one claimant should release, got %d", totalReleased)
	}
	assertUnfreezeBalance(t, pool, 301, 0, 10)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_txns WHERE type='unfreeze'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent claim created duplicate ledger transaction: %d %v", count, err)
	}
	// 迟到用量增加同一小时的应收；相同 now 再跑只释放新增部分。
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE settlement_lines SET payout=payout+2.5 WHERE seller_id=301`); err != nil {
		t.Fatal(err)
	}
	if _, err := Post(ctx, tx, "adjust", "late-usage-fixture", "", nil, []Entry{
		{Kind: KindPlatformCash, Amount: "2.5"},
		{Kind: KindSellerPending, OwnerID: 301, Amount: "-2.5"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := UnfreezeHours(ctx, pool, 24, now); err != nil || n != 1 {
		t.Fatalf("late usage increment should release even with identical now: n=%d err=%v", n, err)
	}
	assertUnfreezeBalance(t, pool, 301, 0, 12.5)
	if n, err := UnfreezeHours(ctx, pool, 24, now); err != nil || n != 0 {
		t.Fatalf("late usage must not release twice: n=%d err=%v", n, err)
	}
}

func TestUnfreezeRollsBackClaimWhenLedgerFails(t *testing.T) {
	pool := unfreezeDB(t)
	ctx := context.Background()
	hour := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	seedUnfreezeSettlement(t, pool, 401, hour, "5")
	if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_unfreeze_fixture() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'simulated ledger failure'; END $$;
		CREATE TRIGGER fail_unfreeze_fixture BEFORE INSERT ON ledger_txns
		FOR EACH ROW WHEN (NEW.type = 'unfreeze') EXECUTE FUNCTION fail_unfreeze_fixture()`); err != nil {
		t.Fatal(err)
	}
	if _, err := UnfreezeHours(ctx, pool, 24, hour.Add(26*time.Hour)); err == nil {
		t.Fatal("expected simulated ledger failure")
	}
	assertUnfreezeBalance(t, pool, 401, 5, 0)
	var unfrozen bool
	if err := pool.QueryRow(ctx, `SELECT unfrozen_payout <> 0 FROM settlement_lines WHERE seller_id=401`).Scan(&unfrozen); err != nil || unfrozen {
		t.Fatalf("failed ledger must roll back settlement claim: unfrozen=%v err=%v", unfrozen, err)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER fail_unfreeze_fixture ON ledger_txns`); err != nil {
		t.Fatal(err)
	}
	if n, err := UnfreezeHours(ctx, pool, 24, hour.Add(26*time.Hour)); err != nil || n != 1 {
		t.Fatalf("retry after rollback must release once: n=%d err=%v", n, err)
	}
	assertUnfreezeBalance(t, pool, 401, 0, 5)
}
