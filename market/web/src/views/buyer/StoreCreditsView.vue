<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeCreditsBuyer, type CreditMerchant, type CreditOffer, type CreditPack } from '@/api/storeCreditsBuyer'
import PageHeader from '@/components/PageHeader.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { useAction } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { dateTime, money, usd } from '@/utils/format'
import { label } from '@/utils/labels'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const { busy, run } = useAction()
const merchants = ref<CreditMerchant[]>([])
const offers = ref<CreditOffer[]>([])
const packs = ref<CreditPack[]>([])
const offersLoading = ref(false)
const packsLoading = ref(false)
const offersError = ref(false)
const packsError = ref(false)
const selectedOffer = ref<CreditOffer | null>(null)
const purchaseOpen = ref(false)
// A failed response may follow a successful debit. Retain the key for a retry.
const purchaseKeys = new Map<number, string>()

const sellerId = computed(() => {
  const raw = route.query.seller_id
  const n = typeof raw === 'string' && /^\d+$/.test(raw) ? Number(raw) : 0
  return Number.isSafeInteger(n) && n > 0 ? n : 0
})
const sellerOptions = computed(() => {
  const ids = new Set(merchants.value.map(m => m.seller_id))
  if (sellerId.value) ids.add(sellerId.value)
  packs.value.forEach(p => ids.add(p.seller_id))
  return [...ids].sort((a, b) => a - b)
})
const activePacks = computed(() => packs.value.filter(p => ['active', 'provisioning'].includes(p.status)))
const statusText: Record<CreditPack['status'], string> = {
  provisioning: '开通中', active: '可使用', depleted: '已用完', refunding: '退款处理中', refunded: '已退款',
}
function scope(platforms: string[] = [], models: string[] = []) {
  const p = platforms.length ? platforms.map(x => label('platform', x).text).join('、') : '本店全部厂商'
  return `${p} · ${models.length ? models.join('、') : '全部模型'}`
}
function selectSeller(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  router.replace({ query: { ...route.query, seller_id: value || undefined } })
}
async function loadOffers() {
  const id = sellerId.value
  offers.value = []
  offersError.value = false
  if (!id) { offersLoading.value = false; return }
  offersLoading.value = true
  try {
    const result = await storeCreditsBuyer.offers(id)
    if (id === sellerId.value) offers.value = result ?? []
  } catch (e) {
    if (id === sellerId.value) { offersError.value = true; toast.error(e) }
  } finally {
    if (id === sellerId.value) offersLoading.value = false
  }
}
async function loadPacks() {
  packsLoading.value = true
  packsError.value = false
  try { packs.value = await storeCreditsBuyer.packs() ?? [] }
  catch (e) { packsError.value = true; toast.error(e) }
  finally { packsLoading.value = false }
}
function chooseOffer(offer: CreditOffer) {
  selectedOffer.value = offer
  purchaseOpen.value = true
}
async function purchase() {
  const offer = selectedOffer.value
  if (!offer) return
  let key = purchaseKeys.get(offer.id)
  if (!key) { key = crypto.randomUUID(); purchaseKeys.set(offer.id, key) }
  const result = await run(() => storeCreditsBuyer.purchase(offer.id, key!), '购买已受理，可在我的活动包查看状态')
  if (result) {
    purchaseKeys.delete(offer.id)
    purchaseOpen.value = false
    await loadPacks()
  }
}
function usedPercent(pack: CreditPack) {
  return Math.max(0, Math.min(100, (1 - pack.remaining_usd / pack.face_value_usd) * 100))
}
watch(sellerId, loadOffers, { immediate: true })
onMounted(async () => {
  await Promise.allSettled([
    loadPacks(),
    storeCreditsBuyer.merchants().then(items => { merchants.value = items ?? [] }).catch(e => toast.error(e)),
  ])
})
</script>

