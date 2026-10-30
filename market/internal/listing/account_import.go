package listing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// ImportAccountInput 支持 sub2api 导出文件、账号数组、单个账号及 credentials 对象。
// 多账号文件用 account_index 选择本次接入的账号。上游已绑定代理时沿用该代理；
// 未绑定时使用导出文件中该账号的代理，连通性检测通过后才会导入。
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
	ProxyKey    string         `json:"proxy_key"`
	// proxy 是导出文件 proxies 中 proxy_key 对应的代理，找不到时为空。
	proxy *importedProxy
}

// importedProxy 是 sub2api 导出文件中的代理，只取地址与认证信息。
type importedProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// key 与 sub2api 导入时的规则一致：缺少 proxy_key 时由地址和认证信息拼出。
func (p importedProxy) key() string {
	if p.ProxyKey != "" {
		return p.ProxyKey
	}
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(p.Protocol), strings.TrimSpace(p.Host), p.Port, strings.TrimSpace(p.Username), strings.TrimSpace(p.Password))
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
	var proxies []importedProxy
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
			if v, ok := obj["proxies"]; ok && json.Unmarshal(v, &proxies) != nil {
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
	if account.ProxyKey != "" {
		for i := range proxies {
			if proxies[i].key() == account.ProxyKey {
				account.proxy = &proxies[i]
				break
			}
		}
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
	// 导入、授权与模型选择共用连接锁，外部请求期间不占用数据库事务。
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
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
	if isCodingPlanAuthType(u.Platform, u.AuthType) && IsOAuthAuthType(u.Platform, u.AuthType) {
		return nil, httpx.BadRequest("IMPORT_OAUTH_UNSUPPORTED", "Coding Plan OAuth accounts must connect through official authorization, not account import")
	}
	a, err := parseImportedAccount(in)
	if err != nil {
		return nil, err
	}
	if err := a.validate(u.Platform, u.AuthType); err != nil {
		return nil, err
	}
	if u.ProxyID == nil && a.proxy != nil {
		if err := s.bindImportedProxy(ctx, u, a.proxy); err != nil {
			return nil, err
		}
	} else if u.ProxyID == nil && a.ProxyKey != "" {
		return nil, httpx.BadRequest("IMPORT_PROXY_NOT_FOUND", "the proxy referenced by this account is missing from the import file")
	}
	// 文件中没有代理时按平台代理策略决定能否无代理接入；须在占用账号身份之前检查。
	if _, err := s.proxySub2APIID(ctx, u); err != nil {
		return nil, err
	}
	if IsCodingAuthType(u.Platform, u.AuthType) {
		// 沿用固定官方地址、团队参数校验及官方订阅额度校验。
		return s.submitCodingKey(ctx, sellerID, upstreamID, CodingKeyInput{
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
	if err := s.saveAccountProfile(ctx, u.ID, spec, tok); err != nil {
		return nil, err
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
	return s.afterAccountCreated(ctx, u.ID, true)
}

// bindImportedProxy 把导出文件中的代理登记到卖家代理库存，实测连通后独占绑定到上游。
// 检测失败时代理留在库存中（附带失败报告），上游保持未绑定，可修复后重新导入。
func (s *Service) bindImportedProxy(ctx context.Context, u *Upstream, in *importedProxy) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = fmt.Sprintf("sub2api %s:%d", strings.TrimSpace(in.Host), in.Port)
	}
	proxies := &ProxyService{DB: s.DB, Admin: s.Admin, Keyring: s.Keyring}
	p, err := proxies.ensureSeller(ctx, u.SellerID, CreateProxyInput{
		Name: name, Protocol: strings.ToLower(strings.TrimSpace(in.Protocol)), Host: in.Host, Port: in.Port,
		Username: strings.TrimSpace(in.Username), Password: in.Password,
	})
	if err != nil {
		return err
	}
	if p.Status != "available" && p.Status != "assigned" {
		return httpx.Conflict("PROXY_IN_USE", "proxy is disabled")
	}
	if p, err = proxies.CheckProxy(ctx, &u.SellerID, p.ID, false); err != nil {
		return err
	}
	if p.ConnectionReport == nil || !p.ConnectionReport.Success {
		msg := "proxy connection test failed"
		if p.ConnectionReport != nil && p.ConnectionReport.Message != "" {
			msg += ": " + p.ConnectionReport.Message
		}
		return httpx.BadRequest("IMPORT_PROXY_UNREACHABLE", msg)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedID int64
	err = tx.QueryRow(ctx, `SELECT id FROM proxies WHERE id = $1 AND seller_id = $2
		AND status IN ('available', 'assigned') FOR UPDATE`, p.ID, u.SellerID).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.Conflict("PROXY_IN_USE", "proxy is disabled or unavailable")
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE upstreams SET proxy_id = $2 WHERE id = $1 AND proxy_id IS NULL AND status <> 'removed'`, u.ID, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBadState
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	u.ProxyID = &p.ID
	return nil
}
