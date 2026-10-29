<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { buyer, market } from '@/api'
import { merchants } from '@/api/merchants'
import { isTested } from '@/api/market-detection'
import type { MarketItem, RechargeOrder, ReviewEligible, ReviewItem, SellerRatingStats } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'
import { useToast } from '@/stores/toast'
import QrCode from '@/components/QrCode.vue'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'
import ReviewForm from '@/components/reviews/ReviewForm.vue'
import ReviewList from '@/components/reviews/ReviewList.vue'
import { apiToPlat, brandIconFor, coverFor } from '@/market/config'
import { authTypeText, label } from '@/utils/labels'
import { percent } from '@/utils/format'

const PREVIEW_N = 2

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
const { busy: reviewBusy, run: runReview } = useAction()
const reviewStats = ref<SellerRatingStats | null>(null)
const reviewItems = ref<ReviewItem[]>([])
const reviewsLoading = ref(false)
const eligible = ref<ReviewEligible | null>(null)
const showReviewForm = ref(false)
const reviewsExpanded = ref(false)
const detectOpen = ref(true)
const reviewsEl = ref<HTMLElement | null>(null)

async function loadReviews(sellerId: number) {
  reviewsLoading.value = true
  try {
    const res = await market.sellerReviews(sellerId, { limit: 30, offset: 0 })
    reviewStats.value = res.stats
    reviewItems.value = res.items ?? []
  } catch {
    reviewStats.value = null
    reviewItems.value = []
  } finally {
    reviewsLoading.value = false
  }
}

async function loadEligible(sellerId: number) {
  if (!auth.loggedIn) {
    eligible.value = null
    return
  }
  try {
    const out = await buyer.reviewEligible(sellerId)
    eligible.value = Array.isArray(out) ? out.find((e) => e.seller_id === sellerId) ?? null : out
  } catch {
    eligible.value = null
  }
}

watch(
  () => item.data.value?.seller_id,
  (sid) => {
    if (!sid) return
    loadReviews(sid)
    loadEligible(sid)
  },
)

async function submitListingReview(payload: { rating: number; content: string; tags: string[] }) {
  const it = item.data.value
  if (!it?.seller_id) return
  const ok = await runReview(
    () =>
      buyer.createReview({
        seller_id: it.seller_id,
        listing_id: it.listing_id,
        rating: payload.rating,
        content: payload.content || undefined,
        tags: payload.tags,
      }),
    '评价已提交',
  )
  if (ok) {
    showReviewForm.value = false
    reviewsExpanded.value = true
    await loadReviews(it.seller_id)
    await loadEligible(it.seller_id)
  }
}

async function jumpToReviews() {
  reviewsExpanded.value = true
  await nextTick()
  const box = reviewsEl.value?.querySelector('.sku-reviews__scroll') as HTMLElement | null
  box?.scrollTo({ top: 0, behavior: 'smooth' })
}

function startWriteReview() {
  if (!auth.loggedIn) {
    const id = item.data.value?.listing_id
    loginModal.show({ redirect: id ? `/market/listings/${id}` : '/' })
    return
  }
  if (!eligible.value?.can_review) {
    if (eligible.value?.can_follow_up) {
      router.push('/buyer/reviews')
      return
    }
    toast.info('需对该商家有过用量或套餐后才能评价')
    return
  }
  reviewsExpanded.value = true
  showReviewForm.value = true
}

const sellerScoreText = computed(() => {
  const n = reviewStats.value?.rating_count ?? item.data.value?.seller_rating_count ?? 0
  if (!n) return ''
  const avg = reviewStats.value?.avg_rating ?? item.data.value?.seller_avg_rating ?? 0
  return Number(avg).toFixed(1)
})

const reviewCount = computed(
  () => reviewStats.value?.rating_count ?? item.data.value?.seller_rating_count ?? 0,
)

const visibleReviews = computed(() => {
  const all = reviewItems.value ?? []
  return reviewsExpanded.value ? all : all.slice(0, PREVIEW_N)
})

const hasMoreReviews = computed(() => (reviewItems.value?.length ?? 0) > PREVIEW_N)

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

