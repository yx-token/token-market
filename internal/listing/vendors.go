package listing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
)

// oauthSpec 描述一个厂商在 sub2api 中的授权接口，以及如何把换到的令牌转成账号凭证。
// 各字段对照 sub2api 0.2.14 前端的调用方式（frontend/src/composables/use*OAuth.ts）。
type oauthSpec struct {
	startPath    string
	exchangePath string
	stateFrom    string // response：授权链接接口直接返回 state；url：从授权链接的 query 中解析；none：不需要
	accountType  string
	credentials  func(tok map[string]any) map[string]any
	extra        func(tok map[string]any) map[string]any
	identity     func(tok map[string]any) string // 官方账号身份，用于去重
	tier         func(tok map[string]any) string
}

var oauthSpecs = map[string]map[string]oauthSpec{
	"anthropic": {
		"oauth": {
			startPath: "/api/v1/admin/accounts/generate-auth-url", exchangePath: "/api/v1/admin/accounts/exchange-code",
			stateFrom: "none", accountType: "oauth",
			credentials: copyAll, extra: pick("org_uuid", "account_uuid", "email_address"),
			identity: first("account_uuid", "email_address"), tier: none,
		},
		"setup-token": {
			startPath: "/api/v1/admin/accounts/generate-setup-token-url", exchangePath: "/api/v1/admin/accounts/exchange-setup-token-code",
			stateFrom: "none", accountType: "setup-token",
			credentials: copyAll, extra: pick("org_uuid", "account_uuid", "email_address"),
			identity: first("account_uuid", "email_address"), tier: none,
		},
	},
	"openai": {
		"oauth": {
			startPath: "/api/v1/admin/openai/generate-auth-url", exchangePath: "/api/v1/admin/openai/exchange-code",
			stateFrom: "url", accountType: "oauth",
			credentials: pick("access_token", "expires_at", "refresh_token", "id_token", "email", "chatgpt_account_id",
				"chatgpt_user_id", "organization_id", "plan_type", "subscription_expires_at", "client_id"),
			extra:    pick("email", "name", "privacy_mode"),
			identity: first("chatgpt_account_id", "chatgpt_user_id", "email"),
			tier:     field("plan_type"),
		},
	},
	"gemini": {
		"oauth": {
			startPath: "/api/v1/admin/gemini/oauth/auth-url", exchangePath: "/api/v1/admin/gemini/oauth/exchange-code",
			stateFrom: "response", accountType: "oauth",
			credentials: withStringExpiry(pick("access_token", "refresh_token", "token_type", "expires_at", "scope", "project_id", "oauth_type", "tier_id")),
			extra:       nested("extra"),
			identity:    first("project_id", "refresh_token"),
			tier:        field("tier_id"),
		},
	},
	"antigravity": {
		"oauth": {
			startPath: "/api/v1/admin/antigravity/oauth/auth-url", exchangePath: "/api/v1/admin/antigravity/oauth/exchange-code",
			stateFrom: "response", accountType: "oauth",
			credentials: withStringExpiry(pick("access_token", "refresh_token", "token_type", "expires_at", "project_id", "email", "plan_type")),
			extra:       pick(),
			identity:    first("email", "project_id"),
			tier:        field("plan_type"),
		},
	},
	"grok": {
		"oauth": {
			startPath: "/api/v1/admin/grok/oauth/auth-url", exchangePath: "/api/v1/admin/grok/oauth/exchange-code",
			stateFrom: "response", accountType: "oauth",
			credentials: pick("access_token", "token_type", "expires_at", "client_id", "scope", "email", "sub", "team_id",
				"subscription_tier", "entitlement_status", "refresh_token", "id_token"),
			extra:    pick("email", "subscription_tier", "entitlement_status"),
			identity: first("sub", "email"),
			tier:     field("subscription_tier"),
		},
	},
}

// codingPlatforms 是用 key 接入的订阅（Coding Plan）平台及其 account_mode。
var codingPlatforms = map[string]string{
	"kimi":        "coding",
	"zhipu":       "coding",
	"minimax":     "coding",
	"opencode_go": "go",
}

// privacyPlatforms 是 sub2api 支持 set-privacy 的平台（仅 OAuth 账号）。
var privacyPlatforms = map[string]bool{"openai": true, "antigravity": true}

// IsOAuthAuthType 判断平台的某接入方式是否走 OAuth 授权。
func IsOAuthAuthType(platform, authType string) bool {
	_, ok := oauthSpecs[platform][authType]
	return ok
}

// IsCodingAuthType 判断是否为 Coding Plan key 接入。
func IsCodingAuthType(platform, authType string) bool {
	mode, ok := codingPlatforms[platform]
	return ok && (authType == mode || authType == "coding")
}

