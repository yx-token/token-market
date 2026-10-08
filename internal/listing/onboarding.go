package listing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/sub2api/provision"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QualityHook 由质量检测模块实现。首次配置的上游必须通过上架检测才能挂单、被调度（与厂商的检测开关无关，
// 开关只控制之后的检测）：账号创建后排队上架检测，上游保持 verifying，检测完成后由质量模块推进状态。
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
func (s *Service) CreateUpstream(ctx context.Context, sellerID int64, platform, authType string, reference bool) (*Upstream, error) {
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
	err = tx.QueryRow(ctx, `SELECT id FROM proxies WHERE status = 'available' AND sub2api_proxy_id IS NOT NULL
		ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&proxyID)
	switch {
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

func (s *Service) proxySub2APIID(ctx context.Context, u *Upstream) (*int64, error) {
	if u.ProxyID == nil {
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

// StartAuthorization 生成厂商授权链接。卖家在官方页面登录后把回调地址或授权码提交回来。
func (s *Service) StartAuthorization(ctx context.Context, sellerID, upstreamID int64) (*AuthorizationStart, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_OAUTH", "this upstream is not authorized via oauth")
	}
	if u.Status != StatusDraft && u.Status != StatusAuthorizing && u.Status != StatusFailed {
		return nil, ErrBadState
	}
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		return nil, fmt.Errorf("prepare probe group: %w", err)
	}
	st, err := s.beginOAuth(ctx, u, spec)
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
func (s *Service) StartReauthorization(ctx context.Context, sellerID, upstreamID int64) (*AuthorizationStart, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	spec, ok := oauthSpecs[u.Platform][u.AuthType]
	if !ok {
		return nil, httpx.BadRequest("NOT_OAUTH", "this upstream is not authorized via oauth")
	}
	if u.Sub2APIAccountID == nil || u.Status == StatusRemoving || u.Status == StatusRemoved {
		return nil, ErrBadState
	}
	return s.beginOAuth(ctx, u, spec)
}

// beginOAuth 向 sub2api 申请授权链接并保存授权会话（30 分钟有效）。
func (s *Service) beginOAuth(ctx context.Context, u *Upstream, spec oauthSpec) (*AuthorizationStart, error) {
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	start, err := startOAuth(ctx, s.Admin, spec, proxyID)
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
	if tier := spec.tier(tok); tier != "" {
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET tier = $2 WHERE id = $1", u.ID, tier); err != nil {
			return nil, err
		}
	}
	if err := s.createAccount(ctx, u, spec.accountType, spec.credentials(tok), spec.extra(tok), proxyID); err != nil {
		_ = setUpstreamStatus(ctx, s.DB, u.ID, StatusFailed, strPtr("account creation failed"))
		return nil, err
	}
	_, _ = s.DB.Exec(ctx, "DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1", u.ID)
	return s.afterAccountCreated(ctx, u.ID)
}

func (s *Service) applyReauthorization(ctx context.Context, u *Upstream, spec oauthSpec, tok map[string]any) (*Upstream, error) {
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
	if _, err := s.Admin.UpdateAccount(ctx, *u.Sub2APIAccountID, adminclient.UpdateAccountInput{Credentials: creds}); err != nil {
		return nil, fmt.Errorf("update credentials: %w", err)
	}
	if err := s.Admin.ClearError(ctx, *u.Sub2APIAccountID); err != nil {
		s.Log.Warn("clear account error after reauthorization", "upstream_id", u.ID, "err", err)
	}
	if tier := spec.tier(tok); tier != "" {
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET tier = $2 WHERE id = $1", u.ID, tier); err != nil {
			return nil, err
		}
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

// SubmitCodingKey 接入国产 Coding Plan：用 key 创建账号，并向官方查询窗口用量确认确实是订阅 key。
func (s *Service) SubmitCodingKey(ctx context.Context, sellerID, upstreamID int64, apiKey string) (*Upstream, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	mode, ok := codingPlatforms[u.Platform]
	if !ok || !IsCodingAuthType(u.Platform, u.AuthType) {
		return nil, httpx.BadRequest("NOT_CODING_PLAN", "this upstream is not a coding plan")
	}
	if u.Status != StatusDraft && u.Status != StatusFailed {
		return nil, ErrBadState
	}
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) < 16 {
		return nil, httpx.BadRequest("INVALID_KEY", "api key looks invalid")
	}
	if err := s.ensureProbeGroup(ctx, u); err != nil {
		return nil, fmt.Errorf("prepare probe group: %w", err)
	}
	if err := s.claimIdentity(ctx, u.ID, u.Platform+":key:"+apiKey); err != nil {
		return nil, err
	}
	proxyID, err := s.proxySub2APIID(ctx, u)
	if err != nil {
		return nil, err
	}
	// 不填 base_url：sub2api 按 account_mode 使用官方默认地址，防止中转 key 冒充订阅
	creds := map[string]any{"api_key": apiKey, "account_mode": mode}
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
	return s.afterAccountCreated(ctx, u.ID)
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

// afterAccountCreated 完成隐私设置并决定是否需要上架检测。
func (s *Service) afterAccountCreated(ctx context.Context, upstreamID int64) (*Upstream, error) {
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	if privacyPlatforms[u.Platform] && u.AuthType == "oauth" {
		ok := false
		if acc, err := s.Admin.SetPrivacy(ctx, *u.Sub2APIAccountID); err != nil {
			s.Log.Warn("set privacy failed", "upstream_id", u.ID, "err", err)
		} else if mode, _ := acc.Extra["privacy_mode"].(string); mode != "" {
			ok = true
		}
		if _, err := s.DB.Exec(ctx, "UPDATE upstreams SET privacy_ok = $2 WHERE id = $1", u.ID, ok); err != nil {
			return nil, err
		}
		if !ok {
			if err := setUpstreamStatus(ctx, s.DB, u.ID, StatusVerifyFailed, strPtr("cannot disable training data usage on this account")); err != nil {
				return nil, err
			}
			return GetUpstream(ctx, s.DB, u.ID)
		}
	}
	return s.startVerification(ctx, u)
}

func (s *Service) startVerification(ctx context.Context, u *Upstream) (*Upstream, error) {
	if err := setUpstreamStatus(ctx, s.DB, u.ID, StatusVerifying, nil); err != nil {
		return nil, err
	}
	if s.Quality == nil {
		return nil, errors.New("quality engine not configured")
	}
	if err := s.Quality.StartOnboardingCheck(ctx, u.ID, u.Platform, u.AuthType); err != nil {
		var he *httpx.Error
		if !errors.As(err, &he) {
			return nil, err
		}
		// 检测无法开始（例如策略没有配置模型）：记为检测失败，配置修正后可以重新检测
		if err := MarkVerified(ctx, s.DB, u.ID, false, "onboarding check could not start: "+he.Message); err != nil {
			return nil, err
		}
	}
	return GetUpstream(ctx, s.DB, u.ID)
}

// RetryVerification 在上架检测失败后重新检测。
func (s *Service) RetryVerification(ctx context.Context, sellerID, upstreamID int64) (*Upstream, error) {
	u, err := GetSellerUpstream(ctx, s.DB, sellerID, upstreamID)
	if err != nil {
		return nil, err
	}
	if u.Status != StatusVerifyFailed || u.Sub2APIAccountID == nil {
		return nil, ErrBadState
	}
	return s.afterAccountCreated(ctx, u.ID)
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
