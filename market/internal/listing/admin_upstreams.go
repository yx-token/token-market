package listing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
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
	var email string
	_ = s.DB.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, sellerID).Scan(&email)
	return email
}

func attachSellerEmails(ctx context.Context, db DB, ups []Upstream) ([]AdminUpstreamRow, error) {
	out := make([]AdminUpstreamRow, len(ups))
	if len(ups) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(ups))
	seen := map[int64]bool{}
	for _, u := range ups {
		if !seen[u.SellerID] {
			seen[u.SellerID] = true
			ids = append(ids, u.SellerID)
		}
	}
	emails := map[int64]string{}
	if len(ids) > 0 {
		var b strings.Builder
		args := make([]any, len(ids))
		b.WriteString(`SELECT id, email FROM users WHERE id IN (`)
		for i, id := range ids {
			if i > 0 {
				b.WriteByte(',')
			}
			args[i] = id
			fmt.Fprintf(&b, "$%d", i+1)
		}
		b.WriteByte(')')
		rows, err := db.Query(ctx, b.String(), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var email string
			if err := rows.Scan(&id, &email); err != nil {
				return nil, err
			}
			emails[id] = email
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
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
