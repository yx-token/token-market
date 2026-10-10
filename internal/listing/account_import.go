package listing

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DueGin/token-market/internal/platform/httpx"
)

// ImportAccountInput 支持 sub2api 导出文件、账号数组、单个账号及 credentials 对象。
// 多账号文件用 account_index 选择本次接入的账号，始终沿用上游已绑定的专用代理。
type ImportAccountInput struct {
	Data         json.RawMessage `json:"data"`
	AccountIndex int             `json:"account_index"`
}

type importedAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
}

func parseImportedAccount(in ImportAccountInput) (*importedAccount, error) {
	return parseImportedAccountDepth(in, 0)
}

func parseImportedAccountDepth(in ImportAccountInput, depth int) (*importedAccount, error) {
	bad := func() (*importedAccount, error) {
		return nil, httpx.BadRequest("INVALID_ACCOUNT_IMPORT", "invalid sub2api account data or account index")
	}
	raw := json.RawMessage(strings.TrimSpace(string(in.Data)))
	if len(raw) == 0 || in.AccountIndex < 0 || depth > 2 {
		return bad()
	}
	var accounts []json.RawMessage
	if raw[0] == '[' {
		if json.Unmarshal(raw, &accounts) != nil {
			return bad()
		}
	} else {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return bad()
		}
		if wrapped, ok := obj["data"]; ok {
			return parseImportedAccountDepth(ImportAccountInput{Data: wrapped, AccountIndex: in.AccountIndex}, depth+1)
		}
		if list, ok := obj["accounts"]; ok {
			var kind string
			var version int
			if v, ok := obj["type"]; ok {
				if json.Unmarshal(v, &kind) != nil || (kind != "" && kind != "sub2api-data" && kind != "sub2api-bundle") {
					return bad()
				}
			}
			if v, ok := obj["version"]; ok {
				if json.Unmarshal(v, &version) != nil || (version != 0 && version != 1) {
					return bad()
				}
			}
			if json.Unmarshal(list, &accounts) != nil {
				return bad()
			}
		} else {
			accounts = []json.RawMessage{raw}
		}
	}
	if in.AccountIndex >= len(accounts) {
		return bad()
	}
	var account importedAccount
	if json.Unmarshal(accounts[in.AccountIndex], &account) != nil {
		return bad()
	}
	if account.Credentials == nil {
		// 只有授权信息时允许直接提交 credentials；带账号字段但缺凭证的记录不能被当成凭证。
		if account.Platform != "" || account.Type != "" || account.Name != "" {
			return bad()
		}
		if json.Unmarshal(accounts[in.AccountIndex], &account.Credentials) != nil {
			return bad()
		}
	}
	if len(account.Credentials) == 0 {
		return bad()
	}
	return &account, nil
}

func (a *importedAccount) validate(platform, authType string) error {
	if a.Platform != "" && a.Platform != platform {
		return httpx.BadRequest("IMPORT_ACCOUNT_MISMATCH", "account platform does not match the selected upstream")
	}
	wantType := "apikey"
	if spec, ok := oauthSpecs[platform][authType]; ok {
		wantType = spec.accountType
	}
	if a.Type != "" && a.Type != wantType {
		return httpx.BadRequest("IMPORT_ACCOUNT_MISMATCH", "account type does not match the selected authorization method")
	}
	// 必须是字符串令牌，避免把错误的对象或数字保存成可用凭证。
	for _, key := range []string{"access_token", "refresh_token", "api_key"} {
		if val, ok := a.Credentials[key]; ok {
			v, valid := val.(string)
			if !valid {
				return httpx.BadRequest("INVALID_IMPORT_CREDENTIALS", "tokens must be strings")
			}
			a.Credentials[key] = strings.TrimSpace(v)
		}
	}
	if wantType == "apikey" {
		if str(a.Credentials, "api_key") == "" {
			return httpx.BadRequest("INVALID_IMPORT_CREDENTIALS", "api_key is required")
		}
	} else if str(a.Credentials, "access_token") == "" {
		return httpx.BadRequest("INVALID_IMPORT_CREDENTIALS", "access_token is required")
	}
	if platform == "gemini" {
		expected := "code_assist"
		if authType == "google_one" {
			expected = "google_one"
		}
		if actual := str(a.Credentials, "oauth_type"); actual != "" && actual != expected {
			return httpx.BadRequest("IMPORT_ACCOUNT_MISMATCH", "Gemini subscription type does not match")
		}
		a.Credentials["oauth_type"] = expected
	}
	return nil
}

func (s *Service) ImportAccount(ctx context.Context, sellerID, upstreamID int64, in ImportAccountInput) (*Upstream, error) {
	// 同一上游的导入串行执行，防止并发提交两份凭证争用账号与身份记录。
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", -upstreamID).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, ErrBadState
	}
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if u.Sub2APIAccountID != nil || (u.Status != StatusDraft && u.Status != StatusAuthorizing && u.Status != StatusFailed) {
		return nil, ErrBadState
	}
	vendor, err := GetVendor(ctx, s.DB, u.Platform)
	if err != nil {
		return nil, err
	}
	if !vendor.Enabled {
		return nil, ErrVendorDisabled
	}
	if !vendor.SupportsAuthType(u.AuthType) {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", "this authorization method is not open")
	}
	a, err := parseImportedAccount(in)
	if err != nil {
		return nil, err
	}
	if err := a.validate(u.Platform, u.AuthType); err != nil {
		return nil, err
	}
	if IsCodingAuthType(u.Platform, u.AuthType) {
		// 沿用固定官方地址、团队参数校验及官方订阅额度校验。
		return s.SubmitCodingKey(ctx, sellerID, upstreamID, CodingKeyInput{
			APIKey: str(a.Credentials, "api_key"), Organization: str(a.Credentials, "zhipu_organization"), Project: str(a.Credentials, "zhipu_project"),
		})
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", "unsupported account type")
	}
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		return nil, err
	}
	// 仅复制授权及身份元数据，不接收导出文件的分组、倍率、路由和限流配置。
	tok := copyAll(a.Credentials)
	for _, k := range []string{"org_uuid", "account_uuid", "email_address", "email", "plan_type", "tier_id", "subscription_tier"} {
		if str(tok, k) == "" && a.Extra[k] != nil {
			tok[k] = a.Extra[k]
		}
	}
	creds := spec.credentials(tok)
	if u.Platform == "anthropic" {
		creds = pick("access_token", "refresh_token", "expires_at", "token_type", "scope", "org_uuid", "account_uuid", "email_address")(tok)
	}
	extra := spec.extra(tok)
	if u.Platform == "gemini" {
		extra = pick("email", "tier_id")(tok)
	}
	identity := spec.identity(tok)
	if identity == "" {
		identity = first("refresh_token", "access_token")(tok)
	}
	if err := s.claimIdentity(ctx, u.ID, u.Platform+":"+identity); err != nil {
		return nil, err
	}
	if tier := spec.tier(tok); tier != "" {
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET tier = $2 WHERE id = $1", u.ID, tier); err != nil {
			return nil, err
		}
	}
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	if err := s.createAccount(ctx, u, spec.accountType, creds, extra, proxyID); err != nil {
		_ = setUpstreamStatus(ctx, s.DB, u.ID, StatusFailed, strPtr("account import failed"))
		return nil, err
	}
	_, _ = s.DB.Exec(ctx, "DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1", u.ID)
	return s.afterAccountCreated(ctx, u.ID)
}
