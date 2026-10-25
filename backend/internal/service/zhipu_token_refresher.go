package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

const zhipuReauthorizationMessage = "GLM Coding Plan 授权已失效，请重新授权"

var ErrZhipuReauthorizationRequired = infraerrors.New(400, "ZHIPU_REAUTHORIZATION_REQUIRED", zhipuReauthorizationMessage)

type ZhipuTokenRefresher struct {
	oauth      *ZhipuOAuthService
	force      bool
	version    int64
	credential string
}

func NewZhipuTokenRefresher(oauth *ZhipuOAuthService) *ZhipuTokenRefresher {
	return &ZhipuTokenRefresher{oauth: oauth}
}
func ZhipuTokenCacheKey(a *Account) string                { return "zhipu:account:" + strconv.FormatInt(a.ID, 10) }
func (r *ZhipuTokenRefresher) CacheKey(a *Account) string { return ZhipuTokenCacheKey(a) }
func (r *ZhipuTokenRefresher) CanRefresh(a *Account) bool {
	return a != nil && a.IsZhipuOAuth() && !a.IsCredentialShadow()
}
func (r *ZhipuTokenRefresher) NeedsRefresh(a *Account, _ time.Duration) bool {
	if !r.CanRefresh(a) {
		return false
	}
	if r.force && a.GetCredentialAsInt64("_token_version") == r.version && a.GetCredential("api_key") == r.credential {
		return true
	}
	if a.GetCredential("api_key") == "" || a.GetCredential("access_token") == "" {
		return true
	}
	if a.GetCredential("oauth_provider") == string(zhipu.ProviderZai) {
		exp := a.GetCredentialAsTime("business_expires_at")
		unknown, _ := a.Credentials["business_expiry_unknown"].(bool)
		return unknown || exp == nil || time.Until(*exp) <= 5*time.Minute
	}
	return false
}

func (r *ZhipuTokenRefresher) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	provider := zhipu.Provider(a.GetCredential("oauth_provider"))
	if provider != zhipu.ProviderBigModel && provider != zhipu.ProviderZai {
		return nil, ErrZhipuReauthorizationRequired
	}
	proxy, err := r.oauth.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, err
	}
	client, err := r.oauth.factory(proxy, a.ID, a.Concurrency)
	if err != nil {
		return nil, err
	}
	tokens := &zhipu.TokenSet{Provider: provider, AccessToken: a.GetCredential("access_token"), ZCodeJWT: a.GetCredential("zcode_jwt"), RefreshToken: a.GetCredential("refresh_token")}
	businessExchanged := false
	if provider == zhipu.ProviderZai {
		tokens.OAuthAccessToken = a.GetCredential("zai_oauth_access_token")
		exp := a.GetCredentialAsTime("business_expires_at")
		unknown, _ := a.Credentials["business_expiry_unknown"].(bool)
		if exp != nil && !unknown {
			tokens.BusinessExpiresAt = *exp
		}
		// Re-deriving an inference key does not require a new business login when
		// the existing business token is still usable.
		if tokens.AccessToken == "" || tokens.BusinessExpiring(time.Now()) {
			if tokens.OAuthAccessToken == "" {
				return nil, ErrZhipuReauthorizationRequired
			}
			tokens.AccessToken, tokens.BusinessExpiresAt, err = client.ExchangeBusinessToken(ctx, tokens.OAuthAccessToken)
			if err != nil {
				return nil, classifyZhipuCredentialError(err)
			}
			businessExchanged = true
		}
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return nil, ErrZhipuReauthorizationRequired
	}
	key, err := client.DeriveKey(ctx, provider, tokens.AccessToken)
	if provider == zhipu.ProviderZai && !businessExchanged && tokens.OAuthAccessToken != "" && errors.Is(classifyZhipuCredentialError(err), ErrZhipuReauthorizationRequired) {
		// A business token may be revoked before its advertised expiry. Try the
		// retained OAuth token once before declaring the authorization unusable.
		tokens.AccessToken, tokens.BusinessExpiresAt, err = client.ExchangeBusinessToken(ctx, tokens.OAuthAccessToken)
		if err == nil {
			key, err = client.DeriveKey(ctx, provider, tokens.AccessToken)
		}
	}
	if err != nil {
		return nil, classifyZhipuCredentialError(err)
	}
	// No inferred refresh grant, JWT, refresh token or display field is overwritten
	// with a missing response value. DeriveKey always lists before it creates.
	creds := map[string]any{"api_key": key.Credential, "access_token": tokens.AccessToken, "zhipu_organization": key.OrganizationID, "zhipu_project": key.ProjectID, "client_version": r.oauth.version}
	if provider == zhipu.ProviderZai {
		creds["business_expiry_unknown"] = tokens.BusinessExpiresAt.IsZero()
		if !tokens.BusinessExpiresAt.IsZero() {
			creds["business_expires_at"] = tokens.BusinessExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	return MergeCredentials(a.Credentials, creds), nil
}

func classifyZhipuCredentialError(err error) error {
	var upstream *zhipu.APIError
	if errors.As(err, &upstream) && (upstream.Status == 401 || isZhipuCredentialErrorCode(upstream.Code)) {
		return ErrZhipuReauthorizationRequired
	}
	return err
}

func isZhipuCredentialErrorCode(code string) bool {
	switch strings.ToUpper(code) {
	case "VERIFY_APIKEY_EXPIRED", "INVALID_TOKEN", "TOKEN_EXPIRED", "INVALID_GRANT", "UNAUTHORIZED", "401", "1000", "1001", "1002", "1003", "1004":
		return true
	}
	return false
}
