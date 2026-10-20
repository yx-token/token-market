package quality

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

// 迁移前原定义的独立夹具，防止移动时题面或 regex 悄悄改变。
func legacyCandyItem(t *testing.T) Item {
	t.Helper()
	raw, err := os.ReadFile("testdata/astra_candy_legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	var it Item
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatal(err)
	}
	// RawMessage 的 JSON null 还原为原定义的 nil，不作为生成器配置。
	if string(it.Generator) == "null" {
		it.Generator = nil
	}
	return it
}

func astraCandyItem() Item {
	for _, it := range inspectionBuiltinItems() {
		if it.BuiltinKey != nil && *it.BuiltinKey == "inspection/pigeonhole/astra/1" {
			return it
		}
	}
	panic("降智检测糖果题缺失")
}

// 迁移后的旧库题目与新库内置题必须完全一致，避免两条来源写出的分类不同。
func TestAstraCandyBuiltinMatchesMigrationShape(t *testing.T) {
	it := astraCandyItem()
	if it.Suite != "inspection_degradation" || it.CheckKey != "D14" || !reflect.DeepEqual(it.Models, []string{"gpt-6-astra"}) {
		t.Fatalf("限定型号的内置题应归入降智检测：%+v", it)
	}
	if _, ok := inspection.Lookup(it.CheckKey); !ok {
		t.Fatalf("检测项未登记：%s", it.CheckKey)
	}
}

func TestAstraCandyBuiltinDefinitionAndGrader(t *testing.T) {
	it, want := astraCandyItem(), legacyCandyItem(t)
	key := "inspection/pigeonhole/astra/1"
	// 限定型号的题目属于降智检测：迁移 0033 与新建内置题都归到 inspection_degradation / D14。
	want.Suite, want.CheckKey, want.BuiltinKey = "inspection_degradation", "D14", &key
	want.Modes, want.Generator = []string{"full", "quick"}, json.RawMessage(`{}`)
	want.Status, want.Weight = "active", 1
	if !reflect.DeepEqual(it, want) {
		t.Fatalf("新内置题应只改归类及质检元数据：得到 %+v，期望 %+v", it, want)
	}
	// 回答中出现独立的 21 即通过：前后不能紧挨数字或小数点（句末的句点除外）
	cases := []struct {
		text string
		want bool
	}{
		{"最少需要取出 21 个糖果。", true},
		{"答案：**21**", true},
		{"21", true},
		{"so the answer is 21.", true},
		{"至少要取 29 个", false},
		{"共 121 颗", false},
		{"210", false},
		{"21.5", false},
		{"0.21", false},
	}
	for _, c := range cases {
		if got, err := grade(it.Grader, c.text, ""); err != nil || got != c.want {
			t.Errorf("pigeonhole grade(%q) = %v, %v; want %v", c.text, got, err, c.want)
		}
	}
}

// 型号限定的糖果题进入降智检测的按模型证据，不再混入测智的 D07。
func TestAstraCandyBothModesProduceInspectionEvidence(t *testing.T) {
	it := astraCandyItem()
	candy := scopedStaticOutcome()
	candy.Suite, candy.CheckKey, candy.RequestedModel = it.Suite, it.CheckKey, "gpt-6-astra"
	rows := append(modelScopedD07Rows(), candy)
	// 只跑降智流程时，限定型号的糖果题属于 D14；D07 因未选中测智流程而不判定。
	for _, st := range []InspectionSettings{defaultInspectionSettings(), calibratedInspectionSettings()} {
		checks, _ := Inspect(rows, st, time.Now(), InspectOptions{Flows: []string{inspection.FlowDegradation}, FrozenDegradationModels: []string{"gpt-6-astra"}})
		// 降智题按厂商或型号限定，D14 的最小配对数为 1：单题通过即通过。
		if c := checks["D14"]; c.Samples.Paired != 1 || c.Samples.Observed != 1 || c.Status != "pass" || c.Factor == nil || *c.Factor != 1 {
			t.Fatalf("糖果题未进入 D14 配对证据：%+v", c)
		}
		if c := checks["D07"]; c.Status != "not_run" {
			t.Fatalf("未选中测智流程时 D07 不应判定：%+v", c)
		}
		if a := assessFormal(rows, th, st); a.Samples != 0 || a.AvailableTotal != 20 || a.Verdict != "normal" {
			t.Fatalf("糖果题不应再触发旧SPRT：%+v", a)
		}
	}
}
