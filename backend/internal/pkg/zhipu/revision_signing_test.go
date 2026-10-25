//go:build unit

package zhipu

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var v4HeaderNames = []string{"X-Client-Ts", "X-Client-Version", "X-Client-Sig", "X-Client-Nonce", "X-Client-Pow", "X-App-Id", "X-Client-Sign-Verified"}

func TestUnsignedRequestsClearAllV4Headers(t *testing.T) {
	c, server := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unsigned request triggered network") })
	for _, scenario := range []string{"cross_origin", "unsigned_path", "never", "bypass"} {
		t.Run(scenario, func(t *testing.T) {
			mode := SignatureAlways
			if scenario == "never" {
				mode = SignatureNever
			}
			signer := testSigner(t, c, server.URL, mode, nil)
			req := signingRequest(t, server)
			defer closeRequestBody(req)
			switch scenario {
			case "cross_origin":
				req.URL.Host = "other.invalid"
			case "unsigned_path":
				req.URL.Path = "/api/v1/zcode-plan/chat/completions"
			case "bypass":
				signer.Bypass("key-id.secret")
			}
			for _, name := range v4HeaderNames {
				req.Header.Set(name, "stale")
				req.Header[strings.ToLower(name)] = []string{"also-stale"}
			}
			if err := signer.Sign(context.Background(), req, "key-id.secret", "end-user-session"); err != nil {
				t.Fatal(err)
			}
			for name := range req.Header {
				for _, forbidden := range v4HeaderNames {
					if strings.EqualFold(name, forbidden) {
						t.Fatalf("unsigned request retained %s", name)
					}
				}
			}
			if req.Header.Get("User-Agent") != "ZCode/"+ClientVersion {
				t.Fatal("unsigned request lost source headers")
			}
		})
	}
}

func TestSigningRoutingRetriesAndBypassExpiry(t *testing.T) {
	c, server, fixture := newSigningServer(t)
	fixture.alwaysReject = true
	originalURL := server.URL + "/api/coding/paas/v4/chat/completions?model=glm"
	var configCalls atomic.Int64
	remote, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		configCalls.Add(1)
		if r.Header.Get("X-Api-Key") != "key-id.secret" || r.Header.Get("Authorization") != "" {
			t.Error("route config credential mismatch")
		}
		respond(t, w, 0, map[string]any{"proxyEndpoint": map[string]any{"mapping": []EndpointMapping{{From: server.URL + "/api/coding/paas/v4/chat/completions/", To: "https://proxy.invalid/routed/"}}}})
	})
	signer := testSigner(t, c, server.URL, SignatureAlways, nil)
	now := time.Now()
	signer.now = func() time.Time { return now }
	var routed int
	var signatures []string
	router := &RoutingTransport{Config: NewRemoteConfigCache(remote), Credential: "key-id.secret", Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		routed++
		if req.URL.String() != "https://proxy.invalid/routed?model=glm" || req.Host != "" {
			t.Errorf("not routed after signing: %s host=%q", req.URL, req.Host)
		}
		signatures = append(signatures, req.Header.Get("X-Client-Sig"))
		// Send the mapped request to our protocol-verifying TLS fixture. The V4
		// signature must already exist; the fixture independently verifies it and PoW.
		out := req.Clone(req.Context())
		var err error
		out.URL, err = url.Parse(originalURL)
		if err != nil {
			return nil, err
		}
		return server.Client().Transport.RoundTrip(out)
	})}
	transport := &SigningTransport{Signer: signer, Credential: "key-id.secret", SessionID: "end-user-session", Base: router}
	send := func() {
		t.Helper()
		req := signingRequest(t, server)
		// Simulate a caller setting Host explicitly; routing must replace it.
		req.Host = req.URL.Host
		originalHost := req.Host
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("unsigned fallback failed: status=%d err=%v", resp.StatusCode, err)
		}
		if req.URL.String() != originalURL || req.Host != originalHost || req.Header.Get("X-Client-Sig") != "" {
			t.Fatal("routing/retry mutated original request")
		}
	}
	send()
	if routed != 3 || len(signatures) != 3 || signatures[0] == "" || signatures[1] == "" || signatures[0] == signatures[1] || signatures[2] != "" || fixture.handshakes.Load() != 2 {
		t.Fatalf("expected two fresh signatures then unsigned: routed=%d handshakes=%d", routed, fixture.handshakes.Load())
	}
	if !signer.bypassing("key-id.secret") || signer.bypassing("other.secret") {
		t.Fatal("bypass not isolated by credential")
	}
	otherOrigin := testSigner(t, c, "https://other.invalid", SignatureAlways, nil)
	if otherOrigin.bypassing("key-id.secret") {
		t.Fatal("bypass leaked across origins")
	}
	signer.mu.Lock()
	remaining := len(signer.keys)
	signer.mu.Unlock()
	if remaining != 0 {
		t.Fatal("second rejected key remains cached")
	}
	now = now.Add(SignatureBypassTTL - time.Second)
	send()
	if routed != 4 || signatures[3] != "" || fixture.handshakes.Load() != 2 {
		t.Fatal("bypass ended too early")
	}
	now = now.Add(time.Second)
	send() // Fixture still rejects signatures; expiration must produce two new handshakes.
	if routed != 7 || fixture.handshakes.Load() != 4 || signatures[4] == "" || signatures[5] == "" || signatures[6] != "" || configCalls.Load() != 1 {
		t.Fatalf("bypass did not expire/re-route: routed=%d handshakes=%d config=%d", routed, fixture.handshakes.Load(), configCalls.Load())
	}
	signer.Invalidate("key-id.secret")
	if signer.bypassing("key-id.secret") {
		t.Fatal("Invalidate retained bypass")
	}
	signer.Bypass("key-id.secret")
	signer.Clear()
	if signer.bypassing("key-id.secret") {
		t.Fatal("Clear retained bypass")
	}
}

