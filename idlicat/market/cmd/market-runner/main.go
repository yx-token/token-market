// market-runner 是站内更新的稳定启动器，随镜像发布，不随应用热更新替换。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yx-token/idlicat/market/internal/platform/config"
	"github.com/yx-token/idlicat/market/internal/update"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("runner exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	component := "api"
	args := os.Args[1:]
	if len(args) > 0 {
		component, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("market-runner", flag.ContinueOnError)
	configPath := flags.String("config", "", "应用配置文件")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	addr := cfg.HTTP.Addr
	switch component {
	case "api":
	case "worker":
		addr = cfg.Worker.AdminAddr
	case "edge":
		addr = cfg.Edge.AdminAddr
	default:
		return errors.New("组件应为 api、worker 或 edge")
	}
	root, err := filepath.Abs(cfg.Update.RuntimeDir)
	if err != nil {
		return err
	}
	r := update.Runtime{Root: root}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	imageDir := filepath.Dir(filepath.Dir(executable))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		err := r.Seed(imageDir)
		if !errors.Is(err, update.ErrBusy) {
			if err != nil {
				return err
			}
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
	return r.Run(ctx, component, addr, args, log)
}
