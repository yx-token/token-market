package quality

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBankStringDateIndependentSolvers(t *testing.T) {
	for _, kind := range []string{"transform", "sort", "date"} {
		t.Run(kind, func(t *testing.T) {
			for seed := uint64(0); seed < 40; seed++ {
				got := bankNumericVerifyDeterminism(t, generateStringDate, map[string]any{"kind": kind}, seed)
				bankNumericVerifyAnswer(t, got, bankStringDateIndependent(t, kind, got.Request.Prompt))
			}
		})
	}
}

func bankStringDateIndependent(t *testing.T, kind, prompt string) string {
	t.Helper()
	switch kind {
	case "transform":
		match := regexp.MustCompile(`原字符串为 ([a-z]+)。`).FindStringSubmatch(prompt)
		if match == nil {
			t.Fatal("字符串题缺少源字符串")
		}
		shift := bankNumericRenderedInts(t, `移动 (\d+) 位`, prompt)[0]
		source, answer := match[1], ""
		for position := 1; position < len(source); position += 2 {
			// 从原文直接定位被保留的字符，不构造反转或替换中间串。
			letter := source[len(source)-1-position]
			if at := strings.IndexByte("aeiou", letter); at >= 0 {
				letter = "eioua"[at]
			}
			alphabetIndex := strings.IndexByte("abcdefghijklmnopqrstuvwxyz", letter)
			answer += string("abcdefghijklmnopqrstuvwxyz"[(alphabetIndex+shift)%26])
		}
		return answer
	case "sort":
		match := regexp.MustCompile(`自定义字母表从小到大为 ([a-z]+)。.*?排序：([a-z ]+)。`).FindStringSubmatch(prompt)
		if match == nil || len(match[1]) != 26 {
			t.Fatal("排序题的字母表或单词缺失")
		}
		alphabet := match[1]
		less := func(a, b string) bool {
			for i := 0; i < len(a) && i < len(b); i++ {
				if a[i] != b[i] {
					return strings.IndexByte(alphabet, a[i]) < strings.IndexByte(alphabet, b[i])
				}
			}
			return len(a) < len(b)
		}
		answer := []string{}
		for _, word := range strings.Fields(match[2]) {
			at := 0
			for at < len(answer) && less(answer[at], word) {
				at++
			}
			answer = append(answer, "")
			copy(answer[at+1:], answer[at:])
			answer[at] = word
		}
		return strings.Join(answer, " ")
	case "date":
		startMatch := regexp.MustCompile(`今天是 (\d{4}-\d{2}-\d{2})。`).FindStringSubmatch(prompt)
		holidayMatch := regexp.MustCompile(`节假日为 ([0-9、-]+)。`).FindStringSubmatch(prompt)
		fallMatch := regexp.MustCompile(`纽约在 (\d{4}-\d{2}-\d{2}) 当地 02:00`).FindStringSubmatch(prompt)
		if startMatch == nil || holidayMatch == nil || fallMatch == nil {
			t.Fatal("日期题缺少日期或节假日")
		}
		v := bankNumericRenderedInts(t, `第 (\d+) 个工作日.*?经过 (\d+) 个绝对小时`, prompt)
		year, month, day := bankCivilParse(t, startMatch[1])
		startOrdinal := bankCivilOrdinal(year, month, day)
		holidaySet := map[int]bool{}
		for _, date := range strings.Split(holidayMatch[1], "、") {
			y, m, d := bankCivilParse(t, date)
			holidaySet[bankCivilOrdinal(y, m, d)] = true
		}
		deadline := startOrdinal
		for count := 0; count < v[0]; {
			deadline++
			if deadline%7 < 5 && !holidaySet[deadline] {
				count++
			}
		}
		// 公历 0001-01-01 是周一，用整日序数独立算星期与夏令时切换。
		fall := bankCivilOrdinal(year, 11, 1)
		for fall%7 != 6 {
			fall++
		}
		if bankCivilFormat(fall) != fallMatch[1] {
			t.Fatal("题面夏令时结束日期不是十一月第一个周日")
		}
		offset := -4
		if deadline >= fall {
			offset = -5
		}
		utcMinutes := deadline*1440 + 9*60 + 30 - offset*60 + v[1]*60
		endOffset := -4
		if utcMinutes >= fall*1440+6*60 {
			endOffset = -5
		}
		localMinutes := utcMinutes + endOffset*60
		return fmt.Sprintf("DEADLINE=%s; CALL=%s %02d:%02d", bankCivilFormat(deadline), bankCivilFormat(localMinutes/1440), localMinutes%1440/60, localMinutes%60)
	}
	t.Fatalf("验算器没有该题型：%s", kind)
	return ""
}

