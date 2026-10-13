<script setup lang="ts">
/**
 * 可搜索下拉选择：单选（string）或多选（string[]），用于表单里引用其它实体的字段。
 * - 选项里没有的已存值不会被丢弃；普通字段标注“（未注册）”，开放字段标注“（自定义）”
 * - creatable 显式开启自由录入，适用于模型 ID 等开放集合；默认仍限制在选项内
 * - options 为空、非远程且不在 loading 时（选项接口失败的降级路径），单选/多选都允许回车录入输入文本，不卡住表单保存；
 *   选项正常加载后拒绝录入不存在的值，避免拼错写入脏数据
 * - 远程搜索默认拒绝随意录入，避免拼错 ID 写入脏数据
 * - 远程搜索：选中后缓存其 text，关闭/重开面板都不会误判为“未注册”
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { usePopoverPosition } from '@/composables/usePopoverPosition'
import ConsoleIcon from '@/components/icons/ConsoleIcon.vue'
import type { SelectOption } from './searchSelect'

export type { SelectOption }

const props = withDefaults(
  defineProps<{
    id?: string
    /** 单选用 string，多选用 string[] */
    modelValue?: string | string[]
    options: SelectOption[]
    multiple?: boolean
    /** 允许空值；单选时显示 placeholder，多选为空数组 */
    allowEmpty?: boolean
    placeholder?: string
    disabled?: boolean
    /** 选项接口尚未就绪 */
    loading?: boolean
    /** 显式允许输入自定义值；默认只保留空选项时的降级录入规则 */
    creatable?: boolean
    /** 面板宽度，默认跟随触发器、至少 240px，并限制在视口内 */
    panelWidth?: number
    /** 选中值的展示文案，未命中 options 时回退为原值 */
    textOf?: (value: string) => string
    /** 远程搜索：传入后按关键字拉取选项，本地 options 作为初始与降级底数 */
    remote?: (q: string) => Promise<SelectOption[]>
    /** 远程搜索防抖毫秒，默认 250 */
    remoteDebounce?: number
  }>(),
  { modelValue: '', multiple: false, allowEmpty: true, placeholder: '搜索选择', disabled: false, loading: false, creatable: false, remoteDebounce: 250 },
)

const emit = defineEmits<{
  'update:modelValue': [string | string[]]
  /** 远程搜索状态变化，供外部展示 loading */
  'remote:state': [{ loading: boolean; error: string | null }]
}>()

const isOpen = ref(false)
const query = ref('')
const active = ref(0)
const triggerRef = ref<HTMLElement | null>(null)
const listRef = ref<HTMLElement | null>(null)
const searchRef = ref<HTMLInputElement | null>(null)
const { style: panelStyle, update: updatePanel, open: placePanel, close: unplacePanel } = usePopoverPosition(triggerRef, {
  width: (rect) => props.panelWidth ?? Math.max(rect.width, 240),
  maxHeight: 320,
})

// 多选标签换行或外部修改宽度后，重新对齐仍打开的面板。
watch(() => [props.modelValue, props.panelWidth], async () => {
  await nextTick()
  if (isOpen.value) updatePanel()
}, { deep: true })

// 远程搜索结果与状态；remote 未提供时退回纯本地过滤
const remoteResults = ref<SelectOption[]>([])
const remoteLoading = ref(false)
const remoteError = ref<string | null>(null)
let remoteTimer: ReturnType<typeof setTimeout> | null = null
let remoteSeq = 0

// 已选值的展示缓存：远程搜索选中后把对应 text 记下，避免面板关闭 / 重开时因 options 为空被误判为“未注册”。
// 远程结果或本地 options 出现同名值时同步刷新其 text，保证文案最新。
const selectedCache = ref<Map<string, string>>(new Map())

const selected = computed<string[]>(() => {
  const v = props.modelValue
  if (Array.isArray(v)) return v.filter((x) => x !== '')
  return v ? [v] : []
})

/** 合并选项池：本地 options + 远程结果 + 已选缓存（用于判定“已注册”与展示文案） */
const pool = computed<SelectOption[]>(() => {
  const seen = new Map<string, SelectOption>()
  for (const o of props.options) seen.set(o.value, o)
  for (const o of remoteResults.value) seen.set(o.value, o)
  for (const [value, text] of selectedCache.value) {
    if (!seen.has(value)) seen.set(value, { value, text })
  }
  return [...seen.values()]
})

const known = computed(() => new Set(pool.value.map((o) => o.value)))

