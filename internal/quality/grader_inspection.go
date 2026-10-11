package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// GraderOptions 是新质检题的规则；匿名嵌入保持既有 grader JSON 结构兼容。
type GraderOptions struct {
	Tag               string             `json:"tag,omitempty"`
	Norm              string             `json:"norm,omitempty"`
	Fields            map[string]string  `json:"fields,omitempty"`
	Inline            bool               `json:"inline,omitempty"`
	Items             []string           `json:"items,omitempty"`
	Sep               string             `json:"sep,omitempty"`
	Lines             [][]string         `json:"lines,omitempty"`
	StripPrefix       []string           `json:"strip_prefix,omitempty"`
	Checks            []Constraint       `json:"checks,omitempty"`
	Schema            json.RawMessage    `json:"schema,omitempty"`
	NoFence           bool               `json:"no_fence,omitempty"`
	Extra             []Constraint       `json:"extra,omitempty"`
	Expect            []ExpectedToolCall `json:"expect,omitempty"`
	Match             string             `json:"match,omitempty"`
	Tol               map[string]float64 `json:"tol,omitempty"`
	AllowExtraCalls   bool               `json:"allow_extra_calls,omitempty"`
	TextRegex         string             `json:"text_regex,omitempty"`
	Patterns          []string           `json:"patterns,omitempty"`
	NegativePatterns  []string           `json:"negative_patterns,omitempty"`
	Fence             string             `json:"fence,omitempty"`
	SHA256            string             `json:"sha256,omitempty"`
	MinLines          int                `json:"min_lines,omitempty"`
	Counts            map[string]int     `json:"counts,omitempty"`
	Lazy              string             `json:"lazy,omitempty"`
	RequireNormalStop bool               `json:"require_normal_stop,omitempty"`
	Numbers           []int64            `json:"numbers"`
	Count             *int               `json:"count,omitempty"`
	Sum               *int64             `json:"sum,omitempty"`
	Outcome           string             `json:"outcome,omitempty"`
	HTTPStatus        []int              `json:"http_status,omitempty"`
	ErrorCodes        []string           `json:"error_codes,omitempty"`
}

type ExpectedToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func (call *ExpectedToolCall) UnmarshalJSON(raw []byte) error {
	type fields ExpectedToolCall
	var decoded fields
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*call = ExpectedToolCall(decoded)
	return nil
}

// GradeResult 的部分分只用于展示；严格通过才进入二元统计。
type GradeResult struct {
	Pass    bool     `json:"pass"`
	Partial *float64 `json:"partial"`
	Flags   []string `json:"flags"`
}

var inspectionGraderTypes = map[string]bool{
	"answer_tag": true, "fields_exact": true, "set_exact": true, "lines_alias": true,
	"constraints": true, "json_schema": true, "tool_calls": true, "abstain": true,
	"full_output": true, "num_list": true, "accept_reject": true,
}

// gradeResult 在新规则上保留格式、截断等证据，旧题继续沿用原来的六种判法。
func gradeResult(g Grader, r ProbeResult, needle string) (GradeResult, error) {
	if inspectionGraderTypes[g.Type] {
		return gradeInspection(g, r, needle, false)
	}
	pass, err := grade(g, r.Text, needle)
	return GradeResult{Pass: pass, Flags: slices.Clone(r.Flags)}, err
}

