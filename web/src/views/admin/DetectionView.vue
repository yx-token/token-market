<script setup lang="ts">
import { computed, onMounted, reactive, ref, toRaw } from 'vue'
import { admin } from '@/api'
import type { DetectionFlow, DetectionFlows, DetectionProvider, Policy, ThirdPartyCheck, Upstream, VendorConfig } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DateTimeField from '@/components/common/DateTimeField.vue'
import DataTable from '@/components/DataTable.vue'
import SearchSelect from '@/components/common/SearchSelect.vue'
import type { SelectOption } from '@/components/common/searchSelect'
import TabBar from '@/components/TabBar.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { authTypeText, label, options, upstreamName } from '@/utils/labels'
import { dateTime } from '@/utils/format'
import { detectionFlowText, identitySourceText } from '@/utils/inspection'

const toast = useToast()
const tab = ref<'policies' | 'upstream'>('policies')
const pageTitle = computed(() => (tab.value === 'policies' ? '检测策略' : '单个上游'))
const pageDescription = computed(() =>
  tab.value === 'policies'
    ? '首次接入始终执行上架检测；这里的开关控制之后的检测：是否检测、是否定时检测。'
    : '按上游覆盖策略，或手动排队一次检测。',
)
const showCreate = ref(false)
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  platform: '',
  enabled: '',
  startDate: '',
  endDate: '',
})

const policies = useLoader(() => admin.policies(), [] as Policy[] | null)
const providers = useLoader(() => admin.providers(), [] as DetectionProvider[] | null)
const triggerOptions = options('trigger')
const mismatchOptions = [
  { value: 'degraded', text: '暂停调度' },
  { value: 'suspected', text: '标记疑似' },
  { value: 'ignore', text: '仅记录' },
]
// 模型是开放集合：合并价目、全部策略（含第三方检测）与题库限定的模型。
// 厂商与上游仍按注册表选择，已存值不在表中时保留并标注。
const priceModels = ref<string[]>([])
const probeModels = ref<string[]>([])
const vendors = ref<VendorConfig[]>([])
const upstreams = ref<Upstream[]>([])
const modelOptions = computed<SelectOption[]>(() => {
  const policyModels = (policies.data.value ?? []).flatMap((p) => [
    ...(p.flows?.intelligence.models.map((m) => m.model) ?? p.models), ...(p.third_party ?? []).map((t) => t.model ?? ''),
  ])
  return [...new Set([...priceModels.value, ...policyModels, ...probeModels.value])]
    .filter(Boolean).map((m) => ({ value: m, text: m }))
})
const vendorOptions = computed<SelectOption[]>(() =>
  vendors.value.map((v) => ({ value: v.platform, text: `${label('platform', v.platform).text}（${v.platform}）` })),
)
const upstreamOptions = computed<SelectOption[]>(() =>
  upstreams.value.map((u) => ({
    value: String(u.id),
    text: `${upstreamName(u)} · ${label('platform', u.platform).text}`,
    keywords: `${u.id} ${u.platform} ${u.account_email ?? ''}`,
  })),
)
const createAuthTypes = computed(() => vendors.value.find((v) => v.platform === newAuth.platform)?.supported_auth_types ?? [])
type TPForm = { enabled: boolean; model: string; format: string; onMismatch: string; onSubmodel: string }
const thirdParty = reactive<Record<string, TPForm>>({})
const { busy, run } = useAction()
const editing = ref<Policy | null>(null)
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
    intelligence: { enabled: true, scheduled_enabled: true, interval_minutes: 360, triggers: [] as string[], full_triggers: [] as string[], models: [] as { model: string; expected_pass_rate: number | '' | null }[] },
    identity: { enabled: false, source: 'third_party' as 'third_party' | 'self', scheduled_enabled: false, interval_minutes: 1440, triggers: [] as string[], full_triggers: [] as string[] },
  },
})
const newAuth = reactive({ platform: '', authType: '' })
const override = reactive({ upstream: '', mode: 'on', scheduled: '', expires: '', reason: '' })
const selectedFlows = ref<DetectionFlow[]>(['intelligence', 'identity'])
const flowOptions = (['intelligence', 'identity'] as DetectionFlow[]).map((value) => ({ value, text: detectionFlowText(value) }))

