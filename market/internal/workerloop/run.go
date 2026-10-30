// Package workerloop 市场后台任务循环：可被独立 market-worker 或宿主进程（MARKET_EMBED）调用。
package workerloop

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yx-token/token-market/market/contract"
	"github.com/yx-token/token-market/market/internal/app"
	"github.com/yx-token/token-market/market/internal/billing"
	"github.com/yx-token/token-market/market/internal/platform/buildinfo"
	"github.com/yx-token/token-market/market/internal/platform/config"
	"github.com/yx-token/token-market/market/internal/sub2api/compatguard"
	"github.com/yx-token/token-market/market/internal/sub2api/ops"
	"github.com/yx-token/token-market/market/internal/sub2api/opshandlers"
	"github.com/yx-token/token-market/market/internal/support"
)

// Run 在持有领导锁后跑兼容守卫、ops 发送、调度、入账与周期 job，直到 ctx 取消。
// 不关闭 a；由调用方负责 App 生命周期。
func Run(ctx context.Context, a *app.App, cfg *config.Config, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	go serveAdmin(ctx, cfg.Worker.AdminAddr, a.DB, log)

	lost, release, err := acquireLeader(ctx, a.DB, log)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer release()
	isLeader.Store(true)
	defer isLeader.Store(false)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-lost:
			log.Error("worker leadership lost, stopping")
			cancel()
		case <-ctx.Done():
		}
	}()

	spec, err := contract.Load()
	if err != nil {
		return err
	}
	guard := compatguard.New(spec, a.Admin, a.Replica, a.DB, log)
	sender := ops.NewSender(a.DB, ops.SenderConfig{
		BatchSize: cfg.Worker.OpsBatchSize,
		Retryable: opshandlers.Retryable,
		Gate:      guard.AllowWrites,
		Logger:    log,
	})
	opshandlers.Register(sender, a.Admin)
	if _, err := guard.Check(ctx); err != nil {
		log.Warn("initial compat check failed", "err", err)
	}

	writes := func(fn func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) error {
			if ok, _ := guard.AllowWrites(ctx); !ok {
				return nil
			}
			return fn(ctx)
		}
	}
	a.Ingester.Gate = guard.AllowWrites
	a.Quality.Gate = guard.AllowWrites
	jobs := []job{
		{"funds.track_crediting", 10 * time.Second, a.Funds.TrackCrediting},
		{"funds.advance_refunds", 10 * time.Second, a.Funds.AdvanceRefunds},
		{"funds.sync_payments", 15 * time.Second, a.Funds.SyncPayments},
		{"funds.refund_payouts", 15 * time.Second, a.Funds.AdvanceRefundPayouts},
		{"packs.fulfill_checkouts", 10 * time.Second, a.Keys.FulfillPackCheckouts},
		{"credits.provision", 5 * time.Second, writes(a.Keys.ProvisionCreditPacks)},
		{"credits.refunds", 10 * time.Second, writes(a.Funds.AdvanceCreditPacks)},
		{"keys.provision_pending", 15 * time.Second, writes(a.Keys.ProvisionPending)},
		{"keys.publish_skip_list", 15 * time.Second, a.Keys.PublishSkipList},
		{"keys.publish_all", 10 * time.Minute, a.Keys.PublishAll},
		{"keys.cleanup_deleted", 5 * time.Minute, writes(func(ctx context.Context) error {
			_, err := a.Keys.CleanupDeletedKeys(ctx)
			return err
		})},
		{"keys.limits", time.Minute, a.Limits.RunOnce},
		{"quality.tick", cfg.Worker.QualityInterval, writes(a.Quality.Tick)},
		{"billing.unfreeze", time.Minute, func(ctx context.Context) error {
			st, err := a.Settings(ctx)
			if err != nil {
				return err
			}
			n, err := billing.UnfreezeHours(ctx, a.DB, st.FreezeHours, time.Now())
			if n > 0 {
				log.Info("seller earnings unfrozen", "sellers", n)
			}
			return err
		}},
		{"reconcile.attempts", cfg.Worker.ReconcileInterval, writes(func(ctx context.Context) error {
			res, err := a.Reconciler.RunAttempts(ctx)
			if res != nil && (res.R1+res.R2+res.R3+res.R4 > 0 || res.FrozenKeys > 0) {
				log.Warn("attempt reconciliation found anomalies", "r1", res.R1, "r2", res.R2, "r3", res.R3, "r4", res.R4, "frozen_keys", res.FrozenKeys)
			}
			return err
		})},
		{"reconcile.balances", cfg.Worker.BalanceReconcileInterval, writes(func(ctx context.Context) error {
			n, err := a.Reconciler.RunBalances(ctx)
			if n > 0 {
				log.Warn("balance reconciliation mismatches", "count", n)
			}
			return err
		})},
		{"risk.detect", 15 * time.Minute, func(ctx context.Context) error {
			n, err := a.Risk.Detect(ctx)
			if n > 0 {
				log.Warn("risk events detected", "count", n)
			}
			return err
		}},
		{"support.close_idle", time.Minute, func(ctx context.Context) error {
			n, err := (&support.Service{DB: a.DB}).CloseIdle(ctx)
			if n > 0 {
				log.Info("support conversations auto-closed", "count", n)
			}
			return err
		}},
	}

	var wg sync.WaitGroup
	spawn := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	spawn(func() { guard.Run(ctx, cfg.Worker.CompatGuardInterval) })
	spawn(func() { sender.Run(ctx, cfg.Worker.OpsPollInterval) })
	spawn(func() { a.Scheduler.Run(ctx, cfg.Worker.SchedulerInterval) })
	spawn(func() { a.Ingester.Run(ctx, cfg.Worker.IngestInterval) })
	spawn(func() { a.Quality.RunBatches(ctx, cfg.Worker.QualityInterval) })
	for _, j := range jobs {
		spawn(func() { j.loop(ctx, log) })
	}
	log.Info("worker started", "jobs", len(jobs)+5, "version", buildinfo.Version)
	wg.Wait()
	log.Info("worker stopped")
	return nil
}

