<script setup lang="ts">
/**
 * 风控 / 计费异常：TablePageLayout + DataTable；用户显示邮箱。
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
import { sellerAccount } from './sellerDisplay'

defineOptions({ name: 'MarketAdminRisk' })

interface RiskEvent {
  id: number
  kind: string
  severity: string
  user_id?: number
  related_user_id?: number
  upstream_id?: number
  detail?: Record<string, unknown>
  status: string
  created_at: string
}

interface Anomaly {
  id: number
  rule: string
  buyer_user_id?: number
  amount?: number
  detail?: Record<string, unknown>
  status: string
  note?: string
  created_at: string
}

interface MarketUser {
  id: number
  email: string
}

const tab = ref<'risk' | 'anomalies'>('risk')
const risks = ref<RiskEvent[]>([])
const anomalies = ref<Anomaly[]>([])
const userEmailById = ref<Record<number, string>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const STATUS_OPTIONS = [
  { value: 'open', label: '待处理' },
  { value: 'resolved', label: '已处理' },
  { value: 'ignored', label: '已忽略' },
  { value: '', label: '全部' },
]

const SEVERITY_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: 'open',
  severity: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const riskColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: '编号', class: 'col-id' },
  { key: 'kind', label: '类型', class: 'col-kind' },
  { key: 'severity', label: '级别', class: 'col-sev' },
  { key: 'user', label: '用户', class: 'col-user' },
  { key: 'detail', label: '详情', class: 'col-detail' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'created_at', label: '时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const anomalyColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: '编号', class: 'col-id' },
  { key: 'rule', label: '规则', class: 'col-kind' },
  { key: 'user', label: '买家', class: 'col-user' },
  { key: 'amount', label: '金额', class: 'col-amt' },
  { key: 'detail', label: '详情', class: 'col-detail' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'created_at', label: '时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const columns = computed(() => (tab.value === 'risk' ? riskColumns : anomalyColumns))

function userLabel(id?: number | null) {
  if (!id) return '—'
  return sellerAccount(userEmailById.value[id], id)
}

const filteredRisks = computed(() => {
  let rows = risks.value
  if (queryParams.value.severity) rows = rows.filter((e) => e.severity === queryParams.value.severity)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((e) =>
      [e.id, e.kind, e.user_id, e.related_user_id, userLabel(e.user_id), userLabel(e.related_user_id)]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return rows
})

const filteredAnomalies = computed(() => {
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (!kw) return anomalies.value
  return anomalies.value.filter((a) =>
    [a.id, a.rule, a.buyer_user_id, a.note, userLabel(a.buyer_user_id)]
      .map((v) => String(v ?? '').toLowerCase())
      .some((s) => s.includes(kw)),
  )
})

const pageSource = computed(() => (tab.value === 'risk' ? filteredRisks.value : filteredAnomalies.value))
const total = computed(() => pageSource.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return pageSource.value.slice(start, start + queryParams.value.pageSize) as Array<RiskEvent & Anomaly>
})

watch(
  () => [queryParams.value.keyword, queryParams.value.severity, queryParams.value.pageSize, tab.value],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function detailText(d?: Record<string, unknown>) {
  if (!d) return '—'
  try {
    return JSON.stringify(d)
  } catch {
    return '—'
  }
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function statusLabel(s: string) {
  return ({ open: '待处理', resolved: '已处理', ignored: '已忽略' } as Record<string, string>)[s] || s
}

function handleQuery() {
  queryParams.value.pageNum = 1
  load()
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
}

async function loadUsers() {
  try {
    const users = (await marketGet<MarketUser[] | null>(`${MARKET_API_PREFIX}/users?limit=500`)) ?? []
    const map: Record<number, string> = {}
    for (const u of users) map[u.id] = u.email
    userEmailById.value = map
  } catch {
    /* ignore */
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const st = queryParams.value.status
    if (tab.value === 'risk') {
      const q = st ? `?status=${encodeURIComponent(st)}` : ''
      risks.value = (await marketGet<RiskEvent[] | null>(`${MARKET_API_PREFIX}/risk-events${q}`)) ?? []
    } else {
      const q = st ? `?status=${encodeURIComponent(st)}` : ''
      anomalies.value = (await marketGet<Anomaly[] | null>(`${MARKET_API_PREFIX}/anomalies${q}`)) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    risks.value = []
    anomalies.value = []
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function resolveRisk(e: RiskEvent, status: string) {
  return run('已处理', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/risk-events/${e.id}/resolve`, { status }),
  )
}

function resolveAnomaly(a: Anomaly, status: string) {
  const note = window.prompt('处理说明，可留空', '') ?? ''
  return run('异常已处理', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/anomalies/${a.id}/resolve`, { status, note }),
  )
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
  load()
})
watch(() => queryParams.value.status, load)

