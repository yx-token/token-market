<script setup lang="ts">
// 买家 Key 的路由编辑：默认分组，或指定卖家链（最多 5 跳，最后一跳可以是默认分组）。
import { computed } from 'vue'
import type { MarketItem, PoolItem } from '@/api/types'
import { label } from '@/utils/labels'
import { multiplier } from '@/utils/format'
import { validateChain, type HopChoice } from '@/utils/route'
import { detectionText, isTested } from '@/api/market-detection'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'

const props = defineProps<{ listings: MarketItem[]; pools: PoolItem[]; maxHops: number }>()
const type = defineModel<'default' | 'chain'>('type', { required: true })
const pool = defineModel<string>('pool', { required: true })
const hops = defineModel<HopChoice[]>('hops', { required: true })

const chainPlatform = computed(() => hops.value.find((h) => !h.pool)?.platform ?? '')
const error = computed(() => (type.value === 'chain' ? validateChain(hops.value, props.maxHops) : pool.value ? null : '请选择默认分组'))
const lastIsPool = computed(() => hops.value.length > 0 && !!hops.value[hops.value.length - 1].pool)

function listingOptions(current?: number) {
  return props.listings.filter(
    (l) => l.listing_id === current || ((!chainPlatform.value || l.platform === chainPlatform.value) && !hops.value.some((h) => h.listingId === l.listing_id)),
  )
}
function poolOptions() {
  return props.pools.filter((p) => !chainPlatform.value || p.platform === chainPlatform.value || p.platform === 'composite')
}
function addListing() {
  const insertAt = lastIsPool.value ? hops.value.length - 1 : hops.value.length
  hops.value.splice(insertAt, 0, { listingId: undefined, platform: chainPlatform.value })
}
function addPool() {
  const p = poolOptions()[0]
  if (p) hops.value.push({ pool: p.name, platform: p.platform })
}
function setListing(i: number, id: string) {
  const l = props.listings.find((x) => x.listing_id === Number(id))
  hops.value[i] = { listingId: l?.listing_id, platform: l?.platform ?? '' }
}
function setPool(i: number, name: string) {
  const p = props.pools.find((x) => x.name === name)
  hops.value[i] = { pool: name, platform: p?.platform ?? '' }
}
function move(i: number, d: number) {
  const j = i + d
  if (j < 0 || j >= hops.value.length) return
  const arr = [...hops.value]
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  hops.value = arr
}
defineExpose({ error })
</script>

<template>
  <div class="space-y-3">
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <label :class="['choice flex cursor-pointer items-start gap-2.5', type === 'default' && 'choice-on']">
        <input v-model="type" type="radio" value="default" class="mt-0.5" />
        <span><span class="font-medium">平台默认分组</span><span class="block text-xs text-ink-500">由平台挑选可用上游，保障可用</span></span>
      </label>
      <label :class="['choice flex cursor-pointer items-start gap-2.5', type === 'chain' && 'choice-on']">
        <input v-model="type" type="radio" value="chain" class="mt-0.5" />
        <span><span class="font-medium">商家清单</span><span class="block text-xs text-ink-500">选择挂单并排序，优先调用靠前的挂单</span></span>
      </label>
    </div>
    <div v-if="type === 'default'">
      <select v-model="pool" class="input" aria-label="默认分组">
        <option value="" disabled>选择默认分组</option>
        <option v-for="p in pools" :key="p.name" :value="p.name">{{ p.name }}（{{ label('platform', p.platform).text }}，{{ multiplier(p.price) }}）</option>
      </select>
    </div>
    <div v-else class="space-y-2">
      <div v-for="(h, i) in hops" :key="i" class="space-y-1">
      <div class="flex items-center gap-2">
        <span class="w-12 shrink-0 font-mono text-[11px] text-ink-500">第 {{ String(i + 1).padStart(2, '0') }} 跳</span>
        <select v-if="!h.pool" class="input" :value="h.listingId ?? ''" :aria-label="`第 ${i + 1} 跳挂单`" @change="setListing(i, ($event.target as HTMLSelectElement).value)">
          <option value="" disabled>选择卖家挂单</option>
          <option v-if="h.listingId && !listings.some((l) => l.listing_id === h.listingId)" :value="h.listingId" disabled>挂单 #{{ h.listingId }} · 已移出清单或暂不可用</option>
          <option v-for="l in listingOptions(h.listingId)" :key="l.listing_id" :value="l.listing_id">
            商家 #{{ l.seller_id }} / 挂单 #{{ l.listing_id }} · {{ label('platform', l.platform).text }} · {{ multiplier(l.price) }} · {{ detectionText(l.detection_status) }}{{ isTested(l.detection_status) && l.grade ? ` · ${l.grade} 级` : '' }}
          </option>
        </select>
        <select v-else class="input" :value="h.pool" :aria-label="`第 ${i + 1} 跳默认分组`" @change="setPool(i, ($event.target as HTMLSelectElement).value)">
          <option v-for="p in poolOptions()" :key="p.name" :value="p.name">默认分组 {{ p.name }}（{{ multiplier(p.price) }}）</option>
        </select>
        <button type="button" class="btn-ghost btn-sm" :disabled="i === 0 || !!h.pool" aria-label="上移" @click="move(i, -1)">↑</button>
        <button type="button" class="btn-ghost btn-sm" :disabled="i >= hops.length - 1 || !!hops[i + 1]?.pool || !!h.pool" aria-label="下移" @click="move(i, 1)">↓</button>
        <button type="button" class="btn-ghost btn-sm text-red-600" aria-label="删除这一跳" @click="hops.splice(i, 1)">✕</button>
      </div>
      <MarketDetectionSummary v-for="l in listings.filter((l) => l.listing_id === h.listingId)" :key="l.listing_id" class="pl-14" :status="l.detection_status" :models="l.models" :checks="l.model_checks" />
      </div>
      <div class="flex gap-2">
        <button type="button" class="btn-secondary btn-sm" :disabled="hops.length >= maxHops || listings.length === 0" @click="addListing">＋ 添加清单内挂单</button>
        <button type="button" class="btn-secondary btn-sm" :disabled="hops.length >= maxHops || lastIsPool || poolOptions().length === 0" @click="addPool">
          ＋ 以默认分组兜底
        </button>
      </div>
      <p class="hint">同一请求只在首字节返回前切换；链内各跳需为同一平台，兜底分组需同平台或全模型分组。</p>
    </div>
    <p v-if="error" class="text-sm text-amber-700">{{ error }}</p>
  </div>
</template>
