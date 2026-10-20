package quality

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

func calibratedInspectionSettings() InspectionSettings {
	st := defaultInspectionSettings()
	st.Mode = "formal"
	for _, d := range inspection.Known() {
		enabled := true
		st.CheckOverrides[d.Key] = inspection.CheckOverride{Enabled: &enabled, Pricing: &enabled, Calibrated: true, CalibrationNote: "离线替身校准记录；只验证程序口径，不代表真实运营校准"}
	}
	return st
}

func int64Pointer(n int64) *int64 { return &n }
func pairedInspectionOutcome(key string, pass, refPass bool) Outcome {
	ref := ProbeResult{HTTPStatus: 200, ResponseModel: "claude-sonnet-4-5", ReasoningObserved: true, ReasoningTokens: 100, Flags: []string{"stop_normal"}}
	return Outcome{CheckKey: key, Suite: "inspection_capability", Target: ref, Ref: &ref, TargetPass: pass, RefPass: refPass, RequestedModel: ref.ResponseModel, PrimaryModel: true, StaticComparable: true}
}

func TestInspectIntelligenceAndCalibration(t *testing.T) {
	rows := []Outcome{}
	for i := 0; i < 30; i++ {
		o := pairedInspectionOutcome("D07", i >= 15, true)
		o.ItemID = int64(i)
		rows = append(rows, o)
	}
	st := calibratedInspectionSettings()
	checks, gates := Inspect(rows, st, time.Now())
	if checks["D07"].Status != "fail" || checks["D07"].Factor == nil || *checks["D07"].Factor != .7 {
		t.Fatalf("配对扣价未检出：%+v", checks["D07"])
	}
	aggregated := inspection.Aggregate(checks, gates, st.Mode, st)
	if aggregated.QualityFactor == nil || *aggregated.QualityFactor != .7 || !aggregated.Checks["D07"].Applied {
		t.Fatalf("分组扣价失败：%+v", aggregated)
	}
	st.CheckOverrides = map[string]inspection.CheckOverride{}
	aggregated = inspection.Aggregate(checks, gates, st.Mode, st)
	if aggregated.QualityFactor != nil || aggregated.Checks["D07"].Applied {
		t.Fatal("未开启用于扣价的结果不能计价")
	}
	for _, st := range []InspectionSettings{defaultInspectionSettings(), st} {
		if got := assessFormal(rows, th, st); got.Samples != 0 || got.Verdict != "normal" || got.AvailableTotal != 30 {
			t.Fatalf("新题未进入可用统计或仍触发旧SPRT：%+v", got)
		}
	}
	rows[0].Ref = nil
	if got := assessFormal(rows[:1], th); got.AvailableTotal != 1 || got.Samples != 0 {
		t.Fatal("无同期基线仍应统计有效请求")
	}

}

func TestInspectFunctionTruncationAndRefusal(t *testing.T) {
	st := calibratedInspectionSettings()
	for _, key := range []string{"D08", "A11.tools", "A11.image_pdf", "A11.multiturn"} {
		o := pairedInspectionOutcome(key, false, true)
		o.Target.Flags = []string{"truncated"}
		checks, _ := Inspect([]Outcome{o}, st, time.Now())
		if checks[key].Status != "fail" {
			t.Fatalf("%s的截断必须作为功能失败：%+v", key, checks[key])
		}
		o.RefPass = false
		checks, _ = Inspect([]Outcome{o}, st, time.Now())
		if checks[key].Status != "not_applicable" {
			t.Fatalf("基线也失败时不归因卖家：%+v", checks[key])
		}
	}
	rows := []Outcome{}
	for i := 0; i < 10; i++ {
		o := pairedInspectionOutcome("D07", false, true)
		o.Target.Flags = []string{"refusal"}
		rows = append(rows, o)
	}
	checks, _ := Inspect(rows, st, time.Now())
	if checks["A12"].Status != "warning" || checks["D07"].Samples.Paired != 0 {
		t.Fatal("拒答应提示，不能计作智力失败")
	}
}

