package quality

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildJSON(t *testing.T, platform, model string, req ProbeRequest) map[string]any {
	t.Helper()
	p := protocolForModel(platform, model)
	_, body, _ := p.build(req)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProtocolModelParameterGoldens(t *testing.T) {
	tests := []struct{ platform, model string }{
		{"anthropic", "claude-opus-4-7"}, {"anthropic", "claude-opus-4-8"},
		{"anthropic", "claude-opus-5-5"}, {"anthropic", "claude-sonnet-5"},
		{"anthropic", "claude-haiku-5-5"}, {"anthropic", "claude-fable-5-1"},
		{"anthropic", "claude-sonnet-4-5-20250929"}, {"anthropic", "claude-haiku-4-5-20251001"},
		{"gemini", "gemini-3.8-flash"}, {"gemini", "gemini-2.5-pro"},
		{"openai", "gpt-6.1-sol"}, {"kimi", "kimi-k3"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			req := ProbeRequest{Model: tt.model, Prompt: "只输出最终答案。", System: "只返回题目指定格式。", MaxTokens: 1024, Effort: "high", ReasoningEffort: "xhigh",
				Tools: []ProbeTool{{Name: "lookup_schedule", Description: "查询本地日程", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}, "additionalProperties": false}}}}
			path, body, stream := protocolForModel(tt.platform, tt.model).build(req)
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			var canonicalBody map[string]any
			if err := json.Unmarshal(encoded, &canonicalBody); err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(map[string]any{"path": path, "body": canonicalBody, "stream": stream}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "protocol", tt.model+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(append(got, '\n'), want) {
				t.Fatalf("协议请求与 golden 不一致：\n%s", got)
			}
		})
	}
}

