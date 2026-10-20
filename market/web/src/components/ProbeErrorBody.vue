<script setup lang="ts">
// 检测失败时返回的脱敏错误体：折叠为「查看错误体」按钮，展开后支持渲染 / 原文切换。
import { ref } from 'vue'
import MarkdownText from '@/components/MarkdownText.vue'

defineProps<{ body?: string | null }>()

const open = ref(false)
</script>

<template>
  <div v-if="body" class="mt-1">
    <button
      type="button"
      class="inline-flex items-center gap-1 text-xs text-red-700 hover:underline"
      :aria-expanded="open"
      @click="open = !open"
    >
      <span aria-hidden="true" class="inline-block text-[9px] transition-transform" :class="open ? 'rotate-90' : ''">▶</span>
      {{ open ? '收起错误体' : '查看错误体' }}（已脱敏）
    </button>
    <div v-if="open" class="mt-1 max-w-sm rounded border border-red-100 bg-red-50/60 p-2">
      <MarkdownText :text="body" :collapsed-lines="6" max-height-class="max-h-48" />
    </div>
  </div>
</template>
