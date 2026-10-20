package inspection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 能力只登记官方事实；空指针表示未登记，不能按不支持处理。
type ModelCapability struct {
	Platform            string         `json:"platform"`
	Model               string         `json:"model"`
	ContextWindow       int            `json:"context_window"`
	ContextWindowByAuth map[string]int `json:"context_window_by_auth"`
	MaxOutput           int            `json:"max_output"`
	MaxOutputUnlimited  *bool          `json:"max_output_unlimited"`
	Tools               *bool          `json:"tools"`
	JSONSchema          *bool          `json:"json_schema"`
	Image               *bool          `json:"image"`
	PDF                 *bool          `json:"pdf"`
	MultiTurn           *bool          `json:"multiturn"`
	Thinking            *bool          `json:"thinking"`
	PromptCache         *bool          `json:"prompt_cache"`
	Efforts             []string       `json:"efforts"`
	Source              string         `json:"source"`
}

// 保留补丁字段的存在性：未传字段继承默认，显式 null 可恢复为未登记。
type ModelCapabilityOverride map[string]json.RawMessage

func capabilityBool(value bool) *bool { return &value }

var capabilityDefaults = buildCapabilityDefaults()

func buildCapabilityDefaults() []ModelCapability {
	var result []ModelCapability
	add := func(platform, model string, window, output int, efforts []string, cache *bool, source string) {
		result = append(result, ModelCapability{Platform: platform, Model: model, ContextWindow: window,
			MaxOutput: output, Efforts: efforts, Thinking: capabilityBool(true), PromptCache: cache, Source: source})
	}
	five := []string{"low", "medium", "high", "xhigh", "max"}
	for _, model := range []string{"claude-fable-5-1", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-opus-5", "claude-fable-5", "claude-sonnet-5", "claude-opus-4-8", "claude-opus-4-7"} {
		add("anthropic", model, 1000000, 128000, five, capabilityBool(true), "docs/research/inspection/model-landscape.md 第1章2、3节；https://platform.claude.com/docs/en/about-claude/models/overview")
	}
	for _, model := range []string{"claude-opus-4-6", "claude-sonnet-4-6"} {
		add("anthropic", model, 1000000, 128000, []string{"low", "medium", "high", "max"}, capabilityBool(true), "docs/research/inspection/model-landscape.md 第1章2、3节")
	}
	add("anthropic", "claude-haiku-4-5-20251001", 200000, 64000, []string{}, capabilityBool(true), "docs/research/inspection/model-landscape.md 第1章2、3节")
	add("anthropic", "claude-haiku-4-5", 200000, 64000, []string{}, capabilityBool(true), "docs/research/inspection/model-landscape.md 第1章2、3节")
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4", "gpt-5.3-codex", "gpt-5.2"} {
		efforts := append([]string{"none"}, five...)
		if model == "gpt-6-astra" || model == "gpt-6.1-sol" || model == "gpt-5.3-codex" {
			efforts = append([]string(nil), five...)
		}
		if model == "gpt-5.5" || model == "gpt-5.4" || model == "gpt-5.3-codex" || model == "gpt-5.2" {
			efforts = efforts[:len(efforts)-1]
		}
		// 档位表登记 API 与 Codex 的官方取值并集，具体入口限制仍须查来源。
		if model == "gpt-6-astra" || model == "gpt-6.1-sol" || model == "gpt-6-sol" || model == "gpt-5.6-sol" || model == "gpt-5.6-terra" {
			efforts = append(efforts, "ultra")
		}
		window := 1050000
		if model == "gpt-5.3-codex" || model == "gpt-5.2" {
			window = 400000
		}
		add("openai", model, window, 128000, efforts, nil, "docs/research/inspection/model-landscape.md 第2章2.1、2.2节")
		result[len(result)-1].ContextWindowByAuth = map[string]int{"oauth": 272000}
	}
	for _, model := range []string{"gemini-3.8-flash", "gemini-3.6-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview", "gemini-2.5-pro", "gemini-2.5-flash"} {
		efforts := []string{"minimal", "low", "medium", "high"}
		if model == "gemini-3.8-flash" || model == "gemini-3.1-pro-preview" {
			efforts = efforts[1:]
		} else if strings.HasPrefix(model, "gemini-2.5-") {
			efforts = nil
		}
		add("gemini", model, 1048576, 65536, efforts, nil, "docs/research/inspection/model-landscape.md 第3章2节；https://ai.google.dev/gemini-api/docs/models")
	}
	for _, model := range []string{"grok-4.7", "grok-4.6", "grok-4.5", "grok-4.3", "grok-build-0.1"} {
		window, efforts := 500000, []string{"low", "medium", "high", "xhigh"}
		switch model {
		case "grok-4.5":
			efforts = efforts[:3] // 官方文档对 xhigh 有冲突，只登记一致部分。
		case "grok-4.3":
			window, efforts = 1000000, append([]string{"none"}, efforts...)
		case "grok-build-0.1":
			window, efforts = 256000, nil
		}
		// 无输出上限不能伪造为一个有限值，来源中保留官方说明。
		add("grok", model, window, 0, efforts, capabilityBool(true), "docs/research/inspection/model-landscape.md 第4章7节；https://docs.x.ai/developers/models/"+model)
		if model == "grok-4.7" || model == "grok-4.6" {
			result[len(result)-1].MaxOutputUnlimited = capabilityBool(true)
		}
	}
	for _, model := range []string{"k3", "kimi-k3", "kimi-for-coding", "kimi-for-coding-highspeed"} {
		window, output, efforts := 1048576, 0, []string{"low", "high", "max"}
		var cache *bool
		switch model {
		case "kimi-k3":
			output, cache = 1048576, capabilityBool(true)
		case "kimi-for-coding-highspeed":
			window, efforts = 262144, nil
		}
		add("kimi", model, window, output, efforts, cache, "docs/research/inspection/model-landscape.md 第5章1.2节；https://www.kimi.com/code/docs/kimi-code/models.html")
	}
	for _, model := range []string{"glm-5.3", "glm-5.3-flash"} {
		add("zhipu", model, 1000000, 128000, []string{"low", "high", "max"}, capabilityBool(true), "docs/research/inspection/model-landscape.md 第5章2.1、2.2节；https://docs.bigmodel.cn/cn/guide/start/migrate-to-glm-new.md")
	}
	for _, model := range []string{"MiniMax-M3.1-Flash-Preview", "MiniMax-M3", "MiniMax-M2.7"} {
		window, output, efforts := 1000000, 524288, five
		switch model {
		case "MiniMax-M3":
			efforts = nil
		case "MiniMax-M2.7":
			window, output, efforts = 204800, 204800, nil
		}
		add("minimax", model, window, output, efforts, nil, "docs/research/inspection/model-landscape.md 第5章3.2节；https://platform.minimax.cn/docs/guides/text-generation.md")
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Platform+"/"+result[i].Model < result[j].Platform+"/"+result[j].Model
	})
	return result
}

