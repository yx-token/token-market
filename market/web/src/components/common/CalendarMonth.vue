<script setup lang="ts">
/**
 * Element Plus 风格单月历格（周一起始）
 */
import { computed } from 'vue'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import { addMonths, buildMonthCells, startOfMonth, WEEKDAYS_ZH, type CalendarCell } from './dateUtils'

const props = defineProps<{
  viewMonth: Date
  /** 选中起 */
  start?: string
  /** 选中止 */
  end?: string
  /** 悬停预览终点 */
  hover?: string
  /** 是否显示左右翻月（右侧面板可只显示下一月） */
  showPrev?: boolean
  showNext?: boolean
}>()

const emit = defineEmits<{
  'update:viewMonth': [Date]
  pick: [ymd: string]
  'cell-hover': [ymd: string]
  'cell-leave': []
}>()

const title = computed(() => {
  const d = props.viewMonth
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`
})

const cells = computed(() => buildMonthCells(props.viewMonth, 1))

function cellClass(c: CalendarCell) {
  const start = props.start || ''
  const end = props.end || ''
  const hover = props.hover || ''
  let rangeEnd = end
  if (start && !end && hover) rangeEnd = hover
  const selected = (start && c.ymd === start) || (end && c.ymd === end)
  const isStart = !!start && c.ymd === start
  const isEnd = !!end && c.ymd === end
  let inRange = false
  if (start && rangeEnd) {
    const lo = start <= rangeEnd ? start : rangeEnd
    const hi = start <= rangeEnd ? rangeEnd : start
    inRange = c.ymd > lo && c.ymd < hi
  }
  return {
    'is-other': !c.inCurrentMonth,
    'is-today': c.isToday,
    'is-selected': selected,
    'is-start': isStart,
    'is-end': isEnd,
    'is-in-range': inRange,
  }
}

function prevMonth() {
  emit('update:viewMonth', addMonths(startOfMonth(props.viewMonth), -1))
}
function nextMonth() {
  emit('update:viewMonth', addMonths(startOfMonth(props.viewMonth), 1))
}
function prevYear() {
  emit('update:viewMonth', addMonths(startOfMonth(props.viewMonth), -12))
}
function nextYear() {
  emit('update:viewMonth', addMonths(startOfMonth(props.viewMonth), 12))
}
</script>

<template>
  <div class="cal-month">
    <div class="cal-month-head">
      <div class="cal-nav-group">
        <button v-if="showPrev !== false" type="button" class="cal-nav" aria-label="上一年" @click="prevYear">
          <ConsoleIcon name="chevrons-left" size="xs" />
        </button>
        <button v-if="showPrev !== false" type="button" class="cal-nav" aria-label="上一月" @click="prevMonth">
          <ConsoleIcon name="chevron-left" size="xs" />
        </button>
      </div>
      <span class="cal-month-title">{{ title }}</span>
      <div class="cal-nav-group cal-nav-group--end">
        <button v-if="showNext !== false" type="button" class="cal-nav" aria-label="下一月" @click="nextMonth">
          <ConsoleIcon name="chevron-right" size="xs" />
        </button>
        <button v-if="showNext !== false" type="button" class="cal-nav" aria-label="下一年" @click="nextYear">
          <ConsoleIcon name="chevrons-right" size="xs" />
        </button>
      </div>
    </div>
    <div class="cal-weekdays">
      <span v-for="w in WEEKDAYS_ZH" :key="w">{{ w }}</span>
    </div>
    <div class="cal-grid" @mouseleave="emit('cell-leave')">
      <button
        v-for="c in cells"
        :key="c.ymd"
        type="button"
        class="cal-cell"
        :class="cellClass(c)"
        @click="emit('pick', c.ymd)"
        @mouseenter="emit('cell-hover', c.ymd)"
      >
        <span>{{ c.day }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.cal-month {
  width: 280px;
  padding: 8px 12px 12px;
  user-select: none;
}
.cal-month-head {
  display: flex;
  align-items: center;
  margin-bottom: 8px;
  min-height: 28px;
}
.cal-nav-group {
  display: flex;
  align-items: center;
  gap: 2px;
  width: 56px;
  flex-shrink: 0;
}
.cal-nav-group--end {
  justify-content: flex-end;
}
.cal-month-title {
  flex: 1;
  text-align: center;
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}
.cal-nav {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: #a8abb2;
  cursor: pointer;
}
.cal-nav:hover {
  color: #0555ce;
  background: rgba(5, 85, 206, 0.08);
}
.cal-weekdays {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  margin-bottom: 4px;
  font-size: 12px;
  color: #a8abb2;
  text-align: center;
}
.cal-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 2px 0;
}
.cal-cell {
  position: relative;
  display: grid;
  place-items: center;
  height: 32px;
  border: 0;
  padding: 0;
  background: transparent;
  font-size: 12px;
  color: #606266;
  cursor: pointer;
}
.cal-cell span {
  position: relative;
  z-index: 1;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
}
.cal-cell.is-other {
  color: #c0c4cc;
}
.cal-cell.is-today span {
  color: #0555ce;
  font-weight: 600;
}
.cal-cell.is-in-range {
  background: #e6eefa;
}
.cal-cell.is-selected span,
.cal-cell.is-start span,
.cal-cell.is-end span {
  background: #0555ce;
  color: #fff;
  font-weight: 600;
}
.cal-cell.is-start {
  border-radius: 16px 0 0 16px;
  background: #e6eefa;
}
.cal-cell.is-end {
  border-radius: 0 16px 16px 0;
  background: #e6eefa;
}
.cal-cell.is-start.is-end {
  border-radius: 16px;
  background: transparent;
}
.cal-cell:hover:not(.is-selected):not(.is-start):not(.is-end) span {
  background: #f2f3f5;
}
</style>
