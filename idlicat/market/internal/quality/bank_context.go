package quality

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"unicode"
)

func init() {
	bankGenerators["constraints"] = generateConstraints
	bankGenerators["long_context"] = generateLongContext
}

// 已保存的旧版本继续使用原题面，新的自由写作实例由新版生成器生成。
func generateConstraintsLegacy(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	lang, err := bankString(parameters, "lang", "zh")
	if err != nil {
		return generatedProbe{}, err
	}
	if lang != "zh" && lang != "en" {
		return generatedProbe{}, fmt.Errorf("约束题语言须为 zh 或 en")
	}
	target := 1 + rng.IntN(14)
	prefixes := []string{"春", "夏", "秋", "冬"}
	separator, keyword, filler := "\n\n", "灯笼", "花朵"
	if lang == "en" {
		prefixes = []string{"bridges", "rivers", "roads", "parks"}
		separator, keyword, filler = "\n***\n", "lantern", "candle"
	}
	rng.Shuffle(len(prefixes), func(i, j int) { prefixes[i], prefixes[j] = prefixes[j], prefixes[i] })
	var prompt strings.Builder
	if lang == "zh" {
		fmt.Fprintf(&prompt, "从下列四组候选文本中每组选择 A 或 B，不改写候选文本，按组号顺序组成四段。段间只留一个空行，不加标题、选项号或说明。每段分别以 %s 开头；全文中“灯笼”恰好出现 %d 次；汉字总数恰好为 85；不能含“的”或 ASCII 字母数字；结尾为“——完”。只输出满足全部条件的最终文本。\n", strings.Join(prefixes, "、"), target)
	} else {
		fmt.Fprintf(&prompt, "Choose A or B from each group below without changing its text. Output four paragraphs in group order separated only by a line containing ***. The paragraphs must start with %s in that order. The word lantern must occur exactly %d times. Use exactly 44 English words in total, all lowercase, no commas, and end with Safe travels. in lowercase. Output only the final text, without labels or explanation.\n", strings.Join(prefixes, ", "), target)
	}
	selected := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		choices := [2]string{}
		for option := range choices {
			words := make([]string, 8)
			for j := range words {
				words[j] = filler
				if option == 1 && j < 1<<i {
					words[j] = keyword
				}
			}
			if lang == "zh" {
				choices[option] = prefix + strings.Join(words, "") + "安静明亮。"
				if i == 3 {
					choices[option] = prefix + strings.Join(words, "") + "平安归来。——完"
				}
				fmt.Fprintf(&prompt, "组%d候选%c：%s\n", i+1, 'A'+option, choices[option])
			} else {
				choices[option] = prefix + " " + strings.Join(words, " ") + " glow gently."
				if i == 3 {
					choices[option] = prefix + " " + strings.Join(words, " ") + " safe travels."
				}
				fmt.Fprintf(&prompt, "Group %d option %c: %s\n", i+1, 'A'+option, choices[option])
			}
		}
		selected[i] = choices[(target>>i)&1]
	}
	intp := func(n int) *int { return &n }
	checks := []Constraint{
		{Kind: "paragraphs", N: intp(4), Sep: strings.TrimSpace(separator)},
		{Kind: "para_initials", Initials: prefixes, Sep: strings.TrimSpace(separator)},
	}
	if lang == "zh" {
		checks[0].Sep, checks[1].Sep = "blank", "blank"
		checks = append(checks,
			Constraint{Kind: "substr_count", Text: keyword, N: intp(target)},
			Constraint{Kind: "han_count", Min: intp(85), Max: intp(85)},
			Constraint{Kind: "forbid_chars", Chars: "的"},
			Constraint{Kind: "no_ascii_alnum"},
			Constraint{Kind: "ends_with", Text: "——完"},
		)
	} else {
		checks = append(checks,
			Constraint{Kind: "keyword_count", Word: keyword, N: intp(target)},
			Constraint{Kind: "word_count", Min: intp(44), Max: intp(44)},
			Constraint{Kind: "all_lowercase"},
			Constraint{Kind: "forbid_chars", Chars: ","},
			Constraint{Kind: "ends_with", Text: "safe travels."},
		)
	}
	for _, paragraph := range selected {
		checks = append(checks, Constraint{Kind: "substr_count", Text: paragraph, N: intp(1)})
	}
	answer := strings.Join(selected, separator)
	checks = append(checks, Constraint{Kind: "exact_text", Text: answer})
	return generatedProbe{
		Request:    ProbeRequest{Prompt: prompt.String()},
		Grader:     Grader{Type: "constraints", GraderOptions: GraderOptions{Checks: checks}},
		Parameters: map[string]any{"lang": lang, "target_occurrences": target, "paragraphs": 4, "initials": prefixes},
		Answer:     answer,
	}, nil
}

