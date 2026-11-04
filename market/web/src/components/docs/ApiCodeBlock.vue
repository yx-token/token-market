<script setup lang="ts">
/** 文档 / 导入页共用的代码块：深色底 + 闲猫黄高亮，带复制按钮。 */
import { computed, onBeforeUnmount, ref } from 'vue'
import { codeLanguageLabel, tokenizeCode } from '@/utils/apiDocs'
import { copyText } from '@/utils/clipboard'
import { useToast } from '@/stores/toast'

const props = withDefaults(
  defineProps<{
    code: string
    language?: string
    /** 复制成功后的提示文案 */
    copiedText?: string
    /** 没有内容时显示的占位 */
    placeholder?: string
    ariaLabel?: string
    /** 预览里是打码内容时关掉复制，避免复制到假 Key */
    copyable?: boolean
  }>(),
  { language: 'shell', copiedText: '已复制', placeholder: '', ariaLabel: '代码示例', copyable: true },
)

const toast = useToast()
const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

const tokens = computed(() => (props.code ? tokenizeCode(props.code, props.language) : []))

async function copy() {
  if (!props.code) return
  if (await copyText(props.code)) {
    copied.value = true
    clearTimeout(timer)
    timer = setTimeout(() => (copied.value = false), 1600)
    toast.success(props.copiedText)
  } else {
    toast.error('复制失败，请手动选中复制')
  }
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="api-code">
    <div class="api-code__bar">
      <span class="api-code__lang">{{ codeLanguageLabel(language) }}</span>
      <slot name="bar" />
      <button v-if="code && copyable" type="button" class="api-code__copy" :aria-label="`复制${codeLanguageLabel(language)}`" @click="copy">
        <svg v-if="copied" viewBox="0 0 16 16" width="13" height="13" fill="none" aria-hidden="true">
          <path d="M3.5 8.5 6.5 11.5 12.5 4.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <svg v-else viewBox="0 0 16 16" width="13" height="13" fill="none" aria-hidden="true">
          <rect x="5" y="5" width="8.5" height="8.5" rx="2" stroke="currentColor" stroke-width="1.5" />
          <path d="M10.5 3.5V3a1.5 1.5 0 0 0-1.5-1.5H4A1.5 1.5 0 0 0 2.5 3v5A1.5 1.5 0 0 0 4 9.5h.5" stroke="currentColor" stroke-width="1.5" />
        </svg>
        {{ copied ? '已复制' : '复制' }}
      </button>
    </div>
    <pre class="api-code__pre" tabindex="0" :aria-label="ariaLabel"><code v-if="code"><template v-for="(t, i) in tokens" :key="i"><span v-if="t.cls" :class="t.cls">{{ t.text }}</span><template v-else>{{ t.text }}</template></template></code><code v-else class="api-code__placeholder">{{ placeholder }}</code></pre>
  </div>
</template>

<style scoped>
.api-code {
  overflow: hidden;
  border-radius: 14px;
  background: #1c1b18;
  border: 1px solid rgba(0, 0, 0, 0.6);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
}
.api-code__bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px 8px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.07);
}
.api-code__lang {
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: #9c968a;
  text-transform: lowercase;
}
.api-code__copy {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 4px 9px;
  background: rgba(255, 255, 255, 0.04);
  color: #e9e4d8;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.api-code__copy:hover {
  background: #ffd400;
  border-color: #ffd400;
  color: #1f1f1f;
}
.api-code__copy:focus-visible {
  outline: 2px solid #ffd400;
  outline-offset: 2px;
}
:where(.api-code__pre) code {
  border-radius: 0;
  background: transparent;
  padding: 0;
  font: inherit;
  color: inherit;
}
.api-code__pre {
  margin: 0;
  max-height: 460px;
  overflow: auto;
  padding: 14px 16px 16px;
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12.5px;
  line-height: 1.7;
  color: #ece7db;
  white-space: pre;
  tab-size: 4;
}
.api-code__pre:focus-visible {
  outline: 2px solid #ffd400;
  outline-offset: -2px;
}
.api-code__placeholder {
  color: #8f897c;
  white-space: pre-wrap;
}
.api-code :deep(.api-doc-code-token--string) { color: #ffd84d; }
.api-code :deep(.api-doc-code-token--number) { color: #ffab73; }
.api-code :deep(.api-doc-code-token--comment) { color: #8a8478; font-style: italic; }
.api-code :deep(.api-doc-code-token--flag) { color: #f5c97a; }
.api-code :deep(.api-doc-code-token--keyword) { color: #8fd6b0; font-weight: 600; }
.api-code :deep(.api-doc-code-token--property) { color: #a9c7ff; }
.api-code :deep(.api-doc-code-token--punctuation) { color: #a39d90; }
</style>
