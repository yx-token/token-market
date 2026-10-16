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
  coverFor,
  platBrands,
  type FeedSku,
  type MarketAction,
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
const hotPills = computed(() => (discoveryStore.hotSearches ?? []).slice(0, 4))

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

function setPill(id: string) {
  if (id === 'all' || id === 'channel' || id === 'pack') {
    setKind(id)
    return
  }
  const query = { ...route.query } as Record<string, string>
  delete query.kind
  if (id === 'pool') {
    query.plat = '保障池'
    delete query.tag
  } else {
    query.tag = id
    delete query.plat
  }
  router.replace({ query })
}

function pillOn(id: string) {
  if (id === 'all') {
    return (!state.kind || state.kind === 'all') && !state.tag && !state.plat
  }
  if (id === 'channel' || id === 'pack') {
    return state.kind === id && !state.tag
  }
  if (id === 'pool') return state.plat === '保障池'
  return state.tag === id
}

function setModel(event: Event) {
  const model = (event.target as HTMLInputElement).value.trim()
  const query = { ...route.query }
  if (model) query.model = model
  else delete query.model
  router.replace({ query })
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

/** 信息条图标墙：完整品牌清单；有货的排前面 */
const infoBrands = computed(() => {
  const counts = new Map<string, number>()
  for (const s of allSkus.value) {
    if (!s.plat || s.plat === '保障池') continue
    counts.set(s.plat, (counts.get(s.plat) || 0) + 1)
    if (s.models?.length) {
      for (const m of s.models) counts.set(m, (counts.get(m) || 0) + 1)
    }
  }
  const score = (b: (typeof platBrands)[number]) => {
    if (b.plat && counts.has(b.plat)) return counts.get(b.plat)!
    if (b.q) {
      const q = b.q.toLowerCase()
      let n = 0
      for (const [k, v] of counts) if (k.toLowerCase().includes(q)) n += v
      return n
    }
    return 0
  }
  return [...platBrands]
    .map((b) => ({ ...b, count: score(b) }))
    .sort((a, b) => b.count - a.count || 0)
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
  if (state.sort === 'price') list.sort((a, b) => (a.kind === 'pack' ? (a.face ?? 0) : a.mult) - (b.kind === 'pack' ? (b.face ?? 0) : b.mult))
  else if (state.sort === 'success') list.sort((a, b) => b.success - a.success)
  else if (state.sort === 'ttft') list.sort((a, b) => a.ttft - b.ttft)
  else if (cfg.value.feed.pin_pool_first) list.sort((a, b) => Number(!!b.pool) - Number(!!a.pool))
  return list
})

function labelOn(action: MarketAction) {
  if (action.href) return false
  if (action.plat !== undefined && !action.tag && !action.q && !action.kindTab) {
    return (action.plat || '') === (state.plat || '') && !state.tag && !state.q
  }
  if (action.tag && action.plat === undefined && !action.q) return state.tag === action.tag
  if (action.kindTab) return state.kind === action.kindTab
  return false
}

onMounted(() => {
  syncFromRoute()
  listings.load()
  pools.load()
  void discoveryStore.ensure()
})

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
  <div class="page lib-page">
    <section class="info-strip" aria-label="模型品牌">
      <div class="info-brand-wall" role="list" aria-label="全球模型品牌">
        <a
          v-for="b in infoBrands"
          :key="b.icon + b.label"
          class="info-brand"
          :class="{ on: brandIsOn(b), live: b.count > 0 }"
          role="listitem"
          href="#feed"
          :title="b.count ? `${b.label} · ${b.count} 个在售` : b.label"
          @click.prevent="pickBrand(b)"
        >
          <span class="info-brand-pad">
            <img :src="b.icon" alt="" width="26" height="26" loading="lazy" />
          </span>
          <span class="info-brand-name">{{ b.label }}</span>
        </a>
      </div>
    </section>

    <div id="feed" class="filter-bar">
      <div class="pills" role="tablist" aria-label="筛选">
        <button type="button" class="pill" :class="{ on: pillOn('all') }" @click="setPill('all')">全部</button>
        <button type="button" class="pill" :class="{ on: pillOn('channel') }" @click="setPill('channel')">按量通道</button>
        <button type="button" class="pill" :class="{ on: pillOn('pack') }" @click="setPill('pack')">订阅套餐</button>
        <button
          v-for="h in hotPills"
          :key="h.id"
          type="button"
          class="pill"
          :class="{ on: labelOn(h.action) }"
          @click="applyAction(h.action)"
        >{{ h.label }}</button>
      </div>
      <div class="filter-tools">
        <span class="feed-count">{{ feedSkus.length }} 个</span>
        <input
          id="f-model"
          :value="state.model"
          list="market-models"
          placeholder="模型"
          class="tool-input"
          aria-label="模型"
          @change="setModel"
        />
        <datalist id="market-models"><option v-for="model in modelChoices" :key="model" :value="model" /></datalist>
        <select id="f-grade" v-model="state.grade" class="tool-select" aria-label="等级">
          <option value="">等级</option>
          <option value="S">S</option>
          <option value="A">A</option>
          <option value="B">B</option>
          <option value="C">C</option>
        </select>
        <select id="f-sort" v-model="state.sort" class="tool-select" aria-label="排序">
          <option value="default">综合</option>
          <option value="price">倍率/价格</option>
          <option value="success">成功率</option>
          <option value="ttft">更快</option>
        </select>
      </div>
    </div>

    <div class="feed lib-feed">
      <MarketListingCard
        v-for="sku in feedSkus"
        :key="sku.id"
        :sku="sku"
        :busy="openingId === sku.id"
        @quick-open="quickOpen"
      />
      <div v-if="!feedSkus.length && !listings.loading.value" class="feed-empty">
        暂无在售闲置TOKEN。
        <RouterLink to="/seller/publish" class="ml-1 font-bold underline underline-offset-2">去发布闲置TOKEN</RouterLink>
      </div>
      <div v-else-if="!feedSkus.length" class="feed-empty">加载中…</div>
    </div>

    <p class="note-wrap">
      <span class="note">
        卖家完成授权后可直接上架。未检测挂单会明确标注，挑好商家后加入清单配置 Key。
        <RouterLink to="/seller/publish">发布闲置TOKEN</RouterLink>
      </span>
    </p>

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
  </div>
</template>
