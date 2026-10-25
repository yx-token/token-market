package zhipu

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ManualFlowTTL = 5 * time.Minute

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("zhipu random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GeneratePollToken returns 32 cryptographically random bytes as lowercase hex.
func GeneratePollToken() (string, error) { return randomHex(32) }

type InitResult struct {
	FlowID          string    `json:"flow_id"`
	State           string    `json:"state"`
	AuthorizeURL    string    `json:"authorize_url"`
	ExpiresAt       time.Time `json:"expires_at"`
	PollIntervalSec int       `json:"poll_interval_sec"`
}

func (c *Client) Init(ctx context.Context, provider Provider, pollToken string) (*InitResult, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(pollToken) == "" {
		return nil, errors.New("zhipu: missing poll token")
	}
	var data struct {
		FlowID          string          `json:"flow_id"`
		AuthorizeURL    string          `json:"authorize_url"`
		ExpiresAt       json.RawMessage `json:"expires_at"`
		PollIntervalSec int             `json:"poll_interval_sec"`
	}
	err := c.do(ctx, http.MethodPost, c.zcode+"/api/v1/oauth/cli/init", "Bearer "+pollToken,
		map[string]Provider{"provider": provider}, "0", &data)
	if err != nil {
		return nil, err
	}
	expires, err := parseTimestamp(data.ExpiresAt)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(data.AuthorizeURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.TrimSpace(data.FlowID) == "" || data.PollIntervalSec < 1 || !expires.After(time.Now()) {
		return nil, errors.New("zhipu: invalid OAuth init response")
	}
	query, err := url.ParseQuery(u.RawQuery)
	state := strings.TrimSpace(query.Get("state"))
	remaining := time.Until(expires).Seconds()
	if err != nil || state == "" || len(query["state"]) != 1 || float64(data.PollIntervalSec) >= remaining {
		return nil, errors.New("zhipu: invalid OAuth flow state or poll interval")
	}
	// Match the desktop client: the intermediate page bridges CLI polling and
	// exposes the same deep link used by manual code exchange.
	redirectParam := "redirect"
	if provider == ProviderZai {
		redirectParam = "redirect_uri"
	}
	query.Set(redirectParam, c.DesktopRedirectURI())
	u.RawQuery = query.Encode()
	return &InitResult{FlowID: data.FlowID, State: state, AuthorizeURL: u.String(), ExpiresAt: expires, PollIntervalSec: data.PollIntervalSec}, nil
}

type PollStatus string

const (
	PollPending PollStatus = "pending"
	PollReady   PollStatus = "ready"
	PollFailed  PollStatus = "failed"
	PollExpired PollStatus = "expired"
)

type PollResult struct {
	Status PollStatus
	Tokens *TokenData
}

// Poll makes one request. The caller owns flow expiry, cadence and single consumption.
// Ready data is not a business token yet; pass it to NormalizeTokens with the stored provider.
func (c *Client) Poll(ctx context.Context, provider Provider, flowID, pollToken string) (*PollResult, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}
	if flowID == "" || strings.TrimSpace(pollToken) == "" {
		return nil, errors.New("zhipu: missing flow ID or poll token")
	}
	var data struct {
		Status PollStatus `json:"status"`
		TokenData
	}
	if err := c.do(ctx, http.MethodGet, c.zcode+"/api/v1/oauth/cli/poll/"+url.PathEscape(flowID), "Bearer "+pollToken, nil, "0", &data); err != nil {
		return nil, err
	}
	result := &PollResult{Status: data.Status}
	switch data.Status {
	case PollPending, PollFailed, PollExpired:
	case PollReady:
		access := data.BigModel.value()
		if provider == ProviderZai {
			access = data.Zai.AccessToken
		}
		userID, ok := data.User["user_id"].(string)
		if strings.TrimSpace(data.Token) == "" || strings.TrimSpace(access) == "" || !ok || strings.TrimSpace(userID) == "" {
			return nil, errors.New("zhipu: incomplete ready token data")
		}
		result.Tokens = &data.TokenData
	default:
		return nil, errors.New("zhipu: unknown polling status")
	}
	return result, nil
}

type Callback struct {
	Code        string
	State       string
	ChannelID   string
	UTMSource   string
	UTMCampaign string
}

// ParseAuthorizationInput accepts a deep link, query, or JSON code/authCode+state.
// A bare code is rejected because the provider requires state.
func ParseAuthorizationInput(provider Provider, raw string) (*Callback, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("zhipu: empty callback")
	}
	var values url.Values
	var err error
	if strings.HasPrefix(raw, "{") {
		var fields map[string]string
		if err = json.Unmarshal([]byte(raw), &fields); err != nil {
			return nil, errors.New("zhipu: invalid callback JSON")
		}
		values = make(url.Values)
		for k, v := range fields {
			values.Set(k, v)
		}
	} else if strings.Contains(raw, "://") {
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Scheme != "zcode" || u.Host != "oauth" || u.Path != "/callback" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("zhipu: expected zcode://oauth/callback")
		}
		values, err = url.ParseQuery(u.RawQuery)
	} else {
		values, err = url.ParseQuery(strings.TrimPrefix(raw, "?"))
	}
	if err != nil {
		return nil, errors.New("zhipu: invalid callback query")
	}
	for _, name := range []string{"code", "authCode", "state"} {
		if len(values[name]) > 1 {
			return nil, errors.New("zhipu: ambiguous callback parameters")
		}
	}
	primary, fallback := "authCode", "code"
	if provider == ProviderZai {
		primary, fallback = fallback, primary
	}
	code := strings.TrimSpace(values.Get(primary))
	if code == "" {
		code = strings.TrimSpace(values.Get(fallback))
	}
	state := strings.TrimSpace(values.Get("state"))
	if code == "" || state == "" {
		return nil, errors.New("zhipu: callback requires code/authCode and state")
	}
	return &Callback{code, state, values.Get("channel_id"), values.Get("utm_source"), values.Get("utm_campaign")}, nil
}

