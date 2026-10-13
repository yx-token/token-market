package quality

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/jackc/pgx/v5"
)

var inspectionSuites = []string{"inspection_authenticity", "inspection_capability", "inspection_context", "inspection_function", "inspection_output", "inspection_performance"}

// 限定型号的旧智力题在新流程中默认计入 D07，管理员显式归类优先。
// 只派生运行时归类，不改题库数据或旧套件，保留试运行的原判定与执行语义。
func inspectionItemKey(it Item) string {
	if it.CheckKey == "" && len(it.Models) > 0 && (it.Suite == "iq" || it.Suite == "reasoning") {
		return "D07"
	}
	return it.CheckKey
}

// 正式模式只跳过未限定型号且仍与原定义一致的旧内置题。
func formalItems(items []Item) []Item {
	result := make([]Item, 0, len(items))
	for _, it := range items {
		legacy := false
		if len(it.Models) == 0 && !strings.HasPrefix(it.Suite, "inspection_") {
			for _, old := range defaultItems() {
				if it.Suite == old.Suite && it.Prompt == old.Prompt {
					legacy = true
					break
				}
			}
		}
		if !legacy {
			result = append(result, it)
		}
	}
	return result
}

func (e *Engine) inspectionAssessment(ctx context.Context, run runRow, ref *runRow, u *upstreamRow, plan executionSnapshot, rows []Outcome, st InspectionSettings, a Assessment) (Assessment, error) {
	if plan.Settings != nil {
		st = *plan.Settings
	}
	if plan.InspectionMode == "formal" {
		a = assessFormal(rows, plan.Thresholds, st)
	}
	a.InspectionMode = plan.InspectionMode
	a.InspectionSettings = &st
	for i := range rows {
		rows[i].PrimaryModel = rows[i].RequestedModel == run.model
	}
	capability := inspection.LookupCapability(u.platform, run.model, st)
	opts := InspectOptions{Platform: u.platform, AuthType: u.authType, Capability: &capability}
	checks, gates := Inspect(rows, st, e.now(), opts)
	if plan.InspectionMode == "formal" && a.Verdict == "degraded" {
		d, _ := inspection.Effective("D07", st)
		reason := "有效配对的序贯检验支持能力下降。"
		if ref == nil {
			reason = "按题目判定的序贯检验支持能力低于期望；未与基线对照。"
		}
		gates = append(gates, inspection.Gate{CheckKey: "D07", Code: "sprt_degraded", Health: "degraded", Confirmed: d.Enabled && d.Calibrated, OccurredAt: e.now(), Reason: reason})
	}
	// 正式可用率门槛由 Inspect 的目录样本阈值决定，不沿用旧流程的两请求捷径。
	if err := e.inspectCache(ctx, run, ref, plan, st, checks, capability); err != nil {
		return a, err
	}
	if err := e.inspectRecoveryAvailability(ctx, run, u, st, checks, &gates); err != nil {
		return a, err
	}
	if err := e.inspectProduction(ctx, u, run.model, st, checks, &gates); err != nil {
		return a, err
	}
	if snapshot, err := e.accountSnapshot(ctx, u); err == nil {
		known, low := windowStatus(snapshot, e.now())
		if known {
			c := checks["D05"]
			c.Status = "pass"
			c.Reason = "已取得被动窗口快照。"
			c.Evidence.Code = "window_known"
			if low {
				c.Status = "warning"
				c.Reason = "窗口余量不足，后续检测将顺延。"
			}
			c.Samples.Observed = 1
			checks["D05"] = c
		}
	}
	// 快检继承最近完整质检的未测项，并保留原 checked_at；不能把缺测当修复。
	if run.mode == "quick" {
		var previous []byte
		err := e.DB.QueryRow(ctx, `SELECT v.checks FROM quality_verdicts v JOIN probe_runs r ON r.id=v.run_id WHERE v.upstream_id=$1 AND v.model=$2 AND r.mode='full' AND r.summary->>'inspection_mode'='formal' AND r.execution_snapshot->'inspection_settings'=$3::jsonb ORDER BY v.created_at DESC,v.id DESC LIMIT 1`, u.id, run.model, mustJSON(st)).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return a, err
		}
		if len(previous) > 0 {
			old := map[string]inspection.Check{}
			if err := json.Unmarshal(previous, &old); err != nil {
				return a, err
			}
			for key, c := range checks {
				if c.Status == "not_run" {
					if prior, ok := old[key]; ok && e.now().Sub(prior.CheckedAt) <= 7*24*time.Hour && prior.Applied {
						prior.Evidence.Source = "history"
						checks[key] = prior
					}
				}
			}
		}
	}
	aggregated := inspection.Aggregate(checks, gates, plan.InspectionMode, st)
	a.Checks, a.Gates, a.QualityFactor, a.GroupFactors, a.Provisional = aggregated.Checks, aggregated.Gates, aggregated.QualityFactor, aggregated.GroupFactors, aggregated.Provisional
	if plan.InspectionMode == "formal" {
		// 非门槛扣价不会自动改变健康；未校准的候选门槛只展示。
		a.Verdict = "normal"
		if a.AvailableTotal == 0 || a.AvailableOK == 0 {
			a.Verdict = "inconclusive"
		}
		for _, gate := range a.Gates {
			if gate.Confirmed {
				if gate.Health == "unavailable" {
					a.Verdict = "unavailable"
					break
				}
				a.Verdict = "degraded"
			}
		}
	}
	return a, nil
}

