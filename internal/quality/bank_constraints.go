package quality

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"unicode"
)

// 参考文本只证明约束可同时满足，不出现在题面中，也不参与逐字判分。
func generateConstraints(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	lang, err := bankString(parameters, "lang", "zh")
	if err != nil {
		return generatedProbe{}, err
	}
	var probe generatedProbe
	switch lang {
	case "zh":
		probe = generateChineseConstraints(rng)
	case "en":
		probe = generateEnglishConstraints(rng)
	default:
		return generatedProbe{}, fmt.Errorf("约束题语言须为 zh 或 en")
	}
	result, err := gradeResult(probe.Grader, ProbeResult{HTTPStatus: 200, Text: probe.Answer}, "")
	if err != nil || !result.Pass {
		return generatedProbe{}, fmt.Errorf("约束参考文本未通过可满足性自检：%v", err)
	}
	// 每条规则均需要有单独失效的见证，避免重复规则或恒成立规则冒充难度。
	for i, rule := range probe.Grader.Checks {
		mutant, err := constraintSingleViolation(probe, lang, rule.Kind)
		if err != nil {
			return generatedProbe{}, err
		}
		for j, other := range probe.Grader.Checks {
			if checkConstraint(mutant, other) != (i != j) {
				return generatedProbe{}, fmt.Errorf("约束 %s 的单项违例自检失败：影响 %s", rule.Kind, other.Kind)
			}
		}
		result, err := gradeResult(probe.Grader, ProbeResult{HTTPStatus: 200, Text: mutant}, "")
		if err != nil || result.Pass {
			return generatedProbe{}, fmt.Errorf("约束 %s 的违例未被判分器拒绝：%v", rule.Kind, err)
		}
	}
	return probe, nil
}

func constraintInt(n int) *int { return &n }

func constraintHanCount(text string) int {
	n := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}

func generateChineseConstraints(rng *rand.Rand) generatedProbe {
	keyword := []string{"灯笼", "街灯", "烛光"}[rng.IntN(3)]
	occurrences := 2 + rng.IntN(2)
	paragraphs := []string{
		"桥边清水映出浅云，旅人沿岸慢行，青石传来轻响。远处KEY照亮窄巷，孩子轻声谈笑，老人停步听雨。",
		"河畔庭院飘来茶香，竹椅依窗摆放，邻里分享新米。门前KEY泛起暖光，小猫伏在石阶，晚钟悠悠回响。",
		"巷口夜色渐深，小镇一片安静，归人慢步走进家门。夜间KEY照亮长街，朋友互道平安，脚步渐远融入暮色。",
	}
	// 关键词个数、段落顺序、窄区间和逐句上限一起变化。
	for i := range paragraphs {
		replacement := keyword
		if i >= occurrences {
			replacement = "月光"
		}
		paragraphs[i] = strings.ReplaceAll(paragraphs[i], "KEY", replacement)
	}
	rng.Shuffle(len(paragraphs), func(i, j int) { paragraphs[i], paragraphs[j] = paragraphs[j], paragraphs[i] })
	answer := strings.Join(paragraphs, "\n\n") + "——完"
	initials := make([]string, len(paragraphs))
	maxSentence := 0
	for i, paragraph := range paragraphs {
		initials[i] = string([]rune(paragraph)[0])
		for _, sentence := range strings.Split(paragraph, "。") {
			maxSentence = max(maxSentence, constraintHanCount(sentence))
		}
	}
	maxSentence += rng.IntN(3)
	count := constraintHanCount(answer)
	low, high := count-1-rng.IntN(2), count+1+rng.IntN(2)
	checks := []Constraint{
		{Kind: "paragraph_shape", N: constraintInt(3), Initials: initials, Sep: "blank", Language: "zh"},
		{Kind: "han_count", Min: constraintInt(low), Max: constraintInt(high)},
		{Kind: "substr_count", Text: keyword, N: constraintInt(occurrences)},
		{Kind: "forbid_chars", Chars: "的"},
		{Kind: "sentence_han_count", Max: constraintInt(maxSentence), Language: "zh"},
		{Kind: "no_ascii_alnum"},
		{Kind: "ends_with", Text: "——完"},
	}
	prompt := fmt.Sprintf("自由写作一篇描写虚构小镇日常的短文，自行选择词句，不加标题或说明。必须同时满足以下七条规则：\n"+
		"1. 恰好三段，空行分段；每段第一个字依次为“%s”“%s”“%s”。\n"+
		"2. 全文汉字数在 %d 到 %d 之间，标点与空白不计，结尾中的汉字也计入。\n"+
		"3. 连续字符串“%s”恰好出现 %d 次。\n"+
		"4. 不能出现“的”字。\n"+
		"5. 以中文句号、问号或感叹号分句，每句至多 %d 个汉字；未加句末标点的末尾部分也算一句。\n"+
		"6. 不能出现 ASCII 英文字母或数字。\n"+
		"7. 全文必须以“——完”结尾。\n只输出最终短文，不输出推理过程。",
		initials[0], initials[1], initials[2], low, high, keyword, occurrences, maxSentence)
	return generatedProbe{
		Request: ProbeRequest{Prompt: prompt}, Grader: Grader{Type: "constraints", GraderOptions: GraderOptions{Checks: checks}},
		Parameters: map[string]any{"lang": "zh", "constraint_count": len(checks), "paragraphs": 3, "initials": initials,
			"keyword": keyword, "target_occurrences": occurrences, "min_han": low, "max_han": high, "sentence_max_han": maxSentence},
		Answer: answer,
	}
}

