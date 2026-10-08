package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPAHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assets", "app-abc.js"), []byte("console.log(1)"), 0o644)
	h := spaHandler(dir)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/assets/app-abc.js"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "console.log") || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	for _, p := range []string{"/", "/seller/upstreams/3"} {
		if rec := get(p); rec.Code != 200 || rec.Body.String() != "<html>app</html>" || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s should fall back to index.html: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/../../etc/passwd"); rec.Code != http.StatusBadRequest { // 不允许路径穿越
		t.Fatalf("path traversal: %d %q", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-GET should not be served: %d", rec.Code)
	}
}
