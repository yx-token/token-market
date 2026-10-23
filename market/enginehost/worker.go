package enginehost

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/workerloop"
)

// WorkerEnabled 宿主是否内嵌市场 worker。默认随 MARKET_EMBED 开启；可设 MARKET_EMBED_WORKER=0 关闭。
func WorkerEnabled() bool {
	if !Enabled() {
		return false
	}
	v := strings.TrimSpace(os.Getenv("MARKET_EMBED_WORKER"))
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return false
	}
	return true
}

// StartWorker 在后台启动市场 worker 循环（与宿主同生命周期）。失败只打日志，不阻塞宿主 HTTP。
func StartWorker(ctx context.Context) {
	if !WorkerEnabled() {
		return
	}
	go func() {
		m, err := market(ctx)
		if err != nil {
			slog.Error("enginehost: embed worker unavailable", "err", err)
			return
		}
		cfg, err := config.Load("")
		if err != nil {
			slog.Error("enginehost: embed worker config failed", "err", err)
			return
		}
		// 内嵌时不另开 worker admin 端口，避免与宿主冲突；独立进程仍用配置地址。
		cfg.Worker.AdminAddr = ""
		slog.Info("enginehost: market worker starting")
		if err := workerloop.Run(ctx, m, cfg, slog.Default()); err != nil {
			slog.Error("enginehost: market worker exited", "err", err)
		}
	}()
}
