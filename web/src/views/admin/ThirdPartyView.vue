<script setup lang="ts">
// 第三方检测站：启用与参数配置、提交频率、最近的检测记录。在哪些检测中追加第三方检测由"检测策略"配置。
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { DetectionProvider, ThirdPartyCheckView } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import TabBar from '@/components/TabBar.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ThirdPartyResult from '@/components/ThirdPartyResult.vue'
import { options } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const tab = ref<'config' | 'checks'>('checks')
const pageTitle = computed(() => (tab.value === 'config' ? '检测站配置' : '最近检测'))
const pageDescription = computed(() =>
  tab.value === 'config'
    ? '启用第三方检测站、配置接口与提交频率。在哪些检测中追加由「检测策略」决定。'
    : '第三方检测站最近提交的模型身份检测记录。',
)
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  provider: '',
  status: '',
  startDate: '',
  endDate: '',
})

const providers = useLoader(() => admin.providers(), [] as DetectionProvider[] | null)
const checks = useLoader(() => admin.thirdPartyChecks({ limit: 100 }), [] as ThirdPartyCheckView[] | null)
const { busy, run } = useAction()
const forms = reactive<Record<string, { enabled: boolean; baseURL: string; rate: number; timeout: number; apiKey: string; settings: Record<string, unknown> }>>({})
const openCheck = ref<ThirdPartyCheckView | null>(null)

const boolOptions = new Set(['run_context_check', 'bias_fingerprint', 'effort_verification'])

async function load() {
  await providers.load()
  for (const p of providers.data.value ?? []) {
    forms[p.name] = { enabled: p.enabled, baseURL: p.base_url, rate: p.max_runs_per_hour, timeout: p.timeout_minutes, apiKey: '', settings: { ...p.settings } }
  }
  checks.load()
}
onMounted(load)

const filtered = computed(() => {
  let rows = checks.data.value ?? []
  if (queryParams.provider) rows = rows.filter((c) => c.provider === queryParams.provider)
  if (queryParams.status) rows = rows.filter((c) => c.status === queryParams.status)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'upstream_id', 'provider', 'model', 'outcome', 'verdict'] as (keyof ThirdPartyCheckView)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  checks.load()
}
function onReset() {
  resetQuery()
  resetPage()
  checks.load()
}

async function save(p: DetectionProvider) {
  const f = forms[p.name]
  const settings: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(f.settings)) {
    if (v === '' || v === null || v === undefined) continue
    settings[k] = v
  }
  const body: Record<string, unknown> = { enabled: f.enabled, base_url: f.baseURL, max_runs_per_hour: Number(f.rate), timeout_minutes: Number(f.timeout), settings }
  if (f.apiKey) body.api_key = f.apiKey
  if (await run(() => admin.updateProvider(p.name, body), '已保存')) load()
}

/** 三态：跟随检测站默认 / 开 / 关 */
function triState(f: Record<string, unknown>, key: string): string {
  return f[key] === true ? 'on' : f[key] === false ? 'off' : ''
}
function setTriState(f: Record<string, unknown>, key: string, v: string) {
  if (v === '') delete f[key]
  else f[key] = v === 'on'
}
</script>

