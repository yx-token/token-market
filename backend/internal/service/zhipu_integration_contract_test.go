//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

func TestZhipuOAuthAccountCreationAndCredentialRedaction(t *testing.T) {
	input := &CreateAccountInput{Name: "GLM", Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "business", "oauth_provider": "bigmodel", "account_mode": AccountModeCoding}}
	// OAuth login supplies api_key later; the generic create validator must not
	// require a separate API-key form field for an OAuth identity.
	a, err := buildAccountForCreate(input, nil)
	require.NoError(t, err)
	require.True(t, a.IsZhipuOAuth())
	a.Credentials["api_key"] = "derived.secret"
	a.Credentials["zcode_jwt"] = "jwt"
	a.Credentials["zai_oauth_access_token"] = "oauth"
	merged := MergePreservingSensitiveCreds(a.Credentials, map[string]any{"base_url": DefaultZhipuCodingBaseURL})
	require.Equal(t, "jwt", merged["zcode_jwt"])
	require.Equal(t, "oauth", merged["zai_oauth_access_token"])
	require.Equal(t, "derived.secret", merged["api_key"])
	for _, key := range []string{"api_key", "access_token", "zcode_jwt", "zai_oauth_access_token"} {
		require.True(t, IsSensitiveCredentialKey(key))
		require.True(t, isAuditSensitiveBodyKey(key))
	}
	repo := &zhipuAccountRepoTest{}
	admin := &adminServiceImpl{accountRepo: repo}
	input.SkipDefaultGroupBind = true
	created, err := admin.CreateAccount(context.Background(), input)
	require.NoError(t, err)
	updated, err := admin.UpdateAccount(context.Background(), created.ID, &UpdateAccountInput{Name: "GLM edited", Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": DefaultZhipuCodingBaseURL, "account_mode": AccountModeCoding}})
	require.NoError(t, err)
	require.Equal(t, "business", updated.GetCredential("access_token"))
	require.True(t, updated.IsZhipuOAuth())
}

func TestZhipuOAuthResultExpirationAndUserWhitelist(t *testing.T) {
	s := NewZhipuOAuthService(nil, nil, nil, "")
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	tokens := &zhipu.TokenSet{Provider: zhipu.ProviderBigModel, AccessToken: "business", ZCodeJWT: "jwt", JWTExpiresAt: expiry, User: map[string]any{"email": "user@example.test", "username": "user", "secret": "never"}}
	result := s.BuildResult(tokens, &zhipu.DerivedKey{Credential: "id.secret", OrganizationID: "org", ProjectID: "proj"})
	require.Equal(t, expiry.Format(time.RFC3339), result.Credentials["zcode_jwt_expires_at"])
	require.Equal(t, expiry.Unix(), result.Display["expires_at"])
	raw, err := json.Marshal(result.Credentials["user_info"])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "never")
	require.Contains(t, result.Credentials, "refresh_token")
	require.Nil(t, result.Credentials["refresh_token"])
	require.Contains(t, result.Credentials, "zai_oauth_access_token")
	require.Nil(t, result.Credentials["zai_oauth_access_token"])
}

