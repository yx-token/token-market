<script setup lang="ts">
/**
 * 默认分组：TablePageLayout + filter-grid + DataTable；名称/标题分列。
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

defineOptions({ name: 'MarketAdminPools' })

interface Pool {
  id: number
  name: string
  platform: string
  sub2api_group_id?: number | null
  sell_multiplier: number
  min_margin: number
  safety_buffer: number
  enabled: boolean
  market_published: boolean
  market_title?: string
  market_description?: string
}

const PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: '智谱' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'opencode_go', label: 'OpenCode Go' },
  { value: 'composite', label: '综合池' },
]

const PLATFORM_FILTER_OPTIONS = [{ value: '', label: '全部' }, ...PLATFORM_OPTIONS]

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '启用' },
  { value: 'false', label: '停用' },
]

const MARKET_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '已上架' },
  { value: 'false', label: '未上架' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  platform: '',
  enabled: '',
  market: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const pools = ref<Pool[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const show = ref(false)
const editing = ref<Pool | null>(null)
const form = reactive({
  name: 'tm-pool-',
  platform: 'anthropic',
  sell: '',
  margin: '0.10',
  buffer: '0.02',
  enabled: true,
  market_published: false,
  market_title: '',
  market_description: '',
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'name', label: '名称', class: 'col-name' },
  { key: 'market_title', label: '市场标题', class: 'col-title' },
  { key: 'platform', label: '平台', class: 'col-platform' },
  { key: 'sell', label: '售价 D', class: 'col-num' },
  { key: 'margin', label: '毛利 / 缓冲', class: 'col-num' },
  { key: 'cap', label: '寄售上限', class: 'col-num' },
  { key: 'enabled', label: '状态', class: 'col-status' },
  { key: 'market', label: '市场', class: 'col-status' },
  { key: 'sub2api', label: 'sub2api', class: 'col-sub2api' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const filtered = computed(() => {
  let rows = pools.value
  if (queryParams.value.platform) {
    rows = rows.filter((p) => p.platform === queryParams.value.platform)
  }
  if (queryParams.value.enabled === 'true') rows = rows.filter((p) => p.enabled)
  if (queryParams.value.enabled === 'false') rows = rows.filter((p) => !p.enabled)
  if (queryParams.value.market === 'true') rows = rows.filter((p) => p.market_published)
  if (queryParams.value.market === 'false') rows = rows.filter((p) => !p.market_published)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((p) =>
      [p.id, p.name, p.platform, p.sub2api_group_id, p.market_title]
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
  const all = pools.value
  return {
    total: all.length,
    enabled: all.filter((p) => p.enabled).length,
    published: all.filter((p) => p.market_published).length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.platform,
    queryParams.value.enabled,
    queryParams.value.market,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function platformLabel(v: string) {
  return PLATFORM_OPTIONS.find((o) => o.value === v)?.label || v
}

function defaultTitle(p: Pick<Pool, 'name' | 'platform'>) {
  const base = p.name.replace(/^tm-pool-/, '') || p.name
  return p.platform === 'composite' ? `${base} · 平台保障池` : `${base} · 平台保障`
}

function cap(p: Pool) {
  return p.sell_multiplier * (1 - p.min_margin - p.safety_buffer)
}

function fmtMul(n: number) {
  return Number.isFinite(n) ? n.toFixed(3) : '—'
}

function fmtPct(n: number) {
  return Number.isFinite(n) ? `${(n * 100).toFixed(0)}%` : '—'
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
    pools.value = (await marketGet<Pool[] | null>(`${MARKET_API_PREFIX}/pools`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    pools.value = []
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  Object.assign(form, {
    name: 'tm-pool-',
    platform: 'anthropic',
    sell: '',
    margin: '0.10',
    buffer: '0.02',
    enabled: true,
    market_published: false,
    market_title: '',
    market_description: '',
  })
  show.value = true
}

function openEdit(p: Pool) {
  editing.value = p
  Object.assign(form, {
    name: p.name,
    platform: p.platform,
    sell: String(p.sell_multiplier),
    margin: String(p.min_margin),
    buffer: String(p.safety_buffer),
    enabled: p.enabled,
    market_published: p.market_published,
    market_title: p.market_title || defaultTitle(p),
    market_description: p.market_description || '',
  })
  show.value = true
}

function closeModal() {
  show.value = false
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

async function save() {
  const body = {
    name: form.name,
    platform: form.platform,
    sell_multiplier: Number(form.sell),
    min_margin: Number(form.margin),
    safety_buffer: Number(form.buffer),
    enabled: form.enabled,
    market_published: form.market_published,
    market_title: form.market_title.trim(),
    market_description: form.market_description.trim(),
  }
  if (!Number.isFinite(body.sell_multiplier) || body.sell_multiplier <= 0) {
    error.value = '请填写有效的售价 D'
    return
  }
  const ok = editing.value
    ? await run('已保存', () => marketSend('PUT', `${MARKET_API_PREFIX}/pools/${editing.value!.id}`, body))
    : await run('已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/pools`, body))
  if (ok) {
    closeModal()
    await load()
  }
}

async function sync(p: Pool) {
  if (await run(`分组 ${p.name} 已同步`, () => marketSend('POST', `${MARKET_API_PREFIX}/pools/${p.id}/sync`))) {
    await load()
  }
}

async function toggleMarket(p: Pool) {
  const next = !p.market_published
  if (
    await run(next ? '已上架市场' : '已下架', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/pools/${p.id}`, {
        market_published: next,
        market_title: p.market_title || defaultTitle(p),
        market_description: p.market_description || '',
      }),
    )
  ) {
    await load()
  }
}

async function toggleEnabled(p: Pool) {
  if (
    await run(`分组 ${p.name} 已${p.enabled ? '关闭' : '开启'}`, () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/pools/${p.id}`, {
        name: p.name,
        platform: p.platform,
        enabled: !p.enabled,
        sell_multiplier: p.sell_multiplier,
        min_margin: p.min_margin,
        safety_buffer: p.safety_buffer,
        market_published: p.market_published,
        market_title: p.market_title || '',
        market_description: p.market_description || '',
      }),
    )
  ) {
    await load()
  }
}

onMounted(load)
</script>

<template>
  <section id="market-pools-page" class="pools-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">默认分组</h2>
          <p class="y-page-desc">买家走平台默认分组时按售价 D 计费；可将分组上架到市场推销</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy" @click="openNew">
          <Icon name="plus" size="sm" />
          新建分组
        </button>
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
                <div class="stat-card__label">分组总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已启用</div>
                <div class="stat-card__value">{{ allStats.enabled.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">已上架</div>
                <div class="stat-card__value">{{ allStats.published.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">分组信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="名称 / 标题 / ID"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">平台</label>
              <Select
                v-model="queryParams.platform"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PLATFORM_FILTER_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ENABLED_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">市场</label>
              <Select
                v-model="queryParams.market"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="MARKET_OPTIONS"
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
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-name="{ row }">{{ row.name }}</template>
          <template #cell-market_title="{ row }">
            {{ row.market_published ? row.market_title || '—' : '—' }}
          </template>
          <template #cell-platform="{ row }">{{ platformLabel(row.platform) }}</template>
          <template #cell-sell="{ row }">{{ fmtMul(row.sell_multiplier) }}</template>
          <template #cell-margin="{ row }">
            {{ fmtPct(row.min_margin) }} / {{ fmtPct(row.safety_buffer) }}
          </template>
          <template #cell-cap="{ row }">{{ fmtMul(cap(row)) }}</template>
          <template #cell-enabled="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '启用' : '停用' }}
            </span>
          </template>
          <template #cell-market="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.market_published ? 'ok' : 'idle'" />
              {{ row.market_published ? '已上架' : '未上架' }}
            </span>
          </template>
          <template #cell-sub2api="{ row }">
            {{ row.sub2api_group_id ? '已同步' : '未同步' }}
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="openEdit(row)">编辑</button>
              <button type="button" class="y-link" :disabled="busy" @click="sync(row)">同步</button>
              <button type="button" class="y-link" :disabled="busy" @click="toggleEnabled(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button
                type="button"
                class="y-link"
                :disabled="busy || (!row.market_published && (!row.enabled || !row.sub2api_group_id))"
                :title="!row.enabled || !row.sub2api_group_id ? '需先启用并同步到 sub2api' : ''"
                @click="toggleMarket(row)"
              >
                {{ row.market_published ? '下架' : '上架' }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              title="暂无分组"
              description="点右上角「新建分组」创建，或调整筛选条件"
              action-text="新建分组"
              @action="openNew"
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

    <div v-if="show" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="editing ? '编辑默认分组' : '新建默认分组'">
        <header class="y-modal__head">
          <h3>{{ editing ? '编辑默认分组' : '新建默认分组' }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>名称（sub2api 分组名，以 tm-pool- 开头）</span>
            <input v-model="form.name" class="input w-full" :disabled="!!editing" />
          </label>
          <label class="y-field">
            <span>平台</span>
            <select v-model="form.platform" class="input w-full" :disabled="!!editing">
              <option v-for="o in PLATFORM_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </label>
          <div class="y-field-row y-field-row--3">
            <label class="y-field">
              <span>售价 D</span>
              <input v-model="form.sell" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>最低毛利</span>
              <input v-model="form.margin" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>安全缓冲</span>
              <input v-model="form.buffer" class="input w-full" inputmode="decimal" />
            </label>
          </div>
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用</label>
          <div class="y-sep">
            <label class="y-check">
              <input v-model="form.market_published" type="checkbox" :disabled="!editing" />
              作为商品上架到市场
            </label>
            <p class="y-hint">
              {{ editing ? '上架后出现在市场信息流，买家点击后去开通保障池 Key。' : '新建后请先同步，再编辑上架。' }}
            </p>
            <label class="y-field">
              <span>市场标题</span>
              <input
                v-model="form.market_title"
                class="input w-full"
                placeholder="如：Claude · 平台保障"
                :disabled="!editing"
              />
            </label>
            <label class="y-field">
              <span>市场介绍</span>
              <textarea
                v-model="form.market_description"
                class="input w-full y-textarea"
                rows="3"
                placeholder="一两句话介绍平台保障池优势、适用场景"
                :disabled="!editing"
              />
            </label>
          </div>
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
#market-pools-page.pools-shell {
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

#market-pools-page :deep(.filter-grid .select-trigger),
#market-pools-page :deep(.filter-grid .select-trigger-sm),
#market-pools-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-pools-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-pools-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-pools-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-pools-page :deep(th.col-name),
#market-pools-page :deep(td.col-name) {
  min-width: 8rem;
}
#market-pools-page :deep(th.col-title),
#market-pools-page :deep(td.col-title) {
  min-width: 9rem;
}
#market-pools-page :deep(th.col-platform),
#market-pools-page :deep(td.col-platform) {
  min-width: 6rem;
}
#market-pools-page :deep(th.col-num),
#market-pools-page :deep(td.col-num) {
  min-width: 5.5rem;
}
#market-pools-page :deep(th.col-status),
#market-pools-page :deep(td.col-status),
#market-pools-page :deep(th.col-sub2api),
#market-pools-page :deep(td.col-sub2api) {
  min-width: 5rem;
}
#market-pools-page :deep(th.col-ops),
#market-pools-page :deep(td.col-ops) {
  min-width: 14rem;
  width: 14rem;
  box-sizing: border-box;
}
#market-pools-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-pools-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-pools-page :deep(tbody tr:hover td.col-ops) {
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
  width: min(520px, 100%);
  max-height: min(90vh, 720px);
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
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.y-sep {
  display: grid;
  gap: 10px;
  padding-top: 8px;
  border-top: 1px solid rgb(0 0 0 / 6%);
}
.y-hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.y-textarea {
  height: auto !important;
  min-height: 88px;
  padding: 8px 10px;
  resize: vertical;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
  .y-field-row--3 {
    grid-template-columns: 1fr;
  }
}
</style>