func TestSignatureModesAndConfigFailureRecovery(t *testing.T) {
	var configCalls atomic.Int64
	var unavailable atomic.Bool
	unavailable.Store(true)
	remote, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		configCalls.Add(1)
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		respond(t, w, 0, map[string]any{"codingPlanSignature": map[string]bool{"enable": true}})
	})
	cache := NewRemoteConfigCache(remote)
	now := time.Now()
	cache.now = func() time.Time { return now }
	c, server, fixture := newSigningServer(t)
	auto := testSigner(t, c, server.URL, SignatureAuto, cache)
	for range 2 {
		req := signingRequest(t, server)
		if err := auto.Sign(context.Background(), req, "key-id.secret", "end-user-session"); err != nil || req.Header.Get("X-Client-Sig") != "" {
			t.Fatalf("auto should be unsigned on failure: %v", err)
		}
		closeRequestBody(req)
	}
	if configCalls.Load() != 1 || fixture.handshakes.Load() != 0 {
		t.Fatal("failure backoff not honored")
	}
	for _, mode := range []SignatureMode{SignatureAlways, SignatureNever} {
		req := signingRequest(t, server)
		signer := testSigner(t, c, server.URL, mode, cache)
		if err := signer.Sign(context.Background(), req, "key-id.secret", "end-user-session"); err != nil {
			t.Fatal(err)
		}
		closeRequestBody(req)
		if (req.Header.Get("X-Client-Sig") != "") != (mode == SignatureAlways) || configCalls.Load() != 1 {
			t.Fatal("always/never depended on remote switch")
		}
	}
	unavailable.Store(false)
	now = now.Add(time.Minute)
	req := signingRequest(t, server)
	defer closeRequestBody(req)
	if err := auto.Sign(context.Background(), req, "key-id.secret", "end-user-session"); err != nil || req.Header.Get("X-Client-Sig") == "" || configCalls.Load() != 2 {
		t.Fatalf("auto did not recover after short backoff: %v", err)
	}
}

func TestHTTPSRequiredUnlessExplicitlyTestOnly(t *testing.T) {
	for _, field := range []string{"zcode", "bigmodel", "zai", "chat"} {
		opts := Options{}
		switch field {
		case "zcode":
			opts.ZCodeOrigin = "http://localhost:1234"
		case "bigmodel":
			opts.BigModelOrigin = "http://localhost:1234"
		case "zai":
			opts.ZaiOrigin = "http://localhost:1234"
		case "chat":
			opts.ZaiChatOrigin = "http://localhost:1234"
		}
		if _, err := NewClient(opts); err == nil {
			t.Fatalf("HTTP %s origin silently accepted", field)
		}
		opts.AllowInsecureHTTP = true
		if _, err := NewClient(opts); err != nil {
			t.Fatalf("explicit test origin rejected: %v", err)
		}
	}
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {}, true)
	if _, err := NewSigner(c, SignerOptions{Origin: server.URL}); err == nil {
		t.Fatal("client's test flag silently enabled HTTP signer")
	}
	if _, err := NewSigner(c, SignerOptions{Origin: server.URL, AllowInsecureHTTP: true}); err != nil {
		t.Fatal(err)
	}
}
