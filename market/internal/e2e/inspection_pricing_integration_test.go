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

	"github.com/yx-token/token-market/market/internal/buyer"
	"github.com/yx-token/token-market/market/internal/inspection"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/market"
	"github.com/yx-token/token-market/market/internal/quality"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/testutil/fakesub2api"
)

type inspectionPricingFixture struct {
	e          *env
	admin      *client
	seller     *client
	upstreamID int64
	listingID  int64
	onboarding int64
	scopedItem int64
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

// 不指定题目时保留默认题库；其余生命周期只抽测其断言所需的项，
// 数学题维持有效智力配对和等级。默认题库的全部模板由正式上架场景覆盖。
func newInspectionPricingFixture(t *testing.T, builtinKeys ...string) *inspectionPricingFixture {
	t.Helper()
	return newInspectionPricingModelFixture(t, "claude-sonnet-4-5", builtinKeys...)
}

func newInspectionPricingModelFixture(t *testing.T, model string, builtinKeys ...string) *inspectionPricingFixture {
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
	var originalSuites []string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT suites FROM detection_policies WHERE platform='anthropic' AND auth_type=''`), &originalSuites)
	if !reflect.DeepEqual(originalSuites, []string{"availability", "latency", "iq"}) {
		t.Fatalf("基础策略默认 suites 改变：%v", originalSuites)
	}
	policy := map[string]any{"enabled": true, "scheduled_enabled": false, "item_parallelism": 8, "flows": testDetectionFlows([]string{model}, "self", false)}
	// 测智按流程选题，旧 suites 仅保留历史兼容。
	admin.must("PUT", "/api/admin/detection-policies/anthropic", policy, nil)
	admin.must("POST", "/api/admin/pools", map[string]any{"name": "tm-pool-正式质检保障池", "platform": "anthropic", "sell_multiplier": .8}, nil)

	// 基线先通过测智，再载入身份题库；健康状态只写独立测试数据库。
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
	if legacy != 0 {
		t.Fatalf("默认题库不应再写入旧题，得到 %d", legacy)
	}
	admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"flows": testDetectionFlows([]string{model}, "self", true)}, nil)
	var scopedItem int64
	var scopedPrompt string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT id,prompt FROM probe_items WHERE builtin_key='inspection/pigeonhole/astra/1'`), &scopedItem, &scopedPrompt)
	if len(builtinKeys) > 0 {
		selectInspectionItems(t, e, append([]string{"inspection/aime_math/1"}, builtinKeys...)...)
		// 定级仍满足 D07 的默认 20 个有效配对，改由廉价数学模板提供，
		// 避免为了保留等级断言重复生成其余能力题。
		if _, err := e.app.DB.Exec(e.ctx, `UPDATE probe_items SET generator=jsonb_set(jsonb_set(generator,'{instances,full}','20'::jsonb),'{instances,quick}','20'::jsonb) WHERE builtin_key='inspection/aime_math/1'`); err != nil {
			t.Fatal(err)
		}
		compactInspectionItems(t, e, admin)
	}
	e.app.Quality.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
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
		// 糖果题沿用线上 regex 判分，没有可由 grader.Answer 读取的答案。
		// 仅为这个已识别题面的离线冻结请求登记标准答案。
		if instance.Request.Prompt == scopedPrompt {
			response.Text = "21"
		}
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
		t.Fatalf("本场景主动发起的正式检测应等待完成，得到 %s", upstream.Status)
	}
	var groupID, onboarding int64
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT probe_group_id FROM upstreams WHERE id=$1`, upstream.ID), &groupID)
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT id FROM probe_runs WHERE upstream_id=$1 AND trigger='onboarding' ORDER BY id DESC LIMIT 1`, upstream.ID), &onboarding)
	f := &inspectionPricingFixture{e: e, admin: admin, seller: seller, upstreamID: upstream.ID, onboarding: onboarding, scopedItem: scopedItem, transport: tr}
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

