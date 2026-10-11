package quality

import (
	"math"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

// 正式判定复用原 SPRT，新增题只在有同期、有效配对时计智力分。
// 拒答、截断、限流和平台错误的剔除与扣价所用配对保持一致。
func assessFormal(outcomes []Outcome, th Thresholds, configurations ...InspectionSettings) Assessment {
	rows := make([]Outcome, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Target.HTTPStatus == 0 && o.Target.ErrorClass == "" {
			continue
		}
		if !strings.HasPrefix(o.Suite, "inspection_") {
			o.RefHistRate = nil
			if !eligiblePair(o) {
				o.Suite = "availability"
			}
			rows = append(rows, o)
			continue
		}
		o.Suite = "availability"
		o.RefHistRate = nil
		scoringEnabled := true
		if len(configurations) > 0 {
			rule, _ := inspection.Effective(o.CheckKey, configurations[0])
			scoringEnabled = rule.Enabled && rule.Calibrated
		}
		if scoringEnabled && (o.CheckKey == "D07" || o.CheckKey == "D07.honesty" || o.CheckKey == "D09") && eligiblePair(o) {
			o.Suite = "iq"
		} else {
			o.RefHistRate = nil
		}
		rows = append(rows, o)
	}
	return Assess(rows, th, true)
}

func eligiblePair(o Outcome) bool {
	return o.Ref != nil && o.Target.EligibleForIntelligence() && o.Ref.EligibleForIntelligence()
}
func numberPointer(n float64) *float64 { return &n }
func metric(n float64, unit string) inspection.Metric {
	return inspection.Metric{Value: numberPointer(n), Unit: unit}
}

func newInspectionCheck(d inspection.EffectiveDefinition, now time.Time) inspection.Check {
	c := inspection.Check{Key: d.Key, Name: d.Name, Category: d.Category, Role: d.Role, Status: "not_run", Metrics: map[string]inspection.Metric{}, Evidence: inspection.Evidence{Source: "paired_probe", SampleUnit: "item_unit", Code: "not_run"}, Reason: "本次未检测此项。", Advice: d.Advice, Impact: "证据不足，不扣价。", CheckedAt: now}
	if d.FactorGroup != "" {
		c.FactorGroup = &d.FactorGroup
	}
	return c
}

func observedRows(rows []Outcome, key string) []Outcome {
	var result []Outcome
	for _, row := range rows {
		if row.CheckKey == key {
			result = append(result, row)
		}
	}
	return result
}

func pairCheck(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) (paired []Outcome) {
	for _, o := range rows {
		if o.Target.HTTPStatus != 0 || o.Target.ErrorClass != "" {
			c.Samples.Observed++
		}
		if inspectionPair(o, d.Key) {
			paired = append(paired, o)
		} else {
			c.Samples.Excluded++
		}
	}
	c.Samples.Paired = len(paired)
	if len(rows) > 0 {
		c.Status = "inconclusive"
		c.Reason = "有效配对或校准证据不足，暂不能判定。"
		c.Evidence.Code = "insufficient_pairs"
	}
	if len(paired) >= int(d.Thresholds["min_pairs"]) && len(paired) > 0 {
		c.Status = "pass"
		c.Factor = numberPointer(1)
		c.Reason = "本次有效配对未见异常。"
		c.Evidence.Code = "paired_pass"
	}
	return paired
}

// Inspect 输出统一目录；采样、显著性与校准分别保留，缺测不能伪造通过。
func Inspect(rows []Outcome, st InspectionSettings, now time.Time) (map[string]inspection.Check, []inspection.Gate) {
	checks := map[string]inspection.Check{}
	gates := []inspection.Gate{}
	for _, definition := range inspection.Known() {
		d, _ := inspection.Effective(definition.Key, st)
		c := newInspectionCheck(d, now)
		own := observedRows(rows, d.Key)
		if !d.Enabled {
			c.Status = "not_applicable"
			c.Reason = "此项规则未启用。"
			c.Evidence.Code = "disabled"
			checks[d.Key] = c
			continue
		}
		switch d.Key {
		case "D07", "D07.honesty", "D09":
			paired := pairCheck(&c, own, d)
			if c.Status == "pass" {
				inspectIntelligence(&c, paired, d)
			}
		case "A15":
			inspectParameterFingerprint(&c, own)
		case "D06":
			inspectReasoning(&c, rows, d)
		case "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "D08":
			paired := pairCheck(&c, own, d)
			if c.Status == "pass" {
				relevant, failures := 0, 0
				for _, o := range paired {
					if o.RefPass {
						relevant++
						if !o.TargetPass {
							failures++
						}
					}
				}
				if relevant == 0 {
					c.Status = "not_applicable"
					c.Factor = nil
					c.Reason = "同期基线也未兑现此功能，不归因卖家。"
					c.Evidence.Code = "reference_unsupported"
				} else if failures > 0 {
					c.Status = "fail"
					c.Factor = numberPointer(d.Coefficients["failure"])
					c.Reason = "基线通过但被测未兑现功能。"
					c.Evidence.Code = "paired_function_failed"
				}
			}
		case "A11.thinking":
			paired := pairCheck(&c, own, d)
			if c.Status == "pass" {
				observed, failed := 0, 0
				for _, o := range paired {
					if o.Ref.ReasoningObserved && o.Ref.ReasoningTokens > 0 && o.Target.ReasoningObserved {
						observed++
						if o.Target.ReasoningTokens == 0 {
							failed++
						}
					}
				}
				if observed == 0 {
					c.Status = "inconclusive"
					c.Factor = nil
					c.Reason = "当前通道未提供可比较的推理用量。"
					c.Evidence.Code = "reasoning_usage_unknown"
				} else if failed > 0 {
					c.Status = "fail"
					c.Factor = numberPointer(d.Coefficients["failure"])
					c.Reason = "基线明确提供推理用量，被测明确返回零推理用量。"
					c.Evidence.Code = "thinking_not_observed"
				}
			}
		case "A02", "A03", "A12", "D10":
			inspectSharedEvidence(&c, rows, d)
		case "A04", "A05":
			inspectPermissionBoundary(&c, own, d, &gates, now)
		case "D13":
			inspectAvailability(&c, rows, d, &gates, now)
		case "D04":
			c.Status = "inconclusive"
			c.Reason = "缺少可验证的额度窗口，429 不作为异常扣价。"
			c.Evidence.Code = "window_not_verified"
		case "A01":
			c.Status = "inconclusive"
			c.Reason = "订阅档位为授权资料，未经过官方接口独立核验。"
			c.Evidence.Source = "account_snapshot"
			c.Evidence.Code = "tier_unverified"
		case "D05":
			c.Status = "inconclusive"
			c.Reason = "窗口余量未知。"
			c.Evidence.Source = "account_snapshot"
			c.Evidence.Code = "window_unknown"
		case "D12":
			// 两步缓存由引擎读取完整步骤，不能仅凭最后一步跨账号比较。
			pairCheck(&c, own, d)
			if len(own) == 0 {
				break
			}
			c.Status = "inconclusive"
			c.Factor = nil
			c.Reason = "缓存需要同账号两步的使用量证据。"
		}
		if !d.Calibrated && (c.Status == "fail" || c.Status == "warning") {
			c.Impact = "待运营校准，当前不扣价。"
		}
		checks[d.Key] = c
	}
	if strength, ok := checks["D06"]; ok && len(strength.Metrics) > 0 {
		c := checks["A06"]
		c.Status = "pass"
		c.Samples = strength.Samples
		c.Metrics = strength.Metrics
		c.Reason = "已显式发推理档位请求；响应量只作观测，不等同厂商档位认证。"
		c.Evidence.Code = "explicit_effort_observed"
		checks["A06"] = c
	}
	return checks, gates
}

func inspectIntelligence(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) {
	target, ref, bad, good := 0, 0, 0, 0
	for _, o := range rows {
		if o.TargetPass {
			target++
		}
		if o.RefPass {
			ref++
		}
		if !o.TargetPass && o.RefPass {
			bad++
		}
		if o.TargetPass && !o.RefPass {
			good++
		}
	}
	if ref == 0 {
		c.Status = "inconclusive"
		c.Factor = nil
		c.Reason = "同期基线全部未通过，无法确认被测能力。"
		return
	}
	ratio := float64(target) / float64(ref)
	p := mcnemarExact(bad, good)
	c.Metrics["relative_score"] = metric(ratio, "ratio")
	c.Metrics["mcnemar_p"] = metric(p, "fraction")
	c.Metrics["target_pass_rate"] = metric(float64(target)/float64(len(rows)), "fraction")
	if d.Key == "D09" {
		if ratio < d.Thresholds["warning_relative"] && bad > good {
			c.Status = "warning"
			c.Factor = numberPointer(d.Coefficients["warning"])
			c.Reason = "长上下文样本弱于同期基线。"
			c.Evidence.Code = "context_depth_low"
		}
		return
	}
	if p >= d.Thresholds["alpha"] || bad <= good {
		return
	}
	if ratio < d.Thresholds["severe_relative"] {
		c.Status = "fail"
		c.Factor = numberPointer(d.Coefficients["severe"])
	} else if ratio < d.Thresholds["warning_relative"] {
		c.Status = "warning"
		c.Factor = numberPointer(d.Coefficients["warning"])
	} else {
		return
	}
	c.Reason = "配对准确率显著低于同期基线。"
	c.Evidence.Code = "paired_intelligence_low"
}

// 精确双侧 McNemar：只使用两个不一致格；小样本不使用卡方近似。
func mcnemarExact(b, c int) float64 {
	n := b + c
	if n == 0 {
		return 1
	}
	k := min(b, c)
	logs := make([]float64, k+1)
	logp := -float64(n) * math.Ln2
	maximum := logp
	for i := 0; i <= k; i++ {
		if i > 0 {
			logp += math.Log(float64(n-i+1) / float64(i))
		}
		logs[i] = logp
		maximum = math.Max(maximum, logp)
	}
	sum := 0.0
	for _, v := range logs {
		sum += math.Exp(v - maximum)
	}
	return math.Min(1, 2*math.Exp(maximum)*sum)
}

func inspectReasoning(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) {
	logs := []float64{}
	anchors := map[string]bool{}
	for _, o := range rows {
		if o.CheckKey == "D06" {
			anchors[fmtInspectionKey(o.ItemID, o.InstanceIdx%1000)] = true
		}
	}
	for _, o := range rows {
		if o.CheckKey != "D07" || o.InstanceIdx >= 1000 {
			continue
		}
		if len(anchors) > 0 && !anchors[fmtInspectionKey(o.ItemID, o.InstanceIdx)] {
			continue
		}
		if len(logs) >= 20 {
			break
		}
		c.Samples.Observed++
		if !eligiblePair(o) || !o.Target.ReasoningObserved || !o.Ref.ReasoningObserved || o.Target.ReasoningTokens <= 0 || o.Ref.ReasoningTokens <= 0 {
			c.Samples.Excluded++
			continue
		}
		logs = append(logs, math.Log(float64(o.Target.ReasoningTokens)/float64(o.Ref.ReasoningTokens)))
	}
	c.Samples.Paired = len(logs)
	if len(logs) < max(2, int(d.Thresholds["min_samples"])) {
		c.Status = "inconclusive"
		c.Reason = "有效推理量配对不足，零值或缺失用量不能证明降档。"
		c.Evidence.Code = "reasoning_pairs_insufficient"
		return
	}
	mean := 0.0
	for _, v := range logs {
		mean += v
	}
	mean /= float64(len(logs))
	variance := 0.0
	for _, v := range logs {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(logs) - 1)
	critical := inspectionTCritical(len(logs) - 1)
	margin := critical * math.Sqrt(variance/float64(len(logs)))
	ci := [2]float64{math.Exp(mean - margin), math.Exp(mean + margin)}
	ratio := math.Exp(mean)
	c.Metrics["reasoning_ratio"] = inspection.Metric{Value: numberPointer(ratio), Unit: "ratio", CI: &ci, Confidence: numberPointer(.95)}
	c.Status = "pass"
	c.Factor = numberPointer(1)
	c.Reason = "有效配对推理量未见明显下降。"
	c.Evidence.Code = "reasoning_ratio_normal"
	if ratio < d.Thresholds["severe_ratio"] && ci[1] < d.Thresholds["ci_upper"] {
		c.Status = "fail"
		c.Factor = numberPointer(d.Coefficients["severe"])
	} else if ratio < d.Thresholds["warning_ratio"] && ci[1] < 1 {
		c.Status = "warning"
		c.Factor = numberPointer(d.Coefficients["warning"])
	}
	if c.Status != "pass" {
		c.Reason = "配对推理量低于同期基线。"
		c.Evidence.Code = "reasoning_ratio_low"
	}
	// 同一题面的 high/low 对照只衡量档位响应，不把 low 的准确率并入智力。
	high, low := map[string]Outcome{}, map[string]Outcome{}
	for _, o := range rows {
		key := fmtInspectionKey(o.ItemID, o.InstanceIdx%1000)
		if o.CheckKey == "D07" {
			high[key] = o
		}
		if o.CheckKey == "D06" {
			low[key] = o
		}
	}
	response := []float64{}
	referenceResponse := []float64{}
	for key, h := range high {
		l, ok := low[key]
		if !ok || !eligiblePair(h) || !eligiblePair(l) || l.Target.ReasoningTokens <= 0 || h.Target.ReasoningTokens <= 0 {
			continue
		}
		response = append(response, float64(h.Target.ReasoningTokens)/float64(l.Target.ReasoningTokens))
		if h.Ref.ReasoningTokens > 0 && l.Ref.ReasoningTokens > 0 {
			referenceResponse = append(referenceResponse, float64(h.Ref.ReasoningTokens)/float64(l.Ref.ReasoningTokens))
		}
	}
	if v, ok := percentile(response, .5); ok {
		c.Metrics["high_low_ratio"] = metric(v, "ratio")
	}
	if v, ok := percentile(referenceResponse, .5); ok {
		c.Metrics["baseline_high_low_ratio"] = metric(v, "ratio")
	}
}

// 两侧95%区间使用小样本t临界值；比率分布与估算误差仍需运营校准。
func inspectionTCritical(df int) float64 {
	values := []float64{0, 12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228, 2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086, 2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042}
	if df < len(values) {
		return values[max(1, df)]
	}
	if df <= 60 {
		return 2.042
	}
	return 2.0
}
