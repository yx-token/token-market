<script setup lang="ts">
/** API 文档里的单个端点：折叠头 + 请求参数 / 响应参数 / 调用示例三个页签。 */
import { computed, ref } from 'vue'
import ApiCodeBlock from '@/components/docs/ApiCodeBlock.vue'
import { EXAMPLE_LANGUAGES, buildExampleCode, buildMarkdownDocument, type ApiDocEndpoint, type ExampleLanguage } from '@/utils/apiDocs'
import { copyText } from '@/utils/clipboard'
import { useToast } from '@/stores/toast'

const props = defineProps<{
  endpoint: ApiDocEndpoint
  baseUrl: string
  model: string
  expanded: boolean
}>()
const emit = defineEmits<{ toggle: [] }>()

type Panel = 'request' | 'response' | 'examples'
const toast = useToast()
const panel = ref<Panel>('request')
const language = ref<ExampleLanguage>('curl')

const panels = computed(() => [
  { id: 'request' as const, label: '请求参数', count: props.endpoint.requestParams.length },
  { id: 'response' as const, label: '响应参数', count: props.endpoint.responseParams.length },
  { id: 'examples' as const, label: '调用示例', count: undefined },
])
const example = computed(() => buildExampleCode(props.endpoint, props.baseUrl, props.model, language.value))
const requestBody = computed(() => props.endpoint.request?.trim() ?? '')
const bodyId = computed(() => `endpoint-${props.endpoint.id}-body`)

/** 描述里的 `code` 片段：按反引号拆开，奇数段渲染成行内代码（不用 v-html）。 */
function inlineParts(text: string) {
  return text.split('`').map((t, i) => ({ t, code: i % 2 === 1 }))
}

async function copyMarkdown() {
  const ok = await copyText(buildMarkdownDocument(props.endpoint, props.baseUrl, props.model))
  if (ok) toast.success(`已复制「${props.endpoint.title}」的 Markdown 文档`)
  else toast.error('复制失败，请手动选中复制')
}
</script>

