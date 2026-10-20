<script setup lang="ts">
defineProps<{ label: string; value: string | number; hint?: string; accent?: boolean }>()
</script>

<template>
  <div class="stat">
    <div class="stat-label">{{ label }}</div>
    <div :class="['stat-value', accent ? '!text-brand-600' : '']">{{ value }}</div>
    <div v-if="hint" class="hint">{{ hint }}</div>
  </div>
</template>
