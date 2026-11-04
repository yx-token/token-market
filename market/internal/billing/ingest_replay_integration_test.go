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

type ingestReplayFixture struct {
	db, source                               *pgxpool.Pool
	reader                                   *replica.Reader
	buyerID, sellerID, upstreamID, listingID int64
	keyID                                    int64
}

func newIngestReplayFixture(t *testing.T) ingestReplayFixture {
	t.Helper()
	f := ingestReplayFixture{db: unfreezeDB(t)}
	_, f.source = testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := f.source.Exec(ctx, `CREATE TABLE usage_logs(
		id bigserial PRIMARY KEY,request_id text,created_at timestamptz NOT NULL DEFAULT now(),
		user_id bigint,api_key_id bigint,account_id bigint,group_id bigint,model text,
		input_tokens int DEFAULT 10,output_tokens int DEFAULT 20,
		cache_creation_tokens int DEFAULT 0,cache_read_tokens int DEFAULT 0,
		total_cost numeric(20,10),actual_cost numeric(20,10),account_rate_multiplier numeric(10,4),
		first_token_ms int,duration_ms int,upstream_response_model text,upstream_model_mismatch boolean)`); err != nil {
		t.Fatal(err)
	}
	for _, user := range []struct {
		email string
		id    *int64
	}{{"ingest-replay-buyer@example.test", &f.buyerID}, {"ingest-replay-seller@example.test", &f.sellerID}} {
		if err := f.db.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, user.email).Scan(user.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id)
		VALUES($1,'openai','oauth','listed',301) RETURNING id`, f.sellerID).Scan(&f.upstreamID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO listings(upstream_id,seller_id,mode,status,multiplier,listing_group_id,created_at)
		VALUES($1,$2,'consignment','active',1,401,now()-interval '1 day') RETURNING id`, f.upstreamID, f.sellerID).Scan(&f.listingID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type,status)
		VALUES($1,'ingest replay fixture','\x01','fixture','chain','active') RETURNING id`, f.buyerID).Scan(&f.keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `INSERT INTO internal_keys(buyer_key_id,buyer_user_id,sub2api_group_id,sub2api_key_id,key_enc,key_hash,status)
		VALUES($1,$2,401,501,'\x01','\x01','active')`, f.keyID, f.buyerID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := Post(ctx, tx, "recharge", "ingest-replay-fixture", "", nil, []Entry{
		{Kind: KindPlatformCash, Amount: "20000"},
		{Kind: KindBuyerCash, OwnerID: f.buyerID, Amount: "-20000"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	f.reader, err = replica.New(f.source)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f ingestReplayFixture) ingester() *Ingester {
	return &Ingester{
		DB: f.db, Replica: f.reader, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func (f ingestReplayFixture) assertPosted(t *testing.T, count int64) {
	t.Helper()
	ctx := context.Background()
	var matches bool
	if err := f.db.QueryRow(ctx, `SELECT count(*)=$1 AND count(ledger_txn_id)=$1
		AND count(*) FILTER(WHERE buyer_user_id=$2 AND seller_id=$3 AND buyer_key_id=$4 AND listing_id=$5 AND channel='direct')=$1
		AND COALESCE(sum(total_cost),0)=$1::numeric AND COALESCE(sum(actual_cost),0)=2*$1::numeric
		AND COALESCE(sum(payout),0)=$1::numeric FROM usage_records`, count, f.buyerID, f.sellerID, f.keyID, f.listingID).Scan(&matches); err != nil || !matches {
		t.Fatalf("all %d usage records must be attributed and posted once: matches=%v err=%v", count, matches, err)
	}
	for _, account := range []struct {
		owner string
		id    int64
		kind  string
		want  float64
	}{
		{"buyer", f.buyerID, KindBuyerCash, 20000 - 2*float64(count)},
		{"seller", f.sellerID, KindSellerPending, float64(count)},
		{"platform", 0, KindPlatformRevenue, float64(count)},
		{"platform", 0, KindPlatformCash, 20000},
	} {
		balances, err := Balances(ctx, f.db, account.owner, account.id)
		if err != nil || balances[account.kind] != account.want {
			t.Fatalf("%s %s balance=%v, want %v: %v", account.owner, account.kind, balances[account.kind], account.want, err)
		}
	}
	if err := f.db.QueryRow(ctx, `SELECT COALESCE(sum(request_count),0)=$1
		AND COALESCE(sum(total_cost),0)=$1::numeric AND COALESCE(sum(payout),0)=$1::numeric
		AND COALESCE(sum(buyer_paid),0)=2*$1::numeric FROM settlement_lines WHERE seller_id=$2`, count, f.sellerID).Scan(&matches); err != nil || !matches {
		t.Fatalf("seller settlements must include each request once: matches=%v err=%v", matches, err)
	}
	var unbalanced int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM
		(SELECT txn_id FROM ledger_entries GROUP BY txn_id HAVING sum(amount)<>0) x`).Scan(&unbalanced); err != nil || unbalanced != 0 {
		t.Fatalf("ledger transactions must remain balanced: unbalanced=%d err=%v", unbalanced, err)
	}
	bills, err := BuyerBills(ctx, f.db, f.buyerID, 7)
	if err != nil {
		t.Fatal(err)
	}
	var requests int64
	var cost float64
	for _, day := range bills {
		requests += int64(day.Requests)
		cost += day.Cost
	}
	if requests != count || cost != 2*float64(count) {
		t.Fatalf("buyer bills requests=%d cost=%v, want %d/%v", requests, cost, count, 2*float64(count))
	}
	usage, err := SellerUsageSummary(ctx, f.db, f.sellerID, f.upstreamID, 7)
	if err != nil || usage.Totals.Requests != count || usage.Totals.BuyerRequests != count || usage.Totals.Payout != float64(count) {
		t.Fatalf("seller usage should agree with ledger and settlements: usage=%+v err=%v", usage, err)
	}
}

