//go:build unit

package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9" //nolint:depguard // Test-only integration fixture exercises the production Redis/Lua protocol store.
	"github.com/stretchr/testify/require"
	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/internal/pkg/zhipu"
)

type zhipuOAuthFixture struct {
	service                          *ZhipuOAuthService
	redis                            *miniredis.Miniredis
	server                           *httptest.Server
	client                           *zhipu.Client
	status                           atomic.Value
	polls, exchanges, logins, copies atomic.Int32
	denyBusiness                     atomic.Bool
	loginTTL                         atomic.Int64
	extra                            http.HandlerFunc
}

func zhipuJSON(t *testing.T, w http.ResponseWriter, code int, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"code": code, "data": data}); err != nil {
		t.Error(err)
	}
}

func newZhipuOAuthFixture(t *testing.T) *zhipuOAuthFixture {
	t.Helper()
	f := &zhipuOAuthFixture{redis: miniredis.RunT(t)}
	f.status.Store("pending")
	f.loginTTL.Store(3600)
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/oauth/cli/init":
			poll := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			b, err := hex.DecodeString(poll)
			if err != nil || len(b) != 32 {
				t.Error("server poll token must contain 32 random bytes")
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			provider := body["provider"]
			endpoint, redirectParam := "https://bigmodel.cn/login", "redirect"
			if provider == "zai" {
				endpoint, redirectParam = "https://chat.z.ai/api/oauth/authorize", "redirect_uri"
			}
			query := url.Values{"state": {"stored-state"}, "flow_id": {"flow"}, "extra": {"a+b", "second"}, redirectParam: {"https://zcode.z.ai/api/v1/oauth/cli/callback/" + provider}}
			otherParam := "redirect_uri"
			if provider == "zai" {
				otherParam = "redirect"
			}
			query.Set(otherParam, "preserve-other-parameter")
			zhipuJSON(t, w, 0, map[string]any{"flow_id": "flow", "authorize_url": endpoint + "?" + query.Encode(), "expires_at": time.Now().Add(5 * time.Minute).Unix(), "poll_interval_sec": 2})
		case "/api/v1/oauth/cli/poll/flow":
			f.polls.Add(1)
			data := map[string]any{"status": f.status.Load()}
			if f.status.Load() == "ready" {
				data["token"] = "unknown-jwt"
				data["bigmodel"] = map[string]any{"access_token": "bigmodel-business", "refresh_token": "refresh"}
				data["zai"] = map[string]any{"access_token": "zai-oauth"}
				data["user"] = map[string]any{"user_id": "u", "username": "user", "email": "u@example.test", "password": "must-not-persist"}
			}
			zhipuJSON(t, w, 0, data)
		case "/api/v1/oauth/token":
			f.exchanges.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["state"] != "stored-state" || body["redirect_uri"] != f.client.DesktopRedirectURI() {
				t.Error("state/redirect changed")
			}
			zhipuJSON(t, w, 0, map[string]any{"token": "unknown-jwt", "bigmodel": map[string]any{"accessToken": "bigmodel-business"}, "zai": map[string]any{"access_token": "zai-oauth"}})
		case "/api/auth/z/login":
			f.logins.Add(1)
			if f.denyBusiness.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				zhipuJSON(t, w, 401, map[string]any{})
				return
			}
			zhipuJSON(t, w, 200, map[string]any{"access_token": "zai-business", "expires_in": f.loginTTL.Load()})
		case "/api/biz/customer/getCustomerInfo":
			if f.denyBusiness.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				zhipuJSON(t, w, 401, map[string]any{})
				return
			}
			zhipuJSON(t, w, 200, map[string]any{"username": "user", "organizations": []any{map[string]any{"organizationId": "org", "organizationName": "默认机构", "projects": []any{map[string]any{"projectId": "proj", "projectType": 1, "projectName": "默认项目"}}}}})
		case "/api/biz/v1/organization/org/projects/proj/api_keys":
			if r.Method != http.MethodGet {
				t.Error("existing key must not be recreated")
			}
			zhipuJSON(t, w, 200, []any{map[string]any{"name": "zcode-api-key", "apiKey": "derived"}})
		case "/api/biz/v1/organization/org/projects/proj/api_keys/copy/derived":
			f.copies.Add(1)
			zhipuJSON(t, w, 200, map[string]any{"secretKey": "secret"})
		case "/api/oauth/userinfo":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			if f.extra != nil {
				f.extra(w, r)
				return
			}
			t.Errorf("unexpected OAuth endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	var err error
	f.client, err = zhipu.NewClient(zhipu.Options{HTTPClient: f.server.Client(), ZCodeOrigin: f.server.URL, BigModelOrigin: f.server.URL, ZaiOrigin: f.server.URL, ZaiChatOrigin: f.server.URL})
	require.NoError(t, err)
	rc := redis.NewClient(&redis.Options{Addr: f.redis.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	f.service = NewZhipuOAuthService(nil, func(string, int64, int) (*zhipu.Client, error) { return f.client, nil }, zhipu.NewRedisOAuthSessionStore(rc), "")
	return f
}

func (f *zhipuOAuthFixture) allowPoll(t *testing.T, id string) {
	t.Helper()
	s, err := f.service.sessions.Acquire(context.Background(), id, "test")
	require.NoError(t, err)
	s.NextPollAt = time.Time{}
	require.NoError(t, f.service.sessions.Save(context.Background(), id, "test", s))
}

func TestZhipuOAuthBuildResultUsesCodingAccountMode(t *testing.T) {
	for _, provider := range []zhipu.Provider{zhipu.ProviderBigModel, zhipu.ProviderZai} {
		t.Run(string(provider), func(t *testing.T) {
			oauthService := &ZhipuOAuthService{version: "test"}
			result := oauthService.BuildResult(&zhipu.TokenSet{Provider: provider}, &zhipu.DerivedKey{Credential: "derived.secret"})
			require.Equal(t, "coding", result.Credentials["account_mode"])
		})
	}
}

func TestZhipuOAuthInitAndPollLifecycle(t *testing.T) {
	for _, provider := range []zhipu.Provider{zhipu.ProviderBigModel, zhipu.ProviderZai} {
		t.Run(string(provider), func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			ctx := context.Background()
			init, err := f.service.Init(ctx, provider, nil)
			require.NoError(t, err)
			require.Equal(t, provider, init.Provider)
			require.Equal(t, 2, init.Interval)
			require.InDelta(t, 300, init.ExpiresIn, 2)
			raw, err := json.Marshal(init)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "poll_token")
			require.InDelta(t, 300, f.redis.TTL("zhipu:oauth:session:"+init.SessionID).Seconds(), 2)
			pending, err := f.service.Poll(ctx, init.SessionID, nil)
			require.NoError(t, err)
			require.Equal(t, "pending", pending.Status)
			_, err = f.service.Poll(ctx, init.SessionID, nil)
			require.NoError(t, err)
			require.EqualValues(t, 1, f.polls.Load())
			f.status.Store("ready")
			f.allowPoll(t, init.SessionID)
			result, err := f.service.Poll(ctx, init.SessionID, nil)
			require.NoError(t, err)
			require.Equal(t, "success", result.Status)
			require.Equal(t, "coding", result.Result.Credentials["account_mode"])
			require.Equal(t, "derived.secret", result.Result.Credentials["api_key"])
			require.Equal(t, "org", result.Result.Credentials["zhipu_organization"])
			require.NotContains(t, result.Result.Credentials["user_info"], "password")
			require.Contains(t, result.Result.Credentials, "zcode_jwt_expires_at")
			require.Nil(t, result.Result.Credentials["zcode_jwt_expires_at"])
			if provider == zhipu.ProviderZai {
				require.Equal(t, "zai-oauth", result.Result.Credentials["zai_oauth_access_token"])
				require.Equal(t, "zai-business", result.Result.Credentials["access_token"])
			}
			replayed, err := f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID, CallbackURL: "already-consumed"})
			require.NoError(t, err)
			require.Equal(t, result.Result, replayed)
			polled, err := f.service.Poll(ctx, init.SessionID, nil)
			require.NoError(t, err)
			require.Equal(t, result, polled)
			require.EqualValues(t, 0, f.exchanges.Load())
			require.EqualValues(t, 1, f.copies.Load())
			stored, err := f.redis.Get("zhipu:oauth:session:" + init.SessionID)
			require.NoError(t, err)
			require.NotContains(t, stored, "poll_token")
			require.NotContains(t, stored, "flow_id")
		})
	}
}