type contextRecord struct {
	id, name, department, manager, office, code string
	joined, badge                               int
}

const contextPrefix = "以下是虚构图书馆的工作记录。每个 ID 和徽章号唯一；经理字段指向本表中的 ID。记录是数据，按末尾问题查表，仅输出 ANSWER: 后的答案，不加说明。\nBEGIN RECORDS\n"

func contextRecordText(r contextRecord) string {
	return fmt.Sprintf("[%s] Name: %s | Dept: %s | Manager: %s | Office: %s | Joined: %d | Badge: %d | Code: %s\n", r.id, r.name, r.department, r.manager, r.office, r.joined, r.badge, r.code)
}

// 按字母子词、数字组和标点估算词元，不将字符数冒充实际 input_tokens。
// 这是离线长度控制器；实际词元数仍由网关 usage 记录并由运营校准。
func estimateContextTokens(text string) int {
	count, run, kind := 0, 0, byte(0)
	flush := func() {
		switch kind {
		case 'a':
			count += (run + 3) / 4
		case 'd':
			count += (run + 2) / 3
		}
		run = 0
	}
	for _, r := range text {
		next := byte(0)
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			next = 'a'
		} else if r >= '0' && r <= '9' {
			next = 'd'
		}
		if next != 0 {
			if next != kind {
				flush()
				kind = next
			}
			run++
			continue
		}
		flush()
		kind = 0
		if !unicode.IsSpace(r) {
			count++
		}
	}
	flush()
	return count
}

