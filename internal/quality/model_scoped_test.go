package quality

import (
	"reflect"
	"testing"
	"time"

	"github.com/DueGin/token-market/internal/inspection"
)

func TestFormalItemsRetainModelScopedLegacy(t *testing.T) {
	items := defaultItems()
	got := formalItems(items)
	if len(got) != 1 || !reflect.DeepEqual(got[0], modelScopedCandyItem()) {
		t.Fatalf("正式模式应仅保留型号专用糖果题：%+v", got)
	}
	unscoped := modelScopedCandyItem()
	unscoped.Models = nil
	if len(formalItems([]Item{unscoped})) != 0 {
		t.Fatal("未限定型号的旧内置题仍须跳过")
	}
	scoped := items[0]
	scoped.Models = []string{"gpt-6-astra"}
	scoped.CheckKey = "D08"
	rewritten := unscoped
	rewritten.Prompt += " 管理员改写"
	custom := Item{Suite: "iq", Prompt: "管理员新题"}
	want := []Item{scoped, rewritten, custom}
	if !reflect.DeepEqual(formalItems(want), want) {
		t.Fatal("型号专用题、改写题或管理员新题被跳过")
	}
}

func modelScopedCandyItem() Item {
	for _, it := range defaultItems() {
		if it.Category == "pigeonhole" {
			return it
		}
	}
	panic("内置糖果题缺失")
}

func TestInspectionItemKeyDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, suite, key, want string
		models                 []string
	}{
		{"限定型号智力题", "iq", "", "D07", []string{"gpt-6-astra"}},
		{"限定型号推理题", "reasoning", "", "D07", []string{"gpt-6-astra"}},
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
	it := modelScopedCandyItem()
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
	if c.Samples.Observed != 20 || c.Samples.Paired != 20 || c.Samples.Excluded != 0 || c.Status != "fail" || c.Factor == nil || *c.Factor != .7 || *c.Metrics["relative_score"].Value != .45 {
		t.Fatalf("静态题未与参数题一起计智力扣价：%+v", c)
	}
	aggregated := inspection.Aggregate(checks, gates, st.Mode, st)
	if aggregated.QualityFactor == nil || *aggregated.QualityFactor != .7 || !aggregated.Checks["D07"].Applied {
		t.Fatalf("静态题扣价未进入Q：%+v", aggregated)
	}
	want := append([]Outcome(nil), rows...)
	for i := range want {
		want[i].Suite, want[i].RefHistRate = "iq", nil
	}
	if got := assessFormal(rows, th, st); !reflect.DeepEqual(got, Assess(want, th, true)) || got.Samples != c.Samples.Paired || got.Verdict != "degraded" {
		t.Fatalf("正式SPRT未使用同一组同期配对：%+v", got)
	}
	for _, key := range []string{"D08", "A11.thinking"} {
		rows[19].CheckKey = key
		if got := assessFormal(rows, th, st); got.Samples != 19 {
			t.Fatalf("显式 %s 被旧suite错误计入智力：%+v", key, got)
		}
	}
	rows[19].CheckKey = "D07"
	for _, calibrated := range []bool{false, true} {
		enabled := !calibrated
		st.CheckOverrides["D07"] = inspection.CheckOverride{Enabled: &enabled, Calibrated: calibrated}
		if got := assessFormal(rows, th, st); got.Samples != 0 {
			t.Fatal("关闭或未校准的 D07 不应进入正式门槛")
		}
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
				if a := assessFormal(rows, th, st); a.Samples != c.Samples.Paired {
					t.Fatalf("SPRT 与 D07 剔除不同步：%+v", a)
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
	// 本次只排除旧套件静态题，保留已有新套件 D07 的推理量观测口径。
	for i := range static {
		static[i].Suite = "inspection_capability"
	}
	checks, _ = Inspect(static, st, time.Now())
	if c := checks["D06"]; c.Samples.Paired != 12 || c.Status != "fail" {
		t.Fatalf("旧套件过滤不应扩大到已有新套件静态观测：%+v", c)
	}
}

func TestModelScopedTrialLegacyAssessmentUnchanged(t *testing.T) {
	rows := outcomes(20, 4, true)
	o := scopedStaticOutcome()
	o.CheckKey = ""
	o.RefHistRate = numberPointer(.95)
	rows = append(rows, o)
	before := Assess(rows, th, true)
	rows[len(rows)-1].CheckKey = "D07"
	if after := Assess(rows, th, true); !reflect.DeepEqual(before, after) {
		t.Fatalf("展示归类改变了旧判定：%+v → %+v", before, after)
	}
}
