package quality

import (
	"context"
	"sort"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

func executionItemFlow(it Item) string {
	switch it.Suite {
	case "inspection_capability", "inspection_context":
		return inspection.FlowIntelligence
	case "inspection_degradation":
		return inspection.FlowDegradation
	case "inspection_authenticity", "inspection_function", "inspection_output":
		return "identity"
	case "inspection_performance":
		return "common"
	}
	return ""
}

func executionFlowMode(flow string, modes map[string]string, fallback string) string {
	if mode := modes[flow]; mode != "" {
		return mode
	}
	if fallback == "" || fallback == "legacy" {
		return "full"
	}
	return fallback
}

func flowItemModels(it Item, primary string, models []string) []string {
	if probeFlowFamily(executionItemFlow(it)) != inspection.FlowIntelligence {
		return []string{primary}
	}
	result := []string{}
	for _, model := range models {
		if len(it.Models) == 0 || containsAny(it.Models, model) {
			result = append(result, model)
		}
	}
	return result
}

// 同次运行按流程各自选题，快检不会因另一流程要求完整检测而扩样。
func selectFlowExecutionItems(ctx context.Context, db DB, platform, primary string, models, degradationModels, flows []string, source string, modes map[string]string, st InspectionSettings) ([]Item, error) {
	wanted := []string{}
	// 测智与降智检测各自只跑自己的套件，任一流程有冻结模型就取题。
	if containsAny(flows, inspection.FlowIntelligence) && len(models) > 0 {
		wanted = append(wanted, "inspection_capability", "inspection_context")
	}
	if containsAny(flows, inspection.FlowDegradation) && len(degradationModels) > 0 {
		wanted = append(wanted, "inspection_degradation")
	}
	if containsAny(flows, "identity") && source == "self" {
		wanted = append(wanted, "inspection_authenticity", "inspection_function", "inspection_output")
	}
	if len(wanted) == 0 {
		return []Item{}, nil
	}
	rows, err := db.Query(ctx, "SELECT "+itemColumns+` FROM probe_items WHERE status='active' AND suite=ANY($1) AND (cardinality(platforms)=0 OR $2=ANY(platforms)) ORDER BY id`, wanted, platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		flow := executionItemFlow(*it)
		mode := executionFlowMode(flow, modes, "full")
		if obsoleteInspectionBuiltin(it.BuiltinKey) || len(it.Modes) > 0 && !containsAny(it.Modes, mode) {
			continue
		}
		if rule, known := inspection.Effective(it.CheckKey, st); known && !rule.Enabled {
			continue
		}
		if len(flowItemModels(*it, primary, flowModelsOf(runRow{models: models, degradationModels: degradationModels}, flow))) == 0 ||
			probeFlowFamily(flow) != inspection.FlowIntelligence && len(it.Models) > 0 && !containsAny(it.Models, primary) {
			continue
		}
		result = append(result, *it)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, rows.Err()
}

func executionInstanceLimit(plan executionSnapshot) int {
	// 测智与降智检测各自按冻结的模型数展开实例，上限取两者中较大的集合。
	return 200 * max(1, len(plan.Models), len(plan.DegradationModels))
}

func modelCheckStatusRank(status string) int {
	switch status {
	case "fail":
		return 6
	case "warning":
		return 5
	case "inconclusive":
		return 4
	case "not_run":
		return 3
	case "pass":
		return 2
	}
	return 1
}

// flowFrozenModels 返回该流程本次冻结的模型集合；测智类流程共用一套按模型展开的逻辑。
func flowFrozenModels(flow string, opts InspectOptions) []string {
	if flow == inspection.FlowDegradation {
		return opts.FrozenDegradationModels
	}
	return opts.Models
}

func flowModelsUnconfiguredReason(flow string) string {
	if flow == inspection.FlowDegradation {
		return "挂售模型未配置降智检测。"
	}
	return "挂售模型未配置测智。"
}

func flowModelsUnconfiguredCode(flow string) string {
	if flow == inspection.FlowDegradation {
		return "degradation_models_unconfigured"
	}
	return "intelligence_models_unconfigured"
}

// 先逐模型判定再取最差结论，避免优秀模型的样本掩盖另一模型降智。
// 测智与降智检测都是按模型分别判分，只是检测项与冻结的模型列表不同。
func inspectIntelligenceModels(c *inspection.Check, rows, own []Outcome, d inspection.EffectiveDefinition, st InspectionSettings, opts InspectOptions, now time.Time) bool {
	if d.Key != "D07" && d.Key != "D07.honesty" && d.Key != "D09" && d.Key != "D06" && d.Key != "D14" {
		return false
	}
	frozen, configured := opts.Models, opts.IntelligenceModels
	if d.Key == "D14" {
		frozen, configured = opts.FrozenDegradationModels, opts.DegradationModels
	}
	models := append([]string(nil), frozen...)
	if frozen == nil {
		for _, row := range own {
			if !containsAny(models, row.RequestedModel) {
				models = append(models, row.RequestedModel)
			}
		}
	}
	if len(models) == 0 {
		return false
	}
	results := []inspection.ModelCheckResult{}
	samples := inspection.Samples{}
	var worst *inspection.Check
	for _, model := range models {
		var modelRows []Outcome
		for _, row := range rows {
			if row.RequestedModel == model {
				// 模型偏移只用于存储去重，判定仍使用模型内的 high/low 索引。
				row.InstanceIdx %= 10000
				modelRows = append(modelRows, row)
			}
		}
		modelOwn := observedRows(modelRows, d.Key)
		definition := d
		definition.Thresholds = map[string]float64{}
		for key, value := range d.Thresholds {
			definition.Thresholds[key] = value
		}
		expected := d.Thresholds["expected_pass_rate"]
		for _, entry := range configured {
			if entry.Model == model && entry.ExpectedPassRate != nil {
				expected = *entry.ExpectedPassRate
				break
			}
		}
		if expected > 0 {
			definition.Thresholds["expected_pass_rate"] = expected
			definition.Thresholds["model_expected_pass_rate"] = expected
		}
		current := newInspectionCheck(definition, now)
		if len(modelOwn) > 0 {
			if hasInspectionReference(modelRows) {
				if d.Key == "D06" {
					inspectReasoning(&current, modelRows, definition)
				} else if paired := pairCheck(&current, modelOwn, definition); current.Status == "pass" {
					inspectIntelligence(&current, paired, definition)
				}
			} else {
				current.Evidence.Source = "item_probe"
				if d.Key == "D06" {
					inspectItemReasoning(&current, modelRows, definition)
				} else {
					inspectItemIntelligence(&current, modelOwn, definition)
				}
			}
		}
		if expected > 0 {
			current.Metrics["expected_pass_rate"] = metric(expected, "fraction")
		}
		result := inspection.ModelCheckResult{Model: model, Status: current.Status, TargetPassRate: current.Metrics["target_pass_rate"].Value, ExpectedPassRate: current.Metrics["expected_pass_rate"].Value, Samples: max(0, current.Samples.Observed-current.Samples.Excluded), Metrics: current.Metrics}
		results = append(results, result)
		samples.Observed += current.Samples.Observed
		samples.Paired += current.Samples.Paired
		samples.Excluded += current.Samples.Excluded
		if worst == nil || modelCheckStatusRank(current.Status) > modelCheckStatusRank(worst.Status) || current.Status == worst.Status && current.Factor != nil && (worst.Factor == nil || *current.Factor < *worst.Factor) {
			copyCheck := current
			worst = &copyCheck
		}
	}
	*c = *worst
	c.Samples = samples
	// 模型结果另存一个指标，最差模型的标量指标仍兼容已有报告。
	c.Metrics = map[string]inspection.Metric{}
	for key, value := range worst.Metrics {
		c.Metrics[key] = value
	}
	c.Metrics["models"] = inspection.Metric{Unit: "models", Models: results}
	return true
}
