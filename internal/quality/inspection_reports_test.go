package quality

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestPublicInspectionReportRemovesReferenceErrorsAndPrivateWarnings(t *testing.T) {
	raw := mustJSON(map[string]any{
		"verdict":                "normal",
		"suites":                 map[string]any{"availability": map[string]int{"success": 1, "total": 1}},
		"error_counts":           map[string]int{"platform": 1},
		"reference_error_counts": map[string]int{"platform": 99},
		"warnings": []string{
			"窗口余量未知",
			"影子检测未完成，仅保留证据：基线账号 42，192.0.2.1，ANSWER: 314159，threshold=0.8123",
			"内部定位：基线账号 42，192.0.2.1，ANSWER: 314159，threshold=0.8123",
			"窗口余量未知",
		},
	})
	safe := publicReportSummary(raw)
	for _, private := range []string{"reference_error_counts", "基线账号", "192.0.2.1", "314159", "0.8123", "内部定位"} {
		if strings.Contains(string(safe), private) {
			t.Fatalf("卖家报告泄露内部信息 %q：%s", private, safe)
		}
	}
	var summary struct {
		Warnings    []string       `json:"warnings"`
		ErrorCounts map[string]int `json:"error_counts"`
		Suites      map[string]any `json:"suites"`
	}
	if err := json.Unmarshal(safe, &summary); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(summary.Warnings, []string{"窗口余量未知", "新质检未完成，仅保留本次已取得的证据。"}) || summary.ErrorCounts["platform"] != 1 || summary.Suites["availability"] == nil {
		t.Fatalf("脱敏不能丢失公开结论、兼容套件或安全告警：%s", safe)
	}
	if !strings.Contains(string(raw), "reference_error_counts") || !strings.Contains(string(raw), "192.0.2.1") {
		t.Fatal("公开脱敏不能修改管理员原始证据")
	}
}

