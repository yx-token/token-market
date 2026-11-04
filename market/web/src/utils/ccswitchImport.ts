/** 对齐引擎 frontend/src/utils/ccswitchImport.ts：生成 CC Switch 一键导入 deeplink。 */

export const OPENAI_CC_SWITCH_CODEX_MODEL = 'gpt-5.5'
export const GROK_CC_SWITCH_MODEL = 'grok-4.5'

export type CcSwitchClientType = 'claude' | 'gemini'
export type CcSwitchPlatform =
  | 'anthropic'
  | 'openai'
  | 'gemini'
  | 'antigravity'
  | 'grok'
  | string

export interface CcSwitchImportConfig {
  app: string
  endpoint: string
  model?: string
}

/**
 * CC Switch 导入后的余额查询脚本。`{{baseUrl}}` 由 CCS 填入；
 * 余额入口固定在站点 `/v1/usage`，不使用模型入口的协议前缀。
 */
export const CC_SWITCH_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}".replace(/\\/+$/, "").replace(/\\/(?:antigravity(?:\\/v1)?|v1)$/, "") + "/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.currency ?? response?.quota?.unit ?? "CNY";
      if (response?.error || typeof remaining !== "number" || !Number.isFinite(remaining)) {
        return { isValid: false };
      }
      return {
        isValid: response?.is_active ?? response?.isValid ?? remaining > 0,
        remaining,
        unit
      };
    }
  })`

function withV1Endpoint(baseUrl: string): string {
  const normalized = baseUrl.replace(/\/+$/, '')
  return normalized.endsWith('/v1') ? normalized : `${normalized}/v1`
}

function withoutTrailingSlashes(baseUrl: string): string {
  return baseUrl.replace(/\/+$/, '')
}

/** 市场侧平台名可能是 Claude / ChatGPT 等展示名，归一到引擎分组平台。 */
export function normalizeCcSwitchPlatform(platform?: string | null): CcSwitchPlatform {
  const p = (platform || 'anthropic').toLowerCase()
  if (p.includes('openai') || p.includes('chatgpt') || p === 'codex') return 'openai'
  if (p.includes('gemini') || p.includes('google')) return 'gemini'
  if (p.includes('antigravity')) return 'antigravity'
  if (p.includes('grok') || p.includes('xai')) return 'grok'
  if (p.includes('claude') || p.includes('anthropic')) return 'anthropic'
  return p || 'anthropic'
}

export function resolveCcSwitchImportConfig(
  platform: CcSwitchPlatform | undefined | null,
  clientType: CcSwitchClientType,
  baseUrl: string,
): CcSwitchImportConfig {
  switch (normalizeCcSwitchPlatform(platform)) {
    case 'antigravity':
      return {
        app: clientType === 'gemini' ? 'gemini' : 'claude',
        endpoint: `${baseUrl.replace(/\/+$/, '')}/antigravity`,
      }
    case 'openai':
      return {
        app: 'codex',
        endpoint: withoutTrailingSlashes(baseUrl),
        model: OPENAI_CC_SWITCH_CODEX_MODEL,
      }
    case 'gemini':
      return {
        app: 'gemini',
        endpoint: baseUrl,
      }
    case 'grok':
      return {
        app: 'grokbuild',
        endpoint: withV1Endpoint(baseUrl),
        model: GROK_CC_SWITCH_MODEL,
      }
    default:
      return {
        app: 'claude',
        endpoint: baseUrl,
      }
  }
}

export function buildCcSwitchImportDeeplink(input: {
  baseUrl: string
  platform?: CcSwitchPlatform | null
  clientType: CcSwitchClientType
  providerName: string
  apiKey: string
  usageScript?: string
}): string {
  const config = resolveCcSwitchImportConfig(input.platform, input.clientType, input.baseUrl)
  const usageScript = input.usageScript ?? CC_SWITCH_USAGE_SCRIPT
  const entries: [string, string][] = [
    ['resource', 'provider'],
    ['app', config.app],
    ['name', input.providerName],
    ['homepage', input.baseUrl],
    ['endpoint', config.endpoint],
    ['apiKey', input.apiKey],
    ['configFormat', 'json'],
    ['usageEnabled', 'true'],
    ['usageScript', btoa(usageScript)],
    ['usageAutoInterval', '30'],
  ]
  if (config.model) {
    entries.splice(2, 0, ['model', config.model])
  }
  return `ccswitch://v1/import?${new URLSearchParams(entries).toString()}`
}
