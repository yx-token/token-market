//go:build integration

package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/testutil"
)

type storeCreditFixture struct {
	db, replicaDB                                       *pgxpool.Pool
	funds                                               *Funds
	buyerID, sellerID, otherSellerID, keyID, upstreamID int64
	listingID, packID                                   int64
}

func creditExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func newStoreCreditFixture(t *testing.T) storeCreditFixture {
	t.Helper()
	f := storeCreditFixture{db: unfreezeDB(t)}
	_, f.replicaDB = testutil.NewDatabase(t)
	creditExec(t, f.replicaDB, `CREATE TABLE users(id bigint PRIMARY KEY,balance numeric(20,8),status text,deleted_at timestamptz);
		INSERT INTO users(id,balance,status) VALUES(101,100,'active'),(201,70,'active')`)
	ctx := context.Background()
	for i, id := range []*int64{&f.buyerID, &f.sellerID, &f.otherSellerID} {
		if err := f.db.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, fmt.Sprintf("credit-%d@example.test", i)).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id)
		VALUES($1,'openai','oauth','listed',301) RETURNING id`, f.sellerID).Scan(&f.upstreamID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO listings(upstream_id,seller_id,mode,status,multiplier,listing_group_id)
		VALUES($1,$2,'consignment','active',0.8,401) RETURNING id`, f.upstreamID, f.sellerID).Scan(&f.listingID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type,status)
		VALUES($1,'credit fixture','\x01','fixture','chain','active') RETURNING id`, f.buyerID).Scan(&f.keyID); err != nil {
		t.Fatal(err)
	}
	creditExec(t, f.db, `INSERT INTO shadow_identities(kind,owner_id,status,sub2api_user_id,email,password_enc)
		VALUES('buyer',$1,'active',101,'credit-buyer@shadow.test','\x01')`, f.buyerID)
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := Post(ctx, tx, "recharge", "credit-fixture", "", nil, []Entry{
		{Kind: KindPlatformCash, Amount: "100"},
		{Kind: KindBuyerCash, OwnerID: f.buyerID, Amount: "-100"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var offerID int64
	if err := f.db.QueryRow(ctx, `INSERT INTO store_credit_offers
		(seller_id,title,price_cny,face_value_usd,allowed_platforms,allowed_models)
		VALUES($1,'本店活动',30,10,ARRAY['openai'],ARRAY['gpt-6-astra']) RETURNING id`, f.sellerID).Scan(&offerID); err != nil {
		t.Fatal(err)
	}
	r, err := replica.New(f.replicaDB)
	if err != nil {
		t.Fatal(err)
	}
	f.funds = &Funds{DB: f.db, Replica: r, Shadow: &buyer.ShadowService{DB: f.db},
		Settings: func(context.Context) (settings.Settings, error) {
			st := settings.Defaults()
			st.CreditUSDToBalanceRate = 7
			return st, nil
		}, Now: func() time.Time { return time.Now().Add(time.Hour) }}
	p, err := f.funds.PurchaseCreditPack(ctx, f.buyerID, PurchaseCreditPackInput{OfferID: offerID, IdempotencyKey: "fixture-purchase"})
	if err != nil {
		t.Fatalf("purchase activity pack: %v", err)
	}
	f.packID = p.ID
	if p.Status != "provisioning" || p.RemainingUSD != 10 || p.RemainingBalanceUnits != 70 || p.PrepaidRemainingCNY != 30 || p.USDToBalanceRate != 7 {
		t.Fatalf("unexpected purchased pack: %+v", p)
	}
	f.assertBalances(t, 70, 30, 0, 0)
	f.assertOp(t, fmt.Sprintf("tm-credit-purchase-%d", f.packID), "user.balance_subtract", "user:101", "30")
	creditExec(t, f.db, `UPDATE sub2api_ops SET status='done' WHERE id=(SELECT purchase_op_id FROM store_credit_packs WHERE id=$1)`, f.packID)
	creditExec(t, f.db, `UPDATE store_credit_packs SET status='active' WHERE id=$1`, f.packID)
	creditExec(t, f.db, `INSERT INTO shadow_identities(kind,owner_id,status,sub2api_user_id,email,password_enc)
		VALUES('credit_pack',$1,'active',201,'credit-pack@shadow.test','\x01')`, f.packID)
	creditExec(t, f.replicaDB, `UPDATE users SET balance=70 WHERE id=101`)
	return f
}

func (f storeCreditFixture) addUsage(t *testing.T, id int64, actual, payout string) {
	t.Helper()
	creditExec(t, f.db, `INSERT INTO usage_records
		(sub2api_usage_id,sub2api_request_id,created_at,sub2api_account_id,sub2api_group_id,sub2api_user_id,sub2api_api_key_id,
		model,total_cost,actual_cost,upstream_id,seller_id,buyer_user_id,buyer_key_id,listing_id,channel,payout,credit_pack_id)
		VALUES($1,$2,now(),301,401,201,501,'gpt-6-astra',$3::numeric,$3::numeric,$4,$5,$6,$7,$8,'direct',$9::numeric,$10)`,
		id, fmt.Sprintf("client:credit-usage-%d", id), actual, f.upstreamID, f.sellerID, f.buyerID, f.keyID, f.listingID, payout, f.packID)
	creditExec(t, f.db, `INSERT INTO edge_attempts(edge_request_id,buyer_key_id,hop,listing_id,sub2api_request_id,http_status,committed,end_reason,started_at,ended_at)
		VALUES($1,$2,1,$3,$4,200,true,'completed',now(),now())`, fmt.Sprintf("credit-edge-%d", id), f.keyID, f.listingID, fmt.Sprintf("credit-usage-%d", id))
}

func (f storeCreditFixture) post(t *testing.T, expected int64) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if n, err := postUsage(ctx, tx, 0); err != nil || n != expected {
		t.Fatalf("post activity usage: n=%d want=%d err=%v", n, expected, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit activity usage: %v", err)
	}
}

func (f storeCreditFixture) assertBalances(t *testing.T, cash, escrow, seller, revenue float64) {
	t.Helper()
	for _, account := range []struct {
		kind string
		id   int64
		want float64
	}{{KindBuyerCash, f.buyerID, cash}, {KindBuyerStoreCredit, f.buyerID, escrow}, {KindSellerPending, f.sellerID, seller}, {KindPlatformRevenue, 0, revenue}} {
		var got float64
		if err := f.db.QueryRow(context.Background(), `SELECT -COALESCE(sum(e.amount),0)::float8 FROM ledger_accounts a
			LEFT JOIN ledger_entries e ON e.account_id=a.id WHERE a.kind=$1 AND a.owner_id=$2`, account.kind, account.id).Scan(&got); err != nil || math.Abs(got-account.want) > 1e-9 {
			t.Fatalf("account %s=%v, want %v: %v", account.kind, got, account.want, err)
		}
	}
	var unbalanced int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM(SELECT txn_id FROM ledger_entries GROUP BY txn_id HAVING sum(amount)<>0) x`).Scan(&unbalanced); err != nil || unbalanced != 0 {
		t.Fatalf("unbalanced transactions=%d: %v", unbalanced, err)
	}
}