func (c *Client) DesktopRedirectURI() string {
	q := url.Values{"redirect": {"zcode://oauth/callback"}, "app_version": {c.version}}
	return c.zcode + "/app/oauth/login?" + q.Encode()
}

// ManualAuthorizationURL returns the initial intermediate redirect URI as well.
// Retain that exact URI, state and a five-minute TTL in the application's session.
func (c *Client) ManualAuthorizationURL(provider Provider, state string) (authorizeURL, redirectURI string, err error) {
	if err = provider.validate(); err != nil {
		return "", "", err
	}
	if state == "" {
		return "", "", errors.New("zhipu: missing state")
	}
	redirectURI = c.DesktopRedirectURI()
	if provider == ProviderBigModel {
		return c.bigmodel + "/login?" + url.Values{"redirect": {redirectURI}, "appId": {"zcode"}, "state": {state}}.Encode(), redirectURI, nil
	}
	return c.chat + "/api/oauth/authorize?" + url.Values{"redirect_uri": {redirectURI}, "response_type": {"code"}, "client_id": {"client_P8X5CMWmlaRO9gyO-KSqtg"}, "state": {state}}.Encode(), redirectURI, nil
}

// ExchangeCode checks the stored state before exchanging a manually parsed callback.
// The caller must atomically consume its unexpired session before making this call.
func (c *Client) ExchangeCode(ctx context.Context, provider Provider, callback Callback, expectedState, redirectURI string) (*TokenSet, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}
	if expectedState == "" || subtle.ConstantTimeCompare([]byte(callback.State), []byte(expectedState)) != 1 || callback.Code == "" {
		return nil, errors.New("zhipu: missing code or mismatched OAuth state")
	}
	redirect, err := url.Parse(redirectURI)
	if err != nil || redirect.Scheme != "https" || redirect.User != nil || redirect.Fragment != "" || strings.Contains(redirectURI, "#") {
		return nil, errors.New("zhipu: redirect_uri must be the initial intermediate URL")
	}
	query, err := url.ParseQuery(redirect.RawQuery)
	expected, _ := url.Parse(c.DesktopRedirectURI())
	if err != nil || len(query) != 2 || len(query["redirect"]) != 1 || len(query["app_version"]) != 1 {
		return nil, errors.New("zhipu: redirect_uri query must match initial intermediate URL")
	}
	redirect.RawQuery = query.Encode()
	if redirect.String() != expected.String() {
		return nil, errors.New("zhipu: redirect_uri differs from initial intermediate URL")
	}
	var data TokenData
	if err := c.do(ctx, http.MethodPost, c.zcode+"/api/v1/oauth/token", "", map[string]any{
		"provider": provider, "code": callback.Code, "state": callback.State, "redirect_uri": redirectURI,
	}, "0", &data); err != nil {
		return nil, err
	}
	return c.NormalizeTokens(ctx, provider, data)
}

