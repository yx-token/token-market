package quality

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func snapshotWithInstances(t *testing.T, instances ...plannedInstance) []byte {
	t.Helper()
	return mustJSON(executionSnapshot{SchemaVersion: 1, Instances: instances})
}

func floatp(v float64) *float64 { return &v }

func int64p(v int64) *int64 { return &v }

func TestExpectedAnswersFromSnapshot(t *testing.T) {
	raw := snapshotWithInstances(t,
		plannedInstance{ItemID: 7, InstanceIdx: 0, Hash: "h7-0", Needle: "code-42", Steps: []plannedStep{
			{Step: 0, Answer: "ANSWER: 314159", Expected: Grader{Type: "answer_tag", Answer: "314159"}},
			{Step: 1, Answer: "{{NEEDLE}}", Expected: Grader{Type: "contains", Answer: "{{NEEDLE}}"}},
		}},
		plannedInstance{ItemID: 7, InstanceIdx: 1, Hash: "h7-1", Steps: []plannedStep{
			{Step: 0, Expected: Grader{Type: "numeric", Number: floatp(112)}},
		}},
		plannedInstance{ItemID: 8, InstanceIdx: 0, Hash: "h8-0", Steps: []plannedStep{
			// 快照里没有冻结的 needle，答案仍含占位符，不可读
			{Step: 0, Answer: "{{NEEDLE}}", Expected: Grader{Type: "contains", Answer: "{{NEEDLE}}"}},
		}},
	)
	answers := expectedAnswersFromSnapshot(raw)
	hashes := snapshotAnswerHashes(raw)

	if got := answers[answerKey{itemID: 7, step: 0}]; got != "ANSWER: 314159" {
		t.Fatalf("answer_tag 步骤应取快照 Answer：%q", got)
	}
	if got := answers[answerKey{itemID: 7, instanceIdx: 0, step: 1}]; got != "code-42" {
		t.Fatalf("{{NEEDLE}} 应替换为实例冻结的针：%q", got)
	}
	if got := answers[answerKey{itemID: 7, instanceIdx: 1}]; got != "112" {
		t.Fatalf("numeric 判分器应推出可读答案：%q", got)
	}
	if _, ok := answers[answerKey{itemID: 8}]; ok {
		t.Fatal("未冻结 needle 的答案不应出现在明细里")
	}
	if hashes[answerKey{itemID: 7, instanceIdx: 1}] != "h7-1" {
		t.Fatalf("快照哈希应可用于核对：%v", hashes)
	}
}

func TestExpectedAnswersFromSnapshotRejectsUnusableInput(t *testing.T) {
	for name, raw := range map[string][]byte{
		"空快照":    []byte("{}"),
		"空字节":    nil,
		"非快照":    []byte(`{"foo":1}`),
		"畸形JSON": []byte("{"),
	} {
		if got := expectedAnswersFromSnapshot(raw); len(got) != 0 {
			t.Fatalf("%s 不应解析出标准答案：%v", name, got)
		}
	}
}

func TestExpectedAnswerForMatchesByItemInstanceStep(t *testing.T) {
	raw := snapshotWithInstances(t, plannedInstance{ItemID: 7, InstanceIdx: 1, Hash: "h7-1", Steps: []plannedStep{{Step: 0, Answer: "36"}}})
	answers := expectedAnswersFromSnapshot(raw)
	hashes := snapshotAnswerHashes(raw)
	id, other := int64(7), int64(9)

	if got := expectedAnswerFor(answers, hashes, &id, 1, 0, "h7-1", Grader{}); got != "36" {
		t.Fatalf("三元组命中应返回快照答案：%q", got)
	}
	if got := expectedAnswerFor(answers, hashes, &id, 1, 1, "h7-1", Grader{}); got != "" {
		t.Fatalf("步骤不匹配不应返回答案：%q", got)
	}
	if got := expectedAnswerFor(answers, hashes, &other, 1, 0, "h7-1", Grader{}); got != "" {
		t.Fatalf("题号不匹配不应返回答案：%q", got)
	}
	if got := expectedAnswerFor(answers, hashes, &id, 1, 0, "rotated-hash", Grader{Type: "numeric", Number: floatp(36)}); got != "" {
		t.Fatalf("题库轮换后哈希对不上，不能拿快照答案冒充：%q", got)
	}
}

