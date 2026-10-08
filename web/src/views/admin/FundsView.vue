<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { admin } from '@/api'
import type { RechargeOrder, Refund, Withdrawal } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import TabBar from '@/components/TabBar.vue'
import { dateTime, money } from '@/utils/format'

const tab = ref('recharges')
const rStatus = ref('pending')
const fStatus = ref('paying')
const wStatus = ref('pending_review')
const recharges = useLoader(() => admin.recharges(rStatus.value), [] as RechargeOrder[] | null)
const refunds = useLoader(() => admin.refunds(fStatus.value), [] as Refund[] | null)
const withdrawals = useLoader(() => admin.withdrawals(wStatus.value), [] as Withdrawal[] | null)
const { busy, run } = useAction()

function loadAll() {
  recharges.load()
  refunds.load()
  withdrawals.load()
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
async function refundPaid(r: Refund) {
  const ref = prompt(`退款 #${r.id}（${money(r.amount)}）已打款，填写打款流水号：`)
  if (ref === null) return
  if (await run(() => admin.refundPaid(r.id, ref), '已完成')) refunds.load()
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
    <PageHeader title="充值、退款、提现" description="第一期为人工通道：充值由管理员确认到账，退款与提现由管理员线下打款后登记。" />
    <TabBar
      v-model="tab"
      :tabs="[
        { key: 'recharges', text: '充值' },
        { key: 'refunds', text: '退款' },
        { key: 'withdrawals', text: '提现' },
      ]"
    />

    <div v-if="tab === 'recharges'" class="card">
      <select v-model="rStatus" class="input mb-3 w-36" aria-label="状态">
        <option value="pending">待支付</option>
        <option value="crediting">到账中</option>
        <option value="credited">已到账</option>
        <option value="failed">失败</option>
        <option value="">全部</option>
      </select>
      <DataTable
        :columns="[
          { key: 'id', label: '订单' },
          { key: 'user_id', label: '用户' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'status', label: '状态' },
          { key: 'provider_ref', label: '流水号' },
          { key: 'created_at', label: '创建时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="recharges.data.value"
        :loading="recharges.loading.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="rechargeStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'pending'" class="btn-primary btn-sm" :disabled="busy" @click="confirmRecharge(row)">确认到账</button>
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
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'paying'" class="btn-primary btn-sm" :disabled="busy" @click="refundPaid(row)">登记已打款</button>
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
  </div>
</template>
