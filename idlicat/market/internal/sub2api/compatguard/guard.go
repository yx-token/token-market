// Package compatguard 是运行时兼容自检：定期确认正在运行的 sub2api 版本与迁移集合
// 已在契约中验证过；否则进入安全模式，停止一切对 sub2api 的写操作。
package compatguard

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/yx-token/idlicat/market/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	StateNormal   = "normal"
	StateSafeMode = "safe_mode"
)

type VersionSource interface {
	GetVersion(ctx context.Context) (string, error)
}

type MigrationSource interface {
	AppliedMigrationNames(ctx context.Context) ([]string, error)
}

type Store interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Result struct {
	State       string
	Version     string
	Fingerprint string
	Reason      string
}

type Guard struct {
	spec       *contract.Spec
	versions   VersionSource
	migrations MigrationSource
	store      Store
	log        *slog.Logger
	state      atomic.Value // string
}

func New(spec *contract.Spec, versions VersionSource, migrations MigrationSource, store Store, log *slog.Logger) *Guard {
	if log == nil {
		log = slog.Default()
	}
	g := &Guard{spec: spec, versions: versions, migrations: migrations, store: store, log: log}
	// 启动时在第一次检查完成前保持安全模式，避免对未知版本的 sub2api 写入
	g.state.Store(StateSafeMode)
	return g
}

// Evaluate 纯判定：版本必须在已验证列表中，且迁移指纹是该版本接受的指纹之一。
func Evaluate(spec *contract.Spec, version string, migrations []string) Result {
	fp := contract.MigrationFingerprint(migrations)
	r := Result{State: StateSafeMode, Version: version, Fingerprint: fp}
	v, ok := spec.Verified(version)
	if !ok {
		r.Reason = fmt.Sprintf("sub2api version %q is not in contract verified_versions", version)
		return r
	}
	for _, accepted := range v.MigrationFingerprints {
		if accepted == fp {
			r.State = StateNormal
			return r
		}
	}
	r.Reason = fmt.Sprintf("migration fingerprint %s (%d migrations) is not accepted for version %s", fp, len(migrations), version)
	return r
}

// Check 读取版本与迁移集合并更新状态。读取失败时保留原状态并返回错误：
// sub2api 不可达时写操作本来就会失败，没必要切换状态。
func (g *Guard) Check(ctx context.Context) (Result, error) {
	version, err := g.versions.GetVersion(ctx)
	if err != nil {
		return Result{State: g.State()}, fmt.Errorf("compat guard: read version: %w", err)
	}
	names, err := g.migrations.AppliedMigrationNames(ctx)
	if err != nil {
		return Result{State: g.State()}, fmt.Errorf("compat guard: read migrations: %w", err)
	}
	r := Evaluate(g.spec, version, names)
	prev := g.State()
	g.state.Store(r.State)
	if prev != r.State {
		if r.State == StateSafeMode {
			g.log.Error("entering safe mode: writes to sub2api paused", "version", r.Version, "reason", r.Reason)
		} else {
			g.log.Info("sub2api compatibility verified, writes resumed", "version", r.Version)
		}
	}
	if g.store != nil {
		var reason *string
		if r.Reason != "" {
			reason = &r.Reason
		}
		if _, err := g.store.Exec(ctx, `
			UPDATE compat_state
			SET sub2api_version = $1, migration_fingerprint = $2, guard_state = $3, reason = $4, checked_at = now()
			WHERE id = 1`, r.Version, r.Fingerprint, r.State, reason); err != nil {
			return r, fmt.Errorf("compat guard: save state: %w", err)
		}
	}
	return r, nil
}

func (g *Guard) State() string {
	s, _ := g.state.Load().(string)
	return s
}

// AllowWrites 可直接作为 ops.Sender 的 Gate。
func (g *Guard) AllowWrites(context.Context) (bool, string) {
	if s := g.State(); s != StateNormal {
		return false, "compat guard state is " + s
	}
	return true, ""
}

// Run 按间隔检查，直到 ctx 结束。
func (g *Guard) Run(ctx context.Context, interval time.Duration) {
	for {
		if _, err := g.Check(ctx); err != nil && ctx.Err() == nil {
			g.log.Warn("compat guard check failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
