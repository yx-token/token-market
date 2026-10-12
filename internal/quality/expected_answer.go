package quality

import (
	"strconv"
	"strings"
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

// expectedAnswersFromSnapshot 从 probe_runs.execution_snapshot 解析每个（题，实例，步骤）的标准答案。
// 快照缺失或不是预期结构时返回空表，由调用方回退到判分器摘要。
func expectedAnswersFromSnapshot(raw []byte) map[answerKey]string {
	out := map[answerKey]string{}
	if len(raw) == 0 || string(raw) == "{}" {
		return out
	}
	var plan executionSnapshot
	if err := decodeInspectionJSON(raw, &plan); err != nil {
		return out
	}
	for _, instance := range plan.Instances {
		for _, step := range instance.Steps {
			answer := resolveExpectedAnswer(step, instance.Needle)
			if answer == "" {
				continue
			}
			key := answerKey{itemID: instance.ItemID, instanceIdx: instance.InstanceIdx, step: step.Step}
			if _, ok := out[key]; !ok {
				out[key] = answer
			}
		}
	}
	return out
}

// expectedAnswerFor 按（题，实例，步骤）查找标准答案；实例哈希与快照对不上时判定为不可读，不回落判分器。
// 没有题号的旧记录无法对应到任何题目，一律返回空串。
func expectedAnswerFor(answers, hashes map[answerKey]string, itemID *int64, instanceIdx, step int, instanceHash string, grader Grader) string {
	if itemID == nil {
		return ""
	}
	key := answerKey{itemID: *itemID, instanceIdx: instanceIdx, step: step}
	if answer, ok := answers[key]; ok {
		h, hashed := hashes[key]
		if !hashed || instanceHash == "" || h == instanceHash {
			return answer
		}
		return ""
	}
	// 没有快照的旧检测：从题目判分器推出可读答案
	if answer := readableGraderAnswer(grader); answer != "" && !strings.Contains(answer, "{{") {
		return answer
	}
	return ""
}

// snapshotAnswerHashes 返回快照里每个（题，实例，步骤）所属的实例哈希，用于核对结果行与快照是否同一代题库。
func snapshotAnswerHashes(raw []byte) map[answerKey]string {
	out := map[answerKey]string{}
	if len(raw) == 0 || string(raw) == "{}" {
		return out
	}
	var plan executionSnapshot
	if err := decodeInspectionJSON(raw, &plan); err != nil {
		return out
	}
	for _, instance := range plan.Instances {
		for _, step := range instance.Steps {
			key := answerKey{itemID: instance.ItemID, instanceIdx: instance.InstanceIdx, step: step.Step}
			if _, ok := out[key]; !ok {
				out[key] = instance.Hash
			}
		}
	}
	return out
}
