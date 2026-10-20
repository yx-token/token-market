package quality

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	probeErrorCodeRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	probeIPv4RE      = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	probeIPv6RE      = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{0,4}:){2,}[0-9a-f:]*\b`)
	probeURLRE       = regexp.MustCompile(`(?i)https?://[^\s"<>]+`)
	probeSecretRE    = regexp.MustCompile(`(?i)(?:bearer\s+|(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password)\s*[:=]\s*)[^\s,;"<>]+|\b(?:sk-|sess-)[A-Za-z0-9_-]+`)
)

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds >= 0 {
		if seconds > 3600 {
			seconds = 3600
		}
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		d := deadline.Sub(now)
		if d > time.Hour {
			d = time.Hour
		}
		return d
	}
	return 0
}

func probeErrorObject(m map[string]any) map[string]any {
	e := object(m["error"])
	if e == nil {
		e = object(object(m["response"])["error"])
	}
	if e == nil {
		e = m
	}
	return e
}

func errorDetails(raw []byte) (code, kind, message string) {
	m, err := decodeObject(raw)
	if err != nil {
		return "", "", "上游返回非结构化错误响应"
	}
	e := probeErrorObject(m)
	code, kind, message = str(e["code"]), str(e["type"]), str(e["message"])
	if code == "" {
		if n, ok := e["code"].(json.Number); ok {
			code = string(n)
		} else if n, ok := e["code"].(float64); ok {
			code = strconv.FormatInt(int64(n), 10)
		}
	}
	if kind == "" {
		kind = str(e["status"])
	}
	if code == "" {
		code = kind
	}
	if !probeErrorCodeRE.MatchString(code) {
		code = ""
	}
	if !probeErrorCodeRE.MatchString(kind) {
		kind = ""
	}
	if message == "" {
		message = str(m["error"])
	}
	return code, kind, message
}

func isProbePlatformBalance(status int, raw []byte) bool {
	if status != http.StatusForbidden {
		return false
	}
	m, err := decodeObject(raw)
	if err != nil {
		return false
	}
	// 不使用 errorDetails 的 type/status 回退，平台余额只认原始 code 字段。
	code, ok := probeErrorObject(m)["code"].(string)
	return ok && code == "INSUFFICIENT_BALANCE"
}

func classifyProbeError(res *ProbeResult, raw []byte, req ProbeRequest, key string) {
	code, kind, message := errorDetails(raw)
	res.ErrorCode = code
	lower := strings.ToLower(code + " " + kind + " " + message)
	// 网关业务码先于 HTTP 429 和预期拒绝，避免把本地限制当成官方指纹。
	switch {
	case strings.Contains(lower, "gateway_concurrency_limit") || strings.Contains(lower, "gateway_queue_full") ||
		strings.Contains(lower, "claude_code_version_too_old") || isProbePlatformBalance(res.HTTPStatus, raw):
		res.ErrorClass = "platform"
		res.addFlag("platform")
	case res.HTTPStatus == 429 || res.HTTPStatus == 529 || kind == "rate_limit_error":
		res.ErrorClass = "rate_limited"
		res.addFlag("rate_limited")
	case expectedRejection(req.Expected, res.HTTPStatus, code):
		res.ErrorClass = "expected_reject"
		res.addFlag("expected_reject")
	case res.HTTPStatus >= 400 && res.HTTPStatus < 500 && containsProbeBalanceError(lower):
		// 厂商也可能使用 invalid_request_error 包装余额不足，仍是卖家失败观测。
		res.ErrorClass = "http_4xx"
	case res.HTTPStatus == 400 && (kind == "invalid_request_error" || kind == "INVALID_ARGUMENT" || code == "invalid_request_error" || code == "unsupported_parameter"):
		res.ErrorClass = "configuration"
		res.addFlag("configuration")
	case res.HTTPStatus >= 500:
		res.ErrorClass = "http_5xx"
	case res.HTTPStatus >= 300:
		res.ErrorClass = "http_4xx"
	default:
		res.ErrorClass = "parse"
	}
	res.ErrorBody = sanitizeProbeError(code, kind, message, req, key)
}

