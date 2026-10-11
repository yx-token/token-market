package fakesub2api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GatewayUsage 是响应观测值；账单仍使用 fake.go 的固定用量。
type GatewayUsage struct {
	InputTokens              int
	OutputTokens             int
	ReasoningTokens          int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// GatewayToolCall 只描述虚拟工具调用，替身不会执行工具。
type GatewayToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// GatewayRequest 保留完整请求，并提供最后一条用户输入供旧回答器使用。
type GatewayRequest struct {
	Model  string
	Prompt string
	Body   map[string]any
}

// GatewayResponse 可覆盖文本、用量、结束原因和工具调用。
type GatewayResponse struct {
	Text       string
	Usage      *GatewayUsage
	StopReason string
	ToolCalls  []GatewayToolCall
}

// GatewayConfig 用于按账号模拟不同的响应行为，零值使用正常响应。
type GatewayConfig struct {
	Usage      *GatewayUsage
	StopReason string
	Delay      time.Duration
	ToolCalls  []GatewayToolCall
}

// SetGatewayResponder 设置可读取完整协议请求的回答器，优先于 SetResponder。
func (s *Server) SetGatewayResponder(fn func(accountID int64, request GatewayRequest) GatewayResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gatewayAnswer = fn
}

// SetAccountUsage 覆盖账号的响应用量；nil 恢复按请求估算，显式零值会原样返回。
func (s *Server) SetAccountUsage(accountID int64, usage *GatewayUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.gatewayConfig[accountID]
	if usage == nil {
		c.Usage = nil
	} else {
		copyUsage := *usage
		c.Usage = &copyUsage
	}
	s.gatewayConfig[accountID] = c
}

// SetAccountStopReason 设置协议原始结束原因，空值恢复协议默认值。
func (s *Server) SetAccountStopReason(accountID int64, stop string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.gatewayConfig[accountID]
	c.StopReason = stop
	s.gatewayConfig[accountID] = c
}

// SetAccountDelay 设置转发延迟；请求取消时立即结束等待并释放并发槽。
func (s *Server) SetAccountDelay(accountID int64, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.gatewayConfig[accountID]
	c.Delay = delay
	s.gatewayConfig[accountID] = c
}

// SetAccountToolCalls 设置返回的虚拟工具调用，nil 恢复纯文本响应。
func (s *Server) SetAccountToolCalls(accountID int64, calls []GatewayToolCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.gatewayConfig[accountID]
	c.ToolCalls = cloneCalls(calls)
	s.gatewayConfig[accountID] = c
}

func cloneCalls(calls []GatewayToolCall) []GatewayToolCall {
	if calls == nil {
		return nil
	}
	copyCalls := make([]GatewayToolCall, len(calls))
	for i, call := range calls {
		copyCalls[i] = call
		copyCalls[i].Arguments = nil
		if call.Arguments != nil {
			raw, err := json.Marshal(call.Arguments)
			if err == nil {
				_ = json.Unmarshal(raw, &copyCalls[i].Arguments)
			}
		}
	}
	return copyCalls
}

func (s *Server) acquireAccount(accountID int64, concurrency int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if concurrency < 1 || s.active[accountID] >= concurrency {
		return false
	}
	s.active[accountID]++
	return true
}

func (s *Server) releaseAccount(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[accountID] <= 1 {
		delete(s.active, accountID)
	} else {
		s.active[accountID]--
	}
}

func gatewayConcurrencyError(w http.ResponseWriter) {
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"type": "error",
		"error": map[string]string{
			"type": "rate_limit_error", "code": "gateway_concurrency_limit",
			"message": "Concurrency limit exceeded for account, please retry later",
		},
	})
}

func parseGatewayRequest(body map[string]any, action string) GatewayRequest {
	req := GatewayRequest{Body: body}
	req.Model, _ = body["model"].(string)
	if action != "" {
		req.Model, _, _ = strings.Cut(action, ":")
	}
	if req.Model == "" {
		req.Model = "claude-sonnet-4-5"
	}
	req.Prompt = contentText(body["input"])
	for _, key := range []string{"messages", "contents"} {
		messages, _ := body[key].([]any)
		for i := len(messages) - 1; i >= 0; i-- {
			message, _ := messages[i].(map[string]any)
			role, _ := message["role"].(string)
			if role == "assistant" || role == "model" || role == "system" {
				continue
			}
			if key == "contents" {
				req.Prompt = contentText(message["parts"])
			} else {
				req.Prompt = contentText(message["content"])
			}
			break
		}
	}
	if input, ok := body["input"].([]any); ok {
		for i := len(input) - 1; i >= 0; i-- {
			message, _ := input[i].(map[string]any)
			if message["role"] == "assistant" || message["type"] == "function_call" {
				continue
			}
			if text := contentText(message); text != "" {
				req.Prompt = text
				break
			}
		}
	}
	return req
}

// contentText 识别文本块与工具结果，不把图片数据或工具参数冒充用户题面。
func contentText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			if text := contentText(block); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "parts", "output"} {
			if text := contentText(v[key]); text != "" {
				return text
			}
		}
		if result, ok := v["functionResponse"].(map[string]any); ok {
			raw, err := json.Marshal(result["response"])
			if err == nil {
				return string(raw)
			}
		}
	}
	return ""
}

