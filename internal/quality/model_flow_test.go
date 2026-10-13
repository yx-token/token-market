package quality

import (
	"reflect"
	"testing"
	"time"
)

func TestFlowModesKeepIntelligenceQuickWhenIdentityFull(t *testing.T) {
	st := defaultInspectionSettings()
	for _, tc := range []struct {
		name  string
		flows []string
		mode  string
		low   int
	}{
		{"只有身份", []string{"identity"}, "full", 0},
		{"测智快检身份完整", []string{"intelligence", "identity"}, "quick", 0},
		{"测智完整", []string{"intelligence"}, "full", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := executionSnapshot{Mode: "full", Flows: tc.flows, FlowModes: map[string]string{"intelligence": tc.mode, "identity": "full"}, Models: []string{"模型甲", "模型乙"}, Instances: []plannedInstance{
				{ItemID: 1, CheckKey: "D07", RequestedModel: "模型甲", GeneratorVersion: "aime_math/1", Steps: []plannedStep{{Request: ProbeRequest{Model: "模型甲", Effort: "high"}}}},
				{ItemID: 1, InstanceIdx: 10000, CheckKey: "D07", RequestedModel: "模型乙", GeneratorVersion: "aime_math/1", Steps: []plannedStep{{Request: ProbeRequest{Model: "模型乙", Effort: "high"}}}},
			}}
			addInspectionProbes(&plan, &Policy{}, st)
			low := 0
			for _, instance := range plan.Instances {
				if instance.CheckKey == "D06" {
					low++
					if instance.Steps[0].Request.effectiveEffort() != "low" || instance.RequestedModel != instance.Steps[0].Request.Model {
						t.Fatalf("低档副本未保留请求模型：%+v", instance)
					}
				}
			}
			if low != tc.low {
				t.Fatalf("生成低档副本=%d，期望=%d", low, tc.low)
			}
		})
	}
}

func TestPermissionProbeClonesOnlyWhitelistedModels(t *testing.T) {
	plan := executionSnapshot{Mode: "full", Models: []string{"模型甲", "模型丙"}, ModelWhitelist: []string{"模型甲", "模型乙"}, Instances: []plannedInstance{{ItemID: 1, CheckKey: "A04", RequestedModel: "模型甲", Steps: []plannedStep{{Request: ProbeRequest{Model: "模型甲"}}}}}}
	addInspectionProbes(&plan, &Policy{Models: []string{"不挂售的模型"}}, defaultInspectionSettings())
	models := []string{}
	for _, instance := range plan.Instances {
		models = append(models, instance.Steps[0].Request.Model)
	}
	if !reflect.DeepEqual(models, []string{"模型甲", "模型乙"}) {
		t.Fatalf("型号权限检测越过挂售白名单：%v", models)
	}
}

func modelOutcomes(key, model string, n, passed int) []Outcome {
	rows := itemRows(key, n, passed)
	for i := range rows {
		rows[i].RequestedModel = model
	}
	return rows
}

func TestIntelligenceModelsUseIndividualExpectationsAndWorstResult(t *testing.T) {
	rows := append(modelOutcomes("D07", "模型甲", 100, 60), modelOutcomes("D07", "模型乙", 100, 40)...)
	opts := InspectOptions{Flows: []string{"intelligence"}, Models: []string{"模型甲", "模型乙"}, IntelligenceModels: []IntelligenceModel{{Model: "模型甲", ExpectedPassRate: numberPointer(.5)}, {Model: "模型乙"}}}
	checks, _ := Inspect(rows, defaultInspectionSettings(), time.Time{}, opts)
	c := checks["D07"]
	models := c.Metrics["models"].Models
	if c.Status != "fail" || len(models) != 2 || models[0].Status != "pass" || models[1].Status != "fail" || *models[0].ExpectedPassRate != .5 || *models[1].ExpectedPassRate != .9 || c.Samples.Observed != 200 {
		t.Fatalf("各模型期望或最差结论错误：%+v；模型=%+v", c, models)
	}
	if checks["A02"].Status != "not_run" || checks["A12"].Status != "not_run" {
		t.Fatal("测智题附带证据不能越过身份流程")
	}
}

