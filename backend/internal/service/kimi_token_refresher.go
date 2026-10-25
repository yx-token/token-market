package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

type KimiTokenRefresher struct{ oauthService *KimiOAuthService }

func NewKimiTokenRefresher(s *KimiOAuthService) *KimiTokenRefresher {
	return &KimiTokenRefresher{oauthService: s}
}
func (r *KimiTokenRefresher) CacheKey(a *Account) string { return KimiTokenCacheKey(a) }
func (r *KimiTokenRefresher) CanRefresh(a *Account) bool {
	return a != nil && a.Platform == PlatformKimi && a.Type == AccountTypeOAuth && !a.IsCredentialShadow()
}
func (r *KimiTokenRefresher) NeedsRefresh(a *Account, window time.Duration) bool {
	if !r.CanRefresh(a) {
		return false
	}
	exp := a.GetCredentialAsTime("expires_at")
	return a.GetCredential("access_token") == "" || exp == nil || time.Until(*exp) <= window
}
func (r *KimiTokenRefresher) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	if strings.TrimSpace(a.GetCredential("refresh_token")) == "" {
		return nil, errKimiOAuthRefreshTokenMissing
	}
	if r.oauthService == nil {
		return nil, errors.New("kimi OAuth service not configured")
	}
	info, err := r.oauthService.RefreshAccountToken(ctx, a)
	if err != nil {
		return nil, err
	}
	creds := r.oauthService.BuildAccountCredentials(info)
	// Rotation only replaces token material; keep the selected protocol endpoint.
	if base := a.GetCredential("base_url"); base != "" {
		creds["base_url"] = base
	}
	return MergeCredentials(a.Credentials, creds), nil
}

func KimiTokenCacheKey(a *Account) string {
	if a == nil {
		return "kimi:account:0"
	}
	return "kimi:account:" + strconv.FormatInt(a.ID, 10)
}
