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
	"github.com/yx-token/token-market/market/internal/rewardtask"
)

func TestRewardLandingNeverFailsTransport(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.HandlerEmbed()
	for _, body := range []string{`{`, `{}`, `{"ticket":"rt_0123456789abcdef0123456789abcdef01234567"}`} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/market/reward/landing", strings.NewReader(body)))
		if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"valid":null`) {
			t.Fatalf("%s: %d %s", body, r.Code, r.Body.String())
		}
	}
	// 绑定接口需要登录。
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/me/reward-ticket", strings.NewReader(`{"ticket":"rt_0123456789abcdef0123456789abcdef01234567"}`)))
	if r.Code != http.StatusUnauthorized {
		t.Fatalf("bind without login: %d %s", r.Code, r.Body.String())
	}
}

func TestRewardLandingRateLimitAndAdminResource(t *testing.T) {
	// 被限流时不能触达服务（Service 没有 DB，触达就会 panic）。
	s := &Server{Reward: &rewardtask.Service{}, rewardLandingByIP: ratelimit.NewFailures(1, time.Minute)}
	s.rewardLandingByIP.Fail("192.0.2.1")
	r := httptest.NewRequest(http.MethodPost, "/api/market/reward/landing", strings.NewReader(`{"ticket":"rt_0123456789abcdef0123456789abcdef01234567"}`))
	r.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	if err := s.rewardLanding(rec, r); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data rewardtask.LandingResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Data.Valid != nil {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), err)
	}
	// 全站上限：换一个 IP 也不能再打到 loxo。
	s.rewardLandingByIP = ratelimit.NewFailures(30, time.Minute)
	s.rewardLandingAll = ratelimit.NewFailures(1, time.Minute)
	s.rewardLandingAll.Fail("*")
	r = httptest.NewRequest(http.MethodPost, "/api/market/reward/landing", strings.NewReader(`{"ticket":"rt_0123456789abcdef0123456789abcdef01234567"}`))
	r.RemoteAddr = "198.51.100.7:12345"
	rec = httptest.NewRecorder()
	if err := s.rewardLanding(rec, r); err != nil || !strings.Contains(rec.Body.String(), `"valid":null`) {
		t.Fatalf("global limit: %v %s", err, rec.Body.String())
	}
	seen := 0
	for _, rt := range s.adminRoutes() {
		if strings.Contains(rt.pattern, "/reward-tasks") {
			seen++
			ids := adminRouteResources(rt.pattern)
			if len(ids) != 1 || ids[0] != "console.ops.reward" {
				t.Fatalf("route %s permissions %v", rt.pattern, ids)
			}
		}
	}
	if seen != 6 {
		t.Fatalf("reward admin routes = %d", seen)
	}
}
