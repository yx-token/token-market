//go:build unit

package zhipu

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInitResponseIntegrity(t *testing.T) {
	for _, scenario := range []string{"valid", "http", "no_state", "blank_state", "duplicate_state", "expired", "zero_interval", "interval_equals_ttl", "interval_exceeds_ttl"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				authorize := "https://bigmodel.cn/login?state=flow-state"
				expires, interval := time.Now().Add(time.Minute), 2
				switch scenario {
				case "http":
					authorize = "http://bigmodel.cn/login?state=flow-state"
				case "no_state":
					authorize = "https://bigmodel.cn/login"
				case "blank_state":
					authorize = "https://bigmodel.cn/login?state=+"
				case "duplicate_state":
					authorize += "&state=other"
				case "expired":
					expires = time.Now().Add(-time.Second)
				case "zero_interval":
					interval = 0
				case "interval_equals_ttl":
					expires = time.Now().Add(2 * time.Second)
				case "interval_exceeds_ttl":
					expires = time.Now().Add(time.Second)
				}
				respond(t, w, 0, map[string]any{"flow_id": "flow", "authorize_url": authorize, "expires_at": expires.Format(time.RFC3339Nano), "poll_interval_sec": interval})
			})
			result, err := c.Init(context.Background(), ProviderBigModel, "poll-token")
			if scenario != "valid" {
				if err == nil {
					t.Fatal("invalid init accepted")
				}
				return
			}
			if err != nil || result.State != "flow-state" {
				t.Fatalf("init state lost: %+v, %v", result, err)
			}
		})
	}
}

func TestReadyResponseIntegrity(t *testing.T) {
	for _, provider := range []Provider{ProviderBigModel, ProviderZai} {
		for _, scenario := range []string{"valid", "camel", "missing_jwt", "blank_jwt", "wrong_provider", "flat_access", "no_access", "no_user", "blank_user", "numeric_user"} {
			t.Run(string(provider)+"/"+scenario, func(t *testing.T) {
				c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					access := map[string]string{"access_token": "oauth"}
					data := map[string]any{"status": "ready", "token": "jwt", string(provider): access, "user": map[string]any{"user_id": "u"}}
					switch scenario {
					case "camel":
						data[string(provider)] = map[string]string{"accessToken": "oauth"}
					case "missing_jwt":
						delete(data, "token")
					case "blank_jwt":
						data["token"] = " "
					case "wrong_provider":
						delete(data, string(provider))
						other := ProviderBigModel
						if provider == other {
							other = ProviderZai
						}
						data[string(other)] = access
					case "flat_access":
						delete(data, string(provider))
						data["access_token"] = "oauth"
					case "no_access":
						delete(data, string(provider))
					case "no_user":
						delete(data, "user")
					case "blank_user":
						data["user"] = map[string]any{"user_id": " "}
					case "numeric_user":
						data["user"] = map[string]any{"user_id": 1}
					}
					respond(t, w, 0, data)
				})
				result, err := c.Poll(context.Background(), provider, "flow", "poll")
				valid := scenario == "valid" || (scenario == "camel" && provider == ProviderBigModel)
				if valid {
					if err != nil || result.Tokens == nil {
						t.Fatalf("valid ready rejected: %v", err)
					}
				} else if err == nil {
					t.Fatal("incomplete ready accepted")
				}
			})
		}
	}
}

