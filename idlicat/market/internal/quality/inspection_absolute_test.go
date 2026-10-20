package quality

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yx-token/idlicat/market/internal/inspection"
)

func itemOutcome(key string, pass bool) Outcome {
	o := pairedInspectionOutcome(key, pass, true)
	o.Ref = nil
	return o
}

func itemRows(key string, n, passed int) []Outcome {
	rows := make([]Outcome, n)
	for i := range rows {
		rows[i] = itemOutcome(key, i < passed)
		rows[i].ItemID = int64(i)
	}
	return rows
}

func TestItemIntelligenceWilsonBranches(t *testing.T) {
	for _, key := range []string{"D07", "D07.honesty", "D09"} {
		for _, tt := range []struct {
			name    string
			n, pass int
			status  string
		}{
			{"通过", 100, 90, "pass"}, {"需关注", 100, 60, "warning"}, {"未通过", 100, 40, "fail"},
			{"边缘证据保守通过", 20, 14, "pass"}, {"样本不足", 2, 2, "inconclusive"},
		} {
			t.Run(key+tt.name, func(t *testing.T) {
				checks, _ := Inspect(itemRows(key, tt.n, tt.pass), calibratedInspectionSettings(), time.Time{})
				c := checks[key]
				if c.Status != tt.status || c.Evidence.Source != "item_probe" || c.Samples.Paired != 0 {
					t.Fatalf("判定错误：%+v", c)
				}
				if tt.status == "inconclusive" && !strings.Contains(c.Reason, "按题目判定样本不足") {
					t.Fatal("缺少样本不足说明")
				}
				if tt.status != "inconclusive" && *c.Metrics["expected_pass_rate"].Value != .9 {
					t.Fatal("默认期望错误")
				}
			})
		}
	}
	rows := itemRows("D07", 24, 24)
	rows[0].Target.Flags = []string{"refusal"}
	rows[1].Target.Flags = []string{"truncated"}
	rows[2].Target.Flags = []string{"stop_error"}
	rows[3].Target.ErrorClass = "platform"
	checks, _ := Inspect(rows, calibratedInspectionSettings(), time.Time{})
	if c := checks["D07"]; c.Samples.Excluded != 4 || c.Status != "pass" || *c.Metrics["target_pass_rate"].Value != 1 {
		t.Fatalf("不能把不可计分行当作智力失败：%+v", c)
	}
	st := calibratedInspectionSettings()
	st.CheckOverrides["D07"] = inspection.CheckOverride{Thresholds: map[string]float64{"expected_pass_rate": .5}}
	checks, _ = Inspect(itemRows("D07", 100, 60), st, time.Time{})
	if checks["D07"].Status != "pass" || *checks["D07"].Metrics["expected_pass_rate"].Value != .5 {
		t.Fatal("期望通过率覆盖未生效")
	}
}

func TestItemFunctionOfficialCapabilities(t *testing.T) {
	for _, key := range []string{"A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "D08"} {
		for _, tt := range []struct {
			name    string
			pass    bool
			support *bool
			status  string
		}{
			{"未登记但答对", true, nil, "pass"}, {"支持但答错", false, capabilityBoolPointer(true), "fail"},
			{"不支持", false, capabilityBoolPointer(false), "not_applicable"}, {"未登记", false, nil, "inconclusive"},
		} {
			t.Run(key+tt.name, func(t *testing.T) {
				cap := inspection.ModelCapability{Tools: tt.support, JSONSchema: tt.support, MultiTurn: tt.support, Image: tt.support, PDF: tt.support}
				o := itemOutcome(key, tt.pass)
				o.ProbeOutputTokens = 1000
				if key == "D08" && tt.support != nil {
					cap.MaxOutput = 2000
					if !*tt.support {
						cap.MaxOutput = 500
					}
				}
				checks, _ := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &cap})
				c := checks[key]
				if c.Status != tt.status || c.Samples.Paired != 0 {
					t.Fatalf("功能判定错误：%+v", c)
				}
				if tt.status == "fail" && !strings.Contains(c.Reason, "官方支持该功能，被测未兑现；未与基线对照") {
					t.Fatal("失败归因不明确")
				}
			})
		}
	}
	cap := inspection.ModelCapability{Image: capabilityBoolPointer(true), PDF: capabilityBoolPointer(false)}
	o := itemOutcome("A11.image_pdf", false)
	o.ProbeFeature = "pdf"
	checks, _ := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &cap})
	if checks[o.CheckKey].Status != "not_applicable" {
		t.Fatal("不能用图片能力代替 PDF 能力")
	}
	o.ProbeFeature = "image"
	checks, _ = Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &cap})
	if checks[o.CheckKey].Status != "fail" {
		t.Fatal("图片能力查找错误")
	}
	cap = inspection.LookupCapability("grok", "grok-4.7", inspection.Defaults())
	o = itemOutcome("D08", false)
	o.RequestedModel = "grok-4.7"
	checks, _ = Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &cap})
	if checks["D08"].Status != "fail" {
		t.Fatal("官方无输出上限不能当作未登记")
	}
}

