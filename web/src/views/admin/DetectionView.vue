<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { DetectionProvider, Policy, ThirdPartyCheck } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DateTimeField from '@/components/common/DateTimeField.vue'
import DataTable from '@/components/DataTable.vue'
import TabBar from '@/components/TabBar.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { authTypeText, label, options } from '@/utils/labels'
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
type TPForm = { enabled: boolean; triggers: string[]; model: string; format: string; onMismatch: string; onSubmodel: string }
const thirdParty = reactive<Record<string, TPForm>>({})
const { busy, run } = useAction()
const editing = ref<Policy | null>(null)
const form = reactive({
  enabled: false,
  scheduled: false,
  interval: 360,
  jitter: 30,
  suites: '',
  models: '',
  budget: 1,
  items: 12,
  h0: 0.9,
  h1: 0.7,
  alpha: 0.05,
  beta: 0.1,
  autoPause: true,
  grade: 'B',
})
const newAuth = reactive({ platform: '', authType: '' })
const override = reactive({ upstream: '', mode: 'on', scheduled: '', expires: '', reason: '' })
onMounted(() => {
  policies.load()
  providers.load()
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
    suites: p.suites.join(', '),
    models: p.models.join(', '),
    budget: p.daily_budget_usd,
    items: p.items_per_run,
    h0: p.thresholds.h0,
    h1: p.thresholds.h1,
    alpha: p.thresholds.alpha,
    beta: p.thresholds.beta,
    autoPause: p.auto_pause,
    grade: p.default_grade,
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

const split = (s: string) => s.split(/[,\s]+/).filter(Boolean)

async function save() {
  const p = editing.value
  if (!p) return
  const body = {
    enabled: form.enabled,
    scheduled_enabled: form.enabled && form.scheduled,
    interval_minutes: Number(form.interval),
    jitter_minutes: Number(form.jitter),
    suites: split(form.suites),
    models: split(form.models),
    daily_budget_usd: Number(form.budget),
    items_per_run: Number(form.items),
    thresholds: { h0: Number(form.h0), h1: Number(form.h1), alpha: Number(form.alpha), beta: Number(form.beta) },
    auto_pause: form.autoPause,
    default_grade: form.grade,
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
    toast.error('请输入上游 ID')
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
        { key: 'items_per_run', label: '每次题量', align: 'right' },
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
          <label class="label" for="ou">上游 ID</label>
          <input id="ou" v-model="override.upstream" class="input" inputmode="numeric" placeholder="例如 42" />
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
        <input id="np" v-model="newAuth.platform" class="input" placeholder="anthropic" />
      </div>
      <div>
        <label class="label" for="na">接入方式</label>
        <input id="na" v-model="newAuth.authType" class="input" placeholder="setup-token" />
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
    <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
      <div>
        <label class="label" for="pi">周期（分钟）</label>
        <input id="pi" v-model.number="form.interval" class="input" type="number" min="10" />
      </div>
      <div>
        <label class="label" for="pj">随机抖动（分钟）</label>
        <input id="pj" v-model.number="form.jitter" class="input" type="number" min="0" />
      </div>
      <div>
        <label class="label" for="pn2">每次题量</label>
        <input id="pn2" v-model.number="form.items" class="input" type="number" min="1" max="200" />
      </div>
      <div>
        <label class="label" for="pb">每日预算</label>
        <input id="pb" v-model.number="form.budget" class="input" type="number" min="0" step="0.1" />
      </div>
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="label" for="pm">模型（逗号分隔，多个时轮换）</label>
        <input id="pm" v-model="form.models" class="input" />
      </div>
      <div>
        <label class="label" for="ps">套件</label>
        <input id="ps" v-model="form.suites" class="input" />
        <p class="hint">availability, latency, iq, reasoning, long_context</p>
      </div>
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
                <input :id="`tp-model-${pr.name}`" v-model="thirdParty[pr.name].model" class="input" placeholder="默认同平台探测" />
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
