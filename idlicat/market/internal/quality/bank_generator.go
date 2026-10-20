package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
)

// generatedProbe 保存冻结的题面与答案；答案只用于内部判分及测试替身。
type generatedProbe struct {
	Request    ProbeRequest
	Grader     Grader
	Parameters map[string]any
	Answer     string
}

type generatorDefinition struct {
	TemplateID string         `json:"template_id"`
	Version    string         `json:"version"`
	Parameters map[string]any `json:"parameters"`
	Instances  map[string]int `json:"instances"`
}

func bankString(p map[string]any, key, fallback string) (string, error) {
	v, ok := p[key]
	if !ok {
		return fallback, nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("参数 %s 须为非空字符串", key)
	}
	return s, nil
}

// 参数域只接受整数或两端闭区间，不接受代码表达式。
func bankInt(p map[string]any, key string, fallback, low, high int, rng *rand.Rand) (int, error) {
	v, ok := p[key]
	if !ok {
		return fallback, nil
	}
	integer := func(v any) (int, error) {
		switch n := v.(type) {
		case int:
			return n, nil
		case json.Number:
			x, e := strconv.Atoi(n.String())
			return x, e
		case float64:
			if n >= float64(low) && n <= float64(high) && n == float64(int(n)) {
				return int(n), nil
			}
		}
		return 0, errors.New("须为整数")
	}
	var values []any
	switch x := v.(type) {
	case []any:
		values = x
	case []int:
		for _, n := range x {
			values = append(values, n)
		}
	default:
		values = []any{v}
	}
	if len(values) != 1 && len(values) != 2 {
		return 0, fmt.Errorf("参数 %s 须为整数或两端闭区间", key)
	}
	a, err := integer(values[0])
	if err != nil {
		return 0, fmt.Errorf("参数 %s 须为整数", key)
	}
	b := a
	if len(values) == 2 {
		b, err = integer(values[1])
		if err != nil {
			return 0, fmt.Errorf("参数 %s 须为整数", key)
		}
	}
	if a < low || b > high || a > b {
		return 0, fmt.Errorf("参数 %s 须在 %d 至 %d 之间", key, low, high)
	}
	return a + rng.IntN(b-a+1), nil
}

var bankGenerators = map[string]func(*rand.Rand, map[string]any) (generatedProbe, error){}

// 模板的合法参数域；parseGenerator 依它拒绝未注册参数，管理界面依它提示可填参数
var bankGeneratorParams = map[string]string{
	"zebra": " houses attributes ", "knights_knaves": " people ", "register_machine": " operations ",
	"aime_math":   " kind n modulus remainder digit_modulus excluded_multiple base exponent_base exponent_power length ones run_limit width height blocked_count ",
	"code_output": " kind cases ", "string_date": " kind length shift year start_day business_days elapsed_hours holiday_count ",
	"constraints": " lang ", "long_context": " target_tokens variant ",
}

// GeneratorTemplate 是一个生成器模板及其可用版本、参数域，供管理界面选项展示
type GeneratorTemplate struct {
	ID         string   `json:"id"`
	Versions   []string `json:"versions"`
	Parameters []string `json:"parameters"`
}

// GeneratorTemplates 列出已注册的生成器模板；与 parseGenerator 使用同一注册表，避免两边漂移
func GeneratorTemplates() []GeneratorTemplate {
	out := make([]GeneratorTemplate, 0, len(bankGenerators))
	for id := range bankGenerators {
		versions := []string{"1"}
		if bankGeneratorVersion(id, "2") {
			versions = append(versions, "2")
		}
		out = append(out, GeneratorTemplate{ID: id, Versions: versions,
			Parameters: strings.Fields(bankGeneratorParams[id])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func parseGenerator(raw json.RawMessage) (*generatorDefinition, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil, nil
	}
	var d generatorDefinition
	if err := decodeInspectionJSON(raw, &d); err != nil {
		return nil, fmt.Errorf("解析生成器：%w", err)
	}
	if !bankGeneratorVersion(d.TemplateID, d.Version) || bankGenerators[d.TemplateID] == nil {
		return nil, errors.New("生成器模板或版本未注册")
	}
	allowed := bankGeneratorParams
	for key := range d.Parameters {
		if !strings.Contains(allowed[d.TemplateID], " "+key+" ") {
			return nil, fmt.Errorf("生成器 %s 未注册参数 %s", d.TemplateID, key)
		}
	}
	for mode, n := range d.Instances {
		if (mode != "full" && mode != "quick") || n < 0 || n > 32 {
			return nil, errors.New("生成器实例数须为 full/quick 的 0 至 32")
		}
	}
	return &d, nil
}

func generateBankProbe(d generatorDefinition, seed string) (generatedProbe, error) {
	if len(seed) != 64 {
		return generatedProbe{}, errors.New("生成器 seed 长度须为 64")
	}
	a, err := strconv.ParseUint(seed[:16], 16, 64)
	if err != nil {
		return generatedProbe{}, err
	}
	b, err := strconv.ParseUint(seed[16:32], 16, 64)
	if err != nil {
		return generatedProbe{}, err
	}
	fn := bankGenerators[d.TemplateID]
	if fn == nil || !bankGeneratorVersion(d.TemplateID, d.Version) {
		return generatedProbe{}, errors.New("生成器模板或版本未注册")
	}
	if d.TemplateID == "constraints" && d.Version == "1" {
		fn = generateConstraintsLegacy
	}
	if d.TemplateID == "zebra" && d.Version == "1" {
		// 保留旧版默认规格；新模板的升难度不混入旧历史。
		parameters := map[string]any{"houses": 4, "attributes": 3}
		for key, value := range d.Parameters {
			parameters[key] = value
		}
		d.Parameters = parameters
	}
	return fn(rand.New(rand.NewPCG(a, b)), d.Parameters) //nolint:gosec // seed 来自平台 HMAC，题库生成不是密钥生成。
}

func bankGeneratorVersion(template, version string) bool {
	return version == "1" || (version == "2" && (template == "constraints" || template == "zebra"))
}
