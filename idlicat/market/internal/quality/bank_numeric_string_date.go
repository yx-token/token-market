package quality

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

func init() {
	bankGenerators["string_date"] = generateStringDate
}

func generateStringDate(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	kinds := []string{"transform", "date", "sort"}
	kind, err := bankString(p, "kind", kinds[rng.IntN(len(kinds))])
	if err != nil {
		return generatedProbe{}, err
	}
	switch kind {
	case "transform":
		return bankStringTransform(rng, p)
	case "sort":
		return bankStringSort(rng), nil
	case "date":
		return bankBusinessDate(rng, p)
	default:
		return generatedProbe{}, fmt.Errorf("未知字符串日期题类型：%s", kind)
	}
}

func bankStringTransform(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	length, err := bankNumericPick(p, "length", 14, 30, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	shift, err := bankNumericPick(p, "shift", 1, 25, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	source := make([]byte, length)
	for i := range source {
		source[i] = 'a' + byte(rng.IntN(26))
	}
	transformed := slices.Clone(source)
	slices.Reverse(transformed)
	vowels := map[byte]byte{'a': 'e', 'e': 'i', 'i': 'o', 'o': 'u', 'u': 'a'}
	answer := make([]byte, 0, length/2)
	for i, ch := range transformed {
		if mapped, ok := vowels[ch]; ok {
			ch = mapped
		}
		if i%2 == 1 {
			answer = append(answer, byte((int(ch-'a')+shift)%26)+'a')
		}
	}
	prompt := fmt.Sprintf("原字符串为 %s。依次进行：①整体反转；②同时替换元音 a→e、e→i、i→o、o→u、u→a，其余字母不变，不重复替换；③只保留替换后字符串的第 2、4、6……个字符（从 1 开始编号）；④每个字母沿 a 至 z 循环向后移动 %d 位。只输出最终字符串。", source, shift)
	return generatedProbe{Request: ProbeRequest{Prompt: prompt}, Grader: Grader{Type: "exact", Answer: string(answer)}, Parameters: map[string]any{"kind": "transform", "source": string(source), "length": length, "shift": shift}, Answer: string(answer)}, nil
}

func bankStringSort(rng *rand.Rand) generatedProbe {
	alphabet := []byte("abcdefghijklmnopqrstuvwxyz")
	rng.Shuffle(len(alphabet), func(i, j int) { alphabet[i], alphabet[j] = alphabet[j], alphabet[i] })
	pool := []string{"delta", "lumen", "quartz", "vivid", "nectar", "basalt", "tundra", "glyph", "ember", "jovial", "cipher", "zenith"}
	words := []string{"kernel", "orbit", "kern", "orb"}
	for _, i := range rng.Perm(len(pool))[:8] {
		words = append(words, pool[i])
	}
	rng.Shuffle(len(words), func(i, j int) { words[i], words[j] = words[j], words[i] })
	ranks := [26]int{}
	for i, ch := range alphabet {
		ranks[ch-'a'] = i
	}
	sorted := slices.Clone(words)
	slices.SortFunc(sorted, func(a, b string) int {
		for i := range min(len(a), len(b)) {
			if a[i] != b[i] {
				return ranks[a[i]-'a'] - ranks[b[i]-'a']
			}
		}
		return len(a) - len(b)
	})
	answer := strings.Join(sorted, " ")
	prompt := fmt.Sprintf("自定义字母表从小到大为 %s。按此字母表对这些单词做字典序排序：%s。比较到首个不同字母时按自定义字母表决定顺序；若一个词是另一个词的前缀，则短词在前。只输出排序后的全部单词，用单个空格分隔。", alphabet, strings.Join(words, " "))
	return generatedProbe{Request: ProbeRequest{Prompt: prompt}, Grader: Grader{Type: "exact", Answer: answer}, Parameters: map[string]any{"kind": "sort", "alphabet": string(alphabet), "words": words}, Answer: answer}
}

func bankBusinessDate(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	year, err := bankNumericPick(p, "year", 2024, 2028, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	day, err := bankNumericPick(p, "start_day", 1, 24, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	days, err := bankNumericPick(p, "business_days", 5, 15, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	hours, err := bankNumericPick(p, "elapsed_hours", 100, 400, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	holidayCount, err := bankNumericPick(p, "holiday_count", 1, 3, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	start := time.Date(year, time.October, day, 0, 0, 0, 0, time.UTC)
	holidaySet := map[string]bool{}
	for _, offset := range rng.Perm(30) {
		date := start.AddDate(0, 0, offset+1)
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			holidaySet[date.Format(time.DateOnly)] = true
			if len(holidaySet) == holidayCount {
				break
			}
		}
	}
	holidayDates := make([]string, 0, holidayCount)
	for date := range holidaySet {
		holidayDates = append(holidayDates, date)
	}
	slices.Sort(holidayDates)
	deadline := start
	for passed := 0; passed < days; {
		deadline = deadline.AddDate(0, 0, 1)
		if deadline.Weekday() != time.Saturday && deadline.Weekday() != time.Sunday && !holidaySet[deadline.Format(time.DateOnly)] {
			passed++
		}
	}
	fall := time.Date(year, time.November, 1, 0, 0, 0, 0, time.UTC)
	fall = fall.AddDate(0, 0, (7-int(fall.Weekday()))%7)
	// 题目都在秋季，给出切换规则，避免把日期推理变成时区知识测试。
	startOffset := -4
	if !deadline.Before(fall) {
		startOffset = -5
	}
	startUTC := deadline.Add(9*time.Hour + 30*time.Minute - time.Duration(startOffset)*time.Hour)
	endUTC := startUTC.Add(time.Duration(hours) * time.Hour)
	endOffset := -4
	if !endUTC.Before(fall.Add(6 * time.Hour)) {
		endOffset = -5
	}
	endLocal := endUTC.Add(time.Duration(endOffset) * time.Hour)
	deadlineText, callText := deadline.Format(time.DateOnly), endLocal.Format("2006-01-02 15:04")
	prompt := fmt.Sprintf("今天是 %s。周一至周五为工作日，节假日为 %s。今天不计入，从明天开始计算第 %d 个工作日作为截止日期。以截止日期纽约当地 09:30 为起点，再经过 %d 个绝对小时。纽约在 %s 当地 02:00 从 UTC−04:00 切换为 UTC−05:00，回拨到 01:00；这题涉及的其余秋季日期在切换前使用 UTC−04:00，切换后使用 UTC−05:00。请给出截止日期及经过时长后的纽约当地时间，只输出 DEADLINE=YYYY-MM-DD; CALL=YYYY-MM-DD HH:MM。", start.Format(time.DateOnly), strings.Join(holidayDates, "、"), days, hours, fall.Format(time.DateOnly))
	return generatedProbe{
		Request:    ProbeRequest{Prompt: prompt},
		Grader:     Grader{Type: "fields_exact", GraderOptions: GraderOptions{Inline: true, Fields: map[string]string{"DEADLINE": deadlineText, "CALL": callText}}},
		Parameters: map[string]any{"kind": "date", "start_date": start.Format(time.DateOnly), "business_days": days, "elapsed_hours": hours, "holidays": holidayDates, "timezone": "America/New_York", "dst_end_date": fall.Format(time.DateOnly)},
		Answer:     "DEADLINE=" + deadlineText + "; CALL=" + callText,
	}, nil
}
