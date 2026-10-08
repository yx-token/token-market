<script setup lang="ts">
// 版头：报头式日期线、宋体大标题、说明文字，底部墨线。右侧插槽放操作按钮。
import { computed } from 'vue'

const props = defineProps<{ title: string; description?: string; kicker?: string }>()
const dateline = computed(() => {
  if (props.kicker) return props.kicker
  const d = new Date()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()]
  return `${d.getFullYear()}.${String(d.getMonth() + 1).padStart(2, '0')}.${String(d.getDate()).padStart(2, '0')} · 星期${week}`
})
</script>

<template>
  <div class="mb-7 flex flex-wrap items-end justify-between gap-x-6 gap-y-4 border-b border-ink-900 pb-4">
    <div class="min-w-0">
      <div class="kicker mb-2.5 flex items-center gap-2"><span class="h-px w-6 bg-brand-500" />{{ dateline }}</div>
      <h1 class="text-balance font-display text-[28px] font-bold leading-tight tracking-[0.04em] md:text-[34px]">{{ title }}</h1>
      <p v-if="description" class="mt-2 max-w-3xl text-[13.5px] leading-relaxed text-ink-600">{{ description }}</p>
    </div>
    <div v-if="$slots.default" class="flex flex-wrap items-center gap-2"><slot /></div>
  </div>
</template>
