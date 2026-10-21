package quality

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/token-market/market/internal/inspection"
)

// degradationItem 是一道限定型号的降智题（原 inspection_capability / D07 迁入后的形状）。
func degradationItem() Item {
	return Item{Suite: "inspection_degradation", CheckKey: "D14", Prompt: "计算 5 + 7，只输出整数。",
		Grader: Grader{Type: "exact", Answer: "12"}, Models: []string{"gpt-6-astra"}, MaxTokens: 128,
		Status: "active", Modes: []string{"full", "quick"}, Generator: []byte(`{}`)}
}

func TestExecutionItemFlowSeparatesDegradationSuite(t *testing.T) {
	for _, tc := range []struct {
		suite, want string
	}{
		{"inspection_capability", inspection.FlowIntelligence},
		{"inspection_context", inspection.FlowIntelligence},
		{"inspection_degradation", inspection.FlowDegradation},
		{"inspection_authenticity", inspection.FlowIdentity},
		{"inspection_performance", inspection.FlowCommon},
		{"iq", ""},
	} {
		t.Run(tc.suite, func(t *testing.T) {
			if got := executionItemFlow(Item{Suite: tc.suite}); got != tc.want {
				t.Fatalf("套件归属=%q，期望=%q", got, tc.want)
			}
		})
	}
	if family := probeFlowFamily(inspection.FlowDegradation); family != inspection.FlowIntelligence {
		t.Fatalf("降智检测应按测智类流程展开，得到 %q", family)
	}
}

// 降智题只按降智流程冻结的模型展开，测智流程的模型列表不影响选择题。
func TestDegradationItemModelsUseOwnFrozenSet(t *testing.T) {
	item := degradationItem()
	run := runRow{models: []string{"gpt-6-astra", "gpt-6-sol"}, degradationModels: []string{"gpt-6-astra", "gpt-6-sol"}}
	if got := flowItemModels(item, "gpt-6-astra", flowModelsOf(run, inspection.FlowDegradation)); !reflect.DeepEqual(got, []string{"gpt-6-astra"}) {
		t.Fatalf("限定型号的降智题选错模型：%v", got)
	}
	if got := flowModelsOf(run, inspection.FlowIntelligence); !reflect.DeepEqual(got, []string{"gpt-6-astra", "gpt-6-sol"}) {
		t.Fatalf("测智流程模型被降智集合改写：%v", got)
	}
	item.Models = nil
	if got := flowItemModels(item, "gpt-6-astra", flowModelsOf(run, inspection.FlowDegradation)); !reflect.DeepEqual(got, []string{"gpt-6-astra", "gpt-6-sol"}) {
		t.Fatalf("未限定型号的降智题须测全部冻结模型：%v", got)
	}
}

// 降智按模型分别判分再取最差；降智流程的模型集合为空时不判定。
func TestDegradationModelsWorstCaseAndEmptySet(t *testing.T) {
	st := calibratedInspectionSettings()
	rows := []Outcome{}
	for i := 0; i < 20; i++ {
		good := pairedInspectionOutcome("D14", true, true)
		good.ItemID, good.InstanceIdx, good.GeneratorVersion = 22, i, "static-1"
		good.RequestedModel = "gpt-6-astra"
		rows = append(rows, good)
		worse := pairedInspectionOutcome("D14", i >= 10, true)
		worse.ItemID, worse.InstanceIdx, worse.GeneratorVersion = 22, 10000+i, "static-1"
		worse.RequestedModel = "gpt-6-sol"
		rows = append(rows, worse)
	}
	run := runRow{flows: []string{inspection.FlowDegradation}, models: []string{"gpt-6-astra"}, degradationModels: []string{"gpt-6-astra", "gpt-6-sol"}}
	opts := InspectOptions{Flows: run.flows, Models: run.models, FrozenDegradationModels: run.degradationModels}
	checks, _ := Inspect(rows, st, time.Now(), opts)
	c := checks["D14"]
	if c.Status != "fail" || len(c.Metrics["models"].Models) != 2 {
		t.Fatalf("降智取最差模型结论错误：%+v", c)
	}
	if c.Metrics["models"].Models[0].Model != "gpt-6-astra" || c.Metrics["models"].Models[0].Status != "pass" {
		t.Fatalf("降智模型结果顺序或状态错误：%+v", c.Metrics["models"].Models)
	}
	if summary := summarizeFlow(inspection.FlowDegradation, run, Assessment{InspectionMode: "formal", Checks: checks}); summary.Status != "fail" || len(summary.Models) != 2 {
		t.Fatalf("降智流程摘要错误：%+v", summary)
	}

	empty := run
	empty.degradationModels = []string{}
	emptyChecks, _ := Inspect(rows, st, time.Now(), InspectOptions{Flows: empty.flows, Models: empty.models, FrozenDegradationModels: empty.degradationModels})
	if c := emptyChecks["D14"]; c.Status != "not_applicable" || c.Evidence.Code != "degradation_models_unconfigured" {
		t.Fatalf("降智模型交集为空应记不适用：%+v", c)
	}
	needlesOnly, _ := Inspect(rows, st, time.Now(), InspectOptions{Flows: []string{"intelligence"}, FrozenDegradationModels: nil})
	if c := needlesOnly["D14"]; c.Status != "not_run" {
		t.Fatalf("未跑降智流程时 D14 不应判定：%+v", c)
	}
}