func TestIngesterFreshUsagePostsWithoutTimeLag(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx := context.Background()
	if _, err := f.source.Exec(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-fresh',101,501,301,401,'fresh-model',1,2,1)`); err != nil {
		t.Fatal(err)
	}
	in := f.ingester()
	res, _, err := in.RunOnce(ctx)
	if err != nil || res.Inserted != 1 || res.Posted != 1 {
		t.Fatalf("fresh committed usage must be visible in the first round: result=%+v err=%v", res, err)
	}
	items, err := BuyerUsage(ctx, f.db, f.buyerID, UsageFilter{Model: "fresh-model"}, 10, 0)
	if err != nil || len(items) != 1 || items[0].Cost != 2 || items[0].Channel != "direct" {
		t.Fatalf("buyer usage must show the freshly posted request: items=%+v err=%v", items, err)
	}
	f.assertPosted(t, 1)
	for round := range 4 {
		res, _, err = f.ingester().RunOnce(ctx)
		if err != nil || res.Inserted != 0 || res.Posted != 0 {
			t.Fatalf("replay round %d must not charge again: result=%+v err=%v", round, res, err)
		}
	}
	f.assertPosted(t, 1)
}

func TestIngesterReplayRecoversLateCommitAfterRestart(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx := context.Background()
	late, err := f.source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = late.Rollback(ctx) }()
	var lateID int64
	if err := late.QueryRow(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-late',101,501,301,401,'late-model',1,2,1) RETURNING id`).Scan(&lateID); err != nil {
		t.Fatal(err)
	}
	if lateID != 1 {
		t.Fatalf("fixture should hold the earliest sequence ID, got %d", lateID)
	}
	const earlyCount int64 = 6001
	var lastID int64
	if err := f.source.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		SELECT 'client:ingest-early-'||n,101,501,301,401,'early-model',1,2,1 FROM generate_series(1,$1::int) n RETURNING id)
		SELECT max(id) FROM inserted`, earlyCount).Scan(&lastID); err != nil {
		t.Fatal(err)
	}
	in := f.ingester()
	var cursor, replay, replayEnd int64
	for round := range 12 {
		if _, _, err := in.RunOnce(ctx); err != nil {
			t.Fatalf("ingest committed higher IDs round %d: %v", round, err)
		}
		if err := f.db.QueryRow(ctx, `SELECT
			(SELECT last_id FROM ingest_cursors WHERE source='usage_logs'),
			(SELECT last_id FROM ingest_cursors WHERE source='usage_logs_replay'),
			(SELECT last_id FROM ingest_cursors WHERE source='usage_logs_replay_end')`).Scan(&cursor, &replay, &replayEnd); err != nil {
			t.Fatal(err)
		}
		if cursor == lastID && replay > lateID {
			break
		}
		if round == 11 {
			t.Fatalf("high IDs and replay must progress past the uncommitted hole: cursor=%d replay=%d lastID=%d", cursor, replay, lastID)
		}
	}
	if cursor-lateID <= 2000 {
		t.Fatalf("late ID must be outside the former overlap window: cursor=%d lateID=%d", cursor, lateID)
	}
	if replayEnd <= replay || replayEnd > cursor {
		t.Fatalf("an incomplete replay must persist its bounded high watermark: cursor=%d replay=%d end=%d", cursor, replay, replayEnd)
	}
	var missing bool
	if err := f.db.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM usage_records WHERE sub2api_usage_id=$1)`, lateID).Scan(&missing); err != nil || !missing {
		t.Fatalf("uncommitted source row must not be imported: missing=%v err=%v", missing, err)
	}
	f.assertPosted(t, earlyCount)
	if err := late.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Reconstruct on every round so recovery cannot rely on in-process state.
	var inserted, posted int64
	for round := range 12 {
		res, _, err := f.ingester().RunOnce(ctx)
		if err != nil {
			t.Fatalf("replay after restart round %d: %v", round, err)
		}
		inserted += int64(res.Inserted)
		posted += res.Posted
		if round == 0 {
			var nextReplay, nextEnd int64
			if err := f.db.QueryRow(ctx, `SELECT
				(SELECT last_id FROM ingest_cursors WHERE source='usage_logs_replay'),
				(SELECT last_id FROM ingest_cursors WHERE source='usage_logs_replay_end')`).Scan(&nextReplay, &nextEnd); err != nil {
				t.Fatal(err)
			}
			if nextReplay != 0 && (nextReplay <= replay || nextEnd != replayEnd) {
				t.Fatalf("restart must resume the persisted replay and frozen upper bound: before=%d/%d after=%d/%d", replay, replayEnd, nextReplay, nextEnd)
			}
		}
		var recovered bool
		if err := f.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM usage_records WHERE sub2api_usage_id=$1 AND ledger_txn_id IS NOT NULL)`, lateID).Scan(&recovered); err != nil {
			t.Fatal(err)
		}
		if recovered {
			break
		}
		if round == 11 {
			t.Fatal("persistent replay never recovered the late low ID after restarting the ingester")
		}
	}
	if inserted != 1 || posted != 1 {
		t.Fatalf("late commit must be inserted and charged exactly once: inserted=%d posted=%d", inserted, posted)
	}
	items, err := BuyerUsage(ctx, f.db, f.buyerID, UsageFilter{Model: "late-model"}, 10, 0)
	if err != nil || len(items) != 1 || items[0].SubAPIUsageID != lateID || items[0].Cost != 2 {
		t.Fatalf("buyer must see the recovered late request: items=%+v err=%v", items, err)
	}
	f.assertPosted(t, earlyCount+1)
	for round := range 8 {
		res, _, err := f.ingester().RunOnce(ctx)
		if err != nil || res.Inserted != 0 || res.Posted != 0 {
			t.Fatalf("completed replay round %d must be idempotent: result=%+v err=%v", round, res, err)
		}
	}
	f.assertPosted(t, earlyCount+1)
}

func TestIngesterTailRecoversLateCommitInNextRound(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx := context.Background()
	if _, err := f.source.Exec(ctx, `SELECT setval('usage_logs_id_seq',8500)`); err != nil {
		t.Fatal(err)
	}
	late, err := f.source.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = late.Rollback(ctx) }()
	var lateID int64
	if err := late.QueryRow(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-tail-late',101,501,301,401,'tail-late-model',1,2,1) RETURNING id`).Scan(&lateID); err != nil {
		t.Fatal(err)
	}
	const earlyCount int64 = 1499
	var lastID int64
	if err := f.source.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		SELECT 'client:ingest-tail-early-'||n,101,501,301,401,'tail-early-model',1,2,1
		FROM generate_series(1,$1::int) n RETURNING id) SELECT max(id) FROM inserted`, earlyCount).Scan(&lastID); err != nil {
		t.Fatal(err)
	}
	if lateID != 8501 || lastID != 10000 {
		t.Fatalf("tail fixture IDs=%d..%d, want 8501..10000", lateID, lastID)
	}
	res, _, err := f.ingester().RunOnce(ctx)
	if err != nil || int64(res.Inserted) != earlyCount || res.Posted != earlyCount {
		t.Fatalf("higher committed tail records must post first: result=%+v err=%v", res, err)
	}
	// The full replay is past the hole, so only the fixed tail can recover it now.
	if _, err := f.db.Exec(ctx, `UPDATE ingest_cursors SET last_id=CASE source
		WHEN 'usage_logs' THEN 10000 WHEN 'usage_logs_replay' THEN 9000 ELSE 10000 END
		WHERE source IN('usage_logs','usage_logs_replay','usage_logs_replay_end')`); err != nil {
		t.Fatal(err)
	}
	if err := late.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	res, _, err = f.ingester().RunOnce(ctx)
	if err != nil || res.Inserted != 1 || res.Posted != 1 {
		t.Fatalf("late commit within the tail must post in the very next round: result=%+v err=%v", res, err)
	}
	items, err := BuyerUsage(ctx, f.db, f.buyerID, UsageFilter{Model: "tail-late-model"}, 10, 0)
	if err != nil || len(items) != 1 || items[0].SubAPIUsageID != lateID || items[0].Cost != 2 {
		t.Fatalf("buyer must immediately see the late tail request: items=%+v err=%v", items, err)
	}
	f.assertPosted(t, earlyCount+1)
	res, _, err = f.ingester().RunOnce(ctx)
	if err != nil || res.Inserted != 0 || res.Posted != 0 {
		t.Fatalf("tail/full replay overlap must not charge the request again: result=%+v err=%v", res, err)
	}
	f.assertPosted(t, earlyCount+1)
}

func TestIngesterScopesAttributionAndPostingToFetchedIDs(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx := context.Background()
	if _, err := f.db.Exec(ctx, `INSERT INTO usage_records(
		sub2api_usage_id,sub2api_request_id,created_at,sub2api_account_id,sub2api_group_id,sub2api_user_id,sub2api_api_key_id,
		model,total_cost,actual_cost,channel,payout,upstream_id,seller_id,listing_id,buyer_user_id,buyer_key_id)
		VALUES
		(900001,'client:ingest-outside-unknown',now(),301,401,101,501,'outside-model',1,2,'unknown',0,NULL,NULL,NULL,NULL,NULL),
		(900002,'client:ingest-outside-unposted',now(),301,401,101,501,'outside-model',1,2,'direct',1,$1,$2,$3,$4,$5)`,
		f.upstreamID, f.sellerID, f.listingID, f.buyerID, f.keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Exec(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-scoped',101,501,301,401,'scoped-model',1,2,1)`); err != nil {
		t.Fatal(err)
	}
	res, _, err := f.ingester().RunOnce(ctx)
	if err != nil || res.Inserted != 1 || res.Posted != 1 {
		t.Fatalf("only the fetched source record should post: result=%+v err=%v", res, err)
	}
	var untouched bool
	if err := f.db.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM usage_records WHERE sub2api_usage_id=900001 AND channel='unknown' AND upstream_id IS NULL
		AND seller_id IS NULL AND buyer_user_id IS NULL AND buyer_key_id IS NULL AND payout=0 AND ledger_txn_id IS NULL)
		AND EXISTS(SELECT 1 FROM usage_records WHERE sub2api_usage_id=900002 AND channel='direct' AND ledger_txn_id IS NULL)
		AND (SELECT count(*) FROM usage_records WHERE ledger_txn_id IS NOT NULL)=1`).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("unfetched unknown and unposted records must remain untouched: untouched=%v err=%v", untouched, err)
	}
	for _, account := range []struct {
		owner string
		id    int64
		kind  string
		want  float64
	}{
		{"buyer", f.buyerID, KindBuyerCash, 19998},
		{"seller", f.sellerID, KindSellerPending, 1},
		{"platform", 0, KindPlatformRevenue, 1},
	} {
		balances, err := Balances(ctx, f.db, account.owner, account.id)
		if err != nil || balances[account.kind] != account.want {
			t.Fatalf("scope must limit %s %s to fetched usage: balance=%v want=%v err=%v", account.owner, account.kind, balances[account.kind], account.want, err)
		}
	}
	res, _, err = f.ingester().RunOnce(ctx)
	if err != nil || res.Inserted != 0 || res.Posted != 0 {
		t.Fatalf("replaying fetched IDs must not claim unfetched historical records: result=%+v err=%v", res, err)
	}
}

func TestIngesterConcurrentRoundsChargeOnce(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := f.source.Exec(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-concurrent',101,501,301,401,'concurrent-model',1,2,1)`); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(1953325673)`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		res *IngestResult
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			res, _, err := f.ingester().RunOnce(ctx)
			results <- result{res, err}
		}()
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND query LIKE '%pg_advisory_xact_lock(1953325673)%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent ingestion rounds did not reach the shared advisory lock")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var inserted, posted int64
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		inserted += int64(r.res.Inserted)
		posted += r.res.Posted
	}
	if inserted != 1 || posted != 1 {
		t.Fatalf("concurrent rounds must import and charge once: inserted=%d posted=%d", inserted, posted)
	}
	f.assertPosted(t, 1)
}

func TestIngesterFailedPostingRollsBackAllCursors(t *testing.T) {
	f := newIngestReplayFixture(t)
	ctx := context.Background()
	if _, err := f.source.Exec(ctx, `INSERT INTO usage_logs(request_id,user_id,api_key_id,account_id,group_id,model,total_cost,actual_cost,account_rate_multiplier)
		VALUES('client:ingest-rollback',101,501,301,401,'rollback-model',1,2,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `CREATE FUNCTION fail_ingest_posting_fixture() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'simulated ingest ledger failure'; END $$;
		CREATE TRIGGER fail_ingest_posting_fixture BEFORE INSERT ON ledger_txns
		FOR EACH ROW WHEN (NEW.type='usage') EXECUTE FUNCTION fail_ingest_posting_fixture()`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.ingester().RunOnce(ctx); err == nil {
		t.Fatal("simulated ledger failure must reject the ingestion round")
	}
	var rolledBack bool
	if err := f.db.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM usage_records) AND
		NOT EXISTS(SELECT 1 FROM ledger_txns WHERE type='usage') AND
		(SELECT count(*)=3 AND bool_and(last_id=0) FROM ingest_cursors
		WHERE source IN('usage_logs','usage_logs_replay','usage_logs_replay_end'))`).Scan(&rolledBack); err != nil || !rolledBack {
		t.Fatalf("failed posting must roll back imported rows and every cursor: rolledBack=%v err=%v", rolledBack, err)
	}
	if _, err := f.db.Exec(ctx, `DROP TRIGGER fail_ingest_posting_fixture ON ledger_txns; DROP FUNCTION fail_ingest_posting_fixture()`); err != nil {
		t.Fatal(err)
	}
	res, _, err := f.ingester().RunOnce(ctx)
	if err != nil || res.Inserted != 1 || res.Posted != 1 {
		t.Fatalf("retry after rollback must charge the source request once: result=%+v err=%v", res, err)
	}
	f.assertPosted(t, 1)
}
