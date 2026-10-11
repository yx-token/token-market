package quality

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestInspectionShadowDoesNotChangeLegacyAssessment(t *testing.T) {
	for _, scoring := range []bool{false, true} {
		for _, failures := range []int{0, 3, 15} {
			old := outcomes(20, failures, true)
			before := Assess(old, th, scoring)
			combined := append([]Outcome(nil), old...)
			for _, suite := range []string{"inspection_capability", "inspection_context", "inspection_function", "inspection_output", "inspection_performance", "inspection_authenticity"} {
				for _, class := range []string{"", "rate_limited", "platform", "http_5xx"} {
					ref := ProbeResult{HTTPStatus: 200, ReasoningTokens: 100000}
					combined = append(combined, Outcome{Suite: suite, CheckKey: "D07", Target: ProbeResult{HTTPStatus: 500, ErrorClass: class, ReasoningTokens: 1}, Ref: &ref, RefPass: true})
				}
			}
			after := Assess(combined, th, scoring)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("影子题污染旧判定：%+v → %+v", before, after)
			}
			shadow := summarizeShadow(combined)
			if shadow == nil || shadow.Applied || shadow.Checks["D07"].Observed != 24 {
				t.Fatalf("影子证据未记录：%+v", shadow)
			}
		}
	}
}

func TestInspectionBuiltinDefinitions(t *testing.T) {
	keys := map[string]bool{}
	if len(defaultItems()) != 22 {
		t.Fatal("旧题数量改变")
	}
	for _, it := range inspectionBuiltinItems() {
		if it.BuiltinKey == nil || keys[*it.BuiltinKey] || scoringSuite(it.Suite) {
			t.Fatalf("内置键或套件不兼容：%+v", it)
		}
		keys[*it.BuiltinKey] = true
		if err := validateItem(it); err != nil {
			t.Fatalf("%s：%v", *it.BuiltinKey, err)
		}
		if d, _ := parseGenerator(it.Generator); d != nil {
			continue
		}
		text := staticAnswer(it.Grader)
		calls := []ProbeToolCall{}
		for _, expected := range it.Grader.Expect {
			calls = append(calls, ProbeToolCall{Name: expected.Name, Arguments: expected.Args})
		}
		r := ProbeResult{HTTPStatus: 200, Text: text, ToolCalls: calls, StopReason: "end_turn", Flags: []string{"stop_normal"}}
		result, err := gradeResult(it.Grader, r, "")
		if err != nil || !result.Pass {
			t.Fatalf("内置静态答案错误 %s：%+v %v", *it.BuiltinKey, result, err)
		}
	}
}

func TestInspectionGeneratorValidation(t *testing.T) {
	for _, raw := range []string{`{"template_id":"自由代码","version":"1"}`, `{"template_id":"zebra","version":"2"}`, `{"template_id":"zebra","version":"1","instances":{"full":33}}`} {
		if _, err := parseGenerator(json.RawMessage(raw)); err == nil {
			t.Fatalf("非法配置未拒绝：%s", raw)
		}
	}
	item := Item{Suite: "inspection_capability", MaxTokens: 32, Grader: Grader{Type: "exact", Answer: "1"}, Generator: json.RawMessage(`{}`)}
	if err := validateItem(item); err == nil {
		t.Fatal("空生成器绕过了静态题的空题面校验")
	}
	item.Request.Prompt = "有效静态请求题面"
	if err := validateItem(item); err != nil {
		t.Fatalf("请求题面未被识别：%v", err)
	}
	item.Request.Prompt = ""
	item.Generator = json.RawMessage(`{"template_id":"register_machine","version":"1"}`)
	if err := validateItem(item); err != nil {
		t.Fatalf("合法生成题不应要求静态题面：%v", err)
	}
}
