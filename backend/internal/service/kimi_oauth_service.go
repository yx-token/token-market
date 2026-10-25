package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/internal/pkg/kimi"
)

type KimiOAuthClient interface {
	DeviceAuthorization(context.Context, string, string) (*kimi.DeviceAuthorizationResponse, error)
	Poll(context.Context, string, string, string) (kimi.PollStatus, *kimi.TokenResponse, int64, error)
	Refresh(context.Context, string, string, string) (*kimi.TokenResponse, error)
}

type KimiOAuthService struct {
	sessionStore  *kimi.SessionStore
	proxyRepo     ProxyRepository
	oauthClient   KimiOAuthClient
	tokenProvider *KimiTokenProvider
}

func NewKimiOAuthService(proxyRepo ProxyRepository, client KimiOAuthClient) *KimiOAuthService {
	return &KimiOAuthService{sessionStore: kimi.NewSessionStore(), proxyRepo: proxyRepo, oauthClient: client}
}
func (s *KimiOAuthService) WithSessionStore(store *kimi.SessionStore) *KimiOAuthService {
	if s != nil && store != nil {
		s.sessionStore = store
	}
	return s
}

func (s *KimiOAuthService) Stop() {
	if s != nil && s.sessionStore != nil {
		s.sessionStore.Stop()
	}
}

type KimiDeviceCodeResult struct {
	SessionID               string `json:"session_id"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}
type KimiTokenInfo struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope,omitempty"`
	DeviceID     string `json:"device_id"`
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
}
type KimiPollResult struct {
	Status    string         `json:"status"`
	Interval  int64          `json:"interval,omitempty"`
	Message   string         `json:"message,omitempty"`
	TokenInfo *KimiTokenInfo `json:"token_info,omitempty"`
}

func (s *KimiOAuthService) DeviceCode(ctx context.Context, proxyID *int64) (*KimiDeviceCodeResult, error) {
	if s == nil || s.oauthClient == nil || s.sessionStore == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "KIMI_OAUTH_CLIENT_NOT_CONFIGURED", "kimi oauth client is not configured")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	sid, err := kimi.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "KIMI_OAUTH_SESSION_FAILED", "generate session id: %v", err)
	}
	deviceID := kimi.NewDeviceID()
	device, err := s.oauthClient.DeviceAuthorization(ctx, proxyURL, deviceID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "KIMI_OAUTH_DEVICE_CODE_FAILED", "request device code: %v", err)
	}
	if device == nil || strings.TrimSpace(device.DeviceCode) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "KIMI_OAUTH_DEVICE_CODE_INVALID", "device authorization response is empty")
	}
	expiresIn := device.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = int64(kimi.DeviceSessionTTL / time.Second)
	}
	interval := device.Interval
	if interval <= 0 {
		interval = 5
	}
	verificationURI := device.VerificationURI
	if strings.TrimSpace(verificationURI) == "" {
		verificationURI = device.VerificationURIComplete
	}
	expires := time.Now().Add(time.Duration(expiresIn) * time.Second)
	var storedProxyID *int64
	if proxyID != nil {
		value := *proxyID
		storedProxyID = &value
	}
	if err := s.sessionStore.Set(sid, &kimi.DeviceSession{DeviceCode: device.DeviceCode, ProxyID: storedProxyID, ProxyURL: proxyURL, DeviceID: deviceID, Interval: interval, ExpiresAt: expires}); err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "KIMI_OAUTH_SESSION_UNAVAILABLE", "device authorization session is temporarily unavailable")
	}
	return &KimiDeviceCodeResult{SessionID: sid, UserCode: device.UserCode, VerificationURI: verificationURI, VerificationURIComplete: device.VerificationURIComplete, ExpiresIn: expiresIn, Interval: interval}, nil
}

func (s *KimiOAuthService) Poll(ctx context.Context, sessionID string, _ *int64) (*KimiPollResult, error) {
	if s == nil || s.oauthClient == nil || s.sessionStore == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "KIMI_OAUTH_CLIENT_NOT_CONFIGURED", "kimi oauth client is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	session, ok := s.sessionStore.GetIncludingExpired(sessionID)
	if !ok {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	if time.Now().After(session.ExpiresAt) {
		s.sessionStore.Delete(sessionID)
		return &KimiPollResult{Status: string(kimi.PollExpired), Message: "device authorization expired"}, nil
	}
	claimed, claimErr := s.sessionStore.Claim(ctx, sessionID)
	if claimErr != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "KIMI_OAUTH_SESSION_UNAVAILABLE", "device authorization session is temporarily unavailable")
	}
	if !claimed {
		return &KimiPollResult{Status: string(kimi.PollPending), Interval: session.Interval}, nil
	}
	defer s.sessionStore.Release(context.WithoutCancel(ctx), sessionID)
	status, token, _, err := s.oauthClient.Poll(ctx, session.ProxyURL, session.DeviceCode, session.DeviceID)
	if err != nil {
		if errors.Is(err, kimi.ErrExpiredToken) {
			s.sessionStore.Delete(sessionID)
			return &KimiPollResult{Status: string(kimi.PollExpired), Message: "device authorization expired"}, nil
		}
		if errors.Is(err, kimi.ErrAccessDenied) {
			s.sessionStore.Delete(sessionID)
			return &KimiPollResult{Status: string(kimi.PollDenied), Message: "authorization denied"}, nil
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "KIMI_OAUTH_POLL_FAILED", "poll token: %v", err)
	}
	result := &KimiPollResult{Status: string(status), Interval: session.Interval}
	if status == kimi.PollSlowDown {
		session.Interval += 5
		if err := s.sessionStore.Set(sessionID, session); err != nil {
			return nil, infraerrors.New(http.StatusServiceUnavailable, "KIMI_OAUTH_SESSION_UNAVAILABLE", "device authorization session is temporarily unavailable")
		}
		result.Interval = session.Interval
	}
	if status == kimi.PollFailed {
		result.Message = "Kimi returned incomplete credentials (access_token, refresh_token or expires_in missing); please authorize again"
	}
	if status == kimi.PollSuccess {
		if kimi.ValidateToken(token) != nil {
			result.Status = string(kimi.PollFailed)
			result.Message = "Kimi returned incomplete credentials (access_token, refresh_token or expires_in missing); please authorize again"
			return result, nil
		}
		s.sessionStore.Delete(sessionID)
		result.TokenInfo = s.tokenInfo(token, session.DeviceID)
	}
	return result, nil
}

