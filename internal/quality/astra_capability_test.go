package quality

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
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
	panic("能力质检糖果题缺失")
}

func TestAstraCandyBuiltinDefinitionAndGrader(t *testing.T) {
	it, want := astraCandyItem(), legacyCandyItem(t)
	key := "inspection/pigeonhole/astra/1"
	want.Suite, want.CheckKey, want.BuiltinKey = "inspection_capability", "D07", &key
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

func TestAstraCandyTrialOnlyDisplaysInspectionEvidence(t *testing.T) {
	it := astraCandyItem()
	candy := scopedStaticOutcome()
	candy.Suite, candy.CheckKey = it.Suite, it.CheckKey
	for _, scoring := range []bool{false, true} {
		for _, pass := range []bool{false, true} {
			old := outcomes(20, 4, true)
			candy.TargetPass = pass
			before := Assess(old, th, scoring)
			combined := append(old, candy)
			if after := Assess(combined, th, scoring); !reflect.DeepEqual(before, after) {
				t.Fatalf("糖果题污染试运行旧判定：%+v → %+v", before, after)
			}
			shadow := summarizeShadow(combined)
			if shadow == nil || shadow.Applied || shadow.Checks["D07"].Observed != 1 {
				t.Fatalf("糖果题应仅保留影子证据：%+v", shadow)
			}
		}
	}
	// 正式模式与参数化题共享配对和 SPRT，仍遵守校准与剔除规则。
	rows := append(modelScopedD07Rows(), candy)
	st := calibratedInspectionSettings()
	checks, _ := Inspect(rows, st, time.Now())
	if c := checks["D07"]; c.Samples.Paired != 20 || c.Factor == nil || *c.Factor != .7 {
		t.Fatalf("糖果题未进入正式 D07 扣价配对：%+v", c)
	}
	if a := assessFormal(rows, th, st); a.Samples != 20 || a.Verdict != "degraded" {
		t.Fatalf("糖果题未进入正式 SPRT：%+v", a)
	}
}
