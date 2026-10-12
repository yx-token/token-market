<script setup lang="ts">
// 可折叠的 Markdown 文本块：检测明细里的模型输出 / 标准答案 / 错误体。
// 渲染走 utils/markdown 的 renderMarkdown（html:false，原始 HTML 一律转义）；
// 「原文」视图用等宽 pre 展示，方便逐字比对。
import { computed, ref } from 'vue'
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
// 折叠判断与模板中的 line-clamp 行数对应；多行或长文本才出现展开按钮
const collapsible = computed(() => content.value.length > props.collapsedLines * 30 || (content.value.match(/\n/g)?.length ?? 0) >= props.collapsedLines)
const clampClass = computed(() => {
  const map: Record<number, string> = { 2: 'line-clamp-2', 3: 'line-clamp-3', 4: 'line-clamp-4', 6: 'line-clamp-6' }
  return map[props.collapsedLines] ?? 'line-clamp-4'
})
const clamped = computed(() => (collapsible.value && !expanded.value ? clampClass.value : ''))
const rendered = computed(() => (mode.value === 'rendered' && content.value ? renderMarkdown(content.value) : ''))
</script>

<template>
  <div v-if="content" class="min-w-0" data-testid="md-text">
    <div class="mb-0.5 flex items-center gap-2">
      <div class="seg" role="group" aria-label="显示方式">
        <button type="button" :class="{ on: mode === 'rendered' }" :aria-pressed="mode === 'rendered'" @click="mode = 'rendered'">渲染</button>
        <button type="button" :class="{ on: mode === 'raw' }" :aria-pressed="mode === 'raw'" @click="mode = 'raw'">原文</button>
      </div>
      <span v-if="truncated" class="text-[11px] text-amber-700" title="后端只保存前 500 个字符，Markdown 可能不完整">已截断</span>
    </div>

    <div v-if="mode === 'rendered'" class="markdown-body max-w-full overflow-x-auto overflow-y-auto text-xs" :class="[maxHeightClass, clamped]" v-html="rendered" />
    <pre
      v-else
      class="max-w-full overflow-x-auto whitespace-pre-wrap break-words rounded bg-slate-50 p-2 font-mono text-[11px] leading-relaxed text-slate-700"
      :class="[maxHeightClass, clamped]"
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
