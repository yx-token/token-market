package kimi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/internal/pkg/redissession"
	"github.com/yx-token/token-market/internal/util/logredact"
)

const (
	DefaultOAuthHost = "https://auth.kimi.com"
	OAuthHostEnv     = "KIMI_CODE_OAUTH_HOST"
	ClientID         = "17e5f671-d194-4dfb-9706-5516cb48c098"
	DefaultBaseURL   = "https://api.kimi.com/coding/v1"
	CLIVersion       = "2.2.0"
	DeviceSessionTTL = 15 * time.Minute
	DeviceGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
)

var (
	ErrAuthorizationPending = errors.New("kimi authorization pending")
	ErrSlowDown             = errors.New("kimi authorization slow down")
	ErrExpiredToken         = errors.New("kimi device code expired")
	ErrAccessDenied         = errors.New("kimi authorization denied")
	ErrInvalidGrant         = errors.New("kimi refresh token invalid")
	ErrInvalidTokenResponse = errors.New("kimi OAuth response missing access_token, refresh_token or valid expires_in; please authorize again")
)

type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type DeviceAuthorizationResponse struct {
	UserCode                string `json:"user_code"`
	DeviceCode              string `json:"device_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type Client struct {
	HTTP     *http.Client
	Host     string
	ProxyURL string
}

type DeviceSession struct {
	DeviceCode string    `json:"device_code"`
	ProxyID    *int64    `json:"proxy_id,omitempty"`
	ProxyURL   string    `json:"proxy_url,omitempty"`
	DeviceID   string    `json:"device_id"`
	Interval   int64     `json:"interval"`
	ExpiresAt  time.Time `json:"expires_at"`
	Consumed   bool      `json:"consumed,omitempty"`
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*DeviceSession
	remote   *redissession.Store
}

func NewSessionStore() *SessionStore { return &SessionStore{sessions: make(map[string]*DeviceSession)} }
func NewRedisSessionStore(rdb *redis.Client) *SessionStore {
	s := NewSessionStore()
	if rdb != nil {
		s.remote = redissession.New(rdb, "oauth:session:kimi", DeviceSessionTTL)
	}
	return s
}
func (s *SessionStore) Set(id string, session *DeviceSession) error {
	if s == nil || session == nil {
		return errors.New("kimi session store is not configured")
	}
	if s.remote != nil {
		return s.remote.SetWithTTL(context.Background(), id, session, time.Until(session.ExpiresAt))
	}
	s.mu.Lock()
	copy := *session
	// Updating the polling interval must not release an in-flight poll's claim.
	// Only Release may make the device code available to the next poll.
	if current := s.sessions[id]; current != nil {
		copy.Consumed = current.Consumed
	}
	s.sessions[id] = &copy
	s.mu.Unlock()
	return nil
}
func (s *SessionStore) Get(id string) (*DeviceSession, bool) {
	if s == nil {
		return nil, false
	}
	if s.remote != nil {
		var out DeviceSession
		ok, err := s.remote.Get(context.Background(), id, &out)
		if err == nil && ok && time.Now().Before(out.ExpiresAt) {
			s.mu.Lock()
			s.sessions[id] = &out
			s.mu.Unlock()
			return &out, true
		}
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out, ok := s.sessions[id]
	if !ok || out == nil || time.Now().After(out.ExpiresAt) {
		return nil, false
	}
	copy := *out
	return &copy, true
}

// GetIncludingExpired is used by the device-code poll endpoint so an expired
// but still retained session can be reported as the RFC 8628 terminal
// `expired` status instead of being mistaken for an unknown session.
func (s *SessionStore) GetIncludingExpired(id string) (*DeviceSession, bool) {
	if s == nil {
		return nil, false
	}
	if s.remote != nil {
		var out DeviceSession
		ok, err := s.remote.Get(context.Background(), id, &out)
		if err == nil && ok {
			s.mu.Lock()
			s.sessions[id] = &out
			s.mu.Unlock()
			return &out, true
		}
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out, ok := s.sessions[id]
	if !ok || out == nil {
		return nil, false
	}
	copy := *out
	return &copy, true
}

// Claim serializes upstream polls across processes. Redis failure fails closed.
func (s *SessionStore) Claim(ctx context.Context, id string) (bool, error) {
	if s.remote != nil {
		return s.remote.TryConsume(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok || session.Consumed {
		return false, nil
	}
	session.Consumed = true
	return true, nil
}
func (s *SessionStore) Release(ctx context.Context, id string) {
	if s.remote != nil {
		_ = s.remote.ReleaseClaim(ctx, id)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		session.Consumed = false
	}
}

func (s *SessionStore) Delete(id string) {
	if s == nil {
		return
	}
	if s.remote != nil {
		_ = s.remote.Delete(context.Background(), id)
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}
func (s *SessionStore) Stop() {}

func EffectiveHost() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(OAuthHostEnv)), "/"); v != "" {
		return v
	}
	return DefaultOAuthHost
}

func NewClient(proxyURL string) *Client {
	c := &http.Client{Timeout: 30 * time.Second}
	if strings.TrimSpace(proxyURL) != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			c.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	return &Client{HTTP: c, Host: EffectiveHost(), ProxyURL: proxyURL}
}

func (c *Client) headers(req *http.Request, deviceID string) {
	if req != nil {
		ApplyDeviceHeaders(req.Header, deviceID)
	}
}

// Device identity follows CLI's "OS product-version architecture" / OS release
// semantics. Deployments can supply their host's values without shelling out.
func ApplyDeviceHeaders(headers http.Header, deviceID string) {
	if headers == nil {
		return
	}
	headers.Set("User-Agent", "kimi-code-cli/"+CLIVersion)
	headers.Set("X-Msh-Platform", "kimi_code_cli")
	headers.Set("X-Msh-Version", CLIVersion)
	headers.Set("X-Msh-Device-Name", identityValue("KIMI_CODE_DEVICE_NAME", hostname()))
	osName := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	releaseDefault := map[string]string{"linux": "6.1.0", "darwin": "23.0.0", "windows": "10.0.0"}[runtime.GOOS]
	if releaseDefault == "" {
		releaseDefault = "1.0.0"
	}
	release := identityValue("KIMI_CODE_OS_VERSION", releaseDefault)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	productVersion := release
	if runtime.GOOS == "darwin" {
		// macOS product versions differ from Darwin kernel releases.
		productVersion = "14.0.0"
	}
	headers.Set("X-Msh-Device-Model", identityValue("KIMI_CODE_DEVICE_MODEL", osName+" "+productVersion+" "+arch))
	headers.Set("X-Msh-Os-Version", release)
	if strings.TrimSpace(deviceID) != "" {
		headers.Set("X-Msh-Device-Id", deviceID)
	}
}
func identityValue(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		value = fallback
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return -1
		}
		return r
	}, value)
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "sub2api"
	}
	return h
}

func NewDeviceID() string { return uuid.NewString() }

func GenerateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values, deviceID string) ([]byte, int, error) {
	host := strings.TrimRight(strings.TrimSpace(c.Host), "/")
	if host == "" {
		host = DefaultOAuthHost
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	c.headers(req, deviceID)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > 2<<20 {
		return nil, resp.StatusCode, errors.New("kimi oauth response too large")
	}
	return data, resp.StatusCode, nil
}

func (c *Client) DeviceAuthorization(ctx context.Context, deviceID string) (*DeviceAuthorizationResponse, error) {
	if c == nil {
		return nil, errors.New("kimi oauth client is nil")
	}
	body, status, err := c.postForm(ctx, "/api/oauth/device_authorization", url.Values{"client_id": {ClientID}}, deviceID)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, oauthError("device authorization", status, body)
	}
	var out DeviceAuthorizationResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.DeviceCode == "" || out.UserCode == "" || out.VerificationURIComplete == "" {
		return nil, errors.New("kimi device authorization response missing required fields")
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = int64(DeviceSessionTTL / time.Second)
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	if out.VerificationURI == "" {
		out.VerificationURI = out.VerificationURIComplete
	}
	return &out, nil
}

type PollStatus string

const (
	PollPending  PollStatus = "pending"
	PollSlowDown PollStatus = "slow_down"
	PollSuccess  PollStatus = "success"
	PollExpired  PollStatus = "expired"
	PollDenied   PollStatus = "denied"
	PollFailed   PollStatus = "failed"
)

func (c *Client) Poll(ctx context.Context, deviceCode, deviceID string) (PollStatus, *TokenResponse, int64, error) {
	if c == nil {
		return "", nil, 0, errors.New("kimi oauth client is nil")
	}
	body, status, err := c.postForm(ctx, "/api/oauth/token", url.Values{"client_id": {ClientID}, "device_code": {deviceCode}, "grant_type": {DeviceGrantType}}, deviceID)
	if err != nil {
		return "", nil, 0, err
	}
	var out TokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, 0, fmt.Errorf("parse kimi token response: %w", err)
	}
	if status >= 500 {
		return "", nil, 0, fmt.Errorf("kimi token endpoint HTTP %d", status)
	}
	switch strings.ToLower(strings.TrimSpace(out.Error)) {
	case "authorization_pending":
		return PollPending, nil, 0, nil
	case "slow_down":
		return PollSlowDown, nil, 0, nil /* interval increment handled by service */
	case "expired_token":
		return PollExpired, nil, 0, ErrExpiredToken
	case "access_denied":
		return PollDenied, nil, 0, ErrAccessDenied
	}
	if status != http.StatusOK {
		return "", nil, 0, oauthError("token endpoint", status, body, deviceCode)
	}
	if err := ValidateToken(&out); err != nil {
		return PollFailed, nil, 0, nil
	}
	if out.TokenType == "" {
		out.TokenType = "Bearer"
	}
	return PollSuccess, &out, out.ExpiresIn, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken, deviceID string) (*TokenResponse, error) {
	if c == nil {
		return nil, errors.New("kimi oauth client is nil")
	}
	body, status, err := c.postForm(ctx, "/api/oauth/token", url.Values{"client_id": {ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}, deviceID)
	if err != nil {
		return nil, err
	}
	var out TokenResponse
	_ = json.Unmarshal(body, &out)
	if status == http.StatusUnauthorized || status == http.StatusForbidden || strings.EqualFold(out.Error, "invalid_grant") {
		return nil, ErrInvalidGrant
	}
	if status >= 500 {
		return nil, oauthError("token refresh", status, body, refreshToken)
	}
	if status != http.StatusOK {
		return nil, oauthError("token refresh", status, body, refreshToken)
	}
	if err := ValidateToken(&out); err != nil {
		return nil, err
	}
	if out.TokenType == "" {
		out.TokenType = "Bearer"
	}
	return &out, nil
}

// ValidateToken matches CLI tokenFromResponse: both tokens and positive expiry
// are mandatory; scope and token_type are optional.
func ValidateToken(token *TokenResponse) error {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.RefreshToken) == "" || token.ExpiresIn <= 0 {
		return ErrInvalidTokenResponse
	}
	return nil
}

type OAuthError struct {
	Status            int
	Code, Description string
}

func (e *OAuthError) Error() string {
	return fmt.Sprintf("kimi OAuth HTTP %d: %s %s", e.Status, e.Code, e.Description)
}
func (e *OAuthError) Retryable() bool {
	switch e.Status {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

var safeOAuthCode = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

func oauthError(operation string, status int, body []byte, secrets ...string) error {
	var payload TokenResponse
	_ = json.Unmarshal(body, &payload)
	code := payload.Error
	if !safeOAuthCode.MatchString(code) {
		code = "oauth_error"
	}
	description := logredact.RedactText(payload.ErrorDescription, "device_code", "api_key", "authorization")
	// OAuth endpoints are untrusted: strip token-shaped values, echoed request
	// fields and control characters; never include the raw body.
	for _, value := range append(secrets, payload.AccessToken, payload.RefreshToken) {
		if value != "" {
			description = strings.ReplaceAll(description, value, "[REDACTED]")
		}
	}
	description = strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return ' '
		}
		return r
	}, description)
	if len(description) > 200 {
		description = description[:200]
	}
	return &OAuthError{Status: status, Code: code, Description: operation + ": " + description}
}
