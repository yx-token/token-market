package compat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yx-token/idlicat/market/contract"
)

type Severity string

const (
	SeverityConflict Severity = "conflict" // 会破坏本系统，必须先改本系统
	SeverityWarning  Severity = "warning"  // 需要人工确认或安排维护窗口
	SeverityInfo     Severity = "info"     // 仅提示
)

func (s Severity) rank() int {
	switch s {
	case SeverityConflict:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// Finding 是一条检查结果。
type Finding struct {
	Severity  Severity `json:"severity"`
	Rule      string   `json:"rule"`
	Migration string   `json:"migration,omitempty"`
	Line      int      `json:"line,omitempty"`
	Table     string   `json:"table,omitempty"`
	Column    string   `json:"column,omitempty"`
	Detail    string   `json:"detail"`
	UsedBy    []string `json:"used_by,omitempty"`
}

// analyzer 对单个迁移文件的语句逐条套用规则。
type analyzer struct {
	spec      *contract.Spec
	migration string
	findings  []Finding
}

func (a *analyzer) add(sev Severity, rule string, line int, table, column, detail string) {
	f := Finding{Severity: sev, Rule: rule, Migration: a.migration, Line: line, Table: table, Column: column, Detail: detail}
	if t, ok := a.spec.Table(table); ok {
		f.UsedBy = t.UsedBy
	}
	a.findings = append(a.findings, f)
}

// AnalyzeMigration 扫描一个迁移文件，返回与契约相关的发现。
func AnalyzeMigration(spec *contract.Spec, name, sql string) []Finding {
	a := &analyzer{spec: spec, migration: name}
	for _, st := range SplitStatements(sql) {
		a.statement(st)
	}
	return a.findings
}

func (a *analyzer) statement(st Statement) {
	toks := tokenize(st.Text)
	if len(toks) == 0 {
		return
	}
	switch {
	case toks[0].is("alter") && len(toks) > 1 && toks[1].is("table"):
		a.alterTable(toks, st.Line)
	case toks[0].is("drop") && len(toks) > 1 && toks[1].is("table"):
		a.dropTable(toks, st.Line)
	case toks[0].is("truncate"):
		a.truncate(toks, st.Line)
	case toks[0].is("create"):
		a.create(toks, st.Line)
	case toks[0].is("update"):
		a.dml(toks, 1, st.Line, "UPDATE")
	case toks[0].is("delete") && len(toks) > 1 && toks[1].is("from"):
		a.dml(toks, 2, st.Line, "DELETE")
	case toks[0].is("do"):
		a.proceduralBody(toks, st.Line)
	}
}

func (a *analyzer) alterTable(toks []token, line int) {
	i := 2
	i = skipWords(toks, i, "if", "exists")
	i = skipWords(toks, i, "only")
	table, i := readQualifiedName(toks, i)
	if table == "" {
		return
	}
	if i < len(toks) && toks[i].kind == tokPunct && toks[i].text == "*" {
		i++
	}
	_, inContract := a.spec.Table(table)
	large := false
	if t, ok := a.spec.Table(table); ok {
		large = t.Large
	}

	for _, act := range splitTopLevel(toks[i:]) {
		if len(act) == 0 {
			continue
		}
		switch {
		case act[0].is("rename"):
			a.rename(table, inContract, act, line)
		case act[0].is("drop"):
			a.dropAction(table, act, line)
		case act[0].is("alter"):
			a.alterColumn(table, large, act, line)
		case act[0].is("add"):
			a.addAction(table, inContract, act, line)
		case act[0].is("set") && len(act) > 1 && act[1].is("schema") && inContract:
			a.add(SeverityConflict, "move_table_schema", line, table, "", "表被移到其他 schema："+joinTokens(act))
		case (act[0].is("detach") || act[0].is("attach")) && len(act) > 1 && act[1].is("partition") && inContract:
			a.add(SeverityInfo, "partition_change", line, table, "", "分区变更："+joinTokens(act))
		}
	}
}

func (a *analyzer) rename(table string, inContract bool, act []token, line int) {
	if len(act) < 2 {
		return
	}
	switch {
	case act[1].is("to"):
		if inContract && len(act) > 2 {
			a.add(SeverityConflict, "rename_table", line, table, "", fmt.Sprintf("表改名：%s → %s", table, act[2].name()))
		}
	case act[1].is("constraint"):
	default:
		j := 1
		if act[1].is("column") {
			j = 2
		}
		if j >= len(act) {
			return
		}
		old := act[j].name()
		newName := "?"
		if j+2 < len(act) && act[j+1].is("to") {
			newName = act[j+2].name()
		}
		if a.spec.HasColumn(table, old) {
			a.add(SeverityConflict, "rename_column", line, table, old, fmt.Sprintf("列改名：%s → %s", old, newName))
		}
	}
}

func (a *analyzer) dropAction(table string, act []token, line int) {
	if len(act) < 2 || act[1].is("constraint") {
		return
	}
	j := 1
	if act[1].is("column") {
		j = 2
	}
	j = skipWords(act, j, "if", "exists")
	if j >= len(act) {
		return
	}
	col := act[j].name()
	if a.spec.HasColumn(table, col) {
		a.add(SeverityConflict, "drop_column", line, table, col, "删除列："+col)
	}
}

func (a *analyzer) alterColumn(table string, large bool, act []token, line int) {
	if len(act) < 2 || act[1].is("constraint") {
		return
	}
	j := 1
	if act[1].is("column") {
		j = 2
	}
	if j >= len(act) {
		return
	}
	col := act[j].name()
	rest := act[j+1:]
	used := a.spec.HasColumn(table, col)
	switch {
	case len(rest) > 0 && (rest[0].is("type") || (len(rest) > 2 && rest[0].is("set") && rest[1].is("data") && rest[2].is("type"))):
		if used {
			a.add(SeverityConflict, "alter_column_type", line, table, col, "列类型变更："+joinTokens(rest))
		}
		if large {
			a.add(SeverityWarning, "lock_large_table", line, table, col, "大表改列类型会重写整表并长时间锁表，需要维护窗口")
		}
	case len(rest) >= 3 && rest[0].is("drop") && rest[1].is("not") && rest[2].is("null"):
		if used {
			a.add(SeverityWarning, "drop_not_null", line, table, col, "列变为可空，本系统按非空读取时可能出错")
		}
	case len(rest) >= 3 && rest[0].is("set") && rest[1].is("not") && rest[2].is("null"):
		if used {
			a.add(SeverityInfo, "set_not_null", line, table, col, "列变为非空（只读使用不受影响）")
		}
	}
}

func (a *analyzer) addAction(table string, inContract bool, act []token, line int) {
	if len(act) < 2 {
		return
	}
	switch {
	case act[1].is("constraint"):
		if len(act) > 3 && act[3].is("check") {
			a.checkEnum(table, act[3:], line)
		}
	case act[1].is("check"):
		a.checkEnum(table, act[1:], line)
	case act[1].is("primary", "unique", "foreign", "exclude"):
	default:
		if !inContract {
			return
		}
		j := 1
		if act[1].is("column") {
			j = 2
		}
		j = skipWords(act, j, "if", "not", "exists")
		if j < len(act) {
			a.add(SeverityInfo, "add_column", line, table, act[j].name(), "新增列："+act[j].name())
		}
	}
}

// checkEnum 识别 CHECK (col IN ('a','b')) 与 CHECK (col = ANY (ARRAY['a','b'])) 两种写法，
// 与契约中该列依赖的枚举值比较。
func (a *analyzer) checkEnum(table string, toks []token, line int) {
	for i := 0; i < len(toks); i++ {
		if toks[i].kind != tokIdent && toks[i].kind != tokQuoted {
			continue
		}
		col := toks[i].name()
		expected, ok := a.spec.Enums[table+"."+col]
		if !ok {
			continue
		}
		j := i + 1
		// 跳过包裹列名的右括号与类型转换，如 (platform)::text
		for j < len(toks) {
			if toks[j].kind == tokPunct && toks[j].text == ")" {
				j++
				continue
			}
			if j+2 < len(toks) && toks[j].kind == tokPunct && toks[j].text == ":" && toks[j+1].kind == tokPunct && toks[j+1].text == ":" {
				j += 3
				continue
			}
			break
		}
		var values []string
		switch {
		case j < len(toks) && toks[j].is("in"):
			values = collectStrings(toks, j+1)
		case j+1 < len(toks) && toks[j].kind == tokPunct && toks[j].text == "=" && toks[j+1].is("any"):
			values = collectStrings(toks, j+2)
		default:
			continue
		}
		if len(values) == 0 {
			continue
		}
		a.compareEnum(table, col, expected, values, line)
		return
	}
}

// collectStrings 收集从 toks[i] 开始的第一个括号组内的所有字符串字面量。
func collectStrings(toks []token, i int) []string {
	depth := 0
	var out []string
	for ; i < len(toks); i++ {
		t := toks[i]
		if t.kind == tokPunct {
			switch t.text {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
				if depth <= 0 {
					return out
				}
			}
			continue
		}
		if t.kind == tokString && depth > 0 {
			out = append(out, t.text)
		}
	}
	return out
}

func (a *analyzer) compareEnum(table, col string, expected, actual []string, line int) {
	have := make(map[string]bool, len(actual))
	for _, v := range actual {
		have[v] = true
	}
	want := make(map[string]bool, len(expected))
	for _, v := range expected {
		want[v] = true
	}
	var removed, added []string
	for _, v := range expected {
		if !have[v] {
			removed = append(removed, v)
		}
	}
	for _, v := range actual {
		if !want[v] {
			added = append(added, v)
		}
	}
	sort.Strings(added)
	if len(removed) > 0 {
		a.add(SeverityConflict, "enum_value_removed", line, table, col, "约束删除了本系统依赖的取值："+strings.Join(removed, ", "))
	}
	if len(added) > 0 {
		a.add(SeverityInfo, "enum_value_added", line, table, col, "约束新增取值："+strings.Join(added, ", "))
	}
}

func (a *analyzer) dropTable(toks []token, line int) {
	i := skipWords(toks, 2, "if", "exists")
	for _, part := range splitTopLevel(toks[i:]) {
		name, _ := readQualifiedName(part, 0)
		if _, ok := a.spec.Table(name); ok {
			a.add(SeverityConflict, "drop_table", line, name, "", "删除表："+name)
		}
	}
}

func (a *analyzer) truncate(toks []token, line int) {
	i := skipWords(toks, 1, "table")
	i = skipWords(toks, i, "only")
	for _, part := range splitTopLevel(toks[i:]) {
		name, _ := readQualifiedName(part, 0)
		if _, ok := a.spec.Table(name); ok {
			a.add(SeverityWarning, "truncate_table", line, name, "", "清空表："+name)
		}
	}
}

func (a *analyzer) create(toks []token, line int) {
	i := 1
	i = skipWords(toks, i, "or", "replace")
	i = skipWords(toks, i, "unique")
	switch {
	case i < len(toks) && toks[i].is("index"):
		i++
		concurrently := false
		if i < len(toks) && toks[i].is("concurrently") {
			concurrently = true
			i++
		}
		for i < len(toks) && !toks[i].is("on") {
			i++
		}
		i = skipWords(toks, i, "on")
		i = skipWords(toks, i, "only")
		table, _ := readQualifiedName(toks, i)
		if t, ok := a.spec.Table(table); ok && t.Large && !concurrently {
			a.add(SeverityWarning, "lock_large_table", line, table, "", "在大表上非 CONCURRENTLY 建索引会阻塞写入，需要维护窗口")
		}
	case i < len(toks) && (toks[i].is("function") || toks[i].is("procedure")):
		a.proceduralBody(toks, line)
	}
}

func (a *analyzer) dml(toks []token, i int, line int, verb string) {
	i = skipWords(toks, i, "only")
	table, _ := readQualifiedName(toks, i)
	if t, ok := a.spec.Table(table); ok && t.Large {
		a.add(SeverityWarning, "long_running_dml", line, table, "", verb+" 大表可能长时间运行并占用锁，需要维护窗口")
	}
}

var proceduralDanger = regexp.MustCompile(`(?i)\b(alter\s+table|drop\s+(table|column)|rename|truncate|update|delete\s+from)\b`)

// proceduralBody 无法完整解析 PL/pgSQL，只在函数体或 DO 块同时提到契约表与变更动作时提示人工确认。
func (a *analyzer) proceduralBody(toks []token, line int) {
	for _, t := range toks {
		if t.kind != tokDollar {
			continue
		}
		body := strings.ToLower(t.text)
		if !proceduralDanger.MatchString(body) {
			continue
		}
		var hit []string
		for table := range a.spec.Tables {
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(table) + `\b`).MatchString(body) {
				hit = append(hit, table)
			}
		}
		sort.Strings(hit)
		for _, table := range hit {
			a.add(SeverityWarning, "procedural_manual_review", line, table, "", "PL/pgSQL 代码块涉及该表的变更，需人工确认")
		}
	}
}
