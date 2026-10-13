package inspection

import (
	"math"
	"sort"
	"time"
)

func eligibleStatus(status string) bool {
	return status == "pass" || status == "warning" || status == "fail"
}

// 同一功能的验证面只扣一次；结构化输出、多轮保持不增加方案外乘法因子。
func functionBucket(key string) string {
	switch key {
	case "A11.json_schema", "A11.multiturn", "A11.tools":
		return "tools"
	case "A11.thinking":
		return "thinking"
	case "A11.image_pdf":
		return "image_pdf"
	case "D12":
		return "cache"
	default:
		return key
	}
}

func Aggregate(checks map[string]Check, gates []Gate, mode string, settings Settings) Aggregation {
	result := Aggregation{Checks: map[string]Check{}, Gates: append([]Gate{}, gates...), GroupFactors: map[string]float64{"identity": 1, "capability": 1, "function": 1, "availability": 1, "performance": 1}}
	observed, blocked, pricingEnabled := false, false, false
	functionFactors := map[string]float64{}
	performanceObserved := false
	for i, gate := range result.Gates {
		definition, known := Effective(gate.CheckKey, settings)
		// 第三方门槛直接使用检测站结论，自有门槛仍须登记校准。
		validGate := gate.Confirmed && known && definition.Enabled && (definition.Calibrated || gate.CheckKey == "third_party") && (gate.Health == "degraded" || gate.Health == "unavailable")
		result.Gates[i].Confirmed = validGate && mode == ModeFormal
		blocked = blocked || result.Gates[i].Confirmed
	}
	for key, check := range checks {
		definition, known := Effective(key, settings)
		check.Key, check.Applied = key, false
		invalidFactor := check.Factor != nil && (!finite(*check.Factor) || *check.Factor <= 0 || *check.Factor > 1.05)
		if invalidFactor {
			check.Factor = nil
		}
		if !known {
			check.Factor = nil
			result.Checks[key] = check
			continue
		}
		check.Name, check.Category, check.Role = definition.Name, definition.Category, definition.Role
		if definition.FactorGroup == "" {
			check.FactorGroup = nil
		} else {
			group := definition.FactorGroup
			check.FactorGroup = &group
		}
		valid := definition.Enabled && eligibleStatus(check.Status) && (check.Samples.Observed > 0 || check.Samples.Paired > 0) && !invalidFactor
		if !definition.Enabled || !eligibleStatus(check.Status) {
			check.Factor = nil
		}
		if valid && check.Factor == nil {
			factor := 1.0
			switch check.Status {
			case "warning":
				factor = definition.Coefficients["warning"]
			case "fail":
				factor = definition.Coefficients["severe"]
			}
			check.Factor = &factor
		}
		if definition.Enabled && definition.Pricing && !valid {
			result.Provisional = true
		}
		if valid && definition.Pricing && check.Factor != nil {
			observed = true
			factor := *check.Factor
			switch definition.FactorGroup {
			case "identity", "capability":
				result.GroupFactors[definition.FactorGroup] = math.Min(result.GroupFactors[definition.FactorGroup], factor)
			case "function":
				bucket := functionBucket(key)
				previous, exists := functionFactors[bucket]
				if !exists || factor < previous {
					functionFactors[bucket] = factor
				}
			case "availability":
				result.GroupFactors["availability"] *= factor
			case "performance":
				if !performanceObserved || factor < result.GroupFactors["performance"] {
					result.GroupFactors["performance"] = factor
				}
				performanceObserved = true
			}
			check.Applied = !blocked
		}
		switch {
		case blocked:
			check.Impact = "正式健康门槛已确认，暂停期间不计入扣价。"
		case check.Applied:
			check.Impact = "本项已计入质量系数，影响估价与调度。"
		case !definition.Pricing:
			check.Impact = "本项未开启用于扣价，仅展示检测结果。"
		default:
			check.Impact = "本项证据不足，暂未计入质量系数。"
		}
		result.Checks[key] = check
	}
	for _, definition := range Known() {
		effective, _ := Effective(definition.Key, settings)
		if effective.Enabled && effective.Pricing {
			pricingEnabled = true
			if _, present := checks[definition.Key]; !present {
				result.Provisional = true
			}
		}
	}
	result.Provisional = result.Provisional || !pricingEnabled
	var functionKeys []string
	for key := range functionFactors {
		functionKeys = append(functionKeys, key)
	}
	sort.Strings(functionKeys)
	for _, key := range functionKeys {
		result.GroupFactors["function"] *= functionFactors[key]
	}
	result.GroupFactors["identity"] = math.Max(0.6, result.GroupFactors["identity"])
	result.GroupFactors["function"] = math.Max(0.7, result.GroupFactors["function"])
	result.GroupFactors["availability"] = math.Max(0.6, result.GroupFactors["availability"])
	result.GroupFactors["performance"] = math.Max(0.85, math.Min(1.05, result.GroupFactors["performance"]))
	if observed && !blocked {
		factor := 1.0
		for _, group := range []string{"identity", "capability", "function", "availability", "performance"} {
			factor *= result.GroupFactors[group]
		}
		factor = math.Max(0.5, math.Min(1.05, factor))
		result.QualityFactor = &factor
	}
	return result
}