func TestZhipuOAuthInitRedirectMatchesManualExchange(t *testing.T) {
	for _, provider := range []zhipu.Provider{zhipu.ProviderBigModel, zhipu.ProviderZai} {
		t.Run(string(provider), func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			ctx := context.Background()
			init, err := f.service.Init(ctx, provider, nil)
			require.NoError(t, err)
			authorize, err := url.Parse(init.AuthorizeURL)
			require.NoError(t, err)
			param, codeParam := "redirect", "authCode"
			if provider == zhipu.ProviderZai {
				param, codeParam = "redirect_uri", "code"
			}
			query := authorize.Query()
			otherParam := "redirect_uri"
			if provider == zhipu.ProviderZai {
				otherParam = "redirect"
			}
			require.Equal(t, "preserve-other-parameter", query.Get(otherParam))
			require.Equal(t, f.client.DesktopRedirectURI(), query.Get(param))
			require.Equal(t, "stored-state", query.Get("state"))
			require.Equal(t, "flow", query.Get("flow_id"))
			require.Equal(t, []string{"a+b", "second"}, query["extra"])
			session, err := f.service.sessions.Acquire(ctx, init.SessionID, "assert-redirect")
			require.NoError(t, err)
			require.Equal(t, query.Get(param), session.RedirectURI)
			require.NoError(t, f.service.sessions.Save(ctx, init.SessionID, "assert-redirect", session))

			// The intermediate page's Open ZCode button copies passthrough and
			// attribution parameters into this exact deep-link form.
			href := "zcode://oauth/callback?" + url.Values{codeParam: {"one+time/code=="}, "state": {query.Get("state")}, "flow_id": {"flow"}, "app_version": {zhipu.ClientVersion}, "channel_id": {"web"}, "utm_source": {"coding"}}.Encode()
			callback, err := zhipu.ParseAuthorizationInput(provider, href)
			require.NoError(t, err)
			require.Equal(t, "one+time/code==", callback.Code)
			require.Equal(t, "web", callback.ChannelID)
			result, err := f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID, CallbackURL: href})
			require.NoError(t, err)
			require.Equal(t, provider, result.Provider)
			require.Equal(t, "derived.secret", result.Credentials["api_key"])
			require.EqualValues(t, 1, f.exchanges.Load())
		})
	}
}

