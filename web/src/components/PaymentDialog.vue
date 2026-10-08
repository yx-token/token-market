<script setup lang="ts">
// 充值支付：选择金额与支付方式 → 跳转支付宝收银台 / 展示扫码二维码 / 人工付款说明 → 轮询订单直到到账。
// 也用于"继续支付"与从支付宝网页支付跳回后的结果确认（传入 order）。
import { computed, onUnmounted, ref, watch } from 'vue'
import { buyer } from '@/api'
import type { RechargeOrder } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import ModalDialog from '@/components/ModalDialog.vue'
import QrCode from '@/components/QrCode.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { label } from '@/utils/labels'
import { money, parseAmount } from '@/utils/format'

const props = defineProps<{ order?: RechargeOrder | null }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>('open', { default: false })
const cfg = useConfig()
const toast = useToast()
const { busy, run } = useAction()

const amount = ref('')
const channel = ref('')
const current = ref<RechargeOrder | null>(null)
const now = ref(Date.now())
const presets = [50, 100, 200, 500]

const channels = computed(() => cfg.config.payment_channels ?? [])
const hints: Record<string, string> = {
  alipay_page: '跳转到支付宝收银台，可登录支付宝账号或扫码付款',
  alipay_f2f: '用支付宝 App 扫描二维码付款',
  wechat_native: '用微信扫描二维码付款',
  manual: '线下转账，平台人工确认到账（较慢）',
}

let poll: ReturnType<typeof setInterval> | null = null
let clock: ReturnType<typeof setInterval> | null = null
function stop() {
  if (poll) clearInterval(poll)
  if (clock) clearInterval(clock)
  poll = clock = null
}
onUnmounted(stop)

watch(open, async (v) => {
  stop()
  if (!v) return
  await cfg.ensure()
  current.value = props.order ?? null
  amount.value = ''
  channel.value = channels.value[0] ?? ''
  if (current.value) watchOrder()
})

// 到账中（已支付、正在给余额入账）继续轮询，直到已到账后刷新钱包
const terminal = (s: string) => ['credited', 'cancelled', 'expired', 'failed'].includes(s)

function watchOrder() {
  stop()
  clock = setInterval(() => (now.value = Date.now()), 1000)
  poll = setInterval(refresh, 2000)
  refresh()
}

async function refresh() {
  if (!current.value) return
  try {
    const o = await buyer.getRecharge(current.value.id)
    const before = current.value.status
    current.value = o
    if (before !== o.status) {
      emit('changed')
      if (before === 'pending' && (o.status === 'crediting' || o.status === 'credited')) toast.success('支付成功，余额正在到账')
    }
    if (terminal(o.status)) stop()
  } catch {
    // 网络抖动时下次再查
  }
}

async function submit() {
  const a = parseAmount(amount.value)
  if (a === null) {
    toast.error('请输入正确的金额（最多两位小数）')
    return
  }
  if (a < cfg.config.recharge_min || a > cfg.config.recharge_max) {
    toast.error(`单笔充值金额需在 ${money(cfg.config.recharge_min)} 到 ${money(cfg.config.recharge_max)} 之间`)
    return
  }
  if (!channel.value) {
    toast.error('请选择支付方式')
    return
  }
  const res = await run(() => buyer.recharge(a, channel.value))
  if (!res) return
  emit('changed')
  current.value = res.order
  if (res.order.payment?.kind === 'redirect' && res.order.payment.url) {
    window.location.href = res.order.payment.url
    return
  }
  watchOrder()
}

function continuePay() {
  const p = current.value?.payment
  if (p?.kind === 'redirect' && p.url) window.location.href = p.url
}

async function cancel() {
  if (!current.value) return
  if (await run(() => buyer.cancelRecharge(current.value!.id), '订单已取消')) {
    emit('changed')
    refresh()
  }
}

