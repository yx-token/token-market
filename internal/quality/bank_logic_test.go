package quality

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type parsedZebraRule struct {
	op          string
	x, y, house int
}

type parsedZebra struct {
	houses int
	names  []string
	values [][]string
	rules  []parsedZebraRule
	asked  []string
}

// 校验器只读发布给模型的中文题面，不接触结构化线索与真解。
func parseZebraPrompt(t *testing.T, prompt string) parsedZebra {
	t.Helper()
	var puzzle parsedZebra
	lines := strings.Split(prompt, "\n")
	if _, err := fmt.Sscanf(lines[0], "一排有 %d 座房子", &puzzle.houses); err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]int{}
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "- "), "：", 2)
			values := strings.Split(parts[1], "、")
			a := len(puzzle.names)
			puzzle.names = append(puzzle.names, parts[0])
			puzzle.values = append(puzzle.values, values)
			for v, value := range values {
				var description string
				switch parts[0] {
				case "姓名":
					description = value + "先生"
				case "饮品":
					description = "喝" + value + "的人"
				case "宠物":
					description = "养" + value + "的人"
				case "颜色":
					description = value + "房子的住户"
				default:
					t.Fatalf("未识别属性：%s", parts[0])
				}
				descriptions[description] = a*puzzle.houses + v
			}
		}
		if strings.HasSuffix(line, "：<取值列表>") {
			puzzle.asked = append(puzzle.asked, strings.TrimSuffix(line, "：<取值列表>"))
		}
	}
	patterns := []struct{ op, pattern string }{
		{"not_at", `^(.+)不住在([0-9]+)号房子。$`},
		{"at", `^(.+)住在([0-9]+)号房子。$`},
		{"one_between", `^(.+)和(.+)之间恰好隔着一座房子。$`},
		{"next", `^(.+)和(.+)是隔壁邻居。$`},
		{"imm_left", `^(.+)紧挨着住在(.+)的左边。$`},
		{"left", `^(.+)住在(.+)左边的某处（不一定相邻）。$`},
		{"not_same", `^(.+)不是(.+)。$`},
		{"same", `^(.+)就是(.+)。$`},
	}
	for _, line := range lines {
		clue := regexp.MustCompile(`^[0-9]+\. (.+)$`).FindStringSubmatch(line)
		if clue == nil {
			continue
		}
		parsed := false
		for _, pattern := range patterns {
			m := regexp.MustCompile(pattern.pattern).FindStringSubmatch(clue[1])
			if m == nil {
				continue
			}
			x, ok := descriptions[m[1]]
			if !ok {
				t.Fatalf("线索主体未识别：%s", m[1])
			}
			rule := parsedZebraRule{op: pattern.op, x: x}
			if pattern.op == "at" || pattern.op == "not_at" {
				h, err := strconv.Atoi(m[2])
				if err != nil {
					t.Fatal(err)
				}
				rule.house = h - 1
			} else {
				y, ok := descriptions[m[2]]
				if !ok {
					t.Fatalf("线索对象未识别：%s", m[2])
				}
				rule.y = y
			}
			puzzle.rules = append(puzzle.rules, rule)
			parsed = true
			break
		}
		if !parsed {
			t.Fatalf("无法解析线索：%s", clue[1])
		}
	}
	if len(puzzle.rules) == 0 || len(puzzle.asked) != 2 {
		t.Fatalf("题面缺少线索或问题：%s", prompt)
	}
	return puzzle
}