/** 远程搜索无法穷举合法值；本地未命中的已存值仍保留在选项中。 */
const unlistedValues = computed(() => {
  if (props.remote) return []
  return selected.value.filter((v) => !known.value.has(v))
})
const staleValues = computed(() => props.creatable ? [] : unlistedValues.value)

const merged = computed<SelectOption[]>(() => {
  const extra: SelectOption[] = unlistedValues.value.map((value) => ({
    value, text: `${value}（${props.creatable ? '自定义' : '未注册'}）`, stale: !props.creatable,
  }))
  // 远程搜索有结果时以远程为准；否则用本地 options，保证降级可用
  const base = props.remote && (query.value.trim() || remoteResults.value.length) ? remoteResults.value : props.options
  return [...base, ...extra]
})

/** 开放字段显式允许录入；其余字段保持“非远程、空选项且不在加载”的降级规则。 */
const canFreeText = computed(() =>
  !props.loading && !remoteLoading.value && (props.creatable || (!props.remote && props.options.length === 0)),
)

const filtered = computed(() => {
  const value = query.value.trim()
  const q = value.toLowerCase()
  if (!q) return merged.value
  const list = merged.value.filter((o) => `${o.value} ${o.text} ${o.keywords ?? ''}`.toLowerCase().includes(q))
  if (!props.creatable || !canFreeText.value) return list
  // 完整输入优先，避免输入新模型 ID 时误选同前缀的已有模型；已有值不重复添加。
  const exact = list.find((o) => o.value === value)
  return exact ? [exact, ...list.filter((o) => o !== exact)] : [{ value, text: `${value}（自定义）` }, ...list]
})

async function runRemote(q: string) {
  if (!props.remote) return
  remoteLoading.value = true
  remoteError.value = null
  emit('remote:state', { loading: true, error: null })
  const seq = ++remoteSeq
  try {
    const res = await props.remote(q)
    // 过期请求的结果丢弃，避免旧关键字覆盖新输入
    if (seq === remoteSeq) {
      remoteResults.value = res ?? []
      // 远程结果出现同名已选值时刷新其 text，确保展示文案最新
      for (const o of remoteResults.value) {
        if (selectedCache.value.has(o.value)) selectedCache.value.set(o.value, o.text)
      }
    }
  } catch (e) {
    if (seq === remoteSeq) {
      remoteError.value = typeof e === 'string' ? e : (e as Error)?.message ?? '搜索失败'
      remoteResults.value = []
    }
  } finally {
    if (seq === remoteSeq) {
      remoteLoading.value = false
      emit('remote:state', { loading: false, error: remoteError.value })
    }
  }
}

function scheduleRemote(q: string) {
  if (!props.remote) return
  if (remoteTimer) clearTimeout(remoteTimer)
  remoteTimer = setTimeout(() => {
    remoteTimer = null
    void runRemote(q)
  }, props.remoteDebounce)
}

watch(query, (q) => scheduleRemote(q))

// 预填 / 外部更新 modelValue 时，从 options 与远程结果里补全已选值的展示文案，避免预填值被误判为“未注册”
watch(
  () => [props.modelValue, props.options, remoteResults.value] as const,
  () => {
    for (const v of selected.value) {
      if (selectedCache.value.has(v)) continue
      const hit = props.options.find((o) => o.value === v) ?? remoteResults.value.find((o) => o.value === v)
      if (hit) selectedCache.value.set(v, hit.text)
    }
  },
  { immediate: true, deep: true },
)

const displayText = computed(() => {
  if (props.multiple) {
    if (!selected.value.length) return props.placeholder
    return selected.value.map((v) => displayOne(v)).join('、')
  }
  return selected.value.length ? displayOne(selected.value[0]) : props.placeholder
})

function displayOne(value: string) {
  if (props.textOf) return props.textOf(value)
  const hit = pool.value.find((o) => o.value === value)
  if (hit) return hit.text
  // 开放集合的未知值是自定义值；远程场景只回显原值。
  return props.remote ? value : `${value}（${props.creatable ? '自定义' : '未注册'}）`
}

function toggle(value: string, option?: SelectOption) {
  if (!props.multiple) {
    // 单选选中后缓存其展示文案，远程结果清空时仍能正常显示
    selectedCache.value.set(value, option?.text ?? value)
    emit('update:modelValue', value)
    closePanel()
    return
  }
  const next = new Set(selected.value)
  if (next.has(value)) {
    next.delete(value)
    selectedCache.value.delete(value)
  } else {
    next.add(value)
    selectedCache.value.set(value, option?.text ?? value)
  }
  emit('update:modelValue', [...next])
  query.value = ''
  active.value = 0
  void nextTick(scrollActive)
}