const remaining = computed(() => {
  const exp = current.value?.expires_at
  if (!exp) return ''
  const s = Math.max(0, Math.floor((new Date(exp).getTime() - now.value) / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
})
const title = computed(() => (current.value ? `充值订单 #${current.value.id}` : '充值'))
</script>

<template>
  <ModalDialog v-model:open="open" :title="title">
    <template v-if="!current">
      <div>
        <label class="label" for="amount">金额（元）</label>
        <input id="amount" v-model="amount" class="input" inputmode="decimal" placeholder="例如 100" />
        <div class="mt-2 flex gap-2">
          <button v-for="p in presets" :key="p" type="button" class="btn-secondary btn-sm" @click="amount = String(p)">{{ p }}</button>
        </div>
        <p class="hint">单笔 {{ money(cfg.config.recharge_min) }} – {{ money(cfg.config.recharge_max) }}，1 元 = 1 个余额单位。</p>
      </div>
      <div>
        <div class="label">支付方式</div>
        <div v-if="!channels.length" class="rounded bg-amber-50 p-3 text-sm text-amber-800">暂无可用的支付方式，请联系平台。</div>
        <div class="space-y-2">
          <label
            v-for="c in channels"
            :key="c"
            :class="['flex cursor-pointer items-start gap-3 rounded-md border p-3 text-sm', channel === c ? 'border-brand-500 bg-brand-50' : 'border-slate-200']"
          >
            <input v-model="channel" type="radio" :value="c" class="mt-1" :aria-label="label('paymentChannel', c).text" />
            <span>
              <span class="font-medium">{{ label('paymentChannel', c).text }}</span>
              <span class="block text-xs text-slate-500">{{ hints[c] }}</span>
            </span>
          </label>
        </div>
      </div>
    </template>

    <template v-else>
      <div class="flex items-center justify-between text-sm">
        <span>{{ label('paymentChannel', current.channel).text }} · <b class="text-lg">{{ money(current.amount) }}</b></span>
        <StatusBadge kind="rechargeStatus" :value="current.status" />
      </div>

      <div v-if="current.status === 'crediting' || current.status === 'credited'" class="rounded bg-emerald-50 p-4 text-center text-emerald-800" data-testid="pay-success">
        支付成功，{{ current.status === 'credited' ? '余额已到账' : '余额正在到账（通常几秒内完成）' }}。
      </div>
      <div v-else-if="current.status !== 'pending'" class="rounded bg-slate-50 p-4 text-center text-slate-600">
        订单{{ label('rechargeStatus', current.status).text }}{{ current.close_reason ? `（${label('closeReason', current.close_reason).text}）` : '' }}。如需充值请重新下单。
      </div>
      <template v-else-if="current.payment?.kind === 'qrcode' && current.payment.qr_code">
        <div class="flex flex-col items-center gap-2">
          <QrCode :value="current.payment.qr_code" />
          <p class="text-sm text-slate-600">请用{{ current.channel === 'wechat_native' ? '微信' : '支付宝' }}扫码付款，付款后页面会自动更新。</p>
          <p v-if="remaining" class="text-xs text-slate-400">剩余支付时间 {{ remaining }}</p>
        </div>
      </template>
      <template v-else-if="current.payment?.kind === 'redirect'">
        <p class="text-sm text-slate-600">请在支付宝收银台完成付款。付款完成后会自动跳回本页面；如果没有跳回，可以点击"继续支付"或稍后刷新。</p>
        <p v-if="remaining" class="text-xs text-slate-400">剩余支付时间 {{ remaining }}</p>
      </template>
      <p v-else class="rounded bg-amber-50 p-3 text-sm text-amber-800">
        {{ current.payment?.instructions || '请按平台公布的收款方式付款并备注订单号，管理员确认到账后自动入账。' }}订单号 #{{ current.id }}。
      </p>
    </template>

    <template #footer>
      <button class="btn-secondary" @click="open = false">关闭</button>
      <template v-if="!current">
        <button class="btn-primary" :disabled="busy || !channels.length" @click="submit">{{ channel === 'alipay_page' ? '去支付宝付款' : '下一步' }}</button>
      </template>
      <template v-else-if="current.status === 'pending'">
        <button class="btn-ghost text-red-600" :disabled="busy" @click="cancel">取消订单</button>
        <button v-if="current.payment?.kind === 'redirect'" class="btn-primary" @click="continuePay">继续支付</button>
      </template>
    </template>
  </ModalDialog>
</template>
