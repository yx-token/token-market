<script setup lang="ts">
import type { ReviewItem } from '@/api/types'
import { dateTime } from '@/utils/format'
import ReviewStars from './ReviewStars.vue'

defineProps<{
  items: ReviewItem[] | null | undefined
  loading?: boolean
  empty?: string
}>()

function tagText(tags?: string[] | null) {
  return (tags ?? []).filter(Boolean)
}
</script>

<template>
  <div class="rev-list" :aria-busy="loading">
    <p v-if="loading && !(items?.length)" class="rev-empty">加载评价…</p>
    <p v-else-if="!(items?.length)" class="rev-empty">{{ empty || '暂无评价' }}</p>
    <article v-for="r in items || []" :key="r.id" class="rev-card">
      <header class="rev-card__head">
        <span class="rev-buyer">{{ r.buyer_mask || '买家' }}</span>
        <ReviewStars v-if="r.rating" :value="r.rating" readonly size="sm" />
        <time class="rev-time">{{ dateTime(r.created_at) }}</time>
      </header>
      <p v-if="r.content" class="rev-body">{{ r.content }}</p>
      <p v-else class="rev-body muted">未填写短评</p>
      <div v-if="tagText(r.tags).length" class="rev-tags">
        <span v-for="t in tagText(r.tags)" :key="t" class="rev-tag">{{ t }}</span>
      </div>
      <p v-if="r.listing_id" class="rev-meta">关联挂单 #{{ r.listing_id }}</p>
      <div v-if="r.follow_up" class="rev-follow">
        <div class="rev-follow__label">追评 · {{ dateTime(r.follow_up.created_at) }}</div>
        <p class="rev-body">{{ r.follow_up.content || '（无文字）' }}</p>
        <div v-if="tagText(r.follow_up.tags).length" class="rev-tags">
          <span v-for="t in tagText(r.follow_up.tags)" :key="t" class="rev-tag">{{ t }}</span>
        </div>
      </div>
    </article>
  </div>
</template>

<style scoped>
.rev-list {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.rev-empty {
  padding: 20px 0;
  text-align: center;
  color: #888;
  font-size: 13px;
}
.rev-card {
  border: 0;
  border-radius: 0;
  padding: 8px 0;
  background: transparent;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
}
.rev-card:first-of-type {
  border-top: 0;
  padding-top: 0;
}
.rev-card__head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 10px;
  margin-bottom: 4px;
}
.rev-buyer {
  font-weight: 700;
  font-size: 13px;
}
.rev-time {
  margin-left: auto;
  font-size: 11px;
  color: #999;
}
.rev-body {
  font-size: 13px;
  line-height: 1.55;
  color: #333;
  white-space: pre-wrap;
  word-break: break-word;
}
.rev-body.muted {
  color: #999;
}
.rev-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}
.rev-tag {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: #f5f5f5;
  color: #555;
}
.rev-meta {
  margin-top: 8px;
  font-size: 11px;
  color: #999;
}
.rev-follow {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px dashed rgba(0, 0, 0, 0.08);
}
.rev-follow__label {
  font-size: 11px;
  font-weight: 700;
  color: #666;
  margin-bottom: 6px;
}
</style>
