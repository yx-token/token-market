<script setup lang="ts">
/**
 * 检测站：配置卡片 + 最近检测 DataTable。
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

defineOptions({ name: 'MarketAdminProviders' })

interface DetectionProvider {
  name: string
  label: string
  description: string
  docs_url: string
  default_base_url: string
  options: Record<string, string>
  runs_per_hour_limit: number
  requests_per_run: number
  enabled: boolean
  base_url: string
  has_api_key: boolean
  settings: Record<string, unknown>
  max_runs_per_hour: number
  timeout_minutes: number
  runs_last_hour: number
}

interface ThirdPartyCheck {
  id: number
  upstream_id: number
  provider: string
  model: string
  status: string
  outcome?: string
  verdict?: string
  error?: string
  created_at?: string
}

type FormState = {
  enabled: boolean
  baseURL: string
  rate: number
  timeout: number
  apiKey: string
  settings: Record<string, unknown>
}

const BOOL_OPTIONS = new Set(['run_context_check', 'bias_fingerprint', 'effort_verification'])

const tab = ref<'checks' | 'config'>('config')
const providers = ref<DetectionProvider[]>([])
const checks = ref<ThirdPartyCheck[]>([])
const forms = reactive<Record<string, FormState>>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'pending', label: 'pending' },
  { value: 'running', label: 'running' },
  { value: 'done', label: 'done' },
  { value: 'failed', label: 'failed' },
]

const initQueryParams = { pageNum: 1, pageSize: 15, keyword: '', provider: '', status: '' }
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'upstream', label: '上游', class: 'col-up' },
  { key: 'provider', label: '检测站', class: 'col-prov' },
  { key: 'model', label: '模型', class: 'col-model' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'verdict', label: '判定', class: 'col-verdict' },
  { key: 'created_at', label: '时间', class: 'col-time' },
]

const providerFilterOptions = computed(() => [
  { value: '', label: '全部' },
  ...providers.value.map((p) => ({ value: p.name, label: p.label })),
])

const filteredChecks = computed(() => {
  let rows = checks.value
  if (queryParams.value.provider) rows = rows.filter((c) => c.provider === queryParams.value.provider)
  if (queryParams.value.status) rows = rows.filter((c) => c.status === queryParams.value.status)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((c) =>
      [c.id, c.upstream_id, c.provider, c.model, c.outcome, c.verdict]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return rows
})

const total = computed(() => filteredChecks.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filteredChecks.value.slice(start, start + queryParams.value.pageSize)
})

watch(
  () => [queryParams.value.keyword, queryParams.value.provider, queryParams.value.status, queryParams.value.pageSize],
  () => {
    queryParams.value.pageNum = 1
  },
)

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function providerLabel(name: string) {
  return providers.value.find((p) => p.name === name)?.label || name
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function triState(f: Record<string, unknown>, key: string): string {
  return f[key] === true ? 'on' : f[key] === false ? 'off' : ''
}

function setTriState(f: Record<string, unknown>, key: string, v: string) {
  if (v === '') delete f[key]
  else f[key] = v === 'on'
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
    const [ps, cs] = await Promise.all([
      marketGet<DetectionProvider[] | null>(`${MARKET_API_PREFIX}/detection-providers`),
      marketGet<ThirdPartyCheck[] | null>(`${MARKET_API_PREFIX}/third-party-checks?limit=100`).catch(() => []),
    ])
    providers.value = ps ?? []
    for (const p of providers.value) {
      forms[p.name] = {
        enabled: p.enabled,
        baseURL: p.base_url,
        rate: p.max_runs_per_hour,
        timeout: p.timeout_minutes,
        apiKey: '',
        settings: { ...(p.settings || {}) },
      }
    }
    checks.value = cs ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function save(p: DetectionProvider) {
  const f = forms[p.name]
  if (!f) return
  const settings: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(f.settings)) {
    if (v === '' || v === null || v === undefined) continue
    settings[k] = v
  }
  const body: Record<string, unknown> = {
    enabled: f.enabled,
    base_url: f.baseURL,
    max_runs_per_hour: Number(f.rate),
    timeout_minutes: Number(f.timeout),
    settings,
  }
  if (f.apiKey) body.api_key = f.apiKey
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/detection-providers/${p.name}`, body)
    msg.value = `${p.label} 已保存`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
})

onMounted(load)
</script>

<template>
  <section id="market-providers-page" class="providers-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'config' ? '检测站配置' : '最近检测' }}</h2>
          <p class="y-page-desc">
            {{
              tab === 'config'
                ? '启用第三方检测站、配置接口与提交频率'
                : '第三方检测站最近提交的模型身份检测记录'
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
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'config' }" @click="tab = 'config'">检测站配置</button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'checks' }" @click="tab = 'checks'">最近检测</button>
        </nav>

        <template v-if="tab === 'config'">
          <p v-if="loading" class="y-msg">加载中…</p>
          <template v-else>
            <div v-for="p in providers" :key="p.name" class="provider-card">
              <div class="provider-head">
                <h3>{{ p.label }}</h3>
                <span class="status-cell">
                  <i class="status-dot" :data-tone="p.enabled ? 'ok' : 'idle'" />
                  {{ p.enabled ? '已启用' : '未启用' }}
                </span>
                <a v-if="p.docs_url" :href="p.docs_url" target="_blank" rel="noopener noreferrer" class="y-link">文档</a>
                <span class="provider-meta">最近 1 小时提交 {{ p.runs_last_hour }} 次</span>
              </div>
              <p class="provider-desc">{{ p.description }}</p>
              <p class="provider-hint">
                每次约 {{ p.requests_per_run }} 个请求；检测站限制同出口 IP 每小时 {{ p.runs_per_hour_limit }} 次。
              </p>
              <div v-if="forms[p.name]" class="provider-form">
                <label class="y-check"><input v-model="forms[p.name].enabled" type="checkbox" /> 启用</label>
                <label class="y-field y-field--wide">
                  <span>接口地址</span>
                  <input v-model="forms[p.name].baseURL" class="input w-full" :placeholder="p.default_base_url" />
                </label>
                <label class="y-field">
                  <span>每小时最多提交</span>
                  <input v-model.number="forms[p.name].rate" class="input w-full" type="number" min="1" />
                </label>
                <label class="y-field">
                  <span>等待结果上限（分钟）</span>
                  <input v-model.number="forms[p.name].timeout" class="input w-full" type="number" min="5" />
                </label>
                <label v-for="(desc, key) in p.options" :key="key" class="y-field">
                  <span>{{ desc }}</span>
                  <select
                    v-if="BOOL_OPTIONS.has(String(key))"
                    class="input w-full"
                    :value="triState(forms[p.name].settings, String(key))"
                    @change="setTriState(forms[p.name].settings, String(key), ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">检测站默认</option>
                    <option value="on">开启</option>
                    <option value="off">关闭</option>
                  </select>
                  <input v-else v-model="forms[p.name].settings[key]" class="input w-full" />
                </label>
                <label class="y-field">
                  <span>API Key（可选）</span>
                  <input
                    v-model="forms[p.name].apiKey"
                    class="input w-full"
                    type="password"
                    :placeholder="p.has_api_key ? '已设置，留空不修改' : '无需鉴权时留空'"
                  />
                </label>
                <label class="y-check y-field--wide">
                  <input v-model="forms[p.name].settings.allow_insecure_target" type="checkbox" />
                  允许把 http / 内网地址交给检测站（仅开发）
                </label>
                <div class="provider-actions">
                  <button type="button" class="btn btn-primary" :disabled="busy" @click="save(p)">保存</button>
                </div>
              </div>
            </div>
            <p v-if="!providers.length" class="y-msg">暂无检测站</p>
          </template>
        </template>

        <template v-else>
          <div class="section-block">
            <div class="section-title">检测信息</div>
            <div class="filter-grid">
              <div class="filter-item">
                <label class="filter-label">关键词</label>
                <div class="relative flex-1 w-full min-w-0">
                  <input
                    v-model="queryParams.keyword"
                    class="filter-input w-full"
                    type="search"
                    placeholder="上游 / 模型 / 判定"
                    @keyup.enter="handleQuery"
                  />
                </div>
              </div>
              <div class="filter-item">
                <label class="filter-label">检测站</label>
                <Select
                  v-model="queryParams.provider"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="providerFilterOptions"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label">状态</label>
                <Select
                  v-model="queryParams.status"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="STATUS_OPTIONS"
                />
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
      </template>

      <template v-if="tab === 'checks'" #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" :expandable-actions="false">
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">{{ row.id }}</template>
          <template #cell-upstream="{ row }">{{ row.upstream_id ? `上游 ${row.upstream_id}` : '—' }}</template>
          <template #cell-provider="{ row }">{{ providerLabel(row.provider) }}</template>
          <template #cell-model="{ row }">{{ row.model || '—' }}</template>
          <template #cell-status="{ row }">{{ row.status }}</template>
          <template #cell-verdict="{ row }">{{ row.verdict || row.outcome || row.error || '—' }}</template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #empty>
            <EmptyState title="暂无检测记录" description="启用检测站并触发检测后会出现在这里" />
          </template>
        </DataTable>
      </template>

      <template v-if="tab === 'checks'" #pagination>
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
#market-providers-page.providers-shell {
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
.provider-card {
  margin-bottom: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid rgb(0 0 0 / 6%);
}
.provider-card:last-child { border-bottom: 0; }
.provider-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.provider-head h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.provider-meta {
  margin-left: auto;
  font-size: 12px;
  opacity: 0.65;
}
.provider-desc,
.provider-hint {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--color-text-secondary, #868686);
}
.provider-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 12px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span { color: var(--color-text-secondary, #868686); }
.y-field--wide { grid-column: 1 / -1; }
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.provider-actions {
  grid-column: 1 / -1;
  display: flex;
  justify-content: flex-end;
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
#market-providers-page :deep(.filter-grid .select-trigger),
#market-providers-page :deep(.filter-grid .select-trigger-sm),
#market-providers-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-providers-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-providers-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
}
</style>
