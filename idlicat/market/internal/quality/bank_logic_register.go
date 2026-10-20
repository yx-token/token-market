package quality

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

type registerInstruction struct {
	op                string
	x, y, z, constant int
}

// 执行器只处理生成器自己的结构化指令，不执行题面中的程序或表达式。
func runRegister(initial [5]int, instructions []registerInstruction) [5]int {
	state := initial
	for _, ins := range instructions {
		switch ins.op {
		case "ADD":
			state[ins.x] = (state[ins.x] + state[ins.y]) % 100
		case "SUB":
			state[ins.x] = (state[ins.x] - state[ins.y] + 100) % 100
		case "MUL":
			state[ins.x] = state[ins.x] * ins.constant % 100
		case "SWP":
			state[ins.x], state[ins.y] = state[ins.y], state[ins.x]
		case "IFGT":
			if state[ins.x] > state[ins.y] {
				state[ins.z] = (state[ins.z] + ins.constant) % 100
			}
		case "IFODD":
			if state[ins.x]%2 == 1 {
				state[ins.y] = (state[ins.y] + state[ins.x]) % 100
			}
		case "ROT":
			state = [5]int{state[4], state[0], state[1], state[2], state[3]}
		}
	}
	return state
}

func renderRegisterInstruction(ins registerInstruction) string {
	reg := func(i int) string { return string(rune('A' + i)) }
	switch ins.op {
	case "ADD", "SUB", "SWP", "IFODD":
		return fmt.Sprintf("%s %s %s", ins.op, reg(ins.x), reg(ins.y))
	case "MUL":
		return fmt.Sprintf("MUL %s %d", reg(ins.x), ins.constant)
	case "IFGT":
		return fmt.Sprintf("IFGT %s %s %s %d", reg(ins.x), reg(ins.y), reg(ins.z), ins.constant)
	default:
		return "ROT"
	}
}

func generateRegister(rng *rand.Rand, parameters map[string]any) (generatedProbe, error) {
	operations, err := bankInt(parameters, "operations", 40, 20, 80, rng)
	if err != nil {
		return generatedProbe{}, err
	}
	var initial [5]int
	for i := range initial {
		initial[i] = rng.IntN(31)
	}
	kinds := []string{"ADD", "ADD", "ADD", "SUB", "SUB", "MUL", "MUL", "SWP", "SWP", "IFGT", "IFGT", "IFGT", "ROT", "IFODD", "IFODD"}
	instructions := make([]registerInstruction, operations)
	for i := range operations {
		registers := rng.Perm(5)
		ins := registerInstruction{op: kinds[rng.IntN(len(kinds))], x: registers[0], y: registers[1], z: registers[2]}
		switch ins.op {
		case "MUL":
			ins.constant = 2 + rng.IntN(8)
		case "IFGT":
			ins.constant = 3 + rng.IntN(17)
		}
		instructions[i] = ins
	}
	final := runRegister(initial, instructions)
	var prompt strings.Builder
	prompt.WriteString("有 A、B、C、D、E 五个寄存器。依序执行指令，每次算术赋值都取模 100，结果位于 0 到 99。\n")
	prompt.WriteString("指令规则：ADD X Y 把 X 变为 X+Y；SUB X Y 把 X 变为 X-Y；MUL X k 把 X 乘 k；SWP X Y 交换两值；IFGT X Y Z k 在 X>Y 时给 Z 加 k；IFODD X Y 在 X 为奇数时给 Y 加 X；ROT 同时循环右移，新的 A,B,C,D,E 分别为旧的 E,A,B,C,D。\n")
	fmt.Fprintf(&prompt, "初值：A=%d,B=%d,C=%d,D=%d,E=%d\n指令：\n", initial[0], initial[1], initial[2], initial[3], initial[4])
	for i, ins := range instructions {
		fmt.Fprintf(&prompt, "%02d %s\n", i+1, renderRegisterInstruction(ins))
	}
	prompt.WriteString("只输出最终状态一行，格式为 A=数值,B=数值,C=数值,D=数值,E=数值，不要解释。")
	fields := map[string]string{}
	var answer []string
	for i, value := range final {
		name := string(rune('A' + i))
		fields[name] = strconv.Itoa(value)
		answer = append(answer, name+"="+fields[name])
	}
	return generatedProbe{Request: ProbeRequest{Prompt: prompt.String()}, Grader: Grader{Type: "fields_exact", GraderOptions: GraderOptions{Fields: fields, Inline: true}},
		Parameters: map[string]any{"operations": operations}, Answer: strings.Join(answer, ",")}, nil
}
