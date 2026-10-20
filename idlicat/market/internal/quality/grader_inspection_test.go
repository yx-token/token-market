package quality

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func inspectionGrader(t *testing.T, raw string) Grader {
	t.Helper()
	var g Grader
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatalf("判分器测试配置无法解析：%v", err)
	}
	return g
}

func TestInspectionGraders(t *testing.T) {
	tests := []struct {
		name    string
		grader  string
		text    string
		pass    bool
		partial *float64
		flag    string
	}{
		{"明确答案标记", `{"type":"answer_tag","answer":"721"}`, "过程中的数字 8\nANSWER: 721", true, nil, ""},
		{"不能猜最后数字", `{"type":"answer_tag","answer":"721"}`, "最终数字为 721", false, nil, "format_error"},
		{"取最后明确标记", `{"type":"answer_tag","answer":"721"}`, "ANSWER: 721\nANSWER: 720", false, nil, ""},
		{"围栏内答案", `{"type":"answer_tag","answer":"Paris","norm":"ci"}`, "```text\nANSWER： PARIS\n```", true, nil, ""},
		{"自定义标记", `{"type":"answer_tag","tag":"FINAL","answer":"001234","norm":"int"}`, "FINAL: 1,234", true, nil, ""},
		{"小数不当整数", `{"type":"answer_tag","answer":"12","norm":"int"}`, "ANSWER: 12.0", false, nil, "format_error"},
		{"错误千分位", `{"type":"answer_tag","answer":"12","norm":"int"}`, "ANSWER: 1,2", false, nil, "format_error"},
		{"标记不能跨行取值", `{"type":"answer_tag","answer":"721"}`, "ANSWER:\n721", false, nil, "format_error"},
		{"字段全角标点", `{"type":"fields_exact","fields":{"Pet":"dog, fish","Job":"lawyer, chef"}}`, "PET： Dog，Fish\nJob: lawyer、chef", true, f(1), ""},
		{"字段部分正确", `{"type":"fields_exact","fields":{"A":"59","B":"57"},"inline":true}`, "A＝59，B=58", false, f(0.5), ""},
		{"字段不能缺失", `{"type":"fields_exact","fields":{"A":"59","B":"57"},"inline":true}`, "A=59", false, f(0.5), "format_error"},
		{"重复字段不覆盖", `{"type":"fields_exact","fields":{"A":"59"}}`, "A=58\nA=59", false, f(0), "format_error"},
		{"集合无序", `{"type":"set_exact","items":["Ava","Ben","Dan"]}`, "Dan，Ava Ben", true, f(1), ""},
		{"集合不忽略额外成员", `{"type":"set_exact","items":["Ava","Ben"]}`, "Ben Ava Dan", false, f(2.0 / 3), ""},
		{"集合默认区分大小写", `{"type":"set_exact","items":["Ava","Ben"]}`, "ava Ben", false, f(1.0 / 3), ""},
		{"集合可忽略大小写", `{"type":"set_exact","items":["Ava","Ben"],"case_insensitive":true}`, "AVA ben", true, f(1), ""},
		{"逐行接受别名", `{"type":"lines_alias","lines":[["壹元整","壹元正"],["贰元整"]],"strip_prefix":["人民币"]}`, "1、人民币壹元正\n\n2. 贰元整", true, f(1), ""},
		{"逐行不接受乱序", `{"type":"lines_alias","lines":[["甲"],["乙"]]}`, "乙\n甲", false, f(0), ""},
		{"逐行不忽略额外行", `{"type":"lines_alias","lines":[["甲"]]}`, "甲\n乙", false, f(0.5), "format_error"},
		{"约束全部通过", `{"type":"constraints","checks":[{"kind":"word_count","min":3,"max":3},{"kind":"keyword_count","word":"lantern","n":1},{"kind":"forbid_chars","chars":","}]}`, "One lantern shines.", true, f(1), ""},
		{"约束单项破坏", `{"type":"constraints","checks":[{"kind":"word_count","min":3,"max":3},{"kind":"keyword_count","word":"lantern","n":1},{"kind":"forbid_chars","chars":","}]}`, "One lantern, shines.", false, f(2.0 / 3), ""},
		{"弃权纠正通过", `{"type":"abstain","patterns":["没有.*记录","不存在"],"negative_patterns":["已经确认"]}`, "没有任何可靠记录。", true, nil, ""},
		{"弃权负向优先", `{"type":"abstain","patterns":["不存在"],"negative_patterns":["已经确认"]}`, "不存在疑问，已经确认。", false, nil, ""},
		{"无弃权标记", `{"type":"abstain","patterns":["不存在"]}`, "获奖者是示例人物。", false, nil, ""},
		{"代码完整输出", `{"type":"full_output","fence":"python","min_lines":2,"counts":{"(?m)^print":2},"lazy":"code"}`, "```python\nprint(1)\nprint(2)\n```", true, nil, ""},
		{"代码省略占位", `{"type":"full_output","min_lines":2,"lazy":"code"}`, "print(1)\n# remaining functions omitted", false, nil, "lazy_placeholder"},
		{"代码文字串不误报", `{"type":"full_output","min_lines":1,"lazy":"code"}`, "print('TODO: examples ... unchanged')", true, nil, ""},
		{"未闭合围栏", `{"type":"full_output","min_lines":1}`, "```python\nprint(1)", false, nil, "format_error"},
		{"围栏语言不符", `{"type":"full_output","fence":"python","min_lines":1}`, "```json\n{}\n```", false, nil, "format_error"},
		{"完整输出计数不符", `{"type":"full_output","min_lines":1,"counts":{"(?m)^print":2}}`, "print(1)", false, nil, ""},
		{"数列逐项一致", `{"type":"num_list","numbers":[1,-2,3],"count":3,"sum":2}`, "[1, -2, 3]", true, f(1), ""},
		{"同总和不能乱序", `{"type":"num_list","numbers":[1,2,3],"count":3,"sum":6}`, "3,2,1", false, f(1.0 / 3), ""},
		{"数列多一项", `{"type":"num_list","numbers":[1,2]}`, "1 2 3", false, f(2.0 / 3), ""},
		{"数列省略", `{"type":"num_list","numbers":[1,2,3]}`, "1,2,...,3", false, nil, "lazy_placeholder"},
		{"数列不能误抽小数", `{"type":"num_list","numbers":[1,2]}`, "1.2", false, nil, "format_error"},
		{"数列拒绝解释数字", `{"type":"num_list","numbers":[1,2]}`, "共有2个数：1，2", false, nil, "format_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := inspectionGrader(t, tt.grader)
			result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: tt.text}, "")
			if err != nil || result.Pass != tt.pass {
				t.Fatalf("判分 = %+v，错误 = %v；期望通过 = %v", result, err, tt.pass)
			}
			if tt.partial != nil && (result.Partial == nil || math.Abs(*result.Partial-*tt.partial) > 1e-9) {
				t.Fatalf("部分分 = %v，期望 %v", result.Partial, *tt.partial)
			}
			if tt.flag != "" && !slices.Contains(result.Flags, tt.flag) {
				t.Fatalf("标记 = %v，缺少 %s", result.Flags, tt.flag)
			}
		})
	}
}