func gradeInspection(g Grader, r ProbeResult, needle string, validateOnly bool) (GradeResult, error) {
	res := GradeResult{Flags: []string{}}
	for _, flag := range r.Flags {
		addGradeFlag(&res, flag)
	}
	if err := validateInspectionGrader(g); err != nil {
		return res, err
	}
	if validateOnly {
		return res, nil
	}
	if g.Type != "accept_reject" && (r.ErrorClass != "" || r.HTTPStatus < 200 || r.HTTPStatus >= 300 ||
		slices.Contains(res.Flags, "platform") || slices.Contains(res.Flags, "rate_limited") ||
		slices.Contains(res.Flags, "truncated") || slices.Contains(res.Flags, "refusal") || slices.Contains(res.Flags, "stop_error")) {
		return res, nil
	}
	text := strings.TrimSpace(stripCodeFence(strings.ReplaceAll(r.Text, "\r\n", "\n")))
	answer := strings.ReplaceAll(g.Answer, "{{NEEDLE}}", needle)
	switch g.Type {
	case "answer_tag":
		tag := g.Tag
		if tag == "" {
			tag = "ANSWER"
		}
		re := regexp.MustCompile(`(?im)^[\t ]*` + regexp.QuoteMeta(tag) + `[\t ]*[:：][\t ]*(\S[^\r\n]*)[\t ]*$`)
		matches := re.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			addGradeFlag(&res, "format_error")
			break
		}
		actual, actualOK := normalizeTaggedAnswer(matches[len(matches)-1][1], g.Norm)
		expected, _ := normalizeTaggedAnswer(answer, g.Norm)
		res.Pass = actualOK && actual == expected
		if !actualOK {
			addGradeFlag(&res, "format_error")
		}
	case "fields_exact":
		actual := parseFields(text, g.Inline)
		correct := 0
		for key, expected := range g.Fields {
			value, found := actual[compactAnswer(key)]
			if found && compactAnswer(value) == compactAnswer(strings.ReplaceAll(expected, "{{NEEDLE}}", needle)) {
				correct++
			}
			if !found {
				addGradeFlag(&res, "format_error")
			}
		}
		setPartial(&res, correct, len(g.Fields))
		res.Pass = correct == len(g.Fields)
	case "set_exact":
		sep := g.Sep
		if sep == "" {
			sep = `[\s,，、;；]+`
		}
		actual := stringSet(regexp.MustCompile(sep).Split(text, -1), g.CaseInsensitive)
		expected := stringSet(g.Items, g.CaseInsensitive)
		correct := 0
		for v := range actual {
			if expected[v] {
				correct++
			}
		}
		setPartial(&res, correct, len(actual)+len(expected)-correct)
		res.Pass = correct == len(expected) && len(actual) == len(expected)
	case "lines_alias":
		lines := nonemptyLines(text)
		correct := 0
		for i, aliases := range g.Lines {
			if i >= len(lines) {
				continue
			}
			line := regexp.MustCompile(`^\s*\d+[.、．)）]\s*`).ReplaceAllString(lines[i], "")
			for _, prefix := range g.StripPrefix {
				line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
			for _, alias := range aliases {
				if compactAnswer(line) == compactAnswer(alias) {
					correct++
					break
				}
			}
		}
		setPartial(&res, correct, max(len(lines), len(g.Lines)))
		res.Pass = correct == len(g.Lines) && len(lines) == len(g.Lines)
		if len(lines) != len(g.Lines) {
			addGradeFlag(&res, "format_error")
		}
	case "constraints":
		correct := 0
		for _, c := range g.Checks {
			if checkConstraint(text, c) {
				correct++
			}
		}
		setPartial(&res, correct, len(g.Checks))
		res.Pass = correct == len(g.Checks)
	case "json_schema":
		res.Pass = gradeJSONSchema(g, r.Text)
		if !res.Pass {
			addGradeFlag(&res, "format_error")
		}
	case "tool_calls":
		res.Pass, res.Partial = gradeToolCalls(g, r.ToolCalls, text)
	case "abstain":
		res.Pass = anyPattern(g.Patterns, text) && !anyPattern(g.NegativePatterns, text)
	case "full_output":
		content, valid := fullOutputText(r.Text, g.Fence)
		if !valid {
			addGradeFlag(&res, "format_error")
			break
		}
		if lazyOutput(content, g.Lazy) {
			addGradeFlag(&res, "lazy_placeholder")
			break
		}
		if g.RequireNormalStop && !slices.Contains(res.Flags, "stop_normal") {
			break
		}
		res.Pass = len(strings.Split(content, "\n")) >= g.MinLines
		for pattern, count := range g.Counts {
			res.Pass = res.Pass && len(regexp.MustCompile(pattern).FindAllStringIndex(content, -1)) == count
		}
		res.Pass = res.Pass && (g.SHA256 == "" || hashAnswer(content) == strings.ToLower(g.SHA256))
	case "num_list":
		if lazyOutput(text, "list") {
			addGradeFlag(&res, "lazy_placeholder")
			break
		}
		nums, ok := parseNumberList(text)
		if !ok {
			addGradeFlag(&res, "format_error")
			break
		}
		res.Pass = true
		if g.Count != nil {
			res.Pass = len(nums) == *g.Count
		}
		if g.Numbers != nil {
			correct := 0
			for i, expected := range g.Numbers {
				if i < len(nums) && nums[i] == expected {
					correct++
				}
			}
			setPartial(&res, correct, max(len(nums), len(g.Numbers)))
			res.Pass = res.Pass && correct == len(g.Numbers) && len(nums) == len(g.Numbers)
		}
		var sum int64
		canonical := make([]string, len(nums))
		for i, n := range nums {
			if n > 0 && sum > math.MaxInt64-n || n < 0 && sum < math.MinInt64-n {
				res.Pass = false
				break
			}
			sum += n
			canonical[i] = strconv.FormatInt(n, 10)
		}
		res.Pass = res.Pass && (g.Sum == nil || *g.Sum == sum)
		// 数列哈希以十进制整数逗号连接，避免换行或间隔差异影响判分。
		res.Pass = res.Pass && (g.SHA256 == "" || hashAnswer(strings.Join(canonical, ",")) == strings.ToLower(g.SHA256))
	case "accept_reject":
		if r.ErrorClass == "platform" || r.ErrorClass == "rate_limited" || slices.Contains(res.Flags, "platform") || slices.Contains(res.Flags, "rate_limited") {
			break
		}
		if g.Outcome == "reject" {
			res.Pass = slices.Contains(g.HTTPStatus, r.HTTPStatus) && slices.Contains(g.ErrorCodes, r.ErrorCode) &&
				(r.ErrorClass == "expected_reject" || r.ErrorClass == "http_4xx")
			if res.Pass {
				addGradeFlag(&res, "expected_reject")
			}
		} else {
			res.Pass = r.ErrorClass == "" && r.HTTPStatus >= 200 && r.HTTPStatus < 300 &&
				(len(g.HTTPStatus) == 0 || slices.Contains(g.HTTPStatus, r.HTTPStatus))
		}
	}
	return res, nil
}