const iconSrc = computed(() => {
  const it = item.data.value
  if (!it) return '/assets/card1.jpg'
  const brand = brandIconFor(plat.value)
  if (brand) return brand.startsWith('/') ? brand : `/assets/${brand}`
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
    return `付 ¥${it.face_value} 开通，期内按 ${it.price.toFixed(2)}× 抵扣。`
  }
  return `按官方价 × ${it.price.toFixed(2)} 计费；加入清单后配置 Key 即可调用。`
})

/** 左标签右内容，保证纵向对齐 */
const infoRows = computed(() => {
  const it = item.data.value
  if (!it) return [] as { label: string; value: string }[]
  const rows: { label: string; value: string }[] = [
    { label: '厂商', value: plat.value || '—' },
    { label: '档位', value: it.tier || '—' },
    { label: '计费倍率', value: `${it.price.toFixed(2)}× 官方价` },
    {
      label: '质量等级',
      value: isTested(it.detection_status) ? (it.grade ? `${it.grade} 级` : '暂无等级') : '未检测',
    },
    {
      label: '24h 表现',
      value: `${percent(it.success_rate_24h, 0)} · ${it.ttft_p50_ms == null ? '暂无延迟' : `${Math.round(it.ttft_p50_ms)}ms`}`,
    },
    { label: '接入方式', value: authTypeText(it.platform, it.auth_type) },
  ]
  if (isPack.value) {
    rows.push({ label: '套餐售价', value: `¥${Number(it.face_value ?? 0).toFixed(0)}` })
    rows.push({ label: '可售份数', value: stockLeft.value == null ? '—' : `${stockLeft.value} 份` })
  } else {
    rows.push({
      label: '最低开通',
      value: (it.min_amount ?? 0) === 0 ? '¥0（免费）' : `¥${Number(it.min_amount).toFixed(0)}`,
    })
    rows.push({ label: '扣费顺序', value: '先活动额度，再平台余额' })
  }
  return rows
})

