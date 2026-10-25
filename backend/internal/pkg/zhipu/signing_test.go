//go:build unit

package zhipu

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Independent RFC5869 implementation: the mock server does not reuse signer helpers.
func serverKDF(secret, info string) []byte {
	extract := hmac.New(sha256.New, []byte("WD_CLIENT_SIGN_KDF_SALT"))
	_, _ = extract.Write([]byte(secret))
	expand := hmac.New(sha256.New, extract.Sum(nil))
	_, _ = expand.Write(append([]byte(info), 1))
	return expand.Sum(nil)
}

type signingServer struct {
	handshakes       atomic.Int64
	requests         atomic.Int64
	unsignedRequests atomic.Int64
	reject           atomic.Int64
	alwaysReject     bool
	mu               sync.Mutex
	public           ed25519.PublicKey
	nonces           map[string]bool
	ciphertext       string
}

func newSigningServer(t *testing.T, insecure ...bool) (*Client, *httptest.Server, *signingServer) {
	t.Helper()
	f := &signingServer{nonces: make(map[string]bool)}
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == clientKeyPath {
			f.handshakes.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if r.Method != "POST" || r.Header.Get("Authorization") != "key-id.secret" || body["apiKey"] != "key-id.secret" {
				t.Error("handshake request mismatch")
			}
			if len(body["nonce"]) != 32 {
				t.Error("handshake nonce length")
			}
			if _, err := hex.DecodeString(body["nonce"]); err != nil {
				t.Error(err)
			}
			if _, err := strconv.ParseInt(body["ts"], 10, 64); err != nil {
				t.Error(err)
			}
			mac := hmac.New(sha256.New, serverKDF("secret", "getSignKey_hmac"))
			_, _ = mac.Write([]byte("get_sign_key\nkey-id\n" + body["ts"] + "\n" + body["nonce"]))
			got, err := base64.StdEncoding.DecodeString(body["sig"])
			if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
				t.Error("handshake HMAC mismatch")
			}
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			der, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			block, err := aes.NewCipher(serverKDF("secret", "ed25519_priv"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			gcm, err := cipher.NewGCM(block)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			iv := make([]byte, 12)
			if _, err := rand.Read(iv); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			encrypted := base64.StdEncoding.EncodeToString(append(iv, gcm.Seal(nil, iv, []byte(base64.StdEncoding.EncodeToString(der)), []byte("key-id"))...))
			f.mu.Lock()
			f.public = public
			f.ciphertext = encrypted
			f.mu.Unlock()
			respond(t, w, 200, map[string]string{"privateCipher": encrypted})
			return
		}
		if r.URL.Path != "/api/coding/paas/v4/chat/completions" {
			t.Errorf("unexpected signing path: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		f.requests.Add(1)
		h := r.Header
		if h.Get("X-Client-Sig") == "" {
			f.unsignedRequests.Add(1)
			if !f.alwaysReject {
				t.Error("unexpected unsigned inference")
			}
			for _, name := range []string{"X-Client-Ts", "X-Client-Version", "X-Client-Nonce", "X-Client-Pow", "X-App-Id", "X-Client-Sign-Verified"} {
				if h.Get(name) != "" {
					t.Error("unsigned fallback retained signing header")
				}
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != `{"messages":[]}` {
				t.Error("unsigned body not replayed")
			}
			respond(t, w, 0, map[string]bool{"unsigned": true})
			return
		}
		if h.Get("X-App-Id") != "zcode" || h.Get("X-Client-Version") != "3.11.2" || h.Get("X-Session-Id") != "end-user-session" || h.Get("User-Agent") != "ZCode/3.11.2" {
			t.Error("signing identity headers incorrect")
		}
		nonce := h.Get("X-Client-Nonce")
		if len(nonce) != 32 || nonce != strings.ToLower(nonce) {
			t.Error("invalid signing nonce")
		}
		f.mu.Lock()
		public := append(ed25519.PublicKey(nil), f.public...)
		if f.nonces[nonce] {
			t.Error("nonce reused")
		}
		f.nonces[nonce] = true
		f.mu.Unlock()
		sig, err := base64.StdEncoding.DecodeString(h.Get("X-Client-Sig"))
		message := strings.Join([]string{"key-id", h.Get("X-Client-Ts"), "3.11.2", "end-user-session", nonce}, "\n")
		if err != nil || len(public) != ed25519.PublicKeySize || !ed25519.Verify(public, []byte(message), sig) {
			t.Error("Ed25519 signature incorrect")
		}
		seedDigest := sha256.Sum256([]byte("key-id\nzcode\nend-user-session\n" + h.Get("X-Client-Ts")))
		seed := hex.EncodeToString(seedDigest[:])[:32]
		pow := h.Get("X-Client-Pow")
		powBytes, err := hex.DecodeString(pow)
		digest := sha256.Sum256([]byte(seed + "\n" + pow))
		if err != nil || len(powBytes) != 16 || digest[0] != 0 {
			t.Error("PoW incorrect")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"messages":[]}` {
			t.Error("request body changed or not replayed")
		}
		if f.alwaysReject || f.reject.Add(-1) >= 0 {
			w.WriteHeader(http.StatusUnauthorized)
			respond(t, w, 401, map[string]string{"reason": "VERIFY_SIGNATURE_INVALID"})
			return
		}
		respond(t, w, 0, map[string]bool{"ok": true})
	}, len(insecure) > 0 && insecure[0])
	return c, server, f
}

func testSigner(t *testing.T, c *Client, origin string, mode SignatureMode, config *RemoteConfigCache) *Signer {
	t.Helper()
	s, err := NewSigner(c, SignerOptions{Origin: origin, Mode: mode, Config: config, AllowInsecureHTTP: strings.HasPrefix(origin, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func signingRequest(t *testing.T, server *httptest.Server) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/coding/paas/v4/chat/completions?model=glm", bytes.NewBufferString(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer key-id.secret")
	return req
}

func TestSigningEndToEndAndCache(t *testing.T) {
	c, server, f := newSigningServer(t)
	s := testSigner(t, c, server.URL, "", nil)
	for i := 0; i < 3; i++ {
		req := signingRequest(t, server)
		if err := s.Sign(context.Background(), req, "key-id.secret", "end-user-session"); err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal("signed request failed")
		}
	}
	if f.handshakes.Load() != 1 {
		t.Fatalf("cache missed: %d handshakes", f.handshakes.Load())
	}
	f.mu.Lock()
	encrypted := f.ciphertext
	f.mu.Unlock()
	if _, err := decryptPrivateKey(encrypted, "wrong-id", "secret"); err == nil {
		t.Fatal("AAD ignored")
	}
	if _, err := decryptPrivateKey(encrypted, "key-id", "wrong-secret"); err == nil {
		t.Fatal("wrong encryption key accepted")
	}
	for _, bad := range []string{"", "%%%", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := decryptPrivateKey(bad, "key-id", "secret"); err == nil {
			t.Fatal("bad cipher accepted")
		}
	}
	s.Invalidate("key-id.secret")
	if err := s.Sign(context.Background(), signingRequest(t, server), "key-id.secret", "end-user-session"); err != nil {
		t.Fatal(err)
	}
	if f.handshakes.Load() != 2 {
		t.Fatal("invalidation did not re-handshake")
	}
	s.Clear()
	if err := s.Sign(context.Background(), signingRequest(t, server), "key-id.secret", "end-user-session"); err != nil {
		t.Fatal(err)
	}
	if f.handshakes.Load() != 3 {
		t.Fatal("clear did not re-handshake")
	}
}

func TestStaleRejectionPreservesRefreshedKey(t *testing.T) {
	c, server, f := newSigningServer(t)
	s := testSigner(t, c, server.URL, "", nil)
	old := signingRequest(t, server)
	if err := s.Sign(context.Background(), old, "key-id.secret", "end-user-session"); err != nil {
		t.Fatal(err)
	}
	s.Invalidate("key-id.secret")
	newRequest := signingRequest(t, server)
	if err := s.Sign(context.Background(), newRequest, "key-id.secret", "end-user-session"); err != nil {
		t.Fatal(err)
	}
	s.invalidateForRequest("key-id.secret", old.Header)
	if err := s.Sign(context.Background(), signingRequest(t, server), "key-id.secret", "end-user-session"); err != nil {
		t.Fatal(err)
	}
	if f.handshakes.Load() != 2 {
		t.Fatal("stale rejection evicted refreshed key")
	}
}

func TestHandshakeFailureNotCached(t *testing.T) {
	var requests atomic.Int64
	c, server := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		respond(t, w, 0, map[string]string{"privateCipher": "invalid"}) // Handshake uniquely requires code 200.
	})
	s := testSigner(t, c, server.URL, "", nil)
	for range 2 {
		if err := s.Sign(context.Background(), signingRequest(t, server), "id.secret", "session"); err == nil {
			t.Fatal("handshake code 0 accepted")
		}
	}
	if requests.Load() != 2 {
		t.Fatal("failed handshake cached")
	}
}

func TestSigningTransportRetry(t *testing.T) {
	for _, scenario := range []string{"recovered", "rejected_twice", "unreplayable"} {
		t.Run(scenario, func(t *testing.T) {
			c, server, f := newSigningServer(t)
			f.reject.Store(1)
			f.alwaysReject = scenario == "rejected_twice"
			s := testSigner(t, c, server.URL, SignatureAlways, nil)
			h := &http.Client{Transport: &SigningTransport{Signer: s, Credential: "key-id.secret", SessionID: "end-user-session"}, Timeout: 5 * time.Second}
			req := signingRequest(t, server)
			if scenario == "unreplayable" {
				req.GetBody = nil
			}
			resp, err := h.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := 200
			wantRequests := int64(2)
			wantHandshakes := int64(2)
			if scenario == "rejected_twice" {
				wantRequests = 3
			}
			if scenario == "unreplayable" {
				wantStatus = 401
				wantRequests = 1
				wantHandshakes = 1
			}
			if resp.StatusCode != wantStatus || f.requests.Load() != wantRequests || f.handshakes.Load() != wantHandshakes {
				t.Fatalf("unexpected retries: status=%d requests=%d handshakes=%d", resp.StatusCode, f.requests.Load(), f.handshakes.Load())
			}
			if wantStatus == 401 && !IsSigningError(resp.StatusCode, body) {
				t.Fatal("error response body lost")
			}
			if req.Header.Get("X-Client-Sig") != "" {
				t.Fatal("transport mutated original request")
			}
			if scenario != "recovered" {
				s.mu.Lock()
				remaining := len(s.keys)
				s.mu.Unlock()
				if remaining != 0 {
					t.Fatal("rejected key remains cached")
				}
			}
		})
	}
}

func TestConcurrentHandshake(t *testing.T) {
	c, server, f := newSigningServer(t)
	s := testSigner(t, c, server.URL, "", nil)
	errorsCh := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/anthropic/v1/messages", nil)
			errorsCh <- s.Sign(context.Background(), req, "key-id.secret", "session")
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.handshakes.Load() != 1 {
		t.Fatalf("concurrent handshakes not coalesced: %d", f.handshakes.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Sign(ctx, signingRequest(t, server), "key-id.secret", "session"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSigningValidationAndModes(t *testing.T) {
	requests := 0
	c, server := testClient(t, func(w http.ResponseWriter, r *http.Request) { requests++; t.Error("unexpected handshake/config") })
	s := testSigner(t, c, server.URL, SignatureAlways, nil)
	for _, tc := range []struct{ credential, session string }{{"id", "session"}, {"id.secret", ""}, {"id.secret", "\n"}} {
		if err := s.Sign(context.Background(), signingRequest(t, server), tc.credential, tc.session); err == nil {
			t.Fatal("invalid sign inputs accepted")
		}
	}
	req := signingRequest(t, server)
	req.URL.Host = "other.invalid"
	if err := s.Sign(context.Background(), req, "id.secret", "session"); err != nil {
		t.Fatalf("cross-origin credential leakage: %v", err)
	}
	for _, mode := range []SignatureMode{SignatureAlways, SignatureNever} {
		s := testSigner(t, c, server.URL, mode, nil)
		req := signingRequest(t, server)
		if mode == SignatureAlways {
			req.URL.Path = "/api/v1/zcode-plan/chat/completions"
		}
		req.Header.Set("X-Client-Sig", "untrusted")
		if err := s.Sign(context.Background(), req, "invalid", ""); err != nil {
			t.Fatal(err)
		}
		if req.Header.Get("X-Client-Sig") != "" {
			t.Fatal("stale signature retained")
		}
	}
	if requests != 0 {
		t.Fatal("validation contacted upstream")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := computePoW(ctx, "seed", "prefix"); !errors.Is(err, context.Canceled) {
		t.Fatal("PoW ignored cancellation")
	}
}

func TestSigningErrorRecognition(t *testing.T) {
	for _, field := range []string{`"msg":"%s"`, `"reason":"%s"`, `"data":{"reason":"%s"}`, `"error":{"reason":"%s"}`, `"error":{"message":"%s"}`, `"error":{"code":"%s"}`} {
		for _, code := range []string{"VERIFY_SIGNATURE_INVALID", "VERIFY_APIKEY_EXPIRED"} {
			body := []byte("{" + fmt.Sprintf(field, code) + "}")
			if !IsSigningError(401, body) || IsSigningError(500, body) {
				t.Fatalf("bad recognition %s", body)
			}
		}
	}
	if IsSigningError(401, []byte(`{"msg":"unrelated"}`)) || IsSigningError(401, []byte(`invalid`)) {
		t.Fatal("unrelated error retried")
	}
}

func TestSigningTransportReusesAccountProxy(t *testing.T) {
	_, upstream, f := newSigningServer(t, true)
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		out := r.Clone(r.Context())
		out.RequestURI = ""
		resp, err := http.DefaultTransport.RoundTrip(out)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(proxy.Close)
	c, err := NewClient(Options{ProxyURL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	s := testSigner(t, c, upstream.URL, SignatureAlways, nil)
	h := &http.Client{Transport: &SigningTransport{Signer: s, Credential: "key-id.secret", SessionID: "end-user-session"}, Timeout: 5 * time.Second}
	resp, err := h.Do(signingRequest(t, upstream))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || proxyRequests.Load() != 2 || f.handshakes.Load() != 1 || f.requests.Load() != 1 {
		t.Fatal("handshake or inference bypassed account proxy")
	}
}
