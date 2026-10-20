//go:build integration

package edge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/compat"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/sub2api/userclient"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/internal/testutil/fakesub2api"
	"github.com/yx-token/token-market/market/migrations"
	"github.com/jackc/pgx/v5"
)

// 真实 PostgreSQL + sub2api v0.2.14 迁移 + 真实 Admin/User/网关替身，
// 验证独立资金开通、作用域、并发锁与耗尽回退，不只替换选包函数。
func TestCreditRuntimeIndependentWalletScopeAndFallback(t *testing.T) {
	repo := os.Getenv("TM_SUB2API_REPO")
	if repo == "" {
		t.Skip("TM_SUB2API_REPO not set")
	}
	ctx := t.Context()
	_, market := testutil.NewDatabase(t)
	if _, err := db.Migrate(ctx, market, migrations.FS); err != nil {
		t.Fatal(err)
	}
	subDSN, subDB := testutil.NewDatabase(t)
	set, err := testutil.LoadGitMigrations(ctx, repo, "v0.2.14")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, subDSN)
	if err != nil {
		t.Fatal(err)
	}
	err = compat.ApplySub2APIMigrations(ctx, conn, set)
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake := fakesub2api.New(subDB, "admin-test")
	t.Cleanup(fake.Close)
	e := newEnv(t)
	e.srv.cfg.GatewayURL, _ = url.Parse(fake.URL())
	e.srv.cfg.FirstByteTimeout = 5 * time.Second
	admin := adminclient.New(fake.URL(), "admin-test", 5*time.Second)
	reader, err := replica.New(subDB)
	if err != nil {
		t.Fatal(err)
	}
	st := func(context.Context) (settings.Settings, error) { return settings.Defaults(), nil }
	shadow := &buyer.ShadowService{DB: market, Admin: admin,
		User: userclient.New(fake.URL(), 5*time.Second, nil), Replica: reader,
		Keyring: e.kr, Domain: "credit-test.local", Settings: st, Log: slog.Default()}
	keys := &buyer.KeyService{DB: market, Shadow: shadow, Keyring: e.kr, Edge: e.store, Settings: st, Log: slog.Default()}
	sender := ops.NewSender(market, ops.SenderConfig{})
	opshandlers.Register(sender, admin)

	scan := func(row pgx.Row, dest ...any) {
		t.Helper()
		if err := row.Scan(dest...); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := market.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE vendor_configs SET enabled=true WHERE platform='anthropic'`)
	var buyerID, sellerID, otherSellerID int64
	scan(market.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('credit-buyer@test','x') RETURNING id`), &buyerID)
	scan(market.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('credit-seller@test','x') RETURNING id`), &sellerID)
	scan(market.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('other-seller@test','x') RETURNING id`), &otherSellerID)
	var keyID int64
	hash := sha256.Sum256([]byte("tm-buyer"))
	scan(market.QueryRow(ctx, `INSERT INTO buyer_keys(user_id,name,key_hash,key_prefix,route_type)
		VALUES($1,'credit-test',$2,'tm-buyer','chain') RETURNING id`, buyerID, hash[:]), &keyID)
	var listingIDs []int64
	for i, seller := range []int64{sellerID, sellerID, otherSellerID} {
		group, err := admin.CreateGroup(ctx, adminclient.CreateGroupInput{Name: fmt.Sprintf("tm-credit-listing-%d", i), Platform: "anthropic", RateMultiplier: 2, IsExclusive: true})
		if err != nil {
			t.Fatal(err)
		}
		rate := 1.0
		account, err := admin.CreateAccount(ctx, adminclient.CreateAccountInput{Name: fmt.Sprintf("tm-credit-upstream-%d", i),
			Platform: "anthropic", Type: "apikey", Credentials: map[string]any{"api_key": "test"},
			Concurrency: 5, Priority: 1, RateMultiplier: &rate, GroupIDs: []int64{group.ID}}, fmt.Sprintf("credit-account-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		var upstreamID, listingID int64
		scan(market.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,status,sub2api_account_id)
			VALUES($1,'anthropic','apikey','listed',$2) RETURNING id`, seller, account.ID), &upstreamID)
		scan(market.QueryRow(ctx, `INSERT INTO listings(upstream_id,seller_id,mode,status,multiplier,listing_group_id,listing_group_rate)
			VALUES($1,$2,'consignment','active',1,$3,2) RETURNING id`, upstreamID, seller, group.ID), &listingID)
		listingIDs = append(listingIDs, listingID)
		exec(`INSERT INTO buyer_key_hops(key_id,position,listing_id) VALUES($1,$2,$3)`, keyID, i+1, listingID)
		exec(`INSERT INTO buyer_merchants(user_id,seller_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, buyerID, seller)
	}
	if err := keys.Provision(ctx, keyID); err != nil {
		t.Fatal(err)
	}
	if err := e.cache.Load(ctx); err != nil {
		t.Fatal(err)
	}
	baseCfg := *e.cache.Key(edgecfg.HashKey("tm-buyer"))
	if len(baseCfg.Hops) != 3 {
		t.Fatalf("expected three ordinary hops: %+v", baseCfg)
	}
	setHop := func(i int) {
		cfg := baseCfg
		cfg.Hops = []edgecfg.Hop{baseCfg.Hops[i]}
		e.put(t, "tm-buyer", cfg)
	}
	var offerID, packID, purchaseID int64
	scan(market.QueryRow(ctx, `INSERT INTO store_credit_offers(seller_id,title,price_cny,face_value_usd,allowed_platforms,allowed_models)
		VALUES($1,'test-credit',1,1,'{anthropic}','{match}') RETURNING id`, sellerID), &offerID)
	purchaseID, err = ops.Enqueue(ctx, market, ops.NewOp{Type: opshandlers.TypeUserBalanceSubtract, Target: "purchase:test", Payload: map[string]any{}, IdempotencyKey: "credit-test-purchase"})
	if err != nil {
		t.Fatal(err)
	}
	scan(market.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,
		remaining_usd,remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,allowed_platforms,allowed_models,idempotency_key,purchase_op_id)
		VALUES($1,$2,$3,'test-credit',1,1,1,2,1,2,'{anthropic}','{match}','test-buy',$4) RETURNING id`, offerID, buyerID, sellerID, purchaseID), &packID)
	provision := func() {
		t.Helper()
		if err := keys.ProvisionCreditPacks(ctx); err != nil {
			t.Fatal(err)
		}
	}
	provision()
	var count int
	scan(market.QueryRow(ctx, `SELECT count(*) FROM shadow_identities WHERE kind='credit_pack'`), &count)
	if count != 0 {
		t.Fatal("pending purchase must not fund a credit shadow")
	}
	exec(`UPDATE sub2api_ops SET status='done' WHERE id=$1`, purchaseID)
	provision()
	var packStatus string
	scan(market.QueryRow(ctx, `SELECT status FROM store_credit_packs WHERE id=$1`, packID), &packStatus)
	if packStatus != "provisioning" {
		t.Fatal("pending funding must not activate the pack")
	}
	if _, err := sender.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	provision()
	provision() // 重复开通不能再充值或多建 key。
	if err := e.cache.Load(ctx); err != nil {
		t.Fatal(err)
	}
	baseCfg = *e.cache.Key(edgecfg.HashKey("tm-buyer"))
	if !baseCfg.CreditCandidate {
		t.Fatal("provision must publish credit candidate flag")
	}
	var ordinaryUserID, creditUserID int64
	scan(market.QueryRow(ctx, `SELECT sub2api_user_id FROM shadow_identities WHERE kind='buyer' AND owner_id=$1`, buyerID), &ordinaryUserID)
	scan(market.QueryRow(ctx, `SELECT sub2api_user_id FROM shadow_identities WHERE kind='credit_pack' AND owner_id=$1`, packID), &creditUserID)
	balance := func(id int64) float64 {
		var amount float64
		scan(subDB.QueryRow(ctx, `SELECT balance::float8 FROM users WHERE id=$1`, id), &amount)
		return amount
	}
	if balance(ordinaryUserID) != 0 || balance(creditUserID) != 2 {
		t.Fatalf("funds leaked or duplicated: ordinary=%v credit=%v", balance(ordinaryUserID), balance(creditUserID))
	}
	scan(market.QueryRow(ctx, `SELECT count(*) FROM store_credit_keys WHERE pack_id=$1`, packID), &count)
	if count != 2 {
		t.Fatalf("expected one key for each of two own-store listings, got %d", count)
	}
	selector := &DBCreditSelector{DB: market, Keyring: e.kr}
	e.srv.SetCreditSelector(selector)
	setHop(0)
	request := func(model string) int {
		t.Helper()
		return e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`).Code
	}
	if got := request("other-model"); got != 403 {
		t.Fatalf("nonmatching model must use zero cash and fail: %d", got)
	}
	exec(`UPDATE store_credit_packs SET allowed_platforms='{openai}' WHERE id=$1`, packID)
	if got := request("match"); got != 403 {
		t.Fatalf("nonmatching platform must fail: %d", got)
	}
	exec(`UPDATE store_credit_packs SET allowed_platforms='{anthropic}' WHERE id=$1`, packID)
	setHop(2)
	if got := request("match"); got != 403 || balance(creditUserID) != 2 {
		t.Fatalf("other store used credit: status=%d balance=%v", got, balance(creditUserID))
	}
	setHop(0)
	for _, restriction := range []struct{ apply, reset string }{
		{`UPDATE upstreams SET health='unavailable' WHERE seller_id=$1`, `UPDATE upstreams SET health='unknown' WHERE seller_id=$1`},
		{`UPDATE upstreams SET model_whitelist='{other-model}' WHERE seller_id=$1`, `UPDATE upstreams SET model_whitelist='{}' WHERE seller_id=$1`},
		{`UPDATE listings SET status='paused' WHERE seller_id=$1`, `UPDATE listings SET status='active' WHERE seller_id=$1`},
	} {
		exec(restriction.apply, sellerID)
		if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
			t.Fatalf("stale cached route bypassed current availability (%s): %v %v", restriction.apply, lease, err)
		}
		exec(restriction.reset, sellerID)
	}
	exec(`DELETE FROM buyer_merchants WHERE user_id=$1 AND seller_id=$2`, buyerID, sellerID)
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("removed merchant bypassed current authorization: %v %v", lease, err)
	}
	exec(`INSERT INTO buyer_merchants(user_id,seller_id) VALUES($1,$2)`, buyerID, sellerID)
	exec(`UPDATE shadow_identities SET keys_locked=true WHERE kind='buyer' AND owner_id=$1`, buyerID)
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("ordinary buyer refund lock bypassed: %v %v", lease, err)
	}
	exec(`UPDATE shadow_identities SET keys_locked=false WHERE kind='buyer' AND owner_id=$1`, buyerID)
	exec(`INSERT INTO quota_packs(listing_id,buyer_id,buyer_key_id,face_value,remaining,status,paid_amount)
		VALUES($1,$2,$3,1,1,'active',1)`, listingIDs[0], buyerID, keyID)
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("legacy quota pack dedicated key must not spend new credits: %v %v", lease, err)
	}
	exec(`DELETE FROM quota_packs WHERE buyer_key_id=$1`, keyID)
	debtOp, err := ops.Enqueue(ctx, market, ops.NewOp{Type: opshandlers.TypeUserBalanceSubtract,
		Target: fmt.Sprintf("user:%d", ordinaryUserID), Payload: map[string]any{}, IdempotencyKey: "tm-credit-cash-123"})
	if err != nil {
		t.Fatal(err)
	}
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("unpaid previous credit overage admitted: %v %v", lease, err)
	}
	exec(`UPDATE sub2api_ops SET status='dead' WHERE id=$1`, debtOp)
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("dead overage debit must still block: %v %v", lease, err)
	}
	exec(`UPDATE sub2api_ops SET status='done' WHERE id=$1`, debtOp)
	lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0)
	if err != nil || lease == nil {
		t.Fatalf("eligible package not selected: %v", err)
	}
	lockedCtx, cancel := context.WithTimeout(ctx, 70*time.Millisecond)
	_, lockErr := selector.Select(lockedCtx, &baseCfg, baseCfg.Hops[0], "match", 0)
	cancel()
	if lockErr == nil {
		t.Fatal("same credit pack must not admit simultaneous requests")
	}
	lease.Release()
	lease.Release() // 清理必须幂等。
	if got := request("match"); got != 200 {
		t.Fatalf("zero ordinary balance should still spend credit: %d", got)
	}
	setHop(1)
	// 普通路由引用 pack 挂单仍属于本店范围；仅旧 quota_packs 专用 Key 被排除。
	exec(`UPDATE listings SET mode='pack',sku_kind='pack' WHERE id=$1`, listingIDs[1])
	provision()
	if got := request("match"); got != 200 {
		t.Fatalf("credit must work on another listing of same store: %d", got)
	}
	if balance(ordinaryUserID) != 0 || math.Abs(balance(creditUserID)+0.4) > 1e-8 {
		t.Fatalf("group倍率2 must consume1.2 per call, ordinary=%v credit=%v", balance(ordinaryUserID), balance(creditUserID))
	}
	var secondPackID, secondCreditUserID int64
	scan(market.QueryRow(ctx, `INSERT INTO store_credit_packs(offer_id,buyer_id,seller_id,title,price_cny,face_value_usd,
		remaining_usd,remaining_balance_units,prepaid_remaining_cny,usd_to_balance_rate,allowed_platforms,allowed_models,idempotency_key,purchase_op_id)
		VALUES($1,$2,$3,'next-credit',1,1,1,2,1,2,'{anthropic}','{match}','test-buy-next',$4) RETURNING id`, offerID, buyerID, sellerID, purchaseID), &secondPackID)
	provision()
	if _, err := sender.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	provision()
	scan(market.QueryRow(ctx, `SELECT sub2api_user_id FROM shadow_identities WHERE kind='credit_pack' AND owner_id=$1`, secondPackID), &secondCreditUserID)
	if got := request("match"); got != 200 || balance(ordinaryUserID) != 0 || math.Abs(balance(secondCreditUserID)-0.8) > 1e-8 {
		t.Fatalf("first pack denial must try second funded pack before zero cash: status=%d cash=%v credit2=%v", got, balance(ordinaryUserID), balance(secondCreditUserID))
	}
	exec(`UPDATE store_credit_packs SET status='refunding' WHERE id=$1`, secondPackID)
	if _, err := admin.UpdateBalance(ctx, ordinaryUserID, adminclient.BalanceAdd, 5, "cash fallback test", "credit-test-cash-fallback"); err != nil {
		t.Fatal(err)
	}
	// 故意不归集 usage，模拟本地额度尚未刷新；sub2api 的准入余额码仍应回退原 key。
	if got := request("match"); got != 200 || math.Abs(balance(ordinaryUserID)-3.8) > 1e-8 {
		t.Fatalf("depleted shadow must fall back to ordinary cash: status=%d cash=%v", got, balance(ordinaryUserID))
	}
	var creditUsage, cashUsage int
	scan(subDB.QueryRow(ctx, `SELECT count(*) FROM usage_logs WHERE user_id IN($1,$2)`, creditUserID, secondCreditUserID), &creditUsage)
	scan(subDB.QueryRow(ctx, `SELECT count(*) FROM usage_logs WHERE user_id=$1`, ordinaryUserID), &cashUsage)
	if creditUsage != 3 || cashUsage != 1 {
		t.Fatalf("fallback must not double charge: credit usages=%d cash usages=%d", creditUsage, cashUsage)
	}
	exec(`UPDATE store_credit_packs SET status='refunding' WHERE id=$1`, packID)
	if lease, err := selector.Select(ctx, &baseCfg, baseCfg.Hops[0], "match", 0); err != nil || lease != nil {
		t.Fatalf("refunding pack still selected: %+v %v", lease, err)
	}
	provision()
	scan(market.QueryRow(ctx, `SELECT status FROM store_credit_packs WHERE id=$1`, packID), &packStatus)
	if packStatus != "refunding" {
		t.Fatal("provision worker must not resurrect refunding pack")
	}
}
