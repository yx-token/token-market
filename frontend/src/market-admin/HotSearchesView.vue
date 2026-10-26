<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

type MarketAction = {
  plat?: string
  q?: string
  tag?: string
  kindTab?: string
  href?: string
}

type HotSearch = {
  id: string
  label: string
  enabled?: boolean
  sort?: number
  action: MarketAction
}

type DiscoveryConfig = {
  version?: number
  hot_searches?: HotSearch[]
  hot_bar?: { enabled?: boolean }
  feed?: { pin_pool_first?: boolean }
  [key: string]: unknown
}

type MarketCategory = {
  id: string
  name: string
  enabled?: boolean
  seller_pickable?: boolean
  children?: MarketCategory[]
}

const PLATS = ['Claude', 'ChatGPT', 'Gemini', 'Kimi', '智谱', 'MiniMax', 'Grok', 'Antigravity', 'OpenCode', '保障池']
const KINDS = [
  { value: '', label: '不限形态' },
  { value: 'channel', label: '按量通道' },
  { value: 'pack', label: '订阅套餐' },
  { value: 'all', label: '全部闲置TOKEN' },
]

const base = ref<DiscoveryConfig>({})
const rows = ref<HotSearch[]>([])
const tagOpts = ref<{ id: string; name: string }[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const drag = ref<number | null>(null)
const over = ref<number | null>(null)

function flattenPickable(nodes: MarketCategory[], out: { id: string; name: string }[] = []) {
  for (const n of nodes) {
    if (n.seller_pickable && n.enabled !== false) out.push({ id: n.id, name: n.name })
    if (n.children?.length) flattenPickable(n.children, out)
  }
  return out
}

function uid() {
  return `hs_${Math.random().toString(36).slice(2, 8)}`
}

function reSort() {
  rows.value.forEach((h, i) => {
    h.sort = (i + 1) * 10
  })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [remote, cats] = await Promise.all([
      marketGet<DiscoveryConfig>(`${MARKET_API_PREFIX}/market-discovery`),
      marketGet<MarketCategory[] | null>(`${MARKET_API_PREFIX}/market-categories`).catch(() => [] as MarketCategory[]),
    ])
    base.value = remote && typeof remote === 'object' ? { ...remote } : {}
    rows.value = (base.value.hot_searches || []).map((h) => ({
      ...h,
      action: { ...(h.action || {}) },
    }))
    tagOpts.value = flattenPickable(cats ?? [])
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
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

function removeAt(index: number) {
  rows.value.splice(index, 1)
  reSort()
}

function onDragStart(index: number, e: DragEvent) {
  drag.value = index
  e.dataTransfer?.setData('text/plain', String(index))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(index: number, e: DragEvent) {
  e.preventDefault()
  over.value = index
}

function onDrop(index: number) {
  if (drag.value == null) return
  const from = drag.value
  if (from !== index) {
    const [item] = rows.value.splice(from, 1)
    rows.value.splice(index, 0, item)
    reSort()
  }
  onDragEnd()
}

function onDragEnd() {
  drag.value = null
  over.value = null
}

async function save() {
  reSort()
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const body = {
      ...JSON.parse(JSON.stringify(base.value)),
      hot_searches: JSON.parse(JSON.stringify(rows.value)),
    }
    const res = await marketSend<DiscoveryConfig>('PUT', `${MARKET_API_PREFIX}/market-discovery`, body)
    base.value = res && typeof res === 'object' ? { ...res } : body
    rows.value = (base.value.hot_searches || []).map((h) => ({
      ...h,
      action: { ...(h.action || {}) },
    }))
    msg.value = '热搜已保存'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-hot-searches-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">热搜与筛选</h2>
        <p class="y-page-desc">市场页筛选 pill；启用列表前 4 项展示。拖拽调序，保存即生效</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn y-btn--ghost" :disabled="busy" @click="add">＋ 热搜</button>
        <button type="button" class="y-btn" :disabled="busy || loading" @click="save">保存</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!rows.length" class="y-msg">暂无热搜，点右上角添加</p>

      <div v-else class="hot-list">
        <div
          v-for="(h, hi) in rows"
          :key="h.id"
          class="hot-card"
          :class="{ 'is-over': over === hi, 'is-dim': h.enabled === false }"
          draggable="true"
          @dragstart="onDragStart(hi, $event)"
          @dragover="onDragOver(hi, $event)"
          @drop="onDrop(hi)"
          @dragend="onDragEnd"
        >
          <div class="hot-row">
            <span class="hot-grip" title="拖拽调序">⋮⋮</span>
            <input v-model="h.label" class="input" placeholder="显示文案" />
            <input v-model="h.id" class="input font-mono" placeholder="id" />
            <label class="hot-check"><input v-model="h.enabled" type="checkbox" /> 启用</label>
            <button type="button" class="y-link y-link--danger" @click="removeAt(hi)">删</button>
          </div>
          <div class="hot-action">
            <select v-model="h.action.plat" class="input">
              <option :value="undefined">厂商（可选）</option>
              <option v-for="p in PLATS" :key="p" :value="p">{{ p }}</option>
            </select>
            <select v-model="h.action.kindTab" class="input">
              <option v-for="k in KINDS" :key="k.value || 'none'" :value="k.value || undefined">{{ k.label }}</option>
            </select>
            <select v-if="tagOpts.length" v-model="h.action.tag" class="input">
              <option :value="undefined">标签（可选）</option>
              <option v-for="t in tagOpts" :key="t.id" :value="t.id">{{ t.name }}</option>
            </select>
            <input v-else v-model="h.action.tag" class="input" placeholder="标签 id" />
            <input v-model="h.action.q" class="input" placeholder="搜索词" />
            <input v-model="h.action.href" class="input" placeholder="链接 /seller/publish" />
          </div>
        </div>
      </div>
    </div>
    <footer v-if="!loading" class="y-footer">共 {{ rows.length }} 条</footer>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.hot-list {
  display: grid;
  gap: 10px;
}
.hot-card {
  padding: 12px;
  border: 1px solid rgb(0 0 0 / 6%);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.55);
}
.hot-card.is-over {
  outline: 2px solid var(--color-primary, #0555ce);
}
.hot-card.is-dim {
  opacity: 0.55;
}
.hot-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.hot-grip {
  cursor: grab;
  color: #94a3b8;
  user-select: none;
}
.hot-check {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}
.hot-action {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
  gap: 6px;
}
</style>