// 仅调整独立测试库中的题面规模及 A05 边界探针输入长度，保留采样数和判定门槛。
// 128K 及上下文边界的默认大载荷由正式上架完整轮统一覆盖，生产默认设置不变。
func compactInspectionItems(t *testing.T, e *env, admin *client) {
	t.Helper()
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE probe_items SET generator=jsonb_set(generator,'{parameters}','{"houses":4,"attributes":3}'::jsonb) WHERE generator->>'template_id'='zebra'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE probe_items SET generator=jsonb_set(generator,'{parameters,target_tokens}','8192'::jsonb) WHERE generator->>'template_id'='long_context'`); err != nil {
		t.Fatal(err)
	}
	admin.must("PUT", "/api/admin/inspection/checks/A05", map[string]any{"thresholds": map[string]float64{"probe_tokens": 8192}}, nil)
}

// 下线无关题而保留题库记录；旧题、检测规则与恢复采样设置均继续走真实实现。
func selectInspectionItems(t *testing.T, e *env, builtinKeys ...string) {
	t.Helper()
	if _, err := e.app.DB.Exec(e.ctx, `UPDATE probe_items SET status='retired' WHERE suite LIKE 'inspection_%' AND NOT (builtin_key=ANY($1))`, builtinKeys); err != nil {
		t.Fatal(err)
	}
	var active int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*) FROM probe_items WHERE suite LIKE 'inspection_%' AND status='active' AND builtin_key=ANY($1)`, builtinKeys), &active)
	if active != len(builtinKeys) {
		t.Fatalf("定向质检题缺失：期望 %v，实际 %d 道", builtinKeys, active)
	}
}

func calibrateInspectionPricingCheck(t *testing.T, admin *client, key string) {
	t.Helper()
	// 只为离线夹具激活计算路径；这不是上线所需的真实 A/A、阳性对照校准。
	admin.must("PUT", "/api/admin/inspection/checks/"+key, map[string]any{"enabled": true, "pricing": true, "calibrated": true, "calibration_note": "离线测试夹具，非真实校准；Anthropic 同型号替身、v0.2.14 契约、固定标准答案与单项故障对照，记录 inspection-pricing-fixture。"}, nil)
}

func (f *inspectionPricingFixture) execute(t *testing.T, id int64) inspectionPricingSummary {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.e.ctx, 3*time.Minute)
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
	if (summary.Mode != "formal" && summary.Mode != "trial") || len(summary.Checks) == 0 {
		t.Fatalf("缺少流程质检目录：%s", raw)
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
	// 复用默认完整轮验证 gpt-6-astra 专用糖果题；后续既有快检切回
	// 其他型号验证选择隔离，不增加一整轮质检或独立测试环境。
	f := newInspectionPricingModelFixture(t, "gpt-6-astra")
	e := f.e
	var full, generated, bad, legacy, permission, whitelisted int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*),count(*) FILTER (WHERE generator_version<>''),count(*) FILTER (WHERE passed IS NOT TRUE OR input_tokens IS NULL OR input_tokens<=0),count(*) FILTER (WHERE suite NOT LIKE 'inspection_%'),count(*) FILTER (WHERE check_key='A04') FROM probe_results WHERE run_id=$1`, f.onboarding), &full, &generated, &bad, &legacy, &permission)
	// 型号权限按挂售白名单逐个确认，其余题量固定为 90 次（含能力质检糖果题）。
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT COALESCE(cardinality(model_whitelist),0) FROM upstreams WHERE id=$1`, f.upstreamID), &whitelisted)
	if whitelisted == 0 || permission != whitelisted || full-permission != 90 || generated != full || bad != 0 || legacy != 0 {
		t.Fatalf("完整质检应为 90 次质检请求加每个挂售模型一次型号权限，无旧题：总数=%d 型号权限=%d 白名单=%d 版本=%d 错误=%d 旧题=%d", full, permission, whitelisted, generated, bad, legacy)
	}
	var savedSuites []string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT suites FROM detection_policies WHERE platform='anthropic' AND auth_type=''`), &savedSuites)
	if !reflect.DeepEqual(savedSuites, []string{"availability", "latency", "iq"}) {
		t.Fatalf("执行时添加新套件不应改已保存默认策略：%v", savedSuites)
	}
	assertInspectionModelScopedStaticEvidence(t, f)
	// 同时承接影子生命周期原来的完整模板判分覆盖：逐个稳定内置键均须有
	// 成功证据，并精确验证全部生成器版本（含静态多步与上下文边界探针）。
	var builtinItems, missingItems int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT count(*),count(*) FILTER (WHERE NOT EXISTS(SELECT 1 FROM probe_results pr WHERE pr.run_id=$1 AND pr.item_id=i.id AND pr.passed IS TRUE)) FROM probe_items i WHERE i.suite LIKE 'inspection_%' AND i.status='active' AND i.builtin_key IS NOT NULL`, f.onboarding), &builtinItems, &missingItems)
	var versions []string
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT array_agg(DISTINCT generator_version ORDER BY generator_version) FROM probe_results WHERE run_id=$1`, f.onboarding), &versions)
	wantVersions := []string{"aime_math/1", "code_output/1", "constraints/2", "context_boundary/1", "knights_knaves/1", "long_context/1", "register_machine/1", "static-1", "string_date/1", "zebra/2"}
	if builtinItems != 32 || missingItems != 0 || !reflect.DeepEqual(versions, wantVersions) {
		t.Fatalf("默认内置题覆盖不完整：内置键=%d 缺失=%d 版本=%v", builtinItems, missingItems, versions)
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
	// 全题完整轮已经验证默认难度；快检保留默认 25 请求与所有判定断言，
	// 只把重复斑马题面的维度缩小。
	compactInspectionItems(t, e, f.admin)
	// 继续使用只含旧套件的策略，快检自动加入新题；糖果题仅因型号不匹配而缺席。
	// 原快检的 25 请求、判定和 Q 回归断言保持不变。
	f.admin.must("PUT", "/api/admin/detection-policies/anthropic", map[string]any{"flows": testDetectionFlows([]string{"claude-sonnet-4-5"}, "self", true)}, nil)
	quickID, summary := f.run(t, "scheduled")
	var mode string
	var quick int
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT r.mode,(SELECT count(*) FROM probe_results WHERE run_id=r.id) FROM probe_runs r WHERE id=$1`, quickID), &mode, &quick)
	if mode != "quick" || quick != 25 || summary.Verdict != "normal" || summary.QualityFactor == nil || !approx(*summary.QualityFactor, 1) {
		t.Fatalf("正式快检应为 25 次请求：模式=%s 数量=%d 摘要=%+v", mode, quick, summary)
	}
	var otherModel, unexpected bool
	mustScan(t, e.app.DB.QueryRow(e.ctx, `SELECT model='claude-sonnet-4-5',EXISTS(SELECT 1 FROM probe_results pr JOIN probe_runs r ON r.id=pr.run_id WHERE r.batch_id=target.batch_id AND pr.item_id=$2) FROM probe_runs target WHERE target.id=$1`, quickID, f.scopedItem), &otherModel, &unexpected)
	if !otherModel || unexpected {
		t.Fatal("其他型号的被测及同批基线不应执行 gpt-6-astra 专用糖果题")
	}
}

