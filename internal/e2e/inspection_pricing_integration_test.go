//go:build integration

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/market"
	"github.com/DueGin/token-market/internal/quality"
	"github.com/DueGin/token-market/internal/scheduling"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/testutil/fakesub2api"
)

type inspectionPricingFixture struct {
	e          *env
	admin      *client
	seller     *client
	upstreamID int64
	listingID  int64
	onboarding int64
	transport  *inspectionPricingTransport
}

type inspectionPricingSummary struct {
	Verdict       string                      `json:"verdict"`
	Grade         string                      `json:"grade"`
	Mode          string                      `json:"inspection_mode"`
	QualityFactor *float64                    `json:"quality_factor"`
	Checks        map[string]inspection.Check `json:"checks"`
	Gates         []inspection.Gate           `json:"gates"`
}

// 所有请求均发往本地替身；按冻结请求登记质检项，故障只影响指定账号和指定项。
type inspectionPricingTransport struct {
	base http.RoundTripper
	mu   sync.RWMutex
	key  string
	bad  map[string]bool
	rows map[[32]byte]string
}

func (tr *inspectionPricingTransport) setBad(keys ...string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.bad = map[string]bool{}
	for _, key := range keys {
		tr.bad[key] = true
	}
}

func (tr *inspectionPricingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))
	tr.mu.RLock()
	check := tr.rows[sha256.Sum256(raw)]
	bad := req.Header.Get("x-api-key") == tr.key && tr.bad[check]
	tr.mu.RUnlock()
	if !bad {
		return tr.base.RoundTrip(req)
	}
	status := http.StatusOK
	var body any
	if check == "A04" {
		status = http.StatusForbidden
		body = map[string]any{"error": map[string]string{"type": "permission_denied", "code": "model_not_allowed", "message": "离线测试：该账号没有此型号权限"}}
	} else {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		body = map[string]any{"type": "message", "model": request.Model, "content": []map[string]string{{"type": "text", "text": "测试错误答案"}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 16, "output_tokens": 16}}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded)), Request: req}, nil
}