// 按房子逐格填属性，同时对尚未填入的取值做位置可达性检查。
func solveZebraCells(puzzle parsedZebra) [][]int {
	n, m := puzzle.houses, len(puzzle.names)
	positions := make([]int, n*m)
	for i := range positions {
		positions[i] = -1
	}
	filled := make([][]bool, m)
	for a := range filled {
		filled[a] = make([]bool, n)
	}
	allows := func(rule parsedZebraRule, x, y int) bool {
		switch rule.op {
		case "at":
			return x == rule.house
		case "not_at":
			return x != rule.house
		case "same":
			return x == y
		case "not_same":
			return x != y
		case "left":
			return y > x
		case "imm_left":
			return y-x == 1
		case "next":
			return (x-y)*(x-y) == 1
		case "one_between":
			return (x-y)*(x-y) == 4
		}
		return false
	}
	possible := func(item int) []int {
		if positions[item] >= 0 {
			return []int{positions[item]}
		}
		var result []int
		for h, occupied := range filled[item/n] {
			if !occupied {
				result = append(result, h)
			}
		}
		return result
	}
	valid := func() bool {
		for _, rule := range puzzle.rules {
			found := false
			for _, x := range possible(rule.x) {
				if rule.op == "at" || rule.op == "not_at" {
					found = found || allows(rule, x, 0)
				} else {
					for _, y := range possible(rule.y) {
						found = found || allows(rule, x, y)
					}
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	var solutions [][]int
	var visit func(int)
	visit = func(cell int) {
		if len(solutions) == 2 {
			return
		}
		if cell == n*m {
			solutions = append(solutions, append([]int(nil), positions...))
			return
		}
		h, a := cell/m, cell%m
		for v := range n {
			item := a*n + v
			if positions[item] >= 0 {
				continue
			}
			positions[item], filled[a][h] = h, true
			if valid() {
				visit(cell + 1)
			}
			positions[item], filled[a][h] = -1, false
		}
	}
	visit(0)
	return solutions
}

func TestBankLogicZebraIndependentUnique(t *testing.T) {
	for _, dimensions := range [][2]int{{4, 3}, {4, 4}, {5, 3}, {5, 4}} {
		for seed := uint64(1); seed <= 6; seed++ {
			t.Run(fmt.Sprintf("%dx%d/%d", dimensions[0], dimensions[1], seed), func(t *testing.T) {
				generated, err := generateZebra(rand.New(rand.NewPCG(seed, 17)), map[string]any{"houses": dimensions[0], "attributes": dimensions[1]})
				if err != nil {
					t.Fatal(err)
				}
				puzzle := parseZebraPrompt(t, generated.Request.Prompt)
				solutions := solveZebraCells(puzzle)
				if len(solutions) != 1 {
					t.Fatalf("独立逐格求解得到 %d 个解", len(solutions))
				}
				var answer []string
				for _, asked := range puzzle.asked {
					for a, name := range puzzle.names {
						if asked == name {
							row := make([]string, puzzle.houses)
							for v, value := range puzzle.values[a] {
								row[solutions[0][a*puzzle.houses+v]] = value
							}
							answer = append(answer, name+"："+strings.Join(row, "，"))
						}
					}
				}
				if strings.Join(answer, "\n") != generated.Answer {
					t.Fatalf("独立解与答案不一致：%v / %s", answer, generated.Answer)
				}
				assertBankLogicAnswer(t, generated)
			})
		}
	}
}

func assertBankLogicAnswer(t *testing.T, generated generatedProbe) {
	t.Helper()
	if err := validateInspectionGrader(generated.Grader); err != nil {
		t.Fatalf("判分器配置无效：%v", err)
	}
	if result, err := gradeResult(generated.Grader, ProbeResult{Text: generated.Answer, HTTPStatus: 200}, ""); err != nil || !result.Pass {
		t.Fatalf("标准答案未通过判分：%+v", result)
	}
}

type parsedKnightRule struct {
	person       int
	dependencies []int
	truth        func([]bool) bool
}

func parseKnightPrompt(t *testing.T, prompt string) ([]string, []parsedKnightRule) {
	t.Helper()
	lines := strings.Split(prompt, "\n")
	var names, statements []string
	for _, line := range lines {
		m := regexp.MustCompile(`^([甲乙丙丁戊己庚辛])：(.+)。$`).FindStringSubmatch(line)
		if m != nil {
			names = append(names, m[1])
			statements = append(statements, m[2])
		}
	}
	index := func(name string) int {
		for i, value := range names {
			if value == name {
				return i
			}
		}
		t.Fatalf("未识别人物：%s", name)
		return -1
	}
	var rules []parsedKnightRule
	for person, statement := range statements {
		rule := parsedKnightRule{person: person}
		if m := regexp.MustCompile(`^我们 ([0-9]+) 个人里恰好有 ([0-9]+) 个骑士$`).FindStringSubmatch(statement); m != nil {
			n, _ := strconv.Atoi(m[1])
			wanted, _ := strconv.Atoi(m[2])
			if n != len(names) {
				t.Fatal("题面人数不一致")
			}
			for i := range names {
				rule.dependencies = append(rule.dependencies, i)
			}
			rule.truth = func(identity []bool) bool {
				total := 0
				for _, knight := range identity {
					if knight {
						total++
					}
				}
				return total == wanted
			}
		} else if m := regexp.MustCompile(`^([甲乙丙丁戊己庚辛])是(骑士|无赖)$`).FindStringSubmatch(statement); m != nil {
			x, positive := index(m[1]), m[2] == "骑士"
			rule.dependencies = []int{x}
			rule.truth = func(identity []bool) bool { return identity[x] == positive }
		} else if m := regexp.MustCompile(`^如果([甲乙丙丁戊己庚辛])是骑士，那么([甲乙丙丁戊己庚辛])是无赖$`).FindStringSubmatch(statement); m != nil {
			x, y := index(m[1]), index(m[2])
			rule.dependencies = []int{x, y}
			rule.truth = func(identity []bool) bool {
				if !identity[x] {
					return true
				}
				return !identity[y]
			}
		} else {
			m := regexp.MustCompile(`^([甲乙丙丁戊己庚辛])和([甲乙丙丁戊己庚辛])(都是骑士|之中至少有一个是无赖|是同一类人|不是同一类人)$`).FindStringSubmatch(statement)
			if m == nil {
				t.Fatalf("无法解析人物陈述：%s", statement)
			}
			x, y := index(m[1]), index(m[2])
			rule.dependencies = []int{x, y}
			switch m[3] {
			case "都是骑士":
				rule.truth = func(identity []bool) bool { return identity[x] && identity[y] }
			case "之中至少有一个是无赖":
				rule.truth = func(identity []bool) bool { return !identity[x] || !identity[y] }
			case "是同一类人":
				rule.truth = func(identity []bool) bool { return identity[x] == identity[y] }
			case "不是同一类人":
				rule.truth = func(identity []bool) bool { return identity[x] != identity[y] }
			}
		}
		rules = append(rules, rule)
	}
	return names, rules
}

// 回溯逐个人赋值，依赖已就绪的陈述立即剪枝；不同于生成器的整数位图穷举。
func solveKnightStatements(names []string, rules []parsedKnightRule) [][]bool {
	identity, assigned := make([]bool, len(names)), make([]bool, len(names))
	var solutions [][]bool
	var visit func(int)
	visit = func(person int) {
		if person == len(names) {
			solutions = append(solutions, append([]bool(nil), identity...))
			return
		}
		for _, value := range []bool{false, true} {
			identity[person], assigned[person] = value, true
			ok := true
			for _, rule := range rules {
				ready := assigned[rule.person]
				for _, dependency := range rule.dependencies {
					ready = ready && assigned[dependency]
				}
				if ready && rule.truth(identity) != identity[rule.person] {
					ok = false
					break
				}
			}
			if ok {
				visit(person + 1)
			}
			assigned[person] = false
		}
	}
	visit(0)
	return solutions
}

func TestBankLogicKnightsIndependentUnique(t *testing.T) {
	for _, people := range []int{4, 6, 8} {
		for seed := uint64(1); seed <= 12; seed++ {
			t.Run(fmt.Sprintf("%d/%d", people, seed), func(t *testing.T) {
				generated, err := generateKnights(rand.New(rand.NewPCG(seed, 23)), map[string]any{"people": people})
				if err != nil {
					t.Fatal(err)
				}
				names, rules := parseKnightPrompt(t, generated.Request.Prompt)
				if len(names) != people || len(rules) != people {
					t.Fatal("人物或陈述数量不一致")
				}
				solutions := solveKnightStatements(names, rules)
				if len(solutions) != 1 {
					t.Fatalf("独立回溯求解得到 %d 个解", len(solutions))
				}
				var answer []string
				for i, knight := range solutions[0] {
					if knight {
						answer = append(answer, names[i])
					}
				}
				if strings.Join(answer, " ") != generated.Answer {
					t.Fatalf("独立解与答案不一致：%v / %s", answer, generated.Answer)
				}
				assertBankLogicAnswer(t, generated)
			})
		}
	}
}

func runRegisterPrompt(t *testing.T, prompt string) ([]int, map[string]bool) {
	t.Helper()
	var state []int
	seen := map[string]bool{}
	index := func(s string) int {
		if len(s) != 1 || s[0] < 'A' || s[0] > 'E' {
			t.Fatalf("未识别寄存器：%s", s)
		}
		return int(s[0] - 'A')
	}
	integer := func(s string) int {
		x, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "初值：") {
			matches := regexp.MustCompile(`[A-E]=([0-9]+)`).FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				state = append(state, integer(match[1]))
			}
		}
		if !regexp.MustCompile(`^[0-9]{2} `).MatchString(line) {
			continue
		}
		if len(state) != 5 {
			t.Fatal("初值缺失")
		}
		p := strings.Fields(line)
		seen[p[1]] = true
		switch p[1] {
		case "ADD":
			x, y := index(p[2]), index(p[3])
			state[x] += state[y]
			state[x] %= 100
		case "SUB":
			x, y := index(p[2]), index(p[3])
			state[x] -= state[y]
			if state[x] < 0 {
				state[x] += 100
			}
		case "MUL":
			x := index(p[2])
			state[x] *= integer(p[3])
			state[x] %= 100
		case "SWP":
			x, y := index(p[2]), index(p[3])
			oldX := state[x]
			state[x], state[y] = state[y], oldX
		case "IFGT":
			x, y, z := index(p[2]), index(p[3]), index(p[4])
			if state[x]-state[y] > 0 {
				state[z] += integer(p[5])
				state[z] %= 100
			}
		case "IFODD":
			x, y := index(p[2]), index(p[3])
			if state[x]&1 != 0 {
				state[y] += state[x]
				state[y] %= 100
			}
		case "ROT":
			last := state[4]
			copy(state[1:], state[:4])
			state[0] = last
		default:
			t.Fatalf("未知指令：%s", p[1])
		}
	}
	return state, seen
}

func TestBankLogicRegisterIndependentExecution(t *testing.T) {
	seen := map[string]bool{}
	for _, operations := range []int{20, 40, 80} {
		for seed := uint64(1); seed <= 12; seed++ {
			t.Run(fmt.Sprintf("%d/%d", operations, seed), func(t *testing.T) {
				generated, err := generateRegister(rand.New(rand.NewPCG(seed, 29)), map[string]any{"operations": operations})
				if err != nil {
					t.Fatal(err)
				}
				state, operationsSeen := runRegisterPrompt(t, generated.Request.Prompt)
				for op := range operationsSeen {
					seen[op] = true
				}
				var answer []string
				for i, value := range state {
					answer = append(answer, fmt.Sprintf("%c=%d", 'A'+i, value))
				}
				if strings.Join(answer, ",") != generated.Answer {
					t.Fatalf("独立文本执行与结构化答案不同：%v / %s", answer, generated.Answer)
				}
				assertBankLogicAnswer(t, generated)
			})
		}
	}
	if len(seen) != 7 {
		t.Fatalf("未覆盖全部七种操作：%v", seen)
	}
}

func TestBankLogicRegisterArithmeticBoundaries(t *testing.T) {
	initial := [5]int{1, 99, 50, 0, 3}
	instructions := []registerInstruction{
		{op: "SUB", x: 0, y: 1},
		{op: "MUL", x: 1, constant: 9},
		{op: "IFODD", x: 4, y: 2},
		{op: "IFGT", x: 1, y: 0, z: 3, constant: 19},
		{op: "SWP", x: 0, y: 4},
		{op: "ROT"},
	}
	if actual := runRegister(initial, instructions); actual != [5]int{2, 3, 91, 53, 19} {
		t.Fatalf("负数模、条件或并行轮转错误：%v", actual)
	}
}

func TestBankLogicSeedsAndParameterDomains(t *testing.T) {
	for _, generator := range []struct {
		name       string
		generate   func(*rand.Rand, map[string]any) (generatedProbe, error)
		parameters map[string]any
		invalid    []map[string]any
	}{
		{"zebra", generateZebra, map[string]any{"houses": []int{4, 5}, "attributes": []any{json.Number("3"), json.Number("4")}}, []map[string]any{{"houses": 6}, {"attributes": 2}, {"houses": []int{5, 4}}}},
		{"knights", generateKnights, map[string]any{"people": []int{4, 8}}, []map[string]any{{"people": 3}, {"people": 9}, {"people": "6"}}},
		{"register", generateRegister, map[string]any{"operations": []int{20, 80}}, []map[string]any{{"operations": 19}, {"operations": 81}, {"operations": 40.5}}},
	} {
		t.Run(generator.name, func(t *testing.T) {
			first, err := generator.generate(rand.New(rand.NewPCG(101, 103)), generator.parameters)
			if err != nil {
				t.Fatal(err)
			}
			second, err := generator.generate(rand.New(rand.NewPCG(101, 103)), generator.parameters)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("同 seed 的题面、参数和答案不同")
			}
			for _, invalid := range generator.invalid {
				if _, err := generator.generate(rand.New(rand.NewPCG(101, 103)), invalid); err == nil {
					t.Fatalf("非法参数域被接受：%v", invalid)
				}
			}
		})
	}
}

func TestBankLogicDefaultGenerationStress(t *testing.T) {
	for _, generator := range []struct {
		name     string
		generate func(*rand.Rand, map[string]any) (generatedProbe, error)
	}{
		{"zebra", generateZebra}, {"knights", generateKnights}, {"register", generateRegister},
	} {
		t.Run(generator.name, func(t *testing.T) {
			for seed := uint64(0); seed < 500; seed++ {
				generated, err := generator.generate(rand.New(rand.NewPCG(seed, seed+17)), nil)
				if err != nil {
					t.Fatalf("seed=%d 默认域生成失败：%v", seed, err)
				}
				assertBankLogicAnswer(t, generated)
			}
		})
	}
}
