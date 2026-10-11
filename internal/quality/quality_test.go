package quality

import (
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"context"
	"io"
)

func f(v float64) *float64 { return &v }

func TestGraders(t *testing.T) {
	cases := []struct {
		g    Grader
		text string
		want bool
	}{
		{Grader{Type: "numeric", Number: f(1591)}, "The answer is 1,591.", true},
		{Grader{Type: "numeric", Number: f(1591)}, "1590", false},
		{Grader{Type: "exact", Answer: "3,7,19"}, "Sure!\n3,7,19", true},
		{Grader{Type: "exact", Answer: "abc", CaseInsensitive: true}, "```\nABC\n```", true},
		{Grader{Type: "contains", Answer: "carol", CaseInsensitive: true}, "Carol is youngest", true},
		{Grader{Type: "regex", Pattern: `(?i)^\W*yes\b`}, "Yes.", true},
		{Grader{Type: "regex", Pattern: `(?i)^\W*yes\b`}, "No, yes is wrong", false},
		{Grader{Type: "choice", Answer: "B"}, "The answer is B", true},
		{Grader{Type: "choice", Answer: "B"}, "A", false},
		{Grader{Type: "json_keys", Keys: []string{"city", "country"}}, "```json\n{\"city\":\"Paris\",\"country\":\"France\"}\n```", true},
		{Grader{Type: "json_keys", Keys: []string{"city", "country"}}, "{\"city\":\"Paris\"}", false},
	}
	for _, c := range cases {
		got, err := grade(c.g, c.text, "")
		if err != nil || got != c.want {
			t.Errorf("grade(%+v, %q) = %v, %v; want %v", c.g, c.text, got, err, c.want)
		}
	}
	if _, err := grade(Grader{Type: "nope"}, "", ""); err == nil {
		t.Error("unknown grader accepted")
	}
}