func (e *Engine) inspectCache(ctx context.Context, run runRow, ref *runRow, plan executionSnapshot, st InspectionSettings, checks map[string]inspection.Check, capabilities ...inspection.ModelCapability) error {
	c, ok := checks["D12"]
	if !ok {
		return nil
	}
	d, _ := inspection.Effective("D12", st)
	if !d.Enabled {
		return nil
	}
	for _, it := range plan.Instances {
		if it.CheckKey != "D12" || len(it.Steps) != 2 {
			continue
		}
		if ref == nil {
			t, err := e.cacheTwoSteps(ctx, run.id, it)
			if err != nil {
				return err
			}
			platform := ""
			if plan.Policy != nil {
				platform = plan.Policy.Platform
			}
			capability := inspection.LookupCapability(platform, run.model, st)
			if len(capabilities) > 0 {
				capability = capabilities[0]
			}
			inspectItemCache(&c, t, d, capability.PromptCache)
			checks["D12"] = c
			continue
		}
		t, err := e.cacheTwoSteps(ctx, run.id, it)
		if err != nil {
			return err
		}
		r, err := e.cacheTwoSteps(ctx, ref.id, it)
		if err != nil {
			return err
		}
		if t == nil || r == nil {
			c.Status = "inconclusive"
			c.Factor = nil
			c.Reason = "缓存使用量未提供或两步未完成。"
		} else if r[1] <= 0 {
			c.Status = "not_applicable"
			c.Factor = nil
			c.Reason = "基线自己的第二步未观察到缓存命中。"
		} else {
			c.Status = "pass"
			c.Factor = numberPointer(1)
			c.Samples = inspection.Samples{Observed: 1, Paired: 1}
			c.Evidence.SampleUnit = "item_unit"
			c.Metrics["first_cache_read_tokens"] = metric(float64(t[0]), "tokens")
			c.Metrics["second_cache_read_tokens"] = metric(float64(t[1]), "tokens")
			c.Reason = "同账号第二步观察到缓存命中。"
			if t[1] <= 0 {
				c.Status = "fail"
				c.Factor = numberPointer(d.Coefficients["failure"])
				c.Reason = "基线第二步有缓存命中，被测第二步未命中。"
				c.Evidence.Code = "cache_not_observed"
			}
		}
		checks["D12"] = c
	}
	return nil
}

func (e *Engine) cacheTwoSteps(ctx context.Context, runID int64, it plannedInstance) (*[2]int64, error) {
	var values [2]int64
	for step := 0; step < 2; step++ {
		r, _, _, found, err := e.loadInstanceResult(ctx, runID, it, step)
		if err != nil {
			return nil, err
		}
		if !found || !r.OK() || r.CacheReadTokens == nil {
			return nil, nil
		}
		values[step] = *r.CacheReadTokens
	}
	return &values, nil
}

