<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ReviewStars from './ReviewStars.vue'

const TAGS = ['稳定', '便宜', '客服好', '延迟低', '掉线多', '限流多', '说明不清'] as const

const props = defineProps<{
  submitLabel?: string
  followUp?: boolean
  initialRating?: number
  busy?: boolean
}>()

const emit = defineEmits<{
  submit: [payload: { rating: number; content: string; tags: string[] }]
}>()

const rating = ref(props.initialRating || 0)
const content = ref('')
const tags = ref<string[]>([])

watch(
  () => props.initialRating,
  (v) => {
    if (v && !rating.value) rating.value = v
  },
)

const canSubmit = computed(() => {
  if (props.followUp) return content.value.trim().length > 0 || tags.value.length > 0
  return rating.value >= 1 && rating.value <= 5
})

function toggleTag(t: string) {
  if (tags.value.includes(t)) tags.value = tags.value.filter((x) => x !== t)
  else tags.value = [...tags.value, t]
}

function onSubmit() {
  if (!canSubmit.value || props.busy) return
  emit('submit', {
    rating: rating.value,
    content: content.value.trim(),
    tags: [...tags.value],
  })
}

function reset() {
  rating.value = props.initialRating || 0
  content.value = ''
  tags.value = []
}

defineExpose({ reset })
</script>

<template>
  <form class="rev-form" @submit.prevent="onSubmit">
    <div v-if="!followUp" class="rev-form__row">
      <label class="rev-form__label">评分</label>
      <ReviewStars v-model="rating" />
    </div>
    <div class="rev-form__row">
      <label class="rev-form__label">{{ followUp ? '追评' : '短评（选填）' }}</label>
      <textarea
        v-model="content"
        class="rev-form__ta"
        maxlength="200"
        rows="3"
        :placeholder="followUp ? '补充使用体验，最多 200 字' : '最多 200 字，可不填'"
      />
      <span class="rev-form__count">{{ content.length }}/200</span>
    </div>
    <div class="rev-form__row">
      <label class="rev-form__label">标签（选填）</label>
      <div class="rev-form__tags">
        <button
          v-for="t in TAGS"
          :key="t"
          type="button"
          class="rev-form__tag"
          :class="{ on: tags.includes(t) }"
          @click="toggleTag(t)"
        >
          {{ t }}
        </button>
      </div>
    </div>
    <button type="submit" class="xm-btn xm-btn-primary" :disabled="!canSubmit || busy">
      {{ busy ? '提交中…' : submitLabel || (followUp ? '发表追评' : '提交评价') }}
    </button>
  </form>
</template>

<style scoped>
.rev-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.rev-form__row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  position: relative;
}
.rev-form__label {
  font-size: 12px;
  font-weight: 700;
  color: #555;
}
.rev-form__ta {
  width: 100%;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 13px;
  resize: vertical;
  min-height: 72px;
}
.rev-form__count {
  position: absolute;
  right: 10px;
  bottom: 8px;
  font-size: 11px;
  color: #aaa;
}
.rev-form__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.rev-form__tag {
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: #fff;
  border-radius: 999px;
  padding: 4px 10px;
  font-size: 12px;
  color: #555;
  cursor: pointer;
}
.rev-form__tag.on {
  border-color: #f59e0b;
  background: #fffbeb;
  color: #92400e;
}
</style>
