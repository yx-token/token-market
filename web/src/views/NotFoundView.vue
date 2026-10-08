<script setup lang="ts">
import SealMark from '@/components/SealMark.vue'
</script>

<template>
  <div class="mx-auto max-w-xl py-20 text-center">
    <div class="flex justify-center"><SealMark :size="72" char="缺" animate /></div>
    <div class="kicker mt-8">错误 404 · 页面不存在</div>
    <h1 class="mt-3 font-display text-[34px] font-bold">这一页没有挂牌。</h1>
    <p class="mt-3 text-[13.5px] text-ink-500">地址可能已经变更，或者这个挂单已经下架。</p>
    <RouterLink to="/" class="btn-primary mt-8">返回市场<span aria-hidden="true">→</span></RouterLink>
  </div>
</template>