// 可用率仅消费可归因的生产观测；旧错误日志来源未知时排除，不推成卖家失败。
func (e *Engine) inspectProduction(ctx context.Context, u *upstreamRow, model string, st InspectionSettings, checks map[string]inspection.Check, gates *[]inspection.Gate) error {
	var n, success, dayN, daySuccess, excluded int
	err := e.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE (a.end_reason='completed' AND a.http_status BETWEEN 200 AND 299) OR a.error_class IN ('http_4xx','http_5xx','network','timeout')),
 count(*) FILTER (WHERE a.end_reason='completed' AND a.http_status BETWEEN 200 AND 299),
 count(*) FILTER (WHERE a.started_at>now()-interval '24 hours' AND ((a.end_reason='completed' AND a.http_status BETWEEN 200 AND 299) OR a.error_class IN ('http_4xx','http_5xx','network','timeout'))),
 count(*) FILTER (WHERE a.started_at>now()-interval '24 hours' AND a.end_reason='completed' AND a.http_status BETWEEN 200 AND 299),
 count(*) FILTER (WHERE a.end_reason<>'completed' AND a.error_class NOT IN ('http_4xx','http_5xx','network','timeout'))
 FROM edge_attempts a JOIN listings l ON l.id=a.listing_id
 WHERE l.upstream_id=$1 AND a.billable AND a.started_at>now()-interval '7 days' AND a.end_reason NOT IN ('skipped','client_cancel')`, u.id).Scan(&n, &success, &dayN, &daySuccess, &excluded)
	if err != nil {
		return err
	}
	d, _ := inspection.Effective("D13", st)
	probe := checks["D13"]
	recovering := (u.health == "degraded" || u.health == "unavailable") && probe.Evidence.Source == "recovery_probe"
	if d.Enabled && !recovering && n >= int(d.Thresholds["min_samples"]) && n > 0 {
		c := checks["D13"]
		c.Samples = inspection.Samples{Observed: n + excluded, Paired: n, Excluded: excluded}
		c.Evidence = inspection.Evidence{Source: "production", SampleUnit: "request", WindowStart: timePointer(e.now().Add(-7 * 24 * time.Hour)), WindowEnd: timePointer(e.now()), Code: "production_availability"}
		applyAvailabilityRate(&c, float64(success)/float64(n), d)
		checks["D13"] = c
		if dayN >= int(d.Thresholds["gate_min_samples"]) && float64(daySuccess)/float64(dayN) < d.Thresholds["gate_rate"] {
			*gates = append(*gates, inspection.Gate{CheckKey: "D13", Code: "production_availability_low", Health: "unavailable", Confirmed: d.Calibrated, OccurredAt: e.now(), Reason: "近24小时有效生产请求可用率低于最低要求。"})
		}
	}
	if err := e.inspectAbnormalLimits(ctx, u, st, checks); err != nil {
		return err
	}
	if err := e.inspectPeerPerformance(ctx, u, model, st, checks); err != nil {
		return err
	}
	// usage_records 记录完整生产成功请求的响应型号，按规范化型号核对官方回退。
	var mismatches, total int
	if err := e.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE upstream_model_mismatch IS TRUE),count(*) FROM usage_records WHERE upstream_id=$1 AND model=$2 AND channel IN ('pool','direct') AND created_at>now()-interval '7 days'`, u.id, model).Scan(&mismatches, &total); err != nil {
		return err
	}
	if total > 0 {
		c := checks["A02"]
		c.Evidence.Source = "production"
		c.Samples.Observed += total
		c.Metrics["production_mismatch_rate"] = metric(float64(mismatches)/float64(total), "fraction")
		if mismatches > 0 {
			c.Status = "warning"
			c.Reason = "生产记录包含型号不一致提示，需按官方回退规则复核。"
		}
		checks["A02"] = c
	}
	return nil
}

func timePointer(t time.Time) *time.Time { return &t }

