package quality

import (
	"slices"
	"testing"
)

func TestInspectionConstraints(t *testing.T) {
	tests := []struct {
		name string
		rule string
		good string
		bad  string
	}{
		{"英文词数边界", `{"kind":"word_count","min":3,"max":3}`, "don't stop now", "don't stop"},
		{"扩展汉字数量", `{"kind":"han_count","min":2,"max":2}`, "甲㐀，", "甲，"},
		{"词频忽略大小写", `{"kind":"keyword_count","word":"lamp","n":2}`, "Lamp lamp lamps", "lamp lamps"},
		{"子串计数", `{"kind":"substr_count","text":"灯笼","n":2}`, "灯笼映着灯笼", "灯笼"},
		{"禁用字符", `{"kind":"forbid_chars","chars":"的0"}`, "一盏灯", "一盏的灯"},
		{"空行分段", `{"kind":"paragraphs","n":2,"sep":"blank"}`, "春\n \n夏", "春\n夏"},
		{"显式分段", `{"kind":"paragraphs","n":2,"sep":"***"}`, "春\n***\n夏", "春\n\n夏"},
		{"段落前缀", `{"kind":"para_initials","initials":["春","夏"]}`, "春天\n\n夏夜", "夏夜\n\n春天"},
		{"结束文本", `{"kind":"ends_with","text":"——完"}`, "春天。——完  ", "——完 春天。"},
		{"开始文本", `{"kind":"starts_with","text":"春"}`, " 春天。", "初春。"},
		{"小写文本", `{"kind":"all_lowercase"}`, "quiet lamp", "Quiet lamp"},
		{"句子最大词数", `{"kind":"sentence_max_words","max":3}`, "A lamp glows. We rest.", "A small lamp glows."},
		{"排除字母数字", `{"kind":"no_ascii_alnum"}`, "雨后的城市", "雨后A座"},
		{"词语最多重复", `{"kind":"max_word_repeat","max":2}`, "The lamp lights the room.", "The lamp lights the room the hall."},
		{"项目符号数量", `{"kind":"bullets","n":2}`, "- 甲\n- 乙", "- 甲\n乙"},
		{"轻量结构类型", `{"kind":"json_schema_lite","fields":{"year":"int","tags":"list:2","name":"str"},"exact_keys":true}`, `{"year":2026,"tags":["甲","乙"],"name":"灯"}`, `{"year":true,"tags":["甲","乙"],"name":"灯"}`},
		{"轻量结构额外键", `{"kind":"json_schema_lite","fields":{"name":"str"},"exact_keys":true}`, `{"name":"灯"}`, `{"name":"灯","extra":1}`},
		{"英文句数", `{"kind":"sentence_count","n":2,"language":"en"}`, "Rain falls. Lamps shine!", "Rain falls and lamps shine."},
		{"中文句数", `{"kind":"sentence_count","n":2,"language":"zh"}`, "雨落下。灯亮起！", "雨落下，灯亮起。"},
		{"逐句词数", `{"kind":"sentence_words","min":2,"max":2}`, "Rain falls. Lamps shine.", "Rain falls. Bright lamps shine."},
		{"逐句汉字数", `{"kind":"sentence_han_count","min":3,"max":3,"language":"zh"}`, "雨落下。灯亮起。", "雨落下。灯亮。"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := inspectionGrader(t, `{"type":"constraints","checks":[`+tt.rule+`]}`)
			for _, test := range []struct {
				text string
				pass bool
			}{{tt.good, true}, {tt.bad, false}} {
				result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: test.text}, "")
				if err != nil || result.Pass != test.pass {
					t.Fatalf("约束判定错误：%q => %+v，%v", test.text, result, err)
				}
			}
		})
	}
}

