import type { ModelCapability, ModelCapabilityOverride } from '@/api/types'

function match(pattern: string, model: string): number {
  if (pattern === model) return 1000000
  if (pattern === '*') return 1
  return pattern.endsWith('*') && model.startsWith(pattern.slice(0, -1)) ? pattern.length : 0
}

// 与后端同样按平台、最长前缀和精确型号合并，未传字段与显式 null 分开。
export function effectiveCapability(platform: string, model: string, defaults: ModelCapability[], overrides: ModelCapabilityOverride[]): ModelCapability {
  const base = defaults.filter((c) => c.platform === platform && match(c.model, model) > 0)
    .sort((a, b) => match(b.model, model) - match(a.model, model))[0]
  let result: ModelCapability = base ? { ...base, context_window_by_auth: base.context_window_by_auth === null ? null : { ...base.context_window_by_auth } } : {
    platform, model, context_window: 0, context_window_by_auth: null, max_output: 0, max_output_unlimited: null,
    tools: null, json_schema: null, image: null, pdf: null, multiturn: null, thinking: null, prompt_cache: null, efforts: null, source: '',
  }
  const patches = overrides.filter((c) => c.platform === platform && match(c.model, model) > 0)
    .sort((a, b) => match(a.model, model) - match(b.model, model))
  for (const patch of patches) {
    const auth = patch.context_window_by_auth
    result = { ...result, ...patch, context_window_by_auth: auth === undefined ? result.context_window_by_auth : auth === null ? null : { ...result.context_window_by_auth, ...auth } }
  }
  return { ...result, platform, model }
}
