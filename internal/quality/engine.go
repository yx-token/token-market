package quality

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/DueGin/token-market/internal/buyer"
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
	DB          *pgxpool.Pool
	Replica     *replica.Reader
	Shadow      *buyer.ShadowService
	Keyring     *crypto.Keyring
	Prober      *Prober
	Listings    *listing.Service
	Kicker      listing.Kicker
	Settings    func(ctx context.Context) (settings.Settings, error)
	Log         *slog.Logger
	Concurrency int
	Now         func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
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
	err := e.DB.QueryRow(ctx, `SELECT id, platform, auth_type, status, health, health_reason, is_reference, probe_group_id, proxy_id, tier, privacy_ok
		FROM upstreams WHERE id = $1`, id).Scan(&u.id, &u.platform, &u.authType, &u.status, &u.health, &reason, &u.isReference,
		&u.probeGroupID, &u.proxyID, &u.tier, &u.privacyOK)
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
	if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM probe_runs WHERE upstream_id = $1 AND status IN ('queued', 'running')`, u.id).Scan(&pending); err != nil {
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
	batch := fmt.Sprintf("b%d-%d", u.id, e.now().UnixNano())
	var id int64
	if err := e.DB.QueryRow(ctx, `INSERT INTO probe_runs (upstream_id, batch_id, trigger, policy_version, model) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		u.id, batch, trigger, p.Version, model).Scan(&id); err != nil {
		return 0, err
	}
	if !u.isReference {
		var refID int64
		err := e.DB.QueryRow(ctx, `SELECT id FROM upstreams WHERE is_reference AND platform = $1 AND status IN ('ready', 'listed')
			AND health NOT IN ('unavailable') AND sub2api_account_id IS NOT NULL ORDER BY random() LIMIT 1`, u.platform).Scan(&refID)
		if err == nil {
			if _, err := e.DB.Exec(ctx, `INSERT INTO probe_runs (upstream_id, batch_id, trigger, policy_version, model) VALUES ($1, $2, 'reference', $3, $4)`,
				refID, batch, p.Version, model); err != nil {
				return 0, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	return id, nil
}

// Tick 是一轮质量调度：排队到期的检测、执行排队中的批次、被动分析、回收续报价、补足探测余额。
func (e *Engine) Tick(ctx context.Context) error {
	if err := e.scheduleDue(ctx); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if err := e.runQueued(ctx); err != nil {
		return fmt.Errorf("run queued: %w", err)
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

func (e *Engine) runQueued(ctx context.Context) error {
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
	conc := e.Concurrency
	if conc <= 0 {
		conc = 4
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, b := range batches {
		sem <- struct{}{}
		wg.Add(1)
		go func(batch string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := e.ExecuteBatch(ctx, batch); err != nil {
				e.Log.Error("probe batch", "batch", batch, "err", err)
			}
		}(b)
	}
	wg.Wait()
	return nil
}

type runRow struct {
	id         int64
	upstreamID int64
	trigger    string
	model      string
}

// ExecuteBatch 执行一个批次：先跑基线账号，再用同一批题跑被测上游，最后判定与处置。
func (e *Engine) ExecuteBatch(ctx context.Context, batch string) error {
	rows, err := e.DB.Query(ctx, `UPDATE probe_runs SET status = 'running', started_at = now()
		WHERE batch_id = $1 AND status = 'queued' RETURNING id, upstream_id, trigger, model`, batch)
	if err != nil {
		return err
	}
	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (runRow, error) {
		var r runRow
		err := row.Scan(&r.id, &r.upstreamID, &r.trigger, &r.model)
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
	seed := seedOf(batch)
	for _, t := range targets {
		if err := e.executeTarget(ctx, t, ref, seed); err != nil {
			_, _ = e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'failed', finished_at = now(), summary = $2 WHERE id = $1`,
				t.id, mustJSON(map[string]string{"error": err.Error()}))
			e.Log.Error("probe run failed", "run_id", t.id, "err", err)
		}
	}
	if ref != nil {
		_, _ = e.DB.Exec(ctx, `UPDATE probe_runs SET status = 'done', finished_at = now() WHERE id = $1 AND status = 'running'`, ref.id)
	}
	return nil
}

func seedOf(s string) int64 {
	sum := sha256.Sum256([]byte(s))
	return int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (e *Engine) executeTarget(ctx context.Context, run runRow, ref *runRow, seed int64) error {
	u, err := e.loadUpstream(ctx, run.upstreamID)
	if err != nil {
		return err
	}
	p, err := EffectivePolicy(ctx, e.DB, u.id, u.platform, u.authType)
	if err != nil {
		return err
	}
	// 上架检测是挂单的前提，不受检测开关与每日预算限制；管理员检测与申诉复测同样强制执行
	override := run.trigger == "onboarding" || run.trigger == "admin" || run.trigger == "appeal"
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

	n := p.ItemsPerRun
	if run.trigger == "appeal" {
		n *= 3
	}
	scoring := containsAny(p.Suites, "iq", "reasoning", "long_context")
	items, err := selectItems(ctx, e.DB, u.platform, p.Suites, n*2, seed)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("no active probe items for this platform; add items to the question bank")
	}
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(run.id))) //nolint:gosec // 生成干扰文本，无安全要求
	var outcomes []Outcome
	var assessment Assessment
	for i, it := range items {
		// 先跑完基础题量，之后只有在检验尚无结论时才追加（最多两倍题量）
		if i >= n {
			assessment = Assess(outcomes, p.Thresholds, scoring)
			if assessment.Decided() {
				break
			}
		}
		prompt, needle := expandPrompt(it.Prompt, rng)
		effort := ""
		if it.ReasoningEffort != nil {
			effort = *it.ReasoningEffort
		}
		req := ProbeRequest{Model: run.model, Prompt: prompt, MaxTokens: it.MaxTokens, ReasoningEffort: effort}
		o := Outcome{ItemID: it.ID, Suite: it.Suite}
		if refKey != "" {
			r := e.probe(ctx, refUp.platform, refKey, req)
			pass, _ := grade(it.Grader, r.Text, needle)
			o.Ref, o.RefPass = &r, pass && r.OK()
			if err := e.saveResult(ctx, ref.id, it, r, o.RefPass); err != nil {
				return err
			}
		}
		t := e.probe(ctx, u.platform, targetKey, req)
		pass, _ := grade(it.Grader, t.Text, needle)
		o.Target, o.TargetPass = t, pass && t.OK()
		if hist, ok, err := e.refHistory(ctx, u.platform, it.ID); err != nil {
			return err
		} else if ok {
			o.RefHistRate = &hist
		}
		if err := e.saveResult(ctx, run.id, it, t, o.TargetPass); err != nil {
			return err
		}
		outcomes = append(outcomes, o)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	assessment = Assess(outcomes, p.Thresholds, scoring)
	return e.conclude(ctx, run, u, p, assessment)
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

func (e *Engine) probe(ctx context.Context, platform, key string, req ProbeRequest) ProbeResult {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	return e.Prober.Probe(pctx, platform, key, req)
}

func (e *Engine) saveResult(ctx context.Context, runID int64, it Item, r ProbeResult, passed bool) error {
	excerpt := r.Text
	if len(excerpt) > 500 {
		excerpt = excerpt[:500]
	}
	var status *int
	if r.HTTPStatus > 0 {
		status = &r.HTTPStatus
	}
	var errClass *string
	if r.ErrorClass != "" {
		errClass = &r.ErrorClass
	}
	tps := r.OutputTPS()
	_, err := e.DB.Exec(ctx, `INSERT INTO probe_results (run_id, item_id, suite, passed, http_status, error_class, ttft_ms, duration_ms,
			output_tokens, reasoning_tokens, output_tps, response_model, sub2api_request_id, raw_excerpt)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12, ''), NULLIF($13, ''), $14)`,
		runID, it.ID, it.Suite, passed, status, errClass, int(r.TTFT/time.Millisecond), int(r.Duration/time.Millisecond),
		r.OutputTokens, r.ReasoningTokens, tps, r.ResponseModel, r.Sub2APIRequestID, excerpt)
	return err
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
func (e *Engine) conclude(ctx context.Context, run runRow, u *upstreamRow, p *Policy, a Assessment) error {
	attribution := ""
	if a.Verdict == "degraded" {
		var err error
		if attribution, err = e.attribute(ctx, u, a); err != nil {
			return err
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
		case p.AutoPause:
			newHealth, reason = listing.HealthDegraded, fmt.Sprintf("degraded: relative score %.2f", rel)
			scheduleRecovery()
		default:
			newHealth, reason = listing.HealthSuspected, fmt.Sprintf("degraded (auto pause off): relative score %.2f", rel)
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
	case "suspected":
		if u.health == listing.HealthHealthy || u.health == listing.HealthUnknown {
			newHealth, reason = listing.HealthSuspected, fmt.Sprintf("suspected: relative score %.2f", rel)
		}
	}
	// 上架检测通过但结论不够明确（样本不足以判定）：按疑似处理，降低并发接流量并尽快追加检测
	if run.trigger == "onboarding" && newHealth == listing.HealthUnknown && a.Verdict != "degraded" && a.Verdict != "unavailable" {
		newHealth, reason = listing.HealthSuspected, "onboarding check inconclusive: "+a.Verdict
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
		if _, err := tx.Exec(ctx, `INSERT INTO quality_verdicts (upstream_id, run_id, model, verdict, relative_score, reasoning_ratio, llr, attribution, grade)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, u.id, run.id, run.model, a.Verdict, a.RelativeScore, a.ReasoningRatio, a.LLR, attr, grade); err != nil {
			return err
		}
		if grade != nil && a.Baseline {
			if _, err := tx.Exec(ctx, `UPDATE upstreams SET grade = $2, updated_at = now() WHERE id = $1`, u.id, *grade); err != nil {
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
	summary := e.buildSummary(run, u, a, attribution)
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
		if a.Verdict == "normal" {
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
	if newHealth != healthBefore {
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
