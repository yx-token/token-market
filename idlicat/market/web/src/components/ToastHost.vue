<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useToast } from '@/stores/toast'

const toast = useToast()
const route = useRoute()
/** 市场首页用深色提示；买卖中心 / 控制台用浅色，避免白底工作台上看不清 */
const darkToast = computed(() => {
  const p = route.path
  return p === '/' || p.startsWith('/market') || p.startsWith('/login') || p.startsWith('/register')
})
const lightMeta = {
  success: { bar: 'bg-emerald-500', tag: '完成', tagCls: 'text-emerald-600' },
  error: { bar: 'bg-red-500', tag: '出错', tagCls: 'text-red-600' },
  info: { bar: 'bg-[#0555ce]', tag: '提示', tagCls: 'text-[#0555ce]' },
}
const darkMeta = {
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
          darkToast
            ? 'border border-ink-900 bg-ink-900 text-white shadow-hard-red'
            : 'rounded-xl border border-black/10 bg-white text-[#1f1f1f] shadow-lg',
        ]"
        role="status"
        @click="toast.dismiss(t.id)"
      >
        <span :class="['w-1 shrink-0', (darkToast ? darkMeta : lightMeta)[t.kind].bar]" />
        <div class="flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5">
          <span
            :class="[
              'mt-px shrink-0 text-[10.5px] font-semibold uppercase tracking-wide',
              darkToast ? 'font-mono tracking-[0.14em]' : '',
              (darkToast ? darkMeta : lightMeta)[t.kind].tagCls,
            ]"
          >
            {{ (darkToast ? darkMeta : lightMeta)[t.kind].tag }}
          </span>
          <span class="min-w-0 break-words leading-relaxed">{{ t.text }}</span>
        </div>
      </div>
    </TransitionGroup>
  </div>
</template>
