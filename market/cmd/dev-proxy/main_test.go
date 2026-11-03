package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminMarketProxyRewritesPathAndUsesLocalKey(t *testing.T) {
	var gotPath, gotKey, gotAuthorization, gotForwardedHost, gotForwardedProto string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotAuthorization = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		gotForwardedHost, gotForwardedProto = r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	h := newHandler(targetURL, "local-admin-key", "")
	req := httptest.NewRequest(http.MethodGet, "/api/admin/pools?limit=5", nil)
	req.Host = "market.local:8080"
	req.Header.Set("Authorization", "Bearer untrusted-browser-token")
	req.Header.Set("x-api-key", "untrusted-browser-key")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.Code)
	}
	if gotPath != "/api/v1/admin/market/pools" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotKey != "local-admin-key" || gotAuthorization != "" {
		t.Fatalf("headers = x-api-key %q, Authorization %q", gotKey, gotAuthorization)
	}
	if gotForwardedHost != "market.local:8080" || gotForwardedProto != "http" {
		t.Fatalf("forwarded origin = %q://%q", gotForwardedProto, gotForwardedHost)
	}
}

func TestAdminStatusShimRewritesToEngineMarketStatus(t *testing.T) {
	var gotPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	h := newHandler(targetURL, "key", "")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/sub2api/status", nil))
	if res.Code != http.StatusOK || gotPath != "/api/v1/admin/market/status" {
		t.Fatalf("status/path = %d %q", res.Code, gotPath)
	}
}

func TestEngineAdminRoutesAreNotRewrittenOrGivenMarketKey(t *testing.T) {
	var gotPath, gotKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	h := newHandler(targetURL, "local-admin-key", "")
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/engine/session", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.local")
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || gotPath != "/api/admin/engine/session" || gotKey != "" {
		t.Fatalf("status/path/key = %d %q %q", res.Code, gotPath, gotKey)
	}
}

func TestMarketAdminProxyRejectsCrossSiteFetchMetadata(t *testing.T) {
	var calls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	h := newHandler(mustURL(t, target.URL), "key", "")
	for _, site := range []string{"cross-site", "same-site", "none; mode=navigate"} {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/pools", nil)
		req.Header.Set("Sec-Fetch-Site", site)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Errorf("Sec-Fetch-Site %q status = %d, want 403", site, res.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("rejected requests reached upstream: %d", calls)
	}
}

func TestMarketAdminProxyChecksOriginAndAllowsDevCurl(t *testing.T) {
	var calls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	h := newHandler(mustURL(t, target.URL), "key", "")
	for _, origin := range []string{"https://evil.local", "http://market.local:80", "null", "not a url"} {
		req := httptest.NewRequest(http.MethodPost, "http://market.local/api/admin/pools", nil)
		req.Host = "market.local"
		req.Header.Set("Origin", origin)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Errorf("Origin %q status = %d, want 403", origin, res.Code)
		}
	}

	for _, tc := range []struct {
		name string
		set  func(*http.Request)
	}{
		{name: "no browser headers"},
		{name: "matching origin", set: func(r *http.Request) { r.Header.Set("Origin", "http://market.local") }},
		{name: "same origin fetch", set: func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }},
		{name: "none fetch", set: func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "none") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://market.local/api/admin/pools", nil)
			req.Host = "market.local"
			if tc.set != nil {
				tc.set(req)
			}
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", res.Code)
			}
		})
	}
	if calls != 4 {
		t.Fatalf("allowed requests reached upstream %d times, want 4", calls)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAdminSPAAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "admin-assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("admin index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin-assets", "app.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("logo"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetURL, _ := url.Parse("http://127.0.0.1:1")
	h := newHandler(targetURL, "", dir)
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/admin/market/pools", "admin index"},
		{"/admin-assets/app.js", "asset"},
		{"/logo.svg", "logo"},
	} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), tc.want) {
			t.Errorf("GET %s = %d %q", tc.path, res.Code, res.Body.String())
		}
	}
}