func TestProtocolToolsAlwaysAuto(t *testing.T) {
	req := ProbeRequest{Model: "claude-opus-5-5", Prompt: "调用工具", MaxTokens: 100, Tools: []ProbeTool{{Name: "lookup", Description: "本地查询", InputSchema: map[string]any{"type": "object"}}}}
	for _, platform := range []string{"anthropic", "openai", "gemini", "kimi"} {
		body := buildJSON(t, platform, req.Model, req)
		if platform == "anthropic" {
			if body["tool_choice"].(map[string]any)["type"] != "auto" {
				t.Errorf("%s tool_choice=%v", platform, body["tool_choice"])
			}
		} else if platform == "gemini" {
			mode := body["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)["mode"]
			if mode != "AUTO" {
				t.Errorf("%s tool mode=%v", platform, mode)
			}
		} else if body["tool_choice"] != "auto" {
			t.Errorf("%s tool_choice=%v", platform, body["tool_choice"])
		}
	}
}

func TestSSERequiresCompleteAndParsesTool(t *testing.T) {
	res := ProbeResult{}
	stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"c1\",\"name\":\"lookup\"}}\n\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\\\"x\\\":1}\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt\",\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n"
	if err := (responsesProtocol{}).parseStream(strings.NewReader(stream), time.Now(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Text != "ok" || len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "lookup" || res.InputTokens == nil {
		t.Fatalf("解析结果=%+v", res)
	}
	if err := (responsesProtocol{}).parseStream(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"), time.Now(), &ProbeResult{}); err == nil {
		t.Fatal("不完整流不应成功")
	}
}

func TestProbeErrorClassificationAndBudget(t *testing.T) {
	tests := []struct {
		name, body, want string
		status           int
		expected         ProbeExpected
	}{
		{"网关并发优先", `{"error":{"type":"rate_limit_error","code":"gateway_concurrency_limit","message":"Concurrency limit exceeded"}}`, "platform", 429, ProbeExpected{}},
		{"网关队列", `{"error":{"code":"gateway_queue_full"}}`, "platform", 429, ProbeExpected{}},
		{"客户端版本", `{"error":{"code":"claude_code_version_too_old"}}`, "platform", 400, ProbeExpected{}},
		{"平台余额", `{"code":"INSUFFICIENT_BALANCE","message":"余额不足"}`, "platform", 403, ProbeExpected{}},
		{"嵌套平台余额", `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "platform", 403, ProbeExpected{}},
		{"响应内平台余额", `{"response":{"error":{"code":"INSUFFICIENT_BALANCE"}}}`, "platform", 403, ProbeExpected{}},
		{"小写余额码", `{"error":{"code":"insufficient_balance"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额码错误状态", `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 402, ProbeExpected{}},
		{"余额码限流状态", `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "rate_limited", 429, ProbeExpected{}},
		{"余额码故障状态", `{"error":{"code":"INSUFFICIENT_BALANCE"}}`, "http_5xx", 500, ProbeExpected{}},
		{"余额类型不是码", `{"error":{"type":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额状态不是码", `{"error":{"status":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额键不是码", `{"error":{"errorKey":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额消息不是码", `{"error":{"message":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额码前缀", `{"error":{"code":"VENDOR_INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额码后缀", `{"error":{"code":"INSUFFICIENT_BALANCE_EXHAUSTED"}}`, "http_4xx", 403, ProbeExpected{}},
		{"余额码空格", `{"error":{"code":" INSUFFICIENT_BALANCE "}}`, "http_4xx", 403, ProbeExpected{}},
		{"厂商余额提示", `{"error":{"message":"Insufficient balance"}}`, "http_4xx", 402, ProbeExpected{}},
		{"厂商中文余额", `{"error":{"message":"余额不足"}}`, "http_4xx", 403, ProbeExpected{}},
		{"厂商余额不足码", `{"error":{"code":"balance_not_enough"}}`, "http_4xx", 403, ProbeExpected{}},
		{"厂商信用余额", `{"error":{"message":"insufficient credit balance"}}`, "http_4xx", 403, ProbeExpected{}},
		{"厂商余额参数类型", `{"error":{"type":"invalid_request_error","message":"insufficient credit balance"}}`, "http_4xx", 400, ProbeExpected{}},
		{"厂商反序余额", `{"error":{"message":"balance insufficient"}}`, "http_5xx", 500, ProbeExpected{}},
		{"普通并发提示", `{"error":{"message":"Concurrency limit exceeded"}}`, "rate_limited", 429, ProbeExpected{}},
		{"普通并发故障", `{"error":{"message":"Concurrency limit exceeded"}}`, "http_5xx", 503, ProbeExpected{}},
		{"不解析余额请求回显", `{"request":{"code":"INSUFFICIENT_BALANCE"}}`, "http_4xx", 403, ProbeExpected{}},
		{"普通故障", `{"error":{"type":"overloaded_error","message":"no available accounts"}}`, "http_5xx", 503, ProbeExpected{}},
		{"普通限流", `{"error":{"type":"rate_limit_error","code":"rate_limit_error"}}`, "rate_limited", 429, ProbeExpected{}},
		{"预期拒绝", `{"error":{"type":"invalid_request_error","code":"invalid_request_error"}}`, "expected_reject", 400, ProbeExpected{Outcome: "reject", HTTPStatus: []int{400}, ErrorCodes: []string{"invalid_request_error"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ProbeResult{HTTPStatus: tt.status}
			classifyProbeError(&res, []byte(tt.body), ProbeRequest{Model: "m", Expected: tt.expected}, "secret-key-123456")
			if res.ErrorClass != tt.want {
				t.Fatalf("分类=%q，预期 %q (%+v)", res.ErrorClass, tt.want, res)
			}
			if res.HasFlag("platform") != (tt.want == "platform") {
				t.Fatalf("平台标记须与分类一致，厂商余额错误不能排除观测：%+v", res)
			}
			if len(res.ErrorBody) > 4096 {
				t.Fatalf("脱敏错误体超过4KiB: %d", len(res.ErrorBody))
			}
		})
	}
	in, out, err := probeTokenReservation("anthropic", ProbeRequest{Model: "claude-haiku-4-5", Prompt: "题目", MaxTokens: 100, ReasoningEffort: "high"})
	if err != nil || in <= 0 || out != 4096 {
		t.Fatalf("Claude 预算=%d/%d err=%v", in, out, err)
	}
	_, out, err = probeTokenReservation("gemini", ProbeRequest{Model: "gemini-2.5-pro", Prompt: "题目", MaxTokens: 100, Effort: "high"})
	if err != nil || out != 2148 {
		t.Fatalf("Gemini 预算=%d err=%v", out, err)
	}
}
