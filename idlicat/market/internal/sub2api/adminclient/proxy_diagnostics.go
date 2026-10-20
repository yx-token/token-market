package adminclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// 字段与 sub2api v0.2.14 的代理检测响应保持一致。
type ProxyTestResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	LatencyMs   int64  `json:"latency_ms"`
	IPAddress   string `json:"ip_address,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

type ProxyQualityResult struct {
	Score          int                `json:"score"`
	Grade          string             `json:"grade"`
	Summary        string             `json:"summary"`
	ExitIP         string             `json:"exit_ip,omitempty"`
	Country        string             `json:"country,omitempty"`
	CountryCode    string             `json:"country_code,omitempty"`
	BaseLatencyMs  int64              `json:"base_latency_ms"`
	PassedCount    int                `json:"passed_count"`
	WarnCount      int                `json:"warn_count"`
	FailedCount    int                `json:"failed_count"`
	ChallengeCount int                `json:"challenge_count"`
	CheckedAt      int64              `json:"checked_at"`
	Items          []ProxyQualityItem `json:"items"`
}

type ProxyQualityItem struct {
	Target     string `json:"target"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"http_status,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	Message    string `json:"message,omitempty"`
	CFRay      string `json:"cf_ray,omitempty"`
}

func (c *Client) TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error) {
	var out *ProxyTestResult
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/proxies/%d/test", id), nil, "", &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("sub2api returned no proxy connectivity report")
	}
	return out, nil
}

func (c *Client) CheckProxyQuality(ctx context.Context, id int64) (*ProxyQualityResult, error) {
	// sub2api 依次探测多个目标，完整报告可能超过普通 Admin API 的 30 秒超时。
	if c.writeGate != nil {
		if err := c.writeGate(ctx); err != nil {
			return nil, err
		}
	}
	var out *ProxyQualityResult
	h := http.Header{"X-Api-Key": []string{c.apiKey}}
	if err := c.diagnosticsWire.Do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/proxies/%d/quality-check", id), h, nil, &out); err != nil {
		return nil, err
	}
	if out == nil || out.Grade == "" || len(out.Items) == 0 {
		return nil, errors.New("sub2api returned no proxy quality report")
	}
	return out, nil
}
