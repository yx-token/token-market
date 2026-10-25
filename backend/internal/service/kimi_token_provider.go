package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
)

const (
	kimiTokenRefreshSkew    = 2 * time.Minute
	kimiRefreshWaitTimeout  = 2 * time.Second
	kimiRefreshPollInterval = 25 * time.Millisecond
)

var (
	errKimiOAuthRefreshTokenMissing  = errors.New("kimi OAuth reauthorization required: refresh token missing")
	errKimiOAuthRefreshNotConfigured = errors.New("kimi oauth refresh is not configured")
	errKimiOAuthAccessTokenMissing   = errors.New("kimi oauth access token is missing")
	errKimiOAuthAccessTokenExpired   = errors.New("kimi oauth access token is expired")
)

type KimiTokenProvider struct {
	accountRepo      AccountRepository
	tokenCache       GeminiTokenCache
	refreshAPI       *OAuthRefreshAPI
	executor         OAuthRefreshExecutor
	tempUnschedCache TempUnschedCache
	runtimeBlocker   AccountRuntimeBlocker
}

func NewKimiTokenProvider(repo AccountRepository, cache GeminiTokenCache) *KimiTokenProvider {
	return &KimiTokenProvider{accountRepo: repo, tokenCache: cache}
}

func (p *KimiTokenProvider) SetRefreshAPI(api *OAuthRefreshAPI, executor OAuthRefreshExecutor) {
	p.refreshAPI, p.executor = api, executor
}

// GetAccessToken returns a current Kimi Coding Plan OAuth token. The short
// Kimi lifetime (normally 15 minutes) makes the two minute pre-refresh window
// deliberate. A worker that loses the distributed refresh lock waits for the
// authoritative DB version instead of sending an expired token.
func (p *KimiTokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil || account.Platform != PlatformKimi || account.Type != AccountTypeOAuth {
		return "", errors.New("not a kimi oauth account")
	}
	key := KimiTokenCacheKey(account)
	accountToken := strings.TrimSpace(account.GetCredential("access_token"))
	expiresAt := account.GetCredentialAsTime("expires_at")
	if accountToken == "" && p.refreshAPI == nil {
		return "", errKimiOAuthAccessTokenMissing
	}

	if p.tokenCache != nil {
		if token, err := p.tokenCache.GetAccessToken(ctx, key); err == nil {
			cached := strings.TrimSpace(token)
			if cached != "" && cached == accountToken && expiresAt != nil && time.Until(*expiresAt) > kimiTokenRefreshSkew {
				return cached, nil
			}
		}
	}

	needsRefresh := accountToken == "" || expiresAt == nil || time.Until(*expiresAt) <= kimiTokenRefreshSkew
	if needsRefresh {
		if p.refreshAPI == nil || p.executor == nil {
			return "", errKimiOAuthRefreshNotConfigured
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		result, err := p.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(refreshCtx), account, p.executor, kimiTokenRefreshSkew)
		if err != nil {
			return "", err
		}
		if result != nil && result.LockHeld {
			if accountToken != "" && expiresAt != nil && time.Now().Before(*expiresAt) {
				return accountToken, nil
			}
			return p.waitForRefreshedToken(refreshCtx, account, key)
		}
		if result != nil && result.Account != nil {
			account = result.Account
			accountToken = strings.TrimSpace(account.GetCredential("access_token"))
			expiresAt = account.GetCredentialAsTime("expires_at")
		}
	}

	if accountToken == "" {
		return "", errKimiOAuthAccessTokenMissing
	}
	if expiresAt != nil && !time.Now().Before(*expiresAt) {
		return "", errKimiOAuthAccessTokenExpired
	}

	// A background refresh can win between the request's account snapshot and
	// cache publication. Do not overwrite its newer token with this stale one.
	if p.tokenCache != nil {
		latestAccount, stale := CheckTokenVersion(ctx, account, p.accountRepo)
		if stale && latestAccount != nil {
			account = latestAccount
			accountToken = strings.TrimSpace(account.GetCredential("access_token"))
			expiresAt = account.GetCredentialAsTime("expires_at")
			if accountToken == "" {
				return "", errKimiOAuthAccessTokenMissing
			}
			if expiresAt == nil || !time.Now().Before(*expiresAt) {
				return "", errKimiOAuthAccessTokenExpired
			}
		} else {
			ttl := 30 * time.Minute
			if expiresAt != nil {
				until := time.Until(*expiresAt)
				if until > kimiTokenRefreshSkew {
					ttl = until - kimiTokenRefreshSkew
				} else if until > 0 {
					ttl = until
				}
			}
			_ = p.tokenCache.SetAccessToken(ctx, key, accountToken, ttl)
		}
	}
	return accountToken, nil
}