func TestItemThinkingBranches(t *testing.T) {
	for _, tt := range []struct {
		name     string
		tokens   int
		observed bool
		support  *bool
		status   string
	}{
		{"有推理", 1, true, nil, "pass"}, {"明确为零", 0, true, capabilityBoolPointer(true), "fail"},
		{"用量缺失", 0, false, capabilityBoolPointer(true), "inconclusive"}, {"能力未登记", 0, true, nil, "inconclusive"},
		{"官方不支持", 0, true, capabilityBoolPointer(false), "not_applicable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := itemOutcome("A11.thinking", true)
			o.Target.ReasoningTokens, o.Target.ReasoningObserved = tt.tokens, tt.observed
			checks, _ := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &inspection.ModelCapability{Thinking: tt.support}})
			if c := checks[o.CheckKey]; c.Status != tt.status {
				t.Fatalf("推理用量判定错误：%+v", c)
			}
		})
	}
}

func TestItemPermissionAndContextBranches(t *testing.T) {
	for _, tt := range []struct {
		name, key        string
		window           int
		ok, marks        bool
		input            *int64
		code, errorClass string
		status           string
		gate             bool
	}{
		{"权限接受", "A04", 0, true, false, nil, "", "", "pass", false},
		{"权限拒绝", "A04", 0, false, false, nil, "model_access_denied", "http_4xx", "fail", true},
		{"权限错误来源未知", "A04", 0, false, false, nil, "model_access_denied", "platform", "inconclusive", false},
		{"权限其他错误", "A04", 0, false, false, nil, "", "network", "inconclusive", false},
		{"窗口不足", "A05", 200000, false, false, nil, "context_length_exceeded", "expected_reject", "not_applicable", false},
		{"边界通过", "A05", 272000, true, true, int64Pointer(262145), "", "", "pass", false},
		{"官方支持但超限", "A05", 272000, false, false, nil, "context_length_exceeded", "expected_reject", "fail", true},
		{"窗口未登记接受且证据完整", "A05", 0, true, true, int64Pointer(262145), "", "", "pass", false},
		{"窗口未登记接受但标记失败", "A05", 0, true, false, nil, "", "", "inconclusive", false},
		{"窗口未登记拒绝", "A05", 0, false, false, nil, "context_length_exceeded", "expected_reject", "inconclusive", false},
		{"标记失败", "A05", 272000, true, false, int64Pointer(262145), "", "", "inconclusive", false},
		{"计数不足", "A05", 272000, true, true, int64Pointer(200000), "", "", "inconclusive", false},
		{"计数缺失", "A05", 272000, true, true, nil, "", "", "inconclusive", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := itemOutcome(tt.key, tt.marks)
			o.Target.InputTokens = tt.input
			if !tt.ok {
				o.Target.HTTPStatus = 403
				o.Target.ErrorCode, o.Target.ErrorClass = tt.code, tt.errorClass
			}
			cap := inspection.ModelCapability{ContextWindow: 1000000, ContextWindowByAuth: map[string]int{"oauth": tt.window}}
			checks, gates := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{AuthType: "oauth", Capability: &cap})
			if c := checks[tt.key]; c.Status != tt.status || (len(gates) > 0) != tt.gate || c.Factor != nil {
				t.Fatalf("边界判定错误：%+v %+v", c, gates)
			}
			if tt.gate && !gates[0].Confirmed {
				t.Fatal("校准门槛未确认")
			}
		})
	}
	o := itemOutcome("A04", false)
	o.Target.HTTPStatus, o.Target.ErrorCode, o.Target.ErrorClass = 403, "model_access_denied", "http_4xx"
	_, gates := Inspect([]Outcome{o}, defaultInspectionSettings(), time.Time{})
	if len(gates) != 1 || gates[0].Confirmed {
		t.Fatal("未校准门槛不能确认")
	}
	o.PrimaryModel = false
	_, gates = Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{})
	if len(gates) != 0 {
		t.Fatal("其他型号权限不能阻塞主型号")
	}
}

