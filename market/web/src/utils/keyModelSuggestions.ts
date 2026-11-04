import { normalizeCcSwitchPlatform } from '@/utils/ccswitchImport'
import { normalizeKeyModels } from '@/utils/clientImport'

/** 平台价目表里的模型是否属于该平台（只用于给保障池 Key 补模型建议）。 */
export function modelMatchesPlatform(model: string, platform: string | undefined | null): boolean {
  const m = model.toLowerCase()
  switch (normalizeCcSwitchPlatform(platform)) {
    case 'anthropic':
      return m.startsWith('claude')
    case 'openai':
      return m.startsWith('gpt') || m.startsWith('codex') || /^o\d/.test(m)
    case 'gemini':
      return m.startsWith('gemini')
    case 'grok':
      return m.startsWith('grok')
    case 'antigravity':
      return m.startsWith('claude') || m.startsWith('gemini')
    default:
      return true
  }
}

export interface KeyModelSuggestions {
  models: string[]
  /** listing：来自 Key 路线上卖家开放的模型；catalog：保障池或读取失败时退回平台价目表 */
  source: 'listing' | 'catalog'
}

/**
 * 导入客户端时的模型建议：优先用路线上各挂单的模型白名单（卖家实际开放的），
 * 没有挂单（全走保障池）时退回平台价目表里同平台的模型，过滤后为空则给全部。
 */
export function suggestKeyModels(listingModels: readonly unknown[], priceModels: readonly string[], platform: string | undefined | null): KeyModelSuggestions {
  const fromListings = normalizeKeyModels([...listingModels])
  if (fromListings.length) return { models: fromListings, source: 'listing' }
  const catalog = normalizeKeyModels([...priceModels])
  const sameVendor = catalog.filter((m) => modelMatchesPlatform(m, platform))
  return { models: sameVendor.length ? sameVendor : catalog, source: 'catalog' }
}