func TestDefaultItems(t *testing.T) {
	var pigeonhole *Item
	for _, it := range defaultItems() {
		if err := validateItem(it); err != nil {
			t.Errorf("default item %q invalid: %v", it.Category, err)
		}
		if it.Category == "pigeonhole" {
			pigeonhole = &it
		}
	}
	if pigeonhole == nil || len(pigeonhole.Models) != 1 || pigeonhole.Models[0] != "gpt-6-astra" {
		t.Fatalf("pigeonhole item should target gpt-6-astra: %+v", pigeonhole)
	}
	// 回答中出现独立的 21 即通过：前后不能紧挨数字或小数点（句末的句点除外）
	cases := []struct {
		text string
		want bool
	}{
		{"最少需要取出 21 个糖果。", true},
		{"答案：**21**", true},
		{"21", true},
		{"so the answer is 21.", true},
		{"至少要取 29 个", false},
		{"共 121 颗", false},
		{"210", false},
		{"21.5", false},
		{"0.21", false},
	}
	for _, c := range cases {
		if got, err := grade(pigeonhole.Grader, c.text, ""); err != nil || got != c.want {
			t.Errorf("pigeonhole grade(%q) = %v, %v; want %v", c.text, got, err, c.want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	long := "The code is 21。" + strings.Repeat("这是一段很长的中文解释。", 80)
	got := truncateStr(long, 500)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 500 || !strings.HasPrefix(got, "The code is 21。这是") {
		t.Fatalf("truncate long chinese: valid=%v runes=%d", utf8.ValidString(got), utf8.RuneCountInString(got))
	}
	if got := truncateStr("短回答", 500); got != "短回答" {
		t.Fatalf("short text changed: %q", got)
	}
	if got := truncateStr("a\x00b\xffc", 500); got != "abc" {
		t.Fatalf("NUL and invalid bytes should be removed: %q", got)
	}
}

func TestExpandHaystack(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	prompt, needle := expandPrompt("{{HAYSTACK:5000}}\nWhat is the code?", rng)
	if needle == "" || !strings.Contains(prompt, needle) || len(prompt) < 5000 {
		t.Fatalf("haystack not expanded: needle=%q len=%d", needle, len(prompt))
	}
	ok, _ := grade(Grader{Type: "contains", Answer: "{{NEEDLE}}"}, "The code is "+needle, needle)
	if !ok {
		t.Fatal("needle answer not graded")
	}
}

func outcomes(n, targetPass int, refPass bool) []Outcome {
	var out []Outcome
	for i := 0; i < n; i++ {
		ok := ProbeResult{HTTPStatus: 200, Text: "x", TTFT: 300 * time.Millisecond, Duration: time.Second, OutputTokens: 50}
		ref := ok
		out = append(out, Outcome{ItemID: int64(i), Suite: "iq", Target: ok, TargetPass: i < targetPass, Ref: &ref, RefPass: refPass})
	}
	return out
}

var th = Thresholds{H0: 0.9, H1: 0.7, Alpha: 0.05, Beta: 0.1}

func TestAssessVerdicts(t *testing.T) {
	if a := Assess(outcomes(12, 12, true), th, true); a.Verdict != "normal" || a.RelativeScore == nil || *a.RelativeScore != 1 || a.Grade != "S" {
		t.Fatalf("all pass should be normal/S: %+v", a)
	}
	if a := Assess(outcomes(12, 4, true), th, true); a.Verdict != "degraded" || a.Grade != "D" {
		t.Fatalf("4/12 vs baseline 12/12 should be degraded: %+v", a)
	}
	if a := Assess(outcomes(12, 9, true), th, true); a.Verdict == "normal" || a.Verdict == "degraded" {
		t.Fatalf("9/12 should be undecided (needs more samples): %+v", a)
	}
	failing := outcomes(4, 0, true)
	for i := range failing {
		failing[i].Target = ProbeResult{HTTPStatus: 503, ErrorClass: "http_5xx"}
	}
	if a := Assess(failing, th, true); a.Verdict != "unavailable" {
		t.Fatalf("all errors should be unavailable: %+v", a)
	}
	// 基线本身失败的题不计入
	refFail := outcomes(6, 0, false)
	for i := range refFail {
		refFail[i].Ref.ErrorClass = "http_5xx"
	}
	if a := Assess(refFail, th, true); a.Samples != 0 {
		t.Fatalf("items with failed baseline must be excluded: %+v", a)
	}
}

func TestAssessReasoningRatio(t *testing.T) {
	var out []Outcome
	for i := 0; i < 4; i++ {
		tgt := ProbeResult{HTTPStatus: 200, Text: "1", ReasoningTokens: 300}
		ref := ProbeResult{HTTPStatus: 200, Text: "1", ReasoningTokens: 1000}
		out = append(out, Outcome{Suite: "reasoning", Target: tgt, TargetPass: true, Ref: &ref, RefPass: true})
	}
	a := Assess(out, th, true)
	if a.ReasoningRatio == nil || *a.ReasoningRatio < 0.29 || *a.ReasoningRatio > 0.31 {
		t.Fatalf("reasoning ratio: %+v", a.ReasoningRatio)
	}
}

func TestProtocolsParseStreams(t *testing.T) {
	cases := []struct {
		platform, ctype, body string
		wantText              string
		wantOut, wantReason   int
		wantModel             string
	}{
		{"anthropic", "text/event-stream",
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-x\"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"15\"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"91\"}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n",
			"1591", 7, 0, "claude-x"},
		{"openai", "text/event-stream",
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"1591\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-x\",\"usage\":{\"output_tokens\":40,\"output_tokens_details\":{\"reasoning_tokens\":30}}}}\n\n",
			"1591", 40, 30, "gpt-x"},
		{"kimi", "text/event-stream",
			"data: {\"model\":\"kimi-x\",\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
				"data: {\"model\":\"kimi-x\",\"choices\":[{\"delta\":{\"content\":\"1591\"}}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"completion_tokens\":12,\"completion_tokens_details\":{\"reasoning_tokens\":5}}}\n\n" +
				"data: [DONE]\n\n",
			"1591", 12, 5, "kimi-x"},
		{"gemini", "application/json",
			`{"modelVersion":"gemini-x","candidates":[{"content":{"parts":[{"text":"hidden","thought":true},{"text":"1591"}]}}],"usageMetadata":{"candidatesTokenCount":9,"thoughtsTokenCount":20}}`,
			"1591", 9, 20, "gemini-x"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", c.ctype)
			w.Header().Set("X-Client-Request-ID", "rid-1")
			_, _ = io.WriteString(w, c.body)
		}))
		p := &Prober{GatewayURL: srv.URL, Client: srv.Client()}
		res := p.Probe(context.Background(), c.platform, "k", ProbeRequest{Model: "m", Prompt: "q", MaxTokens: 100})
		srv.Close()
		if !res.OK() || res.Text != c.wantText || res.OutputTokens != c.wantOut || res.ReasoningTokens != c.wantReason ||
			res.ResponseModel != c.wantModel || res.Sub2APIRequestID != "rid-1" {
			t.Errorf("%s: %+v", c.platform, res)
		}
	}
}

func TestProbeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	res := (&Prober{GatewayURL: srv.URL, Client: srv.Client()}).Probe(context.Background(), "anthropic", "k", ProbeRequest{Model: "m", Prompt: "q", MaxTokens: 10})
	if res.OK() || res.ErrorClass != "http_5xx" {
		t.Fatalf("503 should be an error: %+v", res)
	}
}

func TestThirdPartyCheckVerdicts(t *testing.T) {
	c := ThirdPartyCheck{Provider: "bazaarlink", Triggers: []string{"onboarding"}}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"pass": "normal", "mismatch": "degraded", "spoof": "degraded", "submodel_mismatch": "suspected",
		"inconclusive": "inconclusive"}
	for outcome, want := range cases {
		if got := c.VerdictFor(outcome); got != want {
			t.Errorf("%s: got %s want %s", outcome, got, want)
		}
	}
	c.OnMismatch = "ignore"
	if c.VerdictFor("mismatch") != "inconclusive" {
		t.Error("ignore should not affect the verdict")
	}
	if !c.Applies("onboarding") || c.Applies("scheduled") {
		t.Error("Applies")
	}
	for _, bad := range []ThirdPartyCheck{
		{Provider: "unknown", Triggers: []string{"onboarding"}},
		{Provider: "bazaarlink"},
		{Provider: "bazaarlink", Triggers: []string{"nope"}},
		{Provider: "bazaarlink", Triggers: []string{"admin"}, Format: "gemini"},
		{Provider: "bazaarlink", Triggers: []string{"admin"}, OnMismatch: "ban"},
	} {
		if err := bad.normalize(); err == nil {
			t.Errorf("%+v should be rejected", bad)
		}
	}
	if DefaultFormat("anthropic") != "anthropic" || DefaultFormat("kimi") != "openai" {
		t.Error("DefaultFormat")
	}
}