// 返回副本，管理员查看或恢复默认不会改写进程内置表。
func CapabilityDefaults() []ModelCapability {
	raw, _ := json.Marshal(capabilityDefaults)
	var result []ModelCapability
	_ = json.Unmarshal(raw, &result)
	return result
}

func (c ModelCapability) ContextForAuth(auth string) int {
	if window, ok := c.ContextWindowByAuth[auth]; ok {
		return window
	}
	return c.ContextWindow
}

func capabilityMatch(pattern, model string) int {
	if pattern == model {
		return 1000000
	}
	if pattern == "*" {
		return 1
	}
	if strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
		return len(pattern)
	}
	return 0
}

// 精确型号优先于最长前缀与平台默认；管理员补丁按从宽到窄逐字段覆盖。
func LookupCapability(platform, model string, st Settings) ModelCapability {
	best := ModelCapability{Platform: platform, Model: model}
	rank := 0
	for _, c := range capabilityDefaults {
		if score := capabilityMatch(c.Model, model); c.Platform == platform && score > rank {
			best, rank = c, score
		}
	}
	raw, _ := json.Marshal(best)
	var patches []ModelCapabilityOverride
	for _, patch := range st.ModelCapabilities {
		var c ModelCapability
		data, _ := json.Marshal(patch)
		if json.Unmarshal(data, &c) == nil && c.Platform == platform && capabilityMatch(c.Model, model) > 0 {
			patches = append(patches, patch)
		}
	}
	sort.SliceStable(patches, func(i, j int) bool {
		var a, b string
		_ = json.Unmarshal(patches[i]["model"], &a)
		_ = json.Unmarshal(patches[j]["model"], &b)
		return capabilityMatch(a, model) < capabilityMatch(b, model)
	})
	for _, patch := range patches {
		data, _ := json.Marshal(patch)
		raw, _ = mergeJSON(raw, data)
	}
	best = ModelCapability{}
	_ = json.Unmarshal(raw, &best)
	best.Platform, best.Model = platform, model
	return best
}

func validateCapabilityOverrides(overrides []ModelCapabilityOverride) error {
	seen := map[string]bool{}
	for _, patch := range overrides {
		raw, err := json.Marshal(patch)
		if err != nil {
			return fmt.Errorf("模型官方能力格式无效：%w", err)
		}
		var c ModelCapability
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&c); err != nil {
			return fmt.Errorf("模型官方能力含未知字段或无效值：%w", err)
		}
		switch c.Platform {
		case "anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "minimax", "opencode_go", "composite":
		default:
			return fmt.Errorf("模型官方能力的平台无效：%s", c.Platform)
		}
		if strings.TrimSpace(c.Model) != c.Model || c.Model == "" || len(c.Model) > 256 || strings.ContainsAny(c.Model, " \t\r\n") || strings.Count(c.Model, "*") > 1 || strings.Contains(strings.TrimSuffix(c.Model, "*"), "*") || strings.EqualFold(c.Model, "unknown") || c.Model == "未登记" || c.Model == "未知" {
			return fmt.Errorf("模型官方能力的型号无效：%s", c.Model)
		}
		key := c.Platform + "/" + c.Model
		if seen[key] {
			return fmt.Errorf("模型官方能力重复登记：%s", key)
		}
		seen[key] = true
		if c.ContextWindow < 0 || c.ContextWindow > 10000000 || c.MaxOutput < 0 || c.MaxOutput > 10000000 || len(c.Source) > 4096 {
			return fmt.Errorf("模型官方能力 %s 的窗口、输出或来源超出范围", key)
		}
		for auth, window := range c.ContextWindowByAuth {
			switch auth {
			case "oauth", "apikey", "setup-token", "coding", "coding-intl", "google_one", "go":
			default:
				return fmt.Errorf("模型官方能力 %s 的认证类型无效：%s", key, auth)
			}
			if window < 0 || window > 10000000 {
				return fmt.Errorf("模型官方能力 %s 的认证窗口超出范围", key)
			}
		}
		for _, effort := range c.Efforts {
			switch effort {
			case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
			default:
				return fmt.Errorf("模型官方能力 %s 的推理档位无效：%s", key, effort)
			}
		}
	}
	return nil
}
