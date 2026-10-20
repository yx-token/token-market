package listing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/crypto"
	"github.com/yx-token/idlicat/market/internal/platform/httpx"
	"github.com/yx-token/idlicat/market/internal/settings"
	"github.com/yx-token/idlicat/market/internal/sub2api/adminclient"
	"github.com/yx-token/idlicat/market/internal/sub2api/provision"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QualityHook 由质量检测模块实现。卖家可主动检测；有效授权与白名单允许未检测挂单，不能据此生成检测通过证据。
type QualityHook interface {
	StartOnboardingCheck(ctx context.Context, upstreamID int64, platform, authType string) error
}

// Kicker 通知调度器尽快重新计算期望状态。
type Kicker interface {
	Kick(ctx context.Context)
}

// thresholdPlatforms 是 sub2api 支持账号级窗口阈值（account_scheduling_threshold）的平台。
var thresholdPlatforms = map[string]bool{
	"openai": true, "anthropic": true, "grok": true, "kimi": true, "zhipu": true, "minimax": true, "opencode_go": true,
}

// SupportsSelfReserve 判断平台是否支持卖家自留比例。
func SupportsSelfReserve(platform string) bool { return thresholdPlatforms[platform] }

type Service struct {
	DB       *pgxpool.Pool
	Admin    *adminclient.Client
	Prov     *provision.Provisioner
	Keyring  *crypto.Keyring
	Quality  QualityHook
	Kicker   Kicker
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
	Now      func() time.Time

	modelLocksOnce sync.Once
	modelLockSlots chan struct{}
}

