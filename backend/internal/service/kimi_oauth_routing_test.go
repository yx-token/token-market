//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/kimi"
)

func TestKimiOAuthProtocolRoutingUsesCredentialsBaseURL(t *testing.T) {
	account := &Account{
		Platform: PlatformKimi,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "oauth-access",
			"base_url":     "https://api.kimi.com/coding/v1",
			"api_protocol": APIProtocolAnthropic,
			"account_mode": AccountModeCoding,
		},
	}
	require.Equal(t, APIProtocolAnthropic, account.GetAPIProtocol())
	require.Equal(t, "https://api.kimi.com/coding", account.GetAnthropicProtocolBaseURL())
	require.Empty(t, account.GetOpenAIProtocolAPIKey())
	require.Empty(t, account.GetCNAPIKey())

	account.Credentials["api_protocol"] = APIProtocolAdaptive
	account.Credentials["api_base_urls"] = map[string]any{
		APIProtocolChatCompletions: "https://relay.example/chat/v1",
		APIProtocolAnthropic:       "https://relay.example/anthropic",
	}
	require.Equal(t, "https://relay.example/chat/v1", account.GetOpenAIBaseURL())
	require.Equal(t, "https://relay.example/anthropic", account.GetAnthropicProtocolBaseURL())
}

func TestKimiOAuthNativeAnthropicRequestUsesBearerAndDeviceHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := &Account{
		ID:       71,
		Platform: PlatformKimi,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "oauth-access",
			"device_id":    "device-71",
			"base_url":     kimi.DefaultBaseURL,
			"api_protocol": APIProtocolAnthropic,
			"account_mode": AccountModeCoding,
		},
	}

	svc := &OpenAIGatewayService{}
	req, _, err := svc.buildNativeAnthropicUpstreamRequest(
		context.Background(), c, account,
		[]byte(`{"model":"kimi-k2","messages":[]}`),
		"oauth-access", "https://api.kimi.com/coding/v1/v1/messages",
	)
	require.NoError(t, err)
	require.Equal(t, "Bearer oauth-access", req.Header.Get("Authorization"))
	require.Empty(t, req.Header.Get("X-Api-Key"))
	require.Equal(t, "device-71", req.Header.Get("X-Msh-Device-Id"))
	require.Equal(t, "kimi_code_cli", req.Header.Get("X-Msh-Platform"))
}

func TestKimiTokenProviderRefreshesExpiredToken(t *testing.T) {
	account := &Account{
		ID:       72,
		Platform: PlatformKimi,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
			"expires_at":    time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"account_mode":  AccountModeCoding,
		},
	}
	repo := &kimiCASAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	oauthSvc := NewKimiOAuthService(nil, &kimiOAuthClientStub{
		refresh: &kimi.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 900, Scope: "kimi-code"},
	})
	provider := NewKimiTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), NewKimiTokenRefresher(oauthSvc))

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-access", token)
	require.Equal(t, "new-refresh", repo.accountsByID[account.ID].GetCredential("refresh_token"))
	require.Equal(t, "new-access", cache.setToken)
}