onMounted(async () => {
  await loadUsers()
  await load()
})
</script>

<template>
  <section id="market-risk-page" class="risk-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'risk' ? '风控事件' : '计费异常' }}</h2>
          <p class="y-page-desc">
            {{
              tab === 'risk'
                ? '未处理风险会影响提现审核；必要时到「用户与实名」冻结账号'
                : '用量/计费异常待确认'
            }}
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
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>
        <nav class="y-tabs mb-3">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'risk' }" @click="tab = 'risk'">风控</button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'anomalies' }" @click="tab = 'anomalies'">异常</button>
        </nav>
        <div class="section-block">
          <div class="section-title">筛选条件</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="编号 / 类型 / 用户邮箱"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select v-model="queryParams.status" class="flex-1 w-full min-w-0" size="sm" :options="STATUS_OPTIONS" />
            </div>
            <div v-show="tab === 'risk'" class="filter-item">
              <label class="filter-label">级别</label>
              <Select v-model="queryParams.severity" class="flex-1 w-full min-w-0" size="sm" :options="SEVERITY_OPTIONS" />
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
      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">{{ row.id }}</template>
          <template #cell-kind="{ row }">{{ row.kind }}</template>
          <template #cell-rule="{ row }">{{ row.rule }}</template>
          <template #cell-severity="{ row }">
            <span :class="row.severity === 'high' ? 'y-chip y-chip--err' : 'y-chip y-chip--warn'">
              {{ row.severity === 'high' ? '高' : row.severity === 'medium' ? '中' : row.severity }}
            </span>
          </template>
          <template #cell-user="{ row }">
            <template v-if="tab === 'risk'">
              <template v-if="row.user_id || row.related_user_id">
                <span v-if="row.user_id">{{ userLabel(row.user_id) }}</span>
                <span v-if="row.related_user_id">{{ row.user_id ? ' / ' : '' }}{{ userLabel(row.related_user_id) }}</span>
              </template>
              <span v-else>—</span>
            </template>
            <template v-else>{{ userLabel(row.buyer_user_id) }}</template>
          </template>
          <template #cell-amount="{ row }">{{ row.amount ?? '—' }}</template>
          <template #cell-detail="{ row }">
            <span class="detail-cell" :title="detailText(row.detail)">{{ detailText(row.detail) }}</span>
          </template>
          <template #cell-status="{ row }">{{ statusLabel(row.status) }}</template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="tab === 'risk' && row.status === 'open'">
                <button type="button" class="y-link" :disabled="busy" @click="resolveRisk(row, 'resolved')">处理</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="resolveRisk(row, 'ignored')">忽略</button>
              </template>
              <template v-else-if="tab === 'anomalies' && row.status === 'open'">
                <button type="button" class="y-link" :disabled="busy" @click="resolveAnomaly(row, 'resolved')">确认</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="resolveAnomaly(row, 'ignored')">忽略</button>
              </template>
              <span v-else>{{ tab === 'anomalies' ? row.note || '—' : '—' }}</span>
            </div>
          </template>
          <template #empty>
            <EmptyState :title="tab === 'risk' ? '暂无风险事件' : '暂无异常'" description="调整筛选条件后重试" />
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
#market-risk-page.risk-shell {
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
}
.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}
.detail-cell {
  display: inline-block;
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
#market-risk-page :deep(.filter-grid .select-trigger),
#market-risk-page :deep(.filter-grid .select-trigger-sm),
#market-risk-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-risk-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-risk-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
}
#market-risk-page :deep(th.col-ops),
#market-risk-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}
#market-risk-page :deep(thead th.col-ops) { background: #f5f7fa !important; }
#market-risk-page :deep(tbody td.col-ops) { background: #fff !important; }
#market-risk-page :deep(tbody tr:hover td.col-ops) { background: #f5f7fa !important; }
</style>