func TestIntelligenceSamplesCannotPoolAcrossModels(t *testing.T) {
	rows := append(modelOutcomes("D07", "模型甲", 10, 0), modelOutcomes("D07", "模型乙", 10, 10)...)
	checks, _ := Inspect(rows, defaultInspectionSettings(), time.Time{}, InspectOptions{Models: []string{"模型甲", "模型乙"}})
	c := checks["D07"]
	if c.Status != "inconclusive" || c.Factor != nil || len(c.Metrics["models"].Models) != 2 {
		t.Fatalf("两模型不足样本被错误合并计分：%+v", c)
	}
}

func TestEmptyModelIntersectionIsNotApplicable(t *testing.T) {
	checks, _ := Inspect(nil, defaultInspectionSettings(), time.Time{}, InspectOptions{Flows: []string{"intelligence", "identity"}, Models: []string{}, IdentitySource: "third_party"})
	for _, key := range []string{"D07", "D07.honesty", "D09", "D06"} {
		if c := checks[key]; c.Status != "not_applicable" || c.Evidence.Code != "intelligence_models_unconfigured" {
			t.Fatalf("%s 空交集未记不适用：%+v", key, c)
		}
	}
	for _, key := range []string{"A02", "A12", "A04", "A11.tools"} {
		if checks[key].Status != "not_run" {
			t.Fatalf("第三方身份模式仍执行自有项 %s", key)
		}
	}
}

func TestRecoveryProbeWorksWithoutSelectedPermissionItem(t *testing.T) {
	e := Engine{Keyring: testInspectionKeyring(t)}
	plan := executionSnapshot{Mode: "full", Flows: []string{"intelligence"}}
	carrier := plannedInstance{ItemID: 1, CheckKey: "A04", Steps: []plannedStep{{Request: ProbeRequest{Model: "模型甲", MaxTokens: 128}}}}
	if err := e.addAvailabilityRecoveryProbes(&plan, "独立恢复载体", 20, carrier); err != nil {
		t.Fatal(err)
	}
	if len(plan.Instances) != 20 {
		t.Fatal("未载入独立恢复样本")
	}
	for _, instance := range plan.Instances {
		if instance.CheckKey != "D13" || !instance.TargetOnly || instance.Steps[0].Request.Model != "模型甲" {
			t.Fatalf("恢复采样不应产生 A04 结论：%+v", instance)
		}
	}
}

func TestPerModelReasoningKeepsHighLowAnchorsSeparate(t *testing.T) {
	rows := []Outcome{}
	for n, model := range []string{"模型甲", "模型乙"} {
		for idx := range 10 {
			high := itemOutcome("D07", true)
			high.RequestedModel, high.ItemID, high.InstanceIdx, high.GeneratorVersion = model, 1, n*10000+idx, "aime_math/1"
			high.Target.ReasoningTokens = 200
			low := high
			low.CheckKey, low.InstanceIdx, low.Target.ReasoningTokens = "D06", n*10000+1000+idx, 100
			rows = append(rows, high, low)
		}
	}
	checks, _ := Inspect(rows, defaultInspectionSettings(), time.Time{}, InspectOptions{Models: []string{"模型甲", "模型乙"}})
	c := checks["D06"]
	if c.Status != "pass" || len(c.Metrics["models"].Models) != 2 || c.Samples.Observed != 20 {
		t.Fatalf("按模型的推理锚点丢失：%+v", c)
	}
	for _, model := range c.Metrics["models"].Models {
		if model.Samples != 10 || *model.Metrics["high_low_ratio"].Value != 2 {
			t.Fatalf("推理模型指标错误：%+v", model)
		}
	}
}

func TestModelSpecificPairedChecksRetainReferenceEvidence(t *testing.T) {
	rows := make([]Outcome, 100)
	for i := range rows {
		rows[i] = pairedInspectionOutcome("D07", i < 60, true)
		rows[i].RequestedModel = "模型甲"
	}
	checks, _ := Inspect(rows, calibratedInspectionSettings(), time.Time{}, InspectOptions{Models: []string{"模型甲"}, IntelligenceModels: []IntelligenceModel{{Model: "模型甲", ExpectedPassRate: numberPointer(.5)}}})
	c := checks["D07"]
	if c.Status != "pass" || c.Samples.Paired != 100 || c.Evidence.Source != "paired_probe" || *c.Metrics["expected_pass_rate"].Value != .5 {
		t.Fatalf("模型期望覆盖或配对证据丢失：%+v", c)
	}
}