func generateEnglishConstraints(rng *rand.Rand) generatedProbe {
	keyword := []string{"lantern", "beacon", "candle"}[rng.IntN(3)]
	occurrences := 2 + rng.IntN(2)
	paragraphs := []string{
		"bridges stretch across quiet water as KEY reflects beneath weathered arches. patient travelers pause near carved stones before walking toward distant market stalls. evening air carries faint music through sheltered lanes beside open windows.",
		"rivers wind around green banks where KEY guides careful visitors after sunset. small boats drift past willow trees while silver ripples soften each passing sound. families share warm bread under calm skies then exchange stories about recent journeys.",
		"roads lead beyond stone gates as KEY brightens the way through gentle hills. curious children collect smooth pebbles until twilight encourages everyone back inside. neighbors greet returning friends with quiet smiles before doors close slowly.",
	}
	for i := range paragraphs {
		replacement := keyword
		if i >= occurrences {
			replacement = "sunlight"
		}
		paragraphs[i] = strings.ReplaceAll(paragraphs[i], "KEY", replacement)
	}
	rng.Shuffle(len(paragraphs), func(i, j int) { paragraphs[i], paragraphs[j] = paragraphs[j], paragraphs[i] })
	answer := strings.Join(paragraphs, "\n***\n") + " safe travels."
	initials := make([]string, len(paragraphs))
	maxSentence := 0
	for i, paragraph := range paragraphs {
		initials[i] = strings.Fields(paragraph)[0]
		for _, sentence := range strings.Split(paragraph, ".") {
			maxSentence = max(maxSentence, len(englishWords.FindAllString(sentence, -1)))
		}
	}
	maxSentence += rng.IntN(3)
	count := len(englishWords.FindAllString(answer, -1))
	low, high := count-1-rng.IntN(2), count+1+rng.IntN(2)
	checks := []Constraint{
		{Kind: "paragraph_shape", N: constraintInt(3), Initials: initials, Sep: "***", Language: "en"},
		{Kind: "word_count", Min: constraintInt(low), Max: constraintInt(high)},
		{Kind: "keyword_count", Word: keyword, N: constraintInt(occurrences)},
		{Kind: "forbid_chars", Chars: ","},
		{Kind: "sentence_max_words", Max: constraintInt(maxSentence), Language: "en"},
		{Kind: "max_word_repeat", Max: constraintInt(3)},
		{Kind: "all_lowercase"},
		{Kind: "ends_sentence", Text: "safe travels."},
	}
	prompt := fmt.Sprintf("Freely write a travel note about an imaginary river town. Choose your own sentences. Follow all eight rules:\n"+
		"1. Exactly three paragraphs separated by a line containing only ***. Their first words must be %s, %s, and %s in that order.\n"+
		"2. Use between %d and %d English words in total. A word is a contiguous sequence of ASCII letters with an optional internal apostrophe; punctuation is not a word.\n"+
		"3. Use the word %s exactly %d times.\n"+
		"4. Do not use commas.\n"+
		"5. Every sentence has at most %d words. Periods, question marks, and exclamation marks followed by whitespace or end of text mark sentence boundaries; any trailing fragment also counts.\n"+
		"6. No word may occur more than three times in the whole response, ignoring case.\n"+
		"7. All letters must be lowercase.\n"+
		"8. End the whole response with the exact sentence safe travels.\nOutput only the note. Do not describe your reasoning.",
		initials[0], initials[1], initials[2], low, high, keyword, occurrences, maxSentence)
	return generatedProbe{
		Request: ProbeRequest{Prompt: prompt}, Grader: Grader{Type: "constraints", GraderOptions: GraderOptions{Checks: checks}},
		Parameters: map[string]any{"lang": "en", "constraint_count": len(checks), "paragraphs": 3, "initials": initials,
			"keyword": keyword, "target_occurrences": occurrences, "min_words": low, "max_words": high, "sentence_max_words": maxSentence, "max_word_repeat": 3},
		Answer: answer,
	}
}