const modelList = computed(() => item.data.value?.models?.filter(Boolean) ?? [])

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
  <div class="page sku-page sku-page--neat">
    <nav class="sku-back">
      <RouterLink to="/">← 回市场</RouterLink>
    </nav>

    <div v-if="item.data.value" class="sku-board">
      <div class="sku-left">
      <!-- 左上：标题 -->
      <header class="sku-head">
        <div class="sku-head__icon"><img :src="iconSrc" alt="" /></div>
        <div class="sku-head__text">
          <div class="sku-head__meta">
            <span class="kind" :class="{ pack: isPack }">{{ isPack ? '订阅套餐' : '按量通道' }}</span>
            <span class="plat">{{ plat }}</span>
            <button type="button" class="rating-link" @click="jumpToReviews">
              <template v-if="sellerScoreText">★ {{ sellerScoreText }}（{{ reviewCount }}）</template>
              <template v-else>暂无评价</template>
            </button>
          </div>
          <h1>{{ title }}</h1>
          <p class="lead">{{ desc }}</p>
        </div>
      </header>

      <!-- 左下：信息 + 模型 + 检测（区域内滚动，展开不顶整页） -->
      <section class="sku-main">
        <div class="sku-block">
          <h2 class="sku-card__h">商品信息</h2>
          <dl class="sku-dl">
            <div v-for="row in infoRows" :key="row.label" class="sku-dl__row">
              <dt>{{ row.label }}</dt>
              <dd>{{ row.value }}</dd>
            </div>
          </dl>
        </div>
        <div class="sku-block">
          <h2 class="sku-card__h">挂售模型</h2>
          <div v-if="modelList.length" class="sku-models">
            <span v-for="m in modelList" :key="m">{{ m }}</span>
          </div>
          <p v-else class="sku-muted">卖家未声明模型</p>
        </div>
        <div class="sku-block sku-block--detect">
          <button type="button" class="sku-fold" @click="detectOpen = !detectOpen">
            <span>质量检测</span>
            <em>{{ detectOpen ? '收起' : '展开' }}</em>
          </button>
          <div v-show="detectOpen" class="sku-fold__body">
            <MarketDetectionSummary
              :status="item.data.value.detection_status"
              :models="item.data.value.models"
              :checks="item.data.value.model_checks"
              :last-checked-at="item.data.value.last_checked_at"
            />
          </div>
        </div>
      </section>
      </div>

      <!-- 右：购买 + 评价填满空白 -->
      <aside class="sku-side">
        <div class="sku-checkout">
          <div class="sku-checkout__top">
            <div class="pay-label">{{ isPack ? '本单应付' : '调用倍率' }}</div>
            <div class="pay-price">{{ isPack ? `¥${payAmount.toFixed(0)}` : `${item.data.value.price.toFixed(2)}×` }}</div>
            <p class="pay-sub">
              <template v-if="isPack">支付后获得 Key，期内按倍率抵扣</template>
              <template v-else>加入清单免费，再到 Key 页排序调用</template>
            </p>
          </div>
          <div class="sku-checkout__body">
            <template v-if="payOrder && isPack">
              <div class="pay-pending">
                <div class="pay-label">待支付 · {{ payOrder.channel }}</div>
                <p class="hint">订单 #{{ payOrder.id }} · {{ payOrder.status }}</p>
                <div v-if="payOrder.payment?.kind === 'qrcode' && payOrder.payment.qr_code" class="qr">
                  <QrCode :value="payOrder.payment.qr_code" />
                </div>
                <p v-else-if="payOrder.payment?.kind === 'redirect'" class="hint">
                  若未跳转，
                  <a :href="payOrder.payment.url" rel="noopener">打开收银台</a>
                </p>
                <p class="hint">支付成功后自动签发 Key</p>
                <button class="btn-pay" type="button" :disabled="busy" @click="cancelPay">取消订单</button>
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
                {{ busy ? '处理中…' : isPack ? (stockLeft === 0 ? '已售罄' : '支付并订阅') : '加入并配置 Key' }}
              </button>
            </template>
            <div class="sku-checkout__links">
              <RouterLink :to="{ path: '/buyer/store-credits', query: { seller_id: item.data.value.seller_id } }">活动额度</RouterLink>
              <RouterLink to="/buyer/merchants">商家清单</RouterLink>
            </div>
            <p class="rules">失败不计费 · Key 顺序优先 · 可平台分组兜底</p>
            <div v-if="issuedKey" class="done">
              已签发 Key
              <code>{{ issuedKey }}</code>
              <RouterLink to="/buyer/keys">去买家中心 →</RouterLink>
            </div>
          </div>
        </div>

        <section v-if="item.data.value.seller_id" ref="reviewsEl" class="sku-reviews">
          <div class="sku-card__head">
            <h2 class="sku-card__h">商家评价</h2>
            <div class="sku-card__actions">
              <button
                v-if="eligible?.can_review || !auth.loggedIn"
                type="button"
                class="link-btn"
                @click="startWriteReview"
              >
                写评价
              </button>
              <RouterLink v-else-if="eligible?.can_follow_up" to="/buyer/reviews" class="link-btn">去追评</RouterLink>
            </div>
          </div>
          <p v-if="sellerScoreText" class="sku-reviews__sum">★ {{ sellerScoreText }} · {{ reviewCount }} 条</p>
          <p v-else class="sku-muted">还没有评价</p>

          <div v-if="showReviewForm" class="sku-reviews__form">
            <div class="sku-reviews__form-head">
              <strong>写评价</strong>
              <button type="button" class="link-btn" @click="showReviewForm = false">取消</button>
            </div>
            <ReviewForm :busy="reviewBusy" @submit="submitListingReview" />
          </div>

          <div class="sku-reviews__scroll">
            <ReviewList :items="visibleReviews" :loading="reviewsLoading" empty="该商家暂无公开评价" />
          </div>

          <button
            v-if="hasMoreReviews && !reviewsExpanded"
            type="button"
            class="sku-more"
            @click="reviewsExpanded = true"
          >
            展开全部 {{ reviewItems.length }} 条
          </button>
          <button
            v-else-if="hasMoreReviews && reviewsExpanded"
            type="button"
            class="sku-more"
            @click="reviewsExpanded = false"
          >
            收起
          </button>
        </section>
      </aside>
    </div>

    <p v-else-if="item.loading.value" class="sku-empty">加载中…</p>
    <p v-else class="sku-empty">挂单不存在或已下架 · <RouterLink to="/">回市场</RouterLink></p>
  </div>
