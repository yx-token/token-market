// market-edge：买家 API 入口，按买家 Key 的卖家链把请求转发到 sub2api 并故障转移。
package main

import (
	"context"
	"errors"
	"expvar"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DueGin/token-market/internal/edge"
	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/redis/go-redis/v9"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（可选，敏感项建议用环境变量）")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*configPath, log); err != nil {
		log.Error("edge exited", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	// 尝试日志只在后台写库，数据库不可用时写本地暂存文件，不影响转发
	var attempts *edge.AttemptLogger
	if cfg.Database.DSN != "" {
		pool, err := db.Open(ctx, cfg.Database.DSN, 4)
		if err != nil {
			log.Warn("edge database unavailable, attempts will be spooled", "err", err)
		}
		attempts = edge.NewAttemptLogger(pool, cfg.Edge.SpoolDir, log)
		if pool != nil {
			defer pool.Close()
		}
	} else {
		attempts = edge.NewAttemptLogger(nil, cfg.Edge.SpoolDir, log)
	}
	attemptCtx, stopAttempts := context.WithCancel(context.Background())
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

	public := &http.Server{Addr: cfg.Edge.Addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !cache.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	adminMux.Handle("GET /debug/vars", expvar.Handler())
	admin := &http.Server{Addr: cfg.Edge.AdminAddr, Handler: adminMux, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 2)
	go func() { errCh <- public.ListenAndServe() }()
	go func() { errCh <- admin.ListenAndServe() }()
	log.Info("edge listening", "addr", cfg.Edge.Addr, "admin", cfg.Edge.AdminAddr, "gateway", gateway.String())

	select {
	case err := <-errCh:
		stopAttempts()
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = public.Shutdown(shutdownCtx)
	_ = admin.Shutdown(shutdownCtx)
	stopAttempts()
	attempts.Wait()
	return nil
}
