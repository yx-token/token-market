package listing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/platform/usermail"
)

// AdminUpstreamRow 管理端上游列表/详情（附卖家账号信息）。
type AdminUpstreamRow struct {
	Upstream
	SellerEmail string `json:"seller_email"`
}

// adminSellerID 按上游 ID 解析属主卖家，供管理端代操作接入/模型。
func (s *Service) adminSellerID(ctx context.Context, upstreamID int64) (int64, *Upstream, error) {
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return 0, nil, err
	}
	if u.Status == StatusRemoved {
		return 0, nil, httpx.NotFound("UPSTREAM_GONE", "upstream removed")
	}
	return u.SellerID, u, nil
}

func (s *Service) sellerEmail(ctx context.Context, sellerID int64) string {
	emails, err := usermail.Lookup(ctx, s.DB, []int64{sellerID})
	if err != nil {
		return ""
	}
	return emails[sellerID]
}

func attachSellerEmails(ctx context.Context, db DB, ups []Upstream) ([]AdminUpstreamRow, error) {
	out := make([]AdminUpstreamRow, len(ups))
	if len(ups) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(ups))
	for _, u := range ups {
		ids = append(ids, u.SellerID)
	}
	emails, err := usermail.Lookup(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	for i, u := range ups {
		out[i] = AdminUpstreamRow{Upstream: u, SellerEmail: emails[u.SellerID]}
	}
	return out, nil
}

// AdminListUpstreams 管理端列表（含卖家邮箱）。
func (s *Service) AdminListUpstreams(ctx context.Context, status string, limit, offset int) ([]AdminUpstreamRow, error) {
	ups, err := ListUpstreams(ctx, s.DB, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return attachSellerEmails(ctx, s.DB, ups)
}

// AdminCreateUpstreamInput 管理端代指定卖家创建上游草稿。
type AdminCreateUpstreamInput struct {
	SellerID        int64  `json:"seller_id"`
	Platform        string `json:"platform"`
	AuthType        string `json:"auth_type"`
	Name            string `json:"name"`
	Note            string `json:"note"`
	ProxyID         *int64 `json:"proxy_id"`
	ProxyFromImport bool   `json:"proxy_from_import"`
}

// AdminCreateUpstream 代卖家创建上游并进入接入流程（可跳过「必须卖家代理」门禁，走平台代理池）。
func (s *Service) AdminCreateUpstream(ctx context.Context, in AdminCreateUpstreamInput) (*AdminUpstreamRow, error) {
	if in.SellerID <= 0 {
		return nil, httpx.BadRequest("SELLER_REQUIRED", "seller_id is required")
	}
	platform := strings.TrimSpace(in.Platform)
	authType := strings.TrimSpace(in.AuthType)
	if platform == "" || authType == "" {
		return nil, httpx.BadRequest("PLATFORM_REQUIRED", "platform and auth_type are required")
	}
	name := strings.TrimSpace(in.Name)
	note := strings.TrimSpace(in.Note)
	if len([]rune(name)) > 64 {
		return nil, httpx.BadRequest("NAME_TOO_LONG", "name must be at most 64 characters")
	}
	if len([]rune(note)) > 500 {
		return nil, httpx.BadRequest("NOTE_TOO_LONG", "note must be at most 500 characters")
	}
	var status string
	err := s.DB.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, in.SellerID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("USER_NOT_FOUND", "seller not found")
	}
	if err != nil {
		return nil, err
	}
	if status == "frozen" {
		return nil, httpx.Conflict("USER_FROZEN", "seller is frozen")
	}
	u, err := s.createUpstream(ctx, in.SellerID, platform, authType, false, in.ProxyID, in.ProxyFromImport, true)
	if err != nil {
		return nil, err
	}
	if name != "" || note != "" {
		var nameArg, noteArg any
		if name != "" {
			nameArg = name
		}
		if note != "" {
			noteArg = note
		}
		if _, err := s.DB.Exec(ctx, `UPDATE upstreams SET name = COALESCE($2, name), note = COALESCE($3, note), updated_at = now() WHERE id = $1`,
			u.ID, nameArg, noteArg); err != nil {
			return nil, err
		}
		u, err = GetUpstream(ctx, s.DB, u.ID)
		if err != nil {
			return nil, err
		}
	}
	return &AdminUpstreamRow{Upstream: *u, SellerEmail: s.sellerEmail(ctx, in.SellerID)}, nil
}

// AdminGetUpstream 管理端上游详情（含卖家邮箱）。
func (s *Service) AdminGetUpstream(ctx context.Context, upstreamID int64) (*AdminUpstreamRow, error) {
	_, u, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return &AdminUpstreamRow{Upstream: *u, SellerEmail: s.sellerEmail(ctx, u.SellerID)}, nil
}

// AdminGetModels 管理端读取模型目录与白名单。
func (s *Service) AdminGetModels(ctx context.Context, upstreamID int64) (*UpstreamModels, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.GetModels(ctx, sellerID, upstreamID)
}

// AdminRefreshModels 管理端刷新模型目录。
func (s *Service) AdminRefreshModels(ctx context.Context, upstreamID int64) (*UpstreamModels, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.RefreshModels(ctx, sellerID, upstreamID)
}

// AdminSetModels 管理端代选挂售模型（可将 selecting_models 推进到 ready）。
func (s *Service) AdminSetModels(ctx context.Context, upstreamID int64, models []string) (*Upstream, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.SetModels(ctx, sellerID, upstreamID, models)
}

