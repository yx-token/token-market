//go:build integration

package quality

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
	"github.com/yx-token/token-market/market/internal/listing"
	"github.com/yx-token/token-market/market/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func detectionStateFixture(t *testing.T) (*pgxpool.Pool, *Engine, runRow, *Policy, InspectionSettings) {
	t.Helper()
	pool, run, _ := inspectionDB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET status='ready',health='healthy',sub2api_account_id=1 WHERE id=$1`, run.upstreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vendor_configs SET appraisal_base='{"default":0.4}' WHERE platform='anthropic'`); err != nil {
		t.Fatal(err)
	}
	st := defaultInspectionSettings()
	pricing := true
	st.CheckOverrides["D07"] = inspection.CheckOverride{Pricing: &pricing}
	if _, err := inspection.Update(ctx, pool, mustJSON(st)); err != nil {
		t.Fatal(err)
	}
	p := inspectionTestPolicy(run)
	p.Enabled = true
	p.Flows.Intelligence.Triggers = append(p.Flows.Intelligence.Triggers, "adaptive", "seller_manual", "appeal")
	p.Flows.Identity.Triggers = append(p.Flows.Identity.Triggers, "seller_manual", "appeal")
	if _, err := pool.Exec(ctx, `UPDATE detection_policies SET enabled=true,flows=$1,third_party='[]' WHERE platform='anthropic'`, mustJSON(p.Flows)); err != nil {
		t.Fatal(err)
	}
	e := &Engine{DB: pool, Settings: func(context.Context) (settings.Settings, error) { return settings.Defaults(), nil }}
	return pool, e, run, p, st
}

func detectionStateAssessment(st InspectionSettings, key, status string, at time.Time) Assessment {
	d, _ := inspection.Effective(key, st)
	c := newInspectionCheck(d, at)
	c.Status = status
	c.Samples.Observed = 100
	c.Evidence.Source = "item_probe"
	result := inspection.Aggregate(map[string]inspection.Check{key: c}, nil, st.Mode, st)
	return Assessment{AvailableOK: 100, AvailableTotal: 100, InspectionMode: st.Mode, InspectionSettings: &st,
		Checks: result.Checks, Gates: result.Gates, QualityFactor: result.QualityFactor, GroupFactors: result.GroupFactors, Provisional: result.Provisional}
}

