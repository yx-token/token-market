<script setup lang="ts">
// 翻牌式数字：每个字符一块牌，数值变化时依次翻转。
defineProps<{ value: string; tone?: 'red' | 'light' }>()
</script>

<template>
  <span :key="value" class="inline-flex gap-[2px]" :aria-label="value" style="perspective: 400px">
    <span
      v-for="(ch, i) in value.split('')"
      :key="i"
      :class="[
        'relative inline-grid h-[1.55em] min-w-[0.95em] origin-top animate-flip place-items-center rounded-[2px] border border-ink-700 bg-gradient-to-b from-ink-800 to-ink-900 px-[0.12em] font-mono font-medium leading-none',
        tone === 'light' ? 'text-white' : 'text-[#FF8A5C] [text-shadow:0_0_10px_rgba(255,106,61,0.35)]',
      ]"
      :style="{ animationDelay: `${i * 45}ms` }"
      aria-hidden="true"
    >
      {{ ch }}
      <span class="pointer-events-none absolute inset-x-0 top-1/2 h-px bg-ink-950/70" />
    </span>
  </span>
</template>
