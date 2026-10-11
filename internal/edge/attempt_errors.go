package edge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

const attemptErrorBodyLimit = 64 << 10

// 错误正文只在透传期间暂存有限前缀，用于分类后立即丢弃，不进入尝试日志。
type attemptErrorCapture struct{ data []byte }

func (c *attemptErrorCapture) Write(p []byte) (int, error) {
	remaining := attemptErrorBodyLimit - len(c.data)
	if remaining > 0 {
		c.data = append(c.data, p[:min(len(p), remaining)]...)
	}
	return len(p), nil
}

type attemptCapturedBody struct {
	io.ReadCloser
	capture *attemptErrorCapture
}

func (b *attemptCapturedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.capture.Write(p[:n])
	}
	return n, err
}

func attemptNetworkClass(err error) string {
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return "timeout"
	}
	return "network"
}

// 本地并发、版本与余额错误优先识别，避免把平台问题归到卖家的生产可用率。
func attemptHTTPClass(status int, raw []byte) string {
	if status < http.StatusBadRequest {
		return ""
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	var fields []string
	var platformBalance bool
	var readError func(any, int)
	readError = func(value any, depth int) {
		if depth > 8 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			for _, key := range []string{"code", "type", "status", "errorKey", "message", "errorMessage"} {
				if s, ok := node[key].(string); ok {
					fields = append(fields, strings.ToLower(s))
					if key == "code" && s == "INSUFFICIENT_BALANCE" {
						platformBalance = true
					}
				}
			}
			for _, key := range []string{"error", "cause", "details"} {
				readError(node[key], depth+1)
			}
		case []any:
			for _, entry := range node {
				readError(entry, depth+1)
			}
		case string:
			fields = append(fields, strings.ToLower(node))
		}
	}
	readError(payload, 0)
	details := strings.Join(fields, " ")
	for _, marker := range []string{"gateway_concurrency_limit", "gateway_queue_full", "claude_code_version_too_old"} {
		if strings.Contains(details, marker) {
			return "platform"
		}
	}
	// 平台余额码大小写与状态必须同时匹配，厂商余额文案仍按原状态归类。
	if status == http.StatusForbidden && platformBalance {
		return "platform"
	}
	if status == http.StatusTooManyRequests || status == 529 {
		return "rate_limited"
	}
	// 明确的厂商余额不足仍是卖家失败观测，不能落到 unknown 后被排除。
	if status < http.StatusInternalServerError {
		for _, marker := range []string{"insufficient_balance", "balance_insufficient", "balance_not_enough", "balance insufficient", "insufficient balance", "insufficient credit balance", "余额不足"} {
			if strings.Contains(details, marker) {
				return "http_4xx"
			}
		}
	}
	if strings.Contains(details, "invalid_request") || strings.Contains(details, "unsupported_parameter") || strings.Contains(details, "invalid_argument") {
		return "configuration"
	}
	if status >= http.StatusInternalServerError {
		return "http_5xx"
	}
	return "unknown"
}
