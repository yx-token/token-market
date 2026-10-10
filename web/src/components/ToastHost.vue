<script setup lang="ts">
import { useToast } from '@/stores/toast'
import { useIsConsole } from '@/composables/useIsConsole'

const toast = useToast()
const isConsole = useIsConsole()
const meta = {
  success: { bar: 'bg-emerald-500', tag: '完成', tagCls: 'text-emerald-600' },
  error: { bar: 'bg-red-500', tag: '出错', tagCls: 'text-red-600' },
  info: { bar: 'bg-[#0555ce]', tag: '提示', tagCls: 'text-[#0555ce]' },
}
const marketMeta = {
  success: { bar: 'bg-emerald-400', tag: '完成', tagCls: 'text-emerald-300' },
  error: { bar: 'bg-brand-500', tag: '出错', tagCls: 'text-brand-300' },
  info: { bar: 'bg-sky-400', tag: '提示', tagCls: 'text-sky-200' },
}
</script>

<template>
  <div class="pointer-events-none fixed right-4 top-24 z-50 flex w-[340px] flex-col gap-2" aria-live="polite">
    <TransitionGroup
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="translate-x-6 opacity-0"
      leave-active-class="transition duration-200 ease-in"
      leave-to-class="translate-x-6 opacity-0"
    >
      <div
        v-for="t in toast.items"
        :key="t.id"
        :class="[
          'pointer-events-auto flex cursor-pointer overflow-hidden text-[13px]',
          isConsole
            ? 'rounded-xl border border-white/50 bg-white/95 text-gray-800 shadow-lg backdrop-blur-md'
            : 'border border-ink-900 bg-ink-900 text-white shadow-hard-red',
        ]"
        role="status"
        @click="toast.dismiss(t.id)"
      >
        <span :class="['w-1 shrink-0', (isConsole ? meta : marketMeta)[t.kind].bar]" />
        <div class="flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5">
          <span
            :class="[
              'mt-px shrink-0 text-[10.5px] font-semibold uppercase tracking-wide',
              isConsole ? '' : 'font-mono tracking-[0.14em]',
              (isConsole ? meta : marketMeta)[t.kind].tagCls,
            ]"
          >
            {{ (isConsole ? meta : marketMeta)[t.kind].tag }}
          </span>
          <span class="min-w-0 break-words leading-relaxed">{{ t.text }}</span>
        </div>
      </div>
    </TransitionGroup>
  </div>
</template>