// legacy 检测没有执行快照，只能从题目判分器推出可读答案；推不出时返回空串而不是错误。
func TestExpectedAnswerForFallsBackToGrader(t *testing.T) {
	id := int64(42)
	for name, tc := range map[string]struct {
		grader Grader
		want   string
	}{
		"数值":   {Grader{Type: "numeric", Number: floatp(36)}, "36"},
		"精确":   {Grader{Type: "exact", Answer: "36"}, "36"},
		"正则":   {Grader{Type: "regex", Pattern: "^3[0-9]$"}, "匹配 /^3[0-9]$/"},
		"键名":   {Grader{Type: "json_keys", Keys: []string{"a", "b"}}, "JSON 含键 a, b"},
		"长上下文": {Grader{Type: "contains", Answer: "{{NEEDLE}}"}, ""},
		"空判分器": {Grader{}, ""},
	} {
		if got := expectedAnswerFor(nil, nil, &id, 0, 0, "", tc.grader); got != tc.want {
			t.Fatalf("%s 判分器摘要：得到 %q，期望 %q", name, got, tc.want)
		}
	}
	if got := expectedAnswerFor(nil, nil, nil, 0, 0, "", Grader{Type: "exact", Answer: "x"}); got != "" {
		t.Fatalf("没有题号的旧记录不应臆测答案：%q", got)
	}
}

// 题库私有：管理员明细带 expected_answer，卖家报告响应里既没有该字段也不能带出判分器原文。
func TestExpectedAnswerOnlyInAdminRunDetail(t *testing.T) {
	detail := RunDetail{Run: RunSummary{ID: 1, BatchID: "b", Trigger: "admin"}, Results: []RunResult{
		{ItemID: int64p(7), Suite: "iq", Category: "math", Prompt: "15% of 240?", Passed: boolp(false), Grader: Grader{Type: "numeric", Number: floatp(36)}, ExpectedAnswer: "36"},
		{ItemID: int64p(8), Suite: "iq", Category: "choice", Prompt: "closest planet", Passed: boolp(true), Grader: Grader{Type: "choice", Answer: "B"}},
	}}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"expected_answer":"36"`) {
		t.Fatalf("管理员检测明细必须带标准答案：%s", raw)
	}
	// 前端按 excerpt 读取模型输出，字段名不能漂移
	excerpt := "模型输出"
	withExcerpt, _ := json.Marshal(RunResult{Excerpt: &excerpt})
	if !strings.Contains(string(withExcerpt), `"excerpt":"模型输出"`) {
		t.Fatalf("模型输出字段名必须为 excerpt：%s", withExcerpt)
	}
	if strings.Contains(string(raw), `"grader"`) || strings.Contains(string(raw), `"36"`) && strings.Count(string(raw), "36") != 1 {
		t.Fatalf("判分器原文不应随明细返回：%s", raw)
	}

	// 卖家看到的报告类型里没有这两个字段
	seller := Report{ID: 9, UpstreamID: 3, Trigger: "scheduled", Verdict: "normal", Summary: json.RawMessage(`{"verdict":"normal"}`), Impact: json.RawMessage(`{}`)}
	rawSeller, err := json.Marshal(seller)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"expected_answer", "grader", "results", "instance_hash"} {
		if strings.Contains(string(rawSeller), leaked) {
			t.Fatalf("卖家报告响应泄露管理员字段 %q：%s", leaked, rawSeller)
		}
	}
	fields := reflect.TypeOf(Report{}).NumField()
	for i := 0; i < fields; i++ {
		if name := reflect.TypeOf(Report{}).Field(i).Name; name == "ExpectedAnswer" || name == "Grader" {
			t.Fatalf("卖家报告结构不应持有 %s", name)
		}
	}
}

func boolp(v bool) *bool { return &v }
