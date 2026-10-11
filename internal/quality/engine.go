package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
	"github.com/DueGin/token-market/internal/edgecfg"
	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/listing"
	"github.com/DueGin/token-market/internal/platform/crypto"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/DueGin/token-market/internal/platform/randx"
	"github.com/DueGin/token-market/internal/settings"
	"github.com/DueGin/token-market/internal/sub2api/ops"
	"github.com/DueGin/token-market/internal/sub2api/opshandlers"
	"github.com/DueGin/token-market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Engine 调度并执行探测、判定、处置。实现 listing.QualityHook。
type Engine struct {
	DB       *pgxpool.Pool
	Replica  *replica.Reader
	Shadow   *buyer.ShadowService
	Keyring  *crypto.Keyring
	Prober   *Prober
	Listings *listing.Service
	Kicker   listing.Kicker
	Settings func(ctx context.Context) (settings.Settings, error)
	Log      *slog.Logger
	// RegisterInstance 仅供离线替身登记答案；生产环境不设置。
	RegisterInstance func(ProbeRegistration)
	Concurrency      int // 同时执行的检测批次上限
	// Gate 返回 false 时 RunBatches 不派发新批次（兼容自检未通过的安全模式）
	Gate func(ctx context.Context) (bool, string)
	Now  func() time.Time
	// Wait 供离线测试替换等待；生产默认使用支持取消的真实计时器。
	Wait func(context.Context, time.Duration) error
	// 第三方检测：Edge 配置存储（发布临时令牌）、Edge 对外地址（交给检测站）、访问检测站的 HTTP 客户端
	Edge           edgecfg.Store
	EdgePublicURL  string
	ThirdPartyHTTP *http.Client

	batches         sync.WaitGroup      // 后台执行中的批次
	mu              sync.Mutex          // 保护 running、wake
	running         map[string]struct{} // 本进程正在执行的批次
	requestRunning  map[int64]int
	requestTotal    int
	requestWake     chan struct{}
	recoveryRunning map[int64]bool
	wake            chan struct{} // 批次结束时通知 RunBatches 立即补位
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Wait != nil {
		return e.Wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) kick(ctx context.Context) {
	if e.Kicker != nil {
		e.Kicker.Kick(ctx)
	}
}

// StartOnboardingCheck 实现 listing.QualityHook：排队上架检测。首次配置必须检测，不受厂商检测开关影响。
func (e *Engine) StartOnboardingCheck(ctx context.Context, upstreamID int64, _, _ string) error {
	_, err := e.QueueRun(ctx, upstreamID, "onboarding")
	return err
}

type upstreamRow struct {
	accountID    *int64
	id           int64
	platform     string
	authType     string
	status       string
	health       string
	healthReason string
	isReference  bool
	probeGroupID *int64
	proxyID      *int64
	tier         *string
	privacyOK    *bool
}

func (e *Engine) loadUpstream(ctx context.Context, id int64) (*upstreamRow, error) {
	var u upstreamRow
	var reason *string
	err := e.DB.QueryRow(ctx, `SELECT id, platform, auth_type, status, health, health_reason, is_reference, probe_group_id, proxy_id, tier, privacy_ok, sub2api_account_id
		FROM upstreams WHERE id = $1`, id).Scan(&u.id, &u.platform, &u.authType, &u.status, &u.health, &reason, &u.isReference,
		&u.probeGroupID, &u.proxyID, &u.tier, &u.privacyOK, &u.accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, listing.ErrUpstreamNotFound
	}
	if reason != nil {
		u.healthReason = *reason
	}
	return &u, err
}

// QueueRun 为上游排队一次检测，并在同一批次中为同平台的基线账号排队一次对照检测。
func (e *Engine) QueueRun(ctx context.Context, upstreamID int64, trigger string) (int64, error) {
	u, err := e.loadUpstream(ctx, upstreamID)
	if err != nil {
		return 0, err
	}
	p, err := EffectivePolicy(ctx, e.DB, u.id, u.platform, u.authType)
	if err != nil {
		return 0, err
	}
	if len(p.Models) == 0 {
		return 0, httpx.BadRequest("NO_MODELS", "detection policy has no models configured")
	}
	var pending int
	if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND status IN ('queued', 'running', 'third_party')`, u.id).Scan(&pending); err != nil {
		return 0, err
	}
	if pending > 0 && trigger != "appeal" && trigger != "admin" && trigger != "onboarding" {
		return 0, httpx.Conflict("RUN_PENDING", "a check is already queued for this upstream")
	}
	// 多个模型时按检测次数轮换
	var runs int
	if err := e.DB.QueryRow(ctx, "SELECT count(*) FROM probe_runs WHERE upstream_id = $1", u.id).Scan(&runs); err != nil {
		return 0, err
	}
	model := p.Models[runs%len(p.Models)]
	inspectionConfig, err := loadInspectionSettings(ctx, e.DB)
	if err != nil {
		return 0, err
	}
	// 新题尚未同步或全部下线时仍走旧流程；有新题的试运行也计算完整/快检。
	mode := "legacy"
	var newItems bool
	if err := e.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM probe_items WHERE status='active' AND suite LIKE 'inspection_%')`).Scan(&newItems); err != nil {
		return 0, err
	}
	if newItems || inspectionConfig.Mode == "formal" {
		mode = "quick"
	}
	// 新套件启用后使用完整/快检；旧题继续按既有超时和取消行为运行。
	for _, fullTrigger := range p.FullTriggers {
		if mode != "legacy" && fullTrigger == trigger {
			mode = "full"
			break
		}
	}
	if mode != "legacy" && (trigger == "onboarding" || trigger == "appeal" || trigger == "admin") {
		mode = "full"
	}
	if mode != "legacy" && trigger == "recovery" {
		var failed []string
		if err := e.DB.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&failed); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		if len(failed) > 0 {
			mode = "full"
		}
	}
	batch := fmt.Sprintf("b%d-%d", u.id, e.now().UnixNano())
	var id int64
	if err := e.DB.QueryRow(ctx, `INSERT INTO probe_runs (upstream_id, batch_id, trigger, policy_version, model, mode) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		u.id, batch, trigger, p.Version, model, mode).Scan(&id); err != nil {
		return 0, err
	}
	if !u.isReference {
		var refID int64
		selectionMode := mode
		if inspectionConfig.Mode == "trial" {
			selectionMode = "legacy"
		}
		err := e.DB.QueryRow(ctx, `SELECT u.id FROM upstreams u WHERE is_reference AND platform=$1 AND status IN ('ready','listed')
 AND health <> 'unavailable' AND sub2api_account_id IS NOT NULL
 AND ($2='legacy' OR (health='healthy' AND auth_type=$3 AND tier IS NOT DISTINCT FROM $4::text))
 ORDER BY (SELECT count(*) FROM probe_runs r WHERE r.upstream_id=u.id AND r.status IN ('queued','running')),
 (SELECT max(queued_at) FROM probe_runs r WHERE r.upstream_id=u.id) NULLS FIRST, u.id LIMIT 1`, u.platform, selectionMode, u.authType, u.tier).Scan(&refID)
		if err == nil {
			if _, err := e.DB.Exec(ctx, `INSERT INTO probe_runs (upstream_id, batch_id, trigger, policy_version, model, mode) VALUES ($1, $2, 'reference', $3, $4, $5)`,
				refID, batch, p.Version, model, mode); err != nil {
				return 0, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	return id, nil
}

// Tick 是一轮质量调度：排队到期的检测、推进第三方检测、被动分析、回收续报价、补足探测余额。
// 排队中的批次由 RunBatches 在后台执行，不占用这里的单轮时限。
func (e *Engine) Tick(ctx context.Context) error {
	if err := e.scheduleDue(ctx); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if err := e.advanceThirdParty(ctx); err != nil {
		e.Log.Warn("third-party detection", "err", err)
	}
	if err := e.Passive(ctx); err != nil {
		e.Log.Warn("passive analysis", "err", err)
	}
	if err := e.requoteExpired(ctx); err != nil {
		e.Log.Warn("requote", "err", err)
	}
	if err := e.ensureProbeBalance(ctx); err != nil {
		e.Log.Warn("probe balance", "err", err)
	}
	return nil
}

func (e *Engine) scheduleDue(ctx context.Context) error {
	rows, err := e.DB.Query(ctx, `SELECT u.id, u.platform, u.auth_type, u.health, u.health_reason,
			q.next_scheduled_at, q.next_recovery_at, q.last_run_at
		FROM upstreams u LEFT JOIN upstream_quality_state q ON q.upstream_id = u.id
		WHERE u.status IN ('ready', 'listed') AND u.sub2api_account_id IS NOT NULL AND NOT u.is_reference`)
	if err != nil {
		return err
	}
	type item struct {
		id                         int64
		platform, authType, health string
		reason                     *string
		nextSched, nextRec, last   *time.Time
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.platform, &it.authType, &it.health, &it.reason, &it.nextSched, &it.nextRec, &it.last); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	now := e.now()
	for _, it := range items {
		p, err := EffectivePolicy(ctx, e.DB, it.id, it.platform, it.authType)
		if err != nil {
			return err
		}
		if !p.Enabled {
			continue
		}
		trigger := ""
		probeCaused := it.health == listing.HealthDegraded || (it.health == listing.HealthUnavailable && it.reason != nil && strings.HasPrefix(*it.reason, "probe:"))
		switch {
		case probeCaused && it.nextRec != nil && !now.Before(*it.nextRec):
			trigger = "recovery"
		case it.health == listing.HealthSuspected && (it.last == nil || now.Sub(*it.last) > 30*time.Minute):
			trigger = "adaptive"
		case p.ScheduledEnabled && (it.nextSched == nil || !now.Before(*it.nextSched)):
			trigger = "scheduled"
		}
		if trigger == "" {
			continue
		}
		if _, err := e.QueueRun(ctx, it.id, trigger); err != nil {
			var he *httpx.Error
			if errors.As(err, &he) {
				continue
			}
			return err
		}
		next := now.Add(time.Duration(p.IntervalMinutes) * time.Minute)
		if p.JitterMinutes > 0 {
			next = next.Add(time.Duration(rand.IntN(2*p.JitterMinutes+1)-p.JitterMinutes) * time.Minute) //nolint:gosec // 调度抖动
		}
		if _, err := e.DB.Exec(ctx, `INSERT INTO upstream_quality_state (upstream_id, next_scheduled_at) VALUES ($1, $2)
			ON CONFLICT (upstream_id) DO UPDATE SET next_scheduled_at = CASE WHEN $3 = 'scheduled' OR upstream_quality_state.next_scheduled_at IS NULL
				THEN EXCLUDED.next_scheduled_at ELSE upstream_quality_state.next_scheduled_at END,
				next_recovery_at = CASE WHEN $3 = 'recovery' THEN NULL ELSE upstream_quality_state.next_recovery_at END`,
			it.id, next, trigger); err != nil {
			return err
		}
	}
	return nil
}

// probeLimits 是检测的超时设置，取自系统设置，修改后对之后开始的批次生效。
type probeLimits struct {
	request time.Duration // 单次请求
	batch   time.Duration // 整批
}

func limitsOf(st settings.Settings) probeLimits {
	return probeLimits{
		request: time.Duration(st.ProbeRequestTimeoutSeconds) * time.Second,
		batch:   time.Duration(st.ProbeBatchTimeoutMinutes) * time.Minute,
	}
}

// orphanGrace 是批次超过总时限后的宽限时间，过后仍处于执行中且不在本进程执行的批次视为已中断。
const orphanGrace = 5 * time.Minute

// RunBatches 循环派发排队中的检测批次，直到 ctx 结束；退出前等待进行中的批次收尾。
// 批次在后台执行，慢批次不会拖住排队、第三方检测推进等其他步骤，也不受质量调度单轮时限的限制。
func (e *Engine) RunBatches(ctx context.Context, interval time.Duration) {
	defer e.WaitBatches()
	wake := e.wakeup()
	for {
		allowed := true
		if e.Gate != nil {
			allowed, _ = e.Gate(ctx)
		}
		if allowed {
			if err := e.DispatchQueued(ctx); err != nil && ctx.Err() == nil {
				e.Log.Error("dispatch probe batches", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		case <-wake:
		}
	}
}

// WaitBatches 等待已派发的批次全部结束。
func (e *Engine) WaitBatches() {
	e.batches.Wait()
}

// DispatchQueued 回收已中断的批次，再把排队中的批次交给后台执行（同时最多 Concurrency 个），不等待批次结束。
// 批次在 ctx 取消时中断并重新排队，调用方需保证 ctx 覆盖批次的执行时间。
func (e *Engine) DispatchQueued(ctx context.Context) error {
	st, err := e.Settings(ctx)
	if err != nil {
		return err
	}
	lim := limitsOf(st)
	if err := e.failOrphaned(ctx, lim); err != nil {
		return fmt.Errorf("orphaned batches: %w", err)
	}
	conc := e.Concurrency
	if conc <= 0 {
		conc = 4
	}
	e.mu.Lock()
	full := len(e.running) >= conc
	e.mu.Unlock()
	if full {
		return nil
	}
	rows, err := e.DB.Query(ctx, `SELECT batch_id FROM probe_runs WHERE status = 'queued' GROUP BY batch_id ORDER BY min(queued_at) LIMIT 50`)
	if err != nil {
		return err
	}
	batches, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(batches) == 0 {
		return err
	}
	// 探测用户余额不足时请求会被 sub2api 拒绝（403），会被误判为不可用；先补足余额，批次留到下一轮
	if funded, err := e.probeFunded(ctx); err != nil {
		return fmt.Errorf("probe funding: %w", err)
	} else if !funded {
		e.Log.Info("probe user balance is being topped up, probe batches deferred", "batches", len(batches))
		return nil
	}
	for _, b := range batches {
		if !e.claim(b, conc) {
			continue
		}
		e.batches.Add(1)
		go func() {
			defer e.batches.Done()
			defer e.release(b)
			if err := e.executeBatch(ctx, b, lim); err != nil {
				e.Log.Error("probe batch", "batch", b, "err", err)
			}
		}()
	}
	return nil
}

// claim 登记本进程开始执行批次；并发已满或该批次已在执行时返回 false。
func (e *Engine) claim(batch string, limit int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.running[batch]; ok || len(e.running) >= limit {
		return false
	}
	if e.running == nil {
		e.running = map[string]struct{}{}
	}
	e.running[batch] = struct{}{}
	return true
}

func (e *Engine) release(batch string) {
	e.mu.Lock()
	delete(e.running, batch)
	e.mu.Unlock()
	select {
	case e.wakeup() <- struct{}{}:
	default:
	}
}

func (e *Engine) wakeup() chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.wake == nil {
		e.wake = make(chan struct{}, 1)
	}
	return e.wake
}

// failOrphaned 把超过总时限仍处于执行中、且不在本进程执行的检测记为失败。只有持主锁的 worker 执行检测，
// 这类记录来自已经退出的进程（例如崩溃）；不处理会一直算作"检测中"，挡住该上游之后的检测。
func (e *Engine) failOrphaned(ctx context.Context, lim probeLimits) error {
	rows, err := e.DB.Query(ctx, `SELECT id, upstream_id, trigger, model, batch_id, mode, deadline_at FROM probe_runs
		WHERE status = 'running' AND ((mode <> 'legacy' AND execution_snapshot <> '{}' AND started_at < now() - interval '5 minutes') OR (deadline_at IS NOT NULL AND deadline_at < now() - interval '5 minutes') OR (deadline_at IS NULL AND started_at < now() - make_interval(secs => $1)))`, (lim.batch + orphanGrace).Seconds())
	if err != nil {
		return err
	}
	type orphan struct {
		run   runRow
		batch string
	}
	orphans, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (orphan, error) {
		var o orphan
		err := row.Scan(&o.run.id, &o.run.upstreamID, &o.run.trigger, &o.run.model, &o.batch, &o.run.mode, &o.run.deadline)
		return o, err
	})
	if err != nil {
		return err
	}
	for _, o := range orphans {
		e.mu.Lock()
		_, mine := e.running[o.batch]
		e.mu.Unlock()
		if mine {
			continue
		}
		if o.run.mode != "legacy" && o.run.deadline != nil && e.now().Before(*o.run.deadline) {
			if err := e.requeueBatch(ctx, o.batch); err != nil {
				return err
			}
			continue
		}
		e.Log.Warn("probe run orphaned, marking failed", "run_id", o.run.id, "batch", o.batch)
		if err := e.failRun(ctx, o.run, "check was interrupted before finishing"); err != nil {
			return err
		}
	}
	return nil
}

type runRow struct {
	id         int64
	upstreamID int64
	trigger    string
	model      string
	mode       string
	deadline   *time.Time
	batch      string
}

// ExecuteBatch 按当前系统设置的超时执行一个批次，见 executeBatch。
func (e *Engine) ExecuteBatch(ctx context.Context, batch string) error {
	st, err := e.Settings(ctx)
	if err != nil {
		return err
	}
	return e.executeBatch(ctx, batch, limitsOf(st))
}

// executeBatch 用预生成的同一批实例并行探测被测与基线，最后沿用既有判定与处置。
// 整批不超过 lim.batch；超时或出错时记为检测失败。ctx 被取消时重新排队；legacy 保持旧重跑行为，full/quick 保留已完成单元。
func (e *Engine) executeBatch(ctx context.Context, batch string, lim probeLimits) error {
	rows, err := e.DB.Query(ctx, `UPDATE probe_runs SET status = 'running', started_at = COALESCE(started_at, now())
		WHERE batch_id = $1 AND status = 'queued' RETURNING id, upstream_id, trigger, model, mode, deadline_at, batch_id`, batch)
	if err != nil {
		return err
	}
	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (runRow, error) {
		var r runRow
		err := row.Scan(&r.id, &r.upstreamID, &r.trigger, &r.model, &r.mode, &r.deadline, &r.batch)
		return r, err
	})
	if err != nil || len(runs) == 0 {
		return err
	}
	var ref *runRow
	var targets []runRow
	for i := range runs {
		if runs[i].trigger == "reference" {
			ref = &runs[i]
		} else {
			targets = append(targets, runs[i])
		}
	}
	if len(targets) == 0 {
		_, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'done', finished_at = now() WHERE batch_id = $1`, batch)
		return err
	}
	deadline := e.now().Add(lim.batch)
	if targets[0].mode != "legacy" {
		if targets[0].deadline != nil {
			deadline = *targets[0].deadline
		} else {
			st, err := loadInspectionSettings(ctx, e.DB)
			if err != nil {
				return err
			}
			minutes := st.Timeouts.Quick
			if targets[0].mode == "full" {
				minutes = st.Timeouts.Full
			}
			deadline = e.now().Add(time.Duration(minutes) * time.Minute)
			if time.Duration(minutes)*time.Minute < lim.request {
				return errors.New("质检截止时间不能短于单请求时限")
			}
			if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET deadline_at = $2 WHERE batch_id = $1 AND deadline_at IS NULL`, batch, deadline); err != nil {
				return err
			}
		}
	}
	if targets[0].mode == "legacy" {
		if targets[0].deadline != nil {
			deadline = *targets[0].deadline
		} else if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET deadline_at=$2 WHERE batch_id=$1 AND deadline_at IS NULL`, batch, deadline); err != nil {
			return err
		}
	}
	bctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	// 超时或中断后 ctx 已失效，收尾的写入改用独立的短时 ctx，否则记录会一直停在"执行中"
	cleanup := func(fn func(ctx context.Context) error) error {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		return fn(cctx)
	}
	for _, t := range targets {
		err := e.executeTarget(bctx, t, ref, lim.request)
		if err == nil {
			continue
		}
		if errors.Is(err, errWindowInsufficient) {
			return cleanup(func(ctx context.Context) error { return e.deferWindow(ctx, batch) })
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			e.Log.Info("probe batch interrupted, requeued", "batch", batch)
			return cleanup(func(ctx context.Context) error { return e.requeueBatch(ctx, batch) })
		}
		msg := err.Error()
		if errors.Is(bctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("check did not finish within %s", lim.batch)
		}
		e.Log.Error("probe run failed", "run_id", t.id, "err", err)
		if err := cleanup(func(ctx context.Context) error { return e.failRun(ctx, t, msg) }); err != nil {
			return err
		}
	}
	if ref != nil {
		return cleanup(func(ctx context.Context) error {
			_, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'done', finished_at = now() WHERE id = $1 AND status = 'running'`, ref.id)
			return err
		})
	}
	return nil
}

// requeueBatch 保留新模式的已完成结果，旧模式继续清空结果以兼容既有流程。
func (e *Engine) requeueBatch(ctx context.Context, batch string) error {
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE probe_runs r SET summary = COALESCE(summary,'{}') || jsonb_build_object('resume_budget',jsonb_build_object(
 'input_tokens_per_side',COALESCE((summary->'resume_budget'->>'input_tokens_per_side')::bigint,0)+(SELECT COALESCE(sum((step_state->>'input_charged')::bigint),0) FROM probe_results WHERE run_id=r.id),
 'output_tokens_per_side',COALESCE((summary->'resume_budget'->>'output_tokens_per_side')::bigint,0)+(SELECT COALESCE(sum((step_state->>'output_charged')::bigint),0) FROM probe_results WHERE run_id=r.id))) WHERE batch_id=$1 AND status='running' AND mode='legacy'`, batch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM probe_results WHERE run_id IN (SELECT id FROM probe_runs WHERE batch_id = $1 AND status = 'running' AND mode = 'legacy')`, batch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE probe_runs SET status = 'queued', started_at = NULL WHERE batch_id = $1 AND status = 'running'`, batch); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// failRun 记录检测失败（超时、中断或出错），并让依赖这次检测的流程继续：上架检测记为不通过（卖家可以重新检测），
// 申诉回到待处理，恢复复测按退避重新安排。
func (e *Engine) failRun(ctx context.Context, run runRow, msg string) error {
	return e.failRunWithSummary(ctx, run, msg, nil)
}

func (e *Engine) failRunWithSummary(ctx context.Context, run runRow, msg string, summary map[string]any) error {
	if summary == nil {
		summary = map[string]any{}
	}
	summary["error"] = msg
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE probe_runs SET status = 'failed', finished_at = now(), summary = $2 WHERE id = $1 AND status IN ('running', 'third_party')`,
		run.id, mustJSON(summary))
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if run.trigger != "reference" {
		// 失败也记为一次检测，避免疑似上游的追加检测每轮立即重排
		if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET last_run_at = now() WHERE upstream_id = $1`, run.upstreamID); err != nil {
			return err
		}
	}
	switch run.trigger {
	case "onboarding":
		if err := listing.MarkVerified(ctx, tx, run.upstreamID, false, "onboarding check failed: "+msg); err != nil {
			return err
		}
	case "appeal":
		if _, err := tx.Exec(ctx, `UPDATE appeals SET status = 'open'
			WHERE status = 'retesting' AND report_id IN (SELECT id FROM test_reports WHERE upstream_id = $1)`, run.upstreamID); err != nil {
			return err
		}
	case "recovery":
		// 恢复复测排队时清空了下次时间，失败后不重新安排就不会再自动复测
		var attempts int
		err := tx.QueryRow(ctx, `SELECT recovery_attempts FROM upstream_quality_state WHERE upstream_id = $1`, run.upstreamID).Scan(&attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		next := e.now().Add(recoveryBackoff[min(attempts, len(recoveryBackoff)-1)])
		if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET next_recovery_at = $2, recovery_attempts = recovery_attempts + 1
			WHERE upstream_id = $1 AND next_recovery_at IS NULL`, run.upstreamID, next); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (e *Engine) executeTarget(ctx context.Context, run runRow, ref *runRow, timeout time.Duration) error {
	u, err := e.loadUpstream(ctx, run.upstreamID)
	if err != nil {
		return err
	}
	p, err := EffectivePolicy(ctx, e.DB, u.id, u.platform, u.authType)
	if err != nil {
		return err
	}
	var frozenRaw []byte
	if err := e.DB.QueryRow(ctx, `SELECT execution_snapshot FROM probe_runs WHERE id=$1`, run.id).Scan(&frozenRaw); err != nil {
		return err
	}
	var frozen executionSnapshot
	if err := decodeInspectionJSON(frozenRaw, &frozen); err != nil {
		return err
	}
	if frozen.Policy != nil {
		p = frozen.Policy
	}
	// 已开始的批次沿用策略快照；硬预算独立于美元预算，上架、管理员与申诉仍强制执行。
	override := frozen.SchemaVersion == 1 || run.trigger == "onboarding" || run.trigger == "admin" || run.trigger == "appeal"
	if !p.Enabled && !override {
		_, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'skipped_disabled', finished_at = now() WHERE id = $1`, run.id)
		return err
	}
	if !override {
		var spent float64
		if err := e.DB.QueryRow(ctx, `SELECT COALESCE(sum(total_cost), 0)::float8 FROM usage_records
			WHERE upstream_id = $1 AND channel = 'probe' AND created_at >= date_trunc('day', now())`, u.id).Scan(&spent); err != nil {
			return err
		}
		if spent >= p.DailyBudgetUSD {
			_, err := e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'skipped_budget', finished_at = now() WHERE id = $1`, run.id)
			return err
		}
	}
	targetKey, err := e.probeKey(ctx, u)
	if err != nil {
		return fmt.Errorf("probe key: %w", err)
	}
	var refKey string
	var refUp *upstreamRow
	if ref != nil {
		if refUp, err = e.loadUpstream(ctx, ref.upstreamID); err == nil {
			if refKey, err = e.probeKey(ctx, refUp); err != nil {
				e.Log.Warn("reference probe key", "upstream_id", ref.upstreamID, "err", err)
				refKey = ""
			}
		}
	}

	st, err := loadInspectionSettings(ctx, e.DB)
	if err != nil {
		return err
	}
	if run.mode == "legacy" {
		st.Mode = "trial"
	}
	suites := p.Suites
	if run.mode != "legacy" {
		suites = append(append([]string(nil), suites...), inspectionSuites...)
	}
	runtimePolicy := *p
	runtimePolicy.Suites = suites
	p = &runtimePolicy
	items, err := selectExecutionItems(ctx, e.DB, u.platform, run.model, suites, run.mode)
	if err != nil {
		return err
	}
	enabledItems := make([]Item, 0, len(items))
	for _, it := range items {
		definition, known := inspection.Effective(it.CheckKey, st)
		if known && !definition.Enabled {
			continue
		}
		enabledItems = append(enabledItems, it)
	}
	items = enabledItems
	if st.Mode == "formal" {
		items = formalItems(items)
	}
	plan, err := e.executionPlan(ctx, run, p, items, st, timeout)
	if err != nil {
		return err
	}
	if plan.RequestTimeoutSeconds > 0 {
		timeout = time.Duration(plan.RequestTimeoutSeconds) * time.Second
	}
	outcomes, warnings, err := e.executePlan(ctx, run, ref, u, refUp, targetKey, refKey, plan, p, st, timeout)
	if err != nil {
		return err
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// 影子阶段到期只停止新请求；给已完成的旧题留出数据库收尾时间。
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
	}
	assessment := Assess(outcomes, plan.Thresholds, plan.Scoring)
	assessment, err = e.inspectionAssessment(ctx, run, ref, u, plan, outcomes, st, assessment)
	if err != nil {
		return err
	}
	assessment.Warnings = warnings
	if plan.InspectionMode != "formal" {
		assessment.Shadow = summarizeShadow(outcomes)
	}
	if incomplete, err := e.failUnobservedRun(ctx, run, u, assessment); incomplete {
		return err
	}
	if waiting, err := e.startThirdParty(ctx, run, u, p, assessment); err != nil || waiting {
		return err
	}
	return e.conclude(ctx, run, u, p, assessment, nil)
}

func (e *Engine) failUnobservedRun(ctx context.Context, run runRow, u *upstreamRow, a Assessment) (bool, error) {
	confirmed := false
	for _, gate := range a.Gates {
		confirmed = confirmed || gate.Confirmed
	}
	noValidation := a.AvailableTotal == 0 || a.InspectionMode == "formal" && a.AvailableOK == 0 && !confirmed
	if !noValidation || !containsAny([]string{"onboarding", "appeal", "recovery"}, run.trigger) {
		return false, nil
	}
	// 没有观测不能证明上架、申诉或恢复通过；检测失败只推进流程，不修改健康状态。
	msg := "没有有效观测：限流/平台错误，可稍后重试"
	if a.AvailableTotal > 0 {
		msg = "没有成功观测：本次请求均未成功完成，证据不足，可稍后重试"
	}
	if a.ErrorCounts["configuration"] > 0 {
		msg += "；存在题目配置错误，请管理员检查"
	}
	summary := e.buildSummary(run, u, a, "")
	summary["incomplete"] = true
	return true, e.failRunWithSummary(ctx, run, msg, summary)
}

func containsAny(list []string, want ...string) bool {
	for _, a := range list {
		for _, w := range want {
			if a == w {
				return true
			}
		}
	}
	return false
}

func (e *Engine) probe(ctx context.Context, platform, key string, req ProbeRequest, timeout time.Duration) ProbeResult {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.Prober.Probe(pctx, platform, key, req)
}

// refHistory 返回该题近 7 天在同平台基线账号上的通过率（至少 5 个样本）。
func (e *Engine) refHistory(ctx context.Context, platform string, itemID int64) (float64, bool, error) {
	var n int
	var rate float64
	err := e.DB.QueryRow(ctx, `SELECT count(*), COALESCE(avg(CASE WHEN pr.passed THEN 1 ELSE 0 END), 0)::float8
		FROM probe_results pr JOIN probe_runs r ON r.id = pr.run_id JOIN upstreams u ON u.id = r.upstream_id
		WHERE u.is_reference AND u.platform = $1 AND pr.item_id = $2 AND pr.error_class IS NULL
			AND pr.created_at > now() - interval '7 days'`, platform, itemID).Scan(&n, &rate)
	if err != nil || n < 5 {
		return 0, false, err
	}
	return rate, true, nil
}

// attribute 判定降智的原因：官方全局（基线也下降）、代理（同代理其他上游近期也降智）、账号本身。
func (e *Engine) attribute(ctx context.Context, u *upstreamRow, a Assessment) (string, error) {
	var refNow, refHist *float64
	if err := e.DB.QueryRow(ctx, `SELECT
			(SELECT avg(CASE WHEN pr.passed THEN 1 ELSE 0 END)::float8 FROM probe_results pr JOIN probe_runs r ON r.id = pr.run_id
				JOIN upstreams x ON x.id = r.upstream_id
				WHERE x.is_reference AND x.platform = $1 AND pr.suite IN ('iq', 'reasoning', 'long_context') AND pr.error_class IS NULL
					AND pr.created_at > now() - interval '3 hours'),
			(SELECT avg(CASE WHEN pr.passed THEN 1 ELSE 0 END)::float8 FROM probe_results pr JOIN probe_runs r ON r.id = pr.run_id
				JOIN upstreams x ON x.id = r.upstream_id
				WHERE x.is_reference AND x.platform = $1 AND pr.suite IN ('iq', 'reasoning', 'long_context') AND pr.error_class IS NULL
					AND pr.created_at BETWEEN now() - interval '7 days' AND now() - interval '3 hours')`, u.platform).Scan(&refNow, &refHist); err != nil {
		return "", err
	}
	if refNow != nil && refHist != nil && *refHist > 0 && *refNow < 0.8**refHist {
		return "vendor_wide", nil
	}
	if u.proxyID != nil {
		var peers int
		if err := e.DB.QueryRow(ctx, `SELECT count(DISTINCT v.upstream_id) FROM quality_verdicts v JOIN upstreams x ON x.id = v.upstream_id
			WHERE x.proxy_id = $1 AND x.id <> $2 AND v.verdict = 'degraded' AND v.created_at > now() - interval '24 hours'`, *u.proxyID, u.id).Scan(&peers); err != nil {
			return "", err
		}
		if peers > 0 {
			return "proxy", nil
		}
	}
	if !a.Baseline {
		return "unknown", nil
	}
	return "account", nil
}

var recoveryBackoff = []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 12 * time.Hour}

// conclude 记录判定、更新等级与健康状态、推进上架检测与申诉、生成卖家报告。
// tp 是第三方检测的结果：折算后的结论比内部判定更严重时以其为准（身份不一致归因于账号本身）。
func (e *Engine) conclude(ctx context.Context, run runRow, u *upstreamRow, p *Policy, a Assessment, tp []tpResult) error {
	if incomplete, err := e.failUnobservedRun(ctx, run, u, a); incomplete {
		return err
	}
	internalVerdict, tpCause := a.Verdict, ""
	thirdPartyApplies := true
	if a.InspectionMode == "formal" {
		st := defaultInspectionSettings()
		if a.InspectionSettings != nil {
			st = *a.InspectionSettings
		}
		d, _ := inspection.Effective("third_party", st)
		thirdPartyApplies = d.Enabled && d.Calibrated
	}
	for _, c := range tp {
		// 只有检测站给出明确的问题结论才会加重判定；检测失败、证据不足不影响内部判定
		if thirdPartyApplies && (c.Verdict == "suspected" || c.Verdict == "degraded") && verdictRank(c.Verdict) > verdictRank(a.Verdict) {
			a.Verdict, tpCause = c.Verdict, c.Provider
		}
	}
	if a.Checks == nil {
		a.Checks = map[string]inspection.Check{}
	}
	if a.Gates == nil {
		a.Gates = []inspection.Gate{}
	}
	addThirdPartyInspection(&a, tp, e.now())
	decision, failedChecks, err := e.finalizeInspection(ctx, run, u, &a)
	if err != nil {
		return err
	}
	if a.InspectionMode == "formal" && (u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable || run.trigger == "recovery") && a.Verdict == "normal" {
		complete, err := e.inspectionRecoveryComplete(ctx, u, a)
		if err != nil {
			return err
		}
		if !complete {
			a.Verdict = "inconclusive"
			a.Warnings = append(a.Warnings, "恢复复测未有效覆盖全部上次失败项，已退避重排")
		}
	}
	attribution := ""
	if a.Verdict == "degraded" {
		if tpCause != "" {
			attribution = "account"
		} else {
			var err error
			if a.InspectionMode == "formal" {
				attribution, err = e.attributeInspection(ctx, run, u, a)
			} else {
				attribution, err = e.attribute(ctx, u, a)
			}
			if err != nil {
				return err
			}
		}
	}
	healthBefore := u.health
	newHealth, reason := u.health, u.healthReason
	var st struct {
		consecutive int
		attempts    int
	}
	_ = e.DB.QueryRow(ctx, `SELECT consecutive_normal, recovery_attempts FROM upstream_quality_state WHERE upstream_id = $1`, u.id).
		Scan(&st.consecutive, &st.attempts)
	var nextRecovery *time.Time
	scheduleRecovery := func() {
		d := recoveryBackoff[min(st.attempts, len(recoveryBackoff)-1)]
		t := e.now().Add(d)
		nextRecovery = &t
		st.attempts++
		st.consecutive = 0
	}
	rel := 0.0
	if a.RelativeScore != nil {
		rel = *a.RelativeScore
	}
	degradedReason := fmt.Sprintf("degraded: relative score %.2f", rel)
	suspectedReason := fmt.Sprintf("suspected: relative score %.2f", rel)
	if tpCause != "" {
		degradedReason = "third-party check " + tpCause + ": model identity mismatch"
		suspectedReason = "third-party check " + tpCause + ": model identity suspected"
	}
	switch a.Verdict {
	case "unavailable":
		newHealth, reason = listing.HealthUnavailable, fmt.Sprintf("probe: %d/%d requests succeeded", a.AvailableOK, a.AvailableTotal)
		scheduleRecovery()
	case "degraded":
		switch {
		case attribution == "vendor_wide":
			// 官方全局下降，不处罚单个上游
		case attribution == "proxy":
			newHealth, reason = listing.HealthSuspected, "proxy suspected; please rotate proxy"
		case p.AutoPause || a.InspectionMode == "formal":
			newHealth, reason = listing.HealthDegraded, degradedReason
			scheduleRecovery()
		default:
			newHealth, reason = listing.HealthSuspected, strings.Replace(degradedReason, "degraded", "degraded (auto pause off)", 1)
		}
	case "normal":
		st.consecutive++
		need := 1
		// 从降智或不可用恢复需要连续两次正常；上架检测（含失败后重新检测）是完整检测，一次正常即可
		if (u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable) && run.trigger != "onboarding" {
			need = 2
		}
		if u.health != listing.HealthHealthy && st.consecutive >= need {
			newHealth, reason = listing.HealthHealthy, ""
			st.attempts = 0
		} else if u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable {
			// 第一次恢复正常，尽快复测确认
			t := e.now().Add(10 * time.Minute)
			nextRecovery = &t
		}
	case "inconclusive":
		if a.InspectionMode == "formal" && (run.trigger == "recovery" || u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable) {
			scheduleRecovery()
		}
	case "suspected":
		if u.health == listing.HealthHealthy || u.health == listing.HealthUnknown {
			newHealth, reason = listing.HealthSuspected, suspectedReason
		}
	}
	// 上架检测通过但结论不够明确（样本不足以判定）：按疑似处理，降低并发接流量并尽快追加检测
	if run.trigger == "onboarding" && newHealth == listing.HealthUnknown && a.Verdict != "degraded" && a.Verdict != "unavailable" {
		newHealth, reason = listing.HealthSuspected, "onboarding check inconclusive: "+a.Verdict
	}
	// 首次恢复通过仍处于暂停状态；不能仅凭本轮没有新门槛提前恢复估价。
	if a.Appraisal != nil && (newHealth == listing.HealthDegraded || newHealth == listing.HealthUnavailable) {
		a.Appraisal.Factor, a.Appraisal.Multiplier = nil, nil
	}

	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var attr *string
	if attribution != "" {
		attr = &attribution
	}
	var grade *string
	if a.Grade != "" {
		grade = &a.Grade
	}
	if !u.isReference {
		if _, err := tx.Exec(ctx, `INSERT INTO quality_verdicts (upstream_id, run_id, model, verdict, relative_score, reasoning_ratio, llr, attribution, grade,quality_factor,checks,gates)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,$10,$11,$12)`, u.id, run.id, run.model, a.Verdict, a.RelativeScore, a.ReasoningRatio, a.LLR, attr, grade, a.QualityFactor, mustJSON(a.Checks), mustJSON(a.Gates)); err != nil {
			return err
		}
		if grade != nil && (a.Baseline || a.InspectionMode == "formal" && validInspectionQ(a.QualityFactor)) {
			if _, err := tx.Exec(ctx, `UPDATE upstreams SET grade = $2, updated_at = now() WHERE id = $1`, u.id, *grade); err != nil {
				return err
			}
		}
		if len(a.Checks) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE upstreams SET appraised_at=$2,quality_factor=CASE WHEN $3 THEN COALESCE($4,quality_factor) ELSE quality_factor END WHERE id=$1`, u.id, e.now(), a.InspectionMode == "formal", a.QualityFactor); err != nil {
				return err
			}
		}
		if newHealth != u.health || reason != u.healthReason {
			if _, err := listing.SetHealth(ctx, tx, u.id, newHealth, reason); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO upstream_quality_state (upstream_id, last_run_at, consecutive_normal, recovery_attempts, next_recovery_at)
		VALUES ($1, now(), $2, $3, $4)
		ON CONFLICT (upstream_id) DO UPDATE SET last_run_at = now(), consecutive_normal = EXCLUDED.consecutive_normal,
			recovery_attempts = EXCLUDED.recovery_attempts, next_recovery_at = EXCLUDED.next_recovery_at`,
		u.id, st.consecutive, st.attempts, nextRecovery); err != nil {
		return err
	}
	if a.InspectionMode == "formal" {
		// 暂停恢复的第一次通过仍保留待复测项；第二次有效通过才清除。
		if newHealth == listing.HealthDegraded || newHealth == listing.HealthUnavailable {
			var previousFailed []string
			if err := tx.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&previousFailed); err != nil {
				return err
			}
			for _, key := range previousFailed {
				if !containsAny(failedChecks, key) {
					failedChecks = append(failedChecks, key)
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET pending_grade=NULLIF($2,''),pending_grade_count=$3,last_failed_checks=$4 WHERE upstream_id=$1`, u.id, decision.PendingGrade, decision.PendingGradeCount, failedChecks); err != nil {
			return err
		}
	}
	summary := e.buildSummary(run, u, a, attribution)
	if len(tp) > 0 {
		summary["third_party"] = tp
		if internalVerdict != a.Verdict {
			summary["internal_verdict"] = internalVerdict
		}
		if tpCause != "" {
			// 身份不一致时"检查共享使用、重新授权"之类的建议不适用
			sug := []string{"third_party_mismatch"}
			if old, _ := summary["suggestions"].([]string); containsAny(old, "disable_training") {
				sug = append(sug, "disable_training")
			}
			summary["suggestions"] = sug
		}
	}
	impact := map[string]any{"health_before": healthBefore, "health_after": newHealth,
		"buyer_facing_paused": newHealth == listing.HealthDegraded || newHealth == listing.HealthUnavailable}
	if nextRecovery != nil {
		impact["next_check_at"] = nextRecovery
	}
	if _, err := tx.Exec(ctx, `UPDATE probe_runs SET status = 'done', finished_at = now(), summary = $2 WHERE id = $1`, run.id, mustJSON(summary)); err != nil {
		return err
	}
	if !u.isReference {
		if _, err := tx.Exec(ctx, `INSERT INTO test_reports (upstream_id, run_id, trigger, verdict, summary, impact) VALUES ($1, $2, $3, $4, $5, $6)`,
			u.id, run.id, run.trigger, a.Verdict, mustJSON(summary), mustJSON(impact)); err != nil {
			return err
		}
	}
	switch run.trigger {
	case "onboarding":
		passed := a.Verdict != "degraded" && a.Verdict != "unavailable"
		why := ""
		if !passed {
			why = "onboarding check failed: " + a.Verdict
		}
		if err := listing.MarkVerified(ctx, tx, u.id, passed, why); err != nil {
			return err
		}
	case "appeal":
		status := "upheld"
		if a.Verdict == "normal" && !hasAppliedDiscount(a.Checks) {
			status = "overturned"
		}
		if _, err := tx.Exec(ctx, `UPDATE appeals SET status = $2, resolved_at = now()
			WHERE status = 'retesting' AND report_id IN (SELECT id FROM test_reports WHERE upstream_id = $1)`, u.id, status); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if newHealth != healthBefore || a.InspectionMode == "formal" {
		e.kick(ctx)
	}
	return nil
}

