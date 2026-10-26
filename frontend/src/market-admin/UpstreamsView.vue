<script setup lang="ts">
/**
 * 卖家上游管理：对齐挂单列表 TablePageLayout / list-filter / DataTable 标准；
 * 状态点用蓝/灰表达进行中，管理端不用黄色 chip。
 */
import { computed, nextTick, onMounted, reactive, ref, toRefs, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { useConsoleTabs } from '@/stores/consoleTabs'
import 'element-plus/es/components/date-picker/style/css'

defineOptions({ name: 'MarketAdminUpstreams' })

const route = useRoute()
const router = useRouter()
const { open: openTab } = useConsoleTabs()

interface Upstream {
  id: number
  seller_id: number
  seller_email?: string
  platform: string
  auth_type?: string
  account_email?: string
  status: string
  health?: string
  health_reason?: string
  failure_reason?: string
  grade?: string
  privacy_ok?: boolean | null
  is_reference?: boolean
  created_at?: string
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
  { value: 'opencode_go', label: 'OpenCode Go' },
]

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'ready', label: '就绪' },
  { value: 'listed', label: '已挂单' },
  { value: 'selecting_models', label: '待选模型' },
  { value: 'verifying', label: '检测中' },
  { value: 'suspended', label: '已冻结' },
  { value: 'verify_failed', label: '检测失败' },
  { value: 'draft', label: '草稿' },
  { value: 'authorizing', label: '授权中' },
  { value: 'removing', label: '移除中' },
]

const STATUS_LABEL: Record<string, string> = Object.fromEntries(
  STATUS_OPTIONS.filter((o) => o.value).map((o) => [o.value, o.label]),
)

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  platform: '',
  startDate: '',
  endDate: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

interface MarketUser {
  id: number
  email: string
  status: string
}

interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types?: string[]
}

