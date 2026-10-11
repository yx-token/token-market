package quality

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

type anthropicProtocol struct{}
type responsesProtocol struct{}
type chatProtocol struct{}
type geminiProtocol struct{}

func (anthropicProtocol) authHeader(h http.Header, key string) {
	h.Set("x-api-key", key)
	h.Set("anthropic-version", "2023-06-01")
}
func (responsesProtocol) authHeader(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) }
func (chatProtocol) authHeader(h http.Header, key string)      { h.Set("Authorization", "Bearer "+key) }
func (geminiProtocol) authHeader(h http.Header, key string)    { h.Set("x-goog-api-key", key) }

var probeExtraFields = map[string]bool{
	"temperature": true, "top_p": true, "top_k": true,
	"thinking": true, "output_config": true, "reasoning": true, "reasoning_effort": true,
	"tool_choice": true, "response_format": true, "text": true,
	"generationConfig": true, "cache_control": true, "context_management": true,
}

func validateProbeRequest(platform string, req ProbeRequest) error {
	if strings.TrimSpace(req.Model) == "" || strings.ContainsAny(req.Model, "/?#\r\n") {
		return fmt.Errorf("探测型号无效")
	}
	if req.MaxTokens < 0 {
		return fmt.Errorf("输出 token 上限不能为负数")
	}
	for k := range req.Extra {
		if !probeExtraFields[k] {
			return fmt.Errorf("探测额外参数不在白名单内")
		}
	}
	if len(req.Attachments) > 8 {
		return fmt.Errorf("探测附件数量超过上限")
	}
	seen, total := map[string]bool{}, 0
	for _, a := range req.Attachments {
		if a.ID == "" || seen[a.ID] || a.Encoding != "base64" {
			return fmt.Errorf("附件编号或编码无效")
		}
		seen[a.ID] = true
		switch a.MediaType {
		case "image/png", "image/jpeg", "image/webp", "image/gif", "application/pdf":
		default:
			return fmt.Errorf("不支持该附件类型")
		}
		if len(a.Data) > 12<<20 {
			return fmt.Errorf("探测附件超过大小上限")
		}
		decoded, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil || len(decoded) == 0 || len(decoded) > 8<<20 {
			return fmt.Errorf("探测附件编码或大小无效")
		}
		total += len(decoded)
	}
	if total > 12<<20 {
		return fmt.Errorf("探测附件总量超过上限")
	}
	for _, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
			return fmt.Errorf("探测消息角色无效")
		}
		for _, c := range m.Content {
			switch c.Type {
			case "text":
			case "image", "document":
				if !seen[c.AttachmentID] {
					return fmt.Errorf("探测消息引用了不存在的附件")
				}
				a := attachmentFor(req, c.AttachmentID)
				if (c.Type == "document") != (a.MediaType == "application/pdf") {
					return fmt.Errorf("附件与内容块类型不一致")
				}
				if _, chat := protocolForModel(platform, req.Model).(chatProtocol); chat && c.Type == "document" {
					return fmt.Errorf("当前聊天协议不支持文档附件")
				}
			case "tool_use":
				if c.ID == "" || c.Name == "" || c.Input == nil {
					return fmt.Errorf("工具调用内容块不完整")
				}
			case "tool_result":
				if c.ToolUseID == "" {
					return fmt.Errorf("工具结果缺少调用编号")
				}
			default:
				return fmt.Errorf("探测内容块类型无效")
			}
		}
	}
	for _, tool := range req.Tools {
		if tool.Name == "" || tool.InputSchema == nil {
			return fmt.Errorf("本地工具定义不完整")
		}
	}
	return nil
}

func requestMessages(req ProbeRequest) []ProbeMessage {
	if len(req.Messages) > 0 {
		return req.Messages
	}
	return []ProbeMessage{{Role: "user", Content: []ProbeContent{{Type: "text", Text: req.Prompt}}}}
}

func attachmentFor(req ProbeRequest, id string) ProbeAttachment {
	for _, a := range req.Attachments {
		if a.ID == id {
			return a
		}
	}
	return ProbeAttachment{}
}

func applyExtra(body map[string]any, req ProbeRequest) {
	for k, v := range req.Extra {
		if probeExtraFields[k] && (k != "tool_choice" || req.Expected.Outcome == "reject") {
			body[k] = v
		}
	}
}

func claudeAdaptive(model string) bool {
	m := strings.ReplaceAll(strings.ToLower(model), ".", "-")
	return claudeAlwaysThinking(m) || strings.Contains(m, "opus-4-7") || strings.Contains(m, "opus-4-8") ||
		strings.Contains(m, "opus-4-6") || strings.Contains(m, "sonnet-4-6") || strings.Contains(m, "mythos-preview")
}

func claudeAlwaysThinking(model string) bool {
	parts := strings.Split(strings.ReplaceAll(strings.ToLower(model), ".", "-"), "-")
	return len(parts) > 2 && parts[0] == "claude" && parts[2] == "5"
}

func (anthropicProtocol) build(req ProbeRequest) (string, any, bool) {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	for _, m := range requestMessages(req) {
		content := make([]map[string]any, 0, len(m.Content))
		for _, c := range m.Content {
			switch c.Type {
			case "image", "document":
				a := attachmentFor(req, c.AttachmentID)
				content = append(content, map[string]any{"type": c.Type, "source": map[string]any{"type": "base64", "media_type": a.MediaType, "data": a.Data}})
			case "tool_use":
				content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Input})
			case "tool_result":
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": c.ToolUseID, "content": c.Content})
			default:
				content = append(content, map[string]any{"type": "text", "text": c.Text})
			}
		}
		role := m.Role
		if role == "tool" {
			role = "user"
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}
	body := map[string]any{"model": req.Model, "max_tokens": req.MaxTokens, "stream": true, "messages": messages}
	if req.System != "" {
		body["system"] = req.System
	}
	if effort := req.effectiveEffort(); effort != "" {
		if claudeAdaptive(req.Model) {
			body["thinking"] = map[string]any{"type": "adaptive"}
			body["output_config"] = map[string]any{"effort": effort}
		} else {
			budget := 2048
			if req.MaxTokens <= budget {
				body["max_tokens"] = budget * 2
			}
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
	}
	if len(req.Tools) > 0 {
		body["tools"], body["tool_choice"] = req.Tools, map[string]any{"type": "auto"}
	}
	applyExtra(body, req)
	return "/v1/messages", body, true
}

