<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" :class="['modal-content', widthClasses]" tabindex="-1" @click.stop>
          <!-- Header -->
          <div class="modal-header">
            <h3 :id="dialogId" class="modal-title">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              @click="emit('close')"
              class="-mr-2 rounded-xl p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:text-dark-500 dark:hover:bg-dark-700 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900"
              :aria-label="closeLabel"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" class="modal-body">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let dialogIdCounter = 0
const openDialogs = new Set<string>()
// 弹窗打开顺序：只有最上层的弹窗响应 Escape，Tab 焦点循环也只作用于它。
const dialogStack: string[] = []

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  '[tabindex]:not([tabindex="-1"])',
  '[contenteditable="true"]'
].join(', ')

function isElementVisible(element: HTMLElement): boolean {
  if (element.hidden) return false
  if (element.getAttribute('aria-hidden') === 'true') return false
  if (element.closest('[hidden], [aria-hidden="true"]')) return false
  // jsdom 里 offsetWidth/offsetHeight 恒为 0，仅在真实布局下用它过滤。
  if (element.offsetWidth === 0 && element.offsetHeight === 0) {
    const style = window.getComputedStyle(element)
    if (style.display === 'none' || style.visibility === 'hidden') return false
  }
  return true
}
</script>

<script setup lang="ts">
import { computed, watch, onMounted, onUnmounted, ref, nextTick } from 'vue'
import Icon from '@/components/icons/Icon.vue'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
let previousActiveElement: HTMLElement | null = null

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  zIndex: 50
})

const emit = defineEmits<Emits>()

const closeLabel = 'Close dialog'

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

const isTopmostDialog = () => dialogStack[dialogStack.length - 1] === dialogId

const handleEscape = (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || !props.show || !props.closeOnEscape) return
  // 多层弹窗只关闭最顶层，避免一次 Escape 把所有弹窗关掉。
  if (!isTopmostDialog()) return
  event.stopPropagation()
  event.preventDefault()
  emit('close')
}

const getFocusableElements = (): HTMLElement[] => {
  if (!dialogRef.value) return []
  return Array.from(dialogRef.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => isElementVisible(element)
  )
}

const handleFocusTrap = (event: KeyboardEvent) => {
  if (event.key !== 'Tab' || !props.show || !isTopmostDialog()) return
  const focusable = getFocusableElements()
  if (focusable.length === 0) {
    // 没有可聚焦元素时，把焦点留在对话框容器上。
    event.preventDefault()
    dialogRef.value?.focus()
    return
  }
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  const active = document.activeElement as HTMLElement | null

  if (event.shiftKey) {
    if (active === first || !active || !dialogRef.value?.contains(active)) {
      event.preventDefault()
      last.focus()
    }
  } else if (active === last || !active || !dialogRef.value?.contains(active)) {
    event.preventDefault()
    first.focus()
  }
}

const handleKeydown = (event: KeyboardEvent) => {
  handleEscape(event)
  handleFocusTrap(event)
}

const updateScrollLock = (isOpen: boolean) => {
  if (isOpen) {
    openDialogs.add(dialogId)
    if (!dialogStack.includes(dialogId)) dialogStack.push(dialogId)
  } else {
    openDialogs.delete(dialogId)
    const index = dialogStack.indexOf(dialogId)
    if (index >= 0) dialogStack.splice(index, 1)
  }
  document.body.classList.toggle('modal-open', openDialogs.size > 0)
}

const restoreFocus = () => {
  const target = previousActiveElement
  previousActiveElement = null
  // 如果原焦点元素已被移除（例如它属于另一个已关闭的弹窗），不再强行聚焦。
  if (target && target.isConnected && typeof target.focus === 'function') {
    target.focus()
  }
}

const handleDialogClosed = () => {
  const wasTopmost = isTopmostDialog()
  updateScrollLock(false)
  // 非顶层弹窗关闭时不能把焦点抢出当前弹窗。
  if (wasTopmost) restoreFocus()
  else previousActiveElement = null
}

// Prevent body scroll when modal is open and manage focus
watch(
  () => props.show,
  async (isOpen) => {
    if (isOpen) {
      // 保存当前焦点元素
      previousActiveElement = document.activeElement as HTMLElement | null
      // 使用CSS类而不是直接操作style,更易于管理多个对话框
      updateScrollLock(true)

      // 等待DOM更新后设置焦点到对话框
      await nextTick()
      if (!props.show || !isTopmostDialog()) return
      if (modalBodyRef.value) {
        modalBodyRef.value.scrollTop = 0
      }
      if (dialogRef.value) {
        const firstFocusable = getFocusableElements()[0]
        ;(firstFocusable ?? dialogRef.value).focus()
      }
    } else {
      handleDialogClosed()
    }
  },
  { immediate: true }
)

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
  // 确保组件卸载时移除滚动锁定，并把焦点还给触发元素。
  handleDialogClosed()
})
</script>
