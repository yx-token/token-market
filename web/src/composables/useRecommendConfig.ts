import { ref } from 'vue'
import { admin } from '@/api'
import type { RecommendConfig } from '@/api/types'
import { useToast } from '@/stores/toast'

const cache = ref<RecommendConfig | null>(null)

export function useRecommendConfig() {
  const toast = useToast()
  const loading = ref(false)
  const saving = ref(false)

  async function load(force = false) {
    if (cache.value && !force) return cache.value
    loading.value = true
    try {
      cache.value = await admin.marketRecommend()
      return cache.value
    } catch (e) {
      toast.error(e)
      throw e
    } finally {
      loading.value = false
    }
  }

  async function save(body: RecommendConfig, okMsg = '已保存') {
    saving.value = true
    try {
      cache.value = await admin.updateMarketRecommend(body)
      toast.success(okMsg)
      return cache.value
    } catch (e) {
      toast.error(e)
      return null
    } finally {
      saving.value = false
    }
  }

  function invalidate() {
    cache.value = null
  }

  return { cache, loading, saving, load, save, invalidate }
}

export const filterOpts = [
  { kind: 'tag', val: 'low_rate', label: '低倍率按量通道' },
  { kind: 'tag', val: 'coding', label: '写代码相关' },
  { kind: 'tag', val: 'featured', label: '精选挂单' },
  { kind: 'tag', val: 'grade_s', label: 'S 级质量' },
  { kind: 'kind', val: 'pack', label: '额度包' },
  { kind: 'kind', val: 'channel', label: '全部按量通道' },
  { kind: 'pool', val: 'pool', label: '保障池' },
]

export const themeOpts = [
  { value: 'deal', label: '橙黄 · 特价', swatch: '#ffc107' },
  { value: 'code', label: '蓝色 · 写代码', swatch: '#64b5f6' },
  { value: 'pool', label: '绿色 · 保障池', swatch: '#66bb6a' },
  { value: 'buyer', label: '粉色 · 额度包', swatch: '#f06292' },
]