func (e *Engine) buildSummary(run runRow, u *upstreamRow, a Assessment, attribution string) map[string]any {
	suites := map[string]any{
		"availability": map[string]int{"success": a.AvailableOK, "total": a.AvailableTotal},
	}
	latency := map[string]any{}
	if a.TTFTp50Ms != nil {
		latency["ttft_p50_ms"] = *a.TTFTp50Ms
	}
	if a.TTFTp95Ms != nil {
		latency["ttft_p95_ms"] = *a.TTFTp95Ms
	}
	if a.OutputTPS != nil {
		latency["output_tps"] = *a.OutputTPS
	}
	suites["latency"] = latency
	if a.RelativeScore != nil {
		iq := map[string]any{"relative_score": *a.RelativeScore, "samples": a.Samples}
		if a.Confidence != nil {
			iq["confidence"] = *a.Confidence
		}
		suites["iq"] = iq
	}
	if a.ReasoningRatio != nil {
		suites["reasoning"] = map[string]any{"ratio_to_baseline": *a.ReasoningRatio}
	}
	// 建议以代码形式保存，由前端翻译成文案
	var suggestions []string
	switch {
	case a.Verdict == "unavailable":
		suggestions = append(suggestions, "check_authorization")
	case a.Verdict == "degraded" && attribution == "account":
		suggestions = append(suggestions, "check_shared_usage", "reauthorize")
	case attribution == "proxy":
		suggestions = append(suggestions, "proxy_rotation")
	case attribution == "vendor_wide":
		suggestions = append(suggestions, "vendor_wide")
	}
	if u.privacyOK != nil && !*u.privacyOK {
		suggestions = append(suggestions, "disable_training")
	}
	out := map[string]any{
		"verdict":    a.Verdict,
		"trigger":    run.trigger,
		"model":      run.model,
		"checked_at": e.now(),
		"suites":     suites,
		"baseline":   a.Baseline,
		"mode":       run.mode,
	}
	if len(a.Checks) > 0 {
		out["inspection_mode"], out["checks"], out["quality_factor"], out["gates"] = a.InspectionMode, a.Checks, a.QualityFactor, a.Gates
		out["group_factors"], out["appraisal"], out["proposed_grade"], out["repaired_grade"] = a.GroupFactors, a.Appraisal, a.ProposedGrade, a.RepairedGrade
	}
	if a.Shadow != nil {
		out["shadow"] = a.Shadow
	}
	if len(a.Warnings) > 0 {
		out["warnings"] = a.Warnings
	}
	if len(a.ErrorCounts) > 0 {
		out["error_counts"] = a.ErrorCounts
	}
	if len(a.ReferenceErrorCounts) > 0 {
		out["reference_error_counts"] = a.ReferenceErrorCounts
	}
	if attribution != "" {
		out["attribution"] = attribution
	}
	if a.Grade != "" && a.Baseline {
		out["grade"] = a.Grade
	}
	if u.tier != nil {
		out["tier"] = *u.tier
	}
	if len(suggestions) > 0 {
		out["suggestions"] = suggestions
	}
	return out
}

