package quality

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

func fmtInspectionKey(id int64, idx int) string { return fmt.Sprintf("%d/%d", id, idx) }

// 日期后缀只作规范化；没有型号观测时不能用请求型号冒充响应型号。
func canonicalInspectionModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if len(m) > 9 && m[len(m)-9] == '-' {
		digits := m[len(m)-8:]
		numeric := true
		for _, c := range digits {
			numeric = numeric && c >= '0' && c <= '9'
		}
		if numeric {
			m = m[:len(m)-9]
		}
	}
	return strings.ReplaceAll(m, ".", "-")
}

func inspectSharedEvidence(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) {
	candidates := []Outcome{}
	for _, o := range rows {
		if o.InstanceIdx < 1000 && strings.HasPrefix(o.Suite, "inspection_") && o.CheckKey != "A05" {
			candidates = append(candidates, o)
		}
	}
	paired := pairCheck(c, candidates, d)
	if c.Status != "pass" {
		return
	}
	mismatch, observed := 0, 0
	ttft, tps := []float64{}, []float64{}
	for _, o := range paired {
		switch d.Key {
		case "A02":
			if o.Target.ResponseModel == "" || o.Ref.ResponseModel == "" {
				continue
			}
			observed++
			// 同期基线的官方自动路由是观察白名单，不惩罚两侧同样的官方回退。
			if canonicalInspectionModel(o.Target.ResponseModel) != canonicalInspectionModel(o.RequestedModel) && canonicalInspectionModel(o.Target.ResponseModel) != canonicalInspectionModel(o.Ref.ResponseModel) {
				mismatch++
			}
		case "A03":
			if !o.StaticComparable {
				continue
			}
			t, r := totalInspectionInput(o.Target), totalInspectionInput(*o.Ref)
			if t == nil || r == nil {
				continue
			}
			observed++
			if *t != *r {
				mismatch++
			}
		case "A12":
			observed++
			if o.Target.HasFlag("refusal") && !o.Ref.HasFlag("refusal") {
				mismatch++
			}
		case "D10":
			if o.Target.TTFT > 0 && o.Ref.TTFT > 0 {
				ttft = append(ttft, float64(o.Target.TTFT)/float64(o.Ref.TTFT))
			}
			if o.Target.OutputTPS() > 0 && o.Ref.OutputTPS() > 0 {
				tps = append(tps, o.Target.OutputTPS()/o.Ref.OutputTPS())
			}
		}
	}
	if d.Key == "D10" {
		if len(ttft) < int(d.Thresholds["min_pairs"]) || len(tps) < int(d.Thresholds["min_pairs"]) {
			c.Status = "inconclusive"
			c.Factor = nil
			c.Reason = "同型号配对速度样本不足。"
			return
		}
		t, _ := percentile(ttft, .5)
		p, _ := percentile(tps, .5)
		c.Metrics["ttft_ratio"] = metric(t, "ratio")
		c.Metrics["output_tps_ratio"] = metric(p, "ratio")
		slow := math.Max(t, 1/p)
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
		}
		if c.Status != "pass" {
			c.Reason = "同型号配对速度慢于基线。"
			c.Evidence.Code = "paired_speed_slow"
		}
		// 基线配对不能伪装同行 P25/P75；只有真实同行历史满足条件才可加成。
		return
	}
	if observed < int(d.Thresholds["min_pairs"]) {
		c.Status = "inconclusive"
		c.Factor = nil
		c.Reason = "该观测字段缺失或配对不足。"
		return
	}
	c.Metrics["mismatches"] = metric(float64(mismatch), "count")
	c.Metrics["mismatch_rate"] = metric(float64(mismatch)/float64(observed), "fraction")
	c.Factor = nil // 身份、计数和拒答提示不使用未经批准的重扣系数。
	if mismatch > 0 {
		c.Status = "warning"
		c.Reason = "存在与同期基线不同的观测，需复检与校准。"
		c.Evidence.Code = "paired_mismatch"
	}
}

func totalInspectionInput(r ProbeResult) *int64 {
	if r.InputTokens == nil {
		return nil
	}
	n := *r.InputTokens
	// 仅 Claude 缓存与普通输入分开；其他协议 cached 是输入的子集。
	if strings.HasPrefix(r.ResponseModel, "claude") {
		if r.CacheCreationTokens != nil {
			n += *r.CacheCreationTokens
		}
		if r.CacheReadTokens != nil {
			n += *r.CacheReadTokens
		}
	}
	return &n
}