func assertInspectionModelScopedStaticEvidence(t *testing.T, f *inspectionPricingFixture) {
	t.Helper()
	var detail quality.RunDetail
	f.admin.must("GET", fmt.Sprintf("/api/admin/probe-runs/%d", f.onboarding), nil, &detail)
	find := func(rows []quality.RunResult) *quality.RunResult {
		var result *quality.RunResult
		for i := range rows {
			if rows[i].ItemID != nil && *rows[i].ItemID == f.scopedItem {
				if result != nil {
					t.Fatal("型号专用静态题被重复执行或生成了 D06 锚点")
				}
				result = &rows[i]
			}
		}
		return result
	}
	target, reference := find(detail.Results), find(detail.RefResults)
	for _, row := range []*quality.RunResult{target, reference} {
		if row == nil || row.Suite != "inspection_degradation" || row.CheckKey != "D14" || row.InstanceIdx != 0 || row.Step != 0 || row.GeneratorVersion != "static-1" || len(row.InstanceHash) != 64 || row.Passed == nil || !*row.Passed || row.Excerpt == nil || *row.Excerpt != "21" || !strings.Contains(row.Prompt, "糖果") || !reflect.DeepEqual(row.Models, []string{"gpt-6-astra"}) {
			t.Fatalf("静态糖果题应保留题面、套件与型号，并产生成功 D14 逐题证据：%+v", row)
		}
	}
	if detail.Run.Model != "gpt-6-astra" || detail.Reference == nil || detail.Reference.BatchID != detail.Run.BatchID || detail.Reference.Model != detail.Run.Model || target.InstanceHash != reference.InstanceHash {
		t.Fatal("静态题被测与基线的型号、批次或冻结配对元数据不一致")
	}
	var summary inspectionPricingSummary
	if err := json.Unmarshal(detail.Run.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	// 限定型号的糖果题属降智检测：D14 单独判分，测智的 D07 只统计通用参数题。
	parameterPairs, anchors := 0, 0
	for _, row := range detail.Results {
		if strings.HasPrefix(row.Suite, "inspection_") && row.CheckKey == "D07" {
			parameterPairs++
		}
		if row.CheckKey == "D06" {
			anchors++
		}
	}
	d07, d06, d14 := summary.Checks["D07"], summary.Checks["D06"], summary.Checks["D14"]
	if parameterPairs < 20 || d07.Samples.Paired != parameterPairs || d07.Samples.Observed != parameterPairs || d07.Samples.Excluded != 0 || d07.Status != "pass" || d07.Evidence.Source != "paired_probe" || d07.Factor == nil || !approx(*d07.Factor, 1) || anchors != 10 || d06.Samples.Observed != anchors {
		t.Fatalf("测智只统计通用参数题，且不进入 D06 推理量样本：参数化=%d 锚点=%d D07=%+v D06=%+v", parameterPairs, anchors, d07, d06)
	}
	if d14.Samples.Paired != 1 || d14.Status != "pass" || d14.Factor == nil || !approx(*d14.Factor, 1) || d14.Evidence.Source != "paired_probe" {
		t.Fatalf("型号专用糖果题应单独产生 D14 配对证据：%+v", d14)
	}
	var unchanged bool
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT check_key='D14' AND suite='inspection_degradation' AND builtin_key='inspection/pigeonhole/astra/1' AND modes=ARRAY['full','quick']::text[] AND models=ARRAY['gpt-6-astra']::text[] AND max_tokens=8192 AND reasoning_effort='high' AND generator='{}'::jsonb AND status='active' FROM probe_items WHERE id=$1`, f.scopedItem), &unchanged)
	if !unchanged {
		t.Fatal("降智检测糖果题的落库定义错误")
	}
	var bank []quality.Item
	f.admin.must("GET", "/api/admin/probe-items", nil, &bank)
	found := false
	for _, it := range bank {
		if it.ID == f.scopedItem {
			found = it.Suite == "inspection_degradation" && it.CheckKey == "D14" && it.BuiltinKey != nil && *it.BuiltinKey == "inspection/pigeonhole/astra/1"
		}
	}
	if !found {
		t.Fatal("后台题库未展示降智检测/D14 的稳定内置题")
	}
	var stats []quality.ItemStat
	f.admin.must("GET", "/api/admin/probe-item-stats?platform=anthropic&model=gpt-6-astra", nil, &stats)
	seen := false
	for _, stat := range stats {
		if stat.ItemID == f.scopedItem {
			seen = stat.Suite == "inspection_degradation" && strings.Contains(stat.Prompt, "糖果") && reflect.DeepEqual(stat.Models, []string{"gpt-6-astra"}) && stat.Asked == 1 && stat.Passed == 1 && stat.RefAsked == 1 && stat.RefPassed == 1
		}
	}
	if !seen {
		t.Fatalf("后台按题统计丢失型号专用能力题：%+v", stats)
	}
	var results []quality.ItemResult
	f.admin.must("GET", fmt.Sprintf("/api/admin/probe-items/%d/results", f.scopedItem), nil, &results)
	if len(results) != 2 {
		t.Fatalf("后台逐题结果应包含同批被测和基线：%+v", results)
	}
	for _, result := range results {
		if result.Model != "gpt-6-astra" || result.CheckKey != "D14" || result.InstanceHash != target.InstanceHash || result.Excerpt == nil || *result.Excerpt != "21" {
			t.Fatalf("后台逐题结果的 D14 证据不完整：%+v", result)
		}
	}
	var reportID int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT id FROM test_reports WHERE run_id=$1`, f.onboarding), &reportID)
	var report quality.Report
	status, raw := f.seller.call("GET", fmt.Sprintf("/api/seller/reports/%d", reportID), nil, &report)
	if status != http.StatusOK || strings.Contains(raw, "糖果") || strings.Contains(raw, "五角星") || strings.Contains(raw, "\"prompt\"") || strings.Contains(raw, "\"answer\"") || strings.Contains(raw, "\"excerpt\"") || strings.Contains(raw, "\"instance_hash\"") {
		t.Fatalf("卖家报告泄露题面、答案或内部逐题定位：%s", raw)
	}
	var public struct {
		Checks map[string]inspection.Check `json:"checks"`
	}
	if err := json.Unmarshal(report.Summary, &public); err != nil {
		t.Fatal(err)
	}
	if public.Checks["D07"].Samples.Paired != d07.Samples.Paired || len(public.Checks["D07"].Metrics) != 0 || public.Checks["D07"].Evidence.Code != "" {
		t.Fatalf("卖家应能看到 D07 聚合结果且沿用现有脱敏：%s", report.Summary)
	}
}

