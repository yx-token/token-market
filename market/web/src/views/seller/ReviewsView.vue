<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { seller } from '@/api'
import type { ReviewItem, SellerRatingStats } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import ReviewList from '@/components/reviews/ReviewList.vue'
import ReviewStars from '@/components/reviews/ReviewStars.vue'

const emptyStats: SellerRatingStats = { seller_id: 0, avg_rating: 0, rating_count: 0 }
const data = useLoader(
  async () => {
    const res = await seller.reviews({ limit: 100, offset: 0 })
    return {
      stats: res.stats ?? emptyStats,
      items: (res.items ?? []) as ReviewItem[],
    }
  },
  { stats: emptyStats, items: [] as ReviewItem[] },
)

onMounted(data.load)

const avgText = computed(() => {
  const n = data.data.value.stats.rating_count
  if (!n) return '—'
  return Number(data.data.value.stats.avg_rating).toFixed(1)
})
</script>

<template>
  <div>
    <PageHeader title="买家评价" description="公开可见的星评与短评。评分计入商家均分，展示在市场货架。">
      <button type="button" class="xm-btn xm-btn-ghost" :disabled="data.loading.value" @click="data.load()">刷新</button>
    </PageHeader>

    <div class="mb-5 grid grid-cols-2 gap-4 lg:grid-cols-3">
      <div class="stat">
        <div class="stat-label">综合评分</div>
        <div class="stat-value flex items-center gap-2">
          {{ avgText }}
          <ReviewStars
            v-if="data.data.value.stats.rating_count"
            :value="Math.round(data.data.value.stats.avg_rating)"
            readonly
            size="sm"
          />
        </div>
      </div>
      <div class="stat">
        <div class="stat-label">评价条数</div>
        <div class="stat-value">{{ data.data.value.stats.rating_count }}</div>
      </div>
    </div>

    <div class="xm-key-card">
      <ReviewList :items="data.data.value.items" :loading="data.loading.value" empty="还没有买家评价" />
    </div>
  </div>
</template>
