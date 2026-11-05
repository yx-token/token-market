package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarketEmbedForwardsPublicPromoVisit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	const payload = `{"code":"v2ex-post","visitor_id":"visitor_123","landing_path":"/","referrer":"https://v2ex.com/"}`
	calls := 0
	registerMarketEmbed(router, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/market/promo/visit", r.URL.Path)
		require.Equal(t, "source=link", r.URL.RawQuery)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "203.0.113.42", r.Header.Get("X-Forwarded-For"))
		require.Equal(t, "promo-visitor-test", r.UserAgent())
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, payload, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, err = io.WriteString(w, `{"accepted":true}`)
		require.NoError(t, err)
	}))

	// The public visit endpoint must have an explicit API mount, even if SPA fallback changes.
	haveRoute := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/market/*filepath" {
			haveRoute = true
			break
		}
	}
	require.True(t, haveRoute, "missing public market POST mount")

	request := httptest.NewRequest(http.MethodPost, "/api/market/promo/visit?source=link", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "203.0.113.42")
	request.Header.Set("User-Agent", "promo-visitor-test")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, 1, calls)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.JSONEq(t, `{"accepted":true}`, recorder.Body.String())
}