type oauthStart struct {
	AuthURL   string
	SessionID string
	State     string
}

func startOAuth(ctx context.Context, admin *adminclient.Client, spec oauthSpec, proxyID *int64) (*oauthStart, error) {
	body := map[string]any{}
	if proxyID != nil {
		body["proxy_id"] = *proxyID
	}
	var resp map[string]any
	if err := admin.Post(ctx, spec.startPath, body, "", &resp); err != nil {
		return nil, err
	}
	out := &oauthStart{AuthURL: str(resp, "auth_url"), SessionID: str(resp, "session_id")}
	if out.AuthURL == "" || out.SessionID == "" {
		return nil, errors.New("sub2api returned no auth_url or session_id")
	}
	switch spec.stateFrom {
	case "response":
		out.State = str(resp, "state")
	case "url":
		if u, err := url.Parse(out.AuthURL); err == nil {
			out.State = u.Query().Get("state")
		}
	}
	return out, nil
}

// parseAuthorizationInput 接受卖家粘贴的回调地址或授权码，返回 code 与 state。
// Claude 等厂商展示的授权码可能形如 "code#state"，原样交给 sub2api 处理。
func parseAuthorizationInput(input string) (code, state string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", httpx.BadRequest("EMPTY_CODE", "authorization code is required")
	}
	if strings.Contains(input, "code=") {
		raw := input
		if i := strings.Index(raw, "?"); i >= 0 {
			raw = raw[i+1:]
		}
		if i := strings.Index(raw, "#"); i >= 0 && !strings.Contains(raw[:i], "code=") {
			raw = raw[i+1:]
		}
		q, perr := url.ParseQuery(raw)
		if perr != nil || q.Get("code") == "" {
			return "", "", httpx.BadRequest("INVALID_CALLBACK", "cannot parse code from callback url")
		}
		return q.Get("code"), q.Get("state"), nil
	}
	return input, "", nil
}

func exchangeOAuth(ctx context.Context, admin *adminclient.Client, spec oauthSpec, sessionID, storedState, input string, proxyID *int64) (map[string]any, error) {
	code, state, err := parseAuthorizationInput(input)
	if err != nil {
		return nil, err
	}
	if state != "" && storedState != "" && state != storedState {
		return nil, httpx.BadRequest("STATE_MISMATCH", "authorization state does not match, please restart authorization")
	}
	if state == "" {
		state = storedState
	}
	body := map[string]any{"session_id": sessionID, "code": code}
	if spec.stateFrom != "none" {
		if state == "" {
			return nil, httpx.BadRequest("STATE_REQUIRED", "please paste the full callback url")
		}
		body["state"] = state
	}
	if proxyID != nil {
		body["proxy_id"] = *proxyID
	}
	var tok map[string]any
	if err := admin.Post(ctx, spec.exchangePath, body, "", &tok); err != nil {
		return nil, err
	}
	if str(tok, "access_token") == "" {
		return nil, errors.New("sub2api returned no access token")
	}
	return tok, nil
}

// ---- 凭证构建工具 ----

func copyAll(tok map[string]any) map[string]any {
	out := make(map[string]any, len(tok))
	for k, v := range tok {
		out[k] = v
	}
	return out
}

func pick(keys ...string) func(map[string]any) map[string]any {
	return func(tok map[string]any) map[string]any {
		out := map[string]any{}
		for _, k := range keys {
			if v, ok := tok[k]; ok && v != nil && v != "" {
				out[k] = v
			}
		}
		return out
	}
}

func nested(key string) func(map[string]any) map[string]any {
	return func(tok map[string]any) map[string]any {
		if m, ok := tok[key].(map[string]any); ok {
			return m
		}
		return map[string]any{}
	}
}

// withStringExpiry 把数字形式的 expires_at 转成整数字符串（Gemini / Antigravity 账号要求）。
func withStringExpiry(inner func(map[string]any) map[string]any) func(map[string]any) map[string]any {
	return func(tok map[string]any) map[string]any {
		out := inner(tok)
		if f, ok := out["expires_at"].(float64); ok && !math.IsNaN(f) {
			out["expires_at"] = strconv.FormatInt(int64(f), 10)
		}
		return out
	}
}

func first(keys ...string) func(map[string]any) string {
	return func(tok map[string]any) string {
		for _, k := range keys {
			if v := str(tok, k); v != "" {
				return k + ":" + v
			}
		}
		return ""
	}
}

func field(key string) func(map[string]any) string {
	return func(tok map[string]any) string { return str(tok, key) }
}

func none(map[string]any) string { return "" }

func str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}
