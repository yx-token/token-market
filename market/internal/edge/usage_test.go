package edge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/platform/ratelimit"
)

type usageReaderFunc func(context.Context, int64, int64) (UsageSnapshot, error)

func (f usageReaderFunc) ReadUsage(ctx context.Context, keyID, userID int64) (UsageSnapshot, error) {
	return f(ctx, keyID, userID)
}

func decodeUsageResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode usage response: %v: %s", err, rec.Body.String())
	}
	return got
}

func TestUsageReturnsBuyerBalancesWithoutUpstreamOrAttempts(t *testing.T) {
	quotaLimit := 10.0
	fiveHourLimit, dayLimit, weekLimit := 9.0, 8.0, 7.0
	tests := []struct {
		name      string
		data      UsageSnapshot
		remaining float64
	}{
		{name: "wallet", data: UsageSnapshot{Cash: 25.5, Used: 4.5}, remaining: 25.5},
		{name: "quota pack", data: UsageSnapshot{Cash: 20, QuotaPack: 7.25, StoreCredit: 5, IsQuotaPack: true, Used: 2.75, QuotaLimit: &quotaLimit}, remaining: 7.25},
		{name: "store credit", data: UsageSnapshot{Cash: 3.5, StoreCredit: 18.25, Used: 1.25}, remaining: 21.75},
		{name: "exhausted", data: UsageSnapshot{}},
		{name: "depleted quota pack does not fall back to wallet", data: UsageSnapshot{Cash: 20, StoreCredit: 5, IsQuotaPack: true}},
		{name: "key quota caps wallet", data: UsageSnapshot{Cash: 20, Used: 3.25, QuotaLimit: &quotaLimit}, remaining: 6.75},
		{name: "5h limit", data: UsageSnapshot{Cash: 20, Used5h: 5, RateLimit5h: &fiveHourLimit}, remaining: 4},
		{name: "1d limit", data: UsageSnapshot{Cash: 20, Used1d: 5, RateLimit1d: &dayLimit}, remaining: 3},
		{name: "7d limit", data: UsageSnapshot{Cash: 20, Used7d: 5, RateLimit7d: &weekLimit}, remaining: 2},
		{name: "lowest limit wins", data: UsageSnapshot{Cash: 20, Used: 1, Used5h: 4, Used1d: 6, Used7d: 1, QuotaLimit: &quotaLimit, RateLimit5h: &fiveHourLimit, RateLimit1d: &dayLimit, RateLimit7d: &weekLimit}, remaining: 2},
		{name: "overdrawn wallet", data: UsageSnapshot{Cash: -1}},
		{name: "overrun quota", data: UsageSnapshot{Cash: 20, Used: 12, QuotaLimit: &quotaLimit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			// A balance read must work even when the key has no available seller hop.
			e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 41, BuyerUserID: 73, Status: "active", CreditCandidate: true})
			calls := 0
			e.srv.SetUsageReader(usageReaderFunc(func(_ context.Context, keyID, userID int64) (UsageSnapshot, error) {
				calls++
				if keyID != 41 || userID != 73 {
					t.Fatalf("usage read for wrong buyer: key=%d user=%d", keyID, userID)
				}
				return tt.data, nil
			}))
			e.srv.SetCreditSelector(creditSelectFunc(func(context.Context, *edgecfg.KeyConfig, edgecfg.Hop, string, int64) (*CreditLease, error) {
				t.Fatal("balance lookup must not acquire credit or call an upstream")
				return nil, nil
			}))
			beforeAttempts := metricAttempts.String()
			got := decodeUsageResponse(t, e.do(http.MethodGet, "/v1/usage", "tm-usage", ""))
			for _, field := range []string{"remaining", "balance", "quota_remaining"} {
				if got[field] != tt.remaining {
					t.Errorf("%s=%v want %v", field, got[field], tt.remaining)
				}
			}
			if got["unit"] != "CNY" || got["currency"] != "CNY" {
				t.Errorf("currency must use market accounting units, got %v", got)
			}
			if got["used"] != tt.data.Used {
				t.Errorf("used=%v want %v", got["used"], tt.data.Used)
			}
			if got["is_active"] != (tt.remaining > 0) {
				t.Errorf("is_active=%v want %t", got["is_active"], tt.remaining > 0)
			}
			if tt.data.QuotaLimit != nil && got["limit"] != *tt.data.QuotaLimit {
				t.Errorf("limit=%v want %v", got["limit"], *tt.data.QuotaLimit)
			}
			if calls != 1 || len(e.gateway.seenKeys) != 0 {
				t.Errorf("balance request must read once locally: calls=%d upstream=%v", calls, e.gateway.seenKeys)
			}
			if metricAttempts.String() != beforeAttempts {
				t.Errorf("balance reads must not generate attempts: before=%s after=%s", beforeAttempts, metricAttempts.String())
			}
		})
	}
}

func TestUsageAcceptsExistingBuyerKeyLocations(t *testing.T) {
	for _, location := range []string{"bearer", "api-key", "goog-header", "query"} {
		t.Run(location, func(t *testing.T) {
			e := newEnv(t)
			e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 2, Status: "active"})
			e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
				return UsageSnapshot{Cash: 1.5}, nil
			}))
			req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
			switch location {
			case "bearer":
				req.Header.Set("Authorization", "Bearer tm-usage")
			case "api-key":
				req.Header.Set("x-api-key", "tm-usage")
			case "goog-header":
				req.Header.Set("x-goog-api-key", "tm-usage")
			case "query":
				req.URL.RawQuery = "key=tm-usage"
			}
			rec := httptest.NewRecorder()
			e.srv.ServeHTTP(rec, req)
			got := decodeUsageResponse(t, rec)
			if got["remaining"] != 1.5 {
				t.Fatalf("remaining=%v", got["remaining"])
			}
		})
	}
}

