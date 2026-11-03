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
