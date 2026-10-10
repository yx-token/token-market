<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { market } from '@/api'
import type { MarketCategory, MarketItem, PoolItem, RecommendResult } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import MarketListingCard from '@/components/MarketListingCard.vue'
import { useMarketDiscovery } from '@/stores/marketDiscovery'
import {
  apiToPlat,
  catIconSvg,
  categoryToAction,
  coverFor,
  tileFallbackThumbs,
  tileThemeIcon,
  type FeedSku,
  type MarketAction,
} from '@/market/config'

type TileThumb = { key: string; img: string; price: string }
type NavLabel = { text: string; action: MarketAction }
type NavGroup = {
  id: string
  icon: string
  labels: NavLabel[]
  sections: { title: string; items: NavLabel[] }[]
}
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
const discoveryStore = useMarketDiscovery()
const recommend = ref<RecommendResult | null>(null)
const catTree = ref<MarketCategory[]>([])
const cfg = computed(() => discoveryStore.cfg)
const banner = computed(() => cfg.value.slots.find((s) => s.id === 'banner' && s.enabled !== false))

/** 侧栏类目：直接读市场分类树 */
const groups = computed((): NavGroup[] => {
  const roots = [...(catTree.value ?? [])]
    .filter((r) => r.enabled !== false)
    .sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id))
  return roots.map((root) => {
    const children = [...(root.children ?? [])]
      .filter((c) => c.enabled !== false)
      .sort((a, b) => a.sort - b.sort || a.id.localeCompare(b.id))
    const items = children.map((c) => ({
      text: c.name,
      action: categoryToAction(c, root.id),
    }))
    const labels = (items.length ? items.slice(0, 3) : [{ text: root.name, action: categoryToAction(root, root.id) }]) as NavLabel[]
    return {
      id: root.id,
      icon: root.icon || 'chat',
      labels,
      sections: items.length ? [{ title: root.name, items }] : [],
    }
  })
})

/** 四宫格：推荐运营结果 */
const tiles = computed((): DisplayTile[] =>
  (recommend.value?.tiles ?? []).slice(0, 4).map((t) => ({
    id: t.id,
    title: t.title,
    desc: t.desc,
    eng: t.eng,
    theme: t.theme || 'deal',
    action: t.action || {},
  })),
)

const state = reactive({
  kind: 'all' as string,
  plat: '',
  grade: '',
  sort: 'default',
  q: '',
  tag: '',
})

const listings = useLoader(() => market.listings({ sort: 'price', limit: '200' }), [] as MarketItem[] | null)
const pools = useLoader(() => market.pools(), [] as PoolItem[] | null)