// 第三方也是可复测的质检项；缺失、失败或疑似结果不能作为身份修复的证据。
func addThirdPartyInspection(a *Assessment, results []tpResult, now time.Time) {
	if len(results) == 0 || a.InspectionSettings == nil {
		return
	}
	st := *a.InspectionSettings
	d, _ := inspection.Effective("third_party", st)
	c := newInspectionCheck(d, now)
	c.Evidence.Source = "third_party"
	c.Evidence.SampleUnit = "item_unit"
	c.Status, c.Reason = "pass", "本次第三方核验未见身份异常。"
	for _, r := range results {
		c.Samples.Observed++
		switch r.Verdict {
		case "degraded":
			c.Status, c.Reason = "fail", "第三方报告确认型号身份异常。"
		case "suspected":
			if c.Status != "fail" {
				c.Status, c.Reason = "warning", "第三方报告提示身份疑似异常，需重新核验。"
			}
		case "normal":
			c.Samples.Paired++
		default:
			c.Samples.Excluded++
			if c.Status == "pass" {
				c.Status, c.Reason = "inconclusive", "第三方核验未完成或证据不足。"
			}
		}
	}
	if !d.Enabled {
		c.Status, c.Reason = "not_applicable", "此项规则未启用。"
	}
	a.Checks["third_party"] = c
	if c.Status == "fail" {
		a.Gates = append(a.Gates, inspection.Gate{CheckKey: "third_party", Code: "identity_mismatch", Health: "degraded", Confirmed: d.Enabled && d.Calibrated, OccurredAt: now, Reason: c.Reason})
	}
	aggregated := inspection.Aggregate(a.Checks, a.Gates, a.InspectionMode, st)
	a.Checks, a.Gates, a.QualityFactor, a.GroupFactors, a.Provisional = aggregated.Checks, aggregated.Gates, aggregated.QualityFactor, aggregated.GroupFactors, aggregated.Provisional
}

func (e *Engine) finalizeInspection(ctx context.Context, run runRow, u *upstreamRow, a *Assessment) (inspection.GradeDecision, []string, error) {
	st := defaultInspectionSettings()
	if a.InspectionSettings != nil {
		st = *a.InspectionSettings
	}
	var previous, pending *string
	var count int
	err := e.DB.QueryRow(ctx, `SELECT u.grade,s.pending_grade,COALESCE(s.pending_grade_count,0) FROM upstreams u LEFT JOIN upstream_quality_state s ON s.upstream_id=u.id WHERE u.id=$1`, u.id).Scan(&previous, &pending, &count)
	if err != nil {
		return inspection.GradeDecision{}, nil, err
	}
	current, next := "", ""
	if previous != nil {
		current = *previous
	}
	if pending != nil {
		next = *pending
	}
	history := inspection.GradeHistory{FirstListing: run.trigger == "onboarding"}
	var first, lastGate *time.Time
	var days int
	var available float64
	err = e.DB.QueryRow(ctx, `SELECT min(v.created_at),max(v.created_at) FILTER (WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(v.gates) gate WHERE gate->>'confirmed'='true')),
 COALESCE(max((v.checks->'D13'->'metrics'->'success_rate'->>'value')::float8),0),
 count(DISTINCT date_trunc('day',v.created_at)) FILTER (WHERE v.checks->'D13'->'evidence'->>'source'='production' AND v.created_at>now()-interval '7 days')
 FROM quality_verdicts v JOIN probe_runs r ON r.id=v.run_id WHERE v.upstream_id=$1 AND r.summary->>'inspection_mode'='formal'`, u.id).Scan(&first, &lastGate, &available, &days)
	if err != nil {
		return inspection.GradeDecision{}, nil, err
	}
	if first != nil {
		since := *first
		if lastGate != nil && lastGate.After(since) {
			since = *lastGate
		}
		history.GateFreeDays = int(e.now().Sub(since).Hours() / 24)
	}
	history.AvailabilityDays, history.Availability = days, -1
	for _, gate := range a.Gates {
		if gate.Confirmed {
			history.GateFreeDays = 0
		}
	}
	_ = available
	// S 使用最近窗口的可用率，不使用历史最大值。
	if c, ok := a.Checks["D13"]; ok && c.Evidence.Source == "production" {
		if m, ok := c.Metrics["success_rate"]; ok && m.Value != nil {
			history.Availability = *m.Value
		}
	}
	formal := st
	formal.Mode = "formal"
	proposed := inspection.Grade(a.QualityFactor, current, next, count, history, formal)
	a.ProposedGrade = proposed.Grade
	repaired := 1.0
	repairedDecision := inspection.Grade(&repaired, "", "", 0, history, formal)
	a.RepairedGrade = repairedDecision.Grade
	decision := inspection.Grade(a.QualityFactor, current, next, count, history, st)
	if a.InspectionMode == "formal" {
		a.Grade = decision.Grade
	}
	failed := []string{}
	for key, c := range a.Checks {
		if c.Applied && (c.Status == "fail" || c.Status == "warning" && (c.Factor != nil && *c.Factor < 1)) {
			failed = append(failed, key)
		}
	}
	for _, gate := range a.Gates {
		if gate.Confirmed && !containsAny(failed, gate.CheckKey) {
			failed = append(failed, gate.CheckKey)
		}
	}
	var base []byte
	if err := e.DB.QueryRow(ctx, `SELECT appraisal_base FROM vendor_configs WHERE platform=$1`, u.platform).Scan(&base); err != nil {
		return decision, failed, err
	}
	values := map[string]float64{}
	if err := json.Unmarshal(base, &values); err != nil {
		return decision, failed, err
	}
	tier := ""
	if u.tier != nil {
		tier = *u.tier
	}
	at := e.now()
	appraisal := inspection.Appraise(values, tier, a.QualityFactor, &at, a.InspectionMode != "formal", a.Provisional)
	a.Appraisal = &appraisal
	return decision, failed, nil
}

