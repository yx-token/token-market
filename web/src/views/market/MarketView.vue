<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { buyer, market } from '@/api'
import type { MarketCategory, MarketItem, PoolItem, RecommendResult } from '@/api/types'
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
  buildCategoryNavGroups,
  catIconSvg,
  coverFor,
  tileThemeIcon,
  type FeedSku,
  type MarketAction,
} from '@/market/config'

type TileThumb = { key: string; img: string; price: string; href?: string }
type DisplayTile = {
  id: string
  title: string
  desc: string
  eng: string
  theme: string
  action: MarketAction
}

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const loginModal = useLoginModal()
const toast = useToast()
const appCfg = useConfig()
const discoveryStore = useMarketDiscovery()
const recommend = ref<RecommendResult | null>(null)
const catTree = ref<MarketCategory[]>([])
const cfg = computed(() => discoveryStore.cfg)
const banner = computed(() => cfg.value.slots.find((s) => s.id === 'banner' && s.enabled !== false))
const openingId = ref('')
const created = ref<{ key: string; platform: string; title: string } | null>(null)
const edgeBase = computed(() => appCfg.config.edge_url || window.location.origin)
const createdSample = computed(() =>
  created.value ? sampleRequest(created.value.platform, edgeBase.value, created.value.key, appCfg.config.price_models ?? []) : null,
)

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

/** 侧栏类目：分类树切成多组飞层，每行 3 个快捷入口 */
const groups = computed(() => buildCategoryNavGroups(catTree.value))

/** 四宫格：优先推荐接口；不足时用运营位默认补齐到 4 格 */
const tiles = computed((): DisplayTile[] => {
  const fromRec = (recommend.value?.tiles ?? []).map((t) => ({
    id: t.id,
    title: t.title,
    desc: t.desc,
    eng: t.eng,
    theme: t.theme || 'deal',
    action: (t.action || {}) as MarketAction,
  }))
  if (fromRec.length >= 4) return fromRec.slice(0, 4)
  const seen = new Set(fromRec.map((t) => t.id))
  const extras: DisplayTile[] = []
  for (const s of cfg.value.slots) {
    if (!s.id.startsWith('tile_') || s.enabled === false) continue
    const id = s.id.replace(/^tile_/, '')
    if (seen.has(id)) continue
    extras.push({
      id,
      title: s.title || s.name || id,
      desc: s.desc || '',
      eng: s.eng || '',
      theme: s.theme || 'deal',
      action: s.action || {},
    })
    if (fromRec.length + extras.length >= 4) break
  }
  return [...fromRec, ...extras].slice(0, 4)
})

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
  router.replace({ query })
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
    models: it.models ?? [],
    apiPlatform: it.platform,
    kind: isPack ? 'pack' : 'channel',
    plat,
    tier: it.tier || '',
    title: it.title || `${plat}${it.tier ? ` · ${it.tier}` : ''} · ${isPack ? `${it.face_value ?? '—'} 元额度包` : '按量通道'}`,
    img: coverFor(plat, it.listing_id),
    mult: it.price,
    min: it.min_amount ?? 0,
    face: it.face_value,
    stock: stockLeft,
    grade: it.grade || 'B',
    success: it.success_rate_24h ?? 0.95,
    ttft: Math.round(it.ttft_p50_ms ?? 450),
    badge: isPack ? '可退货' : cats.includes('low_rate') || it.price <= 0.55 ? '低倍率' : undefined,
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

function matchAction(skus: FeedSku[], action: MarketAction): FeedSku[] {
  let list = skus
  if (action.kindTab === 'pack' || action.kindTab === 'channel') list = list.filter((s) => s.kind === action.kindTab)
  if (action.plat === '保障池') list = list.filter((s) => s.pool || s.plat === '保障池')
  else if (action.plat) list = list.filter((s) => s.plat === action.plat)
  if (action.tag === 'low_rate') {
    list = list.filter((s) => hasCat(s, 'low_rate') || (s.kind === 'channel' && s.mult <= 0.55))
  } else if (action.tag === 'grade_s') {
    list = list.filter((s) => s.grade === 'S')
  } else if (action.tag === 'coding') {
    list = list.filter((s) => hasCat(s, 'coding') || codingPlats.has(s.plat))
  } else if (action.tag === 'refundable' || action.tag === 'pack') {
    list = list.filter((s) => s.kind === 'pack')
  } else if (action.tag === 'consignment') {
    list = list.filter((s) => s.kind === 'channel' && !s.pool)
  } else if (action.tag) {
    list = list.filter((s) => hasCat(s, action.tag!))
  }
  if (action.q) {
    const q = action.q.toLowerCase()
    list = list.filter((s) => (s.plat + ' ' + s.tier + ' ' + s.title + ' ' + (s.desc ?? '')).toLowerCase().includes(q))
  }
  return list
}