func TestInspectionConstraintResearchReferences(t *testing.T) {
	english := inspectionGrader(t, `{"type":"constraints","checks":[{"kind":"paragraphs","n":3,"sep":"***"},{"kind":"para_initials","sep":"***","initials":["Bridges","Rivers","Roads"]},{"kind":"word_count","min":90,"max":130},{"kind":"keyword_count","word":"lantern","n":3},{"kind":"forbid_chars","chars":","},{"kind":"sentence_max_words","max":18},{"kind":"ends_with","text":"Safe travels."}]}`)
	text := "Bridges in our old town glow at night because every arch holds a small lantern. Visitors stop to watch the light dance on the stones. The city repaints the rails each spring.\n***\n" +
		"Rivers carry the reflections far downstream. Fishermen say the lantern light helps them find the narrow channel before dawn. Children count the boats from the bank.\n***\n" +
		"Roads leading out of town are quieter now. A single lantern marks the last bus stop near the farms. Pack water and a warm coat if you plan to walk. Safe travels."
	result, err := gradeResult(english, ProbeResult{HTTPStatus: 200, Text: text}, "")
	if err != nil || !result.Pass || result.Partial == nil || *result.Partial != 1 {
		t.Fatalf("调研英文参考答案未通过：%+v，%v", result, err)
	}
	chinese := inspectionGrader(t, `{"type":"constraints","checks":[{"kind":"paragraphs","n":4,"sep":"blank"},{"kind":"para_initials","sep":"blank","initials":["春","夏","秋","冬"]},{"kind":"han_count","min":120,"max":160},{"kind":"forbid_chars","chars":"的"},{"kind":"substr_count","text":"灯笼","n":2},{"kind":"no_ascii_alnum"},{"kind":"ends_with","text":"——完"}]}`)
	text = "春天来到小镇，河边柳树抽出新芽，孩子们在石桥上追逐嬉戏，老人坐在门口晒着太阳。\n\n" +
		"夏夜闷热，街坊们把竹椅搬到巷口乘凉，屋檐下挂起一串红灯笼，蝉声一直响到深夜。\n\n" +
		"秋风吹过稻田，金黄稻穗低垂着头，农人忙着收割晾晒，空气里满是新米清香。\n\n" +
		"冬至那天下起小雪，家家户户包饺子，门前灯笼映着白雪，一年就这样安静地过去了。——完"
	result, err = gradeResult(chinese, ProbeResult{HTTPStatus: 200, Text: text}, "")
	if err != nil || !result.Pass || result.Partial == nil || *result.Partial != 1 {
		t.Fatalf("调研中文参考答案未通过：%+v，%v", result, err)
	}
}

func TestInspectionJSONSchema(t *testing.T) {
	g := inspectionGrader(t, `{"type":"json_schema","no_fence":true,"schema":{"type":"object","required":["name","year","tags","summary"],"additionalProperties":false,"properties":{"name":{"type":"string","pattern":"^[a-z]+$"},"year":{"type":"integer","minimum":2020,"maximum":2030},"tags":{"type":"array","minItems":3,"maxItems":3,"items":{"type":"string","pattern":"^[a-z]+$"}},"summary":{"type":"string"}}},"extra":[{"path":"summary","kind":"word_count","min":3,"max":5},{"path":"summary","kind":"forbid_chars","chars":","}]}`)
	tests := []struct {
		name string
		text string
		pass bool
	}{
		{"正确结构", `{"name":"lamp","year":2026,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp"}`, true},
		{"额外键", `{"name":"lamp","year":2026,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp","extra":1}`, false},
		{"必填字段", `{"name":"lamp","year":2026,"tags":["light","desk","quiet"]}`, false},
		{"布尔值不是整数", `{"name":"lamp","year":true,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp"}`, false},
		{"数字越界", `{"name":"lamp","year":2031,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp"}`, false},
		{"数组长度", `{"name":"lamp","year":2026,"tags":["light","desk"],"summary":"A quiet desk lamp"}`, false},
		{"附加规则", `{"name":"lamp","year":2026,"tags":["light","desk","quiet"],"summary":"A quiet, desk lamp"}`, false},
		{"不忽略前导语", `这是结果：{"name":"lamp","year":2026,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp"}`, false},
		{"不忽略第二对象", `{"name":"lamp","year":2026,"tags":["light","desk","quiet"],"summary":"A quiet desk lamp"} {}`, false},
		{"不允许围栏", "```json\n{\"name\":\"lamp\",\"year\":2026,\"tags\":[\"light\",\"desk\",\"quiet\"],\"summary\":\"A quiet desk lamp\"}\n```", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: tt.text}, "")
			if err != nil || result.Pass != tt.pass {
				t.Fatalf("JSON Schema 判定错误：%+v，%v", result, err)
			}
			if !tt.pass && !slices.Contains(result.Flags, "format_error") {
				t.Fatalf("无效 JSON 未保留格式标记：%+v", result)
			}
		})
	}
}

func TestInspectionJSONSchemaReferences(t *testing.T) {
	g := inspectionGrader(t, `{"type":"json_schema","schema":{"$defs":{"amount":{"type":"integer","minimum":1}},"type":"object","properties":{"value":{"$ref":"#/$defs/amount"}}},"extra":[]}`)
	result, err := gradeResult(g, ProbeResult{HTTPStatus: 200, Text: "```json\n{\"value\":1}\n```"}, "")
	if err != nil || !result.Pass {
		t.Fatalf("本地 Schema 引用失败：%+v，%v", result, err)
	}
	g = inspectionGrader(t, `{"type":"json_schema","schema":{"$ref":"https://example.invalid/external.json"}}`)
	if _, err := grade(g, "", ""); err == nil {
		t.Fatal("外部 Schema 引用没有被拒绝")
	}
	g = inspectionGrader(t, `{"type":"json_schema","schema":{"type":"object"},"extra":[{"path":"/a~1b/0","kind":"word_count","min":2,"max":2}]}`)
	result, err = gradeResult(g, ProbeResult{HTTPStatus: 200, Text: `{"a/b":["quiet lamp"]}`}, "")
	if err != nil || !result.Pass {
		t.Fatalf("JSON Pointer 附加规则失败：%+v，%v", result, err)
	}
}
