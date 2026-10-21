<script setup lang="ts">
// 列表刷新按钮：加载中禁用并转动图标，点击事件透传到 button。
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'

defineProps<{ loading?: boolean }>()
</script>

<template>
  <button type="button" class="btn-secondary" :disabled="loading">
    <ConsoleIcon name="refresh-cw" size="xs" :class="{ 'animate-spin': loading }" />
    刷新
  </button>
</template>
