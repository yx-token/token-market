// Package edgeloop 市场 Edge 服务循环：可被独立 market-edge 或宿主进程（MARKET_EMBED）调用。
package edgeloop

import (
	"context"
	"errors"
	"expvar"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yx-token/token-market/market/internal/edge"
	"github.com/yx-token/token-market/market/internal/edgecfg"
	"github.com/yx-token/token-market/market/internal/platform/buildinfo"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/platform/crypto"
	"github.com/yx-token/token-market/market/internal/platform/db"
)

// Run 启动 Edge 公网口与可选管理口，直到 ctx 取消。
func Run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Sub2API.GatewayURL == "" || cfg.Redis.Addr == "" {
		return errors.New("sub2api.gateway_url (or base_url) and redis.addr are required")
	}
	gateway, err := url.Parse(cfg.Sub2API.GatewayURL)
	if err != nil {
		return err
	}
	kr, err := crypto.NewKeyring(cfg.Crypto)
	if err != nil {
		return err
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer func() { _ = rdb.Close() }()
	cache := edge.NewCache(edgecfg.Store{Redis: rdb}, log)
	for {
		if err := cache.Load(ctx); err == nil {
			break
		} else {
			log.Warn("edge waiting for redis", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
	go cache.Watch(ctx, 5*time.Minute)

	var attempts *edge.AttemptLogger
	var credits edge.CreditSelector
	if cfg.Database.DSN != "" {
		pool, err := db.Open(ctx, cfg.Database.DSN, cfg.Database.MaxConns, cfg.Database.TimeZone)
		if err != nil {
			log.Warn("edge database unavailable, attempts will be spooled", "err", err)
		}
		attempts = edge.NewAttemptLogger(pool, cfg.Edge.SpoolDir, log)
		credits = &edge.DBCreditSelector{DB: pool, Keyring: kr}
		if pool != nil {
			defer pool.Close()
		}
	} else {
		attempts = edge.NewAttemptLogger(nil, cfg.Edge.SpoolDir, log)
	}
	attemptCtx, stopAttempts := context.WithCancel(context.Background())
	defer stopAttempts()
	go attempts.Run(attemptCtx)

	srv := edge.NewServer(edge.Config{
		GatewayURL:       gateway,
		ConnectTimeout:   cfg.Edge.ConnectTimeout,
		FirstByteTimeout: cfg.Edge.FirstByteTimeout,
		MaxBodyBytes:     cfg.Edge.MaxBodyBytes,
		BreakerFailures:  cfg.Edge.BreakerFailures,
		BreakerCooldown:  cfg.Edge.BreakerCooldown,
		TrustedProxies:   cfg.Edge.TrustedProxies,
		InvalidKeyLimit:  cfg.Edge.InvalidKeyLimit,
	}, cache, kr, attempts, log)
	srv.SetCreditSelector(credits)

	public := &http.Server{Addr: cfg.Edge.Addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- public.ListenAndServe() }()

	var admin *http.Server
	if cfg.Edge.AdminAddr != "" {
		adminMux := http.NewServeMux()
		adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
			if !cache.Ready() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		})
		adminMux.Handle("GET /debug/vars", expvar.Handler())
		admin = &http.Server{Addr: cfg.Edge.AdminAddr, Handler: adminMux, ReadHeaderTimeout: 10 * time.Second}
		go func() { errCh <- admin.ListenAndServe() }()
	}

	log.Info("edge listening", "version", buildinfo.Version, "addr", cfg.Edge.Addr, "admin", cfg.Edge.AdminAddr, "gateway", gateway.String())

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		stopAttempts()
		attempts.Wait()
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = public.Shutdown(shutdownCtx)
	if admin != nil {
		_ = admin.Shutdown(shutdownCtx)
	}
	stopAttempts()
	attempts.Wait()
	return nil
}
