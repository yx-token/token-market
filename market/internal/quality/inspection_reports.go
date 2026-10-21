package quality

import (
	"encoding/json"
	"strings"

	"github.com/yx-token/token-market/market/internal/inspection"
)

// 卖家报告只取已登记字段，逐题、阈值、基线身份与错误体留在管理接口。
// 卖家不展示第三方检测渠道：provider、站点报告地址、external_id 与站点原始明细都不出现，
// 由固定的核验结果区块（verification）代替。
func publicReportSummary(raw json.RawMessage) json.RawMessage {
	var summary map[string]json.RawMessage
	if json.Unmarshal(raw, &summary) != nil {
		return raw
	}
	allowed := []string{"verdict", "trigger", "model", "checked_at", "baseline", "grade", "tier", "suggestions", "suites", "inspection_mode", "mode", "checks", "quality_factor", "appraisal", "gates", "group_factors", "proposed_grade", "repaired_grade", "error_counts", "attribution", "internal_verdict", "shadow", "incomplete", "error", "flows", "models", "intelligence", "degradation", "identity"}
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
	// 测智与降智检测的流程摘要结构相同，都只给型号与状态；不适用的流程不下发，避免卖家报告里
	// 出现一整块「不适用」的空区块（例如白名单里没有配置降智题）。
	for _, flow := range []string{"intelligence", inspection.FlowDegradation} {
		value, ok := summary[flow]
		if !ok {
			continue
		}
		var header struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(value, &header) == nil && header.Status == "not_applicable" {
			delete(public, flow)
			continue
		}
		delete(public, flow)
		var detail map[string]json.RawMessage
		if json.Unmarshal(value, &detail) != nil {
			continue
		}
		var models []struct {
			Model  string `json:"model"`
			Status string `json:"status"`
		}
		if json.Unmarshal(detail["models"], &models) == nil {
			detail["models"] = mustJSON(models)
		} else {
			delete(detail, "models")
		}
		public[flow] = mustJSON(detail)
	}
	// 身份流程只给卖家结论与沿用时间：来源（第三方 / 自有题库）属于平台内部实现，卖家不需要知道这次怎么检测的。
	if value, ok := summary["identity"]; ok {
		delete(public, "identity")
		var detail map[string]json.RawMessage
		if json.Unmarshal(value, &detail) == nil {
			delete(detail, "source")
			public["identity"] = mustJSON(detail)
		}
	}
	if value, ok := summary["checks"]; ok {
		var checks map[string]inspection.Check
		delete(public, "checks")
		if json.Unmarshal(value, &checks) == nil {
			// 第三方核验项不走逐项检查展示，卖家侧改为 verification 区块。
			delete(checks, "third_party")
			public["checks"] = mustJSON(inspection.PublicChecks(checks))
		}
	}
	if value, ok := summary["third_party"]; ok {
		if verification := publicVerification(value, verificationCheckedAt(summary)); verification != nil {
			public["verification"] = verification
		}
	}
	if value, ok := summary["gates"]; ok {
		var gates []inspection.Gate
		delete(public, "gates")
		if json.Unmarshal(value, &gates) == nil {
			kept := gates[:0]
			for _, gate := range gates {
				// 身份门槛来自第三方核验渠道，卖家侧只在 verification 里看到折算结论。
				if gate.CheckKey == "third_party" {
					continue
				}
				gate.Code = ""
				gate.Reason = "本次质检记录了门槛事件，请按对应质检项建议修复后复检。"
				kept = append(kept, gate)
			}
			public["gates"] = mustJSON(kept)
		}
	}
	return mustJSON(public)
}

// verificationVerdictText 是检测站逐项判定状态对卖家的中文文案（与前端口径一致）。
var verificationVerdictText = map[string]string{
	"clean_match":                   "家族与具体型号都与声明一致",
	"clean_match_family_only":       "家族一致，具体型号无法确定",
	"clean_match_submodel_mismatch": "家族一致，但不是声明的具体型号",
	"plain_mismatch":                "证据指向其他模型家族",
	"spoof_behavior_induced":        "行为被诱导成声明的模型，指纹指向其他模型",
	"spoof_selfclaim_forged":        "自称是声明的模型，但行为指向其他模型",
	"ambiguous":                     "证据相互矛盾，无法判定",
	"insufficient_data":             "有效证据不足",
}

// worstThirdPartyVerdict 取两条第三方折算结论中更严重的一条；未登记的取值视为 inconclusive。
func worstThirdPartyVerdict(current, next string) string {
	ranks := map[string]int{"normal": 1, "inconclusive": 2, "suspected": 3, "degraded": 4}
	if ranks[next] > ranks[current] {
		return next
	}
	return current
}

// verificationCheckedAt 取本次检测的核验时间；旧报告没有该字段时不下发 checked_at。
func verificationCheckedAt(summary map[string]json.RawMessage) json.RawMessage {
	if value, ok := summary["checked_at"]; ok {
		return value
	}
	return nil
}

// publicVerification 把第三方检测结果折算成卖家可见的核验结果：只有结论、各大类计数与风险项，
// 不含渠道名、站点报告地址、external_id 与逐题明细。
func publicVerification(raw json.RawMessage, checkedAt json.RawMessage) json.RawMessage {
	var results []struct {
		Verdict string `json:"verdict"`
		Summary struct {
			VerdictStatus string          `json:"verdict_status"`
			TrueFamily    string          `json:"true_family"`
			TrueModel     string          `json:"true_model"`
			Categories    json.RawMessage `json:"categories"`
			Risks         json.RawMessage `json:"risks"`
		} `json:"summary"`
	}
	if json.Unmarshal(raw, &results) != nil || len(results) == 0 {
		return nil
	}
	// 多条第三方核验按折算结论取最差（degraded > suspected > inconclusive > normal）。
	status := ""
	for _, result := range results {
		status = worstThirdPartyVerdict(status, result.Verdict)
		if status == "degraded" {
			break
		}
	}
	if status == "" {
		return nil
	}
	out := map[string]any{"status": status, "verdict_text": verificationVerdictText[results[0].Summary.VerdictStatus]}
	if out["verdict_text"] == "" {
		out["verdict_text"] = results[0].Summary.VerdictStatus
	}
	if family := results[0].Summary.TrueFamily; family != "" {
		out["true_family"] = family
	}
	if model := results[0].Summary.TrueModel; model != "" {
		out["true_model"] = model
	}
	if len(results[0].Summary.Categories) > 0 {
		out["categories"] = results[0].Summary.Categories
	}
	if len(results[0].Summary.Risks) > 0 {
		out["risks"] = results[0].Summary.Risks
	}
	// 核验时间取本次检测的完成时间，卖家据此判断结论的新旧。
	if len(checkedAt) > 0 {
		out["checked_at"] = checkedAt
	}
	return mustJSON(out)
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
