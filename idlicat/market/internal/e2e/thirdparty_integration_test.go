//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/testutil/fakebazaar"
)

// 两条流程上架一起检测，第三方在内部题之前提交；身份不符按处置规则暂停，结束后临时令牌失效。
func TestThirdPartyDetection(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	admin := e.login("admin@market.test", "admin-password-1")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true}, nil)
	for i := 1; i <= 3; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.5", "port": 6080 + i}, nil)
	}
	ed := e.startEdge()
	e.app.Quality.EdgePublicURL = ed.url

	bl := fakebazaar.New()
	t.Cleanup(bl.Close)
	// http 地址的 Edge 只有开启 allow_insecure_target 才能交给检测站
	admin.must("PUT", "/api/admin/detection-providers/bazaarlink", map[string]any{"enabled": true, "base_url": bl.URL(), "max_runs_per_hour": 10}, nil)
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"enabled": true, "flows": testDetectionFlows([]string{"claude-sonnet-4-5"}, "third_party", true), "third_party": []map[string]any{{"provider": "bazaarlink"}}}, nil)
	admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"mode": "formal"}, nil)
	admin.expect(400, "PUT", "/api/admin/detection-policies/anthropic", map[string]any{"third_party": []map[string]any{{
		"provider": "nope", "triggers": []string{"onboarding"}}}})

	var mu sync.Mutex
	impostor := map[int64]bool{}
	submittedBeforeInternal := false
	e.fake.SetResponder(func(acc int64, prompt string) string {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(prompt, "Which model") || strings.Contains(prompt, "Which company"):
			if impostor[acc] {
				return "I am gpt-4o, trained by OpenAI."
			}
			return "I am claude-sonnet-4-5 by Anthropic."
		case strings.Contains(prompt, "17 * 23"):
			return "391"
		case strings.Contains(prompt, "READY"):
			return "READY"
		}
		if pongRe.MatchString(prompt) {
			var checks int
			err := e.app.DB.QueryRow(ctx, `SELECT count(*) FROM third_party_checks c JOIN upstreams u ON u.id=c.upstream_id WHERE u.sub2api_account_id=$1`, acc).Scan(&checks)
			if err != nil {
				t.Errorf("检查第三方前置提交：%v", err)
			}
			submittedBeforeInternal = submittedBeforeInternal || checks > 0
		}
		return "The code is " + pongRe.FindString(prompt)
	})

	seller := e.newUser("seller-tp@market.test")
	onboard := func(code string, bad bool) int64 {
		var up idOnly
		seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &up)
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", up.ID), nil, nil)
		seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", up.ID), map[string]string{"code": code}, nil)
		var acc int64
		mustScan(t, e.app.DB.QueryRow(ctx, "SELECT sub2api_account_id FROM upstreams WHERE id = $1", up.ID), &acc)
		mu.Lock()
		impostor[acc] = bad
		mu.Unlock()
		return up.ID
	}
	// wait 推进质量调度直到上游的检测记录全部结束（跳过轮询间隔）
	wait := func(upstreamID int64) {
		t.Helper()
		for i := 0; i < 60; i++ {
			e.tick()
			var open int
			mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND status IN ('queued', 'running', 'third_party')`,
				upstreamID), &open)
			if open == 0 {
				return
			}
			if _, err := e.app.DB.Exec(ctx, "UPDATE third_party_checks SET next_attempt_at = now() WHERE status IN ('pending', 'running')"); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("detection did not finish")
	}

	// 未允许 http 目标：身份流程没有结论，上架必须失败。
	good0 := onboard("tp-ivan", false)
	wait(good0)
	var status, errMsg string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, COALESCE(error, '') FROM third_party_checks WHERE upstream_id = $1", good0), &status, &errMsg)
	if status != "failed" || !strings.Contains(errMsg, "https") {
		t.Fatalf("insecure target should be refused: %s %s", status, errMsg)
	}
	var health string
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, health FROM upstreams WHERE id = $1", good0), &status, &health)
	if status != "verify_failed" || health != "unknown" {
		t.Fatalf("第三方没有结论时不得放行上架：%s %s", status, health)
	}

	admin.must("PUT", "/api/admin/detection-providers/bazaarlink", map[string]any{"settings": map[string]any{"allow_insecure_target": true}}, nil)
	good := onboard("tp-judy", false)
	bad := onboard("tp-kate", true)
	wait(good)
	wait(bad)

	// 卖家报告不下发第三方渠道：只有脱敏的核验结论、大类计数与风险项。
	type report struct {
		Verdict string `json:"verdict"`
		Summary struct {
			InternalVerdict string   `json:"internal_verdict"`
			Suggestions     []string `json:"suggestions"`
			Verification    struct {
				Status      string `json:"status"`
				VerdictText string `json:"verdict_text"`
				TrueModel   string `json:"true_model"`
			} `json:"verification"`
		} `json:"summary"`
	}
	reportOf := func(id int64) (report, map[string]json.RawMessage) {
		var rs []struct {
			Verdict string                     `json:"verdict"`
			Summary map[string]json.RawMessage `json:"summary"`
		}
		seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d/reports", id), nil, &rs)
		if len(rs) != 1 {
			t.Fatalf("reports of %d: %+v", id, rs)
		}
		raw := rs[0].Summary
		var r report
		// 直接把脱敏后的摘要解进断言结构；Raw 仅用于检查键是否存在。
		encoded, _ := json.Marshal(map[string]any{"verdict": rs[0].Verdict, "summary": raw})
		if err := json.Unmarshal(encoded, &r); err != nil {
			t.Fatal(err)
		}
		return r, raw
	}
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status, health FROM upstreams WHERE id = $1", good), &status, &health)
	r, raw := reportOf(good)
	// 渠道名、站点报告地址与 external_id 都不能出现在卖家响应里。
	encoded, _ := json.Marshal(raw)
	for _, secret := range []string{"provider", "report_url", "external_id", "bazaarlink", "BazaarLink"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("卖家报告泄露第三方渠道信息 %q：%s", secret, encoded)
		}
	}
	if status != "ready" || health != "healthy" || r.Verdict != "normal" || r.Summary.Verification.Status != "normal" ||
		r.Summary.Verification.VerdictText != "家族与具体型号都与声明一致" {
		t.Fatalf("matching identity should pass: %s %s %+v raw=%s", status, health, r, encoded)
	}
	if _, ok := raw["third_party"]; ok {
		t.Fatalf("卖家报告不应携带第三方逐条结果：%s", encoded)
	}

	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT status FROM upstreams WHERE id = $1", bad), &status)
	r, _ = reportOf(bad)
	if status != "verify_failed" || r.Verdict != "degraded" || r.Summary.Verification.Status != "degraded" ||
		r.Summary.Verification.TrueModel != "gpt-4o" || r.Summary.Verification.VerdictText != "证据指向其他模型家族" {
		t.Fatalf("identity mismatch should fail onboarding: %s %+v", status, r)
	}
	// 管理端仍保留渠道名与站点报告地址。
	var adminRuns []struct {
		Status  string `json:"status"`
		Summary struct {
			ThirdParty []struct {
				Provider  string         `json:"provider"`
				Outcome   string         `json:"outcome"`
				Verdict   string         `json:"verdict"`
				ReportURL string         `json:"report_url"`
				Summary   map[string]any `json:"summary"`
			} `json:"third_party"`
		} `json:"summary"`
	}
	admin.must("GET", fmt.Sprintf("/api/admin/upstreams/%d/probe-runs", good), nil, &adminRuns)
	found := false
	for _, run := range adminRuns {
		if len(run.Summary.ThirdParty) != 1 {
			continue
		}
		entry := run.Summary.ThirdParty[0]
		if entry.Outcome == "pass" && entry.Verdict == "normal" && strings.HasPrefix(entry.ReportURL, bl.URL()) && entry.Summary["verdict_status"] == "clean_match" {
			found = true
		}
	}
	if !found {
		t.Fatalf("管理端应保留第三方渠道详情：%+v", adminRuns)
	}

	mu.Lock()
	early := submittedBeforeInternal
	mu.Unlock()
	if !early {
		t.Fatal("第三方必须先于内部题提交")
	}
	var thirdPartyCalibrated bool
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT COALESCE((value->'check_overrides'->'third_party'->>'calibrated')::bool,false) FROM settings WHERE key='inspection'`), &thirdPartyCalibrated)
	if thirdPartyCalibrated {
		t.Fatal("本测试须验证未校准第三方在正式模式仍按身份不符暂停")
	}

	// 检测结束后临时令牌失效
	keys := bl.APIKeys()
	if len(keys) != 2 || !strings.HasPrefix(keys[0], "tm-tp-") {
		t.Fatalf("detector keys: %v", keys)
	}
	time.Sleep(200 * time.Millisecond)
	if code, _ := ed.call(keys[0]); code != 401 {
		t.Fatalf("revoked detector token should be rejected, got %d", code)
	}
	var checks []struct {
		UpstreamID int64           `json:"upstream_id"`
		Status     string          `json:"status"`
		Summary    json.RawMessage `json:"summary"`
	}
	admin.must("GET", "/api/admin/third-party-checks", nil, &checks)
	if len(checks) != 3 {
		t.Fatalf("admin third-party checks: %+v", checks)
	}
	var providers []struct {
		Name         string `json:"name"`
		Enabled      bool   `json:"enabled"`
		RunsLastHour int    `json:"runs_last_hour"`
	}
	admin.must("GET", "/api/admin/detection-providers", nil, &providers)
	if len(providers) != 1 || !providers[0].Enabled || providers[0].RunsLastHour != 2 {
		t.Fatalf("providers: %+v", providers)
	}

	// 只跑第三方身份：没有内部题，跳过执行计划，仍需等待身份结论。
	var identityRun struct {
		RunID int64 `json:"run_id"`
	}
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/probe", good), map[string]any{"flows": []string{"identity"}}, &identityRun)
	wait(good)
	var internal, submitted int
	var source string
	var flows []string
	var identityStatus string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT flows,identity_source,summary->'identity'->>'status',
        (SELECT count(*) FROM probe_results WHERE run_id=r.id),(SELECT count(*) FROM third_party_checks WHERE run_id=r.id)
        FROM probe_runs r WHERE id=$1`, identityRun.RunID), &flows, &source, &identityStatus, &internal, &submitted)
	if internal != 0 || submitted != 1 || source != "third_party" || len(flows) != 1 || flows[0] != "identity" || identityStatus != "pass" {
		t.Fatalf("纯第三方运行不应生成内部题且须正常结案：%v %s %s %d %d", flows, source, identityStatus, internal, submitted)
	}

	// 检测站关闭后不再追加第三方检测
	admin.must("PUT", "/api/admin/detection-providers/bazaarlink", map[string]any{"enabled": false}, nil)
	admin.must("POST", fmt.Sprintf("/api/admin/upstreams/%d/probe", good), nil, nil)
	wait(good)
	var n int
	mustScan(t, e.app.DB.QueryRow(ctx, "SELECT count(*) FROM third_party_checks WHERE upstream_id = $1", good), &n)
	if n != 2 {
		t.Fatalf("disabled provider should not get new checks: %d", n)
	}

	admin.must("PUT", "/api/admin/detection-providers/bazaarlink", map[string]any{"enabled": true}, nil)
	for i := 4; i <= 5; i++ {
		admin.must("POST", "/api/admin/proxies", map[string]any{"name": fmt.Sprintf("p%d", i), "protocol": "socks5", "host": "10.0.0.5", "port": 6080 + i}, nil)
	}
	base := http.DefaultTransport
	if e.app.Quality.Prober.Client != nil && e.app.Quality.Prober.Client.Transport != nil {
		base = e.app.Quality.Prober.Client.Transport
	}
	initialKeys := len(bl.APIKeys())

	// 第三方先完成也须等待内部题；worker 中断续跑沿用已有提交。
	entered := make(chan struct{})
	var enteredOnce sync.Once
	e.app.Quality.Prober.Client = &http.Client{Transport: qualityRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		enteredOnce.Do(func() { close(entered) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	earlyUpstream := onboard("tp-early-finish", false)
	e.drain()
	var earlyRun int64
	var earlyBatch string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT id,batch_id FROM probe_runs WHERE upstream_id=$1 AND status='queued'`, earlyUpstream), &earlyRun, &earlyBatch)
	workerCtx, stopWorker := context.WithTimeout(ctx, 20*time.Second)
	defer stopWorker()
	workerDone := make(chan error, 1)
	go func() { workerDone <- e.app.Quality.ExecuteBatch(workerCtx, earlyBatch) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("内部题未开始，无法验证第三方提前完成")
	}
	var externalID string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT external_id FROM third_party_checks WHERE run_id=$1`, earlyRun), &externalID)
	bl.Wait(externalID)
	if _, err := e.app.DB.Exec(ctx, `UPDATE third_party_checks SET next_attempt_at=now() WHERE run_id=$1`, earlyRun); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Quality.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var checkStatus, upstreamStatus string
	var reports int
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT r.status,u.status,c.status,(SELECT count(*) FROM test_reports WHERE run_id=r.id)
        FROM probe_runs r JOIN upstreams u ON u.id=r.upstream_id JOIN third_party_checks c ON c.run_id=r.id WHERE r.id=$1`, earlyRun), &status, &upstreamStatus, &checkStatus, &reports)
	if status != "running" || upstreamStatus != "verifying" || checkStatus != "completed" || reports != 0 {
		t.Fatalf("第三方提前完成不得结案：%s %s %s 报告=%d", status, upstreamStatus, checkStatus, reports)
	}
	stopWorker()
	if err := <-workerDone; err != nil {
		t.Fatal(err)
	}
	e.app.Quality.Prober.Client = &http.Client{Transport: base}
	wait(earlyUpstream)
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT count(*) FROM third_party_checks WHERE run_id=$1`, earlyRun), &n)
	if n != 1 || len(bl.APIKeys()) != initialKeys+1 {
		t.Fatalf("重新排队不得重复提交第三方：记录=%d 令牌=%d", n, len(bl.APIKeys())-initialKeys)
	}
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status FROM upstreams WHERE id=$1`, earlyUpstream), &status)
	if status != "ready" {
		t.Fatalf("内部续跑结束后应合并第三方正常结案：%s", status)
	}

	// 仅内部请求不可用，第三方已提交的临时令牌必须立即撤销。
	admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"mode": "trial"}, nil)
	e.app.Quality.Prober.Client = &http.Client{Transport: qualityRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"离线模拟内部不可用"}}`)), Request: r}, nil
	})}
	downUpstream := onboard("tp-internal-unavailable", false)
	e.drain()
	var downRun int64
	var downBatch string
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT id,batch_id FROM probe_runs WHERE upstream_id=$1 AND status='queued'`, downUpstream), &downRun, &downBatch)
	if err := e.app.Quality.ExecuteBatch(ctx, downBatch); err != nil {
		t.Fatal(err)
	}
	var revoked bool
	mustScan(t, e.app.DB.QueryRow(ctx, `SELECT c.status,c.token_revoked,c.external_id,u.status FROM third_party_checks c JOIN upstreams u ON u.id=c.upstream_id WHERE c.run_id=$1`, downRun), &checkStatus, &revoked, &externalID, &upstreamStatus)
	if checkStatus != "skipped" || !revoked || upstreamStatus != "verify_failed" {
		t.Fatalf("内部不可用须撤销第三方并阻止上架：%s 撤销=%v 上游=%s", checkStatus, revoked, upstreamStatus)
	}
	bl.Wait(externalID)
	keys = bl.APIKeys()
	time.Sleep(200 * time.Millisecond)
	if code, _ := ed.call(keys[len(keys)-1]); code != http.StatusUnauthorized {
		t.Fatalf("内部不可用后的令牌应失效：HTTP %d", code)
	}
}
