package quality

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
)

var bankZebraAttributes = []string{"姓名", "饮品", "宠物", "颜色", "职业", "乐器"}
var bankZebraValues = [][]string{
	{"赵", "钱", "孙", "李", "周", "吴"},
	{"茶", "咖啡", "牛奶", "果汁", "矿泉水", "可可"},
	{"猫", "狗", "鹦鹉", "金鱼", "兔子", "仓鼠"},
	{"红", "绿", "蓝", "黄", "白", "紫"},
	{"教师", "画家", "厨师", "木匠", "园丁", "会计"},
	{"钢琴", "小提琴", "笛子", "吉他", "鼓", "二胡"},
}

type zebraItem struct{ attribute, value int }
type zebraClue struct {
	kind  string
	x, y  zebraItem
	house int
}

func zebraHolds(c zebraClue, positions [][]int) bool {
	x := positions[c.x.attribute][c.x.value]
	if c.kind == "at" {
		return x == c.house
	}
	if c.kind == "not_at" {
		return x != c.house
	}
	y := positions[c.y.attribute][c.y.value]
	switch c.kind {
	case "same":
		return x == y
	case "not_same":
		return x != y
	case "left":
		return x < y
	case "imm_left":
		return x+1 == y
	case "next":
		return x-y == 1 || y-x == 1
	case "one_between":
		return x-y == 2 || y-x == 2
	}
	return false
}

// 按属性排列搜索；提前找到两解或达到确定性步数上限即停止。
func zebraSolutionCount(houses, attributes int, clues []zebraClue) (int, bool) {
	var permutations [][]int
	p := make([]int, houses)
	used := make([]bool, houses)
	var permute func(int)
	permute = func(i int) {
		if i == houses {
			permutations = append(permutations, append([]int(nil), p...))
			return
		}
		for v := range houses {
			if !used[v] {
				p[i], used[v] = v, true
				permute(i + 1)
				used[v] = false
			}
		}
	}
	permute(0)
	positions := make([][]int, attributes)
	candidates := make([][][]int, attributes)
	for a := range attributes {
		for _, permutation := range permutations {
			positions[a] = permutation
			ok := true
			for _, c := range clues {
				if c.x.attribute == a && (c.kind == "at" || c.kind == "not_at" || c.y.attribute == a) && !zebraHolds(c, positions) {
					ok = false
					break
				}
			}
			if ok {
				candidates[a] = append(candidates[a], permutation)
			}
		}
	}
	order := make([]int, attributes)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return len(candidates[order[i]]) < len(candidates[order[j]]) })
	assigned := make([]bool, attributes)
	count, steps, complete := 0, 0, true
	var visit func(int)
	visit = func(level int) {
		if count >= 2 || !complete {
			return
		}
		if level == attributes {
			count++
			return
		}
		a := order[level]
		assigned[a] = true
		for _, permutation := range candidates[a] {
			steps++
			if steps > 50000 {
				complete = false
				break
			}
			positions[a] = permutation
			ok := true
			for _, c := range clues {
				if assigned[c.x.attribute] && (c.kind == "at" || c.kind == "not_at" || assigned[c.y.attribute]) && !zebraHolds(c, positions) {
					ok = false
					break
				}
			}
			if ok {
				visit(level + 1)
			}
			if count >= 2 || !complete {
				break
			}
		}
		assigned[a] = false
	}
	visit(0)
	return count, complete
}

func zebraDescription(x zebraItem) string {
	value := bankZebraValues[x.attribute][x.value]
	switch x.attribute {
	case 0:
		return value + "先生"
	case 1:
		return "喝" + value + "的人"
	case 2:
		return "养" + value + "的人"
	case 3:
		return value + "房子的住户"
	case 4:
		return "当" + value + "的人"
	default:
		return "演奏" + value + "的人"
	}
}

func renderZebraClue(c zebraClue) string {
	x, y := zebraDescription(c.x), zebraDescription(c.y)
	switch c.kind {
	case "at":
		return fmt.Sprintf("%s住在%d号房子。", x, c.house+1)
	case "not_at":
		return fmt.Sprintf("%s不住在%d号房子。", x, c.house+1)
	case "same":
		return x + "就是" + y + "。"
	case "not_same":
		return x + "不是" + y + "。"
	case "left":
		return x + "住在" + y + "左边的某处（不一定相邻）。"
	case "imm_left":
		return x + "紧挨着住在" + y + "的左边。"
	case "next":
		return x + "和" + y + "是隔壁邻居。"
	default:
		return x + "和" + y + "之间恰好隔着一座房子。"
	}
}

