package quality

import (
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestFlowItemModelsHonorPerItemScope(t *testing.T) {
	item := scopedIntelligenceItem()
	if got := flowItemModels(item, "gpt-6-astra", []string{"gpt-6-astra", "gpt-6-sol"}); !reflect.DeepEqual(got, []string{"gpt-6-astra"}) {
		t.Fatalf("型号专用题选错模型：%v", got)
	}
	item.Models = nil
	if got := flowItemModels(item, "gpt-6-astra", []string{"gpt-6-astra", "gpt-6-sol"}); len(got) != 2 {
		t.Fatal("未限定型号的测智题须测所有选中模型")
	}
}

func scopedIntelligenceItem() Item {
	return Item{Suite: "inspection_capability", CheckKey: "D07", Prompt: "计算 5 + 7，只输出整数。", Grader: Grader{Type: "exact", Answer: "12"}, Models: []string{"gpt-6-astra"}, MaxTokens: 128, Status: "active", Modes: []string{"full", "quick"}, Generator: []byte(`{}`)}
}

func TestInspectionItemKeyDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, suite, key, want string
		models                 []string
	}{
		{"限定型号智力题", "iq", "", "", []string{"gpt-6-astra"}},
		{"限定型号推理题", "reasoning", "", "", []string{"gpt-6-astra"}},
		{"显式功能归类", "reasoning", "D08", "D08", []string{"gpt-6-astra"}},
		{"显式诚实归类", "iq", "D07.honesty", "D07.honesty", []string{"gpt-6-astra"}},
		{"不限型号", "reasoning", "", "", nil},
		{"旧长上下文不缺省", "long_context", "", "", []string{"gpt-6-astra"}},
		{"新套件不缺省", "inspection_capability", "", "", []string{"gpt-6-astra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := Item{Suite: tc.suite, CheckKey: tc.key, Models: tc.models, Status: "active"}
			before := it
			if got := inspectionItemKey(it); got != tc.want {
				t.Fatalf("运行时归类=%q，期望 %q", got, tc.want)
			}
			if !reflect.DeepEqual(it, before) {
				t.Fatal("派生归类不应修改题库字段")
			}
		})
	}
}

func scopedStaticOutcome() Outcome {
	it := scopedIntelligenceItem()
	o := pairedInspectionOutcome(inspectionItemKey(it), false, true)
	o.Suite, o.ItemID, o.GeneratorVersion = it.Suite, 22, "static-1"
	return o
}

func modelScopedD07Rows() []Outcome {
	rows := make([]Outcome, 19)
	for i := range rows {
		rows[i] = pairedInspectionOutcome("D07", i >= 10, true)
		rows[i].ItemID, rows[i].InstanceIdx, rows[i].GeneratorVersion = 23, i, "aime_math/1"
	}
	return rows
}

func TestModelScopedIntelligenceUsesD07Pairs(t *testing.T) {
	st := calibratedInspectionSettings()
	rows := modelScopedD07Rows()
	checks, _ := Inspect(rows, st, time.Now())
	if checks["D07"].Status != "inconclusive" || checks["D07"].Samples.Paired != 19 {
		t.Fatal("十九个参数题配对不足，不能提前扣价")
	}
	static := scopedStaticOutcome()
	static.RefHistRate = numberPointer(.01) // 正式模式只用本批基线，忽略旧历史。
	rows = append(rows, static)
	checks, gates := Inspect(rows, st, time.Now())
	c := checks["D07"]
	if c.Samples.Observed != 20 || c.Samples.Paired != 20 || c.Samples.Excluded != 0 || c.Status != "fail" || c.Factor == nil || *c.Factor != .7 || *c.Metrics["relative_score"].Value != .5 {
		t.Fatalf("静态题未与参数题一起计智力扣价：%+v", c)
	}
	aggregated := inspection.Aggregate(checks, gates, st.Mode, st)
	if aggregated.QualityFactor == nil || *aggregated.QualityFactor != .7 || !aggregated.Checks["D07"].Applied {
		t.Fatalf("静态题扣价未进入Q：%+v", aggregated)
	}
	if got := assessFormal(rows, th, st); got.Samples != 0 || got.Verdict != "normal" || got.AvailableTotal != 20 {
		t.Fatalf("测智结论不应再触发旧序贯门槛：%+v", got)
	}

}

