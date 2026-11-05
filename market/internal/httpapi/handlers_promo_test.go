package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/ratelimit"
)

func TestPromoVisitAlwaysAcceptsHTTPTransport(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.HandlerEmbed()
	for _, body := range []string{`{`, `{}`, `{"code":"missing","visitor_id":"visitor_123"}`, `{"unexpected":1}`} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/market/promo/visit", strings.NewReader(body)))
		if r.Code != http.StatusOK || strings.TrimSpace(r.Body.String()) != `{"accepted":false}` {
			t.Fatalf("%s: %d %s", body, r.Code, r.Body.String())
		}
	}
}

func TestPromoVisitRateLimitAndAdminResource(t *testing.T) {
	s := &Server{promoVisitsByIP: ratelimit.NewFailures(1, time.Minute)}
	s.promoVisitsByIP.Fail("192.0.2.1")
	r := httptest.NewRequest(http.MethodPost, "/api/market/promo/visit", strings.NewReader(`{"code":"valid","visitor_id":"visitor_123"}`))
	r.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	if err := s.promoVisit(rec, r); err != nil {
		t.Fatal(err)
	}
	var out map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || out["accepted"] {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for _, rt := range s.adminRoutes() {
		if strings.Contains(rt.pattern, "/promo-channels") {
			ids := adminRouteResources(rt.pattern)
			if len(ids) != 1 || ids[0] != "console.ops.promo" {
				t.Fatalf("route %s permissions %v", rt.pattern, ids)
			}
		}
	}
}
