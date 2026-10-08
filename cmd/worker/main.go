// market-worker：后台任务进程。M0 包含：本系统数据库迁移、sub2api 兼容自检、sub2api 写操作出站发送器。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/DueGin/token-market/contract"
	"github.com/DueGin/token-market/internal/platform/config"
	"github.com/DueGin/token-market/internal/platform/db"
	"github.com/DueGin/token-market/internal/sub2api/adminclient"
	"github.com/DueGin/token-market/internal/sub2api/compatguard"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/DueGin/token-market/migrations"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（可选，敏感项建议用环境变量）")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(*configPath, log); err != nil {
		log.Error("worker exited", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	switch {
	case cfg.Database.DSN == "":
		return errors.New("database.dsn is required")
	case cfg.Sub2API.BaseURL == "" || cfg.Sub2API.AdminAPIKey == "":
		return errors.New("sub2api.base_url and sub2api.admin_api_key are required")
	case cfg.Sub2API.ReplicaDSN == "":
		return errors.New("sub2api.replica_dsn is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.Database.DSN, cfg.Database.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("applied market migrations", "files", applied)
	}

	replicaPool, err := db.Open(ctx, cfg.Sub2API.ReplicaDSN, 5)
	if err != nil {
		return err
	}
	defer replicaPool.Close()
	reader, err := replica.New(replicaPool)
	if err != nil {
		return err
	}

	spec, err := contract.Load()
	if err != nil {
		return err
	}
	admin := adminclient.New(cfg.Sub2API.BaseURL, cfg.Sub2API.AdminAPIKey, cfg.Sub2API.Timeout)
	guard := compatguard.New(spec, admin, reader, pool, log)

	sender := ops.NewSender(pool, ops.SenderConfig{
		BatchSize: cfg.Worker.OpsBatchSize,
		Retryable: opshandlers.Retryable,
		Gate:      guard.AllowWrites,
		Logger:    log,
	})
	opshandlers.Register(sender, admin)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); guard.Run(ctx, cfg.Worker.CompatGuardInterval) }()
	go func() { defer wg.Done(); sender.Run(ctx, cfg.Worker.OpsPollInterval) }()
	log.Info("worker started")
	wg.Wait()
	log.Info("worker stopped")
	return nil
}
