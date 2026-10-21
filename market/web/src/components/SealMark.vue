<script setup lang="ts">
// 印章标识：朱红方印，内框留白，"闲"字阳文。
withDefaults(defineProps<{ size?: number; char?: string; animate?: boolean }>(), { size: 36, char: '闲', animate: false })
</script>

<template>
  <span
    :class="['relative inline-grid shrink-0 -rotate-[4deg] place-items-center bg-brand-500 font-display font-bold text-white', animate ? 'animate-stamp' : '']"
    :style="{ width: `${size}px`, height: `${size}px`, fontSize: `${size * 0.56}px`, borderRadius: `${Math.max(2, size * 0.08)}px` }"
    aria-hidden="true"
  >
    <span class="absolute inset-[9%] rounded-[1px] border border-white/80" />
    <span class="relative leading-none">{{ char }}</span>
  </span>
</template>
