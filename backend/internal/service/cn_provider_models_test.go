//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

func TestCNModelPickerRefreshesKimiOAuthBeforeDiscovery(t *testing.T) {
	a := newKimiRegressionAccount()
	client := rotatingKimiClient()
	p, _, _ := newKimiRegressionProvider(a, client, nil)
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, "Bearer new-access", req.Header.Get("Authorization"))
		require.Equal(t, "/coding/v1/models", req.URL.Path)
		return ordinaryModelsUpstreamResponse(`{"data":[{"id":"kimi-for-coding"}]}`), nil
	}}
	gateway := newCodexModelsAPIKeyTestService(upstream)
	gateway.SetKimiTokenProvider(p)
	s := &AccountTestService{openaiGatewayService: gateway}
	models, err := s.FetchOpenAIAccountModels(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"kimi-for-coding"}, pickerModelIDs(models))
	require.Equal(t, 1, client.calls)
}

func TestCNModelPickerRefreshesZhipuOAuthBeforeDiscovery(t *testing.T) {
	f := newZhipuOAuthFixture(t)
	f.extra = func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/agent/configs", r.URL.Path)
		zhipuJSON(t, w, 0, map[string]any{})
	}
	a := zhipuTestAccount(zhipu.ProviderZai)
	a.Credentials["base_url"] = "https://models.example/v1"
	a.Credentials["business_expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	p, _ := zhipuTestProvider(f, a)
	calls := 0
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		calls++
		require.Equal(t, "Bearer derived.secret", req.Header.Get("Authorization"))
		require.Equal(t, "/v1/models", req.URL.Path)
		require.NotEmpty(t, req.Header.Get("X-Session-Id"), "must use the shared Zhipu gateway")
		return ordinaryModelsUpstreamResponse(`{"data":[{"id":"glm-5"}]}`), nil
	}}
	gateway := newCodexModelsAPIKeyTestService(upstream)
	gateway.cfg.Gateway.Zhipu.SignatureMode = "never"
	gateway.zhipuTokenProvider = p
	gateway.zhipuGateway = NewZhipuGateway(upstream, f.service.factory, p, gateway.cfg)
	s := &AccountTestService{openaiGatewayService: gateway}
	models, err := s.FetchOpenAIAccountModels(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"glm-5"}, pickerModelIDs(models))
	require.Equal(t, 1, calls)
	require.EqualValues(t, 1, f.logins.Load())
	require.EqualValues(t, 1, f.copies.Load())
}

func TestCNGroupModelCandidatesUseNativeDefaults(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax} {
		require.Equal(t, DefaultCNModelIDs(platform), defaultModelsListCandidateIDs(platform))
	}
	require.Contains(t, defaultModelsListCandidateIDs(PlatformAnthropic), "claude-sonnet-4-6")
}