func TestPublicInspectionReportDropsUnknownAndMalformedWarnings(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"warnings":["窗口余量未知：192.0.2.1","未知告警含秘密"]}`),
		json.RawMessage(`{"warnings":"内部错误正文"}`),
		json.RawMessage(`{"warnings":[{"message":"内部错误正文"}]}`),
	} {
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(publicReportSummary(raw), &summary); err != nil {
			t.Fatal(err)
		}
		if _, exists := summary["warnings"]; exists {
			t.Fatalf("不在白名单或结构畸形的告警不能公开：%s", raw)
		}
	}
}

func TestPublicInspectionWarningsKeepSafeOperationalAdvice(t *testing.T) {
	warnings := []string{
		"旧题模式 token 预算超出，仅告警，检测继续",
		"影子计划超过实例或快照上限，已停止扩样",
		"影子模板配置无效，请管理员检查",
		"影子实例生成失败，请管理员检查模板参数",
		"影子实例判分配置无效，请管理员检查",
		"影子步骤配置无效，请管理员检查",
		"恢复复测未有效覆盖全部上次失败项，已退避重排",
	}
	want := []string{
		"旧题检测预算超出预估，检测继续。",
		"部分质检超过执行上限，未完成项目不会记为通过。",
		"部分质检配置需要管理员复核，已保留其他有效结果。",
		"恢复复测未完成全部必测项目，稍后重新检测。",
	}
	if got := publicInspectionWarnings(warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("安全告警映射或去重错误：%v", got)
	}
}

func TestPublicItemReportDoesNotExposeCapabilitiesOrThresholds(t *testing.T) {
	st := calibratedInspectionSettings()
	checks, gates := Inspect(itemRows("D07", 100, 40), st, time.Time{})
	aggregated := inspection.Aggregate(checks, gates, "trial", st)
	checks = aggregated.Checks
	raw := mustJSON(map[string]any{
		"checks": checks, "gates": gates, "inspection_settings": st,
		"model_capability_defaults": inspection.CapabilityDefaults(),
		"model_capabilities":        []inspection.ModelCapability{{Platform: "openai", Model: "私有型号登记"}},
	})
	safe := publicReportSummary(raw)
	for _, secret := range []string{"expected_pass_rate", "relative_score", "target_pass_rate", "model_capabilit", "inspection_settings", "私有型号登记", "item_pass_rate_low", "0.9"} {
		if strings.Contains(string(safe), secret) {
			t.Fatalf("卖家接口泄露 %s：%s", secret, safe)
		}
	}
	var report struct {
		Checks map[string]inspection.Check `json:"checks"`
	}
	if err := json.Unmarshal(safe, &report); err != nil {
		t.Fatal(err)
	}
	if c := report.Checks["D07"]; c.Status != "fail" || c.Evidence.Source != "item_probe" || c.Factor == nil {
		t.Fatalf("卖家接口丢失安全结论或单项系数：%+v", c)
	}
}
func thirdPartySummaryFixture() json.RawMessage {
	return mustJSON(map[string]any{
		"verdict":         "suspected",
		"trigger":         "onboarding",
		"model":           "gpt-6-astra",
		"flows":           []string{"intelligence", "degradation", "identity"},
		"identity_source": "third_party",
		"models":          []string{"gpt-6-astra"},
		"degradation": map[string]any{
			"status": "fail",
			"models": []map[string]any{{"model": "gpt-6-astra", "status": "fail", "target_pass_rate": 0.514286, "samples": 41}},
		},
		"third_party": []map[string]any{{
			"provider":   "bazaarlink",
			"status":     "completed",
			"verdict":    "suspected",
			"model":      "gpt-6-astra",
			"report_url": "https://bazaarlink.ai/probe/abc123",
			"summary": map[string]any{
				"verdict_status": "clean_match_submodel_mismatch",
				"true_family":    "openai",
				"true_model":     "gpt-4o",
				"confidence":     "high",
				"categories": []map[string]any{
					{"key": "identity", "name": "身份识别", "total": 29, "passed": 29, "warning": 0, "failed": 0, "error": 0},
					{"key": "unmapped", "name": "unmapped", "total": 1, "passed": 0, "warning": 0, "failed": 1, "error": 0},
				},
				"risks":         []map[string]any{{"category": "integrity", "category_name": "完整性检查", "label": "Context 长度測試", "severity": "fail", "reason": "HTTP 503"}},
				"failed_probes": []map[string]any{{"label": "Context 长度測試", "group": "integrity", "reason": "HTTP 503"}},
				"items":         map[string]int{"total": 86, "passed": 73},
			},
			"external_id": "run-abc123",
		}},
		"checks": map[string]inspection.Check{
			"D14":         {Key: "D14", Status: "fail", Evidence: inspection.Evidence{Source: "item_probe"}},
			"third_party": {Key: "third_party", Status: "fail", Evidence: inspection.Evidence{Source: "third_party"}},
		},
		"gates": []inspection.Gate{
			{CheckKey: "third_party", Code: "identity_mismatch", Health: "degraded", Confirmed: true, Reason: "第三方报告确认型号身份异常。"},
			{CheckKey: "D14", Code: "degradation_low", Health: "degraded", Confirmed: true, Reason: "配对准确率显著低于同期基线。"},
		},
		"checked_at": "2026-10-14T02:00:00Z",
	})
}

// 卖家报告不得出现第三方核验渠道，改为下发脱敏的 verification。
func TestPublicReportSummaryHidesThirdPartyChannel(t *testing.T) {
	safe := string(publicReportSummary(thirdPartySummaryFixture()))
	for _, secret := range []string{"bazaarlink", "report_url", "external_id", "failed_probes", "identity_source",
		"provider", "abc123", "probe/abc123"} {
		if strings.Contains(safe, secret) {
			t.Fatalf("卖家报告泄露第三方渠道信息 %q：%s", secret, safe)
		}
	}
	var public struct {
		Verification struct {
			Status      string `json:"status"`
			VerdictText string `json:"verdict_text"`
			CheckedAt   string `json:"checked_at"`
			TrueFamily  string `json:"true_family"`
			TrueModel   string `json:"true_model"`
			Categories  []struct {
				Key   string `json:"key"`
				Name  string `json:"name"`
				Total int    `json:"total"`
			} `json:"categories"`
			Risks []struct {
				CategoryName string `json:"category_name"`
				Label        string `json:"label"`
				Severity     string `json:"severity"`
				Reason       string `json:"reason"`
			} `json:"risks"`
		} `json:"verification"`
		Degradation struct {
			Status string                       `json:"status"`
			Models []map[string]json.RawMessage `json:"models"`
		} `json:"degradation"`
		Checks map[string]json.RawMessage `json:"checks"`
	}
	if err := json.Unmarshal([]byte(safe), &public); err != nil {
		t.Fatal(err)
	}
	// 折算结论保留现有枚举，前端口径一致。
	if public.Verification.Status != "suspected" || public.Verification.VerdictText != "家族一致，但不是声明的具体型号" ||
		public.Verification.CheckedAt == "" || public.Verification.TrueFamily != "openai" || public.Verification.TrueModel != "gpt-4o" {
		t.Fatalf("核验结论缺失：%+v", public.Verification)
	}
	if len(public.Verification.Categories) != 2 || public.Verification.Categories[1].Key != "unmapped" || public.Verification.Categories[1].Name != "unmapped" {
		t.Fatalf("核验大类或未知分组丢失：%+v", public.Verification.Categories)
	}
	if len(public.Verification.Risks) != 1 || public.Verification.Risks[0].Severity != "fail" || public.Verification.Risks[0].Label != "Context 长度測試" {
		t.Fatalf("核验风险项缺失：%+v", public.Verification.Risks)
	}
	if public.Degradation.Status != "fail" || len(public.Degradation.Models) != 1 {
		t.Fatalf("降智按模型结论缺失：%+v", public.Degradation)
	}
	if len(public.Degradation.Models[0]) != 2 {
		t.Fatalf("卖家降智模型项只能含型号与状态：%+v", public.Degradation.Models[0])
	}
	if _, ok := public.Checks["third_party"]; ok {
		t.Fatalf("卖家 checks 不得出现第三方项：%s", safe)
	}
	if _, ok := public.Checks["D14"]; !ok {
		t.Fatalf("卖家 checks 应保留自有检测项：%s", safe)
	}
	if strings.Contains(safe, "identity_mismatch") {
		t.Fatalf("卖家报告泄露第三方门槛证据码：%s", safe)
	}
}

// 没有第三方检测时不下发 verification。
func TestPublicReportSummaryWithoutVerification(t *testing.T) {
	safe := string(publicReportSummary(mustJSON(map[string]any{"verdict": "normal", "flows": []string{"intelligence"}})))
	if strings.Contains(safe, "verification") {
		t.Fatalf("无第三方检测不应下发核验结果：%s", safe)
	}
}

// 折算结论按最差取值，前端状态档位据此着色。
func TestPublicVerificationStatusEnums(t *testing.T) {
	for _, tc := range []struct {
		verdicts []string
		want     string
	}{
		{[]string{"normal"}, "normal"},
		{[]string{"inconclusive"}, "inconclusive"},
		{[]string{"normal", "suspected"}, "suspected"},
		{[]string{"suspected", "degraded"}, "degraded"},
		{[]string{"degraded", "normal"}, "degraded"},
	} {
		results := []map[string]any{}
		for _, verdict := range tc.verdicts {
			results = append(results, map[string]any{"verdict": verdict, "summary": map[string]any{"verdict_status": "clean_match"}})
		}
		var out struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(publicVerification(mustJSON(results), []byte(`"2026-10-14T02:00:00Z"`)), &out); err != nil {
			t.Fatal(err)
		}
		if out.Status != tc.want {
			t.Fatalf("折算结论=%s，期望=%s：%v", out.Status, tc.want, tc.verdicts)
		}
	}
}
