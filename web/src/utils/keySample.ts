// 按 Key 首跳平台生成调用示例。不同平台走不同的协议端点与鉴权头，模型名也随平台取，
// 避免出现「选了 GPT 分组却给出 Claude 示例」这种误导。

interface Preset {
  /** 平台默认模型；市场价目表里有同平台模型时会优先用价目表里的 */
  model: string
  /** 价目表里的模型按此前缀匹配 */
  prefix: string
  label: string
  code: (o: { base: string; key: string; model: string }) => string
  note: string
}

const anthropicCode = (o: { base: string; key: string; model: string }) => `curl ${o.base}/v1/messages \\
  -H "x-api-key: ${o.key}" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{"model":"${o.model}","max_tokens":256,"messages":[{"role":"user","content":"你好"}]}'`

const openaiCode = (o: { base: string; key: string; model: string }) => `curl ${o.base}/v1/chat/completions \\
  -H "Authorization: Bearer ${o.key}" \\
  -H "content-type: application/json" \\
  -d '{"model":"${o.model}","max_tokens":256,"messages":[{"role":"user","content":"你好"}]}'`

const claudeStyle = (model: string, prefix: string, note: string): Preset => ({
  model,
  prefix,
  label: 'Anthropic Messages',
  code: anthropicCode,
  note,
})

const openaiStyle = (model: string, prefix: string, note: string): Preset => ({
  model,
  prefix,
  label: 'OpenAI Chat Completions',
  code: openaiCode,
  note,
})

const presets: Record<string, Preset> = {
  anthropic: claudeStyle('claude-sonnet-4-5', 'claude-', 'Anthropic 官方协议，Claude Code 直接把 Base URL 指到上面即可。'),
  openai: openaiStyle('gpt-5.4', 'gpt-', 'OpenAI 兼容协议，Codex CLI / 官方 SDK 可用。'),
  gemini: {
    model: 'gemini-2.5-pro',
    prefix: 'gemini-',
    label: 'Gemini generateContent',
    note: 'Gemini 原生协议，鉴权可以用 x-goog-api-key 头或 ?key= 查询参数。',
    code: (o) => `curl "${o.base}/v1beta/models/${o.model}:generateContent" \\
  -H "x-goog-api-key: ${o.key}" \\
  -H "content-type: application/json" \\
  -d '{"contents":[{"parts":[{"text":"你好"}]}]}'`,
  },
  antigravity: claudeStyle('claude-sonnet-4-5', 'claude-', 'Antigravity 分组走 Anthropic Messages 协议，模型名与 Claude 一致。'),
  grok: openaiStyle('grok-4', 'grok-', 'Grok 走 OpenAI 兼容协议。'),
  kimi: openaiStyle('kimi-for-coding', 'kimi', 'Kimi 走 OpenAI 兼容协议，Coding Plan 用专用模型名。'),
  zhipu: openaiStyle('glm-4.6', 'glm', '智谱走 OpenAI 兼容协议。'),
  minimax: openaiStyle('MiniMax-M2', 'minimax', 'MiniMax 走 OpenAI 兼容协议。'),
  opencode_go: openaiStyle('glm-5.3', 'glm', 'OpenCode Go 走 OpenAI 兼容协议。'),
  composite: claudeStyle('claude-sonnet-4-5', 'claude-', '全模型分组按请求里的模型名路由：填 Claude 模型名走 Anthropic 协议，填 GPT 等模型名走对应协议。'),
}

export interface KeySample {
  /** 示例用的协议名，用于展示 */
  label: string
  /** 示例请求代码块 */
  code: string
  /** 补充说明 */
  note: string
}

/** 从市场价目表里挑一个该平台的模型，挑不到就用内置默认值。 */
function pickModel(preset: Preset, models: string[]): string {
  const p = preset.prefix.toLowerCase()
  return models.find((m) => m.toLowerCase().startsWith(p)) ?? preset.model
}

/**
 * sampleRequest 生成该平台的 curl 示例。
 * @param platform Key 首跳的平台；未知平台按 Anthropic 协议处理
 * @param baseURL 调用地址（Edge）
 * @param key Key 明文
 * @param models 市场价目表模型（可选），用于挑同平台的模型名
 */
export function sampleRequest(platform: string, baseURL: string, key: string, models: string[] = []): KeySample {
  const preset = presets[platform] ?? claudeStyle('claude-sonnet-4-5', 'claude-', '未知平台，按 Anthropic Messages 协议给出示例。')
  const model = pickModel(preset, models)
  return { label: preset.label, code: preset.code({ base: baseURL, key, model }), note: preset.note }
}
