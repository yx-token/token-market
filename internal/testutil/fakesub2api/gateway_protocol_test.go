package fakesub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseGatewayRequest(t *testing.T) {
	tests := []struct {
		name, body, action, model, prompt string
	}{
		{"消息字符串", `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"PONG1"}]}`, "", "claude-sonnet-4-5", "PONG1"},
		{"消息内容块", `{"model":"claude-opus-4-8","messages":[{"role":"user","content":[{"type":"text","text":"PONG2"},{"type":"image","source":{"data":"忽略"}},{"type":"text","text":"继续"}]}]}`, "", "claude-opus-4-8", "PONG2\n继续"},
		{"末尾助手消息", `{"model":"gpt-6","messages":[{"role":"user","content":"PONG3"},{"role":"assistant","content":"旧答案"}]}`, "", "gpt-6", "PONG3"},
		{"Responses字符串", `{"model":"gpt-6","input":"PONG4"}`, "", "gpt-6", "PONG4"},
		{"Responses内容块", `{"model":"gpt-6","input":[{"role":"user","content":[{"type":"input_text","text":"旧问题"}]},{"role":"assistant","content":[{"type":"output_text","text":"旧答案"}]},{"role":"user","content":[{"type":"input_text","text":"PONG5"}]}]}`, "", "gpt-6", "PONG5"},
		{"Responses工具结果", `{"model":"gpt-6","input":[{"type":"function_call","name":"lookup_schedule","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"本地结果"}]}`, "", "gpt-6", "本地结果"},
		{"Gemini多块", `{"contents":[{"role":"user","parts":[{"text":"PONG6"},{"inlineData":{"data":"忽略"}},{"text":"继续"}]}]}`, "gemini-3-flash:generateContent", "gemini-3-flash", "PONG6\n继续"},
		{"Gemini工具结果", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"lookup_schedule","response":{"value":3}}}]}]}`, "gemini-2.5-flash:generateContent", "gemini-2.5-flash", `{"value":3}`},
		{"Claude工具结果", `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"本地结果"}]}]}]}`, "", "claude-sonnet-4-5", "本地结果"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(tt.body), &body); err != nil {
				t.Fatal(err)
			}
			request := parseGatewayRequest(body, tt.action)
			if request.Model != tt.model || request.Prompt != tt.prompt {
				t.Fatalf("请求解析不符：model=%q prompt=%q", request.Model, request.Prompt)
			}
			if request.Body == nil {
				t.Fatal("完整请求丢失")
			}
		})
	}
}

func TestGatewayUsageIncludesRequestContent(t *testing.T) {
	request := GatewayRequest{Body: map[string]any{"input": "题目", "max_output_tokens": 32}}
	response := GatewayResponse{Text: "答案"}
	base := estimateGatewayUsage(request, response)
	if base.InputTokens <= 0 || base.OutputTokens <= 0 || base.InputTokens == InputTokens {
		t.Fatalf("默认用量应来自请求：%+v", base)
	}
	request.Body["system"] = strings.Repeat("系统指令", 32)
	withSystem := estimateGatewayUsage(request, response)
	if withSystem.InputTokens <= base.InputTokens {
		t.Fatal("系统指令未计入用量")
	}
	request.Body["tools"] = []any{map[string]any{"name": "lookup_schedule", "input_schema": map[string]any{"type": "object"}}}
	withTools := estimateGatewayUsage(request, response)
	if withTools.InputTokens <= withSystem.InputTokens {
		t.Fatal("工具定义未计入用量")
	}
	request.Body["max_output_tokens"] = 100000
	if changed := estimateGatewayUsage(request, response); changed.InputTokens != withTools.InputTokens {
		t.Fatal("输出上限不应改变输入用量")
	}
	response.Text = strings.Repeat("答案", 100)
	if changed := estimateGatewayUsage(request, response); changed.OutputTokens <= withTools.OutputTokens {
		t.Fatal("输出用量未随回答变化")
	}
}

func TestGatewayAccountOverrides(t *testing.T) {
	s := NewUnstarted(nil, "test-key")
	request := GatewayRequest{Prompt: "PONG1", Body: map[string]any{"input": "PONG1"}}
	s.SetResponder(func(_ int64, prompt string) string { return prompt })
	first, ok := s.gatewayResponse(context.Background(), 1, request)
	if !ok || first.Text != "PONG1" {
		t.Fatalf("旧回答器不兼容：%+v", first)
	}
	second, _ := s.gatewayResponse(context.Background(), 2, request)
	if *first.Usage != *second.Usage {
		t.Fatal("相同请求的默认用量应一致")
	}
	override := GatewayUsage{InputTokens: 100, OutputTokens: 30, ReasoningTokens: 20, CacheReadInputTokens: 40, CacheCreationInputTokens: 50}
	s.SetAccountUsage(1, &override)
	override.InputTokens = 999
	s.SetAccountStopReason(1, "max_tokens")
	calls := []GatewayToolCall{{ID: "call_1", Name: "lookup_schedule", Arguments: map[string]any{"city": "示例城市"}}}
	s.SetAccountToolCalls(1, calls)
	calls[0].Arguments["city"] = "被修改"
	withOverrides, _ := s.gatewayResponse(context.Background(), 1, request)
	if withOverrides.Usage.InputTokens != 100 || withOverrides.StopReason != "max_tokens" || withOverrides.ToolCalls[0].Arguments["city"] != "示例城市" {
		t.Fatalf("账号配置未独立保存：%+v", withOverrides)
	}
	s.SetAccountUsage(1, &GatewayUsage{})
	zero, _ := s.gatewayResponse(context.Background(), 1, request)
	if *zero.Usage != (GatewayUsage{}) {
		t.Fatal("显式零观测不应变成默认值")
	}
	s.SetAccountUsage(1, nil)
	reset, _ := s.gatewayResponse(context.Background(), 1, request)
	if reset.Usage.InputTokens != first.Usage.InputTokens {
		t.Fatal("nil 未恢复默认用量")
	}
	s.SetGatewayResponder(func(accountID int64, req GatewayRequest) GatewayResponse {
		if accountID != 1 || req.Body["input"] != "PONG1" {
			t.Error("完整请求回答器未收到原请求")
		}
		return GatewayResponse{Text: "完整回答", Usage: &GatewayUsage{InputTokens: 7}, StopReason: "end_turn", ToolCalls: []GatewayToolCall{}}
	})
	full, _ := s.gatewayResponse(context.Background(), 1, request)
	if full.Text != "完整回答" || full.Usage.InputTokens != 7 || full.StopReason != "end_turn" || len(full.ToolCalls) != 0 {
		t.Fatalf("回答器应优先于账号配置：%+v", full)
	}
}

func TestGatewayResponseProtocols(t *testing.T) {
	usage := &GatewayUsage{InputTokens: 101, OutputTokens: 30, ReasoningTokens: 20, CacheReadInputTokens: 40, CacheCreationInputTokens: 50}
	response := GatewayResponse{Usage: usage, ToolCalls: []GatewayToolCall{{Name: "lookup_schedule", Arguments: map[string]any{"city": "示例城市"}}}}
	tests := []struct {
		path, usageKey, inputKey, outputKey, callsKey, stop string
	}{
		{"/v1/messages", "usage", "input_tokens", "output_tokens", "content", "tool_use"},
		{"/v1/responses", "usage", "input_tokens", "output_tokens", "output", "completed"},
		{"/v1/chat/completions", "usage", "prompt_tokens", "completion_tokens", "choices", "tool_calls"},
		{"/v1beta/models/gemini-3-flash:generateContent", "usageMetadata", "promptTokenCount", "candidatesTokenCount", "candidates", "STOP"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			raw, err := json.Marshal(responseBody(tt.path, "req_1", "测试型号", response))
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			actualUsage := body[tt.usageKey].(map[string]any)
			if actualUsage[tt.inputKey] != float64(101) || actualUsage[tt.outputKey] != float64(30) {
				t.Fatalf("用量字段错误：%s", raw)
			}
			if !strings.Contains(string(raw), "lookup_schedule") || !strings.Contains(string(raw), tt.stop) || body[tt.callsKey] == nil {
				t.Fatalf("工具或结束原因丢失：%s", raw)
			}
		})
	}
	response.StopReason = "max_output_tokens"
	truncated := responseBody("/v1/responses", "req_1", "测试型号", response)
	if truncated["status"] != "incomplete" || truncated["incomplete_details"].(map[string]string)["reason"] != "max_output_tokens" {
		t.Fatalf("Responses 截断响应错误：%+v", truncated)
	}
}

func TestGatewayConcurrency(t *testing.T) {
	s := NewUnstarted(nil, "test-key")
	first := s.acquireAccount(1, 2)
	second := s.acquireAccount(1, 2)
	excess := s.acquireAccount(1, 2)
	if !first || !second || excess {
		t.Fatal("账号实际并发上限未生效")
	}
	if !s.acquireAccount(2, 1) {
		t.Fatal("不同账号应有独立并发槽")
	}
	if s.acquireAccount(1, 1) {
		t.Fatal("已降低的账号并发应立即生效")
	}
	s.releaseAccount(1)
	s.releaseAccount(1)
	if !s.acquireAccount(1, 1) {
		t.Fatal("已释放槽应可再次获得")
	}
	s.releaseAccount(1)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.acquireAccount(1, 2) {
				s.mu.Lock()
				if s.active[1] > 2 {
					t.Error("竞争条件导致并发超限")
				}
				s.mu.Unlock()
				s.releaseAccount(1)
			}
		}()
	}
	wg.Wait()
	recorder := httptest.NewRecorder()
	gatewayConcurrencyError(recorder)
	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), `"code":"gateway_concurrency_limit"`) {
		t.Fatalf("平台并发错误码不符：%d %s", recorder.Code, recorder.Body)
	}
}

func TestGatewayCancellationReleasesSlot(t *testing.T) {
	s := NewUnstarted(nil, "test-key")
	s.SetAccountDelay(1, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !s.acquireAccount(1, 1) {
		t.Fatal("未获得测试槽")
	}
	func() {
		defer s.releaseAccount(1)
		if _, ok := s.gatewayResponse(ctx, 1, GatewayRequest{}); ok {
			t.Fatal("取消请求不应得到成功响应")
		}
	}()
	if !s.acquireAccount(1, 1) {
		t.Fatal("取消后未释放并发槽")
	}
	s.releaseAccount(1)
	s.SetAccountDelay(1, 0)
	blocked, release := make(chan struct{}), make(chan struct{})
	s.SetResponder(func(_ int64, _ string) string {
		close(blocked)
		<-release
		return "迟到响应"
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel = context.WithCancel(context.Background())
	finished := make(chan bool, 1)
	go func() {
		_, ok := s.gatewayResponse(ctx, 1, GatewayRequest{})
		finished <- ok
	}()
	<-blocked
	cancel()
	select {
	case ok := <-finished:
		if ok {
			t.Fatal("挂起回答器取消后不应返回成功")
		}
	case <-time.After(time.Second):
		t.Fatal("请求取消不应等待挂起回答器")
	}
}
