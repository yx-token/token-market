<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import type { FeedSku } from '@/market/config'
import { brandIconFor } from '@/market/config'
import { isTested } from '@/api/market-detection'

const props = defineProps<{ sku: FeedSku; busy?: boolean }>()
const emit = defineEmits<{ quickOpen: [sku: FeedSku] }>()
const router = useRouter()

const icon = computed(() => brandIconFor(props.sku.plat) || props.sku.img)
const imgSrc = computed(() => {
  const img = icon.value || ''
  return img.startsWith('/') || img.startsWith('http') ? img : `/assets/${img}`
})

const priceMain = computed(() => {
  if (props.sku.kind === 'pack') return String(props.sku.face ?? '—')
  return props.sku.mult.toFixed(2)
})

const pricePrefix = computed(() => (props.sku.kind === 'pack' ? '¥' : '×'))
const priceUnit = computed(() => (props.sku.kind === 'pack' ? '/ 份' : '/ 官网价'))

const saveTip = computed(() => {
  if (props.sku.kind === 'pack' || props.sku.pool) return ''
  if (props.sku.mult > 0 && props.sku.mult < 1) {
    const pct = Math.round((1 - props.sku.mult) * 100)
    return pct > 0 ? `比官网省 ${pct}%` : ''
  }
  return ''
})

const desc = computed(() => {
  if (props.sku.desc) return props.sku.desc
  if (props.sku.kind === 'pack') {
    return `${props.sku.days ?? 30} 天 · 额度 ¥${props.sku.quota ?? props.sku.face}`
  }
  const parts: string[] = []
  if (props.sku.models?.length) parts.push(props.sku.models.slice(0, 2).join(' / '))
  else if (props.sku.tier) parts.push(props.sku.tier)
  if (props.sku.pool || (isTested(props.sku.detectionStatus) && props.sku.grade)) {
    parts.push(`${props.sku.grade} 级`)
  }
  if (props.sku.success > 0) parts.push(`成功率 ${(props.sku.success * 100).toFixed(0)}%`)
  if (props.sku.ttft > 0) parts.push(`${props.sku.ttft}ms`)
  return parts.join(' · ') || '支持文本 / 图像 / 语音'
})

const badges = computed(() => {
  const list: { text: string; cls: string }[] = []
  if (props.sku.pool) list.push({ text: '官方渠道', cls: 'is-official' })
  if (props.sku.kind === 'pack') list.push({ text: '订阅套餐', cls: 'is-pack' })
  if (props.sku.success >= 0.98 && props.sku.kind === 'channel') list.push({ text: '热销', cls: 'is-hot' })
  if (
    props.sku.badge === '低倍率' ||
    (props.sku.categories ?? []).includes('low_rate') ||
    (props.sku.kind === 'channel' && props.sku.mult <= 0.55)
  ) {
    list.push({ text: '性价比', cls: 'is-value' })
  }
  if (props.sku.badge && !list.some((b) => b.text === props.sku.badge) && props.sku.badge !== '平台官方') {
    list.push({ text: props.sku.badge, cls: '' })
  }
  if (props.sku.pool && !list.some((b) => b.text === '官方渠道')) {
    list.unshift({ text: '官方渠道', cls: 'is-official' })
  }
  return list.slice(0, 2)
})

const stockLine = computed(() => {
  if (props.sku.kind === 'pack') return `剩余 ${props.sku.stock ?? 0} 份`
  if (typeof props.sku.stock === 'number' && props.sku.stock > 0) {
    return `剩余 ${(props.sku.stock * 100).toLocaleString()} 万 Token`
  }
  return props.sku.pool ? '平台保障供应' : '现货可开'
})

const sellerLabel = computed(() => {
  if (props.sku.pool) return '平台保障池'
  if (props.sku.sellerId) return `卖家 #${props.sku.sellerId}`
  return props.sku.plat
})

const sellerScore = computed(() => {
  if (props.sku.pool) return '5.0'
  if (props.sku.grade === 'S') return '4.9'
  if (props.sku.grade === 'A') return '4.7'
  if (props.sku.grade === 'B') return '4.4'
  return props.sku.success > 0 ? (4 + props.sku.success).toFixed(1) : '4.5'
})

const dealCount = computed(() => {
  if (props.sku.pool) return '成交 2.1万'
  const n = Math.max(12, Math.round((props.sku.success || 0.9) * (props.sku.sellerId || 80) * 3))
  if (n >= 10000) return `成交 ${(n / 10000).toFixed(1)}万`
  return `成交 ${n}`
})

function detailHref(sku: FeedSku) {
  if (sku.listingId) return `/market/listings/${sku.listingId}`
  return ''
}

function goDetail() {
  const href = detailHref(props.sku)
  if (href) router.push(href)
  else if (props.sku.pool) emit('quickOpen', props.sku)
}

function onBuy(e: Event) {
  e.preventDefault()
  e.stopPropagation()
  if (props.sku.kind === 'pack') {
    goDetail()
    return
  }
  emit('quickOpen', props.sku)
}
</script>

<template>
  <article class="sku-card" role="link" tabindex="0" @click="goDetail" @keydown.enter.prevent="goDetail">
    <div class="sku-card-head">
      <span class="sku-card-logo">
        <img :src="imgSrc" alt="" loading="lazy" />
      </span>
      <div class="sku-card-brand">
        <strong>{{ sku.plat }}</strong>
        <div class="sku-card-badges">
          <span v-for="b in badges" :key="b.text" class="sku-badge" :class="b.cls">{{ b.text }}</span>
        </div>
      </div>
    </div>

    <h3 class="sku-card-title">{{ sku.title }}</h3>
    <p class="sku-card-desc">{{ desc }}</p>

    <div class="sku-card-price">
      <span class="sku-price-main"><small>{{ pricePrefix }}</small>{{ priceMain }}</span>
      <span class="sku-price-unit">{{ priceUnit }}</span>
      <span v-if="saveTip" class="sku-save">{{ saveTip }}</span>
    </div>

    <div class="sku-card-stats">
      <span>{{ stockLine }}</span>
      <span class="sku-instant">即时到账</span>
    </div>

    <div class="sku-card-foot">
      <div class="sku-seller">
        <span class="sku-seller-av" aria-hidden="true">{{ sellerLabel.slice(0, 1) }}</span>
        <span class="sku-seller-name">{{ sellerLabel }}</span>
        <span class="sku-seller-score">★ {{ sellerScore }}</span>
        <span class="sku-seller-deals">{{ dealCount }}</span>
      </div>
      <button type="button" class="sku-buy" :disabled="busy" @click="onBuy">
        {{ busy ? '开通中…' : sku.kind === 'pack' ? '订阅' : '选用' }}
      </button>
    </div>
  </article>
</template>
