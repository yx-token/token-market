package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Constraint 共用于指令遵循及 JSON 字段的附加规则，不执行题目中的代码。
type Constraint struct {
	Kind      string            `json:"kind"`
	Path      string            `json:"path,omitempty"`
	Min       *int              `json:"min,omitempty"`
	Max       *int              `json:"max,omitempty"`
	N         *int              `json:"n,omitempty"`
	Word      string            `json:"word,omitempty"`
	Text      string            `json:"text,omitempty"`
	Chars     string            `json:"chars,omitempty"`
	Sep       string            `json:"sep,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Initials  []string          `json:"initials,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	ExactKeys bool              `json:"exact_keys,omitempty"`
	Language  string            `json:"language,omitempty"`
}

var (
	englishWords      = regexp.MustCompile(`[A-Za-z]+(?:'[A-Za-z]+)?`)
	blankParagraphs   = regexp.MustCompile(`\n\s*\n`)
	englishSentences  = regexp.MustCompile(`[.!?]+(?:\s+|$)`)
	chineseSentences  = regexp.MustCompile(`[。！？]+`)
	asciiAlphaNumeric = regexp.MustCompile(`[A-Za-z0-9]`)
)

func validateConstraint(c Constraint) error {
	for _, value := range []*int{c.Min, c.Max, c.N} {
		if value != nil && *value < 0 {
			return errors.New("约束的个数或边界不能为负")
		}
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return errors.New("约束的最小值不能超过最大值")
	}
	if c.Language != "" && c.Language != "en" && c.Language != "zh" {
		return errors.New("句子约束的语言只能是 en 或 zh")
	}
	switch c.Kind {
	case "word_count", "han_count", "sentence_words", "sentence_han_count":
		if c.Min == nil && c.Max == nil {
			return errors.New("字词数量约束需要 min 或 max")
		}
	case "keyword_count", "substr_count", "paragraphs", "bullets", "sentence_count":
		if c.N == nil {
			return errors.New("计数约束需要 n")
		}
		if c.Kind == "keyword_count" && (c.Word == "" || len(englishWords.FindAllString(c.Word, -1)) != 1 || englishWords.FindString(c.Word) != c.Word) {
			return errors.New("keyword_count 需要一个完整英文单词")
		}
		if c.Kind == "substr_count" && c.Text == "" {
			return errors.New("substr_count 的匹配文本不能为空")
		}
	case "forbid_chars":
		if c.Chars == "" {
			return errors.New("forbid_chars 的禁用字符不能为空")
		}
	case "para_initials":
		if len(c.Initials) == 0 {
			return errors.New("para_initials 需要段落前缀")
		}
		for _, initial := range c.Initials {
			if initial == "" {
				return errors.New("段落前缀不能为空")
			}
		}
	case "ends_with", "starts_with":
		if c.Text == "" {
			return errors.New("首尾约束的文本不能为空")
		}
	case "exact_text":
		if c.Text == "" {
			return errors.New("完整文本约束需要非空 text")
		}
	case "sentence_max_words", "max_word_repeat":
		if c.Max == nil {
			return errors.New("最大值约束需要 max")
		}
	case "all_lowercase", "no_ascii_alnum":
	case "json_schema_lite":
		if len(c.Fields) == 0 {
			return errors.New("json_schema_lite 需要字段类型")
		}
		for _, typ := range c.Fields {
			if typ == "int" || typ == "str" || typ == "list" {
				continue
			}
			if !strings.HasPrefix(typ, "list:") {
				return errors.New("json_schema_lite 的字段类型无效")
			}
			n, err := strconv.Atoi(strings.TrimPrefix(typ, "list:"))
			if err != nil || n < 0 {
				return errors.New("json_schema_lite 的数组长度无效")
			}
		}
	default:
		return fmt.Errorf("未知约束类型：%s", c.Kind)
	}
	return nil
}