func validateInspectionGrader(g Grader) error {
	if !inspectionGraderTypes[g.Type] {
		return fmt.Errorf("未知质检判分器：%s", g.Type)
	}
	compile := func(pattern string) error {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("判分器正则无效：%w", err)
		}
		return nil
	}
	switch g.Type {
	case "answer_tag":
		if g.Answer == "" || strings.ContainsAny(g.Tag, "\r\n") {
			return errors.New("answer_tag 需要答案及单行标记")
		}
		if g.Norm != "" && g.Norm != "str" && g.Norm != "ci" && g.Norm != "int" {
			return errors.New("answer_tag 的 norm 只能是 str、ci 或 int")
		}
		if !strings.Contains(g.Answer, "{{NEEDLE}}") {
			if _, ok := normalizeTaggedAnswer(g.Answer, g.Norm); !ok {
				return errors.New("answer_tag 的答案无法按指定方式规范化")
			}
		}
	case "fields_exact":
		if len(g.Fields) == 0 {
			return errors.New("fields_exact 需要字段答案")
		}
		seen := map[string]bool{}
		for key := range g.Fields {
			norm := compactAnswer(key)
			if norm == "" || seen[norm] || strings.ContainsAny(key, ":：=＝\n\r") {
				return errors.New("fields_exact 的字段名为空、重复或包含分隔符")
			}
			seen[norm] = true
		}
	case "set_exact":
		if len(g.Items) == 0 || len(stringSet(g.Items, g.CaseInsensitive)) != len(g.Items) {
			return errors.New("set_exact 需要非空且不重复的集合")
		}
		if g.Sep != "" {
			if err := compile(g.Sep); err != nil {
				return err
			}
			if regexp.MustCompile(g.Sep).MatchString("") {
				return errors.New("set_exact 分隔符不能匹配空串")
			}
		}
	case "lines_alias":
		if len(g.Lines) == 0 {
			return errors.New("lines_alias 需要逐行接受集合")
		}
		for _, line := range g.Lines {
			if len(line) == 0 {
				return errors.New("lines_alias 的每一行需要至少一个别名")
			}
		}
	case "constraints", "json_schema":
		checks := g.Checks
		if g.Type == "constraints" && len(checks) == 0 {
			return errors.New("constraints 需要至少一个约束")
		}
		if g.Type == "json_schema" {
			if _, err := compileJSONSchema(g.Schema); err != nil {
				return err
			}
			checks = g.Extra
		}
		for _, c := range checks {
			if err := validateConstraint(c); err != nil {
				return err
			}
		}
	case "tool_calls":
		if g.Match != "" && g.Match != "set" && g.Match != "sequence" {
			return errors.New("tool_calls 的 match 只能是 set 或 sequence")
		}
		for _, c := range g.Expect {
			if strings.TrimSpace(c.Name) == "" {
				return errors.New("tool_calls 的期望工具名不能为空")
			}
		}
		for _, tol := range g.Tol {
			if tol < 0 || math.IsNaN(tol) || math.IsInf(tol, 0) {
				return errors.New("tool_calls 的数值容差必须有限且非负")
			}
		}
		if g.TextRegex != "" {
			return compile(g.TextRegex)
		}
	case "abstain":
		if len(g.Patterns) == 0 {
			return errors.New("abstain 需要弃权或纠错正则")
		}
		for _, pattern := range append(slices.Clone(g.Patterns), g.NegativePatterns...) {
			if pattern == "" {
				return errors.New("abstain 的正则不能为空")
			}
			if err := compile(pattern); err != nil {
				return err
			}
		}
	case "full_output", "num_list":
		if g.SHA256 != "" {
			b, err := hex.DecodeString(g.SHA256)
			if err != nil || len(b) != sha256.Size {
				return errors.New("输出规则的 sha256 必须是 64 位十六进制值")
			}
		}
		if g.Type == "num_list" {
			if g.Numbers == nil && g.SHA256 == "" {
				return errors.New("num_list 需要期望数列或哈希，不能仅按个数与总和判断")
			}
			if g.Count != nil && *g.Count < 0 {
				return errors.New("num_list 的个数不能为负")
			}
			break
		}
		if g.MinLines < 0 || strings.ContainsAny(g.Fence, "\r\n`") {
			return errors.New("full_output 的行数或围栏语言无效")
		}
		if g.SHA256 == "" && g.MinLines == 0 && len(g.Counts) == 0 {
			return errors.New("full_output 需要哈希、行数或计数约束")
		}
		if g.Lazy != "" && g.Lazy != "code" && g.Lazy != "list" {
			return errors.New("full_output 的 lazy 只能是 code 或 list")
		}
		for pattern, count := range g.Counts {
			if count < 0 {
				return errors.New("full_output 的计数不能为负")
			}
			if err := compile(pattern); err != nil {
				return err
			}
		}
	case "accept_reject":
		if g.Outcome != "" && g.Outcome != "accept" && g.Outcome != "reject" {
			return errors.New("accept_reject 的 outcome 只能是 accept 或 reject")
		}
		if g.Outcome == "reject" && (len(g.HTTPStatus) == 0 || len(g.ErrorCodes) == 0) {
			return errors.New("预期拒绝必须指定状态码与业务错误码")
		}
		for _, status := range g.HTTPStatus {
			if g.Outcome == "reject" && (status < 400 || status >= 500 || status == 429) ||
				g.Outcome != "reject" && (status < 200 || status >= 300) {
				return errors.New("accept_reject 的预期状态码与接受或拒绝模式不符")
			}
		}
		for _, code := range g.ErrorCodes {
			if strings.TrimSpace(code) == "" {
				return errors.New("预期错误业务码不能为空")
			}
		}
	}
	return nil
}