func (p *KimiTokenProvider) waitForRefreshedToken(ctx context.Context, account *Account, cacheKey string) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, kimiRefreshWaitTimeout)
	defer cancel()
	initialToken := strings.TrimSpace(account.GetCredential("access_token"))
	initialVersion := account.GetCredentialAsInt64("_token_version")
	ticker := time.NewTicker(kimiRefreshPollInterval)
	defer ticker.Stop()
	sawAuthoritativeState := false
	var lastReadErr error

	for {
		if p.accountRepo != nil {
			latest, err := p.accountRepo.GetByID(waitCtx, account.ID)
			if err != nil {
				lastReadErr = err
			} else if latest == nil {
				return "", errOAuthRefreshAccountStateChanged
			} else {
				sawAuthoritativeState = true
				token := strings.TrimSpace(latest.GetCredential("access_token"))
				version := latest.GetCredentialAsInt64("_token_version")
				expiresAt := latest.GetCredentialAsTime("expires_at")
				changed := token != "" && (token != initialToken || (version > 0 && version > initialVersion))
				if changed && expiresAt != nil && time.Now().Before(*expiresAt) {
					if p.tokenCache != nil {
						ttl := time.Until(*expiresAt) - kimiTokenRefreshSkew
						if ttl <= 0 {
							ttl = time.Until(*expiresAt)
						}
						_ = p.tokenCache.SetAccessToken(waitCtx, cacheKey, token, ttl)
					}
					return token, nil
				}
			}
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if !sawAuthoritativeState && lastReadErr != nil {
				return "", fmt.Errorf("%w: %v", errOAuthRefreshAccountRereadFailed, lastReadErr)
			}
			return "", errOAuthRefreshAccountStateChanged
		case <-ticker.C:
		}
	}
}

// RefreshAccount is also used for one controlled upstream-401 retry. A newer
// token already persisted by another worker satisfies this forced refresh.
func (p *KimiTokenProvider) RefreshAccount(ctx context.Context, account *Account) (*Account, error) {
	return p.refreshAccount(ctx, account, true)
}
func (p *KimiTokenProvider) refreshAccount(ctx context.Context, account *Account, recoverState bool) (*Account, error) {
	if p == nil || p.refreshAPI == nil || p.executor == nil {
		return nil, errKimiOAuthRefreshNotConfigured
	}
	timeout := min(15*time.Second, p.refreshAPI.lockTTL/2)
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := p.refreshAPI.ForceKimiRefresh(refreshCtx, account, p.executor)
	if err != nil {
		return nil, err
	}
	if result == nil || result.LockHeld {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "KIMI_OAUTH_REFRESH_BUSY", "Kimi OAuth refresh lock unavailable; retry later")
	}
	if result.Account == nil {
		return nil, errOAuthRefreshAccountStateChanged
	}
	latest := result.Account
	// CAS recovery only the credential generation that actually succeeded.
	if result.Refreshed && recoverState {
		repo, ok := p.accountRepo.(KimiOAuthRepository)
		if !ok {
			return nil, errors.New("kimi OAuth CAS repository is not configured")
		}
		applied, recoveryErr := repo.RecoverKimiOAuthIfUnchanged(ctx, latest.ID, latest.Credentials, latest.ProxyID)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		if applied {
			if p.tempUnschedCache != nil {
				if err := p.tempUnschedCache.DeleteTempUnsched(ctx, latest.ID); err != nil {
					return nil, err
				}
			}
			if p.runtimeBlocker != nil {
				p.runtimeBlocker.ClearAccountSchedulingBlock(latest.ID)
			}
		}
		latest, err = p.accountRepo.GetByID(ctx, latest.ID)
	}
	return latest, err
}
