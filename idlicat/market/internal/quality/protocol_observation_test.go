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

func TestChatTextContentRemainsString(t *testing.T) {
	for _, platform := range []string{"kimi", "glm", "minimax"} {
		t.Run(platform, func(t *testing.T) {
			req := ProbeRequest{Model: "coding-model", MaxTokens: 100, Messages: []ProbeMessage{
				{Role: "user", Content: []ProbeContent{{Type: "text", Text: "第一段。"}, {Type: "text", Text: "第二段。"}}},
				{Role: "assistant", Content: []ProbeContent{{Type: "text", Text: "答案"}}},
			}}
			body := buildJSON(t, platform, req.Model, req)
			messages := body["messages"].([]any)
			if got := messages[0].(map[string]any)["content"]; got != "第一段。第二段。" {
				t.Fatalf("纯文本块应拼接为字符串：%v", got)
			}
			if got := messages[1].(map[string]any)["content"]; got != "答案" {
				t.Fatalf("助手纯文本应使用字符串：%v", got)
			}
		})
	}

	req := ProbeRequest{Model: "coding-model", MaxTokens: 100, Attachments: []ProbeAttachment{{ID: "图", MediaType: "image/png", Encoding: "base64", Data: "eA=="}},
		Messages: []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: "查看图像。"}, {Type: "image", AttachmentID: "图"}}}}}
	body := buildJSON(t, "kimi", req.Model, req)
	content, ok := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if !ok || len(content) != 2 || content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("图片消息应保留内容块：%v", body)
	}
}

func TestCodexSystemRequirementIsUserContent(t *testing.T) {
	req := ProbeRequest{Model: "gpt-6.1-sol", System: "只输出最终答案。", MaxTokens: 100, Messages: []ProbeMessage{
		{Role: "user", Content: []ProbeContent{{Type: "text", Text: "第一题"}}},
		{Role: "assistant", Content: []ProbeContent{{Type: "text", Text: "42"}}},
		{Role: "user", Content: []ProbeContent{{Type: "text", Text: "第二题"}}},
	}}
	body := buildJSON(t, "openai", req.Model, req)
	if _, ok := body["instructions"]; ok {
		t.Fatal("Codex 请求不能覆盖官方 instructions")
	}
	input := body["input"].([]any)
	for _, item := range input {
		if item.(map[string]any)["role"] == "system" {
			t.Fatal("Codex 请求不能携带 system 角色")
		}
	}
	first := input[0].(map[string]any)["content"].([]any)
	if first[0].(map[string]any)["text"] != req.System+"\n\n" || first[1].(map[string]any)["text"] != "第一题" {
		t.Fatalf("系统要求未前置首条用户消息：%v", first)
	}
	last := input[2].(map[string]any)["content"].([]any)
	if len(last) != 1 || last[0].(map[string]any)["text"] != "第二题" || req.Messages[0].Content[0].Text != "第一题" {
		t.Fatal("系统要求被重复加入或原始消息被修改")
	}

	req.Prompt, req.Messages = "第一题", nil
	body = buildJSON(t, "openai", req.Model, req)
	if body["input"] != req.System+"\n\n第一题" {
		t.Fatalf("短请求系统要求未前置用户题面：%v", body)
	}
}

func TestContextWindowExceededIsTruncated(t *testing.T) {
	res := ProbeResult{StopReason: "model_context_window_exceeded"}
	normalizeStop(&res)
	if !res.HasFlag("truncated") || res.HasFlag("unknown_stop_reason") || res.EligibleForIntelligence() {
		t.Fatalf("上下文窗口耗尽应记为截断并排除智力统计：%+v", res)
	}
}

func TestProbeErrorNumericCode(t *testing.T) {
	code, _, _ := errorDetails([]byte(`{"error":{"code":429,"message":"请求过多"}}`))
	if code != "429" {
		t.Fatalf("数字错误码未保留：%q", code)
	}
}

