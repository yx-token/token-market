//go:build integration

package e2e

import (
	"context"
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
	// 限定模型的题：只考本次检测的模型（策略默认 claude-sonnet-4-5），其他模型的题不出
	var sameModel, otherModel idOnly
	admin.must("POST", "/api/admin/probe-items", map[string]any{
		"suite": "iq", "category": "recall", "prompt": "Q13: reply with the code PONG13 and nothing else",
		"grader": map[string]any{"type": "contains", "answer": "PONG13"}, "max_tokens": 32, "models": []string{"claude-sonnet-4-5"},
	}, &sameModel)
	admin.must("POST", "/api/admin/probe-items", map[string]any{
		"suite": "iq", "category": "recall", "prompt": "Q99: reply with the code PONG99 and nothing else",
		"grader": map[string]any{"type": "contains", "answer": "PONG99"}, "max_tokens": 32, "models": []string{"gpt-6-astra"},
	}, &otherModel)
	// 开启检测（含定时检测），未检测的上游不接买家流量
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{
		"enabled": true, "scheduled_enabled": true,
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
			if err := e.qualityRound(ctx); err != nil {
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
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}})
	tick()
	var status, health string
	var grade *string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, health, grade FROM upstreams WHERE id = $1", up.ID), &status, &health, &grade)
	if status != "ready" || health != "healthy" || grade == nil || *grade != "S" {
		t.Fatalf("after passing onboarding: status=%s health=%s grade=%v", status, health, grade)
	}
	// 每次检测测全部适用题目，不抽样：公共准备与本测试各 12 道通用题 + 1 道限定本模型的题，不含其他模型的题
	var asked int
	var askedSame, askedOther bool
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*), bool_or(r.item_id = $2), bool_or(r.item_id = $3) FROM probe_results r
		JOIN probe_runs pr ON pr.id = r.run_id WHERE pr.upstream_id = $1 AND pr.trigger = 'onboarding'`, up.ID, sameModel.ID, otherModel.ID),
		&asked, &askedSame, &askedOther)
	if asked != 25 || !askedSame || askedOther {
		t.Fatalf("onboarding should ask all 25 applicable items and skip other models: asked=%d same=%v other=%v", asked, askedSame, askedOther)
	}
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}}, &created)
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
	seller.expect(409, "POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", up.ID), map[string]any{"mode": "consignment", "multiplier": 0.5, "categories": []string{"coding"}})
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

// 检测超时与中断：超过总时限记为检测失败（上架检测不通过，可以重新检测）；worker 停止时整批重新排队；
// 进程中断遗留的"执行中"记录超过时限后记为失败，不再挡住之后的检测。超时在系统设置中调整。
func TestProbeTimeouts(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	for i := 1; i <= 8; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.5", "port": 6080 + i}, nil)
	}

	// 默认单次请求 180 秒、整批 30 分钟；整批不能短于单次请求
	var st struct {
		Request int `json:"probe_request_timeout_seconds"`
		Batch   int `json:"probe_batch_timeout_minutes"`
	}
	admin.must("GET", "/api/admin/settings", nil, &st)
	if st.Request != 180 || st.Batch != 30 {
		t.Fatalf("default probe timeouts: %+v", st)
	}
	admin.expect(400, "PUT", "/api/admin/settings", map[string]any{"probe_request_timeout_seconds": 600, "probe_batch_timeout_minutes": 5})
	admin.must("PUT", "/api/admin/settings", map[string]any{"probe_request_timeout_seconds": 120, "probe_batch_timeout_minutes": 20}, &st)
	if st.Request != 120 || st.Batch != 20 {
		t.Fatalf("probe timeouts should be adjustable: %+v", st)
	}

	// 账号回答 hangAfter 次后挂起，直到测试结束
	var mu sync.Mutex
	hangAfter, answered := map[int64]int{}, map[int64]int{}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	e.fake.SetResponder(func(acc int64, prompt string) string {
		mu.Lock()
		n, ok := hangAfter[acc]
		hang := ok && answered[acc] >= n
		answered[acc]++
		mu.Unlock()
		if hang {
			<-release
		}
		return "The code is " + pongRe.FindString(prompt)
	})
	setHang := func(acc int64, after int) {
		mu.Lock()
		defer mu.Unlock()
		answered[acc] = 0
		if after < 0 {
			delete(hangAfter, acc)
		} else {
			hangAfter[acc] = after
		}
	}
	seller := e.newUser("seller@market.test")
	onboard := func(code string, hang int) (upstreamID, acc int64, batch string) {
		var up idOnly
		seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": code}, nil)
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up.ID), &acc)
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT batch_id FROM probe_runs WHERE upstream_id = $1 AND status = 'queued'", up.ID), &batch)
		setHang(acc, hang)
		return up.ID, acc, batch
	}
	failedOnboarding := func(upstreamID int64, batch, runErr string) {
		t.Helper()
		var status, summary, upStatus, reason string
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, summary->>'error' FROM probe_runs WHERE batch_id = $1", batch), &status, &summary)
		if status != "failed" || !strings.Contains(summary, runErr) {
			t.Fatalf("run of batch %s should fail with %q: %s %s", batch, runErr, status, summary)
		}
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, failure_reason FROM upstreams WHERE id = $1", upstreamID), &upStatus, &reason)
		if upStatus != "verify_failed" || !strings.Contains(reason, "onboarding check failed: "+runErr) {
			t.Fatalf("upstream %d should fail its onboarding check so the seller can retry: %s %s", upstreamID, upStatus, reason)
		}
	}
	// 先让一个正常账号通过上架检测（同时给探测用户充值）
	first, _, _ := onboard("ivy", -1)
	e.passOnboarding(first)

	// 超过总时限：检测失败、上架检测不通过；修复后可以重新检测
	up1, acc1, batch1 := onboard("jack", 0)
	tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err := e.app.Quality.ExecuteBatch(tctx, batch1)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	failedOnboarding(up1, batch1, "check did not finish within 20m0s")
	setHang(acc1, -1)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retry-verification", up1), nil, nil)
	e.passOnboarding(up1)

	// worker 停止（ctx 取消）：整批放回队列并丢弃部分结果，之后正常执行
	up2, acc2, batch2 := onboard("kate", 1)
	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(time.Second, cancel)
	if err := e.app.Quality.ExecuteBatch(cctx, batch2); err != nil {
		t.Fatal(err)
	}
	var status string
	var cleared bool
	var results int
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status, started_at IS NULL, (SELECT count(*) FROM probe_results WHERE run_id = r.id)
		FROM probe_runs r WHERE batch_id = $1`, batch2), &status, &cleared, &results)
	if status != "queued" || !cleared || results != 0 {
		t.Fatalf("interrupted batch should be requeued from scratch: status=%s cleared=%v results=%d", status, cleared, results)
	}
	setHang(acc2, -1)
	e.passOnboarding(up2)

	// 进程中断遗留的"执行中"记录：超过总时限与宽限后记为失败，卖家可以重新检测
	up3, _, batch3 := onboard("liam", -1)
	if _, err := e.app.DB.Exec(ctx, "UPDATE probe_runs SET status = 'running', started_at = now() - interval '24 minutes' WHERE batch_id = $1", batch3); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Quality.DispatchQueued(ctx); err != nil {
		t.Fatal(err)
	}
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM probe_runs WHERE batch_id = $1", batch3), &status)
	if status != "running" {
		t.Fatalf("a run still within its time limit must not be touched: %s", status)
	}
	if _, err := e.app.DB.Exec(ctx, "UPDATE probe_runs SET started_at = now() - interval '26 minutes' WHERE batch_id = $1", batch3); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Quality.DispatchQueued(ctx); err != nil {
		t.Fatal(err)
	}
	failedOnboarding(up3, batch3, "check was interrupted before finishing")
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retry-verification", up3), nil, nil)
	e.passOnboarding(up3)

	// 后台执行：同时最多 Concurrency 个批次，一批结束立即补位而不等下一轮；停止时进行中的批次放回队列
	e.app.Quality.Concurrency = 1
	upA, _, _ := onboard("mia", -1)
	upB, _, _ := onboard("noah", -1)
	upC, accC, batchC := onboard("olga", 0)
	rctx, stop := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		e.app.Quality.RunBatches(rctx, time.Hour)
		close(stopped)
	}()
	for i := 0; ; i++ {
		var ok bool
		mustScan(t, e.app.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM upstreams WHERE id IN ($1, $2) AND status = 'ready') = 2
			AND (SELECT status FROM probe_runs WHERE batch_id = $3) = 'running'`, upA, upB, batchC), &ok)
		if ok {
			break
		}
		if i == 150 {
			t.Fatal("background batches should run one after another without waiting for the next round")
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	<-stopped
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM probe_runs WHERE batch_id = $1", batchC), &status)
	if status != "queued" {
		t.Fatalf("stopping the worker should requeue the running batch: %s", status)
	}
	setHang(accC, -1)
	e.passOnboarding(upC)
}
