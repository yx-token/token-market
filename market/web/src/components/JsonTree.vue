<script setup lang="ts">
// JSON 结构视图：对象 / 数组可折叠，按类型着色；值一律走文本插值，不会形成 HTML。
import { computed, ref } from 'vue'

defineOptions({ name: 'JsonTree' })

const props = withDefaults(
  defineProps<{
    value: unknown
    /** 当前节点的键名或下标，根节点为空 */
    name?: string | number
    depth?: number
    /** 默认展开的层数，更深的节点折叠 */
    openDepth?: number
    /** 是否为父容器里的最后一项，决定是否补逗号 */
    last?: boolean
  }>(),
  { name: undefined, depth: 0, openDepth: 2, last: true },
)

// 大数组只渲染前若干项，避免一次性生成过多节点
const LIMIT = 200
// 只含标量的短容器（如 ["黄", "白", "蓝"]）整行显示，不逐项换行
const INLINE_MAX = 72

const isArray = computed(() => Array.isArray(props.value))
const isContainer = computed(() => props.value !== null && typeof props.value === 'object')
const entries = computed<[string | number, unknown][]>(() => {
  if (!isContainer.value) return []
  return isArray.value ? (props.value as unknown[]).map((v, i) => [i, v]) : Object.entries(props.value as Record<string, unknown>)
})
const shown = computed(() => entries.value.slice(0, LIMIT))
const open = ref(props.depth < props.openDepth)
const brackets = computed(() => (isArray.value ? ['[', ']'] : ['{', '}']))
const inline = computed(() => {
  if (!isContainer.value || !entries.value.length || entries.value.some(([, v]) => v !== null && typeof v === 'object')) return ''
  const text = isArray.value
    ? `[${(props.value as unknown[]).map((v) => JSON.stringify(v)).join(', ')}]`
    : `{${entries.value.map(([k, v]) => `${JSON.stringify(k)}: ${JSON.stringify(v)}`).join(', ')}}`
  return text.length <= INLINE_MAX ? text : ''
})

const primitive = computed(() => {
  const v = props.value
  if (v === null) return { text: 'null', cls: 'text-violet-700' }
  switch (typeof v) {
    case 'string':
      return { text: JSON.stringify(v), cls: 'text-emerald-700' }
    case 'number':
      return { text: String(v), cls: 'text-sky-700' }
    case 'boolean':
      return { text: String(v), cls: 'text-violet-700' }
    default:
      return { text: String(v), cls: 'text-slate-600' }
  }
})
const label = computed(() => (props.name === undefined ? '' : typeof props.name === 'number' ? '' : JSON.stringify(props.name)))
</script>

<template>
  <div class="json-tree font-mono text-[11px] leading-5" :class="depth ? 'pl-4' : ''">
    <div v-if="inline" class="break-all pl-3">
      <span v-if="label" class="text-rose-700">{{ label }}: </span><span class="text-slate-700">{{ inline }}</span
      ><span class="text-slate-500">{{ last ? '' : ',' }}</span>
    </div>
    <template v-else-if="isContainer">
      <button
        type="button"
        class="inline-flex items-baseline gap-1 rounded text-left hover:bg-slate-100 focus-visible:outline focus-visible:outline-1 focus-visible:outline-sky-600"
        :aria-expanded="open"
        @click="open = !open"
      >
        <span aria-hidden="true" class="inline-block w-2 text-[8px] text-slate-400 transition-transform" :class="open ? 'rotate-90' : ''">▶</span>
        <span v-if="label" class="text-rose-700">{{ label }}:</span>
        <span class="text-slate-500">{{ brackets[0] }}</span>
        <span v-if="!open" class="text-slate-400">… {{ entries.length }} 项 {{ brackets[1] }}{{ last ? '' : ',' }}</span>
      </button>
      <template v-if="open">
        <JsonTree
          v-for="([k, v], i) in shown"
          :key="k"
          :name="k"
          :value="v"
          :depth="depth + 1"
          :open-depth="openDepth"
          :last="i === entries.length - 1"
        />
        <div v-if="entries.length > LIMIT" class="pl-4 text-slate-400">… 另有 {{ entries.length - LIMIT }} 项未显示</div>
        <div class="pl-3 text-slate-500">{{ brackets[1] }}{{ last ? '' : ',' }}</div>
      </template>
    </template>
    <div v-else class="break-all pl-3">
      <span v-if="label" class="text-rose-700">{{ label }}: </span><span :class="primitive.cls">{{ primitive.text }}</span
      ><span class="text-slate-500">{{ last ? '' : ',' }}</span>
    </div>
  </div>
</template>