<template>
  <ConsoleListPage
    id="console-third-party-page"
    :title="pageTitle"
    :description="pageDescription"
  >
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'checks', text: '最近检测' },
          { key: 'config', text: '检测站配置' },
        ]"
      />
    </template>

    <template v-if="tab === 'checks'" #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="上游 / 模型 / 判定" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">检测站</label>
        <select v-model="queryParams.provider" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="p in providers.data.value ?? []" :key="p.name" :value="p.name">{{ p.label }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('tpStatus')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">时间</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <div v-if="tab === 'config'" class="console-panel">
      <div v-for="p in providers.data.value ?? []" :key="p.name" class="mb-8 border-b border-gray-100 pb-8 last:mb-0 last:border-0 last:pb-0">
        <div class="mb-4 flex flex-wrap items-center gap-3">
          <h2 class="card-title !mb-0">{{ p.label }}</h2>
          <span :class="['chip', p.enabled ? 'chip-green' : 'chip-gray']">
            {{ p.enabled ? '已启用' : '未启用' }}
          </span>
          <a :href="p.docs_url" target="_blank" rel="noopener noreferrer" class="link text-sm">文档</a>
          <span class="ml-auto text-xs text-slate-500">最近 1 小时提交 {{ p.runs_last_hour }} 次</span>
        </div>
        <p class="mb-3 text-sm text-slate-600">{{ p.description }}</p>
        <p class="hint mb-4">
          每次检测约向被测账号发送 {{ p.requests_per_run }} 个请求（计入平台检测成本、消耗卖家订阅窗口），检测站限制同一出口 IP 每小时
          {{ p.runs_per_hour_limit }} 次。建议只在上架检测、管理员检测、申诉复测中启用。检测站需要能访问 Edge 的公网 https 地址（edge.public_url）。
        </p>
        <div v-if="forms[p.name]" class="grid grid-cols-1 gap-4 md:grid-cols-4">
          <label class="flex items-center gap-2 text-sm md:col-span-4"><input v-model="forms[p.name].enabled" type="checkbox" /> 启用</label>
          <div class="md:col-span-2">
            <label class="label" :for="`${p.name}-url`">接口地址</label>
            <input :id="`${p.name}-url`" v-model="forms[p.name].baseURL" class="input" :placeholder="p.default_base_url" />
          </div>
          <div>
            <label class="label" :for="`${p.name}-rate`">每小时最多提交</label>
            <input :id="`${p.name}-rate`" v-model.number="forms[p.name].rate" class="input" type="number" min="1" />
          </div>
          <div>
            <label class="label" :for="`${p.name}-timeout`">等待结果上限（分钟）</label>
            <input :id="`${p.name}-timeout`" v-model.number="forms[p.name].timeout" class="input" type="number" min="5" />
          </div>
          <div v-for="(desc, key) in p.options" :key="key">
            <label class="label" :for="`${p.name}-${key}`">{{ desc }}</label>
            <select
              v-if="boolOptions.has(String(key))"
              :id="`${p.name}-${key}`"
              class="input"
              :value="triState(forms[p.name].settings, String(key))"
              @change="setTriState(forms[p.name].settings, String(key), ($event.target as HTMLSelectElement).value)"
            >
              <option value="">检测站默认</option>
              <option value="on">开启</option>
              <option value="off">关闭</option>
            </select>
            <input v-else :id="`${p.name}-${key}`" v-model="forms[p.name].settings[key]" class="input" />
          </div>
          <div>
            <label class="label" :for="`${p.name}-key`">API Key（可选）</label>
            <input :id="`${p.name}-key`" v-model="forms[p.name].apiKey" class="input" type="password" :placeholder="p.has_api_key ? '已设置，留空不修改' : '检测站无需鉴权时留空'" />
          </div>
          <label class="flex items-center gap-2 text-sm md:col-span-4">
            <input v-model="forms[p.name].settings.allow_insecure_target" type="checkbox" /> 允许把 http / 内网地址交给检测站（仅开发环境，真实检测站会拒绝）
          </label>
        </div>
        <div class="mt-5 flex justify-end">
          <button class="btn btn-primary" :disabled="busy" @click="save(p)">保存</button>
        </div>
      </div>
    </div>

    <DataTable
      v-else
      :columns="[
        { key: 'created_at', label: '时间' },
        { key: 'upstream_id', label: '上游' },
        { key: 'provider', label: '检测站' },
        { key: 'model', label: '模型' },
        { key: 'status', label: '状态' },
        { key: 'outcome', label: '身份判定' },
        { key: 'verdict', label: '折算结论' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="checks.loading.value"
      row-key="id"
      empty="没有符合条件的检测记录"
    >
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-upstream_id="{ row }">#{{ row.upstream_id }}</template>
      <template #cell-provider="{ row }">{{ providers.data.value?.find((p) => p.name === row.provider)?.label ?? row.provider }}</template>
      <template #cell-model="{ row }">{{ row.model }} <span class="text-xs text-slate-400">{{ row.format }}</span></template>
      <template #cell-status="{ row }"><StatusBadge kind="tpStatus" :value="row.status" /></template>
      <template #cell-outcome="{ row }"><StatusBadge v-if="row.outcome" kind="tpOutcome" :value="row.outcome" /></template>
      <template #cell-verdict="{ row }"><StatusBadge v-if="row.verdict" kind="verdict" :value="row.verdict" /></template>
      <template #cell-actions="{ row }"><button class="btn-ghost btn-sm" @click="openCheck = row">详情</button></template>
    </DataTable>

    <template v-if="tab === 'checks'" #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog :open="!!openCheck" :title="`第三方检测 #${openCheck?.id ?? ''}`" wide @update:open="(v: boolean) => !v && (openCheck = null)">
    <ThirdPartyResult
      v-if="openCheck"
      :provider="openCheck.provider"
      :status="openCheck.status"
      :outcome="openCheck.outcome"
      :verdict="openCheck.verdict"
      :model="openCheck.model"
      :report-url="openCheck.report_url"
      :error="openCheck.error"
      :summary="openCheck.summary"
    />
    <p v-if="openCheck?.external_id" class="text-xs text-slate-400">检测站编号 {{ openCheck.external_id }}，检测记录 #{{ openCheck.run_id }}</p>
  </ModalDialog>
</template>
