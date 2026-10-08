<script setup lang="ts">
import { computed } from 'vue'
import { label, type Tone } from '@/utils/labels'

const props = defineProps<{ kind: string; value?: string | null }>()
const l = computed(() => label(props.kind, props.value))
const tones: Record<Tone, string> = {
  gray: 'bg-slate-100 text-slate-600',
  green: 'bg-emerald-50 text-emerald-700',
  blue: 'bg-sky-50 text-sky-700',
  yellow: 'bg-amber-50 text-amber-700',
  red: 'bg-red-50 text-red-700',
  purple: 'bg-violet-50 text-violet-700',
}
</script>

<template>
  <span :class="['inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium', tones[l.tone]]">{{ l.text }}</span>
</template>
