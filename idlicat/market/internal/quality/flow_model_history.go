package quality

import "github.com/yx-token/idlicat/market/internal/inspection"

// 只继承当前挂售模型的有效结果，撤下模型的旧失败不能继续扣价。
func inheritedModelsCheck(c inspection.Check, models []string) (inspection.Check, bool) {
	if len(models) == 0 {
		return c, false
	}
	available := map[string]inspection.ModelCheckResult{}
	for _, result := range c.Metrics["models"].Models {
		if _, duplicate := available[result.Model]; duplicate {
			return c, false
		}
		available[result.Model] = result
	}
	filtered := []inspection.ModelCheckResult{}
	seen := map[string]bool{}
	var worst *inspection.ModelCheckResult
	samples := inspection.Samples{}
	for _, model := range models {
		if seen[model] {
			continue
		}
		seen[model] = true
		result, ok := available[model]
		if !ok || !checkConclusive(result.Status) {
			return c, false
		}
		result.Metrics = scalarModelMetrics(result.Metrics)
		filtered = append(filtered, result)
		if worst == nil || worseInheritedModel(result, *worst) {
			copyResult := result
			worst = &copyResult
		}
		samples.Observed += max(0, result.Samples)
		if result.Metrics["mcnemar_p"].Value != nil || result.Metrics["reasoning_ratio"].Value != nil {
			samples.Paired += max(0, result.Samples)
		}
	}
	c.Status, c.Factor, c.Applied = worst.Status, nil, false
	c.Metrics = scalarModelMetrics(worst.Metrics)
	if worst.TargetPassRate != nil {
		c.Metrics["target_pass_rate"] = inspection.Metric{Value: worst.TargetPassRate, Unit: "fraction"}
	}
	if worst.ExpectedPassRate != nil {
		c.Metrics["expected_pass_rate"] = inspection.Metric{Value: worst.ExpectedPassRate, Unit: "fraction"}
	}
	c.Metrics["models"] = inspection.Metric{Unit: "models", Models: filtered}
	c.Samples = samples
	c.Reason = "沿用当前挂售模型的有效检测结果。"
	return c, true
}

func scalarModelMetrics(source map[string]inspection.Metric) map[string]inspection.Metric {
	result := map[string]inspection.Metric{}
	for key, value := range source {
		if key != "models" && len(value.Models) == 0 {
			result[key] = value
		}
	}
	return result
}

func worseInheritedModel(candidate, previous inspection.ModelCheckResult) bool {
	if modelCheckStatusRank(candidate.Status) != modelCheckStatusRank(previous.Status) {
		return modelCheckStatusRank(candidate.Status) > modelCheckStatusRank(previous.Status)
	}
	for _, key := range []string{"relative_score", "reasoning_ratio", "high_low_ratio"} {
		a, b := candidate.Metrics[key].Value, previous.Metrics[key].Value
		if a != nil && b != nil && *a != *b {
			return *a < *b
		}
	}
	return false
}

// 模型级期望变化后重测；留空时检测项阈值的变化由生效定义哈希另行判断。
// flow 是测智类流程：期望通过率分别来自各流程自己的模型列表。
func ratesModelsMatch(oldPolicy, newPolicy *Policy, flow string, models []string) bool {
	if len(models) == 0 {
		return true
	}
	if oldPolicy == nil || newPolicy == nil {
		return false
	}
	for _, model := range models {
		oldRate, oldOK := configuredModelRate(oldPolicy, flow, model)
		newRate, newOK := configuredModelRate(newPolicy, flow, model)
		if !oldOK || !newOK || (oldRate == nil) != (newRate == nil) {
			return false
		}
		if oldRate != nil && *oldRate != *newRate {
			return false
		}
	}
	return true
}

func configuredModelRate(policy *Policy, flow, model string) (*float64, bool) {
	var rate *float64
	found := false
	configured := policy.Flows.Intelligence.Models
	if flow == inspection.FlowDegradation {
		configured = policy.Flows.Degradation.Models
	}
	for _, entry := range configured {
		if entry.Model != model {
			continue
		}
		if found || entry.ExpectedPassRate != nil && !(*entry.ExpectedPassRate > 0 && *entry.ExpectedPassRate <= 1) {
			return nil, false
		}
		rate, found = entry.ExpectedPassRate, true
	}
	return rate, found
}