func (s *Service) kick(ctx context.Context) {
	if s.Kicker != nil {
		s.Kicker.Kick(ctx)
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var (
	ErrNoProxy       = httpx.Unavailable("NO_PROXY_AVAILABLE", "no proxy available, please contact the platform")
	ErrAlreadyListed = httpx.Conflict("ACCOUNT_ALREADY_LISTED", "this account is already on the platform")
	ErrBadState      = httpx.Conflict("INVALID_STATE", "operation not allowed in current state")
)

// CreateUpstream 创建上游草稿并分配专用代理。reference 为 true 时创建平台基线账号（仅管理员）。
// proxyFromImport 为 true 时暂不绑定代理，等导入账号时使用导出文件中的代理（检测连通后绑定）。
func (s *Service) CreateUpstream(ctx context.Context, sellerID int64, platform, authType string, reference bool, selectedProxyID *int64, proxyFromImport bool) (*Upstream, error) {
	v, err := GetVendor(ctx, s.DB, platform)
	if err != nil {
		return nil, err
	}
	if !v.Enabled && !reference {
		return nil, ErrVendorDisabled
	}
	if !v.SupportsAuthType(authType) {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", fmt.Sprintf("%s does not support %s", platform, authType))
	}
	if !IsOAuthAuthType(platform, authType) && !IsCodingAuthType(platform, authType) {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", "auth type not implemented")
	}
	st, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if selectedProxyID != nil && *selectedProxyID <= 0 {
		return nil, httpx.BadRequest("INVALID_PROXY", "proxy_id must be positive")
	}
	if proxyFromImport && (reference || selectedProxyID != nil) {
		return nil, httpx.BadRequest("INVALID_PROXY", "proxy_from_import cannot be combined with proxy_id")
	}
	if !reference && !proxyFromImport && st.RequireSellerProxy && selectedProxyID == nil {
		return nil, httpx.BadRequest("SELLER_PROXY_REQUIRED", "please configure and select your own proxy before adding an account")
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO upstreams (seller_id, platform, auth_type, is_reference) VALUES ($1, $2, $3, $4) RETURNING id`,
		sellerID, platform, authType, reference).Scan(&id); err != nil {
		return nil, err
	}
	var proxyID int64
	if selectedProxyID != nil {
		p, err := scanProxy(tx.QueryRow(ctx, "SELECT "+proxyColumns+" FROM proxies WHERE id = $1 AND seller_id = $2 FOR UPDATE", *selectedProxyID, sellerID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.NotFound("PROXY_NOT_FOUND", "proxy not found")
		}
		if err != nil {
			return nil, err
		}
		if p.Status != "available" || p.AssignedUpstreamID != nil {
			return nil, httpx.Conflict("PROXY_IN_USE", "proxy is assigned or disabled")
		}
		if p.Sub2APIProxyID == nil {
			return nil, httpx.Conflict("PROXY_NOT_SYNCED", "synchronize the proxy before selecting it")
		}
		if p.ConnectionReport == nil || !p.ConnectionReport.Success {
			return nil, httpx.Conflict("PROXY_NOT_CONNECTED", "test the proxy connection successfully before selecting it")
		}
		proxyID = p.ID
	} else if !proxyFromImport {
		err = tx.QueryRow(ctx, `SELECT id FROM proxies WHERE seller_id IS NULL AND status = 'available'
			AND assigned_upstream_id IS NULL AND sub2api_proxy_id IS NOT NULL
			ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&proxyID)
	}
	switch {
	case proxyFromImport:
		// 导入账号时再登记、检测并绑定导出文件中的代理。
	case err == nil:
		if _, err := tx.Exec(ctx, `UPDATE proxies SET status = 'assigned', assigned_upstream_id = $2 WHERE id = $1`, proxyID, id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE upstreams SET proxy_id = $2 WHERE id = $1`, id, proxyID); err != nil {
			return nil, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !st.AllowNoProxy {
			return nil, ErrNoProxy
		}
	default:
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	u, err := GetUpstream(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	// 探测分组失败不影响创建，开始授权时会重试
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		s.Log.Warn("ensure probe group", "upstream_id", id, "err", err)
	}
	return GetUpstream(ctx, s.DB, id)
}

// ensureProbeGroup 确保上游的探测分组（只含该账号的独占分组）存在。
func (s *Service) ensureProbeGroup(ctx context.Context, u *Upstream) error {
	if u.ProbeGroupID != nil {
		return nil
	}
	gid, err := s.Prov.EnsureGroup(ctx, provision.GroupSpec{
		Name:           ProbeGroupName(u.ID),
		Platform:       u.Platform,
		RateMultiplier: 1,
		Exclusive:      true,
		Description:    "token-market probe group",
	})
	if err != nil {
		return err
	}
	if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET probe_group_id = $2, updated_at = now() WHERE id = $1", u.ID, gid); err != nil {
		return err
	}
	u.ProbeGroupID = &gid
	return nil
}

// ProbeGroupName / AccountName 是本系统在 sub2api 中的资源命名，状态对账依赖这些前缀。
func ProbeGroupName(upstreamID int64) string  { return fmt.Sprintf("tm-probe-%d", upstreamID) }
func AccountName(upstreamID int64) string     { return fmt.Sprintf("tm-u-%d", upstreamID) }
func ListingGroupName(listingID int64) string { return fmt.Sprintf("tm-l-%d", listingID) }

// proxySub2APIID 返回上游绑定代理在 sub2api 中的 ID。尚未创建账号的上游没有绑定代理时（例如等待导入文件带入代理），
// 必须符合当前代理策略，不能借此绕过卖家代理必填直接授权或提交 key。
func (s *Service) proxySub2APIID(ctx context.Context, u *Upstream) (*int64, error) {
	if u.ProxyID == nil {
		if u.Sub2APIAccountID != nil {
			return nil, nil
		}
		st, err := s.Settings(ctx)
		if err != nil {
			return nil, err
		}
		if !u.IsReference && st.RequireSellerProxy {
			return nil, httpx.BadRequest("SELLER_PROXY_REQUIRED", "this upstream has no proxy; import an account that carries a reachable proxy")
		}
		if !st.AllowNoProxy {
			return nil, ErrNoProxy
		}
		return nil, nil
	}
	var id *int64
	if err := s.DB.QueryRow(ctx, "SELECT sub2api_proxy_id FROM proxies WHERE id = $1", *u.ProxyID).Scan(&id); err != nil {
		return nil, err
	}
	return id, nil
}

type AuthorizationStart struct {
	AuthURL   string    `json:"auth_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AuthorizationOptions 是开始授权时卖家可选填写的参数。
type AuthorizationOptions struct {
	// ProjectID 是 Gemini Code Assist 所在的 GCP 项目；留空时由 sub2api 自动探测（付费版通常需要填写）。
	ProjectID string `json:"project_id"`
}

// gcpProjectID 匹配 GCP 项目 ID（可带旧式的域名前缀，例如 example.com:my-project）。
var gcpProjectID = regexp.MustCompile(`^([a-z0-9.-]+:)?[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

func (o AuthorizationOptions) validate(spec oauthSpec) (AuthorizationOptions, error) {
	o.ProjectID = strings.TrimSpace(o.ProjectID)
	if o.ProjectID == "" {
		return o, nil
	}
	if !spec.projectID {
		return o, httpx.BadRequest("PROJECT_ID_NOT_SUPPORTED", "this authorization type does not take a project id")
	}
	if !gcpProjectID.MatchString(o.ProjectID) {
		return o, httpx.BadRequest("INVALID_PROJECT_ID", "invalid GCP project id")
	}
	return o, nil
}

// StartAuthorization 生成厂商授权链接。卖家在官方页面登录后把回调地址或授权码提交回来。
func (s *Service) StartAuthorization(ctx context.Context, sellerID, upstreamID int64, opts AuthorizationOptions) (*AuthorizationStart, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_OAUTH", "this upstream is not authorized via oauth")
	}
	if opts, err = opts.validate(spec); err != nil {
		return nil, err
	}
	if u.Status != StatusDraft && u.Status != StatusAuthorizing && u.Status != StatusFailed {
		return nil, ErrBadState
	}
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		return nil, fmt.Errorf("prepare probe group: %w", err)
	}
	st, err := s.beginOAuth(ctx, u, spec, opts)
	if err != nil {
		return nil, err
	}
	if err := setUpstreamStatus(ctx, s.DB, u.ID, StatusAuthorizing, nil); err != nil {
		return nil, err
	}
	return st, nil
}

// StartReauthorization 授权失效（令牌被撤销、密码修改等）时重新授权：沿用原 sub2api 账号，
// 换到新令牌后只更新凭证，上游状态、挂单与质量记录都保持不变。必须是同一个官方账号。
func (s *Service) StartReauthorization(ctx context.Context, sellerID, upstreamID int64, opts AuthorizationOptions) (*AuthorizationStart, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_OAUTH", "this upstream is not authorized via oauth")
	}
	if opts, err = opts.validate(spec); err != nil {
		return nil, err
	}
	if u.Sub2APIAccountID == nil || u.Status == StatusRemoving || u.Status == StatusRemoved {
		return nil, ErrBadState
	}
	return s.beginOAuth(ctx, u, spec, opts)
}

// beginOAuth 向 sub2api 申请授权链接并保存授权会话（30 分钟有效）。
func (s *Service) beginOAuth(ctx context.Context, u *Upstream, spec oauthSpec, opts AuthorizationOptions) (*AuthorizationStart, error) {
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	start, err := startOAuth(ctx, s.Admin, spec, proxyID, opts.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("start oauth: %w", err)
	}
	expires := s.now().Add(30 * time.Minute)
	if _, err := s.DB.Exec(ctx, `INSERT INTO upstream_oauth_sessions (upstream_id, session_id, state, auth_type, auth_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (upstream_id) DO UPDATE SET session_id = EXCLUDED.session_id, state = EXCLUDED.state,
			auth_type = EXCLUDED.auth_type, auth_url = EXCLUDED.auth_url, expires_at = EXCLUDED.expires_at, created_at = now()`,
		u.ID, start.SessionID, start.State, u.AuthType, start.AuthURL, expires); err != nil {
		return nil, err
	}
	return &AuthorizationStart{AuthURL: start.AuthURL, ExpiresAt: expires}, nil
}

// CompleteAuthorization 用卖家提交的授权码换取令牌并在 sub2api 中创建账号；
// 已有账号的上游（重新授权）只更新凭证。
func (s *Service) CompleteAuthorization(ctx context.Context, sellerID, upstreamID int64, input string) (*Upstream, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_OAUTH", "this upstream is not authorized via oauth")
	}
	reauth := u.Sub2APIAccountID != nil
	if !reauth && u.Status != StatusAuthorizing {
		return nil, ErrBadState
	}
	var sessionID string
	var state *string
	var expires time.Time
	err = s.DB.QueryRow(ctx, "SELECT session_id, state, expires_at FROM upstream_oauth_sessions WHERE upstream_id = $1", u.ID).
		Scan(&sessionID, &state, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && s.now().After(expires)) {
		return nil, httpx.BadRequest("AUTH_SESSION_EXPIRED", "authorization session expired, please start again")
	}
	if err != nil {
		return nil, err
	}
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	storedState := ""
	if state != nil {
		storedState = *state
	}
	tok, err := exchangeOAuth(ctx, s.Admin, spec, sessionID, storedState, input, proxyID)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			return nil, err
		}
		return nil, httpx.BadRequest("AUTH_EXCHANGE_FAILED", "authorization failed: "+err.Error())
	}
	if reauth {
		return s.applyReauthorization(ctx, u, spec, tok)
	}

	if id := spec.identity(tok); id != "" {
		if err := s.claimIdentity(ctx, u.ID, u.Platform+":"+id); err != nil {
			return nil, err
		}
	}
	if err := s.saveAccountProfile(ctx, u.ID, spec, tok); err != nil {
		return nil, err
	}
	if err := s.createAccount(ctx, u, spec.accountType, spec.credentials(tok), spec.extra(tok), proxyID); err != nil {
		_ = setUpstreamStatus(ctx, s.DB, u.ID, StatusFailed, strPtr("account creation failed"))
		return nil, err
	}
	_, _ = s.DB.Exec(ctx, "DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1", u.ID)
	return s.afterAccountCreated(ctx, u.ID, true)
}

func (s *Service) applyReauthorization(ctx context.Context, u *Upstream, spec oauthSpec, tok map[string]any) (*Upstream, error) {
	u, err := GetUpstream(ctx, s.DB, u.ID)
	if err != nil {
		return nil, err
	}
	if u.Sub2APIAccountID == nil || u.Status == StatusRemoving || u.Status == StatusRemoved {
		return nil, ErrBadState
	}
	if id := spec.identity(tok); id != "" {
		var current *string
		if err := s.DB.QueryRow(ctx, "SELECT identity_hash FROM upstreams WHERE id = $1", u.ID).Scan(&current); err != nil {
			return nil, err
		}
		if current != nil && *current != s.Keyring.BlindIndex("upstream-identity:"+u.Platform+":"+id) {
			return nil, httpx.Conflict("IDENTITY_MISMATCH", "please authorize the same account that was originally listed")
		}
	}
	creds := spec.credentials(tok)
	// sub2api 更新凭证是整体替换（敏感键除外），窗口阈值存放在凭证里，需要一并带上
	if SupportsSelfReserve(u.Platform) {
		creds["account_scheduling_threshold"] = 100 - u.SelfReservePercent
	}
	// 映射是非敏感凭证键，PUT 不会自动保留；与新令牌一起带上，避免出现不限模型的窗口。
	if u.ModelWhitelist != nil {
		creds["model_mapping"] = modelMapping(u.ModelWhitelist)
	}
	if _, err := s.Admin.UpdateAccount(ctx, *u.Sub2APIAccountID, adminclient.UpdateAccountInput{Credentials: creds}); err != nil {
		return nil, fmt.Errorf("update credentials: %w", err)
	}
	if err := s.Admin.SetAccountModelWhitelist(ctx, *u.Sub2APIAccountID, u.ModelWhitelist); err != nil {
		return nil, fmt.Errorf("重新授权后同步挂售模型：%w", err)
	}
	if err := s.Admin.ClearError(ctx, *u.Sub2APIAccountID); err != nil {
		s.Log.Warn("clear account error after reauthorization", "upstream_id", u.ID, "err", err)
	}
	if err := s.saveAccountProfile(ctx, u.ID, spec, tok); err != nil {
		return nil, err
	}
	if privacyPlatforms[u.Platform] && u.AuthType == "oauth" {
		ok := false
		if acc, err := s.Admin.SetPrivacy(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Warn("set privacy after reauthorization", "upstream_id", u.ID, "err", err)
		} else if mode, _ := acc.Extra["privacy_mode"].(string); mode != "" {
			ok = true
		}
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET privacy_ok = $2 WHERE id = $1", u.ID, ok); err != nil {
			return nil, err
		}
	}
	_, _ = s.DB.Exec(ctx, "DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1", u.ID)
	s.kick(ctx) // 账号恢复正常后由调度器同步健康状态
	return GetUpstream(ctx, s.DB, u.ID)
}

// CodingKeyInput 是卖家提交的 Coding Plan key；Organization / Project 只用于智谱团队版。
type CodingKeyInput struct {
	APIKey       string `json:"api_key"`
	Organization string `json:"organization"`
	Project      string `json:"project"`
}

// teamID 匹配智谱团队版的组织 / 项目 ID。
var teamID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// SubmitCodingKey 接入国产 Coding Plan：用 key 创建账号，并向官方查询窗口用量确认确实是订阅 key。
func (s *Service) SubmitCodingKey(ctx context.Context, sellerID, upstreamID int64, in CodingKeyInput) (*Upstream, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.submitCodingKey(ctx, sellerID, upstreamID, in)
}

// submitCodingKey 由已经持有上游锁的直接接入或账号导入调用，避免重复加锁。
func (s *Service) submitCodingKey(ctx context.Context, sellerID, upstreamID int64, in CodingKeyInput) (*Upstream, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := codingSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_CODING_PLAN", "this upstream is not a coding plan")
	}
	if u.Status != StatusDraft && u.Status != StatusFailed {
		return nil, ErrBadState
	}
	apiKey := strings.TrimSpace(in.APIKey)
	if len(apiKey) < 16 {
		return nil, httpx.BadRequest("INVALID_KEY", "api key looks invalid")
	}
	org, project := strings.TrimSpace(in.Organization), strings.TrimSpace(in.Project)
	if (org != "" || project != "") && !spec.team {
		return nil, httpx.BadRequest("TEAM_NOT_SUPPORTED", "this coding plan has no team edition")
	}
	if project != "" && org == "" {
		return nil, httpx.BadRequest("ORGANIZATION_REQUIRED", "organization id is required for a team plan")
	}
	for _, v := range []string{org, project} {
		if v != "" && !teamID.MatchString(v) {
			return nil, httpx.BadRequest("INVALID_TEAM_ID", "invalid organization or project id")
		}
	}
	// 先检查代理策略，未通过时不能占用账号身份。
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		return nil, fmt.Errorf("prepare probe group: %w", err)
	}
	if err := s.claimIdentity(ctx, u.ID, u.Platform+":key:"+apiKey); err != nil {
		return nil, err
	}
	// 推理地址只用官方固定地址（国内站留空由 sub2api 按 account_mode 选默认），防止中转 key 冒充订阅
	creds := map[string]any{"api_key": apiKey, "account_mode": spec.mode}
	if spec.baseURL != "" {
		creds["base_url"] = spec.baseURL
	}
	if org != "" {
		creds["zhipu_organization"] = org
		if project != "" {
			creds["zhipu_project"] = project
		}
	}
	if err := s.createAccount(ctx, u, "apikey", creds, map[string]any{}, proxyID); err != nil {
		_ = setUpstreamStatus(ctx, s.DB, u.ID, StatusFailed, strPtr("account creation failed"))
		return nil, err
	}
	u, err = GetUpstream(ctx, s.DB, u.ID)
	if err != nil {
		return nil, err
	}
	quota, qerr := s.Admin.CNProviderQuota(ctx, *u.Sub2APIAccountID)
	if qerr != nil || !quota.Success || !quota.CredentialValid || len(quota.Tiers) == 0 {
		reason := "cannot read official coding plan quota; only subscription keys are accepted"
		if qerr != nil {
			s.Log.Warn("coding plan quota check failed", "upstream_id", u.ID, "err", qerr)
		}
		if err := s.Admin.DeleteAccount(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Error("delete rejected coding plan account", "upstream_id", u.ID, "err", err)
		}
		_, _ = s.DB.Exec(ctx, `UPDATE upstreams SET status = 'failed', failure_reason = $2, sub2api_account_id = NULL,
			identity_hash = NULL, updated_at = now() WHERE id = $1`, u.ID, reason)
		return nil, httpx.BadRequest("NOT_SUBSCRIPTION_KEY", reason)
	}
	if quota.PlanLevel != "" {
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET tier = $2 WHERE id = $1", u.ID, quota.PlanLevel); err != nil {
			return nil, err
		}
	}
	return s.afterAccountCreated(ctx, u.ID, true)
}

// claimIdentity 记录官方账号身份，同一账号不能被重复挂到平台。
func (s *Service) claimIdentity(ctx context.Context, upstreamID int64, identity string) error {
	h := s.Keyring.BlindIndex("upstream-identity:" + identity)
	_, err := s.DB.Exec(ctx, "UPDATE upstreams SET identity_hash = $2, updated_at = now() WHERE id = $1", upstreamID, h)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAlreadyListed
	}
	return err
}

// saveAccountProfile 保存从令牌信息中识别出的订阅档位与官方账号邮箱，识别不到的项保持原值。
func (s *Service) saveAccountProfile(ctx context.Context, upstreamID int64, spec oauthSpec, tok map[string]any) error {
	tier, email := spec.tier(tok), AccountEmail(tok)
	if tier == "" && email == "" {
		return nil
	}
	_, err := s.DB.Exec(ctx, `UPDATE upstreams SET tier = COALESCE(NULLIF($2, ''), tier),
		account_email = COALESCE(NULLIF($3, ''), account_email) WHERE id = $1`, upstreamID, tier, email)
	return err
}

func (s *Service) createAccount(ctx context.Context, u *Upstream, accountType string, creds, extra map[string]any, proxyID *int64) error {
	v, err := GetVendor(ctx, s.DB, u.Platform)
	if err != nil {
		return err
	}
	if u.ProbeGroupID == nil {
		return errors.New("probe group missing")
	}
	// 初始就带上厂商的安全限流参数，避免调度器第一次下发前出现无限制的窗口
	for k, val := range v.ExtraLimits {
		extra[k] = val
	}
	if u.SelfReservePercent > 0 && SupportsSelfReserve(u.Platform) {
		creds["account_scheduling_threshold"] = 100 - u.SelfReservePercent
	}
	// 授权与导入都只使用平台保存的白名单，不接收导出文件里的别名映射和透传配置。
	delete(creds, "model_mapping")
	if u.ModelWhitelist != nil {
		creds["model_mapping"] = modelMapping(u.ModelWhitelist)
	}
	notes := fmt.Sprintf("tm:upstream:%d", u.ID)
	rate := 1.0
	accountID, err := s.Prov.EnsureAccount(ctx, adminclient.CreateAccountInput{
		Name:           AccountName(u.ID),
		Notes:          &notes,
		Platform:       u.Platform,
		Type:           accountType,
		Credentials:    creds,
		Extra:          extra,
		ProxyID:        proxyID,
		Concurrency:    v.DefaultConcurrency,
		Priority:       99,
		RateMultiplier: &rate,
		GroupIDs:       []int64{*u.ProbeGroupID},
	}, fmt.Sprintf("tm-account-upstream-%d", u.ID))
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, "UPDATE upstreams SET sub2api_account_id = $2, updated_at = now() WHERE id = $1", u.ID, accountID)
	return err
}

// afterAccountCreated 先缓存模型目录，再等待新卖家账号选择挂售模型。
func (s *Service) afterAccountCreated(ctx context.Context, upstreamID int64, requireModelSelection bool) (*Upstream, error) {
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	if requireModelSelection && !u.IsReference && u.ModelWhitelist == nil {
		if err := s.transitionAccountStatus(ctx, u, StatusSelectingModels, nil); err != nil {
			return nil, err
		}
	}
	// 新账号必须在写入白名单前获取完整目录；获取失败时保留空目录，允许卖家手动填写。
	if u.ModelCatalogAt == nil {
		if err := s.fetchModelCatalog(ctx, u); err != nil {
			var he *httpx.Error
			if !errors.As(err, &he) || he.Code != "MODEL_CATALOG_UNAVAILABLE" {
				return nil, err
			}
			s.Log.Warn("获取上游模型目录失败", "upstream_id", u.ID, "err", err)
		}
	}
	// 基线账号和存量未声明白名单的检测重试仍不限模型；新接入须完成模型选择。
	if u.IsReference || u.ModelWhitelist != nil || !requireModelSelection {
		if err := s.Admin.SetAccountModelWhitelist(ctx, *u.Sub2APIAccountID, u.ModelWhitelist); err != nil {
			return nil, fmt.Errorf("接入后同步挂售模型：%w", err)
		}
		if u.IsReference {
			return s.startVerification(ctx, u)
		}
		return s.prepareAuthorizedListing(ctx, u)
	}
	return GetUpstream(ctx, s.DB, u.ID)
}

// transitionAccountStatus 只推进本次读取的状态，防止并发删除或冻结被接入流程覆盖。
func (s *Service) transitionAccountStatus(ctx context.Context, u *Upstream, next string, reason *string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE upstreams SET status = $2, failure_reason = $3, updated_at = now()
		WHERE id = $1 AND status = $4`, u.ID, next, reason, u.Status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrBadState
	}
	u.Status = next
	return nil
}

// failStartingVerification 即使请求已取消，也让未能入队的上游回到可重试状态。
func (s *Service) failStartingVerification(ctx context.Context, upstreamID int64, reason string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return MarkVerified(cleanup, s.DB, upstreamID, false, reason)
}

// ensureListingPrivacy 保留授权流程中的隐私保护，跳过质检不能跳过此步骤。
func (s *Service) ensureListingPrivacy(ctx context.Context, u *Upstream) (bool, error) {
	// 新账号先保存白名单；隐私设置失败后的重试不会绕过首次模型选择。
	if privacyPlatforms[u.Platform] && u.AuthType == "oauth" {
		ok := false
		if acc, err := s.Admin.SetPrivacy(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Warn("set privacy failed", "upstream_id", u.ID, "err", err)
		} else if mode, _ := acc.Extra["privacy_mode"].(string); mode != "" {
			ok = true
		}
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET privacy_ok = $2 WHERE id = $1", u.ID, ok); err != nil {
			return false, err
		}
		if !ok {
			if err := s.transitionAccountStatus(ctx, u, StatusVerifyFailed, strPtr("cannot disable training data usage on this account")); err != nil {
				return false, err
			}
			return false, nil
		}
	}
	return true, nil
}

// requireAuthorizedAccount 阻止已撤销/停用的远端账号借未检测路径上架。
func (s *Service) requireAuthorizedAccount(ctx context.Context, u *Upstream) error {
	if u.Sub2APIAccountID == nil {
		return ErrBadState
	}
	account, err := s.Admin.GetAccount(ctx, *u.Sub2APIAccountID)
	if err != nil {
		return err
	}
	if account.Status != "active" {
		return httpx.Conflict("AUTHORIZATION_UNAVAILABLE", "upstream authorization is unavailable; authorize an active account before listing")
	}
	return nil
}

// prepareAuthorizedListing 只将刚完成授权、模型选择或隐私重试的账号置为 ready。
// 不改变 health、grade、Q 或任何质检记录，不能复活冻结、移除或已判定不健康账号。
func (s *Service) prepareAuthorizedListing(ctx context.Context, u *Upstream) (*Upstream, error) {
	if u.IsReference || u.Sub2APIAccountID == nil || len(u.ModelWhitelist) == 0 ||
		(u.Status != StatusSelectingModels && u.Status != StatusVerifyFailed && u.Status != StatusFailed && u.Status != StatusAuthorizing && u.Status != StatusDraft) ||
		u.Health == HealthDegraded || u.Health == HealthUnavailable {
		return nil, ErrBadState
	}
	v, err := GetVendor(ctx, s.DB, u.Platform)
	if err != nil {
		return nil, err
	}
	if !v.Enabled {
		return nil, ErrVendorDisabled
	}
	if err := s.requireAuthorizedAccount(ctx, u); err != nil {
		return nil, err
	}
	ok, err := s.ensureListingPrivacy(ctx, u)
	if err != nil {
		return nil, err
	}
	if !ok {
		return GetUpstream(ctx, s.DB, u.ID)
	}
	if err := s.transitionAccountStatus(ctx, u, StatusReady, nil); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetUpstream(ctx, s.DB, u.ID)
}

func (s *Service) startVerification(ctx context.Context, u *Upstream) (*Upstream, error) {
	ok, err := s.ensureListingPrivacy(ctx, u)
	if err != nil {
		return nil, err
	}
	if !ok {
		return GetUpstream(ctx, s.DB, u.ID)
	}
	if err := s.transitionAccountStatus(ctx, u, StatusVerifying, nil); err != nil {
		// 更新响应丢失时也尝试清理；条件更新不会覆盖删除、冻结或已经完成的检测。
		if errors.Is(err, ErrBadState) {
			return nil, err
		}
		return nil, errors.Join(err, s.failStartingVerification(ctx, u.ID, "上架检测暂时无法开始，请重试"))
	}
	var startErr error
	if s.Quality == nil {
		startErr = errors.New("未配置质量检测引擎")
	} else {
		startErr = s.Quality.StartOnboardingCheck(ctx, u.ID, u.Platform, u.AuthType)
	}
	if startErr != nil {
		var he *httpx.Error
		reason := "上架检测暂时无法开始，请重试"
		if errors.As(startErr, &he) {
			// 沿用既有策略错误的失败原因格式，保持检测接口兼容。
			reason = "onboarding check could not start: " + he.Message
		}
		cleanupErr := s.failStartingVerification(ctx, u.ID, reason)
		if he == nil || cleanupErr != nil {
			return nil, errors.Join(startErr, cleanupErr)
		}
	}
	return GetUpstream(ctx, s.DB, u.ID)
}

// RetryVerification 在 ready 时主动检测，或在上次检测失败后重新检测。
func (s *Service) RetryVerification(ctx context.Context, sellerID, upstreamID int64) (*Upstream, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if (u.Status != StatusVerifyFailed && u.Status != StatusReady) || u.Sub2APIAccountID == nil {
		return nil, ErrBadState
	}
	if err := s.Admin.SetAccountModelWhitelist(ctx, *u.Sub2APIAccountID, u.ModelWhitelist); err != nil {
		return nil, fmt.Errorf("检测前同步挂售模型：%w", err)
	}
	return s.startVerification(ctx, u)
}

// SetSelfReserve 设置卖家自留比例（窗口用量达到 100 − 自留比例 时暂停调度）。
func (s *Service) SetSelfReserve(ctx context.Context, sellerID, upstreamID int64, percent int) (*Upstream, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if percent < 0 || percent > 90 {
		return nil, httpx.BadRequest("INVALID_PERCENT", "self reserve must be between 0 and 90")
	}
	if percent > 0 && !SupportsSelfReserve(u.Platform) {
		return nil, httpx.BadRequest("UNSUPPORTED", "this vendor does not support self reserve; the whole account is sold")
	}
	if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET self_reserve_percent = $2, updated_at = now() WHERE id = $1", u.ID, percent); err != nil {
		return nil, err
	}
	s.kick(ctx)
	return GetUpstream(ctx, s.DB, u.ID)
}

// Remove 删除上游：关闭挂单并交给调度器从 sub2api 移除账号。
func (s *Service) Remove(ctx context.Context, sellerID, upstreamID int64) error {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return err
	}
	defer release()
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return err
	}
	return s.remove(ctx, u)
}

