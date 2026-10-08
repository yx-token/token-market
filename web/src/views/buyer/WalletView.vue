<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { buyer } from '@/api'
import type { Refund, Wallet } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime, money, parseAmount } from '@/utils/format'

const toast = useToast()
const wallet = useLoader(() => buyer.wallet(), null as Wallet | null)
const refunds = useLoader(() => buyer.refunds(), [] as Refund[] | null)
const { busy, run } = useAction()

const showRecharge = ref(false)
const amount = ref('')
const instructions = ref('')
const showRefund = ref(false)
const refundNote = ref('')

function reload() {
  wallet.load()
  refunds.load()
}
onMounted(reload)

async function recharge() {
  const a = parseAmount(amount.value)
  if (a === null) {
    toast.error('请输入正确的金额（最多两位小数）')
    return
  }
  const res = await run(() => buyer.recharge(a))
  if (res) {
    instructions.value = `订单 #${res.order.id} 已创建（${money(res.order.amount)}）。请按平台公布的付款方式付款并备注订单号，平台确认到账后余额自动增加。`
    amount.value = ''
    reload()
  }
}

async function cancel(id: number) {
  if (await run(() => buyer.cancelRecharge(id), '订单已取消')) reload()
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
      <button class="btn-primary" @click="(showRecharge = true), (instructions = '')">充值</button>
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
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="rechargeStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-credited_at="{ row }">{{ dateTime(row.credited_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="row.status === 'pending'" class="btn-ghost btn-sm" :disabled="busy" @click="cancel(row.id)">取消</button>
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
          { key: 'created_at', label: '申请时间' },
          { key: 'finished_at', label: '完成时间' },
        ]"
        :rows="refunds.data.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="refundStatus" :value="row.status" /></template>
        <template #cell-amount="{ row }">{{ money(row.amount) }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-finished_at="{ row }">{{ dateTime(row.finished_at) }}</template>
      </DataTable>
    </div>

    <ModalDialog v-model:open="showRecharge" title="充值">
      <template v-if="!instructions">
        <div>
          <label class="label" for="amount">金额（元）</label>
          <input id="amount" v-model="amount" class="input" inputmode="decimal" placeholder="例如 100" />
          <p class="hint">第一期由平台人工确认到账。</p>
        </div>
      </template>
      <p v-else class="rounded bg-emerald-50 p-3 text-sm text-emerald-800">{{ instructions }}</p>
      <template #footer>
        <button class="btn-secondary" @click="showRecharge = false">关闭</button>
        <button v-if="!instructions" class="btn-primary" :disabled="busy" @click="recharge">创建订单</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showRefund" title="申请退款">
      <p class="text-sm text-slate-600">
        退还全部剩余余额（精确到分）。提交后你的所有 API Key 会先暂停，等待进行中的请求结束后扣减余额，平台确认后原路退回。
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