<template>
  <article :id="`endpoint-${endpoint.id}`" class="api-ep" :class="{ 'is-open': expanded }">
    <button
      type="button"
      class="api-ep__head"
      :aria-expanded="expanded"
      :aria-controls="bodyId"
      @click="emit('toggle')"
    >
      <span class="api-method" :class="`api-method--${endpoint.method.toLowerCase()}`">{{ endpoint.method }}</span>
      <code class="api-ep__path">{{ endpoint.path }}</code>
      <span class="api-ep__title">{{ endpoint.title }}</span>
      <svg class="api-ep__chev" viewBox="0 0 16 16" width="16" height="16" fill="none" aria-hidden="true">
        <path d="M4 6l4 4 4-4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>

    <div v-if="expanded" :id="bodyId" class="api-ep__body">
      <p class="api-ep__desc">
        <template v-for="(p, i) in inlineParts(endpoint.description)" :key="i">
          <code v-if="p.code" class="api-inline">{{ p.t }}</code><template v-else>{{ p.t }}</template>
        </template>
      </p>
      <div class="api-ep__meta">
        <span class="api-ep__auth">
          <svg viewBox="0 0 16 16" width="13" height="13" fill="none" aria-hidden="true">
            <rect x="3" y="7" width="10" height="7" rx="2" stroke="currentColor" stroke-width="1.5" />
            <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" stroke="currentColor" stroke-width="1.5" />
          </svg>
          {{ endpoint.auth }}
        </span>
        <button type="button" class="api-ep__md" @click="copyMarkdown">复制 Markdown</button>
      </div>

      <div class="api-tabs" role="tablist" :aria-label="`${endpoint.title} 文档`">
        <button
          v-for="p in panels"
          :key="p.id"
          type="button"
          role="tab"
          class="api-tab"
          :class="{ on: panel === p.id }"
          :aria-selected="panel === p.id"
          @click="panel = p.id"
        >
          {{ p.label }}<span v-if="p.count !== undefined" class="api-tab__count">{{ p.count }}</span>
        </button>
      </div>

      <div v-if="panel === 'request'" class="api-ep__panel" role="tabpanel">
        <table v-if="endpoint.requestParams.length" class="api-params">
          <thead>
            <tr><th>参数</th><th>类型</th><th>必填</th><th>默认值</th><th>说明</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in endpoint.requestParams" :key="p.name">
              <td><code>{{ p.name }}</code></td>
              <td><span class="api-type">{{ p.type }}</span></td>
              <td><span :class="p.required ? 'api-req' : 'api-opt'">{{ p.required ? '必填' : '可选' }}</span></td>
              <td><code class="api-default">{{ p.defaultValue }}</code></td>
              <td>{{ p.description }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="api-empty">此接口没有请求参数，只需在 Header 里带上 API Key。</p>
        <div v-if="requestBody" class="api-ep__sample">
          <div class="api-label">请求体示例</div>
          <ApiCodeBlock :code="requestBody" language="json" copied-text="请求体已复制" :aria-label="`${endpoint.title} 请求体示例`" />
        </div>
      </div>

      <div v-else-if="panel === 'response'" class="api-ep__panel" role="tabpanel">
        <table v-if="endpoint.responseParams.length" class="api-params">
          <thead>
            <tr><th>字段</th><th>类型</th><th>必有</th><th>默认值</th><th>说明</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in endpoint.responseParams" :key="p.name">
              <td><code>{{ p.name }}</code></td>
              <td><span class="api-type">{{ p.type }}</span></td>
              <td><span :class="p.required ? 'api-req' : 'api-opt'">{{ p.required ? '必有' : '可选' }}</span></td>
              <td><code class="api-default">{{ p.defaultValue }}</code></td>
              <td>{{ p.description }}</td>
            </tr>
          </tbody>
        </table>
        <div class="api-ep__sample">
          <div class="api-label">响应示例</div>
          <ApiCodeBlock :code="endpoint.response.trim()" language="json" copied-text="响应示例已复制" :aria-label="`${endpoint.title} 响应示例`" />
        </div>
      </div>

      <div v-else class="api-ep__panel" role="tabpanel">
        <div class="api-langs" role="tablist" aria-label="示例语言">
          <button
            v-for="l in EXAMPLE_LANGUAGES"
            :key="l.id"
            type="button"
            role="tab"
            class="api-lang"
            :class="{ on: language === l.id }"
            :aria-selected="language === l.id"
            @click="language = l.id"
          >{{ l.label }}</button>
        </div>
        <ApiCodeBlock :code="example" :language="language" copied-text="调用示例已复制" :aria-label="`${endpoint.title} 调用示例`" />
      </div>
    </div>
  </article>
</template>

<style scoped>
:where(.api-ep) code {
  border-radius: 0;
  background: transparent;
  padding: 0;
  font-size: inherit;
  color: inherit;
}
.api-ep {
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 16px;
  background: #fff;
  transition: border-color 0.15s, box-shadow 0.15s;
  scroll-margin-top: 76px;
}
.api-ep.is-open {
  border-color: rgba(0, 0, 0, 0.14);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.05);
}
.api-ep__head {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border: none;
  background: transparent;
  font: inherit;
  text-align: left;
  color: inherit;
  cursor: pointer;
  border-radius: 16px;
}
.api-ep__head:hover { background: #fffdf3; }
.api-ep__head:focus-visible {
  outline: 2px solid #1f1f1f;
  outline-offset: -2px;
}
.api-ep__path {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13.5px;
  font-weight: 600;
  color: #1f1f1f;
}
.api-ep__title {
  margin-left: auto;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
  color: #777;
}
.api-ep__chev {
  flex-shrink: 0;
  color: #999;
  transition: transform 0.2s;
}
.api-ep.is-open .api-ep__chev { transform: rotate(180deg); }
.api-ep__body {
  padding: 2px 18px 18px;
  border-top: 1px dashed rgba(0, 0, 0, 0.08);
}
.api-ep__desc {
  margin: 14px 0 10px;
  font-size: 14px;
  line-height: 1.7;
  color: #444;
}
.api-ep__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 14px;
}
.api-ep__auth {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 999px;
  background: #fff8cc;
  padding: 5px 11px;
  font-size: 12px;
  font-weight: 700;
  color: #6b5600;
}
.api-ep__md {
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 9px;
  background: #fff;
  padding: 6px 11px;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  color: #333;
  cursor: pointer;
}
.api-ep__md:hover { border-color: #1f1f1f; }
.api-ep__md:focus-visible,
.api-tab:focus-visible,
.api-lang:focus-visible {
  outline: 2px solid #1f1f1f;
  outline-offset: 2px;
}
.api-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  margin-bottom: 14px;
  overflow-x: auto;
}
.api-tab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: transparent;
  padding: 9px 12px 11px;
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  color: #888;
  cursor: pointer;
  white-space: nowrap;
}
.api-tab.on { color: #1f1f1f; }
.api-tab.on::after {
  content: '';
  position: absolute;
  left: 10px;
  right: 10px;
  bottom: -1px;
  height: 3px;
  border-radius: 3px 3px 0 0;
  background: #ffd400;
}
.api-tab__count {
  min-width: 18px;
  border-radius: 999px;
  background: #f1f1f1;
  padding: 1px 6px;
  font-size: 11px;
  color: #666;
  text-align: center;
}
.api-tab.on .api-tab__count { background: #fff3a8; color: #6b5600; }
.api-params {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.api-params th {
  padding: 8px 10px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  font-size: 12px;
  font-weight: 700;
  color: #888;
  text-align: left;
  white-space: nowrap;
}
.api-params td {
  padding: 10px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  color: #444;
  line-height: 1.55;
  vertical-align: top;
}
.api-params tr:last-child td { border-bottom: none; }
.api-params td:first-child code {
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12.5px;
  font-weight: 600;
  color: #1f1f1f;
  white-space: nowrap;
}
.api-type {
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: #8a6a00;
  white-space: nowrap;
}
.api-default {
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  color: #666;
}
.api-req,
.api-opt {
  display: inline-block;
  border-radius: 6px;
  padding: 1px 7px;
  font-size: 11px;
  font-weight: 800;
  white-space: nowrap;
}
.api-req { background: #1f1f1f; color: #ffd400; }
.api-opt { background: #f1f1f1; color: #777; }
.api-empty {
  margin: 4px 0 0;
  border-radius: 12px;
  background: #fafafa;
  padding: 14px;
  font-size: 13px;
  color: #777;
}
.api-ep__sample { margin-top: 16px; }
.api-label {
  margin-bottom: 8px;
  font-size: 12px;
  font-weight: 700;
  color: #888;
}
.api-langs {
  display: inline-flex;
  gap: 4px;
  margin-bottom: 10px;
  border-radius: 11px;
  background: #f3f3f3;
  padding: 3px;
}
.api-lang {
  border: none;
  border-radius: 8px;
  background: transparent;
  padding: 6px 12px;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  color: #777;
  cursor: pointer;
}
.api-lang.on {
  background: #fff;
  color: #1f1f1f;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);
}
.api-inline {
  border-radius: 5px;
  background: #fff3a8;
  padding: 1px 5px;
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.88em;
  color: #3a2f00;
}
@media (max-width: 720px) {
  .api-ep__head { flex-wrap: wrap; gap: 8px; }
  .api-ep__title { margin-left: 0; width: 100%; order: 3; }
  .api-ep__chev { margin-left: auto; }
  .api-ep__body { padding: 2px 12px 14px; }
  .api-params { display: block; overflow-x: auto; }
}
@media (prefers-reduced-motion: reduce) {
  .api-ep__chev,
  .api-ep { transition: none; }
}
</style>

<style>
/* 端点卡片与文档页共用的方法徽标 */
.api-method {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 48px;
  border-radius: 7px;
  padding: 3px 7px;
  font-family: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.02em;
}
.api-method--get { background: #e5f6ec; color: #1a7a45; }
.api-method--post { background: #1f1f1f; color: #ffd400; }
</style>
