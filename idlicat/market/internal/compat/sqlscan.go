package compat

import (
	"strings"
	"unicode"
)

// Statement 是迁移脚本中的一条 SQL 语句。Text 已去掉注释，Line 是语句起始行（从 1 开始）。
type Statement struct {
	Text string
	Line int
}

// SplitStatements 按顶层分号切分 SQL，正确跳过单引号字符串、双引号标识符、
// 行注释、可嵌套的块注释和 $tag$ 美元引号块（PL/pgSQL 函数体、DO 块）。
func SplitStatements(src string) []Statement {
	var (
		out       []Statement
		buf       strings.Builder
		line      = 1
		stmtLine  = 0
		runes     = []rune(src)
		n         = len(runes)
		hasTokens bool
	)
	emit := func() {
		text := strings.TrimSpace(buf.String())
		if hasTokens && text != "" {
			out = append(out, Statement{Text: text, Line: stmtLine})
		}
		buf.Reset()
		hasTokens = false
		stmtLine = 0
	}
	mark := func() {
		if !hasTokens {
			hasTokens = true
			stmtLine = line
		}
	}

	for i := 0; i < n; i++ {
		c := runes[i]
		switch {
		case c == '\n':
			line++
			buf.WriteRune(c)

		case c == '-' && i+1 < n && runes[i+1] == '-':
			// 行注释：跳到行尾，换行留给下一轮处理
			for i+1 < n && runes[i+1] != '\n' {
				i++
			}
			buf.WriteRune(' ')

		case c == '/' && i+1 < n && runes[i+1] == '*':
			depth := 1
			i++
			for i+1 < n && depth > 0 {
				i++
				switch {
				case runes[i] == '\n':
					line++
				case runes[i] == '/' && i+1 < n && runes[i+1] == '*':
					depth++
					i++
				case runes[i] == '*' && i+1 < n && runes[i+1] == '/':
					depth--
					i++
				}
			}
			buf.WriteRune(' ')

		case c == '\'':
			mark()
			escapes := i > 0 && (runes[i-1] == 'E' || runes[i-1] == 'e') && (i < 2 || !isIdentRune(runes[i-2]))
			buf.WriteRune(c)
			for i+1 < n {
				i++
				buf.WriteRune(runes[i])
				if runes[i] == '\n' {
					line++
				}
				if escapes && runes[i] == '\\' && i+1 < n {
					i++
					buf.WriteRune(runes[i])
					continue
				}
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i++
						buf.WriteRune(runes[i])
						continue
					}
					break
				}
			}

		case c == '"':
			mark()
			buf.WriteRune(c)
			for i+1 < n {
				i++
				buf.WriteRune(runes[i])
				if runes[i] == '\n' {
					line++
				}
				if runes[i] == '"' {
					if i+1 < n && runes[i+1] == '"' {
						i++
						buf.WriteRune(runes[i])
						continue
					}
					break
				}
			}

		case c == '$':
			tag, ok := dollarTag(runes, i)
			if !ok {
				mark()
				buf.WriteRune(c)
				continue
			}
			mark()
			buf.WriteString(tag)
			i += len([]rune(tag)) - 1
			closing := []rune(tag)
			for i+1 < n {
				i++
				if runes[i] == '\n' {
					line++
				}
				if runes[i] == '$' && hasPrefixAt(runes, i, closing) {
					buf.WriteString(tag)
					i += len(closing) - 1
					break
				}
				buf.WriteRune(runes[i])
			}

		case c == ';':
			emit()

		default:
			if !unicode.IsSpace(c) {
				mark()
			}
			buf.WriteRune(c)
		}
	}
	emit()
	return out
}

// dollarTag 识别 $$ 或 $tag$；$1 这样的位置参数不是美元引号。
func dollarTag(runes []rune, i int) (string, bool) {
	j := i + 1
	if j < len(runes) && runes[j] == '$' {
		return "$$", true
	}
	if j >= len(runes) || (!unicode.IsLetter(runes[j]) && runes[j] != '_') {
		return "", false
	}
	for j < len(runes) && isIdentRune(runes[j]) {
		j++
	}
	if j < len(runes) && runes[j] == '$' {
		return string(runes[i : j+1]), true
	}
	return "", false
}

func hasPrefixAt(runes []rune, i int, prefix []rune) bool {
	if i+len(prefix) > len(runes) {
		return false
	}
	for k, r := range prefix {
		if runes[i+k] != r {
			return false
		}
	}
	return true
}

func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

type tokKind int

const (
	tokIdent  tokKind = iota // 未加引号的标识符或关键字，已转小写
	tokQuoted                // 双引号标识符，保留大小写
	tokString                // 单引号字符串，Text 为去掉引号后的内容
	tokDollar                // 美元引号块，Text 为块内内容
	tokNumber
	tokPunct
)

type token struct {
	kind tokKind
	text string
}

