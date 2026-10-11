package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func bankNumericTestRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed+7919)) //nolint:gosec // 单元测试要求可复现的固定种子。
}

func bankNumericRenderedInts(t *testing.T, pattern, text string) []int {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("无法从题面独立读取参数：%s", text)
	}
	values := make([]int, len(match)-1)
	for i, value := range match[1:] {
		var err error
		values[i], err = strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	return values
}

func bankNumericVerifyAnswer(t *testing.T, got generatedProbe, want string) {
	t.Helper()
	if got.Answer != want {
		t.Fatalf("两种独立方法答案不一致：标准=%q 独立=%q\n%s", got.Answer, want, got.Request.Prompt)
	}
	result, err := gradeResult(got.Grader, ProbeResult{HTTPStatus: 200, Text: got.Answer, Flags: []string{"stop_normal"}}, "")
	if err != nil || !result.Pass {
		t.Fatalf("标准答案不能通过自己的判分器：%+v %v", result, err)
	}
}

func bankNumericVerifyDeterminism(t *testing.T, fn func(*rand.Rand, map[string]any) (generatedProbe, error), parameters map[string]any, seed uint64) generatedProbe {
	t.Helper()
	a, err := fn(bankNumericTestRNG(seed), parameters)
	if err != nil {
		t.Fatal(err)
	}
	b, err := fn(bankNumericTestRNG(seed), parameters)
	if err != nil {
		t.Fatal(err)
	}
	aa, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(aa) != sha256.Sum256(bb) {
		t.Fatal("同 seed 生成的冻结实例哈希不一致")
	}
	return a
}

func TestBankMathIndependentSolvers(t *testing.T) {
	for _, kind := range []string{"count", "lastdigits", "binary", "grid"} {
		t.Run(kind, func(t *testing.T) {
			for seed := uint64(0); seed < 40; seed++ {
				got := bankNumericVerifyDeterminism(t, generateMath, map[string]any{"kind": kind}, seed)
				bankNumericVerifyAnswer(t, got, "ANSWER: "+bankMathIndependent(t, kind, got.Request.Prompt))
			}
		})
	}
}

// 验算器只从渲染题面读参，避免参数与题面的写入错误被同一答案算法遮住。
func bankMathIndependent(t *testing.T, kind, prompt string) string {
	t.Helper()
	switch kind {
	case "count":
		v := bankNumericRenderedInts(t, `1 ≤ n ≤ (\d+)；n 除以 (\d+) 的余数为 (\d+)；十进制数位和能被 (\d+) 整除；n 不能被 (\d+) 整除`, prompt)
		answer := 0
		for n := 1; n <= v[0]; n++ {
			digitSum := 0
			for _, ch := range strconv.Itoa(n) {
				digitSum += int(ch - '0')
			}
			if n%v[1] == v[2] && digitSum%v[3] == 0 && n%v[4] != 0 {
				answer++
			}
		}
		return strconv.Itoa(answer)
	case "lastdigits":
		v := bankNumericRenderedInts(t, `求 (\d+)\^\((\d+)\^(\d+)\)`, prompt)
		e := 1
		for range v[2] {
			e *= v[1]
		}
		// 独立使用两个互素模数的周期及 CRT，不调用快速幂实现。
		x8 := 1
		if e%2 == 1 {
			x8 = v[0] % 8
		}
		x125 := 1
		for range e % 100 {
			x125 = x125 * v[0] % 125
		}
		for n := range 1000 {
			if n%8 == x8 && n%125 == x125 {
				return fmt.Sprintf("%03d", n)
			}
		}
		t.Fatal("CRT 没有解")
	case "binary":
		v := bankNumericRenderedInts(t, `长度为 (\d+) 的二进制字符串中，恰好含有 (\d+) 个 1，且从不出现连续 (\d+) 个 1`, prompt)
		answer := 0
		for mask := range 1 << v[0] {
			if bits.OnesCount(uint(mask)) != v[1] {
				continue
			}
			runs := mask
			for shift := 1; shift < v[2]; shift++ {
				runs &= mask >> shift
			}
			if runs == 0 {
				answer++
			}
		}
		return strconv.Itoa(answer)
	case "grid":
		v := bankNumericRenderedInts(t, `走到 \((\d+),(\d+)\)`, prompt)
		blockedText := strings.Split(strings.Split(prompt, "阻塞点 ")[1], "。")[0]
		points := [][2]int{}
		for _, match := range regexp.MustCompile(`\((\d+),(\d+)\)`).FindAllStringSubmatch(blockedText, -1) {
			x, _ := strconv.Atoi(match[1])
			y, _ := strconv.Atoi(match[2])
			points = append(points, [2]int{x, y})
		}
		slices.SortFunc(points, func(a, b [2]int) int {
			if a[0] != b[0] {
				return a[0] - b[0]
			}
			return a[1] - b[1]
		})
		points = append(points, [2]int{v[0], v[1]})
		paths := func(a, b [2]int) int {
			dx, dy := b[0]-a[0], b[1]-a[1]
			if dx < 0 || dy < 0 {
				return 0
			}
			answer := 1
			for i := 1; i <= dx; i++ {
				answer = answer * (dy + i) / i
			}
			return answer
		}
		// 障碍点按坐标排序，用“先经过哪个障碍”的容斥计算。
		counts := make([]int, len(points))
		for i, point := range points {
			counts[i] = paths([2]int{}, point)
			for j := 0; j < i; j++ {
				counts[i] -= counts[j] * paths(points[j], point)
			}
		}
		return strconv.Itoa(counts[len(counts)-1])
	}
	t.Fatalf("验算器没有该题型：%s", kind)
	return ""
}

func TestBankMathDomains(t *testing.T) {
	for _, p := range []map[string]any{
		{"kind": "count", "n": 800, "modulus": 6, "remainder": 0, "digit_modulus": 3, "excluded_multiple": 5},
		{"kind": "count", "n": 2000, "modulus": 13, "remainder": 12, "digit_modulus": 7, "excluded_multiple": 11},
		{"kind": "lastdigits", "base": 99, "exponent_base": 9, "exponent_power": 6},
		{"kind": "binary", "length": 14, "ones": 5, "run_limit": 3},
		{"kind": "binary", "length": 18, "ones": 8, "run_limit": 4},
		{"kind": "grid", "width": 6, "height": 5, "blocked_count": 4},
		{"kind": "grid", "width": 8, "height": 7, "blocked_count": 6},
		{"kind": "count", "n": []int{800, 2000}},
	} {
		got := bankNumericVerifyDeterminism(t, generateMath, p, 33)
		bankNumericVerifyAnswer(t, got, "ANSWER: "+bankMathIndependent(t, p["kind"].(string), got.Request.Prompt))
	}
	for _, p := range []map[string]any{{"kind": "missing"}, {"kind": "count", "n": 799}, {"kind": "count", "modulus": 6, "remainder": 6}, {"kind": "lastdigits", "base": 10}, {"kind": "binary", "length": 19}, {"kind": "grid", "blocked_count": 7}, {"kind": "grid", "height": "6"}} {
		if _, err := generateMath(bankNumericTestRNG(1), p); err == nil {
			t.Fatalf("非法参数被接受：%v", p)
		}
	}
}
