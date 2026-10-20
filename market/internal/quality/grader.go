package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

var (
	numberRe = regexp.MustCompile(`-?\d+(?:,\d{3})*(?:\.\d+)?`)
	choiceRe = regexp.MustCompile(`\b([A-E])\b`)
)

// grade 判定模型输出是否正确。text 为空时只校验判分规则本身（用于新建题目）。
// needle 是长上下文题在运行时生成的答案，替换 Answer 中的 {{NEEDLE}}。
func grade(g Grader, text, needle string) (bool, error) {
	answer := strings.ReplaceAll(g.Answer, "{{NEEDLE}}", needle)
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		if g.CaseInsensitive {
			s = strings.ToLower(s)
		}
		return s
	}
	switch g.Type {
	case "exact":
		if g.Answer == "" {
			return false, errors.New("exact grader needs answer")
		}
		t := norm(stripCodeFence(text))
		// 整体相等，或最后一个非空行相等（容忍模型多说一句前导语）
		return t == norm(answer) || lastLine(t) == norm(answer), nil
	case "contains":
		if g.Answer == "" {
			return false, errors.New("contains grader needs answer")
		}
		return strings.Contains(norm(text), norm(answer)), nil
	case "regex":
		re, err := regexp.Compile(g.Pattern)
		if err != nil || g.Pattern == "" {
			return false, fmt.Errorf("invalid regex: %v", err)
		}
		return re.MatchString(strings.TrimSpace(text)), nil
	case "numeric":
		if g.Number == nil {
			return false, errors.New("numeric grader needs number")
		}
		nums := numberRe.FindAllString(text, -1)
		if len(nums) == 0 {
			return false, nil
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(nums[len(nums)-1], ",", ""), 64)
		if err != nil {
			return false, nil
		}
		tol := g.Tolerance
		if tol == 0 {
			tol = 1e-6
		}
		return math.Abs(v-*g.Number) <= tol, nil
	case "choice":
		if len(g.Answer) != 1 {
			return false, errors.New("choice grader needs a single-letter answer")
		}
		m := choiceRe.FindAllStringSubmatch(strings.ToUpper(text), -1)
		if len(m) == 0 {
			return false, nil
		}
		return m[len(m)-1][1] == strings.ToUpper(g.Answer), nil
	case "json_keys":
		if len(g.Keys) == 0 {
			return false, errors.New("json_keys grader needs keys")
		}
		obj := extractJSONObject(text)
		if obj == nil {
			return false, nil
		}
		for _, k := range g.Keys {
			if _, ok := obj[k]; !ok {
				return false, nil
			}
		}
		return true, nil
	default:
		if inspectionGraderTypes[g.Type] {
			res, err := gradeInspection(g, ProbeResult{HTTPStatus: 200, Text: text}, needle, text == "")
			return res.Pass, err
		}
		return false, fmt.Errorf("unknown grader type %q", g.Type)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return s
}

func extractJSONObject(text string) map[string]any {
	text = stripCodeFence(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal([]byte(text[start:end+1]), &obj) != nil {
		return nil
	}
	return obj
}

var fillerSentences = []string{
	"The committee reviewed the quarterly logistics report without raising objections.",
	"Several researchers noted that the river level had remained stable throughout the season.",
	"A new bakery opened near the station and quickly attracted a loyal morning crowd.",
	"The archive contains letters written by travelers who crossed the mountains decades ago.",
	"Engineers replaced the old cooling system after a routine inspection in early spring.",
	"Volunteers organized the library shelves by subject and then by publication year.",
	"The weather forecast predicted light rain followed by clear skies over the weekend.",
	"Local farmers discussed crop rotation strategies at the monthly cooperative meeting.",
}

// expandPrompt 展开长上下文题的占位符 {{HAYSTACK:N}}（约 N 个字符的干扰文本，中间藏一条秘密代码），
// 返回实际 prompt 与本次生成的答案。
func expandPrompt(prompt string, rng *rand.Rand) (string, string) {
	re := regexp.MustCompile(`\{\{HAYSTACK:(\d+)\}\}`)
	m := re.FindStringSubmatch(prompt)
	if m == nil {
		return prompt, ""
	}
	size, _ := strconv.Atoi(m[1])
	if size > 400000 {
		size = 400000
	}
	needle := fmt.Sprintf("TM-%04d-%04d", rng.IntN(10000), rng.IntN(10000))
	var b strings.Builder
	insertAt := size/4 + rng.IntN(size/2+1)
	inserted := false
	for b.Len() < size {
		if !inserted && b.Len() >= insertAt {
			b.WriteString("Remember this carefully: the secret code is " + needle + ". ")
			inserted = true
		}
		b.WriteString(fillerSentences[rng.IntN(len(fillerSentences))])
		b.WriteString(" ")
	}
	if !inserted {
		b.WriteString("Remember this carefully: the secret code is " + needle + ". ")
	}
	return strings.Replace(prompt, m[0], b.String(), 1), needle
}