func newInspectionPricingFixture(t *testing.T) *inspectionPricingFixture {
	t.Helper()
	e := setup(t)
	admin := e.login("admin@market.test", "admin-password-1")
	var defaults inspection.Settings
	admin.must("GET", "/api/admin/inspection/settings", nil, &defaults)
	if defaults.Mode != inspection.ModeTrial || defaults.Scheduling.Enabled {
		t.Fatalf("质检与调度默认值不安全：%+v", defaults)
	}
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"enabled": true, "default_concurrency": 2, "appraisal_base": map[string]float64{"default": .3}}, nil)
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "正式质检离线代理", "protocol": "socks5", "host": "10.0.0.4", "port": 2083}, nil)
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"enabled": true, "scheduled_enabled": false, "item_parallelism": 8}, nil)
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-正式质检保障池", "platform": "anthropic", "sell_multiplier": .8}, nil)

	// 基线先通过旧流程；仅在独立测试数据库中登记运营确认的健康状态。
	var reference struct {
		Upstream idOnly `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &reference)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", reference.Upstream.ID), nil, nil)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", reference.Upstream.ID), map[string]string{"code": "pricing-reference"}, nil)
	e.passOnboarding(reference.Upstream.ID)
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE upstreams SET health='healthy' WHERE id=$1`, reference.Upstream.ID); err != nil {
		t.Fatal(err)
	}

	// setup 的口令题属于管理员自建项；换成真实内置题，检验正式模式的旧题隔离。
	if _, err := e.app.DB.Exec(e.ctx, `DELETE FROM probe_results`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(e.ctx, `DELETE FROM probe_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.SeedDefaultItems(e.ctx, e.app.DB); err != nil {
		t.Fatal(err)
	}
	var legacy int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*) FROM probe_items WHERE suite NOT LIKE 'inspection_%'`), &legacy)
	if legacy != 22 {
		t.Fatalf("旧内置题夹具应保留 22 道，得到 %d", legacy)
	}
	var before, after []byte
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT value FROM settings WHERE key='platform'`), &before)
	admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"mode": "formal", "scheduling": map[string]any{"enabled": true}}, nil)
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT value FROM settings WHERE key='platform'`), &after)
	if !bytes.Equal(before, after) {
		t.Fatal("独立 inspection 设置改动了 platform 设置")
	}
	for _, key := range []string{"D07", "D09", "A04"} {
		calibrateInspectionPricingCheck(t, admin, key)
	}
	// enabled 本身不能成为校准凭据。
	admin.must("PUT", "/api/admin/inspection/checks/D08", map[string]any{"enabled": true, "calibrated": false}, nil)
	var checks []inspection.EffectiveDefinition
	admin.must("GET", "/api/admin/inspection/checks", nil, &checks)
	for _, check := range checks {
		if check.Key == "D08" && (!check.Enabled || check.Calibrated) {
			t.Fatalf("未校准的启用规则状态错误：%+v", check)
		}
	}

	tr := &inspectionPricingTransport{base: http.DefaultTransport, bad: map[string]bool{}, rows: map[[32]byte]string{}}
	if e.app.Quality.Prober.Client != nil && e.app.Quality.Prober.Client.Transport != nil {
		tr.base = e.app.Quality.Prober.Client.Transport
	}
	e.app.Quality.Prober.Client = &http.Client{Transport: tr}
	e.app.Quality.RegisterInstance = func(instance quality.ProbeRegistration) {
		if instance.CheckKey == "" {
			return
		}
		tr.mu.Lock()
		tr.rows[sha256.Sum256(instance.WireBody)] = instance.CheckKey
		tr.mu.Unlock()
		response := fakesub2api.GatewayResponse{Text: instance.Answer, ToolCalls: []fakesub2api.GatewayToolCall{}}
		for _, call := range instance.Grader.Expect {
			response.ToolCalls = append(response.ToolCalls, fakesub2api.GatewayToolCall{Name: call.Name, Arguments: call.Args})
		}
		if err := e.fake.RegisterProbeAnswer(instance.WireBody, response); err != nil {
			t.Errorf("登记离线标准答案：%v", err)
		}
	}
	identity, err := e.app.Shadow.Get(e.ctx, buyer.KindProbe, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Admin.UpdateBalance(e.ctx, *identity.Sub2APIUserID, adminclient.BalanceAdd, 10000, "离线质检测试", "inspection-pricing-fixture"); err != nil {
		t.Fatal(err)
	}

	seller := e.newUser("pricing-seller@market.test")
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "正式质检被测离线代理", "protocol": "socks5", "host": "10.0.0.5", "port": 2084}, nil)
	var upstream listing.Upstream
	seller.must("POST", "/api/seller/upstreams", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &upstream)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", upstream.ID), nil, nil)
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", upstream.ID), map[string]string{"code": "pricing-target"}, &upstream)
	if upstream.Status != "verifying" {
		t.Fatalf("正式上架应等待检测，得到 %s", upstream.Status)
	}
	var groupID, onboarding int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT probe_group_id FROM upstreams WHERE id=$1`, upstream.ID), &groupID)
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT id FROM probe_runs WHERE upstream_id=$1 AND trigger='onboarding' ORDER BY id DESC LIMIT 1`, upstream.ID), &onboarding)
	f := &inspectionPricingFixture{e: e, admin: admin, seller: seller, upstreamID: upstream.ID, onboarding: onboarding, transport: tr}
	f.execute(t, onboarding)
	var encrypted []byte
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT key_enc FROM system_keys WHERE sub2api_group_id=$1`, groupID), &encrypted)
	tr.key, err = e.app.Keyring.OpenString(encrypted, fmt.Sprintf("system-key:%d", groupID))
	if err != nil {
		t.Fatal(err)
	}
	f.assertHealth(t, "healthy")
	seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", upstream.ID), nil, &upstream)
	if upstream.Status != "ready" || upstream.Grade == nil || *upstream.Grade != "A" {
		t.Fatalf("首次正式上架最高 A：%+v", upstream)
	}
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", upstream.ID), map[string]any{"mode": "consignment", "multiplier": .45, "categories": []string{"coding"}}, &created)
	f.listingID = created.Listing.ID
	e.schedule()
	return f
}

func calibrateInspectionPricingCheck(t *testing.T, admin *client, key string) {
	t.Helper()
	// 只为离线夹具激活计算路径；这不是上线所需的真实 A/A、阳性对照校准。
	admin.must("PUT", "/api/admin/inspection/checks/"+key, map[string]any{"enabled": true, "calibrated": true, "calibration_note": "离线测试夹具，非真实校准；Anthropic 同型号替身、v0.2.14 契约、固定标准答案与单项故障对照，记录 inspection-pricing-fixture。"}, nil)
}

func (f *inspectionPricingFixture) execute(t *testing.T, id int64) inspectionPricingSummary {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.e.ctx, 90*time.Second)
	defer cancel()
	var batch string
	mustScan(t, f.e.app.DB.QueryRow(ctx, `SELECT batch_id FROM probe_runs WHERE id=$1`, id), &batch)
	if err := f.e.app.Quality.ExecuteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var status string
	var raw []byte
	mustScan(t, f.e.app.DB.QueryRow(ctx, `SELECT status,summary FROM probe_runs WHERE id=$1`, id), &status, &raw)
	if status != "done" {
		t.Fatalf("正式检测未完成：%s %s", status, raw)
	}
	var summary inspectionPricingSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Mode != "formal" || len(summary.Checks) == 0 {
		t.Fatalf("缺少正式质检目录：%s", raw)
	}
	return summary
}

func (f *inspectionPricingFixture) run(t *testing.T, trigger string) (int64, inspectionPricingSummary) {
	t.Helper()
	id, err := f.e.app.Quality.QueueRun(f.e.ctx, f.upstreamID, trigger)
	if err != nil {
		t.Fatal(err)
	}
	return id, f.execute(t, id)
}

func (f *inspectionPricingFixture) assertHealth(t *testing.T, want string) {
	t.Helper()
	var got string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT health FROM upstreams WHERE id=$1`, f.upstreamID), &got)
	if got != want {
		t.Fatalf("健康状态应为 %s，得到 %s", want, got)
	}
}