func TestInspectionGraderValidation(t *testing.T) {
	invalid := []string{
		`{"type":"answer_tag"}`,
		`{"type":"answer_tag","answer":"abc","norm":"int"}`,
		`{"type":"answer_tag","answer":"1","norm":"float"}`,
		`{"type":"fields_exact"}`,
		`{"type":"fields_exact","fields":{"A":"1","a":"1"}}`,
		`{"type":"set_exact","items":["a","a"]}`,
		`{"type":"set_exact","items":["a"],"sep":"["}`,
		`{"type":"set_exact","items":["a"],"sep":".*"}`,
		`{"type":"lines_alias","lines":[[]]}`,
		`{"type":"constraints","checks":[{"kind":"unknown"}]}`,
		`{"type":"constraints","checks":[{"kind":"word_count","min":4,"max":3}]}`,
		`{"type":"constraints","checks":[{"kind":"keyword_count","word":"word"}]}`,
		`{"type":"json_schema"}`,
		`{"type":"json_schema","schema":{"type":"nonsense"}}`,
		`{"type":"tool_calls","match":"any"}`,
		`{"type":"tool_calls","text_regex":"["}`,
		`{"type":"tool_calls","expect":[{"name":""}]}`,
		`{"type":"tool_calls","tol":{"a":-1}}`,
		`{"type":"abstain","patterns":["["]}`,
		`{"type":"abstain","patterns":[""]}`,
		`{"type":"full_output"}`,
		`{"type":"full_output","min_lines":-1}`,
		`{"type":"full_output","min_lines":1,"sha256":"bad"}`,
		`{"type":"full_output","counts":{"[":1}}`,
		`{"type":"num_list","count":3,"sum":6}`,
		`{"type":"num_list","numbers":[1],"count":-1}`,
		`{"type":"accept_reject","outcome":"reject","http_status":[400]}`,
		`{"type":"accept_reject","outcome":"reject","http_status":[429],"error_codes":["limit"]}`,
		`{"type":"accept_reject","outcome":"accept","http_status":[400]}`,
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if _, err := grade(inspectionGrader(t, raw), "", ""); err == nil {
				t.Fatal("无效规则没有被拒绝")
			}
		})
	}
	valid := []string{
		`{"type":"answer_tag","answer":"{{NEEDLE}}"}`,
		`{"type":"fields_exact","fields":{"A":"1"}}`,
		`{"type":"set_exact","items":["a"]}`,
		`{"type":"lines_alias","lines":[["甲"]]}`,
		`{"type":"constraints","checks":[{"kind":"word_count","min":1}]}`,
		`{"type":"json_schema","schema":{"type":"object"}}`,
		`{"type":"tool_calls","expect":[]}`,
		`{"type":"abstain","patterns":["不存在"]}`,
		`{"type":"full_output","min_lines":1}`,
		`{"type":"num_list","numbers":[]}`,
		`{"type":"accept_reject"}`,
	}
	for _, raw := range valid {
		if _, err := grade(inspectionGrader(t, raw), "", ""); err != nil {
			t.Errorf("有效规则 %s 校验失败：%v", raw, err)
		}
	}
}

