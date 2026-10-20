<script setup lang="ts">
defineProps<{ value: unknown }>()
</script>

<template>
  <pre class="codeblock max-h-80 overflow-auto">{{ JSON.stringify(value, null, 2) }}</pre>
</template>
