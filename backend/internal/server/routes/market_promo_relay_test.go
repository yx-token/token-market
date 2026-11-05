package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/idlecat"
)

func TestPromoChannelsAdminRoutesUseRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/promo-channels?from=2026-10-07&to=2026-11-05", ""},
		{http.MethodPost, "/promo-channels", `{"name":"V2EX","code":"v2ex-post"}`},
		{http.MethodPut, "/promo-channels/42", `{"name":"V2EX","landing_path":"/","note":""}`},
		{http.MethodPost, "/promo-channels/42/status", `{"status":"disabled"}`},
		{http.MethodDelete, "/promo-channels/42", ""},
		{http.MethodGet, "/promo-channels/42/daily?from=2026-10-07&to=2026-11-05", ""},
		{http.MethodGet, "/promo-channels/42/users?page=2&page_size=20", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, "/api/v1/admin/market"+tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "promo channels must not query the native thin store")
			// An uninitialized market host returns 503 only after the request reaches ServeAdmin.
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "market ledger unavailable")
		})
	}
}
