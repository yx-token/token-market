package thirdparty_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DueGin/token-market/internal/quality/thirdparty"
	"github.com/DueGin/token-market/internal/testutil/fakebazaar"
)

// target 是一个按 Anthropic Messages 协议作答的被测端点。
func target(t *testing.T, answer string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "tm-tp-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "content": []map[string]string{{"type": "text", "text": answer}},
			"usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func provider(t *testing.T, fake *fakebazaar.Server) thirdparty.Provider {
	p, err := thirdparty.New("bazaarlink", thirdparty.Config{BaseURL: fake.URL()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBazaarLinkAgainstFake(t *testing.T) {
	fake := fakebazaar.New()
	t.Cleanup(fake.Close)
	p := provider(t, fake)
	ctx := context.Background()
	cases := []struct {
		answer, outcome, verdict string
	}{
		{"I am claude-sonnet-4-5", thirdparty.OutcomePass, "clean_match"},
		{"I am claude-haiku-4-5", thirdparty.OutcomeSubmodelMismatch, "clean_match_submodel_mismatch"},
		{"I am gpt-4o by OpenAI", thirdparty.OutcomeMismatch, "plain_mismatch"},
	}
	for _, c := range cases {
		srv := target(t, c.answer)
		id, err := p.Submit(ctx, thirdparty.Target{BaseURL: srv.URL + "/v1", APIKey: "tm-tp-secret", Model: "claude-sonnet-4-5",
			ClaimedModel: "claude-sonnet-4-5", Format: "anthropic"})
		if err != nil {
			t.Fatal(err)
		}
		fake.Wait(id)
		res, err := p.Poll(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != "completed" || res.Outcome != c.outcome || res.Summary["verdict_status"] != c.verdict ||
			!strings.HasPrefix(res.ReportURL, fake.URL()+"/api/probe/report/") {
			t.Fatalf("%q: %+v", c.answer, res)
		}
		raw, _ := json.Marshal(res.Summary)
		if strings.Contains(string(raw), "203.0.113.1") || strings.Contains(string(raw), c.answer) {
			t.Fatalf("summary must not keep submitter ip or raw responses: %s", raw)
		}
	}
	if _, err := p.Poll(ctx, "missing"); !errors.Is(err, thirdparty.ErrRunNotFound) {
		t.Fatalf("missing run: %v", err)
	}
}

func TestBazaarLinkRateLimitAndFailures(t *testing.T) {
	fake := fakebazaar.New()
	fake.LimitPerHour = 1
	t.Cleanup(fake.Close)
	p := provider(t, fake)
	ctx := context.Background()
	// 被测端点不可达：检测完成但没有身份评估
	id, err := p.Submit(ctx, thirdparty.Target{BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Model: "m", Format: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	fake.Wait(id)
	res, err := p.Poll(ctx, id)
	if err != nil || res.Status != "completed" || res.Outcome != thirdparty.OutcomeInconclusive {
		t.Fatalf("unreachable target: %+v %v", res, err)
	}
	_, err = p.Submit(ctx, thirdparty.Target{BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Model: "m"})
	var rl *thirdparty.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, err := p.Submit(ctx, thirdparty.Target{}); err == nil {
		t.Fatal("missing fields should be rejected")
	}
}

// 真实服务的响应结构（节选并脱敏）：verdict 以详细判定为准，原始回答、响应头、提交者 IP 不进入摘要。
func TestBazaarLinkParsesRealShape(t *testing.T) {
	const body = `{"runId":"r1","status":"completed","modelId":"claude-sonnet-4-5","protocolPath":"/v1/messages",
	"reportUrl":"/api/probe/report/r1","totalInputTokens":1200,"totalOutputTokens":3400,"wallClockMs":98000,
	"startedByIp":"2409:8a55::1","userKey":"2409:8a55::1",
	"warnings":[{"id":"w1","label":"Token 膨胀","severity":"warning","evidence":"x"}],
	"identityAssessment":{"status":"match","claimedModel":"claude-sonnet-4-5","predictedFamily":"anthropic","confidence":0.97,
		"riskFlags":["proxy_headers"],"verdict":{"status":"spoof_selfclaim_forged","trueFamily":"openai","trueModel":null,
		"spoofMethod":"system_prompt","confidence":"high","reasoning":"behavior points elsewhere"}},
	"items":[{"probeId":"a","label":"A","group":"identity","status":"done","passed":true,"response":"secret answer","responseHeaders":{"x":"y"}},
		{"probeId":"b","label":"B","group":"integrity","status":"done","passed":false,"passReason":"wrong"},
		{"probeId":"c","label":"C","group":"integrity","status":"done","passed":"warning"},
		{"probeId":"d","label":"D","group":"security","status":"error","passed":null,"error":"HTTP 500"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	p, _ := thirdparty.New("bazaarlink", thirdparty.Config{BaseURL: srv.URL}, nil)
	res, err := p.Poll(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	items := res.Summary["items"].(map[string]int)
	if res.Outcome != thirdparty.OutcomeSpoof || res.Summary["true_family"] != "openai" || res.Summary["spoof_method"] != "system_prompt" ||
		items["total"] != 4 || items["passed"] != 1 || items["failed"] != 1 || items["warning"] != 1 || items["error"] != 1 ||
		res.ReportURL != srv.URL+"/api/probe/report/r1" {
		t.Fatalf("parsed: %+v", res)
	}
	raw, _ := json.Marshal(res.Summary)
	if strings.Contains(string(raw), "secret answer") || strings.Contains(string(raw), "2409:8a55") {
		t.Fatalf("summary leaks sensitive fields: %s", raw)
	}
}

// 逐项明细折算成卖家可见的大类计数与风险项：error 与 pending 只计入 total，风险项 failed 在前。
func TestBazaarLinkItemCategoriesAndRisks(t *testing.T) {
	longReason := strings.Repeat("截", 200)
	body := mustJSON(map[string]any{
		"runId": "r1", "status": "completed", "modelId": "claude-sonnet-4-5",
		"items": []map[string]any{
			{"probeId": "1", "label": "家族识别", "group": "identity", "status": "done", "passed": true},
			{"probeId": "2", "label": "型号识别", "group": "submodel", "status": "done", "passed": true},
			{"probeId": "3", "label": "安全探针 1", "group": "safety", "status": "done", "passed": "warning", "passReason": "回答包含轻微越界"},
			{"probeId": "4", "label": "安全探针 2", "group": "safety", "status": "done", "passed": false, "passReason": longReason},
			{"probeId": "5", "label": "安全探针 3", "group": "safety", "status": "error", "passed": nil, "error": "HTTP 503"},
			{"probeId": "6", "label": "安全探针 4", "group": "safety", "status": "pending", "passed": nil},
			{"probeId": "7", "label": "自定义分组项", "group": "custom_group", "status": "done", "passed": false, "passReason": "自定义分组失败"},
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	p, err := thirdparty.New("bazaarlink", thirdparty.Config{BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Poll(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	categories, _ := json.Marshal(res.Summary["categories"])
	var got []struct {
		Key, Name                             string
		Total, Passed, Warning, Failed, Error int
	}
	if err := json.Unmarshal(categories, &got); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		Key, Name                             string
		Total, Passed, Warning, Failed, Error int
	}{
		{"identity", "身份识别", 1, 1, 0, 0, 0},
		{"submodel", "子模型识别", 1, 1, 0, 0, 0},
		{"safety", "安全探针", 4, 0, 1, 1, 1},
		{"custom_group", "custom_group", 1, 0, 0, 1, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("大类数量=%d，期望=%d：%s", len(got), len(want), categories)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第%d个大类=%+v，期望=%+v", i, got[i], want[i])
		}
	}
	if strings.Contains(string(categories), "自定义分组") {
		t.Fatalf("未知分组不能凭空命名：%s", categories)
	}
	risks, _ := json.Marshal(res.Summary["risks"])
	var gotRisks []struct {
		Category     string `json:"category"`
		CategoryName string `json:"category_name"`
		Label        string `json:"label"`
		Severity     string `json:"severity"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal(risks, &gotRisks); err != nil {
		t.Fatal(err)
	}
	if len(gotRisks) != 3 {
		t.Fatalf("风险项应只收 failed 与 warning：%s", risks)
	}
	// failed 在前（保持分组出现顺序），warning 在后。
	if gotRisks[0].Severity != "fail" || gotRisks[0].CategoryName != "安全探针" || gotRisks[0].Label != "安全探针 2" ||
		gotRisks[1].Severity != "fail" || gotRisks[1].Category != "custom_group" {
		t.Fatalf("失败项排序或归属错误：%+v", gotRisks)
	}
	if runes := []rune(gotRisks[0].Reason); len(runes) != 160 {
		t.Fatalf("原因应按字符截断到 160：%d", len(runes))
	}
	if gotRisks[2].Severity != "warning" || gotRisks[2].Reason != "回答包含轻微越界" {
		t.Fatalf("警告项缺失或原因错误：%+v", gotRisks[2])
	}
	raw, _ := json.Marshal(res.Summary)
	if strings.Contains(string(raw), "HTTP 503") {
		t.Fatalf("error 明细不应进入风险项：%s", raw)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "responseHeaders") {
		t.Fatalf("摘要泄露敏感字段：%s", raw)
	}
}

// 风险项上限 20 条，失败的项先占位，警告不会把失败挤出列表。
func TestBazaarLinkRisksAreCappedAndPreferFailures(t *testing.T) {
	items := []map[string]any{}
	for i := 0; i < 25; i++ {
		items = append(items, map[string]any{"probeId": "w", "label": "警告项", "group": "safety", "status": "done", "passed": "warning"})
	}
	for i := 0; i < 25; i++ {
		items = append(items, map[string]any{"probeId": "f", "label": "失败项", "group": "integrity", "status": "done", "passed": false, "passReason": "原因"})
	}
	body := mustJSON(map[string]any{"runId": "r1", "status": "completed", "modelId": "m", "items": items})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	p, err := thirdparty.New("bazaarlink", thirdparty.Config{BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Poll(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	risks, _ := json.Marshal(res.Summary["risks"])
	var got []struct {
		Label    string `json:"label"`
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(risks, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("风险项上限应为 20：%d", len(got))
	}
	for i, item := range got {
		if item.Severity != "fail" || item.Label != "失败项" {
			t.Fatalf("第%d条风险项应以失败项优先：%+v", i, item)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
