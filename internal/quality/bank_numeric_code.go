package quality

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
)

func init() {
	bankGenerators["code_output"] = generateCode
}

func generateCode(rng *rand.Rand, p map[string]any) (generatedProbe, error) {
	kinds := []string{"A", "B", "D", "G"}
	kind, err := bankString(p, "kind", kinds[rng.IntN(len(kinds))])
	if err != nil {
		return generatedProbe{}, err
	}
	cases, err := bankInt(p, "cases", 3, 3, 6, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	var code, answer string
	var parameters map[string]any
	switch kind {
	case "A":
		code, answer, parameters = bankCodeA(rng, cases)
	case "B":
		code, answer, parameters = bankCodeB(rng, cases)
	case "D":
		code, answer, parameters = bankCodeD(rng, cases)
	case "G":
		code, answer, parameters = bankCodeG(rng, cases)
	default:
		return generatedProbe{}, fmt.Errorf("未知代码输出题类型：%s", kind)
	}
	parameters["kind"], parameters["cases"] = kind, cases
	return generatedProbe{
		Request: ProbeRequest{Prompt: "下面是确定性的 Python 3 程序。只输出它打印的全部内容，保持行序、空格与标点；不解释，不写代码围栏。\n```python\n" + code + "```"},
		Grader:  Grader{Type: "exact", Answer: answer}, Parameters: parameters, Answer: answer,
	}, nil
}

func bankPythonList(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func bankCodeA(rng *rand.Rand, cases int) (string, string, map[string]any) {
	k1, divisor, multiplier, k2 := 3+rng.IntN(3), 2+rng.IntN(3), 2+rng.IntN(3), 3+rng.IntN(3)
	remainder, start, stride := rng.IntN(k1), rng.IntN(3), 2+rng.IntN(2)
	end := start + 2 + rng.IntN(3)
	inputs := make([][]int, cases)
	lines := make([]string, cases)
	var code strings.Builder
	fmt.Fprintf(&code, "def f(xs):\n    out = []\n    for i, x in enumerate(xs):\n        if x %% %d == %d:\n            out.insert(0, x // %d)\n        elif i %% 2 == 1:\n            out.append(x * %d - i)\n        else:\n            mid = len(out) // 2\n            out[mid:mid] = [x + i]\n    out = sorted(out, key=lambda v: (v %% %d, -v))\n    return out[%d:%d] + out[::- %d][:2]\n\n", k1, remainder, divisor, multiplier, k2, start, end, stride)
	for ci := range cases {
		xs := make([]int, 7+rng.IntN(4))
		out := []int{}
		for i := range xs {
			xs[i] = 1 + rng.IntN(40)
			switch x := xs[i]; {
			case x%k1 == remainder:
				out = append([]int{x / divisor}, out...)
			case i%2 == 1:
				out = append(out, x*multiplier-i)
			default:
				at := len(out) / 2
				out = slices.Insert(out, at, x+i)
			}
		}
		slices.SortFunc(out, func(a, b int) int {
			am, bm := (a%k2+k2)%k2, (b%k2+k2)%k2
			if am != bm {
				return am - bm
			}
			return b - a
		})
		result := slices.Clone(out[min(start, len(out)):min(end, len(out))])
		for j, at := 0, len(out)-1; j < 2 && at >= 0; j, at = j+1, at-stride {
			result = append(result, out[at])
		}
		inputs[ci], lines[ci] = xs, bankPythonList(result)
		fmt.Fprintf(&code, "print(f(%s))\n", bankPythonList(xs))
	}
	return code.String(), strings.Join(lines, "\n"), map[string]any{"k1": k1, "remainder": remainder, "divisor": divisor, "multiplier": multiplier, "k2": k2, "start": start, "end": end, "stride": stride, "inputs": inputs}
}

func bankCodeB(rng *rand.Rand, cases int) (string, string, map[string]any) {
	multiplier, period := 3+rng.IntN(5), 2+rng.IntN(2)
	alphabet := "bcdfghklmnprstaeiou0123456789"
	inputs, lines := make([]string, cases), make([]string, cases)
	var code strings.Builder
	fmt.Fprintf(&code, "def g(s):\n    res = []\n    for i, ch in enumerate(s):\n        if ch.isdigit():\n            res.append(str((int(ch) * %d + i) %% 10))\n        elif ch in \"aeiou\":\n            res.insert(0, ch.upper())\n        elif i %% %d == 0:\n            res.append(ch * 2)\n        else:\n            res = res[::-1]\n    return \"\".join(res)\n\n", multiplier, period)
	for ci := range cases {
		input := make([]byte, 10+rng.IntN(4))
		for i := range input {
			input[i] = alphabet[rng.IntN(len(alphabet))]
		}
		chunks := []string{}
		for i, ch := range input {
			switch {
			case ch >= '0' && ch <= '9':
				chunks = append(chunks, strconv.Itoa((int(ch-'0')*multiplier+i)%10))
			case strings.ContainsRune("aeiou", rune(ch)):
				chunks = append([]string{strings.ToUpper(string(ch))}, chunks...)
			case i%period == 0:
				chunks = append(chunks, strings.Repeat(string(ch), 2))
			default:
				slices.Reverse(chunks)
			}
		}
		inputs[ci], lines[ci] = string(input), strings.Join(chunks, "")
		fmt.Fprintf(&code, "print(g(%q))\n", input)
	}
	return code.String(), strings.Join(lines, "\n"), map[string]any{"multiplier": multiplier, "period": period, "inputs": inputs}
}

type bankCodeDInput struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	Negative  int `json:"negative"`
	Divisor   int `json:"divisor"`
	HalfTwice int `json:"half_twice"`
}

func bankCodeD(rng *rand.Rand, cases int) (string, string, map[string]any) {
	n, row, column, value := 3+rng.IntN(4), rng.IntN(3), rng.IntN(3), 2+rng.IntN(8)
	var code strings.Builder
	fmt.Fprintf(&code, "def acc(x, bucket=[]):\n    bucket.append(x)\n    return sum(bucket)\n\nfs = [lambda y: y * i for i in range(%d)]\na = [[0] * 3] * 3\na[%d][%d] = %d\n", n, row, column, value)
	inputs, lines := make([]bankCodeDInput, cases), make([]string, 0, cases+1)
	accumulator := 0
	for i := range cases {
		in := bankCodeDInput{X: 2 + rng.IntN(8), Y: 2 + rng.IntN(8), Negative: -7 - rng.IntN(23), Divisor: 3 + rng.IntN(4), HalfTwice: 1 + 2*rng.IntN(5)}
		accumulator += in.X
		quotient := int(math.Floor(float64(in.Negative) / float64(in.Divisor)))
		remainder := in.Negative - quotient*in.Divisor
		rounded := int(math.RoundToEven(float64(in.HalfTwice) / 2))
		lines = append(lines, fmt.Sprintf("%d %d %d %d %d %d", accumulator, in.Y*(n-1), value*3, quotient, remainder, rounded))
		fmt.Fprintf(&code, "print(acc(%d), fs[0](%d), sum(map(sum, a)), %d // %d, %d %% %d, round(%.1f))\n", in.X, in.Y, in.Negative, in.Divisor, in.Negative, in.Divisor, float64(in.HalfTwice)/2)
		inputs[i] = in
	}
	code.WriteString("try:\n    print(7 // 0)\nexcept Exception as e:\n    print(type(e).__name__)\n")
	lines = append(lines, "ZeroDivisionError")
	return code.String(), strings.Join(lines, "\n"), map[string]any{"n": n, "row": row, "column": column, "value": value, "inputs": inputs}
}

func bankCodeG(rng *rand.Rand, cases int) (string, string, map[string]any) {
	var code strings.Builder
	code.WriteString("from functools import lru_cache\n\n@lru_cache(maxsize=None)\ndef w(n, k):\n    if n == 0:\n        return 1\n    if n < 0 or k == 0:\n        return 0\n    return w(n - k, k) + w(n, k - 1)\n\n")
	inputs, lines := make([][]int, cases), make([]string, cases)
	type state struct{ n, k int }
	cache := map[state]int{}
	var count func(int, int) int
	count = func(n, k int) int {
		if n == 0 {
			return 1
		}
		if n < 0 || k == 0 {
			return 0
		}
		key := state{n, k}
		if answer, ok := cache[key]; ok {
			return answer
		}
		cache[key] = count(n-k, k) + count(n, k-1)
		return cache[key]
	}
	for i := range cases {
		n, k := 14+rng.IntN(9), 4+rng.IntN(3)
		inputs[i] = []int{n, k}
		lines[i] = fmt.Sprintf("%d %d", count(n, k), count(n, 2))
		fmt.Fprintf(&code, "print(w(%d, %d), w(%d, 2))\n", n, k, n)
	}
	return code.String(), strings.Join(lines, "\n"), map[string]any{"inputs": inputs}
}
