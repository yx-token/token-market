package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/auth"
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// authenticateAdmin 接受闲猫 JWT，或已登录的 sub2api 管理员 Bearer（同一身份，不再二次登录）。
func (s *Server) authenticateAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			httpx.WriteError(w, r, httpx.Unauthorized("UNAUTHORIZED", "missing bearer token"))
			return
		}
		if claims, err := s.Issuer.Parse(token); err == nil {
			if err := s.Accounts.CheckActive(r.Context(), claims.UserID); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(httpx.WithUser(r.Context(), claims)))
			return
		}
		email, role, err := s.engineAuthMe(r.Context(), token)
		if err != nil || role != "admin" || email == "" {
			httpx.WriteError(w, r, httpx.Unauthorized("UNAUTHORIZED", "invalid or expired token"))
			return
		}
		u, err := s.Accounts.LookupActiveAdminByEmail(r.Context(), email)
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized("UNAUTHORIZED", "market admin not linked"))
			return
		}
		claims := auth.Claims{UserID: u.ID, IsAdmin: u.IsAdmin}
		next.ServeHTTP(w, r.WithContext(httpx.WithUser(r.Context(), claims)))
	})
}

func (s *Server) engineAuthMe(ctx context.Context, bearer string) (email, role string, err error) {
	if s.EngineProxyURL == "" {
		return "", "", httpx.Unavailable("ENGINE_UNAVAILABLE", "engine not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(s.EngineProxyURL, "/")+"/api/v1/auth/me", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", httpx.Unauthorized("UNAUTHORIZED", "invalid engine token")
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Code != 0 {
		return "", "", httpx.Unauthorized("UNAUTHORIZED", "invalid engine user")
	}
	return env.Data.Email, env.Data.Role, nil
}