func (f storeCreditFixture) assertPack(t *testing.T, usd, units, prepaid, status string) {
	t.Helper()
	var matches bool
	err := f.db.QueryRow(context.Background(), `SELECT remaining_usd=$2::numeric AND remaining_balance_units=$3::numeric
		AND prepaid_remaining_cny=$4::numeric AND status=$5 FROM store_credit_packs WHERE id=$1`, f.packID, usd, units, prepaid, status).Scan(&matches)
	if err != nil || !matches {
		p, _ := f.funds.GetCreditPack(context.Background(), f.buyerID, f.packID)
		t.Fatalf("pack want USD=%s units=%s prepaid=%s status=%s, got %+v: %v", usd, units, prepaid, status, p, err)
	}
}

func (f storeCreditFixture) assertAllocation(t *testing.T, usageID int64, usd, units, prepaid, cash, payout string) {
	t.Helper()
	var matches bool
	err := f.db.QueryRow(context.Background(), `SELECT a.covered_usd=$2::numeric AND a.covered_balance_units=$3::numeric
		AND a.prepaid_cny=$4::numeric AND a.cash_cny=$5::numeric AND ur.payout=$6::numeric
		AND ur.credit_covered_units=$3::numeric AND ur.credit_prepaid_cny=$4::numeric AND ur.cash_cost=$5::numeric
		FROM store_credit_allocations a JOIN usage_records ur ON ur.sub2api_usage_id=a.usage_id
		WHERE a.usage_id=$1`, usageID, usd, units, prepaid, cash, payout).Scan(&matches)
	if err != nil || !matches {
		t.Fatalf("usage %d allocation differs from USD=%s units=%s prepaid=%s cash=%s payout=%s: %v", usageID, usd, units, prepaid, cash, payout, err)
	}
}

