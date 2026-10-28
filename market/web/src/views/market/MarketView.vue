<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { buyer, market } from '@/api'
import { merchants } from '@/api/merchants'
import { isTested } from '@/api/market-detection'
import type { MarketItem, PoolItem } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import MarketListingCard from '@/components/MarketListingCard.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import SearchSelect from '@/components/common/SearchSelect.vue'
import { useAuth } from '@/stores/auth'
import { useConfig } from '@/stores/config'
import { useLoginModal } from '@/stores/loginModal'
import { useMarketDiscovery } from '@/stores/marketDiscovery'
import { useToast } from '@/stores/toast'
import { copyText } from '@/utils/clipboard'
import { sampleRequest } from '@/utils/keySample'
import {
  apiToPlat,
  brandAction,
  brandIconFor,
  coverFor,
  platBrands,
  type FeedSku,
  type MarketAction,
  type PlatBrand,
} from '@/market/config'

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const loginModal = useLoginModal()
const toast = useToast()
const appCfg = useConfig()
const discoveryStore = useMarketDiscovery()
const cfg = computed(() => discoveryStore.cfg)
const openingId = ref('')
const created = ref<{ key: string; platform: string; title: string } | null>(null)
const edgeBase = computed(() => appCfg.config.edge_url || window.location.origin)
const createdSample = computed(() =>
  created.value ? sampleRequest(created.value.platform, edgeBase.value, created.value.key, appCfg.config.price_models ?? []) : null,
)
const heroQ = ref('')
const viewMode = ref<'grid' | 'list'>('grid')
const showAllBrands = ref(false)
const trendTab = ref<'hot' | 'price'>('hot')
const guideOpen = ref(false)

const GUIDE_STEPS = [
  {
    n: '01',
    title: '挑选商品',
    desc: '用左侧筛选或顶部搜索锁定模型，对比倍率、成功率与响应速度。',
  },
  {
    n: '02',
    title: '选用开通',
    desc: '点击「选用」登录后加入商家，平台会为你签发可调用的 API Key。',
  },
  {
    n: '03',
    title: '充值余额',
    desc: '按量通道按实际用量扣费；余额不足时先在钱包充值即可继续调用。',
  },
  {
    n: '04',
    title: '配置调用',
    desc: '把 Edge 地址与 API Key 填进你的客户端或代码，即可像官网一样调用。',
  },
  {
    n: '05',
    title: '查看用量',
    desc: '在买家中心查看用量统计与账单，订单与结算记录可随时追溯。',
  },
] as const

function openGuide() {
  guideOpen.value = true
}

function closeGuide() {
  guideOpen.value = false
  if (route.hash === '#guide') {
    router.replace({ path: route.path, query: route.query, hash: '' })
  }
}

function guideGoFeed() {
  closeGuide()
  requestAnimationFrame(() => document.getElementById('feed')?.scrollIntoView({ behavior: 'smooth' }))
}

function guideGoPublish() {
  closeGuide()
  goPublish()
}

function guideGoWallet() {
  closeGuide()
  if (!auth.loggedIn) {
    loginModal.show({ redirect: '/buyer/wallet' })
    return
  }
  router.push('/buyer/wallet')
}

const MODEL_TYPES = [
  { id: 'all', label: '大语言模型' },
  { id: 'channel', label: '多模态' },
  { id: 'pack', label: '图像生成' },
  { id: 'low_rate', label: '视频生成' },
  { id: 'coding', label: 'Embedding' },
  { id: 'pool', label: '其他' },
] as const

/** 设计稿提供方；映射到站内 plat / q */
const PROVIDERS = [
  { label: 'OpenAI', plat: 'ChatGPT' },
  { label: 'Anthropic', plat: 'Claude' },
  { label: 'Google', plat: 'Gemini' },
  { label: '阿里云', q: '通义' },
  { label: '腾讯云', q: '混元' },
  { label: '智谱AI', plat: '智谱' },
  { label: 'DeepSeek', q: 'DeepSeek' },
  { label: '其他', plat: '' },
] as const

const STOCK_BUCKETS = [
  { id: '', label: '不限' },
  { id: 'any', label: '1 亿以下' },
  { id: 'mid', label: '1 亿 – 10 亿' },
  { id: 'hi', label: '10 亿 – 50 亿' },
  { id: 'pack', label: '50 亿以上' },
] as const

