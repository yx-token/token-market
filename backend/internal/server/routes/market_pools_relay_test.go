package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yx-token/token-market/internal/idlecat"
)

// 保障池列表必须走 relay（market 完整处理函数）。
// idlecat.Store 的简表不含 min_margin / safety_buffer / market_description：编辑弹窗回显成 undefined，
// 列表页的「上架/下架」「开启/关闭」按钮又会把没拿到的市场描述当空串写回库里。
func TestPoolsReadUsesRelayNotThinStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 简表 Store 没有数据库句柄：谁误走简表读取，会在访问 s.Market 时 panic，而不是静默返回缺列的结果。
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/market/pools", nil)
	require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "读取不应落到简表 Store")
	// relay 进入 enginehost.ServeAdmin；测试进程没有初始化 market，返回 503 说明已经走进 relay。
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
}

// 保障池的读与写要走同一套 market 处理函数，字段口径才一致。
func TestPoolsRelayCoversReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	have := map[string]bool{}
	for _, r := range router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/admin/market/pools",
		"POST /api/v1/admin/market/pools",
		"PUT /api/v1/admin/market/pools/:id",
		"POST /api/v1/admin/market/pools/:id/sync",
		"DELETE /api/v1/admin/market/pools/:id",
	} {
		require.True(t, have[want], "缺少路由 %s", want)
	}
}

// 厂商与代理后台读取必须使用 market 完整响应，native 简表没有 require_seller_proxy / is_shared。
func TestVendorAndProxyReadsUseRelayNotThinStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	for _, path := range []string{
		"/api/v1/admin/market/vendors",
		"/api/v1/admin/market/proxies",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "读取不应落到简表 Store")
			// 503 表示已进入 relay；本测试进程没有初始化 market host。
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
		})
	}
}

func TestVendorAndProxyRelayCoversReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	have := map[string]bool{}
	for _, r := range router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/admin/market/vendors",
		"PUT /api/v1/admin/market/vendors/:platform",
		"GET /api/v1/admin/market/proxies",
		"POST /api/v1/admin/market/proxies",
		"GET /api/v1/admin/market/engine-proxies/sharing",
		"PUT /api/v1/admin/market/engine-proxies/:id/sharing",
		"POST /api/v1/admin/market/engine-proxies/:id/test",
		"DELETE /api/v1/admin/market/proxies/:id",
		"POST /api/v1/admin/market/proxies/:id/sync",
		"POST /api/v1/admin/market/proxies/:id/test",
		"POST /api/v1/admin/market/proxies/:id/disable",
		"POST /api/v1/admin/market/proxies/:id/enable",
	} {
		require.True(t, have[want], "缺少路由 %s", want)
	}
}

func TestMarketPaymentConfigRelayEndpointsAreRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	have := map[string]bool{}
	for _, r := range router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, removed := range []string{
		"GET /api/v1/admin/market/payments/status",
		"GET /api/v1/admin/market/payments/merchants",
		"PUT /api/v1/admin/market/payments/merchants/:provider",
		"POST /api/v1/admin/market/payments/merchants/:provider/test",
	} {
		require.False(t, have[removed], "已移除的收款配置路由仍被注册：%s", removed)
	}
}

func TestMarketFinancialReadsUseCNYRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})
	for _, path := range []string{"recharges", "refunds", "withdrawals"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/market/"+path, nil)
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) })
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
		})
	}
}
