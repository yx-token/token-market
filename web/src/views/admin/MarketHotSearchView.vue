<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { useMarketDiscovery } from '@/stores/marketDiscovery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import ActionFields from '@/components/console/ActionFields.vue'
import type { MarketCategory } from '@/api/types'
import {
  marketConfigDefaults,
  mergeDiscovery,
  type HotSearch,
  type MarketDiscoveryConfig,
} from '@/market/config'
import '@/styles/console-liquid.css'

const toast = useToast()
const { busy, run } = useAction()
const rows = ref<HotSearch[]>([])
const tagOpts = ref<{ id: string; name: string }[]>([])
const base = ref<MarketDiscoveryConfig>(structuredClone(marketConfigDefaults))

type DragState = { index: number } | null
const drag = ref<DragState>(null)
const over = ref<number | null>(null)

onMounted(load)

function flattenPickable(nodes: MarketCategory[], out: { id: string; name: string }[] = []) {
  for (const n of nodes) {
    if (n.seller_pickable && n.enabled !== false) out.push({ id: n.id, name: n.name })
    if (n.children?.length) flattenPickable(n.children, out)
  }
  return out
}

async function load() {
  try {
    const [remote, cats] = await Promise.all([
      admin.marketDiscovery(),
      admin.marketCategories().catch(() => [] as MarketCategory[]),
    ])
    base.value = mergeDiscovery(marketConfigDefaults, remote)
    rows.value = base.value.hot_searches.map((h) => ({ ...h, action: { ...h.action } }))
    tagOpts.value = flattenPickable(cats ?? [])
  } catch (e) {
    toast.error(e)
  }
}

function uid() {
  return `hs_${Math.random().toString(36).slice(2, 8)}`
}

function reSort() {
  rows.value.forEach((h, i) => {
    h.sort = (i + 1) * 10
  })
}

function add() {
  rows.value.push({
    id: uid(),
    label: '新热搜',
    enabled: true,
    sort: (rows.value.length + 1) * 10,
    action: {},
  })
  reSort()
}

function onDragStart(index: number, e: DragEvent) {
  drag.value = { index }
  e.dataTransfer?.setData('text/plain', String(index))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(index: number, e: DragEvent) {
  e.preventDefault()
  over.value = index
}

function onDrop(index: number) {
  if (!drag.value) return
  const from = drag.value.index
  if (from !== index) {
    const [item] = rows.value.splice(from, 1)
    rows.value.splice(index, 0, item)
    reSort()
  }
  drag.value = null
  over.value = null
}

async function save() {
  reSort()
  const body = {
    ...JSON.parse(JSON.stringify(base.value)),
    hot_searches: JSON.parse(JSON.stringify(rows.value)),
  } as Record<string, unknown>
  const res = await run(() => admin.updateMarketDiscovery(body), '热搜已保存')
  if (res) {
    base.value = mergeDiscovery(marketConfigDefaults, res as Record<string, unknown>)
    rows.value = base.value.hot_searches.map((h) => ({ ...h, action: { ...h.action } }))
    await useMarketDiscovery().reload()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-market-hot-page"
    title="热搜与筛选"
    description="显示在市场页筛选 pill 上，取启用列表前 4 项，排在「全部 / 按量通道 / 订阅套餐」之后。拖拽调序，保存即生效。"
    plain
  >
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="add">＋ 热搜</button>
      <button type="button" class="btn btn-primary" :disabled="busy" @click="save">保存</button>
    </template>

    <div class="space-y-2">
      <div
        v-for="(h, hi) in rows"
        :key="h.id"
        class="lg-card"
        :class="{ 'is-over': over === hi, 'is-dim': h.enabled === false }"
        draggable="true"
        @dragstart="onDragStart(hi, $event)"
        @dragover="onDragOver(hi, $event)"
        @drop="onDrop(hi)"
        @dragend="drag = null; over = null"
      >
        <div class="hot-row">
          <span class="lg-grip" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i></span>
          <input v-model="h.label" class="input !max-w-[10rem]" placeholder="显示文案" />
          <input v-model="h.id" class="input font-mono !max-w-[8rem]" placeholder="id" />
          <label class="hot-check"><input v-model="h.enabled" type="checkbox" /> 启用</label>
          <div class="hot-action">
            <ActionFields v-model="h.action" :tags="tagOpts" />
          </div>
          <button type="button" class="btn-ghost btn-sm text-red-600" @click="rows.splice(hi, 1)">删</button>
        </div>
      </div>
      <p v-if="!rows.length" class="py-8 text-center text-sm text-slate-500">暂无热搜，点右上角添加。</p>
    </div>
  </ConsoleListPage>
</template>

<style scoped>
.hot-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 0.5rem 0.65rem;
  padding: 0.75rem 0.9rem;
}
.hot-check {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 12px;
  color: #475569;
}
.hot-action {
  flex: 1 1 16rem;
  min-width: 0;
}
</style>
