// Package adminclient 是 sub2api Admin API 的客户端（x-api-key 认证）。
//
// 只覆盖本系统用到的接口。字段名对照 sub2api 0.2.14 的 handler 定义，
// 依赖关系登记在 contract/contract.yaml 的 admin_api 中，由 s2a-compat 校验。
package adminclient

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/yx-token/idlicat/market/internal/sub2api/apiwire"
)

type Client struct {
	wire            *apiwire.Client
	diagnosticsWire *apiwire.Client
	apiKey          string
	writeGate       func(context.Context) error
}

type Option func(*options)

type options struct {
	httpClient *http.Client
	writeGate  func(context.Context) error
}

// WithHTTPClient 替换默认的 http.Client（测试或自定义传输层时使用）。
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) { o.httpClient = hc }
}

// ErrWritesPaused 表示写入被门控拦截（sub2api 兼容自检未通过），稍后重试即可。
var ErrWritesPaused = errors.New("sub2api writes paused")

// WithWriteGate 在每个非 GET 请求前调用 gate，返回错误则不发送。
// 用于在兼容自检未通过（安全模式）时阻止同步写入 sub2api，与出站队列的门控保持一致。
func WithWriteGate(gate func(context.Context) error) Option {
	return func(o *options) { o.writeGate = gate }
}

func New(baseURL, apiKey string, timeout time.Duration, opts ...Option) *Client {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	diagnosticsHTTP := &http.Client{}
	if o.httpClient != nil {
		*diagnosticsHTTP = *o.httpClient
	}
	diagnosticsHTTP.Timeout = 3 * time.Minute
	return &Client{wire: apiwire.New(baseURL, timeout, o.httpClient),
		diagnosticsWire: apiwire.New(baseURL, 3*time.Minute, diagnosticsHTTP), apiKey: apiKey, writeGate: o.writeGate}
}

// APIError 与 IsRetryable 透出公共传输层的定义，方便调用方只依赖本包。
type APIError = apiwire.APIError

var IsRetryable = apiwire.IsRetryable

// Retryable 在 sub2api 错误分类之外，把写入门控拦截也视为可重试。
func Retryable(err error) bool {
	return errors.Is(err, ErrWritesPaused) || apiwire.IsRetryable(err)
}

// StatusOf 返回错误中的 HTTP 状态码，非 sub2api 错误返回 0。
var StatusOf = apiwire.StatusOf

func (c *Client) do(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	if method != http.MethodGet && c.writeGate != nil {
		if err := c.writeGate(ctx); err != nil {
			return err
		}
	}
	h := http.Header{}
	h.Set("x-api-key", c.apiKey)
	if idempotencyKey != "" {
		h.Set("Idempotency-Key", idempotencyKey)
	}
	return c.wire.Do(ctx, method, path, h, body, out)
}
