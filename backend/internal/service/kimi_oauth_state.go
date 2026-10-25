package service

import (
	"context"
	"strings"
	"time"
)

// KimiOAuthRepository compares the complete credential document and proxy in
// every mutation so a concurrent reauthorization always wins over stale work.
type KimiOAuthRepository interface {
	UpdateKimiOAuthCredentialsIfUnchanged(context.Context, int64, map[string]any, *int64, map[string]any) (bool, error)
	SetKimiOAuthFailureIfUnchanged(context.Context, int64, map[string]any, *int64, bool, time.Time, string) (bool, error)
	RecoverKimiOAuthIfUnchanged(context.Context, int64, map[string]any, *int64) (bool, error)
}

func (s *RateLimitService) handleKimiOAuthUnauthorized(ctx context.Context, a *Account) {
	repo, ok := s.accountRepo.(KimiOAuthRepository)
	if !ok {
		return
	}
	until := time.Now().Add(30 * time.Second)
	permanent := strings.TrimSpace(a.GetCredential("refresh_token")) == ""
	reason := "Kimi OAuth upstream rejected credentials after refresh; retry later"
	if permanent {
		reason = "Kimi OAuth reauthorization required: refresh token missing"
	}
	applied, err := repo.SetKimiOAuthFailureIfUnchanged(ctx, a.ID, a.Credentials, a.ProxyID, permanent, until, reason)
	if err != nil || !applied {
		return
	}
	if s.tokenCacheInvalidator != nil {
		_ = s.tokenCacheInvalidator.InvalidateToken(ctx, a)
	}
	if permanent {
		until = time.Time{}
	}
	s.notifyAccountSchedulingBlocked(a, until, "kimi_oauth_401")
}