// 旧策略只用于兼容回填；保存时统一写入新的流程配置。
function policyFlows(p: Policy): DetectionFlows {
  if (p.flows) return p.flows
  return {
    intelligence: { enabled: p.enabled, scheduled_enabled: p.scheduled_enabled, interval_minutes: p.interval_minutes, triggers: triggerOptions.map((t) => t.value), full_triggers: p.full_triggers ?? ['onboarding', 'admin', 'appeal', 'recovery'], models: p.models.map((model) => ({ model, expected_pass_rate: null })) },
    identity: { enabled: !!p.third_party?.length, source: 'third_party', scheduled_enabled: false, interval_minutes: 1440, triggers: [...new Set((p.third_party ?? []).flatMap((t) => t.triggers ?? []))], full_triggers: triggerOptions.map((t) => t.value) },
  }
}

function flowSummary(p: Policy, flow: DetectionFlow): string {
  const config = policyFlows(p)[flow]
  const source = flow === 'identity' ? ` · ${identitySourceText(policyFlows(p).identity.source)}` : ''
  return `${config.enabled ? '开' : '关'}${source} · ${config.scheduled_enabled ? `每 ${config.interval_minutes} 分钟` : `定时关（${config.interval_minutes} 分钟）`}`
}
onMounted(async () => {
  policies.load()
  providers.load()
  // 选项接口失败时降级为空列表，表单仍可保存（SearchSelect 支持手动录入）
  const [vendorList, st, upstreamList, itemList] = await Promise.all([
    admin.vendors().catch(() => null),
    admin.settings().catch(() => null),
    admin.upstreams().catch(() => null),
    admin.items().catch(() => null),
  ])
  vendors.value = vendorList ?? []
  priceModels.value = st?.price_models ?? []
  upstreams.value = upstreamList ?? []
  probeModels.value = (itemList ?? []).flatMap((item) => item.models)
})