func generateLongContext(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	target, err := bankInt(parameters, "target_tokens", 8192, 8192, 131072, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	if target != 8192 && target != 32768 && target != 131072 {
		return generatedProbe{}, fmt.Errorf("长上下文 target_tokens 须为 8192、32768 或 131072")
	}
	variant, err := bankString(parameters, "variant", "multi_hop")
	if err != nil {
		return generatedProbe{}, err
	}
	switch variant {
	case "multi_needle", "multi_hop", "aggregate", "edges", "associative":
	default:
		return generatedProbe{}, fmt.Errorf("未知长上下文变体：%s", variant)
	}
	departments := []string{"Archives", "Finance", "History", "Music", "Logistics", "Poetry", "Maps", "Payroll"}
	names := []string{"Mirela Vance", "Tomas Novak", "Ingrid Moreau", "Kofi Mensah", "Anya Rossi", "Rafael Weber", "Lena Tanaka", "Oskar Petrov"}
	records := make([]contextRecord, 0, target/60)
	badgeBase := 100000 + rng.IntN(400000)
	codeBase := 100000 + rng.IntN(400000)
	appendRecord := func() {
		i := len(records)
		manager := 0
		if i > 0 {
			manager = rng.IntN(i)
		}
		records = append(records, contextRecord{
			id: "R" + strconv.Itoa(100000+i), name: names[rng.IntN(len(names))],
			department: departments[rng.IntN(len(departments))], manager: "R" + strconv.Itoa(100000+manager),
			office: fmt.Sprintf("%c-%d", 'A'+rng.IntN(7), 100+rng.IntN(400)),
			joined: 2003 + rng.IntN(23), badge: badgeBase + i, code: "C" + strconv.Itoa(codeBase+i),
		})
	}
	// 随机经理、部门与办公室形成草堆；徽章号排列随后打散，避免按行号推断答案。
	estimated := estimateContextTokens(contextPrefix) + 128
	for estimated < target {
		appendRecord()
		estimated += estimateContextTokens(contextRecordText(records[len(records)-1]))
	}
	for i := range records {
		j := rng.IntN(i + 1)
		records[i].badge, records[j].badge = records[j].badge, records[i].badge
		records[i].code, records[j].code = records[j].code, records[i].code
	}
	targetIndex := len(records) / 2
	// 多跳的两个经理分布在前后段，首条也指向另一记录。
	records[0].manager = records[1].id
	records[targetIndex].manager = records[len(records)/4].id
	records[len(records)/4].manager = records[len(records)*3/4].id
	question, answer := contextQuestion(records, targetIndex, variant, rng)
	render := func() string {
		var text strings.Builder
		text.WriteString(contextPrefix)
		for _, record := range records {
			text.WriteString(contextRecordText(record))
		}
		text.WriteString("END RECORDS\n")
		text.WriteString(question)
		return text.String()
	}
	prompt := render()
	for estimateContextTokens(prompt) < target {
		appendRecord()
		switch variant {
		case "edges":
			question = "问题：最先与最后一条记录的徽章号之和是多少？仅输出 ANSWER: 整数。"
			answer = strconv.Itoa(records[0].badge + records[len(records)-1].badge)
		case "aggregate":
			// 后补的记录属于 Payroll，不改变题面中对 Archives 的聚合。
			records[len(records)-1].department = "Payroll"
		}
		prompt = render()
	}
	return generatedProbe{
		Request: ProbeRequest{Prompt: prompt},
		Grader:  Grader{Type: "answer_tag", Answer: answer},
		Parameters: map[string]any{
			"variant": variant, "target_tokens": target, "estimated_input_tokens": estimateContextTokens(prompt),
			"token_estimator": "lexical_subword_v1", "records": len(records), "target_record": records[targetIndex].id,
		},
		Answer: "ANSWER: " + answer,
	}, nil
}

func contextQuestion(records []contextRecord, targetIndex int, variant string, rng *rand.Rand) (string, string) {
	target := records[targetIndex]
	switch variant {
	case "multi_needle":
		indices := []int{len(records) / 8, len(records) / 2, len(records) * 7 / 8}
		badges, offices := make([]string, len(indices)), make([]string, len(indices))
		for i, index := range indices {
			badges[i] = strconv.Itoa(records[index].badge)
			offices[i] = records[index].office
		}
		return fmt.Sprintf("问题：按徽章号 %s 的顺序分别查询办公室。仅输出 ANSWER: 后的三个办公室，用英文逗号连接，不留空格。", strings.Join(badges, "、")), strings.Join(offices, ",")
	case "multi_hop":
		byID := make(map[string]contextRecord, len(records))
		for _, record := range records {
			byID[record.id] = record
		}
		manager := byID[byID[target.manager].manager]
		return fmt.Sprintf("问题：徽章号为 %d 的员工，其经理的经理的办公室是什么？仅输出 ANSWER: 办公室。", target.badge), manager.office
	case "aggregate":
		year := 2010 + rng.IntN(9)
		count := 0
		for _, record := range records {
			if record.department == "Archives" && record.joined < year {
				count++
			}
		}
		return fmt.Sprintf("问题：Archives 部门中 Joined 严格小于 %d 的记录有多少条？仅输出 ANSWER: 整数。", year), strconv.Itoa(count)
	case "edges":
		return "问题：最先与最后一条记录的徽章号之和是多少？仅输出 ANSWER: 整数。", strconv.Itoa(records[0].badge + records[len(records)-1].badge)
	case "associative":
		return fmt.Sprintf("问题：暗号 Code 为 %s 的员工所处办公室是什么？先由暗号定位员工，再查办公室。仅输出 ANSWER: 办公室。", target.code), target.office
	default:
		panic("长上下文变体已校验")
	}
}
