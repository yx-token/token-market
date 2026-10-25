//go:build unit

package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/config"
	"github.com/yx-token/token-market/internal/pkg/openai"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
	"github.com/yx-token/token-market/internal/service"
)

type cnModelsHTTPUpstream struct {
	service.HTTPUpstream
	requests []*http.Request
	status   int
	body     string
}

func (u *cnModelsHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req.Clone(req.Context()))
	return &http.Response{StatusCode: u.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func cnModelsRouter(t *testing.T, account service.Account, upstream *cnModelsHTTPUpstream) *gin.Engine {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.Zhipu.SignatureMode = "never"
	kimiProvider := service.NewKimiTokenProvider(nil, nil)
	zhipuProvider := service.NewZhipuTokenProvider(nil, nil, nil)
	configServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/agent/configs", r.URL.Path)
		_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
	}))
	t.Cleanup(configServer.Close)
	factory := func(string, int64, int) (*zhipu.Client, error) {
		return zhipu.NewClient(zhipu.Options{HTTPClient: configServer.Client(), ZCodeOrigin: configServer.URL})
	}
	zhipuGateway := service.NewZhipuGateway(upstream, factory, zhipuProvider, cfg)
	gateway := service.ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream,
		nil, nil, nil, nil, nil, nil, nil, nil, kimiProvider, zhipuProvider, zhipuGateway,
	)
	testSvc := service.NewAccountTestService(nil, nil, nil, nil, nil, upstream, cfg, nil)
	testSvc.SetOpenAIGatewayService(gateway)
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	h := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, testSvc, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/accounts/:id/models", h.GetAvailableModels)
	return router
}

func TestAccountHandlerCNModelsDiscoveryAndFallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, identity := range []struct{ platform, accountType string }{
		{service.PlatformKimi, service.AccountTypeOAuth},
		{service.PlatformZhipu, service.AccountTypeOAuth},
		{service.PlatformKimi, service.AccountTypeAPIKey},
		{service.PlatformZhipu, service.AccountTypeAPIKey},
		{service.PlatformDeepseek, service.AccountTypeAPIKey},
		{service.PlatformMiniMax, service.AccountTypeAPIKey},
	} {
		for _, source := range []string{"upstream", "mapping", "defaults", "empty upstream"} {
			t.Run(identity.platform+"/"+identity.accountType+"/"+source, func(t *testing.T) {
				a := service.Account{ID: 91, Platform: identity.platform, Type: identity.accountType, Status: service.StatusActive, Concurrency: 1,
					Credentials: map[string]any{"base_url": "https://models.example/v1", "account_mode": service.AccountModeCoding,
						"api_key": "key.secret", "access_token": "fresh-token", "refresh_token": "refresh",
						"expires_at": time.Now().Add(time.Hour).Unix(), "device_id": "device-91", "oauth_provider": "bigmodel"}}
				u := &cnModelsHTTPUpstream{status: http.StatusOK, body: `{"data":[{"id":"native-model","display_name":"Native Model"}]}`}
				want := []string{"native-model"}
				switch source {
				case "mapping":
					a.Credentials["model_mapping"] = map[string]any{"custom-model": "native-model", "native-*": "native-*"}
					u.status, u.body = 400, `{"error":"discovery unavailable"}`
					want = []string{"custom-model"}
				case "defaults":
					u.status, u.body = 400, `{"error":"discovery unavailable"}`
					want = nil
					for _, model := range service.DefaultCNAccountModels(&a) {
						want = append(want, model.ID)
					}
				case "empty upstream":
					u.body, want = `{"data":[]}`, []string{}
				}
				router := cnModelsRouter(t, a, u)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/91/models", nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var response struct {
					Data []openai.Model `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				got := make([]string, 0, len(response.Data))
				for _, model := range response.Data {
					got = append(got, model.ID)
					require.NotContains(t, strings.ToLower(model.ID), "claude")
				}
				require.Equal(t, want, got)
				require.NotEmpty(t, u.requests, "must try live discovery before local fallback")
				require.Equal(t, "/v1/models", u.requests[0].URL.Path)
				if identity.platform == service.PlatformKimi && identity.accountType == service.AccountTypeOAuth {
					require.Equal(t, "Bearer fresh-token", u.requests[0].Header.Get("Authorization"))
					require.Equal(t, "device-91", u.requests[0].Header.Get("X-Msh-Device-Id"))
				} else {
					require.Equal(t, "Bearer key.secret", u.requests[0].Header.Get("Authorization"))
				}
			})
		}
	}
}

func TestCNAccountModelsPreserveAnthropicFallback(t *testing.T) {
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 91, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}}
	rec := httptest.NewRecorder()
	setupAvailableModelsRouter(adminSvc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/91/models", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "claude-sonnet")
}
