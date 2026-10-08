<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { buyer } from '@/api'
import type { RechargeOrder, Refund, Wallet } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import PaymentDialog from '@/components/PaymentDialog.vue'
import { label } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const wallet = useLoader(() => buyer.wallet(), null as Wallet | null)
const refunds = useLoader(() => buyer.refunds(), [] as Refund[] | null)
const { busy, run } = useAction()

const showPay = ref(false)
const payOrder = ref<RechargeOrder | null>(null)
const showRefund = ref(false)
const refundNote = ref('')

function reload() {
  wallet.load()
  refunds.load()
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

async function refund() {
  if (await run(() => buyer.requestRefund(refundNote.value), '已提交退款申请，所有 Key 已暂停')) {
    showRefund.value = false
    refundNote.value = ''
    reload()
  }
}
</script>

<template>
  <div>
    <PageHeader title="钱包" description="充值 1 元 = 1 个余额单位。调用按 sub2api 实际产生的费用扣减，失败的请求不计费。">
      <button class="btn-secondary" :disabled="!!wallet.data.value?.active_refund" @click="showRefund = true">申请退款</button>
      <button class="btn-primary" @click="newRecharge">充值</button>
    </PageHeader>

    <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
      <StatCard label="可用余额" :value="money(wallet.data.value?.balance)" hint="实时余额" />
      <StatCard label="账面余额" :value="money(wallet.data.value?.ledger_cash)" hint="用量入账有几分钟延迟" />
      <StatCard
        label="退款"
        :value="wallet.data.value?.active_refund ? '处理中' : '无'"
        :hint="wallet.data.value?.active_refund ? '退款完成前所有 Key 暂停使用' : '可随时申请退还剩余余额'"
      />
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
        <template #cell-amount="{ row }">
          {{ money(row.amount) }}
          <span v-if="row.refunded_amount > 0" class="block text-xs text-slate-400">已退 {{ money(row.refunded_amount) }}</span>
        </template>
        <template #cell-status="{ row }"><StatusBadge kind="rechargeStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-credited_at="{ row }">{{ dateTime(row.credited_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'pending'" class="btn-ghost btn-sm" @click="openOrder(row)">{{ row.channel === 'manual' ? '查看' : '继续支付' }}</button>
        </template>
      </DataTable>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">退款记录</h2>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'status', label: '状态' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'payouts', label: '退回方式' },
          { key: 'created_at', label: '申请时间' },
          { key: 'finished_at', label: '完成时间' },
        ]"
        :rows="refunds.data.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="refundStatus" :value="row.status" /></template>
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-payouts="{ row }">
          <ul v-if="row.payouts?.length" class="space-y-0.5 text-xs">
            <li v-for="p in row.payouts" :key="p.id">
              {{ p.channel === 'manual' ? '人工打款' : `原路退回${label('paymentChannel', p.channel).text}` }} {{ money(p.amount) }}
              <StatusBadge kind="payoutStatus" :value="p.status" />
            </li>
          </ul>
          <span v-else class="text-slate-400">—</span>
        </template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-finished_at="{ row }">{{ dateTime(row.finished_at) }}</template>
      </DataTable>
    </div>

    <PaymentDialog v-model:open="showPay" :order="payOrder" @changed="reload" />

    <ModalDialog v-model:open="showRefund" title="申请退款">
      <p class="text-sm text-slate-600">
        退还全部剩余余额（精确到分）。提交后你的所有 API Key 会先暂停，等待进行中的请求结束后扣减余额：近期通过支付宝、微信支付的充值原路退回，
        其余部分由平台人工打款。
      </p>
      <div>
        <label class="label" for="note">备注（可选）</label>
        <input id="note" v-model="refundNote" class="input" />
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showRefund = false">取消</button>
        <button class="btn-danger" :disabled="busy" @click="refund">确认退款</button>
      </template>
    </ModalDialog>
  </div>
</template>