func inspectPermissionBoundary(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition, gates *[]inspection.Gate, now time.Time) {
	observed, paired, failed, primaryFailed, uncertain := 0, 0, 0, 0, 0
	for _, o := range rows {
		if o.Target.HTTPStatus != 0 {
			observed++
		}
		if o.Target.ErrorClass == "platform" || o.Target.ErrorClass == "rate_limited" || o.Target.ErrorClass == "configuration" {
			continue
		}
		if o.Ref == nil || !o.Ref.OK() {
			continue
		}
		if o.Target.OK() {
			paired++
			if d.Key == "A05" && (!o.RefPass || !o.TargetPass) {
				target, reference := totalInspectionInput(o.Target), totalInspectionInput(*o.Ref)
				if o.RefPass && target != nil && reference != nil && *reference > 0 && float64(*target)/float64(*reference) < d.Thresholds["min_input_ratio"] {
					failed++
				} else {
					uncertain++
				}
			}
			continue
		}
		denied := (o.Target.HTTPStatus == 403 && (o.Target.ErrorCode == "permission_denied" || o.Target.ErrorCode == "model_access_denied" || o.Target.ErrorCode == "PERMISSION_DENIED")) || o.Target.ErrorCode == "model_not_found" || o.Target.ErrorCode == "model_not_allowed"
		contextDenied := o.Target.ErrorCode == "context_length_exceeded" || o.Target.ErrorCode == "max_input_tokens" || o.Target.ErrorCode == "input_too_long"
		if d.Key == "A04" && denied || d.Key == "A05" && contextDenied {
			paired++
			failed++
			if o.PrimaryModel {
				primaryFailed++
			}
		}
	}
	c.Samples = inspection.Samples{Observed: observed, Paired: paired, Excluded: len(rows) - paired}
	if len(rows) == 0 {
		return
	}
	if paired == 0 {
		c.Status = "inconclusive"
		c.Reason = "基线或错误来源不足以确认权限/边界。"
		return
	}
	c.Status = "pass"
	c.Reason = "本次测试点被接受，不代表核验全部权限或完整上下文上限。"
	c.Evidence.Code = "tested_point_accepted"
	if uncertain > 0 {
		c.Status = "inconclusive"
		c.Reason = "首尾标记未通过且计数证据不足，不能确认边界或作为恢复依据。"
		c.Evidence.Code = "context_preservation_unknown"
	}
	if failed > 0 {
		c.Status = "fail"
		c.Reason = "同期基线接受但被测明确拒绝测试点。"
		c.Evidence.Code = "permission_or_context_denied"
		if d.Key == "A05" || primaryFailed > 0 {
			*gates = append(*gates, inspection.Gate{CheckKey: d.Key, Code: c.Evidence.Code, Health: "unavailable", Confirmed: d.Calibrated, OccurredAt: now, Reason: c.Reason})
		}
	}
}

func inspectAvailability(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition, gates *[]inspection.Gate, now time.Time) {
	success, total := 0, 0
	for _, o := range rows {
		if !strings.HasPrefix(o.Suite, "inspection_") || o.Target.HTTPStatus == 0 && o.Target.ErrorClass == "" {
			continue
		}
		c.Samples.Observed++
		switch o.Target.ErrorClass {
		case "rate_limited", "platform", "configuration", "expected_reject":
			c.Samples.Excluded++
			continue
		}
		total++
		if o.Target.OK() {
			success++
		}
	}
	c.Samples.Paired = total
	if total < int(d.Thresholds["min_samples"]) {
		c.Status = "inconclusive"
		c.Reason = "探针可用率样本不足，等待生产流量积累。"
		c.Evidence.Code = "availability_samples_insufficient"
		return
	}
	rate := float64(success) / float64(total)
	applyAvailabilityRate(c, rate, d)
	if rate < d.Thresholds["gate_rate"] && total >= int(d.Thresholds["gate_min_samples"]) {
		*gates = append(*gates, inspection.Gate{CheckKey: d.Key, Code: "availability_below_minimum", Health: "unavailable", Confirmed: d.Calibrated, OccurredAt: now, Reason: "有效请求可用率低于最低要求。"})
	}
}

func applyAvailabilityRate(c *inspection.Check, rate float64, d inspection.EffectiveDefinition) {
	c.Metrics["success_rate"] = metric(rate, "fraction")
	c.Status = "pass"
	c.Reason = "有效请求可用率满足当前档位。"
	c.Evidence.Code = "availability_normal"
	var coefficient float64
	switch {
	case rate >= .995:
		coefficient = d.Coefficients["tier_995"]
	case rate >= .99:
		coefficient = d.Coefficients["tier_99"]
	case rate >= .98:
		coefficient = d.Coefficients["tier_98"]
	case rate >= .95:
		coefficient = d.Coefficients["tier_95"]
	default:
		coefficient = d.Coefficients["tier_90"]
	}
	c.Factor = numberPointer(coefficient)
	if coefficient < 1 {
		c.Status = "warning"
		c.Reason = "有效请求可用率低于正常档位。"
		c.Evidence.Code = "availability_low"
	}
}

// 功能与拒答检查不能把 HTTP200 的截断/拒答当成没有观测。
func inspectionPair(o Outcome, key string) bool {
	if key == "D07" || key == "D07.honesty" || key == "D09" {
		return eligiblePair(o)
	}
	return o.Ref != nil && o.Target.OK() && o.Ref.EligibleForIntelligence()
}

// 参数指纹只接受管理员登记的型号级规则；网关本地拦截不构成厂商观测。
func inspectParameterFingerprint(c *inspection.Check, rows []Outcome) {
	if len(rows) == 0 {
		return
	}
	c.Status = "inconclusive"
	mismatches := 0
	for _, o := range rows {
		c.Samples.Observed++
		usable := func(r ProbeResult) bool { return r.OK() || r.ErrorClass == "expected_reject" }
		if o.Ref == nil || !usable(o.Target) || !usable(*o.Ref) {
			c.Samples.Excluded++
			continue
		}
		c.Samples.Paired++
		if o.Target.HTTPStatus != o.Ref.HTTPStatus || o.Target.ErrorCode != o.Ref.ErrorCode {
			mismatches++
		}
	}
	if c.Samples.Paired < 2 {
		c.Reason = "没有足够的厂商级参数规则配对。"
		return
	}
	c.Status = "pass"
	c.Reason = "已登记参数规则与同期基线一致。"
	c.Metrics["mismatches"] = metric(float64(mismatches), "count")
	if mismatches > 0 {
		c.Status = "warning"
		c.Reason = "参数规则与同期基线有差异，需复核网关行为。"
	}
}
