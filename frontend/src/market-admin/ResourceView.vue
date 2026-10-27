<script setup lang="ts">
/**
 * 资金/申诉通用列表：TablePageLayout + filter-grid + DataTable；用户/卖家显示邮箱。
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
import { sellerAccount } from './sellerDisplay'

defineOptions({ name: 'MarketAdminResource' })

type Kind = 'probe-runs' | 'appeals' | 'recharges' | 'refunds' | 'withdrawals'

const props = defineProps<{ kind: Kind }>()
const { t } = useI18n()

const catalog = computed<
  Record<Kind, { title: string; path: string; pick?: (data: unknown) => unknown[] }>
>(() => ({
  'probe-runs': { title: t('nav.marketProbeRuns'), path: `${MARKET_API_PREFIX}/probe-runs` },
  appeals: { title: t('nav.marketAppeals'), path: `${MARKET_API_PREFIX}/appeals` },
  recharges: { title: t('nav.marketFundsRecharge'), path: `${MARKET_API_PREFIX}/recharges` },
  refunds: { title: t('nav.marketFundsRefund'), path: `${MARKET_API_PREFIX}/refunds` },
  withdrawals: { title: t('nav.marketFundsWithdraw'), path: `${MARKET_API_PREFIX}/withdrawals` },
}))

const rows = ref<Record<string, unknown>[]>([])
const error = ref('')
const loading = ref(false)
const actionMsg = ref('')
const busy = ref(false)

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
}
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const meta = computed(() => catalog.value[props.kind])

const fundsTabs = computed(() => [
  { kind: 'recharges' as const, to: '/admin/market/recharges', label: t('nav.marketRecharges') },
  { kind: 'refunds' as const, to: '/admin/market/refunds', label: t('nav.marketRefunds') },
  { kind: 'withdrawals' as const, to: '/admin/market/withdrawals', label: t('nav.marketWithdrawals') },
])

const showFundsTabs = computed(() =>
  ['recharges', 'refunds', 'withdrawals'].includes(props.kind),
)

const hasActions = computed(() =>
  ['appeals', 'recharges', 'refunds', 'withdrawals'].includes(props.kind),
)

const STATUS_LABEL: Record<string, string> = {
  pending: '待处理',
  pending_review: '待审核',
  paying: '打款中',
  paid: '已打款',
  confirmed: '已确认',
  approved: '已通过',
  rejected: '已拒绝',
  failed: '失败',
  open: '待处理',
  upheld: '维持',
  overturned: '推翻',
  retest: '复测',
  closed: '已关闭',
}

const columns = computed<Column[]>(() => {
  const base: Column[] = [{ key: 'serial', label: '序号', class: 'w-16' }]
  if (props.kind === 'recharges' || props.kind === 'refunds') {
    base.push(
      { key: 'id', label: '单号', class: 'col-id' },
      { key: 'status', label: '状态', class: 'col-status' },
      { key: 'amount', label: '金额', class: 'col-amount' },
      { key: 'user', label: '用户', class: 'col-user' },
      { key: 'channel', label: '渠道', class: 'col-channel' },
    )
  } else if (props.kind === 'withdrawals') {
    base.push(
      { key: 'id', label: '单号', class: 'col-id' },
      { key: 'status', label: '状态', class: 'col-status' },
      { key: 'amount', label: '金额', class: 'col-amount' },
      { key: 'seller', label: '卖家', class: 'col-user' },
      { key: 'channel', label: '渠道', class: 'col-channel' },
    )
  } else if (props.kind === 'appeals') {
    base.push(
      { key: 'id', label: '单号', class: 'col-id' },
      { key: 'status', label: '状态', class: 'col-status' },
      { key: 'report_id', label: '报告', class: 'col-id' },
      { key: 'seller', label: '卖家', class: 'col-user' },
    )
  } else {
    base.push(
      { key: 'id', label: 'ID', class: 'col-id' },
      { key: 'status', label: '状态', class: 'col-status' },
      { key: 'platform', label: '平台', class: 'col-channel' },
    )
  }
  if (hasActions.value) {
    base.push({ key: 'actions', label: '操作', class: 'col-ops' })
  }
  return base
})

const statusOptions = computed(() => {
  const set = new Set<string>()
  for (const row of rows.value) {
    const s = String(row.status ?? '').trim()
    if (s) set.add(s)
  }
  return [
    { value: '', label: '全部' },
    ...[...set].sort().map((value) => ({
      value,
      label: STATUS_LABEL[value] || value,
    })),
  ]
})

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.status) {
    list = list.filter((row) => String(row.status ?? '') === queryParams.value.status)
  }
  const keyword = queryParams.value.keyword.trim().toLowerCase()
  if (keyword) {
    list = list.filter((row) =>
      [
        row.id,
        row.status,
        row.amount,
        row.channel,
        row.user_email,
        row.user_id,
        row.seller_email,
        row.seller_id,
        row.report_id,
        row.platform,
        row.upstream_id,
      ]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(keyword)),
    )
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
  const pending = all.filter((r) =>
    ['pending', 'pending_review', 'paying', 'open', 'approved'].includes(String(r.status ?? '')),
  ).length
  return { total: all.length, pending, done: all.length - pending }
})

watch(
  () => [queryParams.value.keyword, queryParams.value.status, queryParams.value.pageSize],
  () => {
    queryParams.value.pageNum = 1
  },
)

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

function statusLabel(s: unknown) {
  const v = String(s ?? '')
  return STATUS_LABEL[v] || v || '—'
}

function statusTone(s: unknown) {
  const v = String(s ?? '')
  if (['paid', 'confirmed', 'approved', 'upheld', 'closed'].includes(v)) return 'ok'
  if (['rejected', 'failed', 'overturned', 'error'].includes(v)) return 'err'
  if (['pending', 'pending_review', 'paying', 'open', 'retest'].includes(v)) return 'progress'
  return 'idle'
}

function userLabel(row: Record<string, unknown>) {
  return sellerAccount(String(row.user_email ?? ''), row.user_id as number | string | null)
}

function sellerLabel(row: Record<string, unknown>) {
  return sellerAccount(String(row.seller_email ?? ''), row.seller_id as number | string | null)
}

function cellText(v: unknown) {
  if (v == null || v === '') return '—'
  if (typeof v === 'boolean') return v ? '是' : '否'
  return String(v)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const raw = await marketGet<unknown>(meta.value.path)
    const picked = meta.value.pick
      ? meta.value.pick(raw)
      : Array.isArray(raw)
        ? raw
        : raw == null
          ? []
          : [raw]
    rows.value = (picked as Record<string, unknown>[]) || []
  } catch (e) {
    error.value = e instanceof Error ? e.message : t('common.loadFailed')
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  actionMsg.value = ''
  error.value = ''
  try {
    await fn()
    actionMsg.value = label
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function confirmRecharge(row: Record<string, unknown>) {
  if (row.status !== 'pending') return
  if (!window.confirm(`确认充值单 ${row.id} 已到账？在线支付单只能由支付渠道确认。`)) return
  const providerRef = window.prompt('到账流水号', 'manual')
  if (providerRef == null || !providerRef.trim()) return
  return run(`充值 ${row.id} 已确认`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/recharges/${row.id}/confirm`, {
      provider_ref: providerRef.trim(),
    }),
  )
}

function syncRecharge(row: Record<string, unknown>) {
  if (row.status !== 'pending') return
  return run(`充值 ${row.id} 已同步查单`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/recharges/${row.id}/sync`),
  )
}

function markRefundPaid(row: Record<string, unknown>) {
  if (row.status !== 'paying') return
  const payoutRef = window.prompt(`退款 ${row.id} 的打款流水号`)
  if (payoutRef == null || !payoutRef.trim()) return
  return run(`退款 ${row.id} 已标记打款`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/refunds/${row.id}/paid`, {
      payout_ref: payoutRef.trim(),
    }),
  )
}

function reviewWithdrawal(row: Record<string, unknown>, approve: boolean) {
  if (row.status !== 'pending_review') return
  let reason = ''
  if (approve) {
    if (!window.confirm(`通过提现 ${row.id}？`)) return
  } else {
    const input = window.prompt(`拒绝提现 ${row.id} 的原因`)
    if (input == null || !input.trim()) return
    reason = input.trim()
  }
  return run(`提现 ${row.id} 已${approve ? '通过' : '拒绝'}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/withdrawals/${row.id}/review`, { approve, reason }),
  )
}

function markWithdrawalPaid(row: Record<string, unknown>, failed: boolean) {
  if (row.status !== 'approved' && row.status !== 'paying') return
  const payoutRef = failed ? '' : window.prompt(`提现 ${row.id} 的打款流水号`)
  if (!failed && (payoutRef == null || !payoutRef.trim())) return
  if (failed && !window.confirm(`标记提现 ${row.id} 打款失败？`)) return
  return run(`提现 ${row.id} 已${failed ? '标记失败' : '登记打款'}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/withdrawals/${row.id}/paid`, {
      failed,
      payout_ref: failed ? undefined : payoutRef!.trim(),
    }),
  )
}

function resolveAppeal(row: Record<string, unknown>, action: 'uphold' | 'overturn' | 'retest') {
  if (row.status !== 'open') return
  const label = action === 'uphold' ? '维持原判' : action === 'overturn' ? '推翻并恢复质量' : '安排复测'
  if (!window.confirm(`申诉 ${row.id}：${label}？`)) return
  const note = window.prompt('处理说明，可留空', '') ?? ''
  return run(`申诉 ${row.id}：${label}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/appeals/${row.id}/resolve`, { action, note }),
  )
}

onMounted(load)
watch(
  () => props.kind,
  () => {
    Object.assign(queryParams.value, { ...initQueryParams })
    load()
  },
)
</script>

<template>
  <section id="market-resource-page" class="resource-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ meta.title }}</h2>
          <p class="y-page-desc">
            {{
              showFundsTabs
                ? '充值 / 退款 / 提现审核与打款登记'
                : kind === 'appeals'
                  ? '卖家申诉处理：维持、推翻或安排复测'
                  : '资源列表'
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
        <p v-else-if="actionMsg" class="y-msg y-msg--ok mb-2">{{ actionMsg }}</p>

        <nav v-if="showFundsTabs" class="funds-tabs mb-3">
          <RouterLink
            v-for="tab in fundsTabs"
            :key="tab.kind"
            :to="tab.to"
            class="funds-tab"
            :class="{ 'is-on': kind === tab.kind }"
          >
            {{ tab.label }}
          </RouterLink>
        </nav>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">待处理</div>
                <div class="stat-card__value">{{ allStats.pending.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已完结</div>
                <div class="stat-card__value">{{ allStats.done.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">筛选</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="单号 / 邮箱 / 渠道"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="statusOptions"
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
          <template #cell-id="{ row }">{{ cellText(row.id) }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ statusLabel(row.status) }}
            </span>
          </template>
          <template #cell-amount="{ row }">{{ cellText(row.amount) }}</template>
          <template #cell-user="{ row }">{{ userLabel(row) }}</template>
          <template #cell-seller="{ row }">{{ sellerLabel(row) }}</template>
          <template #cell-channel="{ row }">{{ cellText(row.channel) }}</template>
          <template #cell-report_id="{ row }">{{ cellText(row.report_id) }}</template>
          <template #cell-platform="{ row }">{{ cellText(row.platform) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <template v-if="kind === 'recharges' && row.status === 'pending'">
                <button
                  v-if="row.channel === 'manual' || !row.channel"
                  type="button"
                  class="y-link"
                  :disabled="busy"
                  @click="confirmRecharge(row)"
                >
                  确认到账
                </button>
                <button v-else type="button" class="y-link" :disabled="busy" @click="syncRecharge(row)">
                  同步查单
                </button>
              </template>
              <button
                v-else-if="kind === 'refunds' && row.status === 'paying'"
                type="button"
                class="y-link"
                :disabled="busy"
                @click="markRefundPaid(row)"
              >
                标记已打款
              </button>
              <template v-else-if="kind === 'withdrawals' && row.status === 'pending_review'">
                <button type="button" class="y-link" :disabled="busy" @click="reviewWithdrawal(row, true)">
                  通过
                </button>
                <button
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="reviewWithdrawal(row, false)"
                >
                  拒绝
                </button>
              </template>
              <template
                v-else-if="kind === 'withdrawals' && (row.status === 'approved' || row.status === 'paying')"
              >
                <button type="button" class="y-link" :disabled="busy" @click="markWithdrawalPaid(row, false)">
                  登记已打款
                </button>
                <button
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="markWithdrawalPaid(row, true)"
                >
                  打款失败
                </button>
              </template>
              <template v-else-if="kind === 'appeals' && row.status === 'open'">
                <button type="button" class="y-link" :disabled="busy" @click="resolveAppeal(row, 'uphold')">
                  维持
                </button>
                <button
                  type="button"
                  class="y-link y-link--danger"
                  :disabled="busy"
                  @click="resolveAppeal(row, 'overturn')"
                >
                  推翻
                </button>
                <button type="button" class="y-link" :disabled="busy" @click="resolveAppeal(row, 'retest')">
                  复测
                </button>
              </template>
              <span v-else class="ops-idle">—</span>
            </div>
          </template>
          <template #empty>
            <EmptyState title="暂无数据" description="调整筛选条件后重试，或点击刷新重新加载" />
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
#market-resource-page.resource-shell {
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

.funds-tabs {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 4px;
  border-radius: 10px;
  background: #f1f5f9;
}

.funds-tab {
  padding: 6px 14px;
  border-radius: 8px;
  font-size: 13px;
  color: #64748b;
  text-decoration: none;
}

.funds-tab.is-on {
  background: #fff;
  color: #0f172a;
  font-weight: 600;
  box-shadow: 0 1px 2px rgb(15 23 42 / 8%);
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

.ops-idle {
  color: #94a3b8;
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

#market-resource-page :deep(.filter-grid .select-trigger),
#market-resource-page :deep(.filter-grid .select-trigger-sm),
#market-resource-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-resource-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-resource-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-resource-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-resource-page :deep(th.col-id),
#market-resource-page :deep(td.col-id) {
  min-width: 5rem;
}
#market-resource-page :deep(th.col-status),
#market-resource-page :deep(td.col-status) {
  min-width: 6rem;
}
#market-resource-page :deep(th.col-amount),
#market-resource-page :deep(td.col-amount) {
  min-width: 5rem;
}
#market-resource-page :deep(th.col-user),
#market-resource-page :deep(td.col-user) {
  min-width: 10rem;
}
#market-resource-page :deep(th.col-channel),
#market-resource-page :deep(td.col-channel) {
  min-width: 5rem;
}
#market-resource-page :deep(th.col-ops),
#market-resource-page :deep(td.col-ops) {
  min-width: 12rem;
  width: 12rem;
  box-sizing: border-box;
}
#market-resource-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-resource-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-resource-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
</style>