func (s *KimiOAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyID *int64, deviceID string) (*KimiTokenInfo, error) {
	if s == nil || s.oauthClient == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "KIMI_OAUTH_CLIENT_NOT_CONFIGURED", "kimi oauth client is not configured")
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_NO_REFRESH_TOKEN", "refresh_token is required")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	// CLI maxRetries=3 means three total attempts, with 1s/2s backoff.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<(attempt-1)) * time.Second):
			}
		}
		resp, callErr := s.oauthClient.Refresh(ctx, proxyURL, refreshToken, deviceID)
		if callErr == nil {
			if validationErr := kimi.ValidateToken(resp); validationErr != nil {
				return nil, validationErr
			}
			return s.tokenInfo(resp, deviceID), nil
		}
		if errors.Is(callErr, kimi.ErrInvalidGrant) {
			return nil, infraerrors.New(http.StatusUnauthorized, "KIMI_OAUTH_INVALID_GRANT", "invalid_grant: refresh token is invalid; reauthorization required")
		}
		if errors.Is(callErr, kimi.ErrInvalidTokenResponse) {
			return nil, callErr
		}
		var oauthErr *kimi.OAuthError
		if attempt == 2 || (errors.As(callErr, &oauthErr) && !oauthErr.Retryable()) {
			return nil, infraerrors.New(http.StatusBadGateway, "KIMI_OAUTH_TOKEN_REFRESH_FAILED", "Kimi token refresh failed; retry later")
		}
	}
	return nil, infraerrors.New(http.StatusBadGateway, "KIMI_OAUTH_TOKEN_REFRESH_FAILED", "refresh token failed")
}

func (s *KimiOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*KimiTokenInfo, error) {
	if account == nil || account.Platform != PlatformKimi {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_INVALID_ACCOUNT", "account is not a Kimi account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_INVALID_ACCOUNT_TYPE", "account is not an OAuth account")
	}
	return s.RefreshToken(ctx, account.GetCredential("refresh_token"), account.ProxyID, account.GetCredential("device_id"))
}

func (s *KimiOAuthService) BuildAccountCredentials(info *KimiTokenInfo) map[string]any {
	if info == nil {
		return nil
	}
	out := map[string]any{"access_token": info.AccessToken, "expires_at": time.Unix(info.ExpiresAt, 0).UTC().Format(time.RFC3339), "base_url": kimi.DefaultBaseURL, "account_mode": AccountModeCoding, "device_id": info.DeviceID, "client_id": info.ClientID}
	if info.RefreshToken != "" {
		out["refresh_token"] = info.RefreshToken
	}
	if info.TokenType != "" {
		out["token_type"] = info.TokenType
	}
	if info.Scope != "" {
		out["scope"] = info.Scope
	}
	return out
}
func (s *KimiOAuthService) tokenInfo(resp *kimi.TokenResponse, deviceID string) *KimiTokenInfo {
	if resp == nil {
		return nil
	}
	expires := resp.ExpiresIn
	if expires <= 0 {
		expires = 900
	}
	tokenType := resp.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return &KimiTokenInfo{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken, TokenType: tokenType, ExpiresIn: expires, ExpiresAt: time.Now().Add(time.Duration(expires) * time.Second).Unix(), Scope: resp.Scope, DeviceID: deviceID, BaseURL: kimi.DefaultBaseURL, ClientID: kimi.ClientID}
}
func (s *KimiOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	p, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil || p == nil {
		return "", infraerrors.New(http.StatusBadRequest, "KIMI_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	return p.URL(), nil
}

// RefreshAccount uses exactly the same rotation lock and CAS as request and
// background refreshes. The provider's force mode also performs conditional recovery.
func (s *KimiOAuthService) RefreshAccount(ctx context.Context, account *Account) (*Account, error) {
	if s.tokenProvider == nil {
		return nil, errKimiOAuthRefreshNotConfigured
	}
	return s.tokenProvider.RefreshAccount(ctx, account)
}