function syncFromRoute() {
  state.kind = typeof route.query.kind === 'string' ? route.query.kind : 'all'
  state.plat = typeof route.query.plat === 'string' ? route.query.plat : ''
  state.tag = typeof route.query.tag === 'string' ? route.query.tag : ''
  state.q = typeof route.query.q === 'string' ? route.query.q : ''
  state.grade = typeof route.query.grade === 'string' ? route.query.grade : state.grade
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

function toSku(it: MarketItem): FeedSku {
  const plat = apiToPlat[it.platform] || it.platform
  const isPack = it.sku_kind === 'pack' || it.mode === 'pack'
  const stockLeft = Math.max(0, (it.stock ?? 0) - (it.stock_sold ?? 0))
  const cats = it.categories ?? []
  return {
    id: `l-${it.listing_id}`,
    listingId: it.listing_id,
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
    list = list.filter((s) => (s.plat + ' ' + s.tier + ' ' + s.title).toLowerCase().includes(q))
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

const allSkus = computed(() => (listings.data.value ?? []).map(toSku))

/** 四宫格右侧三图：推荐 items，不足再本地匹配/占位 */
const tileThumbs = computed(() => {
  const map: Record<string, TileThumb[]> = {}
  for (const t of tiles.value) {
    const theme = t.theme || 'deal'
    const picks: TileThumb[] = []
    const recItems = recommend.value?.tiles?.find((r) => r.id === t.id)?.items

    if (recItems?.length) {
      for (const it of recItems.slice(0, 3)) {
        const plat = apiToPlat[it.plat] || it.plat
        picks.push({
          key: it.key,
          img: assetUrl(coverFor(plat, it.listing_id ?? it.key)),
          price: it.price,
        })
      }
    }

    if (picks.length < 3 && t.action.plat === '保障池') {
      for (const [i, p] of (pools.data.value ?? []).slice(0, 3).entries()) {
        if (picks.length >= 3) break
        const plat = apiToPlat[p.platform] || p.platform
        const key = `pool-${p.platform}-${i}`
        if (picks.some((x) => x.key === key)) continue
        picks.push({
          key,
          img: assetUrl(coverFor(plat, i)),
          price: `${Number(p.price).toFixed(2)}×`,
        })
      }
    }

    if (picks.length < 3) {
      const matched = [...matchAction(allSkus.value, t.action)]
      if (t.action.tag === 'low_rate') matched.sort((a, b) => a.mult - b.mult)
      else if (t.action.kindTab === 'pack') matched.sort((a, b) => (a.face ?? 0) - (b.face ?? 0))
      else matched.sort((a, b) => b.success - a.success)
      for (const s of matched) {
        if (picks.length >= 3) break
        if (picks.some((x) => x.key === s.id)) continue
        picks.push({ key: s.id, img: assetUrl(s.img), price: priceLabel(s) })
      }
    }

    const fallbacks = tileFallbackThumbs[theme] || tileFallbackThumbs.deal
    for (const [i, f] of fallbacks.entries()) {
      if (picks.length >= 3) break
      picks.push({ key: `${t.id}-fb-${i}`, img: f.img, price: f.price })
    }
    map[t.id] = picks
  }
  return map
})

const feedSkus = computed(() => {
  // 只展示真实挂单；空货架不再用 demoSkus 假数据
  let list = [...allSkus.value]
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
    list = list.filter((s) => (s.plat + ' ' + s.tier + ' ' + s.title).toLowerCase().includes(q))
  }
  if (state.sort === 'price') list.sort((a, b) => (a.kind === 'pack' ? (a.face ?? 0) : a.mult) - (b.kind === 'pack' ? (b.face ?? 0) : b.mult))
  else if (state.sort === 'success') list.sort((a, b) => b.success - a.success)
  else if (state.sort === 'ttft') list.sort((a, b) => a.ttft - b.ttft)
  else if (cfg.value.feed.pin_pool_first) list.sort((a, b) => Number(!!b.pool) - Number(!!a.pool))
  return list
})

const feedTitle = computed(() => {
  const kindText = state.kind === 'pack' ? '额度包' : state.kind === 'channel' ? '按量通道' : '在售供给'
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

onMounted(async () => {
  syncFromRoute()
  listings.load()
  pools.load()
  await discoveryStore.reload()
  try {
    catTree.value = (await market.categories()) ?? []
  } catch {
    catTree.value = []
  }
  try {
    recommend.value = await market.recommend()
  } catch {
    recommend.value = null
  }
})

watch(
  () => route.query,
  () => syncFromRoute(),
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
            <div v-for="th in tileThumbs[t.id]" :key="th.key" class="thumb">
              <div class="thumb-pic"><img :src="th.img" alt="" /></div>
              <div class="thumb-price">{{ th.price }}</div>
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
      <span class="facet-hint">{{ feedSkus.length }} 个真实供给</span>
    </div>

    <div class="feed">
      <MarketListingCard v-for="sku in feedSkus" :key="sku.id" :sku="sku" />
      <div v-if="!feedSkus.length && !listings.loading.value" class="feed-empty">
        暂无在售供给。
        <RouterLink to="/seller/publish" class="ml-1 font-bold underline underline-offset-2">去发布</RouterLink>
      </div>
      <div v-else-if="!feedSkus.length" class="feed-empty">加载中…</div>
    </div>

    <p class="note-wrap">
      <span class="note">
        货架只展示真实挂单。卖家完成检测并上架后会出现在这里。
        <RouterLink to="/seller/publish">发布供给</RouterLink>
      </span>
    </p>
  </div>
</template>
