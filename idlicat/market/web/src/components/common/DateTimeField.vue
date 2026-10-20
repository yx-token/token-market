<script setup lang="ts">
/**
 * Element Plus 风格日期时间；面板 Teleport 到 body。
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
  { modelValue: '', disabled: false, placeholder: '选择日期时间' },
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
  maxHeight: 440,
})
const datePart = ref('')
const hour = ref('00')
const minute = ref('00')
const viewMonth = ref(startOfMonth(new Date()))

const hours = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0'))
const minutes = Array.from({ length: 60 }, (_, i) => String(i).padStart(2, '0'))

function splitValue(v: string) {
  if (!v) {
    datePart.value = ''
    hour.value = '00'
    minute.value = '00'
    return
  }
  const [d, t = '00:00'] = v.split('T')
  datePart.value = d || ''
  const [h = '00', m = '00'] = t.split(':')
  hour.value = h.padStart(2, '0')
  minute.value = m.padStart(2, '0')
}

watch(
  () => props.modelValue,
  (v) => {
    splitValue(v || '')
    const d = parseYmd(datePart.value)
    if (d) viewMonth.value = startOfMonth(d)
  },
  { immediate: true },
)

const displayValue = computed(() => {
  if (!props.modelValue) return props.placeholder
  const [d, t] = props.modelValue.split('T')
  return `${displayZhDate(d)} ${t || '00:00'}`
})

function composed() {
  if (!datePart.value) return ''
  return `${datePart.value}T${hour.value}:${minute.value}`
}

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
  splitValue(props.modelValue || '')
  viewMonth.value = startOfMonth(parseYmd(datePart.value) || new Date())
  isOpen.value = true
  await placePanel()
}

function onPick(ymd: string) {
  datePart.value = ymd
}

function apply() {
  if (!datePart.value) return
  const v = composed()
  emit('update:modelValue', v)
  emit('change', v)
  closePanel()
}

function clear() {
  datePart.value = ''
  hour.value = '00'
  minute.value = '00'
  emit('update:modelValue', '')
  emit('change', '')
  closePanel()
}

function setToday() {
  datePart.value = formatYmd(new Date())
  const now = new Date()
  hour.value = String(now.getHours()).padStart(2, '0')
  minute.value = String(now.getMinutes()).padStart(2, '0')
  viewMonth.value = startOfMonth(now)
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
      class="dtf-trigger"
      :class="{ 'is-open': isOpen, 'is-disabled': disabled }"
      :disabled="disabled"
      @click="toggle"
    >
      <span class="dtf-icon"><ConsoleIcon name="clock" size="sm" /></span>
      <span class="dtf-value" :class="{ 'is-placeholder': !modelValue }">{{ displayValue }}</span>
      <span class="dtf-chevron" :class="{ 'is-open': isOpen }"><ConsoleIcon name="chevron-down" size="sm" /></span>
    </button>

    <Teleport to="body">
      <Transition name="dtf-pop">
        <div v-if="isOpen" ref="panelRef" class="dtf-panel" :style="panelStyle" role="dialog" aria-label="选择日期时间">
          <CalendarMonth v-model:view-month="viewMonth" :start="datePart" :end="datePart" @pick="onPick" />
          <div class="dtf-time">
            <label class="dtf-time-label">时间</label>
            <select v-model="hour" class="dtf-select" aria-label="时">
              <option v-for="h in hours" :key="h" :value="h">{{ h }}</option>
            </select>
            <span class="dtf-colon">:</span>
            <select v-model="minute" class="dtf-select" aria-label="分">
              <option v-for="m in minutes" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
          <div class="dtf-footer">
            <button type="button" class="dtf-link" @click="clear">清除</button>
            <div class="dtf-footer-right">
              <button type="button" class="dtf-link" @click="setToday">此刻</button>
              <button type="button" class="dtf-apply" :disabled="!datePart" @click="apply">确定</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.dtf-trigger {
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
.dtf-trigger.is-open,
.dtf-trigger:focus {
  outline: none;
  border-color: #0555ce;
  box-shadow: 0 0 0 3px rgba(5, 85, 206, 0.12);
}
.dtf-trigger.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.dtf-icon {
  color: #a8abb2;
}
.dtf-value {
  flex: 1;
  min-width: 0;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dtf-value.is-placeholder {
  font-weight: 400;
  color: #a8abb2;
}
.dtf-chevron {
  color: #a8abb2;
  transition: transform 0.2s ease;
}
.dtf-chevron.is-open {
  transform: rotate(180deg);
}
.dtf-panel {
  border-radius: 8px;
  background: #fff;
  border: 1px solid #e4e7ed;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.12);
  overflow: hidden;
}
.dtf-time {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-top: 1px solid #e4e7ed;
}
.dtf-time-label {
  font-size: 12px;
  color: #909399;
  margin-right: 4px;
}
.dtf-select {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 13px;
  color: #303133;
  background: #fff;
}
.dtf-select:focus {
  outline: none;
  border-color: #0555ce;
}
.dtf-colon {
  color: #909399;
  font-weight: 600;
}
.dtf-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px 10px;
  border-top: 1px solid #e4e7ed;
}
.dtf-footer-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dtf-link {
  border: 0;
  background: transparent;
  color: #0555ce;
  font-size: 13px;
  cursor: pointer;
  padding: 4px 6px;
}
.dtf-apply {
  border: 1px solid #0555ce;
  border-radius: 4px;
  background: #0555ce;
  color: #fff;
  font-size: 13px;
  padding: 5px 14px;
  cursor: pointer;
}
.dtf-apply:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.dtf-pop-enter-active,
.dtf-pop-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.dtf-pop-enter-from,
.dtf-pop-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
