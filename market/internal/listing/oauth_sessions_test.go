package listing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	platformdb "github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/yx-token/token-market/market/internal/sub2api/adminclient"
	"github.com/yx-token/token-market/market/internal/sub2api/provision"
	"github.com/yx-token/token-market/market/internal/sub2api/replica"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

type emptyOAuthReplica struct{ pool *pgxpool.Pool }

func (rep emptyOAuthReplica) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return rep.pool.Query(ctx, "SELECT 1 WHERE false")
}

func (rep emptyOAuthReplica) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return rep.pool.QueryRow(ctx, query, args...)
}

func (rep emptyOAuthReplica) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return rep.pool.Exec(ctx, query, args...)
}

func TestOAuthSessionsAndPolling(t *testing.T) {
	_, pool := testutil.NewDatabase(t)
	ctx := context.Background()
	if _, err := platformdb.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"kimi", "zhipu"} {
		authTypes := SupportedAuthTypes(platform)
		migrated, err := GetVendor(ctx, pool, platform)
		if err != nil {
			t.Fatal(err)
		}
		for _, authType := range authTypes {
			if !migrated.SupportsAuthType(authType) {
				t.Fatalf("migration did not enable %s/%s", platform, authType)
			}
		}
		enabled := true
		vendor, err := UpdateVendor(ctx, pool, platform, UpdateVendorInput{AuthTypes: &authTypes, Enabled: &enabled})
		if err != nil || len(vendor.AuthTypes) != len(authTypes) {
			t.Fatalf("vendor update rejected OAuth auth types for %s: %+v %v", platform, vendor, err)
		}
	}
	var sellerID, otherID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('oauth-seller@test.local','test') RETURNING id`).Scan(&sellerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('oauth-other@test.local','test') RETURNING id`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	keyring, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := replica.New(emptyOAuthReplica{pool})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ platform, authType string }{{"kimi", "oauth"}, {"zhipu", "oauth"}, {"zhipu", "oauth-intl"}} {
		t.Run(test.platform+"/"+test.authType, func(t *testing.T) {
			vendor, err := GetVendor(ctx, pool, test.platform)
			if err != nil || !vendor.SupportsAuthType(test.authType) {
				t.Fatalf("migration did not enable auth type: %v %v", vendor, err)
			}
			var upstreamID, proxyID int64
			if err := pool.QueryRow(ctx, `INSERT INTO proxies(name,protocol,host,port) VALUES('oauth proxy','http','127.0.0.1',8080) RETURNING id`).Scan(&proxyID); err != nil {
				t.Fatal(err)
			}
			engineProxyID := proxyID + 9000
			if _, err := pool.Exec(ctx, `UPDATE proxies SET sub2api_proxy_id = $2 WHERE id = $1`, proxyID, engineProxyID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id,probe_group_id) VALUES($1,$2,$3,$4,42) RETURNING id`, sellerID, test.platform, test.authType, proxyID).Scan(&upstreamID); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 28, 1, 2, 3, 0, time.UTC)
			status, calls, created, engineCalls := "pending", 0, 0, 0
			provider, expiresIn, interval := "", 893, 5
			callback := "zcode://oauth/callback?authCode=callback-secret&state=private-state"
			if test.platform == "zhipu" {
				provider, expiresIn, interval = "bigmodel", 293, 2
				if test.authType == "oauth-intl" {
					provider = "zai"
				}
			}
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				engineCalls++
				var body map[string]any
				if r.Body != nil && r.ContentLength != 0 {
					_ = json.NewDecoder(r.Body).Decode(&body)
				}
				var data any
				switch {
				case strings.HasSuffix(r.URL.Path, "/device-code"), strings.HasSuffix(r.URL.Path, "/init"):
					if body["proxy_id"] != float64(engineProxyID) || str(body, "provider") != provider {
						t.Errorf("start proxy/provider not from server: %#v", body)
					}
					data = map[string]any{"session_id": "private-engine-session", "user_code": "USER-CODE", "verification_uri": "https://auth.test/device",
						"verification_uri_complete": "https://auth.test/device?user_code=USER-CODE", "authorize_url": "https://auth.test/login", "expires_in": expiresIn, "interval": interval}
				case strings.HasSuffix(r.URL.Path, "/poll"):
					calls++
					if body["session_id"] != "private-engine-session" || body["proxy_id"] != float64(engineProxyID) {
						t.Errorf("poll session/proxy not from server: %#v", body)
					}
					data = map[string]any{"status": status, "interval": interval}
					if status == "success" {
						if test.platform == "kimi" {
							data.(map[string]any)["token_info"] = map[string]any{"access_token": "private-access", "refresh_token": "private-refresh", "expires_at": 2000000000, "token_type": "Bearer", "scope": "coding", "device_id": "device", "client_id": "client"}
						} else {
							data.(map[string]any)["result"] = map[string]any{"credentials": map[string]any{"access_token": "private-access", "api_key": "private-key", "oauth_provider": provider, "zcode_jwt": "private-jwt"}, "display": map[string]any{"email": "glm-" + test.authType + "@example.test"}}
						}
					}
				case strings.HasSuffix(r.URL.Path, "/exchange-code"):
					if body["session_id"] != "private-engine-session" || body["proxy_id"] != float64(engineProxyID) || body["callback_url"] != callback {
						t.Errorf("complete session/proxy/callback not from server: %#v", body)
					}
					data = map[string]any{"credentials": map[string]any{"access_token": "private-access", "api_key": "private-key", "oauth_provider": provider, "zcode_jwt": "private-jwt"}, "display": map[string]any{"email": "callback-" + test.authType + "@example.test"}}
				case r.URL.Path == "/api/v1/admin/accounts":
					created++
					creds := body["credentials"].(map[string]any)
					if creds["account_mode"] != "coding" || body["proxy_id"] != float64(engineProxyID) || creds["access_token"] != "private-access" {
						t.Errorf("incomplete created account: %#v", body)
					}
					data = map[string]any{"id": upstreamID + 1000 + int64(created-1)*100000}
				case strings.HasSuffix(r.URL.Path, "/models/sync-upstream"):
					data = map[string]any{"models": []string{"coding-model"}}
				default:
					t.Errorf("unexpected engine request: %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer engine.Close()
			admin := adminclient.New(engine.URL, "test", time.Second)
			service := &Service{DB: pool, Admin: admin, Prov: &provision.Provisioner{Admin: admin, Replica: reader}, Keyring: keyring,
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now },
				Settings: func(context.Context) (settings.Settings, error) { return settings.Settings{AllowNoProxy: true}, nil }}
			if _, err := service.ImportAccount(ctx, sellerID, upstreamID, ImportAccountInput{Data: json.RawMessage(`{"api_key":"not-oauth"}`)}); err == nil || !strings.Contains(err.Error(), "official authorization") {
				t.Fatalf("Coding OAuth import must be explicitly rejected: %v", err)
			}
			for _, operation := range []func() error{
				func() error {
					_, err := service.StartAuthorization(ctx, otherID, upstreamID, AuthorizationOptions{})
					return err
				},
				func() error { _, err := service.PollAuthorization(ctx, otherID, upstreamID); return err },
				func() error { _, err := service.CompleteAuthorization(ctx, otherID, upstreamID, "code"); return err },
				func() error {
					_, err := service.StartReauthorization(ctx, otherID, upstreamID, AuthorizationOptions{})
					return err
				},
			} {
				if err := operation(); !errors.Is(err, ErrUpstreamNotFound) {
					t.Fatalf("cross-seller operation must be not found: %v", err)
				}
			}
			if engineCalls != 0 {
				t.Fatal("unauthorized seller reached engine")
			}
			for _, authType := range AuthTypeEquivalents(test.platform, test.authType) {
				grade := "B"
				if authType == test.authType {
					grade = "S"
				} else if authType != "" {
					grade = "A"
				}
				if _, err := pool.Exec(ctx, `INSERT INTO detection_policies(platform,auth_type,default_grade) VALUES($1,$2,$3)
					ON CONFLICT(platform,auth_type) DO UPDATE SET default_grade=EXCLUDED.default_grade`, test.platform, authType, grade); err != nil {
					t.Fatal(err)
				}
			}
			if grade, err := policyDefaultGrade(ctx, pool, test.platform, test.authType); err != nil || grade != "S" {
				t.Fatalf("explicit OAuth grade not preferred: %s %v", grade, err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM detection_policies WHERE platform=$1 AND auth_type=$2`, test.platform, test.authType); err != nil {
				t.Fatal(err)
			}
			if grade, err := policyDefaultGrade(ctx, pool, test.platform, test.authType); err != nil || grade != "A" {
				t.Fatalf("OAuth did not inherit Coding Plan grade: %s %v", grade, err)
			}
			start, err := service.StartAuthorization(ctx, sellerID, upstreamID, AuthorizationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !start.ExpiresAt.Equal(now.Add(time.Duration(expiresIn) * time.Second)) {
				t.Fatalf("session TTL ignored engine expires_in: %v", start.ExpiresAt)
			}
			var storedExpiry time.Time
			var storedKind, storedProvider, storedSession string
			if err := pool.QueryRow(ctx, `SELECT expires_at,kind,COALESCE(provider,''),session_id FROM upstream_oauth_sessions WHERE upstream_id=$1`, upstreamID).Scan(&storedExpiry, &storedKind, &storedProvider, &storedSession); err != nil {
				t.Fatal(err)
			}
			if storedKind != start.Flow || storedProvider != provider || storedSession != "private-engine-session" || !storedExpiry.Equal(start.ExpiresAt) {
				t.Fatalf("incorrect stored authorization session: %s %s %s %v", storedKind, storedProvider, storedSession, storedExpiry)
			}
			poll, err := service.PollAuthorization(ctx, sellerID, upstreamID)
			if err != nil || poll.Status != "pending" || calls != 0 {
				t.Fatalf("initial poll not throttled: %+v %v calls=%d", poll, err, calls)
			}
			now = now.Add(time.Duration(interval) * time.Second)
			poll, err = service.PollAuthorization(ctx, sellerID, upstreamID)
			if err != nil || poll.Status != "pending" || calls != 1 {
				t.Fatalf("pending: %+v %v calls=%d", poll, err, calls)
			}
			status = "slow_down"
			now = now.Add(time.Duration(interval) * time.Second)
			poll, err = service.PollAuthorization(ctx, sellerID, upstreamID)
			if err != nil || poll.Status != "slow_down" || poll.Interval != interval+5 || calls != 2 {
				t.Fatalf("slow_down: %+v %v calls=%d", poll, err, calls)
			}
			var nextPoll time.Time
			if err := pool.QueryRow(ctx, `SELECT next_poll_at FROM upstream_oauth_sessions WHERE upstream_id=$1`, upstreamID).Scan(&nextPoll); err != nil || !nextPoll.Equal(now.Add(time.Duration(poll.Interval)*time.Second)) {
				t.Fatalf("next_poll_at not saved: %v %v", nextPoll, err)
			}
			if _, err := service.PollAuthorization(ctx, sellerID, upstreamID); err != nil || calls != 2 {
				t.Fatalf("slow_down not throttled: %v calls=%d", err, calls)
			}
			for _, terminal := range []string{"expired", "denied", "failed"} {
				now = nextPoll
				status = terminal
				poll, err = service.PollAuthorization(ctx, sellerID, upstreamID)
				want := terminal
				if terminal == "failed" {
					want = "denied"
				}
				if err != nil || poll.Status != want {
					t.Fatalf("terminal %s: %+v %v", terminal, poll, err)
				}
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM upstream_oauth_sessions WHERE upstream_id=$1`, upstreamID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("terminal session not deleted: %d %v", count, err)
				}
				if _, err := service.StartAuthorization(ctx, sellerID, upstreamID, AuthorizationOptions{}); err != nil {
					t.Fatal(err)
				}
				nextPoll = now.Add(time.Duration(interval) * time.Second)
			}
			callsBeforeExpiry := calls
			now = now.Add(time.Duration(expiresIn) * time.Second)
			poll, err = service.PollAuthorization(ctx, sellerID, upstreamID)
			if err != nil || poll.Status != "expired" || calls != callsBeforeExpiry {
				t.Fatalf("local expiry queried engine: %+v %v calls=%d", poll, err, calls)
			}
			if _, err := service.AdminStartAuthorization(ctx, upstreamID, AuthorizationOptions{}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Duration(interval) * time.Second)
			status = "success"
			poll, err = service.AdminPollAuthorization(ctx, upstreamID)
			if err != nil || poll.Status != "ready" || poll.Upstream == nil || poll.Upstream.Sub2APIAccountID == nil || poll.Upstream.Status != StatusSelectingModels || created != 1 {
				t.Fatalf("ready did not provision account: %+v err=%v creates=%d", poll, err, created)
			}
			raw, _ := json.Marshal(poll)
			for _, forbidden := range []string{"session_id", "private-engine-session", "private-access", "private-refresh", "private-key", "private-jwt", "credentials"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatalf("poll leaked %s: %s", forbidden, raw)
				}
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE actor_type='seller' AND actor_id=$1 AND action='upstream.oauth.poll-success' AND target=$2`, sellerID, fmt.Sprintf("upstream:%d", upstreamID)).Scan(&count); err != nil || count != 1 {
				t.Fatalf("seller success audit missing: %d %v", count, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM upstream_oauth_sessions WHERE upstream_id=$1`, upstreamID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("ready session not consumed: %d %v", count, err)
			}
			if test.platform == "zhipu" {
				var completeID int64
				if err := pool.QueryRow(ctx, `INSERT INTO proxies(name,protocol,host,port) VALUES('callback proxy','http','127.0.0.1',8081) RETURNING id`).Scan(&proxyID); err != nil {
					t.Fatal(err)
				}
				engineProxyID = proxyID + 9000
				if _, err := pool.Exec(ctx, `UPDATE proxies SET sub2api_proxy_id=$2 WHERE id=$1`, proxyID, engineProxyID); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `INSERT INTO upstreams(seller_id,platform,auth_type,proxy_id,probe_group_id) VALUES($1,$2,$3,$4,42) RETURNING id`, sellerID, test.platform, test.authType, proxyID).Scan(&completeID); err != nil {
					t.Fatal(err)
				}
				if _, err := service.AdminStartAuthorization(ctx, completeID, AuthorizationOptions{}); err != nil {
					t.Fatal(err)
				}
				completed, err := service.AdminCompleteAuthorization(ctx, completeID, callback)
				if err != nil || completed == nil || completed.Sub2APIAccountID == nil || completed.Status != StatusSelectingModels || created != 2 {
					t.Fatalf("callback did not provision account: %+v err=%v creates=%d", completed, err, created)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE actor_type='seller' AND actor_id=$1 AND action='upstream.oauth.complete' AND target=$2`, sellerID, fmt.Sprintf("upstream:%d", completeID)).Scan(&count); err != nil || count != 1 {
					t.Fatalf("seller complete audit missing: %d %v", count, err)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM upstream_oauth_sessions WHERE upstream_id=$1`, completeID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("callback session not consumed: %d %v", count, err)
				}
				completedJSON, _ := json.Marshal(completed)
				for _, forbidden := range []string{"session_id", "private-engine-session", "private-access", "private-key", "private-jwt", "credentials", "callback-secret"} {
					if strings.Contains(string(completedJSON), forbidden) {
						t.Fatalf("complete leaked %s: %s", forbidden, completedJSON)
					}
				}
			}
		})
	}
}
