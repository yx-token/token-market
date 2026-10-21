package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEngineProxyBlocksOnlineUpdate(t *testing.T) {
	var proxied []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = append(proxied, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer engine.Close()
	h := (&Server{EngineProxyURL: engine.URL, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()

	for _, path := range []string{"/api/v1/admin/system/update", "/api/v1/admin/system/rollback", "/api/v1/admin/system/update/x"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/version", nil))
	if rec.Code != http.StatusOK || len(proxied) != 1 || proxied[0] != "GET /api/v1/admin/system/version" {
		t.Fatalf("other engine routes should be proxied: code=%d proxied=%v", rec.Code, proxied)
	}
}
