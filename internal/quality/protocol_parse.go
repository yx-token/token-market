package quality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return jsonString(v)
}
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
func list(v any) []any {
	a, _ := v.([]any)
	return a
}
func str(v any) string { s, _ := v.(string); return s }
func count(m map[string]any, key string) *int64 {
	var n int64
	switch v := m[key].(type) {
	case float64:
		if v < 0 || v != float64(int64(v)) {
			return nil
		}
		n = int64(v)
	case json.Number:
		var err error
		n, err = v.Int64()
		if err != nil || n < 0 {
			return nil
		}
	default:
		return nil
	}
	return &n
}
func decodeObject(b []byte) (map[string]any, error) {
	var m map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err := decoder.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("协议响应不是对象")
	}
	return m, nil
}
func setReasoning(res *ProbeResult, n *int64) {
	if n != nil {
		res.ReasoningTokens, res.ReasoningObserved = int(*n), true
	}
}
func anthropicUsage(u map[string]any, res *ProbeResult) {
	if n := count(u, "input_tokens"); n != nil {
		res.InputTokens = n
	}
	if n := count(u, "cache_read_input_tokens"); n != nil {
		res.CacheReadTokens = n
	}
	if n := count(u, "cache_creation_input_tokens"); n != nil {
		res.CacheCreationTokens = n
	}
	if n := count(u, "output_tokens"); n != nil {
		res.OutputTokens = int(*n)
		res.OutputObserved = true
	}
	for _, key := range []string{"reasoning_tokens", "thinking_tokens"} {
		if n := count(object(u["output_tokens_details"]), key); n != nil {
			setReasoning(res, n)
			return
		}
		if n := count(u, key); n != nil {
			setReasoning(res, n)
			return
		}
	}
}
func responsesUsage(u map[string]any, res *ProbeResult) {
	if n := count(u, "input_tokens"); n != nil {
		res.InputTokens = n
	}
	if n := count(object(u["input_tokens_details"]), "cached_tokens"); n != nil {
		res.CacheReadTokens = n
	}
	if n := count(u, "output_tokens"); n != nil {
		res.OutputTokens = int(*n)
		res.OutputObserved = true
	}
	setReasoning(res, count(object(u["output_tokens_details"]), "reasoning_tokens"))
}
func chatUsage(u map[string]any, res *ProbeResult) {
	if n := count(u, "prompt_tokens"); n != nil {
		res.InputTokens = n
	}
	if n := count(object(u["prompt_tokens_details"]), "cached_tokens"); n != nil {
		res.CacheReadTokens = n
	}
	if n := count(u, "completion_tokens"); n != nil {
		res.OutputTokens = int(*n)
		res.OutputObserved = true
	}
	setReasoning(res, count(object(u["completion_tokens_details"]), "reasoning_tokens"))
	if !res.ReasoningObserved {
		setReasoning(res, count(u, "reasoning_tokens"))
	}
}

type streamedTool struct {
	ID, Name, Arguments string
	Input               map[string]any
}

