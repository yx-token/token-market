<script setup lang="ts">
/**
 * 质检项：TablePageLayout + filter-grid + DataTable；字段分列，保留编辑弹窗。
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

defineOptions({ name: 'MarketAdminChecks' })

interface InspectionCheck {
  key: string
  name: string
  flow?: string
  category?: string
  role?: string
  enabled: boolean
  pricing?: boolean | null
  default_pricing?: boolean
  calibrated: boolean
  calibration_note?: string
  coefficients: Record<string, number>
  thresholds: Record<string, number>
  description?: string
  advice?: string
}

const CATEGORY_LABEL: Record<string, string> = {
  capability: '能力',
  identity: '身份',
  quality: '质量',
  safety: '安全',
  other: '其他',
}

const FLOW_LABEL: Record<string, string> = {
  intelligence: '测智',
  degradation: '降智',
  identity: '身份',
}

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '开启' },
  { value: 'false', label: '关闭' },
]

const PRICING_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '开' },
  { value: 'false', label: '关' },
]

const CALIBRATED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '已完成' },
  { value: 'false', label: '待校准' },
]

const FLOW_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'intelligence', label: '测智' },
  { value: 'degradation', label: '降智' },
  { value: 'identity', label: '身份' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  category: '',
  flow: '',
  enabled: '',
  pricing: '',
  calibrated: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const checks = ref<InspectionCheck[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const editing = ref<InspectionCheck | null>(null)
const form = reactive({
  enabled: false,
  pricing: null as boolean | null,
  calibrated: false,
  calibrationNote: '',
  warning: 1 as number | string,
  severe: 1 as number | string,
  otherCoefficients: '',
  thresholds: '',
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'key', label: '键', class: 'col-key' },
  { key: 'name', label: '名称', class: 'col-name' },
  { key: 'flow', label: '流程', class: 'col-flow' },
  { key: 'category', label: '分组', class: 'col-category' },
  { key: 'enabled', label: '启用', class: 'col-enabled' },
  { key: 'pricing', label: '扣价', class: 'col-pricing' },
  { key: 'calibrated', label: '校准', class: 'col-calibrated' },
  { key: 'coefficients', label: '系数', class: 'col-coef' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const hasOtherCoefficients = computed(
  () => !!editing.value && Object.keys(editing.value.coefficients || {}).some((k) => k !== 'warning' && k !== 'severe'),
)

const categoryOptions = computed(() => {
  const set = new Set(checks.value.map((c) => c.category).filter(Boolean) as string[])
  return [
    { value: '', label: '全部' },
    ...[...set].sort().map((value) => ({ value, label: CATEGORY_LABEL[value] || value })),
  ]
})

const filtered = computed(() => {
  let rows = checks.value
  if (queryParams.value.category) rows = rows.filter((c) => c.category === queryParams.value.category)
  if (queryParams.value.flow) rows = rows.filter((c) => c.flow === queryParams.value.flow)
  if (queryParams.value.enabled === 'true') rows = rows.filter((c) => c.enabled)
  if (queryParams.value.enabled === 'false') rows = rows.filter((c) => !c.enabled)
  if (queryParams.value.pricing === 'true') {
    rows = rows.filter((c) => !!(c.pricing ?? c.default_pricing ?? false))
  }
  if (queryParams.value.pricing === 'false') {
    rows = rows.filter((c) => !(c.pricing ?? c.default_pricing ?? false))
  }
  if (queryParams.value.calibrated === 'true') rows = rows.filter((c) => c.calibrated)
  if (queryParams.value.calibrated === 'false') rows = rows.filter((c) => !c.calibrated)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter(
      (c) =>
        `${c.key} ${c.name} ${c.flow || ''} ${c.category || ''} ${c.description || ''}`
          .toLowerCase()
          .includes(kw),
    )
  }
  return rows
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

const allStats = computed(() => {
  const all = checks.value
  return {
    total: all.length,
    enabled: all.filter((c) => c.enabled).length,
    pending: all.filter((c) => !c.calibrated).length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.category,
    queryParams.value.flow,
    queryParams.value.enabled,
    queryParams.value.pricing,
    queryParams.value.calibrated,
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

function pricingOn(row: InspectionCheck) {
  return !!(row.pricing ?? row.default_pricing ?? false)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    checks.value = (await marketGet<InspectionCheck[] | null>(`${MARKET_API_PREFIX}/inspection/checks`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    checks.value = []
  } finally {
    loading.value = false
  }
}

function openEdit(check: InspectionCheck) {
  editing.value = check
  const other = Object.fromEntries(
    Object.entries(check.coefficients || {}).filter(([key]) => key !== 'warning' && key !== 'severe'),
  )
  Object.assign(form, {
    enabled: check.enabled,
    pricing: check.pricing ?? null,
    calibrated: check.calibrated,
    calibrationNote: check.calibration_note || '',
    warning: check.coefficients?.warning ?? 1,
    severe: check.coefficients?.severe ?? 1,
    otherCoefficients: JSON.stringify(other, null, 2),
    thresholds: JSON.stringify(check.thresholds || {}, null, 2),
  })
}

function closeModal() {
  editing.value = null
}

function numberObject(text: string): Record<string, number> {
  const value = JSON.parse(text)
  if (
    !value ||
    Array.isArray(value) ||
    typeof value !== 'object' ||
    Object.values(value).some((n) => typeof n !== 'number' || !Number.isFinite(n))
  ) {
    throw new Error('invalid')
  }
  return value
}

async function save() {
  if (!editing.value) return
  const warning = Number(form.warning)
  const severe = Number(form.severe)
  if ([warning, severe].some((v) => !Number.isFinite(v) || v <= 0 || v > 1.05)) {
    error.value = '系数须为大于 0 且不超过 1.05 的数值'
    return
  }
  let otherCoefficients: Record<string, number>
  let thresholds: Record<string, number>
  try {
    otherCoefficients = numberObject(form.otherCoefficients || '{}')
    thresholds = numberObject(form.thresholds || '{}')
  } catch {
    error.value = '其他系数与阈值需要是数值 JSON 对象'
    return
  }
  const coefficients = { ...otherCoefficients, warning, severe }
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/inspection/checks/${editing.value.key}`, {
      enabled: form.enabled,
      pricing: form.pricing,
      calibrated: form.calibrated,
      calibration_note: form.calibrationNote,
      coefficients,
      thresholds,
    })
    msg.value = '质检项已保存'
    closeModal()
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

async function quickToggle(check: InspectionCheck) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/inspection/checks/${check.key}`, {
      enabled: !check.enabled,
    })
    msg.value = `${check.key} 已${check.enabled ? '关闭' : '开启'}`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-checks-page" class="checks-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">质检项</h2>
          <p class="y-page-desc">启用规则、扣价开关、校准状态与系数阈值</p>
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

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">质检项总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已开启</div>
                <div class="stat-card__value">{{ allStats.enabled.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">待校准</div>
                <div class="stat-card__value">{{ allStats.pending.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">质检项信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="键 / 名称 / 描述"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">分组</label>
              <Select
                v-model="queryParams.category"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="categoryOptions"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">流程</label>
              <Select
                v-model="queryParams.flow"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="FLOW_OPTIONS"
              />
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
              <label class="filter-label">扣价</label>
              <Select
                v-model="queryParams.pricing"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PRICING_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">校准</label>
              <Select
                v-model="queryParams.calibrated"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="CALIBRATED_OPTIONS"
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
            {{ getSerialNumber(pageRows.findIndex((r) => r.key === row.key)) }}
          </template>
          <template #cell-key="{ row }">
            <span class="mono-cell">{{ row.key }}</span>
          </template>
          <template #cell-name="{ row }">{{ row.name || '—' }}</template>
          <template #cell-flow="{ row }">{{ row.flow ? FLOW_LABEL[row.flow] || row.flow : '—' }}</template>
          <template #cell-category="{ row }">
            {{ CATEGORY_LABEL[row.category || ''] || row.category || '—' }}
          </template>
          <template #cell-enabled="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '开启' : '关闭' }}
            </span>
          </template>
          <template #cell-pricing="{ row }">{{ pricingOn(row) ? '开' : '关' }}</template>
          <template #cell-calibrated="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.calibrated ? 'ok' : 'progress'" />
              {{ row.calibrated ? '已完成' : '待校准' }}
            </span>
          </template>
          <template #cell-coefficients="{ row }">
            关注 {{ row.coefficients?.warning ?? '—' }} · 严重 {{ row.coefficients?.severe ?? '—' }}
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="quickToggle(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无质检项" description="调整筛选条件后重试，或点击刷新重新加载" />
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

    <div v-if="editing" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="`编辑 ${editing.key}`">
        <header class="y-modal__head">
          <h3>编辑 {{ editing.key }} · {{ editing.name }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用质检项</label>
          <label class="y-field">
            <span>用于扣价</span>
            <select v-model="form.pricing" class="input w-full">
              <option :value="true">开</option>
              <option :value="false">关</option>
              <option :value="null">恢复默认</option>
            </select>
            <span class="hint">恢复默认后：{{ editing.default_pricing ? '开' : '关' }}</span>
          </label>
          <label class="y-check"><input v-model="form.calibrated" type="checkbox" /> 已完成运营校准</label>
          <p class="hint">校准只影响门槛（是否暂停），不影响扣价。</p>
          <label class="y-field">
            <span>校准记录</span>
            <textarea v-model="form.calibrationNote" class="input w-full y-textarea" rows="2" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>需关注系数</span>
              <input v-model.number="form.warning" type="number" min="0.01" max="1.05" step="0.01" class="input w-full" />
            </label>
            <label class="y-field">
              <span>严重异常系数</span>
              <input v-model.number="form.severe" type="number" min="0.01" max="1.05" step="0.01" class="input w-full" />
            </label>
          </div>
          <label v-if="hasOtherCoefficients" class="y-field">
            <span>其他系数（JSON）</span>
            <textarea v-model="form.otherCoefficients" class="input w-full y-textarea mono" rows="2" />
          </label>
          <label class="y-field">
            <span>阈值（JSON）</span>
            <textarea v-model="form.thresholds" class="input w-full y-textarea mono" rows="2" />
          </label>
          <p v-if="editing.description || editing.advice" class="hint">
            {{ editing.description }}{{ editing.advice }}
          </p>
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
#market-checks-page.checks-shell {
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

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
}

.mono {
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
.status-dot[data-tone='progress'] {
  background: #0555ce;
}

#market-checks-page :deep(.filter-grid .select-trigger),
#market-checks-page :deep(.filter-grid .select-trigger-sm),
#market-checks-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-checks-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-checks-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-checks-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-checks-page :deep(th.col-key),
#market-checks-page :deep(td.col-key) {
  min-width: 8rem;
}
#market-checks-page :deep(th.col-name),
#market-checks-page :deep(td.col-name) {
  min-width: 8rem;
}
#market-checks-page :deep(th.col-flow),
#market-checks-page :deep(td.col-flow),
#market-checks-page :deep(th.col-category),
#market-checks-page :deep(td.col-category),
#market-checks-page :deep(th.col-enabled),
#market-checks-page :deep(td.col-enabled),
#market-checks-page :deep(th.col-pricing),
#market-checks-page :deep(td.col-pricing),
#market-checks-page :deep(th.col-calibrated),
#market-checks-page :deep(td.col-calibrated) {
  min-width: 5rem;
}
#market-checks-page :deep(th.col-coef),
#market-checks-page :deep(td.col-coef) {
  min-width: 10rem;
}
#market-checks-page :deep(th.col-ops),
#market-checks-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}
#market-checks-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-checks-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-checks-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

.hint {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
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
  width: min(560px, 100%);
  max-height: min(92vh, 800px);
  overflow: auto;
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
.y-field > span:first-child {
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
.y-textarea {
  height: auto !important;
  min-height: 64px;
  padding: 8px 10px;
  resize: vertical;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
  .y-field-row {
    grid-template-columns: 1fr;
  }
}
</style>
