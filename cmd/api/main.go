// market-api：控制面 HTTP 接口（账号、市场、买家、卖家、管理后台）。
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DueGin/token-market/internal/app"
	"github.com/DueGin/token-market/internal/httpapi"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/quality"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（可选，敏感项建议用环境变量）")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*configPath, log); err != nil {
		log.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.Close()

	if cfg.Auth.BootstrapAdminEmail != "" {
		created, err := a.Accounts.EnsureBootstrapAdmin(ctx, cfg.Auth.BootstrapAdminEmail, cfg.Auth.BootstrapAdminPassword)
		if err != nil {
			return err
		}
		if created {
			log.Info("bootstrap admin created", "email", cfg.Auth.BootstrapAdminEmail)
		}
	}
	if n, err := quality.SeedDefaultItems(ctx, a.DB); err != nil {
		return err
	} else if n > 0 {
		log.Info("seeded default probe items", "count", n)
	}

	api := &httpapi.Server{
		DB: a.DB, Issuer: a.Issuer, Accounts: a.Accounts, Listings: a.Listings, Proxies: a.Proxies, Pools: a.Pools,
		Keys: a.Keys, Funds: a.Funds, Market: a.Market, Quality: a.Quality, Risk: a.Risk, Kicker: a.Kicker, Log: log,
		TrustedProxies: cfg.HTTP.TrustedProxies, WebDir: cfg.HTTP.WebDir, EdgeURL: cfg.Edge.PublicURL,
	}
	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("api listening", "addr", cfg.HTTP.Addr)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