func concludeDetectionState(t *testing.T, e *Engine, run runRow, p *Policy, a Assessment) {
	t.Helper()
	u, err := e.loadUpstream(context.Background(), run.upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.conclude(context.Background(), run, u, p, a, nil); err != nil {
		t.Fatal(err)
	}
}

func queueDetectionState(t *testing.T, e *Engine, previous runRow, trigger, flow string) runRow {
	t.Helper()
	id, err := e.QueueRun(context.Background(), previous.upstreamID, trigger, flow)
	if err != nil {
		t.Fatal(err)
	}
	run := previous
	run.id, run.trigger = id, trigger
	if err := e.DB.QueryRow(context.Background(), `SELECT batch_id,model,mode,flows,COALESCE(identity_source,''),models FROM probe_runs WHERE id=$1`, id).
		Scan(&run.batch, &run.model, &run.mode, &run.flows, &run.identitySource, &run.models); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestDetectionFlowStateAdaptiveBackoffAndIndependentRecovery(t *testing.T) {
	t.Parallel()
	pool, e, run, p, st := detectionStateFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	e.Now = func() time.Time { return now }
	run.flows, run.identitySource = []string{"intelligence"}, ""
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['intelligence'],identity_source=NULL WHERE id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	// 同一疑似持续失败时逐次退避，超过第五次仍保持六小时上限。
	for attempt, delay := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour, 6 * time.Hour} {
		if attempt > 0 {
			run = queueDetectionState(t, e, run, "adaptive", "intelligence")
		}
		concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "fail", now))
		var health, verdict string
		var suspected, failed []string
		var attempts int
		var next time.Time
		var q float64
		if err := pool.QueryRow(ctx, `SELECT u.health,u.quality_factor,s.suspected_flows,s.adaptive_attempts,s.last_failed_checks,s.next_intelligence_at,r.summary->>'verdict'
			FROM upstreams u JOIN upstream_quality_state s ON s.upstream_id=u.id JOIN probe_runs r ON r.id=$2 WHERE u.id=$1`, run.upstreamID, run.id).
			Scan(&health, &q, &suspected, &attempts, &failed, &next, &verdict); err != nil || health != listing.HealthSuspected || verdict != "suspected" ||
			!reflect.DeepEqual(suspected, []string{"intelligence"}) || !reflect.DeepEqual(failed, []string{"D07"}) || attempts != attempt+1 || !next.Equal(now.Add(delay)) || math.Abs(q-.7) > 1e-9 {
			t.Fatalf("测智疑似退避或试运行扣价错误：次数=%d 健康=%s 结论=%s 疑似=%v 待复测=%v 下次=%s Q=%g 错误=%v", attempts, health, verdict, suspected, failed, next, q, err)
		}
		now = now.Add(10 * time.Minute)
	}
	var nextBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT next_intelligence_at FROM upstream_quality_state WHERE upstream_id=$1`, run.upstreamID).Scan(&nextBefore); err != nil {
		t.Fatal(err)
	}
	run = queueDetectionState(t, e, run, "admin", "identity")
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "A04", "pass", now))
	var health, verdict string
	var suspected, failed []string
	var attempts int
	var nextAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT u.health,s.suspected_flows,s.adaptive_attempts,s.last_failed_checks,s.next_intelligence_at,r.summary->>'verdict'
		FROM upstreams u JOIN upstream_quality_state s ON s.upstream_id=u.id JOIN probe_runs r ON r.id=$2 WHERE u.id=$1`, run.upstreamID, run.id).
		Scan(&health, &suspected, &attempts, &failed, &nextAfter, &verdict); err != nil || health != listing.HealthSuspected || verdict != "normal" ||
		!reflect.DeepEqual(suspected, []string{"intelligence"}) || !reflect.DeepEqual(failed, []string{"D07"}) || attempts != 6 || !nextAfter.Equal(nextBefore) {
		t.Fatalf("另一流程正常不能清测智疑似或改其排期：健康=%s 结论=%s 疑似=%v 待复测=%v 次数=%d 下次=%s 错误=%v", health, verdict, suspected, failed, attempts, nextAfter, err)
	}
	// 白名单没有测智模型时，实际入队为空交集，并解除不再适用的测智疑似。
	if _, err := pool.Exec(ctx, `UPDATE upstreams SET model_whitelist=ARRAY['未配置测智的挂售型号'] WHERE id=$1`, run.upstreamID); err != nil {
		t.Fatal(err)
	}
	run = queueDetectionState(t, e, run, "admin", "intelligence")
	if len(run.models) != 0 {
		t.Fatalf("空交集仍入队测智模型：%v", run.models)
	}
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "not_applicable", now))
	if err := pool.QueryRow(ctx, `SELECT u.health,s.suspected_flows,s.adaptive_attempts,s.last_failed_checks,r.summary->'intelligence'->>'status'
		FROM upstreams u JOIN upstream_quality_state s ON s.upstream_id=u.id JOIN probe_runs r ON r.id=$2 WHERE u.id=$1`, run.upstreamID, run.id).
		Scan(&health, &suspected, &attempts, &failed, &verdict); err != nil || health != listing.HealthHealthy || len(suspected) != 0 || attempts != 0 || len(failed) != 0 || verdict != "not_applicable" {
		t.Fatalf("不再适用的测智疑似未解除：健康=%s 状态=%s 疑似=%v 待复测=%v 次数=%d 错误=%v", health, verdict, suspected, failed, attempts, err)
	}
}

func TestDetectionFlowStateUnobservedFailureKeepsFullRetestAndBackoff(t *testing.T) {
	t.Parallel()
	for _, history := range []bool{false, true} {
		t.Run(map[bool]string{false: "失败项本次未测", true: "沿用通过不能自证修复"}[history], func(t *testing.T) {
			pool, e, run, p, st := detectionStateFixture(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Second)
			e.Now = func() time.Time { return now }
			run.flows, run.identitySource = []string{"intelligence"}, ""
			concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D09", "fail", now))
			run = queueDetectionState(t, e, run, "adaptive", "intelligence")
			a := detectionStateAssessment(st, "D07", "pass", now)
			c := newInspectionCheck(mustEffective("D09", st), now)
			if history {
				c.Status, c.Evidence.Source = "pass", "history"
			}
			a.Checks["D09"] = c
			concludeDetectionState(t, e, run, p, a)
			var failed, suspected []string
			var next time.Time
			var attempts int
			if err := pool.QueryRow(ctx, `SELECT last_failed_checks,suspected_flows,next_intelligence_at,adaptive_attempts FROM upstream_quality_state WHERE upstream_id=$1`, run.upstreamID).Scan(&failed, &suspected, &next, &attempts); err != nil || !reflect.DeepEqual(failed, []string{"D09"}) || !reflect.DeepEqual(suspected, []string{"intelligence"}) || !next.Equal(now.Add(time.Hour)) || attempts != 2 {
				t.Fatalf("未重测的失败项须保留并继续退避：失败=%v 疑似=%v 下次=%s 次数=%d 错误=%v", failed, suspected, next, attempts, err)
			}
			run = queueDetectionState(t, e, run, "adaptive", "intelligence")
			if run.mode != "full" {
				t.Fatalf("未修复 D09 的下轮不能退回快检：%s", run.mode)
			}
		})
	}
}