func bankCivilParse(t *testing.T, date string) (int, int, int) {
	t.Helper()
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		t.Fatal("非法公历日期")
	}
	values := [3]int{}
	for i := range values {
		var err error
		values[i], err = strconv.Atoi(parts[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	return values[0], values[1], values[2]
}

func bankCivilMonthLength(year, month int) int {
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month-1]
	if month == 2 && (year%400 == 0 || year%4 == 0 && year%100 != 0) {
		days++
	}
	return days
}

func bankCivilOrdinal(year, month, day int) int {
	y := year - 1
	ordinal := y*365 + y/4 - y/100 + y/400 + day - 1
	for m := 1; m < month; m++ {
		ordinal += bankCivilMonthLength(year, m)
	}
	return ordinal
}

func bankCivilFormat(ordinal int) string {
	year := 1
	for {
		days := 365
		if bankCivilMonthLength(year, 2) == 29 {
			days++
		}
		if ordinal < days {
			break
		}
		ordinal -= days
		year++
	}
	month := 1
	for ordinal >= bankCivilMonthLength(year, month) {
		ordinal -= bankCivilMonthLength(year, month)
		month++
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, ordinal+1)
}

func TestBankBusinessDateTimezoneCrossCheck(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("本机没有纽约时区资料；公历序数验算仍完整执行")
	}
	for seed := uint64(0); seed < 40; seed++ {
		got, err := generateStringDate(bankNumericTestRNG(seed), map[string]any{"kind": "date"})
		if err != nil {
			t.Fatal(err)
		}
		deadline, err := time.ParseInLocation("2006-01-02 15:04", got.Grader.Fields["DEADLINE"]+" 09:30", location)
		if err != nil {
			t.Fatal(err)
		}
		want := deadline.Add(time.Duration(got.Parameters["elapsed_hours"].(int)) * time.Hour).Format("2006-01-02 15:04")
		if got.Grader.Fields["CALL"] != want {
			t.Fatalf("题面明示偏移与系统时区不一致：%v %s", got.Grader.Fields, want)
		}
	}
}

func TestBankStringDateDomains(t *testing.T) {
	for _, p := range []map[string]any{
		{"kind": "transform", "length": 14, "shift": 1},
		{"kind": "transform", "length": 30, "shift": 25},
		{"kind": "date", "year": 2024, "start_day": 1, "business_days": 5, "elapsed_hours": 100, "holiday_count": 1},
		{"kind": "date", "year": 2028, "start_day": 24, "business_days": 15, "elapsed_hours": 400, "holiday_count": 3},
		{"kind": "date", "business_days": []int{5, 15}},
	} {
		got := bankNumericVerifyDeterminism(t, generateStringDate, p, 34)
		bankNumericVerifyAnswer(t, got, bankStringDateIndependent(t, p["kind"].(string), got.Request.Prompt))
	}
	for _, p := range []map[string]any{{"kind": "unknown"}, {"kind": "transform", "length": 31}, {"kind": "transform", "shift": 0}, {"kind": "date", "year": 2023}, {"kind": "date", "start_day": 0}, {"kind": "date", "business_days": 4}, {"kind": "date", "elapsed_hours": 99}, {"kind": "date", "holiday_count": 4}} {
		if _, err := generateStringDate(bankNumericTestRNG(1), p); err == nil {
			t.Fatalf("非法参数被接受：%v", p)
		}
	}
}
