<script setup lang="ts">
// 等级印：圆形细框里的宋体字母；S 级为朱红实心。
defineProps<{ grade?: string | null }>()
</script>

<template>
  <span
    v-if="grade"
    :class="[
      'inline-grid h-7 w-7 place-items-center rounded-full border font-display text-[13px] font-bold',
      grade === 'S' ? 'border-brand-500 bg-brand-500 text-white' : grade === 'A' ? 'border-ink-900 text-ink-900' : 'border-ink-300 text-ink-600',
    ]"
    :title="`${grade} 级`"
  >
    {{ grade }}
  </span>
  <span v-else class="font-mono text-xs text-ink-400" title="未定级">—</span>
</template>
