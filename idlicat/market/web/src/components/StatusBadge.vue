<script setup lang="ts">
// 状态印记：细边框 + 浅底 + 前置方点；"purple" 档为墨色实心印，用于需要突出的类型（如平台回收、平台检测）。
import { computed } from 'vue'
import { label, type Tone } from '@/utils/labels'

const props = defineProps<{ kind: string; value?: string | null }>()
const l = computed(() => label(props.kind, props.value))
const tones: Record<Tone, string> = {
  gray: 'chip-gray',
  green: 'chip-green',
  blue: 'chip-blue',
  yellow: 'chip-yellow',
  red: 'chip-red',
  purple: 'chip-ink',
}
</script>

<template>
  <span :class="['chip', tones[l.tone]]">{{ l.text }}</span>
</template>