func normalizeTaggedAnswer(text, norm string) (string, bool) {
	text = strings.TrimSpace(text)
	switch norm {
	case "int":
		// 千分位只接受三位分组，防止把 1,2 或小数误当作整数。
		if !regexp.MustCompile(`^[+-]?(?:\d+|\d{1,3}(?:,\d{3})+)$`).MatchString(text) {
			return "", false
		}
		v, err := strconv.ParseInt(strings.ReplaceAll(text, ",", ""), 10, 64)
		return strconv.FormatInt(v, 10), err == nil
	case "ci":
		return strings.ToLower(text), true
	default:
		return text, true
	}
}

func compactAnswer(text string) string {
	text = strings.NewReplacer("，", ",", "、", ",", "：", ":", "；", ";", "＝", "=").Replace(text)
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text))
}

func parseFields(text string, inline bool) map[string]string {
	result := map[string]string{}
	var parts []string
	if inline {
		parts = regexp.MustCompile(`[\n,，;；]+`).Split(text, -1)
	} else {
		parts = strings.Split(text, "\n")
	}
	seen := map[string]bool{}
	for _, part := range parts {
		at := strings.IndexAny(part, ":：=＝")
		if at < 0 {
			continue
		}
		_, size := utf8RuneAt(part[at:])
		key := compactAnswer(part[:at])
		if seen[key] {
			// 重复字段存在相互矛盾的风险，不采用最后一个覆盖前一个。
			delete(result, key)
			continue
		}
		seen[key] = true
		result[key] = strings.TrimSpace(part[at+size:])
	}
	return result
}

