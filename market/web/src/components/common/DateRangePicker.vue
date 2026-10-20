<script setup lang="ts">
/**
 * Element Plus 风格日期范围：侧栏快捷 + 双月历；面板 Teleport 到 body，避免被筛选栏裁切。
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import { usePopoverPosition } from '@/composables/usePopoverPosition'
import CalendarMonth from './CalendarMonth.vue'
import {
  addMonths,
  compareYmd,
  displayZhDate,
  formatYmd,
  parseYmd,
  startOfMonth,
} from './dateUtils'

interface DatePreset {
  label: string
  value: string
  getRange: () => { start: string; end: string }
}

const props = withDefaults(
  defineProps<{
    startDate?: string
    endDate?: string
    disabled?: boolean
  }>(),
  { startDate: '', endDate: '', disabled: false },
)

const emit = defineEmits<{
  'update:startDate': [value: string]
  'update:endDate': [value: string]
  change: [range: { start: string; end: string; startDate: string; endDate: string; preset: string | null }]
}>()

const isOpen = ref(false)
const triggerRef = ref<HTMLElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const { style: panelStyle, open: placePanel, close: unplacePanel } = usePopoverPosition(triggerRef, {
  width: 688,
  maxHeight: 400,
})

const localStart = ref(props.startDate)
const localEnd = ref(props.endDate)
const picking = ref<'start' | 'end'>('start')
const hoverDay = ref('')
const activePreset = ref<string | null>(null)

const leftMonth = ref(startOfMonth(parseYmd(props.startDate) || new Date()))
const rightMonth = computed({
  get: () => addMonths(leftMonth.value, 1),
  set: (d: Date) => {
    leftMonth.value = addMonths(startOfMonth(d), -1)
  },
})

function formatDateToString(date: Date): string {
  return formatYmd(date)
}

const todayStr = computed(() => formatYmd(new Date()))

const presets: DatePreset[] = [
  {
    label: '今天',
    value: 'today',
    getRange: () => ({ start: todayStr.value, end: todayStr.value }),
  },
  {
    label: '昨天',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const y = formatDateToString(d)
      return { start: y, end: y }
    },
  },
  {
    label: '近 7 天',
    value: '7days',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 6)
      return { start: formatDateToString(d), end: todayStr.value }
    },
  },
  {
    label: '近 14 天',
    value: '14days',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 13)
      return { start: formatDateToString(d), end: todayStr.value }
    },
  },
  {
    label: '近 30 天',
    value: '30days',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 29)
      return { start: formatDateToString(d), end: todayStr.value }
    },
  },
  {
    label: '本月',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      return {
        start: formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1)),
        end: todayStr.value,
      }
    },
  },
  {
    label: '上月',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      return {
        start: formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1)),
        end: formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0)),
      }
    },
  },
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const p = presets.find((x) => x.value === activePreset.value)
    if (p) return p.label
  }
  if (props.startDate && props.endDate) {
    if (props.startDate === props.endDate) return displayZhDate(props.startDate)
    return `${displayZhDate(props.startDate)} – ${displayZhDate(props.endDate)}`
  }
  if (props.startDate || props.endDate) {
    return `${props.startDate ? displayZhDate(props.startDate) : '开始'} – ${props.endDate ? displayZhDate(props.endDate) : '结束'}`
  }
  return '选择日期范围'
})

function syncPreset() {
  activePreset.value = null
  if (!localStart.value || !localEnd.value) return
  for (const p of presets) {
    const r = p.getRange()
    if (r.start === localStart.value && r.end === localEnd.value) {
      activePreset.value = p.value
      break
    }
  }
}

function closePanel() {
  isOpen.value = false
  unplacePanel()
  picking.value = 'start'
  hoverDay.value = ''
}

function commit(start: string, end: string, preset: string | null = null) {
  let s = start
  let e = end
  if (s && e && compareYmd(s, e) > 0) [s, e] = [e, s]
  localStart.value = s
  localEnd.value = e
  activePreset.value = preset
  emit('update:startDate', s)
  emit('update:endDate', e)
  emit('change', { start: s, end: e, startDate: s, endDate: e, preset })
  closePanel()
}

function selectPreset(p: DatePreset) {
  const r = p.getRange()
  const start = parseYmd(r.start)
  if (start) leftMonth.value = startOfMonth(start)
  commit(r.start, r.end, p.value)
}

function onPick(ymd: string) {
  activePreset.value = null
  if (picking.value === 'start' || (localStart.value && localEnd.value)) {
    localStart.value = ymd
    localEnd.value = ''
    picking.value = 'end'
    hoverDay.value = ''
    return
  }
  commit(localStart.value, ymd, null)
}

function onClear() {
  commit('', '', null)
}

async function toggle() {
  if (props.disabled) return
  if (isOpen.value) {
    closePanel()
    return
  }
  localStart.value = props.startDate
  localEnd.value = props.endDate
  picking.value = props.startDate && !props.endDate ? 'end' : 'start'
  const base = parseYmd(props.startDate) || parseYmd(props.endDate) || new Date()
  leftMonth.value = startOfMonth(base)
  syncPreset()
  isOpen.value = true
  await placePanel()
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

watch(
  () => [props.startDate, props.endDate] as const,
  ([s, e]) => {
    localStart.value = s
    localEnd.value = e
    syncPreset()
  },
)

onMounted(() => {
  document.addEventListener('mousedown', onOutside)
  document.addEventListener('keydown', onEsc)
  syncPreset()
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
      ref="triggerRef"
      type="button"
      class="drp-trigger"
      :class="{ 'is-open': isOpen, 'is-disabled': disabled }"
      :disabled="disabled"
      @click="toggle"
    >
      <span class="drp-icon"><ConsoleIcon name="calendar" size="sm" /></span>
      <span class="drp-value" :class="{ 'is-placeholder': !startDate && !endDate }">{{ displayValue }}</span>
      <span class="drp-chevron" :class="{ 'is-open': isOpen }"><ConsoleIcon name="chevron-down" size="sm" /></span>
    </button>

    <Teleport to="body">
      <Transition name="drp-pop">
        <div
          v-if="isOpen"
          ref="panelRef"
          class="drp-panel"
          :style="panelStyle"
          role="dialog"
          aria-label="选择日期范围"
        >
          <div class="drp-body">
            <aside class="drp-shortcuts">
              <button
                v-for="p in presets"
                :key="p.value"
                type="button"
                class="drp-shortcut"
                :class="{ 'is-active': activePreset === p.value }"
                @click="selectPreset(p)"
              >
                {{ p.label }}
              </button>
            </aside>
            <div class="drp-calendars">
              <CalendarMonth
                v-model:view-month="leftMonth"
                :start="localStart"
                :end="localEnd"
                :hover="picking === 'end' ? hoverDay : ''"
                :show-next="false"
                @pick="onPick"
                @cell-hover="hoverDay = $event"
                @cell-leave="hoverDay = ''"
              />
              <div class="drp-divider" />
              <CalendarMonth
                :view-month="rightMonth"
                :start="localStart"
                :end="localEnd"
                :hover="picking === 'end' ? hoverDay : ''"
                :show-prev="false"
                @update:view-month="rightMonth = $event"
                @pick="onPick"
                @cell-hover="hoverDay = $event"
                @cell-leave="hoverDay = ''"
              />
            </div>
          </div>
          <div class="drp-footer">
            <span class="drp-hint">
              <template v-if="picking === 'end' && localStart">再选结束日期</template>
              <template v-else-if="localStart && localEnd">
                {{ displayZhDate(localStart) }} – {{ displayZhDate(localEnd) }}
              </template>
              <template v-else>选择开始日期</template>
            </span>
            <button type="button" class="drp-clear" @click="onClear">清除</button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.drp-trigger {
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
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.drp-trigger:hover:not(:disabled) {
  border-color: #c0c4cc;
}
.drp-trigger.is-open,
.drp-trigger:focus {
  outline: none;
  border-color: #0555ce;
  box-shadow: 0 0 0 3px rgba(5, 85, 206, 0.12);
}
.drp-trigger.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.drp-icon {
  color: #a8abb2;
  display: grid;
  place-items: center;
}
.drp-value {
  flex: 1;
  min-width: 0;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.drp-value.is-placeholder {
  font-weight: 400;
  color: #a8abb2;
}
.drp-chevron {
  color: #a8abb2;
  transition: transform 0.2s ease;
}
.drp-chevron.is-open {
  transform: rotate(180deg);
}

.drp-panel {
  display: flex;
  flex-direction: column;
  border-radius: 8px;
  background: #fff;
  border: 1px solid #e4e7ed;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.12);
  overflow: hidden;
  min-width: 688px;
  max-width: calc(100vw - 16px);
}

.drp-body {
  display: flex;
  flex-direction: row;
  flex-wrap: nowrap;
  align-items: stretch;
}

.drp-shortcuts {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 110px;
  flex-shrink: 0;
  padding: 8px 0;
  border-right: 1px solid #e4e7ed;
  background: #fafafa;
}
.drp-shortcut {
  border: 0;
  background: transparent;
  padding: 7px 16px;
  text-align: left;
  font-size: 13px;
  color: #606266;
  cursor: pointer;
}
.drp-shortcut:hover {
  color: #0555ce;
  background: #e6eefa;
}
.drp-shortcut.is-active {
  color: #0555ce;
  font-weight: 600;
}

.drp-calendars {
  display: flex;
  flex-direction: row;
  flex-wrap: nowrap;
  flex: 1;
  min-width: 560px;
}
.drp-divider {
  width: 1px;
  flex-shrink: 0;
  background: #e4e7ed;
  align-self: stretch;
}

.drp-footer {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 16px;
  border-top: 1px solid #e4e7ed;
  background: #fff;
}
.drp-hint {
  font-size: 12px;
  color: #909399;
}
.drp-clear {
  border: 0;
  background: transparent;
  color: #0555ce;
  font-size: 13px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.drp-clear:hover {
  background: #e6eefa;
}

.drp-pop-enter-active,
.drp-pop-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.drp-pop-enter-from,
.drp-pop-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

@media (max-width: 720px) {
  .drp-panel {
    min-width: 0;
  }
  .drp-body {
    flex-direction: column;
  }
  .drp-shortcuts {
    width: 100%;
    flex-direction: row;
    flex-wrap: wrap;
    border-right: 0;
    border-bottom: 1px solid #e4e7ed;
    padding: 8px;
  }
  .drp-calendars {
    flex-direction: column;
    min-width: 0;
  }
  .drp-divider {
    width: 100%;
    height: 1px;
  }
}
</style>