func detectionStateAppeal(t *testing.T, pool *pgxpool.Pool, run runRow, status string) int64 {
	t.Helper()
	ctx := context.Background()
	var reportID, appealID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM test_reports WHERE run_id=$1`, run.id).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO appeals(report_id,seller_id,reason,status) SELECT $1,seller_id,'核对检测证据',$3 FROM upstreams WHERE id=$2 RETURNING id`, reportID, run.upstreamID, status).Scan(&appealID); err != nil {
		t.Fatal(err)
	}
	return appealID
}

func TestDetectionFlowStateAutomaticAppealKeepsCurrentAppraisal(t *testing.T) {
	t.Parallel()
	for _, started := range []bool{true, false} {
		t.Run(map[bool]string{true: "以开始时间清旧证据", false: "未开始时使用入队时间"}[started], func(t *testing.T) {
			pool, e, run, p, st := detectionStateFixture(t)
			ctx := context.Background()
			run.flows, run.identitySource = []string{"intelligence"}, ""
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['intelligence'],identity_source=NULL WHERE id=$1`, run.id); err != nil {
				t.Fatal(err)
			}
			concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "fail", e.now()))
			appealID := detectionStateAppeal(t, pool, run, "retesting")
			oldRun := run
			anchor := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
			if _, err := pool.Exec(ctx, `UPDATE quality_verdicts SET created_at=$2 WHERE run_id=$1`, oldRun.id, anchor.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			run = queueDetectionState(t, e, run, "appeal", "intelligence")
			var startedAt *time.Time
			queuedAt := anchor
			if started {
				startedAt = &anchor
				queuedAt = anchor.Add(-time.Minute)
			}
			if _, err := pool.Exec(ctx, `UPDATE probe_runs SET queued_at=$2,started_at=$3 WHERE id=$1`, run.id, queuedAt, startedAt); err != nil {
				t.Fatal(err)
			}
			concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "pass", e.now()))
			var appealStatus, health string
			var reset time.Time
			var suspected, failed []string
			if err := pool.QueryRow(ctx, `SELECT a.status,u.health,s.deductions_reset_at,s.suspected_flows,s.last_failed_checks
				FROM appeals a JOIN test_reports t ON t.id=a.report_id JOIN upstreams u ON u.id=t.upstream_id JOIN upstream_quality_state s ON s.upstream_id=u.id WHERE a.id=$1`, appealID).
				Scan(&appealStatus, &health, &reset, &suspected, &failed); err != nil || appealStatus != "overturned" || health != listing.HealthHealthy || !reset.Equal(anchor) || len(suspected) != 0 || len(failed) != 0 {
				t.Fatalf("自动申诉推翻的清理边界错误：申诉=%s 健康=%s 边界=%s 疑似=%v 待复测=%v 错误=%v", appealStatus, health, reset, suspected, failed, err)
			}
			views, err := listing.LoadInspectionViews(ctx, pool, []int64{run.upstreamID})
			view := views[run.upstreamID]
			if err != nil || view.QualityFactor == nil || *view.QualityFactor != 1 || view.LastCheckedAt == nil || !view.LastCheckedAt.After(reset) ||
				view.Appraisal.Factor == nil || *view.Appraisal.Factor != 1 || view.Appraisal.Multiplier == nil || math.Abs(*view.Appraisal.Multiplier-.4) > 1e-9 || view.QualityChecks["D07"].Status != "pass" {
				t.Fatalf("自动申诉不能抹去本轮通过估价：摘要=%+v 错误=%v", view, err)
			}
		})
	}
}

func TestDetectionFlowStateManualAppealResetsPriceUntilNewEvidence(t *testing.T) {
	t.Parallel()
	pool, e, run, p, st := detectionStateFixture(t)
	ctx := context.Background()
	run.flows, run.identitySource = []string{"intelligence"}, ""
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['intelligence'],identity_source=NULL WHERE id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "fail", e.now()))
	appealID := detectionStateAppeal(t, pool, run, "open")
	// 原扣价确实已对估价生效，避免只验证默认未检测状态。
	before, err := listing.LoadInspectionViews(ctx, pool, []int64{run.upstreamID})
	if err != nil || before[run.upstreamID].QualityFactor == nil || math.Abs(*before[run.upstreamID].QualityFactor-.7) > 1e-9 {
		t.Fatalf("人工推翻前没有有效扣价：%+v %v", before, err)
	}
	resetAt := time.Now().UTC().Truncate(time.Microsecond)
	e.Now = func() time.Time { return resetAt }
	appeal, err := e.ResolveAppeal(ctx, appealID, "overturn", "原证据不足，等待新检测")
	if err != nil || appeal.Status != "overturned" {
		t.Fatalf("人工推翻失败：%+v %v", appeal, err)
	}
	var q float64
	var appraisedAt *time.Time
	var reset time.Time
	var health string
	var suspected, failed []string
	if err := pool.QueryRow(ctx, `SELECT u.quality_factor,u.appraised_at,u.health,s.deductions_reset_at,s.suspected_flows,s.last_failed_checks
		FROM upstreams u JOIN upstream_quality_state s ON s.upstream_id=u.id WHERE u.id=$1`, run.upstreamID).
		Scan(&q, &appraisedAt, &health, &reset, &suspected, &failed); err != nil || q != 1 || appraisedAt != nil || health != listing.HealthHealthy || !reset.Equal(resetAt) || len(suspected) != 0 || len(failed) != 0 {
		t.Fatalf("人工推翻未同步清旧扣价：Q=%g 估价时间=%v 健康=%s 边界=%s 疑似=%v 待复测=%v 错误=%v", q, appraisedAt, health, reset, suspected, failed, err)
	}
	after, err := listing.LoadInspectionViews(ctx, pool, []int64{run.upstreamID})
	view := after[run.upstreamID]
	if err != nil || view.QualityFactor != nil || view.LastCheckedAt != nil || view.Appraisal.CheckedAt != nil || view.Appraisal.Multiplier != nil || len(view.QualityChecks) != 0 {
		t.Fatalf("人工推翻后仍展示旧检测估价：%+v %v", view, err)
	}
	var historicalReports int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM test_reports WHERE upstream_id=$1`, run.upstreamID).Scan(&historicalReports); err != nil || historicalReports != 1 {
		t.Fatalf("人工推翻不能删除历史报告：%d %v", historicalReports, err)
	}
	run = queueDetectionState(t, e, run, "admin", "intelligence")
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D07", "pass", e.now()))
	latest, err := listing.LoadInspectionViews(ctx, pool, []int64{run.upstreamID})
	view = latest[run.upstreamID]
	if err != nil || view.QualityFactor == nil || *view.QualityFactor != 1 || view.LastCheckedAt == nil || !view.LastCheckedAt.After(reset) || view.Appraisal.Multiplier == nil || math.Abs(*view.Appraisal.Multiplier-.4) > 1e-9 {
		t.Fatalf("新检测通过后没有恢复估价：%+v %v", view, err)
	}
}

func TestDetectionFlowStateAppliedDiscountAllowsHealthyManualRetest(t *testing.T) {
	t.Parallel()
	pool, e, run, p, st := detectionStateFixture(t)
	ctx := context.Background()
	pricing := true
	st.CheckOverrides = map[string]inspection.CheckOverride{"D08": {Pricing: &pricing}}
	if _, err := inspection.Update(ctx, pool, mustJSON(st)); err != nil {
		t.Fatal(err)
	}
	run.flows = []string{"identity"}
	if _, err := pool.Exec(ctx, `UPDATE probe_runs SET flows=ARRAY['identity'] WHERE id=$1`, run.id); err != nil {
		t.Fatal(err)
	}
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D08", "fail", e.now()))
	var health, verdict string
	var applied bool
	var q float64
	if err := pool.QueryRow(ctx, `SELECT u.health,u.quality_factor,r.summary->>'verdict',(r.summary->'checks'->'D08'->>'applied')::boolean
		FROM upstreams u JOIN probe_runs r ON r.id=$2 WHERE u.id=$1`, run.upstreamID, run.id).Scan(&health, &q, &verdict, &applied); err != nil || health != listing.HealthHealthy || verdict != "normal" || !applied || q >= 1 {
		t.Fatalf("扣价夹具必须健康正常且实际生效：健康=%s 结论=%s 生效=%v Q=%g 错误=%v", health, verdict, applied, q, err)
	}
	u, err := listing.GetUpstream(ctx, pool, run.upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := e.ManualRetestEligibility(ctx, u)
	if err != nil || !eligibility.Allowed || eligibility.Code != "" {
		t.Fatalf("健康正常但扣价已生效也应允许复测：%+v %v", eligibility, err)
	}
	run = queueDetectionState(t, e, run, "seller_manual", "identity")
	concludeDetectionState(t, e, run, p, detectionStateAssessment(st, "D08", "pass", e.now()))
	u, err = listing.GetUpstream(ctx, pool, run.upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err = e.ManualRetestEligibility(ctx, u)
	if err != nil || eligibility.Allowed || eligibility.Code != "RETEST_NOT_NEEDED" {
		t.Fatalf("通过且没有扣价后应拒绝无必要复测：%+v %v", eligibility, err)
	}
}
