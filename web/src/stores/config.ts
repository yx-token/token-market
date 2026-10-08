import { defineStore } from 'pinia'
import { ref } from 'vue'
import { market } from '@/api'
import type { MarketConfig } from '@/api/types'

// 平台公开参数（Edge 地址、抽佣率等），首次使用时加载。
export const useConfig = defineStore('config', () => {
  const config = ref<MarketConfig>({ edge_url: '', commission_rate: 0.2, freeze_days: 7, withdraw_min: 10, max_hops: 5, payment_channels: [], recharge_min: 1, recharge_max: 50000 })
  let loaded = false
  async function ensure() {
    if (loaded) return config.value
    try {
      config.value = await market.config()
      loaded = true
    } catch {
      // 使用默认值，下次再试
    }
    return config.value
  }
  return { config, ensure }
})