function remove(value: string, e: Event) {
  e.stopPropagation()
  e.preventDefault()
  selectedCache.value.delete(value)
  emit('update:modelValue', selected.value.filter((v) => v !== value))
}

async function openPanel() {
  if (props.disabled) return
  if (isOpen.value) {
    closePanel()
    return
  }
  isOpen.value = true
  query.value = ''
  active.value = 0
  // 远程搜索：打开时拉一次空查询列表，给用户初始可选项；不清空 selectedCache，已选值展示不丢
  if (props.remote) {
    void runRemote('')
  }
  await placePanel()
  await nextTick(() => searchRef.value?.focus())
}

function closePanel() {
  isOpen.value = false
  unplacePanel()
  // Escape 关闭后焦点回到触发器，键盘用户可继续操作
  void nextTick(() => triggerRef.value?.focus())
}

function scrollActive() {
  const el = listRef.value?.querySelector('[data-active="true"]')
  // jsdom 没有 scrollIntoView，浏览器里平滑滚到高亮项
  ;(el as HTMLElement | null)?.scrollIntoView?.({ block: 'nearest' })
}

function onKeydown(e: KeyboardEvent) {
  if (!isOpen.value) {
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
      e.preventDefault()
      void openPanel()
    }
    return
  }
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    closePanel()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    active.value = Math.min(active.value + 1, filtered.value.length - 1)
    void nextTick(scrollActive)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    active.value = Math.max(active.value - 1, 0)
    void nextTick(scrollActive)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const hit = filtered.value[active.value]
    if (hit) {
      toggle(hit.value, hit)
    } else if (canFreeText.value && query.value.trim()) {
      // 空选项接口失败时降级录入；开放字段由上方的自定义选项处理。
      toggle(query.value.trim())
    }
  } else if (e.key === 'Backspace' && props.multiple && !query.value && selected.value.length) {
    const last = selected.value[selected.value.length - 1]
    selectedCache.value.delete(last)
    emit('update:modelValue', selected.value.slice(0, -1))
  }
}

function onOutside(e: MouseEvent) {
  if (!isOpen.value) return
  const t = e.target as Node
  if (triggerRef.value?.contains(t) || listRef.value?.contains(t)) return
  closePanel()
}

onMounted(() => {
  document.addEventListener('mousedown', onOutside)
})
onUnmounted(() => {
  document.removeEventListener('mousedown', onOutside)
  if (remoteTimer) clearTimeout(remoteTimer)
  unplacePanel()
})

watch(filtered, () => {
  active.value = 0
})

const busy2 = computed(() => props.loading || remoteLoading.value)

const emptyHint = computed(() => {
  if (busy2.value) return remoteLoading.value ? '正在搜索…' : '正在加载选项…'
  const q = query.value.trim()
  if (q) {
    // 选项接口失败降级（非远程且 options 为空）时提示可回车录入；其余提示无匹配
    return canFreeText.value ? `没有匹配「${q}」，回车可直接录入` : `没有匹配「${q}」`
  }
  return props.remote ? '输入关键字搜索…' : '暂无可选项'
})
</script>