func generateZebra(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	houses, err := bankInt(parameters, "houses", 5, 4, 6, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	attributes, err := bankInt(parameters, "attributes", 5, 3, 6, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	positions := make([][]int, attributes)
	for a := range attributes {
		positions[a] = rng.Perm(houses)
	}
	var pool []zebraClue
	add := func(c zebraClue, probability float64) {
		if zebraHolds(c, positions) && rng.Float64() < probability {
			pool = append(pool, c)
		}
	}
	for a := range attributes {
		for v := range houses {
			x := zebraItem{a, v}
			for h := range houses {
				add(zebraClue{kind: "at", x: x, house: h}, 0.15)
				add(zebraClue{kind: "not_at", x: x, house: h}, 0.5)
			}
			for b := range attributes {
				for w := range houses {
					y := zebraItem{b, w}
					if x == y {
						continue
					}
					if a != b && a*houses+v < b*houses+w {
						add(zebraClue{kind: "same", x: x, y: y}, 1)
						add(zebraClue{kind: "not_same", x: x, y: y}, 0.6)
					}
					add(zebraClue{kind: "left", x: x, y: y}, 0.8)
					add(zebraClue{kind: "imm_left", x: x, y: y}, 1)
					if a*houses+v < b*houses+w {
						add(zebraClue{kind: "next", x: x, y: y}, 1)
						add(zebraClue{kind: "one_between", x: x, y: y}, 1)
					}
				}
			}
		}
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	var clues []zebraClue
	unique := false
	for i, c := range pool {
		if i == 64 {
			break
		}
		clues = append(clues, c)
		if len(clues) >= attributes*2 {
			count, complete := zebraSolutionCount(houses, attributes, clues)
			if count == 1 && complete {
				unique = true
				break
			}
		}
	}
	if !unique {
		// 候选或搜索预算不足时加入真解位置线索；不会产出多解或无解题。
		for a := range attributes {
			for v := range houses {
				clues = append(clues, zebraClue{kind: "at", x: zebraItem{a, v}, house: positions[a][v]})
			}
		}
	}
	rng.Shuffle(len(clues), func(i, j int) { clues[i], clues[j] = clues[j], clues[i] })
	for i := 0; i < len(clues); {
		trial := append([]zebraClue(nil), clues[:i]...)
		trial = append(trial, clues[i+1:]...)
		count, complete := zebraSolutionCount(houses, attributes, trial)
		if count == 1 && complete {
			clues = trial
		} else {
			i++
		}
	}
	count, complete := zebraSolutionCount(houses, attributes, clues)
	if count != 1 || !complete {
		return generatedProbe{}, fmt.Errorf("斑马谜题未能在搜索预算内验证唯一解")
	}
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "一排有 %d 座房子，从左到右编号 1 到 %d。每座房子每种属性恰好一个取值，同一属性的取值不重复；下列每种属性的每个取值恰好属于一座房子：\n", houses, houses)
	for a := range attributes {
		fmt.Fprintf(&prompt, "- %s：%s\n", bankZebraAttributes[a], strings.Join(bankZebraValues[a][:houses], "、"))
	}
	prompt.WriteString("“左边”指编号更小。线索：\n")
	for i, c := range clues {
		fmt.Fprintf(&prompt, "%d. %s\n", i+1, renderZebraClue(c))
	}
	fields := map[string]string{}
	var answer []string
	prompt.WriteString("满足全部线索的安排唯一。只输出以下两行，按 1 到末号房子的顺序列出取值，用逗号分隔，不要解释：\n")
	for a := attributes - 2; a < attributes; a++ {
		values := make([]string, houses)
		for v, house := range positions[a] {
			values[house] = bankZebraValues[a][v]
		}
		fields[bankZebraAttributes[a]] = strings.Join(values, "，")
		answer = append(answer, bankZebraAttributes[a]+"："+fields[bankZebraAttributes[a]])
		fmt.Fprintf(&prompt, "%s：<取值列表>\n", bankZebraAttributes[a])
	}
	return generatedProbe{Request: ProbeRequest{Prompt: prompt.String()}, Grader: Grader{Type: "fields_exact", GraderOptions: GraderOptions{Fields: fields}},
		Parameters: map[string]any{"houses": houses, "attributes": attributes, "clue_count": len(clues)}, Answer: strings.Join(answer, "\n")}, nil
}
