//go:build integration

package billing

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/testutil"
)

type usagePostingFixture struct {
	buyerID, sellerID int64
}

func seedUsagePosting(t *testing.T, pool *pgxpool.Pool) usagePostingFixture {
	t.Helper()
	ctx := context.Background()
	var f usagePostingFixture
	for _, user := range []struct {
		email string
		id    *int64
	}{{"usage-buyer@example.test", &f.buyerID}, {"usage-seller@example.test", &f.sellerID}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, user.email).Scan(user.id); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := Post(ctx, tx, "recharge", "usage-posting-fixture", "", nil, []Entry{
		{Kind: KindPlatformCash, Amount: "10"},
		{Kind: KindBuyerCash, OwnerID: f.buyerID, Amount: "-10"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO usage_records
		(sub2api_usage_id,sub2api_request_id,created_at,sub2api_account_id,sub2api_group_id,
		sub2api_user_id,sub2api_api_key_id,model,total_cost,actual_cost,upstream_id,seller_id,buyer_user_id,channel,payout)
		VALUES(1,'usage-posting-fixture','2026-10-14T12:30:00Z',9001,7001,5001,3001,
		'gpt-6-astra',1,2,9001,$1,$2,'direct',1)`, f.sellerID, f.buyerID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return f
}

func assertUsagePostingState(t *testing.T, pool *pgxpool.Pool, f usagePostingFixture, posted bool) {
	t.Helper()
	ctx := context.Background()
	var count, expectedCount int
	var expectedBuyer, expectedSeller, expectedRevenue string
	if posted {
		expectedCount, expectedBuyer, expectedSeller, expectedRevenue = 1, "-8", "-1", "-1"
	} else {
		expectedCount, expectedBuyer, expectedSeller, expectedRevenue = 0, "-10", "0", "0"
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_txns WHERE type='usage'`).Scan(&count); err != nil || count != expectedCount {
		t.Fatalf("usage transactions=%d, want %d: %v", count, expectedCount, err)
	}
	var entries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries e JOIN ledger_txns t ON t.id=e.txn_id WHERE t.type='usage'`).Scan(&entries); err != nil || entries != 3*expectedCount {
		t.Fatalf("usage entries=%d, want %d: %v", entries, 3*expectedCount, err)
	}
	for _, expected := range []struct {
		kind   string
		owner  int64
		amount string
	}{
		{KindBuyerCash, f.buyerID, expectedBuyer},
		{KindSellerPending, f.sellerID, expectedSeller},
		{KindPlatformRevenue, 0, expectedRevenue},
		{KindPlatformCash, 0, "10"},
	} {
		var matches bool
		if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(e.amount),0)=$3::numeric
			FROM ledger_accounts a LEFT JOIN ledger_entries e ON e.account_id=a.id
			WHERE a.kind=$1 AND a.owner_id=$2`, expected.kind, expected.owner, expected.amount).Scan(&matches); err != nil || !matches {
			t.Fatalf("account %s owner %d should have raw entries sum %s: matches=%v err=%v", expected.kind, expected.owner, expected.amount, matches, err)
		}
	}
	var summaryMatches, usageMatches bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=$1::int AND COALESCE(sum(request_count),0)=$1::int
		AND COALESCE(sum(total_cost),0)=$1::numeric AND COALESCE(sum(payout),0)=$1::numeric
		AND COALESCE(sum(buyer_paid),0)=2*$1::numeric AND COALESCE(sum(unfrozen_payout),0)=0
		FROM settlement_lines WHERE seller_id=$2`, expectedCount, f.sellerID).Scan(&summaryMatches); err != nil || !summaryMatches {
		t.Fatalf("settlement summary should reflect exactly %d usage record: matches=%v err=%v", expectedCount, summaryMatches, err)
	}
	if err := pool.QueryRow(ctx, `SELECT (ledger_txn_id IS NOT NULL)=$1 FROM usage_records WHERE sub2api_usage_id=1`, posted).Scan(&usageMatches); err != nil || !usageMatches {
		t.Fatalf("usage claim should match committed ledger: matches=%v err=%v", usageMatches, err)
	}
	var unbalanced int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM
		(SELECT txn_id FROM ledger_entries GROUP BY txn_id HAVING sum(amount)<>0) x`).Scan(&unbalanced); err != nil || unbalanced != 0 {
		t.Fatalf("all ledger transactions must balance: unbalanced=%d err=%v", unbalanced, err)
	}
}

