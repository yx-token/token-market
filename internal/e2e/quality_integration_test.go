//go:build integration

package e2e

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// 质量检测：基线账号、上架检测、定级、降智后不调度、卖家看报告、恢复后重新调度。
func TestQualityDetectionLifecycle(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "consignment_min": 0.1, "consignment_max": 2.0}, nil)
	for i := 1; i <= 3; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.2", "port": 2080 + i}, nil)
	}
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-anthropic", "platform": "anthropic", "sell_multiplier": 0.8}, nil)

	// 题库：每题要求回答固定的口令
	for i := 1; i <= 12; i++ {
		admin.must("POST", "/api/admin/probe-items", map[string]any{
			"suite": "iq", "category": "recall", "prompt": fmt.Sprintf("Q%d: reply with the code PONG%d and nothing else", i, i),
			"grader": map[string]any{"type": "contains", "answer": fmt.Sprintf("PONG%d", i)}, "max_tokens": 32, "status": "active",
		}, nil)
	}
	// 开启检测（含定时检测），未检测的上游不接买家流量
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{
		"enabled": true, "scheduled_enabled": true, "items_per_run": 12,
	}, nil)

	// 被测账号的回答由 degraded 控制：降智时答错
	var mu sync.Mutex
	degraded := map[int64]bool{}
	setDegraded := func(acc int64, v bool) {
		mu.Lock()
		defer mu.Unlock()
		degraded[acc] = v
	}
	re := regexp.MustCompile(`PONG\d+`)
	e.fake.SetResponder(func(accountID int64, prompt string) string {
		mu.Lock()
		defer mu.Unlock()
		if degraded[accountID] {
			return "I am not sure."
		}
		return "The code is " + re.FindString(prompt)
	})
	tick := func() {
		t.Helper()
		for i := 0; i < 3; i++ {
			if err := e.app.Quality.Tick(ctx); err != nil {
				t.Fatalf("quality tick: %v", err)
			}
			e.drain() // 探测用户充值等出站操作
		}
	}

	// 平台基线账号
	var ref struct {
		Upstream idOnly `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &ref)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", ref.Upstream.ID), nil, nil)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", ref.Upstream.ID), map[string]string{"code": "reference"}, nil)
	tick()
	var refStatus string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM upstreams WHERE id = $1", ref.Upstream.ID), &refStatus)
	if refStatus != "ready" {
		t.Fatalf("reference account should pass its onboarding check: %s", refStatus)
	}

	// 卖家上架：检测中 → 通过并定级 → 挂单
	seller := e.newUser("seller@market.test")
	var up struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": "carol"}, &up)
	if up.Status != "verifying" {
		t.Fatalf("detection is on, upstream should be verifying: %+v", up)
	}
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5})
	tick()
	var status, health string
	var grade *string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, health, grade FROM upstreams WHERE id = $1", up.ID), &status, &health, &grade)
	if status != "ready" || health != "healthy" || grade == nil || *grade != "S" {
		t.Fatalf("after passing onboarding: status=%s health=%s grade=%v", status, health, grade)
	}
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5}, &created)
	e.schedule()
	var acc, poolGroup int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up.ID), &acc)
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_group_id FROM pools WHERE name = 'tm-pool-anthropic'"), &poolGroup)
	inPool := func() bool {
		var in bool
		mustScan(t, e.replica.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM account_groups WHERE account_id = $1 AND group_id = $2)", acc, poolGroup), &in)
		return in
	}
	if !inPool() {
		t.Fatal("verified upstream should serve the default pool")
	}

	// 降智：管理员触发检测 → 判定降智 → 不再调度（移出面向买家的分组，保留探测分组）
	setDegraded(acc, true)
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/probe", up.ID), nil, nil)
	tick()
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT health FROM upstreams WHERE id = $1", up.ID), &health)
	if health != "degraded" {
		t.Fatalf("health after degraded answers: %s", health)
	}
	e.schedule()
	if inPool() {
		t.Fatal("degraded upstream must be removed from the default pool")
	}
	var probeGroup int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT probe_group_id FROM upstreams WHERE id = $1", up.ID), &probeGroup)
	var inProbe bool
	mustScan(t, e.replica.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM account_groups WHERE account_id = $1 AND group_id = $2)", acc, probeGroup), &inProbe)
	if !inProbe {
		t.Fatal("degraded upstream must stay in its probe group for recovery checks")
	}
	var items []map[string]any
	e.anon().must("GET", "/api/market/listings", nil, &items)
	if len(items) != 0 {
		t.Fatalf("degraded listing must not be shown in the market: %+v", items)
	}

	// 卖家查看报告：能看到降智结论与建议，看不到题目与答案
	var reports []struct {
		ID      int64          `json:"id"`
		Verdict string         `json:"verdict"`
		Trigger string         `json:"trigger"`
		Summary map[string]any `json:"summary"`
	}
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/reports", up.ID), nil, &reports)
	if len(reports) < 2 || reports[0].Verdict != "degraded" || reports[0].Summary["attribution"] != "account" {
		t.Fatalf("seller reports: %+v", reports)
	}
	_, raw := seller.call("GET", fmt.Sprintf("/api/seller/reports/%d", reports[0].ID), nil, nil)
	if strings.Contains(raw, "PONG") || strings.Contains(raw, "reply with") {
		t.Fatalf("report leaks probe content: %s", raw)
	}

	// 申诉：管理员安排复测，仍然降智则维持原判
	var ap idOnly
	seller.must("POST", fmt.Sprintf("/api/seller/reports/%d/appeal", reports[0].ID), map[string]string{"reason": "my account works fine"}, &ap)
	admin.must("POST", fmt.Sprintf("/api/admin/appeals/%d/resolve", ap.ID), map[string]string{"action": "retest", "note": "retest with more items"}, nil)
	tick()
	var appeals []struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	admin.must("GET", "/api/admin/appeals", nil, &appeals)
	if len(appeals) != 1 || appeals[0].Status != "upheld" {
		t.Fatalf("appeal retest on a still-degraded account should uphold: %+v", appeals)
	}

	// 恢复：到期复测两次正常后恢复调度
	setDegraded(acc, false)
	for i := 0; i < 2; i++ {
		if _, err := e.app.DB.Exec(ctx, "UPDATE upstream_quality_state SET next_recovery_at = now() - interval '1 minute' WHERE upstream_id = $1", up.ID); err != nil {
			t.Fatal(err)
		}
		tick()
	}
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT health FROM upstreams WHERE id = $1", up.ID), &health)
	if health != "healthy" {
		t.Fatalf("health after recovery checks: %s", health)
	}
	e.schedule()
	if !inPool() {
		t.Fatal("recovered upstream should serve the default pool again")
	}

	// 卖家手动复测（每天限次）
	var rt struct {
		RunID int64 `json:"run_id"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retest", up.ID), nil, &rt)
	tick()
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/reports", up.ID), nil, &reports)
	if reports[0].Trigger != "seller_manual" || reports[0].Verdict != "normal" {
		t.Fatalf("manual retest report: %+v", reports[0])
	}
	var trend []struct {
		Checks   int `json:"checks"`
		Degraded int `json:"degraded"`
	}
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/quality-trend", up.ID), nil, &trend)
	if len(trend) == 0 || trend[len(trend)-1].Degraded == 0 {
		t.Fatalf("quality trend should show today's degraded checks: %+v", trend)
	}

	// 探测流量计入平台探测成本，不向买家收费
	e.app.Ingester.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	if _, _, err := e.app.Ingester.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var probeCount, chargedBuyers int
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT count(*) FILTER (WHERE channel = 'probe'), count(*) FILTER (WHERE buyer_user_id IS NOT NULL) FROM usage_records"),
		&probeCount, &chargedBuyers)
	if probeCount == 0 || chargedBuyers != 0 {
		t.Fatalf("probe usage: probe=%d buyer_charged=%d", probeCount, chargedBuyers)
	}
	var ledger struct {
		ProbeCost float64 `json:"probe_cost"`
	}
	admin.must("GET", "/api/admin/ledger", nil, &ledger)
	if ledger.ProbeCost <= 0 {
		t.Fatalf("probe cost should be recorded: %+v", ledger)
	}
}

