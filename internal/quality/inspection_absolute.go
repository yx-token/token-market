package quality

import (
	"math"
	"strings"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

type InspectOptions struct {
	Platform   string
	AuthType   string
	Capability *inspection.ModelCapability
}

func hasInspectionReference(rows []Outcome) bool {
	for _, o := range rows {
		if o.Ref != nil {
			return true
		}
	}
	return false
}

func itemCapability(o Outcome, st InspectionSettings, opts InspectOptions) inspection.ModelCapability {
	if opts.Capability != nil && (opts.Capability.Model == "" || opts.Capability.Model == o.RequestedModel) {
		return *opts.Capability
	}
	return inspection.LookupCapability(opts.Platform, o.RequestedModel, st)
}

// 无基线分支独立处理，不改变有基线时的样本剔除、校准或门槛逻辑。
func inspectItemProbe(c *inspection.Check, rows, own []Outcome, d inspection.EffectiveDefinition, st InspectionSettings, opts InspectOptions, gates *[]inspection.Gate, now time.Time) bool {
	switch d.Key {
	case "D07", "D07.honesty", "D09", "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "A11.thinking", "D08", "A04", "A05", "D06", "A02", "A03", "A12", "A15", "D10", "D12":
	default:
		return false
	}
	c.Evidence.Source = "item_probe"
	switch d.Key {
	case "D07", "D07.honesty", "D09":
		inspectItemIntelligence(c, own, d)
	case "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "D08":
		inspectItemFunction(c, own, d, st, opts)
	case "A11.thinking":
		inspectItemThinking(c, own, d, st, opts)
	case "A04", "A05":
		inspectItemBoundary(c, own, d, st, opts, gates, now)
	case "D06":
		inspectItemReasoning(c, rows, d)
	case "A02", "A12":
		inspectItemShared(c, rows, d)
	case "A03", "A15", "D10":
		c.Status, c.Reason, c.Evidence.Code = "inconclusive", "该项需要基线账号对照。", "needs_baseline"
	case "D12":
		if len(own) > 0 {
			c.Status, c.Reason = "inconclusive", "缓存需要同账号两步的使用量证据。"
		}
	}
	return true
}

func itemObserved(c *inspection.Check, rows []Outcome, usable func(Outcome) bool) []Outcome {
	var result []Outcome
	for _, o := range rows {
		if o.Target.HTTPStatus == 0 && o.Target.ErrorClass == "" {
			continue
		}
		c.Samples.Observed++
		if usable(o) {
			result = append(result, o)
		} else {
			c.Samples.Excluded++
		}
	}
	if c.Samples.Observed > 0 {
		c.Status, c.Reason, c.Evidence.Code = "inconclusive", "被测可计分证据不足；未与基线对照。", "insufficient_samples"
	}
	return result
}

func itemPass(c *inspection.Check) {
	c.Status, c.Reason, c.Evidence.Code = "pass", "按题目判定通过；未与基线对照。", "item_pass"
	c.Factor = numberPointer(1)
}

func itemCapabilityUnknown(c *inspection.Check) {
	c.Status, c.Reason, c.Evidence.Code = "inconclusive", "模型官方能力未登记，请在模型官方能力中登记后复检；未与基线对照。", "capability_unknown"
	c.Factor = nil
}

func itemUnsupported(c *inspection.Check) {
	c.Status, c.Reason, c.Evidence.Code = "not_applicable", "官方不支持该测试功能，本项不适用。", "capability_unsupported"
	c.Factor = nil
}

func itemFunctionFailed(c *inspection.Check, d inspection.EffectiveDefinition) {
	c.Status, c.Reason, c.Evidence.Code = "fail", "官方支持该功能，被测未兑现；未与基线对照。", "item_function_failed"
	c.Factor = numberPointer(d.Coefficients["failure"])
}

func inspectItemIntelligence(c *inspection.Check, own []Outcome, d inspection.EffectiveDefinition) {
	usable := itemObserved(c, own, func(o Outcome) bool { return o.Target.EligibleForIntelligence() })
	if len(usable) < int(d.Thresholds["min_pairs"]) || len(usable) == 0 {
		if len(own) > 0 {
			c.Status, c.Reason, c.Evidence.Code = "inconclusive", "按题目判定样本不足。", "insufficient_samples"
		}
		return
	}
	passed := 0
	for _, o := range usable {
		if o.TargetPass {
			passed++
		}
	}
	n, expected := float64(len(usable)), d.Thresholds["expected_pass_rate"]
	rate := float64(passed) / n
	relative := rate / expected
	c.Metrics["target_pass_rate"] = metric(rate, "fraction")
	c.Metrics["expected_pass_rate"] = metric(expected, "fraction")
	c.Metrics["relative_score"] = metric(relative, "ratio")
	itemPass(c)
	if relative >= d.Thresholds["warning_relative"] {
		return
	}
	// 单侧95% Wilson 上界，仅在样本已足以确认偏低时提示或扣价。
	z := 1.6448536269514722
	upper := (rate + z*z/(2*n) + z*math.Sqrt(rate*(1-rate)/n+z*z/(4*n*n))) / (1 + z*z/n)
	if upper/expected < d.Thresholds["severe_relative"] {
		c.Status, c.Factor = "fail", numberPointer(d.Coefficients["severe"])
	} else if upper/expected < d.Thresholds["warning_relative"] {
		c.Status, c.Factor = "warning", numberPointer(d.Coefficients["warning"])
	}
	if c.Status != "pass" {
		c.Reason, c.Evidence.Code = "按题目判定的通过率显著低于期望；未与基线对照。", "item_pass_rate_low"
	}
}

func itemFunctionSupport(o Outcome, capability inspection.ModelCapability) *bool {
	switch o.CheckKey {
	case "A11.tools":
		return capability.Tools
	case "A11.json_schema":
		return capability.JSONSchema
	case "A11.multiturn":
		return capability.MultiTurn
	case "A11.image_pdf":
		if o.ProbeFeature == "image" {
			return capability.Image
		}
		if o.ProbeFeature == "pdf" {
			return capability.PDF
		}
		// 旧结果未标识题目介质时，只有两项均登记且一致才能确认能力。
		if capability.Image != nil && capability.PDF != nil && *capability.Image == *capability.PDF {
			return capability.Image
		}
	case "D08":
		if capability.MaxOutputUnlimited != nil && *capability.MaxOutputUnlimited {
			return capabilityBoolPointer(true)
		}
		if capability.MaxOutput > 0 {
			return capabilityBoolPointer(o.ProbeOutputTokens <= capability.MaxOutput)
		}
	}
	return nil
}

func capabilityBoolPointer(value bool) *bool { return &value }

func inspectItemFunction(c *inspection.Check, own []Outcome, d inspection.EffectiveDefinition, st InspectionSettings, opts InspectOptions) {
	usable := itemObserved(c, own, func(o Outcome) bool {
		return o.Target.OK() || o.Target.ErrorClass == "http_4xx" || o.Target.ErrorClass == "expected_reject"
	})
	if len(usable) == 0 {
		return
	}
	failed, unknown, unsupported := false, false, false
	for _, o := range usable {
		if o.Target.OK() && o.TargetPass {
			continue
		}
		support := itemFunctionSupport(o, itemCapability(o, st, opts))
		switch {
		case support == nil:
			unknown = true
		case *support:
			failed = true
		default:
			unsupported = true
		}
	}
	switch {
	case failed:
		itemFunctionFailed(c, d)
	case unknown:
		itemCapabilityUnknown(c)
	case unsupported:
		itemUnsupported(c)
	default:
		itemPass(c)
	}
}

func inspectItemThinking(c *inspection.Check, own []Outcome, d inspection.EffectiveDefinition, st InspectionSettings, opts InspectOptions) {
	usable := itemObserved(c, own, func(o Outcome) bool { return o.Target.OK() && o.Target.ReasoningObserved })
	if len(usable) == 0 {
		if c.Samples.Observed > 0 {
			c.Reason, c.Evidence.Code = "当前通道未提供推理用量，暂时无法判定。", "reasoning_usage_unknown"
		}
		return
	}
	failed, unknown, unsupported := false, false, false
	for _, o := range usable {
		if o.Target.ReasoningTokens > 0 {
			continue
		}
		support := itemCapability(o, st, opts).Thinking
		switch {
		case support == nil:
			unknown = true
		case *support:
			failed = true
		default:
			unsupported = true
		}
	}
	switch {
	case failed:
		itemFunctionFailed(c, d)
	case unknown:
		itemCapabilityUnknown(c)
	case unsupported:
		itemUnsupported(c)
	default:
		itemPass(c)
	}
}

func inspectItemBoundary(c *inspection.Check, own []Outcome, d inspection.EffectiveDefinition, st InspectionSettings, opts InspectOptions, gates *[]inspection.Gate, now time.Time) {
	usable := itemObserved(c, own, func(o Outcome) bool {
		return o.Target.ErrorClass != "platform" && o.Target.ErrorClass != "configuration" && o.Target.ErrorClass != "rate_limited"
	})
	if len(usable) == 0 {
		return
	}
	failed, uncertain, applicable, primaryFailed := false, false, false, false
	for _, o := range usable {
		window, probe := itemCapability(o, st, opts).ContextForAuth(opts.AuthType), o.ProbeTokens
		if probe <= 0 {
			probe = int(d.Thresholds["probe_tokens"])
		}
		if d.Key == "A05" && window > 0 && window < probe {
			continue
		}
		applicable = true
		denied := (o.Target.HTTPStatus == 403 && (o.Target.ErrorCode == "permission_denied" || o.Target.ErrorCode == "model_access_denied" || o.Target.ErrorCode == "PERMISSION_DENIED")) || o.Target.ErrorCode == "model_not_found" || o.Target.ErrorCode == "model_not_allowed"
		contextDenied := o.Target.ErrorCode == "context_length_exceeded" || o.Target.ErrorCode == "max_input_tokens" || o.Target.ErrorCode == "input_too_long"
		if !o.Target.OK() && (d.Key == "A04" && denied || d.Key == "A05" && window > 0 && contextDenied) {
			failed = true
			primaryFailed = primaryFailed || o.PrimaryModel
			continue
		}
		if !o.Target.OK() {
			uncertain = true
			continue
		}
		// 窗口未登记也要核对首尾标记与输入计数，网关静默截断不能算作接受
		if d.Key == "A05" {
			input := totalInspectionInput(o.Target)
			if input == nil || float64(*input) < float64(probe)*d.Thresholds["min_input_ratio"] || !o.TargetPass {
				uncertain = true
			}
		}
	}
	switch {
	case failed:
		c.Status, c.Reason, c.Evidence.Code = "fail", "官方支持测试点，被测明确拒绝；未与基线对照。", "item_function_failed"
		if d.Key == "A05" || primaryFailed {
			*gates = append(*gates, inspection.Gate{CheckKey: d.Key, Code: c.Evidence.Code, Health: "unavailable", Confirmed: d.Calibrated, OccurredAt: now, Reason: c.Reason})
		}
	case !applicable:
		itemUnsupported(c)
		c.Reason = "官方上下文窗口小于本次探针长度，本项不适用。"
	case uncertain:
		c.Status, c.Reason, c.Evidence.Code = "inconclusive", "测试点缺少明确的接受、计数或首尾标记证据；未与基线对照。", "context_preservation_unknown"
		if d.Key == "A05" && itemCapability(usable[0], st, opts).ContextForAuth(opts.AuthType) == 0 {
			itemCapabilityUnknown(c)
		}
	default:
		itemPass(c)
	}
	// 权限与边界只控制门槛，不新增价格系数。
	c.Factor = nil
}

func inspectItemReasoning(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) {
	high := map[string]Outcome{}
	for _, o := range rows {
		if o.CheckKey == "D07" && o.InstanceIdx < 1000 && o.GeneratorVersion != "static-1" && strings.HasPrefix(o.Suite, "inspection_") {
			high[fmtInspectionKey(o.ItemID, o.InstanceIdx)] = o
		}
	}
	var ratios []float64
	for _, low := range rows {
		if low.CheckKey != "D06" {
			continue
		}
		c.Samples.Observed++
		h, ok := high[fmtInspectionKey(low.ItemID, low.InstanceIdx%1000)]
		if !ok || !h.Target.EligibleForIntelligence() || !low.Target.EligibleForIntelligence() || !h.Target.ReasoningObserved || !low.Target.ReasoningObserved || h.Target.ReasoningTokens <= 0 || low.Target.ReasoningTokens <= 0 {
			c.Samples.Excluded++
			continue
		}
		ratios = append(ratios, float64(h.Target.ReasoningTokens)/float64(low.Target.ReasoningTokens))
	}
	c.Status, c.Reason, c.Evidence.Code = "inconclusive", "按题目判定的推理档位样本不足，零值或缺失用量不能证明降档。", "reasoning_pairs_insufficient"
	if len(ratios) < int(d.Thresholds["min_samples"]) || len(ratios) == 0 {
		return
	}
	ratio, _ := percentile(ratios, .5)
	c.Metrics["high_low_ratio"] = metric(ratio, "ratio")
	if ratio >= d.Thresholds["absolute_high_low_ratio"] {
		itemPass(c)
	} else {
		c.Reason, c.Evidence.Code = "未见明确的推理档位量差；无基线无法区分厂商默认行为与被降档。", "needs_baseline"
	}
}

func inspectItemShared(c *inspection.Check, rows []Outcome, d inspection.EffectiveDefinition) {
	var candidates []Outcome
	for _, o := range rows {
		if o.InstanceIdx < 1000 && strings.HasPrefix(o.Suite, "inspection_") && o.CheckKey != "A05" {
			candidates = append(candidates, o)
		}
	}
	usable := itemObserved(c, candidates, func(o Outcome) bool {
		return o.Target.OK() && (d.Key != "A02" || o.Target.ResponseModel != "" && o.RequestedModel != "")
	})
	if len(usable) == 0 {
		return
	}
	mismatch := 0
	for _, o := range usable {
		if d.Key == "A02" && canonicalInspectionModel(o.Target.ResponseModel) != canonicalInspectionModel(o.RequestedModel) || d.Key == "A12" && o.Target.HasFlag("refusal") {
			mismatch++
		}
	}
	itemPass(c)
	c.Factor = nil
	c.Metrics["mismatches"] = metric(float64(mismatch), "count")
	if mismatch > 0 {
		c.Status, c.Evidence.Code = "warning", "item_function_failed"
		c.Reason = "被测响应型号与请求型号不一致，请按官方回退规则复核；未与基线对照。"
		if d.Key == "A12" {
			c.Reason = "无害题观察到拒答标记，需复核接入行为；未与基线对照。"
		}
	}
}

func inspectItemCache(c *inspection.Check, steps *[2]int64, d inspection.EffectiveDefinition, support *bool) {
	c.Evidence.Source = "item_probe"
	c.Samples = inspection.Samples{Observed: 1}
	c.Status, c.Reason, c.Evidence.Code, c.Factor = "inconclusive", "缓存使用量未提供或两步未完成。", "cache_usage_unknown", nil
	if steps == nil {
		c.Samples.Excluded = 1
		return
	}
	c.Metrics["first_cache_read_tokens"] = metric(float64(steps[0]), "tokens")
	c.Metrics["second_cache_read_tokens"] = metric(float64(steps[1]), "tokens")
	switch {
	case steps[1] > 0:
		itemPass(c)
	case support == nil:
		itemCapabilityUnknown(c)
	case !*support:
		itemUnsupported(c)
	default:
		c.Status, c.Reason, c.Evidence.Code = "warning", "官方支持缓存，被测第二步未命中；未与基线对照。", "item_function_failed"
		c.Factor = numberPointer(d.Coefficients["failure"])
	}
}
