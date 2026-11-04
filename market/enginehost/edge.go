package enginehost

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yx-token/token-market/market/internal/edge"
	"github.com/yx-token/token-market/market/internal/edgeloop"
	"github.com/yx-token/token-market/market/internal/platform/config"
)

var (
	edgeOnce    sync.Once
	edgeRun     *edgeloop.Runtime
	edgeErr     error
	hostGateway atomic.Pointer[string]
)

// SetHostGateway is called by the host before constructing routes or starting Edge.
// The embedded gateway always returns to this host, independently of market relay settings.
func SetHostGateway(host string, port int) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 8080
	}
	value := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	hostGateway.Store(&value)
}

func hostGatewayURL() string {
	if value := hostGateway.Load(); value != nil {
		return *value
	}
	return "http://127.0.0.1:8080"
}

// EdgeEnabled 宿主是否内嵌 Edge。默认随 MARKET_EMBED 开启；可设 MARKET_EMBED_EDGE=0 关闭。
func EdgeEnabled() bool {
	if !Enabled() {
		return false
	}
	v := strings.TrimSpace(os.Getenv("MARKET_EMBED_EDGE"))
	return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "no")
}

// ensureEdge 创建一次 Edge runtime。宿主路由与 8090 监听共用同一 Server、缓存、熔断器和日志器。
func ensureEdge() (*edgeloop.Runtime, error) {
	if !EdgeEnabled() {
		return nil, nil
	}
	edgeOnce.Do(func() {
		cfg, err := config.Load("")
		if err != nil {
			edgeErr = err
			return
		}
		if cfg.Edge.Addr == "" {
			cfg.Edge.Addr = ":8090"
		}
		// 内嵌时不另开 edge admin 端口；网关走本机宿主，避免经 docker 主机名绕行。
		cfg.Edge.AdminAddr = ""
		cfg.Sub2API.GatewayURL = hostGatewayURL()
		edgeRun, edgeErr = edgeloop.NewRuntime(cfg, slog.Default())
	})
	return edgeRun, edgeErr
}

// EdgeRequestHandler 返回嵌入 Edge 的共享 handler。关闭或初始化失败时返回 503 handler。
func EdgeRequestHandler() http.Handler {
	runtime, err := ensureEdge()
	if err != nil {
		slog.Error("enginehost: embed edge config failed", "err", err)
	}
	if runtime == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"market edge unavailable"}}` + "\n"))
		})
	}
	return runtime.Handler()
}

// IsEdgeRequest 判断请求是否应由闲猫 Edge 接管；非 tm- Key 保持走 sub2api 原生网关。
// 此处只识别宿主网关请求；能否放行仍由 Edge 原有白名单决定。
func IsEdgeRequest(r *http.Request) bool {
	return EdgeEnabled() && edge.IsBuyerGatewayRequest(r)
}

// StartEdge 在后台启动买家模型入口（默认 :8090），返回关闭完成信号。
func StartEdge(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	runtime, err := ensureEdge()
	if err != nil {
		slog.Error("enginehost: embed edge config failed", "err", err)
		close(done)
		return done
	}
	if runtime == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if err := runtime.Run(ctx); err != nil {
			slog.Error("enginehost: market edge exited", "err", err)
		}
	}()
	return done
}
