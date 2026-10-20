package compat

import (
	"context"
	"sort"

	"github.com/yx-token/token-market/market/contract"
)

// Report 是一次兼容检查的结果。
type Report struct {
	Target   string    `json:"target"`
	Current  string    `json:"current"`
	Pending  []string  `json:"pending_migrations"`
	Findings []Finding `json:"findings"`
}

// Count 返回某级别的发现数量。
func (r *Report) Count(sev Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// Failed 判断报告是否应阻止升级：存在 failOn 或更严重级别的发现。
func (r *Report) Failed(failOn Severity) bool {
	for _, f := range r.Findings {
		if f.Severity.rank() <= failOn.rank() {
			return true
		}
	}
	return false
}

// CheckStatic 是 s2a-compat 的静态预检：算出目标版本相对当前已执行迁移的待执行迁移，
// 逐个扫描其中与契约相关的变更。
func CheckStatic(_ context.Context, spec *contract.Spec, current Applied, target *MigrationSet) *Report {
	r := &Report{Target: target.Desc, Current: current.Desc}

	targetNames := make(map[string]bool, len(target.Names))
	for _, name := range target.Names {
		targetNames[name] = true
		applied, ok := current.Checksums[name]
		if !ok {
			r.Pending = append(r.Pending, name)
			continue
		}
		if applied != Checksum(target.Contents[name]) {
			r.Findings = append(r.Findings, Finding{
				Severity:  SeverityWarning,
				Rule:      "migration_modified",
				Migration: name,
				Detail:    "已执行的迁移在目标版本中内容被修改，sub2api 启动时的 checksum 校验可能失败，需确认目标版本是否带兼容规则",
			})
		}
	}
	for _, name := range current.Names() {
		if !targetNames[name] {
			r.Findings = append(r.Findings, Finding{
				Severity:  SeverityInfo,
				Rule:      "migration_removed",
				Migration: name,
				Detail:    "已执行的迁移在目标版本中不存在",
			})
		}
	}

	for _, name := range r.Pending {
		r.Findings = append(r.Findings, AnalyzeMigration(spec, name, string(target.Contents[name]))...)
	}

	order := make(map[string]int, len(target.Names))
	for i, name := range target.Names {
		order[name] = i
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Severity.rank() != b.Severity.rank() {
			return a.Severity.rank() < b.Severity.rank()
		}
		if order[a.Migration] != order[b.Migration] {
			return order[a.Migration] < order[b.Migration]
		}
		return a.Line < b.Line
	})
	return r
}
