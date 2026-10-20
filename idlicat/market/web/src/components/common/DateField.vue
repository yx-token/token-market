<script setup lang="ts">
/**
 * Element Plus 风格单日选择；面板 Teleport 到 body。
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import { usePopoverPosition } from '@/composables/usePopoverPosition'
import CalendarMonth from './CalendarMonth.vue'
import { displayZhDate, formatYmd, parseYmd, startOfMonth } from './dateUtils'

const props = withDefaults(
  defineProps<{
    id?: string
    modelValue?: string
    disabled?: boolean
    placeholder?: string
  }>(),
  { modelValue: '', disabled: false, placeholder: '选择日期' },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const isOpen = ref(false)
const triggerRef = ref<HTMLElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const { style: panelStyle, open: placePanel, close: unplacePanel } = usePopoverPosition(triggerRef, {
  width: 304,
  maxHeight: 360,
})
const viewMonth = ref(startOfMonth(parseYmd(props.modelValue) || new Date()))

const displayValue = computed(() => (props.modelValue ? displayZhDate(props.modelValue) : props.placeholder))

watch(
  () => props.modelValue,
  (v) => {
    const d = parseYmd(v)
    if (d) viewMonth.value = startOfMonth(d)
  },
)

function closePanel() {
  isOpen.value = false
  unplacePanel()
}

async function toggle() {
  if (props.disabled) return
  if (isOpen.value) {
    closePanel()
    return
  }
  viewMonth.value = startOfMonth(parseYmd(props.modelValue) || new Date())
  isOpen.value = true
  await placePanel()
}

function onPick(ymd: string) {
  emit('update:modelValue', ymd)
  emit('change', ymd)
  closePanel()
}

function clear() {
  emit('update:modelValue', '')
  emit('change', '')
  closePanel()
}

function onOutside(e: MouseEvent) {
  if (!isOpen.value) return
  const t = e.target as Node
  if (triggerRef.value?.contains(t) || panelRef.value?.contains(t)) return
  closePanel()
}
function onEsc(e: KeyboardEvent) {
  if (e.key === 'Escape' && isOpen.value) closePanel()
}

onMounted(() => {
  document.addEventListener('mousedown', onOutside)
  document.addEventListener('keydown', onEsc)
})
onUnmounted(() => {
  document.removeEventListener('mousedown', onOutside)
  document.removeEventListener('keydown', onEsc)
  unplacePanel()
})
</script>

<template>
  <div class="relative w-full">
    <button
      :id="id"
      ref="triggerRef"
      type="button"
      class="df-trigger"
      :class="{ 'is-open': isOpen, 'is-disabled': disabled }"
      :disabled="disabled"
      @click="toggle"
    >
      <span class="df-icon"><ConsoleIcon name="calendar" size="sm" /></span>
      <span class="df-value" :class="{ 'is-placeholder': !modelValue }">{{ displayValue }}</span>
      <span class="df-chevron" :class="{ 'is-open': isOpen }"><ConsoleIcon name="chevron-down" size="sm" /></span>
    </button>

    <Teleport to="body">
      <Transition name="df-pop">
        <div v-if="isOpen" ref="panelRef" class="df-panel" :style="panelStyle" role="dialog" aria-label="选择日期">
          <CalendarMonth v-model:view-month="viewMonth" :start="modelValue" :end="modelValue" @pick="onPick" />
          <div class="df-footer">
            <button type="button" class="df-link" @click="clear">清除</button>
            <button type="button" class="df-link" @click="onPick(formatYmd(new Date()))">今天</button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.df-trigger {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  padding: 0.375rem 0.625rem;
  font-size: 0.75rem;
  color: #374151;
  background: #fff;
  border: 1px solid #e5e7eb;
  cursor: pointer;
  text-align: left;
}
.df-trigger.is-open,
.df-trigger:focus {
  outline: none;
  border-color: #0555ce;
  box-shadow: 0 0 0 3px rgba(5, 85, 206, 0.12);
}
.df-trigger.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.df-icon {
  color: #a8abb2;
}
.df-value {
  flex: 1;
  min-width: 0;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.df-value.is-placeholder {
  font-weight: 400;
  color: #a8abb2;
}
.df-chevron {
  color: #a8abb2;
  transition: transform 0.2s ease;
}
.df-chevron.is-open {
  transform: rotate(180deg);
}
.df-panel {
  border-radius: 8px;
  background: #fff;
  border: 1px solid #e4e7ed;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.12);
  overflow: hidden;
}
.df-footer {
  display: flex;
  justify-content: space-between;
  padding: 4px 16px 10px;
  border-top: 1px solid #e4e7ed;
}
.df-link {
  border: 0;
  background: transparent;
  color: #0555ce;
  font-size: 13px;
  cursor: pointer;
  padding: 4px 0;
}
.df-pop-enter-active,
.df-pop-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.df-pop-enter-from,
.df-pop-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
