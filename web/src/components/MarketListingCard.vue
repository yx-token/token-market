<script setup lang="ts">
import type { FeedSku } from '@/market/config'
import { inspectionSummary } from '@/utils/inspection'
import { dateTime } from '@/utils/format'

defineProps<{ sku: FeedSku }>()

function hrefOf(sku: FeedSku) {
  if (sku.listingId) return `/market/listings/${sku.listingId}`
  if (sku.pool) return '/buyer/keys'
  return '/?kind=' + sku.kind
}
</script>

<template>
  <article class="xm-card">
    <RouterLink class="card-cover" :to="hrefOf(sku)">
      <img :src="`/assets/${sku.img}`" :alt="sku.title" loading="lazy" />
      <div class="shade" />
      <span v-if="sku.badge" class="badge">{{ sku.badge }}</span>
      <span class="plat">{{ sku.plat }}</span>
    </RouterLink>
    <div class="card-body">
      <span v-if="sku.pool" class="card-kind is-pool">保障池 · 按量</span>
      <span v-else-if="sku.kind === 'pack'" class="card-kind is-pack">额度包</span>
      <span v-else class="card-kind">按量通道</span>
      <h3 class="card-title">
        <RouterLink :to="hrefOf(sku)">{{ sku.title }}</RouterLink>
      </h3>
      <div class="card-meta">
        <span class="grade">{{ sku.grade }} 级</span>
        <span>成功率 {{ (sku.success * 100).toFixed(0) }}%</span>
        <span>{{ sku.ttft }}ms</span>
      </div>
      <div v-if="sku.listingId" class="my-2 text-xs leading-relaxed text-slate-500">
        <p v-for="line in inspectionSummary(sku.qualityChecks)" :key="line">{{ line }}</p>
        <p>上次检测：{{ sku.lastCheckedAt ? dateTime(sku.lastCheckedAt) : '未提供' }}</p>
        <p v-if="sku.inspectionMode === 'trial'">试运行，不影响价格</p>
        <p>检测是抽样，不能保证没有偷换</p>
      </div>
      <div class="card-foot">
        <div class="price-block">
          <template v-if="sku.kind === 'pack'">
            <div class="price">¥{{ sku.face }}</div>
            <div class="price-sub">包内 {{ sku.mult.toFixed(2) }}× · 库存 {{ sku.stock }}</div>
          </template>
          <template v-else>
            <div class="price"><small>×</small>{{ sku.mult.toFixed(2) }}</div>
            <div class="price-sub">{{ sku.min === 0 ? '起步免费开通' : `起步 ¥${sku.min}` }} · 超额扣余额</div>
          </template>
        </div>
        <RouterLink class="buy" :to="hrefOf(sku)">{{ sku.kind === 'pack' ? '购买额度包' : '选用并开通' }}</RouterLink>
      </div>
    </div>
  </article>
</template>
