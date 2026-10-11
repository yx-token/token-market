<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { buyer, market } from '@/api'
import type { MarketItem, RechargeOrder } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { useToast } from '@/stores/toast'
import QrCode from '@/components/QrCode.vue'
import { apiToPlat, coverFor } from '@/market/config'
import { authTypeText, label } from '@/utils/labels'
import { percent } from '@/utils/format'

const props = defineProps<{ id: string }>()
const auth = useAuth()
const loginModal = useLoginModal()
const router = useRouter()
const toast = useToast()
const item = useLoader(() => market.listing(Number(props.id)), null as MarketItem | null)
const busy = ref(false)
const issuedKey = ref('')
const payWay = ref<'balance' | 'alipay' | 'wechat'>('balance')
const payOrder = ref<RechargeOrder | null>(null)

const isPack = computed(() => {
  const it = item.data.value
  return !!it && (it.sku_kind === 'pack' || it.mode === 'pack')
})

const plat = computed(() => {
  const it = item.data.value
  if (!it) return ''
  return apiToPlat[it.platform] || label('platform', it.platform).text
})

const title = computed(() => {
  const it = item.data.value
  if (!it) return ''
  if (it.title) return it.title
  const tier = it.tier ? ` · ${it.tier}` : ''
  return isPack.value ? `${plat.value}${tier} · ${it.face_value ?? '—'} 元额度包` : `${plat.value}${tier} · 按量通道`
})

const cover = computed(() => {
  const it = item.data.value
  if (!it) return '/assets/card1.jpg'
  return `/assets/${coverFor(plat.value, it.listing_id)}`
})

const stockLeft = computed(() => {
  const it = item.data.value
  if (!it?.stock && it?.stock !== 0) return null
  return Math.max(0, (it.stock ?? 0) - (it.stock_sold ?? 0))
})

const payAmount = computed(() => {
  const it = item.data.value
  if (!it) return 0
  return isPack.value ? Number(it.face_value ?? 0) : Number(it.min_amount ?? 0)
})

const desc = computed(() => {
  const it = item.data.value
  if (!it) return ''
  if (it.description) return it.description
  if (isPack.value) {
    return `预购 ¥${it.face_value} 调用额度，独占本包；包内按 ${it.price.toFixed(2)}× 计费。未用完可退货（退未用量并释放库存）。`
  }
  const m = it.min_amount ?? 0
  return `按量通道，倍率 ${it.price.toFixed(2)}×，起步金 M=${m === 0 ? '0（免费开通）' : '¥' + m}。用完 M 后继续扣平台余额；余额不足提示充值。`
})

const channelMap = {
  balance: 'balance',
  alipay: 'alipay_page',
  wechat: 'wechat_native',
} as const

let poll: ReturnType<typeof setInterval> | null = null
onUnmounted(() => {
  if (poll) clearInterval(poll)
})

onMounted(item.load)

function stopPoll() {
  if (poll) clearInterval(poll)
  poll = null
}

async function watchPackPay(order: RechargeOrder) {
  payOrder.value = order
  if (order.payment?.kind === 'redirect' && order.payment.url) {
    window.location.href = order.payment.url
    return
  }
  stopPoll()
  poll = setInterval(async () => {
    try {
      const o = await buyer.getRecharge(order.id)
      payOrder.value = o
      if (o.status === 'credited' || o.status === 'crediting') {
        const packs = await buyer.packs()
        const got = (packs ?? []).find((p) => p.listing_id === item.data.value?.listing_id && p.status === 'active')
        if (got?.buyer_key_id) {
          stopPoll()
          toast.success('支付成功，额度包已到账')
          await item.load()
          router.push('/buyer/keys')
        }
      }
      if (['cancelled', 'expired', 'failed'].includes(o.status)) {
        stopPoll()
        toast.error('支付未完成')
      }
    } catch {
      /* retry */
    }
  }, 2000)
}

async function checkout() {
  const it = item.data.value
  if (!it) return
  if (!auth.loggedIn) {
    loginModal.show({ redirect: `/market/listings/${it.listing_id}` })
    return
  }
  if (isPack.value) {
    const channel = channelMap[payWay.value]
    busy.value = true
    try {
      const out = await buyer.purchasePack(it.listing_id, channel)
      if (out.key) {
        issuedKey.value = out.key
        toast.success('额度包已购买，Key 已签发')
        await item.load()
        return
      }
      if (out.recharge) {
        toast.success('请完成支付，到账后自动签发 Key')
        await watchPackPay(out.recharge)
        return
      }
      toast.error('下单异常，请稍后重试')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '购买失败')
    } finally {
      busy.value = false
    }
    return
  }
  router.push({ path: '/buyer/keys', query: { listing: String(it.listing_id) } })
}

