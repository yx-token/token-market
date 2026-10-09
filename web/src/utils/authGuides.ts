// 卖家接入上游时各接入方式的说明：适用的账号、去哪里拿 key、使用的官方地址。
// 键为 "厂商:接入方式"，与后端 internal/listing/vendors.go 中实现的接入方式一一对应。

export interface AuthGuide {
  /** 适用的账号或订阅 */
  summary: string
  /** 授权或获取 key 的说明 */
  how?: string
  /** 平台使用的官方地址（key 类），卖家不能修改 */
  endpoint?: string
}

export const authGuides: Record<string, AuthGuide> = {
  'anthropic:oauth': { summary: 'Claude Pro / Max 订阅账号，完整授权（推理与账号资料）。' },
  'anthropic:setup-token': { summary: 'Claude Pro / Max 订阅账号，只授权推理，与 claude setup-token 相同。' },
  'openai:oauth': { summary: 'ChatGPT Plus / Pro / Team 订阅账号（Codex 额度）。授权后平台会关闭训练数据使用。' },
  'gemini:google_one': { summary: '订阅了 Google AI Pro / Ultra 的个人 Google 账号，不需要 GCP 项目。' },
  'gemini:oauth': {
    summary: '开通了 Gemini Code Assist（Standard / Enterprise）的 GCP 项目，项目需已激活并绑定结算账号。',
    how: '填写项目 ID 可以确保使用付费项目的配额；留空时由平台自动探测。',
  },
  'antigravity:oauth': { summary: 'Antigravity 账号（Google 账号登录，额度随 Google AI 订阅）。授权后平台会关闭训练数据使用。' },
  'grok:oauth': { summary: 'Grok 订阅账号（SuperGrok 等，xAI 账号登录）。' },
  'kimi:coding': { summary: 'Kimi 会员的 Kimi For Coding key。', how: '在 Kimi 会员页面创建 Kimi For Coding 的 key。', endpoint: 'api.kimi.com/coding' },
  'zhipu:coding': {
    summary: '智谱开放平台（bigmodel.cn）的 GLM Coding Plan，个人版与团队版均可。',
    how: '订阅 GLM Coding Plan 后在 API Keys 页面创建 key；团队版还需要填写组织 ID。',
    endpoint: 'open.bigmodel.cn',
  },
  'zhipu:coding-intl': { summary: 'Z.ai 国际站的 GLM Coding Plan。', how: '在 z.ai 订阅 GLM Coding Plan 后创建 API key。', endpoint: 'api.z.ai' },
  'minimax:coding': { summary: 'MiniMax 国内站（minimaxi.com）的 Coding Plan。', how: '订阅 Coding Plan 后在开放平台获取 key。', endpoint: 'api.minimaxi.com' },
  'minimax:coding-intl': { summary: 'MiniMax 国际站（minimax.io）的 Coding Plan。', how: '订阅 Coding Plan 后在国际站开放平台获取 key。', endpoint: 'api.minimax.io' },
  'opencode_go:go': { summary: 'OpenCode Go 订阅。', how: '在 opencode.ai 订阅 Go 后获取 API key。', endpoint: 'opencode.ai/zen/go' },
}

/** 需要（可选）填写 GCP 项目 ID 的接入方式 */
export function takesProjectID(platform: string, authType: string): boolean {
  return platform === 'gemini' && authType === 'oauth'
}

/** 支持团队版（组织 / 项目 ID）的接入方式 */
export function takesTeam(platform: string, authType: string): boolean {
  return platform === 'zhipu' && authType === 'coding'
}
