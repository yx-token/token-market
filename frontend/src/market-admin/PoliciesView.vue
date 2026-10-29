<script setup lang="ts">
/**
 * 检测策略：TablePageLayout + filter-grid + DataTable；策略列表 / 单个上游覆盖。
 */
import { computed, onMounted, reactive, ref, toRaw, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminPolicies' })

type FlowPolicy = {
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  triggers: string[]
  full_triggers: string[]
  models?: { model: string; expected_pass_rate: number | null }[]
  source?: 'third_party' | 'self'
}

type DetectionFlows = {
  intelligence: FlowPolicy
  degradation: FlowPolicy
  identity: FlowPolicy
}

interface Policy {
  id: number
  platform: string
  auth_type: string
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  jitter_minutes: number
  models: string[]
  daily_budget_usd: number
  thresholds: { h0: number; h1: number; alpha: number; beta: number }
  auto_pause: boolean
  default_grade: string
  third_party: { provider: string; triggers: string[]; model?: string; format?: string; on_mismatch: string; on_submodel_mismatch: string }[] | null
  item_parallelism?: number
  full_triggers?: string[]
  token_budgets?: Record<string, unknown>
  flows?: DetectionFlows
  updated_at?: string
}

interface Provider {
  name: string
  label: string
  enabled: boolean
}

interface Vendor {
  platform: string
  supported_auth_types: string[]
}

const TRIGGER_OPTS = [
  { value: 'onboarding', text: '上架' },
  { value: 'admin', text: '管理员' },
  { value: 'appeal', text: '申诉' },
  { value: 'recovery', text: '恢复' },
  { value: 'scheduled', text: '定时' },
]

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
  { value: 'true', label: '开' },
  { value: 'false', label: '关' },
]

const SCHEDULED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '开' },
  { value: 'false', label: '关' },
]

const AUTH_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'oauth', label: 'OAuth' },
  { value: 'api_key', label: 'API Key' },
  { value: 'cookie', label: 'Cookie' },
  { value: 'coding', label: 'Coding' },
  { value: '__all__', label: '全部接入（空）' },
]

