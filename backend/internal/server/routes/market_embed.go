package routes

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yx-token/token-market/market/enginehost"
)

// MarketEdgeDispatch routes market buyer keys before sub2api gateway auth runs.
func MarketEdgeDispatch() gin.HandlerFunc {
	return marketEdgeDispatch(enginehost.EdgeRequestHandler(), enginehost.IsEdgeRequest)
}

func marketEdgeDispatch(edgeHandler http.Handler, isEdgeRequest func(*http.Request) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if edgeHandler != nil && isEdgeRequest != nil && isEdgeRequest(c.Request) {
			edgeHandler.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RegisterMarketEmbed 把市场 C 端 API/静态挂进宿主进程（MARKET_EMBED=1）。
// 管理端仍走宿主 frontend（/admin）与 /api/v1/admin/market/*。
func RegisterMarketEmbed(r *gin.Engine) {
	h := enginehost.PublicHandler()
	if h == nil {
		if enginehost.Enabled() {
			log.Printf("market embed: enabled but handler unavailable")
		}
		return
	}
	registerMarketEmbed(r, h)
	log.Printf("market embed: public handler mounted (TM_HTTP_WEB_DIR=%s)", strings.TrimSpace(os.Getenv("TM_HTTP_WEB_DIR")))
}

func registerMarketEmbed(r *gin.Engine, h http.Handler) {
	wrap := gin.WrapH(h)
	// 控制面 API（含 /open-admin 换宿主会话的 /api/admin/engine/*）
	for _, p := range []string{
		"/api/auth", "/api/me", "/api/buyer", "/api/seller", "/api/market", "/api/payments",
		"/api/admin/engine", "/healthz",
	} {
		r.Any(p, wrap)
		r.Any(p+"/*filepath", wrap)
	}
	// C 端页面与静态（管理端 /admin* 不走这里）
	for _, p := range []string{"/buyer", "/seller", "/login", "/register", "/shop", "/open-admin", "/assets"} {
		r.Any(p, wrap)
		r.Any(p+"/*filepath", wrap)
	}
	// 根路径与未命中路由：交给市场 SPA（排除 /admin* 与管理端 public 根图标）
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/admin" || strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/admin-assets/") ||
			path == "/xianmao-logo.svg" || path == "/logo.svg" || path == "/favicon.ico" || path == "/logo.png" {
			c.Status(http.StatusNotFound)
			return
		}
		// 宿主自身 API / 网关路径不交给市场
		if strings.HasPrefix(path, "/api/v1") || strings.HasPrefix(path, "/v1/") ||
			strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/backend-api/") ||
			path == "/health" || path == "/models" {
			c.Status(http.StatusNotFound)
			return
		}
		h.ServeHTTP(c.Writer, c.Request)
	})
}
