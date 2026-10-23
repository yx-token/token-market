package enginehost

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/yx-token/token-market/market/internal/httpapi"
	"github.com/yx-token/token-market/market/internal/platform/config"
)

var (
	handlerOnce sync.Once
	publicH     http.Handler
	publicErr   error
)

// Enabled 宿主是否内嵌市场控制面（C 端 API + 静态）。MARKET_EMBED=1 且能加载市场配置时开启。
func Enabled() bool {
	v := strings.TrimSpace(os.Getenv("MARKET_EMBED"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// PublicHandler 返回挂进宿主的市场 Handler（无 /api/v1 反代、无市场 /admin SPA）。
// 未启用或构建失败时返回 nil。
func PublicHandler() http.Handler {
	if !Enabled() {
		return nil
	}
	handlerOnce.Do(func() {
		m, err := market(context.Background())
		if err != nil {
			publicErr = err
			slog.Error("enginehost: embed market app failed", "err", err)
			return
		}
		cfg, err := config.Load("")
		if err != nil {
			publicErr = err
			slog.Error("enginehost: embed config failed", "err", err)
			return
		}
		api := &httpapi.Server{
			DB: m.DB, Issuer: m.Issuer, Accounts: m.Accounts, IAM: m.IAM, Listings: m.Listings,
			Proxies: m.Proxies, Pools: m.Pools, Keys: m.Keys, Funds: m.Funds, Payments: m.Payments,
			Market: m.Market, Quality: m.Quality, Replica: m.Replica, Risk: m.Risk, Kicker: m.Kicker,
			Log: slog.Default(), TrustedProxies: cfg.HTTP.TrustedProxies, WebDir: cfg.HTTP.WebDir,
			EdgeURL: cfg.Edge.PublicURL,
		}
		publicH = api.HandlerEmbed()
		slog.Info("enginehost: market public handler embedded", "web_dir", cfg.HTTP.WebDir)
	})
	if publicErr != nil {
		return nil
	}
	return publicH
}