function assetUrl(img: string): string {
  return img.startsWith('/') || img.startsWith('http') ? img : `/assets/${img}`
}

function priceLabel(s: FeedSku): string {
  if (s.kind === 'pack') return `¥${s.face ?? '—'}`
  return `${Number(s.mult).toFixed(2)}×`
}

const allSkus = computed(() => {
  const listingSkus = (listings.data.value ?? []).map(toSku)
  const poolSkus = (pools.data.value ?? []).filter((p) => p.market_published).map(toPoolSku)
  return [...poolSkus, ...listingSkus]
})

function skuHref(s: FeedSku): string {
  if (s.listingId) return `/market/listings/${s.listingId}`
  if (s.pool) return '/buyer/keys'
  return '/#feed'
}

function pushThumb(picks: TileThumb[], s: FeedSku) {
  if (picks.length >= 3 || picks.some((x) => x.key === s.id)) return
  picks.push({ key: s.id, img: assetUrl(s.img), price: priceLabel(s), href: skuHref(s) })
}

/** 四宫格右侧三图：只展示真实挂单/保障池；本专场不足则用其它在售补齐 */
const tileThumbs = computed(() => {
  const map: Record<string, TileThumb[]> = {}
  for (const t of tiles.value) {
    const picks: TileThumb[] = []
    const recItems = recommend.value?.tiles?.find((r) => r.id === t.id)?.items

    if (recItems?.length) {
      for (const it of recItems.slice(0, 3)) {
        const plat = apiToPlat[it.plat] || it.plat
        const key = it.key
        if (picks.some((x) => x.key === key)) continue
        const href = it.listing_id
          ? `/market/listings/${it.listing_id}`
          : it.pool
            ? '/buyer/keys'
            : undefined
        picks.push({
          key,
          img: assetUrl(coverFor(plat, it.listing_id ?? it.key)),
          price: it.price,
          href,
        })
      }
    }

    if (picks.length < 3 && t.action.plat === '保障池') {
      const published = (pools.data.value ?? []).filter((p) => p.market_published)
      const poolList = published.length ? published : (pools.data.value ?? [])
      for (const [i, p] of poolList.entries()) {
        if (picks.length >= 3) break
        const plat = apiToPlat[p.platform] || p.platform
        const key = `pool-${p.id || p.name}`
        if (picks.some((x) => x.key === key)) continue
        picks.push({
          key,
          img: assetUrl(coverFor(plat, p.id || i)),
          price: `${Number(p.price).toFixed(2)}×`,
          href: '/buyer/keys',
        })
      }
    }

    if (picks.length < 3) {
      const matched = [...matchAction(allSkus.value, t.action)]
      if (t.action.tag === 'low_rate') matched.sort((a, b) => a.mult - b.mult)
      else if (t.action.kindTab === 'pack') matched.sort((a, b) => (a.face ?? 0) - (b.face ?? 0))
      else matched.sort((a, b) => b.success - a.success)
      for (const s of matched) pushThumb(picks, s)
    }

    if (picks.length < 3) {
      const rest = [...allSkus.value].sort((a, b) => b.success - a.success)
      for (const s of rest) pushThumb(picks, s)
    }

    map[t.id] = picks
  }
  return map
})

