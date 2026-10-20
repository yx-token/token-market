<script setup lang="ts">
// 模型输出片段：Markdown 渲染 + 原文切换，超行折叠；后端只保留前 500 个字符，达到上限时标注「已截断」。
import { computed } from 'vue'
import MarkdownText from '@/components/MarkdownText.vue'

const props = defineProps<{ text?: string | null }>()

// 后端按字符（rune）截取，这里按码点计数，避免把代理对算成两个字符
const EXCERPT_LIMIT = 500
const truncated = computed(() => [...(props.text ?? '')].length >= EXCERPT_LIMIT)
</script>

<template>
  <MarkdownText :text="text" :truncated="truncated" :collapsed-lines="3" max-height-class="max-h-48" />
</template>
