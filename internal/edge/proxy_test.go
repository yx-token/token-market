package edge

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type fakeGateway struct {
	mu       sync.Mutex
	byKey    map[string]func(w http.ResponseWriter, r *http.Request)
	seenKeys []string
	seenPath []string
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("x-api-key")
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		key = v
	}
	if v := r.URL.Query().Get("key"); v != "" {
		key = v
	}
	f.mu.Lock()
	f.seenKeys = append(f.seenKeys, key)
	f.seenPath = append(f.seenPath, r.URL.Path)
	h := f.byKey[key]
	f.mu.Unlock()
	w.Header().Set("X-Client-Request-ID", "sub2api-"+key)
	if h == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	h(w, r)
}

type testEnv struct {
	srv     *Server
	gateway *fakeGateway
	store   edgecfg.Store
	kr      *crypto.Keyring
	cache   *Cache
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := edgecfg.Store{Redis: rdb}
	kr, err := crypto.NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32)))}})
	if err != nil {
		t.Fatal(err)
	}
	gw := &fakeGateway{byKey: map[string]func(http.ResponseWriter, *http.Request){}}
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	cache := NewCache(store, slog.Default())
	srv := NewServer(Config{GatewayURL: u, ConnectTimeout: time.Second, FirstByteTimeout: 2 * time.Second, MaxBodyBytes: 1 << 20,
		BreakerFailures: 2, BreakerCooldown: time.Minute}, cache, kr, nil, slog.Default())
	return &testEnv{srv: srv, gateway: gw, store: store, kr: kr, cache: cache}
}

func (e *testEnv) hop(t *testing.T, id int64, listing int64, internal string) edgecfg.Hop {
	t.Helper()
	enc, err := e.kr.SealString(internal, edgecfg.InternalKeyAAD(id))
	if err != nil {
		t.Fatal(err)
	}
	label := "pool:tm-pool-claude"
	if listing > 0 {
		label = "listing:" + string(rune('0'+listing))
	}
	return edgecfg.Hop{ListingID: listing, Label: label, InternalKeyID: id, InternalKeyEnc: enc}
}

