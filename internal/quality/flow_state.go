package quality

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
	"github.com/DueGin/token-market/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

var detectionFlows = []string{inspection.FlowIntelligence, inspection.FlowIdentity}
var adaptiveBackoff = []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour}

func flowPolicy(p *Policy, flow string) FlowPolicy {
	if flow == inspection.FlowIdentity {
		return p.Flows.Identity.FlowPolicy
	}
	return p.Flows.Intelligence.FlowPolicy
}

func intelligenceModels(p *Policy, whitelist []string) []string {
	out := []string{}
	for _, m := range p.Flows.Intelligence.Models {
		if whitelist == nil || slices.Contains(whitelist, m.Model) {
			out = append(out, m.Model)
		}
	}
	return out
}

// 单次运行只冻结目标流程；异常流程复检不能顺带清除另一流程的疑似。
func (e *Engine) selectedFlows(ctx context.Context, u *upstreamRow, p *Policy, trigger string, requested []string) ([]string, error) {
	for _, flow := range requested {
		if !slices.Contains(detectionFlows, flow) {
			return nil, httpx.BadRequest("INVALID_FLOW", "未知检测流程")
		}
	}
	if len(requested) == 0 && (trigger == "seller_manual" || trigger == "recovery" || trigger == "adaptive") {
		var suspected, failed []string
		err := e.DB.QueryRow(ctx, `SELECT suspected_flows,last_failed_checks FROM upstream_quality_state WHERE upstream_id=$1`, u.id).Scan(&suspected, &failed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		requested = append(requested, suspected...)
		if trigger != "adaptive" {
			for _, key := range failed {
				if d, ok := inspection.Lookup(key); ok {
					flow := d.Flow
					if flow == inspection.FlowCommon {
						flow = inspection.FlowIntelligence
					}
					if !slices.Contains(requested, flow) {
						requested = append(requested, flow)
					}
				}
			}
		}
		if len(requested) == 0 && trigger == "seller_manual" {
			_ = e.DB.QueryRow(ctx, `SELECT flows FROM probe_runs WHERE upstream_id=$1 AND status='failed' ORDER BY id DESC LIMIT 1`, u.id).Scan(&requested)
		}
	}
	out := []string{}
	for _, flow := range detectionFlows {
		policy := flowPolicy(p, flow)
		if !policy.Enabled {
			continue
		}
		if len(requested) > 0 {
			if slices.Contains(requested, flow) && (trigger == "onboarding" || trigger == "admin" || trigger == "appeal" || policy.Applies(trigger)) {
				out = append(out, flow)
			}
			continue
		}
		if trigger == "onboarding" || trigger == "admin" || trigger == "appeal" || policy.Applies(trigger) {
			out = append(out, flow)
		}
	}
	if len(out) == 0 {
		return nil, httpx.BadRequest("NO_FLOWS", "没有适用于本次触发的已启用流程")
	}
	return out, nil
}

func modesForFlows(p *Policy, flows []string, trigger string, failed []string) (string, map[string]string) {
	mode := "quick"
	modes := map[string]string{}
	for _, flow := range flows {
		m := "quick"
		if flowPolicy(p, flow).Full(trigger) || trigger == "onboarding" || trigger == "appeal" {
			m = "full"
		}
		// 复测必须重新观察失败项，不能用快检沿用的旧扣价代替修复证据。
		if ((trigger == "recovery" || trigger == "seller_manual") && len(failed) > 0) || (trigger == "adaptive" && flow == "intelligence" && (slices.Contains(failed, "D09") || slices.Contains(failed, "D06"))) {
			m = "full"
		}
		modes[flow] = m
		if m == "full" {
			mode = m
		}
	}
	return mode, modes
}

type flowSummary struct {
	Source      string                        `json:"source,omitempty"`
	Status      string                        `json:"status"`
	InheritedAt *time.Time                    `json:"inherited_at"`
	Models      []inspection.ModelCheckResult `json:"models,omitempty"`
}

func checkConclusive(status string) bool {
	return slices.Contains([]string{"pass", "warning", "fail", "not_applicable"}, status)
}
func worstStatus(a, b string) string {
	ranks := map[string]int{"not_run": 0, "not_applicable": 1, "inconclusive": 2, "pass": 3, "warning": 4, "fail": 5}
	if ranks[b] > ranks[a] {
		return b
	}
	return a
}

func summarizeFlow(flow string, run runRow, a Assessment) flowSummary {
	summary := flowSummary{Status: "not_run"}
	if flow == inspection.FlowIdentity {
		summary.Source = run.identitySource
	}
	selected := slices.Contains(run.flows, flow)
	var inherited *time.Time
	for key, c := range a.Checks {
		d, ok := inspection.Lookup(key)
		if !ok || d.Flow != flow {
			continue
		}

		if !selected && c.Evidence.Source != "history" {
			continue
		}
		if flow == "identity" && !selected {
			if key == "third_party" {
				summary.Source = "third_party"
			} else if c.Status != "not_run" {
				summary.Source = "self"
			}
		}
		if c.Evidence.Code == "disabled" {
			continue
		}
		if flow == inspection.FlowIdentity && run.identitySource == "third_party" && key != "third_party" {
			continue
		}
		summary.Status = worstStatus(summary.Status, c.Status)
		if c.Evidence.Source == "history" && (inherited == nil || c.CheckedAt.Before(*inherited)) {
			at := c.CheckedAt
			inherited = &at
		}
	}
	summary.InheritedAt = inherited
	if flow == inspection.FlowIntelligence {
		if selected && len(run.models) == 0 {
			summary.Status = "not_applicable"
		}
		models := append([]string(nil), run.models...)
		if !selected && len(models) == 0 {
			for _, key := range []string{"D07", "D07.honesty", "D09", "D06"} {
				c := a.Checks[key]
				if c.Evidence.Source != "history" {
					continue
				}
				for _, result := range c.Metrics["models"].Models {
					if !slices.Contains(models, result.Model) {
						models = append(models, result.Model)
					}
				}
			}
		}
		for _, model := range models {
			result := inspection.ModelCheckResult{Model: model, Status: "not_run"}
			for _, key := range []string{"D07", "D07.honesty", "D09", "D06"} {
				c := a.Checks[key]
				if !selected && c.Evidence.Source != "history" {
					continue
				}
				for _, m := range c.Metrics["models"].Models {
					if m.Model != model {
						continue
					}
					result.Status = worstStatus(result.Status, m.Status)
					if key == "D07" {
						result.TargetPassRate, result.ExpectedPassRate, result.Samples = m.TargetPassRate, m.ExpectedPassRate, m.Samples
					}
				}
			}
			if result.Status == "not_run" && len(models) == 1 {
				result.Status = summary.Status
			}
			if selected && !checkConclusive(result.Status) {
				summary.Status = "inconclusive"
			}
			summary.Models = append(summary.Models, result)
		}
	}
	return summary
}

// 测智只触发疑似；暂停由可用率或身份门槛决定，旧 SPRT 不参与结论。
func decideVerdict(run runRow, a Assessment, tp []tpResult) string {
	if a.InspectionMode == inspection.ModeTrial && a.AvailableTotal >= 2 && float64(a.AvailableOK)/float64(a.AvailableTotal) < .5 {
		return "unavailable"
	}
	if a.InspectionMode == inspection.ModeFormal {
		for _, g := range a.Gates {
			if g.Confirmed && g.Health == "unavailable" {
				return "unavailable"
			}
		}
		for _, g := range a.Gates {
			if g.Confirmed && g.Health == "degraded" {
				return "degraded"
			}
		}
	}
	if slices.Contains(run.flows, inspection.FlowIdentity) && run.identitySource == "third_party" {
		for _, r := range tp {
			if r.Verdict == "degraded" {
				return "degraded"
			}
		}
		for _, r := range tp {
			if r.Verdict == "suspected" {
				return "suspected"
			}
		}
	}
	for _, flow := range run.flows {
		s := summarizeFlow(flow, run, a)
		if flow == inspection.FlowIntelligence && (s.Status == "warning" || s.Status == "fail") {
			return "suspected"
		}
	}
	for _, flow := range run.flows {
		if !checkConclusive(summarizeFlow(flow, run, a).Status) {
			return "inconclusive"
		}
	}
	return "normal"
}

func definitionHash(key string, st InspectionSettings) string {
	d, _ := inspection.Effective(key, st)
	sum := sha256.Sum256(mustJSON(d))
	return hex.EncodeToString(sum[:])
}

// 按项定义与流程查历史，不要求整份设置相同；申诉前的扣价证据不再继承。
func (e *Engine) inheritInspection(ctx context.Context, run runRow, u *upstreamRow, p *Policy, st InspectionSettings, checks map[string]inspection.Check, gateLists ...*[]inspection.Gate) error {
	modelsToInherit := run.models
	if !slices.Contains(run.flows, inspection.FlowIntelligence) && len(modelsToInherit) == 0 {
		modelsToInherit = intelligenceModels(p, u.modelWhitelist)
	}
	var currentRaw []byte
	if err := e.DB.QueryRow(ctx, `SELECT execution_snapshot FROM probe_runs WHERE id=$1`, run.id).Scan(&currentRaw); err != nil {
		return err
	}
	var current executionSnapshot
	if err := json.Unmarshal(currentRaw, &current); err != nil {
		return err
	}
	rows, err := e.DB.Query(ctx, `SELECT r.flows,r.models,r.model,COALESCE(r.identity_source,''),r.mode,r.execution_snapshot,r.summary,r.finished_at,s.deductions_reset_at
 FROM probe_runs r LEFT JOIN upstream_quality_state s ON s.upstream_id=r.upstream_id
 WHERE r.upstream_id=$1 AND r.id<$2 AND r.status='done' AND r.finished_at>COALESCE(s.deductions_reset_at,'-infinity')
 ORDER BY r.finished_at DESC,r.id DESC LIMIT 100`, u.id, run.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var flows, models []string
		var model, source, mode string
		var raw, summary []byte
		var finished time.Time
		var reset *time.Time
		if err := rows.Scan(&flows, &models, &model, &source, &mode, &raw, &summary, &finished, &reset); err != nil {
			return err
		}
		var snap executionSnapshot
		var old struct {
			Checks map[string]inspection.Check `json:"checks"`
			Gates  []inspection.Gate           `json:"gates"`
		}
		if json.Unmarshal(raw, &snap) != nil || json.Unmarshal(summary, &old) != nil || snap.Settings == nil {
			continue
		}
		for key, c := range checks {
			d, ok := inspection.Lookup(key)
			if !ok || d.Flow == inspection.FlowCommon || !slices.Contains(flows, d.Flow) {
				continue
			}
			selected := slices.Contains(run.flows, d.Flow)
			if selected && (executionFlowMode(d.Flow, current.FlowModes, run.mode) != "quick" || c.Status != "not_run" || executionFlowMode(d.Flow, snap.FlowModes, mode) != "full") {
				continue
			}
			if !selected && c.Status != "not_run" {
				continue
			}
			if d.Flow == inspection.FlowIdentity {
				if source != p.Flows.Identity.Source || model != run.model {
					continue
				}
				// 权限项覆盖全部挂售型号，不能把旧白名单的结论移到新型号上。
				if key == "A04" && !slices.Equal(permissionProbeModels(snap, snap.Policy), permissionProbeModels(current, p)) {
					continue
				}
				if key == "third_party" && (snap.Policy == nil || string(mustJSON(snap.Policy.ThirdParty)) != string(mustJSON(p.ThirdParty))) {
					continue
				}
			}
			ttl := max(7*24*time.Hour, 2*time.Duration(flowPolicy(p, d.Flow).IntervalMinutes)*time.Minute)
			prior, ok := old.Checks[key]
			if !ok || (reset != nil && !prior.CheckedAt.After(*reset)) || e.now().Sub(prior.CheckedAt) > ttl || definitionHash(key, st) != definitionHash(key, *snap.Settings) {
				continue
			}
			if d.Flow == inspection.FlowIntelligence {
				if !ratesModelsMatch(snap.Policy, p, modelsToInherit) {
					continue
				}
				prior, ok = inheritedModelsCheck(prior, modelsToInherit)
				if !ok {
					continue
				}
			}
			if !checkConclusive(prior.Status) {
				continue
			}
			prior.Applied = false
			prior.Evidence.Source = "history"
			checks[key] = prior
			if len(gateLists) > 0 {
				for _, gate := range old.Gates {
					if gate.CheckKey == key {
						*gateLists[0] = append(*gateLists[0], gate)
					}
				}
			}
		}
	}
	return rows.Err()
}

func mustEffective(key string, st InspectionSettings) inspection.EffectiveDefinition {
	d, _ := inspection.Effective(key, st)
	return d
}
func anyPricing(st InspectionSettings) bool {
	for _, d := range inspection.Known() {
		effective, _ := inspection.Effective(d.Key, st)
		if effective.Enabled && effective.Pricing {
			return true
		}
	}
	return false
}

func formalBlocked(a Assessment) bool {
	if a.InspectionMode != "formal" {
		return false
	}
	for _, gate := range a.Gates {
		if gate.Confirmed {
			return true
		}
	}
	return false
}
func resetFlowDeductions(ctx context.Context, db DB, id int64, flows []string, at time.Time) error {
	keys := []string{}
	for _, d := range inspection.Known() {
		if containsAny(flows, d.Flow) || d.Flow == "common" && containsAny(flows, "intelligence") {
			keys = append(keys, d.Key)
		}
	}
	_, err := db.Exec(ctx, `UPDATE upstream_quality_state SET suspected_flows=ARRAY(SELECT f FROM unnest(suspected_flows) f WHERE NOT f=ANY($2::text[])),deductions_reset_at=$3,adaptive_attempts=CASE WHEN suspected_flows <@ $2::text[] THEN 0 ELSE adaptive_attempts END,last_failed_checks=ARRAY(SELECT k FROM unnest(last_failed_checks) k WHERE NOT k=ANY($4::text[])) WHERE upstream_id=$1`, id, flows, at, keys)
	return err
}