func TestGeminiMissingToolIDMultiStep(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup_schedule","args":{"city":"杭州"}},"thoughtSignature":"opaque-signature"}]},"finishReason":"STOP"}]}`)
	first := ProbeResult{}
	if err := (geminiProtocol{}).parseJSON(raw, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID == "" || !first.ToolCalls[0].LocalID || first.ToolCalls[0].ThoughtSignature != "opaque-signature" {
		t.Fatalf("缺省调用编号或签名未规范化：%+v", first.ToolCalls)
	}
	// 状态经过数据库的 JSON 往返后，仍保留真实签名与内部编号。
	state := resumedState{Complete: true, ToolCalls: first.ToolCalls}
	var restored resumedState
	if err := decodeInspectionJSON(mustJSON(state), &restored); err != nil {
		t.Fatal(err)
	}
	previous := ProbeRequest{Model: "gemini-3.8-flash", Prompt: "查询杭州班次", MaxTokens: 100, Tools: []ProbeTool{{Name: "lookup_schedule", InputSchema: map[string]any{"type": "object"}}}}
	step := plannedStep{UsePrevious: true, Request: ProbeRequest{Model: previous.Model, Prompt: "只输出发车时间", MaxTokens: 100, Tools: previous.Tools}, ToolResults: map[string]any{"lookup_schedule": map[string]any{"departure": "09:30"}}}
	req := dynamicRequest(step, previous, restored)
	for _, platform := range []string{"gemini", "antigravity"} {
		if err := validateProbeRequest(platform, req); err != nil {
			t.Fatalf("%s 的无编号工具多步请求无效：%v", platform, err)
		}
		body := buildJSON(t, platform, req.Model, req)
		contents := body["contents"].([]any)
		part := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)
		call := part["functionCall"].(map[string]any)
		if _, present := call["id"]; present || part["thoughtSignature"] != "opaque-signature" || call["name"] != "lookup_schedule" {
			t.Fatalf("本地编号进入 wire 或签名/名称丢失：%v", part)
		}
		response := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
		if _, present := response["id"]; present || response["name"] != "lookup_schedule" {
			t.Fatalf("本地工具结果编号或名称无效：%v", response)
		}
		object, ok := response["response"].(map[string]any)
		if !ok || object["departure"] != "09:30" {
			t.Fatalf("Gemini 工具结果须为对象：%v", response)
		}
	}
}

func TestGeminiToolIDsAreDeterministicAndDistinct(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"n":1}}},{"functionCall":{"id":"tm-gemini-local-0","name":"lookup","args":{"n":2}}},{"functionCall":{"name":"lookup","args":{"n":3}}}]},"finishReason":"STOP"}]}`)
	var first, second ProbeResult
	if err := (geminiProtocol{}).parseJSON(raw, &first); err != nil {
		t.Fatal(err)
	}
	if err := (geminiProtocol{}).parseJSON(raw, &second); err != nil {
		t.Fatal(err)
	}
	if string(mustJSON(first.ToolCalls)) != string(mustJSON(second.ToolCalls)) {
		t.Fatal("相同响应的本地工具编号不稳定")
	}
	seen := map[string]bool{}
	for i, call := range first.ToolCalls {
		if call.ID == "" || seen[call.ID] || call.LocalID != (i != 1) {
			t.Fatalf("工具编号为空、冲突或错误标记：%+v", first.ToolCalls)
		}
		seen[call.ID] = true
	}
	if first.ToolCalls[1].ID != "tm-gemini-local-0" {
		t.Fatal("上游真实调用编号被覆盖")
	}
}

func TestGeminiProvidedToolIDsAndResponseObject(t *testing.T) {
	req := ProbeRequest{Model: "gemini-3.8-flash", MaxTokens: 100, Messages: []ProbeMessage{
		{Role: "assistant", Content: []ProbeContent{{Type: "tool_use", ID: "upstream-call", Name: "lookup", Input: map[string]any{}, ThoughtSignature: "opaque-signature"}}},
		{Role: "tool", Content: []ProbeContent{{Type: "tool_result", ToolUseID: "upstream-call", Name: "lookup", Content: `{"n":9007199254740993}`}}},
	}}
	if err := validateProbeRequest("gemini", req); err != nil {
		t.Fatal(err)
	}
	_, raw, _ := (geminiProtocol{}).build(req)
	body, err := decodeObject(mustJSON(raw))
	if err != nil {
		t.Fatal(err)
	}
	contents := list(body["contents"])
	call := object(object(list(object(contents[0])["parts"])[0])["functionCall"])
	response := object(object(list(object(contents[1])["parts"])[0])["functionResponse"])
	if call["id"] != "upstream-call" || response["id"] != "upstream-call" || object(response["response"])["n"] != json.Number("9007199254740993") {
		t.Fatalf("真实编号或工具结果大整数丢失：%v / %v", call, response)
	}
}
