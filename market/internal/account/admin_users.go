package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yx-token/token-market/market/internal/platform/auth"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// AdminUserRow 管理端用户列表/详情。
type AdminUserRow struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	IsAdmin      bool       `json:"is_admin"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	KYCStatus    string     `json:"kyc_status"`
	EngineUserID *int64     `json:"engine_user_id,omitempty"`
}

// AdminUserFilter 列表筛选。
type AdminUserFilter struct {
	Q         string
	Status    string
	KYCStatus string
	Limit     int
	Offset    int
}

// AdminUpdateUserInput 管理端可写字段。
type AdminUpdateUserInput struct {
	Email     *string `json:"email"`
	Status    *string `json:"status"`
	IsAdmin   *bool   `json:"is_admin"`
	Password  *string `json:"password"`
	KYCStatus *string `json:"kyc_status"`
	KYCNote   string  `json:"kyc_note"`
}

// AdminListUsers 列出市场用户。
func (s *Service) AdminListUsers(ctx context.Context, f AdminUserFilter) ([]AdminUserRow, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var b strings.Builder
	args := make([]any, 0, 8)
	b.WriteString(`SELECT u.id, u.email, u.is_admin, u.status, u.created_at, u.last_login_at,
		COALESCE(k.status, 'none'), si.sub2api_user_id
		FROM users u
		LEFT JOIN kyc_profiles k ON k.user_id = u.id
		LEFT JOIN shadow_identities si ON si.kind = 'buyer' AND si.owner_id = u.id
		WHERE 1=1`)
	if f.Status != "" {
		args = append(args, f.Status)
		fmt.Fprintf(&b, " AND u.status = $%d", len(args))
	}
	if f.KYCStatus != "" {
		if f.KYCStatus == "none" {
			b.WriteString(" AND k.user_id IS NULL")
		} else {
			args = append(args, f.KYCStatus)
			fmt.Fprintf(&b, " AND k.status = $%d", len(args))
		}
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		p := len(args)
		fmt.Fprintf(&b, " AND (lower(u.email) LIKE $%d OR CAST(u.id AS text) LIKE $%d)", p, p)
	}
	args = append(args, f.Limit, f.Offset)
	fmt.Fprintf(&b, " ORDER BY u.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := s.db.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminUserRow, 0)
	for rows.Next() {
		var u AdminUserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt, &u.LastLoginAt, &u.KYCStatus, &u.EngineUserID); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AdminGetUser 用户详情。
func (s *Service) AdminGetUser(ctx context.Context, id int64) (*AdminUserRow, error) {
	rows, err := s.AdminListUsers(ctx, AdminUserFilter{Q: fmt.Sprintf("%d", id), Limit: 20})
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i], nil
		}
	}
	var u AdminUserRow
	err = s.db.QueryRow(ctx, `SELECT u.id, u.email, u.is_admin, u.status, u.created_at, u.last_login_at,
		COALESCE(k.status, 'none'), si.sub2api_user_id
		FROM users u
		LEFT JOIN kyc_profiles k ON k.user_id = u.id
		LEFT JOIN shadow_identities si ON si.kind = 'buyer' AND si.owner_id = u.id
		WHERE u.id = $1`, id).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.Status, &u.CreatedAt, &u.LastLoginAt, &u.KYCStatus, &u.EngineUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("USER_NOT_FOUND", "user not found")
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// AdminUpdateUser 更新用户字段。
func (s *Service) AdminUpdateUser(ctx context.Context, adminID, userID int64, in AdminUpdateUserInput) (*AdminUserRow, error) {
	if _, err := s.AdminGetUser(ctx, userID); err != nil {
		return nil, err
	}
	if in.Email != nil {
		email, err := normalizeEmail(*in.Email)
		if err != nil {
			return nil, err
		}
		tag, err := s.db.Exec(ctx, `UPDATE users SET email = $2 WHERE id = $1`, userID, email)
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, httpx.NotFound("USER_NOT_FOUND", "user not found")
		}
	}
	if in.Status != nil {
		if err := s.SetStatus(ctx, userID, *in.Status); err != nil {
			return nil, err
		}
	}
	if in.IsAdmin != nil {
		if _, err := s.db.Exec(ctx, `UPDATE users SET is_admin = $2 WHERE id = $1`, userID, *in.IsAdmin); err != nil {
			return nil, err
		}
	}
	if in.Password != nil && strings.TrimSpace(*in.Password) != "" {
		hash, err := auth.HashPassword(strings.TrimSpace(*in.Password))
		if err != nil {
			return nil, httpx.BadRequest("WEAK_PASSWORD", err.Error())
		}
		if _, err := s.db.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return nil, err
		}
	}
	if in.KYCStatus != nil {
		st := strings.TrimSpace(*in.KYCStatus)
		switch st {
		case "none":
			_, _ = s.db.Exec(ctx, `DELETE FROM kyc_profiles WHERE user_id = $1`, userID)
		case "pending", "verified", "rejected":
			tag, err := s.db.Exec(ctx, `UPDATE kyc_profiles SET status = $2, reviewed_at = now(), reviewed_by = $3, note = $4
				WHERE user_id = $1`, userID, st, adminID, in.KYCNote)
			if err != nil {
				return nil, err
			}
			if tag.RowsAffected() == 0 {
				return nil, httpx.NotFound("KYC_NOT_FOUND", "no kyc profile to correct; user must submit KYC first")
			}
		default:
			return nil, httpx.BadRequest("INVALID_KYC_STATUS", "kyc_status must be none, pending, verified, or rejected")
		}
	}
	return s.AdminGetUser(ctx, userID)
}
