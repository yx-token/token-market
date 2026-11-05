//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/internal/account"
	"github.com/yx-token/token-market/market/internal/app"
	"github.com/yx-token/token-market/market/internal/platform/auth"
	"github.com/yx-token/token-market/market/internal/platform/db"
	"github.com/yx-token/token-market/market/internal/platform/ratelimit"
	"github.com/yx-token/token-market/market/internal/promo"
	"github.com/yx-token/token-market/market/internal/testutil"
	"github.com/yx-token/token-market/market/migrations"
)

func promoTestServer(t *testing.T) (*Server, *pgxpool.Pool) {
	t.Helper()
	_, pool := testutil.NewDatabase(t)
	if _, err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	issuer, err := auth.NewIssuer(strings.Repeat("p", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{DB: pool, Accounts: account.NewService(pool, nil, issuer), Issuer: issuer, Promo: &promo.Service{DB: pool}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, pool
}

func TestPromoRegistrationIsBestEffortAndLoginCompatible(t *testing.T) {
	s, pool := promoTestServer(t)
	ctx := context.Background()
	active, err := s.Promo.Create(ctx, promo.Input{Name: "active", Code: "campaign"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.Promo.Create(ctx, promo.Input{Name: "disabled", Code: "disabled"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Promo.SetStatus(ctx, disabled.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for i, tc := range []struct {
		name, extra string
		attributed  bool
	}{
		{"no_promo", ``, false},
		{"valid", `,"channel_code":"campaign","visitor_id":"visitor_123","channel_touched_at":"` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + `"`, true},
		{"no_touch", `,"channel_code":"campaign"`, true},
		{"expired", `,"channel_code":"campaign","channel_touched_at":"` + time.Now().Add(-31*24*time.Hour).UTC().Format(time.RFC3339) + `"`, false},
		{"disabled", `,"channel_code":"disabled"`, false},
		{"missing", `,"channel_code":"unknown"`, false},
		{"invalid_code", `,"channel_code":"invalid!"`, false},
		{"invalid_time", `,"channel_code":"campaign","channel_touched_at":"bad-date"`, false},
		{"invalid_visitor", `,"channel_code":"campaign","visitor_id":"x"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := fmt.Sprintf("promo-%d@example.com", i)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"`+email+`","password":"test-password-123"`+tc.extra+`}`)))
			if rec.Code != http.StatusCreated {
				t.Fatalf("registration %d %s", rec.Code, rec.Body.String())
			}
			var out struct {
				Data struct {
					ID int64 `json:"id"`
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			var channelID int64
			if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT channel_id FROM promo_channel_users WHERE user_id=$1),0)`, out.Data.ID).Scan(&channelID); err != nil {
				t.Fatal(err)
			}
			if (channelID == active.ID) != tc.attributed {
				t.Fatalf("channel=%d expected attributed=%t", channelID, tc.attributed)
			}
			login := httptest.NewRecorder()
			h.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"`+email+`","password":"test-password-123"}`)))
			if login.Code != http.StatusOK {
				t.Fatalf("login %d %s", login.Code, login.Body.String())
			}
		})
	}
	// Best-effort attribution also has a bounded wait when the analytics table is locked.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE promo_channel_users IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	locked := httptest.NewRecorder()
	h.ServeHTTP(locked, httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"storage-lock@example.com","password":"test-password-123","channel_code":"campaign"}`)))
	if locked.Code != http.StatusCreated || time.Since(started) > 5*time.Second {
		t.Fatalf("locked attribution stalled registration: %v %d %s", time.Since(started), locked.Code, locked.Body.String())
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A storage failure after user creation must not turn a successful signup into an error.
	if _, err := pool.Exec(ctx, `ALTER TABLE promo_channel_users RENAME TO promo_channel_users_unavailable`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"storage-failure@example.com","password":"test-password-123","channel_code":"campaign"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("storage failure affected registration: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPromoAdminHTTPContracts(t *testing.T) {
	s, pool := promoTestServer(t)
	ctx := context.Background()
	var uid int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email,password_hash,is_admin) VALUES ('promo-admin@example.com','test',true) RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Issuer.Issue(auth.Claims{UserID: uid, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	call := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: want %d got %d %s", method, path, status, rec.Code, rec.Body.String())
		}
		return rec
	}
	base := "/api/admin/promo-channels"
	call("GET", base, "", 200)
	call("POST", base, `{"name":"Bad","code":"bad!"}`, 400)
	call("POST", base, `{"name":"Bad","landing_path":"//evil.example"}`, 400)
	rec := call("POST", base, `{"name":"Campaign","code":"campaign"}`, 201)
	var result struct{ Data promo.Channel }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Stats.RechargeAmount != "0.00" {
		t.Fatalf("bad response %s", rec.Body.String())
	}
	path := fmt.Sprintf("%s/%d", base, result.Data.ID)
	if rec := call("POST", base, `{"name":"Duplicate","code":"campaign"}`, 409); !strings.Contains(rec.Body.String(), "PROMO_CODE_TAKEN") {
		t.Fatal(rec.Body.String())
	}
	call("PUT", path, `{"name":"Renamed","landing_path":"/market?sort=recent","note":"test"}`, 200)
	call("PUT", path, `{"name":"No","code":"new-code"}`, 400)
	call("POST", path+"/status", `{"status":"bad"}`, 400)
	call("POST", path+"/status", `{"status":"disabled"}`, 200)
	call("GET", path+"/daily?from=2026-11-01&to=2026-11-03", "", 200)
	call("GET", path+"/daily?from=2025-01-01&to=2026-11-03", "", 400)
	call("GET", path+"/users?page=1&page_size=20", "", 200)
	call("GET", base+"?from=bad-date", "", 400)
	// The engine namespace is served by relay, not this standalone handler.
	status, body := ServeAdmin(&app.App{DB: pool, Accounts: s.Accounts, Issuer: s.Issuer, Promo: s.Promo}, ctx, "GET", "promo-channels", "", nil, uid)
	if status != 200 || !strings.Contains(string(body), "campaign") {
		t.Fatalf("relay: %d %s", status, body)
	}
	call("DELETE", path, "", 200)
	call("GET", path+"/daily", "", 404)

	channel, err := s.Promo.Create(ctx, promo.Input{Name: "Has visits", Code: "visits"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	visit := httptest.NewRecorder()
	h.ServeHTTP(visit, httptest.NewRequest("POST", "/api/market/promo/visit", strings.NewReader(`{"code":"visits","visitor_id":"visitor_123"}`)))
	if visit.Code != 200 || strings.TrimSpace(visit.Body.String()) != `{"accepted":true}` {
		t.Fatalf("visit: %d %s", visit.Code, visit.Body.String())
	}
	// Another visitor on the same IP is limited before any channel lookup/write.
	s.promoVisitsByIP = ratelimit.NewFailures(1, time.Minute)
	s.promoVisitsByIP.Fail("192.0.2.1")
	limited := httptest.NewRecorder()
	h.ServeHTTP(limited, httptest.NewRequest("POST", "/api/market/promo/visit", strings.NewReader(`{"code":"visits","visitor_id":"visitor_456"}`)))
	if limited.Code != 200 || strings.TrimSpace(limited.Body.String()) != `{"accepted":false}` {
		t.Fatalf("limited visit: %d %s", limited.Code, limited.Body.String())
	}
	var visits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM promo_channel_visits WHERE channel_id=$1`, channel.ID).Scan(&visits); err != nil || visits != 1 {
		t.Fatalf("limited visit persisted: count=%d err=%v", visits, err)
	}
	if rec := call("DELETE", fmt.Sprintf("%s/%d", base, channel.ID), "", 409); !strings.Contains(rec.Body.String(), "PROMO_CHANNEL_HAS_DATA") {
		t.Fatal(rec.Body.String())
	}
}
