package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func contextRNG(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed+1)) }

func contextHash(t testing.TB, value generatedProbe) [32]byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

func TestBankConstraintsLegacyIndependentUniqueSolution(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"zh", "en"} {
		t.Run(lang, func(t *testing.T) {
			for seed := uint64(0); seed < 40; seed++ {
				probe, err := generateConstraintsLegacy(contextRNG(seed), map[string]any{"lang": lang})
				if err != nil {
					t.Fatal(err)
				}
				// 独立算法只读题面中的候选及要求，遍历二选一组合，不读取实际参数或标准答案。
				candidates := regexp.MustCompile(`(?m)^组\d候选[A-B]：(.*)$`).FindAllStringSubmatch(probe.Request.Prompt, -1)
				targetText := regexp.MustCompile(`“灯笼”恰好出现 (\d+) 次`).FindStringSubmatch(probe.Request.Prompt)
				separator, needle := "\n\n", "灯笼"
				if lang == "en" {
					candidates = regexp.MustCompile(`(?m)^Group \d option [A-B]: (.*)$`).FindAllStringSubmatch(probe.Request.Prompt, -1)
					targetText = regexp.MustCompile(`lantern must occur exactly (\d+) times`).FindStringSubmatch(probe.Request.Prompt)
					separator, needle = "\n***\n", "lantern"
				}
				if len(candidates) != 8 || len(targetText) != 2 {
					t.Fatalf("seed %d：题面不能解析", seed)
				}
				target, _ := strconv.Atoi(targetText[1])
				solutions := []string{}
				for mask := 0; mask < 16; mask++ {
					paragraphs := make([]string, 4)
					for group := range paragraphs {
						paragraphs[group] = candidates[group*2+(mask>>group)&1][1]
					}
					text := strings.Join(paragraphs, separator)
					if strings.Count(text, needle) == target {
						solutions = append(solutions, text)
					}
					graded, err := gradeResult(probe.Grader, ProbeResult{Text: text, HTTPStatus: 200}, "")
					if err != nil || graded.Pass != (strings.Count(text, needle) == target) {
						t.Fatalf("seed %d 组合 %d：判分与题面不一致：%+v %v", seed, mask, graded, err)
					}
				}
				if len(solutions) != 1 || solutions[0] != probe.Answer {
					t.Fatalf("seed %d：唯一解数量 %d，与标准答案不一致", seed, len(solutions))
				}
				duplicate, err := generateConstraintsLegacy(contextRNG(seed), map[string]any{"lang": lang})
				if err != nil || contextHash(t, duplicate) != contextHash(t, probe) {
					t.Fatalf("seed %d：哈希不确定：%v", seed, err)
				}
			}
		})
	}
}

func TestBankConstraintsLegacyRejectRewrittenText(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		t.Run(lang, func(t *testing.T) {
			probe, err := generateConstraintsLegacy(contextRNG(17), map[string]any{"lang": lang})
			if err != nil {
				t.Fatal(err)
			}
			separator := "\n\n"
			if lang == "en" {
				separator = "\n***\n"
			}
			for name, output := range map[string]string{
				"插入标点":  strings.Replace(probe.Answer, separator, "!"+separator, 1),
				"额外分隔符": strings.Replace(probe.Answer, separator, separator+separator, 1),
				"额外空行":  strings.Replace(probe.Answer, separator, "\n"+separator, 1),
			} {
				t.Run(name, func(t *testing.T) {
					result, err := gradeResult(probe.Grader, ProbeResult{Text: output, HTTPStatus: 200}, "")
					if err != nil || result.Pass {
						t.Fatalf("改写候选文本不能通过：%+v %v", result, err)
					}
					if result.Partial == nil || *result.Partial <= 0 || *result.Partial >= 1 {
						t.Fatalf("仍须保留其他约束的部分分：%+v", result)
					}
				})
			}
		})
	}
}

func TestConstraintExactText(t *testing.T) {
	if err := validateConstraint(Constraint{Kind: "exact_text"}); err == nil {
		t.Fatal("完整文本约束须提供非空文本")
	}
	constraint := Constraint{Kind: "exact_text", Text: "春。\n\n夏。"}
	if err := validateConstraint(constraint); err != nil {
		t.Fatal(err)
	}
	if !checkConstraint(constraint.Text, constraint) || checkConstraint("春！\n\n夏。", constraint) || checkConstraint("春。\n\n\n夏。", constraint) {
		t.Fatal("完整文本约束须逐字匹配")
	}
}

type parsedContextRecord struct {
	id, department, manager, office, code string
	joined, badge                         int
}

