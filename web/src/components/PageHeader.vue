<script setup lang="ts">
// 市场：报头式日期线 + 宋体标题；控制台：YourHub 轻量页头（无墨线 / 无 kicker）
import { computed } from 'vue'
import { useIsConsole } from '@/composables/useIsConsole'

const props = defineProps<{ title: string; description?: string; kicker?: string }>()
const isConsole = useIsConsole()

const dateline = computed(() => {
  if (props.kicker) return props.kicker
  const d = new Date()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()]
  return `${d.getFullYear()}.${String(d.getMonth() + 1).padStart(2, '0')}.${String(d.getDate()).padStart(2, '0')} · 星期${week}`
})
</script>

<template>
  <div
    v-if="isConsole"
    class="mb-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-3 border-b border-[var(--color-border,#e8e8e8)] pb-3"
  >
    <div class="min-w-0">
      <h1 class="text-[20px] font-semibold leading-tight text-[var(--color-text-primary,#262626)]">{{ title }}</h1>
      <p v-if="description" class="mt-1 max-w-3xl text-[13px] leading-relaxed text-[var(--color-text-secondary,#868686)]">
        {{ description }}
      </p>
    </div>
    <div v-if="$slots.default" class="flex flex-wrap items-center gap-2"><slot /></div>
  </div>
  <div v-else class="mb-7 flex flex-wrap items-end justify-between gap-x-6 gap-y-4 border-b border-ink-900 pb-4">
    <div class="min-w-0">
      <div class="kicker mb-2.5 flex items-center gap-2"><span class="h-px w-6 bg-brand-500" />{{ dateline }}</div>
      <h1 class="text-balance font-display text-[28px] font-bold leading-tight tracking-[0.04em] md:text-[34px]">{{ title }}</h1>
      <p v-if="description" class="mt-2 max-w-3xl text-[13.5px] leading-relaxed text-ink-600">{{ description }}</p>
    </div>
    <div v-if="$slots.default" class="flex flex-wrap items-center gap-2"><slot /></div>
  </div>
</template>
