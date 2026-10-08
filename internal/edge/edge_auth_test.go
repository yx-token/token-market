package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/platform/clientip"
	"github.com/DueGin/token-market/internal/platform/ratelimit"
)

func TestInvalidKeyThrottlingAndWhitelistIgnoresSpoofedHeader(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = ok(`{}`)
	e.put(t, "tm-good", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", IPWhitelist: []string{"1.2.3.4"}, Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	e.srv.authFail = ratelimit.NewFailures(2, time.Minute)
	for i := 0; i < 2; i++ {
		if rec := e.do(http.MethodPost, "/v1/messages", "tm-wrong", `{}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid key: %d", rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, "/v1/messages", "tm-good", `{}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ip over the invalid key limit should be throttled even with a valid key: %d", rec.Code)
	}

	e.srv.authFail = ratelimit.NewFailures(2, time.Minute)
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
		req.RemoteAddr = "203.0.113.9:1234"
		req.Header.Set("X-Forwarded-For", "1.2.3.4") // 伪造白名单地址
		req.Header.Set("x-api-key", "tm-good")
		return req
	}
	if rec := serve(e, newReq()); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed X-Forwarded-For must not pass the ip whitelist: %d", rec.Code)
	}
	e.srv.ips = clientip.New([]string{"203.0.113.9"}) // 来自受信任的负载均衡
	if rec := serve(e, newReq()); rec.Code != http.StatusOK {
		t.Fatalf("forwarded ip from a trusted proxy should pass: %d", rec.Code)
	}
}

func serve(e *testEnv, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}
