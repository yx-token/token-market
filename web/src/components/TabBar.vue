<script setup lang="ts">
defineProps<{ tabs: { key: string; text: string; count?: number }[] }>()
const active = defineModel<string>({ required: true })
</script>

<template>
  <div class="mb-4 flex gap-1 border-b border-slate-200" role="tablist">
    <button
      v-for="t in tabs"
      :key="t.key"
      role="tab"
      :aria-selected="active === t.key"
      :class="['-mb-px border-b-2 px-3 py-2 text-sm', active === t.key ? 'border-brand-600 font-medium text-brand-700' : 'border-transparent text-slate-500 hover:text-slate-700']"
      @click="active = t.key"
    >
      {{ t.text }}<span v-if="t.count" class="ml-1 rounded-full bg-red-100 px-1.5 text-xs text-red-700">{{ t.count }}</span>
    </button>
  </div>
</template>