const tab = ref<'policies' | 'upstream'>('policies')
const policies = ref<Policy[]>([])
const providers = ref<Provider[]>([])
const vendors = ref<Vendor[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  platform: '',
  enabled: '',
  scheduled: '',
  auth_type: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const platformFilterOptions = computed(() => [
  { value: '', label: '全部' },
  ...Object.entries(PLATFORM_LABEL).map(([value, label]) => ({ value, label })),
])

const editing = ref<Policy | null>(null)
const showCreate = ref(false)
const newAuth = reactive({ platform: '', authType: '' })
const thirdParty = reactive<Record<string, { enabled: boolean; model: string; format: string; onMismatch: string; onSubmodel: string }>>({})

const form = reactive({
  enabled: false,
  scheduled: false,
  jitter: 30,
  budget: 1,
  h0: 0.9,
  h1: 0.7,
  alpha: 0.05,
  beta: 0.1,
  autoPause: true,
  grade: 'B',
  parallelism: 2,
  tokenBudgets: '{}',
  flows: {
    intelligence: emptyFlow(true),
    degradation: emptyFlow(false),
    identity: { ...emptyFlow(false), source: 'third_party' as const },
  },
})

const override = reactive({
  upstream: '',
  mode: 'on',
  scheduled: '',
  expires: '',
  reason: '',
})
const selectedFlows = ref<string[]>(['intelligence', 'degradation', 'identity'])

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'platform', label: '厂商', class: 'col-platform' },
  { key: 'auth_type', label: '接入', class: 'col-auth' },
  { key: 'enabled', label: '检测', class: 'col-enabled' },
  { key: 'scheduled', label: '定时', class: 'col-sched' },
  { key: 'intelligence', label: '测智', class: 'col-flow' },
  { key: 'degradation', label: '降智', class: 'col-flow' },
  { key: 'identity', label: '身份', class: 'col-flow' },
  { key: 'third_party', label: '第三方', class: 'col-flow' },
  { key: 'budget', label: '预算', class: 'col-budget' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

function emptyFlow(enabled: boolean): FlowPolicy {
  return {
    enabled,
    scheduled_enabled: enabled,
    interval_minutes: 360,
    triggers: TRIGGER_OPTS.map((t) => t.value),
    full_triggers: ['onboarding', 'admin', 'appeal', 'recovery'],
    models: [],
  }
}

function policyFlows(p: Policy): DetectionFlows {
  if (p.flows) return structuredClone(toRaw(p.flows))
  const models = (p.models || []).map((model) => ({ model, expected_pass_rate: null as number | null }))
  // 总开关只管上架以外的检测；测智与降智流程开关不继承它，与后端 legacy 和 0030 迁移一致。
  return {
    intelligence: {
      enabled: true,
      scheduled_enabled: p.scheduled_enabled,
      interval_minutes: p.interval_minutes,
      triggers: TRIGGER_OPTS.map((t) => t.value),
      full_triggers: p.full_triggers ?? ['onboarding', 'admin', 'appeal', 'recovery'],
      models: structuredClone(models),
    },
    degradation: {
      enabled: true,
      scheduled_enabled: false,
      interval_minutes: p.interval_minutes,
      triggers: TRIGGER_OPTS.map((t) => t.value),
      full_triggers: p.full_triggers ?? ['onboarding', 'admin', 'appeal', 'recovery'],
      models: structuredClone(models),
    },
    identity: {
      enabled: !!(p.third_party?.length),
      source: 'third_party',
      scheduled_enabled: false,
      interval_minutes: 1440,
      triggers: [...new Set((p.third_party ?? []).flatMap((t) => t.triggers ?? []))],
      full_triggers: TRIGGER_OPTS.map((t) => t.value),
    },
  }
}

function flowSummary(p: Policy, flow: keyof DetectionFlows) {
  const config = policyFlows(p)[flow]
  if (!config) return '—'
  let schedule = '定时关'
  if (config.scheduled_enabled) {
    schedule = `每 ${config.interval_minutes} 分`
    if (!p.enabled) schedule = '总检测关'
    else if (!p.scheduled_enabled) schedule = '总定时关'
  }
  return `${config.enabled ? '开' : '关'} · ${schedule}`
}

const filtered = computed(() => {
  let rows = policies.value
  if (queryParams.value.platform) rows = rows.filter((p) => p.platform === queryParams.value.platform)
  if (queryParams.value.enabled === 'true') rows = rows.filter((p) => p.enabled)
  if (queryParams.value.enabled === 'false') rows = rows.filter((p) => !p.enabled)
  if (queryParams.value.scheduled === 'true') rows = rows.filter((p) => p.scheduled_enabled)
  if (queryParams.value.scheduled === 'false') rows = rows.filter((p) => !p.scheduled_enabled)
  if (queryParams.value.auth_type === '__all__') rows = rows.filter((p) => !p.auth_type)
  else if (queryParams.value.auth_type) rows = rows.filter((p) => p.auth_type === queryParams.value.auth_type)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((p) =>
      [p.platform, platLabel(p.platform), p.auth_type, p.default_grade]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
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
  const all = policies.value
  return {
    total: all.length,
    enabled: all.filter((p) => p.enabled).length,
    scheduled: all.filter((p) => p.scheduled_enabled).length,
  }
})

const createAuthTypes = computed(
  () => vendors.value.find((v) => v.platform === newAuth.platform)?.supported_auth_types ?? [],
)

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.platform,
    queryParams.value.enabled,
    queryParams.value.scheduled,
    queryParams.value.auth_type,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

watch(tab, () => {
  error.value = ''
  msg.value = ''
})

function platLabel(p: string) {
  return PLATFORM_LABEL[p] || p
}

function providerLabel(name: string) {
  return providers.value.find((provider) => provider.name === name)?.label ?? name
}

function providerDisabled(name: string) {
  const provider = providers.value.find((item) => item.name === name)
  return provider ? !provider.enabled : false
}

const disabledThirdParty = computed(() =>
  Object.entries(thirdParty)
    .filter(([name, config]) => config.enabled && providerDisabled(name))
    .map(([name]) => providerLabel(name)),
)

function policyPath(platform: string, authType: string) {
  const q = authType ? `?auth_type=${encodeURIComponent(authType)}` : ''
  return `${MARKET_API_PREFIX}/detection-policies/${platform}${q}`
}

function modelsText(flow: FlowPolicy) {
  return (flow.models || []).map((m) => m.model).join(', ')
}

function setModelsText(flow: FlowPolicy, text: string) {
  flow.models = text
    .split(/[,，\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((model) => ({ model, expected_pass_rate: null }))
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
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
    const [ps, pr, vs] = await Promise.all([
      marketGet<Policy[] | null>(`${MARKET_API_PREFIX}/detection-policies`),
      marketGet<Provider[] | null>(`${MARKET_API_PREFIX}/detection-providers`).catch(() => []),
      marketGet<Vendor[] | null>(`${MARKET_API_PREFIX}/vendors`).catch(() => []),
    ])
    policies.value = ps ?? []
    providers.value = pr ?? []
    vendors.value = vs ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    policies.value = []
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

function openEdit(p: Policy) {
  editing.value = p
  const flows = policyFlows(p)
  Object.assign(form, {
    enabled: p.enabled,
    scheduled: p.scheduled_enabled,
    jitter: p.jitter_minutes,
    budget: p.daily_budget_usd,
    h0: p.thresholds?.h0 ?? 0.9,
    h1: p.thresholds?.h1 ?? 0.7,
    alpha: p.thresholds?.alpha ?? 0.05,
    beta: p.thresholds?.beta ?? 0.1,
    autoPause: p.auto_pause,
    grade: p.default_grade || 'B',
    parallelism: p.item_parallelism ?? 2,
    tokenBudgets: JSON.stringify(p.token_budgets ?? {}, null, 2),
    flows,
  })
  for (const name of Object.keys(thirdParty)) delete thirdParty[name]
  const names = new Set([
    ...providers.value.map((pr) => pr.name),
    ...(p.third_party ?? []).map((t) => t.provider),
  ])
  for (const name of names) {
    const c = (p.third_party ?? []).find((x) => x.provider === name)
    thirdParty[name] = {
      enabled: !!c,
      model: c?.model ?? '',
      format: c?.format ?? '',
      onMismatch: c?.on_mismatch ?? 'degraded',
      onSubmodel: c?.on_submodel_mismatch ?? 'suspected',
    }
  }
}

function closeModal() {
  editing.value = null
}

async function save() {
  const p = editing.value
  if (!p) return
  let tokenBudgets: Record<string, unknown>
  try {
    tokenBudgets = JSON.parse(form.tokenBudgets)
    if (!tokenBudgets || Array.isArray(tokenBudgets) || typeof tokenBudgets !== 'object') throw new Error()
  } catch {
    error.value = 'token 预算需要是 JSON 对象'
    return
  }
  for (const flow of [form.flows.intelligence, form.flows.degradation, form.flows.identity]) {
    if (flow.scheduled_enabled && !flow.enabled) {
      error.value = '流程定时检测需要先启用该流程'
      return
    }
    if (!Number.isInteger(Number(flow.interval_minutes)) || Number(flow.interval_minutes) < 10) {
      error.value = '检测周期需为至少 10 分钟的整数'
      return
    }
  }
  if (
    form.flows.identity.enabled &&
    form.flows.identity.source === 'third_party' &&
    !Object.entries(thirdParty).some(([name, t]) => t.enabled && providers.value.some((pr) => pr.name === name && pr.enabled))
  ) {
    error.value = '第三方身份核验需要选择至少一个已启用的检测站'
    return
  }
  const flows = {
    intelligence: {
      ...form.flows.intelligence,
      interval_minutes: Number(form.flows.intelligence.interval_minutes),
      models: (form.flows.intelligence.models || []).map((m) => ({
        model: m.model.trim(),
        expected_pass_rate: m.expected_pass_rate,
      })),
    },
    degradation: {
      ...form.flows.degradation,
      interval_minutes: Number(form.flows.degradation.interval_minutes),
      models: (form.flows.degradation.models || []).map((m) => ({
        model: m.model.trim(),
        expected_pass_rate: m.expected_pass_rate,
      })),
    },
    identity: {
      ...form.flows.identity,
      interval_minutes: Number(form.flows.identity.interval_minutes),
    },
  }
  const body = {
    flows,
    enabled: form.enabled,
    scheduled_enabled: form.enabled && form.scheduled,
    jitter_minutes: Number(form.jitter),
    daily_budget_usd: Number(form.budget),
    thresholds: { h0: Number(form.h0), h1: Number(form.h1), alpha: Number(form.alpha), beta: Number(form.beta) },
    auto_pause: form.autoPause,
    default_grade: form.grade,
    item_parallelism: Number(form.parallelism),
    token_budgets: tokenBudgets,
    third_party: Object.entries(thirdParty)
      .filter(([, t]) => t.enabled)
      .map(([provider, t]) => ({
        provider,
        // 已废弃：仅为旧配置与回退版本保留；执行触发类型以 flows.identity.triggers 为准。
        triggers: [...(flows.identity.triggers || [])],
        model: t.model.trim() || undefined,
        format: t.format || undefined,
        on_mismatch: t.onMismatch,
        on_submodel_mismatch: t.onSubmodel,
      })),
  }
  if (await run('策略已保存，下一轮生效', () => marketSend('PUT', policyPath(p.platform, p.auth_type), body))) {
    closeModal()
  }
}

async function quick(p: Policy, field: 'enabled' | 'scheduled_enabled') {
  const body: Record<string, boolean> = { [field]: !p[field] }
  if (field === 'enabled' && p.enabled) body.scheduled_enabled = false
  if (field === 'scheduled_enabled' && !p.enabled) {
    error.value = '需要先开启检测'
    return
  }
  await run('已更新', () => marketSend('PUT', policyPath(p.platform, p.auth_type), body))
}

async function createAuthPolicy() {
  if (!newAuth.platform || !newAuth.authType) {
    error.value = '请填写厂商与接入方式'
    return
  }
  if (await run('已创建接入方式级策略', () => marketSend('PUT', policyPath(newAuth.platform, newAuth.authType), {}))) {
    showCreate.value = false
    newAuth.platform = ''
    newAuth.authType = ''
  }
}

async function setOverride() {
  const id = Number(override.upstream)
  if (!id) {
    error.value = '请填写上游 ID'
    return
  }
  const body: Record<string, unknown> = { enabled: override.mode === 'on', reason: override.reason }
  if (override.scheduled) body.scheduled_enabled = override.scheduled === 'on'
  if (override.expires) body.expires_at = new Date(override.expires).toISOString()
  await run('覆盖已设置', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/upstreams/${id}/detection-override`, body),
  )
}

async function clearOverride() {
  const id = Number(override.upstream)
  if (!id) {
    error.value = '请填写上游 ID'
    return
  }
  await run('覆盖已删除', () =>
    marketSend('DELETE', `${MARKET_API_PREFIX}/upstreams/${id}/detection-override`),
  )
}

async function probe() {
  const id = Number(override.upstream)
  if (!id) {
    error.value = '请填写上游 ID'
    return
  }
  if (!selectedFlows.value.length) {
    error.value = '请选择至少一个检测流程'
    return
  }
  await run('已排队检测', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${id}/probe`, { flows: [...selectedFlows.value] }),
  )
}

onMounted(load)
</script>

<template>
  <section id="market-policies-page" class="policies-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'policies' ? '检测策略' : '单个上游' }}</h2>
          <p class="y-page-desc">
            {{
              tab === 'policies'
                ? '控制测智 / 降智 / 身份核验流程与定时检测'
                : '按上游覆盖策略，或手动排队检测'
            }}
          </p>
        </div>
      </template>

      <template #actions>
        <button
          v-if="tab === 'policies'"
          type="button"
          class="btn btn-primary page-action-btn"
          @click="showCreate = true"
        >
          <Icon name="plus" size="sm" />
          新建策略
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>

        <nav class="y-tabs mb-3">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'policies' }" @click="tab = 'policies'">
            策略列表
          </button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'upstream' }" @click="tab = 'upstream'">
            单个上游
          </button>
        </nav>

        <template v-if="tab === 'policies'">
          <div class="section-block">
            <div class="section-title">基本信息</div>
            <div class="stats-row">
              <article class="stat-card stat-card--blue">
                <div>
                  <div class="stat-card__label">策略总数</div>
                  <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
                </div>
              </article>
              <article class="stat-card stat-card--green">
                <div>
                  <div class="stat-card__label">检测开启</div>
                  <div class="stat-card__value">{{ allStats.enabled.toLocaleString() }}</div>
                </div>
              </article>
              <article class="stat-card stat-card--orange">
                <div>
                  <div class="stat-card__label">定时开启</div>
                  <div class="stat-card__value">{{ allStats.scheduled.toLocaleString() }}</div>
                </div>
              </article>
            </div>
          </div>

          <div class="section-block mt-3">
            <div class="section-title">策略信息</div>
            <div class="filter-grid">
              <div class="filter-item">
                <label class="filter-label">关键词</label>
                <div class="relative flex-1 w-full min-w-0">
                  <input
                    v-model="queryParams.keyword"
                    class="filter-input w-full"
                    type="search"
                    placeholder="厂商 / 接入方式 / 等级"
                    @keyup.enter="handleQuery"
                  />
                </div>
              </div>
              <div class="filter-item">
                <label class="filter-label">厂商</label>
                <Select
                  v-model="queryParams.platform"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="platformFilterOptions"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label">接入</label>
                <Select
                  v-model="queryParams.auth_type"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="AUTH_OPTIONS"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label">检测</label>
                <Select
                  v-model="queryParams.enabled"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="ENABLED_OPTIONS"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label">定时</label>
                <Select
                  v-model="queryParams.scheduled"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="SCHEDULED_OPTIONS"
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
      </template>

      <template #table>
        <template v-if="tab === 'policies'">
          <DataTable
            :columns="columns"
            :data="pageRows"
            :loading="loading"
            :expandable-actions="false"
          >
            <template #cell-serial="{ row }">
              {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
            </template>
            <template #cell-platform="{ row }">
              <div>{{ platLabel(row.platform) }}</div>
              <div class="row-sub">{{ row.platform }}</div>
            </template>
            <template #cell-auth_type="{ row }">{{ row.auth_type || '全部' }}</template>
            <template #cell-enabled="{ row }">
              <span class="status-cell">
                <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
                {{ row.enabled ? '开' : '关' }}
              </span>
            </template>
            <template #cell-scheduled="{ row }">
              <span class="status-cell">
                <i class="status-dot" :data-tone="row.scheduled_enabled ? 'ok' : 'idle'" />
                {{ row.scheduled_enabled ? '开' : '关' }}
              </span>
            </template>
            <template #cell-intelligence="{ row }">{{ flowSummary(row, 'intelligence') }}</template>
            <template #cell-degradation="{ row }">{{ flowSummary(row, 'degradation') }}</template>
            <template #cell-identity="{ row }">{{ flowSummary(row, 'identity') }}</template>
            <template #cell-third_party="{ row }">
              <span v-if="!row.third_party?.length" class="hint">—</span>
              <span v-for="site in row.third_party ?? []" :key="site.provider" class="mr-2">
                {{ providerLabel(site.provider) }}<span v-if="providerDisabled(site.provider)" class="hint">（已停用）</span>
              </span>
            </template>
            <template #cell-budget="{ row }">{{ row.daily_budget_usd }}</template>
            <template #cell-actions="{ row }">
              <div class="table-ops">
                <button type="button" class="y-link" :disabled="busy" @click="quick(row, 'enabled')">
                  {{ row.enabled ? '关检测' : '开检测' }}
                </button>
                <button type="button" class="y-link" :disabled="busy" @click="quick(row, 'scheduled_enabled')">
                  {{ row.scheduled_enabled ? '关定时' : '开定时' }}
                </button>
                <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
              </div>
            </template>
            <template #empty>
              <EmptyState
                title="暂无策略"
                description="调整筛选条件后重试，或新建接入方式级策略"
                action-text="新建策略"
                @action="showCreate = true"
              />
            </template>
          </DataTable>
        </template>

        <template v-else>
          <div class="pay-form">
            <label class="y-field">
              <span>上游 ID</span>
              <input v-model="override.upstream" class="input w-full" inputmode="numeric" placeholder="123" />
            </label>
            <label class="y-field">
              <span>覆盖检测</span>
              <select v-model="override.mode" class="input w-full">
                <option value="on">强制开启</option>
                <option value="off">强制关闭</option>
              </select>
            </label>
            <label class="y-field">
              <span>覆盖定时</span>
              <select v-model="override.scheduled" class="input w-full">
                <option value="">不覆盖</option>
                <option value="on">开</option>
                <option value="off">关</option>
              </select>
            </label>
            <label class="y-field">
              <span>过期时间（可选）</span>
              <input v-model="override.expires" class="input w-full" type="datetime-local" />
            </label>
            <label class="y-field y-field--wide">
              <span>原因</span>
              <input v-model="override.reason" class="input w-full" />
            </label>
            <div class="y-field y-field--wide">
              <span>手动检测流程</span>
              <div class="auth-list">
                <label v-for="f in ['intelligence', 'degradation', 'identity']" :key="f" class="y-check">
                  <input v-model="selectedFlows" type="checkbox" :value="f" />
                  {{ f === 'intelligence' ? '测智' : f === 'degradation' ? '降智' : '身份' }}
                </label>
              </div>
            </div>
            <div class="pay-actions">
              <button type="button" class="btn btn-secondary" :disabled="busy" @click="clearOverride">清除覆盖</button>
              <button type="button" class="btn btn-secondary" :disabled="busy" @click="setOverride">设置覆盖</button>
              <button type="button" class="btn btn-primary" :disabled="busy" @click="probe">排队检测</button>
            </div>
          </div>
        </template>
      </template>

      <template #pagination>
        <Pagination
          v-if="tab === 'policies' && total > 0"
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

    <div v-if="showCreate" class="y-modal-mask" @click.self="showCreate = false">
      <div class="y-modal" role="dialog" aria-label="新建策略">
        <header class="y-modal__head">
          <h3>新建接入方式级策略</h3>
          <button type="button" class="y-link" @click="showCreate = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>厂商</span>
            <select v-model="newAuth.platform" class="input w-full" @change="newAuth.authType = ''">
              <option value="">选择</option>
              <option v-for="v in vendors" :key="v.platform" :value="v.platform">{{ platLabel(v.platform) }}</option>
            </select>
          </label>
          <label class="y-field">
            <span>接入方式</span>
            <select v-model="newAuth.authType" class="input w-full">
              <option value="">选择</option>
              <option v-for="t in createAuthTypes" :key="t" :value="t">{{ t }}</option>
            </select>
          </label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="showCreate = false">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="createAuthPolicy">创建</button>
        </footer>
      </div>
    </div>

    <div v-if="editing" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal y-modal--wide" role="dialog" :aria-label="`编辑 ${editing.platform}`">
        <header class="y-modal__head">
          <h3>编辑 {{ platLabel(editing.platform) }} · {{ editing.auth_type || '全部' }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 开启检测</label>
          <label class="y-check"><input v-model="form.scheduled" type="checkbox" :disabled="!form.enabled" /> 开启定时检测</label>
          <p class="hint">定时执行需同时开启总检测、总定时检测，以及对应流程和流程定时。</p>
          <div class="y-field-row y-field-row--4">
            <label class="y-field"><span>抖动(分)</span><input v-model.number="form.jitter" type="number" class="input w-full" /></label>
            <label class="y-field"><span>每日预算$</span><input v-model.number="form.budget" type="number" class="input w-full" step="0.1" /></label>
            <label class="y-field"><span>默认等级</span><input v-model="form.grade" class="input w-full" /></label>
            <label class="y-field"><span>并发</span><input v-model.number="form.parallelism" type="number" class="input w-full" min="1" /></label>
          </div>
          <div class="y-field-row y-field-row--4">
            <label class="y-field"><span>h0</span><input v-model.number="form.h0" type="number" step="0.01" class="input w-full" /></label>
            <label class="y-field"><span>h1</span><input v-model.number="form.h1" type="number" step="0.01" class="input w-full" /></label>
            <label class="y-field"><span>alpha</span><input v-model.number="form.alpha" type="number" step="0.01" class="input w-full" /></label>
            <label class="y-field"><span>beta</span><input v-model.number="form.beta" type="number" step="0.01" class="input w-full" /></label>
          </div>
          <label class="y-check"><input v-model="form.autoPause" type="checkbox" /> 不达标自动暂停</label>

          <div v-for="key in (['intelligence', 'degradation', 'identity'] as const)" :key="key" class="flow-block">
            <h4>{{ key === 'intelligence' ? '测智' : key === 'degradation' ? '降智检测' : '身份核验' }}</h4>
            <label class="y-check"><input v-model="form.flows[key].enabled" type="checkbox" /> 启用</label>
            <label class="y-check">
              <input v-model="form.flows[key].scheduled_enabled" type="checkbox" :disabled="!form.enabled || !form.scheduled" /> 定时
            </label>
            <label class="y-field">
              <span>周期（分钟）</span>
              <input v-model.number="form.flows[key].interval_minutes" type="number" min="10" class="input w-full" />
            </label>
            <label v-if="key !== 'identity'" class="y-field">
              <span>模型（逗号分隔）</span>
              <input
                class="input w-full"
                :value="modelsText(form.flows[key])"
                @input="setModelsText(form.flows[key], ($event.target as HTMLInputElement).value)"
              />
            </label>
            <label v-else class="y-field">
              <span>来源</span>
              <select v-model="form.flows.identity.source" class="input w-full">
                <option value="third_party">第三方</option>
                <option value="self">自研</option>
              </select>
            </label>
          </div>

          <div v-if="form.flows.identity.enabled && form.flows.identity.source === 'third_party'" data-testid="third-party-config" class="flow-block">
            <h4>第三方检测站</h4>
            <p v-if="disabledThirdParty.length" data-testid="disabled-provider-warning" class="hint">
              检测站 {{ disabledThirdParty.join('、') }} 已停用，执行时会跳过；请先启用检测站，或在本策略中取消选择。
            </p>
            <div v-for="pr in providers" :key="pr.name" class="tp-row">
              <template v-if="thirdParty[pr.name]">
                <label class="y-check">
                  <input v-model="thirdParty[pr.name].enabled" type="checkbox" />
                  {{ pr.label }}
                  <span v-if="!pr.enabled" class="hint">（站未启用）</span>
                </label>
                <template v-if="thirdParty[pr.name].enabled">
                  <input v-model="thirdParty[pr.name].model" class="input" placeholder="模型" />
                  <select v-model="thirdParty[pr.name].onMismatch" class="input">
                    <option value="degraded">暂停调度</option>
                    <option value="suspected">标记疑似</option>
                    <option value="ignore">仅记录</option>
                  </select>
                </template>
              </template>
            </div>
          </div>

          <label class="y-field">
            <span>token 预算（JSON）</span>
            <textarea v-model="form.tokenBudgets" class="input w-full y-textarea mono" rows="3" />
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
#market-policies-page.policies-shell {
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

.pay-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 12px;
  padding: 8px 0 16px;
}
.pay-actions {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field > span:first-child {
  color: var(--color-text-secondary, #868686);
}
.y-field--wide {
  grid-column: 1 / -1;
}
.y-field-row {
  display: grid;
  gap: 12px;
  grid-template-columns: 1fr 1fr;
}
.y-field-row--4 {
  grid-template-columns: repeat(4, 1fr);
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.hint {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.flow-block {
  display: grid;
  gap: 8px;
  padding: 10px 0;
  border-top: 1px solid rgb(0 0 0 / 6%);
}
.flow-block h4 {
  margin: 0;
  font-size: 14px;
}
.tp-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 6px;
}

#market-policies-page :deep(.filter-grid .select-trigger),
#market-policies-page :deep(.filter-grid .select-trigger-sm),
#market-policies-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-policies-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-policies-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-policies-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-policies-page :deep(th.col-platform),
#market-policies-page :deep(td.col-platform) {
  min-width: 8rem;
}
#market-policies-page :deep(th.col-auth),
#market-policies-page :deep(td.col-auth),
#market-policies-page :deep(th.col-enabled),
#market-policies-page :deep(td.col-enabled),
#market-policies-page :deep(th.col-sched),
#market-policies-page :deep(td.col-sched),
#market-policies-page :deep(th.col-budget),
#market-policies-page :deep(td.col-budget) {
  min-width: 5rem;
}
#market-policies-page :deep(th.col-flow),
#market-policies-page :deep(td.col-flow) {
  min-width: 7rem;
}
#market-policies-page :deep(th.col-ops),
#market-policies-page :deep(td.col-ops) {
  min-width: 14rem;
  width: 14rem;
  box-sizing: border-box;
}
#market-policies-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-policies-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-policies-page :deep(tbody tr:hover td.col-ops) {
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
  max-height: min(92vh, 860px);
  overflow: auto;
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal--wide {
  width: min(720px, 100%);
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
.y-textarea {
  height: auto !important;
  min-height: 72px;
  padding: 8px 10px;
  resize: vertical;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
  .y-field-row--4 {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
