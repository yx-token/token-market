//go:build unit

package zhipu

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	return newTestClient(t, handler, false)
}

func newTestClient(t *testing.T, handler http.HandlerFunc, insecureHTTP bool) (*Client, *httptest.Server) {
	t.Helper()
	var server *httptest.Server
	if insecureHTTP {
		server = httptest.NewServer(handler)
	} else {
		server = httptest.NewTLSServer(handler)
	}
	t.Cleanup(server.Close)
	c, err := NewClient(Options{HTTPClient: server.Client(), ZCodeOrigin: server.URL, BigModelOrigin: server.URL, ZaiOrigin: server.URL, ZaiChatOrigin: server.URL, AllowInsecureHTTP: insecureHTTP})
	if err != nil {
		t.Fatal(err)
	}
	return c, server
}

func respond(t *testing.T, w http.ResponseWriter, code any, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"code": code, "data": data}); err != nil {
		t.Error(err)
	}
}

func testJWT(exp string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":`+exp+`}`)) + ".fake"
}

func TestCLILogin(t *testing.T) {
	for _, provider := range []Provider{ProviderBigModel, ProviderZai} {
		t.Run(string(provider), func(t *testing.T) {
			token, err := GeneratePollToken()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := hex.DecodeString(token)
			if err != nil || len(decoded) != 32 || strings.ToLower(token) != token {
				t.Fatal("invalid poll token")
			}
			status := "pending"
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("poll auth mismatch")
				}
				switch r.URL.Path {
				case "/api/v1/oauth/cli/init":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || body["provider"] != string(provider) {
						t.Error("init request mismatch")
					}
					respond(t, w, 0, map[string]any{"flow_id": "flow", "authorize_url": "https://bigmodel.cn/login?state=x", "poll_token": "ignored-server-token", "expires_at": 2000000000, "poll_interval_sec": 2})
				case "/api/v1/oauth/cli/poll/flow":
					if r.Method != http.MethodGet {
						t.Error("poll must GET")
					}
					data := map[string]any{"status": status}
					if status == "ready" {
						data["token"] = testJWT("2000000000")
						data[string(provider)] = map[string]string{"access_token": "oauth"}
						data["user"] = map[string]string{"user_id": "u"}
					}
					respond(t, w, 0, data)
				default:
					t.Error("unexpected endpoint")
					w.WriteHeader(http.StatusNotFound)
				}
			})
			init, err := c.Init(context.Background(), provider, token)
			if err != nil {
				t.Fatal(err)
			}
			if init.FlowID != "flow" || init.PollIntervalSec != 2 || !init.ExpiresAt.Equal(time.Unix(2000000000, 0)) {
				t.Fatalf("unexpected init: %+v", init)
			}
			for _, next := range []string{"pending", "ready", "failed", "expired", "unknown"} {
				status = next
				result, err := c.Poll(context.Background(), provider, init.FlowID, token)
				if next == "unknown" {
					if err == nil {
						t.Fatal("unknown state accepted")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if string(result.Status) != next {
					t.Fatal("status mismatch")
				}
				if next == "ready" && (result.Tokens == nil || result.Tokens.Token == "") {
					t.Fatal("missing ready tokens")
				}
			}
		})
	}
}

func TestOAuthProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		payload string
	}{
		{"http", 500, `{"code":2007,"msg":"secret must not escape"}`},
		{"business", 200, `{"code":2007,"data":{}}`},
		{"explicit_failure", 200, `{"code":0,"success":false,"data":{"status":"pending"}}`},
		{"json", 200, `broken`}, {"missing", 200, `{"code":0}`},
		{"incomplete_ready", 200, `{"code":0,"data":{"status":"ready","token":"jwt"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			})
			_, err := c.Poll(context.Background(), ProviderBigModel, "flow", "poll")
			if err == nil || strings.Contains(err.Error(), "secret must not escape") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid local input reached upstream") })
	if _, err := c.Init(context.Background(), "invalid", "token"); err == nil {
		t.Fatal("bad provider accepted")
	}
	if _, err := c.Poll(context.Background(), ProviderBigModel, "", "token"); err == nil {
		t.Fatal("empty flow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Init(ctx, ProviderBigModel, "token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestParseAuthorizationInput(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		provider    Provider
		code, state string
	}{
		{"deep_link", "zcode://oauth/callback?authCode=a%2Bb&state=s&channel_id=c", ProviderBigModel, "a+b", "s"},
		{"query", "code=code&authCode=auth&state=s", ProviderBigModel, "auth", "s"},
		{"zai_preference", "?code=code&authCode=auth&state=s", ProviderZai, "code", "s"},
		{"bigmodel_fallback", "code=code&state=s", ProviderBigModel, "code", "s"},
		{"zai_fallback", "authCode=auth&state=s", ProviderZai, "auth", "s"},
		{"json", `{"authCode":"auth","state":"s"}`, ProviderBigModel, "auth", "s"},
		{"attribution", "code=c&state=s&channel_id=ch&utm_source=src&utm_campaign=cam", ProviderZai, "c", "s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			callback, err := ParseAuthorizationInput(tc.provider, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if callback.Code != tc.code || callback.State != tc.state {
				t.Fatalf("wrong callback: %+v", callback)
			}
			if tc.name == "attribution" && (callback.ChannelID != "ch" || callback.UTMSource != "src" || callback.UTMCampaign != "cam") {
				t.Fatal("attribution lost")
			}
		})
	}
	for _, input := range []string{"", "bare-code", "code=c", "state=s", "code=x&state=s&state=s", "code=%QQ&state=s", "https://zcode.z.ai/app/oauth/login?code=c&state=s", "zcode://other/callback?code=c&state=s", `{"code":1,"state":"s"}`} {
		if _, err := ParseAuthorizationInput(ProviderBigModel, input); err == nil {
			t.Errorf("accepted invalid callback %q", input)
		}
	}
}

func TestExchangeAndNormalize(t *testing.T) {
	for _, tc := range []struct {
		name                string
		provider            Provider
		nested, camel, user bool
	}{
		{"bigmodel", ProviderBigModel, true, false, true}, {"bigmodel_camel", ProviderBigModel, true, true, false},
		{"bigmodel_flat", ProviderBigModel, false, false, false}, {"zai", ProviderZai, true, false, true}, {"zai_userinfo", ProviderZai, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userinfoCalls, businessCalls, tokenCalls := 0, 0, 0
			var redirectURI string
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/oauth/token":
					tokenCalls++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if r.Method != "POST" || body["provider"] != string(tc.provider) || body["redirect_uri"] != redirectURI || body["code"] != "auth" || body["state"] != "state" {
						t.Error("bad token body")
					}
					access := map[string]any{"access_token": "oauth", "refresh_token": "refresh"}
					if tc.camel {
						access = map[string]any{"accessToken": "oauth", "refreshToken": "refresh"}
					}
					data := map[string]any{"token": testJWT("2000000000"), "expires_in": 3600}
					if tc.nested {
						data[string(tc.provider)] = access
					} else {
						for k, v := range access {
							data[k] = v
						}
					}
					if tc.user {
						data["user"] = map[string]any{"user_id": "u"}
					}
					respond(t, w, 0, data)
				case "/api/auth/z/login":
					businessCalls++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["token"] != "oauth" || r.Header.Get("Authorization") != "" {
						t.Error("bad business exchange")
					}
					respond(t, w, 200, map[string]any{"accessToken": "business", "expires_in": 1800})
				case "/api/biz/customer/getCustomerInfo", "/api/oauth/userinfo":
					userinfoCalls++
					want := "oauth"
					if tc.provider == ProviderZai {
						want = "Bearer business"
					}
					if r.Header.Get("Authorization") != want {
						t.Error("bad userinfo auth")
					}
					respond(t, w, 0, map[string]any{"name": "user"})
				default:
					t.Error("unexpected endpoint")
					w.WriteHeader(http.StatusNotFound)
				}
			})
			authorize, redirect, err := c.ManualAuthorizationURL(tc.provider, "state")
			if err != nil {
				t.Fatal(err)
			}
			redirectURI = redirect
			u, err := url.Parse(authorize)
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Get("state") != "state" || strings.HasPrefix(redirectURI, "zcode:") {
				t.Fatal("bad manual authorization URL")
			}
			tokens, err := c.ExchangeCode(context.Background(), tc.provider, Callback{Code: "auth", State: "state"}, "state", redirect)
			if err != nil {
				t.Fatal(err)
			}
			if tokens.OAuthAccessToken != "oauth" || !tokens.JWTExpiresAt.Equal(time.Unix(2000000000, 0)) || tokens.OAuthExpiresAt.IsZero() {
				t.Fatal("token normalization failed")
			}
			if tc.provider == ProviderZai {
				if tokens.AccessToken != "business" || businessCalls != 1 || tokens.BusinessExpiresAt.IsZero() || tokens.RefreshToken != "" {
					t.Fatal("business token normalization failed")
				}
			} else if tokens.AccessToken != "oauth" || tokens.RefreshToken != "refresh" || businessCalls != 0 {
				t.Fatal("BigModel token normalization failed")
			}
			if (userinfoCalls == 0) != tc.user {
				t.Fatal("userinfo fallback mismatch")
			}
			if _, err := c.ExchangeCode(context.Background(), tc.provider, Callback{Code: "auth", State: "wrong"}, "state", redirect); err == nil {
				t.Fatal("mismatched state accepted")
			}
			if _, err := c.ExchangeCode(context.Background(), tc.provider, Callback{Code: "auth", State: "state"}, "state", "zcode://oauth/callback"); err == nil {
				t.Fatal("deep link used as redirect_uri")
			}
			if tokenCalls != 1 {
				t.Fatal("invalid exchange contacted upstream")
			}
		})
	}
}

func TestJWTExpiration(t *testing.T) {
	for _, exp := range []string{"2000000000", "2000000000.5"} {
		got, err := ParseJWTExpiration(testJWT(exp))
		if err != nil {
			t.Fatal(err)
		}
		if got.Unix() != 2000000000 {
			t.Fatalf("wrong exp: %s", got)
		}
	}
	for _, token := range []string{"broken", "e30.%%%.x", testJWT(`"2000000000"`), testJWT("null"), testJWT("-1"), testJWT("1e999")} {
		if _, err := ParseJWTExpiration(token); err == nil {
			t.Errorf("accepted invalid JWT %s", token)
		}
	}
	now := time.Unix(2000000000, 0)
	tokens := TokenSet{JWTExpiresAt: now.Add(30 * time.Second), BusinessExpiresAt: now.Add(300 * time.Second)}
	if !tokens.JWTExpiring(now) || !tokens.BusinessExpiring(now) {
		t.Fatal("expiry margins missing")
	}
	for _, raw := range []string{`2000000000`, `2000000000000`, `"2033-05-18T03:33:20Z"`} {
		got, err := parseTimestamp(json.RawMessage(raw))
		if err != nil || got.Unix() != 2000000000 {
			t.Fatalf("timestamp %s: %s %v", raw, got, err)
		}
	}
}

func TestOAuthInitMalformed(t *testing.T) {
	for _, field := range []string{"flow_id", "authorize_url", "expires_at", "poll_interval_sec"} {
		t.Run(field, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				data := map[string]any{"flow_id": "flow", "authorize_url": "https://bigmodel.cn/login", "expires_at": 2000000000, "poll_interval_sec": 2}
				delete(data, field)
				respond(t, w, 0, data)
			})
			if _, err := c.Init(context.Background(), ProviderBigModel, "poll"); err == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
}

func ExampleParseAuthorizationInput() {
	callback, err := ParseAuthorizationInput(ProviderBigModel, "authCode=one-time-code&state=stored-state")
	if err != nil {
		return
	}
	fmt.Println(callback.State)
	// Output: stored-state
}

func TestUnwrappedZaiUserInfo(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer business" || r.URL.Path != "/api/oauth/userinfo" {
			t.Error("bad userinfo request")
		}
		_, _ = w.Write([]byte(`{"sub":"user-id","name":"name"}`))
	})
	profile, err := c.UserInfo(context.Background(), ProviderZai, "business")
	if err != nil || profile["sub"] != "user-id" {
		t.Fatalf("raw userinfo unsupported: %v", err)
	}
}

func FuzzParseAuthorizationInput(f *testing.F) {
	for _, seed := range []string{"zcode://oauth/callback?authCode=abc&state=def", "code=x&state=y", "%", "", `{"authCode":"code","state":"state"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		for _, provider := range []Provider{ProviderBigModel, ProviderZai} {
			callback, err := ParseAuthorizationInput(provider, input)
			if err == nil && (callback.Code == "" || callback.State == "") {
				t.Fatal("successful parser omitted required fields")
			}
		}
	})
}
