<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { DetectionProvider, Policy, ThirdPartyCheck, Upstream, VendorConfig } from '@/api/types'
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
// 套件与后端 validSuites 保持一致；未知套件在保存时被 INVALID_SUITE 拒绝
const suiteOptions = [
  'availability',
  'latency',
  'iq',
  'reasoning',
  'long_context',
  'inspection_authenticity',
  'inspection_capability',
  'inspection_context',
  'inspection_function',
  'inspection_output',
  'inspection_performance',
].map((value) => ({ value, text: value }))
// 模型是开放集合：合并价目、全部策略（含第三方检测）与题库限定的模型。
// 厂商与上游仍按注册表选择，已存值不在表中时保留并标注。
const priceModels = ref<string[]>([])
const probeModels = ref<string[]>([])
const vendors = ref<VendorConfig[]>([])
const upstreams = ref<Upstream[]>([])
const modelOptions = computed<SelectOption[]>(() => {
  const policyModels = (policies.data.value ?? []).flatMap((p) => [
    ...p.models, ...(p.third_party ?? []).map((t) => t.model ?? ''),
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
type TPForm = { enabled: boolean; triggers: string[]; model: string; format: string; onMismatch: string; onSubmodel: string }
const thirdParty = reactive<Record<string, TPForm>>({})
const { busy, run } = useAction()
const editing = ref<Policy | null>(null)
const form = reactive({
  enabled: false,
  scheduled: false,
  interval: 360,
  jitter: 30,
  suites: [] as string[],
  models: [] as string[],
  budget: 1,
  h0: 0.9,
  h1: 0.7,
  alpha: 0.05,
  beta: 0.1,
  autoPause: true,
  grade: 'B',
  parallelism: 2,
  fullTriggers: [] as string[],
  tokenBudgets: '{}',
})
const newAuth = reactive({ platform: '', authType: '' })
const override = reactive({ upstream: '', mode: 'on', scheduled: '', expires: '', reason: '' })
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
    interval: p.interval_minutes,
    jitter: p.jitter_minutes,
    suites: [...p.suites],
    models: [...p.models],
    budget: p.daily_budget_usd,
    h0: p.thresholds.h0,
    h1: p.thresholds.h1,
    alpha: p.thresholds.alpha,
    beta: p.thresholds.beta,
    autoPause: p.auto_pause,
    grade: p.default_grade,
    parallelism: p.item_parallelism ?? 2,
    fullTriggers: [...(p.full_triggers ?? ['onboarding', 'admin', 'appeal', 'recovery'])],
    tokenBudgets: JSON.stringify(p.token_budgets ?? {}, null, 2),
  })
  for (const pr of providers.data.value ?? []) {
    const c = (p.third_party ?? []).find((x) => x.provider === pr.name)
    thirdParty[pr.name] = {
      enabled: !!c,
      triggers: c?.triggers ?? ['onboarding', 'admin', 'appeal'],
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
  const body = {
    enabled: form.enabled,
    scheduled_enabled: form.enabled && form.scheduled,
    interval_minutes: Number(form.interval),
    jitter_minutes: Number(form.jitter),
    suites: [...form.suites],
    models: [...form.models],
    daily_budget_usd: Number(form.budget),
    thresholds: { h0: Number(form.h0), h1: Number(form.h1), alpha: Number(form.alpha), beta: Number(form.beta) },
    auto_pause: form.autoPause,
    default_grade: form.grade,
    item_parallelism: Number(form.parallelism),
    full_triggers: [...form.fullTriggers],
    token_budgets: tokenBudgets,
    third_party: Object.entries(thirdParty)
      .filter(([, t]) => t.enabled)
      .map(
        ([provider, t]): ThirdPartyCheck => ({
          provider,
          triggers: t.triggers,
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
  if (id) await run(() => admin.probe(id), '已排队检测')
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
        { key: 'interval', label: '周期' },
        { key: 'models', label: '模型' },
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
      <template #cell-interval="{ row }">{{ row.interval_minutes }} 分钟 ± {{ row.jitter_minutes }}</template>
      <template #cell-models="{ row }">{{ row.models.join(', ') || '—' }}</template>
      <template #cell-daily_budget_usd="{ row }">{{ row.daily_budget_usd }}</template>
      <template #cell-third_party="{ row }">
        <span v-if="!row.third_party?.length" class="text-slate-400">—</span>
        <span v-for="t in row.third_party ?? []" :key="t.provider" class="mr-2 text-xs" :title="t.triggers.map((x: string) => label('trigger', x).text).join('、')">
          {{ providerName(t.provider) }}（{{ t.triggers.length }} 类检测）
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
    <div class="flex gap-6 text-sm">
      <label class="flex items-center gap-2"><input v-model="form.enabled" type="checkbox" /> 是否检测</label>
      <label class="flex items-center gap-2"><input v-model="form.scheduled" type="checkbox" :disabled="!form.enabled" /> 是否定时检测</label>
      <label class="flex items-center gap-2"><input v-model="form.autoPause" type="checkbox" /> 确认降智后自动停止调度</label>
    </div>
    <div class="grid grid-cols-2 gap-3 md:grid-cols-3">
      <div>
        <label class="label" for="pi">周期（分钟）</label>
        <input id="pi" v-model.number="form.interval" class="input" type="number" min="10" />
      </div>
      <div>
        <label class="label" for="pj">随机抖动（分钟）</label>
        <input id="pj" v-model.number="form.jitter" class="input" type="number" min="0" />
      </div>
      <div>
        <label class="label" for="pb">每日预算</label>
        <input id="pb" v-model.number="form.budget" class="input" type="number" min="0" step="0.1" />
      </div>
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="label" for="pm">模型（多个时轮换）</label>
        <SearchSelect id="pm" v-model="form.models" :options="modelOptions" multiple creatable placeholder="选择模型" />
      </div>
      <div>
        <label class="label" for="ps">套件</label>
        <SearchSelect id="ps" v-model="form.suites" :options="suiteOptions" multiple placeholder="选择套件" />
        <p class="hint">试运行新题仅记录；正式模式只使用质检题。未注册的套件保存时会被拒绝。</p>
      </div>
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div><label class="label" for="policy-parallelism">题目并行上界</label><input id="policy-parallelism" v-model.number="form.parallelism" class="input" type="number" min="1" /><p class="hint">实际取策略、质检设置与账号当前下发并发的最小值；被测与基线各自限流。</p></div>
      <div><label class="label" for="policy-full-triggers">执行完整质检的触发类型</label><SearchSelect id="policy-full-triggers" v-model="form.fullTriggers" :options="triggerOptions" multiple placeholder="选择触发类型" /><p class="hint">未知触发类型保存时会被拒绝。</p></div>
      <div class="col-span-2"><label class="label" for="policy-token-budgets">每侧 token 预算覆盖（JSON）</label><textarea id="policy-token-budgets" v-model="form.tokenBudgets" class="input font-mono" /><p class="hint">{} 使用质检设置；可按 full/quick 配 input_tokens_per_side 与 output_tokens_per_side。</p></div>
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
    <div class="border-t border-slate-100 pt-3">
      <div class="mb-1 text-sm font-medium text-slate-700">第三方检测</div>
      <p class="hint mb-2">平台探测完成后，在选中的检测类型中追加第三方身份检测；最终结论取平台探测与第三方检测中更严重的一项，检测站失败或证据不足时不影响结论。"判为降智"会停止调度（开启自动停止时），"判为疑似"会降低流量。协议按平台自动选择时 Claude 用 Anthropic Messages，其余用 OpenAI Chat Completions。</p>
      <p v-if="!providers.data.value?.length" class="text-sm text-slate-400">暂无可接入的检测站</p>
      <div v-for="pr in providers.data.value ?? []" :key="pr.name" class="mb-3 rounded-xl border border-gray-100 bg-white/70 p-3 text-sm">
        <template v-if="thirdParty[pr.name]">
          <label class="flex items-center gap-2 font-medium">
            <input v-model="thirdParty[pr.name].enabled" type="checkbox" /> {{ pr.label }}
            <span v-if="!pr.enabled" class="text-xs font-normal text-amber-600">（检测站未启用，在"第三方检测"中开启后生效）</span>
          </label>
          <div v-if="thirdParty[pr.name].enabled" class="mt-2 space-y-2">
            <div class="flex flex-wrap gap-3">
              <span class="text-slate-500">执行于</span>
              <label v-for="t in triggerOptions" :key="t.value" class="flex items-center gap-1">
                <input v-model="thirdParty[pr.name].triggers" type="checkbox" :value="t.value" /> {{ t.text }}
              </label>
            </div>
            <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
              <div>
                <label class="label" :for="`tp-model-${pr.name}`">检测模型</label>
                <SearchSelect :id="`tp-model-${pr.name}`" v-model="thirdParty[pr.name].model" :options="modelOptions" creatable placeholder="默认同平台探测" />
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
                  <option v-for="a in options('tpAction')" :key="a.value" :value="a.value">{{ a.text }}</option>
                </select>
              </div>
              <div>
                <label class="label" :for="`tp-sub-${pr.name}`">仅具体型号不一致时</label>
                <select :id="`tp-sub-${pr.name}`" v-model="thirdParty[pr.name].onSubmodel" class="input">
                  <option v-for="a in options('tpAction')" :key="a.value" :value="a.value">{{ a.text }}</option>
                </select>
              </div>
            </div>
          </div>
        </template>
      </div>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="editing = null">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>
</template>