// 验算器从渲染行解析字段，再根据渲染问题扫描查表，不使用生成器内存中的记录。
func solveContextText(t testing.TB, prompt string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^\[(R\d+)\] Name: [^|]+ \| Dept: (\w+) \| Manager: (R\d+) \| Office: ([A-G]-\d+) \| Joined: (\d+) \| Badge: (\d+) \| Code: (C\d+)$`)
	matches := pattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) < 3 {
		t.Fatal("长上下文缺少可解析记录")
	}
	rows := make([]parsedContextRecord, len(matches))
	ids, badges, codes := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for i, m := range matches {
		year, _ := strconv.Atoi(m[5])
		badge, _ := strconv.Atoi(m[6])
		rows[i] = parsedContextRecord{id: m[1], department: m[2], manager: m[3], office: m[4], joined: year, badge: badge, code: m[7]}
		if ids[m[1]] || badges[badge] || codes[m[7]] {
			t.Fatal("ID、徽章号或暗号不唯一")
		}
		ids[m[1]], badges[badge], codes[m[7]] = true, true, true
	}
	byID := func(id string) parsedContextRecord {
		for _, row := range rows {
			if row.id == id {
				return row
			}
		}
		t.Fatalf("经理 %s 不在记录中", id)
		return parsedContextRecord{}
	}
	byBadge := func(badge int) parsedContextRecord {
		for _, row := range rows {
			if row.badge == badge {
				return row
			}
		}
		t.Fatalf("徽章 %d 不在记录中", badge)
		return parsedContextRecord{}
	}
	question := strings.Split(prompt, "END RECORDS\n")[1]
	switch {
	case strings.Contains(question, "按徽章号"):
		m := regexp.MustCompile(`按徽章号 ([0-9、]+) 的顺序`).FindStringSubmatch(question)
		out := []string{}
		for _, s := range strings.Split(m[1], "、") {
			n, _ := strconv.Atoi(s)
			out = append(out, byBadge(n).office)
		}
		return strings.Join(out, ",")
	case strings.Contains(question, "经理的经理"):
		m := regexp.MustCompile(`徽章号为 (\d+)`).FindStringSubmatch(question)
		badge, _ := strconv.Atoi(m[1])
		return byID(byID(byBadge(badge).manager).manager).office
	case strings.Contains(question, "Joined 严格小于"):
		m := regexp.MustCompile(`(\w+) 部门中 Joined 严格小于 (\d+)`).FindStringSubmatch(question)
		year, _ := strconv.Atoi(m[2])
		count := 0
		for _, row := range rows {
			if row.department == m[1] && row.joined < year {
				count++
			}
		}
		return strconv.Itoa(count)
	case strings.Contains(question, "最先与最后"):
		return strconv.Itoa(rows[0].badge + rows[len(rows)-1].badge)
	case strings.Contains(question, "暗号 Code"):
		m := regexp.MustCompile(`Code 为 (C\d+)`).FindStringSubmatch(question)
		for _, row := range rows {
			if row.code == m[1] {
				return row.office
			}
		}
	}
	t.Fatal("未能解答渲染的长上下文问题")
	return ""
}

var bankContextTestTargets = []int{8192, 32768, 131072}
var bankContextTestVariants = []string{"multi_needle", "multi_hop", "aggregate", "edges", "associative"}

// 每个长度与变体组合保留一个固定 seed；原三个 seed 见 LongContextIndependentSolutionsStress benchmark。
func TestBankLongContextIndependentSolutions(t *testing.T) {
	t.Parallel()
	for _, target := range bankContextTestTargets {
		for _, variant := range bankContextTestVariants {
			t.Run(fmt.Sprintf("%s/%d", variant, target), func(t *testing.T) {
				assertBankLongContextIndependentSolution(t, target, variant, 0)
			})
		}
	}
}

func assertBankLongContextIndependentSolution(t testing.TB, target int, variant string, seed uint64) {
	t.Helper()
	params := map[string]any{"target_tokens": target, "variant": variant}
	probe, err := generateLongContext(contextRNG(seed), params)
	if err != nil {
		t.Fatal(err)
	}
	answer := "ANSWER: " + solveContextText(t, probe.Request.Prompt)
	if answer != probe.Answer {
		t.Fatalf("seed %d：独立求解不一致：%s / %s", seed, answer, probe.Answer)
	}
	tokens := probe.Parameters["estimated_input_tokens"].(int)
	if tokens < target || tokens >= target+128 || probe.Parameters["token_estimator"] != "lexical_subword_v1" {
		t.Fatalf("词元估算不在目标附近：%+v", probe.Parameters)
	}
	if tokens == len(probe.Request.Prompt) {
		t.Fatal("不能将字节数作为词元数")
	}
	result, err := gradeResult(probe.Grader, ProbeResult{Text: answer, HTTPStatus: 200}, "")
	if err != nil || !result.Pass {
		t.Fatalf("独立答案没有通过：%+v %v", result, err)
	}
	duplicate, err := generateLongContext(contextRNG(seed), params)
	if err != nil || contextHash(t, probe) != contextHash(t, duplicate) {
		t.Fatalf("同 seed 哈希不一致：%v", err)
	}
}

func TestBankContextParameterValidation(t *testing.T) {
	for _, params := range []map[string]any{{"lang": "fr"}, {"lang": 1}} {
		if _, err := generateConstraints(contextRNG(1), params); err == nil {
			t.Fatalf("约束题应拒绝参数：%v", params)
		}
	}
	for _, params := range []map[string]any{
		{"target_tokens": 8191}, {"target_tokens": 9000}, {"target_tokens": 262144},
		{"target_tokens": "8192"}, {"variant": "unknown"}, {"variant": 1},
	} {
		if _, err := generateLongContext(contextRNG(1), params); err == nil {
			t.Fatalf("长上下文应拒绝参数：%v", params)
		}
	}
}

func TestBankContextTokenEstimator(t *testing.T) {
	if tokens := estimateContextTokens("abcdefgh abc def 1234567 中。"); tokens != 9 {
		t.Fatalf("词元分段估算：得到 %d，预期 9", tokens)
	}
	if estimateContextTokens("abcdefgh") == estimateContextTokens("a b c d e f g h") {
		t.Fatal("相同字母数但词边界不同，应产生不同估算")
	}
}