<template>
  <div class="store-credits">
    <PageHeader title="店铺活动额度包" description="人民币购买美元额度，仅限对应店铺；匹配的调用先扣活动额度，用完后自动扣平台余额。">
      <RouterLink to="/buyer/keys" class="xm-btn xm-btn-ghost">配置 API Key</RouterLink>
      <RouterLink to="/buyer/wallet" class="xm-btn xm-btn-primary">充值余额</RouterLink>
    </PageHeader>

    <section class="credit-summary" aria-label="使用规则">
      <div><span>我的可用活动包</span><strong>{{ activePacks.length }} <small>个</small></strong></div>
      <p>使用你已有的 API Key 调用对应店铺即可抵扣。不同店铺的额度分别保管，适用厂商与模型以购买时的规则为准。</p>
    </section>

    <section class="credit-section" aria-labelledby="credit-shop-heading">
      <div class="credit-section-head">
        <div><h2 id="credit-shop-heading">选购店铺活动</h2><p>从已加入的商家中选择，也可从市场商品详情进入。</p></div>
        <label class="credit-shop-select">店铺
          <select :value="sellerId || ''" class="input" aria-label="选择活动包店铺" @change="selectSeller">
            <option value="">请选择商家</option>
            <option v-for="id in sellerOptions" :key="id" :value="id">商家 #{{ id }}</option>
          </select>
        </label>
      </div>
      <p v-if="offersLoading" class="credit-empty" role="status">正在加载店铺活动…</p>
      <div v-else-if="offersError" class="credit-empty" role="alert"><p>店铺活动加载失败</p><button class="xm-btn xm-btn-ghost" @click="loadOffers">重试</button></div>
      <div v-else-if="!sellerId" class="credit-empty"><p>先选择一家店铺，查看它的活动额度包。</p><RouterLink to="/" class="xm-btn xm-btn-ghost">去市场挑商家</RouterLink></div>
      <p v-else-if="!offers.length" class="credit-empty">这家店铺暂未上架活动额度包，可继续按量使用。</p>
      <div v-else class="credit-grid">
        <article v-for="offer in offers" :key="offer.id" class="credit-offer">
          <div class="credit-offer-top"><span class="xm-tag pack">店铺专享</span><span>商家 #{{ offer.seller_id }}</span></div>
          <h3>{{ offer.title }}</h3>
          <p v-if="offer.description" class="credit-description">{{ offer.description }}</p>
          <div class="credit-price"><strong>{{ money(offer.price_cny) }}</strong><span>购买 <b>{{ usd(offer.face_value_usd) }}</b> 额度</span></div>
          <p class="credit-scope">{{ scope(offer.allowed_platforms, offer.allowed_models) }}</p>
          <button class="xm-btn xm-btn-primary" :disabled="busy" @click="chooseOffer(offer)">购买活动包</button>
        </article>
      </div>
    </section>

    <section class="credit-section" aria-labelledby="credit-owned-heading">
      <div class="credit-section-head"><div><h2 id="credit-owned-heading">我的活动包</h2><p>按店铺实际计费价抵扣，不足部分使用平台余额。</p></div><button class="xm-btn xm-btn-ghost" :disabled="packsLoading" @click="loadPacks">刷新</button></div>
      <p v-if="packsLoading && !packs.length" class="credit-empty" role="status">正在加载已购活动包…</p>
      <div v-else-if="packsError && !packs.length" class="credit-empty" role="alert"><p>已购活动包加载失败</p><button class="xm-btn xm-btn-ghost" @click="loadPacks">重试</button></div>
      <p v-else-if="!packs.length" class="credit-empty">还没有购买活动包。购买后可在这里查看额度与适用范围。</p>
      <div v-else class="credit-owned-list">
        <article v-for="pack in packs" :key="pack.id" class="credit-owned">
          <div class="credit-owned-main">
            <div class="credit-offer-top"><span>商家 #{{ pack.seller_id }} · 包 #{{ pack.id }}</span><span class="credit-status" :class="{ 'is-active': pack.status === 'active' }">{{ statusText[pack.status] || pack.status }}</span></div>
            <h3>{{ pack.title }}</h3>
            <div class="credit-remaining"><strong>{{ usd(pack.remaining_usd) }}</strong><span> / {{ usd(pack.face_value_usd) }} 剩余</span></div>
            <div class="credit-progress" role="progressbar" :aria-valuenow="Math.round(usedPercent(pack))" aria-valuemin="0" aria-valuemax="100" :aria-label="`${pack.title} 已用额度`"><span :style="{ width: `${usedPercent(pack)}%` }" /></div>
            <p class="credit-scope">{{ scope(pack.allowed_platforms, pack.allowed_models) }}</p>
            <p class="credit-meta">实付 {{ money(pack.price_cny) }} · 购买时每 $1 抵 {{ money(pack.usd_to_balance_rate) }} · {{ dateTime(pack.created_at) }}</p>
            <p v-if="pack.status === 'provisioning'" class="credit-meta">正在开通，稍后刷新查看。开通后自动抵扣符合范围的调用。</p>
            <p v-if="pack.status === 'depleted'" class="credit-meta">额度已用完，后续调用继续使用平台余额。</p>
          </div>
          <div class="credit-owned-actions">
            <button class="xm-btn xm-btn-ghost" @click="router.push({ query: { seller_id: String(pack.seller_id) } })">查看本店活动</button>
          </div>
        </article>
      </div>
    </section>

    <ModalDialog v-model:open="purchaseOpen" title="确认购买活动额度包">
      <template v-if="selectedOffer">
        <h3>{{ selectedOffer.title }}</h3>
        <p>从平台余额支付 <strong>{{ money(selectedOffer.price_cny) }}</strong>，获得 <strong>{{ usd(selectedOffer.face_value_usd) }}</strong> 店铺额度。</p>
        <p>仅限商家 #{{ selectedOffer.seller_id }}：{{ scope(selectedOffer.allowed_platforms, selectedOffer.allowed_models) }}。</p>
        <p class="hint">符合范围的调用优先扣此包，额度用完后自动扣平台余额。美元额度不能转到其他店铺；购买后不可退款。</p>
      </template>
      <template #footer><button class="btn-secondary" :disabled="busy" @click="purchaseOpen = false">取消</button><button class="btn-primary" :disabled="busy" @click="purchase">{{ busy ? '正在购买…' : '确认支付' }}</button></template>
    </ModalDialog>
  </div>
