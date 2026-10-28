package fakesub2api

import (
	"net/http"
	"strings"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/randx"
)

func (s *Server) codingOAuthStart(_ http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Provider string `json:"provider"`
		ProxyID  *int64 `json:"proxy_id"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	kimi := strings.Contains(r.URL.Path, "/kimi/")
	if !kimi && in.Provider != "bigmodel" && in.Provider != "zai" {
		return nil, badRequest("invalid provider")
	}
	sessionID := randx.String(24)
	s.mu.Lock()
	s.sessions[sessionID] = oauthSession{provider: in.Provider, proxyID: in.ProxyID}
	s.mu.Unlock()
	if kimi {
		return map[string]any{"session_id": sessionID, "user_code": "TEST-CODE", "verification_uri": "https://auth.example.test/device",
			"verification_uri_complete": "https://auth.example.test/device?user_code=TEST-CODE", "expires_in": 900, "interval": 5}, nil
	}
	return map[string]any{"session_id": sessionID, "provider": in.Provider, "authorize_url": "https://auth.example.test/login?state=test-state", "expires_in": 300, "interval": 2}, nil
}

func (s *Server) codingOAuthSession(r *http.Request, exchange bool) (string, oauthSession, error) {
	var in struct {
		SessionID   string `json:"session_id"`
		ProxyID     *int64 `json:"proxy_id"`
		CallbackURL string `json:"callback_url"`
	}
	if err := decode(r, &in); err != nil {
		return "", oauthSession{}, err
	}
	if exchange && in.CallbackURL == "" {
		return "", oauthSession{}, badRequest("callback_url required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[in.SessionID]
	if !ok {
		return "", oauthSession{}, nil
	}
	if (session.proxyID == nil) != (in.ProxyID == nil) || (session.proxyID != nil && *session.proxyID != *in.ProxyID) {
		return "", oauthSession{}, &apiErr{400, "PROXY_MISMATCH", "proxy does not match authorization session"}
	}
	session.polls++
	s.sessions[in.SessionID] = session
	return in.SessionID, session, nil
}

func (s *Server) codingOAuthPoll(_ http.ResponseWriter, r *http.Request) (any, error) {
	sessionID, session, err := s.codingOAuthSession(r, false)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		return map[string]any{"status": "expired"}, nil
	}
	if session.polls == 1 {
		return map[string]any{"status": "pending", "interval": 5}, nil
	}
	if strings.Contains(r.URL.Path, "/kimi/") {
		return map[string]any{"status": "success", "token_info": map[string]any{
			"access_token": "kimi-access", "refresh_token": "kimi-refresh", "expires_at": time.Now().Add(time.Hour).Unix(),
			"token_type": "Bearer", "scope": "coding", "device_id": "test-device", "client_id": "test-client",
		}}, nil
	}
	return map[string]any{"status": "success", "result": codingOAuthResult(session.provider)}, nil
}

func (s *Server) codingOAuthExchange(_ http.ResponseWriter, r *http.Request) (any, error) {
	sessionID, session, err := s.codingOAuthSession(r, true)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, badRequest("session expired")
	}
	return codingOAuthResult(session.provider), nil
}

func codingOAuthResult(provider string) map[string]any {
	return map[string]any{"provider": provider, "credentials": map[string]any{
		"access_token": "glm-access", "refresh_token": "glm-refresh", "api_key": "derived.secret", "zcode_jwt": "glm-jwt",
		"oauth_provider": provider, "account_mode": "coding", "zhipu_organization": "org-test", "zhipu_project": "project-test",
	}, "display": map[string]any{"email": "glm@example.test", "username": "glm-test"}}
}
