package httpapi

import (
	"net/http"
	"strings"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

// bindIdentityShadow 把市场买家绑定到同邮箱的引擎用户，作为 buyer 影子身份。
func (s *Server) bindIdentityShadow(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return httpx.BadRequest("INVALID_EMAIL", "email required")
	}
	if s.Funds == nil || s.Funds.Shadow == nil || s.Replica == nil {
		return httpx.Unavailable("SHADOW_UNAVAILABLE", "shadow bind not configured")
	}
	var ownerID int64
	var isAdmin bool
	err := s.DB.QueryRow(r.Context(), `SELECT id, is_admin FROM users WHERE lower(email) = lower($1)`, email).Scan(&ownerID, &isAdmin)
	if err != nil {
		return httpx.NotFound("USER_NOT_FOUND", "market user not found")
	}
	if isAdmin {
		return httpx.BadRequest("ADMIN_SKIP", "admin uses the engine account directly")
	}
	engineUser, err := s.Replica.UserByEmail(r.Context(), email)
	if err != nil {
		return err
	}
	if engineUser == nil {
		return httpx.BadRequest("ENGINE_USER_MISSING", "sync the engine user before binding the shadow")
	}
	it, err := s.Funds.Shadow.BindLinkedBuyer(r.Context(), ownerID, engineUser.ID, email, in.Password)
	if err != nil {
		return httpx.BadRequest("SHADOW_BIND_FAILED", err.Error())
	}
	return ok(w, map[string]any{
		"email":          it.Email,
		"market_user_id": it.OwnerID,
		"engine_user_id": it.Sub2APIUserID,
		"shadow_status":  it.Status,
		"shadow_kind":    it.Kind,
		"token_ready":    it.Status == "active",
	})
}
