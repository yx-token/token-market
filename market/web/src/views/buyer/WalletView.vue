<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { buyer } from '@/api'
import type { RechargeOrder, Wallet } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import PaymentDialog from '@/components/PaymentDialog.vue'
import { label } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const wallet = useLoader(() => buyer.wallet(), null as Wallet | null)

const showPay = ref(false)
const payOrder = ref<RechargeOrder | null>(null)

function reload() {
  wallet.load()
}

onMounted(async () => {
  reload()
  // 支付宝网页支付完成后跳回 /buyer/wallet?order=ID：展示该订单的结果
  const id = Number(route.query.order)
  if (id) {
    router.replace({ query: {} })
    const o = await buyer.getRecharge(id).catch(() => null)
    if (o) openOrder(o)
  }
})

function newRecharge() {
  payOrder.value = null
  showPay.value = true
}

function openOrder(o: RechargeOrder) {
  payOrder.value = o
  showPay.value = true
}
</script>

<template>
  <div>
    <PageHeader title="钱包" description="充值后可支付调用费用、购买店铺活动包或订阅套餐。活动包仅限对应店铺与适用模型；订阅套餐需余额 ≥ 套餐价。充值后不可退款，按实际调用扣费。">
      <button type="button" class="xm-btn xm-btn-primary" @click="newRecharge">充值</button>
    </PageHeader>

    <div class="xm-stats" style="grid-template-columns: repeat(2, 1fr)">
      <div class="xm-stat">
        <b>可调用余额</b>
        <strong>{{ money(wallet.data.value?.balance) }}</strong>
        <em>按成功调用扣费，不可退款</em>
      </div>
      <div class="xm-stat">
        <b>账面余额</b>
        <strong>{{ money(wallet.data.value?.ledger_cash) }}</strong>
        <em>含订阅套餐预付后的账本</em>
      </div>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">充值记录</h2>
      <DataTable
        :columns="[
          { key: 'id', label: '订单' },
          { key: 'channel', label: '支付方式' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '创建时间' },
          { key: 'credited_at', label: '到账时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="wallet.data.value?.recent_recharges"
        :loading="wallet.loading.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-channel="{ row }">{{ label('paymentChannel', row.channel).text }}</template>
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="rechargeStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-credited_at="{ row }">{{ dateTime(row.credited_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'pending'" class="btn-ghost btn-sm" @click="openOrder(row)">{{ row.channel === 'manual' ? '查看' : '继续支付' }}</button>
        </template>
      </DataTable>
    </div>

    <PaymentDialog v-model:open="showPay" :order="payOrder" @changed="reload" />
  </div>
</template>