func (f *inspectionPricingFixture) marketItem(t *testing.T) *market.Item {
	t.Helper()
	var items []market.Item
	f.e.anon().must("GET", "/api/market/listings", nil, &items)
	for i := range items {
		if items[i].ListingID == f.listingID {
			return &items[i]
		}
	}
	return nil
}

func TestInspectionPricingFormalOnboardingQuickAndAppraisal(t *testing.T) {
	f := newInspectionPricingFixture(t)
	e := f.e
	var full, generated, bad, legacy int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*),count(*) FILTER (WHERE generator_version<>''),count(*) FILTER (WHERE passed IS NOT TRUE OR input_tokens IS NULL OR input_tokens<=0),count(*) FILTER (WHERE suite NOT LIKE 'inspection_%') FROM probe_results WHERE run_id=$1`, f.onboarding), &full, &generated, &bad, &legacy)
	if full != 90 || generated != full || bad != 0 || legacy != 0 {
		t.Fatalf("完整质检应为 90 次新题请求：总数=%d 版本=%d 错误=%d 旧题=%d", full, generated, bad, legacy)
	}
	var same, singleSnapshot bool
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT v.checks=r.summary->'checks', (SELECT count(*)=1 FROM probe_runs p WHERE p.batch_id=r.batch_id AND p.execution_snapshot<>'{}'::jsonb) FROM quality_verdicts v JOIN probe_runs r ON r.id=v.run_id WHERE v.run_id=$1`, f.onboarding), &same, &singleSnapshot)
	if !same || !singleSnapshot {
		t.Fatal("checks 持久化不一致或同一批次重复保存快照")
	}
	var upstream listing.Upstream
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
	item := f.marketItem(t)
	if upstream.QualityFactor == nil || !approx(*upstream.QualityFactor, 1) || upstream.Appraisal.Multiplier == nil || !approx(*upstream.Appraisal.Multiplier, .3) || item == nil || item.Appraisal.Multiplier == nil || !approx(*item.Appraisal.Multiplier, .3) || item.Grade == nil || *item.Grade != "A" || item.LastCheckedAt == nil {
		t.Fatalf("卖家和市场应展示独立估价：上游=%+v 市场=%+v", upstream.InspectionView, item)
	}
	for _, check := range item.QualityChecks {
		if len(check.Metrics) != 0 || check.Evidence.Code != "" || check.Evidence.GeneratorVersion != nil {
			t.Fatalf("市场泄露内部质检定位：%+v", check)
		}
	}
	checked := upstream.LastCheckedAt
	f.admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"appraisal_base": map[string]float64{"default": .4}}, nil)
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
	item = f.marketItem(t)
	if upstream.Appraisal.Multiplier == nil || !approx(*upstream.Appraisal.Multiplier, .4) || item == nil || item.Appraisal.Multiplier == nil || !approx(*item.Appraisal.Multiplier, .4) || checked == nil || upstream.LastCheckedAt == nil || !checked.Equal(*upstream.LastCheckedAt) {
		t.Fatal("修改估价基准应立即重算价格，并保留原检测时间")
	}
	// 去掉独立估价配置后不能回退到回收基准。
	f.admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"appraisal_base": map[string]float64{}}, nil)
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
	if upstream.Appraisal.Configured || upstream.Appraisal.Multiplier != nil {
		t.Fatal("未配置 appraisal_base 时不应提供估价")
	}
	quickID, summary := f.run(t, "scheduled")
	var mode string
	var quick int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT r.mode,(SELECT count(*) FROM probe_results WHERE run_id=r.id) FROM probe_runs r WHERE id=$1`, quickID), &mode, &quick)
	if mode != "quick" || quick != 25 || summary.Verdict != "normal" || summary.QualityFactor == nil || !approx(*summary.QualityFactor, 1) {
		t.Fatalf("正式快检应为 25 次请求：模式=%s 数量=%d 摘要=%+v", mode, quick, summary)
	}
}

func TestInspectionPricingPermissionGateAndEffectiveRecovery(t *testing.T) {
	f := newInspectionPricingFixture(t)
	f.transport.setBad("A04")
	_, failed := f.run(t, "admin")
	confirmed := false
	for _, gate := range failed.Gates {
		confirmed = confirmed || gate.CheckKey == "A04" && gate.Confirmed
	}
	if failed.Verdict != "unavailable" || failed.QualityFactor != nil || !confirmed {
		t.Fatalf("明确权限拒绝应触发已校准门槛且不估价：%+v", failed)
	}
	f.assertHealth(t, "unavailable")
	f.e.schedule()
	var buyerFacing bool
	var groups []int64
	var probeGroup int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT rp.buyer_facing,rp.group_ids,u.probe_group_id FROM upstream_runtime_params rp JOIN upstreams u ON u.id=rp.upstream_id WHERE u.id=$1`, f.upstreamID), &buyerFacing, &groups, &probeGroup)
	if buyerFacing || len(groups) != 1 || groups[0] != probeGroup || f.marketItem(t) != nil {
		t.Fatalf("门槛应暂停买家流量且保留复测组：buyer_facing=%v groups=%v", buyerFacing, groups)
	}
	var upstream listing.Upstream
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
	if upstream.Appraisal.Multiplier != nil {
		t.Fatal("门槛暂停期间不应估价")
	}
	f.transport.setBad()
	for attempt := 1; attempt <= 2; attempt++ {
		id, recovered := f.run(t, "recovery")
		var mode string
		var failedChecks []string
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT mode FROM probe_runs WHERE id=$1`, id), &mode)
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failedChecks)
		if mode != "full" || recovered.Verdict != "normal" || recovered.Checks["A04"].Status != "pass" || recovered.Checks["A04"].Evidence.Source == "history" {
			t.Fatalf("恢复须完整且有效覆盖旧失败项：%s %+v", mode, recovered)
		}
		if attempt == 1 {
			f.assertHealth(t, "unavailable")
			if len(failedChecks) == 0 {
				t.Fatal("第一次恢复通过不应清除待确认失败项")
			}
		} else {
			f.assertHealth(t, "healthy")
			if len(failedChecks) != 0 {
				t.Fatalf("第二次有效恢复后应清除失败项：%v", failedChecks)
			}
		}
	}
	f.e.schedule()
	if f.marketItem(t) == nil {
		t.Fatal("两次有效恢复后挂单未重新进入市场")
	}
}

func TestInspectionPricingDiscountAppealAndQuickHistory(t *testing.T) {
	f := newInspectionPricingFixture(t)
	f.transport.setBad("D08")
	_, uncalibrated := f.run(t, "admin")
	if uncalibrated.Checks["D08"].Status != "fail" || uncalibrated.Checks["D08"].Applied || uncalibrated.QualityFactor == nil || !approx(*uncalibrated.QualityFactor, 1) {
		t.Fatalf("enabled=true 不能把未校准失败变成扣价：%+v", uncalibrated)
	}
	calibrateInspectionPricingCheck(t, f.admin, "D08")
	f.transport.setBad("D08", "D09")
	fullID, full := f.run(t, "admin")
	if full.Verdict != "normal" || full.QualityFactor == nil || !approx(*full.QualityFactor, .85) || !full.Checks["D08"].Applied || !full.Checks["D09"].Applied {
		t.Fatalf("非门槛扣价应保持正常健康：%+v", full)
	}
	f.assertHealth(t, "healthy")
	// 本轮快检没有长上下文题；即使当前所有已测题通过，也不能清除旧上下文失败。
	f.transport.setBad()
	quickID, quick := f.run(t, "scheduled")
	var askedContext int
	var failedChecks []string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT count(*) FROM probe_results WHERE run_id=$1 AND check_key='D09'`, quickID), &askedContext)
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failedChecks)
	old, inherited := full.Checks["D09"], quick.Checks["D09"]
	if askedContext != 0 || inherited.Status != "warning" || inherited.Evidence.Source != "history" || !inherited.CheckedAt.Equal(old.CheckedAt) || !inherited.Applied || !strings.Contains(strings.Join(failedChecks, ","), "D09") || quick.QualityFactor == nil || !approx(*quick.QualityFactor, .85) {
		t.Fatalf("快检缺测不得推成上下文修复：原项=%+v 继承项=%+v 失败项=%v", old, inherited, failedChecks)
	}
	var reportID int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT id FROM test_reports WHERE run_id=$1`, fullID), &reportID)
	var appeal idOnly
	f.seller.must("POST", fmt.Sprintf("/api/seller/reports/%d/appeal", reportID), map[string]string{"reason": "已修复完整输出和上下文，请复检正式扣价项。"}, &appeal)
	f.admin.must("POST", fmt.Sprintf("/api/admin/appeals/%d/resolve", appeal.ID), map[string]string{"action": "retest", "note": "离线单项故障已关闭，安排完整复测。"}, nil)
	var appealRun int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT id FROM probe_runs WHERE upstream_id=$1 AND trigger='appeal' ORDER BY id DESC LIMIT 1`, f.upstreamID), &appealRun)
	retested := f.execute(t, appealRun)
	var status string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT status FROM appeals WHERE id=$1`, appeal.ID), &status)
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failedChecks)
	if status != "overturned" || retested.Verdict != "normal" || retested.QualityFactor == nil || !approx(*retested.QualityFactor, 1) || len(failedChecks) != 0 {
		t.Fatalf("完整复测通过后应撤销扣价申诉：状态=%s 失败项=%v 摘要=%+v", status, failedChecks, retested)
	}
}

func TestInspectionPricingTrialAutomaticallySamplesWithoutApplying(t *testing.T) {
	e := setup(t)
	upstreamID, accountID, key := inspectionProbeAccount(t, e)
	admin := e.login("admin@market.test", "admin-password-1")
	seller := e.login("inspection-seller@market.test", "password-123")
	admin.must("PUT", "/api/admin/vendors/anthropic", map[string]any{"appraisal_base": map[string]float64{"default": .3}}, nil)
	admin.must("POST", "/api/admin/proxies", map[string]any{"name": "试运行基线离线代理", "protocol": "socks5", "host": "10.0.0.6", "port": 2085}, nil)
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-试运行兼容保障池", "platform": "anthropic", "sell_multiplier": .8}, nil)
	var reference struct {
		Upstream idOnly `json:"upstream"`
	}
	admin.must("POST", "/api/admin/reference-accounts", map[string]string{"platform": "anthropic", "auth_type": "oauth"}, &reference)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/start", reference.Upstream.ID), nil, nil)
	admin.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/oauth/complete", reference.Upstream.ID), map[string]string{"code": "trial-pricing-reference"}, nil)
	e.passOnboarding(reference.Upstream.ID)
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE upstreams SET health='healthy',tier=(SELECT tier FROM upstreams WHERE id=$2) WHERE id=$1`, reference.Upstream.ID, upstreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.SyncInspectionItems(e.ctx, e.app.DB); err != nil {
		t.Fatal(err)
	}
	var policySuites []string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT suites FROM detection_policies WHERE platform='anthropic' AND auth_type=''`), &policySuites)
	for _, suite := range policySuites {
		if strings.HasPrefix(suite, "inspection_") {
			t.Fatal("试运行夹具不应显式启用新套件")
		}
	}
	for _, check := range []string{"D07", "D09", "D08"} {
		calibrateInspectionPricingCheck(t, admin, check)
	}
	// 即使调度开关开启，trial 仍按原规划器运行。
	admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"scheduling": map[string]any{"enabled": true, "minimum_grade": "S"}}, nil)
	tr := &inspectionPricingTransport{base: http.DefaultTransport, key: key, bad: map[string]bool{}, rows: map[[32]byte]string{}}
	if e.app.Quality.Prober.Client != nil && e.app.Quality.Prober.Client.Transport != nil {
		tr.base = e.app.Quality.Prober.Client.Transport
	}
	e.app.Quality.Prober.Client = &http.Client{Transport: tr}
	e.app.Quality.RegisterInstance = func(instance quality.ProbeRegistration) {
		if instance.CheckKey == "" {
			return
		}
		tr.mu.Lock()
		tr.rows[sha256.Sum256(instance.WireBody)] = instance.CheckKey
		tr.mu.Unlock()
		response := fakesub2api.GatewayResponse{Text: instance.Answer, ToolCalls: []fakesub2api.GatewayToolCall{}}
		for _, call := range instance.Grader.Expect {
			response.ToolCalls = append(response.ToolCalls, fakesub2api.GatewayToolCall{Name: call.Name, Arguments: call.Args})
		}
		if err := e.fake.RegisterProbeAnswer(instance.WireBody, response); err != nil {
			t.Errorf("登记试运行离线答案：%v", err)
		}
	}
	// 零输出用量让旧题的吞吐指标保持未知，避免亚毫秒观测入库舍入影响复算。
	var referenceAccount int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT sub2api_account_id FROM upstreams WHERE id=$1`, reference.Upstream.ID), &referenceAccount)
	for _, id := range []int64{accountID, referenceAccount} {
		e.fake.SetAccountUsage(id, &fakesub2api.GatewayUsage{InputTokens: 11})
	}
	identity, err := e.app.Shadow.Get(e.ctx, buyer.KindProbe, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Admin.UpdateBalance(e.ctx, *identity.Sub2APIUserID, adminclient.BalanceAdd, 10000, "试运行离线测试", "inspection-trial-pricing-fixture"); err != nil {
		t.Fatal(err)
	}
	var created struct {
		Listing idOnly `json:"listing"`
	}
	seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/listing", upstreamID), map[string]any{"mode": "consignment", "multiplier": .45, "categories": []string{"coding"}}, &created)
	tr.setBad("D08", "D09")
	for _, tc := range []struct {
		trigger, mode string
		newRequests   int
		q             float64
	}{{"admin", "full", 90, .85}, {"scheduled", "quick", 25, 1}} {
		t.Run(tc.mode, func(t *testing.T) {
			id, err := e.app.Quality.QueueRun(e.ctx, upstreamID, tc.trigger)
			if err != nil {
				t.Fatal(err)
			}
			var batch string
			mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT batch_id FROM probe_runs WHERE id=$1`, id), &batch)
			ctx, cancel := context.WithTimeout(e.ctx, 90*time.Second)
			defer cancel()
			if err := e.app.Quality.ExecuteBatch(ctx, batch); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			var status, mode string
			var newRequests, oldRequests int
			mustScan(t, e.app.DB.QueryRow(ctx, `SELECT status,mode,summary,(SELECT count(*) FROM probe_results WHERE run_id=r.id AND suite LIKE 'inspection_%'),(SELECT count(*) FROM probe_results WHERE run_id=r.id AND suite NOT LIKE 'inspection_%') FROM probe_runs r WHERE id=$1`, id), &status, &mode, &raw, &newRequests, &oldRequests)
			var summary struct {
				inspectionPricingSummary
				Suites    map[string]json.RawMessage `json:"suites"`
				Appraisal inspection.Appraisal       `json:"appraisal"`
			}
			if err := json.Unmarshal(raw, &summary); err != nil {
				t.Fatal(err)
			}
			if status != "done" || mode != tc.mode || summary.Mode != "trial" || newRequests != tc.newRequests || oldRequests != 12 || summary.QualityFactor == nil || !approx(*summary.QualityFactor, tc.q) || summary.Appraisal.Multiplier == nil || !approx(*summary.Appraisal.Multiplier, .3*tc.q) || !summary.Appraisal.Provisional {
				t.Fatalf("默认试运行应自动采新题并展示暂估价：状态=%s 模式=%s 新题=%d 旧题=%d 摘要=%s", status, mode, newRequests, oldRequests, raw)
			}
			for _, check := range summary.Checks {
				if check.Applied || !strings.Contains(check.Impact, "试运行") {
					t.Fatalf("试运行项不得应用或丢失局限声明：%+v", check)
				}
			}
			legacy := inspectionPricingLegacyAssessment(t, e, id, upstreamID)
			if summary.Verdict != legacy.Verdict || summary.Grade != legacy.Grade {
				t.Fatalf("影子题改变旧判定：摘要=%s/%s 旧题复算=%s/%s", summary.Verdict, summary.Grade, legacy.Verdict, legacy.Grade)
			}
			wantSuites := inspectionPricingLegacySuites(legacy)
			for key, want := range wantSuites {
				var got any
				if err := json.Unmarshal(summary.Suites[key], &got); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				var normalized any
				if err := json.Unmarshal(encoded, &normalized); err != nil || !reflect.DeepEqual(got, normalized) {
					t.Fatalf("旧套件 %s 被新题污染：%s，期望 %s", key, summary.Suites[key], encoded)
				}
			}
			if len(summary.Suites) != len(wantSuites) {
				t.Fatalf("旧套件结构改变：%v", summary.Suites)
			}
			var health, grade string
			var factor float64
			mustScan(t, e.app.DB.QueryRow(ctx, `SELECT health,grade,quality_factor FROM upstreams WHERE id=$1`, upstreamID), &health, &grade, &factor)
			if legacy.Verdict != "normal" || health != "healthy" || grade != legacy.Grade || !approx(factor, 1) {
				t.Fatalf("试运行修改旧健康/等级/Q：%s %s %g，旧判定=%+v", health, grade, factor, legacy)
			}
			e.schedule()
			platformSettings, err := settings.Load(ctx, e.app.DB)
			if err != nil {
				t.Fatal(err)
			}
			pc, ups, err := scheduling.LoadPlanInputs(ctx, e.app.DB, platformSettings)
			if err != nil {
				t.Fatal(err)
			}
			legacyContext := pc
			legacyContext.Inspection = inspection.Settings{}
			legacyPlan, trialPlan := scheduling.Plan(legacyContext, ups), scheduling.Plan(pc, ups)
			if !reflect.DeepEqual(trialPlan, legacyPlan) {
				t.Fatalf("试运行调度开关改变旧规划：旧=%+v 试运行=%+v", legacyPlan, trialPlan)
			}
			for _, desired := range legacyPlan {
				if desired.UpstreamID != upstreamID {
					continue
				}
				var actual scheduling.Desired
				mustScan(t, e.app.DB.QueryRow(ctx, `SELECT buyer_facing,group_ids,priority,concurrency,rate_multiplier FROM upstream_runtime_params WHERE upstream_id=$1`, upstreamID), &actual.BuyerFacing, &actual.GroupIDs, &actual.Priority, &actual.Concurrency, &actual.RateMultiplier)
				if actual.BuyerFacing != desired.BuyerFacing || !reflect.DeepEqual(actual.GroupIDs, desired.GroupIDs) || actual.Priority != desired.Priority || actual.Concurrency != desired.Concurrency || !approx(actual.RateMultiplier, desired.RateMultiplier) {
					t.Fatalf("实际下发不符合旧调度：实际=%+v 旧期望=%+v", actual, desired)
				}
			}
			var upstream listing.Upstream
			seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", upstreamID), nil, &upstream)
			if upstream.Appraisal.Multiplier == nil || !approx(*upstream.Appraisal.Multiplier, .3*tc.q) || !upstream.Appraisal.Provisional || upstream.InspectionMode != "trial" {
				t.Fatalf("卖家缺少试运行暂估价：%+v", upstream.InspectionView)
			}
		})
	}
	var afterSuites []string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT suites FROM detection_policies WHERE platform='anthropic' AND auth_type=''`), &afterSuites)
	if !reflect.DeepEqual(afterSuites, policySuites) {
		t.Fatal("自动试运行改写了检测策略套件")
	}
}

// 从同一批次的旧题记录复算原判定，避免用另一批次的速度与随机样本作比较。
func inspectionPricingLegacyAssessment(t *testing.T, e *env, runID, upstreamID int64) quality.Assessment {
	t.Helper()
	rows, err := e.app.DB.Query(e.ctx, `SELECT tr.item_id,tr.suite,tr.passed,COALESCE(tr.http_status,0),COALESCE(tr.error_class,''),COALESCE(tr.ttft_ms,0),COALESCE(tr.duration_ms,0),COALESCE(tr.output_tokens,0),COALESCE(tr.reasoning_tokens,0),tr.flags,
 COALESCE(rr.http_status,0),COALESCE(rr.error_class,''),COALESCE(rr.passed,false),rr.flags
 FROM probe_results tr JOIN probe_runs target ON target.id=tr.run_id
 JOIN probe_runs reference ON reference.batch_id=target.batch_id AND reference.trigger='reference'
 JOIN probe_results rr ON rr.run_id=reference.id AND rr.item_id=tr.item_id AND rr.instance_idx=tr.instance_idx AND rr.step=tr.step
 WHERE tr.run_id=$1 AND tr.suite NOT LIKE 'inspection_%' ORDER BY tr.item_id,tr.instance_idx,tr.step`, runID)
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []quality.Outcome
	for rows.Next() {
		var o quality.Outcome
		var ref quality.ProbeResult
		var ttft, duration int64
		if err := rows.Scan(&o.ItemID, &o.Suite, &o.TargetPass, &o.Target.HTTPStatus, &o.Target.ErrorClass, &ttft, &duration, &o.Target.OutputTokens, &o.Target.ReasoningTokens, &o.Target.Flags, &ref.HTTPStatus, &ref.ErrorClass, &o.RefPass, &ref.Flags); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		// 正常请求的正 TTFT 入库按毫秒截断；保留正值即可复算整数毫秒分位数。
		o.Target.TTFT = time.Duration(ttft)*time.Millisecond + time.Nanosecond
		o.Target.Duration = time.Duration(duration) * time.Millisecond
		o.Ref = &ref
		outcomes = append(outcomes, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(outcomes) != 12 {
		t.Fatalf("旧题配对记录不完整：%d %v", len(outcomes), err)
	}
	policy, err := quality.EffectivePolicy(e.ctx, e.app.DB, upstreamID, "anthropic", "oauth")
	if err != nil {
		t.Fatal(err)
	}
	return quality.Assess(outcomes, policy.Thresholds, true)
}

func inspectionPricingLegacySuites(a quality.Assessment) map[string]any {
	suites := map[string]any{"availability": map[string]int{"success": a.AvailableOK, "total": a.AvailableTotal}}
	latency := map[string]any{}
	if a.TTFTp50Ms != nil {
		latency["ttft_p50_ms"] = *a.TTFTp50Ms
	}
	if a.TTFTp95Ms != nil {
		latency["ttft_p95_ms"] = *a.TTFTp95Ms
	}
	if a.OutputTPS != nil {
		latency["output_tps"] = *a.OutputTPS
	}
	suites["latency"] = latency
	if a.RelativeScore != nil {
		iq := map[string]any{"relative_score": *a.RelativeScore, "samples": a.Samples}
		if a.Confidence != nil {
			iq["confidence"] = *a.Confidence
		}
		suites["iq"] = iq
	}
	if a.ReasoningRatio != nil {
		suites["reasoning"] = map[string]any{"ratio_to_baseline": *a.ReasoningRatio}
	}
	return suites
}

func TestInspectionPricingProductionAvailabilityGateAndRecovery(t *testing.T) {
	f := newInspectionPricingFixture(t)
	calibrateInspectionPricingCheck(t, f.admin, "D13")
	st, err := inspection.Load(f.e.ctx, f.e.app.DB)
	if err != nil {
		t.Fatal(err)
	}
	rule, _ := inspection.Effective("D13", st)
	if rule.Thresholds["min_samples"] != 100 || rule.Thresholds["gate_min_samples"] != 100 {
		t.Fatalf("本测试须使用默认 100 样本门槛：%+v", rule.Thresholds)
	}
	if _, err := f.e.app.DB.Exec(f.e.ctx, `INSERT INTO edge_attempts(edge_request_id,buyer_key_id,hop,listing_id,http_status,committed,billable,end_reason,error_class,started_at,ended_at)
 SELECT '离线生产可用率故障-'||n,1,0,$1,503,true,true,'upstream_error','http_5xx',now(),now() FROM generate_series(1,100) n`, f.listingID); err != nil {
		t.Fatal(err)
	}
	_, failed := f.run(t, "admin")
	confirmed := false
	for _, gate := range failed.Gates {
		confirmed = confirmed || gate.CheckKey == "D13" && gate.Code == "production_availability_low" && gate.Confirmed
	}
	check := failed.Checks["D13"]
	if failed.Verdict != "unavailable" || failed.QualityFactor != nil || !confirmed || check.Evidence.Source != "production" || check.Samples.Paired != 100 || check.Metrics["success_rate"].Value == nil || *check.Metrics["success_rate"].Value != 0 {
		t.Fatalf("默认 100 个生产失败应触发已校准门槛：%+v", failed)
	}
	f.assertHealth(t, "unavailable")
	f.e.schedule()
	if f.marketItem(t) != nil {
		t.Fatal("生产可用率门槛后挂单仍在市场")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		id, recovered := f.run(t, "recovery")
		var requests, shortRequests, shortFailures int
		var mode string
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT r.mode,count(pr.*),count(*) FILTER(WHERE pr.check_key='D13' AND pr.instance_idx>=2000),count(*) FILTER(WHERE pr.check_key='D13' AND pr.instance_idx>=2000 AND (pr.http_status<>200 OR pr.passed IS NOT TRUE)) FROM probe_runs r JOIN probe_results pr ON pr.run_id=r.id WHERE r.id=$1 GROUP BY r.mode`, id), &mode, &requests, &shortRequests, &shortFailures)
		fresh := recovered.Checks["D13"]
		if mode != "full" || requests != 190 || shortRequests != 100 || shortFailures != 0 || recovered.Verdict != "normal" || fresh.Status != "pass" || fresh.Evidence.Source != "paired_probe" || fresh.Samples.Paired < 100 {
			t.Fatalf("恢复须补足 100 个本次短样本，旧生产窗口不得覆盖：模式=%s 请求=%d 短题=%d 失败=%d 摘要=%+v", mode, requests, shortRequests, shortFailures, recovered)
		}
		for _, gate := range recovered.Gates {
			if gate.Confirmed {
				t.Fatalf("已通过的新鲜探针被旧生产失败再次阻塞：%+v", gate)
			}
		}
		var failures []string
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failures)
		var upstream listing.Upstream
		f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
		if attempt == 1 {
			f.assertHealth(t, "unavailable")
			if !strings.Contains(strings.Join(failures, ","), "D13") || upstream.Appraisal.Multiplier != nil || upstream.QualityFactor != nil {
				t.Fatalf("第一次恢复需保留暂停及暂不估价：失败项=%v 摘要=%+v", failures, upstream.InspectionView)
			}
		} else {
			f.assertHealth(t, "healthy")
			if len(failures) != 0 || upstream.Appraisal.Multiplier == nil || !approx(*upstream.Appraisal.Multiplier, .3) {
				t.Fatalf("第二次有效恢复后应恢复独立估价：失败项=%v 摘要=%+v", failures, upstream.InspectionView)
			}
		}
	}
	var retained int
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT count(*) FROM edge_attempts WHERE listing_id=$1 AND error_class='http_5xx'`, f.listingID), &retained)
	if retained != 100 {
		t.Fatal("恢复流程不应删除生产故障历史")
	}
	f.e.schedule()
	if f.marketItem(t) == nil {
		t.Fatal("两次可用率恢复后挂单未重返市场")
	}
}
