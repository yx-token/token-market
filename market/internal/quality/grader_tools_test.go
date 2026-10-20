package quality

import (
	"encoding/json"
	"testing"
)

func TestInspectionToolCalls(t *testing.T) {
	g := inspectionGrader(t, `{"type":"tool_calls","expect":[{"name":"convert","args":{"amount":1250,"currency":"CNY","options":{"round":true},"tags":["quiet","lamp"]}},{"name":"lookup","args":{"city":"Shanghai"}}],"match":"set"}`)
	correct := []ProbeToolCall{
		{Name: "lookup", Arguments: map[string]any{"city": "SHANGHAI"}},
		{Name: "convert", Arguments: map[string]any{"amount": 1250.0, "currency": " cny ", "options": map[string]any{"round": true}, "tags": []any{"quiet", "lamp"}}},
	}
	result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: correct}, "")
	if err != nil || !result.Pass || result.Partial == nil || *result.Partial != 1 {
		t.Fatalf("工具集合语义匹配失败：%+v，%v", result, err)
	}
	wrong := []struct {
		name  string
		calls []ProbeToolCall
	}{
		{"不能用文本冒充调用", nil},
		{"缺少一次调用", correct[:1]},
		{"额外调用", append(append([]ProbeToolCall{}, correct...), ProbeToolCall{Name: "other", Arguments: map[string]any{}})},
		{"布尔值不是数字", []ProbeToolCall{{Name: "lookup", Arguments: map[string]any{"city": "Shanghai"}}, {Name: "convert", Arguments: map[string]any{"amount": true, "currency": "CNY", "options": map[string]any{"round": true}, "tags": []any{"quiet", "lamp"}}}}},
		{"不忽略多余参数", []ProbeToolCall{{Name: "lookup", Arguments: map[string]any{"city": "Shanghai", "extra": 1}}, correct[1]}},
		{"数组顺序须保留", []ProbeToolCall{correct[0], {Name: "convert", Arguments: map[string]any{"amount": 1250, "currency": "CNY", "options": map[string]any{"round": true}, "tags": []any{"lamp", "quiet"}}}}},
	}
	for _, tt := range wrong {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: `convert({"amount":1250})`, ToolCalls: tt.calls}, "")
			if err != nil || result.Pass {
				t.Fatalf("错误工具响应被接受：%+v，%v", result, err)
			}
		})
	}
	g.Match = "sequence"
	result, err = gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: correct}, "")
	if err != nil || result.Pass {
		t.Fatalf("顺序匹配不应接受乱序：%+v，%v", result, err)
	}
	g.Match = "set"
	g.AllowExtraCalls = true
	result, err = gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: append(append([]ProbeToolCall{}, correct...), ProbeToolCall{Name: "other"})}, "")
	if err != nil || !result.Pass {
		t.Fatalf("显式允许额外调用未生效：%+v，%v", result, err)
	}
}

func TestInspectionToolCallsTolerance(t *testing.T) {
	g := inspectionGrader(t, `{"type":"tool_calls","expect":[{"name":"weather","args":{"temperature_celsius":20}},{"name":"weather","args":{"temperature_celsius":20.3}}],"tol":{"temperature_celsius":0.3}}`)
	// 第一条期望能匹配两条响应，第二条只能匹配第一条；不能贪心占用。
	calls := []ProbeToolCall{{Name: "weather", Arguments: map[string]any{"temperature_celsius": 20.1}}, {Name: "weather", Arguments: map[string]any{"temperature_celsius": 19.8}}}
	result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: calls}, "")
	if err != nil || !result.Pass {
		t.Fatalf("工具集合应寻找完整匹配：%+v，%v", result, err)
	}
	g = inspectionGrader(t, `{"type":"tool_calls","expect":[{"name":"weather","args":{"temperature_celsius":20}}],"tol":{"temperature_celsius":0.3}}`)
	for _, tt := range []struct {
		value float64
		pass  bool
	}{{20.3, true}, {20.31, false}} {
		result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: []ProbeToolCall{{Name: "weather", Arguments: map[string]any{"temperature_celsius": tt.value}}}}, "")
		if err != nil || result.Pass != tt.pass {
			t.Fatalf("容差边界错误：%v => %+v，%v", tt.value, result, err)
		}
	}
	g = inspectionGrader(t, `{"type":"tool_calls","expect":[{"name":"lookup","args":{"id":9007199254740992}}]}`)
	result, err = gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: []ProbeToolCall{{Name: "lookup", Arguments: map[string]any{"id": json.Number("9007199254740993")}}}}, "")
	if err != nil || result.Pass {
		t.Fatalf("大整数不应因浮点舍入被误接受：%+v，%v", result, err)
	}
	g = inspectionGrader(t, `{"type":"tool_calls","expect":[{"name":"lookup","args":{"id":9007199254740993}}]}`)
	result, err = gradeResult(g, ProbeResult{HTTPStatus: 200, ToolCalls: []ProbeToolCall{{Name: "lookup", Arguments: map[string]any{"id": json.Number("9007199254740993")}}}}, "")
	if err != nil || !result.Pass {
		t.Fatalf("期望大整数在 JSON 解析时丢失精度：%+v，%v", result, err)
	}
}

func TestInspectionToolCallsNoCall(t *testing.T) {
	g := inspectionGrader(t, `{"type":"tool_calls","expect":[],"text_regex":"[?？]"}`)
	for _, tt := range []struct {
		text  string
		calls []ProbeToolCall
		pass  bool
	}{{"请提供邮箱与时间？", nil, true}, {"请提供邮箱与时间。", nil, false}, {"请提供邮箱与时间？", []ProbeToolCall{{Name: "send"}}, false}} {
		result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: tt.text, ToolCalls: tt.calls}, "")
		if err != nil || result.Pass != tt.pass {
			t.Fatalf("不调用工具规则错误：%+v，%v", result, err)
		}
	}
}