// 正式恢复必须重新覆盖旧失败项；没有覆盖或仍失败不能累计恢复次数。
func (e *Engine) inspectionRecoveryComplete(ctx context.Context, u *upstreamRow, a Assessment) (bool, error) {
	var failed []string
	err := e.DB.QueryRow(ctx, `SELECT last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&failed)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, key := range failed {
		c, ok := a.Checks[key]
		if !ok || c.Status != "pass" || c.Evidence.Source == "history" {
			return false, nil
		}
	}
	return true, nil
}

func validInspectionQ(q *float64) bool {
	return q != nil && !math.IsNaN(*q) && !math.IsInf(*q, 0) && *q >= .5 && *q <= 1.05
}

func hasAppliedDiscount(checks map[string]inspection.Check) bool {
	for _, check := range checks {
		if check.Applied && check.Factor != nil && *check.Factor < 1 {
			return true
		}
	}
	return false
}

func (e *Engine) inspectAbnormalLimits(ctx context.Context, u *upstreamRow, st InspectionSettings, checks map[string]inspection.Check) error {
	d, _ := inspection.Effective("D04", st)
	if !d.Enabled {
		return nil
	}
	if checks["D04"].Evidence.Source == "recovery_probe" {
		return nil
	}
	var total, limited, fresh, freshLimited int
	var lastGate *time.Time
	err := e.DB.QueryRow(ctx, `SELECT max(created_at) FROM quality_verdicts WHERE upstream_id=$1 AND EXISTS (SELECT 1 FROM jsonb_array_elements(gates) g WHERE g->>'confirmed'='true')`, u.id).Scan(&lastGate)
	if err != nil {
		return err
	}
	err = e.DB.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE 'abnormal_rate_limited'=ANY(pr.flags)),
 count(*) FILTER (WHERE pr.created_at>$2),count(*) FILTER (WHERE pr.created_at>$2 AND 'abnormal_rate_limited'=ANY(pr.flags))
 FROM probe_results pr JOIN probe_runs r ON r.id=pr.run_id
 WHERE r.upstream_id=$1 AND pr.created_at>now()-interval '7 days' AND 'window_available'=ANY(pr.flags)
 AND (pr.error_class IS NULL OR 'abnormal_rate_limited'=ANY(pr.flags))`, u.id, lastGate).Scan(&total, &limited, &fresh, &freshLimited)
	if err != nil {
		return err
	}
	if total < int(d.Thresholds["min_samples"]) || total == 0 {
		return nil
	}
	c := checks["D04"]
	// 恢复时只使用门槛之后足够的已核实窗口新样本，旧失败窗口不能永久阻塞复检。
	if (u.health == "degraded" || u.health == "unavailable") && fresh >= int(d.Thresholds["min_samples"]) {
		total, limited = fresh, freshLimited
		c.Evidence.Source = "paired_probe"
		c.Evidence.WindowStart = lastGate
	} else {
		c.Evidence.Source = "history"
		c.Evidence.WindowStart = timePointer(e.now().Add(-7 * 24 * time.Hour))
	}
	c.Samples = inspection.Samples{Observed: total}
	c.Evidence.SampleUnit = "request"
	c.Evidence.WindowEnd = timePointer(e.now())
	rate := float64(limited) / float64(total)
	c.Metrics["abnormal_429_rate"] = metric(rate, "fraction")
	c.Status = "pass"
	c.Factor = numberPointer(1)
	c.Reason = "已核实窗口的请求未见频繁异常限流。"
	if rate > d.Thresholds["severe_rate"] {
		c.Status = "fail"
		c.Factor = numberPointer(d.Coefficients["severe"])
	} else if rate >= d.Thresholds["warning_rate"] {
		c.Status = "warning"
		c.Factor = numberPointer(d.Coefficients["warning"])
	}
	if c.Status != "pass" {
		c.Reason = "窗口未耗尽时仍观察到异常限流。"
		c.Evidence.Code = "abnormal_429"
	}
	checks["D04"] = c
	return nil
}

