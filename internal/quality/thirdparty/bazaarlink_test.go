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
