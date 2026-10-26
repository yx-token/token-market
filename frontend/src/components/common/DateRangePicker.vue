<template>
  <!-- Element 风格：datetimerange（快捷侧栏 + 双日历 + 清空/确定） -->
  <div v-if="mode === 'datetimerange'" class="relative w-full date-range-el" ref="containerRef">
    <el-config-provider :locale="elLocale">
      <el-date-picker
        v-model="rangeModel"
        type="datetimerange"
        class="date-range-el__picker"
        range-separator="-"
        :start-placeholder="startPlaceholder"
        :end-placeholder="endPlaceholder"
        format="YYYY-MM-DD HH:mm:ss"
        value-format="YYYY-MM-DD HH:mm:ss"
        :shortcuts="elShortcuts"
        :disabled="disabled"
        :clearable="clearable"
        :default-time="defaultTime"
        teleported
        @change="onElChange"
      />
    </el-config-provider>
  </div>

  <!-- 默认：预设快捷 + 自定义日期（Usage / Dashboard） -->
  <div v-else class="relative w-full" ref="containerRef">
    <button
      type="button"
      @click="toggle"
      :class="['date-picker-trigger', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="date-picker-dropdown">
      <div v-if="isOpen" class="date-picker-dropdown">
        <div class="date-picker-presets">
          <button
            v-for="preset in presets"
            :key="preset.value"
            @click="selectPreset(preset)"
            :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
          >
            {{ t(preset.labelKey) }}
          </button>
        </div>

        <div class="date-picker-divider"></div>

        <div class="date-picker-custom">
          <div class="date-picker-field">
            <label class="date-picker-label">{{ t('dates.startDate') }}</label>
            <input
              type="date"
              v-model="localStartDate"
              :max="localEndDate || tomorrow()"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
          <div class="date-picker-separator">
            <Icon name="arrowRight" size="sm" class="text-gray-400" />
          </div>
          <div class="date-picker-field">
            <label class="date-picker-label">{{ t('dates.endDate') }}</label>
            <input
              type="date"
              v-model="localEndDate"
              :min="localStartDate"
              :max="tomorrow()"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
        </div>

        <div class="date-picker-actions">
          <button @click="apply" class="date-picker-apply">
            {{ t('dates.apply') }}
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElConfigProvider, ElDatePicker } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import Icon from '@/components/icons/Icon.vue'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

interface Props {
  startDate: string
  endDate: string
  /** preset=默认快捷面板；datetimerange=Element 风格日期时间范围 */
  mode?: 'preset' | 'datetimerange'
  disabled?: boolean
  clearable?: boolean
  startPlaceholder?: string
  endPlaceholder?: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = withDefaults(defineProps<Props>(), {
  mode: 'preset',
  disabled: false,
  clearable: true,
  startPlaceholder: '开始时间',
  endPlaceholder: '结束时间',
})
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>('last24Hours')

const elLocale = computed(() => (locale.value === 'zh' || String(locale.value).startsWith('zh') ? zhCn : en))

const defaultTime: [Date, Date] = [
  new Date(2000, 0, 1, 0, 0, 0),
  new Date(2000, 0, 1, 23, 59, 59),
]

function rangeEndingNow(daysBack: number): [Date, Date] {
  const end = new Date()
  end.setHours(23, 59, 59, 999)
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  start.setDate(start.getDate() - daysBack)
  return [start, end]
}

const elShortcuts = computed(() => [
  { text: '最近一周', value: () => rangeEndingNow(6) },
  { text: '最近两周', value: () => rangeEndingNow(13) },
  { text: '最近一个月', value: () => rangeEndingNow(29) },
  { text: '最近三个月', value: () => rangeEndingNow(89) },
  { text: '最近半年', value: () => rangeEndingNow(179) },
  { text: '最近一年', value: () => rangeEndingNow(364) },
])

const rangeModel = computed<[string, string] | null>({
  get() {
    if (!props.startDate && !props.endDate) return null
    const start = props.startDate
      ? props.startDate.length <= 10
        ? `${props.startDate} 00:00:00`
        : props.startDate
      : ''
    const end = props.endDate
      ? props.endDate.length <= 10
        ? `${props.endDate} 23:59:59`
        : props.endDate
      : ''
    if (!start || !end) return null
    return [start, end]
  },
  set(val) {
    if (!val || val.length !== 2) {
      emit('update:startDate', '')
      emit('update:endDate', '')
      emit('change', { startDate: '', endDate: '', preset: null })
      return
    }
    emit('update:startDate', val[0])
    emit('update:endDate', val[1])
    emit('change', { startDate: val[0], endDate: val[1], preset: null })
  },
})

function onElChange(val: [string, string] | null) {
  if (!val) {
    emit('update:startDate', '')
    emit('update:endDate', '')
    emit('change', { startDate: '', endDate: '', preset: null })
  }
}

const today = () => formatDateToString(new Date())

const tomorrow = () => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatDateToString(d)
}

