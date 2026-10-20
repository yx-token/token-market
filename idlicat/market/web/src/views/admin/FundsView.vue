<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { admin } from '@/api'
import type { RechargeOrder, Refund, RefundPayout, Withdrawal } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import RechargeRefundDialog from '@/components/RechargeRefundDialog.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import TabBar from '@/components/TabBar.vue'
import { label } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const tab = ref('recharges')
const defaultStatus: Record<string, string> = {
  recharges: 'pending',
  refunds: 'paying',
  withdrawals: 'pending_review',
}
const pageMeta: Record<string, { title: string; description: string }> = {
  recharges: {
    title: '充值',
    description: '在线充值（支付宝、微信支付）由渠道通知与主动查单自动到账；商户与支付方式在「支付配置」中设置。',
  },
  refunds: {
    title: '退款',
    description: '买家退全部余额或按充值单退款，优先原路退回，其余部分由管理员线下打款后登记。按单退款可在「充值」中代买家发起。',
  },
  withdrawals: {
    title: '提现',
    description: '卖家提现由管理员审核并线下打款后登记。',
  },
}
const pageTitle = computed(() => pageMeta[tab.value]?.title ?? '资金')
const pageDescription = computed(() => pageMeta[tab.value]?.description ?? '')

const { queryParams, handleQuery } = useListQuery({
  keyword: '',
  status: defaultStatus.recharges,
  startDate: '',
  endDate: '',
})

const recharges = useLoader(() => admin.recharges(queryParams.status), [] as RechargeOrder[] | null)
const refunds = useLoader(() => admin.refunds(queryParams.status), [] as Refund[] | null)
const withdrawals = useLoader(() => admin.withdrawals(queryParams.status), [] as Withdrawal[] | null)
const { busy, run } = useAction()

function loadCurrent() {
  if (tab.value === 'recharges') recharges.load()
  else if (tab.value === 'refunds') refunds.load()
  else withdrawals.load()
}
onMounted(loadCurrent)

watch(tab, (t) => {
  queryParams.status = defaultStatus[t] ?? ''
  queryParams.keyword = ''
  queryParams.startDate = ''
  queryParams.endDate = ''
  resetAllPages()
  loadCurrent()
})

