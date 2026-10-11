<script setup lang="ts">
// 按充值单退款：输入金额（不超过这笔订单的未退金额，买家还受可退余额限制），不暂停 Key。买家钱包与后台充值订单共用。
import { computed, ref, watch } from 'vue'
import { admin, buyer } from '@/api'
import type { RechargeOrder } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import ModalDialog from '@/components/ModalDialog.vue'
import { label } from '@/utils/labels'
import { money, parseAmount } from '@/utils/format'

const props = defineProps<{
  order: RechargeOrder | null
  /** 买家当前可退余额（实时余额减去未用完的额度包）；管理员不传，由后端校验 */
  refundable?: number | null
  asAdmin?: boolean
}>()
const emit = defineEmits<{ done: [] }>()
const open = defineModel<boolean>('open', { default: false })
const toast = useToast()
const { busy, run } = useAction()

const amount = ref('')
const note = ref('')

const orderLeft = computed(() => (props.order ? Math.max(0, Math.round((props.order.amount - props.order.refunded_amount) * 100) / 100) : 0))
const max = computed(() => (props.refundable == null ? orderLeft.value : Math.max(0, Math.min(orderLeft.value, props.refundable))))
const limitHint = computed(() => {
  if (props.asAdmin) return '，同时不能超过买家当前的可退余额'
  return max.value < orderLeft.value ? '，受当前可退余额限制（未用完的额度包不计入）' : ''
})

watch(open, (v) => {
  if (!v) return
  amount.value = ''
  note.value = ''
})

async function submit() {
  const o = props.order
  if (!o) return
  const a = parseAmount(amount.value)
  if (a === null) {
    toast.error('请输入正确的金额（最多两位小数）')
    return
  }
  if (a > max.value + 1e-9) {
    toast.error(`最多可退 ${money(max.value)}`)
    return
  }
  const req = () => (props.asAdmin ? admin.refundRecharge(o.id, a, note.value) : buyer.refundRecharge(o.id, a, note.value))
  if (await run(req, '已提交退款')) {
    open.value = false
    emit('done')
  }
}
</script>

<template>
  <ModalDialog v-model:open="open" :title="order ? `充值单 #${order.id} 退款` : '退款'">
    <template v-if="order">
      <div class="flex items-center justify-between text-sm">
        <span>{{ label('paymentChannel', order.channel).text }} · 实付 <b>{{ money(order.amount) }}</b></span>
        <span class="text-slate-500">已退 {{ money(order.refunded_amount) }}</span>
      </div>
      <div>
        <label class="label" for="refund-amount">退款金额（元）</label>
        <div class="flex gap-2">
          <input id="refund-amount" v-model="amount" class="input" inputmode="decimal" :placeholder="`最多 ${money(max)}`" />
          <button type="button" class="btn-secondary btn-sm shrink-0" :disabled="max <= 0" @click="amount = max.toFixed(2)">全部</button>
        </div>
        <p class="hint">最多可退 {{ money(max) }}{{ limitHint }}</p>
      </div>
      <div>
        <label class="label" for="refund-note">备注（可选）</label>
        <input id="refund-note" v-model="note" class="input" />
      </div>
      <p class="text-sm text-slate-600">
        提交后立即从{{ asAdmin ? '买家' : '你' }}的余额中扣减，API Key 照常可用。在线支付的订单在退款期限内原路退回，人工充值或超过期限的由平台人工打款。
      </p>
    </template>
    <template #footer>
      <button class="btn-secondary" @click="open = false">取消</button>
      <button class="btn-danger" :disabled="busy || max <= 0" @click="submit">确认退款</button>
    </template>
  </ModalDialog>
</template>
