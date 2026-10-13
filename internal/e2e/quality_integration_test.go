//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/quality"
)

// 两条流程独立选题；测智偏低只标疑似，开启扣价后在试运行也写入 Q。
func TestQualityDetectionLifecycle(t *testing.T) {
	f := newInspectionPricingFixture(t, "inspection/model-permission/1")
	f.admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"mode": "trial"}, nil)
	f.admin.must("PUT", "/api/admin/inspection/checks/D07", map[string]any{"pricing": false, "calibrated": false}, nil)
	f.admin.must("PUT", "/api/admin/inspection/checks/D06", map[string]any{"enabled": false}, nil)
	var onboardingFlows, models []string
	var source string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT flows,identity_source,models FROM probe_runs WHERE id=$1`, f.onboarding), &onboardingFlows, &source, &models)
	if !reflect.DeepEqual(onboardingFlows, []string{"intelligence", "identity"}) || source != "self" || !reflect.DeepEqual(models, []string{"claude-sonnet-4-5"}) {
		t.Fatalf("上架须冻结两条流程与模型：%v %s %v", onboardingFlows, source, models)
	}
	f.transport.setBad("D07")
	_, display := f.run(t, "admin")
	if display.Verdict != "suspected" || display.QualityFactor == nil || !approx(*display.QualityFactor, 1) || display.Checks["D07"].Applied {
		t.Fatalf("测智未通过只标疑似，未开扣价时Q为1：%+v", display)
	}
	f.assertHealth(t, "suspected")
	var suspected []string
	var attempts int
	var due time.Time
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT suspected_flows,adaptive_attempts,next_intelligence_at FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &suspected, &attempts, &due)
	if !reflect.DeepEqual(suspected, []string{"intelligence"}) || attempts != 1 || time.Until(due) < 29*time.Minute || time.Until(due) > 31*time.Minute {
		t.Fatalf("首次测智疑似应30分钟后复测：%v %d %s", suspected, attempts, due)
	}
	f.e.schedule()
	var concurrency int
	var buyerFacing bool
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT concurrency,buyer_facing FROM upstream_runtime_params WHERE upstream_id=$1`, f.upstreamID), &concurrency, &buyerFacing)
	if concurrency != 1 || !buyerFacing || f.marketItem(t) == nil {
		t.Fatalf("疑似应保留供给并发减半：%d %t", concurrency, buyerFacing)
	}
	f.admin.must("PUT", "/api/admin/inspection/checks/D07", map[string]any{"pricing": true}, nil)
	failedID, failed := f.run(t, "admin")
	if failed.QualityFactor == nil || !approx(*failed.QualityFactor, .7) || !failed.Checks["D07"].Applied {
		t.Fatalf("试运行测智扣价未生效：%+v", failed)
	}
	var q float64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT quality_factor FROM upstreams WHERE id=$1`, f.upstreamID), &q)
	if !approx(q, .7) {
		t.Fatalf("试运行须持久化Q，得到%g", q)
	}
	var reportID int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT id FROM test_reports WHERE run_id=$1`, failedID), &reportID)
	var report quality.Report
	status, raw := f.seller.call("GET", fmt.Sprintf("/api/seller/reports/%d", reportID), nil, &report)
	if status != http.StatusOK || strings.Contains(raw, "prompt") || strings.Contains(raw, "target_pass_rate") || strings.Contains(raw, "expected_pass_rate") {
		t.Fatalf("卖家报告泄露检测题面或模型通过率：%s", raw)
	}
	var public struct {
		Intelligence struct {
			Status string           `json:"status"`
			Models []map[string]any `json:"models"`
		} `json:"intelligence"`
		Checks map[string]inspection.Check `json:"checks"`
	}
	if err := json.Unmarshal(report.Summary, &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Intelligence.Models) != 1 || len(public.Intelligence.Models[0]) != 2 || public.Intelligence.Models[0]["model"] != "claude-sonnet-4-5" || public.Checks["D07"].Factor == nil || !public.Checks["D07"].Applied {
		t.Fatalf("卖家测智应仅公开模型状态并保留生效系数：%s", report.Summary)
	}
	// 只跑身份通过不能清除测智疑似；管理员可显式选择流程。
	var identityRun struct {
		RunID int64 `json:"run_id"`
	}
	f.admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/probe", f.upstreamID), map[string]any{"flows": []string{"identity"}}, &identityRun)
	f.execute(t, identityRun.RunID)
	f.assertHealth(t, "suspected")
	// 卖家复测检测异常流程，费用由卖家承担；通过后解除测智疑似与扣价。
	f.transport.setBad()
	var retest struct {
		RunID int64 `json:"run_id"`
	}
	f.seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retest", f.upstreamID), nil, &retest)
	restored := f.execute(t, retest.RunID)
	if restored.QualityFactor == nil || !approx(*restored.QualityFactor, 1) || restored.Verdict != "normal" {
		t.Fatalf("测智通过后应解除扣价：%+v", restored)
	}
	f.assertHealth(t, "healthy")
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT suspected_flows FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &suspected)
	if len(suspected) != 0 {
		t.Fatalf("已通过的测智流程应清除疑似：%v", suspected)
	}
	f.e.app.Ingester.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	if _, _, err := f.e.app.Ingester.RunOnce(f.e.ctx); err != nil {
		t.Fatal(err)
	}
	var borne, wrong int
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT count(*) FILTER(WHERE seller_borne),count(*) FILTER(WHERE seller_borne AND payout<>0) FROM usage_records WHERE upstream_id=$1 AND channel='probe'`, f.upstreamID), &borne, &wrong)
	if borne == 0 || wrong != 0 {
		t.Fatalf("上架与卖家复测须由卖家承担且不结算收益：%d %d", borne, wrong)
	}
}

func TestOnboardingCheckIsMandatory(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	for i := 1; i <= 2; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.4", "port": 5080 + i}, nil)
	}
	var mu sync.Mutex
	unavailable := false
	base := http.DefaultTransport
	if e.app.Quality.Prober.Client != nil && e.app.Quality.Prober.Client.Transport != nil {
		base = e.app.Quality.Prober.Client.Transport
	}
	e.app.Quality.Prober.Client = &http.Client{Transport: qualityRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		down := unavailable
		mu.Unlock()
		if down {
			return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"离线模拟上游不可用"}}`)), Request: r}, nil
		}
		return base.RoundTrip(r)
	})}
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
	unavailable = true
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
	if len(reports) != 1 || reports[0].Trigger != "onboarding" || reports[0].Verdict != "unavailable" {
		t.Fatalf("seller should see the failed onboarding report: %+v", reports)
	}
	// 修复后重新检测
	mu.Lock()
	bad[acc] = false
	unavailable = false
	mu.Unlock()
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retry-verification", up.ID), nil, nil)
	e.passOnboarding(up.ID)
	var health string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT health FROM upstreams WHERE id = $1", up.ID), &health)
	if health != "healthy" {
		t.Fatalf("passing the retried onboarding check should make the upstream schedulable: %s", health)
	}

	// 测智模型属于流程配置，启用时必须配置至少一个模型。
	admin.expect(400, "PUT", "/api/admin/detection-policies/anthropic", map[string]any{"flows": testDetectionFlows([]string{}, "self", false)})

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
		if upStatus != "verify_failed" || !strings.Contains(reason, "上架检测未通过："+runErr) {
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

	// worker 停止时整批重新排队，已完成实例供续跑复用。
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
	if status != "queued" || !cleared || results < 1 || results >= 12 {
		t.Fatalf("中断批次应重新排队并保留已完成实例: status=%s cleared=%v results=%d", status, cleared, results)
	}
	setHang(acc2, -1)
	e.passOnboarding(up2)

	// 冻结时限的宽限内不误判进程中断，超过宽限才失败。
	up3, _, batch3 := onboard("liam", -1)
	if _, err := e.app.DB.Exec(ctx, "UPDATE probe_runs SET status = 'running', started_at = now() - interval '24 minutes', deadline_at=now()-interval '4 minutes' WHERE batch_id = $1", batch3); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Quality.DispatchQueued(ctx); err != nil {
		t.Fatal(err)
	}
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM probe_runs WHERE batch_id = $1", batch3), &status)
	if status != "running" {
		t.Fatalf("冻结时限的宽限内不应误判中断：%s", status)
	}
	if _, err := e.app.DB.Exec(ctx, "UPDATE probe_runs SET started_at = now() - interval '26 minutes', deadline_at=now()-interval '6 minutes' WHERE batch_id = $1", batch3); err != nil {
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

type qualityRoundTripFunc func(*http.Request) (*http.Response, error)

func (f qualityRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
