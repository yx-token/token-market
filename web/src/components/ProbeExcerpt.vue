<script setup lang="ts">
// 模型输出片段：Markdown 渲染 + 原文切换，超行折叠；后端只保留前 500 字符，标注「已截断」。
import MarkdownText from '@/components/MarkdownText.vue'

defineProps<{ text?: string | null }>()
</script>

<template>
  <MarkdownText :text="text" truncated :collapsed-lines="3" max-height-class="max-h-48" />
</template>