</template>

<style scoped>
.sku-page--neat {
  --sku-pad: 14px;
  --sku-radius: 12px;
  --sku-line: rgba(0, 0, 0, 0.07);
  max-width: 1120px;
  margin-left: auto;
  margin-right: auto;
  padding-bottom: 24px;
}
.sku-back {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 700;
  color: #666;
}

/* 一整块板：左右分栏固定高度，展开只在区内滚动 */
.sku-board {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 0;
  background: #fff;
  border: 1px solid var(--sku-line);
  border-radius: 16px;
  overflow: hidden;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.04);
  height: calc(100dvh - 120px);
  max-height: calc(100dvh - 120px);
  min-height: 420px;
}
.sku-left {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.sku-side {
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--sku-line);
  background: #fafafa;
  min-height: 0;
  overflow: hidden;
}
@media (max-width: 900px) {
  .sku-board {
    grid-template-columns: 1fr;
    height: auto;
    max-height: none;
  }
  .sku-side {
    border-left: 0;
    border-top: 1px solid var(--sku-line);
    max-height: min(52vh, 480px);
  }
  .sku-main {
    max-height: min(48vh, 420px);
  }
}

.sku-head {
  flex-shrink: 0;
  display: grid;
  grid-template-columns: 56px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  padding: var(--sku-pad) 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
}
.sku-head__icon {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  overflow: hidden;
  background: #f5f5f5;
  border: 1px solid var(--sku-line);
}
.sku-head__icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.sku-head__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.kind {
  display: inline-flex;
  align-items: center;
  font-size: 11px;
  font-weight: 800;
  padding: 2px 7px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.06);
  color: #555;
}
.kind.pack {
  background: rgba(255, 200, 80, 0.45);
  color: #9a3412;
}
.plat {
  font-size: 12px;
  font-weight: 700;
  color: #666;
}
.rating-link {
  border: 0;
  background: #fff3a8;
  color: #8a5a00;
  font: inherit;
  font-size: 12px;
  font-weight: 800;
  padding: 2px 8px;
  border-radius: 999px;
  cursor: pointer;
}
.sku-head h1 {
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 800;
  letter-spacing: -0.02em;
  line-height: 1.25;
  color: var(--ink, #3a3a3a);
}
.lead {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.45;
  color: #666;
}

.sku-main {
  flex: 1;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-template-rows: auto auto;
  align-content: start;
  gap: 0;
  padding: 0;
}
.sku-block {
  padding: 12px 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  border-right: 1px solid rgba(0, 0, 0, 0.05);
  min-width: 0;
}
.sku-block:nth-child(2n) {
  border-right: 0;
}
.sku-block--detect {
  grid-column: 1 / -1;
  border-right: 0;
  border-bottom: 0;
  padding-top: 10px;
  padding-bottom: 10px;
}
@media (max-width: 640px) {
  .sku-main {
    grid-template-columns: 1fr;
  }
  .sku-block {
    border-right: 0;
  }
}

.sku-card__h {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 800;
  color: var(--ink, #3a3a3a);
}
.sku-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 4px;
}
.sku-card__head .sku-card__h {
  margin: 0;
}
.sku-card__actions {
  display: flex;
  gap: 8px;
}

/* 信息两列格子，压低纵向高度 */
.sku-dl {
  margin: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 12px;
}
.sku-dl__row {
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr);
  gap: 6px;
  align-items: baseline;
  padding: 0;
  border: 0;
  min-width: 0;
}
.sku-dl dt {
  margin: 0;
  font-size: 11px;
  font-weight: 700;
  color: #999;
}
.sku-dl dd {
  margin: 0;
  font-size: 12.5px;
  font-weight: 700;
  color: var(--ink, #3a3a3a);
  word-break: break-word;
}
@media (max-width: 640px) {
  .sku-dl {
    grid-template-columns: 1fr;
  }
}

.sku-models {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.sku-models span {
  font-size: 11.5px;
  font-weight: 700;
  padding: 4px 8px;
  border-radius: 999px;
  background: #f0f0f0;
  color: #444;
}
.sku-muted {
  margin: 0;
  font-size: 12.5px;
  color: #999;
}

.sku-checkout {
  flex-shrink: 0;
  background: #fff;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 0;
}
.sku-checkout__top {
  padding: 12px 14px 10px;
  background: linear-gradient(180deg, #fffced, #fff);
}
.sku-checkout__body {
  padding: 0 14px 12px;
}
.pay-label {
  font-size: 11px;
  font-weight: 700;
  color: #888;
}
.pay-price {
  margin: 2px 0 2px;
  font-size: 28px;
  font-weight: 800;
  color: var(--price, #ff7a45);
  letter-spacing: -0.03em;
  line-height: 1.05;
}
.pay-sub,
.hint,
.rules {
  margin: 0;
  font-size: 11.5px;
  color: #888;
  line-height: 1.4;
}
.pay-ways {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin: 8px 0;
}
.pay-way {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1.5px solid rgba(0, 0, 0, 0.08);
  border-radius: 8px;
  cursor: pointer;
  font-size: 12.5px;
  font-weight: 600;
  background: #fff;
}
.pay-way.on {
  border-color: rgba(255, 196, 0, 0.7);
  background: #fffced;
}
.btn-pay {
  display: block;
  width: 100%;
  height: auto;
  margin-top: 6px;
  border: 0;
  border-radius: 10px;
  background: var(--yellow-btn, #ffd400);
  color: var(--ink, #3a3a3a);
  font-size: 14px;
  font-weight: 800;
  line-height: 1.3;
  padding: 10px 12px;
  cursor: pointer;
  box-shadow: 0 3px 10px rgba(255, 196, 0, 0.28);
  flex: none;
  aspect-ratio: auto;
}
.btn-pay:hover {
  background: var(--yellow-hover, #f0c200);
}
.btn-pay:disabled {
  opacity: 0.45;
  cursor: not-allowed;
  box-shadow: none;
}
.sku-checkout__links {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
  margin-top: 8px;
}
.sku-checkout__links a {
  font-size: 12px;
  font-weight: 700;
  color: #555;
  text-decoration: underline;
  text-underline-offset: 2px;
}
.rules {
  margin-top: 6px;
  text-align: center;
}
.qr {
  margin: 8px 0;
  display: flex;
  justify-content: center;
}
.done {
  margin-top: 8px;
  padding: 10px;
  border-radius: 8px;
  background: #f0fdf4;
  color: #166534;
  font-size: 12px;
  line-height: 1.5;
}
.done code {
  display: block;
  margin: 6px 0;
  padding: 6px;
  background: #fff;
  border-radius: 6px;
  word-break: break-all;
  color: #111;
}

/* 评价贴在购买下方，列表区内滚动 */
.sku-reviews {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 12px 14px 14px;
  overflow: hidden;
}
.sku-reviews__sum {
  flex-shrink: 0;
  margin: 0 0 6px;
  font-size: 12px;
  font-weight: 700;
  color: #8a5a00;
}
.sku-reviews__form {
  flex-shrink: 0;
  margin: 0 0 8px;
  padding: 10px;
  border-radius: 10px;
  background: #fffced;
  border: 1px solid rgba(255, 196, 0, 0.25);
  max-height: 220px;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.sku-reviews__form-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}
.sku-reviews__scroll {
  flex: 1;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.sku-more,
.sku-fold,
.link-btn {
  font: inherit;
  font-size: 12px;
  font-weight: 800;
  color: #555;
  background: transparent;
  border: 0;
  cursor: pointer;
  padding: 0;
}
.link-btn {
  text-decoration: underline;
  text-underline-offset: 2px;
}
.sku-more {
  flex-shrink: 0;
  display: block;
  width: 100%;
  margin-top: 6px;
  padding: 7px;
  border-radius: 8px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.06);
  text-decoration: none;
}
.sku-fold {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  font-weight: 800;
  color: var(--ink, #3a3a3a);
  text-decoration: none;
}
.sku-fold em {
  font-style: normal;
  font-size: 12px;
  color: #888;
  font-weight: 700;
}
.sku-fold__body {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid rgba(0, 0, 0, 0.04);
  font-size: 13px;
  max-height: 180px;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.sku-empty {
  padding: 40px 0;
  text-align: center;
  color: #888;
}
</style>