// 同型号同行按上游分别聚合后比较，防止单个高流量账号支配分位数。
func (e *Engine) inspectPeerPerformance(ctx context.Context, u *upstreamRow, model string, st InspectionSettings, checks map[string]inspection.Check) error {
	d, _ := inspection.Effective("D10", st)
	if !d.Enabled {
		return nil
	}
	rows, err := e.DB.Query(ctx, `SELECT r.upstream_id,percentile_cont(.5) WITHIN GROUP (ORDER BY r.first_token_ms)::float8,
 percentile_cont(.5) WITHIN GROUP (ORDER BY r.output_tokens::float8/NULLIF((r.duration_ms-r.first_token_ms)::float8/1000,0))::float8,count(*)
 FROM usage_records r JOIN upstreams x ON x.id=r.upstream_id
 WHERE x.platform=$1 AND r.model=$2 AND r.channel IN ('pool','direct') AND r.first_token_ms>0 AND r.duration_ms>r.first_token_ms AND r.output_tokens>0 AND r.created_at>now()-interval '7 days'
 GROUP BY r.upstream_id HAVING count(*) >= $3`, u.platform, model, int(d.Thresholds["min_pairs"]))
	if err != nil {
		return err
	}
	defer rows.Close()
	var ownT, ownP float64
	var n int
	ts, ps := []float64{}, []float64{}
	for rows.Next() {
		var id int64
		var t, p float64
		var count int
		if err := rows.Scan(&id, &t, &p, &count); err != nil {
			return err
		}
		if id == u.id {
			ownT, ownP, n = t, p, count
		} else {
			ts = append(ts, t)
			ps = append(ps, p)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ts) < 4 || ownT <= 0 || ownP <= 0 {
		return nil
	}
	c := checks["D10"]
	c.Samples = inspection.Samples{Observed: n}
	c.Evidence.Source = "production"
	c.Evidence.SampleUnit = "request"
	c.Status = "pass"
	c.Factor = numberPointer(1)
	c.Reason = "同型号同行性能处于当前正常范围。"
	t25, _ := percentile(ts, .25)
	t50, _ := percentile(ts, .5)
	p50, _ := percentile(ps, .5)
	p75, _ := percentile(ps, .75)
	slow := math.Max(ownT/t50, p50/ownP)
	switch {
	case slow >= 3:
		c.Status = "fail"
		c.Factor = numberPointer(d.Coefficients["worst"])
	case slow >= d.Thresholds["severe_ratio"]:
		c.Status = "warning"
		c.Factor = numberPointer(d.Coefficients["severe"])
	case slow >= d.Thresholds["warning_ratio"]:
		c.Status = "warning"
		c.Factor = numberPointer(d.Coefficients["warning"])
	case ownT <= t25 && ownP >= p75:
		c.Factor = numberPointer(d.Coefficients["bonus"])
	}
	c.Metrics["ttft_p50_ms"] = metric(ownT, "ms")
	c.Metrics["output_tps"] = metric(ownP, "tokens_per_second")
	c.Metrics["peer_upstreams"] = metric(float64(len(ts)), "count")
	checks["D10"] = c
	return nil
}

// 只比较同模板版本与冻结难度参数的基线历史，换题不能自动归为官方下降。
func (e *Engine) attributeInspection(ctx context.Context, run runRow, u *upstreamRow, a Assessment) (string, error) {
	var compared, declined int
	err := e.DB.QueryRow(ctx, `WITH current AS (
 SELECT pr.item_id,pr.generator_version,pr.passed,instance->'parameters' AS parameters
 FROM probe_runs reference JOIN probe_results pr ON pr.run_id=reference.id
 JOIN probe_runs target ON target.batch_id=reference.batch_id AND target.trigger<>'reference'
 CROSS JOIN LATERAL jsonb_array_elements(target.execution_snapshot->'instances') instance
 WHERE target.id=$1 AND reference.trigger='reference' AND pr.check_key IN ('D07','D09') AND pr.error_class IS NULL
 AND NOT (pr.flags && ARRAY['refusal','truncated','stop_error'])
 AND (instance->>'item_id')::bigint=pr.item_id AND (instance->>'instance_idx')::int=pr.instance_idx
 ), historical AS (
 SELECT pr.item_id,pr.generator_version,instance->'parameters' AS parameters,count(*) AS n,avg(CASE WHEN pr.passed THEN 1 ELSE 0 END) AS rate
 FROM probe_runs reference JOIN probe_results pr ON pr.run_id=reference.id
 JOIN probe_runs target ON target.batch_id=reference.batch_id AND target.trigger<>'reference'
 CROSS JOIN LATERAL jsonb_array_elements(target.execution_snapshot->'instances') instance
 JOIN upstreams x ON x.id=reference.upstream_id
 WHERE reference.trigger='reference' AND x.platform=$2 AND x.auth_type=$3 AND x.tier IS NOT DISTINCT FROM $4::text AND reference.model=$5
 AND pr.created_at BETWEEN now()-interval '7 days' AND now()-interval '3 hours' AND pr.error_class IS NULL
 AND NOT (pr.flags && ARRAY['refusal','truncated','stop_error'])
 AND (instance->>'item_id')::bigint=pr.item_id AND (instance->>'instance_idx')::int=pr.instance_idx
 GROUP BY pr.item_id,pr.generator_version,instance->'parameters' HAVING count(*)>=5
 ) SELECT count(*),count(*) FILTER(WHERE h.rate>=.8 AND c.passed IS FALSE) FROM current c JOIN historical h
 ON c.item_id=h.item_id AND c.generator_version=h.generator_version AND c.parameters=h.parameters`, run.id, u.platform, u.authType, u.tier, run.model).Scan(&compared, &declined)
	if err != nil {
		return "", err
	}
	if compared >= 5 && float64(declined)/float64(compared) > .2 {
		return "vendor_wide", nil
	}
	if u.proxyID != nil {
		var peers int
		err := e.DB.QueryRow(ctx, `SELECT count(DISTINCT v.upstream_id) FROM quality_verdicts v JOIN upstreams x ON x.id=v.upstream_id WHERE x.proxy_id=$1 AND x.id<>$2 AND v.verdict='degraded' AND v.created_at>now()-interval '24 hours'`, *u.proxyID, u.id).Scan(&peers)
		if err != nil {
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
