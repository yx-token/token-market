package quality

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestInspectionRequestsParticipateInAvailability(t *testing.T) {
	for _, scoring := range []bool{false, true} {
		var rows []Outcome
		for _, suite := range []string{"inspection_capability", "inspection_context", "inspection_function", "inspection_output", "inspection_performance", "inspection_authenticity"} {
			rows = append(rows, Outcome{Suite: suite, Target: ProbeResult{HTTPStatus: 200}}, Outcome{Suite: suite, Target: ProbeResult{HTTPStatus: 503, ErrorClass: "http_5xx"}}, Outcome{Suite: suite})
			for _, class := range []string{"rate_limited", "platform", "expected_reject", "configuration"} {
				rows = append(rows, Outcome{Suite: suite, Target: ProbeResult{HTTPStatus: 429, ErrorClass: class}})
			}
		}
		a := Assess(rows, th, scoring)
		if a.AvailableTotal != 12 || a.AvailableOK != 6 || a.Verdict != "normal" || a.ErrorCounts["http_5xx"] != 6 || a.ErrorCounts["platform"] != 6 || a.Samples != 0 || a.LLR != 0 || a.Grade != "" {
			t.Fatalf("质检题必须按实际请求统计，排除平台故障和未执行题：%+v", a)
		}
		for i := range rows {
			rows[i].Suite = "availability"
		}
		if plain := Assess(rows, th, scoring); !reflect.DeepEqual(a, plain) {
			t.Fatalf("套件名称不应改变请求统计：%+v → %+v", a, plain)
		}
	}
}

func TestInspectionBuiltinDefinitions(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	for _, it := range inspectionBuiltinItems() {
		if it.BuiltinKey == nil || keys[*it.BuiltinKey] || !strings.HasPrefix(it.Suite, "inspection_") {
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
		// 这道迁移题保留旧 regex，不通过生产答案字段暗改判分。
		if *it.BuiltinKey == "inspection/pigeonhole/astra/1" {
			text = "21"
		}
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

func TestInspectionBuiltinCheckAndPlatformAssignments(t *testing.T) {
	platforms := []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "minimax", "opencode_go"}
	outputPlatforms := []string{"anthropic", "gemini", "antigravity", "grok", "kimi", "zhipu", "minimax", "opencode_go"}
	foundOutput, foundZebra, foundHonesty := false, false, false
	for _, it := range inspectionBuiltinItems() {
		for _, platform := range it.Platforms {
			if !slices.Contains(platforms, platform) {
				t.Fatalf("内置题包含未知平台 %s：%s", *it.BuiltinKey, platform)
			}
		}
		switch *it.BuiltinKey {
		case "inspection/output/2":
			foundOutput = true
			if !slices.Equal(it.Platforms, outputPlatforms) {
				t.Fatalf("完整输出适用平台未覆盖除 Codex 外的全部平台：%v", it.Platforms)
			}
		case "inspection/zebra/2":
			foundZebra = true
			d, err := parseGenerator(it.Generator)
			if err != nil || d == nil || d.Version != "2" || d.Parameters["houses"] != json.Number("5") || d.Parameters["attributes"] != json.Number("5") {
				t.Fatalf("斑马题起步规格须为 5×5：%+v %v", d, err)
			}
		case "inspection/abstain/2":
			foundHonesty = true
			if it.CheckKey != "D07.honesty" {
				t.Fatalf("弃权题应归入诚实子类：%s", it.CheckKey)
			}
		}
		if it.CheckKey == "A08" || *it.BuiltinKey == "inspection/fact/1" {
			t.Fatalf("缺少同期安全事实题时不应登记知识代际内置项：%s", *it.BuiltinKey)
		}
	}
	if !foundOutput || !foundZebra || !foundHonesty {
		t.Fatalf("内置定义缺失：完整输出=%t，斑马=%t，诚实=%t", foundOutput, foundZebra, foundHonesty)
	}
}

func TestBankKnightsPromptUsesActualPeople(t *testing.T) {
	for people := 4; people <= 8; people++ {
		got, err := generateKnights(bankNumericTestRNG(uint64(people)), map[string]any{"people": people})
		if err != nil {
			t.Fatal(err)
		}
		order := "按" + strings.Join(bankKnightNames[:people], "") + "的顺序"
		if !strings.Contains(got.Request.Prompt, order) {
			t.Fatalf("题面未使用实际人数顺序：%s", got.Request.Prompt)
		}
		for _, absent := range bankKnightNames[people:] {
			if strings.Contains(got.Request.Prompt, absent) {
				t.Fatalf("%d 人题面出现多余代号 %s", people, absent)
			}
		}
	}
}

func TestInspectionGeneratorValidation(t *testing.T) {
	for _, raw := range []string{`{"template_id":"自由代码","version":"1"}`, `{"template_id":"zebra","version":"3"}`, `{"template_id":"zebra","version":"1","instances":{"full":33}}`} {
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

func TestValidateItemCheckKey(t *testing.T) {
	item := Item{Suite: "inspection_capability", MaxTokens: 32, Prompt: "题面", Grader: Grader{Type: "exact", Answer: "1"}, Generator: json.RawMessage(`{}`)}
	for _, key := range []string{"", "D07", "A11.tools"} {
		item.CheckKey = key
		if err := validateItem(item); err != nil {
			t.Fatalf("合法质检项 %q 被拒绝：%v", key, err)
		}
	}
	item.CheckKey = "D7"
	if err := validateItem(item); err == nil {
		t.Fatal("未注册质检项未被拒绝")
	}
}
