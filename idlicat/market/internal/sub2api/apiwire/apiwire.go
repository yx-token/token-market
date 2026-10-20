// Package apiwire 是 sub2api HTTP 接口的公共传输层：统一的响应信封解析与错误分类。
//
// sub2api 的响应格式为 {"code":0,"message":"success","reason":"","data":...}，
// code 非 0 或 HTTP 非 2xx 都视为错误。
package apiwire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxErrorBody = 4 << 10

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, timeout time.Duration, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// APIError 是 sub2api 返回的业务错误或非 2xx 响应。
// sub2api 的 code 字段有两种形态：数字（如 400）或字符串错误码（如 "ADMIN_COMPLIANCE_ACK_REQUIRED"），
// 字符串形态放在 ErrorCode 中。
type APIError struct {
	HTTPStatus int
	Code       int
	ErrorCode  string
	Message    string
	Reason     string
	RetryAfter string
}

func (e *APIError) Error() string {
	code := strconv.Itoa(e.Code)
	if e.ErrorCode != "" {
		code = e.ErrorCode
	}
	if e.Reason != "" {
		return fmt.Sprintf("sub2api: http %d code %s %s: %s", e.HTTPStatus, code, e.Reason, e.Message)
	}
	return fmt.Sprintf("sub2api: http %d code %s: %s", e.HTTPStatus, code, e.Message)
}

// Retryable 判断错误是否值得重试：5xx、408、429，以及带 Retry-After 的 409
// （sub2api 幂等协调器在同一幂等键仍在执行时返回）。
func (e *APIError) Retryable() bool {
	switch {
	case e.HTTPStatus >= 500:
		return true
	case e.HTTPStatus == http.StatusRequestTimeout, e.HTTPStatus == http.StatusTooManyRequests:
		return true
	case e.HTTPStatus == http.StatusConflict && e.RetryAfter != "":
		return true
	}
	return false
}

// IsRetryable 判断任意错误是否可重试。非 APIError（网络错误、超时等）视为可重试，
// 因此写操作必须带幂等键或具备"先查后建"的保护。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return !errors.Is(err, context.Canceled)
}

// StatusOf 返回错误中的 HTTP 状态码，非 APIError 返回 0。
func StatusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatus
	}
	return 0
}

type envelope struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

// parseCode 解析 code 字段：返回数字码、字符串码，以及是否表示成功（数字 0）。
func parseCode(raw json.RawMessage) (num int, str string, ok bool) {
	if len(raw) == 0 {
		return 0, "", false
	}
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, "", num == 0
	}
	if err := json.Unmarshal(raw, &str); err == nil {
		return 0, str, false
	}
	return 0, string(raw), false
}

// Do 发送 JSON 请求并把 data 字段解码到 out（out 可为 nil）。
func (c *Client) Do(ctx context.Context, method, path string, headers http.Header, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	var env envelope
	decodeErr := json.Unmarshal(raw, &env)
	num, str, success := parseCode(env.Code)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || decodeErr != nil || !success {
		apiErr := &APIError{HTTPStatus: resp.StatusCode, Code: num, ErrorCode: str, Message: env.Message, Reason: env.Reason, RetryAfter: resp.Header.Get("Retry-After")}
		if decodeErr != nil {
			apiErr.Message = truncate(string(raw))
		}
		return apiErr
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("%s %s: decode data: %w", method, path, err)
	}
	return nil
}

func truncate(s string) string {
	if len(s) > maxErrorBody {
		return s[:maxErrorBody]
	}
	return s
}