func appendTools(tools map[int]*streamedTool, res *ProbeResult) error {
	keys := make([]int, 0, len(tools))
	for i := range tools {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	for _, i := range keys {
		t := tools[i]
		args := t.Input
		if t.Arguments != "" {
			var err error
			args, err = parseToolArguments(t.Arguments)
			if err != nil || args == nil {
				return fmt.Errorf("工具调用参数不完整")
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		if t.Name == "" {
			return fmt.Errorf("工具调用缺少名称")
		}
		res.ToolCalls = append(res.ToolCalls, ProbeToolCall{ID: t.ID, Name: t.Name, Arguments: args})
	}
	return nil
}
func parseToolArguments(v any) (map[string]any, error) {
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	if s, ok := v.(string); ok {
		var m map[string]any
		decoder := json.NewDecoder(strings.NewReader(s))
		decoder.UseNumber()
		if err := decoder.Decode(&m); err == nil && m != nil {
			return m, nil
		}
	}
	return nil, fmt.Errorf("工具调用参数无效")
}
func streamFailure(ev map[string]any, res *ProbeResult) error {
	res.ErrorBody = jsonString(ev)
	return fmt.Errorf("协议流返回错误事件")
}

func (anthropicProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	tools := map[int]*streamedTool{}
	complete := false
	err := sseEvents(r, func(event string, data []byte) error {
		if event == "done" {
			return nil
		}
		ev, err := decodeObject(data)
		if err != nil {
			return err
		}
		switch str(ev["type"]) {
		case "message_start":
			msg := object(ev["message"])
			res.ResponseModel = str(msg["model"])
			anthropicUsage(object(msg["usage"]), res)
		case "content_block_start":
			block := object(ev["content_block"])
			i := 0
			if n := count(ev, "index"); n != nil {
				i = int(*n)
			}
			switch str(block["type"]) {
			case "tool_use":
				markTTFT(res, start)
				tools[i] = &streamedTool{ID: str(block["id"]), Name: str(block["name"]), Input: object(block["input"])}
			case "text":
				if s := str(block["text"]); s != "" {
					markTTFT(res, start)
					text.WriteString(s)
				}
			case "refusal":
				res.addFlag("refusal")
			}
		case "content_block_delta":
			delta := object(ev["delta"])
			if str(delta["text"]) != "" || str(delta["thinking"]) != "" {
				markTTFT(res, start)
			}
			text.WriteString(str(delta["text"]))
			res.ThinkingChars += len(str(delta["thinking"]))
			if str(delta["type"]) == "input_json_delta" {
				i := 0
				if n := count(ev, "index"); n != nil {
					i = int(*n)
				}
				if t := tools[i]; t != nil {
					t.Arguments += str(delta["partial_json"])
				} else {
					return fmt.Errorf("工具参数缺少起始内容块")
				}
			}
		case "message_delta":
			res.StopReason = str(object(ev["delta"])["stop_reason"])
			anthropicUsage(object(ev["usage"]), res)
		case "message_stop":
			complete = true
		case "error":
			return streamFailure(ev, res)
		}
		return nil
	})
	res.Text = text.String()
	if err != nil {
		return err
	}
	if err := requireComplete(complete); err != nil {
		return err
	}
	return appendTools(tools, res)
}

func (anthropicProtocol) parseJSON(b []byte, res *ProbeResult) error {
	msg, err := decodeObject(b)
	if err != nil {
		return err
	}
	if msg["error"] != nil {
		return streamFailure(msg, res)
	}
	res.ResponseModel, res.StopReason = str(msg["model"]), str(msg["stop_reason"])
	anthropicUsage(object(msg["usage"]), res)
	var text strings.Builder
	for _, raw := range list(msg["content"]) {
		c := object(raw)
		switch str(c["type"]) {
		case "text":
			text.WriteString(str(c["text"]))
		case "thinking":
			res.ThinkingChars += len(str(c["thinking"]))
		case "refusal":
			res.addFlag("refusal")
		case "tool_use":
			args, err := parseToolArguments(c["input"])
			if err != nil {
				return err
			}
			res.ToolCalls = append(res.ToolCalls, ProbeToolCall{ID: str(c["id"]), Name: str(c["name"]), Arguments: args})
		}
	}
	res.Text = text.String()
	return nil
}

func parseResponsesObject(resp map[string]any, res *ProbeResult) error {
	if model := str(resp["model"]); model != "" {
		res.ResponseModel = model
	}
	responsesUsage(object(resp["usage"]), res)
	res.StopReason = str(resp["status"])
	if reason := str(object(resp["incomplete_details"])["reason"]); reason != "" {
		res.StopReason = reason
	}
	if resp["error"] != nil {
		return streamFailure(resp, res)
	}
	var text strings.Builder
	var calls []ProbeToolCall
	for _, raw := range list(resp["output"]) {
		o := object(raw)
		if str(o["type"]) == "function_call" {
			args, err := parseToolArguments(o["arguments"])
			if err != nil {
				return err
			}
			calls = append(calls, ProbeToolCall{ID: str(o["call_id"]), Name: str(o["name"]), Arguments: args})
		}
		for _, rawContent := range list(o["content"]) {
			c := object(rawContent)
			if str(c["type"]) == "output_text" {
				text.WriteString(str(c["text"]))
			} else if str(c["type"]) == "refusal" {
				res.addFlag("refusal")
			}
		}
	}
	if text.Len() > 0 {
		res.Text = text.String()
	}
	if len(calls) > 0 {
		res.ToolCalls = calls
	}
	return nil
}

func (responsesProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	tools := map[int]*streamedTool{}
	complete := false
	err := sseEvents(r, func(event string, data []byte) error {
		if event == "done" {
			return nil
		}
		ev, err := decodeObject(data)
		if err != nil {
			return err
		}
		i := 0
		if n := count(ev, "output_index"); n != nil {
			i = int(*n)
		}
		switch str(ev["type"]) {
		case "response.output_text.delta":
			markTTFT(res, start)
			text.WriteString(str(ev["delta"]))
		case "response.refusal.delta", "response.refusal.done":
			markTTFT(res, start)
			res.addFlag("refusal")
		case "response.output_item.added":
			item := object(ev["item"])
			if str(item["type"]) == "function_call" {
				markTTFT(res, start)
				tools[i] = &streamedTool{ID: str(item["call_id"]), Name: str(item["name"]), Arguments: str(item["arguments"])}
			}
		case "response.function_call_arguments.delta":
			if t := tools[i]; t != nil {
				t.Arguments += str(ev["delta"])
			} else {
				return fmt.Errorf("工具参数缺少起始输出项")
			}
		case "response.function_call_arguments.done":
			if t := tools[i]; t != nil {
				t.Arguments = str(ev["arguments"])
			}
		case "response.completed", "response.incomplete":
			complete = true
			res.Text = text.String()
			if err := parseResponsesObject(object(ev["response"]), res); err != nil {
				return err
			}
			if res.StopReason == "" {
				res.StopReason = strings.TrimPrefix(str(ev["type"]), "response.")
			}
		case "response.failed", "error":
			return streamFailure(ev, res)
		default:
			if strings.HasPrefix(str(ev["type"]), "response.reasoning") {
				markTTFT(res, start)
			}
		}
		return nil
	})
	if res.Text == "" {
		res.Text = text.String()
	}
	if err != nil {
		return err
	}
	if err := requireComplete(complete); err != nil {
		return err
	}
	if len(res.ToolCalls) == 0 {
		return appendTools(tools, res)
	}
	return nil
}
func (responsesProtocol) parseJSON(b []byte, res *ProbeResult) error {
	resp, err := decodeObject(b)
	if err != nil {
		return err
	}
	return parseResponsesObject(resp, res)
}

func (chatProtocol) parseStream(r io.Reader, start time.Time, res *ProbeResult) error {
	var text strings.Builder
	tools := map[int]*streamedTool{}
	complete := false
	err := sseEvents(r, func(event string, data []byte) error {
		if event == "done" {
			complete = true
			return nil
		}
		ev, err := decodeObject(data)
		if err != nil {
			return err
		}
		if ev["error"] != nil {
			return streamFailure(ev, res)
		}
		if model := str(ev["model"]); model != "" {
			res.ResponseModel = model
		}
		for _, raw := range list(ev["choices"]) {
			c := object(raw)
			if n := count(c, "index"); n != nil && *n != 0 {
				continue
			}
			delta := object(c["delta"])
			if str(delta["content"]) != "" || str(delta["reasoning_content"]) != "" || len(list(delta["tool_calls"])) > 0 {
				markTTFT(res, start)
			}
			text.WriteString(str(delta["content"]))
			res.ThinkingChars += len(str(delta["reasoning_content"]))
			if str(delta["refusal"]) != "" {
				res.addFlag("refusal")
			}
			if stop := str(c["finish_reason"]); stop != "" {
				res.StopReason, complete = stop, true
			}
			for _, rawCall := range list(delta["tool_calls"]) {
				call := object(rawCall)
				i := 0
				if n := count(call, "index"); n != nil {
					i = int(*n)
				}
				t := tools[i]
				if t == nil {
					t = &streamedTool{}
					tools[i] = t
				}
				if id := str(call["id"]); id != "" {
					t.ID = id
				}
				fn := object(call["function"])
				if name := str(fn["name"]); name != "" {
					t.Name = name
				}
				t.Arguments += str(fn["arguments"])
			}
		}
		chatUsage(object(ev["usage"]), res)
		return nil
	})
	res.Text = text.String()
	if err != nil {
		return err
	}
	if err := requireComplete(complete); err != nil {
		return err
	}
	return appendTools(tools, res)
}
func (chatProtocol) parseJSON(b []byte, res *ProbeResult) error {
	resp, err := decodeObject(b)
	if err != nil {
		return err
	}
	if resp["error"] != nil {
		return streamFailure(resp, res)
	}
	res.ResponseModel = str(resp["model"])
	chatUsage(object(resp["usage"]), res)
	choices := list(resp["choices"])
	if len(choices) == 0 {
		return nil
	}
	choice := object(choices[0])
	res.StopReason = str(choice["finish_reason"])
	msg := object(choice["message"])
	res.Text, res.ThinkingChars = str(msg["content"]), len(str(msg["reasoning_content"]))
	if str(msg["refusal"]) != "" {
		res.addFlag("refusal")
	}
	for _, raw := range list(msg["tool_calls"]) {
		call := object(raw)
		fn := object(call["function"])
		args, err := parseToolArguments(fn["arguments"])
		if err != nil {
			return err
		}
		res.ToolCalls = append(res.ToolCalls, ProbeToolCall{ID: str(call["id"]), Name: str(fn["name"]), Arguments: args})
	}
	return nil
}

func (geminiProtocol) parseStream(io.Reader, time.Time, *ProbeResult) error {
	return fmt.Errorf("当前 Gemini 探测使用非流式请求")
}
func (geminiProtocol) parseJSON(b []byte, res *ProbeResult) error {
	resp, err := decodeObject(b)
	if err != nil {
		return err
	}
	if resp["error"] != nil {
		return streamFailure(resp, res)
	}
	res.ResponseModel = str(resp["modelVersion"])
	usage := object(resp["usageMetadata"])
	res.InputTokens, res.CacheReadTokens = count(usage, "promptTokenCount"), count(usage, "cachedContentTokenCount")
	if n := count(usage, "candidatesTokenCount"); n != nil {
		res.OutputTokens = int(*n)
		res.OutputObserved = true
	}
	setReasoning(res, count(usage, "thoughtsTokenCount"))
	if reason := str(object(resp["promptFeedback"])["blockReason"]); reason != "" {
		res.StopReason = reason
		res.addFlag("refusal")
	}
	candidates := list(resp["candidates"])
	if len(candidates) == 0 {
		return nil
	}
	candidate := object(candidates[0])
	res.StopReason = str(candidate["finishReason"])
	var text strings.Builder
	for _, raw := range list(object(candidate["content"])["parts"]) {
		part := object(raw)
		if thought, _ := part["thought"].(bool); !thought {
			text.WriteString(str(part["text"]))
		}
		if call := object(part["functionCall"]); call != nil {
			args, err := parseToolArguments(call["args"])
			if err != nil {
				return err
			}
			res.ToolCalls = append(res.ToolCalls, ProbeToolCall{ID: str(call["id"]), Name: str(call["name"]), Arguments: args})
		}
	}
	res.Text = text.String()
	return nil
}
