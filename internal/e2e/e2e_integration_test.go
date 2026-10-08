//go:build integration

// Package e2e 用 sub2api 替身（真实表结构）把卖家上架、调度、买家 Key、Edge 转发与故障转移、
// 用量入账、结算、对账、退款与提现串起来跑一遍。
//
// 需要：TM_TEST_DATABASE_DSN（可建库的 PostgreSQL）、TM_SUB2API_REPO（sub2api 仓库，用于取 v0.2.14 迁移）、
// TM_TEST_REDIS_ADDR（Redis，测试会清空其中的第 9 号库）。
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/contract"
	"github.com/DueGin/token-market/internal/app"
	"github.com/DueGin/token-market/internal/billing"
	"github.com/DueGin/token-market/internal/compat"
	"github.com/DueGin/token-market/internal/edge"
	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/httpapi"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/sub2api/compatguard"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/testutil"
	"github.com/DueGin/token-market/internal/testutil/fakesub2api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type env struct {
	t       *testing.T
	ctx     context.Context
	app     *app.App
	fake    *fakesub2api.Server
	replica *pgxpool.Pool
	api     *httptest.Server
	sender  *ops.Sender
	guard   *compatguard.Guard
}

func setup(t *testing.T) *env {
	t.Helper()
	repo := os.Getenv("TM_SUB2API_REPO")
	redisAddr := os.Getenv("TM_TEST_REDIS_ADDR")
	if repo == "" || redisAddr == "" {
		t.Skip("TM_SUB2API_REPO and TM_TEST_REDIS_ADDR are required")
	}
	ctx := context.Background()
	marketDSN, _ := testutil.NewDatabase(t)
	replicaDSN, replicaPool := testutil.NewDatabase(t)

	// sub2api 真实表结构
	set, err := compat.LoadGitMigrations(ctx, repo, "v0.2.14")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, replicaDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.ApplySub2APIMigrations(ctx, conn, set); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 9})
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	_ = rdb.Close()

	fake := fakesub2api.New(replicaPool, "admin-key-for-test")
	t.Cleanup(fake.Close)

	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg := &config.Config{
		Env:      "test",
		Database: config.DatabaseConfig{DSN: marketDSN, MaxConns: 10},
		Redis:    config.RedisConfig{Addr: redisAddr, DB: 9},
		Crypto:   config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": key}, HashPepper: "pepper"},
		Auth: config.AuthConfig{JWTSecret: strings.Repeat("s", 40), TokenTTL: time.Hour,
			BootstrapAdminEmail: "admin@market.test", BootstrapAdminPassword: "admin-password-1"},
		Sub2API: config.Sub2APIConfig{BaseURL: fake.URL(), GatewayURL: fake.URL(), AdminAPIKey: fake.AdminKey,
			ReplicaDSN: replicaDSN, Timeout: 10 * time.Second, ShadowEmailDomain: "shadow.market.test"},
		Worker: config.WorkerConfig{ProbeConcurrency: 2},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	a, err := app.Build(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if _, err := a.Accounts.EnsureBootstrapAdmin(ctx, cfg.Auth.BootstrapAdminEmail, cfg.Auth.BootstrapAdminPassword); err != nil {
		t.Fatal(err)
	}

	spec, err := contract.Load()
	if err != nil {
		t.Fatal(err)
	}
	guard := compatguard.New(spec, a.Admin, a.Replica, a.DB, log)
	if res, err := guard.Check(ctx); err != nil || res.State != compatguard.StateNormal {
		t.Fatalf("compat guard should pass against a v0.2.14 schema: %+v %v", res, err)
	}
	sender := ops.NewSender(a.DB, ops.SenderConfig{BatchSize: 50, Retryable: opshandlers.Retryable, Gate: guard.AllowWrites, Logger: log})
	opshandlers.Register(sender, a.Admin)

	srv := &httpapi.Server{
		DB: a.DB, Issuer: a.Issuer, Accounts: a.Accounts, Listings: a.Listings, Proxies: a.Proxies, Pools: a.Pools,
		Keys: a.Keys, Funds: a.Funds, Market: a.Market, Quality: a.Quality, Risk: a.Risk, Kicker: a.Kicker, Log: log,
	}
	api := httptest.NewServer(srv.Handler())
	t.Cleanup(api.Close)

	// 题库：每题要求回答固定口令，替身默认按口令作答。首次配置的上游必须通过上架检测才能挂单。
	for i := 1; i <= 12; i++ {
		if _, err := quality.CreateItem(ctx, a.DB, quality.Item{Suite: "iq", Category: "recall",
			Prompt: fmt.Sprintf("Q%d: reply with the code PONG%d and nothing else", i, i), MaxTokens: 32,
			Grader: quality.Grader{Type: "contains", Answer: fmt.Sprintf("PONG%d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	fake.SetResponder(func(_ int64, prompt string) string { return "The code is " + pongRe.FindString(prompt) })
	return &env{t: t, ctx: ctx, app: a, fake: fake, replica: replicaPool, api: api, sender: sender, guard: guard}
}

var pongRe = regexp.MustCompile(`PONG\d+`)

// tick 执行几轮质量调度（中间发送出站操作，例如给探测用户充值）。
func (e *env) tick() {
	e.t.Helper()
	for i := 0; i < 3; i++ {
		if err := e.app.Quality.Tick(e.ctx); err != nil {
			e.t.Fatalf("quality tick: %v", err)
		}
		e.drain()
	}
}

// passOnboarding 运行上架检测并确认上游通过（进入 ready）。
func (e *env) passOnboarding(upstreamID int64) {
	e.t.Helper()
	e.tick()
	var status string
	var reason *string
	mustScan(e.t, e.app.DB.QueryRow(e.ctx, "SELECT status, failure_reason FROM upstreams WHERE id = $1", upstreamID), &status, &reason)
	if status != "ready" {
		e.t.Fatalf("upstream %d should pass its onboarding check: status=%s reason=%v", upstreamID, status, reason)
	}
}

// ingestAll 把时钟拨到安全延迟之后，入账全部已有用量（例如上架检测产生的探测流量）。
func (e *env) ingestAll() {
	e.t.Helper()
	e.app.Ingester.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	for {
		_, more, err := e.app.Ingester.RunOnce(e.ctx)
		if err != nil {
			e.t.Fatal(err)
		}
		if !more {
			return
		}
	}
}

// drain 执行出站操作直到队列为空；有失败的操作时让测试失败并打印原因。
func (e *env) drain() {
	e.t.Helper()
	for i := 0; i < 50; i++ {
		n, err := e.sender.RunOnce(e.ctx)
		if err != nil {
			e.t.Fatalf("ops sender: %v", err)
		}
		if n == 0 {
			break
		}
	}
	rows, err := e.app.DB.Query(e.ctx, "SELECT id, op_type, status, COALESCE(last_error, '') FROM sub2api_ops WHERE status <> 'done'")
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var typ, status, last string
		_ = rows.Scan(&id, &typ, &status, &last)
		e.t.Errorf("op %d %s left in status %s: %s", id, typ, status, last)
	}
	if e.t.Failed() {
		e.t.FailNow()
	}
}

// schedule 运行调度直到收敛（不再产生新的操作）。
func (e *env) schedule() {
	e.t.Helper()
	for i := 0; i < 5; i++ {
		res, err := e.app.Scheduler.Cycle(e.ctx)
		if err != nil {
			e.t.Fatalf("scheduler cycle: %v", err)
		}
		if res.Enqueued == 0 {
			return
		}
		e.drain()
	}
	e.t.Fatal("scheduler did not converge after 5 cycles")
}

// ---- HTTP 客户端 ----

type client struct {
	e     *env
	token string
}

func (e *env) anon() *client { return &client{e: e} }

func (e *env) login(email, password string) *client {
	var out struct {
		Token string `json:"token"`
	}
	e.anon().must("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, &out)
	return &client{e: e, token: out.Token}
}

func (e *env) newUser(email string) *client {
	e.anon().must("POST", "/api/auth/register", map[string]string{"email": email, "password": "password-123"}, nil)
	return e.login(email, "password-123")
}

func (c *client) call(method, path string, body, out any) (int, string) {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.e.api.URL+path, r)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			c.e.t.Fatalf("%s %s: decode envelope: %v: %s", method, path, err, raw)
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			c.e.t.Fatalf("%s %s: decode data: %v: %s", method, path, err, env.Data)
		}
	}
	return resp.StatusCode, string(raw)
}

func (c *client) must(method, path string, body, out any) {
	c.e.t.Helper()
	if status, raw := c.call(method, path, body, out); status >= 300 {
		c.e.t.Fatalf("%s %s: http %d: %s", method, path, status, raw)
	}
}

func (c *client) expect(status int, method, path string, body any) string {
	c.e.t.Helper()
	got, raw := c.call(method, path, body, nil)
	if got != status {
		c.e.t.Fatalf("%s %s: want http %d, got %d: %s", method, path, status, got, raw)
	}
	return raw
}

type idOnly struct {
	ID int64 `json:"id"`
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// ---- 测试 ----

func TestMarketplaceEndToEnd(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")

	// 平台准备：开放厂商、代理库存、默认分组、测试用参数
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	for i := 1; i <= 3; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.1", "port": 1080 + i}, nil)
	}
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-anthropic", "platform": "anthropic", "sell_multiplier": 0.8}, nil)
	admin.must("PUT", "/api/admin/settings", map[string]any{"withdraw_min": 0.0001, "refund_lock_seconds": 0, "freeze_days": 7}, nil)

	// 卖家 1：寄售 0.5 倍
	seller1 := e.newUser("seller1@market.test")
	var up1 struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	seller1.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up1)
	var start struct {
		AuthURL string `json:"auth_url"`
	}
	seller1.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up1.ID), nil, &start)
	if start.AuthURL == "" {
		t.Fatal("no auth url")
	}
	seller1.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up1.ID), map[string]string{"code": "alice"}, &up1)
	// 即使厂商关闭了检测，首次配置也必须先通过上架检测
	if up1.Status != "verifying" {
		t.Fatalf("new upstream must be verified before listing: %+v", up1)
	}
	seller1.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up1.ID), map[string]any{"mode": "consignment", "multiplier": 0.5})
	e.passOnboarding(up1.ID)
	var created struct {
		Listing struct {
			ID int64 `json:"id"`
		} `json:"listing"`
	}
	seller1.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up1.ID), map[string]any{"mode": "consignment", "multiplier": 0.5}, &created)
	listing1 := created.Listing.ID

	// 同一个官方账号不能重复挂载
	dup := e.newUser("dup@market.test")
	var dupUp idOnly
	dup.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &dupUp)
	dup.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", dupUp.ID), nil, nil)
	dup.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", dupUp.ID), map[string]string{"code": "alice"})
	dup.must("DELETE", fmt.Sprintf("/api/seller/upstreams/%d", dupUp.ID), nil, nil)

	// 卖家 2：平台回收，接受报价
	seller2 := e.newUser("seller2@market.test")
	var up2 idOnly
	seller2.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up2)
	seller2.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up2.ID), nil, nil)
	seller2.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up2.ID), map[string]string{"code": "bob"}, nil)
	e.passOnboarding(up2.ID)
	var bb struct {
		Listing struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"listing"`
		Quote struct {
			Grade      string  `json:"grade"`
			Multiplier float64 `json:"multiplier"`
		} `json:"quote"`
	}
	seller2.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up2.ID), map[string]any{"mode": "buyback"}, &bb)
	if bb.Listing.Status != "pending_confirm" || !approx(bb.Quote.Multiplier, 0.1275) {
		t.Fatalf("buyback quote should be base 0.15 × grade B 0.85: %+v", bb)
	}
	seller2.must("POST", fmt.Sprintf("/api/seller/listings/%d/accept-quote", bb.Listing.ID), nil, nil)

	// 调度：两个账号进入默认分组，回收卖家优先级更高
	e.schedule()
	// 先入账上架检测的探测流量（卖家照常获得收益，记为平台探测成本），后面只看买家流量带来的变化
	e.ingestAll()
	var base1 struct {
		Pending float64 `json:"pending"`
	}
	seller1.must("GET", "/api/seller/earnings", nil, &base1)
	if base1.Pending <= 0 {
		t.Fatalf("seller should earn from onboarding probe usage: %+v", base1)
	}
	var acc1, acc2, poolGroup int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up1.ID), &acc1)
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up2.ID), &acc2)
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_group_id FROM pools WHERE name = 'tm-pool-anthropic'"), &poolGroup)
	var p1, p2 int
	var r1, r2 float64
	mustScan(t, e.replica.QueryRow(ctx, "SELECT priority, rate_multiplier::float8 FROM accounts WHERE id = $1", acc1), &p1, &r1)
	mustScan(t, e.replica.QueryRow(ctx, "SELECT priority, rate_multiplier::float8 FROM accounts WHERE id = $1", acc2), &p2, &r2)
	if p2 >= p1 || !approx(r1, 0.5) || !approx(r2, 0.1275) {
		t.Fatalf("buyback account should outrank consignment; rate = seller multiplier: p1=%d r1=%v p2=%d r2=%v", p1, r1, p2, r2)
	}
	for _, acc := range []int64{acc1, acc2} {
		var in bool
		mustScan(t, e.replica.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM account_groups WHERE account_id = $1 AND group_id = $2)", acc, poolGroup), &in)
		if !in {
			t.Fatalf("account %d should be in the default pool", acc)
		}
	}

	// 市场展示：买家价 = 0.5 × 1.2
	var items []struct {
		ListingID  int64   `json:"listing_id"`
		BuyerPrice float64 `json:"price"`
	}
	e.anon().must("GET", "/api/market/listings?platform=anthropic", nil, &items)
	if len(items) != 1 || items[0].ListingID != listing1 || !approx(items[0].BuyerPrice, 0.6) {
		t.Fatalf("market listings: %+v", items)
	}

	// 买家充值 10，管理员确认到账
	buyer := e.newUser("buyer@market.test")
	var rc struct {
		Order idOnly `json:"order"`
	}
	buyer.must("POST", "/api/buyer/recharge", map[string]any{"amount": 10}, &rc)
	admin.must("POST", fmt.Sprintf("/api/admin/recharges/%d/confirm", rc.Order.ID), map[string]string{"provider_ref": "bank-1"}, nil)
	e.drain()
	if err := e.app.Funds.TrackCrediting(ctx); err != nil {
		t.Fatal(err)
	}
	var wallet struct {
		Balance    *float64 `json:"balance"`
		LedgerCash float64  `json:"ledger_cash"`
		Recharges  []struct {
			Status string `json:"status"`
		} `json:"recent_recharges"`
	}
	buyer.must("GET", "/api/buyer/wallet", nil, &wallet)
	if wallet.Balance == nil || !approx(*wallet.Balance, 10) || !approx(wallet.LedgerCash, 10) || wallet.Recharges[0].Status != "credited" {
		t.Fatalf("wallet after recharge: %+v", wallet)
	}

	// 买家 Key：先指定卖家 1，失败再走默认分组
	var key struct {
		Key    string `json:"key"`
		Detail struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"detail"`
	}
	buyer.must("POST", "/api/buyer/keys", map[string]any{"name": "k1", "route": map[string]any{
		"type": "chain", "hops": []map[string]any{{"listing_id": listing1}, {"pool": "tm-pool-anthropic"}}}}, &key)
	if !strings.HasPrefix(key.Key, "tm-") || key.Detail.Status != "active" {
		t.Fatalf("key: %+v", key)
	}
	// 挂单专属分组创建后，调度器把卖家 1 的账号加入该分组
	e.schedule()
	var listingGroup int64
	var listingRate float64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT listing_group_id, listing_group_rate::float8 FROM listings WHERE id = $1", listing1), &listingGroup, &listingRate)
	if !approx(listingRate, 0.6) {
		t.Fatalf("listing group rate should equal buyer price: %v", listingRate)
	}
	var inListing bool
	mustScan(t, e.replica.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM account_groups WHERE account_id = $1 AND group_id = $2)", acc1, listingGroup), &inListing)
	if !inListing {
		t.Fatal("seller 1 account should be in its listing group")
	}

	// Edge
	ed := e.startEdge()
	call := func() (int, http.Header) { return ed.call(key.Key) }
	waitKey := func(want string) { ed.waitKey(key.Key, want) }
	// 1) 指定卖家正常 → 第一跳成功
	if status, h := call(); status != 200 || h.Get("X-TM-Listing") != fmt.Sprintf("listing:%d", listing1) {
		t.Fatalf("first call should be served by listing %d: %d %v", listing1, status, h)
	}
	// 2) 卖家 1 故障 → 故障转移到默认分组，由回收卖家 2 提供
	e.fake.FailAccount(acc1, 503)
	if status, h := call(); status != 200 || h.Get("X-TM-Listing") != "pool:tm-pool-anthropic" || h.Get("X-TM-Hop") != "2" {
		t.Fatalf("second call should fail over to the pool: %d %v", status, h)
	}
	e.fake.FailAccount(acc1, 0)
	// 3) 未知 key
	if status, _ := ed.call("tm-unknown-key"); status != 401 {
		t.Fatalf("unknown key should be rejected: %d", status)
	}
	ed.flushAttempts()

	// 入账：把时钟拨到安全延迟之后
	res, _, err := e.app.Ingester.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 2 || res.Posted != 2 {
		t.Fatalf("ingest: %+v", res)
	}
	type rec struct {
		channel        string
		upstream       int64
		actual, payout float64
	}
	recs := map[string]rec{}
	rows, err := e.app.DB.Query(ctx, "SELECT channel, upstream_id, actual_cost::float8, payout::float8 FROM usage_records WHERE channel <> 'probe' ORDER BY sub2api_usage_id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.channel, &r.upstream, &r.actual, &r.payout); err != nil {
			t.Fatal(err)
		}
		recs[r.channel] = r
	}
	rows.Close()
	cost := fakesub2api.TotalCost
	direct, pool := recs["direct"], recs["pool"]
	if direct.upstream != up1.ID || !approx(direct.actual, cost*0.6) || !approx(direct.payout, cost*0.5) {
		t.Fatalf("direct usage: %+v", direct)
	}
	if pool.upstream != up2.ID || !approx(pool.actual, cost*0.8) || !approx(pool.payout, cost*0.1275) {
		t.Fatalf("pool usage: %+v", pool)
	}
	spent := direct.actual + pool.actual

	// 账本：买家余额、卖家待结算、平台收入，sub2api 余额一致
	buyer.must("GET", "/api/buyer/wallet", nil, &wallet)
	if !approx(*wallet.Balance, 10-spent) || !approx(wallet.LedgerCash, 10-spent) {
		t.Fatalf("wallet after usage: balance=%v ledger=%v want %v", *wallet.Balance, wallet.LedgerCash, 10-spent)
	}
	var earn struct {
		Pending   float64 `json:"pending"`
		Available float64 `json:"available"`
	}
	seller1.must("GET", "/api/seller/earnings", nil, &earn)
	if !approx(earn.Pending, base1.Pending+cost*0.5) || earn.Available != 0 {
		t.Fatalf("seller 1 earnings: %+v", earn)
	}
	var ledger struct {
		Revenue float64 `json:"revenue"`
	}
	admin.must("GET", "/api/admin/ledger", nil, &ledger)
	if !approx(ledger.Revenue, spent-cost*0.5-cost*0.1275) {
		t.Fatalf("platform revenue: %+v want %v", ledger, spent-cost*0.5-cost*0.1275)
	}

	// 对账：R1 记录"Edge 认为失败但 sub2api 计费"的情况不应出现；余额恒等式成立
	e.app.Reconciler.Now = func() time.Time { return time.Now().Add(time.Hour) }
	rr, err := e.app.Reconciler.RunAttempts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rr.R1+rr.R2+rr.R3+rr.R4 != 0 {
		var anomalies []billing.Anomaly
		admin.must("GET", "/api/admin/anomalies", nil, &anomalies)
		t.Fatalf("no billing anomalies expected: %+v %+v", rr, anomalies)
	}
	if n, err := e.app.Reconciler.RunBalances(ctx); err != nil || n != 0 {
		t.Fatalf("balance reconciliation: %d %v", n, err)
	}
	var detail []struct {
		Hop       int    `json:"hop"`
		EndReason string `json:"end_reason"`
	}
	var edgeReq string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT edge_request_id FROM edge_attempts WHERE hop = 2"), &edgeReq)
	buyer.must("GET", "/api/buyer/usage/"+edgeReq, nil, &detail)
	if len(detail) != 2 || detail[0].EndReason != "retry" || detail[1].EndReason != "completed" {
		t.Fatalf("request detail should show the failover: %+v", detail)
	}

	// 解冻与提现
	if _, err := billing.Unfreeze(ctx, e.app.DB, 7, time.Now().Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	seller1.must("GET", "/api/seller/earnings", nil, &earn)
	if earn.Pending != 0 || !approx(earn.Available, base1.Pending+cost*0.5) {
		t.Fatalf("seller 1 earnings after unfreeze: %+v", earn)
	}
	seller1.expect(403, "POST", "/api/seller/withdrawals", map[string]any{"amount": 0.1, "channel": "alipay", "account_info": "a@b"})
	seller1.must("POST", "/api/me/kyc", map[string]string{"real_name": "Alice", "id_number": "110101199001011234"}, nil)
	var me struct {
		ID int64 `json:"id"`
	}
	seller1.must("GET", "/api/me", nil, &me)
	admin.must("POST", fmt.Sprintf("/api/admin/kyc/%d/review", me.ID), map[string]any{"approve": true}, nil)
	var wd struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	seller1.must("POST", "/api/seller/withdrawals", map[string]any{"amount": 0.1, "channel": "alipay", "account_info": "a@b"}, &wd)
	if wd.Status != "pending_review" { // 新卖家期需要人工审核
		t.Fatalf("withdrawal of a new seller should need review: %+v", wd)
	}
	admin.must("POST", fmt.Sprintf("/api/admin/withdrawals/%d/review", wd.ID), map[string]any{"approve": true}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/withdrawals/%d/paid", wd.ID), map[string]any{"payout_ref": "pay-1"}, &wd)
	if wd.Status != "paid" {
		t.Fatalf("withdrawal: %+v", wd)
	}

	// 退款：锁定 Key → 扣减 sub2api 余额 → 记账 → 管理员打款
	var refund idOnly
	buyer.must("POST", "/api/buyer/refunds", map[string]string{"note": "done"}, &refund)
	waitKey("disabled")
	if status, _ := call(); status != 403 {
		t.Fatalf("keys must be locked during refund, got %d", status)
	}
	for i := 0; i < 3; i++ {
		if err := e.app.Funds.AdvanceRefunds(ctx); err != nil {
			t.Fatal(err)
		}
		e.drain()
	}
	var refunds []struct {
		Status string  `json:"status"`
		Amount float64 `json:"amount"`
	}
	buyer.must("GET", "/api/buyer/refunds", nil, &refunds)
	if len(refunds) != 1 || refunds[0].Status != "paying" || !approx(refunds[0].Amount, math.Floor((10-spent)*100+1e-6)/100) {
		t.Fatalf("refund: %+v", refunds)
	}
	admin.must("POST", fmt.Sprintf("/api/admin/refunds/%d/paid", refund.ID), map[string]string{"payout_ref": "back-1"}, nil)
	buyer.must("GET", "/api/buyer/wallet", nil, &wallet)
	left := 10 - spent - refunds[0].Amount
	if !approx(*wallet.Balance, left) || !approx(wallet.LedgerCash, left) {
		t.Fatalf("wallet after refund: %+v want %v", wallet, left)
	}
	if n, err := e.app.Reconciler.RunBalances(ctx); err != nil || n != 0 {
		t.Fatalf("balance reconciliation after refund: %d %v", n, err)
	}

	// 下架：删除上游后调度器移除 sub2api 账号（宽限期之后）
	seller1.must("DELETE", fmt.Sprintf("/api/seller/upstreams/%d", up1.ID), nil, nil)
	e.app.Scheduler.Now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	e.schedule()
	e.schedule()
	var deleted bool
	mustScan(t, e.replica.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM accounts WHERE id = $1", acc1), &deleted)
	var st string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM upstreams WHERE id = $1", up1.ID), &st)
	if !deleted || st != "removed" {
		t.Fatalf("removed upstream: account deleted=%v status=%s", deleted, st)
	}
	var probeGroup int64
	var probeGone bool
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT probe_group_id FROM upstreams WHERE id = $1", up1.ID), &probeGroup)
	mustScan(t, e.replica.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM groups WHERE id = $1", probeGroup), &probeGone)
	if !probeGone {
		t.Fatal("probe group of a removed upstream should be deleted")
	}
}

// ---- Edge ----

type edgeEnv struct {
	e        *env
	url      string
	cache    *edge.Cache
	attempts *edge.AttemptLogger
	stop     context.CancelFunc
	flushed  bool
}

// startEdge 启动 Edge：从 Redis 加载 Key 配置并订阅变更，尝试日志写入本系统数据库。
func (e *env) startEdge() *edgeEnv {
	e.t.Helper()
	if err := e.app.Keys.PublishSkipList(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := edge.NewCache(edgecfg.Store{Redis: e.app.Redis}, quiet)
	if err := cache.Load(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	watchCtx, stopWatch := context.WithCancel(e.ctx)
	go cache.Watch(watchCtx, time.Minute)
	time.Sleep(200 * time.Millisecond) // 等订阅建立
	attempts := edge.NewAttemptLogger(e.app.DB, e.t.TempDir(), quiet)
	attemptCtx, stopAttempts := context.WithCancel(context.Background())
	go attempts.Run(attemptCtx)
	gw, _ := url.Parse(e.fake.URL())
	srv := httptest.NewServer(edge.NewServer(edge.Config{
		GatewayURL: gw, ConnectTimeout: 2 * time.Second, FirstByteTimeout: 10 * time.Second, MaxBodyBytes: 1 << 20,
		BreakerFailures: 100, BreakerCooldown: time.Second,
	}, cache, e.app.Keyring, attempts, quiet))
	ed := &edgeEnv{e: e, url: srv.URL, cache: cache, attempts: attempts, stop: stopAttempts}
	e.t.Cleanup(func() {
		srv.Close()
		stopWatch()
		ed.flushAttempts()
	})
	return ed
}

func (ed *edgeEnv) call(key string) (int, http.Header) {
	ed.e.t.Helper()
	req, _ := http.NewRequest("POST", ed.url+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ed.e.t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header
}

// waitKey 等 Edge 缓存看到 Key 的状态（active / disabled），以及可选的限额封禁。
func (ed *edgeEnv) waitKey(key, status string) {
	ed.e.t.Helper()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	for i := 0; i < 50; i++ {
		if c := ed.cache.Key(hash); c != nil && c.Status == status {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	ed.e.t.Fatalf("edge cache did not see key status %s", status)
}

func (ed *edgeEnv) waitBlocked(key string) {
	ed.e.t.Helper()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	for i := 0; i < 50; i++ {
		if c := ed.cache.Key(hash); c != nil && c.BlockedUntil != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	ed.e.t.Fatal("edge cache did not see the spending block")
}

// flushAttempts 停止尝试日志并把队列中的记录写入数据库（只能调用一次）。
func (ed *edgeEnv) flushAttempts() {
	if ed.flushed {
		return
	}
	ed.flushed = true
	ed.stop()
	ed.attempts.Wait()
}

func mustScan(t *testing.T, row pgx.Row, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}