func TestInspectionPricingPermissionGateAndEffectiveRecovery(t *testing.T) {
	f := newInspectionPricingFixture(t, "inspection/model-permission/1")
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
	// D09 默认要求三个配对；保留三种 8K 题和完整输出项，不降低门槛。
	f := newInspectionPricingFixture(t, "inspection/output/2", "inspection/context/0/1", "inspection/context/1/1", "inspection/context/2/1")
	f.transport.setBad("D08")
	_, uncalibrated := f.run(t, "admin")
	if uncalibrated.Checks["D08"].Status != "fail" || uncalibrated.Checks["D08"].Applied || uncalibrated.QualityFactor == nil || !approx(*uncalibrated.QualityFactor, 1) {
		t.Fatalf("未开启扣价的失败项只展示：%+v", uncalibrated)
	}
	calibrateInspectionPricingCheck(t, f.admin, "D08")
	f.transport.setBad("D08", "D09")
	fullID, full := f.run(t, "admin")
	if full.Verdict != "suspected" || full.QualityFactor == nil || !approx(*full.QualityFactor, .95*.85) || !full.Checks["D08"].Applied || !full.Checks["D09"].Applied {
		t.Fatalf("测智未通过应疑似，功能与测智分别扣价：%+v", full)
	}
	f.assertHealth(t, "suspected")
	// 本轮快检没有长上下文题；即使当前所有已测题通过，也不能清除旧上下文失败。
	f.transport.setBad()
	quickID, quick := f.run(t, "scheduled")
	var askedContext int
	var failedChecks []string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT count(*) FROM probe_results WHERE run_id=$1 AND check_key='D09'`, quickID), &askedContext)
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failedChecks)
	old, inherited := full.Checks["D09"], quick.Checks["D09"]
	if askedContext != 0 || (inherited.Status != "warning" && inherited.Status != "fail") || inherited.Evidence.Source != "history" || !inherited.CheckedAt.Equal(old.CheckedAt) || !inherited.Applied || !strings.Contains(strings.Join(failedChecks, ","), "D09") || quick.QualityFactor == nil || !approx(*quick.QualityFactor, *full.QualityFactor) {
		t.Fatalf("快检缺测不得推成上下文修复：原项=%+v 继承项=%+v 失败项=%v", old, inherited, failedChecks)
	}
	var reportID int64
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT id FROM test_reports WHERE run_id=$1`, fullID), &reportID)
	var publicReport quality.Report
	f.seller.must("GET", fmt.Sprintf("/api/seller/reports/%d", reportID), nil, &publicReport)
	var publicSummary inspectionPricingSummary
	if err := json.Unmarshal(publicReport.Summary, &publicSummary); err != nil {
		t.Fatal(err)
	}
	if publicSummary.Checks["D08"].Factor == nil || !publicSummary.Checks["D08"].Applied {
		t.Fatalf("卖家摘要须保留实际扣价状态与生效系数，申诉入口依赖它们：%s", publicReport.Summary)
	}
	otherSeller := f.e.newUser("other-appeal-seller@market.test")
	otherSeller.expect(http.StatusNotFound, "POST", fmt.Sprintf("/api/seller/reports/%d/appeal", reportID), map[string]string{"reason": "不能申诉其他卖家的报告"})
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

