<script setup lang="ts">
/**
 * 热搜配置：TablePageLayout + filter-grid + DataTable；行内编辑，↑↓ 调序后保存。
 */
import { computed, nextTick, onMounted, reactive, ref, toRefs, watch } from 'vue'
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
const loaded = ref(false)
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
const hasActiveFilters = computed(() =>
  Boolean(queryParams.value.keyword.trim() || queryParams.value.enabled || queryParams.value.plat),
)
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

watch(rows, () => {
  const maxPage = Math.max(1, Math.ceil(total.value / queryParams.value.pageSize))
  if (queryParams.value.pageNum > maxPage) queryParams.value.pageNum = maxPage
}, { deep: true })

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
  if (hasActiveFilters.value) return
  msg.value = ''
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
    loaded.value = true
  } catch (e) {
    loaded.value = false
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function add() {
  if (!loaded.value || busy.value) return
  msg.value = ''
  const pageSize = queryParams.value.pageSize
  Object.assign(queryParams.value, { ...initQueryParams, pageSize })
  let id = uid()
  while (rows.value.some((row) => row.id === id)) id = uid()
  rows.value.push({
    id,
    label: '新热搜',
    enabled: true,
    sort: (rows.value.length + 1) * 10,
    action: {},
  })
  reSort()
  await nextTick()
  queryParams.value.pageNum = Math.ceil(rows.value.length / pageSize) || 1
}

function removeAt(h: HotSearch) {
  const i = indexOf(h)
  if (i < 0) return
  msg.value = ''
  rows.value.splice(i, 1)
  reSort()
}

async function save() {
  if (!loaded.value || loading.value || busy.value) return
  const ids = rows.value.map((row) => row.id.trim())
  if (ids.some((id) => !id)) {
    error.value = '热搜 ID 不能为空'
    msg.value = ''
    return
  }
  if (new Set(ids).size !== ids.length) {
    error.value = '热搜 ID 必须唯一'
    msg.value = ''
    return
  }
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
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="busy || !loaded" @click="add">
          <Icon name="plus" size="sm" />
          热搜
        </button>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy || loading || !loaded" @click="save">
          保存
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <div v-if="error" class="y-msg y-msg--error mb-2 load-error" role="alert">
          <span class="break-anywhere">{{ error }}</span>
          <button type="button" class="y-link" :disabled="loading" @click="load">重试</button>
        </div>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2" role="status" aria-live="polite">{{ msg }}</p>

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
              <label class="filter-label" for="hot-searches-keyword">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  id="hot-searches-keyword"
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="文案 / ID / 搜索词"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label" for="hot-searches-enabled">启用</label>
              <Select
                id="hot-searches-enabled"
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                aria-label="热搜启用状态"
                :options="ENABLED_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label" for="hot-searches-plat">厂商</label>
              <Select
                id="hot-searches-plat"
                v-model="queryParams.plat"
                class="flex-1 w-full min-w-0"
                size="sm"
                aria-label="热搜厂商"
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
        <p v-if="hasActiveFilters" class="filter-note mb-2">筛选激活时不可调序；清空筛选后可调整完整顺序</p>
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
            <input v-model="row.label" class="cell-input" type="text" placeholder="显示文案" @input="msg = ''" />
          </template>
          <template #cell-id="{ row }">
            <input v-model="row.id" class="cell-input mono" type="text" placeholder="id" @input="msg = ''" />
          </template>
          <template #cell-enabled="{ row }">
            <label class="y-check">
              <input v-model="row.enabled" type="checkbox" @change="msg = ''" />
              启用
            </label>
          </template>
          <template #cell-plat="{ row }">
            <select v-model="row.action.plat" class="cell-input" @change="msg = ''">
              <option :value="undefined">厂商（可选）</option>
              <option v-for="p in PLATS" :key="p" :value="p">{{ p }}</option>
            </select>
          </template>
          <template #cell-kindTab="{ row }">
            <select v-model="row.action.kindTab" class="cell-input" @change="msg = ''">
              <option v-for="k in KINDS" :key="k.value || 'none'" :value="k.value || undefined">
                {{ k.label }}
              </option>
            </select>
          </template>
          <template #cell-tag="{ row }">
            <select v-if="tagOpts.length" v-model="row.action.tag" class="cell-input" @change="msg = ''">
              <option v-for="o in tagSelectOptions" :key="o.value || 'none'" :value="o.value || undefined">
                {{ o.label }}
              </option>
            </select>
            <input v-else v-model="row.action.tag" class="cell-input" type="text" placeholder="标签 id" @input="msg = ''" />
          </template>
          <template #cell-q="{ row }">
            <input v-model="row.action.q" class="cell-input" type="text" placeholder="搜索词" @input="msg = ''" />
          </template>
          <template #cell-href="{ row }">
            <input v-model="row.action.href" class="cell-input" type="text" placeholder="/seller/publish" @input="msg = ''" />
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="hasActiveFilters || indexOf(row) <= 0" @click="move(row, -1)">上移</button>
              <button
                type="button"
                class="y-link"
                :disabled="hasActiveFilters || indexOf(row) < 0 || indexOf(row) >= rows.length - 1"
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
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
  background: var(--color-bg-elevated, #f8fafc);
}

.stat-card__label {
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-primary, #0f172a);
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
  border: 1px solid var(--color-border, #e5e7eb);
  border-radius: 6px;
  background: var(--color-bg-elevated, #fff);
  color: var(--color-text-primary, #303133);
  font-size: 12px;
}

.load-error {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.break-anywhere {
  overflow-wrap: anywhere;
}

.filter-note {
  padding: 8px 12px;
  color: var(--color-text-secondary, #64748b);
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
  background: var(--color-bg-secondary, #f5f7fa);
  color: var(--color-text-secondary, #606266);
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid var(--color-border, #ebeef5);
}

#market-hot-searches-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid var(--color-border, #ebeef5);
  font-size: 13px;
  color: var(--color-text-primary, #303133);
  vertical-align: middle;
}

#market-hot-searches-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: var(--color-bg-secondary, #f5f7fa);
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
@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  #market-hot-searches-page :deep(.y-header),
  #market-hot-searches-page :deep(.y-header-actions) {
    flex-wrap: wrap;
  }

  #market-hot-searches-page :deep(.y-header-actions) {
    width: 100%;
    margin-left: 0;
  }

  .load-error {
    flex-wrap: wrap;
  }
}
</style>
