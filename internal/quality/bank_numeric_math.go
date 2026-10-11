package quality

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

func init() {
	bankGenerators["aime_math"] = generateMath
}

func bankNumericPick(p map[string]any, key string, low, high int, rng *rand.Rand) (int, error) {
	return bankInt(p, key, low+rng.IntN(high-low+1), low, high, rng)
}

func generateMath(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	kinds := []string{"count", "lastdigits", "binary", "grid"}
	kind, err := bankString(p, "kind", kinds[rng.IntN(len(kinds))])
	if err != nil {
		return generatedProbe{}, err
	}
	switch kind {
	case "count":
		return bankMathCount(rng, p)
	case "lastdigits":
		return bankMathLastDigits(rng, p)
	case "binary":
		return bankMathBinary(rng, p)
	case "grid":
		return bankMathGrid(rng, p)
	default:
		return generatedProbe{}, fmt.Errorf("未知数学题类型：%s", kind)
	}
}

func bankMathProbe(prompt, answer string, parameters map[string]any, norm string) generatedProbe {
	return generatedProbe{
		Request:    ProbeRequest{Prompt: prompt + "只输出最终答案，格式为 ANSWER: <答案>。"},
		Grader:     Grader{Type: "answer_tag", Answer: answer, GraderOptions: GraderOptions{Norm: norm}},
		Parameters: parameters,
		Answer:     "ANSWER: " + answer,
	}
}

func bankMathCount(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	n, err := bankNumericPick(p, "n", 800, 2000, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	m, err := bankNumericPick(p, "modulus", 6, 13, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	a, err := bankNumericPick(p, "remainder", 0, m-1, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	k, err := bankNumericPick(p, "digit_modulus", 3, 7, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	exclusions := []int{5, 7, 11}
	excluded, err := bankInt(p, "excluded_multiple", exclusions[rng.IntN(len(exclusions))], 5, 11, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	answer := 0
	first := a
	if first == 0 {
		first = m
	}
	for value := first; value <= n; value += m {
		sum := 0
		for rest := value; rest > 0; rest /= 10 {
			sum += rest % 10
		}
		if sum%k == 0 && value%excluded != 0 {
			answer++
		}
	}
	prompt := fmt.Sprintf("统计满足以下全部条件的整数 n 的个数：1 ≤ n ≤ %d；n 除以 %d 的余数为 %d；十进制数位和能被 %d 整除；n 不能被 %d 整除。", n, m, a, k, excluded)
	return bankMathProbe(prompt, strconv.Itoa(answer), map[string]any{"kind": "count", "n": n, "modulus": m, "remainder": a, "digit_modulus": k, "excluded_multiple": excluded}, "int"), nil
}

func bankMathLastDigits(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	bases := []int{3, 7, 13, 17, 23, 27, 37}
	a, err := bankInt(p, "base", bases[rng.IntN(len(bases))], 3, 99, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	if a%2 == 0 || a%5 == 0 {
		return generatedProbe{}, fmt.Errorf("末三位题的 base 须与 10 互素")
	}
	b, err := bankNumericPick(p, "exponent_base", 3, 9, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	c, err := bankNumericPick(p, "exponent_power", 3, 6, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	exponent := 1
	for range c {
		exponent *= b
	}
	answer := bankModPow(a, exponent, 1000)
	prompt := fmt.Sprintf("求 %d^(%d^%d) 的末三位。幂运算从右向左结合，答案必须是保留前导零的三位字符串。", a, b, c)
	return bankMathProbe(prompt, fmt.Sprintf("%03d", answer), map[string]any{"kind": "lastdigits", "base": a, "exponent_base": b, "exponent_power": c}, "str"), nil
}

func bankModPow(base, exponent, modulus int) int {
	answer := 1
	for exponent > 0 {
		if exponent%2 == 1 {
			answer = answer * base % modulus
		}
		base = base * base % modulus
		exponent /= 2
	}
	return answer
}

func bankMathBinary(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	n, err := bankNumericPick(p, "length", 14, 18, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	ones, err := bankNumericPick(p, "ones", 5, 8, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	k, err := bankNumericPick(p, "run_limit", 3, 4, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	// 状态按已用 1 的个数及末尾连续 1 的长度计数。
	dp := make([][]int, ones+1)
	for i := range dp {
		dp[i] = make([]int, k)
	}
	dp[0][0] = 1
	for range n {
		next := make([][]int, ones+1)
		for used := range next {
			next[used] = make([]int, k)
		}
		for used, runs := range dp {
			for run, count := range runs {
				next[used][0] += count
				if used < ones && run+1 < k {
					next[used+1][run+1] += count
				}
			}
		}
		dp = next
	}
	answer := 0
	for _, count := range dp[ones] {
		answer += count
	}
	prompt := fmt.Sprintf("长度为 %d 的二进制字符串中，恰好含有 %d 个 1，且从不出现连续 %d 个 1 的字符串有多少个？前导零允许。", n, ones, k)
	return bankMathProbe(prompt, strconv.Itoa(answer), map[string]any{"kind": "binary", "length": n, "ones": ones, "run_limit": k}, "int"), nil
}

func bankMathGrid(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	w, err := bankNumericPick(p, "width", 6, 8, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	h, err := bankNumericPick(p, "height", 5, 7, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	count, err := bankNumericPick(p, "blocked_count", 4, 6, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	positions := rng.Perm((w - 1) * (h - 1))[:count]
	blocked := make([][]int, 0, count)
	for x := 1; x < w; x++ {
		for y := 1; y < h; y++ {
			for _, pos := range positions {
				if pos == (x-1)*(h-1)+y-1 {
					blocked = append(blocked, []int{x, y})
				}
			}
		}
	}
	isBlocked := map[[2]int]bool{}
	coordinates := make([]string, len(blocked))
	for i, point := range blocked {
		isBlocked[[2]int{point[0], point[1]}] = true
		coordinates[i] = fmt.Sprintf("(%d,%d)", point[0], point[1])
	}
	dp := make([][]int, w+1)
	for x := range dp {
		dp[x] = make([]int, h+1)
		for y := range dp[x] {
			if isBlocked[[2]int{x, y}] {
				continue
			}
			if x == 0 && y == 0 {
				dp[x][y] = 1
			}
			if x > 0 {
				dp[x][y] += dp[x-1][y]
			}
			if y > 0 {
				dp[x][y] += dp[x][y-1]
			}
		}
	}
	prompt := fmt.Sprintf("机器人从 (0,0) 走到 (%d,%d)，每步只能向右或向上移动一个整数单位，不得访问阻塞点 %s。共有多少条不同路径？", w, h, strings.Join(coordinates, "、"))
	return bankMathProbe(prompt, strconv.Itoa(dp[w][h]), map[string]any{"kind": "grid", "width": w, "height": h, "blocked": blocked, "blocked_count": count}, "int"), nil
}