func TestInspectionPricingTrialAppliesExplicitDiscountWithoutCalibration(t *testing.T) {
	f := newInspectionPricingFixture(t, "inspection/output/2")
	f.admin.must("PUT", "/api/admin/inspection/settings", map[string]any{"mode": "trial"}, nil)
	for _, key := range []string{"D07", "D09", "A04"} {
		f.admin.must("PUT", "/api/admin/inspection/checks/"+key, map[string]any{"pricing": false}, nil)
	}
	f.transport.setBad("D08")
	var initialGrade string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT grade FROM upstreams WHERE id=$1`, f.upstreamID), &initialGrade)
	_, display := f.run(t, "admin")
	if display.Mode != "trial" || display.Checks["D08"].Status != "fail" || display.Checks["D08"].Applied || (display.QualityFactor != nil && !approx(*display.QualityFactor, 1)) {
		t.Fatalf("未开启扣价应仅展示故障：%+v", display)
	}
	var before listing.Upstream
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &before)
	if !before.Appraisal.Provisional {
		t.Fatal("没有任何项开启扣价时应标试算")
	}
	f.admin.must("PUT", "/api/admin/inspection/checks/D08", map[string]any{"pricing": true, "calibrated": false}, nil)
	_, discounted := f.run(t, "admin")
	if discounted.Verdict != "normal" || !discounted.Checks["D08"].Applied || discounted.QualityFactor == nil || !approx(*discounted.QualityFactor, .95) {
		t.Fatalf("试运行开启扣价后无需校准即生效：%+v", discounted)
	}
	var health, grade string
	var q float64
	var failed []string
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT health,grade,quality_factor FROM upstreams WHERE id=$1`, f.upstreamID), &health, &grade, &q)
	mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, f.upstreamID), &failed)
	if health != "healthy" || grade != initialGrade || !approx(q, .95) || !strings.Contains(strings.Join(failed, ","), "D08") {
		t.Fatalf("试运行须写Q和失败项并保留等级：%s %s %g %v", health, grade, q, failed)
	}
	var upstream listing.Upstream
	f.seller.must("GET", fmt.Sprintf("/api/seller/upstreams/%d", f.upstreamID), nil, &upstream)
	if upstream.Appraisal.Multiplier == nil || !approx(*upstream.Appraisal.Multiplier, .3*.95) || upstream.Appraisal.Provisional {
		t.Fatalf("卖家估价应展示已生效扣价：%+v", upstream.InspectionView)
	}
	var retest struct {
		RunID int64 `json:"run_id"`
	}
	f.transport.setBad()
	f.seller.must("POST", fmt.Sprintf("/api/seller/upstreams/%d/retest", f.upstreamID), nil, &retest)
	restored := f.execute(t, retest.RunID)
	if restored.QualityFactor == nil || !approx(*restored.QualityFactor, 1) || restored.Checks["D08"].Status != "pass" || restored.Checks["D08"].Evidence.Source == "history" {
		var flows, models, deg []string
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT flows,models,degradation_models FROM probe_runs WHERE id=$1`, retest.RunID), &flows, &models, &deg)
		t.Fatalf("健康但存在已生效扣价时须允许卖家复测并解除：flows=%v models=%v deg=%v %+v", flows, models, deg, restored.Checks["D08"])
	}
}

func TestInspectionPricingProductionAvailabilityGateAndRecovery(t *testing.T) {
	f := newInspectionPricingFixture(t, "inspection/model-permission/1")
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
	var waits []time.Duration
	f.e.app.Quality.Wait = func(ctx context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return ctx.Err()
	}
	for attempt := 1; attempt <= 2; attempt++ {
		id, recovered := f.run(t, "recovery")
		var requests, shortRequests, shortFailures int
		var mode string
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT r.mode,count(pr.*),count(*) FILTER(WHERE pr.check_key='D13' AND pr.instance_idx>=2000),count(*) FILTER(WHERE pr.check_key='D13' AND pr.instance_idx>=2000 AND (pr.http_status<>200 OR pr.passed IS NOT TRUE)) FROM probe_runs r JOIN probe_results pr ON pr.run_id=r.id WHERE r.id=$1 GROUP BY r.mode`, id), &mode, &requests, &shortRequests, &shortFailures)
		fresh := recovered.Checks["D13"]
		// 测智流程的 30 个常规请求与 20 个独立恢复样本，不执行身份 A04 本体。
		if mode != "full" || requests != 50 || shortRequests != 20 || shortFailures != 0 || recovered.Verdict != "normal" || fresh.Status != "pass" || fresh.Evidence.Source != "recovery_probe" || fresh.Samples.Paired != 20*attempt {
			t.Fatalf("恢复须累计最近两轮每次 20 个被测新样本，旧生产窗口不得覆盖：模式=%s 请求=%d 短题=%d 失败=%d 摘要=%+v", mode, requests, shortRequests, shortFailures, recovered)
		}
		var referenceExtras int
		mustScan(t, f.e.app.DB.QueryRow(f.e.ctx, `SELECT count(*) FROM probe_results pr JOIN probe_runs r ON r.id=pr.run_id WHERE r.batch_id=(SELECT batch_id FROM probe_runs WHERE id=$1) AND r.trigger='reference' AND pr.generator_version='availability_recovery/2'`, id), &referenceExtras)
		if referenceExtras != 0 || len(waits) != 20*attempt {
			t.Fatalf("额外恢复样本不应发基线且每次须等待：基线=%d 等待=%v", referenceExtras, waits)
		}
		for _, delay := range waits {
			if delay != 3*time.Second {
				t.Fatalf("恢复采样间隔未按设置执行：%v", delay)
			}
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
