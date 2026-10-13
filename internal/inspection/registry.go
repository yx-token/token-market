package inspection

import (
	"sort"
	"strings"
)

type Definition struct {
	Key            string             `json:"key"`
	Name           string             `json:"name"`
	Category       string             `json:"category"`
	Flow           string             `json:"flow"`
	Role           string             `json:"role"`
	FactorGroup    string             `json:"factor_group"`
	Enabled        bool               `json:"enabled"`
	DefaultPricing bool               `json:"default_pricing"`
	Coefficients   map[string]float64 `json:"coefficients"`
	Thresholds     map[string]float64 `json:"thresholds"`
	Description    string             `json:"description"`
	Advice         string             `json:"advice"`
}

type EffectiveDefinition struct {
	Definition
	Pricing         bool   `json:"pricing"`
	Calibrated      bool   `json:"calibrated"`
	CalibrationNote string `json:"calibration_note"`
}

var definitions = map[string]Definition{}

func register(definition Definition) { definitions[definition.Key] = definition }

func init() {
	add := func(key, name, category, role, group, advice string, coefficients, thresholds map[string]float64) {
		if coefficients == nil {
			coefficients = map[string]float64{}
		}
		// 旧的专用系数继续保留，状态兜底也允许独立配置。
		for _, severity := range []string{"warning", "severe"} {
			if _, exists := coefficients[severity]; !exists {
				coefficients[severity] = 1
				if failure, exists := coefficients["failure"]; exists {
					coefficients[severity] = failure
				}
			}
		}
		if thresholds == nil {
			thresholds = map[string]float64{}
		}
		flow := FlowIdentity
		switch key {
		// A06 推理档位由 D06 的用量观测推出，与测智同属一条流程。
		case "A06", "D06", "D07", "D07.honesty", "D09":
			flow = FlowIntelligence
		case "D14":
			flow = FlowDegradation
		case "D13", "D04", "D10", "D05":
			flow = FlowCommon
		}
		register(Definition{Key: key, Name: name, Category: category, Role: role, FactorGroup: group,
			Flow: flow, Enabled: key != "A15", DefaultPricing: false, Coefficients: coefficients, Thresholds: thresholds, Advice: advice,
			Description: "检测为抽样观测；开启用于扣价后影响质量系数，校准只决定正式健康门槛能否确认。"})
	}
	add("A01", "订阅档位", "configuration", "display", "identity", "核对授权档位与上架声明，无法核实时保留未核验状态。", nil, nil)
	add("A02", "响应型号一致", "authenticity", "display", "identity", "核对声明型号、官方回退规则与接入配置，完成复检。", map[string]float64{"warning": 0.85, "severe": 0.70}, map[string]float64{"min_pairs": 2})
	add("A03", "输入计数指纹", "authenticity", "display", "identity", "确认被测与基线采用相同接入方式，复核输入计数差异。", map[string]float64{"warning": 0.85, "severe": 0.70}, map[string]float64{"min_pairs": 2})
	add("A04", "型号权限", "configuration", "gate", "function", "核对账号对上架型号的访问权限，修复后重新检测。", map[string]float64{"warning": 0.70, "severe": 0.70}, map[string]float64{"min_pairs": 1})
	add("A05", "上下文边界", "configuration", "gate", "function", "核对声明上下文长度、请求预算与入口行为，修复后完成完整复检。", map[string]float64{"warning": 0.70, "severe": 0.70}, map[string]float64{"probe_tokens": 262145, "max_probe_tokens": 524288, "min_input_ratio": 0.95})
	add("A06", "推理档位", "configuration", "display", "identity", "核对型号支持的推理档位及实际接入配置。", nil, nil)
	add("A08", "知识代际", "display", "display", "identity", "与同时段同型号基线对照，避免将过期事实或缺失信息作为确定失败。", nil, nil)
	add("A12", "回退与拒答", "authenticity", "display", "identity", "检查无害请求的回退与拒答情况，并对照同期基线。", map[string]float64{"warning": 0.85, "severe": 0.70}, map[string]float64{"min_pairs": 10})
	add("A15", "参数规则指纹", "authenticity", "display", "identity", "完成同型号 A/A 校准；被网关本地拦截的规则不能作为厂商指纹。", map[string]float64{"warning": 0.85, "severe": 0.70}, nil)
	add("third_party", "第三方身份核验", "authenticity", "gate", "identity", "核对第三方核验结果；证据不足时重新核验。", map[string]float64{"warning": 0.85, "severe": 0.70}, nil)
	add("D05", "额度窗口", "display", "display", "availability", "关注账号窗口余量；未知余量不阻塞上架、申诉或管理员检测。", nil, nil)
	add("D06", "推理强度兑现", "capability", "factor", "capability", "检查推理档位及用量观测，修复后与同期基线复检。",
		map[string]float64{"severe": 0.70, "warning": 0.85}, map[string]float64{"min_samples": 10, "severe_ratio": 0.6, "warning_ratio": 0.8, "ci_upper": 0.8, "absolute_high_low_ratio": 1.3})
	add("D07", "智力测试", "capability", "factor", "capability", "检查模型路由与接入配置，修复后复检。",
		map[string]float64{"severe": 0.70, "warning": 0.85}, map[string]float64{"min_pairs": 20, "alpha": 0.05, "severe_relative": 0.7, "warning_relative": 0.85, "expected_pass_rate": 0.9})
	add("D07.honesty", "诚实与弃权", "capability", "factor", "capability", "在信息不足时准确说明无法确定，修复后复检。",
		map[string]float64{"severe": 0.70, "warning": 0.85}, map[string]float64{"min_pairs": 20, "alpha": 0.05, "severe_relative": 0.7, "warning_relative": 0.85, "expected_pass_rate": 0.9})
	add("D09", "上下文深度", "capability", "factor", "capability", "检查长上下文完整保留与检索能力，修复后完成完整复检。",
		map[string]float64{"severe": 0.70, "warning": 0.85}, map[string]float64{"min_pairs": 3, "warning_relative": 0.8, "severe_relative": 0.7, "expected_pass_rate": 0.9})
	// 降智题按厂商或型号限定，单次检测通常只有一两道适用题，最小配对数不能照搬 D07 的 20。
	add("D14", "降智检测", "capability", "factor", "capability", "核对厂商是否把请求路由到其他型号；确认接入配置后复检。",
		map[string]float64{"severe": 0.70, "warning": 0.85}, map[string]float64{"min_pairs": 1, "alpha": 0.05, "severe_relative": 0.7, "warning_relative": 0.85, "expected_pass_rate": 0.9})
	add("D08", "输出完整性", "capability", "factor", "function", "检查输出上限及内容完整性，避免占位或省略后完成复检。",
		map[string]float64{"failure": 0.95}, map[string]float64{"min_pairs": 1})
	for _, feature := range []struct{ key, name string }{{"thinking", "推理功能"}, {"tools", "工具调用"}, {"image_pdf", "图片与 PDF"}, {"json_schema", "结构化输出"}, {"multiturn", "多轮保持"}} {
		add("A11."+feature.key, feature.name, "function", "factor", "function", "核对对应功能是否获得授权且被接入方式保留，修复后复检。",
			map[string]float64{"failure": 0.95}, map[string]float64{"min_pairs": 1})
	}
	add("D12", "缓存功能", "function", "factor", "function", "检查缓存支持与长前缀复用，排除计数缺失后复检。",
		map[string]float64{"failure": 0.85}, map[string]float64{"min_pairs": 1})
	add("D13", "可用率", "availability", "factor", "availability", "检查账号权限、额度与连接稳定性，排除平台侧错误后复检。",
		map[string]float64{"warning": 0.97, "severe": 0.7, "tier_995": 1, "tier_99": 0.97, "tier_98": 0.93, "tier_95": 0.85, "tier_90": 0.7}, map[string]float64{"min_samples": 100, "gate_min_samples": 100, "recovery_min_samples": 20, "gate_rate": 0.9})
	add("D04", "异常限流", "availability", "factor", "availability", "核对额度窗口与限流来源，排除网关本地限制后复检。",
		map[string]float64{"warning": 0.95, "severe": 0.85}, map[string]float64{"min_samples": 100, "recovery_min_samples": 20, "warning_rate": 0.02, "severe_rate": 0.1})
	add("D10", "响应性能", "performance", "factor", "performance", "检查延迟与吞吐，结合同行或同期基线完成复检。",
		map[string]float64{"warning": 0.97, "severe": 0.93, "worst": 0.85, "bonus": 1.05}, map[string]float64{"min_pairs": 4, "warning_ratio": 1.5, "severe_ratio": 2})
}