const formatDateToString = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today()
      return { start: t, end: t }
    },
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatDateToString(d)
      return { start: yesterday, end: yesterday }
    },
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    getRange: () => {
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return {
        start: formatDateToString(start),
        end: formatDateToString(end),
      }
    },
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today()
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatDateToString(d)
      return { start, end }
    },
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today()
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatDateToString(d)
      return { start, end }
    },
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today()
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatDateToString(d)
      return { start, end }
    },
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today() }
    },
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    },
  },
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr + 'T00:00:00')
  const dateLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
}

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
}

const onDateChange = () => {
  activePreset.value = null
  for (const preset of presets) {
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      break
    }
  }
}

const toggle = () => {
  isOpen.value = !isOpen.value
}

const apply = () => {
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value,
  })
  isOpen.value = false
}

const handleClickOutside = (event: MouseEvent) => {
  if (props.mode === 'datetimerange') return
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    isOpen.value = false
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
  }
}

watch(
  isOpen,
  (open) => {
    if (open) return
    localStartDate.value = props.startDate
    localEndDate.value = props.endDate
    onDateChange()
  },
  { flush: 'post' },
)

watch(
  () => props.startDate,
  (val) => {
    localStartDate.value = val
    onDateChange()
  },
)

watch(
  () => props.endDate,
  (val) => {
    localEndDate.value = val
    onDateChange()
  },
)

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
  onDateChange()
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.date-range-el {
  width: 100%;
}

.date-range-el :deep(.date-range-el__picker) {
  width: 100%;
}

.date-range-el :deep(.el-input__wrapper),
.date-range-el :deep(.el-range-editor.el-input__wrapper) {
  width: 100%;
  min-height: 32px;
  height: 32px;
  padding: 0 10px;
  border-radius: 8px;
  box-shadow: 0 0 0 1px var(--color-border, #e8e8e8) inset;
  background: var(--color-white, #fff);
}

.date-range-el :deep(.el-range-editor.el-input__wrapper:hover),
.date-range-el :deep(.el-range-editor.is-active) {
  box-shadow: 0 0 0 1px var(--color-primary, #0555ce) inset;
}

.date-range-el :deep(.el-range-input) {
  font-size: 13px;
  color: var(--color-text-primary, #262626);
}

.date-range-el :deep(.el-range-separator) {
  flex: 0;
  padding: 0 4px;
  font-size: 13px;
  color: var(--color-text-secondary, #868686);
}

.date-picker-trigger {
  @apply flex w-full items-center gap-2;
  @apply rounded-lg px-3 py-2 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-700 dark:text-gray-300;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.date-picker-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.date-picker-icon {
  @apply shrink-0 text-gray-400 dark:text-dark-400;
}

.date-picker-value {
  @apply flex-1 truncate text-left font-medium;
}

.date-picker-chevron {
  @apply ml-auto shrink-0 text-gray-400 dark:text-dark-400;
}

.date-picker-dropdown {
  @apply absolute left-0 z-[100] mt-2;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
  @apply min-w-[320px];
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply rounded-md px-3 py-1.5 text-xs font-medium;
  @apply text-gray-600 dark:text-gray-400;
  @apply hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100 dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-custom {
  @apply flex items-end gap-2 p-3;
}

.date-picker-field {
  @apply flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400;
}

.date-picker-input {
  @apply w-full rounded-md px-2 py-1.5 text-sm;
  @apply bg-gray-50 dark:bg-dark-700;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
}

.date-picker-input::-webkit-calendar-picker-indicator {
  @apply cursor-pointer opacity-60 hover:opacity-100;
  filter: invert(0.5);
}

.dark .date-picker-input::-webkit-calendar-picker-indicator {
  filter: none;
}

.date-picker-separator {
  @apply flex items-center justify-center pb-1;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply {
  @apply rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply transition-colors duration-150;
}

.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
