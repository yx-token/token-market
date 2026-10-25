//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9" //nolint:depguard // Test-only fixture checks HTTP contracts against the production Redis session store.
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
	"github.com/yx-token/token-market/internal/service"
)

func TestZhipuOAuthHandlerFrontendContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := "pending"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{"status": status}
		if strings.HasSuffix(r.URL.Path, "/init") {
			data = map[string]any{"flow_id": "f", "authorize_url": "https://bigmodel.cn/login?state=s", "expires_at": time.Now().Add(5 * time.Minute).Unix(), "poll_interval_sec": 2}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := zhipu.NewClient(zhipu.Options{HTTPClient: srv.Client(), ZCodeOrigin: srv.URL})
	require.NoError(t, err)
	r := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	sessions := zhipu.NewRedisOAuthSessionStore(rc)
	oauth := service.NewZhipuOAuthService(nil, func(string, int64, int) (*zhipu.Client, error) { return client, nil }, sessions, "")
	h := NewZhipuOAuthHandler(oauth, nil, nil)
	router := gin.New()
	router.POST("/admin/zhipu/oauth/init", h.Init)
	router.POST("/admin/zhipu/oauth/poll", h.Poll)
	router.POST("/admin/zhipu/oauth/exchange-code", h.ExchangeCode)
	post := func(path, body string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/zhipu/oauth/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		var value map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &value))
		return rec.Code, value
	}
	object := func(value any) map[string]any {
		t.Helper()
		data, ok := value.(map[string]any)
		require.True(t, ok, "expected JSON object, got %T", value)
		return data
	}
	code, _ := post("init", `{"provider":"invalid"}`)
	require.Equal(t, 400, code)
	for _, terminal := range []string{"pending", "failed", "expired"} {
		status = terminal
		code, init := post("init", `{"provider":"bigmodel"}`)
		require.Equal(t, 200, code)
		data := object(init["data"])
		require.Equal(t, "bigmodel", data["provider"])
		require.EqualValues(t, 2, data["interval"])
		require.NotContains(t, data, "poll_token")
		require.InDelta(t, time.Now().Add(5*time.Minute).Unix(), data["expires_at"], 2)
		sessionID, ok := data["session_id"].(string)
		require.True(t, ok)
		body := `{"session_id":"` + sessionID + `"}`
		code, result := post("poll", body)
		require.Equal(t, 200, code)
		require.Equal(t, terminal, object(result["data"])["status"])
	}
	code, value := post("init", `{"provider":"bigmodel"}`)
	require.Equal(t, 200, code)
	id, ok := object(value["data"])["session_id"].(string)
	require.True(t, ok)
	code, _ = post("exchange-code", `{"session_id":"`+id+`","callback_url":"code=c&state=wrong"}`)
	require.Equal(t, 400, code)
	code, _ = post("exchange-code", `{"session_id":"`+id+`","callback_url":"malformed"}`)
	require.Equal(t, 400, code)
	code, _ = post("exchange-code", `{"session_id":"missing","code":"c","state":"s"}`)
	require.Equal(t, 400, code)
	result, err := json.Marshal(service.ZhipuOAuthResult{Provider: zhipu.ProviderZai, Credentials: map[string]any{"api_key": "id.secret"}, Display: map[string]any{}})
	require.NoError(t, err)
	require.NoError(t, sessions.Create(context.Background(), "consumed", &zhipu.OAuthSession{Status: "success", ExpiresAt: time.Now().Add(time.Minute), Result: result}))
	code, replayed := post("exchange-code", `{"session_id":"consumed"}`)
	require.Equal(t, 200, code)
	require.Equal(t, "id.secret", object(object(replayed["data"])["credentials"])["api_key"])
	_, polled := post("poll", `{"session_id":"consumed"}`)
	require.Equal(t, "success", object(polled["data"])["status"])
}

type zhipuRefreshHandlerRepo struct {
	service.AccountRepository
	a *service.Account
}

func (r *zhipuRefreshHandlerRepo) GetByID(context.Context, int64) (*service.Account, error) {
	a := *r.a
	a.Credentials = maps.Clone(r.a.Credentials)
	return &a, nil
}
func (r *zhipuRefreshHandlerRepo) UpdateZhipuOAuthCredentialsIfUnchanged(_ context.Context, _ int64, _ map[string]any, _ *int64, next map[string]any) (bool, error) {
	r.a.Credentials = next
	return true, nil
}
func (r *zhipuRefreshHandlerRepo) SetZhipuOAuthReauthorizationRequiredIfUnchanged(context.Context, int64, map[string]any, *int64, string) (bool, error) {
	return false, nil
}

type zhipuRefreshHandlerAdmin struct {
	service.AdminService
	repo *zhipuRefreshHandlerRepo
}

func (a zhipuRefreshHandlerAdmin) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	return a.repo.GetByID(ctx, id)
}

type zhipuRefreshHTTP func(*http.Request) (*http.Response, error)

func (f zhipuRefreshHTTP) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestZhipuOAuthRefreshHandlerReturnsAccountDTO(t *testing.T) {
	repo := &zhipuRefreshHandlerRepo{a: &service.Account{ID: 7, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"oauth_provider": "bigmodel", "access_token": "business", "api_key": "old.secret", "zcode_jwt": "jwt-secret", "zai_oauth_access_token": "oauth-secret"}}}
	httpClient := zhipuRefreshHTTP(func(r *http.Request) (*http.Response, error) {
		body := `{"code":200,"data":{"organizations":[{"organizationId":"o","projects":[{"projectId":"p","projectType":1}]}]}}`
		if strings.HasSuffix(r.URL.Path, "/api_keys") {
			body = `{"code":200,"data":[{"name":"zcode-api-key","apiKey":"key"}]}`
		}
		if strings.Contains(r.URL.Path, "/copy/") {
			body = `{"code":200,"data":{"secretKey":"secret"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	client, err := zhipu.NewClient(zhipu.Options{HTTPClient: httpClient})
	require.NoError(t, err)
	oauth := service.NewZhipuOAuthService(nil, func(string, int64, int) (*zhipu.Client, error) { return client, nil }, nil, "")
	tokens := service.NewZhipuTokenProvider(repo, service.NewOAuthRefreshAPI(repo, nil), oauth)
	h := NewZhipuOAuthHandler(oauth, tokens, zhipuRefreshHandlerAdmin{repo: repo})
	router := gin.New()
	router.POST("/admin/zhipu/accounts/:id/refresh", h.RefreshAccountToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/zhipu/accounts/7/refresh", nil))
	require.Equal(t, 200, response.Code)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.EqualValues(t, 7, envelope.Data["id"])
	require.Equal(t, "oauth", envelope.Data["type"])
	require.NotContains(t, envelope.Data, "account")
	require.NotContains(t, response.Body.String(), "jwt-secret")
	require.NotContains(t, response.Body.String(), "oauth-secret")
	require.Equal(t, "key.secret", repo.a.GetCredential("api_key"))
}