func copyMap(source map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func Lookup(key string) (Definition, bool) {
	definition, ok := definitions[key]
	if ok {
		definition.Coefficients, definition.Thresholds = copyMap(definition.Coefficients), copyMap(definition.Thresholds)
	}
	return definition, ok
}

func Known() []Definition {
	result := make([]Definition, 0, len(definitions))
	for key := range definitions {
		definition, _ := Lookup(key)
		result = append(result, definition)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func Effective(key string, settings Settings) (EffectiveDefinition, bool) {
	definition, ok := Lookup(key)
	if !ok {
		return EffectiveDefinition{}, false
	}
	override := settings.CheckOverrides[key]
	if override.Enabled != nil {
		definition.Enabled = *override.Enabled
	}
	for name, value := range override.Coefficients {
		if _, allowed := definition.Coefficients[name]; allowed && finite(value) && value > 0 && value <= 1.05 {
			definition.Coefficients[name] = value
		}
	}
	for name, value := range override.Thresholds {
		if _, allowed := definition.Thresholds[name]; allowed && finite(value) && value >= 0 {
			definition.Thresholds[name] = value
		}
	}
	calibrated := override.Calibrated && strings.TrimSpace(override.CalibrationNote) != ""
	pricing := definition.DefaultPricing
	if override.Pricing != nil {
		pricing = *override.Pricing
	}
	return EffectiveDefinition{Definition: definition, Pricing: pricing, Calibrated: calibrated, CalibrationNote: override.CalibrationNote}, true
}
