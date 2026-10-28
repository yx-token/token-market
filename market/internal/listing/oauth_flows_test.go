package listing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
)

func TestOAuthFlowDispatch(t *testing.T) {
	for _, test := range []struct {
		platform, authType, flow, path, provider string
		response                                 map[string]any
	}{
		{"anthropic", "oauth", oauthLinkCode, "/api/v1/admin/accounts/generate-auth-url", "", map[string]any{"auth_url": "https://auth.test/login", "expires_in": 123}},
		{"kimi", "oauth", oauthDeviceCode, "/api/v1/admin/kimi/oauth/device-code", "", map[string]any{"user_code": "CODE", "verification_uri": "https://auth.test/device", "verification_uri_complete": "https://auth.test/device?code=CODE", "expires_in": 899, "interval": 5}},
		{"zhipu", "oauth", oauthInitPoll, "/api/v1/admin/zhipu/oauth/init", "bigmodel", map[string]any{"authorize_url": "https://auth.test/login", "expires_in": 299, "interval": 2}},
		{"zhipu", "oauth-intl", oauthInitPoll, "/api/v1/admin/zhipu/oauth/init", "zai", map[string]any{"authorize_url": "https://auth.test/login", "expires_in": 298, "interval": 3}},
	} {
		t.Run(test.platform+"/"+test.authType, func(t *testing.T) {
			proxyID := int64(77)
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != test.path {
					t.Errorf("unexpected engine request %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["proxy_id"] != float64(proxyID) || str(body, "provider") != test.provider {
					t.Errorf("unexpected start body: %#v", body)
				}
				response := copyAll(test.response)
				response["session_id"] = "private-engine-session"
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": response})
			}))
			defer engine.Close()
			spec := oauthSpecs[test.platform][test.authType]
			if spec.flowKind() != test.flow || !IsOAuthAuthType(test.platform, test.authType) {
				t.Fatalf("incorrect flow: %+v", spec)
			}
			start, err := startOAuth(context.Background(), adminclient.New(engine.URL, "key", time.Second), spec, &proxyID, "")
			if err != nil {
				t.Fatal(err)
			}
			if start.ExpiresIn != test.response["expires_in"] || start.SessionID != "private-engine-session" {
				t.Fatalf("incorrect start: %+v", start)
			}
			if test.flow == oauthDeviceCode && (start.AuthURL != "" || start.UserCode != "CODE") {
				t.Fatalf("device flow required auth_url or omitted device code: %+v", start)
			}
		})
	}
	if got := SupportedAuthTypes("zhipu"); !reflect.DeepEqual(got, []string{"oauth", "oauth-intl", "coding", "coding-intl"}) {
		t.Fatalf("zhipu supported auth types: %v", got)
	}
	if got := SupportedAuthTypes("kimi"); !reflect.DeepEqual(got, []string{"oauth", "coding"}) {
		t.Fatalf("kimi supported auth types: %v", got)
	}
}

func TestAuthTypeEquivalents(t *testing.T) {
	for _, test := range []struct {
		platform, authType string
		want               []string
	}{
		{"kimi", "oauth", []string{"oauth", "coding", ""}},
		{"zhipu", "oauth", []string{"oauth", "coding", ""}},
		{"zhipu", "oauth-intl", []string{"oauth-intl", "coding-intl", ""}},
		{"anthropic", "oauth", []string{"oauth", ""}},
		{"kimi", "coding", []string{"coding", ""}},
		{"zhipu", "coding-intl", []string{"coding-intl", ""}},
		{"zhipu", "", []string{""}},
	} {
		if got := AuthTypeEquivalents(test.platform, test.authType); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s/%s equivalents = %v, want %v", test.platform, test.authType, got, test.want)
		}
	}
}