const rows = ref<Upstream[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const showCreate = ref(false)
const createBusy = ref(false)
const createError = ref('')
const vendors = ref<VendorConfig[]>([])
const userOptions = ref<MarketUser[]>([])
const userKeyword = ref('')
const createForm = reactive({
  seller_id: '' as string | number,
  platform: '',
  auth_type: '',
})

/** 管理端兜底：与 listing.SupportedAuthTypes 对齐，避免厂商 auth_types 未开放时下拉为空 */
const AUTH_FALLBACK: Record<string, string[]> = {
  anthropic: ['oauth', 'setup-token'],
  openai: ['oauth'],
  gemini: ['oauth', 'google_one'],
  antigravity: ['oauth'],
  grok: ['oauth'],
  kimi: ['coding'],
  zhipu: ['coding', 'coding-intl'],
  minimax: ['coding', 'coding-intl'],
  opencode_go: ['go'],
}

const AUTH_LABEL: Record<string, string> = {
  oauth: 'OAuth 授权',
  'setup-token': 'Setup Token',
  google_one: 'Google One',
  coding: 'Coding Plan',
  'coding-intl': 'Coding Plan 国际站',
  go: 'OpenCode Go',
}

function authTypesForPlatform(platform: string): string[] {
  if (!platform) return []
  const v = vendors.value.find((x) => x.platform === platform)
  const supported = Array.isArray(v?.supported_auth_types) ? v!.supported_auth_types.filter(Boolean) : []
  const opened = Array.isArray(v?.auth_types) ? v!.auth_types.filter(Boolean) : []
  // 管理端优先全部已实现方式；否则已开放；再否则本地兜底
  const merged = [...new Set([...supported, ...opened])]
  if (merged.length) return merged
  return AUTH_FALLBACK[platform] || ['oauth']
}

const createAuthOptions = computed(() =>
  authTypesForPlatform(String(createForm.platform || '')).map((t) => ({
    value: t,
    label: AUTH_LABEL[t] || t,
  })),
)

const createPlatformOptions = computed(() => {
  const fromApi = vendors.value
    .filter((v) => v.enabled)
    .map((v) => ({
      value: v.platform,
      label: PLATFORM_OPTIONS.find((o) => o.value === v.platform)?.label || v.platform,
    }))
  if (fromApi.length) return fromApi
  // 厂商列表拉取失败时仍可选手动接入
  return Object.keys(AUTH_FALLBACK).map((p) => ({
    value: p,
    label: PLATFORM_OPTIONS.find((o) => o.value === p)?.label || p,
  }))
})

const createSellerOptions = computed(() => {
  const list = userOptions.value.filter((u) => u.status !== 'frozen')
  return list.map((u) => ({
    value: String(u.id),
    label: `${u.email}（#${u.id}）`,
  }))
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'account', label: '账号' },
  { key: 'seller', label: '卖家', class: 'w-48' },
  { key: 'platform', label: '厂商', class: 'w-36' },
  { key: 'status', label: '状态', class: 'w-28' },
  { key: 'health', label: '健康', class: 'w-24' },
  { key: 'grade', label: '等级', class: 'w-20' },
  { key: 'privacy', label: '隐私', class: 'w-28' },
  { key: 'created_at', label: '接入时间', class: 'w-44' },
  { key: 'actions', label: '操作', class: 'w-64' },
]

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.status) list = list.filter((u) => u.status === queryParams.value.status)
  if (queryParams.value.platform) list = list.filter((u) => u.platform === queryParams.value.platform)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((u) =>
      [u.id, u.seller_id, u.seller_email, u.account_email, u.platform, u.failure_reason, u.health_reason]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  if (queryParams.value.startDate || queryParams.value.endDate) {
    list = list.filter((u) => {
      const ts = u.created_at || ''
      if (!ts) return false
      const start = queryParams.value.startDate
      const end = queryParams.value.endDate
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
  const pending = all.filter((u) =>
    ['draft', 'authorizing', 'selecting_models', 'verifying'].includes(u.status),
  )
  const ready = all.filter((u) => u.status === 'ready')
  const blocked = all.filter((u) =>
    ['suspended', 'verify_failed', 'failed', 'removing'].includes(u.status),
  )
  return {
    total: all.length,
    todayCreated: all.filter((u) => isToday(u.created_at)).length,
    ready: ready.length,
    todayReady: ready.filter((u) => isToday(u.created_at)).length,
    pending: pending.length,
    todayPending: pending.filter((u) => isToday(u.created_at)).length,
    blocked: blocked.length,
    todayBlocked: blocked.filter((u) => isToday(u.created_at)).length,
  }
})

const supplyStatus = computed(() => {
  if (allStats.value.ready > 0) return { text: '可挂单', tone: 'run' as const }
  if (allStats.value.pending > 0) return { text: '接入中', tone: 'wait' as const }
  if (allStats.value.total === 0) return { text: '暂无上游', tone: 'wait' as const }
  return { text: '无就绪', tone: 'idle' as const }
})

const showAddHint = computed(() => {
  const raw = route.query.add
  const v = Array.isArray(raw) ? raw[0] : raw
  return v === '1' || v === 'true'
})

const focusId = computed(() => {
  const raw = route.query.focus
  const n = Number(Array.isArray(raw) ? raw[0] : raw)
  return Number.isFinite(n) && n > 0 ? n : 0
})

function platformLabel(v: string) {
  return PLATFORM_OPTIONS.find((o) => o.value === v)?.label || v
}

function statusLabel(status: string) {
  return STATUS_LABEL[status] || status || '—'
}

function statusTone(status: string): string {
  if (status === 'ready' || status === 'listed') return 'ok'
  if (['suspended', 'verify_failed', 'failed'].includes(status)) return 'err'
  if (['draft', 'authorizing', 'selecting_models', 'verifying', 'removing'].includes(status)) return 'progress'
  return 'idle'
}

function displayName(u: Upstream) {
  return u.account_email || `上游 #${u.id}`
}

function sellerLabel(u: Upstream) {
  return u.seller_email?.trim() || `卖家 #${u.seller_id}`
}

function privacyLabel(u: Upstream) {
  if (u.privacy_ok === undefined || u.privacy_ok === null) return '—'
  return u.privacy_ok ? '已关闭训练' : '未关闭'
}

function healthLabel(h?: string) {
  if (!h || h === 'unknown') return '未知'
  const map: Record<string, string> = {
    healthy: '健康',
    unhealthy: '异常',
    degraded: '降级',
  }
  return map[h] || h
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

async function getList() {
  loading.value = true
  error.value = ''
  try {
    rows.value = (await marketGet<Upstream[] | null>(`${MARKET_API_PREFIX}/upstreams`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
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
    await getList()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function probe(u: Upstream) {
  return run(`上游 #${u.id} 已排队检测`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/probe`),
  )
}

function suspend(u: Upstream) {
  const reason = window.prompt('冻结原因')
  if (reason === null) return
  return run(`上游 #${u.id} 已冻结`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/suspend`, { reason }),
  )
}

function unsuspend(u: Upstream) {
  return run(`上游 #${u.id} 已解冻`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/unsuspend`),
  )
}

function remove(u: Upstream) {
  if (!window.confirm(`强制删除上游 #${u.id}？`)) return
  return run(`上游 #${u.id} 已删除`, () =>
    marketSend('DELETE', `${MARKET_API_PREFIX}/upstreams/${u.id}`),
  )
}

function openEdit(u: Upstream) {
  const path = `/admin/market/upstreams/${u.id}`
  openTab(path, { title: `上游 #${u.id}` })
  router.push(path)
}

function editLabel(u: Upstream) {
  if (['draft', 'authorizing', 'failed'].includes(u.status)) return '继续接入'
  if (u.status === 'selecting_models') return '选模型'
  return '编辑'
}

async function loadVendors() {
  try {
    vendors.value = (await marketGet<VendorConfig[] | null>(`${MARKET_API_PREFIX}/vendors`)) ?? []
  } catch {
    vendors.value = []
  }
}

async function searchUsers() {
  const q = new URLSearchParams()
  q.set('limit', '50')
  if (userKeyword.value.trim()) q.set('q', userKeyword.value.trim())
  try {
    userOptions.value =
      (await marketGet<MarketUser[] | null>(`${MARKET_API_PREFIX}/users?${q}`)) ?? []
  } catch {
    userOptions.value = []
  }
}

async function openCreate() {
  createError.value = ''
  createForm.seller_id = ''
  createForm.platform = ''
  createForm.auth_type = ''
  userKeyword.value = ''
  showCreate.value = true
  await Promise.all([loadVendors(), searchUsers()])
  if (!createForm.platform && createPlatformOptions.value.length) {
    createForm.platform = createPlatformOptions.value[0].value
  }
  await nextTick()
  syncCreateAuthType()
}

function syncCreateAuthType() {
  const opts = createAuthOptions.value
  if (!opts.length) {
    createForm.auth_type = ''
    return
  }
  if (!opts.some((o) => o.value === createForm.auth_type)) {
    createForm.auth_type = opts[0].value
  }
}

watch(
  () => createForm.platform,
  async () => {
    await nextTick()
    syncCreateAuthType()
  },
)

async function submitCreate() {
  const sellerId = Number(createForm.seller_id)
  if (!sellerId) {
    createError.value = '请选择卖家用户'
    return
  }
  if (!createForm.platform || !createForm.auth_type) {
    createError.value = '请选择厂商与接入方式'
    return
  }
  createBusy.value = true
  createError.value = ''
  try {
    const created = await marketSend<Upstream>('POST', `${MARKET_API_PREFIX}/upstreams`, {
      seller_id: sellerId,
      platform: createForm.platform,
      auth_type: createForm.auth_type,
    })
    showCreate.value = false
    msg.value = `已为卖家创建上游 #${created.id}，请继续完成接入`
    await getList()
    openEdit(created)
  } catch (e) {
    createError.value = e instanceof Error ? e.message : '创建失败'
  } finally {
    createBusy.value = false
  }
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  queryParams.value = { ...initQueryParams }
  handleQuery()
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
  void getList()
})
</script>

<template>
  <section id="market-upstreams-page" class="upstreams-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">卖家上游</h2>
          <p class="y-page-desc">代用户接入上游、选模型与检测；仅就绪状态可挂单</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" @click="openCreate">
          <Icon name="plus" size="sm" />
          添加上游
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
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>
        <p v-if="showAddHint" class="y-msg mb-2">
          从「新建挂单」过来：可点「添加上游」代用户接入，或对已有账号点「继续接入 / 选模型」推进到
          <b>就绪</b> 后再回挂单页刷新。
        </p>
        <p v-else-if="focusId" class="y-msg mb-2">
          已定位上游 #{{ focusId }}。可点「继续接入 / 选模型」完成编辑。
        </p>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">上游总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天新建</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayCreated }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">可挂单（就绪）</div>
                <div class="stat-card__value">{{ allStats.ready.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天就绪</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayReady }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">接入中</div>
                <div class="stat-card__value">{{ allStats.pending.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">当天接入变动</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.todayPending }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">供给状态</div>
                <div class="stat-card__badge" :data-tone="supplyStatus.tone">{{ supplyStatus.text }}</div>
              </div>
              <div>
                <div class="stat-card__label">异常 / 冻结</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.blocked }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">上游信息</div>
          <!-- 工单列表标准：同一行 label+控件，时间加宽，操作按钮靠右 -->
          <div class="list-filter">
            <div class="list-filter__row">
              <div class="list-filter__item list-filter__item--time">
                <label class="list-filter__label">接入时间</label>
                <div class="list-filter__control">
                  <DateRangePicker
                    mode="datetimerange"
                    v-model:start-date="queryParams.startDate"
                    v-model:end-date="queryParams.endDate"
                  />
                </div>
              </div>
              <div class="list-filter__item">
                <label class="list-filter__label">关键词</label>
                <div class="list-filter__control">
                  <input
                    v-model="queryParams.keyword"
                    class="filter-input w-full"
                    type="search"
                    placeholder="请输入上游 ID / 卖家邮箱 / 账号"
                    @keyup.enter="handleQuery"
                  />
                </div>
              </div>
              <div class="list-filter__item">
                <label class="list-filter__label">状态</label>
                <div class="list-filter__control">
                  <Select v-model="queryParams.status" class="w-full" size="sm" :options="STATUS_OPTIONS" />
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
            <div class="list-filter__row list-filter__row--sub">
              <div class="list-filter__item">
                <label class="list-filter__label">厂商</label>
                <div class="list-filter__control">
                  <Select
                    v-model="queryParams.platform"
                    class="w-full"
                    size="sm"
                    :options="PLATFORM_OPTIONS"
                  />
                </div>
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
          <template #cell-account="{ row }">
            <div class="account-cell">
              <span>{{ displayName(row) }}</span>
              <span v-if="row.is_reference" class="ref-tag">基线</span>
            </div>
            <div v-if="row.account_email" class="row-sub">#{{ row.id }}</div>
          </template>
          <template #cell-seller="{ row }">
            <div>{{ sellerLabel(row) }}</div>
            <div class="row-sub">#{{ row.seller_id }}</div>
          </template>
          <template #cell-platform="{ row }">
            {{ platformLabel(row.platform) }}
            <span v-if="row.auth_type" class="row-sub inline"> · {{ row.auth_type }}</span>
          </template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ statusLabel(row.status) }}
            </span>
            <div v-if="row.failure_reason" class="row-sub" :title="row.failure_reason">
              {{ row.failure_reason }}
            </div>
          </template>
          <template #cell-health="{ row }">
            {{ healthLabel(row.health) }}
            <div v-if="row.health_reason" class="row-sub" :title="row.health_reason">
              {{ row.health_reason }}
            </div>
          </template>
          <template #cell-grade="{ row }">{{ row.grade || '—' }}</template>
          <template #cell-privacy="{ row }">{{ privacyLabel(row) }}</template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" @click="openEdit(row)">{{ editLabel(row) }}</button>
              <button type="button" class="y-link" :disabled="busy" @click="probe(row)">检测</button>
              <button
                v-if="row.status !== 'suspended'"
                type="button"
                class="y-link"
                :disabled="busy"
                @click="suspend(row)"
              >
                冻结
              </button>
              <button v-else type="button" class="y-link" :disabled="busy" @click="unsuspend(row)">
                解冻
              </button>
              <button type="button" class="y-link y-link--danger" :disabled="busy" @click="remove(row)">
                删除
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              title="暂无上游"
              description="点右上角「添加上游」代用户接入，或调整筛选条件"
              action-text="添加上游"
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

    <div v-if="showCreate" class="create-mask" @click.self="showCreate = false">
      <div class="create-modal" role="dialog" aria-labelledby="create-upstream-title">
        <header class="create-modal__head">
          <h3 id="create-upstream-title">添加上游</h3>
          <button type="button" class="y-link" @click="showCreate = false">关闭</button>
        </header>
        <div class="create-modal__body">
          <p class="create-modal__hint">
            选择卖家与厂商后创建草稿，随后进入授权 / 选模型对接流程，完成后状态为就绪即可挂单。
          </p>
          <p v-if="createError" class="y-msg y-msg--error">{{ createError }}</p>

          <label class="create-field">
            <span>搜索卖家</span>
            <div class="create-field__row">
              <input
                v-model="userKeyword"
                class="filter-input flex-1"
                type="search"
                placeholder="邮箱 / 用户 ID"
                @keyup.enter="searchUsers"
              />
              <button type="button" class="btn btn-secondary" @click="searchUsers">查找</button>
            </div>
          </label>

          <label class="create-field">
            <span>卖家用户</span>
            <Select
              v-model="createForm.seller_id"
              class="w-full"
              size="sm"
              :options="[{ value: '', label: '— 请选择 —' }, ...createSellerOptions]"
            />
          </label>

          <label class="create-field">
            <span>厂商</span>
            <Select
              v-model="createForm.platform"
              class="w-full"
              size="sm"
              placeholder="— 请选择厂商 —"
              :options="createPlatformOptions"
            />
          </label>

          <label class="create-field">
            <span>接入方式</span>
            <Select
              v-model="createForm.auth_type"
              class="w-full"
              size="sm"
              placeholder="— 请选择接入方式 —"
              :disabled="!createForm.platform"
              :options="createAuthOptions"
            />
            <span v-if="createForm.platform && !createAuthOptions.length" class="create-field__err">
              该厂商暂无可用接入方式
            </span>
          </label>
        </div>
        <footer class="create-modal__foot">
          <button type="button" class="btn btn-secondary" @click="showCreate = false">取消</button>
          <button type="button" class="btn btn-primary" :disabled="createBusy" @click="submitCreate">
            创建并开始接入
          </button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
#market-upstreams-page.upstreams-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

#market-upstreams-page :deep(.table-page-layout) {
  height: 100%;
  min-height: 0;
  gap: 12px;
}

#market-upstreams-page :deep(.y-header) {
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

#market-upstreams-page :deep(.y-title) {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  line-height: 26px;
  color: #1f1f1f;
}

#market-upstreams-page :deep(.y-page-desc) {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 20px;
  color: #8c8c8c;
}

#market-upstreams-page :deep(.y-header-actions) {
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

/* 与 y-col-op 的 16px 间距对齐；DataTable 操作列没有 td.y-col-op */
.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 16px;
  white-space: nowrap;
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

/* —— 查询条件：对齐挂单/工单列表 —— */
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

.list-filter__row--sub {
  margin-top: 12px;
  grid-template-columns: minmax(180px, 0.85fr) 1fr;
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
  .list-filter__row,
  .list-filter__row--sub {
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

#market-upstreams-page :deep(.list-filter .el-date-editor.el-input__wrapper),
#market-upstreams-page :deep(.list-filter .el-date-editor.el-range-editor) {
  width: 100% !important;
  height: 32px !important;
  min-height: 32px !important;
}

#market-upstreams-page :deep(.list-filter .select-trigger),
#market-upstreams-page :deep(.list-filter .select-trigger-sm),
#market-upstreams-page :deep(.list-filter .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-upstreams-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-upstreams-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-upstreams-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
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

.row-sub {
  margin-top: 2px;
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
}

.row-sub.inline {
  display: inline;
  margin: 0;
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

.status-dot[data-tone='ok'] {
  background: var(--color-success, #52c41a);
}

.status-dot[data-tone='err'] {
  background: var(--color-error, #ff4d4f);
}

/* 进行中：管理端用蓝色，不用黄色 */
.status-dot[data-tone='progress'] {
  background: var(--color-primary, #0555ce);
}

.status-dot[data-tone='idle'] {
  background: #bfbfbf;
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
  background: var(--color-primary, #0555ce);
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

.create-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}

.create-modal {
  width: min(480px, 100%);
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}

.create-modal__head,
.create-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}

.create-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.create-modal__foot {
  justify-content: flex-end;
}

.create-modal__body {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
}

.create-modal__hint {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: #666;
}

.create-field {
  display: grid;
  gap: 6px;
  font-size: 13px;
}

.create-field > span {
  color: #606266;
  font-weight: 500;
}

.create-field__row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.create-field__err {
  color: #b45309;
  font-size: 12px;
}

.flex-1 {
  flex: 1;
  min-width: 0;
}
</style>
