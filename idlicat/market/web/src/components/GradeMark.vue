<script setup lang="ts">
import { useIsConsole } from '@/composables/useIsConsole'

defineProps<{ grade?: string | null }>()
const isConsole = useIsConsole()
</script>

<template>
  <span
    v-if="grade"
    :class="[
      'inline-grid h-7 w-7 place-items-center rounded-full border text-[13px] font-bold',
      isConsole ? 'font-sans' : 'font-display',
      grade === 'S'
        ? isConsole
          ? 'border-[#0555ce] bg-[#0555ce] text-white'
          : 'border-brand-500 bg-brand-500 text-white'
        : grade === 'A'
          ? isConsole
            ? 'border-gray-800 text-gray-800'
            : 'border-ink-900 text-ink-900'
          : isConsole
            ? 'border-gray-300 text-gray-500'
            : 'border-ink-300 text-ink-600',
    ]"
    :title="`${grade} 级`"
  >
    {{ grade }}
  </span>
  <span v-else :class="isConsole ? 'text-xs text-gray-400' : 'font-mono text-xs text-ink-400'" title="未定级">—</span>
</template>
