package buyer

import (
	"strings"

	"github.com/yx-token/token-market/market/internal/platform/httpx"
	"github.com/yx-token/token-market/market/internal/scheduling"
)

// 一键使用支持的客户端（含助手内新增的 Agent）。
var oneClickClients = map[string]struct{}{
	"codex":            {},
	"claude-code":      {},
	"claude-desktop":   {},
	"pi":               {},
	"hermes":           {},
	"openclaw":         {},
	"paseo":            {},
	"zcode":            {},
	"deepseek-harness": {},
	"obsidian":         {},
	"cherry-studio":    {},
	"chatbox":          {},
	"helper":           {}, // 助手浏览器登录授权（不对应具体 Agent）
	// 扩展 Agent
	"opencode":  {},
	"cline":     {},
	"aider":     {},
	"kimi-code": {},
	"qwen-code": {},
	"workbuddy": {},
	"codebuddy": {},
	"trae":      {},
	"gemini":    {},
	"goose":     {},
	"crush":     {},
	"kilo-code": {},
}

// oneClickAgentVendors 每个客户端可用的保障池平台。
// 缺省或空切片表示「多厂商」：可用任意已上架保障池，未选时默认 openai（ChatGPT）。
var oneClickAgentVendors = map[string][]string{
	"codex":            {"openai"},
	"claude-code":      {"anthropic"},
	"claude-desktop":   {"anthropic"},
	"obsidian":         {"anthropic"},
	"deepseek-harness": {"deepseek"},
	"gemini":           {"gemini"},
	"kimi-code":        {"kimi", "openai"},
	// 以下为多厂商（OpenAI 兼容 / 可切厂商）
	"opencode":      nil,
	"cline":         nil,
	"aider":         nil,
	"qwen-code":     nil,
	"workbuddy":     nil,
	"codebuddy":     nil,
	"trae":          nil,
	"goose":         nil,
	"crush":         nil,
	"kilo-code":     nil,
	"pi":            nil,
	"hermes":        nil,
	"openclaw":      nil,
	"paseo":         nil,
	"zcode":         nil,
	"cherry-studio": nil,
	"chatbox":       nil,
}

const defaultMultiVendorPlatform = "openai"

var platformDisplayNames = map[string]string{
	"openai":      "ChatGPT",
	"anthropic":   "Claude",
	"gemini":      "Gemini",
	"deepseek":    "DeepSeek",
	"kimi":        "Kimi",
	"grok":        "Grok",
	"minimax":     "MiniMax",
	"zhipu":       "智谱",
	"composite":   "综合保障池",
	"antigravity": "Antigravity",
}

// OneClickVendorOption 助手/网页下拉选项。
type OneClickVendorOption struct {
	Platform string `json:"platform"`
	Label    string `json:"label"`
	Default  bool   `json:"default,omitempty"`
}

func normalizeOneClickClient(v string) (string, error) {
	id := strings.TrimSpace(strings.ToLower(v))
	if _, ok := oneClickClients[id]; !ok {
		return "", httpx.BadRequest("INVALID_CLIENT", "unsupported one-click client")
	}
	return id, nil
}

// AgentSupportedVendors 返回该客户端声明支持的平台；nil 表示不限（多厂商）。
func AgentSupportedVendors(client string) []string {
	client = strings.TrimSpace(strings.ToLower(client))
	if v, ok := oneClickAgentVendors[client]; ok {
		return v
	}
	return nil
}

// AgentIsMultiVendor 是否支持多个厂商（未限定或声明 ≥2）。
func AgentIsMultiVendor(client string) bool {
	v := AgentSupportedVendors(client)
	return v == nil || len(v) >= 2
}

// preferredOneClickPlatform 未指定厂商时的默认保障池平台。
// 单厂商客户端用其唯一平台；多厂商默认 ChatGPT（openai）。
func preferredOneClickPlatform(client string) string {
	v := AgentSupportedVendors(client)
	if len(v) == 1 {
		return v[0]
	}
	if len(v) >= 2 {
		for _, p := range v {
			if p == defaultMultiVendorPlatform {
				return defaultMultiVendorPlatform
			}
		}
		return v[0]
	}
	// nil = 任意厂商 → 默认 ChatGPT
	return defaultMultiVendorPlatform
}

func normalizeOneClickPlatform(raw string) string {
	return strings.TrimSpace(strings.ToLower(raw))
}

// ValidateAgentPlatform 校验平台是否属于该客户端支持范围；空串返回默认平台。
func ValidateAgentPlatform(client, platform string) (string, error) {
	client, err := normalizeOneClickClient(client)
	if err != nil {
		return "", err
	}
	platform = normalizeOneClickPlatform(platform)
	if platform == "" {
		return preferredOneClickPlatform(client), nil
	}
	supported := AgentSupportedVendors(client)
	if supported == nil {
		return platform, nil
	}
	for _, p := range supported {
		if p == platform {
			return platform, nil
		}
	}
	return "", httpx.BadRequest("INVALID_PLATFORM", "platform not supported by this client")
}

func platformLabel(platform string) string {
	if n, ok := platformDisplayNames[platform]; ok {
		return n
	}
	return platform
}

// ListOneClickVendors 取「客户端支持 ∩ 已上架保障池」的厂商列表，并标出默认项。
func ListOneClickVendors(client string, pools []scheduling.Pool) ([]OneClickVendorOption, error) {
	client, err := normalizeOneClickClient(client)
	if err != nil {
		return nil, err
	}
	if client == "helper" {
		return nil, nil
	}
	supported := AgentSupportedVendors(client)
	allow := map[string]bool{}
	if supported != nil {
		for _, p := range supported {
			allow[p] = true
		}
	}
	seen := map[string]bool{}
	var out []OneClickVendorOption
	appendPool := func(p scheduling.Pool) {
		plat := strings.TrimSpace(strings.ToLower(p.Platform))
		if plat == "" || seen[plat] {
			return
		}
		if supported != nil && !allow[plat] {
			return
		}
		seen[plat] = true
		out = append(out, OneClickVendorOption{
			Platform: plat,
			Label:    platformLabel(plat),
		})
	}
	for _, p := range pools {
		if !p.Enabled || p.Sub2APIGroupID == nil || !p.MarketPublished {
			continue
		}
		appendPool(p)
	}
	if len(out) == 0 {
		for _, p := range pools {
			if p.Enabled && p.Sub2APIGroupID != nil {
				appendPool(p)
			}
		}
	}
	def := preferredOneClickPlatform(client)
	if len(out) == 0 {
		// 库里暂时没有池时仍返回声明支持的厂商，便于下拉展示
		if supported == nil {
			out = append(out, OneClickVendorOption{Platform: def, Label: platformLabel(def), Default: true})
			return out, nil
		}
		for _, p := range supported {
			out = append(out, OneClickVendorOption{Platform: p, Label: platformLabel(p)})
		}
	}
	marked := false
	for i := range out {
		if out[i].Platform == def {
			out[i].Default = true
			marked = true
			break
		}
	}
	if !marked && len(out) > 0 {
		out[0].Default = true
	}
	return out, nil
}