func utf8RuneAt(text string) (rune, int) {
	for _, r := range text {
		return r, len(string(r))
	}
	return 0, 0
}

func stringSet(values []string, ci bool) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if ci {
			value = strings.ToLower(value)
		}
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func nonemptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func setPartial(res *GradeResult, correct, total int) {
	value := 0.0
	if total > 0 {
		value = float64(correct) / float64(total)
	}
	res.Partial = &value
}

func addGradeFlag(res *GradeResult, flag string) {
	if !slices.Contains(res.Flags, flag) {
		res.Flags = append(res.Flags, flag)
	}
}

func anyPattern(patterns []string, text string) bool {
	for _, pattern := range patterns {
		if regexp.MustCompile(pattern).MatchString(text) {
			return true
		}
	}
	return false
}

func hashAnswer(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func fullOutputText(text, fence string) (string, bool) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if !strings.Contains(text, "```") {
		return text, text != ""
	}
	if strings.Count(text, "```") != 2 {
		return "", false
	}
	start := strings.Index(text, "```") + 3
	newline := strings.IndexByte(text[start:], '\n')
	if newline < 0 {
		return "", false
	}
	language := strings.TrimSpace(text[start : start+newline])
	if fence != "" && language != "" && language != fence {
		return "", false
	}
	content := text[start+newline+1 : strings.LastIndex(text, "```")]
	return strings.TrimSpace(content), true
}

var lazyCodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^\s*(?:#|//|/\*|--|<!--)?\s*(?:\.\.\.|…)\s*(?:\*/|-->)?\s*$`),
	regexp.MustCompile(`(?im)^\s*(?:#|//|/\*|--|<!--).*?(?:\.{2,}|\b(?:rest of|remaining|other|existing|previous)\b.{0,30}\b(?:code|file|functions?|methods?|implementation|logic)\b|\b(?:unchanged|omitted|for brevity|TODO|FIXME)\b|same as (?:before|above)|implement (?:this|here)|为简洁起见|省略|其余.*不变|同上|此处略|下同|略去)`),
	regexp.MustCompile(`(?im)^\s*(?:pass\s*#.*(?:TODO|省略)|(?:unchanged|omitted|for brevity|same as before|为简洁起见|省略|同上|此处略|下同|略去))\s*$`),
}

func lazyOutput(text, context string) bool {
	if context == "list" {
		return regexp.MustCompile(`(?i)\.\.\.|…|\betc\b|\band so on\b|等等|[以依]此类推`).MatchString(text)
	}
	if context == "code" {
		for _, re := range lazyCodePatterns {
			if re.MatchString(text) {
				return true
			}
		}
	}
	return false
}

func parseNumberList(text string) ([]int64, bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	if text == "" {
		return []int64{}, true
	}
	parts := regexp.MustCompile(`[\s,，;；、]+`).Split(text, -1)
	numbers := make([]int64, len(parts))
	for i, part := range parts {
		if !regexp.MustCompile(`^[+-]?\d+$`).MatchString(part) {
			return nil, false
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, false
		}
		numbers[i] = n
	}
	return numbers, true
}
