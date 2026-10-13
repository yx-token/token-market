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
	raw := mustJSON(map[string]any{
		"checks": checks, "gates": gates, "inspection_settings": st,
		"model_capability_defaults": inspection.CapabilityDefaults(),
		"model_capabilities":        []inspection.ModelCapability{{Platform: "openai", Model: "私有型号登记"}},
	})
	safe := publicReportSummary(raw)
	for _, secret := range []string{"expected_pass_rate", "relative_score", "target_pass_rate", "model_capabilit", "inspection_settings", "私有型号登记", "item_pass_rate_low", "0.9", "0.7"} {
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
	if c := report.Checks["D07"]; c.Status != "fail" || c.Evidence.Source != "item_probe" || c.Factor != nil {
		t.Fatalf("卖家接口丢失安全结论或泄露系数：%+v", c)
	}
}
