package quality

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

type writingRule struct {
	kind   string
	passes func(string) bool
}

// 独立验算器读取自然语言题面，不读取 Grader 或 Parameters，也不调用生产判分函数。
func writingRulesFromPrompt(t testing.TB, lang, prompt string) []writingRule {
	t.Helper()
	capture := func(pattern string) []string {
		match := regexp.MustCompile(pattern).FindStringSubmatch(prompt)
		if len(match) == 0 {
			t.Fatalf("无法从自由写作题面解析 %s", pattern)
		}
		return match[1:]
	}
	number := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	han := func(text string) int {
		n := 0
		for _, c := range text {
			if unicode.Is(unicode.Han, c) {
				n++
			}
		}
		return n
	}
	if lang == "zh" {
		initials := capture(`每段第一个字依次为“([^”]+)”“([^”]+)”“([^”]+)”`)
		bounds := capture(`汉字数在 (\d+) 到 (\d+) 之间`)
		low, high := number(bounds[0]), number(bounds[1])
		keyword := capture(`连续字符串“([^”]+)”恰好出现 (\d+) 次`)
		n := number(keyword[1])
		forbidden := capture(`不能出现“([^”]+)”字`)[0]
		maxSentence := number(capture(`每句至多 (\d+) 个汉字`)[0])
		ending := capture(`必须以“([^”]+)”结尾`)[0]
		return []writingRule{
			{"paragraph_shape", func(text string) bool {
				paragraphs := strings.Split(strings.TrimSpace(text), "\n\n")
				if len(paragraphs) != 3 {
					return false
				}
				for i, p := range paragraphs {
					if !strings.HasPrefix(p, initials[i]) {
						return false
					}
				}
				return true
			}},
			{"han_count", func(text string) bool { n := han(text); return n >= low && n <= high }},
			{"substr_count", func(text string) bool { return len(strings.Split(text, keyword[0]))-1 == n }},
			{"forbid_chars", func(text string) bool { return !strings.Contains(text, forbidden) }},
			{"sentence_han_count", func(text string) bool {
				for _, s := range strings.FieldsFunc(text, func(c rune) bool { return c == '。' || c == '？' || c == '！' }) {
					if han(s) > maxSentence {
						return false
					}
				}
				return true
			}},
			{"no_ascii_alnum", func(text string) bool {
				for _, c := range text {
					if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
						return false
					}
				}
				return true
			}},
			{"ends_with", func(text string) bool { return strings.HasSuffix(strings.TrimSpace(text), ending) }},
		}
	}
	initials := capture(`first words must be ([a-z]+), ([a-z]+), and ([a-z]+) in that order`)
	bounds := capture(`between (\d+) and (\d+) English words`)
	low, high := number(bounds[0]), number(bounds[1])
	keyword := capture(`word ([a-z]+) exactly (\d+) times`)
	n := number(keyword[1])
	maxSentence := number(capture(`sentence has at most (\d+) words`)[0])
	ending := capture(`exact sentence ([a-z ]+\.)`)[0]
	return []writingRule{
		{"paragraph_shape", func(text string) bool {
			paragraphs := strings.Split(strings.TrimSpace(text), "\n***\n")
			if len(paragraphs) != 3 {
				return false
			}
			for i, p := range paragraphs {
				words := independentWritingWords(p)
				if len(words) == 0 || words[0] != initials[i] || !strings.HasPrefix(p, words[0]) {
					return false
				}
			}
			return true
		}},
		{"word_count", func(text string) bool { n := len(independentWritingWords(text)); return n >= low && n <= high }},
		{"keyword_count", func(text string) bool {
			count := 0
			for _, word := range independentWritingWords(text) {
				if strings.EqualFold(word, keyword[0]) {
					count++
				}
			}
			return count == n
		}},
		{"forbid_chars", func(text string) bool { return !strings.ContainsRune(text, ',') }},
		{"sentence_max_words", func(text string) bool {
			for _, s := range strings.FieldsFunc(text, func(c rune) bool { return c == '.' || c == '?' || c == '!' }) {
				if len(independentWritingWords(s)) > maxSentence {
					return false
				}
			}
			return true
		}},
		{"max_word_repeat", func(text string) bool {
			counts := map[string]int{}
			for _, word := range independentWritingWords(text) {
				word = strings.ToLower(word)
				counts[word]++
				if counts[word] > 3 {
					return false
				}
			}
			return true
		}},
		{"all_lowercase", func(text string) bool {
			for _, c := range text {
				if unicode.IsUpper(c) {
					return false
				}
			}
			return true
		}},
		{"ends_sentence", func(text string) bool {
			text = strings.TrimSpace(text)
			if !strings.HasSuffix(text, ending) {
				return false
			}
			prefix := strings.TrimSuffix(text, ending)
			if prefix != "" && !strings.ContainsRune(" \t\r\n", rune(prefix[len(prefix)-1])) {
				return false
			}
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				return true
			}
			last := prefix[len(prefix)-1]
			return last == '.' || last == '?' || last == '!'
		}},
	}
}

