<script setup lang="ts">
/**
 * 平台账本 / 审计日志：TablePageLayout + DataTable。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet } from './api'

defineOptions({ name: 'MarketAdminAudit' })

interface PlatformLedger {
  cash: number
  revenue: number
  probe_cost: number
  adjustment: number
  buyer_balances: number
  buyer_store_credit?: number
  seller_payable: number
}

interface AuditLog {
  id: number
  actor_type: string
  actor_id?: number
  action: string
  target?: string
  detail: unknown
  created_at: string
}

const tab = ref<'ledger' | 'audit'>('ledger')
const ledger = ref<PlatformLedger | null>(null)
const logs = ref<AuditLog[]>([])
const loading = ref(false)
const error = ref('')

const initQueryParams = { pageNum: 1, pageSize: 15, keyword: '', actor_type: '' }
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'actor', label: '操作者', class: 'col-actor' },
  { key: 'action', label: '动作', class: 'col-action' },
  { key: 'target', label: '目标', class: 'col-target' },
  { key: 'detail', label: '详情', class: 'col-detail' },
  { key: 'created_at', label: '时间', class: 'col-time' },
]

const actorOptions = computed(() => {
  const set = new Set<string>()
  for (const l of logs.value) if (l.actor_type) set.add(l.actor_type)
  return [{ value: '', label: '全部' }, ...[...set].sort().map((v) => ({ value: v, label: v }))]
})

const filtered = computed(() => {
  let list = logs.value
  if (queryParams.value.actor_type) list = list.filter((l) => l.actor_type === queryParams.value.actor_type)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((l) =>
      [l.id, l.actor_type, l.actor_id, l.action, l.target, detailText(l.detail)]
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

watch(
  () => [queryParams.value.keyword, queryParams.value.actor_type, queryParams.value.pageSize],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function detailText(d: unknown) {
  if (d == null) return '—'
  if (typeof d === 'string') return d
  try {
    return JSON.stringify(d)
  } catch {
    return '—'
  }
}

function actorLabel(l: AuditLog) {
  if (l.actor_id == null) return l.actor_type || '—'
  return `${l.actor_type} ${l.actor_id}`
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function handleQuery() {
  queryParams.value.pageNum = 1
  if (tab.value === 'audit') load()
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
  if (tab.value === 'audit') load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'ledger') {
      ledger.value = await marketGet<PlatformLedger>(`${MARKET_API_PREFIX}/ledger`)
    } else {
      const q = queryParams.value.keyword.trim()
        ? `?q=${encodeURIComponent(queryParams.value.keyword.trim())}`
        : ''
      logs.value = (await marketGet<AuditLog[] | null>(`${MARKET_API_PREFIX}/audit-logs${q}`)) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    ledger.value = null
    logs.value = []
  } finally {
    loading.value = false
  }
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
})
onMounted(load)
</script>

<template>
  <section id="market-audit-page" class="audit-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'ledger' ? '平台账本' : '审计日志' }}</h2>
          <p class="y-page-desc">
            {{ tab === 'ledger' ? '平台现金、收入与应付概览' : '管理操作留痕' }}
          </p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>
      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <nav class="y-tabs mb-3">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'ledger' }" @click="tab = 'ledger'">账本</button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'audit' }" @click="tab = 'audit'">审计</button>
        </nav>

        <template v-if="tab === 'ledger'">
          <div class="section-block">
            <div class="section-title">账本概览</div>
            <p v-if="loading" class="y-msg">加载中…</p>
            <p v-else-if="!ledger" class="y-msg">暂无账本数据</p>
            <div v-else class="ledger-grid">
              <div class="ledger-card"><span>现金</span><strong>{{ ledger.cash }}</strong></div>
              <div class="ledger-card"><span>收入</span><strong>{{ ledger.revenue }}</strong></div>
              <div class="ledger-card"><span>检测成本</span><strong>{{ ledger.probe_cost }}</strong></div>
              <div class="ledger-card"><span>调整</span><strong>{{ ledger.adjustment }}</strong></div>
              <div class="ledger-card"><span>买家余额</span><strong>{{ ledger.buyer_balances }}</strong></div>
              <div class="ledger-card"><span>店铺抵扣</span><strong>{{ ledger.buyer_store_credit ?? 0 }}</strong></div>
              <div class="ledger-card"><span>卖家应付</span><strong>{{ ledger.seller_payable }}</strong></div>
            </div>
          </div>
        </template>

        <template v-else>
          <div class="section-block">
            <div class="section-title">审计信息</div>
            <div class="filter-grid">
              <div class="filter-item">
                <label class="filter-label">关键词</label>
                <div class="relative flex-1 w-full min-w-0">
                  <input
                    v-model="queryParams.keyword"
                    class="filter-input w-full"
                    type="search"
                    placeholder="动作 / 目标 / 操作者"
                    @keyup.enter="handleQuery"
                  />
                </div>
              </div>
              <div class="filter-item">
                <label class="filter-label">操作者</label>
                <Select
                  v-model="queryParams.actor_type"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="actorOptions"
                />
              </div>
              <div class="filter-actions lg:col-span-4">
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
      </template>

      <template v-if="tab === 'audit'" #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">{{ row.id }}</template>
          <template #cell-actor="{ row }">{{ actorLabel(row) }}</template>
          <template #cell-action="{ row }">{{ row.action }}</template>
          <template #cell-target="{ row }">{{ row.target || '—' }}</template>
          <template #cell-detail="{ row }">
            <span class="detail-cell" :title="detailText(row.detail)">{{ detailText(row.detail) }}</span>
          </template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #empty>
            <EmptyState title="暂无审计日志" description="调整筛选条件后重试" />
          </template>
        </DataTable>
      </template>

      <template v-if="tab === 'audit'" #pagination>
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
#market-audit-page.audit-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.page-heading { min-width: 0; }
.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.section-block { min-width: 0; }
.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
}
.ledger-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}
.ledger-card {
  display: grid;
  gap: 6px;
  padding: 14px;
  border-radius: 10px;
  background: #f8fafc;
  border: 1px solid rgb(0 0 0 / 6%);
}
.ledger-card span { font-size: 12px; color: #868686; }
.ledger-card strong { font-size: 18px; font-weight: 650; }
.detail-cell {
  display: inline-block;
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
#market-audit-page :deep(.filter-grid .select-trigger),
#market-audit-page :deep(.filter-grid .select-trigger-sm),
#market-audit-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-audit-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-audit-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
}
</style>