func TestModelScopedIntelligenceExcludesBothSides(t *testing.T) {
	st := calibratedInspectionSettings()
	for _, side := range []string{"被测", "基线"} {
		for _, exclusion := range []string{"refusal", "truncated", "rate_limited", "platform", "no_reference"} {
			t.Run(side+"/"+exclusion, func(t *testing.T) {
				o := scopedStaticOutcome()
				r := &o.Target
				if side == "基线" {
					r = o.Ref
				}
				switch exclusion {
				case "refusal", "truncated":
					r.Flags = []string{exclusion}
				case "rate_limited", "platform":
					r.HTTPStatus, r.ErrorClass = 429, exclusion
				case "no_reference":
					o.Ref = nil
				}
				rows := append(modelScopedD07Rows(), o)
				checks, _ := Inspect(rows, st, time.Now())
				c := checks["D07"]
				if c.Samples.Paired != 19 || c.Samples.Excluded != 1 || c.Factor != nil || c.Status != "inconclusive" {
					t.Fatalf("无效静态配对未整体剔除：%+v", c)
				}
				if checks, _ := Inspect([]Outcome{o}, st, time.Now()); checks["D07"].Status != "inconclusive" || checks["D07"].Factor != nil {
					t.Fatal("单独无效或无基线样本不能扣价")
				}
			})
		}
	}
}

func TestModelScopedStaticDoesNotEnterD06(t *testing.T) {
	st := calibratedInspectionSettings()
	static := make([]Outcome, 12)
	generated := make([]Outcome, 12)
	low := make([]Outcome, 12)
	for i := range static {
		static[i] = scopedStaticOutcome()
		static[i].ItemID = int64(i + 1)
		static[i].Target.ReasoningTokens = 1
		generated[i] = pairedInspectionOutcome("D07", true, true)
		generated[i].ItemID, generated[i].InstanceIdx, generated[i].GeneratorVersion = 23, i, "aime_math/1"
		low[i] = generated[i]
		low[i].CheckKey, low[i].InstanceIdx = "D06", 1000+i
	}
	checks, _ := Inspect(static, st, time.Now())
	if c := checks["D06"]; c.Samples.Observed != 0 || c.Samples.Paired != 0 || c.Factor != nil {
		t.Fatalf("静态题不能用于推理档位比较：%+v", c)
	}
	for _, anchors := range [][]Outcome{nil, low} {
		rows := append(append(append([]Outcome(nil), static...), generated...), anchors...)
		checks, _ := Inspect(rows, st, time.Now())
		c := checks["D06"]
		if c.Status != "pass" || c.Samples.Observed != 12 || c.Samples.Paired != 12 || c.Factor == nil || *c.Factor != 1 || *c.Metrics["reasoning_ratio"].Value != 1 {
			t.Fatalf("有/无低档锚点时静态题污染 D06：%+v", c)
		}
	}
	// 迁移后的糖果题和其他静态 D07 题在有/无 low 锚点时都不能混入 D06。
	for i := range static {
		static[i].Suite = astraCandyItem().Suite
	}
	for _, anchors := range [][]Outcome{nil, low} {
		checks, _ = Inspect(append(append([]Outcome(nil), static...), anchors...), st, time.Now())
		if c := checks["D06"]; c.Samples.Observed != 0 || c.Samples.Paired != 0 || c.Factor != nil {
			t.Fatalf("新套件静态 D07 不应进入推理量观测：%+v", c)
		}
		checks, _ = Inspect(append(append(append([]Outcome(nil), static...), generated...), anchors...), st, time.Now())
		if c := checks["D06"]; c.Samples.Paired != 12 || c.Status != "pass" || *c.Metrics["reasoning_ratio"].Value != 1 {
			t.Fatalf("糖果静态题污染参数化推理量观测：%+v", c)
		}
	}
}

func TestInspectionItemMetadataDoesNotChangeAvailability(t *testing.T) {
	rows := modelScopedD07Rows()
	before := assessFormal(rows, th)
	rows[0].CheckKey = "D08"
	if after := assessFormal(rows, th); !reflect.DeepEqual(before, after) {
		t.Fatalf("检测项归类改变了可用统计：%+v → %+v", before, after)
	}
}
