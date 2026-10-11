package quality

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
)

func gradeToolCalls(g Grader, calls []ProbeToolCall, text string) (bool, *float64) {
	match := func(expected ExpectedToolCall, actual ProbeToolCall) bool {
		if expected.Name != actual.Name {
			return false
		}
		// 双侧先转成同一种 JSON 值，避免 int、float64 与 json.Number 的表示差异。
		expectedJSON, expectedErr := json.Marshal(expected.Args)
		actualJSON, actualErr := json.Marshal(actual.Arguments)
		if expectedErr != nil || actualErr != nil {
			return false
		}
		a, err := strictJSON(string(expectedJSON))
		if err != nil {
			return false
		}
		b, err := strictJSON(string(actualJSON))
		return err == nil && equalToolArgument(a, b, "", g.Tol)
	}
	matched := 0
	if g.Match == "sequence" {
		for i, expected := range g.Expect {
			if i < len(calls) && match(expected, calls[i]) {
				matched++
			}
		}
	} else {
		// 用二部图匹配，防止有容差的调用先占用了另一条唯一可匹配的响应。
		owners := make([]int, len(calls))
		for i := range owners {
			owners[i] = -1
		}
		var assign func(int, []bool) bool
		assign = func(expectedIndex int, visited []bool) bool {
			for callIndex, actual := range calls {
				if visited[callIndex] || !match(g.Expect[expectedIndex], actual) {
					continue
				}
				visited[callIndex] = true
				if owners[callIndex] == -1 || assign(owners[callIndex], visited) {
					owners[callIndex] = expectedIndex
					return true
				}
			}
			return false
		}
		for i := range g.Expect {
			if assign(i, make([]bool, len(calls))) {
				matched++
			}
		}
	}
	pass := matched == len(g.Expect) && (g.AllowExtraCalls || len(calls) == len(g.Expect))
	if g.TextRegex != "" {
		pass = pass && regexp.MustCompile(g.TextRegex).MatchString(text)
	}
	total := len(g.Expect)
	if !g.AllowExtraCalls {
		total = max(total, len(calls))
	}
	partial := 0.0
	if total > 0 {
		partial = float64(matched) / float64(total)
	} else if pass {
		partial = 1
	}
	return pass, &partial
}

func equalToolArgument(expected, actual any, path string, tolerances map[string]float64) bool {
	switch a := expected.(type) {
	case map[string]any:
		b, ok := actual.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for key, value := range a {
			actualValue, found := b[key]
			if !found || !equalToolArgument(value, actualValue, strings.TrimPrefix(path+"."+key, "."), tolerances) {
				return false
			}
		}
		return true
	case []any:
		b, ok := actual.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i, value := range a {
			if !equalToolArgument(value, b[i], path, tolerances) {
				return false
			}
		}
		return true
	case json.Number:
		b, ok := actual.(json.Number)
		if !ok {
			return false
		}
		x, xOK := new(big.Rat).SetString(a.String())
		y, yOK := new(big.Rat).SetString(b.String())
		if !xOK || !yOK {
			return false
		}
		if x.Cmp(y) == 0 {
			return true
		}
		tolerance, found := tolerances[path]
		if !found {
			parts := strings.Split(path, ".")
			tolerance = tolerances[parts[len(parts)-1]]
		}
		if tolerance <= 0 {
			return false
		}
		diff := new(big.Rat).Abs(new(big.Rat).Sub(x, y))
		delta, _ := diff.Float64()
		return delta <= tolerance
	case string:
		b, ok := actual.(string)
		normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
		return ok && normalize(a) == normalize(b)
	case bool:
		b, ok := actual.(bool)
		return ok && a == b
	case nil:
		return actual == nil
	}
	return false
}