func estimateTokens(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

func estimateGatewayUsage(req GatewayRequest, response GatewayResponse) GatewayUsage {
	input := 0
	for _, key := range []string{"system", "instructions", "messages", "input", "contents", "systemInstruction", "tools"} {
		if value, ok := req.Body[key]; ok {
			raw, err := json.Marshal(value)
			if err == nil {
				input += estimateTokens(string(raw))
			}
		}
	}
	output := estimateTokens(response.Text)
	if len(response.ToolCalls) > 0 {
		raw, err := json.Marshal(response.ToolCalls)
		if err == nil {
			output += estimateTokens(string(raw))
		}
	}
	return GatewayUsage{InputTokens: input, OutputTokens: output}
}

func (s *Server) gatewayResponse(ctx context.Context, accountID int64, req GatewayRequest) (GatewayResponse, bool) {
	s.mu.Lock()
	cfg := s.gatewayConfig[accountID]
	answer, fullAnswer := s.answer, s.gatewayAnswer
	s.mu.Unlock()
	if cfg.Delay > 0 {
		timer := time.NewTimer(cfg.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return GatewayResponse{}, false
		case <-timer.C:
		}
	}
	response := GatewayResponse{Text: "ok"}
	if fullAnswer != nil || answer != nil {
		answered := make(chan GatewayResponse, 1)
		go func() {
			if fullAnswer != nil {
				answered <- fullAnswer(accountID, req)
			} else {
				answered <- GatewayResponse{Text: answer(accountID, req.Prompt)}
			}
		}()
		select {
		case <-ctx.Done():
			return GatewayResponse{}, false
		case response = <-answered:
		}
	}
	if response.StopReason == "" {
		response.StopReason = cfg.StopReason
	}
	if response.ToolCalls == nil {
		response.ToolCalls = cloneCalls(cfg.ToolCalls)
	}
	if response.Usage == nil {
		if cfg.Usage != nil {
			usage := *cfg.Usage
			response.Usage = &usage
		}
	}
	if response.Usage == nil {
		usage := estimateGatewayUsage(req, response)
		response.Usage = &usage
	}
	return response, ctx.Err() == nil
}

func callID(call GatewayToolCall, index int) string {
	if call.ID != "" {
		return call.ID
	}
	return fmt.Sprintf("call_%d", index)
}

// responseBody 按协议构造非流式响应；探测在拿不到 SSE 时照常解析 JSON。
func responseBody(path, rid, model string, response GatewayResponse) map[string]any {
	usage := *response.Usage
	stop := response.StopReason
	if strings.HasPrefix(path, "/v1beta/") {
		if stop == "" {
			stop = "STOP"
		}
		parts := []map[string]any{}
		if response.Text != "" {
			parts = append(parts, map[string]any{"text": response.Text})
		}
		for _, call := range response.ToolCalls {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"id": call.ID, "name": call.Name, "args": call.Arguments}})
		}
		return map[string]any{
			"modelVersion": model,
			"candidates":   []map[string]any{{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": stop}},
			"usageMetadata": map[string]int{"promptTokenCount": usage.InputTokens, "candidatesTokenCount": usage.OutputTokens,
				"thoughtsTokenCount": usage.ReasoningTokens, "cachedContentTokenCount": usage.CacheReadInputTokens,
				"totalTokenCount": usage.InputTokens + usage.OutputTokens + usage.ReasoningTokens},
		}
	}
	switch path {
	case "/v1/chat/completions":
		if stop == "" {
			stop = "stop"
			if len(response.ToolCalls) > 0 {
				stop = "tool_calls"
			}
		}
		message := map[string]any{"role": "assistant", "content": response.Text}
		if len(response.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(response.ToolCalls))
			for i, call := range response.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				calls = append(calls, map[string]any{"id": callID(call, i), "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(args)}})
			}
			message["tool_calls"] = calls
		}
		return map[string]any{
			"id": "chatcmpl-" + rid, "object": "chat.completion", "model": model,
			"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": stop}},
			"usage": map[string]any{"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens,
				"total_tokens":              usage.InputTokens + usage.OutputTokens,
				"prompt_tokens_details":     map[string]int{"cached_tokens": usage.CacheReadInputTokens},
				"completion_tokens_details": map[string]int{"reasoning_tokens": usage.ReasoningTokens}},
		}
	case "/v1/responses":
		output := []map[string]any{}
		if response.Text != "" {
			output = append(output, map[string]any{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": response.Text}}})
		}
		for i, call := range response.ToolCalls {
			args, _ := json.Marshal(call.Arguments)
			output = append(output, map[string]any{"type": "function_call", "call_id": callID(call, i), "name": call.Name, "arguments": string(args)})
		}
		body := map[string]any{
			"id": "resp_" + rid, "object": "response", "model": model, "status": "completed", "output": output,
			"usage": map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
				"total_tokens":          usage.InputTokens + usage.OutputTokens,
				"input_tokens_details":  map[string]int{"cached_tokens": usage.CacheReadInputTokens},
				"output_tokens_details": map[string]int{"reasoning_tokens": usage.ReasoningTokens}},
		}
		if stop != "" && stop != "completed" {
			body["status"] = "incomplete"
			body["incomplete_details"] = map[string]string{"reason": stop}
		}
		return body
	default:
		if stop == "" {
			stop = "end_turn"
			if len(response.ToolCalls) > 0 {
				stop = "tool_use"
			}
		}
		content := []map[string]any{}
		if response.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": response.Text})
		}
		for i, call := range response.ToolCalls {
			content = append(content, map[string]any{"type": "tool_use", "id": callID(call, i), "name": call.Name, "input": call.Arguments})
		}
		return map[string]any{
			"id": "msg_" + rid, "type": "message", "role": "assistant", "model": model, "stop_reason": stop,
			"content": content,
			"usage": map[string]int{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
				"reasoning_tokens": usage.ReasoningTokens, "cache_read_input_tokens": usage.CacheReadInputTokens,
				"cache_creation_input_tokens": usage.CacheCreationInputTokens},
		}
	}
}
