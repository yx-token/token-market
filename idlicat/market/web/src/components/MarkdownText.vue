<script setup lang="ts">
// 可折叠的 Markdown 文本块：检测明细里的模型输出 / 标准答案 / 错误体。
// 渲染走 utils/markdown 的 renderMarkdown（html:false，原始 HTML 一律转义）；
// 「原文」视图用等宽 pre 展示，方便逐字比对。整段是 JSON 时改为可折叠的结构视图；
// 形似 JSON 却解析失败（多为截断）时按原文等宽显示，避免被 Markdown 打乱。
import { computed, ref } from 'vue'
import JsonTree from '@/components/JsonTree.vue'
import { detectJson } from '@/utils/jsonDetect'
import { renderMarkdown } from '@/utils/markdown'

const props = withDefaults(
  defineProps<{
    text?: string | null
    /** 内容是否被后端截断（如回答片段只保留前 500 字符），截断的 Markdown 可能不完整 */
    truncated?: boolean
    /** 折叠时的最大行数（按约 30 字符一行估算是否需要展开按钮） */
    collapsedLines?: number
    /** 渲染视图的最大高度，超出内部滚动，避免撑破表格与弹窗 */
    maxHeightClass?: string
  }>(),
  { text: '', truncated: false, collapsedLines: 4, maxHeightClass: 'max-h-64' },
)

const expanded = ref(false)
const mode = ref<'rendered' | 'raw'>('rendered')

const content = computed(() => (props.text ?? '').trim())
const json = computed(() => detectJson(content.value))
const isJson = computed(() => json.value?.kind === 'json')
// 折叠判断与模板中的 line-clamp 行数对应；多行或长文本才出现展开按钮。JSON 结构视图自带折叠，不再按行截断
const collapsible = computed(
  () =>
    !(isJson.value && mode.value === 'rendered') &&
    (content.value.length > props.collapsedLines * 30 || (content.value.match(/\n/g)?.length ?? 0) >= props.collapsedLines),
)
const clampClass = computed(() => {
  const map: Record<number, string> = { 2: 'line-clamp-2', 3: 'line-clamp-3', 4: 'line-clamp-4', 6: 'line-clamp-6' }
  return map[props.collapsedLines] ?? 'line-clamp-4'
})
const clamped = computed(() => (collapsible.value && !expanded.value ? clampClass.value : ''))
const rendered = computed(() => (mode.value === 'rendered' && content.value && !json.value ? renderMarkdown(content.value) : ''))
const jsonValue = computed(() => (json.value?.kind === 'json' ? json.value.value : null))
</script>

<template>
  <div v-if="content" class="min-w-0" data-testid="md-text">
    <div class="mb-0.5 flex items-center gap-2">
      <div class="seg" role="group" aria-label="显示方式">
        <button type="button" :class="{ on: mode === 'rendered' }" :aria-pressed="mode === 'rendered'" @click="mode = 'rendered'">{{ isJson ? '结构' : '渲染' }}</button>
        <button type="button" :class="{ on: mode === 'raw' }" :aria-pressed="mode === 'raw'" @click="mode = 'raw'">原文</button>
      </div>
      <span v-if="json" class="rounded bg-slate-100 px-1 font-mono text-[10px] text-slate-500">JSON</span>
      <span v-if="truncated" class="text-[11px] text-amber-700" title="后端只保存前 500 个字符，Markdown 可能不完整">已截断</span>
    </div>

    <div
      v-if="mode === 'rendered' && jsonValue"
      class="max-w-full overflow-y-auto overflow-x-hidden rounded bg-slate-50 p-2"
      :class="maxHeightClass"
      data-testid="json-view"
    >
      <JsonTree :value="jsonValue" />
    </div>
    <template v-else-if="mode === 'rendered' && json?.kind === 'broken'">
      <pre
        class="max-w-full whitespace-pre-wrap break-words rounded bg-slate-50 p-2 font-mono text-[11px] leading-relaxed text-slate-700"
        :class="[maxHeightClass, clamped || 'overflow-y-auto overflow-x-hidden']"
        >{{ content }}</pre
      >
      <p class="mt-0.5 text-[11px] text-slate-500">JSON 不完整，无法解析（可能被截断），按原文显示。</p>
    </template>
    <div v-else-if="mode === 'rendered'" class="markdown-body markdown-wrap max-w-full text-xs" :class="[maxHeightClass, clamped || 'overflow-y-auto overflow-x-hidden']" v-html="rendered" />
    <pre
      v-else
      class="max-w-full whitespace-pre-wrap break-words rounded bg-slate-50 p-2 font-mono text-[11px] leading-relaxed text-slate-700"
      :class="[maxHeightClass, clamped || 'overflow-y-auto overflow-x-hidden']"
      >{{ content }}</pre
    >

    <button
      v-if="collapsible"
      type="button"
      class="mt-0.5 inline-flex items-center gap-0.5 text-xs text-sky-700 hover:underline"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <span aria-hidden="true" class="inline-block text-[9px] transition-transform" :class="expanded ? 'rotate-90' : ''">▶</span>
      {{ expanded ? '收起' : '展开' }}
    </button>
  </div>
  <span v-else class="text-slate-400">—</span>
</template>

<style scoped>
/* 检测明细空间有限：代码块与表格折行显示，不出现横向滚动条（全局 .markdown-body 仍保留横向滚动） */
.markdown-wrap :deep(pre) {
  overflow-x: hidden;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.markdown-wrap :deep(table) {
  display: table;
  width: 100%;
  table-layout: fixed;
}
.markdown-wrap :deep(:is(p, li, td, th, code)) {
  overflow-wrap: anywhere;
}
</style>
