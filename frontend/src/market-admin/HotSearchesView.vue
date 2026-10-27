<script setup lang="ts">
/**
 * 热搜配置：TablePageLayout + filter-grid + DataTable；行内编辑，↑↓ 调序后保存。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminHotSearches' })

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

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '启用' },
  { value: 'false', label: '停用' },
]

const PLAT_OPTIONS = [
  { value: '', label: '不限厂商' },
  ...PLATS.map((p) => ({ value: p, label: p })),
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  enabled: '',
  plat: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const base = ref<DiscoveryConfig>({})
const rows = ref<HotSearch[]>([])
const tagOpts = ref<{ id: string; name: string }[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'label', label: '文案', class: 'col-label' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'enabled', label: '启用', class: 'col-enabled' },
  { key: 'plat', label: '厂商', class: 'col-plat' },
  { key: 'kindTab', label: '形态', class: 'col-kind' },
  { key: 'tag', label: '标签', class: 'col-tag' },
  { key: 'q', label: '搜索词', class: 'col-q' },
  { key: 'href', label: '链接', class: 'col-href' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

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

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.enabled === 'true') list = list.filter((h) => h.enabled !== false)
  if (queryParams.value.enabled === 'false') list = list.filter((h) => h.enabled === false)
  if (queryParams.value.plat) {
    list = list.filter((h) => (h.action?.plat || '') === queryParams.value.plat)
  }
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((h) =>
      [h.id, h.label, h.action?.plat, h.action?.tag, h.action?.q, h.action?.href, h.action?.kindTab]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

const allStats = computed(() => {
  const all = rows.value
  return {
    total: all.length,
    enabled: all.filter((h) => h.enabled !== false).length,
    preview: Math.min(4, all.filter((h) => h.enabled !== false).length),
  }
})

const tagSelectOptions = computed(() => [
  { value: '', label: '标签（可选）' },
  ...tagOpts.value.map((t) => ({ value: t.id, label: t.name })),
])

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.enabled,
    queryParams.value.plat,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  const n = (queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1
  return String(n).padStart(2, '0')
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

function indexOf(h: HotSearch) {
  return rows.value.findIndex((r) => r.id === h.id)
}

function move(h: HotSearch, delta: number) {
  const i = indexOf(h)
  if (i < 0) return
  const j = i + delta
  if (j < 0 || j >= rows.value.length) return
  const next = [...rows.value]
  const [item] = next.splice(i, 1)
  next.splice(j, 0, item)
  rows.value = next
  reSort()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [remote, cats] = await Promise.all([
      marketGet<DiscoveryConfig>(`${MARKET_API_PREFIX}/market-discovery`),
      marketGet<MarketCategory[] | null>(`${MARKET_API_PREFIX}/market-categories`).catch(
        () => [] as MarketCategory[],
      ),
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
  queryParams.value.pageNum = Math.ceil(rows.value.length / queryParams.value.pageSize) || 1
}

function removeAt(h: HotSearch) {
  const i = indexOf(h)
  if (i < 0) return
  rows.value.splice(i, 1)
  reSort()
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
  <section id="market-hot-searches-page" class="hot-searches-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">热搜与筛选</h2>
          <p class="y-page-desc">市场页筛选 pill；启用列表前 4 项展示。↑↓ 调序，保存即生效</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="busy" @click="add">
          <Icon name="plus" size="sm" />
          热搜
        </button>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy || loading" @click="save">
          保存
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">热搜总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已启用</div>
                <div class="stat-card__value">{{ allStats.enabled.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">展示位</div>
                <div class="stat-card__value">{{ allStats.preview }} / 4</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">热搜筛选</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="文案 / ID / 搜索词"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">启用</label>
              <Select
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ENABLED_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">厂商</label>
              <Select
                v-model="queryParams.plat"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PLAT_OPTIONS"
              />
            </div>
            <div class="filter-actions lg:col-span-4 sm:col-span-2">
              <button type="button" class="btn btn-secondary" @click="resetQuery">
                <Icon name="refresh" size="sm" />
                重置
              </button>
              <button type="button" class="btn btn-primary" @click="handleQuery">
                <Icon name="search" size="sm" />
                搜索
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="pageRows"
          :loading="loading"
          :expandable-actions="false"
        >
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-label="{ row }">
            <input v-model="row.label" class="cell-input" placeholder="显示文案" />
          </template>
          <template #cell-id="{ row }">
            <input v-model="row.id" class="cell-input mono" placeholder="id" />
          </template>
          <template #cell-enabled="{ row }">
            <label class="y-check">
              <input v-model="row.enabled" type="checkbox" />
              启用
            </label>
          </template>
          <template #cell-plat="{ row }">
            <select v-model="row.action.plat" class="cell-input">
              <option :value="undefined">厂商（可选）</option>
              <option v-for="p in PLATS" :key="p" :value="p">{{ p }}</option>
            </select>
          </template>
          <template #cell-kindTab="{ row }">
            <select v-model="row.action.kindTab" class="cell-input">
              <option v-for="k in KINDS" :key="k.value || 'none'" :value="k.value || undefined">
                {{ k.label }}
              </option>
            </select>
          </template>
          <template #cell-tag="{ row }">
            <select v-if="tagOpts.length" v-model="row.action.tag" class="cell-input">
              <option v-for="o in tagSelectOptions" :key="o.value || 'none'" :value="o.value || undefined">
                {{ o.label }}
              </option>
            </select>
            <input v-else v-model="row.action.tag" class="cell-input" placeholder="标签 id" />
          </template>
          <template #cell-q="{ row }">
            <input v-model="row.action.q" class="cell-input" placeholder="搜索词" />
          </template>
          <template #cell-href="{ row }">
            <input v-model="row.action.href" class="cell-input" placeholder="/seller/publish" />
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="indexOf(row) <= 0" @click="move(row, -1)">上移</button>
              <button
                type="button"
                class="y-link"
                :disabled="indexOf(row) < 0 || indexOf(row) >= rows.length - 1"
                @click="move(row, 1)"
              >
                下移
              </button>
              <button type="button" class="y-link y-link--danger" @click="removeAt(row)">删除</button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              title="暂无热搜"
              description="点右上角添加热搜项，编辑后记得保存"
              action-text="添加热搜"
              @action="add"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="queryParams.pageNum"
          :page-size="queryParams.pageSize"
          :page-size-options="[15, 20, 50]"
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>
  </section>
</template>

<style scoped>
#market-hot-searches-page.hot-searches-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.page-heading {
  min-width: 0;
}

.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.section-block {
  min-width: 0;
}

.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  color: var(--color-text-primary, #262626);
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.stat-card {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}

.stat-card--blue {
  background: linear-gradient(135deg, #eff6ff, #f8fafc);
}
.stat-card--green {
  background: linear-gradient(135deg, #ecfdf5, #f8fafc);
}
.stat-card--orange {
  background: linear-gradient(135deg, #fff7ed, #f8fafc);
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.2;
}

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

.cell-input {
  width: 100%;
  min-width: 0;
  height: 30px;
  padding: 0 8px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: #fff;
  font-size: 12px;
}

.cell-input.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.y-check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

#market-hot-searches-page :deep(.filter-grid .select-trigger),
#market-hot-searches-page :deep(.filter-grid .select-trigger-sm),
#market-hot-searches-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-hot-searches-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-hot-searches-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
  vertical-align: middle;
}

#market-hot-searches-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-hot-searches-page :deep(th.col-label),
#market-hot-searches-page :deep(td.col-label),
#market-hot-searches-page :deep(th.col-id),
#market-hot-searches-page :deep(td.col-id) {
  min-width: 7rem;
}
#market-hot-searches-page :deep(th.col-enabled),
#market-hot-searches-page :deep(td.col-enabled) {
  min-width: 4.5rem;
}
#market-hot-searches-page :deep(th.col-plat),
#market-hot-searches-page :deep(td.col-plat),
#market-hot-searches-page :deep(th.col-kind),
#market-hot-searches-page :deep(td.col-kind),
#market-hot-searches-page :deep(th.col-tag),
#market-hot-searches-page :deep(td.col-tag),
#market-hot-searches-page :deep(th.col-q),
#market-hot-searches-page :deep(td.col-q),
#market-hot-searches-page :deep(th.col-href),
#market-hot-searches-page :deep(td.col-href) {
  min-width: 7.5rem;
}
#market-hot-searches-page :deep(th.col-ops),
#market-hot-searches-page :deep(td.col-ops) {
  min-width: 10rem;
  width: 10rem;
  box-sizing: border-box;
}
#market-hot-searches-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-hot-searches-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-hot-searches-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
</style>
