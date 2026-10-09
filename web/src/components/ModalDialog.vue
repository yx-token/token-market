<script lang="ts">
// 已打开的对话框（后打开的在上层），Esc 只关闭最上层的一个
const stack: symbol[] = []
</script>

<script setup lang="ts">
// 对话框：一张压在墨色遮罩上的纸，顶部朱红细条，硬投影。
import { onUnmounted, watch } from 'vue'

defineProps<{ title: string; wide?: boolean }>()
const open = defineModel<boolean>('open', { default: false })

const id = Symbol('dialog')
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && stack[stack.length - 1] === id) open.value = false
}
function release() {
  const i = stack.indexOf(id)
  if (i >= 0) stack.splice(i, 1)
  window.removeEventListener('keydown', onKey)
}
watch(
  open,
  (v) => {
    release()
    if (v) {
      stack.push(id)
      window.addEventListener('keydown', onKey)
    }
  },
  { immediate: true },
)
onUnmounted(release)
</script>

<template>
  <Teleport to="body">
    <Transition enter-active-class="transition duration-200 ease-out" enter-from-class="opacity-0" leave-active-class="transition duration-150 ease-in" leave-to-class="opacity-0">
      <div v-if="open" class="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-ink-900/45 p-4 pt-[8vh] backdrop-blur-[2px]" @click.self="open = false">
        <div
          :class="['relative w-full animate-rise border border-ink-900 bg-white shadow-hard', wide ? 'max-w-3xl' : 'max-w-lg']"
          role="dialog"
          :aria-label="title"
        >
          <div class="h-1 bg-brand-500" />
          <div class="flex items-center justify-between border-b border-ink-200 px-6 py-4">
            <h3 class="font-display text-[18px] font-bold tracking-wide">{{ title }}</h3>
            <button class="grid h-7 w-7 place-items-center border border-transparent text-ink-500 transition-colors hover:border-ink-900 hover:text-ink-900" aria-label="关闭" @click="open = false">✕</button>
          </div>
          <div class="space-y-4 px-6 py-5">
            <slot />
          </div>
          <div v-if="$slots.footer" class="flex flex-wrap justify-end gap-2 border-t border-ink-200 bg-paper px-6 py-3.5">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
