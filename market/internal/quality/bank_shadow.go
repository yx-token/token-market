package quality

import (
	"context"
	"strings"
	"time"
)

// ProbeRegistration 只在测试中交给替身，不向真实网关传答案。
type ProbeRegistration struct {
	CheckKey         string
	GeneratorVersion string
	WireBody         []byte
	InstanceHash     string
	Step             int
	Platform         string
	Request          ProbeRequest
	Grader           Grader
	Answer           string
}

// 保留旧内置记录及管理员编辑，新版选择器只选已纠正的内置版本。
func obsoleteInspectionBuiltin(key *string) bool {
	if key == nil {
		return false
	}
	switch *key {
	case "inspection/zebra/1", "inspection/constraints/zh/1", "inspection/constraints/en/1", "inspection/output/1", "inspection/abstain/1", "inspection/fact/1":
		return true
	}
	return false
}

func staticAnswer(g Grader) string {
	switch g.Type {
	case "answer_tag":
		return "ANSWER: " + g.Answer
	case "lines_alias":
		var lines []string
		for _, variants := range g.Lines {
			if len(variants) > 0 {
				lines = append(lines, variants[0])
			}
		}
		return strings.Join(lines, "\n")
	default:
		return g.Answer
	}
}

func (e *Engine) executePlan(ctx context.Context, run runRow, ref *runRow, u, refUp *upstreamRow, targetKey, refKey string, plan executionSnapshot, p *Policy, st InspectionSettings, timeout time.Duration) ([]Outcome, []string, error) {
	return e.executePlanItems(ctx, run, ref, u, refUp, targetKey, refKey, plan, p, st, timeout)
}