<template>
  <div class="relative w-full" @keydown="onKeydown">
    <div
      :id="id"
      ref="triggerRef"
      class="input ss-trigger"
      :class="{ 'is-open': isOpen, 'is-disabled': disabled, 'is-placeholder': !selected.length }"
      role="combobox"
      tabindex="0"
      :aria-expanded="isOpen"
      aria-haspopup="listbox"
      :aria-controls="id ? `${id}-listbox` : undefined"
      :aria-disabled="disabled || undefined"
      @click="openPanel"
    >
      <span v-if="!multiple || !selected.length" class="ss-value">{{ displayText }}</span>
      <span class="ss-tags" v-if="multiple && selected.length">
        <span v-for="v in selected" :key="v" class="ss-tag" :class="{ 'is-stale': staleValues.includes(v) }">
          <span class="ss-tag-text">{{ displayOne(v) }}</span>
          <button type="button" class="ss-tag-x" tabindex="-1" :aria-label="`移除 ${displayOne(v)}`" @click="remove(v, $event)">✕</button>
        </span>
      </span>
      <span class="ss-chevron" :class="{ 'is-open': isOpen }" aria-hidden="true"><ConsoleIcon name="chevron-down" size="sm" /></span>
    </div>

    <Teleport to="body">
      <Transition name="ss-pop">
        <div
          v-if="isOpen"
          ref="listRef"
          class="ss-panel"
          :style="panelStyle"
          role="listbox"
          :id="id ? `${id}-listbox` : undefined"
          :aria-multiselectable="multiple || undefined"
          :aria-label="placeholder"
        >
          <input
            ref="searchRef"
            v-model="query"
            class="input ss-search"
            type="text"
            autocomplete="off"
            :placeholder="multiple && selected.length ? '继续搜索添加…' : '输入关键字筛选…'"
            :aria-activedescendant="id ? `${id}-opt-${active}` : undefined"
            @keydown="onKeydown"
          />
          <ul v-if="filtered.length" class="ss-list">
            <li
              v-for="(o, i) in filtered"
              :id="id ? `${id}-opt-${i}` : undefined"
              :key="o.value"
              class="ss-opt"
              :class="{ 'is-active': i === active, 'is-on': selected.includes(o.value), 'is-stale': o.stale }"
              :data-active="i === active"
              role="option"
              :aria-selected="selected.includes(o.value)"
              @mouseenter="active = i"
              @click="toggle(o.value, o)"
            >
              <span v-if="multiple" class="ss-box" aria-hidden="true">{{ selected.includes(o.value) ? '✓' : '' }}</span>
              <span class="ss-opt-text">{{ o.text }}</span>
            </li>
          </ul>
          <p v-else class="ss-empty">{{ emptyHint }}</p>
          <p v-if="remoteError" class="ss-note ss-note--error">搜索失败：{{ remoteError }}（可检查关键字后重试）</p>
          <p v-if="staleValues.length" class="ss-note">{{ staleValues.length }} 个已存值不在当前选项中，已按原样保留</p>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
/* 触发器直接复用 .input，跟随所在控制台 / 市场 / 账本主题 */
.input.ss-trigger {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  cursor: pointer;
  text-align: left;
}
.ss-trigger:focus-visible {
  outline: 2px solid var(--accent, #3b6fb0);
  outline-offset: 1px;
}
.ss-trigger.is-disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.ss-value {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ss-chevron {
  display: inline-flex;
  flex: none;
  line-height: 0;
  opacity: 0.6;
  transition: transform 0.15s ease;
}
.ss-chevron.is-open {
  transform: rotate(180deg);
}
.ss-tags {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.ss-tag {
  display: inline-flex;
  max-width: 100%;
  min-width: 0;
  align-items: center;
  gap: 0.2rem;
  border-radius: 3px;
  background: #efeae0;
  padding: 1px 0.25rem 1px 0.4rem;
  font-size: 0.75rem;
}
.ss-tag-text {
  min-width: 0;
  overflow-wrap: anywhere;
}
.ss-tag.is-stale {
  background: #fdf3e7;
  color: #b45309;
}
.ss-tag-x {
  flex: none;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font-size: 0.7rem;
  padding: 0 2px;
}
.ss-panel {
  z-index: 3000;
  border: 1px solid #e2dbcd;
  border-radius: 4px;
  background: #fff;
  box-shadow: 0 10px 28px rgba(23, 20, 15, 0.14);
  overflow: hidden;
}
.input.ss-search {
  border: 0;
  border-bottom: 1px solid #e2dbcd;
  border-radius: 0;
  padding: 0.5rem 0.75rem;
  box-shadow: none !important;
}
.ss-list {
  max-height: 260px;
  margin: 0;
  padding: 0;
  list-style: none;
  overflow-y: auto;
}
.ss-opt {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.35rem 0.75rem;
  font-size: 0.875rem;
  cursor: pointer;
}
.ss-opt.is-active {
  background: #f7f4ee;
}
.ss-opt.is-on {
  font-weight: 600;
}
.ss-opt.is-stale .ss-opt-text {
  color: #b45309;
}
.ss-box {
  flex: none;
  width: 0.9rem;
  font-size: 0.75rem;
}
.ss-opt-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ss-empty,
.ss-note {
  margin: 0;
  padding: 0.5rem 0.75rem;
  font-size: 0.75rem;
  color: #7a7366;
}
.ss-note {
  border-top: 1px solid #e2dbcd;
  color: #b7801a;
}
.ss-note--error {
  color: #b91c1c;
}
.ss-pop-enter-active,
.ss-pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.ss-pop-enter-from,
.ss-pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