type job struct {
	name  string
	every time.Duration
	fn    func(context.Context) error
}

func (j job) loop(ctx context.Context, log *slog.Logger) {
	for {
		j.once(ctx, log)
		select {
		case <-ctx.Done():
			return
		case <-time.After(j.every):
		}
	}
}

func (j job) once(ctx context.Context, log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			recordJob(j.name, errors.New("panic"))
			log.Error("job panicked", "job", j.name, "panic", r)
		}
	}()
	timeout := j.every * 4
	if timeout < time.Minute {
		timeout = time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	err := j.fn(runCtx)
	if ctx.Err() != nil {
		return
	}
	recordJob(j.name, err)
	if err != nil {
		log.Error("job failed", "job", j.name, "err", err, "elapsed", time.Since(start).String())
	}
}

const leaderLockKey = 0x746d776b // "tmwk"

func acquireLeader(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) (<-chan struct{}, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	waiting := false
	for {
		var got bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leaderLockKey).Scan(&got); err != nil {
			conn.Release()
			return nil, nil, err
		}
		if got {
			break
		}
		if !waiting {
			log.Info("another worker holds leadership, standing by")
			waiting = true
		}
		select {
		case <-ctx.Done():
			conn.Release()
			return nil, nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	log.Info("worker leadership acquired")
	lost := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := conn.Ping(pingCtx)
				cancel()
				mu.Unlock()
				if err != nil {
					close(lost)
					return
				}
			}
		}
	}()
	release := func() {
		close(done)
		mu.Lock()
		defer mu.Unlock()
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", leaderLockKey)
		conn.Release()
	}
	return lost, release, nil
}
