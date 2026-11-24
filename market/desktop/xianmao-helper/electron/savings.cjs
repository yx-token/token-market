/**
 * 按客户端常用平台，从市场挂单取最低倍率，算出「比官网省 xx%」。
 */
const AGENT_PLATFORMS = {
  codex: ['openai'],
  'claude-code': ['anthropic'],
  'claude-desktop': ['anthropic'],
  obsidian: ['anthropic'],
  'deepseek-harness': ['deepseek'],
  // 通用客户端：取全市场最低
  pi: null,
  hermes: null,
  openclaw: null,
  paseo: null,
  zcode: null,
  'cherry-studio': null,
  chatbox: null,
}

let cache = { at: 0, byAgent: {} }
const CACHE_MS = 60_000

function savePercentFromMult(mult) {
  const m = Number(mult)
  if (!Number.isFinite(m) || m <= 0 || m >= 1) return null
  return Math.round((1 - m) * 100)
}

function collectPrices(listings, pools) {
  const byPlatform = new Map()
  const push = (platform, price) => {
    const p = String(platform || '').trim().toLowerCase()
    const m = Number(price)
    if (!p || !Number.isFinite(m) || m <= 0) return
    const prev = byPlatform.get(p)
    if (prev === undefined || m < prev) byPlatform.set(p, m)
  }
  for (const it of listings || []) {
    if (String(it.sku_kind || it.kind || '') === 'pack') continue
    push(it.platform, it.price)
  }
  for (const p of pools || []) {
    if (p && p.market_published === false) continue
    push(p.platform, p.price)
  }
  return byPlatform
}

function bestForAgent(byPlatform, agentId) {
  const platforms = Object.prototype.hasOwnProperty.call(AGENT_PLATFORMS, agentId)
    ? AGENT_PLATFORMS[agentId]
    : null
  let best = null
  if (platforms && platforms.length) {
    for (const p of platforms) {
      const m = byPlatform.get(p)
      if (m !== undefined && (best === null || m < best)) best = m
    }
  } else {
    for (const m of byPlatform.values()) {
      if (best === null || m < best) best = m
    }
  }
  if (best === null) return null
  const save = savePercentFromMult(best)
  return {
    best_mult: best,
    save_percent: save,
    save_label: save !== null && save > 0 ? `比官网省 ${save}%` : best < 1 ? `约 ${best.toFixed(2)}×` : null,
  }
}

async function fetchJSON(url) {
  const res = await fetch(url, { headers: { Accept: 'application/json' }, cache: 'no-store' })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const body = await res.json().catch(() => ({}))
  return body?.data ?? body
}

/**
 * @param {string} apiBase 市场根地址
 * @param {string[]} agentIds
 * @returns {Promise<Record<string, { best_mult: number, save_percent: number|null, save_label: string|null }>>}
 */
async function loadAgentSavings(apiBase, agentIds) {
  const api = String(apiBase || '').replace(/\/$/, '')
  if (!api) return {}
  const now = Date.now()
  if (now - cache.at < CACHE_MS && cache.byAgent && Object.keys(cache.byAgent).length) {
    return cache.byAgent
  }
  try {
    const [listings, pools] = await Promise.all([
      fetchJSON(`${api}/api/market/listings?limit=200&sort=price`),
      fetchJSON(`${api}/api/market/pools`),
    ])
    const list = Array.isArray(listings) ? listings : listings?.items || []
    const poolList = Array.isArray(pools) ? pools : []
    const byPlatform = collectPrices(list, poolList)
    const byAgent = {}
    for (const id of agentIds || []) {
      const hit = bestForAgent(byPlatform, id)
      if (hit?.save_label) byAgent[id] = hit
    }
    cache = { at: now, byAgent }
    return byAgent
  } catch {
    return cache.byAgent || {}
  }
}

module.exports = {
  AGENT_PLATFORMS,
  loadAgentSavings,
  savePercentFromMult,
  bestForAgent,
  collectPrices,
}