async function copy(text: string) {
  if (await copyText(text)) toast.success('已复制')
  else toast.error('复制失败，请手动选择后按 Ctrl/⌘+C')
}

async function quickOpen(sku: FeedSku) {
  if (sku.kind === 'pack') return
  if (!auth.loggedIn) {
    loginModal.show({ redirect: '/' })
    return
  }
  if (openingId.value) return
  openingId.value = sku.id
  try {
    if (!sku.pool) {
      if (!sku.sellerId || !sku.listingId) throw new Error('商家信息暂不可用，请刷新后重试')
      await merchants.add(sku.sellerId)
      toast.success('已加入商家清单，请选择挂单并设置调用顺序')
      await router.push({ path: '/buyer/keys', query: { listing: sku.listingId } })
      return
    }
    const name = sku.title.slice(0, 64) || (sku.pool ? '平台保障池' : `卖家挂单 #${sku.listingId}`)
    const body = sku.poolName
      ? { name, route: { type: 'default' as const, pool: sku.poolName }, limits: {} }
      : { name, route: { type: 'chain' as const, hops: [{ listing_id: sku.listingId! }] }, limits: {} }
    const res = await buyer.createKey(body)
    created.value = {
      key: res.key,
      platform: res.detail.platform || sku.apiPlatform || '',
      title: sku.title,
    }
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '开通失败')
  } finally {
    openingId.value = ''
  }
}

const state = reactive({
  kind: 'all' as string,
  plat: '',
  grade: '',
  sort: 'default',
  q: '',
  tag: '',
  model: '',
  priceMin: '',
  priceMax: '',
  stock: '',
  feedTab: 'all' as 'all' | 'hot' | 'new',
})

const modelChoices = ref<string[]>([])
let modelRequest = 0
let lastModelItems: MarketItem[] | null = []
const listings = useLoader(async () => {
  const serial = ++modelRequest
  try {
    const items = await market.listings({ sort: 'price', limit: '200', ...(state.model ? { model: state.model } : {}) })
    if (serial !== modelRequest) return lastModelItems
    lastModelItems = items
    modelChoices.value = [...new Set([...modelChoices.value, ...(items ?? []).flatMap((it) => it.models ?? [])])].sort()
    return items
  } catch (e) {
    if (serial !== modelRequest) return lastModelItems
    throw e
  }
}, [] as MarketItem[] | null)
const pools = useLoader(() => market.pools(), [] as PoolItem[] | null)

function syncFromRoute() {
  state.kind = typeof route.query.kind === 'string' ? route.query.kind : 'all'
  state.plat = typeof route.query.plat === 'string' ? route.query.plat : ''
  state.tag = typeof route.query.tag === 'string' ? route.query.tag : ''
  state.q = typeof route.query.q === 'string' ? route.query.q : ''
  heroQ.value = state.q
  state.grade = typeof route.query.grade === 'string' ? route.query.grade : state.grade
  const model = typeof route.query.model === 'string' ? route.query.model.trim() : ''
  state.model = model
}

