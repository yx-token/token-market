package quality

import (
	"context"
	"errors"
	"sort"
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

type ShadowCounts struct {
	Planned         int            `json:"planned"`
	NotRun          int            `json:"not_run"`
	Observed        int            `json:"observed"`
	Passed          int            `json:"passed"`
	Paired          int            `json:"paired"`
	ReferencePassed int            `json:"reference_passed"`
	ErrorCounts     map[string]int `json:"error_counts,omitempty"`
}

type ShadowSummary struct {
	Applied bool                     `json:"applied"`
	Checks  map[string]*ShadowCounts `json:"checks"`
}

func summarizeShadow(rows []Outcome) *ShadowSummary {
	var summary *ShadowSummary
	for _, row := range rows {
		if !strings.HasPrefix(row.Suite, "inspection_") {
			continue
		}
		if summary == nil {
			summary = &ShadowSummary{Checks: map[string]*ShadowCounts{}}
		}
		key := row.CheckKey
		if key == "" {
			key = row.Suite
		}
		c := summary.Checks[key]
		if c == nil {
			c = &ShadowCounts{}
			summary.Checks[key] = c
		}
		c.Planned++
		if row.Target.HTTPStatus == 0 && row.Target.ErrorClass == "" {
			c.NotRun++
			continue
		}
		c.Observed++
		if row.TargetPass {
			c.Passed++
		}
		countProbeError(&c.ErrorCounts, row.Target.ErrorClass)
		if row.Ref != nil && row.Target.EligibleForIntelligence() && row.Ref.EligibleForIntelligence() {
			c.Paired++
			if row.RefPass {
				c.ReferencePassed++
			}
		}
	}
	return summary
}

// 旧选择器保持原样；影子题只由新入口按模式与显式启用套件读取。
func selectExecutionItems(ctx context.Context, db DB, platform, model string, suites []string, mode string) ([]Item, error) {
	items, err := selectItems(ctx, db, platform, model, suites)
	if err != nil || mode == "legacy" {
		return items, err
	}
	var wanted []string
	for _, suite := range suites {
		if strings.HasPrefix(suite, "inspection_") {
			wanted = append(wanted, suite)
		}
	}
	if len(wanted) == 0 {
		return items, nil
	}
	rows, err := db.Query(ctx, "SELECT "+itemColumns+` FROM probe_items WHERE status='active' AND suite=ANY($1) AND (cardinality(platforms)=0 OR $2=ANY(platforms)) AND (cardinality(models)=0 OR $3=ANY(models)) AND (cardinality(modes)=0 OR $4=ANY(modes)) ORDER BY id`, wanted, platform, model, mode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		if obsoleteInspectionBuiltin(it.BuiltinKey) {
			continue
		}
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, rows.Err()
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

// 影子题在旧题完成后执行；其预算、配置或时限失败只保留证据，不能改变旧结论。
func (e *Engine) executePlan(ctx context.Context, run runRow, ref *runRow, u, refUp *upstreamRow, targetKey, refKey string, plan executionSnapshot, p *Policy, st InspectionSettings, timeout time.Duration) ([]Outcome, []string, error) {
	legacy, shadow := plan, plan
	legacy.Instances, shadow.Instances = nil, nil
	legacy.Warnings, shadow.Warnings = nil, nil
	for _, instance := range plan.Instances {
		if strings.HasPrefix(instance.Item.Suite, "inspection_") {
			shadow.Instances = append(shadow.Instances, instance)
		} else {
			legacy.Instances = append(legacy.Instances, instance)
		}
	}
	if len(shadow.Instances) == 0 {
		return e.executePlanItems(ctx, run, ref, u, refUp, targetKey, refKey, plan, p, st, timeout)
	}
	var rows []Outcome
	warnings := append([]string(nil), plan.Warnings...)
	if len(legacy.Instances) > 0 {
		var err error
		var legacyWarnings []string
		rows, legacyWarnings, err = e.executePlanItems(ctx, run, ref, u, refUp, targetKey, refKey, legacy, p, st, timeout)
		warnings = append(warnings, legacyWarnings...)
		if err != nil {
			return rows, warnings, err
		}
	}
	newRows, newWarnings, err := e.executePlanItems(ctx, run, ref, u, refUp, targetKey, refKey, shadow, p, st, timeout)
	rows = append(rows, newRows...)
	warnings = append(warnings, newWarnings...)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return rows, warnings, err
		}
		warnings = append(warnings, "影子检测未完成，仅保留证据："+err.Error())
	}
	return rows, warnings, nil
}