func GradeRank(grade string) int {
	switch grade {
	case "S":
		return 4
	case "A":
		return 3
	case "B":
		return 2
	case "C":
		return 1
	case "D":
		return 0
	default:
		return -1
	}
}

func Grade(q *float64, previous, pending string, count int, history GradeHistory, settings Settings) GradeDecision {
	decision := GradeDecision{Grade: previous}
	if settings.Mode != ModeFormal {
		decision.PendingGrade, decision.PendingGradeCount = pending, count
		return decision
	}
	if q == nil || !finite(*q) || *q < 0.5 || *q > 1.05 {
		return decision
	}
	sEligible := !history.FirstListing && history.GateFreeDays >= settings.SRequirements.GateFreeDays && history.AvailabilityDays >= settings.SRequirements.AvailabilityDays && finite(history.Availability) && history.Availability >= settings.SRequirements.MinAvailability
	target := "D"
	for _, grade := range []string{"S", "A", "B", "C"} {
		if grade == "S" && !sEligible {
			continue
		}
		if *q+1e-12 >= settings.GradeThresholds[grade] {
			target = grade
			break
		}
	}
	if GradeRank(previous) < 0 || history.FirstListing {
		decision.Grade = target
		return decision
	}
	previousRank, targetRank := GradeRank(previous), GradeRank(target)
	if targetRank > previousRank {
		target = previous
		for _, grade := range []string{"S", "A", "B", "C"} {
			if GradeRank(grade) <= previousRank || (grade == "S" && !sEligible) {
				continue
			}
			if *q+1e-12 >= settings.GradeThresholds[grade]+settings.GradeHysteresis {
				target = grade
				break
			}
		}
		decision.Grade = target
		return decision
	}
	if targetRank < previousRank {
		if (previous != "S" || sEligible) && *q+1e-12 >= settings.GradeThresholds[previous]-settings.GradeHysteresis {
			return decision
		}
		if pending == target {
			count++
		} else {
			count = 1
		}
		if count >= settings.DowngradeConfirmations {
			decision.Grade = target
			return decision
		}
		decision.PendingGrade, decision.PendingGradeCount = target, count
	}
	return decision
}

func Appraise(base map[string]float64, tier string, q *float64, at *time.Time, trial, coverageIncomplete bool) Appraisal {
	result := Appraisal{Tier: tier, CheckedAt: at, Provisional: trial || coverageIncomplete}
	value := base[tier]
	if !finite(value) || value <= 0 {
		value = base["default"]
	}
	if !finite(value) || value <= 0 {
		return result
	}
	result.Base, result.Configured = &value, true
	if at != nil && q != nil && finite(*q) && *q >= 0.5 && *q <= 1.05 {
		factor, multiplier := *q, value**q
		result.Factor, result.Multiplier = &factor, &multiplier
	}
	return result
}

// 卖家摘要保留契约形状，删除阈值、置信区间与内部原因定位，仅使用目录安全说明。
func PublicChecks(checks map[string]Check) map[string]Check {
	result := make(map[string]Check, len(checks))
	for key, check := range checks {
		definition, known := Lookup(key)
		if !known {
			continue
		}
		check.Key = key
		check.Metrics = map[string]Metric{}
		check.Evidence.GeneratorVersion = nil
		code := check.Evidence.Code
		check.Evidence.Code = ""
		switch check.Evidence.Source {
		case "paired_probe", "item_probe", "recovery_probe", "production", "account_snapshot", "third_party", "history":
		default:
			check.Evidence.Source = ""
		}
		if check.Evidence.SampleUnit != "request" && check.Evidence.SampleUnit != "item_unit" {
			check.Evidence.SampleUnit = ""
		}
		check.Name, check.Category, check.Role = definition.Name, definition.Category, definition.Role
		if definition.FactorGroup == "" {
			check.FactorGroup = nil
		} else {
			group := definition.FactorGroup
			check.FactorGroup = &group
		}
		check.Advice = definition.Advice
		switch check.Status {
		case "pass":
			check.Reason = "本次抽样观测通过。"
		case "warning":
			check.Reason = "本次抽样观测发现需要关注的差异。"
		case "fail":
			check.Reason = "本次抽样观测未通过，建议修复后复检。"
		case "not_applicable":
			check.Reason = "本次检测条件不适用。"
		case "not_run":
			check.Reason = "本次未检测此项。"
		default:
			check.Status = "inconclusive"
			check.Reason = "本次证据不足，暂时无法判定。"
			if code == "capability_unknown" {
				check.Reason = "模型官方能力尚未登记，暂时无法判定此项。"
			} else if code == "needs_baseline" {
				check.Reason = "该项需要基线账号对照，暂时无法判定。"
			}
		}
		if check.Applied {
			check.Impact = "本项已计入质量系数，影响估价与调度。"
		} else {
			check.Factor = nil
			check.Impact = "本项暂未计入质量系数。"
		}
		result[key] = check
	}
	return result
}