</template>

<style scoped>
.credit-summary{display:flex;align-items:center;gap:2rem;padding:1.4rem 1.6rem;background:#fff9dc;border:1px solid #e5d997;border-radius:16px;margin:1.25rem 0 2rem}.credit-summary>div{flex-shrink:0}.credit-summary span,.credit-summary p{font-size:13px;color:#6b6251}.credit-summary strong{display:block;font-size:30px;color:#343026}.credit-summary small{font-size:13px;font-weight:500}.credit-summary p{line-height:1.8;max-width:46rem}.credit-section{margin-top:2rem}.credit-section-head{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-bottom:1rem}.credit-section-head h2{font-size:18px;font-weight:750}.credit-section-head p{font-size:12px;color:#777;margin-top:5px}.credit-shop-select{display:flex;align-items:center;gap:10px;font-size:13px;white-space:nowrap}.credit-shop-select select{min-width:160px}.credit-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:1rem}.credit-offer,.credit-owned{background:#fff;border:1px solid #e8e5dd;border-radius:14px;padding:1.3rem}.credit-offer-top{display:flex;justify-content:space-between;align-items:center;gap:1rem;color:#777;font-size:11px}.credit-offer h3,.credit-owned h3{font-size:16px;font-weight:750;margin:14px 0 8px;overflow-wrap:anywhere}.credit-description{font-size:13px;line-height:1.7;color:#666;white-space:pre-wrap;overflow-wrap:anywhere}.credit-price{display:flex;flex-wrap:wrap;align-items:baseline;gap:10px;margin:1.2rem 0}.credit-price>strong{font-size:28px;color:#9d3429}.credit-price>span{font-size:13px;color:#555}.credit-price b{color:#353128}.credit-scope{font-size:12px;color:#666;line-height:1.8;overflow-wrap:anywhere;margin:12px 0}.credit-offer>.xm-btn{width:100%;margin-top:8px}.credit-empty{text-align:center;padding:2.5rem 1.5rem;background:#faf9f5;border:1px dashed #dcd8cd;border-radius:14px;color:#777;font-size:13px;line-height:1.8}.credit-empty .xm-btn{margin-top:12px}.credit-owned-list{display:grid;gap:12px}.credit-owned{display:flex;justify-content:space-between;gap:1.5rem}.credit-owned-main{min-width:0;flex:1}.credit-owned-actions{display:flex;flex-direction:column;justify-content:center;align-items:center;gap:12px;flex-shrink:0}.credit-status{background:#f1f1ed;padding:3px 8px;border-radius:5px;white-space:nowrap}.credit-status.is-active{background:#e9f4e6;color:#346c31}.credit-remaining strong{font-size:23px}.credit-remaining>span,.credit-meta{font-size:11px;color:#777;line-height:1.8}.credit-progress{height:5px;border-radius:4px;background:#eeece6;margin:12px 0;overflow:hidden;max-width:500px}.credit-progress>span{display:block;height:100%;background:#a29756}.credit-refund{font-size:12px;color:#8b3d32;text-decoration:underline;text-underline-offset:3px}.credit-refund:disabled{opacity:.5}.store-credits :deep(button:focus-visible),.store-credits :deep(a:focus-visible){outline:2px solid #655814;outline-offset:3px}@media(max-width:640px){.credit-summary{align-items:flex-start;flex-direction:column;gap:12px;padding:1.2rem}.credit-section-head{align-items:flex-start;flex-direction:column}.credit-shop-select,.credit-shop-select select{width:100%}.credit-owned{flex-direction:column}.credit-owned-actions{flex-direction:row;justify-content:flex-start;flex-wrap:wrap}.credit-grid{grid-template-columns:1fr}}
</style>
