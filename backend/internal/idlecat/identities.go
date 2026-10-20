package idlecat

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// IdentityRow 市场用户与引擎用户的对照（统一 users 表的过渡视图）。
type IdentityRow struct {
	MarketUserID   int64  `json:"market_user_id"`
	Email          string `json:"email"`
	IsAdmin        bool   `json:"is_admin"`
	MarketStatus   string `json:"market_status"`
	EngineUserID   *int64 `json:"engine_user_id,omitempty"`
	EngineRole     string `json:"engine_role,omitempty"`
	EngineStatus   string `json:"engine_status,omitempty"`
	ShadowKind     string `json:"shadow_kind,omitempty"`
	ShadowStatus   string `json:"shadow_status,omitempty"`
	ShadowEngineID *int64 `json:"shadow_engine_user_id,omitempty"`
	Linked         bool   `json:"linked"`
}

// ListIdentities 列出市场用户，并按邮箱匹配引擎用户；附带影子身份（若有）。
func (s *Store) ListIdentities(ctx context.Context) ([]IdentityRow, error) {
	rows, err := s.Market.QueryContext(ctx, `
		SELECT u.id, u.email, u.is_admin, u.status,
		       s.kind, s.status, s.sub2api_user_id
		FROM users u
		LEFT JOIN shadow_identities s ON s.owner_id = u.id AND s.kind = 'buyer'
		ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]IdentityRow, 0)
	for rows.Next() {
		var r IdentityRow
		var kind, shadowStatus sql.NullString
		var shadowUID sql.NullInt64
		if err := rows.Scan(&r.MarketUserID, &r.Email, &r.IsAdmin, &r.MarketStatus, &kind, &shadowStatus, &shadowUID); err != nil {
			return nil, err
		}
		if kind.Valid {
			r.ShadowKind = kind.String
		}
		if shadowStatus.Valid {
			r.ShadowStatus = shadowStatus.String
		}
		if shadowUID.Valid {
			v := shadowUID.Int64
			r.ShadowEngineID = &v
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.Engine == nil {
		return out, nil
	}
	for i := range out {
		var id int64
		var role, status string
		err := s.Engine.QueryRowContext(ctx, `
			SELECT id, role, status FROM users
			WHERE deleted_at IS NULL AND lower(email) = lower($1)
			LIMIT 1`, out[i].Email).Scan(&id, &role, &status)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i].EngineUserID = &id
		out[i].EngineRole = role
		out[i].EngineStatus = status
		out[i].Linked = true
	}
	return out, nil
}

// EnsureEngineUser 按邮箱在引擎创建用户（已存在则返回现有 id）。密码写入引擎 bcrypt。
func (s *Store) EnsureEngineUser(ctx context.Context, email, password, role string) (int64, error) {
	if s.Engine == nil {
		return 0, fmt.Errorf("engine db unavailable")
	}
	email = strings.TrimSpace(strings.ToLower(email))
	password = strings.TrimSpace(password)
	if email == "" {
		return 0, fmt.Errorf("email required")
	}
	if role == "" {
		role = "user"
	}
	if role != "user" && role != "admin" {
		return 0, fmt.Errorf("role must be user or admin")
	}
	var id int64
	err := s.Engine.QueryRowContext(ctx, `
		SELECT id FROM users WHERE deleted_at IS NULL AND lower(email) = lower($1) LIMIT 1`, email).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	if len(password) < 6 {
		return 0, fmt.Errorf("password(min 6) required to create engine user")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	username := strings.Split(email, "@")[0]
	if username == "" {
		username = "user"
	}
	err = s.Engine.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, role, username, status, concurrency, signup_source)
		VALUES ($1, $2, $3, $4, 'active', 5, 'email')
		RETURNING id`, email, string(hash), role, username).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
