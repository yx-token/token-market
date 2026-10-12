package quality

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 标准答案只面向管理员明细接口：题库私有且定期轮换，卖家/买家侧的响应不能携带这些值。

// answerKey 用（题，实例，步骤）三元组定位一条 probe_results 记录，与前端 probeResultKey 同构。
type answerKey struct {
	itemID      int64
	instanceIdx int
	step        int
}

// readableGraderAnswer 把判分器转成可读的期望答案；推不出（或仍含运行时占位符）时返回空串。
func readableGraderAnswer(g Grader) string {
	if answer := staticAnswer(g); answer != "" {
		return answer
	}
	switch g.Type {
	case "numeric":
		if g.Number != nil {
			return strconv.FormatFloat(*g.Number, 'f', -1, 64)
		}
	case "regex":
		if g.Pattern != "" {
			return "匹配 /" + g.Pattern + "/"
		}
	case "json_keys":
		if len(g.Keys) > 0 {
			return "JSON 含键 " + strings.Join(g.Keys, ", ")
		}
	}
	return ""
}

// resolveExpectedAnswer 以快照步骤的 Answer 为准，为空时回退判分器摘要；{{NEEDLE}} 用冻结的针替换。
func resolveExpectedAnswer(step plannedStep, instanceNeedle string) string {
	answer := step.Answer
	if answer == "" {
		answer = readableGraderAnswer(step.Expected)
	}
	if answer == "" {
		return ""
	}
	needle := step.Needle
	if needle == "" {
		needle = instanceNeedle
	}
	if needle != "" {
		answer = strings.ReplaceAll(answer, "{{NEEDLE}}", needle)
	}
	// 占位符没有冻结值时答案不可读，宁缺毋滥
	if strings.Contains(answer, "{{") {
		return ""
	}
	return answer
}

// maxInstancePrompt 限制明细里展示的实例题面长度；长上下文题面可达数十万字符。
const maxInstancePrompt = 2000

// snapshotIndex 是一次检测执行快照按（题，实例，步骤）建立的索引，快照只解析一次。
type snapshotIndex struct {
	answers map[answerKey]string
	hashes  map[answerKey]string
	prompts map[answerKey]string
}

// indexSnapshot 解析 probe_runs.execution_snapshot；快照缺失或不是预期结构时返回空索引，由调用方回退。
func indexSnapshot(raw []byte) snapshotIndex {
	idx := snapshotIndex{answers: map[answerKey]string{}, hashes: map[answerKey]string{}, prompts: map[answerKey]string{}}
	if len(raw) == 0 || string(raw) == "{}" {
		return idx
	}
	var plan executionSnapshot
	if err := decodeInspectionJSON(raw, &plan); err != nil {
		return idx
	}
	for _, instance := range plan.Instances {
		for _, step := range instance.Steps {
			key := answerKey{itemID: instance.ItemID, instanceIdx: instance.InstanceIdx, step: step.Step}
			if _, ok := idx.hashes[key]; ok {
				continue
			}
			idx.hashes[key] = instance.Hash
			if answer := resolveExpectedAnswer(step, instance.Needle); answer != "" {
				idx.answers[key] = answer
			}
			if prompt := stepPrompt(step.Request); prompt != "" {
				idx.prompts[key] = prompt
			}
		}
	}
	return idx
}

// stepPrompt 取一步请求实际发出的题面：优先 prompt，否则取最后一条用户消息的文本；超长时截断并注明总长。
func stepPrompt(req ProbeRequest) string {
	text := strings.TrimSpace(req.Prompt)
	for i := len(req.Messages) - 1; text == "" && i >= 0; i-- {
		if req.Messages[i].Role != "user" {
			continue
		}
		var parts []string
		for _, c := range req.Messages[i].Content {
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		text = strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if total := utf8.RuneCountInString(text); total > maxInstancePrompt {
		return truncateStr(text, maxInstancePrompt) + fmt.Sprintf("\n…（题面共 %d 字，仅显示前 %d 字）", total, maxInstancePrompt)
	}
	return text
}

// lookup 按三元组取快照里的值；结果行的实例哈希与快照对不上（题库已轮换）时视为不可用。
func (idx snapshotIndex) lookup(values map[answerKey]string, itemID *int64, instanceIdx, step int, instanceHash string) (string, bool) {
	if itemID == nil {
		return "", false
	}
	key := answerKey{itemID: *itemID, instanceIdx: instanceIdx, step: step}
	value, ok := values[key]
	if !ok {
		return "", false
	}
	if h, hashed := idx.hashes[key]; hashed && h != "" && instanceHash != "" && h != instanceHash {
		return "", true
	}
	return value, true
}

// expectedAnswersFromSnapshot 从执行快照解析每个（题，实例，步骤）的标准答案。
func expectedAnswersFromSnapshot(raw []byte) map[answerKey]string {
	return indexSnapshot(raw).answers
}

// expectedAnswerFor 按（题，实例，步骤）查找标准答案；实例哈希与快照对不上时判定为不可读，不回落判分器。
// 没有题号的旧记录无法对应到任何题目，一律返回空串。
func expectedAnswerFor(answers, hashes map[answerKey]string, itemID *int64, instanceIdx, step int, instanceHash string, grader Grader) string {
	if itemID == nil {
		return ""
	}
	if answer, found := (snapshotIndex{hashes: hashes}).lookup(answers, itemID, instanceIdx, step, instanceHash); found {
		return answer
	}
	// 没有快照的旧检测：从题目判分器推出可读答案
	if answer := readableGraderAnswer(grader); answer != "" && !strings.Contains(answer, "{{") {
		return answer
	}
	return ""
}

// snapshotAnswerHashes 返回快照里每个（题，实例，步骤）所属的实例哈希，用于核对结果行与快照是否同一代题库。
func snapshotAnswerHashes(raw []byte) map[answerKey]string {
	return indexSnapshot(raw).hashes
}

// fillFromSnapshot 为明细行补标准答案与实例题面，二者都只随管理员明细返回。
func fillFromSnapshot(results []RunResult, raw []byte) {
	idx := indexSnapshot(raw)
	for i := range results {
		r := &results[i]
		r.ExpectedAnswer = expectedAnswerFor(idx.answers, idx.hashes, r.ItemID, r.InstanceIdx, r.Step, r.InstanceHash, r.Grader)
		r.InstancePrompt, _ = idx.lookup(idx.prompts, r.ItemID, r.InstanceIdx, r.Step, r.InstanceHash)
	}
}