// 降智流程的失败项只进降智疑似，并按 D14 用完整检测复测。
func TestDecideVerdictDegradationOnlySuspected(t *testing.T) {
	for _, status := range []string{"warning", "fail"} {
		t.Run(status, func(t *testing.T) {
			run := runRow{flows: []string{inspection.FlowDegradation}, degradationModels: []string{"模型甲"}}
			a := Assessment{InspectionMode: inspection.ModeFormal, Verdict: "degraded", LLR: 100,
				Checks: map[string]inspection.Check{"D14": flowStateCheck("D14", status)}}
			if got := decideVerdict(run, a, nil); got != "suspected" {
				t.Fatalf("降智异常只能标疑似，实际=%s", got)
			}
			if summary := summarizeFlow(inspection.FlowDegradation, run, a); summary.Status != status {
				t.Fatalf("降智流程摘要=%s，期望=%s", summary.Status, status)
			}
		})
	}
	p := Policy{Flows: policyFlowsFixture()}
	_, modes := modesForFlows(&p, []string{inspection.FlowDegradation}, "adaptive", []string{"D14"})
	if modes[inspection.FlowDegradation] != "full" {
		t.Fatalf("D14 失败项复测须完整检测：%v", modes)
	}
	_, modes = modesForFlows(&p, []string{inspection.FlowDegradation}, "adaptive", []string{"D07"})
	if modes[inspection.FlowDegradation] != "quick" {
		t.Fatalf("测智失败项不应把降智扩成完整检测：%v", modes)
	}
}

// 卖家摘要下发降智的按模型结论，并做与测智相同的字段裁剪。
func TestPublicReportSummaryDegradationModels(t *testing.T) {
	run := runRow{trigger: "admin", model: "模型甲", models: []string{"模型甲"}, degradationModels: []string{"模型甲", "模型乙"}, flows: []string{"intelligence", inspection.FlowDegradation}}
	c := flowStateCheck("D14", "warning")
	c.Metrics = map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{
		{Model: "模型甲", Status: "pass", TargetPassRate: numberPointer(.987654), Samples: 47, Metrics: map[string]inspection.Metric{"内部指标": {Value: numberPointer(.923456)}}},
		{Model: "模型乙", Status: "warning", Samples: 53},
	}}}
	a := Assessment{InspectionMode: "trial", Verdict: "suspected", Checks: map[string]inspection.Check{"D14": c}}
	e := &Engine{Now: func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }}
	raw := mustJSON(e.buildSummary(run, &upstreamRow{}, a, "account"))
	safe := string(publicReportSummary(raw))
	for _, secret := range []string{"target_pass_rate", ".987654", ".923456", "内部指标"} {
		if strings.Contains(safe, secret) {
			t.Fatalf("卖家摘要泄露降智内部指标 %s：%s", secret, safe)
		}
	}
	var public struct {
		Degradation struct {
			Status string                       `json:"status"`
			Models []map[string]json.RawMessage `json:"models"`
		} `json:"degradation"`
	}
	if err := json.Unmarshal([]byte(safe), &public); err != nil {
		t.Fatal(err)
	}
	if public.Degradation.Status != "warning" || len(public.Degradation.Models) != 2 {
		t.Fatalf("卖家摘要缺少降智按模型结论：%s", safe)
	}
	for i, model := range public.Degradation.Models {
		if len(model) != 2 || model["model"] == nil || model["status"] == nil {
			t.Fatalf("卖家降智模型项只能含型号与状态，第%d项=%v", i, model)
		}
	}
}

// 卖家只看结果：身份流程不带来源（第三方 / 自有题库），不适用的测智类流程整块不下发。
func TestPublicReportSummaryHidesIdentitySourceAndNotApplicableFlows(t *testing.T) {
	raw := mustJSON(map[string]any{
		"verdict":      "normal",
		"identity":     map[string]any{"source": "third_party", "status": "pass", "inherited_at": nil},
		"degradation":  map[string]any{"status": "not_applicable", "models": []map[string]any{{"model": "模型甲", "status": "not_applicable"}}},
		"intelligence": map[string]any{"status": "pass", "models": []map[string]any{{"model": "模型甲", "status": "pass"}}},
	})
	safe := string(publicReportSummary(raw))
	if strings.Contains(safe, "third_party") || strings.Contains(safe, `"source"`) {
		t.Fatalf("卖家摘要不应透露身份核验来源：%s", safe)
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal([]byte(safe), &public); err != nil {
		t.Fatal(err)
	}
	if _, ok := public["degradation"]; ok {
		t.Fatalf("不适用的降智流程不应下发：%s", safe)
	}
	if _, ok := public["intelligence"]; !ok {
		t.Fatalf("有结论的测智流程应保留：%s", safe)
	}
	if !strings.Contains(string(public["identity"]), `"status":"pass"`) {
		t.Fatalf("身份流程应保留结论：%s", public["identity"])
	}
}
