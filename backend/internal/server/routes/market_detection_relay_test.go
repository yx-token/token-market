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

// 检测策略与检测站的读取必须走 relay（market 完整处理函数）。
// idlecat.Store 的简表只有 platform / auth_type / enabled 三列：编辑弹窗按它回显会得到空配置，
// 管理员再保存就会用空值覆盖库里的 flows、third_party、thresholds 等全部设置。
func TestDetectionReadsUseRelayNotThinStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 简表 Store 没有数据库句柄：谁误走简表读取，会在访问 s.Market 时 panic，而不是静默返回三列。
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	for _, path := range []string{
		"/api/v1/admin/market/detection-policies",
		"/api/v1/admin/market/detection-providers",
	} {
		t.Run(strings.TrimPrefix(path, "/api/v1/admin/market/"), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "读取不应落到简表 Store")
			// relay 进入 enginehost.ServeAdmin；测试进程没有初始化 market，返回 503 说明已经走进 relay。
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
		})
	}
}

// relay 注册表要同时覆盖读与写，读写走同一套 market 处理函数，字段口径才一致。
func TestDetectionRelayCoversReadAndWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNativeIdleCat(router.Group("/api/v1/admin"), &idlecat.Store{})

	have := map[string]bool{}
	for _, r := range router.Routes() {
		have[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/v1/admin/market/detection-policies",
		"PUT /api/v1/admin/market/detection-policies/:platform",
		"GET /api/v1/admin/market/detection-providers",
		"PUT /api/v1/admin/market/detection-providers/:name",
	} {
		require.True(t, have[want], "缺少路由 %s", want)
	}
}