const filtered = computed(() => {
  let rows = policies.data.value ?? []
  if (queryParams.platform) rows = rows.filter((p) => p.platform === queryParams.platform)
  if (queryParams.enabled === 'true') rows = rows.filter((p) => p.enabled)
  if (queryParams.enabled === 'false') rows = rows.filter((p) => !p.enabled)
  rows = filterByKeyword(rows, queryParams.keyword, ['platform', 'auth_type', 'default_grade'] as (keyof Policy)[])
  rows = filterByDateField(rows, 'updated_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  policies.load()
}
function onReset() {
  resetQuery()
  resetPage()
  policies.load()
}

function edit(p: Policy) {
  editing.value = p
  Object.assign(form, {
    enabled: p.enabled,
    scheduled: p.scheduled_enabled,
    jitter: p.jitter_minutes,
    budget: p.daily_budget_usd,
    h0: p.thresholds.h0,
    h1: p.thresholds.h1,
    alpha: p.thresholds.alpha,
    beta: p.thresholds.beta,
    autoPause: p.auto_pause,
    grade: p.default_grade,
    parallelism: p.item_parallelism ?? 2,
    tokenBudgets: JSON.stringify(p.token_budgets ?? {}, null, 2),
    flows: structuredClone(toRaw(policyFlows(p))),
  })
  for (const name of Object.keys(thirdParty)) delete thirdParty[name]
  const providerNames = new Set([...(providers.data.value ?? []).map((pr) => pr.name), ...(p.third_party ?? []).map((t) => t.provider)])
  for (const name of providerNames) {
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

function providerName(name: string) {
  return providers.data.value?.find((p) => p.name === name)?.label ?? name
}

async function save() {
  const p = editing.value
  if (!p) return
  let tokenBudgets: Record<string, unknown>
  try {
    tokenBudgets = JSON.parse(form.tokenBudgets)
    if (!tokenBudgets || Array.isArray(tokenBudgets) || typeof tokenBudgets !== 'object') throw new Error()
  } catch {
    toast.error('token 预算需要是 JSON 对象')
    return
  }
  const models = form.flows.intelligence.models.map((m) => ({
    model: m.model.trim(),
    expected_pass_rate: m.expected_pass_rate === '' || m.expected_pass_rate === null ? null : Number(m.expected_pass_rate),
  }))
  if (models.some((m) => !m.model || (m.expected_pass_rate !== null && (!Number.isFinite(m.expected_pass_rate) || m.expected_pass_rate <= 0 || m.expected_pass_rate > 1)))) {
    toast.error('请填写模型 ID，期望通过率需大于 0 且不超过 1，或留空使用默认值')
    return
  }
  if (new Set(models.map((m) => m.model)).size !== models.length) {
    toast.error('测智模型不能重复')
    return
  }
  for (const flow of [form.flows.intelligence, form.flows.identity]) {
    if (!Number.isInteger(Number(flow.interval_minutes)) || Number(flow.interval_minutes) < 10) {
      toast.error('检测周期需为至少 10 分钟的整数')
      return
    }
  }
  if (form.flows.identity.enabled && form.flows.identity.source === 'third_party' && !Object.entries(thirdParty).some(([name, t]) => t.enabled && providers.data.value?.some((pr) => pr.name === name && pr.enabled))) {
    toast.error('第三方身份核验需要选择至少一个已启用的检测站')
    return
  }
  const flows: DetectionFlows = {
    intelligence: { ...form.flows.intelligence, interval_minutes: Number(form.flows.intelligence.interval_minutes), models },
    identity: { ...form.flows.identity, interval_minutes: Number(form.flows.identity.interval_minutes) },
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
      .map(
        ([provider, t]): ThirdPartyCheck => ({
          provider,
          triggers: [...flows.identity.triggers],
          model: t.model.trim() || undefined,
          format: t.format || undefined,
          on_mismatch: t.onMismatch,
          on_submodel_mismatch: t.onSubmodel,
        }),
      ),
  }
  if (await run(() => admin.upsertPolicy(p.platform, p.auth_type, body), '策略已保存，下一轮生效')) {
    editing.value = null
    policies.load()
  }
}

async function quick(p: Policy, field: 'enabled' | 'scheduled_enabled') {
  const body: Record<string, boolean> = { [field]: !p[field] }
  if (field === 'enabled' && p.enabled) body.scheduled_enabled = false
  if (field === 'scheduled_enabled' && !p.enabled) {
    toast.error('需要先开启检测')
    return
  }
  if (await run(() => admin.upsertPolicy(p.platform, p.auth_type, body), '已更新')) policies.load()
}

async function createAuthPolicy() {
  if (!newAuth.platform || !newAuth.authType) {
    toast.error('请填写厂商与接入方式')
    return
  }
  if (await run(() => admin.upsertPolicy(newAuth.platform, newAuth.authType, {}), '已按厂商策略创建接入方式级策略')) {
    showCreate.value = false
    newAuth.platform = ''
    newAuth.authType = ''
    policies.load()
  }
}

async function setOverride() {
  const id = Number(override.upstream)
  if (!id) {
    toast.error('请选择上游')
    return
  }
  const body: Record<string, unknown> = { enabled: override.mode === 'on', reason: override.reason }
  if (override.scheduled) body.scheduled_enabled = override.scheduled === 'on'
  if (override.expires) body.expires_at = new Date(override.expires).toISOString()
  await run(() => admin.setOverride(id, body), '覆盖已设置')
}
async function clearOverride() {
  const id = Number(override.upstream)
  if (id) await run(() => admin.deleteOverride(id), '覆盖已删除')
}
async function probe() {
  const id = Number(override.upstream)
  if (!id) {
    toast.error('请选择上游')
    return
  }
  if (!selectedFlows.value.length) {
    toast.error('请选择至少一个检测流程')
    return
  }
  await run(() => admin.probeFlows(id, [...selectedFlows.value]), '已排队检测')
}
</script>

<template>
  <ConsoleListPage
    id="console-detection-page"
    :title="pageTitle"
    :description="pageDescription"
  >
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'policies', text: '策略列表' },
          { key: 'upstream', text: '单个上游' },
        ]"
      />
    </template>

    <template v-if="tab === 'policies'" #actions>
      <button type="button" class="btn btn-primary" @click="showCreate = true">新建策略</button>
    </template>

    <template v-if="tab === 'policies'" #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="厂商 / 接入方式" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">厂商</label>
        <select v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">是否检测</label>
        <select v-model="queryParams.enabled" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">开</option>
          <option value="false">关</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">更新日</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      v-if="tab === 'policies'"
      :columns="[
        { key: 'platform', label: '厂商' },
        { key: 'auth_type', label: '接入方式' },
        { key: 'enabled', label: '是否检测' },
        { key: 'scheduled_enabled', label: '是否定时检测' },
        { key: 'intelligence', label: '测智' },
        { key: 'identity', label: '身份核验' },
        { key: 'daily_budget_usd', label: '每日预算', align: 'right' },
        { key: 'third_party', label: '第三方检测' },
        { key: 'updated_at', label: '更新时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="policies.loading.value"
      row-key="id"
      empty="没有符合条件的策略"
    >
      <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
      <template #cell-auth_type="{ row }">{{ row.auth_type ? authTypeText(row.platform, row.auth_type) : '全部' }}</template>
      <template #cell-enabled="{ row }">
        <button :class="['switch', row.enabled ? 'chip-green' : 'chip-gray']" :disabled="busy" @click="quick(row, 'enabled')">
          {{ row.enabled ? '开' : '关' }}
        </button>
      </template>
      <template #cell-scheduled_enabled="{ row }">
        <button :class="['switch', row.scheduled_enabled ? 'chip-green' : 'chip-gray']" :disabled="busy" @click="quick(row, 'scheduled_enabled')">
          {{ row.scheduled_enabled ? '开' : '关' }}
        </button>
      </template>
      <template #cell-intelligence="{ row }">{{ flowSummary(row, 'intelligence') }}</template>
      <template #cell-identity="{ row }">{{ flowSummary(row, 'identity') }}</template>
      <template #cell-daily_budget_usd="{ row }">{{ row.daily_budget_usd }}</template>
      <template #cell-third_party="{ row }">
        <span v-if="!row.third_party?.length" class="text-slate-400">—</span>
        <span v-for="t in row.third_party ?? []" :key="t.provider" class="mr-2 text-xs">
          {{ providerName(t.provider) }}
        </span>
      </template>
      <template #cell-updated_at="{ row }">{{ dateTime(row.updated_at) }}</template>
      <template #cell-actions="{ row }"><button class="btn-ghost btn-sm" @click="edit(row)">编辑</button></template>
    </DataTable>

    <div v-else class="detection-upstream-panel">
      <p class="mb-5 text-sm leading-relaxed text-gray-500">
        对某个上游强制开启或关闭检测（例如重点观察可疑账号），可设到期时间；也可以立即检测一次（不受开关限制）。
      </p>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div>
          <label class="label" for="ou">上游</label>
          <SearchSelect id="ou" v-model="override.upstream" :options="upstreamOptions" placeholder="搜索上游 ID / 厂商 / 邮箱" />
        </div>
        <div>
          <label class="label" for="om">检测</label>
          <select id="om" v-model="override.mode" class="input">
            <option value="on">强制开启</option>
            <option value="off">强制关闭</option>
          </select>
        </div>
        <div>
          <label class="label" for="os">定时检测</label>
          <select id="os" v-model="override.scheduled" class="input">
            <option value="">跟随策略</option>
            <option value="on">开启</option>
            <option value="off">关闭</option>
          </select>
        </div>
        <div>
          <label class="label" for="oe">到期</label>
          <DateTimeField id="oe" v-model="override.expires" placeholder="可选到期时间" />
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="or">原因</label>
          <input id="or" v-model="override.reason" class="input" placeholder="可选备注" />
        </div>
      </div>
      <div class="mt-4">
        <label class="label" for="probe-flows">立即检测的流程</label>
        <SearchSelect id="probe-flows" v-model="selectedFlows" :options="flowOptions" multiple placeholder="选择检测流程" />
      </div>
      <div class="mt-6 flex flex-wrap justify-end gap-2 border-t border-gray-100 pt-4">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="clearOverride">删除覆盖</button>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="setOverride">设置覆盖</button>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="probe">立即检测</button>
      </div>
    </div>

    <template v-if="tab === 'policies'" #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog v-model:open="showCreate" title="新建接入方式级策略">
    <p class="text-sm text-gray-500">按厂商默认策略复制一份接入方式级策略，创建后可在列表中编辑。</p>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="label" for="np">厂商</label>
        <SearchSelect id="np" v-model="newAuth.platform" :options="vendorOptions" placeholder="选择厂商" @update:model-value="newAuth.authType = ''" />
      </div>
      <div>
        <label class="label" for="na">接入方式</label>
        <SearchSelect
          id="na"
          v-model="newAuth.authType"
          :options="createAuthTypes.map((t) => ({ value: t, text: authTypeText(newAuth.platform, t) }))"
          :disabled="!newAuth.platform"
          placeholder="选择接入方式"
        />
      </div>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="showCreate = false">取消</button>
      <button type="button" class="btn btn-primary" :disabled="busy" @click="createAuthPolicy">创建</button>
    </template>
  </ModalDialog>

  <ModalDialog :open="!!editing" :title="`编辑策略：${label('platform', editing?.platform).text} ${editing?.auth_type || ''}`" wide @update:open="(v: boolean) => !v && (editing = null)">
    <div class="flex flex-wrap gap-6 text-sm">
      <label class="flex items-center gap-2"><input v-model="form.enabled" type="checkbox" /> 是否检测</label>
      <label class="flex items-center gap-2"><input v-model="form.scheduled" type="checkbox" :disabled="!form.enabled" /> 是否定时检测</label>
      <label class="flex items-center gap-2"><input v-model="form.autoPause" type="checkbox" /> 确认门槛后自动停止调度</label>
    </div>
    <div class="grid grid-cols-2 gap-3 md:grid-cols-3">
      <div>
        <label class="label" for="pj">随机抖动（分钟）</label>
        <input id="pj" v-model.number="form.jitter" class="input" type="number" min="0" />
      </div>
      <div>
        <label class="label" for="pb">每日预算</label>
        <input id="pb" v-model.number="form.budget" class="input" type="number" min="0" step="0.1" />
      </div>
    </div>
    <section aria-labelledby="intelligence-title" class="space-y-3 border-t border-slate-100 pt-3">
      <h3 id="intelligence-title" class="text-sm font-medium text-slate-700">测智</h3>
      <div class="flex flex-wrap gap-6 text-sm">
        <label class="flex items-center gap-2"><input id="intelligence-enabled" v-model="form.flows.intelligence.enabled" type="checkbox" /> 开启测智</label>
        <label class="flex items-center gap-2"><input id="intelligence-scheduled" v-model="form.flows.intelligence.scheduled_enabled" type="checkbox" /> 定时检测</label>
      </div>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div><label class="label" for="intelligence-interval">周期（分钟）</label><input id="intelligence-interval" v-model.number="form.flows.intelligence.interval_minutes" class="input" type="number" min="10" /></div>
        <div><label class="label" for="intelligence-triggers">触发类型</label><SearchSelect id="intelligence-triggers" v-model="form.flows.intelligence.triggers" :options="triggerOptions" multiple placeholder="选择触发类型" /></div>
        <div class="sm:col-span-2"><label class="label" for="intelligence-full-triggers">完整检测触发类型</label><SearchSelect id="intelligence-full-triggers" v-model="form.flows.intelligence.full_triggers" :options="triggerOptions" multiple placeholder="选择触发类型" /></div>
      </div>
      <div>
        <p class="label">测智模型</p>
        <p class="hint mb-2">每次检测全部配置模型与挂售白名单的交集。期望通过率留空时使用检测项默认值。</p>
        <div v-for="(model, index) in form.flows.intelligence.models" :key="index" class="mb-2 grid grid-cols-1 items-end gap-2 sm:grid-cols-[1fr_10rem_auto]">
          <div><label class="label" :for="`intelligence-model-${index}`">模型 ID</label><SearchSelect :id="`intelligence-model-${index}`" v-model="model.model" :options="modelOptions" creatable placeholder="选择或输入模型 ID" /></div>
          <div><label class="label" :for="`intelligence-rate-${index}`">期望通过率</label><input :id="`intelligence-rate-${index}`" v-model="model.expected_pass_rate" class="input" type="number" min="0.0001" max="1" step="0.01" placeholder="使用默认值" /></div>
          <button type="button" class="btn btn-secondary" :aria-label="`删除第 ${index + 1} 个测智模型`" @click="form.flows.intelligence.models.splice(index, 1)">删除</button>
        </div>
        <button type="button" class="btn btn-secondary btn-sm" @click="form.flows.intelligence.models.push({ model: '', expected_pass_rate: null })">添加测智模型</button>
      </div>
    </section>
    <section aria-labelledby="identity-title" class="space-y-3 border-t border-slate-100 pt-3">
      <h3 id="identity-title" class="text-sm font-medium text-slate-700">身份与功能核验</h3>
      <div class="flex flex-wrap gap-6 text-sm">
        <label class="flex items-center gap-2"><input id="identity-enabled" v-model="form.flows.identity.enabled" type="checkbox" /> 开启身份与功能核验</label>
        <label class="flex items-center gap-2"><input id="identity-scheduled" v-model="form.flows.identity.scheduled_enabled" type="checkbox" /> 定时检测</label>
      </div>
      <fieldset class="flex flex-wrap gap-4 text-sm">
        <legend class="label">来源</legend>
        <label class="flex items-center gap-2"><input id="identity-source-third-party" v-model="form.flows.identity.source" type="radio" value="third_party" /> 第三方检测</label>
        <label class="flex items-center gap-2"><input id="identity-source-self" v-model="form.flows.identity.source" type="radio" value="self" /> 自有题库</label>
      </fieldset>
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div><label class="label" for="identity-interval">周期（分钟）</label><input id="identity-interval" v-model.number="form.flows.identity.interval_minutes" class="input" type="number" min="10" /></div>
        <div><label class="label" for="identity-triggers">触发类型</label><SearchSelect id="identity-triggers" v-model="form.flows.identity.triggers" :options="triggerOptions" multiple placeholder="选择触发类型" /></div>
        <div class="sm:col-span-2"><label class="label" for="identity-full-triggers">完整检测触发类型</label><SearchSelect id="identity-full-triggers" v-model="form.flows.identity.full_triggers" :options="triggerOptions" multiple placeholder="选择触发类型" /></div>
      </div>
      <div v-if="form.flows.identity.source === 'third_party'" data-testid="third-party-config" class="border-t border-slate-100 pt-3">
        <div class="mb-1 text-sm font-medium text-slate-700">第三方检测</div>
        <p class="hint mb-2">第三方身份检测独立执行，触发类型以本流程配置为准。身份不符可停止调度或标记疑似；上架时无法得出结论会判检测失败。协议可按平台自动选择。</p>
        <p v-if="!providers.data.value?.length" class="text-sm text-slate-400">暂无可接入的检测站</p>
        <div v-for="pr in providers.data.value ?? []" :key="pr.name" class="mb-3 rounded-xl border border-gray-100 bg-white/70 p-3 text-sm">
          <template v-if="thirdParty[pr.name]">
            <label class="flex items-center gap-2 font-medium">
              <input v-model="thirdParty[pr.name].enabled" type="checkbox" /> {{ pr.label }}
              <span v-if="!pr.enabled" class="text-xs font-normal text-amber-600">（检测站未启用，在"第三方检测"中开启后生效）</span>
            </label>
            <div v-if="thirdParty[pr.name].enabled" class="mt-2 space-y-2">
              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-4">
                <div>
                  <label class="label" :for="`tp-model-${pr.name}`">检测模型</label>
                  <SearchSelect :id="`tp-model-${pr.name}`" v-model="thirdParty[pr.name].model" :options="modelOptions" creatable placeholder="默认取可测模型" />
                </div>
                <div>
                  <label class="label" :for="`tp-format-${pr.name}`">协议</label>
                  <select :id="`tp-format-${pr.name}`" v-model="thirdParty[pr.name].format" class="input">
                    <option value="">按平台自动</option>
                    <option value="anthropic">Anthropic Messages</option>
                    <option value="openai">OpenAI Chat Completions</option>
                  </select>
                </div>
                <div>
                  <label class="label" :for="`tp-mis-${pr.name}`">身份不一致 / 伪装时</label>
                  <select :id="`tp-mis-${pr.name}`" v-model="thirdParty[pr.name].onMismatch" class="input">
                    <option v-for="a in mismatchOptions" :key="a.value" :value="a.value">{{ a.text }}</option>
                  </select>
                </div>
                <div>
                  <label class="label" :for="`tp-sub-${pr.name}`">仅具体型号不一致时</label>
                  <select :id="`tp-sub-${pr.name}`" v-model="thirdParty[pr.name].onSubmodel" class="input">
                    <option v-for="a in mismatchOptions" :key="a.value" :value="a.value">{{ a.text }}</option>
                  </select>
                </div>
              </div>
            </div>
          </template>
        </div>
      </div>
    </section>
    <div class="grid grid-cols-1 gap-3 border-t border-slate-100 pt-3 sm:grid-cols-2">
      <div><label class="label" for="policy-parallelism">题目并行上界</label><input id="policy-parallelism" v-model.number="form.parallelism" class="input" type="number" min="1" /><p class="hint">实际取策略、质检设置与账号当前下发并发的最小值；被测与基线各自限流。</p></div>
      <div class="sm:col-span-2"><label class="label" for="policy-token-budgets">每侧 token 预算覆盖（JSON）</label><textarea id="policy-token-budgets" v-model="form.tokenBudgets" class="input font-mono" /><p class="hint">{} 使用质检设置；可按 full/quick 配 input_tokens_per_side 与 output_tokens_per_side。</p></div>
    </div>
    <div class="grid grid-cols-2 gap-3 md:grid-cols-5">
      <div>
        <label class="label" for="h0">正常线 h0</label>
        <input id="h0" v-model.number="form.h0" class="input" type="number" step="0.01" />
      </div>
      <div>
        <label class="label" for="h1">降智线 h1</label>
        <input id="h1" v-model.number="form.h1" class="input" type="number" step="0.01" />
      </div>
      <div>
        <label class="label" for="pa">α（误判）</label>
        <input id="pa" v-model.number="form.alpha" class="input" type="number" step="0.01" />
      </div>
      <div>
        <label class="label" for="pbeta">β（漏判）</label>
        <input id="pbeta" v-model.number="form.beta" class="input" type="number" step="0.01" />
      </div>
      <div>
        <label class="label" for="pg">默认等级</label>
        <select id="pg" v-model="form.grade" class="input">
          <option v-for="g in ['S', 'A', 'B', 'C', 'D']" :key="g" :value="g">{{ g }}</option>
        </select>
      </div>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="editing = null">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>
</template>