func TestCodingOAuthCredentials(t *testing.T) {
	kimi := map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_at": float64(2000000000),
		"token_type": "Bearer", "scope": "coding", "device_id": "device", "client_id": "client", "base_url": "https://untrusted.test"}
	spec := oauthSpecs["kimi"]["oauth"]
	creds, err := normalizeCredentials(spec, kimi)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"access_token", "refresh_token", "expires_at", "token_type", "scope", "device_id", "client_id", "base_url", "account_mode"} {
		if str(creds, key) == "" {
			t.Errorf("missing Kimi credential %s", key)
		}
	}
	if creds["expires_at"] != time.Unix(2000000000, 0).UTC().Format(time.RFC3339) || creds["base_url"] != "https://api.kimi.com/coding/v1" || creds["account_mode"] != "coding" || spec.identity(kimi) != "" {
		t.Fatalf("incorrect Kimi credentials or identity: %#v", creds)
	}
	for _, key := range []string{"access_token", "refresh_token", "expires_at", "device_id", "client_id"} {
		bad := copyAll(kimi)
		delete(bad, key)
		if _, err := normalizeCredentials(spec, bad); err == nil {
			t.Errorf("incomplete Kimi credentials accepted without %s", key)
		}
	}
	for _, authType := range []string{"oauth", "oauth-intl"} {
		spec := oauthSpecs["zhipu"][authType]
		original := map[string]any{"access_token": "business", "api_key": "derived", "refresh_token": "refresh", "zcode_jwt": "jwt", "business_expires_at": nil, "custom_field": "preserved"}
		tok := map[string]any{"credentials": original, "display": map[string]any{"email": "test@example.test", "username": "name"}}
		creds, err := normalizeCredentials(spec, tok)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range original {
			if creds[key] != value {
				t.Errorf("zhipu lost credential %s", key)
			}
		}
		wantBase := "https://open.bigmodel.cn/api/anthropic"
		if authType == "oauth-intl" {
			wantBase = "https://api.z.ai/api/anthropic"
		}
		if creds["account_mode"] != "coding" || creds["base_url"] != wantBase || creds["api_protocol"] != "anthropic" || spec.identity(tok) != "email:test@example.test" {
			t.Fatalf("incorrect GLM credentials: %#v", creds)
		}
		if original["account_mode"] != nil {
			t.Fatal("credential normalization mutated source")
		}
		original["api_protocol"] = "chat_completions"
		creds, err = normalizeCredentials(spec, tok)
		if err != nil || creds["api_protocol"] != "chat_completions" || !strings.HasSuffix(str(creds, "base_url"), "/coding/paas/v4") {
			t.Fatalf("GLM protocol was not preserved: %#v, %v", creds, err)
		}
		delete(tok["display"].(map[string]any), "email")
		if spec.identity(tok) != "username:name" {
			t.Fatal("missing username identity fallback")
		}
	}
}

func TestZhipuManualCallbackDispatch(t *testing.T) {
	for _, callback := range []string{"zcode://oauth/callback?authCode=secret&state=state", "code=secret&state=state", `{"authCode":"secret","state":"state"}`} {
		engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.URL.Path != "/api/v1/admin/zhipu/oauth/exchange-code" || body["callback_url"] != callback || body["session_id"] != "stored-session" || body["proxy_id"] != float64(88) || body["code"] != nil {
				t.Errorf("callback not forwarded intact: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"credentials": map[string]any{"access_token": "access", "api_key": "key"}}})
		}))
		proxyID := int64(88)
		_, err := exchangeOAuth(context.Background(), adminclient.New(engine.URL, "key", time.Second), oauthSpecs["zhipu"]["oauth"], "stored-session", "", callback, &proxyID)
		engine.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := exchangeOAuth(context.Background(), nil, oauthSpecs["kimi"]["oauth"], "session", "", "code", nil); err == nil {
		t.Fatal("device flow accepted manual exchange")
	}
}

func TestOAuthPublicResponsesDoNotExposeSecrets(t *testing.T) {
	for _, response := range []any{
		AuthorizationStart{AuthURL: "", Flow: oauthDeviceCode, UserCode: "USER-CODE", VerificationURI: "https://auth.test/device", Interval: 5},
		AuthorizationPoll{Status: "pending", Interval: 5},
		AuthorizationPoll{Status: "ready", Upstream: &Upstream{ID: 1}},
	} {
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"session_id", "access_token", "refresh_token", "credentials", "token_info"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("public response leaked %s: %s", forbidden, raw)
			}
		}
	}
}