func (s *Service) remove(ctx context.Context, u *Upstream) error {
	if u.Status == StatusRemoving || u.Status == StatusRemoved {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE listings SET status = 'closed', closed_at = now(), updated_at = now()
		WHERE upstream_id = $1 AND closed_at IS NULL`, u.ID); err != nil {
		return err
	}
	next := StatusRemoving
	if u.Sub2APIAccountID == nil {
		next = StatusRemoved
		if _, err := tx.Exec(ctx, `UPDATE proxies SET status = 'available', assigned_upstream_id = NULL WHERE assigned_upstream_id = $1`, u.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE upstreams SET status = $2, identity_hash = NULL, updated_at = now(),
		removed_at = CASE WHEN $2 = 'removed' THEN now() ELSE removed_at END WHERE id = $1`, u.ID, next); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.kick(ctx)
	return nil
}

// AdminSetSuspended 管理员冻结或解冻上游。冻结后不再面向买家。
func (s *Service) AdminSetSuspended(ctx context.Context, upstreamID int64, suspend bool, reason string) (*Upstream, error) {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	defer release()
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	switch {
	case suspend && u.Status != StatusSuspended:
		if _, err := s.DB.Exec(ctx, `UPDATE upstreams SET status = 'suspended', failure_reason = $2, updated_at = now() WHERE id = $1`, u.ID, reason); err != nil {
			return nil, err
		}
	case !suspend && u.Status == StatusSuspended:
		next := StatusReady
		if l, err := OpenListing(ctx, s.DB, u.ID); err != nil {
			return nil, err
		} else if l != nil {
			next = StatusListed
		}
		if _, err := s.DB.Exec(ctx, `UPDATE upstreams SET status = $2, failure_reason = NULL, updated_at = now() WHERE id = $1`, u.ID, next); err != nil {
			return nil, err
		}
	}
	s.kick(ctx)
	return GetUpstream(ctx, s.DB, u.ID)
}

// AdminRemove 管理员强制删除上游。
func (s *Service) AdminRemove(ctx context.Context, upstreamID int64) error {
	release, err := s.lockAccountModels(ctx, upstreamID)
	if err != nil {
		return err
	}
	defer release()
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return err
	}
	return s.remove(ctx, u)
}

// MarkVerified 由质量模块在上架检测通过后调用。
func MarkVerified(ctx context.Context, db DB, upstreamID int64, passed bool, reason string) error {
	if passed {
		_, err := db.Exec(ctx, `UPDATE upstreams SET status = 'ready', failure_reason = NULL, updated_at = now()
			WHERE id = $1 AND status = 'verifying'`, upstreamID)
		return err
	}
	_, err := db.Exec(ctx, `UPDATE upstreams SET status = 'verify_failed', failure_reason = $2, updated_at = now()
		WHERE id = $1 AND status = 'verifying'`, upstreamID, reason)
	return err
}
