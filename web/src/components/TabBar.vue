<script setup lang="ts">
defineProps<{ tabs: { key: string; text: string; count?: number }[] }>()
const active = defineModel<string>({ required: true })
</script>

<template>
  <div class="mb-5 flex gap-6 overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--rule-strong)]" role="tablist">
    <button
      v-for="t in tabs"
      :key="t.key"
      role="tab"
      :aria-selected="active === t.key"
      :class="[
        'flex items-center gap-1.5 whitespace-nowrap border-b-2 pb-2.5 pt-1 text-[14px] transition-colors',
        active === t.key ? 'border-brand-500 font-medium text-ink-900' : 'border-transparent text-ink-500 hover:text-ink-900',
      ]"
      @click="active = t.key"
    >
      {{ t.text }}<span v-if="t.count" class="bg-brand-500 px-1.5 font-mono text-[10.5px] leading-[16px] text-white">{{ t.count }}</span>
    </button>
  </div>
</template>
