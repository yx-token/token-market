package quality

import (
	"encoding/json"
	"strings"

	"github.com/DueGin/token-market/internal/inspection"
)

// 卖家报告只取已登记字段，逐题、阈值、基线身份与错误体留在管理接口。
func publicReportSummary(raw json.RawMessage) json.RawMessage {
	var summary map[string]json.RawMessage
	if json.Unmarshal(raw, &summary) != nil {
		return raw
	}
	allowed := []string{"verdict", "trigger", "model", "checked_at", "baseline", "grade", "tier", "suggestions", "suites", "inspection_mode", "mode", "checks", "quality_factor", "appraisal", "gates", "group_factors", "proposed_grade", "repaired_grade", "error_counts", "attribution", "third_party", "internal_verdict", "shadow", "incomplete", "error", "flows", "identity_source", "models", "intelligence", "identity"}
	public := map[string]json.RawMessage{}
	for _, key := range allowed {
		if value, ok := summary[key]; ok {
			public[key] = value
		}
	}
	if value, ok := summary["warnings"]; ok {
		var warnings []string
		if json.Unmarshal(value, &warnings) == nil {
			if safe := publicInspectionWarnings(warnings); len(safe) > 0 {
				public["warnings"] = mustJSON(safe)
			}
		}
	}
	if value, ok := summary["intelligence"]; ok {
		var flow map[string]json.RawMessage
		delete(public, "intelligence")
		if json.Unmarshal(value, &flow) == nil {
			var models []struct {
				Model  string `json:"model"`
				Status string `json:"status"`
			}
			if json.Unmarshal(flow["models"], &models) == nil {
				flow["models"] = mustJSON(models)
			} else {
				delete(flow, "models")
			}
			public["intelligence"] = mustJSON(flow)
		}
	}
	if value, ok := summary["checks"]; ok {
		var checks map[string]inspection.Check
		delete(public, "checks")
		if json.Unmarshal(value, &checks) == nil {
			public["checks"] = mustJSON(inspection.PublicChecks(checks))
		}
	}
	if value, ok := summary["gates"]; ok {
		var gates []inspection.Gate
		delete(public, "gates")
		if json.Unmarshal(value, &gates) == nil {
			for i := range gates {
				gates[i].Code = ""
				gates[i].Reason = "本次质检记录了门槛事件，请按对应质检项建议修复后复检。"
			}
			public["gates"] = mustJSON(gates)
		}
	}
	return mustJSON(public)
}

// 告警只返回固定安全文案，底层错误中的题面、阈值和内部定位信息留在管理接口。
func publicInspectionWarnings(warnings []string) []string {
	var safe []string
	for _, warning := range warnings {
		var message string
		switch warning {
		case "窗口余量未知":
			message = "窗口余量未知"
		case "旧题模式 token 预算超出，仅告警，检测继续":
			message = "旧题检测预算超出预估，检测继续。"
		case "影子计划超过实例或快照上限，已停止扩样", "检测计划超过实例或快照上限，已停止扩样", "检测计划超过实例上限，已停止扩样":
			message = "部分质检超过执行上限，未完成项目不会记为通过。"
		case "影子模板配置无效，请管理员检查", "影子实例生成失败，请管理员检查模板参数", "影子实例判分配置无效，请管理员检查", "影子步骤配置无效，请管理员检查", "检测模板配置无效，请管理员检查", "检测实例生成失败，请管理员检查模板参数", "检测实例判分配置无效，请管理员检查", "检测步骤配置无效，请管理员检查":
			message = "部分质检配置需要管理员复核，已保留其他有效结果。"
		case "恢复复测未有效覆盖全部上次失败项，已退避重排":
			message = "恢复复测未完成全部必测项目，稍后重新检测。"
		default:
			if strings.HasPrefix(warning, "影子检测未完成，仅保留证据：") {
				message = "新质检未完成，仅保留本次已取得的证据。"
			}
		}
		if message != "" && !containsAny(safe, message) {
			safe = append(safe, message)
		}
	}
	return safe
}
