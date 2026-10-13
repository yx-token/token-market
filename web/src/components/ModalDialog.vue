<script lang="ts">
// 已打开的对话框（后打开的在上层），Esc 只关闭最上层的一个
const stack: symbol[] = []
</script>

<script setup lang="ts">
import { computed, onUnmounted, watch } from 'vue'
import { documentIsConsole, useIsConsole } from '@/composables/useIsConsole'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'

defineProps<{ title: string; wide?: boolean; size?: 'default' | 'wide' | 'xl' }>()
const open = defineModel<boolean>('open', { default: false })
const routeIsConsole = useIsConsole()
/** Teleport 到 body 后仍以 html 类为准，避免串成市场账本弹层 */
const isConsole = computed(() => routeIsConsole.value || documentIsConsole())

const id = Symbol('dialog')
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && !e.defaultPrevented && stack[stack.length - 1] === id) open.value = false
}
function release() {
  const i = stack.indexOf(id)
  if (i >= 0) stack.splice(i, 1)
  window.removeEventListener('keydown', onKey)
  if (!stack.length) document.body.style.removeProperty('overflow')
}
watch(
  open,
  (v) => {
    release()
    if (v) {
      stack.push(id)
      window.addEventListener('keydown', onKey)
      document.body.style.overflow = 'hidden'
    }
  },
  { immediate: true },
)
onUnmounted(release)
</script>

<template>
  <Teleport to="body">
    <Transition name="yh-modal">
      <div
        v-if="open"
        :class="['yh-modal-mask', isConsole ? 'yh-modal-mask--console' : 'yh-modal-mask--market', size === 'xl' ? 'yh-modal-mask--xl' : '']"
        @click.self="open = false"
      >
        <div
          :class="[
            'yh-modal-panel',
            isConsole ? 'yh-modal-panel--console' : 'yh-modal-panel--market',
            size === 'xl' ? 'yh-modal-panel--xl' : wide || size === 'wide' ? 'yh-modal-panel--wide' : '',
          ]"
          role="dialog"
          aria-modal="true"
          :aria-label="title"
        >
          <div v-if="!isConsole" class="yh-modal-accent" aria-hidden="true" />
          <div class="yh-modal-head">
            <h3 class="yh-modal-title">{{ title }}</h3>
            <button type="button" class="yh-modal-close" aria-label="关闭" @click="open = false">
              <ConsoleIcon v-if="isConsole" name="x" size="sm" />
              <span v-else aria-hidden="true">✕</span>
            </button>
          </div>
          <div class="yh-modal-body">
            <slot />
          </div>
          <div v-if="$slots.footer" class="yh-modal-foot">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
