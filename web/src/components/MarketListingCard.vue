<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import type { FeedSku } from '@/market/config'
import { isTested } from '@/api/market-detection'

const props = defineProps<{ sku: FeedSku; busy?: boolean }>()
const emit = defineEmits<{ quickOpen: [sku: FeedSku] }>()
const router = useRouter()

const imgSrc = computed(() => {
  const img = props.sku.img || ''
  return img.startsWith('/') || img.startsWith('http') ? img : `/assets/${img}`
})

const metric = computed(() => {
  if (props.sku.kind === 'pack') return `¥${props.sku.face ?? '—'}`
  return props.sku.mult.toFixed(2)
})

const isMult = computed(() => props.sku.kind !== 'pack')

const desc = computed(() => {
  if (props.sku.desc) return props.sku.desc
  if (props.sku.kind === 'pack') {
    return `${props.sku.days ?? 30}天 · 额度 ¥${props.sku.quota ?? props.sku.face} · ${props.sku.stock ?? 0} 份`
  }
  const grade = props.sku.pool || (isTested(props.sku.detectionStatus) && props.sku.grade)
    ? `${props.sku.grade} 级 · `
    : ''
  const success = props.sku.success > 0 ? `成功率 ${(props.sku.success * 100).toFixed(0)}% · ` : ''
  const ttft = props.sku.ttft > 0 ? `${props.sku.ttft}ms` : ''
  return `${grade}${success}${ttft}`.replace(/[·\s]+$/, '') || '按量通道'
})

const platLine = computed(() => {
  const tier = props.sku.tier ? ` · ${props.sku.tier}` : ''
  return `${props.sku.plat}${tier}`
})

const cta = computed(() => {
  if (props.busy) return '开通中…'
  return props.sku.kind === 'pack' ? '订阅套餐' : '选用开通'
})

const badge = computed(() => {
  if (props.sku.pool) return { text: '官方', cls: 'is-pool' }
  if (props.sku.kind === 'pack') return { text: '订阅', cls: 'is-pack' }
  if (props.sku.badge) return { text: props.sku.badge, cls: '' }
  return null
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
  <article class="lib-card" role="link" tabindex="0" @click="goDetail" @keydown.enter.prevent="goDetail">
    <div class="lib-preview">
      <img :src="imgSrc" alt="" loading="lazy" />
      <span v-if="badge" class="lib-badge" :class="badge.cls">{{ badge.text }}</span>
      <div class="lib-stage">
        <div class="lib-plat">{{ platLine }}</div>
        <div class="lib-metric">
          <small v-if="isMult">×</small>{{ metric }}
        </div>
        <button type="button" class="lib-cta" :disabled="busy" @click="onBuy">{{ cta }}</button>
      </div>
    </div>
    <div class="lib-meta">
      <div class="lib-meta-text">
        <h3 class="lib-title">{{ sku.title }}</h3>
        <p class="lib-desc">{{ desc }}</p>
      </div>
      <span class="lib-expand" aria-hidden="true">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none">
          <path d="M7 7h4M7 7v4M7 7l5 5M17 17h-4M17 17v-4M17 17l-5-5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </div>
  </article>
</template>
