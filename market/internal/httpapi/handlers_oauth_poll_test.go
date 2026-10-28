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
	"github.com/yx-token/token-market/market/internal/platform/httpx"
)

func TestOAuthRejectsCallerSessionsAndProxies(t *testing.T) {
	for _, handler := range []httpx.Handler{(&Server{}).startOAuth, (&Server{}).adminStartUpstreamOAuth, (&Server{}).reauthorize, (&Server{}).pollOAuth, (&Server{}).adminPollUpstreamOAuth, (&Server{}).completeOAuth, (&Server{}).adminCompleteUpstreamOAuth} {
		for _, body := range []string{`{"session_id":"attacker-session"}`, `{"proxy_id":99}`} {
			request := httptest.NewRequest(http.MethodPost, "/oauth", strings.NewReader(body))
			request.SetPathValue("id", "1")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_JSON") {
				t.Fatalf("caller-controlled session/proxy accepted: %d %s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "session_id") || strings.Contains(recorder.Body.String(), "attacker-session") {
				t.Fatalf("OAuth error echoed internal session field: %s", recorder.Body.String())
			}
		}
	}
}

func TestOAuthRoutesRequireAuthentication(t *testing.T) {
	issuer, err := auth.NewIssuer(strings.Repeat("t", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Issuer: issuer, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, test := range []struct {
		handler http.Handler
		roles   []string
	}{{server.Handler(), []string{"seller", "admin"}}, {server.HandlerEmbed(), []string{"seller"}}} {
		for _, role := range test.roles {
			for _, operation := range []string{"start", "poll", "complete"} {
				path := "/api/" + role + "/upstreams/1/oauth/" + operation
				recorder := httptest.NewRecorder()
				test.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
				if recorder.Code != http.StatusUnauthorized {
					t.Fatalf("OAuth route %s does not require session auth: %d %s", path, recorder.Code, recorder.Body.String())
				}
			}
		}
	}
}

func TestOAuthCallbackInput(t *testing.T) {
	callback := "zcode://oauth/callback?authCode=secret&state=state"
	if got, err := (oauthCompleteInput{CallbackURL: callback}).value(); err != nil || got != callback {
		t.Fatalf("callback input: %s %v", got, err)
	}
	if _, err := (oauthCompleteInput{Code: "code", CallbackURL: callback}).value(); err == nil {
		t.Fatal("ambiguous callback accepted")
	}
}
