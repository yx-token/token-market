import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { market } from '@/api'
import {
  enabledSorted,
  marketConfigDefaults,
  mergeDiscovery,
  type MarketDiscoveryConfig,
} from '@/market/config'

/** 买家端运营配置（热搜 / 横幅 / 货架开关）：顶栏与首页共用。 */
export const useMarketDiscovery = defineStore('marketDiscovery', () => {
  const cfg = ref<MarketDiscoveryConfig>(structuredClone(marketConfigDefaults))
  const loaded = ref(false)
  let inflight: Promise<void> | null = null

  async function ensure() {
    if (loaded.value) return
    if (inflight) {
      await inflight
      return
    }
    inflight = (async () => {
      try {
        const remote = await market.discovery()
        cfg.value = mergeDiscovery(marketConfigDefaults, remote)
      } catch {
        cfg.value = structuredClone(marketConfigDefaults)
      } finally {
        loaded.value = true
        inflight = null
      }
    })()
    await inflight
  }

  async function reload() {
    loaded.value = false
    await ensure()
  }

  const hotSearches = computed(() => enabledSorted(cfg.value.hot_searches))
  const hotBarEnabled = computed(() => cfg.value.hot_bar?.enabled !== false)

  return { cfg, loaded, ensure, reload, hotSearches, hotBarEnabled }
})