func TestItemReasoningAndSharedEvidence(t *testing.T) {
	st := calibratedInspectionSettings()
	var rows []Outcome
	for i := 0; i < 10; i++ {
		h, l := itemOutcome("D07", true), itemOutcome("D06", true)
		h.ItemID, l.ItemID, l.InstanceIdx = int64(i), int64(i), 1000
		h.GeneratorVersion = "math/1"
		h.Target.ReasoningTokens, l.Target.ReasoningTokens = 130, 100
		rows = append(rows, h, l)
	}
	checks, _ := Inspect(rows, st, time.Time{})
	if c := checks["D06"]; c.Status != "pass" || math.Abs(*c.Metrics["high_low_ratio"].Value-1.3) > 1e-12 || c.Samples.Paired != 0 {
		t.Fatalf("自身档位比未生效：%+v", c)
	}
	for i := range rows {
		rows[i].Target.ReasoningTokens = 100
	}
	checks, _ = Inspect(rows, st, time.Time{})
	if c := checks["D06"]; c.Status != "inconclusive" || c.Factor != nil || c.Evidence.Code != "needs_baseline" {
		t.Fatalf("量差不足不能断言降档：%+v", c)
	}
	checks, _ = Inspect(rows[:2], st, time.Time{})
	if checks["D06"].Status != "inconclusive" {
		t.Fatal("D06 样本不足应无法判定")
	}
	o := itemOutcome("D07", true)
	o.Target.ResponseModel, o.RequestedModel = "CLAUDE-SONNET-4-5-20250929", "claude-sonnet-4.5"
	checks, _ = Inspect([]Outcome{o}, st, time.Time{})
	if checks["A02"].Status != "pass" || checks["A12"].Status != "pass" {
		t.Fatal("规范化型号或无拒答判定错误")
	}
	o.Target.ResponseModel = "claude-haiku-4-5"
	o.Target.Flags = []string{"refusal"}
	checks, _ = Inspect([]Outcome{o}, st, time.Time{})
	for _, key := range []string{"A02", "A12"} {
		if c := checks[key]; c.Status != "warning" || c.Factor != nil {
			t.Fatalf("显示项不能扣价：%+v", c)
		}
	}
	for _, key := range []string{"A03", "A15", "D10"} {
		if c := checks[key]; c.Status != "inconclusive" || c.Evidence.Code != "needs_baseline" || !strings.Contains(c.Reason, "需要基线账号对照") {
			t.Fatalf("缺少对照说明：%+v", c)
		}
	}
}

func TestItemCacheBranches(t *testing.T) {
	d, _ := inspection.Effective("D12", calibratedInspectionSettings())
	for _, tt := range []struct {
		name    string
		steps   *[2]int64
		support *bool
		status  string
	}{
		{"命中", &[2]int64{0, 10}, nil, "pass"}, {"支持但未命中", &[2]int64{}, capabilityBoolPointer(true), "warning"},
		{"不支持", &[2]int64{}, capabilityBoolPointer(false), "not_applicable"}, {"未登记", &[2]int64{}, nil, "inconclusive"},
		{"缺少用量", nil, capabilityBoolPointer(true), "inconclusive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newInspectionCheck(d, time.Time{})
			inspectItemCache(&c, tt.steps, d, tt.support)
			if c.Status != tt.status || c.Evidence.Source != "item_probe" || c.Samples.Paired != 0 {
				t.Fatalf("缓存判定错误：%+v", c)
			}
		})
	}
}

func TestItemOnline22FixtureAndFormalScoring(t *testing.T) {
	rows := append(itemRows("D07", 57, 56), itemRows("D09", 10, 9)...)
	for _, key := range []string{"A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "A11.thinking", "A04", "D08"} {
		rows = append(rows, itemOutcome(key, true))
	}
	st := calibratedInspectionSettings()
	checks, _ := Inspect(rows, st, time.Time{}, InspectOptions{Platform: "openai", AuthType: "oauth"})
	for _, key := range []string{"D07", "D09", "A11.tools", "A11.json_schema", "A11.multiturn", "A11.image_pdf", "A11.thinking", "A04", "D08"} {
		if checks[key].Status != "pass" {
			t.Fatalf("#22 %s 应通过：%+v", key, checks[key])
		}
	}
	for _, key := range []string{"A03", "A15"} {
		if !strings.Contains(checks[key].Reason, "需要基线账号对照") {
			t.Fatalf("#22 %s 应需要基线", key)
		}
	}
	a := assessFormal(rows, th, st)
	if a.Samples != 0 || a.Baseline || a.Verdict != "normal" || a.AvailableTotal != len(rows) {
		t.Fatalf("检测项之外的总结应只保留可用统计：%+v", a)
	}
	for i := range rows {
		if rows[i].CheckKey == "D07" || rows[i].CheckKey == "D09" {
			rows[i].TargetPass = false
		}
	}
	checks, _ = Inspect(rows, st, time.Time{})
	if checks["D07"].Status != "fail" || checks["D09"].Status != "fail" {
		t.Fatal("测智未通过应由各检测项判定")
	}
	if got := assessFormal(rows, th, st); got.Verdict != "normal" || got.Samples != 0 {
		t.Fatalf("答题未通过不能触发旧SPRT暂停门槛：%+v", got)
	}

}

