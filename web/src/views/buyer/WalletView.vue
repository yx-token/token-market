<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { buyer } from '@/api'
import type { RechargeOrder, Refund, Wallet } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import PaymentDialog from '@/components/PaymentDialog.vue'
import RechargeRefundDialog from '@/components/RechargeRefundDialog.vue'
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
const showOrderRefund = ref(false)
const refundOrder = ref<RechargeOrder | null>(null)

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

// 已到账且还有未退金额的充值单可以按单退款
const orderRefundable = (o: RechargeOrder) => o.status === 'credited' && o.amount - o.refunded_amount >= 0.01

function openOrderRefund(o: RechargeOrder) {
  refundOrder.value = o
  showOrderRefund.value = true
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
    <PageHeader title="钱包" description="充值后可支付调用费用、购买店铺活动包或订阅套餐。活动包仅限对应店铺与适用模型；订阅套餐需余额 ≥ 套餐价。">
      <button type="button" class="xm-btn xm-btn-ghost" :disabled="!!wallet.data.value?.active_refund" @click="showRefund = true">申请退款</button>
      <button type="button" class="xm-btn xm-btn-primary" @click="newRecharge">充值</button>
    </PageHeader>

    <div class="xm-stats" style="grid-template-columns: repeat(3, 1fr)">
      <div class="xm-stat">
        <b>可调用余额</b>
        <strong>{{ money(wallet.data.value?.balance) }}</strong>
        <em>sub2api 实时扣费</em>
      </div>
      <div class="xm-stat">
        <b>账面余额</b>
        <strong>{{ money(wallet.data.value?.ledger_cash) }}</strong>
        <em>含订阅套餐预付后的账本</em>
      </div>
      <div class="xm-stat">
        <b>退款</b>
        <strong>{{ wallet.data.value?.active_refund ? '处理中' : '无' }}</strong>
        <em v-if="!wallet.data.value?.active_refund">可退全部余额，或在充值记录中按单退款</em>
        <em v-else-if="wallet.data.value?.active_refund?.kind === 'balance'">完成前 Key 暂停</em>
        <em v-else>Key 照常可用</em>
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
        <template #cell-amount="{ row }">
          {{ money(row.amount) }}
          <span v-if="row.refunded_amount > 0" class="block text-xs text-slate-400">已退 {{ money(row.refunded_amount) }}</span>
        </template>
        <template #cell-status="{ row }"><StatusBadge kind="rechargeStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-credited_at="{ row }">{{ dateTime(row.credited_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'pending'" class="btn-ghost btn-sm" @click="openOrder(row)">{{ row.channel === 'manual' ? '查看' : '继续支付' }}</button>
          <button
            v-if="orderRefundable(row)"
            class="btn-ghost btn-sm"
            :disabled="!!wallet.data.value?.active_refund"
            :title="wallet.data.value?.active_refund ? '已有退款在处理中' : ''"
            @click="openOrderRefund(row)"
          >
            退款
          </button>
        </template>
      </DataTable>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">退款记录</h2>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'kind', label: '类型' },
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
        <template #cell-kind="{ row }">
          {{ label('refundKind', row.kind).text }}
          <span v-if="row.recharge_order_id" class="block text-xs text-slate-400">订单 #{{ row.recharge_order_id }}</span>
        </template>
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

    <RechargeRefundDialog v-model:open="showOrderRefund" :order="refundOrder" :refundable="wallet.data.value?.refundable" @done="reload" />

    <ModalDialog v-model:open="showRefund" title="申请退款（全部余额）">
      <p class="text-sm text-slate-600">
        退还全部剩余余额（精确到分）。提交后你的所有 API Key 会先暂停，等待进行中的请求结束后扣减余额：近期通过支付宝、微信支付的充值原路退回，
        其余部分由平台人工打款。
      </p>
      <p class="hint">只想退一部分？在充值记录中对某笔订单点「退款」，输入金额即可，Key 不会暂停。</p>
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
