<script setup lang="ts">
/**
 * 市场分类：TablePageLayout + filter-grid + DataTable（树展平）；保留展开/筛选与 CRUD。
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

defineOptions({ name: 'MarketAdminCategories' })

export interface MarketCategory {
  id: string
  parent_id?: string | null
  name: string
  description?: string
  sort: number
  enabled: boolean
  seller_pickable: boolean
  icon?: string
  children?: MarketCategory[]
}

type FlatRow = MarketCategory & { depth: number; hasChildren: boolean }

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '启用' },
  { value: 'false', label: '停用' },
]

const PICKABLE_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '可选' },
  { value: 'false', label: '不可选' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 50,
  keyword: '',
  enabled: '',
  pickable: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const tree = ref<MarketCategory[]>([])
const collapsed = ref<Set<string>>(new Set())
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const creating = ref(false)
const editing = ref<MarketCategory | null>(null)
const form = reactive({
  id: '',
  parent_id: '' as string,
  name: '',
  description: '',
  sort: 0,
  enabled: true,
  seller_pickable: false,
  icon: '',
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'name', label: '分类', class: 'col-name' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'seller_pickable', label: '卖家可选', class: 'col-pick' },
  { key: 'enabled', label: '状态', class: 'col-enabled' },
  { key: 'sort', label: '排序', class: 'col-sort' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const flatAll = computed(() => {
  const out: FlatRow[] = []
  const walk = (nodes: MarketCategory[], depth: number) => {
    for (const n of nodes) {
      const hasChildren = !!(n.children && n.children.length)
      out.push({ ...n, depth, hasChildren })
      if (hasChildren) walk(n.children!, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

function rowMatches(r: FlatRow) {
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    const hit =
      r.id.toLowerCase().includes(kw) ||
      r.name.toLowerCase().includes(kw) ||
      (r.description || '').toLowerCase().includes(kw)
    if (!hit) return false
  }
  if (queryParams.value.enabled === 'true' && !r.enabled) return false
  if (queryParams.value.enabled === 'false' && r.enabled) return false
  if (queryParams.value.pickable === 'true' && !r.seller_pickable) return false
  if (queryParams.value.pickable === 'false' && r.seller_pickable) return false
  return true
}

const hasActiveFilters = computed(
  () =>
    !!(
      queryParams.value.keyword.trim() ||
      queryParams.value.enabled ||
      queryParams.value.pickable
    ),
)

const filterReveal = computed(() => {
  const show = new Set<string>()
  const forceOpen = new Set<string>()
  if (!hasActiveFilters.value) return { show, forceOpen }
  const byId = new Map(flatAll.value.map((r) => [r.id, r]))
  for (const r of flatAll.value) {
    if (!rowMatches(r)) continue
    let cur: FlatRow | undefined = r
    while (cur) {
      show.add(cur.id)
      if (cur.parent_id) forceOpen.add(cur.parent_id)
      cur = cur.parent_id ? byId.get(cur.parent_id) : undefined
    }
  }
  return { show, forceOpen }
})

const flat = computed(() => {
  const out: FlatRow[] = []
  const filtering = hasActiveFilters.value
  const { show, forceOpen } = filterReveal.value
  const walk = (nodes: MarketCategory[], depth: number) => {
    for (const n of nodes) {
      const hasChildren = !!(n.children && n.children.length)
      if (filtering && !show.has(n.id)) continue
      out.push({ ...n, depth, hasChildren })
      if (!hasChildren) continue
      const open = !collapsed.value.has(n.id) || forceOpen.has(n.id)
      if (open) walk(n.children!, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

const total = computed(() => flat.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return flat.value.slice(start, start + queryParams.value.pageSize)
})

const allStats = computed(() => {
  const all = flatAll.value
  return {
    total: all.length,
    enabled: all.filter((r) => r.enabled).length,
    pickable: all.filter((r) => r.seller_pickable).length,
  }
})

const parentOptions = computed(() => [
  { id: '', label: '（根节点）' },
  ...flatAll.value.map((r) => ({
    id: r.id,
    label: `${'— '.repeat(r.depth)}${r.name} (${r.id})`,
  })),
])

const modalOpen = computed(() => creating.value || !!editing.value)

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.enabled,
    queryParams.value.pickable,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function isExpanded(id: string) {
  return !collapsed.value.has(id) || filterReveal.value.forceOpen.has(id)
}

function toggleExpand(id: string) {
  const next = new Set(collapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  collapsed.value = next
}

function expandAll() {
  collapsed.value = new Set()
}

function collapseAll() {
  collapsed.value = new Set(flatAll.value.filter((r) => r.hasChildren).map((r) => r.id))
}

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

async function load() {
  loading.value = true
  error.value = ''
  try {
    tree.value =
      (await marketGet<MarketCategory[] | null>(`${MARKET_API_PREFIX}/market-categories`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    tree.value = []
  } finally {
    loading.value = false
  }
}

function openCreate(parentId = '') {
  creating.value = true
  editing.value = null
  Object.assign(form, {
    id: '',
    parent_id: parentId,
    name: '',
    description: '',
    sort: 10,
    enabled: true,
    seller_pickable: parentId === 'scene' || parentId === 'region',
    icon: '',
  })
}

function openEdit(c: MarketCategory) {
  creating.value = false
  editing.value = c
  Object.assign(form, {
    id: c.id,
    parent_id: c.parent_id ?? '',
    name: c.name,
    description: c.description ?? '',
    sort: c.sort,
    enabled: c.enabled,
    seller_pickable: c.seller_pickable,
    icon: c.icon ?? '',
  })
}

function closeModal() {
  creating.value = false
  editing.value = null
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function bodyFromForm() {
  return {
    parent_id: form.parent_id || null,
    name: form.name.trim(),
    description: form.description.trim(),
    sort: Number(form.sort) || 0,
    enabled: form.enabled,
    seller_pickable: form.seller_pickable,
    icon: form.icon.trim(),
  }
}

async function save() {
  if (!form.name.trim()) {
    error.value = '请填写名称'
    return
  }
  if (creating.value) {
    if (!/^[a-z0-9_]{1,64}$/.test(form.id.trim())) {
      error.value = 'id 须为小写字母/数字/下划线'
      return
    }
    const ok = await run('已创建', () =>
      marketSend('POST', `${MARKET_API_PREFIX}/market-categories`, {
        id: form.id.trim(),
        ...bodyFromForm(),
      }),
    )
    if (!ok) return
  } else if (editing.value) {
    const ok = await run('已保存', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/market-categories/${editing.value!.id}`, bodyFromForm()),
    )
    if (!ok) return
  }
  closeModal()
  await load()
}

async function remove(c: MarketCategory) {
  if (c.children?.length) {
    error.value = '请先删除子分类'
    return
  }
  if (!window.confirm(`删除分类「${c.name}」(${c.id})？`)) return
  if (await run('已删除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/market-categories/${c.id}`))) {
    await load()
  }
}

async function toggleEnabled(c: MarketCategory) {
  if (
    await run(c.enabled ? '已停用' : '已启用', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/market-categories/${c.id}`, {
        enabled: !c.enabled,
        name: c.name,
        description: c.description ?? '',
        sort: c.sort,
        seller_pickable: c.seller_pickable,
        parent_id: c.parent_id ?? null,
        icon: c.icon ?? '',
      }),
    )
  ) {
    await load()
  }
}

onMounted(load)
</script>

<template>
  <section id="market-categories-page" class="categories-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">市场分类</h2>
          <p class="y-page-desc">树状分类；勾选「卖家可选」的节点会出现在发布挂单页</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="expandAll">
          全部展开
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="collapseAll">
          全部收起
        </button>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy" @click="openCreate()">
          <Icon name="plus" size="sm" />
          根分类
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
                <div class="stat-card__label">分类总数</div>
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
                <div class="stat-card__label">卖家可选</div>
                <div class="stat-card__value">{{ allStats.pickable.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">分类筛选</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="名称 / ID / 说明"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ENABLED_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">卖家可选</label>
              <Select
                v-model="queryParams.pickable"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PICKABLE_OPTIONS"
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
          :row-class="(row: FlatRow) => (!row.enabled ? 'is-disabled' : '')"
        >
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-name="{ row }">
            <div class="cat-name" :style="{ paddingLeft: `${Math.min(row.depth, 4) * 18}px` }">
              <button
                v-if="row.hasChildren"
                type="button"
                class="cat-toggle"
                :aria-expanded="isExpanded(row.id)"
                :title="isExpanded(row.id) ? '收起' : '展开'"
                @click="toggleExpand(row.id)"
              >
                {{ isExpanded(row.id) ? '▾' : '▸' }}
              </button>
              <span v-else class="cat-leaf" aria-hidden="true">·</span>
              <div class="min-w-0">
                <div class="cat-title" :class="{ root: row.depth === 0 }">{{ row.name }}</div>
                <div v-if="row.description" class="row-sub">{{ row.description }}</div>
              </div>
            </div>
          </template>
          <template #cell-id="{ row }">
            <span class="mono-cell">{{ row.id }}</span>
          </template>
          <template #cell-seller_pickable="{ row }">
            {{ row.seller_pickable ? '可选' : '—' }}
          </template>
          <template #cell-enabled="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '启用' : '停用' }}
            </span>
          </template>
          <template #cell-sort="{ row }">{{ row.sort }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="openCreate(row.id)">加子级</button>
              <button type="button" class="y-link" :disabled="busy" @click="openEdit(row)">编辑</button>
              <button type="button" class="y-link" :disabled="busy" @click="toggleEnabled(row)">
                {{ row.enabled ? '停用' : '启用' }}
              </button>
              <button type="button" class="y-link y-link--danger" :disabled="busy" @click="remove(row)">删除</button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="hasActiveFilters ? '没有符合条件的分类' : '暂无分类'"
              description="调整筛选或新建根分类"
              action-text="根分类"
              @action="openCreate()"
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
          :page-size-options="[20, 50, 100]"
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>

    <div v-if="modalOpen" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="creating ? '新建分类' : '编辑分类'">
        <header class="y-modal__head">
          <h3>{{ creating ? '新建分类' : `编辑 ${editing?.id}` }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label v-if="creating" class="y-field">
            <span>ID</span>
            <input v-model="form.id" class="input w-full font-mono" placeholder="low_rate" />
          </label>
          <label class="y-field">
            <span>名称</span>
            <input v-model="form.name" class="input w-full" />
          </label>
          <label class="y-field">
            <span>父分类</span>
            <select v-model="form.parent_id" class="input w-full">
              <option
                v-for="p in parentOptions"
                :key="p.id || 'root'"
                :value="p.id"
                :disabled="!!editing && p.id === editing.id"
              >
                {{ p.label }}
              </option>
            </select>
          </label>
          <label class="y-field">
            <span>说明</span>
            <input v-model="form.description" class="input w-full" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>排序</span>
              <input v-model.number="form.sort" type="number" class="input w-full" />
            </label>
            <label class="y-field">
              <span>图标 key</span>
              <input v-model="form.icon" class="input w-full" placeholder="chat / code" />
            </label>
          </div>
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用</label>
          <label class="y-check"><input v-model="form.seller_pickable" type="checkbox" /> 卖家发布时可勾选</label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="closeModal">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="save">保存</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
#market-categories-page.categories-shell {
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

.row-sub {
  margin-top: 2px;
  max-width: 18rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}
.status-dot[data-tone='ok'] {
  background: #16a34a;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

.cat-name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.35rem;
}
.cat-title {
  font-weight: 600;
}
.cat-title.root {
  font-size: 14px;
}
.cat-toggle {
  width: 1.4rem;
  height: 1.4rem;
  border: 0;
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  line-height: 1;
  flex-shrink: 0;
}
.cat-leaf {
  display: inline-block;
  width: 1.4rem;
  text-align: center;
  opacity: 0.4;
  flex-shrink: 0;
}

#market-categories-page :deep(tr.is-disabled) {
  opacity: 0.55;
}

#market-categories-page :deep(.filter-grid .select-trigger),
#market-categories-page :deep(.filter-grid .select-trigger-sm),
#market-categories-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-categories-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-categories-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-categories-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-categories-page :deep(th.col-name),
#market-categories-page :deep(td.col-name) {
  min-width: 14rem;
}
#market-categories-page :deep(th.col-id),
#market-categories-page :deep(td.col-id) {
  min-width: 7rem;
}
#market-categories-page :deep(th.col-pick),
#market-categories-page :deep(td.col-pick),
#market-categories-page :deep(th.col-enabled),
#market-categories-page :deep(td.col-enabled),
#market-categories-page :deep(th.col-sort),
#market-categories-page :deep(td.col-sort) {
  min-width: 5rem;
}
#market-categories-page :deep(th.col-ops),
#market-categories-page :deep(td.col-ops) {
  min-width: 14rem;
  width: 14rem;
  box-sizing: border-box;
}
#market-categories-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-categories-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-categories-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

.y-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}
.y-modal {
  width: min(480px, 100%);
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head,
.y-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__foot {
  justify-content: flex-end;
}
.y-modal__body {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

@media (max-width: 900px) {
  .stats-row,
  .y-field-row {
    grid-template-columns: 1fr;
  }
}
</style>
