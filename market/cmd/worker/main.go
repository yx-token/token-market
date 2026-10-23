// market-worker：后台任务进程。
//
// 包含：sub2api 兼容自检与写操作出站发送、调度规划与执行、用量入账与结算解冻、
// 充值/退款推进（支付查单、关单、原路退款）、对账、Key 限额、质量检测（含第三方检测）、买家 Key 供给与 Edge 配置发布、风控检测。
//
// 多副本部署时通过 PostgreSQL advisory lock 选主，只有持锁的副本运行任务。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yx-token/token-market/market/internal/app"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/workerloop"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.Close()

	return workerloop.Run(ctx, a, cfg, log)
}