func TestPairedOptionsDoNotChangeResults(t *testing.T) {
	st := calibratedInspectionSettings()
	rows := []Outcome{}
	for _, key := range []string{"D07", "D07.honesty", "D09", "A11.tools", "A11.json_schema", "A11.image_pdf", "A11.multiturn", "A11.thinking", "A04", "A05", "D08", "A15"} {
		for i := 0; i < 30; i++ {
			o := pairedInspectionOutcome(key, i%2 == 0, true)
			o.ItemID = int64(i)
			rows = append(rows, o)
		}
	}
	before, gates := Inspect(rows, st, time.Time{})
	after, newGates := Inspect(rows, st, time.Time{}, InspectOptions{Capability: &inspection.ModelCapability{ContextWindow: 1, Tools: capabilityBoolPointer(false)}})
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(gates, newGates) {
		t.Fatal("能力登记污染有基线判定")
	}
	// 批次只有部分配对时，缺基线行仍须剔除，不能与按题目样本混算。
	rows = itemRows("D07", 30, 30)
	ref := rows[0].Target
	rows[0].Ref, rows[0].RefPass = &ref, true
	checks, _ := Inspect(rows, st, time.Time{})
	if c := checks["D07"]; c.Status != "inconclusive" || c.Samples.Paired != 1 || c.Samples.Excluded != 29 || c.Evidence.Source != "paired_probe" {
		t.Fatalf("混合批次绕过配对：%+v", c)
	}
	if got := assessFormal(rows, th, st); got.Samples != 0 || got.AvailableTotal != 30 {
		t.Fatal("混合批次的成功请求仍应全部计入可用统计")
	}
}

func TestItemMissingObservationsAndFrozenProbeMetadata(t *testing.T) {
	for _, tt := range []struct {
		key    string
		change func(*Outcome)
	}{
		{"A02", func(o *Outcome) { o.Target.ResponseModel = "" }},
		{"A12", func(o *Outcome) { o.Target.ErrorClass = "network"; o.Target.HTTPStatus = 0 }},
		{"A11.tools", func(o *Outcome) { o.Target.ErrorClass = "platform"; o.Target.HTTPStatus = 503 }},
	} {
		o := itemOutcome(tt.key, false)
		tt.change(&o)
		checks, _ := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{})
		if c := checks[tt.key]; c.Status != "inconclusive" || c.Factor != nil {
			t.Fatalf("缺失证据不能断言失败：%+v", c)
		}
	}
	for _, feature := range []string{"image", "pdf"} {
		media := "image/png"
		if feature == "pdf" {
			media = "application/pdf"
		}
		it := plannedInstance{ItemID: 1, CheckKey: "A11.image_pdf", Parameters: map[string]any{"target_tokens": 300000}, Steps: []plannedStep{{Request: ProbeRequest{Model: "gpt-6-astra", Attachments: []ProbeAttachment{{MediaType: media}}}, Answer: "完整输出"}}}
		// 快照从 JSON 读回时，参数是 float64；不能丢失实际探针长度。
		var restored plannedInstance
		if err := json.Unmarshal(mustJSON(it), &restored); err != nil {
			t.Fatal(err)
		}
		o := instanceOutcome(restored)
		if o.ProbeFeature != feature || o.ProbeTokens != 300000 || o.ProbeOutputTokens <= 0 {
			t.Fatalf("冻结探针元数据缺失：%+v", o)
		}
	}
	o := itemOutcome("A05", true)
	o.ProbeTokens = 300000
	checks, _ := Inspect([]Outcome{o}, calibratedInspectionSettings(), time.Time{}, InspectOptions{Capability: &inspection.ModelCapability{ContextWindow: 272000}})
	if checks["A05"].Status != "not_applicable" {
		t.Fatal("不能用新设置的默认探针长度覆盖执行快照")
	}
}
