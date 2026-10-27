<script setup lang="ts">
/**
 * 中间调度：融合账号 / 融合分组
 * 对齐挂单/上游 TablePageLayout + filter-grid + DataTable 规范。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { sellerAccount, sellerFilterLabel } from './sellerDisplay'
import { useConsoleTabs } from '@/stores/consoleTabs'

defineOptions({ name: 'MarketAdminFusedSupply' })

const props = defineProps<{ kind: 'accounts' | 'groups' }>()
const { t } = useI18n()
const { open: openTab } = useConsoleTabs()

interface FusedAccount {
  upstream_id?: number
  upstream_code?: string
  upstream_name?: string
  account_email?: string
  seller_id?: number
  seller_email?: string
  platform: string
  market_status: string
  health?: string
  grade?: string | null
  engine_account_id?: number | null
  engine_name?: string
  engine_status?: string
  schedulable?: boolean | null
  rate_multiplier?: number | null
  concurrency?: number | null
  linked: boolean
  link_state: string
}

interface FusedGroup {
  pool_id?: number
  name: string
  platform: string
  enabled?: boolean | null
  sell_multiplier?: number | null
  engine_group_id?: number | null
  engine_name?: string
  engine_rate?: number | null
  market_status: string
  linked: boolean
  link_state: string
}

type FusedRow = FusedAccount & FusedGroup

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
  { value: 'composite', label: '综合池' },
]

const LINK_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'linked', label: '已关联' },
  { value: 'unbound', label: '仅市场' },
  { value: 'orphan', label: '失效绑定' },
  { value: 'engine_only', label: '仅引擎' },
]

const SCHED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '可调度' },
  { value: 'false', label: '不可调度' },
]

const ENABLED_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'true', label: '已启用' },
  { value: 'false', label: '已停用' },
]

const HEALTH_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'healthy', label: '健康' },
  { value: 'limited', label: '受限' },
  { value: 'suspected', label: '可疑' },
  { value: 'degraded', label: '降智' },
  { value: 'unavailable', label: '不可用' },
  { value: 'unknown', label: '未知' },
]

const MARKET_STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'ready', label: '就绪' },
  { value: 'listed', label: '已挂单' },
  { value: 'selecting_models', label: '待选模型' },
  { value: 'verifying', label: '检测中' },
  { value: 'suspended', label: '已冻结' },
  { value: 'verify_failed', label: '检测失败' },
  { value: 'draft', label: '草稿' },
  { value: 'authorizing', label: '授权中' },
  { value: 'failed', label: '失败' },
  { value: '仅引擎', label: '仅引擎' },
  { value: '市场分组', label: '市场分组' },
  { value: '未同步', label: '未同步' },
]

const STATUS_LABEL: Record<string, string> = Object.fromEntries(
  MARKET_STATUS_OPTIONS.filter((o) => o.value).map((o) => [o.value, o.label]),
)

const LINK_LABEL: Record<string, string> = {
  linked: '已关联',
  unbound: '仅市场',
  orphan: '失效绑定',
  engine_only: '仅引擎',
}

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  platform: '',
  link_state: '',
  market_status: '',
  health: '',
  schedulable: '',
  enabled: '',
  seller_id: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const rows = ref<FusedRow[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const isAccounts = computed(() => props.kind === 'accounts')
const title = computed(() =>
  isAccounts.value ? t('nav.marketSupplyAccounts') : t('nav.marketSupplyGroups'),
)
const hint = computed(() =>
  isAccounts.value ? t('nav.marketSupplyAccountsHint') : t('nav.marketSupplyGroupsHint'),
)
const path = computed(() =>
  isAccounts.value ? `${MARKET_API_PREFIX}/supply/accounts` : `${MARKET_API_PREFIX}/supply/groups`,
)

const accountColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'market', label: '名称', class: 'w-44' },
  { key: 'code', label: '编号', class: 'w-44' },
  { key: 'seller', label: '卖家', class: 'w-48' },
  { key: 'platform', label: '厂商', class: 'w-28' },
  { key: 'market_status', label: '市场状态', class: 'w-28' },
  { key: 'health', label: '健康', class: 'w-24' },
  { key: 'engine', label: '引擎账号', class: 'w-48' },
  { key: 'engine_status', label: '引擎状态', class: 'w-28' },
  { key: 'schedulable', label: '可调度', class: 'w-24' },
  { key: 'rate_multiplier', label: '引擎倍率', class: 'w-24' },
  { key: 'link_state', label: '关联', class: 'w-28' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const groupColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'market', label: '保障池', class: 'w-48' },
  { key: 'platform', label: '厂商', class: 'w-28' },
  { key: 'enabled', label: '启用', class: 'w-20' },
  { key: 'sell_multiplier', label: '池售价倍率', class: 'w-28' },
  { key: 'engine', label: '引擎分组', class: 'w-48' },
  { key: 'engine_rate', label: '引擎倍率', class: 'w-24' },
  { key: 'market_status', label: '市场状态', class: 'w-28' },
  { key: 'link_state', label: '关联', class: 'w-28' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const columns = computed(() => (isAccounts.value ? accountColumns : groupColumns))

const sellerFilterOptions = computed(() => {
  const map = new Map<string, string>()
  for (const r of rows.value) {
    const sid = String(r.seller_id || '').trim()
    if (!sid || map.has(sid)) continue
    map.set(sid, sellerFilterLabel(r.seller_email, r.seller_id))
  }
  return [
    { value: '', label: '全部' },
    ...[...map.entries()]
      .sort((a, b) => Number(a[0]) - Number(b[0]))
      .map(([value, label]) => ({ value, label })),
  ]
})

const allStats = computed(() => {
  const all = rows.value
  const linked = all.filter((r) => r.link_state === 'linked').length
  const orphan = all.filter((r) => r.link_state === 'orphan').length
  const unbound = all.filter((r) => r.link_state === 'unbound').length
  const engineOnly = all.filter((r) => r.link_state === 'engine_only').length
  return { total: all.length, linked, orphan, unbound, engineOnly }
})

const supplyStatus = computed(() => {
  if (allStats.value.orphan > 0) return { text: '有失效绑定', tone: 'warn' as const }
  if (allStats.value.unbound > 0 || allStats.value.engineOnly > 0) {
    return { text: '部分未对齐', tone: 'muted' as const }
  }
  if (allStats.value.total === 0) return { text: '暂无数据', tone: 'muted' as const }
  return { text: '对齐正常', tone: 'ok' as const }
})

const filtered = computed(() => {
  const q = queryParams.value
  let list = rows.value
  if (q.platform) list = list.filter((r) => r.platform === q.platform)
  if (q.link_state) list = list.filter((r) => r.link_state === q.link_state)
  if (q.market_status) list = list.filter((r) => r.market_status === q.market_status)
  if (isAccounts.value) {
    if (q.health) list = list.filter((r) => r.health === q.health)
    if (q.schedulable === 'true') list = list.filter((r) => r.schedulable === true)
    if (q.schedulable === 'false') list = list.filter((r) => r.schedulable === false)
    if (q.seller_id) list = list.filter((r) => String(r.seller_id || '') === q.seller_id)
  } else {
    if (q.enabled === 'true') list = list.filter((r) => r.enabled === true)
    if (q.enabled === 'false') list = list.filter((r) => r.enabled === false)
  }
  const kw = q.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((r) =>
      [
        r.upstream_id,
        r.upstream_code,
        r.upstream_name,
        r.account_email,
        r.seller_id,
        r.seller_email,
        r.engine_account_id,
        r.engine_name,
        r.pool_id,
        r.name,
        r.engine_group_id,
        r.platform,
        r.market_status,
        r.engine_status,
      ]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  queryParams.value = { ...initQueryParams }
}

function getSerialNumber(index: number) {
  return (queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1
}

function platformLabel(v?: string) {
  return PLATFORM_OPTIONS.find((o) => o.value === v)?.label || v || '—'
}

function statusLabel(v?: string) {
  return (v && STATUS_LABEL[v]) || v || '—'
}

function linkLabel(v?: string) {
  return (v && LINK_LABEL[v]) || v || '—'
}

function linkTone(v?: string) {
  if (v === 'linked') return 'ok'
  if (v === 'orphan') return 'danger'
  if (v === 'engine_only') return 'warn'
  return 'muted'
}

function healthLabel(v?: string) {
  return HEALTH_OPTIONS.find((o) => o.value === v)?.label || v || '—'
}

function boolLabel(v: boolean | null | undefined) {
  if (v == null) return '—'
  return v ? t('common.yes') : t('common.no')
}

function fmtRate(v: number | null | undefined) {
  if (v == null || !Number.isFinite(v)) return '—'
  return Number(v).toFixed(3)
}

function marketPrimary(row: FusedRow) {
  if (isAccounts.value) {
    const name = String(row.upstream_name || '').trim()
    if (name) return name
    const account = String(row.account_email || '').trim()
    if (account) return account
    const seller = String(row.seller_email || '').trim()
    if (seller) return seller
    const engine = String(row.engine_name || '').trim()
    if (engine) return engine
    return row.upstream_id ? '未命名上游' : '—'
  }
  return row.name || row.engine_name || '未命名分组'
}

function enginePrimary(row: FusedRow) {
  if (isAccounts.value) {
    return row.engine_name || (row.engine_account_id ? '引擎账号' : '—')
  }
  return row.engine_name || (row.engine_group_id ? '引擎分组' : '—')
}

function engineSub(row: FusedRow) {
  // 副行只给状态弱提示，不展示裸数字编号
  return String(row.engine_status || '').trim()
}

async function load() {
  loading.value = true
  error.value = ''
  msg.value = ''
  try {
    const data = await marketGet<unknown>(path.value)
    rows.value = Array.isArray(data) ? (data as FusedRow[]) : []
  } catch (e) {
    error.value = e instanceof Error ? e.message : t('common.loadFailed')
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function run(okMsg: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = okMsg
    await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openUpstream(row: FusedRow) {
  if (!row.upstream_id) return
  const p = `/admin/market/upstreams/${row.upstream_id}`
  openTab(p, { title: marketPrimary(row) })
}

function openEngineAccount(row: FusedRow) {
  if (!row.engine_account_id) return
  openTab('/admin/accounts', { title: '引擎账号' })
}

function openPool(row: FusedRow) {
  if (!row.pool_id) return
  openTab('/admin/market/pools', { title: '保障池' })
}

function openEngineGroup(row: FusedRow) {
  if (!row.engine_group_id) return
  openTab('/admin/groups', { title: '引擎分组' })
}

async function clearDead(row: FusedRow) {
  if (!row.upstream_id) return
  if (!window.confirm(`确认清除上游 #${row.upstream_id} 的失效引擎绑定？清除后需重新授权。`)) return
  await run('已清除失效引擎账号', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${row.upstream_id}/clear-dead-account`),
  )
}

async function syncPool(row: FusedRow) {
  if (!row.pool_id) return
  await run(`已同步分组 ${row.name || row.pool_id}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/pools/${row.pool_id}/sync`),
  )
}

async function togglePoolEnabled(row: FusedRow) {
  if (!row.pool_id || row.enabled == null) return
  const next = !row.enabled
  await run(next ? '已启用' : '已停用', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/pools/${row.pool_id}`, { enabled: next }),
  )
}

function canClearDead(row: FusedRow) {
  return isAccounts.value && !!row.upstream_id && row.link_state === 'orphan'
}

function canSyncPool(row: FusedRow) {
  return !isAccounts.value && !!row.pool_id && row.link_state !== 'engine_only'
}

onMounted(load)
watch(
  () => props.kind,
  () => {
    resetQuery()
    load()
  },
)
</script>

<template>
  <section id="market-fused-supply-page" class="fused-shell">
    <TablePageLayout>
      <template #title>
        <div>
          <h2 class="y-title">{{ title }}</h2>
          <p class="y-page-desc">{{ hint }}</p>
        </div>
      </template>

      <template #actions>
        <button
          type="button"
          class="btn btn-secondary page-action-btn"
          :disabled="loading"
          @click="load"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>

        <div class="section-block">
          <div class="section-title">对照概览</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">已关联</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.linked }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">失效绑定</div>
                <div class="stat-card__value">{{ allStats.orphan.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">仅市场</div>
                <div class="stat-card__value stat-card__value--sm">{{ allStats.unbound }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">仅引擎</div>
                <div class="stat-card__value">{{ allStats.engineOnly.toLocaleString() }}</div>
              </div>
              <div>
                <div class="stat-card__label">对齐状态</div>
                <div class="stat-card__badge" :data-tone="supplyStatus.tone">{{ supplyStatus.text }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">{{ isAccounts ? '账号筛选' : '分组筛选' }}</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">厂商</label>
              <Select
                v-model="queryParams.platform"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PLATFORM_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">关联</label>
              <Select
                v-model="queryParams.link_state"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="LINK_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">市场状态</label>
              <Select
                v-model="queryParams.market_status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="MARKET_STATUS_OPTIONS"
              />
            </div>
            <div v-show="isAccounts" class="filter-item">
              <label class="filter-label">健康</label>
              <Select
                v-model="queryParams.health"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="HEALTH_OPTIONS"
                :disabled="!isAccounts"
              />
            </div>
            <div v-show="isAccounts" class="filter-item">
              <label class="filter-label">可调度</label>
              <Select
                v-model="queryParams.schedulable"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="SCHED_OPTIONS"
                :disabled="!isAccounts"
              />
            </div>
            <div v-show="isAccounts" class="filter-item">
              <label class="filter-label">卖家</label>
              <Select
                v-model="queryParams.seller_id"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="sellerFilterOptions"
                :disabled="!isAccounts"
              />
            </div>
            <div v-show="!isAccounts" class="filter-item">
              <label class="filter-label">启用</label>
              <Select
                v-model="queryParams.enabled"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="ENABLED_OPTIONS"
                :disabled="isAccounts"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  :placeholder="isAccounts ? '上游编号 / 引擎名 / 邮箱' : '池名 / 引擎分组'"
                  @keyup.enter="handleQuery"
                />
              </div>
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
            {{ getSerialNumber(pageRows.findIndex((r) => r === row)) }}
          </template>
          <template #cell-market="{ row }">
            <div>{{ marketPrimary(row) }}</div>
            <div v-if="isAccounts && row.account_email && row.upstream_name" class="row-sub">
              {{ row.account_email }}
            </div>
          </template>
          <template #cell-code="{ row }">
            {{ String(row.upstream_code || '').trim() || '—' }}
          </template>
          <template #cell-seller="{ row }">
            {{ sellerAccount(row.seller_email, row.seller_id) }}
          </template>
          <template #cell-platform="{ row }">{{ platformLabel(row.platform) }}</template>
          <template #cell-market_status="{ row }">{{ statusLabel(row.market_status) }}</template>
          <template #cell-health="{ row }">{{ healthLabel(row.health) }}</template>
          <template #cell-engine="{ row }">
            <div>{{ enginePrimary(row) }}</div>
            <div v-if="engineSub(row)" class="row-sub">{{ engineSub(row) }}</div>
          </template>
          <template #cell-engine_status="{ row }">{{ row.engine_status || '—' }}</template>
          <template #cell-schedulable="{ row }">{{ boolLabel(row.schedulable) }}</template>
          <template #cell-rate_multiplier="{ row }">{{ fmtRate(row.rate_multiplier) }}</template>
          <template #cell-enabled="{ row }">{{ boolLabel(row.enabled) }}</template>
          <template #cell-sell_multiplier="{ row }">{{ fmtRate(row.sell_multiplier) }}</template>
          <template #cell-engine_rate="{ row }">{{ fmtRate(row.engine_rate) }}</template>
          <template #cell-link_state="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="linkTone(row.link_state)" />
              {{ linkLabel(row.link_state) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="isAccounts">
                <button
                  v-if="row.upstream_id"
                  type="button"
                  class="y-link"
                  @click="openUpstream(row)"
                >
                  上游
                </button>
                <button
                  v-if="row.engine_account_id"
                  type="button"
                  class="y-link"
                  @click="openEngineAccount(row)"
                >
                  引擎账号
                </button>
                <button
                  v-if="canClearDead(row)"
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="clearDead(row)"
                >
                  清除绑定
                </button>
              </template>
              <template v-else>
                <button v-if="row.pool_id" type="button" class="y-link" @click="openPool(row)">
                  保障池
                </button>
                <button
                  v-if="row.engine_group_id"
                  type="button"
                  class="y-link"
                  @click="openEngineGroup(row)"
                >
                  引擎分组
                </button>
                <button
                  v-if="canSyncPool(row)"
                  type="button"
                  class="y-link"
                  :disabled="busy"
                  @click="syncPool(row)"
                >
                  同步引擎
                </button>
                <button
                  v-if="row.pool_id && row.enabled != null"
                  type="button"
                  class="y-link"
                  :disabled="busy"
                  @click="togglePoolEnabled(row)"
                >
                  {{ row.enabled ? '停用' : '启用' }}
                </button>
              </template>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="isAccounts ? '暂无融合账号' : '暂无融合分组'"
              description="调整筛选条件，或先完成卖家上游 / 保障池接入"
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
  </section>
</template>

<style scoped>
#market-fused-supply-page.fused-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

#market-fused-supply-page :deep(.table-page-layout) {
  height: 100%;
  min-height: 0;
  gap: 12px;
}

#market-fused-supply-page :deep(.y-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 0;
  padding: 16px 20px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
}

.y-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: #0f172a;
}

.y-page-desc {
  margin: 4px 0 0;
  font-size: 13px;
  color: #64748b;
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

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

#market-fused-supply-page :deep(th.col-ops),
#market-fused-supply-page :deep(td.col-ops) {
  min-width: 14rem;
  width: 14rem;
  box-sizing: border-box;
}
#market-fused-supply-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-fused-supply-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-fused-supply-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
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
  background: linear-gradient(180deg, #eff6ff 0%, #f8fafc 100%);
}

.stat-card--orange {
  background: linear-gradient(180deg, #fff7ed 0%, #f8fafc 100%);
}

.stat-card--cyan {
  background: linear-gradient(180deg, #ecfeff 0%, #f8fafc 100%);
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 600;
  color: #0f172a;
  line-height: 1.2;
}

.stat-card__value--sm {
  font-size: 18px;
}

.stat-card__badge {
  margin-top: 6px;
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  background: rgba(100, 116, 139, 0.12);
  color: #475569;
}

.stat-card__badge[data-tone='ok'] {
  background: rgba(34, 197, 94, 0.12);
  color: #15803d;
}

.stat-card__badge[data-tone='warn'] {
  background: rgba(249, 115, 22, 0.14);
  color: #c2410c;
}

#market-fused-supply-page :deep(.filter-grid .select-trigger),
#market-fused-supply-page :deep(.filter-grid .select-trigger-sm),
#market-fused-supply-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-fused-supply-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-fused-supply-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-fused-supply-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
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

.status-dot[data-tone='warn'] {
  background: #fa8c16;
}

.status-dot[data-tone='danger'] {
  background: #f5222d;
}

.status-dot[data-tone='muted'] {
  background: #bfbfbf;
}

.y-msg--ok {
  color: #15803d;
}

@media (max-width: 1024px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
</style>