const feedSkus = computed(() => {
  // 只展示真实挂单；空货架不再用 demoSkus 假数据
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

const feedTitle = computed(() => {
  const kindText = state.kind === 'pack' ? '额度包' : state.kind === 'channel' ? '按量通道' : '在售闲置TOKEN'
  return state.plat ? `${state.plat} · ${kindText}` : kindText
})

function labelOn(action: MarketAction) {
  if (action.href) return false
  if (action.plat !== undefined && !action.tag && !action.q && !action.kindTab) {
    return (action.plat || '') === (state.plat || '') && !state.tag && !state.q
  }
  if (action.tag && action.plat === undefined && !action.q) return state.tag === action.tag
  return false
}

onMounted(() => {
  syncFromRoute()
  listings.load()
  pools.load()
  // 类目树 / 四宫格 / 运营配置并行拉，避免串行瀑布拖慢首屏运营区
  void Promise.all([
    discoveryStore.ensure(),
    market.categories().then((v) => { catTree.value = v ?? [] }).catch(() => { catTree.value = [] }),
    market.recommend().then((v) => { recommend.value = v }).catch(() => { recommend.value = null }),
  ])
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
  <div class="page">
    <!-- 运营区：左类目 | 中横幅 | 右四宫格 -->
    <section class="stage" aria-label="市场运营区">
      <aside class="side-cats" aria-label="市场类目">
        <div v-for="g in groups" :key="g.id" class="cat-row">
          <span class="ico" aria-hidden="true" v-html="catIconSvg[g.icon] || catIconSvg.chat" />
          <div class="cat-labels">
            <template v-for="(lab, i) in g.labels" :key="lab.text">
              <span v-if="i" class="sep">/</span>
              <a
                href="#feed"
                :class="{ 'is-on': labelOn(lab.action) }"
                @click.prevent="applyAction(lab.action)"
              >{{ lab.text }}</a>
            </template>
          </div>
          <div v-if="g.sections?.length" class="cat-flyout" role="region" :aria-label="g.labels.map((l) => l.text).join(' / ')">
            <div class="fly-sections">
              <div v-for="sec in g.sections" :key="sec.title" class="sec">
                <div class="sec-title">{{ sec.title }}</div>
                <div class="sec-items">
                  <a
                    v-for="it in sec.items"
                    :key="it.text"
                    href="#feed"
                    @click.prevent="applyAction(it.action)"
                  >{{ it.text }}</a>
                </div>
              </div>
            </div>
          </div>
        </div>
      </aside>

      <a
        v-if="banner"
        class="promo-banner"
        href="#feed"
        @click.prevent="applyAction(banner.action)"
      >
        <div class="bg" aria-hidden="true" />
        <div class="content">
          <h1 v-html="banner.title.replace(/\n/g, '<br />')" />
          <p class="desc">{{ banner.desc }}</p>
          <div class="mascot-spot" aria-hidden="true">
            <img src="/assets/xianmao-pet.svg" alt="闲猫" />
          </div>
          <span class="promo-cta">去看看 ›</span>
        </div>
      </a>

      <div class="promo-grid">
        <a
          v-for="t in tiles"
          :key="t.id"
          class="tile"
          :class="`tile--${t.theme || 'deal'}`"
          href="#feed"
          @click.prevent="applyAction(t.action)"
        >
          <div class="tile-eng" aria-hidden="true">{{ t.eng }}</div>
          <img class="tile-flower" src="/assets/tile-flower.png" alt="" aria-hidden="true" />
          <div class="tile-copy">
            <h3>{{ t.title }} <span class="arrow">›</span></h3>
            <p>{{ t.desc }}</p>
          </div>
          <div class="tile-mascot">
            <img :src="tileThemeIcon[t.theme || 'deal']" alt="" />
          </div>
          <div class="tile-tray">
            <template v-if="tileThumbs[t.id]?.length">
              <RouterLink
                v-for="th in tileThumbs[t.id]"
                :key="th.key"
                class="thumb"
                :to="th.href || '#feed'"
                @click.stop
              >
                <div class="thumb-pic"><img :src="th.img" alt="" /></div>
                <div class="thumb-price">{{ th.price }}</div>
              </RouterLink>
            </template>
            <div v-else class="thumb thumb--empty">
              <div class="thumb-pic thumb-pic--empty" />
              <div class="thumb-price">暂无</div>
            </div>
          </div>
        </a>
      </div>
    </section>

    <div id="feed" class="section-head">
      <h2>{{ feedTitle }}</h2>
      <div class="kind-tabs" role="tablist" aria-label="商品形态">
        <button type="button" class="kind-tab" :class="{ on: state.kind === 'all' || !state.kind }" @click="setKind('all')">全部</button>
        <button type="button" class="kind-tab" :class="{ on: state.kind === 'channel' }" @click="setKind('channel')">按量通道</button>
        <button type="button" class="kind-tab" :class="{ on: state.kind === 'pack' }" @click="setKind('pack')">额度包</button>
      </div>
    </div>

    <div class="facet-bar" aria-label="筛选">
      <label for="f-model">模型</label>
      <input id="f-model" :value="state.model" list="market-models" placeholder="全部模型" class="input !w-48" @change="setModel" />
      <datalist id="market-models"><option v-for="model in modelChoices" :key="model" :value="model" /></datalist>
      <label for="f-grade">等级</label>
      <select id="f-grade" v-model="state.grade">
        <option value="">全部</option>
        <option value="S">S</option>
        <option value="A">A</option>
        <option value="B">B</option>
        <option value="C">C</option>
      </select>
      <label for="f-sort">排序</label>
      <select id="f-sort" v-model="state.sort">
        <option value="default">综合</option>
        <option value="price">倍率/价格</option>
        <option value="success">成功率</option>
        <option value="ttft">更快</option>
      </select>
      <span class="facet-hint">{{ feedSkus.length }} 个闲置TOKEN</span>
    </div>

    <div class="feed">
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
        货架只展示真实挂单。卖家完成检测并上架后会出现在这里。
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
