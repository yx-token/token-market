<script setup lang="ts">
import { useIsConsole } from '@/composables/useIsConsole'

defineProps<{ tabs: { key: string; text: string; count?: number }[] }>()
const active = defineModel<string>({ required: true })
const isConsole = useIsConsole()
</script>

<template>
  <div
    v-if="isConsole"
    class="mb-4 flex gap-1 overflow-x-auto overflow-y-hidden border-b border-[var(--color-border,#e8e8e8)]"
    role="tablist"
  >
    <button
      v-for="t in tabs"
      :key="t.key"
      role="tab"
      :aria-selected="active === t.key"
      :class="[
        'relative flex items-center gap-1.5 whitespace-nowrap px-3 pb-2.5 pt-1 text-[13px] font-medium transition-colors',
        active === t.key ? 'text-[#0555ce]' : 'text-gray-500 hover:text-gray-800',
      ]"
      @click="active = t.key"
    >
      {{ t.text }}
      <span
        v-if="t.count"
        :class="[
          'rounded-md px-1.5 text-[10.5px] leading-[16px]',
          active === t.key ? 'bg-[#0555ce] text-white' : 'bg-gray-100 text-gray-600',
        ]"
      >
        {{ t.count }}
      </span>
      <span
        v-if="active === t.key"
        class="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-[#0555ce]"
      />
    </button>
  </div>
  <div
    v-else
    class="mb-5 flex gap-6 overflow-x-auto overflow-y-hidden shadow-[inset_0_-1px_0_var(--rule-strong)]"
    role="tablist"
  >
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
      {{ t.text }}
      <span v-if="t.count" class="bg-brand-500 px-1.5 font-mono text-[10.5px] leading-[16px] text-white">{{ t.count }}</span>
    </button>
  </div>
</template>
