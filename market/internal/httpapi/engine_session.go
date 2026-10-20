package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

// engineSession 把已登录的闲猫管理员换成引擎 /api/v1 会话，避免二次登录。
func (s *Server) engineSession(w http.ResponseWriter, r *http.Request) error {
	if s.EngineProxyURL == "" {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine bridge not configured")
	}
	u, err := s.Accounts.Get(r.Context(), httpx.MustUser(r).UserID)
	if err != nil {
		return err
	}
	email := strings.TrimSpace(strings.ToLower(u.Email))
	if email == "" {
		return httpx.BadRequest("INVALID_USER", "admin email required")
	}
	if s.EngineLoginPassword != "" {
		return s.enginePasswordLogin(w, r, email)
	}
	if s.EngineAdminKey == "" {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine bridge not configured")
	}
	body, _ := json.Marshal(map[string]string{"email": email})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimRight(s.EngineProxyURL, "/")+"/api/v1/admin/auth/session", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.EngineAdminKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine session failed")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine session rejected")
	}
	var env struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Code != 0 || env.Data == nil {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine session invalid response")
	}
	httpx.WriteJSON(w, http.StatusOK, env.Data)
	return nil
}

func (s *Server) enginePasswordLogin(w http.ResponseWriter, r *http.Request, email string) error {
	body, _ := json.Marshal(map[string]string{"email": email, "password": s.EngineLoginPassword})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimRight(s.EngineProxyURL, "/")+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine login failed")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine login rejected")
	}
	var env struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Code != 0 || env.Data == nil {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine login invalid response")
	}
	if _, ok := env.Data["access_token"]; !ok {
		return httpx.Unavailable("ENGINE_UNAVAILABLE", "engine login missing token")
	}
	httpx.WriteJSON(w, http.StatusOK, env.Data)
	return nil
}
