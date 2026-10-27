<script setup lang="ts">
/**
 * 检测记录：TablePageLayout + filter-grid + DataTable；上游优先邮箱。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import { useRoute } from 'vue-router'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet } from './api'

defineOptions({ name: 'MarketAdminProbeRuns' })

const route = useRoute()

interface SuiteStat {
  items: number
  passed: number
}

interface ProbeRunRecord {
  id: number
  code?: string
  upstream_id: number
  upstream_code?: string
  status: string
  trigger?: string
  verdict?: string | null
  platform?: string
  auth_type?: string
  seller_id?: number
  account_email?: string
  is_reference?: boolean
  model?: string
  models?: string[]
  queued_at?: string
  finished_at?: string | null
  items?: number
  passed?: number
  suites?: Record<string, SuiteStat>
}

interface ProbeRunResult {
  item_id?: number
  suite: string
  check_key?: string
  model?: string
  passed?: boolean | null
  error_class?: string
  duration_ms?: number
  excerpt?: string
  instance_idx?: number
  step?: number
}

interface ProbeRunDetail {
  run: ProbeRunRecord
  upstream_code?: string
  results: ProbeRunResult[]
  reference?: ProbeRunRecord
  reference_results?: ProbeRunResult[]
}

const PLATFORM_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: '智谱' },
  { value: 'minimax', label: 'MiniMax' },
]

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'queued', label: '排队中' },
  { value: 'running', label: '运行中' },
  { value: 'succeeded', label: '成功' },
  { value: 'passed', label: '通过' },
  { value: 'failed', label: '失败' },
  { value: 'error', label: '错误' },
  { value: 'third_party', label: '第三方' },
]

const TRIGGER_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'onboarding', label: '接入' },
  { value: 'admin', label: '管理端' },
  { value: 'seller_manual', label: '卖家手测' },
  { value: 'appeal', label: '申诉' },
  { value: 'scheduled', label: '定时' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  platform: '',
  trigger: '',
}

const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const rows = ref<ProbeRunRecord[]>([])
const loading = ref(false)
const error = ref('')

const detail = ref<ProbeRunDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const showDetail = ref(false)

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'code', label: '编号', class: 'col-code' },
  { key: 'upstream', label: '上游账号', class: 'col-upstream' },
  { key: 'upstream_code', label: '上游编号', class: 'col-ucode' },
  { key: 'platform', label: '平台', class: 'col-platform' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'trigger', label: '触发', class: 'col-trigger' },
  { key: 'suites', label: '套件', class: 'col-suites' },
  { key: 'time', label: '时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

function runCode(r?: ProbeRunRecord | null) {
  return String(r?.code || '').trim() || String(r?.id ?? '')
}

function upstreamAccount(r?: ProbeRunRecord | null) {
  const email = String(r?.account_email || '').trim()
  if (email) return email
  return '未命名上游'
}

function upstreamCode(r?: ProbeRunRecord | null, fallback?: string) {
  return String(fallback || r?.upstream_code || '').trim() || '—'
}

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.status) list = list.filter((r) => r.status === queryParams.value.status)
  if (queryParams.value.platform) list = list.filter((r) => r.platform === queryParams.value.platform)
  if (queryParams.value.trigger) list = list.filter((r) => r.trigger === queryParams.value.trigger)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((r) =>
      [r.code, r.upstream_code, r.id, r.upstream_id, r.account_email, r.platform, r.status, r.trigger, r.verdict, r.model]
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
    running: all.filter((r) => ['queued', 'running', 'third_party'].includes(r.status)).length,
    ok: all.filter((r) => ['succeeded', 'passed'].includes(r.status)).length,
    fail: all.filter((r) => ['failed', 'error'].includes(r.status)).length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.status,
    queryParams.value.platform,
    queryParams.value.trigger,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function fmtTime(v?: string | null) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function suiteText(r: ProbeRunRecord) {
  return Object.entries(r.suites ?? {})
    .map(([s, c]) => `${s} ${c.passed}/${c.items}`)
    .join(' · ')
}

function statusTone(status: string) {
  if (status === 'succeeded' || status === 'passed') return 'ok'
  if (status === 'failed' || status === 'error') return 'err'
  if (['queued', 'running', 'third_party'].includes(status)) return 'progress'
  return 'idle'
}

function statusLabel(status: string) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    succeeded: '成功',
    passed: '通过',
    failed: '失败',
    error: '错误',
    third_party: '第三方',
  }
  return map[status] || status || '—'
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

function applyRouteQuery() {
  const upstreamId = route.query.upstream_id
  if (typeof upstreamId === 'string' && /^\d+$/.test(upstreamId.trim())) {
    queryParams.value.keyword = upstreamId.trim()
    queryParams.value.pageNum = 1
    return
  }
  const q = route.query.q
  if (typeof q === 'string' && q.trim()) {
    queryParams.value.keyword = q.trim()
    queryParams.value.pageNum = 1
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    applyRouteQuery()
    rows.value = (await marketGet<ProbeRunRecord[] | null>(`${MARKET_API_PREFIX}/probe-runs`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

watch(
  () => route.query,
  () => applyRouteQuery(),
)

async function openDetail(row: ProbeRunRecord) {
  showDetail.value = true
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  try {
    detail.value = await marketGet<ProbeRunDetail>(`${MARKET_API_PREFIX}/probe-runs/${row.id}`)
  } catch (e) {
    detailError.value = e instanceof Error ? e.message : '加载明细失败'
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  showDetail.value = false
}

const detailResults = computed(() => detail.value?.results ?? [])

onMounted(load)
</script>

<template>
  <section id="market-probe-runs-page" class="probe-runs-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">检测记录</h2>
          <p class="y-page-desc">所有上游的检测；点「明细」查看逐题结果</p>
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
        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div class="stat-card__label">总记录</div>
              <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div class="stat-card__label">进行中</div>
              <div class="stat-card__value">{{ allStats.running.toLocaleString() }}</div>
            </article>
            <article class="stat-card stat-card--green">
              <div class="stat-card__label">成功</div>
              <div class="stat-card__value">{{ allStats.ok.toLocaleString() }}</div>
            </article>
            <article class="stat-card stat-card--orange">
              <div class="stat-card__label">失败</div>
              <div class="stat-card__value">{{ allStats.fail.toLocaleString() }}</div>
            </article>
          </div>
        </div>
        <div class="section-block mt-3">
          <div class="section-title">检测信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="编号 / 邮箱 / 平台 / 状态"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select v-model="queryParams.status" class="flex-1 w-full min-w-0" size="sm" :options="STATUS_OPTIONS" />
            </div>
            <div class="filter-item">
              <label class="filter-label">平台</label>
              <Select v-model="queryParams.platform" class="flex-1 w-full min-w-0" size="sm" :options="PLATFORM_OPTIONS" />
            </div>
            <div class="filter-item">
              <label class="filter-label">触发</label>
              <Select v-model="queryParams.trigger" class="flex-1 w-full min-w-0" size="sm" :options="TRIGGER_OPTIONS" />
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
          <template #cell-code="{ row }">{{ runCode(row) }}</template>
          <template #cell-upstream="{ row }">
            <div class="account-cell">
              <span>{{ upstreamAccount(row) }}</span>
              <span v-if="row.is_reference" class="ref-tag">基线</span>
            </div>
          </template>
          <template #cell-upstream_code="{ row }">{{ upstreamCode(row) }}</template>
          <template #cell-platform="{ row }">{{ row.platform || '—' }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ statusLabel(row.status) }}
            </span>
          </template>
          <template #cell-trigger="{ row }">{{ row.trigger || '—' }}</template>
          <template #cell-suites="{ row }">
            <span class="suites" :title="suiteText(row)">{{ suiteText(row) || '—' }}</span>
          </template>
          <template #cell-time="{ row }">{{ fmtTime(row.finished_at || row.queued_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" @click="openDetail(row)">明细</button>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无检测记录" description="调整筛选条件，或从上游页发起检测" />
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

    <Teleport to="body">
      <div v-if="showDetail" class="probe-detail-mask" @click.self="closeDetail">
        <div class="probe-detail-modal" role="dialog" aria-modal="true" aria-label="检测明细">
          <header class="probe-detail-modal__head">
            <h3>检测 {{ runCode(detail?.run) || '…' }}</h3>
            <button type="button" class="y-link" @click="closeDetail">关闭</button>
          </header>
          <div class="probe-detail-modal__body">
            <p v-if="detailLoading" class="y-msg">加载明细…</p>
            <p v-else-if="detailError" class="y-msg y-msg--error">{{ detailError }}</p>
            <template v-else-if="detail">
              <dl class="kv">
                <div>
                  <dt>上游账号</dt>
                  <dd>{{ upstreamAccount(detail.run) }}</dd>
                </div>
                <div>
                  <dt>上游编号</dt>
                  <dd>{{ upstreamCode(detail.run, detail.upstream_code) }}</dd>
                </div>
                <div>
                  <dt>状态</dt>
                  <dd>{{ detail.run.status }} / {{ detail.run.verdict || '—' }}</dd>
                </div>
                <div>
                  <dt>触发</dt>
                  <dd>{{ detail.run.trigger || '—' }}</dd>
                </div>
                <div>
                  <dt>基线对照</dt>
                  <dd>
                    <template v-if="detail.reference">
                      {{ upstreamAccount(detail.reference) }} · {{ detail.reference.passed ?? '—' }}/{{
                        detail.reference.items ?? '—'
                      }}
                    </template>
                    <template v-else>—</template>
                  </dd>
                </div>
              </dl>
              <p v-if="!detailResults.length" class="y-msg">无逐题结果</p>
              <div v-else class="detail-table-wrap">
                <table class="detail-table">
                  <thead>
                    <tr>
                      <th>套件</th>
                      <th>题/检查</th>
                      <th>模型</th>
                      <th>通过</th>
                      <th>耗时</th>
                      <th>摘要</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(res, i) in detailResults" :key="i">
                      <td>{{ res.suite }}</td>
                      <td>
                        <span v-if="res.check_key">{{ res.check_key }}</span>
                        <span v-else-if="res.item_id">{{ res.item_id }}</span>
                        <span v-if="res.instance_idx != null" class="sub">
                          · i{{ res.instance_idx }}/s{{ res.step ?? 0 }}
                        </span>
                      </td>
                      <td>{{ res.model || '—' }}</td>
                      <td>
                        <span
                          :class="
                            res.error_class ? 'y-chip y-chip--err' : res.passed ? 'y-chip y-chip--ok' : 'y-chip'
                          "
                        >
                          {{ res.error_class || (res.passed ? '通过' : '未通过') }}
                        </span>
                      </td>
                      <td>{{ res.duration_ms != null ? `${res.duration_ms}ms` : '—' }}</td>
                      <td class="excerpt" :title="res.excerpt || ''">{{ res.excerpt || '—' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
          </div>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
#market-probe-runs-page.probe-runs-shell {
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
  color: var(--color-text-primary, #262626);
}
.stats-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.stat-card {
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}
.stat-card--blue { background: linear-gradient(135deg, #eff6ff, #f8fafc); }
.stat-card--green { background: linear-gradient(135deg, #ecfdf5, #f8fafc); }
.stat-card--cyan { background: linear-gradient(135deg, #ecfeff, #f8fafc); }
.stat-card--orange { background: linear-gradient(135deg, #fff7ed, #f8fafc); }
.stat-card__label { font-size: 12px; color: #64748b; }
.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.2;
}
.account-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.ref-tag {
  display: inline-flex;
  height: 20px;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  align-items: center;
  color: var(--color-primary, #0555ce);
  background: rgba(5, 85, 206, 0.1);
}
.suites {
  display: inline-block;
  max-width: 12rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
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
.status-dot[data-tone='ok'] { background: #16a34a; }
.status-dot[data-tone='err'] { background: #ef4444; }
.status-dot[data-tone='progress'] { background: #0555ce; }
#market-probe-runs-page :deep(.filter-grid .select-trigger),
#market-probe-runs-page :deep(.filter-grid .select-trigger-sm),
#market-probe-runs-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-probe-runs-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}
#market-probe-runs-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}
#market-probe-runs-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}
#market-probe-runs-page :deep(th.col-code),
#market-probe-runs-page :deep(td.col-code),
#market-probe-runs-page :deep(th.col-ucode),
#market-probe-runs-page :deep(td.col-ucode) { min-width: 9rem; }
#market-probe-runs-page :deep(th.col-upstream),
#market-probe-runs-page :deep(td.col-upstream) { min-width: 10rem; }
#market-probe-runs-page :deep(th.col-ops),
#market-probe-runs-page :deep(td.col-ops) {
  min-width: 5rem;
  width: 5rem;
  box-sizing: border-box;
}
#market-probe-runs-page :deep(thead th.col-ops) { background: #f5f7fa !important; }
#market-probe-runs-page :deep(tbody td.col-ops) { background: #fff !important; }
#market-probe-runs-page :deep(tbody tr:hover td.col-ops) { background: #f5f7fa !important; }
.sub { font-size: 12px; color: var(--color-text-secondary, #868686); }
.kv {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  margin: 0 0 12px;
  font-size: 13px;
}
.kv dt { color: var(--color-text-secondary, #868686); }
.kv dd { margin: 2px 0 0; }
.detail-table-wrap {
  overflow: auto;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
}
.detail-table {
  width: 100%;
  font-size: 12px;
  border-collapse: collapse;
}
.detail-table th,
.detail-table td {
  padding: 8px 10px;
  text-align: left;
  border-bottom: 1px solid rgba(15, 23, 42, 0.06);
  vertical-align: top;
}
.detail-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: #f8fafc;
  font-weight: 600;
  color: #64748b;
}
.excerpt {
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.probe-detail-mask {
  position: fixed;
  inset: 0;
  z-index: 2000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 48%);
}
.probe-detail-modal {
  display: flex;
  flex-direction: column;
  width: min(960px, 100%);
  max-height: min(88vh, 820px);
  overflow: hidden;
  border-radius: 12px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  box-shadow: 0 20px 56px rgb(0 0 0 / 22%);
}
.probe-detail-modal__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-shrink: 0;
  padding: 14px 18px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  background: #fff;
}
.probe-detail-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: #1f1f1f;
}
.probe-detail-modal__body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 14px 18px 18px;
}
@media (max-width: 1100px) {
  .stats-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
