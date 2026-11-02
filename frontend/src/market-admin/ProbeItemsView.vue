<script setup lang="ts">
/**
 * 题库 / 基线账号：TablePageLayout + filter-grid + DataTable。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import { useRouter } from 'vue-router'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminProbeItems' })

const router = useRouter()

interface ProbeItem {
  id: number
  suite: string
  category: string
  prompt: string
  grader: Record<string, unknown>
  platforms: string[]
  models: string[]
  max_tokens: number
  reasoning_effort?: string
  status: string
  check_key?: string
  request?: Record<string, unknown>
  generator?: Record<string, unknown>
  modes?: string[]
  weight?: number
  builtin_key?: string
}

interface RefUpstream {
  id: number
  platform: string
  auth_type?: string
  account_email?: string
  status: string
  is_reference?: boolean
}

interface Vendor {
  platform: string
  supported_auth_types: string[]
}

const SUITES = [
  { value: 'inspection_capability', text: '能力检测' },
  { value: 'inspection_identity', text: '身份核验' },
  { value: 'inspection_quality', text: '质量抽检' },
  { value: 'iq', text: '旧题库 (iq)' },
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

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'active', label: '启用' },
  { value: 'retired', label: '已下线' },
]

const SUITE_OPTIONS = [
  { value: '', label: '全部' },
  ...SUITES.map((s) => ({ value: s.value, label: s.text })),
]

const REF_STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'draft', label: 'draft' },
  { value: 'authorizing', label: 'authorizing' },
  { value: 'ready', label: 'ready' },
  { value: 'active', label: 'active' },
  { value: 'error', label: 'error' },
]

const tab = ref<'items' | 'refs'>('items')
const rows = ref<ProbeItem[]>([])
const refs = ref<RefUpstream[]>([])
const vendors = ref<Vendor[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const initQueryParams = {
  pageNum: 1,
  pageSize: getPersistedPageSize(),
  keyword: '',
  status: 'active',
  suite: '',
  category: '',
  platform: '',
  ref_status: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const newRef = reactive({ platform: 'anthropic', authType: 'oauth' })
const showCreateRef = ref(false)

const show = ref(false)
const editing = ref<ProbeItem | null>(null)
const pendingDestructive = ref<
  | { kind: 'retire'; item: ProbeItem }
  | { kind: 'removeRef'; upstream: RefUpstream }
  | null
>(null)
const form = reactive({
  suite: 'inspection_capability',
  category: '',
  prompt: '',
  grader: '{"type":"exact","answer":""}',
  platforms: '',
  models: '',
  max_tokens: 1024,
  check_key: '',
  modes: 'full,quick',
  weight: 1,
  request: '{}',
  generator: '{}',
})

const itemColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'suite', label: '套件', class: 'col-suite' },
  { key: 'category', label: '分类', class: 'col-cat' },
  { key: 'prompt', label: '题目', class: 'col-prompt' },
  { key: 'check_key', label: 'check_key', class: 'col-key' },
  { key: 'weight', label: '权重', class: 'col-weight' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const refColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'id', label: 'ID', class: 'col-id' },
  { key: 'platform', label: '厂商', class: 'col-platform' },
  { key: 'auth_type', label: '接入', class: 'col-auth' },
  { key: 'email', label: '邮箱', class: 'col-email' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const columns = computed(() => (tab.value === 'items' ? itemColumns : refColumns))

const categoryFilterOptions = computed(() => {
  const set = new Set<string>()
  for (const it of rows.value) {
    const c = String(it.category || '').trim()
    if (c) set.add(c)
  }
  return [
    { value: '', label: '全部' },
    ...[...set].sort().map((value) => ({ value, label: value })),
  ]
})

const platformFilterOptions = computed(() => [
  { value: '', label: '全部' },
  ...Object.entries(PLATFORM_LABEL).map(([value, label]) => ({ value, label })),
])

const refAuthTypes = computed(
  () => vendors.value.find((v) => v.platform === newRef.platform)?.supported_auth_types ?? ['oauth', 'api_key'],
)

const filteredItems = computed(() => {
  let list = rows.value
  if (queryParams.value.status) list = list.filter((it) => it.status === queryParams.value.status)
  if (queryParams.value.suite) list = list.filter((it) => it.suite === queryParams.value.suite)
  if (queryParams.value.category) list = list.filter((it) => it.category === queryParams.value.category)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((it) =>
      [it.id, it.suite, it.category, it.prompt, it.check_key, it.status, it.builtin_key, ...(it.platforms || []), ...(it.models || [])]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const filteredRefs = computed(() => {
  let list = refs.value
  if (queryParams.value.platform) list = list.filter((u) => u.platform === queryParams.value.platform)
  if (queryParams.value.ref_status) list = list.filter((u) => u.status === queryParams.value.ref_status)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((u) =>
      [u.id, u.platform, PLATFORM_LABEL[u.platform], u.auth_type, u.account_email, u.status]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const pageSource = computed(() => (tab.value === 'items' ? filteredItems.value : filteredRefs.value))
const total = computed(() => pageSource.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return pageSource.value.slice(start, start + queryParams.value.pageSize) as Array<ProbeItem & RefUpstream>
})

const allStats = computed(() => {
  if (tab.value === 'items') {
    const all = rows.value
    return {
      total: filteredItems.value.length,
      active: all.filter((it) => it.status === 'active').length,
      retired: all.filter((it) => it.status === 'retired').length,
    }
  }
  const all = refs.value
  return {
    total: filteredRefs.value.length,
    active: all.filter((u) => u.status === 'ready' || u.status === 'active').length,
    retired: all.filter((u) => u.status === 'draft' || u.status === 'authorizing').length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.status,
    queryParams.value.suite,
    queryParams.value.category,
    queryParams.value.platform,
    queryParams.value.ref_status,
    queryParams.value.pageSize,
    tab.value,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

watch(
  () => queryParams.value.status,
  () => {
    if (tab.value === 'items') load()
  },
)

watch(total, (value) => {
  const maxPage = Math.max(1, Math.ceil(value / queryParams.value.pageSize))
  queryParams.value.pageNum = Math.min(queryParams.value.pageNum, maxPage)
})

function suiteText(v: string) {
  return SUITES.find((s) => s.value === v)?.text || v
}

function platLabel(p: string) {
  return PLATFORM_LABEL[p] || p
}

function statusLabel(s: string) {
  if (s === 'active') return '启用'
  if (s === 'retired') return '已下线'
  return s || '—'
}

function statusTone(s: string) {
  if (s === 'active' || s === 'ready') return 'ok'
  if (s === 'retired' || s === 'error') return 'err'
  if (s === 'draft' || s === 'authorizing') return 'progress'
  return 'idle'
}

function splitCSV(s: string) {
  return s
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function handleQuery() {
  queryParams.value.pageNum = 1
  if (tab.value === 'items') load()
}

function resetQuery() {
  Object.assign(queryParams.value, {
    ...initQueryParams,
    status: tab.value === 'items' ? 'active' : '',
  })
  if (tab.value === 'items') load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'items') {
      const q = queryParams.value.status ? `?status=${encodeURIComponent(queryParams.value.status)}` : ''
      rows.value = (await marketGet<ProbeItem[] | null>(`${MARKET_API_PREFIX}/probe-items${q}`)) ?? []
    } else {
      const [rs, vs] = await Promise.all([
        marketGet<RefUpstream[] | null>(`${MARKET_API_PREFIX}/reference-accounts`),
        marketGet<Vendor[] | null>(`${MARKET_API_PREFIX}/vendors`).catch(() => []),
      ])
      refs.value = rs ?? []
      vendors.value = vs ?? []
      if (refAuthTypes.value.length && !refAuthTypes.value.includes(newRef.authType)) {
        newRef.authType = refAuthTypes.value[0]
      }
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
    refs.value = []
  } finally {
    loading.value = false
  }
}

async function run<T = unknown>(label: string, fn: () => Promise<T>): Promise<T | false> {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const result = await fn()
    msg.value = label
    await load()
    return result
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openNew() {
  error.value = ''
  msg.value = ''
  editing.value = null
  Object.assign(form, {
    suite: 'inspection_capability',
    category: '',
    prompt: '',
    grader: '{"type":"exact","answer":""}',
    platforms: '',
    models: '',
    max_tokens: 1024,
    check_key: '',
    modes: 'full,quick',
    weight: 1,
    request: '{}',
    generator: '{}',
  })
  show.value = true
}

function openEdit(it: ProbeItem) {
  error.value = ''
  msg.value = ''
  editing.value = it
  Object.assign(form, {
    suite: it.suite,
    category: it.category,
    prompt: it.prompt,
    grader: JSON.stringify(it.grader ?? {}, null, 2),
    platforms: (it.platforms || []).join(', '),
    models: (it.models || []).join(', '),
    max_tokens: it.max_tokens || 1024,
    check_key: it.check_key || '',
    modes: (it.modes || []).join(','),
    weight: it.weight ?? 1,
    request: JSON.stringify(it.request ?? {}, null, 2),
    generator: JSON.stringify(it.generator ?? {}, null, 2),
  })
  show.value = true
}

function openCreateRef() {
  error.value = ''
  msg.value = ''
  showCreateRef.value = true
}

function closeCreateRef() {
  showCreateRef.value = false
  error.value = ''
}

function closeModal() {
  show.value = false
  error.value = ''
}

function parseObject(raw: string, name: string): Record<string, unknown> {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    throw new Error(`${name}需要是 JSON 对象`)
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name}需要是 JSON 对象`)
  return value as Record<string, unknown>
}

async function save() {
  try {
    const grader = parseObject(form.grader, '判分规则')
    if (typeof grader.type !== 'string' || !grader.type) throw new Error('判分规则需要非空 type')
    const request = parseObject(form.request, '请求模板')
    const generator = parseObject(form.generator, '生成器定义')
    if (!form.prompt.trim()) throw new Error('请填写题目内容')
    if (!Number.isFinite(form.weight) || form.weight < 0) throw new Error('权重需要是非负数')

    const body = {
      suite: form.suite,
      category: form.category.trim(),
      prompt: form.prompt.trim(),
      grader,
      platforms: splitCSV(form.platforms),
      models: splitCSV(form.models),
      max_tokens: Number(form.max_tokens) || 1024,
      check_key: form.check_key.trim(),
      request,
      generator,
      modes: splitCSV(form.modes),
      weight: Number(form.weight) || 0,
      status: editing.value?.status ?? 'active',
    }

    const ok = editing.value
      ? await run(`题目 #${editing.value.id} 已保存`, () =>
          marketSend('PUT', `${MARKET_API_PREFIX}/probe-items/${editing.value!.id}`, body),
        )
      : await run('题目已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/probe-items`, body))
    if (ok) closeModal()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  }
}

function retire(it: ProbeItem) {
  if (it.status === 'retired') return
  error.value = ''
  pendingDestructive.value = { kind: 'retire', item: it }
}

async function createRef() {
  const res = await run('基线账号草稿已创建', () =>
    marketSend<{ upstream?: RefUpstream }>('POST', `${MARKET_API_PREFIX}/reference-accounts`, {
      platform: newRef.platform,
      auth_type: newRef.authType,
    }),
  )
  if (res) {
    showCreateRef.value = false
    const id = res.upstream?.id
    if (id) {
      msg.value = `已创建草稿 #${id}，正在打开上游页…`
      await router.push({ path: '/admin/market/upstreams', query: { q: String(id), focus: String(id) } })
    } else {
      msg.value = '基线账号草稿已创建，请到上游页完成授权'
    }
  }
}

function goAuthorize(u: RefUpstream) {
  return router.push({ path: '/admin/market/upstreams', query: { q: String(u.id), focus: String(u.id) } })
}

function removeRef(u: RefUpstream) {
  error.value = ''
  pendingDestructive.value = { kind: 'removeRef', upstream: u }
}

async function confirmDestructive() {
  const pending = pendingDestructive.value
  if (!pending) return
  const result = pending.kind === 'retire'
    ? await run(`题目 #${pending.item.id} 已下线`, () =>
        marketSend('POST', `${MARKET_API_PREFIX}/probe-items/${pending.item.id}/retire`),
      )
    : await run('基线账号已移除', () =>
        marketSend('DELETE', `${MARKET_API_PREFIX}/reference-accounts/${pending.upstream.id}`),
      )
  if (result !== false) pendingDestructive.value = null
}

function switchTab(next: 'items' | 'refs') {
  tab.value = next
  Object.assign(queryParams.value, {
    ...initQueryParams,
    status: next === 'items' ? 'active' : '',
  })
  load()
}

onMounted(load)
</script>

<template>
  <section id="market-probe-items-page" class="probe-items-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ tab === 'items' ? '题库' : '基线账号' }}</h2>
          <p class="y-page-desc">
            {{
              tab === 'items'
                ? '测智与身份核验题库；可创建、编辑与下线'
                : '平台自备干净官方账号，与被测上游同批次答题作对照'
            }}
          </p>
        </div>
      </template>

      <template #actions>
        <button
          v-if="tab === 'items'"
          type="button"
          class="btn btn-primary page-action-btn"
          :disabled="busy"
          @click="openNew"
        >
          <Icon name="plus" size="sm" />
          新建题目
        </button>
        <button
          v-else
          type="button"
          class="btn btn-primary page-action-btn"
          :disabled="busy"
          @click="openCreateRef"
        >
          <Icon name="plus" size="sm" />
          添加基线
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error && !show && !showCreateRef" class="y-msg y-msg--error mb-2" role="alert">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2" role="status" aria-live="polite">{{ msg }}</p>

        <nav class="y-tabs mb-3" aria-label="题库视图">
          <button
            type="button"
            class="y-tab"
            :class="{ 'is-on': tab === 'items' }"
            :aria-pressed="tab === 'items'"
            @click="switchTab('items')"
          >
            题目
          </button>
          <button
            type="button"
            class="y-tab"
            :class="{ 'is-on': tab === 'refs' }"
            :aria-pressed="tab === 'refs'"
            @click="switchTab('refs')"
          >
            基线账号
          </button>
        </nav>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">{{ tab === 'items' ? '当前查询题目' : '当前查询基线' }}</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">{{ tab === 'items' ? '当前加载启用' : '当前加载已就绪' }}</div>
                <div class="stat-card__value">{{ allStats.active.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">{{ tab === 'items' ? '当前加载已下线' : '当前加载待授权' }}</div>
                <div class="stat-card__value">{{ allStats.retired.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">{{ tab === 'items' ? '题目信息' : '基线信息' }}</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label" for="probe-items-keyword">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  id="probe-items-keyword"
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  :placeholder="tab === 'items' ? 'ID / 套件 / 题目 / check_key' : 'ID / 厂商 / 邮箱'"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <template v-if="tab === 'items'">
              <div class="filter-item">
                <label class="filter-label" for="probe-items-status">状态</label>
                <Select
                  id="probe-items-status"
                  v-model="queryParams.status"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="STATUS_OPTIONS"
                  aria-label="按题目状态筛选"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label" for="probe-items-suite">套件</label>
                <Select
                  id="probe-items-suite"
                  v-model="queryParams.suite"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="SUITE_OPTIONS"
                  aria-label="按题库套件筛选"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label" for="probe-items-category">分类</label>
                <Select
                  id="probe-items-category"
                  v-model="queryParams.category"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="categoryFilterOptions"
                  aria-label="按题目分类筛选"
                />
              </div>
            </template>
            <template v-else>
              <div class="filter-item">
                <label class="filter-label" for="probe-refs-platform">厂商</label>
                <Select
                  id="probe-refs-platform"
                  v-model="queryParams.platform"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="platformFilterOptions"
                  aria-label="按基线平台筛选"
                />
              </div>
              <div class="filter-item">
                <label class="filter-label" for="probe-refs-status">状态</label>
                <Select
                  id="probe-refs-status"
                  v-model="queryParams.ref_status"
                  class="flex-1 w-full min-w-0"
                  size="sm"
                  :options="REF_STATUS_OPTIONS"
                  aria-label="按基线状态筛选"
                />
              </div>
            </template>
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
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-id="{ row }">
            {{ row.id }}
          </template>
          <template #cell-suite="{ row }">{{ suiteText(row.suite) }}</template>
          <template #cell-category="{ row }">{{ row.category || '—' }}</template>
          <template #cell-prompt="{ row }">
            <div class="prompt-cell" :title="row.prompt">{{ row.prompt }}</div>
          </template>
          <template #cell-check_key="{ row }">
            <span class="mono-cell">{{ row.check_key || '—' }}</span>
          </template>
          <template #cell-weight="{ row }">{{ row.weight ?? 1 }}</template>
          <template #cell-platform="{ row }">{{ platLabel(row.platform) }}</template>
          <template #cell-auth_type="{ row }">{{ row.auth_type || '—' }}</template>
          <template #cell-email="{ row }">{{ row.account_email || '—' }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ tab === 'items' ? statusLabel(row.status) : row.status || '—' }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="tab === 'items'">
                <button type="button" class="y-link" :disabled="busy" @click="openEdit(row)">编辑</button>
                <button
                  v-if="row.status !== 'retired'"
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="retire(row)"
                >
                  下线
                </button>
              </template>
              <template v-else>
                <button
                  v-if="row.status === 'draft' || row.status === 'authorizing'"
                  type="button"
                  class="y-link"
                  @click="goAuthorize(row)"
                >
                  去授权
                </button>
                <button type="button" class="y-link" @click="goAuthorize(row)">打开上游</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="removeRef(row)">
                  移除
                </button>
              </template>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="tab === 'items' ? '暂无题目' : '暂无基线账号'"
              :description="
                tab === 'items'
                  ? '点右上角「新建题目」，或调整筛选条件'
                  : '点右上角「添加基线」创建草稿并完成授权'
              "
              :action-text="tab === 'items' ? '新建题目' : '添加基线'"
              @action="tab === 'items' ? openNew() : openCreateRef()"
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
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showCreateRef"
      title="添加基线账号"
      close-on-click-outside
      @close="closeCreateRef"
    >
      <div class="dialog-form">
        <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
        <label class="y-field">
          <span>厂商</span>
          <select
            v-model="newRef.platform"
            class="input w-full"
            @change="newRef.authType = refAuthTypes[0] || 'oauth'"
          >
            <option v-for="(label, key) in PLATFORM_LABEL" :key="key" :value="key">{{ label }}</option>
          </select>
        </label>
        <label class="y-field">
          <span>接入方式</span>
          <select v-model="newRef.authType" class="input w-full">
            <option v-for="t in refAuthTypes" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
        <p class="hint">创建后会跳转到上游页，请在卖家端或上游详情完成 OAuth / 密钥导入。</p>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeCreateRef">取消</button>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="createRef">创建</button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="show"
      :title="editing ? '编辑题目' : '新建题目'"
      width="wide"
      close-on-click-outside
      @close="closeModal"
    >
      <div class="dialog-form">
        <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
        <div class="y-field-row">
          <label class="y-field">
            <span>套件</span>
            <select v-model="form.suite" class="input w-full">
              <option v-for="s in SUITES" :key="s.value" :value="s.value">{{ s.text }}</option>
            </select>
          </label>
          <label class="y-field">
            <span>分类</span>
            <input v-model="form.category" class="input w-full" />
          </label>
        </div>
        <label class="y-field">
          <span>题目内容</span>
          <textarea v-model="form.prompt" class="input w-full y-textarea" rows="4" />
        </label>
        <label class="y-field">
          <span>判分规则 (JSON)</span>
          <textarea v-model="form.grader" class="input w-full y-textarea mono" rows="3" />
        </label>
        <div class="y-field-row">
          <label class="y-field">
            <span>平台（逗号分隔，空=全部）</span>
            <input v-model="form.platforms" class="input w-full" placeholder="anthropic, openai" />
          </label>
          <label class="y-field">
            <span>模型（逗号分隔）</span>
            <input v-model="form.models" class="input w-full" />
          </label>
        </div>
        <div class="y-field-row y-field-row--3">
          <label class="y-field">
            <span>max_tokens</span>
            <input v-model.number="form.max_tokens" type="number" class="input w-full" />
          </label>
          <label class="y-field">
            <span>check_key</span>
            <input v-model="form.check_key" class="input w-full" />
          </label>
          <label class="y-field">
            <span>权重</span>
            <input v-model.number="form.weight" type="number" class="input w-full" min="0" step="0.1" />
          </label>
        </div>
        <label class="y-field">
          <span>模式（逗号分隔：full / quick）</span>
          <input v-model="form.modes" class="input w-full" />
        </label>
        <label class="y-field">
          <span>请求模板 (JSON)</span>
          <textarea v-model="form.request" class="input w-full y-textarea mono" rows="2" />
        </label>
        <label class="y-field">
          <span>生成器定义 (JSON)</span>
          <textarea v-model="form.generator" class="input w-full y-textarea mono" rows="2" />
        </label>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeModal">取消</button>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="save">保存</button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="!!pendingDestructive"
      :title="pendingDestructive?.kind === 'retire' ? '下线题目' : '移除基线账号'"
      :message="pendingDestructive?.kind === 'retire'
        ? `确认下线题目 #${pendingDestructive.item.id}？下线后将不再用于检测。`
        : pendingDestructive ? `确认移除基线账号 #${pendingDestructive.upstream.id}？` : ''"
      :confirm-text="pendingDestructive?.kind === 'retire' ? '确认下线' : '确认移除'"
      cancel-text="取消"
      danger
      :busy="busy"
      @confirm="confirmDestructive"
      @cancel="pendingDestructive = null"
    >
      <p v-if="pendingDestructive && error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
    </ConfirmDialog>
  </section>
</template>

<style scoped>
#market-probe-items-page.probe-items-shell {
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
  color: var(--color-text-secondary, #64748b);
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: var(--color-text-primary, #0f172a);
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

.prompt-cell {
  max-width: 22rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mono-cell {
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
.status-dot[data-tone='err'] {
  background: #ef4444;
}
.status-dot[data-tone='progress'] {
  background: #0555ce;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

#market-probe-items-page :deep(.filter-grid .select-trigger),
#market-probe-items-page :deep(.filter-grid .select-trigger-sm),
#market-probe-items-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-probe-items-page :deep(.table-scroll-container table thead th) {
  background: var(--color-bg-secondary, #f5f7fa);
  color: var(--color-text-secondary, #606266);
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid var(--color-border, #ebeef5);
}

#market-probe-items-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid var(--color-border, #ebeef5);
  font-size: 13px;
  color: var(--color-text-primary, #303133);
}

#market-probe-items-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: var(--color-bg-secondary, #f5f7fa);
}

#market-probe-items-page :deep(th.col-id),
#market-probe-items-page :deep(td.col-id) {
  min-width: 4rem;
}
#market-probe-items-page :deep(th.col-suite),
#market-probe-items-page :deep(td.col-suite),
#market-probe-items-page :deep(th.col-cat),
#market-probe-items-page :deep(td.col-cat),
#market-probe-items-page :deep(th.col-platform),
#market-probe-items-page :deep(td.col-platform) {
  min-width: 6rem;
}
#market-probe-items-page :deep(th.col-prompt),
#market-probe-items-page :deep(td.col-prompt) {
  min-width: 12rem;
  max-width: 22rem;
}
#market-probe-items-page :deep(th.col-key),
#market-probe-items-page :deep(td.col-key),
#market-probe-items-page :deep(th.col-email),
#market-probe-items-page :deep(td.col-email) {
  min-width: 8rem;
}
#market-probe-items-page :deep(th.col-ops),
#market-probe-items-page :deep(td.col-ops) {
  min-width: 12rem;
  width: 12rem;
  box-sizing: border-box;
}

.dialog-form {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.y-field-row--3 {
  grid-template-columns: 1fr 1fr 1fr;
}
.y-textarea,
.y-textarea:focus {
  height: auto !important;
  min-height: 72px;
  padding: 8px 10px;
  line-height: 1.5 !important;
  tab-size: 2;
  resize: vertical;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 767px) {
  .prompt-cell {
    max-width: none;
    overflow: visible;
    overflow-wrap: anywhere;
    text-overflow: clip;
    white-space: normal;
  }
}
@media (max-width: 720px) {
  .y-field-row,
  .y-field-row--3 {
    grid-template-columns: 1fr;
  }
}
</style>
