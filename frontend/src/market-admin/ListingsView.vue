<script setup lang="ts">
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet } from './api'
import { useConsoleTabs } from '@/stores/consoleTabs'
import 'element-plus/es/components/date-picker/style/css'

defineOptions({ name: 'MarketAdminListings' })

const route = useRoute()
const router = useRouter()
const { open: openTab } = useConsoleTabs()

interface ListingRow {
  id: number
  upstream_id: number
  seller_id: number
  seller_email?: string
  mode: string
  status: string
  paused_by?: string | null
  multiplier?: number | null
  join_default_pool: boolean
  sku_kind?: string
  title?: string | null
  platform?: string
  created_at?: string
  updated_at?: string
}

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  startDate: '',
  endDate: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const rows = ref<ListingRow[]>([])
const loading = ref(false)
const error = ref('')

const statusOptions = [
  { value: '', label: '全部' },
  { value: 'active', label: '在售' },
  { value: 'paused', label: '已暂停' },
  { value: 'pending_confirm', label: '待确认' },
  { value: 'closed', label: '已关闭' },
]

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: '挂单 ID', class: 'w-24' },
  { key: 'title', label: '标题' },
  { key: 'seller', label: '卖家', class: 'w-48' },
  { key: 'upstream_id', label: '上游', class: 'w-24' },
  { key: 'platform', label: '平台', class: 'w-28' },
  { key: 'mode', label: '模式', class: 'w-24' },
  { key: 'status', label: '状态', class: 'w-24' },
  { key: 'multiplier', label: '倍率', class: 'w-20' },
  { key: 'created_at', label: '创建时间', class: 'w-44' },
  { key: 'updated_at', label: '更新时间', class: 'w-44' },
  { key: 'actions', label: '操作', class: 'w-24' },
]

const allStats = computed(() => {
  const all = rows.value
  const today = new Date()
  const isToday = (iso?: string) => {
    if (!iso) return false
    const d = new Date(iso)
    return (
      d.getFullYear() === today.getFullYear() &&
      d.getMonth() === today.getMonth() &&
      d.getDate() === today.getDate()
    )
  }
  return {
    total: all.length,
    todayCreated: all.filter((r) => isToday(r.created_at)).length,
    active: all.filter((r) => r.status === 'active').length,
    todayActive: all.filter((r) => r.status === 'active' && isToday(r.updated_at || r.created_at)).length,
    paused: all.filter((r) => r.status === 'paused').length,
    todayPaused: all.filter((r) => r.status === 'paused' && isToday(r.updated_at || r.created_at)).length,
  }
})

