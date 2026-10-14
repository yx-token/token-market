package quality

import (
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func modelHistoryCheck() inspection.Check {
	return inspection.Check{Key: "D07", Status: "fail", Factor: numberPointer(.7), Applied: true, CheckedAt: time.Unix(100, 0), Evidence: inspection.Evidence{Source: "paired_probe"}, Metrics: map[string]inspection.Metric{
		"target_pass_rate": metric(.3, "fraction"),
		"models": {Unit: "models", Models: []inspection.ModelCheckResult{
			{Model: "已撤下", Status: "fail", TargetPassRate: numberPointer(.3), ExpectedPassRate: numberPointer(.9), Samples: 100, Metrics: map[string]inspection.Metric{"relative_score": metric(1.0/3, "ratio"), "mcnemar_p": metric(.001, "fraction")}},
			{Model: "继续挂售", Status: "pass", TargetPassRate: numberPointer(.9), ExpectedPassRate: numberPointer(.9), Samples: 80, Metrics: map[string]inspection.Metric{"relative_score": metric(1, "ratio"), "mcnemar_p": metric(1, "fraction")}},
		}},
	}}
}

func TestInheritedModelsDropRemovedFailureAndRecomputeFactor(t *testing.T) {
	previous := modelHistoryCheck()
	before := string(mustJSON(previous))
	filtered, ok := inheritedModelsCheck(previous, []string{"继续挂售"})
	if !ok || filtered.Status != "pass" || filtered.Factor != nil || filtered.Applied || len(filtered.Metrics["models"].Models) != 1 || *filtered.Metrics["target_pass_rate"].Value != .9 || *filtered.Metrics["relative_score"].Value != 1 || filtered.Samples.Observed != 80 || filtered.Samples.Paired != 80 || !filtered.CheckedAt.Equal(previous.CheckedAt) {
		t.Fatalf("撤下模型的失败仍污染当前结果：%+v", filtered)
	}
	if string(mustJSON(previous)) != before {
		t.Fatal("继承过滤修改了历史记录")
	}
	st := calibratedInspectionSettings()
	aggregated := inspection.Aggregate(map[string]inspection.Check{"D07": filtered}, nil, st.Mode, st)
	if aggregated.QualityFactor == nil || *aggregated.QualityFactor != 1 || !aggregated.Checks["D07"].Applied {
		t.Fatalf("过滤后沿用了原失败系数：%+v", aggregated)
	}
}

func TestInheritedModelsRejectMissingOrUnfinishedResults(t *testing.T) {
	for _, status := range []string{"缺失", "not_run", "inconclusive", "未知"} {
		t.Run(status, func(t *testing.T) {
			previous := modelHistoryCheck()
			if status != "缺失" {
				previous.Metrics["models"].Models[1].Status = status
			}
			models := []string{"继续挂售"}
			if status == "缺失" {
				models = append(models, "新挂售")
			}
			if _, ok := inheritedModelsCheck(previous, models); ok {
				t.Fatal("缺失或未完成模型不能沿用旧扣价结果")
			}
		})
	}
	if _, ok := inheritedModelsCheck(modelHistoryCheck(), nil); ok {
		t.Fatal("空交集不能沿用旧测智扣价")
	}
}

func TestInheritedModelsChooseWorstAndCurrentOrder(t *testing.T) {
	previous := modelHistoryCheck()
	filtered, ok := inheritedModelsCheck(previous, []string{"继续挂售", "已撤下"})
	if !ok || filtered.Status != "fail" || filtered.Factor != nil || *filtered.Metrics["target_pass_rate"].Value != .3 {
		t.Fatalf("仍挂售的失败模型未进入结论：%+v", filtered)
	}
	got := []string{}
	for _, model := range filtered.Metrics["models"].Models {
		got = append(got, model.Model)
	}
	if !reflect.DeepEqual(got, []string{"继续挂售", "已撤下"}) {
		t.Fatalf("模型结果未按当前集合排列：%v", got)
	}
	st := calibratedInspectionSettings()
	aggregated := inspection.Aggregate(map[string]inspection.Check{"D07": filtered}, nil, st.Mode, st)
	if aggregated.QualityFactor == nil || *aggregated.QualityFactor != .7 {
		t.Fatalf("有效失败应按当前项系数重新扣价：%+v", aggregated)
	}
}

func TestInheritedModelsNotApplicableDoesNotOverridePassingModel(t *testing.T) {
	previous := modelHistoryCheck()
	previous.Metrics["models"].Models[0].Status = "not_applicable"
	filtered, ok := inheritedModelsCheck(previous, []string{"已撤下", "继续挂售"})
	if !ok || filtered.Status != "pass" {
		t.Fatalf("不适用模型覆盖了有效通过：%+v", filtered)
	}
	filtered, ok = inheritedModelsCheck(previous, []string{"已撤下"})
	if !ok || filtered.Status != "not_applicable" || filtered.Factor != nil {
		t.Fatalf("独立不适用模型错误继承扣价：%+v", filtered)
	}
}

func TestInheritedModelsCanResolveRemovedUnfinishedModel(t *testing.T) {
	previous := modelHistoryCheck()
	previous.Status = "inconclusive"
	previous.Metrics["models"].Models[0].Status = "inconclusive"
	filtered, ok := inheritedModelsCheck(previous, []string{"继续挂售"})
	if !ok || filtered.Status != "pass" {
		t.Fatalf("撤下模型的未完成状态仍阻止有效继承：%+v", filtered)
	}
}

func TestSummaryModelsOnlyUseInheritedEvidenceForUnselectedFlow(t *testing.T) {
	previous := modelHistoryCheck()
	filtered, ok := inheritedModelsCheck(previous, []string{"继续挂售"})
	if !ok {
		t.Fatal("历史夹具缺少模型结果")
	}
	filtered.Evidence.Source = "history"
	unselected := runRow{flows: []string{"identity"}, models: []string{}}
	checks := map[string]inspection.Check{"D07": filtered, "D09": {Key: "D09", Status: "fail", Evidence: inspection.Evidence{Source: "paired_probe"}, Metrics: map[string]inspection.Metric{"models": {Models: []inspection.ModelCheckResult{{Model: "继续挂售", Status: "fail"}, {Model: "其他模型", Status: "fail"}}}}}}
	summary := summarizeFlow("intelligence", unselected, Assessment{Checks: checks})
	if summary.Status != "pass" || summary.InheritedAt == nil || !summary.InheritedAt.Equal(filtered.CheckedAt) || len(summary.Models) != 1 || summary.Models[0].Model != "继续挂售" || summary.Models[0].Status != "pass" || summary.Models[0].Samples != 80 {
		t.Fatalf("未选流程混入本次探测结果或丢失模型：%+v", summary)
	}
	selectedEmpty := runRow{flows: []string{"intelligence"}, models: []string{}}
	summary = summarizeFlow("intelligence", selectedEmpty, Assessment{Checks: checks})
	if summary.Status != "not_applicable" || len(summary.Models) != 0 {
		t.Fatalf("当前空交集错误展示了旧模型：%+v", summary)
	}
}

func TestRatesModelsMatchOnlyCurrentModels(t *testing.T) {
	policy := func(entries ...IntelligenceModel) *Policy {
		return &Policy{Flows: PolicyFlows{Intelligence: IntelligenceFlowPolicy{Models: entries}}}
	}
	for _, tc := range []struct {
		name      string
		old, next *Policy
		models    []string
		want      bool
	}{
		{"空集合", nil, nil, nil, true},
		{"无历史策略", nil, policy(IntelligenceModel{Model: "甲"}), []string{"甲"}, false},
		{"留空一致", policy(IntelligenceModel{Model: "甲"}), policy(IntelligenceModel{Model: "甲"}), []string{"甲"}, true},
		{"数值一致", policy(IntelligenceModel{Model: "甲", ExpectedPassRate: numberPointer(.8)}), policy(IntelligenceModel{Model: "甲", ExpectedPassRate: numberPointer(.8)}), []string{"甲"}, true},
		{"期望变化", policy(IntelligenceModel{Model: "甲", ExpectedPassRate: numberPointer(.8)}), policy(IntelligenceModel{Model: "甲", ExpectedPassRate: numberPointer(.9)}), []string{"甲"}, false},
		{"默认改为显式", policy(IntelligenceModel{Model: "甲"}), policy(IntelligenceModel{Model: "甲", ExpectedPassRate: numberPointer(.9)}), []string{"甲"}, false},
		{"新增模型", policy(IntelligenceModel{Model: "甲"}), policy(IntelligenceModel{Model: "甲"}, IntelligenceModel{Model: "乙"}), []string{"甲", "乙"}, false},
		{"撤下模型变化不影响", policy(IntelligenceModel{Model: "甲"}, IntelligenceModel{Model: "乙", ExpectedPassRate: numberPointer(.5)}), policy(IntelligenceModel{Model: "甲"}, IntelligenceModel{Model: "乙", ExpectedPassRate: numberPointer(.9)}), []string{"甲"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ratesModelsMatch(tc.old, tc.next, inspection.FlowIntelligence, tc.models); got != tc.want {
				t.Fatalf("模型期望匹配=%v，期望=%v", got, tc.want)
			}
		})
	}
}
