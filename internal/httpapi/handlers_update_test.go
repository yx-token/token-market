package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/platform/auth"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/update"
)

type updateStub struct{ starts int }

func (*updateStub) Status() update.Status                             { return update.Status{Version: "v1.0.0", Enabled: true} }
func (*updateStub) Check(context.Context, bool) (update.Check, error) { return update.Check{}, nil }
func (u *updateStub) Start(context.Context, string) (*update.Job, error) {
	u.starts++
	return nil, update.ErrBusy
}

func TestUpdateRoutesRequireAuthentication(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.Handler()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/admin/system/version"}, {"GET", "/api/admin/system/check-update"}, {"POST", "/api/admin/system/update"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", tc.path, rec.Code)
		}
	}
}

func TestSystemUpdateRejectsNonAdminUnknownInputAndConcurrentUpdate(t *testing.T) {
	u := &updateStub{}
	s := &Server{Updater: u}
	issuer, err := auth.NewIssuer(strings.Repeat("t", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := httpx.Authenticate(issuer, nil, httpx.RequireAdmin(httpx.Handler(s.startSystemUpdate)))
	for _, tc := range []struct {
		admin bool
		body  string
		want  int
	}{
		{false, `{"version":"v1.1.0"}`, 403},
		{true, `{"version":"v1.1.0","url":"https://example.com"}`, 400},
		{true, `{}`, 400},
		{true, `{"version":"v1.1.0"}`, 409},
	} {
		token, _, err := issuer.Issue(auth.Claims{UserID: 1, IsAdmin: tc.admin})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/admin/system/update", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("admin=%v body=%s: %d %s", tc.admin, tc.body, rec.Code, rec.Body.String())
		}
	}
	if u.starts != 1 {
		t.Fatalf("unauthorized or invalid update reached manager: %d", u.starts)
	}
}