func TestUsageAntigravityAliasIsLocalAndNonBillable(t *testing.T) {
	e := newEnv(t)
	e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 2, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "private-internal-key")}})
	e.gateway.byKey["private-internal-key"] = ok(`{"remaining":99999,"unit":"USD"}`)
	e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
		return UsageSnapshot{Cash: 2.5}, nil
	}))
	beforeAttempts := metricAttempts.String()
	got := decodeUsageResponse(t, e.do(http.MethodGet, "/antigravity/v1/usage", "tm-usage", ""))
	if got["remaining"] != 2.5 || got["unit"] != "CNY" {
		t.Fatalf("alias must return buyer-local balance, got %v", got)
	}
	if len(e.gateway.seenKeys) != 0 {
		t.Fatalf("alias exposed an internal upstream balance: %v", e.gateway.seenKeys)
	}
	if metricAttempts.String() != beforeAttempts {
		t.Fatalf("alias balance reads must not generate attempts")
	}
	for _, path := range []string{"/v1/usage", "/antigravity/v1/usage"} {
		if billablePath(http.MethodGet, path) {
			t.Errorf("%s must be explicitly non-billable", path)
		}
	}
}

func TestUsagePreservesKeyChecks(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	tests := []struct {
		name   string
		key    string
		config *edgecfg.KeyConfig
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "unknown", key: "tm-unknown", status: http.StatusUnauthorized},
		{name: "ordinary sub2api key", key: "sk-sub2api", status: http.StatusUnauthorized},
		{name: "disabled", key: "tm-disabled", config: &edgecfg.KeyConfig{BuyerKeyID: 1, Status: "disabled"}, status: http.StatusForbidden},
		{name: "deleted", key: "tm-deleted", config: &edgecfg.KeyConfig{BuyerKeyID: 1, Status: "deleted"}, status: http.StatusForbidden},
		{name: "expired", key: "tm-expired", config: &edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", ExpiresAt: &past}, status: http.StatusUnauthorized},
		{name: "blocked", key: "tm-blocked", config: &edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", BlockedUntil: &future}, status: http.StatusTooManyRequests},
		{name: "ip denied", key: "tm-ip", config: &edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", IPWhitelist: []string{"10.0.0.0/8"}}, status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.config != nil {
				e.put(t, tt.key, *tt.config)
			} else if err := e.cache.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
				t.Fatal("rejected key must not read buyer balance")
				return UsageSnapshot{}, nil
			}))
			rec := e.do(http.MethodGet, "/v1/usage", tt.key, "")
			if rec.Code != tt.status {
				t.Fatalf("status=%d want %d body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if len(e.gateway.seenKeys) != 0 {
				t.Fatalf("rejected usage request reached upstream: %v", e.gateway.seenKeys)
			}
		})
	}
}

func TestUsageInvalidKeyLimitIsSharedWithInference(t *testing.T) {
	for _, usageFirst := range []bool{false, true} {
		name := "inference failure blocks usage"
		if usageFirst {
			name = "usage failure blocks inference"
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 2, Status: "active"})
			e.srv.authFail = ratelimit.NewFailures(1, time.Minute)
			e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
				t.Fatal("throttled usage request must not query balance")
				return UsageSnapshot{}, nil
			}))
			firstMethod, firstPath := http.MethodPost, "/v1/messages"
			secondMethod, secondPath := http.MethodGet, "/v1/usage"
			if usageFirst {
				firstMethod, firstPath, secondMethod, secondPath = secondMethod, secondPath, firstMethod, firstPath
			}
			if rec := e.do(firstMethod, firstPath, "tm-unknown", "{}"); rec.Code != http.StatusUnauthorized {
				t.Fatalf("initial invalid key status=%d", rec.Code)
			}
			if rec := e.do(secondMethod, secondPath, "tm-usage", ""); rec.Code != http.StatusTooManyRequests {
				t.Fatalf("usage must share inference invalid-key limit: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUsageDatabaseUnavailableIsNotReportedAsZero(t *testing.T) {
	for _, name := range []string{"unconfigured", "query failure"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 2, Status: "active"})
			if name == "query failure" {
				e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
					return UsageSnapshot{}, errors.New("database offline: private connection details")
				}))
			}
			rec := e.do(http.MethodGet, "/v1/usage", "tm-usage", "")
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d want 503 body=%s", rec.Code, rec.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if _, ok := got["remaining"]; ok {
				t.Fatalf("unavailable balance must not look like a zero balance: %v", got)
			}
			if _, ok := got["error"]; !ok || strings.Contains(rec.Body.String(), "private connection details") {
				t.Fatalf("must return a safe explicit error: %s", rec.Body.String())
			}
			if len(e.gateway.seenKeys) != 0 {
				t.Fatalf("balance error must not fall through to sub2api: %v", e.gateway.seenKeys)
			}
		})
	}
}

func TestUsageOnlyAcceptsGET(t *testing.T) {
	e := newEnv(t)
	e.put(t, "tm-usage", edgecfg.KeyConfig{BuyerKeyID: 1, BuyerUserID: 2, Status: "active"})
	e.srv.SetUsageReader(usageReaderFunc(func(context.Context, int64, int64) (UsageSnapshot, error) {
		t.Fatal("non-GET usage request must not read balance")
		return UsageSnapshot{}, nil
	}))
	for _, path := range []string{"/v1/usage", "/antigravity/v1/usage"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead} {
			if rec := e.do(method, path, "tm-usage", ""); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s status=%d want 404 body=%s", method, path, rec.Code, rec.Body.String())
			}
		}
	}
}