func independentWritingWords(text string) []string {
	isLetter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var words []string
	for i := 0; i < len(text); {
		if !isLetter(text[i]) {
			i++
			continue
		}
		start := i
		for i < len(text) && isLetter(text[i]) {
			i++
		}
		if i+1 < len(text) && text[i] == '\'' && isLetter(text[i+1]) {
			i++
			for i < len(text) && isLetter(text[i]) {
				i++
			}
		}
		words = append(words, text[start:i])
	}
	return words
}

// 代表 seed 覆盖三种关键词、两种次数、六种段落顺序、全部句长和字数边界。
// 原每种语言 100 个 seed 的完整独立校验见 FreeWritingIndependentValidationStress benchmark。
func TestBankConstraintsFreeWritingIndependentValidation(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"zh", "en"} {
		t.Run(lang, func(t *testing.T) {
			domains := map[string]map[string]bool{}
			for _, seed := range []uint64{0, 1, 2, 3, 4, 5, 7, 8, 10, 11, 25, 34} {
				probe := assertBankFreeWritingIndependentValidation(t, lang, seed)
				for field, value := range probe.Parameters {
					if domains[field] == nil {
						domains[field] = map[string]bool{}
					}
					domains[field][fmt.Sprint(value)] = true
				}
			}
			minimum, maximum, sentence := "min_han", "max_han", "sentence_max_han"
			if lang == "en" {
				minimum, maximum, sentence = "min_words", "max_words", "sentence_max_words"
			}
			for field, want := range map[string]int{"keyword": 3, "target_occurrences": 2, "initials": 6, minimum: 2, maximum: 2, sentence: 3} {
				if len(domains[field]) != want {
					t.Fatalf("代表 seed 未覆盖 %s 的全部 %d 个分支：%v", field, want, domains[field])
				}
			}
		})
	}
}

