<script setup lang="ts">
defineProps<{ value: unknown }>()
</script>

<template>
  <pre class="max-h-80 overflow-auto rounded bg-slate-900 p-3 text-xs text-slate-100">{{ JSON.stringify(value, null, 2) }}</pre>
</template>
