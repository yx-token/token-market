<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { admin } from '@/api'
import type { PaymentStatus, RechargeOrder, Refund, RefundPayout, Withdrawal } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import TabBar from '@/components/TabBar.vue'
import { label } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const tab = ref('recharges')
const rStatus = ref('pending')
const fStatus = ref('paying')
const wStatus = ref('pending_review')
const recharges = useLoader(() => admin.recharges(rStatus.value), [] as RechargeOrder[] | null)
const refunds = useLoader(() => admin.refunds(fStatus.value), [] as Refund[] | null)
const withdrawals = useLoader(() => admin.withdrawals(wStatus.value), [] as Withdrawal[] | null)
const payments = useLoader(() => admin.paymentStatus(), null as PaymentStatus | null)
const { busy, run } = useAction()

function loadAll() {
  recharges.load()
  refunds.load()
  withdrawals.load()
  payments.load()
}
onMounted(loadAll)
watch(rStatus, () => recharges.load())
watch(fStatus, () => refunds.load())
watch(wStatus, () => withdrawals.load())

async function confirmRecharge(o: RechargeOrder) {
  const ref = prompt(`确认订单 #${o.id}（${money(o.amount)}）已到账，填写付款流水号：`)
  if (ref === null) return
  if (await run(() => admin.confirmRecharge(o.id, ref), '已确认，余额正在到账')) recharges.load()
}
async function syncRecharge(o: RechargeOrder) {
  const res = await run(() => admin.syncRecharge(o.id))
  if (res) {
    recharges.load()
    payments.load()
  }
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
  <div>
    <PageHeader
      title="充值、退款、提现"
      description="在线充值（支付宝、微信支付）由渠道通知与主动查单自动到账；余额退款优先原路退回，其余部分与提现由管理员线下打款后登记。"
    />
    <TabBar
      v-model="tab"
      :tabs="[
        { key: 'recharges', text: '充值' },
        { key: 'refunds', text: '退款' },
        { key: 'withdrawals', text: '提现' },
        { key: 'channels', text: '支付渠道' },
      ]"
    />

    <div v-if="tab === 'recharges'" class="card">
      <select v-model="rStatus" class="input mb-3 w-36" aria-label="状态">
        <option value="pending">待支付</option>
        <option value="crediting">到账中</option>
        <option value="credited">已到账</option>
        <option value="failed">失败</option>
        <option value="expired">已过期</option>
        <option value="cancelled">已取消</option>
        <option value="">全部</option>
      </select>
      <DataTable
        :columns="[
          { key: 'id', label: '订单' },
          { key: 'user_id', label: '用户' },
          { key: 'channel', label: '支付方式' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'status', label: '状态' },
          { key: 'provider_ref', label: '商户单号 / 渠道流水' },
          { key: 'created_at', label: '创建时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="recharges.data.value"
        :loading="recharges.loading.value"
        row-key="id"
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
        </template>
      </DataTable>
    </div>

    <div v-if="tab === 'refunds'" class="card">
      <select v-model="fStatus" class="input mb-3 w-36" aria-label="状态">
        <option value="paying">待打款</option>
        <option value="locking">暂停 Key 中</option>
        <option value="subtracting">扣减余额中</option>
        <option value="done">已完成</option>
        <option value="">全部</option>
      </select>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'user_id', label: '用户' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'status', label: '状态' },
          { key: 'payouts', label: '打款明细' },
          { key: 'note', label: '备注' },
          { key: 'created_at', label: '申请时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="refunds.data.value"
        :loading="refunds.loading.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
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
    </div>

    <div v-if="tab === 'withdrawals'" class="card">
      <select v-model="wStatus" class="input mb-3 w-36" aria-label="状态">
        <option value="pending_review">待审核</option>
        <option value="approved">待打款</option>
        <option value="paid">已打款</option>
        <option value="rejected">已驳回</option>
        <option value="failed">打款失败</option>
        <option value="">全部</option>
      </select>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'seller_id', label: '卖家' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'account', label: '收款账号' },
          { key: 'status', label: '状态' },
          { key: 'review_reason', label: '审核说明' },
          { key: 'created_at', label: '申请时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="withdrawals.data.value"
        :loading="withdrawals.loading.value"
        row-key="id"
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
    </div>

    <div v-if="tab === 'channels'" class="space-y-5">
      <div class="card">
        <h2 class="card-title">支付方式</h2>
        <p class="hint mb-3">
          在线渠道的商户号与密钥在配置文件（payment.alipay / payment.wechat）中配置，开关在"平台设置"中。支付通知地址：
          <code>{{ payments.data.value?.public_url || '（未配置 payment.public_url）' }}/api/payments/{alipay|wechat}/notify</code>
        </p>
        <DataTable
          :columns="[
            { key: 'channel', label: '支付方式' },
            { key: 'configured', label: '商户配置' },
            { key: 'enabled', label: '后台开关' },
            { key: 'available', label: '买家可用' },
          ]"
          :rows="payments.data.value?.channels"
          row-key="channel"
        >
          <template #cell-channel="{ row }">{{ label('paymentChannel', row.channel).text }}</template>
          <template #cell-configured="{ row }">{{ row.configured ? '已配置' : '未配置' }}</template>
          <template #cell-enabled="{ row }">{{ row.enabled ? '开启' : '关闭' }}</template>
          <template #cell-available="{ row }">
            <span :class="row.available ? 'text-emerald-600' : 'text-slate-400'">{{ row.available ? '可用' : '不可用' }}</span>
          </template>
        </DataTable>
      </div>
      <div class="card">
        <h2 class="card-title">最近的支付通知</h2>
        <DataTable
          :columns="[
            { key: 'created_at', label: '时间' },
            { key: 'provider', label: '渠道' },
            { key: 'out_trade_no', label: '商户单号' },
            { key: 'amount_fen', label: '金额', align: 'right' },
            { key: 'result', label: '处理结果' },
          ]"
          :rows="payments.data.value?.notifications"
          row-key="id"
        >
          <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
          <template #cell-provider="{ row }">{{ row.provider === 'alipay' ? '支付宝' : row.provider === 'wechat' ? '微信支付' : row.provider }}</template>
          <template #cell-amount_fen="{ row }">{{ row.amount_fen != null ? money(row.amount_fen / 100) : '—' }}</template>
          <template #cell-result="{ row }">
            <StatusBadge kind="notifyResult" :value="row.result" />
            <span v-if="row.error" class="block text-xs text-red-600">{{ row.error }}</span>
          </template>
        </DataTable>
      </div>
    </div>
  </div>
</template>