const filteredRecharges = computed(() => {
  let rows = recharges.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'user_id', 'channel', 'out_trade_no', 'provider_ref'] as (keyof RechargeOrder)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const filteredRefunds = computed(() => {
  let rows = refunds.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'user_id', 'recharge_order_id', 'note', 'status'] as (keyof Refund)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const filteredWithdrawals = computed(() => {
  let rows = withdrawals.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'seller_id', 'account_info', 'channel', 'review_reason'] as (keyof Withdrawal)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})

const {
  pageNum: rechargePageNum,
  pageSize: rechargePageSize,
  total: rechargeTotal,
  pageRows: rechargeRows,
  resetPage: resetRechargePage,
} = useClientPage(filteredRecharges)
const {
  pageNum: refundPageNum,
  pageSize: refundPageSize,
  total: refundTotal,
  pageRows: refundRows,
  resetPage: resetRefundPage,
} = useClientPage(filteredRefunds)
const {
  pageNum: withdrawalPageNum,
  pageSize: withdrawalPageSize,
  total: withdrawalTotal,
  pageRows: withdrawalRows,
  resetPage: resetWithdrawalPage,
} = useClientPage(filteredWithdrawals)

function resetAllPages() {
  resetRechargePage()
  resetRefundPage()
  resetWithdrawalPage()
}

const pageNum = computed({
  get: () => (tab.value === 'recharges' ? rechargePageNum.value : tab.value === 'refunds' ? refundPageNum.value : withdrawalPageNum.value),
  set: (n: number) => {
    if (tab.value === 'recharges') rechargePageNum.value = n
    else if (tab.value === 'refunds') refundPageNum.value = n
    else withdrawalPageNum.value = n
  },
})
const pageSize = computed(() =>
  tab.value === 'recharges' ? rechargePageSize.value : tab.value === 'refunds' ? refundPageSize.value : withdrawalPageSize.value,
)
const total = computed(() =>
  tab.value === 'recharges' ? rechargeTotal.value : tab.value === 'refunds' ? refundTotal.value : withdrawalTotal.value,
)

const loading = computed(() => {
  if (tab.value === 'recharges') return recharges.loading.value
  if (tab.value === 'refunds') return refunds.loading.value
  return withdrawals.loading.value
})

function search() {
  handleQuery()
  resetAllPages()
  loadCurrent()
}
function onReset() {
  queryParams.keyword = ''
  queryParams.startDate = ''
  queryParams.endDate = ''
  queryParams.status = defaultStatus[tab.value] ?? ''
  resetAllPages()
  loadCurrent()
}

async function confirmRecharge(o: RechargeOrder) {
  const ref = prompt(`确认订单 #${o.id}（${money(o.amount)}）已到账，填写付款流水号：`)
  if (ref === null) return
  if (await run(() => admin.confirmRecharge(o.id, ref), '已确认，余额正在到账')) recharges.load()
}
async function syncRecharge(o: RechargeOrder) {
  if (await run(() => admin.syncRecharge(o.id))) recharges.load()
}
const showOrderRefund = ref(false)
const refundOrder = ref<RechargeOrder | null>(null)
const orderRefundable = (o: RechargeOrder) => o.status === 'credited' && o.amount - o.refunded_amount >= 0.01
function openOrderRefund(o: RechargeOrder) {
  refundOrder.value = o
  showOrderRefund.value = true
}
const manualPart = (r: Refund) => (r.payouts ?? []).filter((p) => p.status === 'manual').reduce((s, p) => s + p.amount, 0)
const needsManual = (r: Refund) => r.status === 'paying' && (!(r.payouts ?? []).length || manualPart(r) > 0)
async function refundPaid(r: Refund) {
  const amount = (r.payouts ?? []).length ? manualPart(r) : (r.amount ?? 0)
  const ref = prompt(`退款 #${r.id} 需人工打款 ${money(amount)}，打款后填写流水号：`)
  if (ref === null) return
  if (await run(() => admin.refundPaid(r.id, ref), '已登记')) refunds.load()
}
async function retryPayout(p: RefundPayout) {
  if (await run(() => admin.retryPayout(p.id), '已重新发起原路退款')) refunds.load()
}
async function toManual(p: RefundPayout) {
  if (!confirm(`把这笔 ${money(p.amount)} 的原路退款改为人工打款？`)) return
  if (await run(() => admin.payoutToManual(p.id), '已改为人工打款')) refunds.load()
}
async function review(w: Withdrawal, approve: boolean) {
  const reason = approve ? '' : prompt('驳回原因')
  if (!approve && reason === null) return
  if (await run(() => admin.reviewWithdrawal(w.id, approve, reason ?? ''), approve ? '已通过' : '已驳回，金额退回可提现')) withdrawals.load()
}
async function paid(w: Withdrawal, failed: boolean) {
  if (failed) {
    const reason = prompt('打款失败原因')
    if (reason === null) return
    if (await run(() => admin.withdrawalPaid(w.id, { failed: true, reason }), '已标记失败，金额退回可提现')) withdrawals.load()
    return
  }
  const ref = prompt(`提现 #${w.id}（${money(w.amount)}）已打款到 ${w.account_info ?? ''}，填写打款流水号：`)
  if (ref === null) return
  if (await run(() => admin.withdrawalPaid(w.id, { payout_ref: ref }), '已打款')) withdrawals.load()
}
</script>

<template>
  <ConsoleListPage
    id="console-funds-page"
    :title="pageTitle"
    :description="pageDescription"
  >
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'recharges', text: '充值' },
          { key: 'refunds', text: '退款' },
          { key: 'withdrawals', text: '提现' },
        ]"
      />
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input
            v-model="queryParams.keyword"
            class="filter-input w-full"
            :placeholder="tab === 'withdrawals' ? '编号 / 卖家 / 账号' : '编号 / 用户 / 单号'"
            @keyup.enter="search"
          />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <template v-if="tab === 'recharges'">
            <option value="pending">待支付</option>
            <option value="crediting">到账中</option>
            <option value="credited">已到账</option>
            <option value="failed">失败</option>
            <option value="expired">已过期</option>
            <option value="cancelled">已取消</option>
            <option value="">全部</option>
          </template>
          <template v-else-if="tab === 'refunds'">
            <option value="paying">待打款</option>
            <option value="locking">暂停 Key 中</option>
            <option value="subtracting">扣减余额中</option>
            <option value="done">已完成</option>
            <option value="failed">失败</option>
            <option value="">全部</option>
          </template>
          <template v-else>
            <option value="pending_review">待审核</option>
            <option value="approved">待打款</option>
            <option value="paid">已打款</option>
            <option value="rejected">已驳回</option>
            <option value="failed">打款失败</option>
            <option value="">全部</option>
          </template>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">时间</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-item" aria-hidden="true" />
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      v-if="tab === 'recharges'"
      :columns="[
        { key: 'id', label: '订单' },
        { key: 'user_id', label: '用户' },
        { key: 'channel', label: '支付方式' },
        { key: 'amount', label: '金额', align: 'right' },
        { key: 'status', label: '状态' },
        { key: 'provider_ref', label: '商户单号 / 渠道流水' },
        { key: 'created_at', label: '创建时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="rechargeRows"
      :loading="loading"
      row-key="id"
      empty="没有符合条件的充值订单"
    >
      <template #cell-id="{ row }">#{{ row.id }}</template>
      <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
      <template #cell-channel="{ row }">{{ label('paymentChannel', row.channel).text }}</template>
      <template #cell-amount="{ row }">
        {{ money(row.amount) }}
        <span v-if="row.refunded_amount > 0" class="block text-xs text-slate-400">已退 {{ money(row.refunded_amount) }}</span>
      </template>
      <template #cell-status="{ row }">
        <StatusBadge kind="rechargeStatus" :value="row.status" />
        <span v-if="row.close_reason" class="block text-xs text-slate-400">{{ label('closeReason', row.close_reason).text }}</span>
      </template>
      <template #cell-provider_ref="{ row }">
        <span class="text-xs">{{ row.out_trade_no ?? '—' }}</span>
        <span v-if="row.provider_ref" class="block text-xs text-slate-400">{{ row.provider_ref }}</span>
      </template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <button v-if="row.status === 'pending' && row.channel === 'manual'" class="btn-primary btn-sm" :disabled="busy" @click="confirmRecharge(row)">
          确认到账
        </button>
        <button v-if="row.status === 'pending' && row.channel !== 'manual'" class="btn-secondary btn-sm" :disabled="busy" @click="syncRecharge(row)">
          向渠道查单
        </button>
        <button v-if="orderRefundable(row)" class="btn-ghost btn-sm" :disabled="busy" @click="openOrderRefund(row)">退款</button>
      </template>
    </DataTable>

    <DataTable
      v-else-if="tab === 'refunds'"
      :columns="[
        { key: 'id', label: '编号' },
        { key: 'user_id', label: '用户' },
        { key: 'kind', label: '类型' },
        { key: 'amount', label: '金额', align: 'right' },
        { key: 'status', label: '状态' },
        { key: 'payouts', label: '打款明细' },
        { key: 'note', label: '备注' },
        { key: 'created_at', label: '申请时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="refundRows"
      :loading="loading"
      row-key="id"
      empty="没有符合条件的退款"
    >
      <template #cell-id="{ row }">#{{ row.id }}</template>
      <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
      <template #cell-kind="{ row }">
        {{ label('refundKind', row.kind).text }}
        <span v-if="row.recharge_order_id" class="block text-xs text-slate-400">订单 #{{ row.recharge_order_id }}</span>
      </template>
      <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
      <template #cell-status="{ row }"><StatusBadge kind="refundStatus" :value="row.status" /></template>
      <template #cell-payouts="{ row }">
        <ul v-if="row.payouts?.length" class="space-y-1 text-xs">
          <li v-for="p in row.payouts" :key="p.id" class="flex flex-wrap items-center gap-1">
            <span>{{ p.channel === 'manual' ? '人工打款' : `原路·${label('paymentChannel', p.channel).text}` }}</span>
            <span>{{ money(p.amount) }}</span>
            <StatusBadge kind="payoutStatus" :value="p.status" />
            <span v-if="p.recharge_order_id" class="text-slate-400">订单 #{{ p.recharge_order_id }}</span>
            <span v-if="p.error" class="text-red-600" :title="p.error">{{ p.error.slice(0, 40) }}</span>
            <template v-if="p.status === 'failed'">
              <button v-if="p.recharge_order_id" class="btn-ghost btn-sm" :disabled="busy" @click="retryPayout(p)">重试</button>
              <button class="btn-ghost btn-sm" :disabled="busy" @click="toManual(p)">改人工</button>
            </template>
          </li>
        </ul>
        <span v-else class="text-slate-400">—</span>
      </template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <button v-if="needsManual(row)" class="btn-primary btn-sm" :disabled="busy" @click="refundPaid(row)">登记人工打款</button>
      </template>
    </DataTable>

    <DataTable
      v-else
      :columns="[
        { key: 'id', label: '编号' },
        { key: 'seller_id', label: '卖家' },
        { key: 'amount', label: '金额', align: 'right' },
        { key: 'account', label: '收款账号' },
        { key: 'status', label: '状态' },
        { key: 'review_reason', label: '审核说明' },
        { key: 'created_at', label: '申请时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="withdrawalRows"
      :loading="loading"
      row-key="id"
      empty="没有符合条件的提现"
    >
      <template #cell-id="{ row }">#{{ row.id }}</template>
      <template #cell-seller_id="{ row }">#{{ row.seller_id }}</template>
      <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
      <template #cell-account="{ row }">{{ row.channel }}：{{ row.account_info }}</template>
      <template #cell-status="{ row }"><StatusBadge kind="withdrawalStatus" :value="row.status" /></template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <template v-if="row.status === 'pending_review'">
          <button class="btn-primary btn-sm" :disabled="busy" @click="review(row, true)">通过</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="review(row, false)">驳回</button>
        </template>
        <template v-if="row.status === 'approved'">
          <button class="btn-primary btn-sm" :disabled="busy" @click="paid(row, false)">登记已打款</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="paid(row, true)">打款失败</button>
        </template>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <RechargeRefundDialog v-model:open="showOrderRefund" :order="refundOrder" as-admin @done="recharges.load()" />
</template>
