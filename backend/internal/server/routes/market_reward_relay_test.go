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

// 任务对接的配置含加密的 app_secret，只能由 market 原处理函数读写，引擎不得用简表实现。
func TestRewardTasksAdminRoutesUseRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/reward-tasks/config", ""},
		{http.MethodPut, "/reward-tasks/config", `{"enabled":false,"base_url":"https://loxo.cc","app_key":"rk_x","app_secret":"","sdk_enabled":true}`},
		{http.MethodPost, "/reward-tasks/ping", `{}`},
		{http.MethodGet, "/reward-tasks/tickets?state=active&page=2&page_size=20", ""},
		{http.MethodGet, "/reward-tasks/events?state=rejected&page=1&page_size=20", ""},
		{http.MethodPost, "/reward-tasks/events/42/retry", `{}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, "/api/v1/admin/market"+tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "reward tasks must not query the native thin store")
			// An uninitialized market host returns 503 only after the request reaches ServeAdmin.
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "market ledger unavailable")
		})
	}
}
