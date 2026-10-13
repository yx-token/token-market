package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	items := defaultItems()
	if len(items) != 21 {
		t.Fatalf("旧内置题应为 21 道，得到 %d", len(items))
	}
	for _, it := range items {
		if err := validateItem(it); err != nil {
			t.Errorf("default item %q invalid: %v", it.Category, err)
		}
		if it.Category == "pigeonhole" || strings.Contains(it.Prompt, "糖果") {
			t.Fatal("糖果题仍在旧内置定义中")
		}
	}
	// 从 2663df8 的 defaultItems 排除糖果题后冻结，覆盖其余题的字段与顺序。
	if got := fmt.Sprintf("%x", sha256.Sum256(mustJSON(items))); got != "80ad0879904ca22006332176c3a5296f4826ba515bd9add7bab7c7b5823b08ac" {
		t.Fatalf("其余 21 道旧题定义或顺序改变：%s", got)
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
	for _, tc := range []struct {
		name    string
		success int
		failed  int
		want    string
	}{{"没有请求", 0, 0, "inconclusive"}, {"单次失败证据不足", 0, 1, "inconclusive"},
		{"全失败", 0, 4, "unavailable"}, {"低于一半", 1, 3, "unavailable"},
		{"恰好一半", 2, 2, "normal"}, {"全部可用", 12, 0, "normal"}} {
		t.Run(tc.name, func(t *testing.T) {
			rows := outcomes(tc.success+tc.failed, 0, false)
			for i := tc.success; i < len(rows); i++ {
				rows[i].Target = ProbeResult{HTTPStatus: 503, ErrorClass: "http_5xx"}
			}
			for _, scoring := range []bool{false, true} {
				a := Assess(rows, th, scoring)
				if a.Verdict != tc.want || a.AvailableOK != tc.success || a.AvailableTotal != len(rows) {
					t.Fatalf("可用率结论错误：%+v", a)
				}
			}
		})
	}
}

func TestAssessIgnoresLegacyIntelligenceInputs(t *testing.T) {
	rows := outcomes(12, 12, true)
	before := Assess(rows, th, false)
	for i := range rows {
		rows[i].TargetPass = false
		rows[i].RefPass = false
		rows[i].Ref.ErrorClass = "http_5xx"
		rows[i].Ref.ReasoningTokens = 100000
		rows[i].Target.ReasoningTokens = 1
		rows[i].Suite = []string{"iq", "reasoning", "long_context"}[i%3]
		rows[i].ExpectedPassRate = f(.01)
		rows[i].RefHistRate = f(.99)
	}
	a := Assess(rows, Thresholds{}, true)
	if a.ReferenceErrorCounts["http_5xx"] != len(rows) {
		t.Fatalf("基线故障证据丢失：%+v", a)
	}
	a.ReferenceErrorCounts = nil
	if !reflect.DeepEqual(before, a) {
		t.Fatalf("旧测智输入改变可用率或性能：%+v → %+v", before, a)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"llr", "samples", "relative_score", "reasoning_ratio", "target_pass_rate", "confidence", "grade"} {
		if _, ok := summary[key]; ok {
			t.Fatalf("汇总仍输出旧测智指标 %s：%s", key, raw)
		}
	}
}

func TestAssessPerformanceUsesSuccessfulRequests(t *testing.T) {
	rows := outcomes(4, 0, false)
	for i := range rows {
		rows[i].Suite = "inspection_capability"
		rows[i].Target.TTFT = time.Duration(i+1) * 100 * time.Millisecond
		rows[i].Target.Duration = rows[i].Target.TTFT + time.Second
		rows[i].Target.OutputTokens = 10 * (i + 1)
	}
	rows = append(rows, Outcome{Suite: "inspection_function"}, Outcome{Suite: "inspection_function", Target: ProbeResult{HTTPStatus: 503, TTFT: time.Hour, OutputTokens: 100000}})
	a := Assess(rows, th, true)
	if a.AvailableTotal != 5 || a.AvailableOK != 4 || a.TTFTp50Ms == nil || *a.TTFTp50Ms != 200 || a.TTFTp95Ms == nil || *a.TTFTp95Ms != 400 || a.OutputTPS == nil || *a.OutputTPS != 20 {
		t.Fatalf("未执行或失败请求污染性能统计：%+v", a)
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
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
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
	if c.VerdictFor("mismatch") != "normal" {
		t.Error("ignore should not affect the verdict")
	}
	if !c.Applies("onboarding") || c.Applies("scheduled") {
		t.Error("Applies")
	}
	for _, bad := range []ThirdPartyCheck{
		{Provider: "unknown", Triggers: []string{"onboarding"}},
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