// probeKey 返回探测用户在该上游探测分组下的 key（必要时创建）。
func (e *Engine) probeKey(ctx context.Context, u *upstreamRow) (string, error) {
	if u.probeGroupID == nil {
		return "", errors.New("upstream has no probe group")
	}
	group := *u.probeGroupID
	aad := fmt.Sprintf("system-key:%d", group)
	var enc []byte
	var subID *int64
	err := e.DB.QueryRow(ctx, "SELECT key_enc, sub2api_key_id FROM system_keys WHERE sub2api_group_id = $1", group).Scan(&enc, &subID)
	if errors.Is(err, pgx.ErrNoRows) {
		plain := "tmpk_" + randx.String(40)
		if enc, err = e.Keyring.SealString(plain, aad); err != nil {
			return "", err
		}
		if _, err := e.DB.Exec(ctx, `INSERT INTO system_keys (purpose, sub2api_group_id, key_enc) VALUES ('probe', $1, $2) ON CONFLICT DO NOTHING`, group, enc); err != nil {
			return "", err
		}
		if err := e.DB.QueryRow(ctx, "SELECT key_enc, sub2api_key_id FROM system_keys WHERE sub2api_group_id = $1", group).Scan(&enc, &subID); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	plain, err := e.Keyring.OpenString(enc, aad)
	if err != nil {
		return "", err
	}
	if subID != nil {
		return plain, nil
	}
	ident, err := e.Shadow.Ensure(ctx, buyer.KindProbe, 0)
	if err != nil {
		return "", err
	}
	if err := e.Shadow.AllowGroup(ctx, ident.ID, group); err != nil {
		return "", err
	}
	id, err := e.Shadow.CreateKey(ctx, ident, group, fmt.Sprintf("tm-probe-key-%d", group), plain)
	if err != nil {
		return "", err
	}
	if _, err := e.DB.Exec(ctx, "UPDATE system_keys SET sub2api_key_id = $2 WHERE sub2api_group_id = $1", group, id); err != nil {
		return "", err
	}
	return plain, nil
}

// probeFunded 确保探测用户存在且余额可用；余额不足时排队充值并返回 false。
func (e *Engine) probeFunded(ctx context.Context) (bool, error) {
	ident, err := e.Shadow.Ensure(ctx, buyer.KindProbe, 0)
	if err != nil {
		return false, err
	}
	bs, err := e.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
	if err != nil {
		return false, err
	}
	if len(bs) == 1 && bs[0].Balance >= 1 {
		return true, nil
	}
	return false, e.ensureProbeBalance(ctx)
}

// ensureProbeBalance 保持探测用户的 sub2api 余额充足（探测的真实成本按卖家收益在用量入账时记录）。
func (e *Engine) ensureProbeBalance(ctx context.Context) error {
	ident, err := e.Shadow.Get(ctx, buyer.KindProbe, 0)
	if err != nil || ident == nil || ident.Sub2APIUserID == nil {
		return err
	}
	bs, err := e.Replica.UserBalances(ctx, []int64{*ident.Sub2APIUserID})
	if err != nil || len(bs) == 0 || bs[0].Balance >= 20 {
		return err
	}
	target := fmt.Sprintf("user:%d", *ident.Sub2APIUserID)
	pending, err := ops.PendingTargets(ctx, e.DB, []string{target})
	if err != nil || pending[target] {
		return err
	}
	_, err = ops.Enqueue(ctx, e.DB, ops.NewOp{
		Type:           opshandlers.TypeUserBalanceAdd,
		Target:         target,
		Payload:        opshandlers.BalancePayload{UserID: *ident.Sub2APIUserID, Amount: 100, Notes: "tm probe top-up"},
		IdempotencyKey: fmt.Sprintf("tm-probe-topup-%d", e.now().UnixNano()),
	})
	return err
}

// requoteExpired 为报价到期的回收挂单重新报价。
func (e *Engine) requoteExpired(ctx context.Context) error {
	if e.Listings == nil {
		return nil
	}
	rows, err := e.DB.Query(ctx, `SELECT l.id FROM listings l
		WHERE l.mode = 'buyback' AND l.status = 'active' AND NOT EXISTS (
			SELECT 1 FROM buyback_quotes q WHERE q.listing_id = l.id AND q.status = 'accepted' AND q.valid_until > now())`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, id := range ids {
		l, err := listing.GetListing(ctx, e.DB, id)
		if err != nil {
			return err
		}
		if _, err := e.Listings.Requote(ctx, l); err != nil {
			e.Log.Warn("requote listing", "listing_id", id, "err", err)
		}
	}
	return nil
}

// Passive 用不消耗额度的被动信号标记疑似问题：首字延迟远高于同组、响应模型不一致比例高、Edge 失败率高。
func (e *Engine) Passive(ctx context.Context) error {
	type flag struct {
		upstreamID int64
		typ        string
		detail     map[string]any
	}
	var flags []flag
	rows, err := e.DB.Query(ctx, `WITH per AS (
			SELECT upstream_id, model, count(*) AS n, percentile_cont(0.5) WITHIN GROUP (ORDER BY first_token_ms) AS p50,
				avg(CASE WHEN upstream_model_mismatch THEN 1 ELSE 0 END) AS mismatch
			FROM usage_records WHERE created_at > now() - interval '24 hours' AND channel IN ('pool', 'direct') AND upstream_id IS NOT NULL
			GROUP BY upstream_id, model HAVING count(*) >= 20),
		peer AS (SELECT model, percentile_cont(0.5) WITHIN GROUP (ORDER BY p50) AS peer_p50, count(*) AS peers FROM per GROUP BY model)
		SELECT per.upstream_id, per.model, per.p50, peer.peer_p50, per.mismatch, peer.peers
		FROM per JOIN peer USING (model)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var model string
		var p50, peerP50, mismatch *float64
		var peers int
		if err := rows.Scan(&id, &model, &p50, &peerP50, &mismatch, &peers); err != nil {
			rows.Close()
			return err
		}
		if p50 != nil && peerP50 != nil && peers >= 3 && *peerP50 > 0 && *p50 > 3**peerP50 {
			flags = append(flags, flag{id, "slow_ttft", map[string]any{"model": model, "ttft_p50": *p50, "peer_p50": *peerP50}})
		}
		if mismatch != nil && *mismatch > 0.2 {
			flags = append(flags, flag{id, "model_mismatch", map[string]any{"model": model, "ratio": *mismatch}})
		}
	}
	rows.Close()
	rows, err = e.DB.Query(ctx, `SELECT l.upstream_id, count(*),
			avg(CASE WHEN a.end_reason IN ('retry', 'upstream_error', 'timeout') THEN 1 ELSE 0 END)::float8
		FROM edge_attempts a JOIN listings l ON l.id = a.listing_id
		WHERE a.started_at > now() - interval '1 hour' AND a.end_reason <> 'skipped'
		GROUP BY l.upstream_id HAVING count(*) >= 20`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var n int
		var rate float64
		if err := rows.Scan(&id, &n, &rate); err != nil {
			rows.Close()
			return err
		}
		if rate > 0.5 {
			flags = append(flags, flag{id, "edge_failures", map[string]any{"attempts": n, "failure_rate": rate}})
		}
	}
	rows.Close()
	changed := false
	for _, f := range flags {
		var recent int
		if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM health_events WHERE upstream_id = $1 AND type = $2 AND created_at > now() - interval '6 hours'`,
			f.upstreamID, f.typ).Scan(&recent); err != nil {
			return err
		}
		if recent > 0 {
			continue
		}
		source := "usage"
		if f.typ == "edge_failures" {
			source = "edge"
		}
		if _, err := e.DB.Exec(ctx, `INSERT INTO health_events (upstream_id, type, source, detail) VALUES ($1, $2, $3, $4)`,
			f.upstreamID, f.typ, source, mustJSON(f.detail)); err != nil {
			return err
		}
		tag, err := e.DB.Exec(ctx, `UPDATE upstreams SET health = 'suspected', health_reason = $2, health_changed_at = now(), updated_at = now()
			WHERE id = $1 AND health IN ('healthy', 'unknown')`, f.upstreamID, "passive: "+f.typ)
		if err != nil {
			return err
		}
		changed = changed || tag.RowsAffected() > 0
	}
	if changed {
		e.kick(ctx)
	}
	return nil
}