func assertBankFreeWritingIndependentValidation(t testing.TB, lang string, seed uint64) generatedProbe {
	t.Helper()
	probe, err := generateConstraints(contextRNG(seed), map[string]any{"lang": lang})
	if err != nil {
		t.Fatalf("seed %d：%v", seed, err)
	}
	if strings.Contains(probe.Request.Prompt, "候选") || strings.Contains(probe.Request.Prompt, "Group") || strings.Contains(probe.Request.Prompt, probe.Answer) {
		t.Fatal("自由写作不能在题面提供参考文本或候选")
	}
	rules := writingRulesFromPrompt(t, lang, probe.Request.Prompt)
	if len(rules) < 5 || len(rules) > 8 || len(probe.Grader.Checks) != len(rules) {
		t.Fatal("题面与判分规则数量不一致")
	}
	for i, rule := range rules {
		if !rule.passes(probe.Answer) || !checkConstraint(probe.Answer, probe.Grader.Checks[i]) {
			t.Fatalf("seed %d：参考文本违反 %s", seed, rule.kind)
		}
		mutant, err := constraintSingleViolation(probe, lang, rule.kind)
		if err != nil {
			t.Fatal(err)
		}
		for j, other := range rules {
			want := i != j
			if other.passes(mutant) != want || checkConstraint(mutant, probe.Grader.Checks[j]) != want {
				t.Fatalf("seed %d：%s 单项变体对 %s 的结果不正确：%q", seed, rule.kind, other.kind, mutant)
			}
		}
		graded, err := gradeResult(probe.Grader, ProbeResult{HTTPStatus: 200, Text: mutant}, "")
		wantPartial := float64(len(rules)-1) / float64(len(rules))
		if err != nil || graded.Pass || graded.Partial == nil || *graded.Partial != wantPartial {
			t.Fatalf("seed %d：%s 单项违例未被检出或部分分错误：%+v %v", seed, rule.kind, graded, err)
		}
	}
	alternative := strings.Replace(probe.Answer, "旅人", "行客", 1)
	if lang == "en" {
		alternative = strings.Replace(probe.Answer, "patient", "steady", 1)
	}
	graded, err := gradeResult(probe.Grader, ProbeResult{HTTPStatus: 200, Text: alternative}, "")
	if alternative == probe.Answer || err != nil || !graded.Pass {
		t.Fatalf("seed %d：另一份合规文本须通过：%+v %v", seed, graded, err)
	}
	for _, rule := range rules {
		if !rule.passes(alternative) {
			t.Fatalf("seed %d：另一份合规文本须满足题面 %s", seed, rule.kind)
		}
	}
	duplicate, err := generateConstraints(contextRNG(seed), map[string]any{"lang": lang})
	if err != nil || contextHash(t, duplicate) != contextHash(t, probe) {
		t.Fatalf("seed %d：同 seed 哈希不一致：%v", seed, err)
	}
	return probe
}

func TestConstraintParagraphShape(t *testing.T) {
	for _, invalid := range []Constraint{
		{Kind: "paragraph_shape"},
		{Kind: "paragraph_shape", N: constraintInt(1), Initials: []string{"春天"}, Language: "zh", Sep: "blank"},
		{Kind: "paragraph_shape", N: constraintInt(1), Initials: []string{"two words"}, Language: "en", Sep: "***"},
		{Kind: "paragraph_shape", N: constraintInt(2), Initials: []string{"spring"}, Language: "en", Sep: "***"},
		{Kind: "paragraph_shape", N: constraintInt(1), Initials: []string{"spring"}, Language: "en", Sep: "custom"},
	} {
		if validateConstraint(invalid) == nil {
			t.Fatalf("须拒绝无效段落形状：%+v", invalid)
		}
	}
	rule := Constraint{Kind: "paragraph_shape", N: constraintInt(2), Initials: []string{"spring", "summer"}, Language: "en", Sep: "***"}
	if err := validateConstraint(rule); err != nil {
		t.Fatal(err)
	}
	if !checkConstraint("spring rain.\n***\nsummer sun.", rule) {
		t.Fatal("合法段落首词须通过")
	}
	for _, invalid := range []string{
		"springtime rain.\n***\nsummer sun.",
		"#spring rain.\n***\nsummer sun.",
		"spring rain. *** summer sun.",
		"spring rain.\n***\n***\nsummer sun.",
	} {
		if checkConstraint(invalid, rule) {
			t.Fatalf("须严格验证首词、分隔行与空段：%q", invalid)
		}
	}
}

func TestConstraintEndsSentence(t *testing.T) {
	rule := Constraint{Kind: "ends_sentence", Text: "safe travels."}
	if err := validateConstraint(rule); err != nil {
		t.Fatal(err)
	}
	if validateConstraint(Constraint{Kind: "ends_sentence", Text: "safe travels"}) == nil {
		t.Fatal("固定末句须有句末标点")
	}
	for _, text := range []string{"safe travels.", "welcome home. safe travels.", "ready? safe travels."} {
		if !checkConstraint(text, rule) {
			t.Fatalf("合法固定末句应通过：%q", text)
		}
	}
	for _, text := range []string{"unsafe travels.", "we wish you safe travels.", "welcome.safe travels.", "safe travels. welcome home."} {
		if checkConstraint(text, rule) {
			t.Fatalf("不能把单词后缀或长句尾部当成固定末句：%q", text)
		}
	}
}