function applyAction(action: MarketAction) {
  if (action.href) {
    router.push(action.href)
    return
  }
  const query: Record<string, string> = {}
  if (action.kindTab && action.kindTab !== 'all') query.kind = action.kindTab
  if (action.plat !== undefined && action.plat !== '') query.plat = action.plat
  if (action.tag) query.tag = action.tag
  if (action.q) query.q = action.q
  router.push({ path: '/', query })
  document.getElementById('feed')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function setKind(kind: string) {
  const query = { ...route.query } as Record<string, string>
  if (kind && kind !== 'all') query.kind = kind
  else delete query.kind
  delete query.tag
  delete query.plat
  router.replace({ query })
}

/** 模型类型：设计稿文案；行为映射到现有 kind/tag/plat */
function setModelType(id: string) {
  if (id === 'all') {
    setKind('all')
    return
  }
  if (id === 'channel') {
    setKind('channel')
    return
  }
  if (id === 'pack') {
    setKind('pack')
    return
  }
  if (id === 'pool') {
    const query = { ...route.query } as Record<string, string>
    delete query.kind
    delete query.tag
    query.plat = '保障池'
    router.replace({ query })
    return
  }
  const query = { ...route.query } as Record<string, string>
  delete query.kind
  delete query.plat
  query.tag = id
  router.replace({ query })
}

function modelTypeOn(id: string) {
  if (id === 'all') return (!state.kind || state.kind === 'all') && !state.tag && state.plat !== '保障池'
  if (id === 'channel' || id === 'pack') return state.kind === id && !state.tag
  if (id === 'pool') return state.plat === '保障池'
  return state.tag === id
}

function providerOn(p: { label: string; plat?: string; q?: string }) {
  if (p.plat) return state.plat === p.plat
  if (p.q) return state.q === p.q
  return false
}

function toggleProvider(p: { label: string; plat?: string; q?: string }) {
  if (providerOn(p) && (p.plat || p.q)) {
    const query = { ...route.query } as Record<string, string>
    delete query.plat
    delete query.q
    router.replace({ query })
    return
  }
  if (p.plat) applyAction({ plat: p.plat })
  else if (p.q) applyAction({ q: p.q })
  else resetFilters()
}

function heroSearch() {
  const q = heroQ.value.trim()
  const query = { ...route.query } as Record<string, string>
  if (q) query.q = q
  else delete query.q
  router.push({ path: '/', query })
  document.getElementById('feed')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

const modelOptions = computed(() =>
  modelChoices.value.map((model) => ({ value: model, text: model })),
)

function setModel(event: Event) {
  const model = (event.target as HTMLInputElement | HTMLSelectElement).value.trim()
  applyModel(model)
}

function onModelPick(value: string | string[]) {
  applyModel(typeof value === 'string' ? value.trim() : '')
}

function applyModel(model: string) {
  const query = { ...route.query }
  if (model) query.model = model
  else delete query.model
  router.replace({ query })
}

function scrollToFilters() {
  document.getElementById('market-filters')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function resetFilters() {
  state.grade = ''
  state.sort = 'default'
  state.priceMin = ''
  state.priceMax = ''
  state.stock = ''
  state.feedTab = 'all'
  router.replace({ path: '/', query: {} })
}

function goPublish() {
  if (auth.loggedIn) router.push('/seller/publish')
  else loginModal.show({ redirect: '/seller/publish' })
}

function toSku(it: MarketItem): FeedSku {
  const plat = apiToPlat[it.platform] || it.platform
  const isPack = it.sku_kind === 'pack' || it.mode === 'pack'
  const stockLeft = Math.max(0, (it.stock ?? 0) - (it.stock_sold ?? 0))
  const cats = it.categories ?? []
  return {
    id: `l-${it.listing_id}`,
    listingId: it.listing_id,
    sellerId: it.seller_id,
    detectionStatus: it.detection_status,
    modelChecks: it.model_checks,
    models: it.models ?? [],
    apiPlatform: it.platform,
    kind: isPack ? 'pack' : 'channel',
    plat,
    tier: it.tier || '',
    title: it.title || `${plat}${it.tier ? ` · ${it.tier}` : ''} · ${isPack ? '订阅套餐' : '按量通道'}`,
    img: coverFor(plat, it.listing_id),
    mult: it.price,
    min: it.min_amount ?? 0,
    face: it.face_value,
    stock: stockLeft,
    grade: isTested(it.detection_status) ? it.grade || '' : '',
    success: it.success_rate_24h ?? 0,
    ttft: Math.round(it.ttft_p50_ms ?? 0),
    badge: isPack ? '订阅套餐' : cats.includes('low_rate') || it.price <= 0.55 ? '低倍率' : undefined,
    categories: cats,
    inspectionMode: it.inspection_mode,
    qualityChecks: it.quality_checks,
    lastCheckedAt: it.last_checked_at,
  }
}

function toPoolSku(p: PoolItem): FeedSku {
  const plat = apiToPlat[p.platform] || (p.platform === 'composite' ? '保障池' : p.platform)
  const base = p.name.replace(/^tm-pool-/, '') || p.name
  return {
    id: `pool-${p.id || p.name}`,
    poolName: p.name,
    apiPlatform: p.platform,
    kind: 'channel',
    plat,
    tier: '',
    title: p.market_title || `${base} · 平台保障`,
    desc: p.market_description || undefined,
    img: coverFor(plat === '保障池' ? 'Claude' : plat, p.id || p.name),
    mult: p.price,
    min: 0,
    grade: 'S',
    success: 0.99,
    ttft: 320,
    pool: true,
    badge: '平台官方',
  }
}

const codingPlats = new Set(['Kimi', '智谱', 'MiniMax', 'OpenCode'])

function hasCat(s: FeedSku, id: string) {
  return (s.categories ?? []).includes(id)
}

const allSkus = computed(() => {
  const listingSkus = (listings.data.value ?? []).map(toSku)
  const poolSkus = (pools.data.value ?? []).filter((p) => p.market_published).map(toPoolSku)
  return [...poolSkus, ...listingSkus]
})

const popularBrands = computed(() => {
  const preferred = ['OpenAI', 'Claude', 'Gemini', 'DeepSeek', 'Qwen', 'Llama', '智谱', 'Mistral', '可灵']
  const byLabel = new Map(platBrands.map((b) => [b.label, b]))
  const picks: (PlatBrand & { count: number })[] = []
  for (const label of preferred) {
    const b = byLabel.get(label) || platBrands.find((x) => x.plat === label)
    if (b) {
      picks.push({
        ...b,
        count: allSkus.value.filter(
          (s) => s.plat === b.plat || (b.q && s.title.toLowerCase().includes(b.q.toLowerCase())),
        ).length,
      })
    }
  }
  return picks
})

const brandWall = computed(() => {
  if (showAllBrands.value) {
    const counts = new Map<string, number>()
    for (const s of allSkus.value) {
      if (!s.plat || s.plat === '保障池') continue
      counts.set(s.plat, (counts.get(s.plat) || 0) + 1)
    }
    return [...platBrands]
      .map((b) => ({ ...b, count: b.plat ? counts.get(b.plat) || 0 : 0 }))
      .slice(0, 24)
  }
  return popularBrands.value
})

function brandIsOn(b: { plat: string; q?: string; label: string }) {
  if (b.plat && state.plat === b.plat) return true
  const qq = b.q || (!b.plat ? b.label : '')
  return !!qq && state.q === qq
}

function pickBrand(b: { plat: string; q?: string; label: string }) {
  applyAction(brandAction(b))
}

const feedSkus = computed(() => {
  let list = [...allSkus.value]
  if (state.model) list = list.filter((s) => !s.pool && (!s.models?.length || s.models.includes(state.model)))
  if (state.kind === 'pack' || state.kind === 'channel') list = list.filter((s) => s.kind === state.kind)
  if (state.plat === '保障池') list = list.filter((s) => s.pool || s.plat === '保障池')
  else if (state.plat) list = list.filter((s) => s.plat === state.plat)
  if (state.grade) list = list.filter((s) => s.grade === state.grade)
  if (state.tag === 'low_rate') list = list.filter((s) => hasCat(s, 'low_rate') || (s.kind === 'channel' && s.mult <= 0.55))
  else if (state.tag === 'grade_s') list = list.filter((s) => s.grade === 'S')
  else if (state.tag === 'coding') list = list.filter((s) => hasCat(s, 'coding') || codingPlats.has(s.plat))
  else if (state.tag === 'refundable' || state.tag === 'pack') list = list.filter((s) => s.kind === 'pack')
  else if (state.tag === 'consignment') list = list.filter((s) => s.kind === 'channel' && !s.pool)
  else if (state.tag) list = list.filter((s) => hasCat(s, state.tag))
  if (state.q) {
    const q = state.q.toLowerCase()
    list = list.filter((s) => (s.plat + ' ' + s.tier + ' ' + s.title + ' ' + (s.desc ?? '')).toLowerCase().includes(q))
  }
  const min = Number(state.priceMin)
  const max = Number(state.priceMax)
  if (Number.isFinite(min) && state.priceMin !== '') {
    list = list.filter((s) => (s.kind === 'pack' ? (s.face ?? 0) : s.mult) >= min)
  }
  if (Number.isFinite(max) && state.priceMax !== '') {
    list = list.filter((s) => (s.kind === 'pack' ? (s.face ?? 0) : s.mult) <= max)
  }
  if (state.stock === 'any' || state.stock === 'mid' || state.stock === 'hi') {
    list = list.filter((s) => s.pool || (s.stock ?? 0) > 0 || s.kind === 'channel')
  }
  if (state.stock === 'pack') list = list.filter((s) => s.kind === 'pack' && (s.stock ?? 0) > 0)

  if (state.feedTab === 'hot') {
    list = list.filter((s) => s.pool || s.success >= 0.95 || s.mult <= 0.6 || s.badge)
    list.sort((a, b) => b.success - a.success || a.mult - b.mult)
  } else if (state.feedTab === 'new') {
    list = [...list].reverse()
  } else if (state.sort === 'price') {
    list.sort((a, b) => (a.kind === 'pack' ? (a.face ?? 0) : a.mult) - (b.kind === 'pack' ? (b.face ?? 0) : b.mult))
  } else if (state.sort === 'price_desc') {
    list.sort((a, b) => (b.kind === 'pack' ? (b.face ?? 0) : b.mult) - (a.kind === 'pack' ? (a.face ?? 0) : a.mult))
  } else if (state.sort === 'success') list.sort((a, b) => b.success - a.success)
  else if (state.sort === 'ttft') list.sort((a, b) => a.ttft - b.ttft)
  else if (cfg.value.feed.pin_pool_first) list.sort((a, b) => Number(!!b.pool) - Number(!!a.pool))
  return list
})

const marketTrends = computed(() => {
  const channels = [...allSkus.value]
    .filter((s) => s.kind === 'channel')
    .sort((a, b) => (trendTab.value === 'price' ? a.mult - b.mult : b.success - a.success))
    .slice(0, 5)
  return channels.map((s, i) => {
    const save = s.mult > 0 && s.mult < 1 ? -Math.round((1 - s.mult) * 100) : 0
    return {
      rank: i + 1,
      name: s.title.replace(/·.*$/, '').trim() || s.plat,
      plat: s.plat,
      icon: brandIconFor(s.plat) || s.img,
      price: s.mult.toFixed(2),
      change: save || -Math.max(1, Math.round(s.success * 20)),
    }
  })
})

onMounted(() => {
  syncFromRoute()
  listings.load()
  pools.load()
  void discoveryStore.ensure()
})

watch(
  () => route.hash,
  (hash) => {
    if (hash !== '#guide') return
    guideOpen.value = true
    requestAnimationFrame(() => document.getElementById('guide')?.scrollIntoView({ behavior: 'smooth' }))
  },
  { immediate: true },
)

watch(
  () => route.query,
  () => {
    const previousModel = state.model
    syncFromRoute()
    if (previousModel !== state.model) void listings.load()
  },
)
</script>

<template>
  <div class="page market-home">
    <!-- 黄底通栏包裹卖点卡；下方左主列 | 右资讯 -->
    <div class="mh-shell">
      <section class="mh-hero" aria-label="市场主视觉">
        <div class="mh-hero-main">
          <div class="mh-hero-copy">
            <span class="mh-hero-pill">闲置 Token 交易市场</span>
            <h1>别让你的<em>Token</em>落了灰</h1>
            <p class="mh-hero-lead">闲猫连接供需双方，提供安全、便捷、透明的 AI Token 交易服务</p>
            <div class="mh-hero-search-row">
              <form class="mh-hero-search" @submit.prevent="heroSearch">
                <span class="mh-hero-search-ico" aria-hidden="true">
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                    <circle cx="7" cy="7" r="4.5" stroke="currentColor" stroke-width="1.6" />
                    <path d="M10.5 10.5L14 14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
                  </svg>
                </span>
                <input
                  v-model="heroQ"
                  type="search"
                  placeholder="搜索你需要的模型，例如：GPT-4o、Claude、通义千问…"
                  aria-label="搜索商品"
                />
                <button type="submit" class="mh-hero-go">搜索</button>
              </form>
              <button type="button" class="mh-hero-adv" @click="scrollToFilters">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                  <path d="M2.5 3h11l-4 4.5V13l-3-1.5V7.5L2.5 3Z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" />
                </svg>
                高级筛选
              </button>
            </div>
          </div>

          <div class="mh-hero-art" aria-hidden="true">
            <img
              class="mh-hero-art-img"
              src="/assets/hero-api-token-v2.png"
              alt=""
              width="1024"
              height="587"
              decoding="async"
            />
          </div>
        </div>

        <div class="mh-hero-side">
          <ul class="mh-hero-points">
            <li>
              <span class="ico ico-price" aria-hidden="true">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 2v12M5.5 5.5c0-1.2 1.1-2 2.5-2s2.5.8 2.5 2-1 1.7-2.5 2-2.5.9-2.5 2 1.1 2 2.5 2 2.5-.8 2.5-2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
              </span>
              <div>
                <strong>价格更优</strong>
                <span>比官网更低，最高省 50%</span>
              </div>
            </li>
            <li>
              <span class="ico ico-safe" aria-hidden="true">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 2l5 2v4.5c0 3-2.2 4.8-5 5.5-2.8-.7-5-2.5-5-5.5V4l5-2Z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M6 8l1.5 1.5L10.5 6.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>
              </span>
              <div>
                <strong>交易安全</strong>
                <span>平台担保，资金有保障</span>
              </div>
            </li>
            <li>
              <span class="ico ico-fast" aria-hidden="true">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M9 2L4 9h4l-1 5 5-7H8l1-5Z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></svg>
              </span>
              <div>
                <strong>即时到账</strong>
                <span>下单后立即生效</span>
              </div>
            </li>
          </ul>
          <div class="mh-hero-sell">
            <div>
              <strong>想出售闲置 Token？</strong>
              <span>快速发布，轻松变现</span>
            </div>
            <button type="button" class="mh-cta" @click="goPublish">立即发布</button>
          </div>
        </div>

        <svg class="mh-hero-wave" viewBox="0 0 1440 80" preserveAspectRatio="none" aria-hidden="true">
          <path
            d="M0 36C160 72 320 8 480 40C640 72 800 12 960 44C1120 76 1280 16 1440 48V80H0V36Z"
            fill="#fafbfc"
          />
        </svg>
      </section>

      <div class="mh-body">
      <div class="mh-primary">
        <div class="mh-board">
        <section class="mh-popular-wrap" aria-label="热门模型">
          <div class="mh-section-head">
            <h2>热门模型</h2>
          </div>
          <div class="mh-popular">
            <button
              v-for="b in brandWall"
              :key="b.icon + b.label"
              type="button"
              class="mh-pop"
              :class="{ on: brandIsOn(b) }"
              :title="b.label"
              @click="pickBrand(b)"
            >
              <span class="mh-pop-ico"><img :src="b.icon" alt="" loading="lazy" /></span>
              <span class="mh-pop-name">{{ b.label }}</span>
            </button>
            <button type="button" class="mh-pop mh-pop-more" @click="showAllBrands = !showAllBrands">
              <span class="mh-pop-ico more">{{ showAllBrands ? '−' : '+' }}</span>
              <span class="mh-pop-name">{{ showAllBrands ? '收起' : '更多' }}</span>
            </button>
          </div>
        </section>

        <div class="mh-layout">
          <aside id="market-filters" class="mh-side mh-filters" aria-label="筛选条件">
            <h2 class="mh-filter-title">筛选条件</h2>
            <div class="mh-filter-block">
              <h3>模型类型</h3>
              <label v-for="t in MODEL_TYPES" :key="t.id" class="mh-check">
                <input type="checkbox" :checked="modelTypeOn(t.id)" @change="setModelType(t.id)" />
                <span>{{ t.label }}</span>
              </label>
            </div>
            <div class="mh-filter-block">
              <h3>模型提供方</h3>
              <label v-for="p in PROVIDERS" :key="p.label" class="mh-check">
                <input type="checkbox" :checked="providerOn(p)" @change="toggleProvider(p)" />
                <span>{{ p.label }}</span>
              </label>
            </div>
            <div class="mh-filter-block">
              <h3>价格区间</h3>
              <div class="mh-price-row">
                <input v-model="state.priceMin" type="number" step="0.01" min="0" placeholder="¥0" aria-label="最低价" />
                <span>—</span>
                <input v-model="state.priceMax" type="number" step="0.01" min="0" placeholder="¥1000" aria-label="最高价" />
              </div>
              <input
                class="mh-price-slider"
                type="range"
                min="0"
                max="2"
                step="0.05"
                :value="state.priceMax || 1"
                aria-label="价格滑块"
                @input="state.priceMax = String(($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="mh-filter-block">
              <h3>可用额度</h3>
              <label v-for="s in STOCK_BUCKETS" :key="s.id || 'any-all'" class="mh-check">
                <input v-model="state.stock" type="radio" name="stock" :value="s.id" />
                <span>{{ s.label }}</span>
              </label>
            </div>
            <div class="mh-filter-block">
              <h3>挂售模型</h3>
              <input
                id="f-model"
                class="sr-only"
                :value="state.model"
                list="market-models"
                aria-hidden="true"
                tabindex="-1"
                @change="setModel"
              />
              <datalist id="market-models">
                <option v-for="model in modelChoices" :key="model" :value="model" />
              </datalist>
              <SearchSelect
                id="f-model-select"
                :model-value="state.model"
                :options="modelOptions"
                allow-empty
                creatable
                placeholder="搜索或选择模型"
                @update:model-value="onModelPick"
              />
            </div>
            <button type="button" class="mh-reset" @click="resetFilters">重置</button>
          </aside>

          <section id="feed" class="mh-main" aria-label="商品列表">
            <div class="mh-toolbar">
              <div class="mh-tabs" role="tablist">
                <button type="button" class="mh-tab" :class="{ on: state.feedTab === 'all' }" @click="state.feedTab = 'all'">全部商品</button>
                <button type="button" class="mh-tab" :class="{ on: state.feedTab === 'hot' }" @click="state.feedTab = 'hot'">热门推荐</button>
                <button type="button" class="mh-tab" :class="{ on: state.feedTab === 'new' }" @click="state.feedTab = 'new'">最新上架</button>
              </div>
              <div class="mh-tools">
                <span class="mh-count">{{ feedSkus.length }} 个</span>
                <select v-model="state.sort" class="mh-select" aria-label="排序">
                  <option value="default">默认排序</option>
                  <option value="price">价格从低到高</option>
                  <option value="price_desc">价格从高到低</option>
                  <option value="success">服务质量优先</option>
                  <option value="ttft">响应更快</option>
                </select>
                <div class="mh-view-toggle" role="group" aria-label="视图">
                  <button type="button" :class="{ on: viewMode === 'grid' }" aria-label="网格" @click="viewMode = 'grid'">▦</button>
                  <button type="button" :class="{ on: viewMode === 'list' }" aria-label="列表" @click="viewMode = 'list'">☰</button>
                </div>
              </div>
            </div>

            <div class="mh-feed" :class="{ 'is-list': viewMode === 'list' }">
              <MarketListingCard
                v-for="sku in feedSkus"
                :key="sku.id"
                :sku="sku"
                :busy="openingId === sku.id"
                @quick-open="quickOpen"
              />
              <div v-if="!feedSkus.length && !listings.loading.value" class="feed-empty">
                暂无在售闲置 TOKEN。
                <button type="button" class="mh-linkish" @click="goPublish">去发布闲置 TOKEN</button>
              </div>
              <div v-else-if="!feedSkus.length" class="feed-empty">加载中…</div>
            </div>
          </section>
        </div>
        </div>
      </div>

      <aside class="mh-rail" aria-label="市场资讯">
        <div class="mh-widget mh-widget-trends">
          <div class="mh-trend-head">
            <h3>市场行情</h3>
            <a class="mh-more" href="#feed">更多 ›</a>
          </div>
          <div class="mh-trend-tabs">
            <button type="button" :class="{ on: trendTab === 'hot' }" @click="trendTab = 'hot'">热门模型</button>
            <button type="button" :class="{ on: trendTab === 'price' }" @click="trendTab = 'price'">价格趋势</button>
          </div>
          <ol class="mh-trends">
            <li v-for="t in marketTrends" :key="t.rank">
              <span class="rank" :class="'r' + t.rank">{{ t.rank }}</span>
              <span class="tico"><img :src="t.icon.startsWith('/') || t.icon.startsWith('http') ? t.icon : `/assets/${t.icon}`" alt="" /></span>
              <span class="name">{{ t.name }}</span>
              <span class="px">×{{ t.price }}</span>
              <span class="chg" :class="t.change <= 0 ? 'down' : 'up'">
                {{ t.change <= 0 ? '↓' : '↑' }}{{ Math.abs(t.change) }}%
              </span>
            </li>
            <li v-if="!marketTrends.length" class="empty">行情加载中…</li>
          </ol>
        </div>

        <div id="guide" class="mh-widget mh-guide">
          <div class="mh-guide-copy">
            <strong>新手入门指南</strong>
            <p>3 分钟了解如何选购与使用 Token</p>
            <button type="button" class="mh-guide-btn" @click="openGuide">查看教程</button>
          </div>
          <div class="mh-guide-art" aria-hidden="true">
            <img class="mh-guide-art-img" src="/assets/guide-ai-coins.png" alt="" />
          </div>
        </div>

        <div class="mh-widget mh-guarantee">
          <h3>平台保障</h3>
          <ul>
            <li>
              <span class="g-ico" aria-hidden="true">保</span>
              <div>
                <strong>交易保障</strong>
                <span>订单、用量与结算记录可查询</span>
              </div>
            </li>
            <li>
              <span class="g-ico" aria-hidden="true">服</span>
              <div>
                <strong>服务支持</strong>
                <span>问题反馈与工单处理入口</span>
              </div>
            </li>
            <li>
              <span class="g-ico" aria-hidden="true">透</span>
              <div>
                <strong>用量透明</strong>
                <span>可查看用量统计与费用记录</span>
              </div>
            </li>
          </ul>
        </div>
      </aside>
      </div>
    </div>

    <ModalDialog :open="!!created" title="Key 已创建" @update:open="(v: boolean) => !v && (created = null)">
      <p class="text-sm text-emerald-700">「{{ created?.title }}」已开通，可立即调用。</p>
      <p class="text-sm text-amber-700">稍后也可以在买家中心再次查看和复制这个 Key。</p>
      <div class="flex items-center gap-2">
        <code class="flex-1 break-all rounded-xl border border-black/10 !bg-[#fafafa] !px-3 !py-2 text-sm">{{ created?.key }}</code>
        <button type="button" class="btn-secondary btn-sm" @click="copy(created!.key)">复制</button>
      </div>
      <div v-if="createdSample">
        <div class="label">调用示例 · {{ createdSample.label }}</div>
        <pre class="codeblock">{{ createdSample.code }}</pre>
        <button type="button" class="btn-secondary btn-sm" @click="copy(createdSample.code)">复制示例</button>
        <p class="hint">{{ createdSample.note }}</p>
      </div>
      <template #footer>
        <RouterLink class="btn-secondary" to="/buyer/keys" @click="created = null">去买家中心</RouterLink>
        <button type="button" class="btn-primary" @click="created = null">知道了</button>
      </template>
    </ModalDialog>

    <ModalDialog :open="guideOpen" title="新手入门指南" size="wide" @update:open="(v: boolean) => !v && closeGuide()">
      <div class="mh-guide-dialog">
        <p class="mh-guide-dialog-lead">从挑商品到发出第一次调用，按这 5 步走一遍即可。</p>
        <ol class="mh-guide-steps">
          <li v-for="step in GUIDE_STEPS" :key="step.n" class="mh-guide-step">
            <span class="mh-guide-step-n" aria-hidden="true">{{ step.n }}</span>
            <div class="mh-guide-step-body">
              <strong>{{ step.title }}</strong>
              <p>{{ step.desc }}</p>
            </div>
          </li>
        </ol>
        <div class="mh-guide-tips">
          <div class="mh-guide-tip">
            <strong>买家提示</strong>
            <span>选用后 Key 可在「我的订单 / API Key」随时复制；平台担保交易，用量与结算可查。</span>
          </div>
          <div class="mh-guide-tip">
            <strong>卖家提示</strong>
            <span>有闲置额度？点「发布」上架按量通道或订阅套餐，检测合格后可进入保障池。</span>
          </div>
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn-secondary" @click="guideGoPublish">去发布</button>
        <button type="button" class="btn-secondary" @click="guideGoWallet">去充值</button>
        <button type="button" class="btn-primary" @click="guideGoFeed">去逛市场</button>
      </template>
    </ModalDialog>
  </div>
</template>
