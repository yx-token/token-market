package listing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
)

// oauthSpec 描述一个厂商在 sub2api 中的授权接口，以及如何把换到的令牌转成账号凭证。
// 各字段对照 sub2api 0.2.14 前端的调用方式（frontend/src/composables/use*OAuth.ts）。
type oauthSpec struct {
	startPath    string
	exchangePath string
	stateFrom    string         // response：授权链接接口直接返回 state；url：从授权链接的 query 中解析；none：不需要
	params       map[string]any // 授权链接与换取令牌都要带上的固定参数（例如 Gemini 的 oauth_type）
	projectID    bool           // 是否接受卖家填写 GCP 项目 ID（Gemini Code Assist）
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
		// GCP Code Assist：开通了 Gemini Code Assist（Standard / Enterprise）的 GCP 项目；项目 ID 留空时由 sub2api 自动探测
		"oauth": geminiSpec("code_assist", true),
		// Google One：订阅了 Google AI Pro / Ultra 的个人 Google 账号，不需要 GCP 项目
		"google_one": geminiSpec("google_one", false),
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

// geminiSpec 是 Gemini 两种订阅账号的授权方式；两者都用 sub2api 内置的 Gemini CLI 客户端，只是 oauth_type 不同。
func geminiSpec(oauthType string, projectID bool) oauthSpec {
	return oauthSpec{
		startPath: "/api/v1/admin/gemini/oauth/auth-url", exchangePath: "/api/v1/admin/gemini/oauth/exchange-code",
		stateFrom: "response", params: map[string]any{"oauth_type": oauthType}, projectID: projectID, accountType: "oauth",
		credentials: withStringExpiry(pick("access_token", "refresh_token", "token_type", "expires_at", "scope", "project_id", "oauth_type", "tier_id")),
		extra:       nested("extra"),
		identity:    first("project_id", "refresh_token"),
		tier:        field("tier_id"),
	}
}

// codingSpec 是一种用 key 接入的订阅（Coding Plan）：sub2api 的 account_mode 与官方推理地址。
// baseURL 为空时 sub2api 按 account_mode 使用官方默认地址（国内站）。地址只取这里的固定值，
// 不接受卖家自填，防止中转 key 冒充订阅。
type codingSpec struct {
	mode    string
	baseURL string
	team    bool // 是否支持团队版（智谱 GLM Coding Plan 团队版需要组织 / 项目 ID 才能查询额度）
}

var codingSpecs = map[string]map[string]codingSpec{
	"kimi": {"coding": {mode: "coding"}},
	"zhipu": {
		"coding":      {mode: "coding", team: true},
		"coding-intl": {mode: "coding", baseURL: "https://api.z.ai/api/coding/paas/v4"},
	},
	"minimax": {
		"coding":      {mode: "coding"},
		"coding-intl": {mode: "coding", baseURL: "https://api.minimax.io/v1"},
	},
	"opencode_go": {"go": {mode: "go"}},
}

// authTypeOrder 是各接入方式在列表中的先后顺序。
var authTypeOrder = []string{"google_one", "oauth", "setup-token", "coding", "coding-intl", "go"}

// privacyPlatforms 是 sub2api 支持 set-privacy 的平台（仅 OAuth 账号）。
var privacyPlatforms = map[string]bool{"openai": true, "antigravity": true}

// IsOAuthAuthType 判断平台的某接入方式是否走 OAuth 授权。
func IsOAuthAuthType(platform, authType string) bool {
	_, ok := oauthSpecs[platform][authType]
	return ok
}

// IsCodingAuthType 判断是否为 Coding Plan key 接入。
func IsCodingAuthType(platform, authType string) bool {
	_, ok := codingSpecs[platform][authType]
	return ok
}

// SupportedAuthTypes 返回本系统为平台实现了的全部接入方式；厂商配置只能在其中选择开放哪些。
func SupportedAuthTypes(platform string) []string {
	var out []string
	for _, t := range authTypeOrder {
		if IsOAuthAuthType(platform, t) || IsCodingAuthType(platform, t) {
			out = append(out, t)
		}
	}
	return out
}

type oauthStart struct {
	AuthURL   string
	SessionID string
	State     string
}

func startOAuth(ctx context.Context, admin *adminclient.Client, spec oauthSpec, proxyID *int64, projectID string) (*oauthStart, error) {
	body := map[string]any{}
	for k, v := range spec.params {
		body[k] = v
	}
	if proxyID != nil {
		body["proxy_id"] = *proxyID
	}
	if spec.projectID && projectID != "" {
		body["project_id"] = projectID
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
	for k, v := range spec.params {
		body[k] = v
	}
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

// AccountEmail 返回令牌信息或账号 extra 中的官方账号邮箱，取值顺序与 sub2api 账号列表一致：
// Claude 为 email_address，OpenAI、Antigravity、Grok 为 email；Gemini 与 Coding Plan 没有邮箱。
func AccountEmail(m map[string]any) string {
	for _, k := range []string{"email_address", "email"} {
		if v := strings.TrimSpace(str(m, k)); strings.Contains(v, "@") && len(v) <= 320 {
			return v
		}
	}
	return ""
}

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