func checkConstraint(text string, c Constraint) bool {
	inRange := func(n int) bool {
		return (c.Min == nil || n >= *c.Min) && (c.Max == nil || n <= *c.Max)
	}
	words := func(s string) []string { return englishWords.FindAllString(s, -1) }
	hanCount := func(s string) int {
		n := 0
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				n++
			}
		}
		return n
	}
	switch c.Kind {
	case "exact_text":
		return text == c.Text
	case "word_count":
		return inRange(len(words(text)))
	case "han_count":
		return inRange(hanCount(text))
	case "keyword_count":
		n := 0
		for _, word := range words(text) {
			if strings.EqualFold(word, c.Word) {
				n++
			}
		}
		return n == *c.N
	case "substr_count":
		return strings.Count(text, c.Text) == *c.N
	case "forbid_chars":
		return !strings.ContainsAny(text, c.Chars)
	case "paragraphs":
		return len(splitParagraphs(text, c.Sep)) == *c.N
	case "para_initials":
		paragraphs := splitParagraphs(text, c.Sep)
		if len(paragraphs) != len(c.Initials) {
			return false
		}
		for i, p := range paragraphs {
			if !strings.HasPrefix(strings.TrimLeft(p, "#*-•「“\"' "), c.Initials[i]) {
				return false
			}
		}
		return true
	case "ends_with":
		return strings.HasSuffix(strings.TrimSpace(text), c.Text)
	case "starts_with":
		return strings.HasPrefix(strings.TrimSpace(text), c.Text)
	case "all_lowercase":
		return strings.ToLower(text) == text
	case "sentence_count":
		return len(splitSentences(text, c.Language)) == *c.N
	case "sentence_max_words", "sentence_words", "sentence_han_count":
		sentences := splitSentences(text, c.Language)
		if len(sentences) == 0 {
			return false
		}
		for _, sentence := range sentences {
			n := len(words(sentence))
			if c.Kind == "sentence_han_count" {
				n = hanCount(sentence)
			}
			if c.Kind == "sentence_max_words" && n > *c.Max || c.Kind != "sentence_max_words" && !inRange(n) {
				return false
			}
		}
		return true
	case "no_ascii_alnum":
		return !asciiAlphaNumeric.MatchString(text)
	case "max_word_repeat":
		counts := map[string]int{}
		for _, word := range words(text) {
			word = strings.ToLower(word)
			counts[word]++
			if counts[word] > *c.Max {
				return false
			}
		}
		return true
	case "bullets":
		prefix := c.Prefix
		if prefix == "" {
			prefix = "- "
		}
		n := 0
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, prefix) {
				n++
			}
		}
		return n == *c.N
	case "json_schema_lite":
		value, err := strictJSON(stripCodeFence(text))
		if err != nil {
			return false
		}
		obj, ok := value.(map[string]any)
		if !ok || c.ExactKeys && len(obj) != len(c.Fields) {
			return false
		}
		for field, typ := range c.Fields {
			v, found := obj[field]
			if !found {
				return false
			}
			switch typ {
			case "str":
				if _, ok := v.(string); !ok {
					return false
				}
			case "int":
				n, ok := v.(json.Number)
				if !ok {
					return false
				}
				if _, err := n.Int64(); err != nil {
					return false
				}
			default:
				list, ok := v.([]any)
				if !ok {
					return false
				}
				if strings.HasPrefix(typ, "list:") {
					n, _ := strconv.Atoi(strings.TrimPrefix(typ, "list:"))
					if len(list) != n {
						return false
					}
				}
			}
		}
		return true
	}
	return false
}

func splitParagraphs(text, sep string) []string {
	var paragraphs []string
	if sep == "" || sep == "blank" {
		paragraphs = blankParagraphs.Split(strings.TrimSpace(text), -1)
	} else {
		paragraphs = strings.Split(strings.TrimSpace(text), sep)
	}
	return nonemptyParts(paragraphs)
}

func splitSentences(text, language string) []string {
	if language == "zh" || language == "" && strings.ContainsAny(text, "。！？") {
		return nonemptyParts(chineseSentences.Split(strings.TrimSpace(text), -1))
	}
	return nonemptyParts(englishSentences.Split(strings.TrimSpace(text), -1))
}

func nonemptyParts(parts []string) []string {
	var result []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

// 禁止 Schema 在判分期间读取外部资源；题库中的本地 $ref 仍可使用。
type localSchemaLoader struct{}

func (localSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("判分 Schema 不允许外部引用：%s", url)
}

func compileJSONSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 {
		return nil, errors.New("json_schema 需要 schema")
	}
	value, err := strictJSON(string(raw))
	if err != nil {
		return nil, fmt.Errorf("判分 Schema 不是有效 JSON：%w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(localSchemaLoader{})
	const location = "https://token-market.invalid/inspection-schema.json"
	if err := compiler.AddResource(location, value); err != nil {
		return nil, fmt.Errorf("判分 Schema 无效：%w", err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("判分 Schema 编译失败：%w", err)
	}
	return schema, nil
}

func gradeJSONSchema(g Grader, text string) bool {
	if g.NoFence && strings.Contains(text, "```") {
		return false
	}
	content, ok := fullOutputText(text, "json")
	if !ok {
		return false
	}
	value, err := strictJSON(content)
	if err != nil {
		return false
	}
	schema, err := compileJSONSchema(g.Schema)
	if err != nil || schema.Validate(value) != nil {
		return false
	}
	for _, constraint := range g.Extra {
		field, ok := jsonPath(value, constraint.Path)
		if !ok {
			return false
		}
		text, ok := field.(string)
		if !ok || !checkConstraint(text, constraint) {
			return false
		}
	}
	return true
}

func strictJSON(text string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("JSON 后存在多余内容")
	}
	return value, nil
}

func jsonPath(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	parts := strings.Split(path, ".")
	if strings.HasPrefix(path, "/") {
		parts = strings.Split(strings.TrimPrefix(path, "/"), "/")
		for i, part := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		}
	}
	for _, part := range parts {
		switch object := value.(type) {
		case map[string]any:
			var found bool
			value, found = object[part]
			if !found {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(object) {
				return nil, false
			}
			value = object[i]
		default:
			return nil, false
		}
	}
	return value, true
}