func TestInspectZeroObservationAndPermissionSources(t *testing.T) {
	st := calibratedInspectionSettings()
	if got := assessFormal([]Outcome{{Suite: "inspection_capability", CheckKey: "D07"}}, th); got.AvailableTotal != 0 || got.Verdict != "inconclusive" {
		t.Fatal("未派发题不能计有效失败")
	}
	o := pairedInspectionOutcome("A04", false, true)
	o.Target.HTTPStatus = 403
	o.Target.ErrorClass = "platform"
	o.Target.ErrorCode = "model_access_denied"
	checks, gates := Inspect([]Outcome{o}, st, time.Now())
	if len(gates) != 0 || checks["A04"].Status != "inconclusive" {
		t.Fatal("平台403不能触发卖家门槛")
	}
	o.Target.ErrorClass = "http_4xx"
	checks, gates = Inspect([]Outcome{o}, st, time.Now())
	if len(gates) != 1 || !gates[0].Confirmed || checks["A04"].Status != "fail" {
		t.Fatal("明确主型号权限拒绝必须记门槛")
	}
	o.PrimaryModel = false
	_, gates = Inspect([]Outcome{o}, st, time.Now())
	if len(gates) != 0 {
		t.Fatal("其他型号无权限不应作为主型号上架门槛")
	}
	o.CheckKey = "A05"
	o.Target.ErrorClass = "expected_reject"
	o.Target.ErrorCode = "context_length_exceeded"
	o.Target.HTTPStatus = 400
	_, gates = Inspect([]Outcome{o}, st, time.Now())
	if len(gates) != 1 {
		t.Fatal("明确上下文边界拒绝未记录")
	}
}

func TestInspectInputFingerprintSkipsDynamicSteps(t *testing.T) {
	st := calibratedInspectionSettings()
	rows := []Outcome{}
	for i := 0; i < 2; i++ {
		o := pairedInspectionOutcome("A11.tools", true, true)
		input := int64(10)
		refInput := int64(20)
		o.Target.InputTokens = &input
		o.Ref.InputTokens = &refInput
		o.StaticComparable = false
		rows = append(rows, o)
	}
	checks, _ := Inspect(rows, st, time.Now())
	if checks["A03"].Status != "inconclusive" {
		t.Fatal("动态会话末步不能用输入相等指纹")
	}
	for i := range rows {
		rows[i].StaticComparable = true
	}
	checks, _ = Inspect(rows, st, time.Now())
	if checks["A03"].Status != "warning" {
		t.Fatal("单步同输入计数差异应被提示")
	}
}

func TestInspectReasoningSmallSampleAndLowMetadata(t *testing.T) {
	st := calibratedInspectionSettings()
	override := st.CheckOverrides["D06"]
	override.Thresholds = map[string]float64{"min_samples": 1}
	st.CheckOverrides["D06"] = override
	rows := []Outcome{pairedInspectionOutcome("D07", true, true)}
	checks, _ := Inspect(rows, st, time.Now())
	if checks["D06"].Status != "inconclusive" {
		t.Fatal("一个样本不能给方差区间")
	}
	for i := 1; i < 12; i++ {
		o := pairedInspectionOutcome("D07", true, true)
		o.ItemID = int64(i)
		o.Target.ReasoningTokens = 50
		rows = append(rows, o)
	}
	checks, _ = Inspect(rows, st, time.Now())
	if _, err := json.Marshal(checks); err != nil {
		t.Fatal("不能存非有限区间", err)
	}
	if checks["D06"].Factor == nil || *checks["D06"].Factor != .7 {
		t.Fatalf("推理量下降未检出：%+v", checks["D06"])
	}
	low := plannedInstance{ItemID: 1, InstanceIdx: 1003, CheckKey: "D06", GeneratorVersion: "zebra/2", Item: Item{Suite: "inspection_capability"}, Steps: []plannedStep{{Request: ProbeRequest{Model: "claude-opus-5-5"}}}}
	outcome := instanceOutcome(low)
	if outcome.InstanceIdx != 1003 || outcome.RequestedModel != "claude-opus-5-5" || outcome.GeneratorVersion != "zebra/2" {
		t.Fatal("低档观测丢失实例元数据")
	}
}

func TestInspectionExactMcNemar(t *testing.T) {
	if mcnemarExact(0, 0) != 1 || mcnemarExact(3, 3) != 1 || math.Abs(mcnemarExact(10, 0)-.001953125) > 1e-12 {
		t.Fatal("精确二项尾概率错误")
	}
	if math.IsNaN(mcnemarExact(100000, 0)) {
		t.Fatal("大样本概率不能溢出")
	}
}

func TestInspectionReportRejectsMalformedSensitiveFields(t *testing.T) {
	raw := json.RawMessage(`{"suites":{"availability":{"total":1}},"checks":{"A03":{"metrics":{"threshold":0.85},"evidence":{"code":"secret-ip"},"reason":"192.0.2.1"}},"gates":"private-body","execution_snapshot":"secret-answer"}`)
	safe := string(publicReportSummary(raw))
	if strings.Contains(safe, "secret") || strings.Contains(safe, "192.0.2.1") || strings.Contains(safe, "private-body") || strings.Contains(safe, "threshold") {
		t.Fatal("卖家摘要未按白名单拒绝敏感结构", safe)
	}
	if !strings.Contains(safe, "suites") {
		t.Fatal("旧summary.suites必须保留")
	}
}