func TestRedirectURIMustMatchDesktop(t *testing.T) {
	var calls atomic.Int64
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/oauth/token" {
			t.Error("unexpected endpoint")
		}
		respond(t, w, 0, map[string]any{"token": "jwt-unknown", "bigmodel": map[string]string{"access_token": "oauth"}, "user": map[string]string{"user_id": "u"}})
	})
	valid := c.DesktopRedirectURI()
	for _, scenario := range []string{"http", "host", "path", "version", "redirect", "extra", "duplicate", "fragment", "empty_fragment", "userinfo"} {
		t.Run(scenario, func(t *testing.T) {
			u, err := url.Parse(valid)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			switch scenario {
			case "http":
				u.Scheme = "http"
			case "host":
				u.Host = "other.invalid"
			case "path":
				u.Path += "/"
			case "version":
				q.Set("app_version", "0.0.0")
			case "redirect":
				q.Set("redirect", "zcode://other/callback")
			case "extra":
				q.Set("extra", "1")
			case "duplicate":
				q.Add("app_version", ClientVersion)
			case "fragment":
				u.Fragment = "fragment"
			case "userinfo":
				u.User = url.User("user")
			}
			u.RawQuery = q.Encode()
			raw := u.String()
			if scenario == "empty_fragment" {
				raw += "#"
			}
			if _, err := c.ExchangeCode(context.Background(), ProviderBigModel, Callback{Code: "c", State: "s"}, "s", raw); err == nil {
				t.Fatal("non-desktop redirect accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid redirect reached token exchange")
	}
	// Encoding and parameter order may differ while the decoded URL stays identical.
	reordered := c.zcode + "/app/oauth/login?redirect=zcode%3A%2F%2Foauth%2Fcallback&app_version=" + c.version
	if _, err := c.ExchangeCode(context.Background(), ProviderBigModel, Callback{Code: "c", State: "s"}, "s", reordered); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("valid exchange not sent exactly once")
	}
}

func TestNormalizeUnknownJWTAndProfileFallback(t *testing.T) {
	for _, provider := range []Provider{ProviderBigModel, ProviderZai} {
		for _, jwt := range []string{"malformed", "e30.e30.fake", testJWT(`"invalid"`)} {
			t.Run(string(provider)+"/"+jwt, func(t *testing.T) {
				c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/auth/z/login" {
						respond(t, w, 200, map[string]string{"access_token": "business"})
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
				})
				tokens, err := c.NormalizeTokens(context.Background(), provider, TokenData{Token: jwt, BigModel: AccessTokenData{AccessToken: "oauth"}, Zai: AccessTokenData{AccessToken: "oauth"}})
				if err != nil {
					t.Fatal(err)
				}
				if tokens.JWTStatus(time.Now()) != JWTStatusUnknown || tokens.JWTExpiring(time.Now()) || tokens.ZCodeJWT != jwt {
					t.Fatal("unknown JWT blocks inference or was lost")
				}
				if tokens.BusinessExpiring(time.Now()) != (provider == ProviderZai) {
					t.Fatal("unknown business TTL mishandled")
				}
				want := map[string]any{"id": "unknown", "username": "user", "displayName": "User"}
				if !reflect.DeepEqual(tokens.User, want) || tokens.OAuthAccessToken != "oauth" || tokens.AccessToken == "" {
					t.Fatalf("profile fallback lost tokens: %+v", tokens)
				}
			})
		}
	}
	now := time.Now()
	for _, tc := range []struct {
		exp  time.Time
		want JWTStatus
	}{{time.Time{}, JWTStatusUnknown}, {now.Add(31 * time.Second), JWTStatusValid}, {now.Add(30 * time.Second), JWTStatusExpiring}, {now.Add(-time.Hour), JWTStatusExpiring}} {
		if got := (TokenSet{JWTExpiresAt: tc.exp}).JWTStatus(now); got != tc.want {
			t.Fatalf("JWT status = %s, want %s", got, tc.want)
		}
	}
}

func TestTokenExchangeFailuresRemainFatal(t *testing.T) {
	for _, failPath := range []string{"token", "business"} {
		t.Run(failPath, func(t *testing.T) {
			var calls atomic.Int64
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if failPath == "token" || r.URL.Path == "/api/auth/z/login" {
					w.WriteHeader(503)
					return
				}
				respond(t, w, 0, map[string]any{"token": "unknown-jwt", "zai": map[string]string{"access_token": "oauth"}})
			})
			tokens, err := c.ExchangeCode(context.Background(), ProviderZai, Callback{Code: "c", State: "s"}, "s", c.DesktopRedirectURI())
			wantCalls := int64(1)
			if failPath == "business" {
				wantCalls = 2
			}
			if err == nil || tokens != nil || calls.Load() != wantCalls {
				t.Fatalf("exchange failure ignored: tokens=%+v err=%v calls=%d", tokens, err, calls.Load())
			}
		})
	}
}

func TestManualCallbackStrictParsing(t *testing.T) {
	callback, err := ParseAuthorizationInput(ProviderBigModel, " \n authCode=+code+&state=+state+ \n")
	if err != nil || callback.Code != "code" || callback.State != "state" {
		t.Fatalf("whitespace not trimmed: %+v %v", callback, err)
	}
	for _, name := range []string{"code", "authCode", "state"} {
		input := "code=c&authCode=a&state=s&" + name + "=duplicate"
		if _, err := ParseAuthorizationInput(ProviderBigModel, input); err == nil {
			t.Fatalf("duplicate %s accepted", name)
		}
	}
	if _, err := ParseAuthorizationInput(ProviderZai, strings.Repeat(" ", 10)); err == nil {
		t.Fatal("empty callback accepted")
	}
}
