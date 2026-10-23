package enginehost

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/yx-token/token-market/market/internal/edgeloop"
	"github.com/yx-token/token-market/market/internal/platform/config"
)

// EdgeEnabled 宿主是否内嵌 Edge。默认随 MARKET_EMBED 开启；可设 MARKET_EMBED_EDGE=0 关闭。
func EdgeEnabled() bool {
	if !Enabled() {
		return false
	}
	v := strings.TrimSpace(os.Getenv("MARKET_EMBED_EDGE"))
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return false
	}
	return true
}

// StartEdge 在后台启动买家模型入口（默认 :8090），与宿主同生命周期。
func StartEdge(ctx context.Context) {
	if !EdgeEnabled() {
		return
	}
	go func() {
		cfg, err := config.Load("")
		if err != nil {
			slog.Error("enginehost: embed edge config failed", "err", err)
			return
		}
		if cfg.Edge.Addr == "" {
			cfg.Edge.Addr = ":8090"
		}
		// 内嵌时不另开 edge admin 端口；网关走本机宿主（避免经 docker 主机名绕一圈）。
		cfg.Edge.AdminAddr = ""
		cfg.Sub2API.GatewayURL = "http://127.0.0.1:8080"
		slog.Info("enginehost: market edge starting", "addr", cfg.Edge.Addr, "gateway", cfg.Sub2API.GatewayURL)
		if err := edgeloop.Run(ctx, cfg, slog.Default()); err != nil {
			slog.Error("enginehost: market edge exited", "err", err)
		}
	}()
}
