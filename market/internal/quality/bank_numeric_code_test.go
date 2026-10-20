package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func bankGeneratedProgram(t *testing.T, probe generatedProbe) string {
	t.Helper()
	parts := strings.Split(probe.Request.Prompt, "```python\n")
	if len(parts) != 2 || !strings.HasSuffix(parts[1], "```") {
		t.Fatal("程序题围栏格式错误")
	}
	return strings.TrimSuffix(parts[1], "```")
}

func bankCodeReadList(t *testing.T, text string) []int {
	t.Helper()
	values := []int{}
	for _, part := range strings.Split(strings.Trim(text, "[]"), ",") {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return values
}

func TestBankCodeIndependentGoSolvers(t *testing.T) {
	for _, kind := range []string{"A", "B", "D", "G"} {
		t.Run(kind, func(t *testing.T) {
			for seed := uint64(0); seed < 40; seed++ {
				got := bankNumericVerifyDeterminism(t, generateCode, map[string]any{"kind": kind, "cases": 3 + int(seed%4)}, seed)
				program := bankGeneratedProgram(t, got)
				if strings.Contains(program, "out[::- ") {
					t.Fatalf("切片步长前不应出现多余空格：%s", program)
				}
				bankNumericVerifyAnswer(t, got, bankCodeIndependent(t, kind, program))
			}
		})
	}
}

func TestBankCodePythonCrossCheck(t *testing.T) {
	t.Parallel()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("本机没有 python3，跳过自产程序的解释器交叉验证")
	}
	type sample struct {
		kind  string
		seed  uint64
		probe generatedProbe
	}
	var samples []sample
	var programs []string
	for _, kind := range []string{"A", "B", "D", "G"} {
		for seed := uint64(0); seed < 12; seed++ {
			got, err := generateCode(bankNumericTestRNG(seed), map[string]any{"kind": kind})
			if err != nil {
				t.Fatal(err)
			}
			samples = append(samples, sample{kind, seed, got})
			programs = append(programs, bankGeneratedProgram(t, got))
		}
	}
	raw, err := json.Marshal(programs)
	if err != nil {
		t.Fatal(err)
	}
	// 只执行本生成器产生的固定程序；每个程序用独立命名空间，避免可变默认参数跨样本污染。
	const batch = `import contextlib, io, json, sys
outputs = []
for program in json.load(sys.stdin):
    output = io.StringIO()
    with contextlib.redirect_stdout(output):
        exec(compile(program, "<generated>", "exec"), {})
    outputs.append(output.getvalue())
print(json.dumps(outputs))
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-I", "-c", batch)
	command.Stdin = strings.NewReader(string(raw))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("自产程序运行失败：%v %s", err, output)
	}
	var answers []string
	if err := json.Unmarshal(output, &answers); err != nil || len(answers) != len(samples) {
		t.Fatalf("批量解释器未返回全部 %d 个程序输出：%v %s", len(samples), err, output)
	}
	for i, sample := range samples {
		t.Run(fmt.Sprintf("%s/%d", sample.kind, sample.seed), func(t *testing.T) {
			bankNumericVerifyAnswer(t, sample.probe, strings.TrimSuffix(answers[i], "\n"))
		})
	}
}

func bankCodeIndependent(t *testing.T, kind, code string) string {
	t.Helper()
	lines := []string{}
	switch kind {
	case "A":
		v := bankNumericRenderedInts(t, `(?s)if x % (\d+) == (\d+):.*?x // (\d+).*?x \* (\d+) - i.*?v % (\d+), -v.*?out\[(\d+):(\d+)\] \+ out\[::-(\d+)\]`, code)
		for _, call := range regexp.MustCompile(`print\(f\((\[[0-9, ]+\])\)\)`).FindAllStringSubmatch(code, -1) {
			// 排序后插入顺序无影响，独立直接算贡献，再按余数分桶。
			buckets := make([][]int, v[4])
			for i, x := range bankCodeReadList(t, call[1]) {
				value := x + i
				if x%v[0] == v[1] {
					value = x / v[2]
				} else if i%2 == 1 {
					value = x*v[3] - i
				}
				rem := value
				for rem < 0 {
					rem += v[4]
				}
				buckets[rem%v[4]] = append(buckets[rem%v[4]], value)
			}
			ordered := []int{}
			for _, bucket := range buckets {
				slices.Sort(bucket)
				for j := len(bucket) - 1; j >= 0; j-- {
					ordered = append(ordered, bucket[j])
				}
			}
			answer := []string{}
			for i, value := range ordered {
				if i >= v[5] && i < v[6] {
					answer = append(answer, strconv.Itoa(value))
				}
			}
			for at, take := len(ordered)-1, 0; at >= 0 && take < 2; at, take = at-v[7], take+1 {
				answer = append(answer, strconv.Itoa(ordered[at]))
			}
			lines = append(lines, "["+strings.Join(answer, ", ")+"]")
		}
	case "B":
		v := bankNumericRenderedInts(t, `(?s)int\(ch\) \* (\d+) \+ i.*?elif i % (\d+) == 0`, code)
		for _, call := range regexp.MustCompile(`print\(g\("([a-z0-9]+)"\)\)`).FindAllStringSubmatch(code, -1) {
			stored, reversed := "", false
			for i, ch := range []byte(call[1]) {
				part, front := "", false
				if ch >= '0' && ch <= '9' {
					part = strconv.Itoa((int(ch-'0')*v[0] + i) % 10)
				} else if strings.ContainsRune("aeiou", rune(ch)) {
					part, front = strings.ToUpper(string(ch)), true
				} else if i%v[1] == 0 {
					part = strings.Repeat(string(ch), 2)
				} else {
					reversed = !reversed
					continue
				}
				if front != reversed {
					stored = part + stored
				} else {
					stored += part
				}
			}
			if reversed {
				buffer := []byte(stored)
				slices.Reverse(buffer)
				stored = string(buffer)
			}
			lines = append(lines, stored)
		}
	case "D":
		n := bankNumericRenderedInts(t, `range\((\d+)\)`, code)[0]
		a := bankNumericRenderedInts(t, `a\[(\d+)\]\[(\d+)\] = (\d+)`, code)
		shared := []int{0, 0, 0}
		rows := [][]int{shared, shared, shared}
		rows[a[0]][a[1]] = a[2]
		matrixSum := 0
		for _, row := range rows {
			for _, value := range row {
				matrixSum += value
			}
		}
		bucket := []int{}
		for _, call := range regexp.MustCompile(`print\(acc\((\d+)\), fs\[0\]\((\d+)\), sum\(map\(sum, a\)\), (-\d+) // (\d+), (-\d+) % (\d+), round\((\d+)\.5\)\)`).FindAllStringSubmatch(code, -1) {
			v := make([]int, 7)
			for i := range v {
				v[i], _ = strconv.Atoi(call[i+1])
			}
			bucket = append(bucket, v[0])
			accumulator := 0
			for _, x := range bucket {
				accumulator += x
			}
			quotient, remainder := 0, v[2]
			for remainder < 0 {
				remainder += v[3]
				quotient--
			}
			if v[2] != v[4] || v[3] != v[5] {
				t.Fatal("题面整除与余数参数不一致")
			}
			rounded := v[6]
			if rounded%2 == 1 {
				rounded++
			}
			lines = append(lines, fmt.Sprintf("%d %d %d %d %d %d", accumulator, v[1]*(n-1), matrixSum, quotient, remainder, rounded))
		}
		if !strings.Contains(code, "print(7 // 0)") || !strings.Contains(code, "print(type(e).__name__)") {
			t.Fatal("异常捕获题面丢失")
		}
		lines = append(lines, "ZeroDivisionError")
	case "G":
		for _, call := range regexp.MustCompile(`print\(w\((\d+), (\d+)\), w\((\d+), 2\)\)`).FindAllStringSubmatch(code, -1) {
			n, _ := strconv.Atoi(call[1])
			k, _ := strconv.Atoi(call[2])
			if call[1] != call[3] {
				t.Fatal("两个递归调用的 n 参数不一致")
			}
			// 生成函数逐个乘以 1/(1-x^coin)，与递归树独立。
			dp := make([]int, n+1)
			dp[0] = 1
			for coin := 1; coin <= k; coin++ {
				for sum := coin; sum <= n; sum++ {
					dp[sum] += dp[sum-coin]
				}
			}
			lines = append(lines, fmt.Sprintf("%d %d", dp[n], n/2+1))
		}
	}
	if len(lines) < 3 {
		t.Fatalf("独立程序验算器未读到足够的输入：%s", code)
	}
	return strings.Join(lines, "\n")
}

func TestBankCodeDomains(t *testing.T) {
	for _, p := range []map[string]any{{"kind": "C"}, {"kind": "D", "cases": 2}, {"kind": "A", "cases": 7}, {"kind": "B", "cases": "3"}} {
		if _, err := generateCode(bankNumericTestRNG(1), p); err == nil {
			t.Fatalf("非法参数被接受：%v", p)
		}
	}
	seen := map[string]bool{}
	for seed := uint64(0); seed < 64; seed++ {
		got := bankNumericVerifyDeterminism(t, generateCode, nil, seed)
		seen[got.Parameters["kind"].(string)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("默认域没有覆盖全部代码族：%v", seen)
	}
}
