<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { buyer, market } from '@/api'
import { merchants } from '@/api/merchants'
import { isTested } from '@/api/market-detection'
import type { MarketItem, RechargeOrder } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { useToast } from '@/stores/toast'
import QrCode from '@/components/QrCode.vue'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'
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
  return isPack.value ? `${plat.value}${tier} · 订阅套餐` : `${plat.value}${tier} · 按量通道`
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
    return `订阅套餐：付 ¥${it.face_value} 获得周期内调用权，期内按 ${it.price.toFixed(2)}× 抵扣额度。到期或用尽即止；退货按卖家服务标签。`
  }
  return `按量通道，按官方价 × ${it.price.toFixed(2)} 计费。先加入商家清单，再在 API Key 页面选择挂单并调整调用顺序。`
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
      if (o.pack_checkout_status === 'failed' || o.pack_checkout_status === 'cancelled') {
        stopPoll()
        toast.info('额度包未能开通，已支付金额保留在平台余额，可在钱包使用或申请退款。')
        await item.load()
        router.push('/buyer/wallet')
        return
      }
      if (o.status === 'credited' || o.status === 'crediting') {
        const packs = await buyer.packs()
        const got = (packs ?? []).find((p) => p.recharge_id === order.id && p.status === 'active')
        if (got?.buyer_key_id) {
          stopPoll()
          toast.success('支付成功，订阅套餐已开通')
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
  if (busy.value) return
  if (!auth.loggedIn) {
    loginModal.show({ redirect: `/market/listings/${it.listing_id}` })
    return
  }
  if (isPack.value) {
    const channel = channelMap[payWay.value]
    busy.value = true
    try {
      if (!it.seller_id) throw new Error('商家信息暂不可用，请刷新后重试')
      await merchants.add(it.seller_id)
      const out = await buyer.purchasePack(it.listing_id, channel)
      if (out.key) {
        issuedKey.value = out.key
        toast.success('订阅套餐已开通，Key 已签发')
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
  busy.value = true
  try {
    if (!it.seller_id) throw new Error('商家信息暂不可用，请刷新后重试')
    await merchants.add(it.seller_id)
    toast.success('已加入商家清单，请选择挂单并设置调用顺序')
    await router.push({ path: '/buyer/keys', query: { listing: it.listing_id } })
  } catch (e) {
    toast.error(e)
  } finally {
    busy.value = false
  }
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
        <span class="kind" :class="{ pack: isPack }">{{ isPack ? '订阅套餐' : '按量通道' }}</span>
        <h1>{{ title }}</h1>
        <p class="lead">{{ desc }}</p>
        <div class="mb-4 rounded-lg border border-slate-200 p-3 text-sm">
          <MarketDetectionSummary :status="item.data.value.detection_status" :models="item.data.value.models" :checks="item.data.value.model_checks" :last-checked-at="item.data.value.last_checked_at" />
          <p v-if="isTested(item.data.value.detection_status)" class="text-slate-500">检测为抽样结果</p>
        </div>
        <div class="cover">
          <img :src="cover" :alt="title" />
        </div>
        <div class="specs">
          <div class="spec">
            <b>挂售模型</b>
            <span class="break-all">{{ item.data.value.models?.length ? item.data.value.models.join('、') : '卖家未声明' }}</span>
          </div>
          <div class="spec">
            <b>厂商 / 档位</b>
            <span>{{ plat }} · {{ item.data.value.tier || '—' }}</span>
          </div>
          <div class="spec">
            <b>质量等级</b>
            <span>{{ isTested(item.data.value.detection_status) ? (item.data.value.grade ? `${item.data.value.grade} 级` : '暂无等级') : '未检测' }}</span>
          </div>
          <div class="spec">
            <b>计费倍率</b>
            <span>{{ item.data.value.price.toFixed(2) }}× 官方价</span>
          </div>
          <div class="spec">
            <b>24h 成功率</b>
            <span>{{ percent(item.data.value.success_rate_24h, 0) }} · {{ item.data.value.ttft_p50_ms == null ? '暂无延迟数据' : `${Math.round(item.data.value.ttft_p50_ms)}ms` }}</span>
          </div>
          <template v-if="isPack">
            <div class="spec"><b>套餐售价</b><span>¥{{ Number(item.data.value.face_value ?? 0).toFixed(0) }}</span></div>
            <div class="spec"><b>可售份数</b><span>{{ stockLeft ?? '—' }} 份</span></div>
            <div class="spec"><b>计费说明</b><span>期内按倍率抵扣 · 到期停用</span></div>
          </template>
          <template v-else>
            <div class="spec">
              <b>最低额度 M</b>
              <span>{{ (item.data.value.min_amount ?? 0) === 0 ? '¥0（免费开通）' : `¥${Number(item.data.value.min_amount).toFixed(0)}` }}</span>
            </div>
            <div class="spec"><b>扣费顺序</b><span>先扣适用的店铺活动额度，再扣平台余额</span></div>
          </template>
          <div class="spec">
            <b>接入方式</b>
            <span>{{ authTypeText(item.data.value.platform, item.data.value.auth_type) }}</span>
          </div>
          <div class="spec">
            <b>健康状态</b>
            <span>{{ isTested(item.data.value.detection_status) ? label('health', item.data.value.health).text : '未检测' }}</span>
          </div>
        </div>
      </div>

      <aside class="panel">
        <div class="pay-label">{{ isPack ? '本单应付' : '调用倍率' }}</div>
        <div class="pay-price">{{ isPack ? `¥${payAmount.toFixed(0)}` : `${item.data.value.price.toFixed(2)}×` }}</div>
        <div class="pay-sub">
          <template v-if="isPack">支付套餐价后获得 Key；周期内使用期内额度，未用完可退货。</template>
          <template v-else>先保存商家，再选择挂单和调用顺序。加入清单不收费。</template>
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
          <div v-if="isPack" class="pay-ways">
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
            {{ busy ? '处理中…' : isPack ? (stockLeft === 0 ? '已售罄' : '支付并订阅套餐') : '加入商家并配置 Key' }}
          </button>
        </template>
        <RouterLink :to="{ path: '/buyer/store-credits', query: { seller_id: item.data.value.seller_id } }" class="mt-4 block text-center text-sm font-semibold underline underline-offset-4">店铺活动额度包</RouterLink>
        <RouterLink to="/buyer/merchants" class="mt-3 block text-center text-sm underline underline-offset-4">查看商家清单</RouterLink>
        <div class="rules">
          · 挂单按 Key 中的排列顺序优先调用<br />
          · 可选择平台默认分组作为最后兜底<br />
          · 失败请求不计费 · 通过 API Key 调用
          <template v-if="isPack"><br />· 订阅套餐支持按未用量退货</template>
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