// 首次配置必须通过上架检测：厂商关闭检测时同样执行；不通过不能挂单，修复后可以重新检测。
func TestOnboardingCheckIsMandatory(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	for i := 1; i <= 2; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.4", "port": 5080 + i}, nil)
	}
	var mu sync.Mutex
	bad := map[int64]bool{}
	e.fake.SetResponder(func(acc int64, prompt string) string {
		mu.Lock()
		defer mu.Unlock()
		if bad[acc] {
			return "no idea"
		}
		return "The code is " + pongRe.FindString(prompt)
	})

	seller := e.newUser("seller@market.test")
	var up idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": "gina"}, nil)
	var acc int64
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up.ID), &acc)
	mu.Lock()
	bad[acc] = true
	mu.Unlock()
	e.tick()
	var status string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM upstreams WHERE id = $1", up.ID), &status)
	if status != "verify_failed" {
		t.Fatalf("failing the onboarding check must block listing even with detection off: %s", status)
	}
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5})
	var reports []struct {
		Verdict string `json:"verdict"`
		Trigger string `json:"trigger"`
	}
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/reports", up.ID), nil, &reports)
	if len(reports) != 1 || reports[0].Trigger != "onboarding" || reports[0].Verdict != "degraded" {
		t.Fatalf("seller should see the failed onboarding report: %+v", reports)
	}
	// 修复后重新检测
	mu.Lock()
	bad[acc] = false
	mu.Unlock()
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retry-verification", up.ID), nil, nil)
	e.passOnboarding(up.ID)
	var health string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT health FROM upstreams WHERE id = $1", up.ID), &health)
	if health != "healthy" {
		t.Fatalf("passing the retried onboarding check should make the upstream schedulable: %s", health)
	}

	// 策略没有配置模型时检测无法开始：记为检测失败并说明原因，而不是直接放行
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"models": []string{}}, nil)
	var up2 idOnly
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up2)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up2.ID), nil, nil)
	var res struct {
		Status        string  `json:"status"`
		FailureReason *string `json:"failure_reason"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up2.ID), map[string]string{"code": "hank"}, &res)
	if res.Status != "verify_failed" || res.FailureReason == nil || !strings.Contains(*res.FailureReason, "could not start") {
		t.Fatalf("onboarding without models: %+v", res)
	}
}