func TestInspectionBoundaryRequiresPreservedMarkers(t *testing.T) {
	st := calibratedInspectionSettings()
	o := pairedInspectionOutcome("A05", false, true)
	for _, input := range []*int64{nil, int64Pointer(100)} {
		o.Target.InputTokens, o.Ref.InputTokens = input, int64Pointer(100)
		checks, gates := Inspect([]Outcome{o}, st, time.Now())
		if checks["A05"].Status != "inconclusive" || len(gates) != 0 {
			t.Fatal("标记失败且计数不足不能判通过或确认截断")
		}
	}
	o.Target.InputTokens = int64Pointer(80)
	checks, gates := Inspect([]Outcome{o}, st, time.Now())
	if checks["A05"].Status != "fail" || len(gates) != 1 {
		t.Fatal("基线标记通过而被测标记失败并且输入明显减少应检出静默截断")
	}
	o.TargetPass = true
	checks, gates = Inspect([]Outcome{o}, st, time.Now())
	if checks["A05"].Status != "pass" || len(gates) != 0 {
		t.Fatal("标记通过时仅计数差异不能判静默截断")
	}
}

func TestInspectionThinkingDistinguishesMissingAndZero(t *testing.T) {
	st := calibratedInspectionSettings()
	o := pairedInspectionOutcome("A11.thinking", true, true)
	o.Target.ReasoningTokens = 0
	checks, _ := Inspect([]Outcome{o}, st, time.Now())
	if checks["A11.thinking"].Status != "fail" {
		t.Fatal("明确零推理用量未计功能失败")
	}
	o.Target.ReasoningObserved = false
	checks, _ = Inspect([]Outcome{o}, st, time.Now())
	if checks["A11.thinking"].Status != "inconclusive" || checks["A11.thinking"].Factor != nil {
		t.Fatal("缺失推理用量不能当作零扣价")
	}
	o.Target.ReasoningObserved = true
	o.Ref.ReasoningTokens = 0
	checks, _ = Inspect([]Outcome{o}, st, time.Now())
	if checks["A11.thinking"].Status != "inconclusive" {
		t.Fatal("基线也为零时无可靠功能证据")
	}
}

func TestInspectionAvailabilityRequiresRegisteredSamples(t *testing.T) {
	st := calibratedInspectionSettings()
	o := pairedInspectionOutcome("D07", false, true)
	o.Target.HTTPStatus, o.Target.ErrorClass = 503, "http_5xx"
	checks, gates := Inspect([]Outcome{o, o}, st, time.Now())
	if checks["D13"].Status != "inconclusive" || len(gates) != 0 {
		t.Fatal("少于登记样本数时不能确认可用率门槛")
	}
}

func TestInspectionThirdPartyCalibrationAndFreshRecovery(t *testing.T) {
	st := calibratedInspectionSettings()
	a := Assessment{InspectionMode: "formal", InspectionSettings: &st, Checks: map[string]inspection.Check{}}
	addThirdPartyInspection(&a, []tpResult{{Verdict: "degraded"}}, time.Now())
	if a.Checks["third_party"].Status != "fail" || len(a.Gates) != 1 || !a.Gates[0].Confirmed || a.QualityFactor != nil {
		t.Fatal("校准后的第三方明确失败必须写入可复测门槛")
	}
	a.Gates = nil
	addThirdPartyInspection(&a, []tpResult{{Verdict: "normal"}}, time.Now())
	if a.Checks["third_party"].Status != "pass" || a.Checks["third_party"].Evidence.Source != "third_party" {
		t.Fatal("第三方有效通过必须可用于恢复覆盖")
	}
	addThirdPartyInspection(&a, []tpResult{{Verdict: "normal"}, {Verdict: "inconclusive"}}, time.Now())
	if a.Checks["third_party"].Status != "inconclusive" {
		t.Fatal("不完整第三方核验不能当恢复通过")
	}
	st.CheckOverrides = nil
	a.Gates = nil
	addThirdPartyInspection(&a, []tpResult{{Verdict: "degraded"}}, time.Now())
	if !a.Gates[0].Confirmed || a.Checks["third_party"].Applied {
		t.Fatal("第三方正式门槛不要求校准；扣价仍必须显式开启")
	}
}
