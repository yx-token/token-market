package service

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// ZhipuOAuthRefreshRepository protects credential writes and terminal errors from
// concurrent admin reauthorization. Production implements both operations as CAS.
type ZhipuOAuthRefreshRepository interface {
	UpdateZhipuOAuthCredentialsIfUnchanged(context.Context, int64, map[string]any, *int64, map[string]any) (bool, error)
	SetZhipuOAuthReauthorizationRequiredIfUnchanged(context.Context, int64, map[string]any, *int64, string) (bool, error)
}

type ZhipuTokenProvider struct {
	repo       AccountRepository
	refreshAPI *OAuthRefreshAPI
	oauth      *ZhipuOAuthService
}

func NewZhipuTokenProvider(repo AccountRepository, api *OAuthRefreshAPI, oauth *ZhipuOAuthService) *ZhipuTokenProvider {
	return &ZhipuTokenProvider{repo: repo, refreshAPI: api, oauth: oauth}
}

func (p *ZhipuTokenProvider) GetAccessToken(ctx context.Context, a *Account) (string, error) {
	if a == nil || !a.IsZhipuOAuth() {
		return "", errors.New("not a zhipu oauth account")
	}
	if !a.IsActive() {
		return "", ErrZhipuReauthorizationRequired
	}
	// Lifecycle refresh happens once at the shared gateway send boundary.
	if key := a.GetCredential("api_key"); key != "" {
		return key, nil
	}
	current, err := p.Account(ctx, a, false)
	if err != nil {
		return "", err
	}
	return current.GetCredential("api_key"), nil
}

func (p *ZhipuTokenProvider) Account(ctx context.Context, a *Account, force bool) (*Account, error) {
	if a == nil || !a.IsZhipuOAuth() {
		return nil, errors.New("not a zhipu oauth account")
	}
	if !a.IsActive() {
		return nil, ErrZhipuReauthorizationRequired
	}
	r := NewZhipuTokenRefresher(p.oauth)
	r.force, r.version, r.credential = force, a.GetCredentialAsInt64("_token_version"), a.GetCredential("api_key")
	if !r.NeedsRefresh(a, 5*time.Minute) {
		return a, nil
	}
	if p.refreshAPI == nil {
		return nil, errors.New("zhipu OAuth refresh is not configured")
	}
	result, err := p.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(ctx), a, r, 5*time.Minute)
	if err != nil {
		if errors.Is(err, ErrZhipuReauthorizationRequired) {
			attempted := a
			if result != nil && result.Account != nil {
				attempted = result.Account
			}
			if markErr := p.RequireReauthorization(ctx, attempted); markErr != nil {
				return nil, markErr
			}
		}
		return nil, err
	}
	if result != nil && result.LockHeld {
		wait, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			latest, readErr := p.repo.GetByID(wait, a.ID)
			if readErr == nil && latest != nil {
				if !latest.IsZhipuOAuth() || !latest.IsActive() {
					return nil, ErrZhipuReauthorizationRequired
				}
				if (!reflect.DeepEqual(latest.Credentials, a.Credentials) || !reflect.DeepEqual(latest.ProxyID, a.ProxyID)) && latest.GetCredential("api_key") != "" {
					return latest, nil
				}
			}
			select {
			case <-wait.Done():
				return nil, wait.Err()
			case <-ticker.C:
			}
		}
	}
	if result == nil || result.Account == nil {
		return nil, errors.New("zhipu OAuth refresh did not return durable account")
	}
	if !result.Account.IsZhipuOAuth() || !result.Account.IsActive() || result.Account.GetCredential("api_key") == "" {
		return nil, ErrZhipuReauthorizationRequired
	}
	return result.Account, nil
}

func (p *ZhipuTokenProvider) RequireReauthorization(ctx context.Context, a *Account) error {
	repo, ok := p.repo.(ZhipuOAuthRefreshRepository)
	if !ok {
		return errors.New("zhipu OAuth CAS repository is not configured")
	}
	_, err := repo.SetZhipuOAuthReauthorizationRequiredIfUnchanged(ctx, a.ID, a.Credentials, a.ProxyID, zhipuReauthorizationMessage)
	return err
}