// 变体只用于生成时验证约束独立性，不随题面发送，也不作为合法答案限制。
func constraintSingleViolation(probe generatedProbe, lang, kind string) (string, error) {
	text := probe.Answer
	keyword := probe.Parameters["keyword"].(string)
	if lang == "zh" {
		switch kind {
		case "paragraph_shape":
			return "坊" + string([]rune(text)[1:]), nil
		case "han_count":
			return strings.TrimSuffix(text, "——完") + "山青水秀。——完", nil
		case "substr_count":
			return strings.Replace(text, keyword, "暖意", 1), nil
		case "forbid_chars":
			return strings.Replace(text, "青石", "的石", 1), nil
		case "sentence_han_count":
			return strings.Replace(text, "。", "，", 1), nil
		case "no_ascii_alnum":
			return strings.Replace(text, "，", "x，", 1), nil
		case "ends_with":
			return strings.TrimSuffix(text, "完") + "毕", nil
		}
	} else {
		switch kind {
		case "paragraph_shape":
			first := strings.Fields(text)[0]
			return strings.Replace(text, first, "gardens", 1), nil
		case "word_count":
			return strings.TrimSuffix(text, "safe travels.") + "bright notes greet patient visitors beside old towers without haste. safe travels.", nil
		case "keyword_count":
			return strings.Replace(text, keyword, "lamplight", 1), nil
		case "forbid_chars":
			return strings.Replace(text, "quiet", "quiet,", 1), nil
		case "sentence_max_words":
			return strings.Replace(text, ". ", " ", 1), nil
		case "max_word_repeat":
			for _, word := range []string{"weathered", "carved", "sheltered", "green"} {
				text = strings.Replace(text, word, "hush", 1)
			}
			return text, nil
		case "all_lowercase":
			return strings.Replace(text, "quiet", "Quiet", 1), nil
		case "ends_sentence":
			return strings.TrimSuffix(text, "travels.") + "journeys.", nil
		}
	}
	return "", fmt.Errorf("未登记约束 %s 的单项违例构造器", kind)
}
