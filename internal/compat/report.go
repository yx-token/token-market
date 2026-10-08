package compat

import (
	"encoding/json"
	"fmt"
	"strings"
)

var severityLabel = map[Severity]string{
	SeverityConflict: "冲突",
	SeverityWarning:  "警告",
	SeverityInfo:     "提示",
}

// Markdown 渲染给人看的报告。
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# s2a-compat 兼容检查报告\n\n")
	fmt.Fprintf(&b, "- 当前：%s\n- 目标：%s\n", r.Current, r.Target)
	fmt.Fprintf(&b, "- 待执行迁移：%d 个\n", len(r.Pending))
	fmt.Fprintf(&b, "- 结果：冲突 %d，警告 %d，提示 %d\n\n",
		r.Count(SeverityConflict), r.Count(SeverityWarning), r.Count(SeverityInfo))

	if len(r.Pending) > 0 {
		b.WriteString("## 待执行迁移\n\n")
		for _, name := range r.Pending {
			fmt.Fprintf(&b, "- `%s`\n", name)
		}
		b.WriteString("\n")
	}

	if len(r.Findings) == 0 {
		b.WriteString("未发现与契约相关的变更。\n")
		return b.String()
	}
	b.WriteString("## 检查结果\n\n")
	b.WriteString("| 级别 | 规则 | 位置 | 表.列 | 说明 | 影响模块 |\n|---|---|---|---|---|---|\n")
	for _, f := range r.Findings {
		loc := f.Migration
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.Migration, f.Line)
		}
		target := f.Table
		if f.Column != "" {
			target += "." + f.Column
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | %s | %s | %s |\n",
			severityLabel[f.Severity], f.Rule, loc, target, escapeCell(f.Detail), strings.Join(f.UsedBy, ", "))
	}
	return b.String()
}

// JSON 渲染给流水线使用的报告。
func (r *Report) JSON() ([]byte, error) {
	out := struct {
		*Report
		Conflicts int `json:"conflicts"`
		Warnings  int `json:"warnings"`
		Infos     int `json:"infos"`
	}{r, r.Count(SeverityConflict), r.Count(SeverityWarning), r.Count(SeverityInfo)}
	return json.MarshalIndent(out, "", "  ")
}

func escapeCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