func TestZhipuReauthorizationClearsPreviousLifecycle(t *testing.T) {
	for _, provider := range []zhipu.Provider{zhipu.ProviderBigModel, zhipu.ProviderZai} {
		t.Run(string(provider), func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			a := zhipuTestAccount(provider)
			future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
			for _, field := range []string{"business_expires_at", "zcode_jwt_expires_at", "oauth_expires_at"} {
				a.Credentials[field] = future
			}
			a.Credentials["model_mapping"] = map[string]any{"public": "glm-5"}
			result := f.service.BuildResult(&zhipu.TokenSet{Provider: provider, AccessToken: "new-business", ZCodeJWT: "new-jwt", OAuthAccessToken: "new-oauth"}, &zhipu.DerivedKey{Credential: "new.secret", OrganizationID: "org", ProjectID: "proj"})
			incoming := maps.Clone(a.Credentials)
			maps.Copy(incoming, result.Credentials)
			// Exercise the real JSON and generic admin update merge, including
			// sensitive-field preservation. Explicit nulls must survive both.
			body, err := json.Marshal(incoming)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &incoming))
			repo := &zhipuAccountRepoTest{a: a}
			admin := &adminServiceImpl{accountRepo: repo}
			updated, err := admin.UpdateAccount(context.Background(), a.ID, &UpdateAccountInput{Type: AccountTypeOAuth, Credentials: incoming})
			require.NoError(t, err)
			for _, field := range []string{"refresh_token", "business_expires_at", "zcode_jwt_expires_at", "oauth_expires_at"} {
				require.Contains(t, updated.Credentials, field)
				require.Nil(t, updated.Credentials[field], field)
				require.Empty(t, updated.GetCredential(field))
			}
			require.Nil(t, updated.GetCredentialAsTime("business_expires_at"))
			require.Equal(t, "new.secret", updated.GetCredential("api_key"))
			require.Equal(t, a.Credentials["model_mapping"], updated.Credentials["model_mapping"])
			refresher := NewZhipuTokenRefresher(f.service)
			if provider == zhipu.ProviderZai {
				require.Equal(t, true, updated.Credentials["business_expiry_unknown"])
				require.True(t, refresher.NeedsRefresh(updated, time.Minute))
				_, err := refresher.Refresh(context.Background(), updated)
				require.NoError(t, err)
				require.EqualValues(t, 1, f.logins.Load(), "unknown new TTL must trigger business exchange despite the old future TTL")
			} else {
				require.Nil(t, updated.Credentials["zai_oauth_access_token"])
				require.False(t, refresher.NeedsRefresh(updated, time.Minute))
			}
		})
	}
}

func TestZhipuOAuthAuditRedactsCallback(t *testing.T) {
	redacted := RedactAuditBody([]byte(`{"callback_url":"zcode://oauth/callback?authCode=single-use-code&state=state","code":"raw-code"}`), "application/json")
	for _, secret := range []string{"single-use-code", "raw-code"} {
		require.NotContains(t, redacted, secret)
	}
}

func TestZhipuOAuthQuotaAndMonitoringUseDerivedKey(t *testing.T) {
	a := zhipuTestAccount(zhipu.ProviderBigModel)
	a.Credentials["base_url"] = DefaultZhipuCodingBaseURL
	a.Credentials["zhipu_organization"] = "org"
	a.Credentials["zhipu_project"] = "proj"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":10}]}}`))}}
	repo := &zhipuAccountRepoTest{a: a}
	s := NewCNProviderQuotaService(repo, nil, upstream, rawChatCompletionsTestConfig())
	s.zhipuTokens = NewZhipuTokenProvider(repo, nil, nil)
	result, err := s.queryUsageForAccount(context.Background(), a)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "old.secret", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "org", upstream.lastReq.Header.Get("bigmodel-organization"))
	require.Empty(t, upstream.lastReq.URL.RawQuery)
	require.NoError(t, monitorAccountQuotaCapability(a))
	require.True(t, IsUpstreamBillingProbeIdentity(a.Platform, a.Type))
	a.Extra = map[string]any{UpstreamBillingProbeExtraKey: map[string]any{"status": "unsupported"}}
	items := BuildUpstreamBillingRateSnapshotItems([]Account{*a})
	require.NotNil(t, items[0].Snapshot)
}

func TestZhipuDerivedKeyFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		status   int
		body     string
		rejected bool
	}{
		{401, `{"reason":"VERIFY_SIGNATURE_INVALID"}`, false},
		{401, `{"reason":"VERIFY_APIKEY_EXPIRED"}`, true},
		{400, `{"error":{"code":"1002"}}`, true},
		{403, `<html>Cloudflare</html>`, false},
		{403, `{"code":"insufficient_quota"}`, false},
	} {
		require.Equal(t, tc.rejected, zhipuDerivedKeyRejected(tc.status, []byte(tc.body)), tc.body)
	}
}
