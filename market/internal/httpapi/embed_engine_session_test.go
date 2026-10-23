package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/platform/auth"
)

// 单进程嵌入时，C 端「控制台」依赖 POST /api/admin/engine/session；不得落入 /api/ 的 NOT_FOUND。
func TestHandlerEmbedRegistersEngineSession(t *testing.T) {
	issuer, err := auth.NewIssuer(strings.Repeat("t", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Issuer: issuer,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h := s.HandlerEmbed()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/engine/session", nil))
	if rec.Code == http.StatusNotFound || strings.Contains(rec.Body.String(), "endpoint not found") {
		t.Fatalf("embed handler missing engine session: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated want 401, got %d %s", rec.Code, rec.Body.String())
	}

	// 完整 Handler 也有该路由；确认 Embed 未误删
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/admin/engine/session", nil))
	if rec2.Code == http.StatusNotFound {
		t.Fatalf("full handler missing engine session: %d %s", rec2.Code, rec2.Body.String())
	}
}
