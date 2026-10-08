<script setup lang="ts">
import { useToast } from '@/stores/toast'

const toast = useToast()
const tones = { success: 'border-emerald-200 bg-emerald-50 text-emerald-800', error: 'border-red-200 bg-red-50 text-red-800', info: 'border-sky-200 bg-sky-50 text-sky-800' }
</script>

<template>
  <div class="fixed right-4 top-4 z-50 flex w-80 flex-col gap-2" aria-live="polite">
    <div v-for="t in toast.items" :key="t.id" :class="['rounded-md border px-3 py-2 text-sm shadow', tones[t.kind]]" role="status" @click="toast.dismiss(t.id)">
      {{ t.text }}
    </div>
  </div>
</template>