async function cancelPay() {
  if (!payOrder.value) return
  busy.value = true
  try {
    await buyer.cancelRecharge(payOrder.value.id)
    toast.success('订单已取消')
    payOrder.value = null
    stopPoll()
    await item.load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '取消失败')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page sku-page">
    <p class="sku-back">
      <RouterLink to="/">← 回市场</RouterLink>
    </p>

    <div v-if="item.data.value" class="sku-layout">
      <div>
        <span class="kind" :class="{ pack: isPack }">{{ isPack ? '额度包' : '按量通道' }}</span>
        <h1>{{ title }}</h1>
        <p class="lead">{{ desc }}</p>
        <div class="cover">
          <img :src="cover" :alt="title" />
        </div>
        <div class="specs">
          <div class="spec">
            <b>厂商 / 档位</b>
            <span>{{ plat }} · {{ item.data.value.tier || '—' }}</span>
          </div>
          <div class="spec">
            <b>质量等级</b>
            <span>{{ item.data.value.grade || '—' }} 级</span>
          </div>
          <div class="spec">
            <b>计费倍率</b>
            <span>{{ item.data.value.price.toFixed(2) }}× 官方价</span>
          </div>
          <div class="spec">
            <b>24h 成功率</b>
            <span>{{ percent(item.data.value.success_rate_24h, 0) }} · {{ Math.round(item.data.value.ttft_p50_ms ?? 0) }}ms</span>
          </div>
          <template v-if="isPack">
            <div class="spec"><b>包面额</b><span>¥{{ Number(item.data.value.face_value ?? 0).toFixed(0) }}</span></div>
            <div class="spec"><b>可售库存</b><span>{{ stockLeft ?? '—' }} 份</span></div>
          </template>
          <template v-else>
            <div class="spec">
              <b>最低额度 M</b>
              <span>{{ (item.data.value.min_amount ?? 0) === 0 ? '¥0（免费开通）' : `¥${Number(item.data.value.min_amount).toFixed(0)}` }}</span>
            </div>
            <div class="spec"><b>超额计费</b><span>只扣平台余额</span></div>
          </template>
          <div class="spec">
            <b>接入方式</b>
            <span>{{ authTypeText(item.data.value.platform, item.data.value.auth_type) }}</span>
          </div>
          <div class="spec">
            <b>健康状态</b>
            <span>{{ label('health', item.data.value.health).text }}</span>
          </div>
        </div>
      </div>

      <aside class="panel">
        <div class="pay-label">本单应付</div>
        <div class="pay-price">{{ payAmount === 0 ? '¥0' : `¥${payAmount.toFixed(0)}` }}</div>
        <div class="pay-sub">
          <template v-if="isPack">支付整包价后获得 Key + 包内额度。未用完可退货。</template>
          <template v-else-if="payAmount === 0">0 元开通通道，签发 Key。之后调用从平台余额扣费。</template>
          <template v-else>支付起步金开通通道。用完起步额度后继续扣平台余额。</template>
        </div>

        <template v-if="payOrder && isPack">
          <div class="pay-pending">
            <div class="pay-label">待支付 · {{ payOrder.channel }}</div>
            <p class="text-[13px] text-[#666]">订单 #{{ payOrder.id }} · {{ payOrder.status }}</p>
            <div v-if="payOrder.payment?.kind === 'qrcode' && payOrder.payment.qr_code" class="mt-3 flex justify-center">
              <QrCode :value="payOrder.payment.qr_code" />
            </div>
            <p v-else-if="payOrder.payment?.kind === 'redirect'" class="mt-2 text-[13px]">
              若未跳转，
              <a :href="payOrder.payment.url" class="text-[#1a6]" rel="noopener">打开收银台</a>
            </p>
            <p class="mt-2 text-[12px] text-[#888]">支付成功后自动履约签发 Key，请勿关闭页面。</p>
            <button class="btn-pay mt-3" type="button" :disabled="busy" @click="cancelPay">取消订单</button>
          </div>
        </template>
        <template v-else>
          <div class="pay-ways">
            <label class="pay-way" :class="{ on: payWay === 'balance' }">
              <input v-model="payWay" type="radio" value="balance" /> 平台余额
            </label>
            <label class="pay-way" :class="{ on: payWay === 'alipay' }">
              <input v-model="payWay" type="radio" value="alipay" /> 支付宝
            </label>
            <label class="pay-way" :class="{ on: payWay === 'wechat' }">
              <input v-model="payWay" type="radio" value="wechat" /> 微信支付
            </label>
          </div>
          <button
            class="btn-pay"
            type="button"
            :disabled="busy || (isPack && stockLeft === 0)"
            @click="checkout"
          >
            {{ busy ? '处理中…' : isPack ? (stockLeft === 0 ? '已售罄' : '支付并购买额度包') : '支付并开通通道' }}
          </button>
        </template>
        <div class="rules">
          · 下单可用余额或支付宝/微信<br />
          · 调用阶段只扣平台余额，扣完提示充值<br />
          · 失败请求不计费 · 交付物为 API Key（非账号）
          <template v-if="isPack"><br />· 额度包支持按未用量退货</template>
        </div>
        <div v-if="issuedKey" class="done is-on">
          已签发 Key，之后也可以在买家中心再次复制。
          <code>{{ issuedKey }}</code>
          <RouterLink to="/buyer/keys">去买家中心 →</RouterLink>
        </div>
      </aside>
    </div>

    <p v-else-if="item.loading.value" class="sku-empty">加载中…</p>
    <p v-else class="sku-empty">挂单不存在或已下架 · <RouterLink to="/">回市场</RouterLink></p>
  </div>
</template>