func (f storeCreditFixture) assertOp(t *testing.T, key, opType, target, amount string) {
	t.Helper()
	var count int
	err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM sub2api_ops WHERE idempotency_key=$1
		AND op_type=$2 AND target=$3 AND (payload->>'amount')::numeric=$4::numeric`, key, opType, target, amount).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("outbox %s want exactly one %s %s amount=%s: count=%d err=%v", key, opType, target, amount, count, err)
	}
}

func TestStoreCreditsConsumeAndCashRemainder(t *testing.T) {
	f := newStoreCreditFixture(t)
	f.addUsage(t, 1, "3.5", "2.8")
	f.post(t, 1)
	f.assertAllocation(t, 1, "0.5", "3.5", "1.5", "0", "1.2")
	f.assertPack(t, "9.5", "66.5", "28.5", "active")
	f.assertBalances(t, 70, 28.5, 1.2, 0.3)
	summary, err := PlatformLedger(context.Background(), f.db)
	if err != nil || summary.BuyerStoreCredit != 28.5 || summary.BuyerBalances != 98.5 {
		t.Fatalf("platform should report cash plus prepaid liability: %+v %v", summary, err)
	}
	f.addUsage(t, 2, "68", "54.4")
	f.post(t, 1)
	f.assertAllocation(t, 2, "9.5", "66.5", "28.5", "1.5", "24")
	f.assertPack(t, "0", "0", "0", "depleted")
	f.assertBalances(t, 68.5, 0, 25.2, 6.3)
	f.assertOp(t, "tm-credit-cash-2", "user.balance_subtract", "user:101", "1.5")
	f.assertOp(t, "tm-credit-restore-2", "user.balance_add", "user:201", "1.5")
	f.post(t, 0)
	f.assertBalances(t, 68.5, 0, 25.2, 6.3)
	f.assertOp(t, "tm-credit-cash-2", "user.balance_subtract", "user:101", "1.5")
	f.assertOp(t, "tm-credit-restore-2", "user.balance_add", "user:201", "1.5")
	f.assertReports(t)
}

func (f storeCreditFixture) assertReports(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	items, err := BuyerUsage(ctx, f.db, f.buyerID, UsageFilter{}, 10, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("buyer usage items=%+v err=%v", items, err)
	}
	for _, item := range items {
		cost, cash, prepaid, usd, units := 1.5, 0.0, 1.5, 0.5, 3.5
		if item.SubAPIUsageID == 2 {
			cost, cash, prepaid, usd, units = 30, 1.5, 28.5, 9.5, 66.5
		}
		if item.Cost != cost || item.CashCost != cash || item.CreditPrepaidCNY != prepaid || item.CreditCoveredUSD != usd || item.CreditCoveredUnits != units || item.CreditPackID == nil || *item.CreditPackID != f.packID {
			t.Fatalf("buyer usage should display actual paid cost and allocation: %+v", item)
		}
		for _, requestID := range []string{fmt.Sprintf("credit-edge-%d", item.SubAPIUsageID), fmt.Sprintf("client:credit-usage-%d", item.SubAPIUsageID)} {
			details, err := RequestDetail(ctx, f.db, f.buyerID, requestID)
			if err != nil || len(details) != 1 {
				t.Fatalf("request details=%+v err=%v", details, err)
			}
			d := details[0]
			if !d.Charged || d.Cost != cost || d.CashCost != cash || d.CreditPrepaidCNY != prepaid || d.CreditCoveredUSD != usd || d.CreditCoveredUnits != units || d.CreditPackID == nil || *d.CreditPackID != f.packID {
				t.Fatalf("request details should display actual paid cost and allocation: %+v", d)
			}
		}
	}
	bills, err := BuyerBills(ctx, f.db, f.buyerID, 1)
	if err != nil || len(bills) != 1 {
		t.Fatalf("buyer bills=%+v err=%v", bills, err)
	}
	b := bills[0]
	if b.Requests != 2 || b.Cost != 31.5 || b.CashCost != 1.5 || b.CreditPrepaidCNY != 30 || b.CreditCoveredUSD != 10 || b.CreditCoveredUnits != 70 {
		t.Fatalf("buyer bill should sum actual paid cost and allocations: %+v", b)
	}
}

func TestStoreCreditsScopeCannotCoverOtherSellerPlatformOrModel(t *testing.T) {
	for _, scope := range []string{"seller", "platform", "model"} {
		t.Run(scope, func(t *testing.T) {
			f := newStoreCreditFixture(t)
			f.addUsage(t, 1, "3.5", "2.8")
			switch scope {
			case "seller":
				creditExec(t, f.db, `UPDATE upstreams SET seller_id=$2 WHERE id=$1`, f.upstreamID, f.otherSellerID)
				creditExec(t, f.db, `UPDATE usage_records SET seller_id=$1 WHERE sub2api_usage_id=1`, f.otherSellerID)
			case "platform":
				creditExec(t, f.db, `UPDATE upstreams SET platform='anthropic' WHERE id=$1`, f.upstreamID)
			case "model":
				creditExec(t, f.db, `UPDATE usage_records SET model='gpt-other' WHERE sub2api_usage_id=1`)
			}
			f.post(t, 1)
			f.assertAllocation(t, 1, "0", "0", "0", "3.5", "2.8")
			f.assertPack(t, "10", "70", "30", "active")
			if scope == "seller" {
				f.assertBalances(t, 66.5, 30, 0, 0.7)
				balance, err := Balances(context.Background(), f.db, "seller", f.otherSellerID)
				if err != nil || balance[KindSellerPending] != 2.8 {
					t.Fatalf("other seller should receive only cash payment: %+v %v", balance, err)
				}
			} else {
				f.assertBalances(t, 66.5, 30, 2.8, 0.7)
			}
			f.assertOp(t, "tm-credit-cash-1", "user.balance_subtract", "user:101", "3.5")
			f.assertOp(t, "tm-credit-restore-1", "user.balance_add", "user:201", "3.5")
		})
	}
}

func TestStoreCreditsRefundOnlyUnusedPaidAmountAndRetry(t *testing.T) {
	f := newStoreCreditFixture(t)
	f.addUsage(t, 1, "3.5", "2.8")
	f.post(t, 1)
	ctx := context.Background()
	for range 2 {
		_, err := f.funds.RefundCreditPack(ctx, f.buyerID, f.packID)
		var he *httpx.Error
		if !errors.As(err, &he) || he.Status != 403 || he.Code != "BUYER_REFUNDS_DISABLED" {
			t.Fatalf("buyer credit-pack refund must be disabled: %v", err)
		}
	}
	f.assertPack(t, "9.5", "66.5", "28.5", "active")
	f.assertBalances(t, 70, 28.5, 1.2, 0.3)
	var operations, refunds int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM sub2api_ops WHERE idempotency_key LIKE 'tm-credit-refund-%'`).Scan(&operations); err != nil || operations != 0 {
		t.Fatalf("disabled refund must not enqueue ops: operations=%d err=%v", operations, err)
	}
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM ledger_txns WHERE ref=$1`, fmt.Sprintf("store-credit-refund:%d", f.packID)).Scan(&refunds); err != nil || refunds != 0 {
		t.Fatalf("disabled refund must not post ledger: %d %v", refunds, err)
	}
}

// 与真实 sub2api 一致，fixture 余额仅保留8位；消费日志仍保留10位。
// 买家不可退款政策下，精度追回路径不再执行，仅断言各余额形态下入口均被拒绝。
func TestStoreCreditsRefundReclaimsRemotePrecision(t *testing.T) {
	for _, mode := range []string{"legacy", "quantized", "unexplained"} {
		t.Run(mode, func(t *testing.T) {
			f := newStoreCreditFixture(t)
			ctx := context.Background()
			for id := int64(1); id <= 3; id++ {
				f.addUsage(t, id, "0.1234567850", "0.1")
				if mode == "quantized" {
					creditExec(t, f.replicaDB, `UPDATE users SET balance=balance-round(0.1234567850::numeric,8) WHERE id=201`)
				} else {
					creditExec(t, f.replicaDB, `UPDATE users SET balance=balance-0.1234567850::numeric WHERE id=201`)
				}
			}
			f.post(t, 3)
			if mode == "unexplained" {
				creditExec(t, f.replicaDB, `UPDATE users SET balance=balance-0.001 WHERE id=201`)
			}
			_, err := f.funds.RefundCreditPack(ctx, f.buyerID, f.packID)
			var he *httpx.Error
			if !errors.As(err, &he) || he.Status != 403 || he.Code != "BUYER_REFUNDS_DISABLED" {
				t.Fatalf("buyer credit-pack refund must be disabled: %v", err)
			}
			var count int
			if err := f.db.QueryRow(ctx, `SELECT count(*) FROM sub2api_ops WHERE idempotency_key=$1`, fmt.Sprintf("tm-credit-refund-debit-%d", f.packID)).Scan(&count); err != nil || count != 0 {
				t.Fatalf("disabled refund must not enqueue debit: count=%d err=%v", count, err)
			}
		})
	}
}

func TestStoreCreditsRejectUnrepresentableFunding(t *testing.T) {
	for _, rate := range []float64{0.00000001, 1.00000001} {
		t.Run(fmt.Sprintf("rate_%g", rate), func(t *testing.T) {
			f := newStoreCreditFixture(t)
			ctx := context.Background()
			var offerID int64
			if err := f.db.QueryRow(ctx, `INSERT INTO store_credit_offers(seller_id,title,price_cny,face_value_usd)
 VALUES($1,'精度边界',1,0.01) RETURNING id`, f.sellerID).Scan(&offerID); err != nil {
				t.Fatal(err)
			}
			f.funds.Settings = func(context.Context) (settings.Settings, error) {
				st := settings.Defaults()
				st.CreditUSDToBalanceRate = rate
				return st, nil
			}
			_, err := f.funds.PurchaseCreditPack(ctx, f.buyerID, PurchaseCreditPackInput{OfferID: offerID, IdempotencyKey: "precision"})
			var he *httpx.Error
			if !errors.As(err, &he) || he.Code != "INVALID_CREDIT_CONVERSION" {
				t.Fatalf("unrepresentable funding should return conversion error: %v", err)
			}
			var count int
			if err := f.db.QueryRow(ctx, `SELECT count(*) FROM store_credit_packs WHERE buyer_id=$1`, f.buyerID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("invalid conversion created another pack: count=%d err=%v", count, err)
			}
			f.assertBalances(t, 70, 30, 0, 0)
		})
	}
}

func TestStoreCreditsLedgerFailureRollsBackAllocation(t *testing.T) {
	f := newStoreCreditFixture(t)
	f.addUsage(t, 1, "3.5", "2.8")
	creditExec(t, f.db, `CREATE FUNCTION fail_credit_usage_fixture() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF (SELECT type FROM ledger_txns WHERE id=NEW.txn_id)='usage' THEN RAISE EXCEPTION 'simulated credit ledger failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER fail_credit_usage_fixture BEFORE INSERT ON ledger_entries FOR EACH ROW EXECUTE FUNCTION fail_credit_usage_fixture()`)
	ctx := context.Background()
	tx, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := postUsage(ctx, tx, 0); err == nil {
		t.Fatal("expected simulated ledger failure")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	f.assertPack(t, "10", "70", "30", "active")
	f.assertBalances(t, 70, 30, 0, 0)
	var matches bool
	if err := f.db.QueryRow(ctx, `SELECT ledger_txn_id IS NULL AND cash_cost IS NULL AND payout=2.8 AND credit_prepaid_cny=0
		AND NOT EXISTS(SELECT 1 FROM store_credit_allocations) AND NOT EXISTS(SELECT 1 FROM ledger_txns WHERE type='usage')
		AND NOT EXISTS(SELECT 1 FROM settlement_lines) FROM usage_records WHERE sub2api_usage_id=1`).Scan(&matches); err != nil || !matches {
		t.Fatalf("ledger failure should roll back usage, allocation and settlement: matches=%v err=%v", matches, err)
	}
	creditExec(t, f.db, `DROP TRIGGER fail_credit_usage_fixture ON ledger_entries`)
	f.post(t, 1)
	f.assertAllocation(t, 1, "0.5", "3.5", "1.5", "0", "1.2")
	f.assertPack(t, "9.5", "66.5", "28.5", "active")
	f.assertBalances(t, 70, 28.5, 1.2, 0.3)
}