func (responsesProtocol) build(req ProbeRequest) (string, any, bool) {
	body := map[string]any{"model": req.Model, "max_output_tokens": req.MaxTokens, "stream": true}
	if len(req.Messages) == 0 && len(req.Attachments) == 0 {
		prompt := req.Prompt
		if req.System != "" {
			prompt = req.System + "\n\n" + prompt
		}
		body["input"] = prompt
	} else {
		var input []map[string]any
		system := req.System
		for _, m := range requestMessages(req) {
			var content []map[string]any
			if system != "" && m.Role == "user" {
				content = append(content, map[string]any{"type": "input_text", "text": system + "\n\n"})
				system = ""
			}
			for _, c := range m.Content {
				switch c.Type {
				case "tool_use":
					input = append(input, map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": jsonString(c.Input)})
				case "tool_result":
					input = append(input, map[string]any{"type": "function_call_output", "call_id": c.ToolUseID, "output": contentString(c.Content)})
				case "image":
					a := attachmentFor(req, c.AttachmentID)
					content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + a.MediaType + ";base64," + a.Data})
				case "document":
					a := attachmentFor(req, c.AttachmentID)
					content = append(content, map[string]any{"type": "input_file", "filename": a.ID + ".pdf", "file_data": "data:" + a.MediaType + ";base64," + a.Data})
				default:
					typ := "input_text"
					if m.Role == "assistant" {
						typ = "output_text"
					}
					content = append(content, map[string]any{"type": typ, "text": c.Text})
				}
			}
			if len(content) > 0 {
				input = append(input, map[string]any{"role": m.Role, "content": content})
			}
		}
		if system != "" {
			input = append([]map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": system}}}}, input...)
		}
		body["input"] = input
	}
	// 留空 instructions，让 Codex 网关注入官方提示；题目要求放在首条用户消息。
	if effort := req.effectiveEffort(); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.InputSchema})
		}
		body["tools"], body["tool_choice"] = tools, "auto"
	}
	applyExtra(body, req)
	return "/v1/responses", body, true
}

func (chatProtocol) build(req ProbeRequest) (string, any, bool) {
	var messages []map[string]any
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range requestMessages(req) {
		var content, calls []map[string]any
		var text strings.Builder
		hasImage := false
		for _, c := range m.Content {
			switch c.Type {
			case "tool_use":
				calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": jsonString(c.Input)}})
			case "tool_result":
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": c.ToolUseID, "content": contentString(c.Content)})
			case "image":
				hasImage = true
				a := attachmentFor(req, c.AttachmentID)
				content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + a.MediaType + ";base64," + a.Data}})
			default:
				text.WriteString(c.Text)
				content = append(content, map[string]any{"type": "text", "text": c.Text})
			}
		}
		if len(content) > 0 || len(calls) > 0 {
			// Coding Plan 的纯文本消息沿用字符串，图片消息才使用内容块数组。
			var messageContent any = text.String()
			if hasImage {
				messageContent = content
			}
			msg := map[string]any{"role": m.Role, "content": messageContent}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			messages = append(messages, msg)
		}
	}
	body := map[string]any{"model": req.Model, "max_tokens": req.MaxTokens, "stream": true,
		"stream_options": map[string]any{"include_usage": true}, "messages": messages}
	if effort := req.effectiveEffort(); effort != "" {
		body["reasoning_effort"] = effort
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
		}
		body["tools"], body["tool_choice"] = tools, "auto"
	}
	applyExtra(body, req)
	return "/v1/chat/completions", body, true
}

func (geminiProtocol) build(req ProbeRequest) (string, any, bool) {
	cfg := map[string]any{"maxOutputTokens": req.MaxTokens}
	if effort := req.effectiveEffort(); effort != "" {
		thinking := map[string]any{"includeThoughts": false}
		if strings.HasPrefix(req.Model, "gemini-3") {
			thinking["thinkingLevel"] = effort
		} else {
			thinking["thinkingBudget"] = 2048
		}
		cfg["thinkingConfig"] = thinking
	}
	var contents []map[string]any
	for _, m := range requestMessages(req) {
		var parts []map[string]any
		for _, c := range m.Content {
			switch c.Type {
			case "image", "document":
				a := attachmentFor(req, c.AttachmentID)
				parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": a.MediaType, "data": a.Data}})
			case "tool_use":
				parts = append(parts, map[string]any{"functionCall": map[string]any{"id": c.ID, "name": c.Name, "args": c.Input}})
			case "tool_result":
				parts = append(parts, map[string]any{"functionResponse": map[string]any{"id": c.ToolUseID, "name": c.Name, "response": c.Content}})
			default:
				parts = append(parts, map[string]any{"text": c.Text})
			}
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	body := map[string]any{"contents": contents, "generationConfig": cfg}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": req.System}}}
	}
	if len(req.Tools) > 0 {
		var declarations []map[string]any
		for _, t := range req.Tools {
			declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": declarations}}
		body["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
	}
	applyExtra(body, req)
	return "/v1beta/models/" + req.Model + ":generateContent", body, false
}
