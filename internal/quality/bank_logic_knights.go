package quality

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"strings"
)

var bankKnightNames = []string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛"}

type knightStatement struct {
	kind string
	x, y int
}

func knightTruth(s knightStatement, assignment uint) bool {
	x, y := assignment&(1<<s.x) != 0, assignment&(1<<s.y) != 0
	switch s.kind {
	case "is_knight":
		return x
	case "is_knave":
		return !x
	case "both_knights":
		return x && y
	case "either_knave", "if_then":
		return !x || !y
	case "same_type":
		return x == y
	case "diff_type":
		return x != y
	case "count_knights":
		return bits.OnesCount(assignment) == s.x
	}
	return false
}

func knightSolutions(statements []knightStatement) []uint {
	var solutions []uint
	for assignment := uint(0); assignment < 1<<len(statements); assignment++ {
		ok := true
		for person, s := range statements {
			if knightTruth(s, assignment) != (assignment&(1<<person) != 0) {
				ok = false
				break
			}
		}
		if ok {
			solutions = append(solutions, assignment)
			if len(solutions) == 2 {
				return solutions
			}
		}
	}
	return solutions
}

func renderKnightStatement(s knightStatement, people int) string {
	x, y := bankKnightNames[s.x], bankKnightNames[s.y]
	switch s.kind {
	case "is_knight":
		return x + "是骑士。"
	case "is_knave":
		return x + "是无赖。"
	case "both_knights":
		return x + "和" + y + "都是骑士。"
	case "either_knave":
		return x + "和" + y + "之中至少有一个是无赖。"
	case "same_type":
		return x + "和" + y + "是同一类人。"
	case "diff_type":
		return x + "和" + y + "不是同一类人。"
	case "if_then":
		return "如果" + x + "是骑士，那么" + y + "是无赖。"
	default:
		return fmt.Sprintf("我们 %d 个人里恰好有 %d 个骑士。", people, s.x)
	}
}

func generateKnights(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	people, err := bankInt(parameters, "people", 6, 4, 8, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	kinds := []string{"is_knight", "is_knave", "both_knights", "either_knave", "same_type", "diff_type", "if_then", "count_knights"}
	var statements []knightStatement
	var solution uint
	found := false
	for range 2000 {
		statements = make([]knightStatement, people)
		for i := range people {
			others := make([]int, 0, people-1)
			for j := range people {
				if j != i {
					others = append(others, j)
				}
			}
			rng.Shuffle(len(others), func(a, b int) { others[a], others[b] = others[b], others[a] })
			s := knightStatement{kind: kinds[rng.IntN(len(kinds))], x: others[0], y: others[1]}
			if s.kind == "count_knights" {
				s.x = 1 + rng.IntN(people-1)
			}
			statements[i] = s
		}
		solutions := knightSolutions(statements)
		if len(solutions) == 1 && solutions[0] != 0 {
			solution, found = solutions[0], true
			break
		}
	}
	if !found {
		return generatedProbe{}, fmt.Errorf("骑士与无赖未能在尝试上限内生成唯一解")
	}
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "岛上有 %d 个人，每个人是骑士或无赖。骑士总说真话，无赖总说假话。每人只说下面一句话：\n", people)
	var knights []string
	for i, statement := range statements {
		fmt.Fprintf(&prompt, "%s：%s\n", bankKnightNames[i], renderKnightStatement(statement, people))
		if solution&(1<<i) != 0 {
			knights = append(knights, bankKnightNames[i])
		}
	}
	prompt.WriteString("符合全部陈述的身份分配唯一。只输出所有骑士的代号，按甲乙丙丁戊己庚辛的顺序，以单个空格分隔，不要解释。")
	return generatedProbe{Request: ProbeRequest{Prompt: prompt.String()}, Grader: Grader{Type: "set_exact", GraderOptions: GraderOptions{Items: knights, Sep: `\s+`}},
		Parameters: map[string]any{"people": people}, Answer: strings.Join(knights, " ")}, nil
}