func TestStoreCreditsConcurrentConsumptionNeverExceedsRemainder(t *testing.T) {
	f := newStoreCreditFixture(t)
	f.addUsage(t, 1, "50", "40")
	f.addUsage(t, 2, "50", "40")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	blocker, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT id FROM store_credit_packs WHERE id=$1 FOR UPDATE`, f.packID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		n   int64
		err error
	}
	results := make(chan result, 2)
	for _, fromID := range []int64{0, 1} {
		go func() {
			tx, err := f.db.Begin(ctx)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			n, err := postUsage(ctx, tx, fromID)
			if err == nil {
				err = tx.Commit(ctx)
			}
			results <- result{n: n, err: err}
		}()
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND (query LIKE '%store_credit_packs%' OR query LIKE '%pg_advisory_xact_lock(1953325673)%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent credit usage did not reach competing locks")
		case <-ticker.C:
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var posted int64
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		posted += r.n
	}
	if posted != 2 {
		t.Fatalf("two distinct usages must post exactly once each, got %d", posted)
	}
	f.assertPack(t, "0", "0", "0", "depleted")
	f.assertBalances(t, 40, 0, 48, 12)
	var matches bool
	if err := f.db.QueryRow(ctx, `SELECT count(*)=2 AND sum(covered_usd)=10 AND sum(covered_balance_units)=70
		AND sum(prepaid_cny)=30 AND sum(cash_cny)=30 FROM store_credit_allocations WHERE pack_id=$1`, f.packID).Scan(&matches); err != nil || !matches {
		t.Fatalf("concurrent allocation must not overdraw USD, units, or actual paid funds: matches=%v err=%v", matches, err)
	}
	for _, outbox := range []struct{ prefix, target, typ string }{
		{"tm-credit-cash-%", "user:101", "user.balance_subtract"},
		{"tm-credit-restore-%", "user:201", "user.balance_add"},
	} {
		if err := f.db.QueryRow(ctx, `SELECT sum((payload->>'amount')::numeric)=30
			FROM sub2api_ops WHERE idempotency_key LIKE $1 AND target=$2 AND op_type=$3`, outbox.prefix, outbox.target, outbox.typ).Scan(&matches); err != nil || !matches {
			t.Fatalf("concurrent cash compensation should total only the remainder: matches=%v err=%v", matches, err)
		}
	}
	f.post(t, 0)
	f.assertBalances(t, 40, 0, 48, 12)
}