func TestInspectionGraderStopEvidence(t *testing.T) {
	g := inspectionGrader(t, `{"type":"answer_tag","answer":"1"}`)
	for _, flag := range []string{"truncated", "refusal", "stop_error"} {
		result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: "ANSWER: 1", Flags: []string{flag, flag}}, "")
		if err != nil || result.Pass || len(result.Flags) != 1 || result.Flags[0] != flag {
			t.Fatalf("结束状态 %s 未保留或仍判为通过：%+v，%v", flag, result, err)
		}
	}
	full := inspectionGrader(t, `{"type":"full_output","min_lines":1,"require_normal_stop":true}`)
	for _, normal := range []bool{false, true} {
		var flags []string
		if normal {
			flags = []string{"stop_normal"}
		}
		result, err := gradeResult(full, ProbeResult{HTTPStatus: 200, Text: "print(1)", Flags: flags}, "")
		if err != nil || result.Pass != normal {
			t.Fatalf("正常结束约束错误：%+v，%v", result, err)
		}
	}
}

func TestInspectionOutputHashes(t *testing.T) {
	g := Grader{Type: "full_output", GraderOptions: GraderOptions{SHA256: hashAnswer("one\ntwo")}}
	result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: "```text\r\none\r\ntwo\r\n```"}, "")
	if err != nil || !result.Pass {
		t.Fatalf("围栏和换行规范化失败：%+v，%v", result, err)
	}
	g = Grader{Type: "num_list", GraderOptions: GraderOptions{SHA256: hashAnswer("1,-2,3")}}
	for _, tt := range []struct {
		text string
		pass bool
	}{{"+1\n-2\n003", true}, {"3,-2,1", false}, {"1,-2,3,0", false}} {
		result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: tt.text}, "")
		if err != nil || result.Pass != tt.pass {
			t.Fatalf("数列哈希判定错误：%s，%+v，%v", tt.text, result, err)
		}
	}
}

func TestInspectionGraderSnapshotRoundTrip(t *testing.T) {
	g := inspectionGrader(t, `{"type":"num_list","numbers":[]}`)
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var restored Grader
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if _, err := grade(restored, "", ""); err != nil || restored.Numbers == nil {
		t.Fatalf("空数列规则在快照往返后丢失：%s，%v", raw, err)
	}
}

func TestAcceptRejectGrader(t *testing.T) {
	reject := inspectionGrader(t, `{"type":"accept_reject","outcome":"reject","http_status":[400],"error_codes":["invalid_request_error"]}`)
	for _, tt := range []struct {
		name string
		res  ProbeResult
		pass bool
	}{
		{"明确拒绝", ProbeResult{HTTPStatus: 400, ErrorClass: "expected_reject", ErrorCode: "invalid_request_error"}, true},
		{"业务码不符", ProbeResult{HTTPStatus: 400, ErrorClass: "http_4xx", ErrorCode: "wrong_code"}, false},
		{"状态码不符", ProbeResult{HTTPStatus: 403, ErrorClass: "http_4xx", ErrorCode: "invalid_request_error"}, false},
		{"平台错误不当官方拒绝", ProbeResult{HTTPStatus: 400, ErrorClass: "platform", ErrorCode: "invalid_request_error"}, false},
		{"限流不当官方拒绝", ProbeResult{HTTPStatus: 400, ErrorClass: "rate_limited", ErrorCode: "invalid_request_error"}, false},
		{"平台标记优先", ProbeResult{HTTPStatus: 400, ErrorClass: "expected_reject", ErrorCode: "invalid_request_error", Flags: []string{"platform"}}, false},
		{"未拒绝", ProbeResult{HTTPStatus: 200}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gradeResult(reject, tt.res, "")
			if err != nil || result.Pass != tt.pass {
				t.Fatalf("接受拒绝判定错误：%+v，%v", result, err)
			}
			if tt.pass && !slices.Contains(result.Flags, "expected_reject") {
				t.Fatal("未保留预期拒绝标记")
			}
		})
	}
	accept := inspectionGrader(t, `{"type":"accept_reject"}`)
	for _, res := range []ProbeResult{{HTTPStatus: 200}, {HTTPStatus: 201}} {
		result, err := gradeResult(accept, res, "")
		if err != nil || !result.Pass {
			t.Fatalf("接受模式错误：%+v，%v", result, err)
		}
	}
}
