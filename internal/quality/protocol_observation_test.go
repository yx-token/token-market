package quality

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestProtocolUsageMissingVersusZero(t *testing.T) {
	cases := []struct{ platform, body string }{
		{"anthropic", `{"model":"claude-opus-5-5","content":[{"type":"text","text":"答案"}],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens_details":{"thinking_tokens":0}}}`},
		{"openai", `{"model":"gpt-6.1-sol","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"答案"}]}],"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`},
		{"kimi", `{"choices":[{"message":{"content":"答案"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`},
		{"gemini", `{"candidates":[{"content":{"parts":[{"text":"答案"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"thoughtsTokenCount":0,"cachedContentTokenCount":0}}`},
	}
	for _, c := range cases {
		t.Run(c.platform, func(t *testing.T) {
			res := ProbeResult{}
			if err := protocolFor(c.platform).parseJSON([]byte(c.body), &res); err != nil {
				t.Fatal(err)
			}
			if res.InputTokens == nil || *res.InputTokens != 0 || res.CacheReadTokens == nil || *res.CacheReadTokens != 0 || !res.OutputObserved || !res.ReasoningObserved {
				t.Fatalf("明确零观测被丢弃：%+v", res)
			}
			missing := ProbeResult{}
			if err := protocolFor(c.platform).parseJSON([]byte(`{}`), &missing); err != nil {
				t.Fatal(err)
			}
			if missing.InputTokens != nil || missing.CacheReadTokens != nil || missing.OutputObserved || missing.ReasoningObserved {
				t.Fatalf("缺失观测被伪造为零：%+v", missing)
			}
		})
	}
}

func TestProtocolToolArgumentsPreserveLargeIntegers(t *testing.T) {
	cases := []struct{ platform, body string }{
		{"anthropic", `{"content":[{"type":"tool_use","id":"c","name":"lookup","input":{"n":9007199254740993}}],"stop_reason":"tool_use"}`},
		{"openai", `{"status":"completed","output":[{"type":"function_call","call_id":"c","name":"lookup","arguments":"{\"n\":9007199254740993}"}]}`},
		{"kimi", `{"choices":[{"message":{"tool_calls":[{"id":"c","type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740993}"}}]},"finish_reason":"tool_calls"}]}`},
		{"gemini", `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c","name":"lookup","args":{"n":9007199254740993}}}]},"finishReason":"STOP"}]}`},
	}
	for _, c := range cases {
		t.Run(c.platform, func(t *testing.T) {
			res := ProbeResult{}
			if err := protocolFor(c.platform).parseJSON([]byte(c.body), &res); err != nil {
				t.Fatal(err)
			}
			if len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments["n"] != json.Number("9007199254740993") {
				t.Fatalf("工具大整数丢失精度：%+v", res.ToolCalls)
			}
		})
	}
}

func TestProtocolRejectsIncompleteStreams(t *testing.T) {
	for _, c := range []struct{ platform, body string }{
		{"anthropic", "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"答案\"}}\n\n"},
		{"openai", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"答案\"}\n\n"},
		{"kimi", "data: {\"choices\":[{\"delta\":{\"content\":\"答案\"}}]}\n\n"},
	} {
		t.Run(c.platform, func(t *testing.T) {
			res := ProbeResult{}
			if err := protocolFor(c.platform).parseStream(strings.NewReader(c.body), time.Now(), &res); err == nil {
				t.Fatal("缺少完成事件的流不应成功")
			}
		})
	}
}

func TestProbeToolOnlyAndRefusalRemainProtocolSuccess(t *testing.T) {
	for _, body := range []string{
		`{"content":[{"type":"tool_use","id":"c","name":"lookup","input":{}}],"stop_reason":"tool_use","usage":{"output_tokens":5}}`,
		`{"content":[],"stop_reason":"refusal","usage":{"output_tokens":0}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		res := (&Prober{GatewayURL: srv.URL, Client: srv.Client()}).Probe(context.Background(), "anthropic", "secret-key", ProbeRequest{Model: "claude-opus-5-5", Prompt: "题目", MaxTokens: 32})
		srv.Close()
		if !res.OK() {
			t.Fatalf("工具或拒答不应误判为空响应：%+v", res)
		}
		if res.StopReason == "refusal" && res.EligibleForIntelligence() {
			t.Fatal("拒答不应进入智力统计")
		}
	}
}

func TestProbeErrorBodyRedactsCredentialsIPAndPrompt(t *testing.T) {
	message := "secret-key-123 10.20.30.40 https://local.invalid/path 只返回保密题目。 Bearer abcdefghijklmnop " + strings.Repeat("长错误描述", 3000)
	raw, _ := json.Marshal(map[string]any{"error": map[string]any{"code": "invalid_request_error", "message": message, "headers": map[string]string{"Authorization": "secret-key-123"}}, "request": "只返回保密题目。"})
	res := ProbeResult{HTTPStatus: 400}
	classifyProbeError(&res, raw, ProbeRequest{Prompt: "只返回保密题目。"}, "secret-key-123")
	if len(res.ErrorBody) > 4096 || !utf8.ValidString(res.ErrorBody) {
		t.Fatalf("错误体大小或编码无效：%d", len(res.ErrorBody))
	}
	for _, secret := range []string{"secret-key-123", "10.20.30.40", "local.invalid", "保密题目", "abcdefghijklmnop", "Authorization", "headers"} {
		if strings.Contains(res.ErrorBody, secret) {
			t.Fatalf("错误体泄露%s：%s", secret, res.ErrorBody)
		}
	}
}

func TestHaiku55DisabledThinkingOverride(t *testing.T) {
	req := ProbeRequest{Model: "claude-haiku-5-5", Prompt: "题目", MaxTokens: 100, Effort: "high", Extra: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	if err := validateProbeRequest("anthropic", req); err != nil {
		t.Fatal(err)
	}
	b := buildJSON(t, "anthropic", req.Model, req)
	if b["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatal("Haiku5.5合法关闭思考配置被覆盖")
	}
	res := ProbeResult{Text: "答案", OutputTokens: 5, OutputObserved: true}
	estimateClaudeReasoning(req, &res)
	if res.ReasoningObserved || res.HasFlag("reasoning_estimated") {
		t.Fatal("关闭思考的请求不应伪造推理观测")
	}
}