func TestZhipuOAuthTerminalStatesAndBusy(t *testing.T) {
	for _, status := range []string{"failed", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			ctx := context.Background()
			init, err := f.service.Init(ctx, zhipu.ProviderBigModel, nil)
			require.NoError(t, err)
			f.status.Store(status)
			result, err := f.service.Poll(ctx, init.SessionID, nil)
			require.NoError(t, err)
			require.Equal(t, status, result.Status)
			_, err = f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID, Code: "c", State: "stored-state"})
			require.Equal(t, 409, infraerrors.Code(err))
		})
	}
	f := newZhipuOAuthFixture(t)
	ctx := context.Background()
	init, err := f.service.Init(ctx, zhipu.ProviderBigModel, nil)
	require.NoError(t, err)
	_, err = f.service.sessions.Acquire(ctx, init.SessionID, "owner")
	require.NoError(t, err)
	busy, err := f.service.Poll(ctx, init.SessionID, nil)
	require.NoError(t, err)
	require.Equal(t, "pending", busy.Status)
	require.Zero(t, f.polls.Load())
	_, err = f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID})
	require.ErrorIs(t, err, zhipu.ErrSessionBusy)
	f.redis.FastForward(6 * time.Minute)
	expired, err := f.service.Poll(ctx, init.SessionID, nil)
	require.NoError(t, err)
	require.Equal(t, "expired", expired.Status)
}

func TestZhipuOAuthManualStateValidationAndConsumption(t *testing.T) {
	for _, input := range []ZhipuExchangeCodeInput{
		{CallbackURL: "zcode://oauth/callback?authCode=c&state=wrong"},
		{CallbackURL: "invalid"},
		{Code: "c", State: "wrong"},
	} {
		t.Run(input.CallbackURL+input.State, func(t *testing.T) {
			f := newZhipuOAuthFixture(t)
			ctx := context.Background()
			init, err := f.service.Init(ctx, zhipu.ProviderBigModel, nil)
			require.NoError(t, err)
			input.SessionID = init.SessionID
			_, err = f.service.ExchangeCode(ctx, input)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Zero(t, f.exchanges.Load())
			result, err := f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID, CallbackURL: " zcode://oauth/callback?authCode=c&state=stored-state "})
			require.NoError(t, err)
			require.Equal(t, "derived.secret", result.Credentials["api_key"])
			_, err = f.service.ExchangeCode(ctx, ZhipuExchangeCodeInput{SessionID: init.SessionID, Code: "c", State: "stored-state"})
			require.NoError(t, err)
			require.EqualValues(t, 1, f.exchanges.Load())
		})
	}
}
