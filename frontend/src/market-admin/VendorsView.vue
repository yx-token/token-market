<script setup lang="ts">
/**
 * 厂商管理：TablePageLayout + filter-grid + DataTable，便于按接入状态/方式筛选。
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

defineOptions({ name: 'MarketAdminVendors' })

interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types: string[]
  consignment_min: number
  consignment_max: number
  buyback_base: Record<string, number>
  appraisal_base?: Record<string, number>
  default_concurrency: number
  extra_limits: Record<string, unknown>
}

const PLATFORM_LABEL: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  kimi: 'Kimi',
  zhipu: '智谱',
  minimax: 'MiniMax',
  opencode_go: 'OpenCode Go',
}

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '开放' },
  { value: 'false', label: '关闭' },
]

const AUTH_FILTER_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'oauth', label: 'OAuth' },
  { value: 'api_key', label: 'API Key' },
  { value: 'cookie', label: 'Cookie' },
  { value: 'coding', label: 'Coding' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  enabled: '',
  auth_type: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const vendors = ref<VendorConfig[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const editing = ref<VendorConfig | null>(null)
const form = reactive({
  enabled: false,
  authTypes: [] as string[],
  min: '',
  max: '',
  base: '',
  appraisal: '',
  concurrency: '',
  extra: '',
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'platform', label: '厂商', class: 'col-platform' },
  { key: 'enabled', label: '接入', class: 'col-enabled' },
  { key: 'auth_types', label: '接入方式', class: 'col-auth' },
  { key: 'consignment', label: '寄售区间', class: 'col-range' },
  { key: 'concurrency', label: '默认并发', class: 'col-conc' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const filtered = computed(() => {
  let rows = vendors.value
  if (queryParams.value.enabled === 'true') rows = rows.filter((v) => v.enabled)
  if (queryParams.value.enabled === 'false') rows = rows.filter((v) => !v.enabled)
  const auth = queryParams.value.auth_type.trim()
  if (auth) rows = rows.filter((v) => (v.auth_types || []).includes(auth))
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter(
      (v) =>
        v.platform.toLowerCase().includes(kw) ||
        (PLATFORM_LABEL[v.platform] || '').toLowerCase().includes(kw) ||
        (v.auth_types || []).some((t) => t.toLowerCase().includes(kw) || authLabel(t).toLowerCase().includes(kw)),
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
  const all = vendors.value
  const open = all.filter((v) => v.enabled).length
  return {
    total: all.length,
    open,
    closed: all.length - open,
  }
})

watch(
  () => [queryParams.value.keyword, queryParams.value.enabled, queryParams.value.auth_type, queryParams.value.pageSize],
  () => {
    queryParams.value.pageNum = 1
  },
)

function platLabel(p: string) {
  return PLATFORM_LABEL[p] || p
}

function authLabel(t: string) {
  if (t === 'oauth') return 'OAuth'
  if (t === 'api_key') return 'API Key'
  if (t === 'cookie') return 'Cookie'
  if (t === 'coding') return 'Coding'
  return t
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
    vendors.value = (await marketGet<VendorConfig[] | null>(`${MARKET_API_PREFIX}/vendors`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    vendors.value = []
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
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openEdit(v: VendorConfig) {
  editing.value = v
  Object.assign(form, {
    enabled: v.enabled,
    authTypes: [...(v.auth_types || [])],
    min: String(v.consignment_min),
    max: String(v.consignment_max),
    base: JSON.stringify(v.buyback_base ?? {}),
    appraisal: JSON.stringify(v.appraisal_base ?? {}),
    concurrency: String(v.default_concurrency),
    extra: JSON.stringify(v.extra_limits ?? {}),
  })
}

function closeModal() {
  editing.value = null
}

async function quickToggle(v: VendorConfig) {
  await run(v.enabled ? '已关闭接入' : '已开放接入', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/vendors/${v.platform}`, { enabled: !v.enabled }),
  )
}

async function save() {
  if (!editing.value) return
  let base: Record<string, number>
  let appraisal: Record<string, number>
  let extra: Record<string, unknown>
  try {
    base = JSON.parse(form.base)
    appraisal = JSON.parse(form.appraisal)
    extra = JSON.parse(form.extra)
    if (
      !appraisal ||
      Array.isArray(appraisal) ||
      typeof appraisal !== 'object' ||
      Object.values(appraisal).some((value) => typeof value !== 'number' || !Number.isFinite(value) || value <= 0)
    ) {
      throw new Error('估价基准无效')
    }
  } catch {
    error.value = '回收基础倍率、质检估价基准与限流参数需要是 JSON 对象；估价基准须为正数'
    return
  }
  if (!form.authTypes.length) {
    error.value = '至少开放一种接入方式'
    return
  }
  const body = {
    enabled: form.enabled,
    auth_types: form.authTypes,
    consignment_min: Number(form.min),
    consignment_max: Number(form.max),
    buyback_base: base,
    appraisal_base: appraisal,
    default_concurrency: Number(form.concurrency),
    extra_limits: extra,
  }
  if (await run('已保存', () => marketSend('PUT', `${MARKET_API_PREFIX}/vendors/${editing.value!.platform}`, body))) {
    closeModal()
  }
}

onMounted(load)
</script>

<template>
  <section id="market-vendors-page" class="vendors-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">厂商</h2>
          <p class="y-page-desc">开放接入、寄售倍率区间、回收/估价基准与默认并发</p>
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
                <div class="stat-card__label">厂商总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已开放</div>
                <div class="stat-card__value">{{ allStats.open.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">已关闭</div>
                <div class="stat-card__value">{{ allStats.closed.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">厂商信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="厂商名称 / 接入方式"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">接入</label>
              <Select
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ENABLED_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">接入方式</label>
              <Select
                v-model="queryParams.auth_type"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="AUTH_FILTER_OPTIONS"
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
            {{ getSerialNumber(pageRows.findIndex((r) => r.platform === row.platform)) }}
          </template>
          <template #cell-platform="{ row }">
            <div>{{ platLabel(row.platform) }}</div>
            <div class="row-sub">{{ row.platform }}</div>
          </template>
          <template #cell-enabled="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '开放' : '关闭' }}
            </span>
          </template>
          <template #cell-auth_types="{ row }">
            {{ (row.auth_types || []).map((t: string) => authLabel(t)).join('、') || '—' }}
          </template>
          <template #cell-consignment="{ row }">
            {{ row.consignment_min }}–{{ row.consignment_max }}×
          </template>
          <template #cell-concurrency="{ row }">{{ row.default_concurrency }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="quickToggle(row)">
                {{ row.enabled ? '关闭' : '开放' }}
              </button>
              <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无厂商" description="调整筛选条件后重试，或点击刷新重新加载" />
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
      <div class="y-modal" role="dialog" :aria-label="`编辑 ${platLabel(editing.platform)}`">
        <header class="y-modal__head">
          <h3>编辑 {{ platLabel(editing.platform) }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 开放卖家接入</label>
          <div class="y-field">
            <span>开放的接入方式</span>
            <div class="auth-list">
              <label v-for="t in editing.supported_auth_types || []" :key="t" class="y-check">
                <input v-model="form.authTypes" type="checkbox" :value="t" />
                {{ authLabel(t) }}
              </label>
            </div>
          </div>
          <div class="y-field-row">
            <label class="y-field">
              <span>寄售最低倍率</span>
              <input v-model="form.min" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>寄售最高倍率</span>
              <input v-model="form.max" class="input w-full" inputmode="decimal" />
            </label>
          </div>
          <label class="y-field">
            <span>回收基础倍率（JSON）</span>
            <input v-model="form.base" class="input w-full font-mono" />
            <span class="hint">须含 default，如 {"default":0.15,"max":0.2}</span>
          </label>
          <label class="y-field">
            <span>质检估价基准（JSON，正数）</span>
            <input v-model="form.appraisal" class="input w-full font-mono" />
          </label>
          <label class="y-field">
            <span>默认并发</span>
            <input v-model="form.concurrency" class="input w-full" inputmode="numeric" />
          </label>
          <label class="y-field">
            <span>限流参数 extra_limits（JSON）</span>
            <textarea v-model="form.extra" class="input w-full y-textarea font-mono" rows="3" />
          </label>
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
#market-vendors-page.vendors-shell {
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
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
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

#market-vendors-page :deep(.filter-grid .select-trigger),
#market-vendors-page :deep(.filter-grid .select-trigger-sm),
#market-vendors-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-vendors-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-vendors-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-vendors-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-vendors-page :deep(th.col-platform),
#market-vendors-page :deep(td.col-platform) {
  min-width: 8rem;
}
#market-vendors-page :deep(th.col-enabled),
#market-vendors-page :deep(td.col-enabled) {
  min-width: 5rem;
}
#market-vendors-page :deep(th.col-auth),
#market-vendors-page :deep(td.col-auth) {
  min-width: 10rem;
}
#market-vendors-page :deep(th.col-range),
#market-vendors-page :deep(td.col-range),
#market-vendors-page :deep(th.col-conc),
#market-vendors-page :deep(td.col-conc) {
  min-width: 6rem;
}
#market-vendors-page :deep(th.col-ops),
#market-vendors-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}
#market-vendors-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-vendors-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-vendors-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
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
  min-height: 72px;
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