func (e *testEnv) put(t *testing.T, buyerKey string, cfg edgecfg.KeyConfig) {
	t.Helper()
	if err := e.store.Put(context.Background(), edgecfg.HashKey(buyerKey), cfg); err != nil {
		t.Fatal(err)
	}
	if err := e.cache.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) do(method, path, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("x-api-key", auth)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func ok(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

func TestFailoverToSecondHop(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = status(http.StatusServiceUnavailable)
	e.gateway.byKey["tmik_b"] = ok(`{"id":"msg_1"}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a"), e.hop(t, 2, 2, "tmik_b")}})

	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"model":"x"}`)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"id":"msg_1"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-TM-Hop") != "2" || rec.Header().Get("X-TM-Listing") == "" || rec.Header().Get("X-TM-Request-ID") == "" {
		t.Fatalf("missing tm headers: %v", rec.Header())
	}
	if strings.Join(e.gateway.seenKeys, ",") != "tmik_a,tmik_b" {
		t.Fatalf("buyer key must be replaced with internal keys in order: %v", e.gateway.seenKeys)
	}
}

func TestNonRetryableErrorIsReturnedImmediately(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"bad request"}`)
	}
	e.gateway.byKey["tmik_b"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a"), e.hop(t, 2, 2, "tmik_b")}})
	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if rec.Code != http.StatusBadRequest || len(e.gateway.seenKeys) != 1 {
		t.Fatalf("400 must not fail over: code=%d seen=%v", rec.Code, e.gateway.seenKeys)
	}
}

func TestAllHopsFailReturnsLastFailure(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = status(http.StatusTooManyRequests)
	e.gateway.byKey["tmik_b"] = status(http.StatusBadGateway)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a"), e.hop(t, 2, 2, "tmik_b")}})
	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected last failure 502, got %d", rec.Code)
	}
}

func TestSkipListAndBreaker(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = status(http.StatusServiceUnavailable)
	e.gateway.byKey["tmik_b"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a"), e.hop(t, 2, 2, "tmik_b")}})

	// 连续两次失败后熔断，第三次直接跳过第一跳
	e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	e.gateway.seenKeys = nil
	e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if strings.Join(e.gateway.seenKeys, ",") != "tmik_b" {
		t.Fatalf("open breaker should skip first hop: %v", e.gateway.seenKeys)
	}

	// 跳过列表中的挂单不发请求
	e2 := newEnv(t)
	e2.gateway.byKey["tmik_a"] = ok(`{"a":1}`)
	e2.gateway.byKey["tmik_b"] = ok(`{"b":1}`)
	if err := e2.store.SetSkipList(context.Background(), []int64{1}); err != nil {
		t.Fatal(err)
	}
	e2.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e2.hop(t, 1, 1, "tmik_a"), e2.hop(t, 2, 2, "tmik_b")}})
	rec := e2.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if rec.Body.String() != `{"b":1}` {
		t.Fatalf("skipped listing must not receive traffic: %s", rec.Body.String())
	}
}

func TestKeyChecks(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = ok(`{}`)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	e.put(t, "tm-disabled", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "disabled", Reason: "refund in progress", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	e.put(t, "tm-expired", edgecfg.KeyConfig{BuyerKeyID: 2, Status: "active", ExpiresAt: &past, Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	e.put(t, "tm-blocked", edgecfg.KeyConfig{BuyerKeyID: 3, Status: "active", BlockedUntil: &future, Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	e.put(t, "tm-iplocked", edgecfg.KeyConfig{BuyerKeyID: 4, Status: "active", IPWhitelist: []string{"10.0.0.0/8"}, Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})

	cases := map[string]int{
		"":            http.StatusUnauthorized,
		"tm-unknown":  http.StatusUnauthorized,
		"tm-disabled": http.StatusForbidden,
		"tm-expired":  http.StatusUnauthorized,
		"tm-blocked":  http.StatusTooManyRequests,
		"tm-iplocked": http.StatusForbidden,
	}
	for key, want := range cases {
		if rec := e.do(http.MethodPost, "/v1/messages", key, `{}`); rec.Code != want {
			t.Errorf("key %q: got %d want %d", key, rec.Code, want)
		}
	}
	if rec := e.do(http.MethodPost, "/api/v1/admin/users", "tm-blocked", `{}`); rec.Code != http.StatusNotFound {
		t.Errorf("non-gateway path must be rejected, got %d", rec.Code)
	}
	if len(e.gateway.seenKeys) != 0 {
		t.Fatalf("rejected requests must not reach sub2api: %v", e.gateway.seenKeys)
	}
}

func TestKeyLocationsAreRewritten(t *testing.T) {
	e := newEnv(t)
	e.gateway.byKey["tmik_a"] = ok(`{}`)
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 0, "tmik_a")}})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer tm-buyer")
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=tm-buyer&alt=sse", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query key: %d", rec.Code)
	}
	if e.gateway.seenPath[1] != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("path not preserved: %v", e.gateway.seenPath)
	}
}

func TestStreamingResponseAndAttemptRecord(t *testing.T) {
	e := newEnv(t)
	logger := &AttemptLogger{ch: make(chan Attempt, 10)}
	e.srv.attempts = logger
	e.gateway.byKey["tmik_a"] = status(http.StatusServiceUnavailable)
	e.gateway.byKey["tmik_b"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: chunk\n\n")
			f.Flush()
		}
	}
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 7, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a"), e.hop(t, 2, 2, "tmik_b")}})
	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{"stream":true}`)
	if strings.Count(rec.Body.String(), "data: chunk") != 3 {
		t.Fatalf("stream not forwarded: %q", rec.Body.String())
	}
	close(logger.ch)
	var got []Attempt
	for a := range logger.ch {
		got = append(got, a)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 attempts, got %+v", got)
	}
	if got[0].EndReason != "retry" || got[0].HTTPStatus != 503 || got[0].Committed || got[0].Sub2APIRequestID != "sub2api-tmik_a" {
		t.Fatalf("first attempt: %+v", got[0])
	}
	if got[1].EndReason != "completed" || !got[1].Committed || got[1].Hop != 2 || got[1].BuyerKeyID != 7 || got[1].EdgeRequestID != got[0].EdgeRequestID {
		t.Fatalf("second attempt: %+v", got[1])
	}
}

func TestFirstByteDeadline(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.FirstByteTimeout = 200 * time.Millisecond
	e.gateway.byKey["tmik_a"] = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}
	e.put(t, "tm-buyer", edgecfg.KeyConfig{BuyerKeyID: 1, Status: "active", Hops: []edgecfg.Hop{e.hop(t, 1, 1, "tmik_a")}})
	start := time.Now()
	rec := e.do(http.MethodPost, "/v1/messages", "tm-buyer", `{}`)
	if time.Since(start) > time.Second || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("deadline not enforced: %d after %v", rec.Code, time.Since(start))
	}
}