func TestPostUsageConcurrentTransactionsClaimOnce(t *testing.T) {
	pool := unfreezeDB(t)
	f := seedUsagePosting(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(1953325673)`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		posted int64
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			tx, err := pool.Begin(ctx)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			n, err := postUsage(ctx, tx, 0)
			if err == nil {
				err = tx.Commit(ctx)
			}
			results <- result{posted: n, err: err}
		}()
	}
	// 确保两个事务都在同一个入账入口等待，避免只覆盖偶然的顺序执行。
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND query LIKE '%pg_advisory_xact_lock(1953325673)%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent posting transactions did not reach advisory lock")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var posted int64
	var zeroResults int
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		posted += r.posted
		if r.posted == 0 {
			zeroResults++
		}
	}
	if posted != 1 || zeroResults != 1 {
		t.Fatalf("same batch must post once and return zero to the other claimant: posted=%d zero results=%d", posted, zeroResults)
	}
	assertUsagePostingState(t, pool, f, true)
}

func TestPostUsageRollbackRetryAndReplay(t *testing.T) {
	pool := unfreezeDB(t)
	f := seedUsagePosting(t, pool)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if n, err := postUsage(ctx, tx, 0); err != nil || n != 1 {
		t.Fatalf("initial in-transaction posting: n=%d err=%v", n, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertUsagePostingState(t, pool, f, false)
	for _, expected := range []int64{1, 0, 0} {
		retry, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = retry.Rollback(ctx) }()
		if n, err := postUsage(ctx, retry, 0); err != nil || n != expected {
			t.Fatalf("retry/replay: n=%d, want %d, err=%v", n, expected, err)
		}
		if err := retry.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		assertUsagePostingState(t, pool, f, true)
	}
}

func TestIngesterContinuousIDsAdvanceBeyondOverlapWindow(t *testing.T) {
	pool := unfreezeDB(t)
	_, source := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE usage_logs(
		id bigint PRIMARY KEY,request_id text,created_at timestamptz,user_id bigint,api_key_id bigint,account_id bigint,
		group_id bigint,model text,input_tokens int DEFAULT 0,output_tokens int DEFAULT 0,
		cache_creation_tokens int DEFAULT 0,cache_read_tokens int DEFAULT 0,total_cost numeric(20,10),actual_cost numeric(20,10),
		account_rate_multiplier numeric(10,4),first_token_ms int,duration_ms int,upstream_response_model text,upstream_model_mismatch boolean);
		INSERT INTO usage_logs(id,request_id,created_at,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		SELECT id,'probe-'||id,now()-interval '10 minutes',1,101,301,401,'gpt-6-astra',1,0,1 FROM generate_series(1,4001) id`); err != nil {
		t.Fatal(err)
	}
	var sellerID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('ingest-cursor@example.test','fixture') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id)
		VALUES($1,'openai','oauth','listed',301)`, sellerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO system_keys(purpose,sub2api_group_id,key_enc,sub2api_key_id)
		VALUES('probe',401,'\x01',101)`); err != nil {
		t.Fatal(err)
	}
	r, err := replica.New(source)
	if err != nil {
		t.Fatal(err)
	}
	in := Ingester{DB: pool, Replica: r, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	inserted, posted := 0, int64(0)
	for round := range 5 {
		result, more, err := in.RunOnce(ctx)
		if err != nil {
			t.Fatalf("ingest round %d: %v", round, err)
		}
		inserted += result.Inserted
		posted += result.Posted
		if !more {
			break
		}
		if round == 4 {
			t.Fatal("continuous IDs must not loop forever over an already ingested overlap window")
		}
	}
	if inserted != 4001 || posted != 4001 {
		t.Fatalf("all 4001 consecutive records should be inserted and posted once: inserted=%d posted=%d", inserted, posted)
	}
	var cursor int64
	if err := pool.QueryRow(ctx, `SELECT last_id FROM ingest_cursors WHERE source='usage_logs'`).Scan(&cursor); err != nil || cursor != 4001 {
		t.Fatalf("cursor=%d, want 4001: %v", cursor, err)
	}
	result, more, err := in.RunOnce(ctx)
	if err != nil || more || result.Inserted != 0 || result.Posted != 0 {
		t.Fatalf("replaying only overlap should be idle and idempotent: result=%+v more=%v err=%v", result, more, err)
	}
	var matches bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=4001 AND count(ledger_txn_id)=4001 AND sum(payout)=4001
		FROM usage_records`).Scan(&matches); err != nil || !matches {
		t.Fatalf("continuous usage should be claimed exactly once: matches=%v err=%v", matches, err)
	}
	if err := pool.QueryRow(ctx, `SELECT sum(request_count)=4001 AND sum(total_cost)=4001 AND sum(payout)=4001
		AND sum(buyer_paid)=0 FROM settlement_lines WHERE seller_id=$1`, sellerID).Scan(&matches); err != nil || !matches {
		t.Fatalf("settlement must count each continuous record exactly once: matches=%v err=%v", matches, err)
	}
	balances, err := Balances(ctx, pool, "seller", sellerID)
	if err != nil || balances[KindSellerPending] != 4001 {
		t.Fatalf("seller pending should include every posted record once: %+v %v", balances, err)
	}
	platform, err := Balances(ctx, pool, "platform", 0)
	if err != nil || platform[KindPlatformProbeCost] != 4001 {
		t.Fatalf("probe expense should balance continuous seller earnings: %+v %v", platform, err)
	}
	var unbalanced int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM(SELECT txn_id FROM ledger_entries GROUP BY txn_id HAVING sum(amount)<>0) x`).Scan(&unbalanced); err != nil || unbalanced != 0 {
		t.Fatalf("continuous ingestion must keep every ledger transaction balanced: count=%d err=%v", unbalanced, err)
	}
}
