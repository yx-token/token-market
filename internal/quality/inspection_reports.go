package quality

import (
	"encoding/json"
	"github.com/DueGin/token-market/internal/inspection"
)

// 卖家报告只取已登记字段，逐题、阈值、基线身份与错误体留在管理接口。
func publicReportSummary(raw json.RawMessage) json.RawMessage {
	var summary map[string]json.RawMessage
	if json.Unmarshal(raw, &summary) != nil {
		return raw
	}
	allowed := []string{"verdict", "trigger", "model", "checked_at", "baseline", "grade", "tier", "suggestions", "suites", "inspection_mode", "mode", "checks", "quality_factor", "appraisal", "gates", "group_factors", "proposed_grade", "repaired_grade", "warnings", "error_counts", "reference_error_counts", "attribution", "third_party", "internal_verdict", "shadow", "incomplete", "error"}
	public := map[string]json.RawMessage{}
	for _, key := range allowed {
		if value, ok := summary[key]; ok {
			public[key] = value
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
