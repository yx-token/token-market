//go:build integration

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/billing"
)

// 覆盖全部 HTTP 接口与后台任务：多厂商（Claude OAuth、ChatGPT OAuth、Kimi Coding Plan）、两种挂单模式、
// 默认分组与指定卖家路由，以及管理后台的各项操作。主要用来发现 SQL 与接线错误（不应出现 5xx）。
func TestAPISurface(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")

	// 4xx 是业务拒绝，5xx 说明代码有问题
	no5xx := func(c *client, method, path string, body any) int {
		t.Helper()
		status, raw := c.call(method, path, body, nil)
		if status >= 500 {
			t.Fatalf("%s %s: http %d: %s", method, path, status, raw)
		}
		return status
	}

	for _, p := range []string{"anthropic", "openai", "kimi"} {
		admin.must("PUT", "/api/admin/vendors/"+p, map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	}
	for i := 1; i <= 6; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "http", "host": "10.0.1.1", "port": 4000 + i, "region": "us"}, nil)
	}
	var anthropicPool, composite idOnly
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-claude", "platform": "anthropic", "sell_multiplier": 0.8}, &anthropicPool)
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-gpt", "platform": "openai", "sell_multiplier": 0.7}, nil)
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-all", "platform": "composite", "sell_multiplier": 0.9}, &composite)
	admin.must("PUT", "/api/admin/settings", map[string]any{"price_change_cooldown_minutes": 0, "withdraw_min": 0.01}, nil)

	// 卖家 A：Claude 寄售，自留 20%
	sellerA := e.newUser("a@market.test")
	var upA idOnly
	sellerA.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upA)
	sellerA.must("PUT", fmt.Sprintf("/api/seller/upstreams/%d/settings", upA.ID), map[string]any{"self_reserve_percent": 20}, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", upA.ID), nil, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upA.ID), map[string]string{"code": "https://cb.test/callback?code=erin&state=x"}, nil)
	e.passOnboarding(upA.ID)
	var la struct {
		Listing idOnly `json:"listing"`
	}
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", upA.ID), map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}}, &la)

	// 卖家 B：ChatGPT 回收（需要关闭训练数据使用）
	sellerB := e.newUser("b@market.test")
	var upB struct {
		ID        int64 `json:"id"`
		PrivacyOK *bool `json:"privacy_ok"`
	}
	sellerB.must("POST", "/api/seller/upstreams", map[string]string{"platform": "openai", "auth_type": "oauth"}, &upB)
	var startB struct {
		AuthURL string `json:"auth_url"`
	}
	sellerB.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", upB.ID), nil, &startB)
	// OpenAI 需要回调地址中的 state；状态不一致必须拒绝
	sellerB.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upB.ID), map[string]string{"code": "https://cb.test/cb?code=frank&state=wrong"})
	state := startB.AuthURL[len("https://auth.example.test/authorize?state="):]
	sellerB.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upB.ID), map[string]string{"code": "https://cb.test/cb?code=frank&state=" + state}, &upB)
	if upB.PrivacyOK == nil || !*upB.PrivacyOK {
		t.Fatalf("openai oauth account should have privacy enabled: %+v", upB)
	}
	e.passOnboarding(upB.ID) // Responses 协议的探测
	var lb struct {
		Listing idOnly `json:"listing"`
	}
	sellerB.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", upB.ID), map[string]any{"mode": "buyback"}, &lb)
	sellerB.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/quote", upB.ID), nil, nil)
	sellerB.must("POST", fmt.Sprintf("/api/seller/listings/%d/accept-quote", lb.Listing.ID), nil, nil)

	// 卖家 C：Kimi Coding Plan
	sellerC := e.newUser("c@market.test")
	var upC struct {
		ID     int64   `json:"id"`
		Status string  `json:"status"`
		Tier   *string `json:"tier"`
	}
	sellerC.must("POST", "/api/seller/upstreams", map[string]string{"platform": "kimi", "auth_type": "coding"}, &upC)
	sellerC.expect(400, "POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", upC.ID), map[string]string{"api_key": "short"})
	sellerC.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/coding-key", upC.ID), map[string]string{"api_key": "sk-kimi-coding-0123456789"}, &upC)
	if upC.Status != "verifying" || upC.Tier == nil || *upC.Tier != "pro" {
		t.Fatalf("coding plan upstream: %+v", upC)
	}
	e.passOnboarding(upC.ID) // Chat Completions 协议的探测
	var lc struct {
		Listing idOnly `json:"listing"`
	}
	sellerC.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", upC.ID), map[string]any{"mode": "consignment", "multiplier": 0.6, "join_default_pool": false, "categories": []string{"coding"}}, &lc)
	e.schedule()

	// 卖家对挂单的各项操作
	sellerA.must("PUT", fmt.Sprintf("/api/seller/listings/%d/price", la.Listing.ID), map[string]any{"multiplier": 0.45}, nil)
	sellerA.must("PUT", fmt.Sprintf("/api/seller/listings/%d/default-pool", la.Listing.ID), map[string]any{"join": false}, nil)
	sellerA.must("PUT", fmt.Sprintf("/api/seller/listings/%d/default-pool", la.Listing.ID), map[string]any{"join": true}, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/listings/%d/pause", la.Listing.ID), nil, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/listings/%d/resume", la.Listing.ID), nil, nil)
	sellerA.expect(404, "GET", fmt.Sprintf("/api/seller/listings/%d", lb.Listing.ID), nil) // 别人的挂单
	e.schedule()

	// 买家
	buyer := e.newUser("buyer@market.test")
	var rc struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 20}, &rc)
	var cancelMe struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 5}, &cancelMe)
	buyer.must("POST", fmt.Sprintf("/api/buyer/recharge/%d/cancel", cancelMe.Order.ID), nil, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/recharges/%d/confirm", rc.Order.ID), map[string]string{"provider_ref": "bank-9"}, nil)
	e.drain()
	buyer.must("GET", fmt.Sprintf("/api/buyer/recharge/%d", rc.Order.ID), nil, nil)

	var poolKey, chainKey, kimiKey struct {
		Key    string `json:"key"`
		Detail idOnly `json:"detail"`
	}
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "pool", "route": map[string]any{"type": "default", "pool": "tm-pool-claude"}}, &poolKey)
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "chain", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"listing_id": la.Listing.ID}, {"pool": "tm-pool-all"}}}}, &chainKey)
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "kimi", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"listing_id": lc.Listing.ID}}}}, &kimiKey)
	// 路由校验：跨平台、默认分组不在最后、回收挂单不能直接指定
	buyer.expect(400, "POST", "/api/buyer/keys", map[string]any{"name": "x", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"listing_id": la.Listing.ID}, {"listing_id": lc.Listing.ID}}}})
	buyer.expect(400, "POST", "/api/buyer/keys", map[string]any{"name": "x", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"pool": "tm-pool-claude"}, {"listing_id": la.Listing.ID}}}})
	buyer.expect(400, "POST", "/api/buyer/keys", map[string]any{"name": "x", "route": map[string]any{"type": "chain",
		"hops": []map[string]any{{"listing_id": lb.Listing.ID}}}})
	e.schedule()
	e.ingestAll() // 上架检测的探测流量

	ed := e.startEdge()
	for _, k := range []string{poolKey.Key, chainKey.Key, kimiKey.Key} {
		if status, h := ed.call(k); status != 200 {
			t.Fatalf("edge call: %d %v", status, h)
		}
	}
	ed.flushAttempts()
	e.app.Ingester.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	if res, _, err := e.app.Ingester.RunOnce(ctx); err != nil || res.Posted != 3 {
		t.Fatalf("ingest: %+v %v", res, err)
	}

	// 授权失效 → 账号不可用 → 卖家重新授权（必须是同一个官方账号）→ 恢复
	var accA int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", upA.ID), &accA)
	if err := e.fake.SetAccountError(accA, "refresh token revoked"); err != nil {
		t.Fatal(err)
	}
	e.schedule()
	health := func() string {
		var h string
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT health FROM upstreams WHERE id = $1", upA.ID), &h)
		return h
	}
	if h := health(); h != "unavailable" {
		t.Fatalf("revoked account should be unavailable: %s", h)
	}
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/reauthorize", upA.ID), nil, nil)
	sellerA.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upA.ID), map[string]string{"code": "mallory"})
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/reauthorize", upA.ID), nil, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upA.ID), map[string]string{"code": "erin"}, nil)
	creds, err := e.fake.AccountCredentials(accA)
	if err != nil || creds["account_scheduling_threshold"] != float64(80) || creds["access_token"] == nil {
		t.Fatalf("reauthorized credentials must keep the window threshold: %v %v", creds, err)
	}
	e.schedule()
	if h := health(); h != "healthy" { // 已通过上架检测的上游，账号恢复后回到正常
		t.Fatalf("health after reauthorization: %s", h)
	}

	// 账号、价格、题库、基线账号
	var refreshed struct {
		Token string `json:"token"`
	}
	buyer.must("POST", "/api/auth/refresh", nil, &refreshed)
	(&client{e: e, token: refreshed.Token}).must("GET", "/api/me", nil, nil)
	var prices []struct {
		Model       string  `json:"model"`
		InputPer1M  float64 `json:"input_per_1m"`
		OutputPer1M float64 `json:"output_per_1m"`
	}
	e.anon().must("GET", "/api/market/prices", nil, &prices)
	if len(prices) != 1 || prices[0].Model != "claude-sonnet-4-5" || !approx(prices[0].InputPer1M, 3) || !approx(prices[0].OutputPer1M, 15) {
		t.Fatalf("official prices: %+v", prices)
	}
	var ref struct {
		Upstream idOnly `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &ref)
	var refs []idOnly
	admin.must("GET", "/api/admin/reference-accounts", nil, &refs)
	if len(refs) != 1 || refs[0].ID != ref.Upstream.ID {
		t.Fatalf("reference accounts: %+v", refs)
	}
	admin.must("DELETE", fmt.Sprintf("/api/admin/reference-accounts/%d", ref.Upstream.ID), nil, nil)
	admin.expect(400, "DELETE", fmt.Sprintf("/api/admin/reference-accounts/%d", upA.ID), nil)

	// Key 管理
	buyer.must("PUT", fmt.Sprintf("/api/buyer/keys/%d", poolKey.Detail.ID), map[string]any{"name": "renamed",
		"route": map[string]any{"type": "chain", "hops": []map[string]any{{"listing_id": la.Listing.ID}, {"pool": "tm-pool-all"}}}}, nil)
	buyer.must("POST", fmt.Sprintf("/api/buyer/keys/%d/disable", chainKey.Detail.ID), nil, nil)
	buyer.must("POST", fmt.Sprintf("/api/buyer/keys/%d/enable", chainKey.Detail.ID), nil, nil)
	buyer.must("DELETE", fmt.Sprintf("/api/buyer/keys/%d", kimiKey.Detail.ID), nil, nil)
	if n, err := e.app.Keys.CleanupDeletedKeys(ctx); err != nil || n != 2 { // 删除的 Key + 改路由后不再使用的 tm-pool-claude 内部 key
		t.Fatalf("cleanup internal keys: %d %v", n, err)
	}

	// 只读接口
	for _, c := range []struct {
		cl   *client
		path string
	}{
		{buyer, "/api/me"}, {buyer, "/api/market/listings?sort=success"}, {buyer, fmt.Sprintf("/api/market/listings/%d", la.Listing.ID)},
		{buyer, "/api/market/pools"}, {buyer, "/api/market/vendors"}, {buyer, "/api/buyer/wallet"}, {buyer, "/api/buyer/refunds"},
		{buyer, "/api/buyer/keys"}, {buyer, fmt.Sprintf("/api/buyer/keys/%d", poolKey.Detail.ID)}, {buyer, "/api/buyer/usage?model=claude-sonnet-4-5"},
		{buyer, fmt.Sprintf("/api/buyer/usage?key_id=%d", chainKey.Detail.ID)}, {buyer, "/api/buyer/bills?days=7"},
		{sellerA, "/api/seller/vendors"}, {sellerA, "/api/seller/upstreams"}, {sellerA, fmt.Sprintf("/api/seller/upstreams/%d", upA.ID)}, {sellerC, fmt.Sprintf("/api/seller/upstreams/%d", upC.ID)},
		{sellerA, fmt.Sprintf("/api/seller/upstreams/%d/reports", upA.ID)}, {sellerA, fmt.Sprintf("/api/seller/upstreams/%d/quality-trend", upA.ID)},
		{sellerA, "/api/seller/listings"}, {sellerA, fmt.Sprintf("/api/seller/listings/%d", la.Listing.ID)}, {sellerA, "/api/seller/earnings"},
		{sellerA, "/api/seller/earnings/lines"}, {sellerA, fmt.Sprintf("/api/seller/earnings/lines?upstream_id=%d", upA.ID)}, {sellerA, "/api/seller/withdrawals"},
		{admin, "/api/admin/settings"}, {admin, "/api/admin/vendors"}, {admin, "/api/admin/proxies"}, {admin, "/api/admin/pools"},
		{admin, "/api/admin/detection-policies"}, {admin, "/api/admin/probe-items"}, {admin, "/api/admin/quality/overview"},
		{admin, "/api/admin/appeals"}, {admin, "/api/admin/upstreams"}, {admin, "/api/admin/upstreams?status=listed"}, {admin, "/api/admin/kyc"},
		{admin, "/api/admin/recharges"}, {admin, "/api/admin/refunds"}, {admin, "/api/admin/withdrawals"}, {admin, "/api/admin/anomalies"},
		{admin, "/api/admin/risk-events"}, {admin, "/api/admin/ledger"}, {admin, "/api/admin/sub2api/status"}, {admin, "/api/admin/audit-logs"},
		{admin, "/api/admin/audit-logs?target=settings"},
	} {
		c.cl.must("GET", c.path, nil, nil)
	}
	// 权限
	buyer.expect(403, "GET", "/api/admin/settings", nil)
	e.anon().expect(401, "GET", "/api/buyer/wallet", nil)

	// 管理后台操作
	admin.must("PUT", fmt.Sprintf("/api/admin/pools/%d", anthropicPool.ID), map[string]any{"name": "tm-pool-claude", "platform": "anthropic", "sell_multiplier": 0.75}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/pools/%d/sync", anthropicPool.ID), nil, nil)
	admin.must("POST", "/api/admin/proxies/6/sync", nil, nil)
	admin.must("POST", "/api/admin/proxies/6/disable", nil, nil)
	admin.must("POST", "/api/admin/proxies/6/enable", nil, nil)
	admin.must("PUT", "/api/admin/detection-policies/kimi", map[string]any{"enabled": true}, nil)
	admin.must("PUT", "/api/admin/detection-policies/kimi?auth_type=coding", map[string]any{"enabled": false}, nil)
	admin.must("PUT", fmt.Sprintf("/api/admin/upstreams/%d/detection-override", upC.ID), map[string]any{"enabled": true, "reason": "spot check"}, nil)
	admin.must("DELETE", fmt.Sprintf("/api/admin/upstreams/%d/detection-override", upC.ID), nil, nil)
	var item idOnly
	admin.must("POST", "/api/admin/probe-items", map[string]any{"suite": "inspection_capability", "check_key": "D07", "category": "math", "prompt": "What is 17*23? Answer with the number only.",
		"grader": map[string]any{"type": "numeric", "number": 391}, "max_tokens": 16, "status": "active"}, &item)
	admin.must("PUT", fmt.Sprintf("/api/admin/probe-items/%d", item.ID), map[string]any{"suite": "inspection_capability", "check_key": "D07", "category": "math", "prompt": "What is 17*23? Reply with digits only.",
		"grader": map[string]any{"type": "numeric", "number": 391}, "max_tokens": 16, "status": "active"}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/probe-items/%d/retire", item.ID), nil, nil)
	admin.expect(404, "POST", fmt.Sprintf("/api/admin/probe-items/%d/retire", item.ID), nil)
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/probe", upC.ID), nil, nil) // 管理员可以对关闭检测的厂商强制检测
	for i := 0; i < 2; i++ {
		if err := e.qualityRound(ctx); err != nil {
			t.Fatal(err)
		}
		e.drain()
	}
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/suspend", upC.ID), map[string]string{"reason": "check"}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/unsuspend", upC.ID), nil, nil)
	admin.must("POST", "/api/admin/adjustments", map[string]any{"buyer_user_id": userID(t, e, "buyer@market.test"), "amount": 1.5, "reason": "compensation"}, nil)
	admin.expect(400, "POST", "/api/admin/adjustments", map[string]any{"buyer_user_id": userID(t, e, "buyer@market.test"), "amount": 1})
	// 不存在的买家用户应被拒绝，避免 Shadow.Ensure 凭空建孤儿账户
	admin.expect(400, "POST", "/api/admin/adjustments", map[string]any{"buyer_user_id": 99999999, "amount": 1, "reason": "no such user"})
	admin.expect(400, "POST", "/api/admin/adjustments", map[string]any{"buyer_user_id": 0, "amount": 1, "reason": "zero id"})
	e.drain()
	sellerC.must("POST", "/api/me/kyc", map[string]string{"real_name": "Carl", "id_number": "110101199001019999"}, nil)
	var pending []map[string]any
	admin.must("GET", "/api/admin/kyc", nil, &pending)
	if len(pending) != 1 {
		t.Fatalf("pending kyc: %+v", pending)
	}
	admin.must("POST", fmt.Sprintf("/api/admin/kyc/%d/review", userID(t, e, "c@market.test")), map[string]any{"approve": false, "note": "blurry"}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/users/%d/status", userID(t, e, "c@market.test")), map[string]string{"status": "frozen"}, nil)
	sellerC.expect(403, "GET", "/api/me", nil)
	admin.must("POST", fmt.Sprintf("/api/admin/users/%d/status", userID(t, e, "c@market.test")), map[string]string{"status": "active"}, nil)
	admin.must("DELETE", fmt.Sprintf("/api/admin/upstreams/%d", upC.ID), nil, nil)
	sellerA.must("POST", fmt.Sprintf("/api/seller/listings/%d/close", la.Listing.ID), nil, nil)
	no5xx(sellerA, "POST", fmt.Sprintf("/api/seller/upstreams/%d/retry-verification", upA.ID), nil)
	no5xx(sellerA, "POST", fmt.Sprintf("/api/seller/upstreams/%d/retest", upA.ID), nil)

	// 买家余额 = 20 + 1.5 − 消费；sub2api 与账本一致
	var wallet struct {
		Balance    *float64 `json:"balance"`
		LedgerCash float64  `json:"ledger_cash"`
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &wallet)
	if wallet.Balance == nil || !approx(*wallet.Balance, wallet.LedgerCash) || wallet.LedgerCash <= 20 {
		t.Fatalf("wallet after adjustment: %+v", wallet)
	}

	// 后台任务全部跑一轮
	if _, err := e.app.Scheduler.Cycle(ctx); err != nil {
		t.Fatal(err)
	}
	e.drain()
	jobs := map[string]func() error{
		"publish_all":     func() error { return e.app.Keys.PublishAll(ctx) },
		"skip_list":       func() error { return e.app.Keys.PublishSkipList(ctx) },
		"provision":       func() error { return e.app.Keys.ProvisionPending(ctx) },
		"limits":          func() error { return e.app.Limits.RunOnce(ctx) },
		"track_crediting": func() error { return e.app.Funds.TrackCrediting(ctx) },
		"refunds":         func() error { return e.app.Funds.AdvanceRefunds(ctx) },
		"reconcile":       func() error { _, err := e.app.Reconciler.RunAttempts(ctx); return err },
		"balances":        func() error { _, err := e.app.Reconciler.RunBalances(ctx); return err },
		"risk":            func() error { _, err := e.app.Risk.Detect(ctx); return err },
		"quality":         func() error { return e.qualityRound(ctx) },
		"unfreeze":        func() error { _, err := billing.Unfreeze(ctx, e.app.DB, 7, time.Now().Add(8*24*time.Hour)); return err },
	}
	for name, fn := range jobs {
		if err := fn(); err != nil {
			t.Fatalf("job %s: %v", name, err)
		}
	}
	if n, err := e.app.Reconciler.RunBalances(ctx); err != nil || n != 0 {
		t.Fatalf("balances should reconcile after adjustment: %d %v", n, err)
	}
	// 登录防爆破：同一邮箱连续失败 10 次后，即使密码正确也暂时拒绝
	e.newUser("victim@market.test")
	for i := 0; i < 10; i++ {
		e.anon().expect(401, "POST", "/api/auth/login", map[string]string{"email": "victim@market.test", "password": "wrong-password"})
	}
	e.anon().expect(429, "POST", "/api/auth/login", map[string]string{"email": "victim@market.test", "password": "password-123"})

	var audit []map[string]any
	admin.must("GET", "/api/admin/audit-logs", nil, &audit)
	if len(audit) < 10 {
		t.Fatalf("admin actions should be audited: %d entries", len(audit))
	}
}

func userID(t *testing.T, e *env, email string) int64 {
	t.Helper()
	var id int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, "SELECT id FROM users WHERE email = $1", email), &id)
	return id
}