func (t token) is(words ...string) bool {
	if t.kind != tokIdent {
		return false
	}
	for _, w := range words {
		if t.text == w {
			return true
		}
	}
	return false
}

// name 返回标识符的比较用名称：未加引号的已是小写，加引号的原样返回。
func (t token) name() string { return t.text }

func tokenize(stmt string) []token {
	runes := []rune(stmt)
	n := len(runes)
	var out []token
	for i := 0; i < n; i++ {
		c := runes[i]
		switch {
		case unicode.IsSpace(c):
		case c == '\'':
			s, end := readString(runes, i, false)
			out = append(out, token{tokString, s})
			i = end
		case (c == 'e' || c == 'E') && i+1 < n && runes[i+1] == '\'' && (i == 0 || !isIdentRune(runes[i-1])):
			// E'...' 转义字符串：反斜杠是转义符
			s, end := readString(runes, i+1, true)
			out = append(out, token{tokString, s})
			i = end
		case c == '"':
			var b strings.Builder
			for i+1 < n {
				i++
				if runes[i] == '"' {
					if i+1 < n && runes[i+1] == '"' {
						b.WriteRune('"')
						i++
						continue
					}
					break
				}
				b.WriteRune(runes[i])
			}
			out = append(out, token{tokQuoted, b.String()})
		case c == '$':
			if tag, ok := dollarTag(runes, i); ok {
				tr := []rune(tag)
				start := i + len(tr)
				end := start
				for end < n && (runes[end] != '$' || !hasPrefixAt(runes, end, tr)) {
					end++
				}
				out = append(out, token{tokDollar, string(runes[start:min(end, n)])})
				i = end + len(tr) - 1
				continue
			}
			j := i + 1
			for j < n && unicode.IsDigit(runes[j]) {
				j++
			}
			out = append(out, token{tokPunct, string(runes[i:j])})
			i = j - 1
		case unicode.IsLetter(c) || c == '_':
			j := i
			for j < n && (isIdentRune(runes[j]) || runes[j] == '$') {
				j++
			}
			out = append(out, token{tokIdent, strings.ToLower(string(runes[i:j]))})
			i = j - 1
		case unicode.IsDigit(c):
			j := i
			for j < n && (unicode.IsDigit(runes[j]) || runes[j] == '.') {
				j++
			}
			out = append(out, token{tokNumber, string(runes[i:j])})
			i = j - 1
		default:
			out = append(out, token{tokPunct, string(c)})
		}
	}
	return out
}

// readString 从 runes[i]（开头的单引号）读取字符串字面量，返回内容与结束引号的位置。
// escapes 为 true 时按 E” 字符串处理反斜杠转义。
func readString(runes []rune, i int, escapes bool) (string, int) {
	var b strings.Builder
	n := len(runes)
	for i+1 < n {
		i++
		if escapes && runes[i] == '\\' && i+1 < n {
			i++
			b.WriteRune(runes[i])
			continue
		}
		if runes[i] == '\'' {
			if i+1 < n && runes[i+1] == '\'' {
				b.WriteRune('\'')
				i++
				continue
			}
			return b.String(), i
		}
		b.WriteRune(runes[i])
	}
	return b.String(), i
}

// splitTopLevel 按括号外的逗号切分 token 序列（ALTER TABLE 的多个子句）。
func splitTopLevel(toks []token) [][]token {
	var (
		out   [][]token
		cur   []token
		depth int
	)
	for _, t := range toks {
		if t.kind == tokPunct {
			switch t.text {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			case ",":
				if depth == 0 {
					out = append(out, cur)
					cur = nil
					continue
				}
			}
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// readQualifiedName 从 toks[i] 读取 schema.table 形式的名称，返回最后一段与下一个位置。
func readQualifiedName(toks []token, i int) (string, int) {
	if i >= len(toks) || (toks[i].kind != tokIdent && toks[i].kind != tokQuoted) {
		return "", i
	}
	name := toks[i].name()
	i++
	for i+1 < len(toks) && toks[i].kind == tokPunct && toks[i].text == "." &&
		(toks[i+1].kind == tokIdent || toks[i+1].kind == tokQuoted) {
		name = toks[i+1].name()
		i += 2
	}
	return name, i
}

// skipWords 依次跳过可选的关键字序列（如 IF EXISTS），返回新位置。
func skipWords(toks []token, i int, words ...string) int {
	j := i
	for _, w := range words {
		if j >= len(toks) || !toks[j].is(w) {
			return i
		}
		j++
	}
	return j
}

func joinTokens(toks []token) string {
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		switch t.kind {
		case tokString:
			parts = append(parts, "'"+t.text+"'")
		case tokQuoted:
			parts = append(parts, `"`+t.text+`"`)
		case tokDollar:
			parts = append(parts, "$$…$$")
		default:
			parts = append(parts, t.text)
		}
	}
	return strings.Join(parts, " ")
}
