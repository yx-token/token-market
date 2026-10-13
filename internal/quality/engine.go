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
	modelWhitelist []string
	accountID      *int64
	id             int64
	platform       string
	authType       string
	status         string
	health         string
	healthReason   string
	isReference    bool
	probeGroupID   *int64
	proxyID        *int64
	tier           *string
	privacyOK      *bool
}

func (e *Engine) loadUpstream(ctx context.Context, id int64) (*upstreamRow, error) {
	var u upstreamRow
	var reason *string
	err := e.DB.QueryRow(ctx, `SELECT id, platform, auth_type, status, health, health_reason, is_reference, probe_group_id, proxy_id, tier, privacy_ok, sub2api_account_id, model_whitelist
		FROM upstreams WHERE id = $1`, id).Scan(&u.id, &u.platform, &u.authType, &u.status, &u.health, &reason, &u.isReference,
		&u.probeGroupID, &u.proxyID, &u.tier, &u.privacyOK, &u.accountID, &u.modelWhitelist)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, listing.ErrUpstreamNotFound
	}
	if reason != nil {
		u.healthReason = *reason
	}
	return &u, err
}

// QueueRun 为上游排队一次检测，并在同一批次中为同平台的基线账号排队一次对照检测。
func (e *Engine) QueueRun(ctx context.Context, upstreamID int64, trigger string, requested ...string) (int64, error) {
	u, err := e.loadUpstream(ctx, upstreamID)
	if err != nil {
		return 0, err
	}
	p, err := EffectivePolicy(ctx, e.DB, u.id, u.platform, u.authType)
	if err != nil {
		return 0, err
	}
	flows, err := e.selectedFlows(ctx, u, p, trigger, requested)
	if err != nil {
		return 0, err
	}
	candidates := intelligenceModels(p, u.modelWhitelist)
	models := []string{}
	if containsAny(flows, "intelligence") {
		models = candidates
	}
	// 降智检测与测智各自冻结一套模型；两个集合都为空时对应流程记 not_applicable。
	degradationCandidates := degradationModels(p, u.modelWhitelist)
	degradationModelList := []string{}
	if containsAny(flows, inspection.FlowDegradation) {
		degradationModelList = degradationCandidates
	}
	model := ""
	if len(candidates) > 0 {
		model = candidates[0]
	} else if len(degradationCandidates) > 0 {
		model = degradationCandidates[0]
	} else if len(u.modelWhitelist) > 0 {
		model = u.modelWhitelist[0]
	} else {
		for _, site := range p.ThirdParty {
			if site.Model != "" {
				model = site.Model
				break
			}
		}
		if model == "" && len(p.Models) > 0 {
			model = p.Models[0]
		}
	}
	if model == "" && (containsAny(flows, "intelligence") && len(models) > 0 || containsAny(flows, inspection.FlowDegradation) && len(degradationModelList) > 0 || p.Flows.Identity.Source == "self" && containsAny(flows, "identity")) {
		return 0, httpx.BadRequest("NO_MODELS", "检测策略没有配置模型")
	}
	var failed []string
	err = e.DB.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&failed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	mode, flowModes := modesForFlows(p, flows, trigger, failed)
	source := ""
	if containsAny(flows, "identity") {
		source = p.Flows.Identity.Source
	}
	snapshot := executionSnapshot{Policy: p, FlowModes: flowModes, Models: models, DegradationModels: degradationModelList, ModelWhitelist: u.modelWhitelist}
	batch := fmt.Sprintf("b%d-%d", u.id, e.now().UnixNano())
	tx, err := e.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 串行排队检查，避免并发触发重复运行。
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, u.id); err != nil {
		return 0, err
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM probe_runs WHERE upstream_id=$1 AND status IN ('queued','running','third_party')`, u.id).Scan(&pending); err != nil {
		return 0, err
	}
	if pending > 0 && trigger != "appeal" && trigger != "admin" && trigger != "onboarding" {
		return 0, httpx.Conflict("RUN_PENDING", "该上游已有待完成的检测")
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode,flows,identity_source,models,degradation_models,execution_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11) RETURNING id`, u.id, batch, trigger, p.Version, model, mode, flows, source, models, degradationModelList, mustJSON(snapshot)).Scan(&id); err != nil {
		return 0, err
	}
	selfFlows := []string{}
	for _, flow := range flows {
		if probeFlowFamily(flow) == inspection.FlowIntelligence && len(flowModels(p, flow, u.modelWhitelist)) > 0 || flow == "identity" && source == "self" {
			selfFlows = append(selfFlows, flow)
		}
	}
	if !u.isReference && len(selfFlows) > 0 && model != "" {
		referenceModels := append([]string(nil), models...)
		for _, degradationModel := range degradationModelList {
			if !containsAny(referenceModels, degradationModel) {
				referenceModels = append(referenceModels, degradationModel)
			}
		}
		if containsAny(selfFlows, "identity") {
			identityModels := u.modelWhitelist
			if identityModels == nil {
				identityModels = candidates
			}
			for _, identityModel := range append(identityModels, model) {
				if !containsAny(referenceModels, identityModel) {
					referenceModels = append(referenceModels, identityModel)
				}
			}
		}
		var refID int64
		err := tx.QueryRow(ctx, `SELECT u.id FROM upstreams u WHERE is_reference AND platform=$1 AND status IN ('ready','listed') AND health='healthy' AND sub2api_account_id IS NOT NULL AND auth_type=$2 AND tier IS NOT DISTINCT FROM $3::text AND (model_whitelist IS NULL OR model_whitelist @> $4::text[]) ORDER BY (SELECT count(*) FROM probe_runs r WHERE r.upstream_id=u.id AND r.status IN ('queued','running','third_party')),u.id LIMIT 1`, u.platform, u.authType, u.tier, referenceModels).Scan(&refID)
		if err == nil {
			// 基线跑同一批题目，需要与目标运行一致地冻结降智模型集合，否则同一批次的两侧选题不同。
			if _, err := tx.Exec(ctx, `INSERT INTO probe_runs(upstream_id,batch_id,trigger,policy_version,model,mode,flows,identity_source,models,degradation_models,execution_snapshot) VALUES($1,$2,'reference',$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10)`, refID, batch, p.Version, model, mode, selfFlows, source, models, degradationModelList, []byte(`{}`)); err != nil {
				return 0, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
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
	rows, err := e.DB.Query(ctx, `SELECT u.id,u.platform,u.auth_type,u.health,u.health_reason,q.next_intelligence_at,q.next_degradation_at,q.next_identity_at,q.next_recovery_at,q.last_run_at,COALESCE(q.suspected_flows,'{}') FROM upstreams u LEFT JOIN upstream_quality_state q ON q.upstream_id=u.id WHERE u.status IN ('ready','listed') AND u.sub2api_account_id IS NOT NULL AND NOT u.is_reference`)
	if err != nil {
		return err
	}
	type item struct {
		id                                                  int64
		platform, authType, health                          string
		reason                                              *string
		intelligence, degradation, identity, recovery, last *time.Time
		suspected                                           []string
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (item, error) {
		var it item
		err := row.Scan(&it.id, &it.platform, &it.authType, &it.health, &it.reason, &it.intelligence, &it.degradation, &it.identity, &it.recovery, &it.last, &it.suspected)
		return it, err
	})
	if err != nil {
		return err
	}
	now := e.now()
	for _, it := range items {
		p, err := EffectivePolicy(ctx, e.DB, it.id, it.platform, it.authType)
		if err != nil {
			return err
		}
		if !p.Enabled {
			continue
		}
		next := map[string]*time.Time{"intelligence": it.intelligence, "degradation": it.degradation, "identity": it.identity}
		trigger := ""
		flows := []string{}
		probeCaused := it.health == listing.HealthDegraded || it.health == listing.HealthUnavailable && it.reason != nil && strings.HasPrefix(*it.reason, "probe:")
		if probeCaused && it.recovery != nil && !now.Before(*it.recovery) {
			trigger = "recovery"
		}
		if trigger == "" && it.health == listing.HealthSuspected {
			for _, flow := range it.suspected {
				if flowPolicy(p, flow).Enabled && flowPolicy(p, flow).Applies("adaptive") && (next[flow] == nil || !now.Before(*next[flow])) {
					flows = append(flows, flow)
				}
			}
			if len(flows) == 0 && len(it.suspected) == 0 && (it.last == nil || now.Sub(*it.last) >= 30*time.Minute) {
				for _, flow := range detectionFlows {
					if flowPolicy(p, flow).Enabled && flowPolicy(p, flow).Applies("adaptive") {
						flows = append(flows, flow)
					}
				}
			}
			if len(flows) > 0 {
				trigger = "adaptive"
			}
		}
		if trigger == "" && p.ScheduledEnabled {
			for _, flow := range detectionFlows {
				f := flowPolicy(p, flow)
				if f.Enabled && f.ScheduledEnabled && f.Applies("scheduled") && !containsAny(it.suspected, flow) && (next[flow] == nil || !now.Before(*next[flow])) {
					flows = append(flows, flow)
				}
			}
			if len(flows) > 0 {
				trigger = "scheduled"
			}
		}
		if trigger == "" {
			continue
		}
		id, err := e.QueueRun(ctx, it.id, trigger, flows...)
		if err != nil {
			var he *httpx.Error
			if errors.As(err, &he) {
				continue
			}
			return err
		}
		if trigger == "scheduled" {
			for _, flow := range flows {
				scheduled := now.Add(time.Duration(flowPolicy(p, flow).IntervalMinutes) * time.Minute)
				// 旧全局抖动不能把较短流程的下次时间推进到当前时间之前。
				jitter := min(p.JitterMinutes, flowPolicy(p, flow).IntervalMinutes/2)
				if jitter > 0 {
					scheduled = scheduled.Add(time.Duration(rand.IntN(2*jitter+1)-jitter) * time.Minute)
				}
				column := "next_intelligence_at"
				switch flow {
				case inspection.FlowIdentity:
					column = "next_identity_at"
				case inspection.FlowDegradation:
					column = "next_degradation_at"
				}
				if _, err := e.DB.Exec(ctx, `INSERT INTO upstream_quality_state(upstream_id,`+column+`) VALUES($1,$2) ON CONFLICT(upstream_id) DO UPDATE SET `+column+`=EXCLUDED.`+column, it.id, scheduled); err != nil {
					return err
				}
			}
		}
		if trigger == "recovery" {
			if _, err := e.DB.Exec(ctx, `UPDATE upstream_quality_state SET next_recovery_at=NULL WHERE upstream_id=$1`, it.id); err != nil {
				return err
			}
		}
		_ = id
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
	rows, err := e.DB.Query(ctx, `SELECT r.batch_id FROM probe_runs r WHERE r.status='queued' AND NOT EXISTS(SELECT 1 FROM probe_runs older WHERE older.upstream_id=r.upstream_id AND older.batch_id<>r.batch_id AND (older.status IN ('running','third_party') OR older.status='queued' AND older.id<r.id)) GROUP BY r.batch_id ORDER BY min(r.queued_at) LIMIT 50`)
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
		WHERE status = 'running' AND ((mode <> 'legacy' AND COALESCE((execution_snapshot->>'schema_version')::int,0)=1 AND started_at < now() - interval '5 minutes') OR (deadline_at IS NOT NULL AND deadline_at < now() - interval '5 minutes') OR (deadline_at IS NULL AND started_at < now() - make_interval(secs => $1)))`, (lim.batch + orphanGrace).Seconds())
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
	flows             []string
	identitySource    string
	models            []string
	degradationModels []string
	id                int64
	upstreamID        int64
	trigger           string
	model             string
	mode              string
	deadline          *time.Time
	batch             string
}

// flowModelsOf 按流程取测智类流程的模型集合：降智检测用 degradation_models，测智用 models。
func flowModelsOf(run runRow, flow string) []string {
	if flow == inspection.FlowDegradation {
		return run.degradationModels
	}
	return run.models
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
		WHERE batch_id = $1 AND status = 'queued' RETURNING id, upstream_id, trigger, model, mode, deadline_at, batch_id, flows, COALESCE(identity_source,''), models, degradation_models`, batch)
	if err != nil {
		return err
	}
	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (runRow, error) {
		var r runRow
		err := row.Scan(&r.id, &r.upstreamID, &r.trigger, &r.model, &r.mode, &r.deadline, &r.batch, &r.flows, &r.identitySource, &r.models, &r.degradationModels)
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
	summary["flows"], summary["identity_source"], summary["models"] = run.flows, run.identitySource, run.models
	if err := e.cancelThirdParty(ctx, run.id); err != nil {
		return err
	}
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
	case "adaptive":
		var attempts int
		if err := tx.QueryRow(ctx, `SELECT adaptive_attempts FROM upstream_quality_state WHERE upstream_id=$1`, run.upstreamID).Scan(&attempts); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		next := e.now().Add(adaptiveBackoff[min(attempts, len(adaptiveBackoff)-1)])
		if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET adaptive_attempts=adaptive_attempts+1,
			next_intelligence_at=CASE WHEN 'intelligence'=ANY($2::text[]) THEN $3 ELSE next_intelligence_at END,
			next_degradation_at=CASE WHEN 'degradation'=ANY($2::text[]) THEN $3 ELSE next_degradation_at END,
			next_identity_at=CASE WHEN 'identity'=ANY($2::text[]) THEN $3 ELSE next_identity_at END WHERE upstream_id=$1`, run.upstreamID, run.flows, next); err != nil {
			return err
		}
	case "onboarding":
		if err := listing.MarkVerified(ctx, tx, run.upstreamID, false, "上架检测未通过："+msg); err != nil {
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
	st, err := loadInspectionSettings(ctx, e.DB)
	if err != nil {
		return err
	}
	if frozen.Settings != nil {
		st = *frozen.Settings
	}
	frozen.Settings = &st
	frozen.Policy = p
	frozen.InspectionMode = st.Mode
	frozen.Flows = run.flows
	if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=$2 WHERE id=$1`, run.id, mustJSON(frozen)); err != nil {
		return err
	}
	if err := e.submitThirdParty(ctx, run, u, p); err != nil {
		return err
	}
	items, err := selectFlowExecutionItems(ctx, e.DB, u.platform, run.model, run.models, run.degradationModels, run.flows, run.identitySource, frozen.FlowModes, st)
	if err != nil {
		return err
	}
	pure := len(items) == 0 && run.trigger != "recovery"
	if pure {
		frozen.SchemaVersion = 1
		frozen.Mode = run.mode
		if _, err := e.DB.Exec(ctx, `UPDATE probe_runs SET execution_snapshot=$2 WHERE id=$1`, run.id, mustJSON(frozen)); err != nil {
			return err
		}
		a, err := e.inspectionAssessment(ctx, run, nil, u, frozen, nil, st, Assessment{})
		if err != nil {
			return err
		}
		if waiting, err := e.startThirdParty(ctx, run, u, p, a); err != nil || waiting {
			return err
		}
		return e.conclude(ctx, run, u, p, a, nil)
	}
	targetKey, err := e.probeKey(ctx, u)
	if err != nil {
		return fmt.Errorf("检测密钥：%w", err)
	}
	var refKey string
	var refUp *upstreamRow
	if ref != nil {
		if refUp, err = e.loadUpstream(ctx, ref.upstreamID); err == nil {
			if refKey, err = e.probeKey(ctx, refUp); err != nil {
				e.Log.Warn("基线检测密钥", "upstream_id", ref.upstreamID, "err", err)
				refKey = ""
			}
		}
	}
	plan, err := e.executionPlan(ctx, run, p, items, st, timeout)
	if err != nil {
		return err
	}
	if plan.Policy == nil {
		plan.Policy = p
	}
	if plan.Settings == nil {
		plan.Settings = &st
	}
	if plan.RequestTimeoutSeconds > 0 {
		timeout = time.Duration(plan.RequestTimeoutSeconds) * time.Second
	}
	outcomes, warnings, err := e.executePlan(ctx, run, ref, u, refUp, targetKey, refKey, plan, p, st, timeout)
	if err != nil {
		return err
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// 到期后保留已完成请求的证据，并留出数据库收尾时间。
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
	}
	assessment := assessFormal(outcomes, plan.Thresholds, st)
	assessment, err = e.inspectionAssessment(ctx, run, ref, u, plan, outcomes, st, assessment)
	if err != nil {
		return err
	}
	assessment.Warnings = warnings
	if waiting, err := e.startThirdParty(ctx, run, u, p, assessment); err != nil || waiting {
		return err
	}
	return e.conclude(ctx, run, u, p, assessment, nil)
}

func (e *Engine) failUnobservedRun(ctx context.Context, run runRow, u *upstreamRow, a Assessment) (bool, error) {
	if !containsAny([]string{"onboarding", "appeal", "recovery"}, run.trigger) || a.Verdict == "unavailable" || a.Verdict == "degraded" {
		return false, nil
	}
	incomplete := []string{}
	for _, flow := range run.flows {
		if !checkConclusive(summarizeFlow(flow, run, a).Status) {
			incomplete = append(incomplete, flow)
		}
	}
	if len(run.flows) == 0 && a.AvailableTotal == 0 {
		incomplete = append(incomplete, inspection.FlowIntelligence)
	}
	if len(incomplete) == 0 {
		return false, nil
	}
	summary := e.buildSummary(run, u, a, "")
	summary["incomplete"] = true
	return true, e.failRunWithSummary(ctx, run, "目标流程没有有效结论，可稍后重试："+strings.Join(incomplete, "、"), summary)
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

var recoveryBackoff = []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 12 * time.Hour}

// conclude 记录判定、更新等级与健康状态、推进上架检测与申诉、生成卖家报告。
// tp 是第三方检测的结果：折算后的结论比内部判定更严重时以其为准（身份不一致归因于账号本身）。
func (e *Engine) conclude(ctx context.Context, run runRow, u *upstreamRow, p *Policy, a Assessment, tp []tpResult) error {
	internalVerdict, tpCause := a.Verdict, ""
	if a.Checks == nil {
		a.Checks = map[string]inspection.Check{}
	}
	if a.Gates == nil {
		a.Gates = []inspection.Gate{}
	}
	addThirdPartyInspection(&a, tp, e.now())
	a.Verdict = decideVerdict(run, a, tp)
	for _, c := range tp {
		if c.Verdict == "degraded" || c.Verdict == "suspected" {
			tpCause = c.Provider
		}
	}
	if incomplete, err := e.failUnobservedRun(ctx, run, u, a); incomplete {
		return err
	}
	decision, failedChecks, err := e.finalizeInspection(ctx, run, u, &a)
	if err != nil {
		return err
	}
	if (u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable || run.trigger == "recovery") && a.Verdict == "normal" {
		complete, err := e.inspectionRecoveryComplete(ctx, u, a, run.flows...)
		if err != nil {
			return err
		}
		if !complete {
			a.Verdict = "inconclusive"
			a.Warnings = append(a.Warnings, "恢复复测未有效覆盖全部上次失败项，已退避重排")
		}
	}
	attribution := ""
	if a.Verdict == "degraded" || a.Verdict == "suspected" {
		if tpCause != "" {
			attribution = "account"
		} else {
			var err error
			attribution, err = e.attributeInspection(ctx, run, u, a)
			if err != nil {
				return err
			}
		}
	}
	healthBefore := u.health
	newHealth, reason := u.health, u.healthReason
	var st struct {
		consecutive, attempts, adaptive int
		suspected, previousFailed       []string
	}
	err = e.DB.QueryRow(ctx, `SELECT consecutive_normal,recovery_attempts,adaptive_attempts,suspected_flows,last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&st.consecutive, &st.attempts, &st.adaptive, &st.suspected, &st.previousFailed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	for _, key := range st.previousFailed {
		d, ok := inspection.Lookup(key)
		if !ok {
			continue
		}
		// 测智类流程冻结的模型为空表示这项本轮没有适用模型，不能当作「已修复」。
		if probeFlowFamily(d.Flow) == inspection.FlowIntelligence && containsAny(run.flows, d.Flow) && len(flowModelsOf(run, d.Flow)) == 0 {
			continue
		}
		c, observed := a.Checks[key]
		repaired := (containsAny(run.flows, d.Flow) || d.Flow == "common") && observed && (c.Status == "pass" || c.Status == "not_applicable") && c.Evidence.Source != "history"
		// 未观察到的新结果不能抹掉待复测项，否则下轮会错误退回快检。
		if !repaired && !containsAny(failedChecks, key) {
			failedChecks = append(failedChecks, key)
		}
	}
	nextAdaptive := map[string]time.Time{}
	for _, flow := range run.flows {
		summary := summarizeFlow(flow, run, a)
		// 测智类流程失败不暂停账号，只标疑似等待退避复测。
		bad := (summary.Status == "fail" || summary.Status == "warning") && (probeFlowFamily(flow) == inspection.FlowIntelligence || run.identitySource == "third_party")
		if bad && attribution != "vendor_wide" {
			if !containsAny(st.suspected, flow) {
				st.suspected = append(st.suspected, flow)
			}
			nextAdaptive[flow] = e.now().Add(adaptiveBackoff[min(st.adaptive, len(adaptiveBackoff)-1)])
		} else if !checkConclusive(summary.Status) && containsAny(st.suspected, flow) {
			nextAdaptive[flow] = e.now().Add(adaptiveBackoff[min(st.adaptive, len(adaptiveBackoff)-1)])
		} else if summary.Status == "pass" || summary.Status == "not_applicable" {
			repaired := true
			for _, key := range st.previousFailed {
				d, ok := inspection.Lookup(key)
				if !ok || d.Flow != flow || probeFlowFamily(flow) == inspection.FlowIntelligence && summary.Status == "not_applicable" && len(flowModelsOf(run, flow)) == 0 {
					continue
				}
				c := a.Checks[key]
				if (c.Status != "pass" && c.Status != "not_applicable") || c.Evidence.Source == "history" {
					repaired = false
				}
			}
			if repaired {
				remaining := []string{}
				for _, suspected := range st.suspected {
					if suspected != flow {
						remaining = append(remaining, suspected)
					}
				}
				st.suspected = remaining
			} else if containsAny(st.suspected, flow) {
				nextAdaptive[flow] = e.now().Add(adaptiveBackoff[min(st.adaptive, len(adaptiveBackoff)-1)])
			}
		}
	}
	if len(nextAdaptive) > 0 {
		st.adaptive++
	}
	if len(st.suspected) == 0 {
		st.adaptive = 0
	}
	var nextRecovery *time.Time
	scheduleRecovery := func() {
		d := recoveryBackoff[min(st.attempts, len(recoveryBackoff)-1)]
		t := e.now().Add(d)
		nextRecovery = &t
		st.attempts++
		st.consecutive = 0
	}
	degradedReason := "检测门槛未通过"
	suspectedReason := "测智未通过，等待复测"
	if tpCause != "" {
		degradedReason = "第三方身份核验不符：" + tpCause
		suspectedReason = "第三方身份核验疑似异常：" + tpCause
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
		case tpCause != "" || p.AutoPause || a.InspectionMode == "formal":
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
		if u.health != listing.HealthHealthy && st.consecutive >= need && len(st.suspected) == 0 && (u.health != listing.HealthSuspected || strings.HasPrefix(u.healthReason, "flow:") || strings.HasPrefix(u.healthReason, "passive:")) {
			newHealth, reason = listing.HealthHealthy, ""
			st.attempts = 0
		} else if u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable {
			// 第一次恢复正常，尽快复测确认
			t := e.now().Add(10 * time.Minute)
			nextRecovery = &t
		}
	case "inconclusive":
		if run.trigger == "recovery" || u.health == listing.HealthDegraded || u.health == listing.HealthUnavailable {
			scheduleRecovery()
		}
	case "suspected":
		if attribution != "vendor_wide" && (u.health == listing.HealthHealthy || u.health == listing.HealthUnknown || u.health == listing.HealthSuspected) {
			newHealth, reason = listing.HealthSuspected, "flow: "+strings.Join(st.suspected, "、")+"；"+suspectedReason
		}
	}
	if attribution == "vendor_wide" && u.health == listing.HealthUnknown {
		newHealth, reason = listing.HealthHealthy, ""
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
		if grade != nil && a.InspectionMode == "formal" && validInspectionQ(a.QualityFactor) {
			if _, err := tx.Exec(ctx, `UPDATE upstreams SET grade = $2, updated_at = now() WHERE id = $1`, u.id, *grade); err != nil {
				return err
			}
		}
		if len(a.Checks) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE upstreams SET appraised_at=$2,quality_factor=CASE WHEN $3 THEN quality_factor ELSE COALESCE($4::float8,1.0) END WHERE id=$1`, u.id, e.now(), formalBlocked(a), a.QualityFactor); err != nil {
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
	// 暂停恢复的第一次通过仍保留待复测项；第二次有效通过才清除。
	if newHealth == listing.HealthDegraded || newHealth == listing.HealthUnavailable {
		for _, key := range st.previousFailed {
			if !containsAny(failedChecks, key) {
				failedChecks = append(failedChecks, key)
			}
		}
	}
	if st.suspected == nil {
		st.suspected = []string{}
	}
	if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET pending_grade=NULLIF($2,''),pending_grade_count=$3,last_failed_checks=$4,suspected_flows=$5,adaptive_attempts=$6 WHERE upstream_id=$1`, u.id, decision.PendingGrade, decision.PendingGradeCount, failedChecks, st.suspected, st.adaptive); err != nil {
		return err
	}
	for _, flow := range run.flows {
		lastColumn, nextColumn := "last_intelligence_at", "next_intelligence_at"
		switch flow {
		case inspection.FlowIdentity:
			lastColumn, nextColumn = "last_identity_at", "next_identity_at"
		case inspection.FlowDegradation:
			lastColumn, nextColumn = "last_degradation_at", "next_degradation_at"
		}
		next := e.now().Add(time.Duration(flowPolicy(p, flow).IntervalMinutes) * time.Minute)
		if adaptive, ok := nextAdaptive[flow]; ok {
			next = adaptive
		}
		if _, err := tx.Exec(ctx, `UPDATE upstream_quality_state SET `+lastColumn+`=$2,`+nextColumn+`=$3 WHERE upstream_id=$1`, u.id, e.now(), next); err != nil {
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
		passed := a.Verdict != "degraded" && a.Verdict != "unavailable" && a.Verdict != "inconclusive"
		why := ""
		if !passed {
			why = "上架检测未通过：" + a.Verdict
		}
		if err := listing.MarkVerified(ctx, tx, u.id, passed, why); err != nil {
			return err
		}
	case "appeal":
		status := "upheld"
		if a.Verdict == "normal" && !hasAppliedDiscount(a.Checks) {
			status = "overturned"
			// 复测推翻只清除复测开始前的证据，保留本轮通过结果供估价与历史继承。
			var resetAt time.Time
			if err := tx.QueryRow(ctx, `SELECT COALESCE(started_at,queued_at) FROM probe_runs WHERE id=$1`, run.id).Scan(&resetAt); err != nil {
				return err
			}
			if err := resetFlowDeductions(ctx, tx, u.id, run.flows, resetAt); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE appeals SET status = $2, resolved_at = now()
			WHERE status = 'retesting' AND report_id IN (SELECT id FROM test_reports WHERE upstream_id = $1)`, u.id, status); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if newHealth != healthBefore || len(a.Checks) > 0 {
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
		"flows":      run.flows, "identity_source": run.identitySource, "models": run.models,
		"intelligence": summarizeFlow("intelligence", run, a), "identity": summarizeFlow("identity", run, a),
		"degradation": summarizeFlow(inspection.FlowDegradation, run, a),
	}
	if len(a.Checks) > 0 {
		out["inspection_mode"], out["checks"], out["quality_factor"], out["gates"] = a.InspectionMode, a.Checks, a.QualityFactor, a.Gates
		out["group_factors"], out["appraisal"], out["proposed_grade"], out["repaired_grade"] = a.GroupFactors, a.Appraisal, a.ProposedGrade, a.RepairedGrade
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
