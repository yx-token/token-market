<script setup lang="ts">
defineProps<{ title: string; wide?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-slate-900/40 p-4 pt-16" @click.self="open = false">
      <div :class="['w-full rounded-lg bg-white shadow-xl', wide ? 'max-w-3xl' : 'max-w-lg']" role="dialog" :aria-label="title">
        <div class="flex items-center justify-between border-b border-slate-200 px-5 py-3">
          <h3 class="font-semibold text-slate-800">{{ title }}</h3>
          <button class="text-slate-400 hover:text-slate-600" aria-label="关闭" @click="open = false">✕</button>
        </div>
        <div class="space-y-4 px-5 py-4">
          <slot />
        </div>
        <div v-if="$slots.footer" class="flex justify-end gap-2 border-t border-slate-200 px-5 py-3">
          <slot name="footer" />
        </div>
      </div>
    </div>
  </Teleport>
</template>
