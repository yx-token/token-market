// Package adminclient 是 sub2api Admin API 的客户端（x-api-key 认证）。
//
// 只覆盖本系统用到的接口。字段名对照 sub2api 0.2.14 的 handler 定义，
// 依赖关系登记在 contract/contract.yaml 的 admin_api 中，由 s2a-compat 校验。
package adminclient

import (
	"context"
	"net/http"
	"time"

	"github.com/DueGin/token-market/internal/sub2api/apiwire"
)

type Client struct {
	wire   *apiwire.Client
	apiKey string
}

type Option func(*options)

type options struct {
	httpClient *http.Client
}

// WithHTTPClient 替换默认的 http.Client（测试或自定义传输层时使用）。
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) { o.httpClient = hc }
}

func New(baseURL, apiKey string, timeout time.Duration, opts ...Option) *Client {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{wire: apiwire.New(baseURL, timeout, o.httpClient), apiKey: apiKey}
}

// APIError 与 IsRetryable 透出公共传输层的定义，方便调用方只依赖本包。
type APIError = apiwire.APIError

var IsRetryable = apiwire.IsRetryable

func (c *Client) do(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	h := http.Header{}
	h.Set("x-api-key", c.apiKey)
	if idempotencyKey != "" {
		h.Set("Idempotency-Key", idempotencyKey)
	}
	return c.wire.Do(ctx, method, path, h, body, out)
}