func containsProbeBalanceError(details string) bool {
	for _, marker := range []string{"insufficient_balance", "balance_insufficient", "balance_not_enough", "balance insufficient", "insufficient balance", "insufficient credit balance", "余额不足"} {
		if strings.Contains(details, marker) {
			return true
		}
	}
	return false
}

func expectedRejection(expected ProbeExpected, status int, code string) bool {
	if expected.Outcome != "reject" || status < 400 || status >= 500 {
		return false
	}
	matched := false
	for _, allowed := range expected.HTTPStatus {
		matched = matched || status == allowed
	}
	if !matched {
		return false
	}
	if len(expected.ErrorCodes) == 0 {
		return true
	}
	for _, allowed := range expected.ErrorCodes {
		if code == allowed {
			return true
		}
	}
	return false
}

func sanitizeProbeError(code, kind, message string, req ProbeRequest, key string) string {
	secrets := []string{req.Prompt, req.System}
	if len(key) >= 8 {
		secrets = append(secrets, key)
	}
	for _, msg := range req.Messages {
		for _, c := range msg.Content {
			secrets = append(secrets, c.Text)
		}
	}
	for _, a := range req.Attachments {
		secrets = append(secrets, a.Data)
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[已脱敏]")
		}
	}
	message = probeSecretRE.ReplaceAllString(message, "[已脱敏]")
	message = probeURLRE.ReplaceAllString(message, "[地址已脱敏]")
	message = probeIPv4RE.ReplaceAllString(message, "[地址已脱敏]")
	message = probeIPv6RE.ReplaceAllString(message, "[地址已脱敏]")
	message = strings.ToValidUTF8(strings.ReplaceAll(message, "\x00", ""), "")
	if len(message) > 4096 {
		message = message[:4096]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	// 仅保留白名单字段，避免保存题面、请求头与凭据对象。
	for len(message) > 0 {
		body, _ := json.Marshal(map[string]string{"code": code, "type": kind, "message": message})
		if len(body) <= 4096 {
			return string(body)
		}
		_, size := utf8.DecodeLastRuneInString(message)
		message = message[:len(message)-size]
	}
	body, _ := json.Marshal(map[string]string{"code": code, "type": kind})
	return string(body)
}

func normalizeStop(res *ProbeResult) {
	if res.HasFlag("refusal") {
		return
	}
	switch strings.ToLower(res.StopReason) {
	case "end_turn", "stop_sequence", "tool_use", "stop", "tool_calls", "function_call", "completed":
		res.addFlag("stop_normal")
	case "max_tokens", "max_output_tokens", "length", "incomplete", "model_context_window_exceeded":
		res.addFlag("truncated")
	case "refusal", "content_filter", "safety", "recitation", "blocklist", "prohibited_content", "spii", "image_safety", "reasoning_extraction":
		res.addFlag("refusal")
	case "error", "failed", "malformed_function_call", "unexpected_tool_call":
		res.addFlag("stop_error")
	default:
		res.addFlag("unknown_stop_reason")
	}
}

func estimateClaudeReasoning(req ProbeRequest, res *ProbeResult) {
	if res.ReasoningObserved || res.OutputTokens <= 0 || !strings.HasPrefix(req.Model, "claude-") {
		return
	}
	if thinking := object(req.Extra["thinking"]); str(thinking["type"]) == "disabled" {
		return
	}
	if req.effectiveEffort() == "" && !claudeAlwaysThinking(req.Model) {
		return
	}
	// 这里只估算可见文本 token，不使用省略或摘要后的 thinking 字符数。
	visible := (utf8.RuneCountInString(res.Text) + 3) / 4
	res.ReasoningTokens = max(0, res.OutputTokens-visible)
	res.ReasoningObserved = true
	res.addFlag("reasoning_estimated")
}
