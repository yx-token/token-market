// Package zhipu implements the ZCode OAuth, derived-key and V4 signing protocols.
// Application adapters own durable credentials; session.go provides ephemeral Redis flows.
package zhipu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedhttp "github.com/yx-token/token-market/internal/pkg/httpclient"
)

const (
	ClientVersion   = "3.11.2"
	ZCodeOrigin     = "https://zcode.z.ai"
	BigModelOrigin  = "https://bigmodel.cn"
	ZaiOrigin       = "https://api.z.ai"
	ZaiChatOrigin   = "https://chat.z.ai"
	maxResponseBody = 2 << 20
	requestTimeout  = 30 * time.Second
)

type Provider string

const (
	ProviderBigModel Provider = "bigmodel"
	ProviderZai      Provider = "zai"
)

func (p Provider) validate() error {
	if p != ProviderBigModel && p != ProviderZai {
		return errors.New("zhipu: provider must be bigmodel or zai")
	}
	return nil
}

// HTTPClient permits injection of the account's proxy-aware, URL-validated client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options contains trusted server configuration, never values from an end user.
// HTTPClient overrides ProxyURL; an injected client must enforce redirect/URL policy.
type Options struct {
	HTTPClient     HTTPClient
	ProxyURL       string
	ZCodeOrigin    string
	BigModelOrigin string
	ZaiOrigin      string
	ZaiChatOrigin  string
	Version        string
	Source         SourceOptions
	// AllowInsecureHTTP is an explicit test-only escape hatch; production uses HTTPS.
	AllowInsecureHTTP bool
}

type Client struct {
	http                                HTTPClient
	zcode, bigmodel, zai, chat, version string
	source                              SourceOptions
}

func NewClient(opts Options) (*Client, error) {
	h := opts.HTTPClient
	if h == nil {
		pooled, err := sharedhttp.GetClient(sharedhttp.Options{ProxyURL: opts.ProxyURL, Timeout: requestTimeout, ResponseHeaderTimeout: requestTimeout})
		if err != nil {
			return nil, fmt.Errorf("zhipu HTTP client: %w", err)
		}
		clone := *pooled
		clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		h = &clone
	}
	c := &Client{http: h, version: opts.Version, source: opts.Source}
	if c.version == "" {
		c.version = ClientVersion
	}
	for _, item := range []struct {
		dst             *string
		value, fallback string
	}{
		{&c.zcode, opts.ZCodeOrigin, ZCodeOrigin}, {&c.bigmodel, opts.BigModelOrigin, BigModelOrigin},
		{&c.zai, opts.ZaiOrigin, ZaiOrigin}, {&c.chat, opts.ZaiChatOrigin, ZaiChatOrigin},
	} {
		value := item.value
		if value == "" {
			value = item.fallback
		}
		origin, err := parseOrigin(value, opts.AllowInsecureHTTP)
		if err != nil {
			return nil, err
		}
		*item.dst = origin
	}
	c.source.Version = c.version
	c.source.ZCodeOrigin = c.zcode
	return c, nil
}

func parseOrigin(raw string, allowInsecureHTTP bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("zhipu: invalid origin")
	}
	allowedScheme := u.Scheme == "https" || (allowInsecureHTTP && u.Scheme == "http")
	if !allowedScheme || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return "", errors.New("zhipu: invalid origin")
	}
	return u.Scheme + "://" + u.Host, nil
}

// APIError deliberately omits upstream bodies/messages, which can contain secrets.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("zhipu upstream HTTP %d (code %s)", e.Status, e.Code)
}

type envelope struct {
	Code    json.RawMessage `json:"code"`
	Data    json.RawMessage `json:"data"`
	Success *bool           `json:"success"`
}

func remoteCode(raw json.RawMessage) string {
	s := strings.Trim(string(raw), "\"")
	if s == "" {
		return "missing"
	}
	if len(s) > 64 {
		return "unknown"
	}
	for _, r := range s {
		valid := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
		if !valid {
			return "unknown"
		}
	}
	return s
}

// do uses permissive response decoding because upstream adds fields over time.
func (c *Client) do(ctx context.Context, method, endpoint, auth string, body any, wantCode string, out any) error {
	headers := make(http.Header)
	if auth != "" {
		headers.Set("Authorization", auth)
	}
	return c.doWithHeaders(ctx, method, endpoint, headers, body, wantCode, out)
}

func (c *Client) doWithHeaders(ctx context.Context, method, endpoint string, headers http.Header, body any, wantCode string, out any) error {
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("zhipu encode request: %w", err)
		}
		input = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	// Endpoints are assembled internally from fixed paths and trusted server-configured
	// origins. Dynamic path IDs are escaped; the injected client owns the egress policy.
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input) //nolint:gosec // G704: no end-user-controlled origin; see Options and SignerOptions contracts.
	if err != nil {
		return fmt.Errorf("zhipu request: %w", err)
	}
	req.Header = SourceHeaders(c.source)
	req.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		req.Header[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zhipu HTTP: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return fmt.Errorf("zhipu read response: %w", err)
	}
	if len(b) > maxResponseBody {
		return errors.New("zhipu: response too large")
	}
	var env envelope
	decodeErr := json.Unmarshal(b, &env)
	code := remoteCode(env.Code)
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Code: code}
	}
	if decodeErr != nil {
		return errors.New("zhipu: invalid JSON response")
	}
	success := code == wantCode
	if wantCode == "biz" || wantCode == "profile" {
		success = code == "0" || code == "200" || code == "missing" || code == "null"
	}
	if !success || (env.Success != nil && !*env.Success) {
		return &APIError{Status: resp.StatusCode, Code: code}
	}
	if wantCode == "profile" && len(env.Data) == 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return errors.New("zhipu: invalid userinfo response")
		}
		return nil
	}
	if len(env.Data) == 0 || bytes.Equal(env.Data, []byte("null")) {
		return errors.New("zhipu: missing response data")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return errors.New("zhipu: invalid response data")
	}
	return nil
}

func (c *Client) business(p Provider, token string) (string, string, error) {
	if err := p.validate(); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", "", errors.New("zhipu: missing access token")
	}
	if p == ProviderBigModel {
		return c.bigmodel, token, nil
	}
	return c.zai, "Bearer " + token, nil
}