const supplyStatus = computed(() => {
  if (allStats.value.active > 0) return { text: '供给中', tone: 'run' as const }
  if (allStats.value.paused > 0) return { text: '部分暂停', tone: 'wait' as const }
  if (allStats.value.total === 0) return { text: '暂无挂单', tone: 'wait' as const }
  return { text: '无在售', tone: 'idle' as const }
})

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.status) {
    list = list.filter((r) => r.status === queryParams.value.status)
  }
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((r) =>
      [r.id, r.upstream_id, r.seller_id, r.seller_email, r.title, r.platform, r.mode]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  if (queryParams.value.startDate || queryParams.value.endDate) {
    list = list.filter((r) => {
      const ts = r.created_at || ''
      if (!ts) return false
      const start = queryParams.value.startDate
      const end = queryParams.value.endDate
      // 兼容 YYYY-MM-DD 与 YYYY-MM-DD HH:mm:ss
      const startKey = start.length <= 10 ? `${start} 00:00:00` : start
      const endKey = end.length <= 10 ? `${end} 23:59:59` : end
      const rowKey = ts.length <= 10 ? `${ts} 00:00:00` : ts.replace('T', ' ').slice(0, 19)
      if (start && rowKey < startKey) return false
      if (end && rowKey > endKey) return false
      return true
    })
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function statusLabel(s: string) {
  const map: Record<string, string> = {
    active: '在售',
    paused: '已暂停',
    closed: '已关闭',
    pending_confirm: '待确认',
  }
  return map[s] || s
}

function modeLabel(m: string) {
  if (m === 'consignment') return '寄售'
  if (m === 'direct') return '直销'
  return m || '—'
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function sellerLabel(row: ListingRow) {
  return row.seller_email?.trim() || `卖家 #${row.seller_id}`
}

async function getList() {
  loading.value = true
  error.value = ''
  try {
    const q = new URLSearchParams()
    q.set('limit', '500')
    if (queryParams.value.keyword.trim()) q.set('q', queryParams.value.keyword.trim())
    if (queryParams.value.status) q.set('status', queryParams.value.status)
    rows.value = (await marketGet<ListingRow[] | null>(`${MARKET_API_PREFIX}/listings?${q}`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

function handleQuery() {
  queryParams.value.pageNum = 1
  getList()
}

function resetQuery() {
  queryParams.value = { ...initQueryParams }
  handleQuery()
}

function openDetail(row: ListingRow) {
  const path = `/admin/market/listings/${row.id}`
  openTab(path, { title: `挂单 #${row.id}` })
  router.push(path)
}

function openCreate() {
  const path = '/admin/market/listings/new'
  openTab(path, { title: '新建挂单' })
  router.push(path)
}

function applyRouteQuery() {
  const q = route.query.q
  if (typeof q === 'string' && q.trim()) {
    queryParams.value.keyword = q.trim()
    queryParams.value.pageNum = 1
  }
}

watch(
  () => route.query,
  () => applyRouteQuery(),
)

watch(
  () => queryParams.value.pageSize,
  () => {
    queryParams.value.pageNum = 1
  },
)

onMounted(() => {
  applyRouteQuery()
  getList()
})
</script>

<template>
  <section id="market-listings-page" class="listings-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">挂单管理</h2>
          <p class="y-page-desc">卖家供给挂单总览，跨卖家检索与强制暂停/恢复/关闭</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" @click="openCreate">
          <Icon name="plus" size="sm" />
          新建挂单
        </button>
        <button
          type="button"
          class="btn btn-secondary page-action-btn"
          :disabled="loading"
          @click="getList"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">挂单总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天新建</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayCreated }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">在售挂单</div>
                <div class="stat-card__value">{{ allStats.active.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天在售变动</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayActive }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">已暂停</div>
                <div class="stat-card__value">{{ allStats.paused.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天暂停</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayPaused }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">供给状态</div>
                <div class="stat-card__badge" :data-tone="supplyStatus.tone">{{ supplyStatus.text }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">挂单信息</div>
          <!-- 工单列表标准：同一行 label+控件，时间加宽，操作按钮靠右 -->
          <div class="list-filter">
            <div class="list-filter__row">
              <div class="list-filter__item list-filter__item--time">
                <label class="list-filter__label">创建时间</label>
                <div class="list-filter__control">
                  <DateRangePicker
                    mode="datetimerange"
                    v-model:start-date="queryParams.startDate"
                    v-model:end-date="queryParams.endDate"
                  />
                </div>
              </div>
              <div class="list-filter__item">
                <label class="list-filter__label">挂单 ID</label>
                <div class="list-filter__control">
                  <input
                    v-model="queryParams.keyword"
                    class="filter-input w-full"
                    type="search"
                    placeholder="请输入挂单 ID / 卖家 / 标题"
                    @keyup.enter="handleQuery"
                  />
                </div>
              </div>
              <div class="list-filter__item">
                <label class="list-filter__label">状态</label>
                <div class="list-filter__control">
                  <Select v-model="queryParams.status" class="w-full" size="sm" :options="statusOptions" />
                </div>
              </div>
              <div class="list-filter__actions">
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
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">#{{ row.id }}</template>
          <template #cell-title="{ row }">{{ row.title || '—' }}</template>
          <template #cell-seller="{ row }">
            <div>{{ sellerLabel(row) }}</div>
            <div class="row-sub">#{{ row.seller_id }}</div>
          </template>
          <template #cell-upstream_id="{ row }">#{{ row.upstream_id }}</template>
          <template #cell-platform="{ row }">{{ row.platform || '—' }}</template>
          <template #cell-mode="{ row }">{{ modeLabel(row.mode) }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-status="row.status" />
              {{ statusLabel(row.status) }}
            </span>
          </template>
          <template #cell-multiplier="{ row }">{{ row.multiplier ?? '—' }}</template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-updated_at="{ row }">{{ fmtTime(row.updated_at) }}</template>
          <template #cell-actions="{ row }">
            <button type="button" class="y-link" @click="openDetail(row)">详情</button>
          </template>
          <template #empty>
            <EmptyState
              title="暂无挂单"
              description="调整筛选条件，或新建一条挂单"
              action-text="新建挂单"
              @action="openCreate"
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
#market-listings-page.listings-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

#market-listings-page :deep(.table-page-layout) {
  height: 100%;
  min-height: 0;
  gap: 12px;
}

/* 顶栏：白底卡片，左标题右操作（对齐参考） */
#market-listings-page :deep(.y-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 0;
  padding: 16px 20px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
  flex-shrink: 0;
}

.page-heading {
  min-width: 0;
}

#market-listings-page :deep(.y-title) {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  line-height: 26px;
  color: #1f1f1f;
}

#market-listings-page :deep(.y-page-desc) {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 20px;
  color: #8c8c8c;
}

#market-listings-page :deep(.y-header-actions) {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-left: auto;
  flex-shrink: 0;
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

/* —— 查询条件：对齐工单列表（同行、标签右对齐、按钮靠右） —— */
.list-filter {
  width: 100%;
}

.list-filter__row {
  display: grid;
  grid-template-columns: minmax(280px, 1.7fr) minmax(180px, 1fr) minmax(150px, 0.85fr) auto;
  align-items: center;
  gap: 12px 16px;
  width: 100%;
}

.list-filter__item {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.list-filter__item--time {
  min-width: 0;
}

.list-filter__label {
  width: 4.5rem;
  flex-shrink: 0;
  text-align: right;
  font-size: 13px;
  font-weight: 500;
  line-height: 32px;
  color: var(--color-text-secondary, #606266);
  white-space: nowrap;
}

.list-filter__control {
  flex: 1;
  min-width: 0;
  width: 100%;
}

.list-filter__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-shrink: 0;
}

.list-filter__actions .btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
}

@media (max-width: 1200px) {
  .list-filter__row {
    grid-template-columns: minmax(240px, 1.5fr) minmax(160px, 1fr) auto;
  }

  .list-filter__item:nth-child(3) {
    grid-column: 1 / 2;
  }

  .list-filter__actions {
    grid-column: 3 / 4;
    grid-row: 1;
  }
}

@media (max-width: 768px) {
  .list-filter__row {
    grid-template-columns: 1fr;
  }

  .list-filter__item:nth-child(3),
  .list-filter__actions {
    grid-column: auto;
    grid-row: auto;
  }

  .list-filter__actions {
    justify-content: flex-end;
  }
}

#market-listings-page :deep(.list-filter .el-date-editor.el-input__wrapper),
#market-listings-page :deep(.list-filter .el-date-editor.el-range-editor) {
  width: 100% !important;
  height: 32px !important;
  min-height: 32px !important;
}

#market-listings-page :deep(.list-filter .select-trigger),
#market-listings-page :deep(.list-filter .select-trigger-sm),
#market-listings-page :deep(.list-filter .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-listings-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-listings-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-listings-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--color-text-primary, #262626);
}

.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #bfbfbf;
  flex-shrink: 0;
}

.status-dot[data-status='active'] {
  background: var(--color-success, #52c41a);
}

.status-dot[data-status='paused'],
.status-dot[data-status='pending_confirm'] {
  background: var(--color-warning, #faad14);
}

.status-dot[data-status='closed'] {
  background: var(--color-error, #ff4d4f);
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

@media (max-width: 1100px) {
  .stats-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}

.stat-card {
  display: flex;
  gap: 12px;
  min-height: 72px;
  padding: 12px 14px;
  border-radius: 8px;
}

.stat-card > div {
  flex: 1;
  min-width: 0;
}

.stat-card__label {
  font-size: 12px;
  color: var(--color-text-secondary, #8c8c8c);
  line-height: 18px;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  line-height: 28px;
  color: var(--color-text-primary, #1f1f1f);
  font-variant-numeric: tabular-nums;
}

.stat-card__value--sm {
  font-size: 18px;
  line-height: 24px;
}

.stat-card__badge {
  display: inline-flex;
  margin-top: 8px;
  height: 24px;
  padding: 0 10px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  align-items: center;
  color: #fff;
}

.stat-card__badge[data-tone='run'] {
  background: var(--color-success, #52c41a);
}

.stat-card__badge[data-tone='wait'] {
  background: var(--color-warning, #faad14);
}

.stat-card__badge[data-tone='idle'] {
  background: #bfbfbf;
}

.stat-card--blue {
  background: linear-gradient(120deg, #e8f1ff 0%, #fafcff 100%);
}

.stat-card--green {
  background: linear-gradient(120deg, #edf9e8 0%, #fafcf8 100%);
}

.stat-card--orange {
  background: linear-gradient(120deg, #fff1eb 0%, #fffcfa 100%);
}

.stat-card--cyan {
  background: linear-gradient(120deg, #e8f9f8 0%, #f8fefe 100%);
}

.row-sub {
  margin-top: 2px;
  font-size: 12px;
  color: #8c8c8c;
}
</style>