type AccessTokenData struct {
	AccessToken       string `json:"access_token"`
	AccessTokenCamel  string `json:"accessToken"`
	RefreshToken      string `json:"refresh_token"`
	RefreshTokenCamel string `json:"refreshToken"`
}

func (t AccessTokenData) value() string {
	if t.AccessToken != "" {
		return t.AccessToken
	}
	return t.AccessTokenCamel
}

// TokenData is the common /oauth/token and CLI ready payload.
type TokenData struct {
	AccessTokenData
	Token     string          `json:"token"`
	BigModel  AccessTokenData `json:"bigmodel"`
	Zai       AccessTokenData `json:"zai"`
	User      map[string]any  `json:"user"`
	ExpiresIn json.Number     `json:"expires_in"`
}

type TokenSet struct {
	Provider          Provider
	AccessToken       string // BigModel access token or Z.ai business token.
	OAuthAccessToken  string // Preserve the original token for Z.ai business re-exchange.
	RefreshToken      string // BigModel only; no verified refresh grant exists.
	ZCodeJWT          string
	JWTExpiresAt      time.Time
	OAuthExpiresAt    time.Time
	BusinessExpiresAt time.Time
	User              map[string]any
}

type JWTStatus string

const (
	JWTStatusValid    JWTStatus = "valid"
	JWTStatusExpiring JWTStatus = "expiring"
	JWTStatusUnknown  JWTStatus = "unknown"
)

// JWTStatus exposes unknown separately; only known expiration may request reauthorization.
func (t TokenSet) JWTStatus(now time.Time) JWTStatus {
	if t.JWTExpiresAt.IsZero() {
		return JWTStatusUnknown
	}
	if !now.Add(30 * time.Second).Before(t.JWTExpiresAt) {
		return JWTStatusExpiring
	}
	return JWTStatusValid
}

// JWTExpiring uses the 30-second margin; unknown JWT lifetime does not block derived-key inference.
func (t TokenSet) JWTExpiring(now time.Time) bool {
	return t.JWTStatus(now) == JWTStatusExpiring
}
func (t TokenSet) BusinessExpiring(now time.Time) bool {
	if t.BusinessExpiresAt.IsZero() {
		return t.Provider == ProviderZai
	}
	return !now.Add(300 * time.Second).Before(t.BusinessExpiresAt)
}

// ParseJWTExpiration decodes exp only; it does not authenticate or verify a JWT.
func ParseJWTExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("zhipu: malformed JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, errors.New("zhipu: malformed JWT payload")
	}
	var payload struct {
		Exp json.Number `json:"exp"`
	}
	if err := json.Unmarshal(b, &payload); err != nil || payload.Exp == "" {
		return time.Time{}, errors.New("zhipu: missing numeric JWT exp")
	}
	// String exp is rejected; json.Number itself also accepts JSON strings.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil || len(fields["exp"]) == 0 || fields["exp"][0] == '"' {
		return time.Time{}, errors.New("zhipu: missing numeric JWT exp")
	}
	n, err := payload.Exp.Float64()
	if err != nil || n <= 0 || n >= float64(math.MaxInt64) || math.IsNaN(n) || math.IsInf(n, 0) {
		return time.Time{}, errors.New("zhipu: invalid JWT exp")
	}
	seconds, fraction := math.Modf(n)
	return time.Unix(int64(seconds), int64(fraction*1e9)).UTC(), nil
}

