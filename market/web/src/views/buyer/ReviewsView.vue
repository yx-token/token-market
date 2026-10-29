<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { buyer } from '@/api'
import type { ReviewEligible, ReviewItem } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import ReviewForm from '@/components/reviews/ReviewForm.vue'
import ReviewList from '@/components/reviews/ReviewList.vue'
import ReviewStars from '@/components/reviews/ReviewStars.vue'
import { useToast } from '@/stores/toast'

const toast = useToast()
const { busy, run } = useAction()
const mine = useLoader(() => buyer.reviews(), [] as ReviewItem[] | null)
const eligible = useLoader(async () => {
  const out = await buyer.reviewEligible()
  if (!out) return [] as ReviewEligible[]
  return Array.isArray(out) ? out : [out]
}, [] as ReviewEligible[])
const writing = ref<ReviewEligible | null>(null)
const following = ref<ReviewItem | null>(null)
const formRef = ref<{ reset: () => void } | null>(null)

const canWrite = computed(() =>
  (eligible.data.value ?? []).filter((e: ReviewEligible) => e.can_review || e.can_follow_up),
)

onMounted(() => {
  mine.load()
  eligible.load()
})

function sellerLabel(e: ReviewEligible) {
  return e.seller_email?.trim() || `商家 ${e.seller_id}`
}

async function submitNew(payload: { rating: number; content: string; tags: string[] }) {
  const e = writing.value
  if (!e) return
  const ok = await run(
    () =>
      buyer.createReview({
        seller_id: e.seller_id,
        listing_id: e.listing_id ?? undefined,
        rating: payload.rating,
        content: payload.content || undefined,
        tags: payload.tags,
      }),
    '评价已提交',
  )
  if (ok) {
    writing.value = null
    formRef.value?.reset()
    mine.load()
    eligible.load()
  }
}

async function submitFollow(payload: { rating: number; content: string; tags: string[] }) {
  const r = following.value
  if (!r) return
  if (!payload.content.trim() && !payload.tags.length) {
    toast.error('追评请填写内容或选择标签')
    return
  }
  const ok = await run(
    () => buyer.followUpReview(r.id, { content: payload.content, tags: payload.tags }),
    '追评已提交',
  )
  if (ok) {
    following.value = null
    mine.load()
    eligible.load()
  }
}

function startWrite(e: ReviewEligible) {
  if (e.can_review) {
    writing.value = e
    following.value = null
    return
  }
  if (e.can_follow_up && e.existing_id) {
    following.value = {
      id: e.existing_id,
      buyer_id: 0,
      seller_id: e.seller_id,
      content: '',
      tags: [],
      status: 'visible',
      created_at: '',
      updated_at: '',
      rating: e.existing_rating ?? null,
    }
    writing.value = null
  }
}
</script>

<template>
  <div>
    <PageHeader title="我的评价" description="对有过用量或套餐的商家打星；短评与标签选填。满 7 天可追评一次。">
      <button type="button" class="xm-btn xm-btn-ghost" @click="mine.load(); eligible.load()">刷新</button>
    </PageHeader>

    <section v-if="canWrite.length" class="xm-key-card mb-5">
      <h2 class="text-base font-bold">可评价商家</h2>
      <ul class="mt-3 divide-y divide-black/10">
        <li v-for="e in canWrite" :key="`${e.seller_id}-${e.existing_id || 0}`" class="flex flex-wrap items-center justify-between gap-3 py-3">
          <div>
            <div class="font-semibold">{{ sellerLabel(e) }}</div>
            <p class="text-xs text-slate-500">
              <template v-if="e.can_review">尚未评价</template>
              <template v-else>可追评 · 原评分 {{ e.existing_rating ?? '—' }} 星</template>
              <template v-if="e.listing_id"> · 挂单 #{{ e.listing_id }}</template>
            </p>
          </div>
          <button type="button" class="xm-btn xm-btn-primary xm-btn-sm" @click="startWrite(e)">
            {{ e.can_review ? '去评价' : '追评' }}
          </button>
        </li>
      </ul>
    </section>

    <section v-if="writing" class="xm-key-card mb-5">
      <div class="mb-3 flex items-center justify-between gap-3">
        <h2 class="text-base font-bold">评价 · {{ sellerLabel(writing) }}</h2>
        <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="writing = null">取消</button>
      </div>
      <ReviewForm ref="formRef" :busy="busy" @submit="submitNew" />
    </section>

    <section v-if="following" class="xm-key-card mb-5">
      <div class="mb-3 flex items-center justify-between gap-3">
        <h2 class="text-base font-bold">追评</h2>
        <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="following = null">取消</button>
      </div>
      <p v-if="following.rating" class="mb-2 flex items-center gap-2 text-sm text-slate-600">
        原评分 <ReviewStars :value="following.rating" readonly size="sm" />
      </p>
      <ReviewForm follow-up :busy="busy" @submit="submitFollow" />
    </section>

    <section class="xm-key-card">
      <h2 class="mb-3 text-base font-bold">已提交</h2>
      <ReviewList :items="mine.data.value" :loading="mine.loading.value" empty="还没有评价，有用量或套餐后即可打星" />
    </section>
  </div>
</template>
