package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/httpx"
)

// fromEngine 用已登录的引擎管理员会话换发闲猫 tm_token（兼容旧桥接）。
func (s *Server) fromEngine(w http.ResponseWriter, r *http.Request) error {
	if s.EngineProxyURL == "" {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine not configured")
	}
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return httpx.Unauthorized("UNAUTHORIZED", "engine token required")
	}
	bearer := strings.TrimPrefix(authz, "Bearer ")
	email, role, err := s.engineAuthMe(r.Context(), bearer)
	if err != nil || email == "" {
		return httpx.Unauthorized("UNAUTHORIZED", "invalid engine token")
	}
	if role != "admin" {
		return httpx.Forbidden("FORBIDDEN", "admin required")
	}
	tok, exp, u, err := s.Accounts.IssueSessionByEmail(r.Context(), email, httpx.ClientIP(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": tok,
		"token":        tok,
		"expires_at":   exp.UTC().Format(time.RFC3339),
		"user": map[string]any{
			"id": u.ID, "email": u.Email, "role": "admin", "is_admin": true, "status": u.Status,
		},
	})
	return nil
}