// AdminSwitchChannelInput 未完成接入前更换厂商 / 接入方式。
type AdminSwitchChannelInput struct {
	Platform string `json:"platform"`
	AuthType string `json:"auth_type"`
}

// AdminSwitchChannel 草稿/授权中/失败态可换渠道；已绑定引擎账号则需先清绑定或删建。
func (s *Service) AdminSwitchChannel(ctx context.Context, upstreamID int64, in AdminSwitchChannelInput) (*AdminUpstreamRow, error) {
	platform := strings.TrimSpace(in.Platform)
	authType := strings.TrimSpace(in.AuthType)
	if platform == "" || authType == "" {
		return nil, httpx.BadRequest("PLATFORM_REQUIRED", "platform and auth_type are required")
	}
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	if u.Status != StatusDraft && u.Status != StatusAuthorizing && u.Status != StatusFailed {
		return nil, httpx.Conflict("INVALID_STATE", "仅草稿 / 授权中 / 失败状态可换渠道；已接入请删除后新建")
	}
	if u.Sub2APIAccountID != nil {
		return nil, httpx.Conflict("ENGINE_ACCOUNT_BOUND", "已绑定引擎账号，请先「清除失效账号」或删除上游后新建")
	}
	v, err := GetVendor(ctx, s.DB, platform)
	if err != nil {
		return nil, err
	}
	if !v.Enabled && !u.IsReference {
		return nil, ErrVendorDisabled
	}
	if !v.SupportsAuthType(authType) {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", fmt.Sprintf("%s does not support %s", platform, authType))
	}
	if !IsOAuthAuthType(platform, authType) && !IsCodingAuthType(platform, authType) {
		return nil, httpx.BadRequest("UNSUPPORTED_AUTH_TYPE", "auth type not implemented")
	}
	if platform == u.Platform && authType == u.AuthType {
		return &AdminUpstreamRow{Upstream: *u, SellerEmail: s.sellerEmail(ctx, u.SellerID)}, nil
	}
	tag, err := s.DB.Exec(ctx, `UPDATE upstreams SET
		platform = $2,
		auth_type = $3,
		status = 'draft',
		failure_reason = NULL,
		identity_hash = NULL,
		account_email = NULL,
		model_whitelist = NULL,
		updated_at = now()
		WHERE id = $1 AND status IN ('draft', 'authorizing', 'failed') AND sub2api_account_id IS NULL`,
		upstreamID, platform, authType)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrBadState
	}
	_, _ = s.DB.Exec(ctx, `DELETE FROM upstream_oauth_sessions WHERE upstream_id = $1`, upstreamID)
	updated, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	return &AdminUpstreamRow{Upstream: *updated, SellerEmail: s.sellerEmail(ctx, updated.SellerID)}, nil
}

// AdminClearDeadEngineAccount 引擎侧账号已不存在时，清除绑定并回到 authorizing，便于重新 OAuth / 填 Key。
func (s *Service) AdminClearDeadEngineAccount(ctx context.Context, upstreamID int64) (*Upstream, error) {
	u, err := GetUpstream(ctx, s.DB, upstreamID)
	if err != nil {
		return nil, err
	}
	if u.Sub2APIAccountID == nil {
		if u.Status == StatusAuthorizing || u.Status == StatusDraft || u.Status == StatusFailed {
			return u, nil
		}
		return nil, httpx.Conflict("INVALID_STATE", "当前状态无需清除引擎账号")
	}
	if _, err := s.Admin.GetAccount(ctx, *u.Sub2APIAccountID); err == nil {
		return nil, httpx.Conflict("ENGINE_ACCOUNT_EXISTS", "引擎侧账号仍存在，请走重新授权流程，勿直接清除")
	} else if mapped := mapEngineAccountError(err); !errors.Is(mapped, ErrEngineAccountMissing) {
		return nil, mapped
	}
	reason := "引擎账号已失效，已清除绑定，请重新授权"
	tag, err := s.DB.Exec(ctx, `UPDATE upstreams SET
		status = 'authorizing',
		failure_reason = $2,
		sub2api_account_id = NULL,
		identity_hash = NULL,
		account_email = NULL,
		model_whitelist = NULL,
		updated_at = now()
		WHERE id = $1 AND status NOT IN ('removing', 'removed')`, upstreamID, reason)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrBadState
	}
	return GetUpstream(ctx, s.DB, upstreamID)
}

// AdminStartAuthorization 管理端代发起 OAuth。
func (s *Service) AdminStartAuthorization(ctx context.Context, upstreamID int64, opts AuthorizationOptions) (*AuthorizationStart, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.StartAuthorization(ctx, sellerID, upstreamID, opts)
}

// AdminCompleteAuthorization 管理端代完成 OAuth。
func (s *Service) AdminCompleteAuthorization(ctx context.Context, upstreamID int64, code string) (*Upstream, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.CompleteAuthorization(ctx, sellerID, upstreamID, code)
}

// AdminSubmitCodingKey 管理端代提交 Coding Plan Key。
func (s *Service) AdminSubmitCodingKey(ctx context.Context, upstreamID int64, in CodingKeyInput) (*Upstream, error) {
	sellerID, _, err := s.adminSellerID(ctx, upstreamID)
	if err != nil {
		return nil, err
	}
	return s.SubmitCodingKey(ctx, sellerID, upstreamID, in)
}