func expiryAfter(raw json.Number, now time.Time) time.Time {
	n, err := raw.Int64()
	if err != nil || n <= 0 || n > int64(math.MaxInt64)/int64(time.Second) {
		return time.Time{}
	}
	return now.Add(time.Duration(n) * time.Second)
}

func parseTimestamp(raw json.RawMessage) (time.Time, error) {
	s := strings.Trim(string(raw), "\"")
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, errors.New("zhipu: invalid expires_at")
	}
	if n > 1e12 {
		return time.UnixMilli(n).UTC(), nil
	}
	return time.Unix(n, 0).UTC(), nil
}

func (c *Client) NormalizeTokens(ctx context.Context, provider Provider, data TokenData) (*TokenSet, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(data.Token) == "" {
		return nil, errors.New("zhipu: missing zcode token")
	}
	exp, _ := ParseJWTExpiration(data.Token)
	var err error
	access := data.BigModel
	if provider == ProviderZai {
		access = data.Zai
	}
	if access.value() == "" && provider == ProviderBigModel {
		access = data.AccessTokenData
	}
	if access.value() == "" {
		return nil, errors.New("zhipu: missing OAuth access token")
	}
	t := &TokenSet{Provider: provider, AccessToken: access.value(), OAuthAccessToken: access.value(), ZCodeJWT: data.Token, JWTExpiresAt: exp, OAuthExpiresAt: expiryAfter(data.ExpiresIn, time.Now()), User: data.User}
	if provider == ProviderBigModel {
		t.RefreshToken = data.BigModel.RefreshToken
		if t.RefreshToken == "" {
			t.RefreshToken = data.BigModel.RefreshTokenCamel
		}
		if t.RefreshToken == "" {
			t.RefreshToken = access.RefreshToken
		}
		if t.RefreshToken == "" {
			t.RefreshToken = access.RefreshTokenCamel
		}
	} else {
		t.AccessToken, t.BusinessExpiresAt, err = c.ExchangeBusinessToken(ctx, t.OAuthAccessToken)
		if err != nil {
			return nil, err
		}
	}
	if len(t.User) == 0 {
		t.User, err = c.UserInfo(ctx, provider, t.AccessToken)
		if err != nil || len(t.User) == 0 {
			t.User = map[string]any{"id": "unknown", "username": "user", "displayName": "User"}
		}
	}
	return t, nil
}

func (c *Client) ExchangeBusinessToken(ctx context.Context, oauthToken string) (string, time.Time, error) {
	if oauthToken == "" {
		return "", time.Time{}, errors.New("zhipu: missing Z.ai OAuth token")
	}
	var data struct {
		AccessTokenData
		ExpiresIn json.Number `json:"expires_in"`
	}
	if err := c.do(ctx, http.MethodPost, c.zai+"/api/auth/z/login", "", map[string]string{"token": oauthToken}, "biz", &data); err != nil {
		return "", time.Time{}, err
	}
	if data.value() == "" {
		return "", time.Time{}, errors.New("zhipu: missing business access token")
	}
	return data.value(), expiryAfter(data.ExpiresIn, time.Now()), nil
}

func (c *Client) UserInfo(ctx context.Context, provider Provider, accessToken string) (map[string]any, error) {
	origin, auth, err := c.business(provider, accessToken)
	if err != nil {
		return nil, err
	}
	endpoint := origin + "/api/biz/customer/getCustomerInfo"
	if provider == ProviderZai {
		endpoint = c.chat + "/api/oauth/userinfo"
	}
	var data map[string]any
	wantCode := "biz"
	if provider == ProviderZai {
		wantCode = "profile"
	}
	if err := c.do(ctx, http.MethodGet, endpoint, auth, nil, wantCode, &data); err != nil {
		return nil, err
	}
	return data, nil
}
