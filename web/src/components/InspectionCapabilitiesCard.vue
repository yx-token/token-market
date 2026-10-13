<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ModelCapability, ModelCapabilityOverride } from '@/api/types'
import SearchSelect from '@/components/common/SearchSelect.vue'
import { effectiveCapability } from './inspectionCapabilities'

const props = defineProps<{ defaults: ModelCapability[]; overrides: ModelCapabilityOverride[] }>()
const emit = defineEmits<{ 'update:overrides': [ModelCapabilityOverride[]] }>()
const selected = ref('')
const newPlatform = ref('openai')
const newModel = ref('')
const error = ref('')
const platforms = [
  { value: 'anthropic', text: 'Anthropic' }, { value: 'openai', text: 'OpenAI' }, { value: 'gemini', text: 'Gemini' },
  { value: 'antigravity', text: 'Antigravity' }, { value: 'grok', text: 'xAI Grok' }, { value: 'kimi', text: 'Kimi' },
  { value: 'zhipu', text: '智谱' }, { value: 'minimax', text: 'MiniMax' }, { value: 'opencode_go', text: 'OpenCode Go' }, { value: 'composite', text: '混合平台' },
]
const flags = [
  { key: 'tools', label: '工具调用' }, { key: 'json_schema', label: '结构化输出' }, { key: 'multiturn', label: '多轮保持' },
  { key: 'image', label: '图片' }, { key: 'pdf', label: 'PDF' }, { key: 'thinking', label: '推理' },
  { key: 'prompt_cache', label: '提示缓存' }, { key: 'max_output_unlimited', label: '输出无上限' },
] as const
type Flag = typeof flags[number]['key']
const entries = computed(() => {
  const all = new Map<string, { platform: string; model: string }>()
  for (const entry of [...props.defaults, ...props.overrides]) all.set(`${entry.platform}/${entry.model}`, entry)
  return [...all.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([value, entry]) => ({ value, ...entry }))
})
const options = computed(() => entries.value.map((entry) => ({
  value: entry.value,
  text: `${platforms.find((p) => p.value === entry.platform)?.text ?? entry.platform} · ${entry.model}${hasOverride(entry) ? ' · 已覆盖' : ''}`,
})))
watch(entries, (value) => {
  if (!value.some((entry) => entry.value === selected.value)) selected.value = value[0]?.value ?? ''
}, { immediate: true })
const current = computed(() => entries.value.find((entry) => entry.value === selected.value))
const effective = computed(() => current.value ? effectiveCapability(current.value.platform, current.value.model, props.defaults, props.overrides) : null)
const builtin = computed(() => props.defaults.find((entry) => entry.platform === current.value?.platform && entry.model === current.value?.model))

function hasOverride(entry: { platform: string; model: string }): boolean {
  return props.overrides.some((c) => c.platform === entry.platform && c.model === entry.model)
}
function edit(patch: Partial<ModelCapability>) {
  const entry = current.value
  if (!entry) return
  const previous = props.overrides.find((c) => c.platform === entry.platform && c.model === entry.model)
  const next = props.overrides.filter((c) => c.platform !== entry.platform || c.model !== entry.model)
  emit('update:overrides', [...next, { ...previous, ...patch, platform: entry.platform, model: entry.model }])
  error.value = ''
}
function editFlag(key: Flag, event: Event) {
  const value = (event.target as HTMLSelectElement).value
  edit({ [key]: value === 'unknown' ? null : value === 'true' })
}
function editNumber(key: 'context_window' | 'max_output', event: Event) {
  const value = Number((event.target as HTMLInputElement).value)
  if (!Number.isInteger(value) || value < 0 || value > 10000000) { error.value = '窗口与输出须为零到一千万的整数'; return }
  edit({ [key]: value })
}
function editAuth(event: Event) {
  try {
    const value = JSON.parse((event.target as HTMLTextAreaElement).value)
    if (value !== null && (typeof value !== 'object' || Array.isArray(value) || Object.values(value).some((n) => typeof n !== 'number' || !Number.isInteger(n) || n < 0 || n > 10000000))) throw new Error()
    edit({ context_window_by_auth: value })
  } catch { error.value = '认证窗口须为 JSON 对象，值为零到一千万的整数；null 表示未登记' }
}
function reset() {
  if (!current.value) return
  emit('update:overrides', props.overrides.filter((c) => c.platform !== current.value!.platform || c.model !== current.value!.model))
  error.value = ''
}
function add() {
  const model = newModel.value.trim()
  if (!model || /\s/.test(model) || (model.includes('*') && (!model.endsWith('*') || model.indexOf('*') !== model.length - 1))) {
    error.value = '请输入精确型号、末尾带 * 的前缀或平台默认 *'
    return
  }
  if (!entries.value.some((c) => c.platform === newPlatform.value && c.model === model)) {
    emit('update:overrides', [...props.overrides, { platform: newPlatform.value, model }])
  }
  selected.value = `${newPlatform.value}/${model}`
  newModel.value = ''
  error.value = ''
}
</script>

<template>
  <section class="card mt-5" aria-labelledby="capability-title">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 id="capability-title" class="card-title">模型官方能力</h2>
      <button class="btn-secondary" :disabled="!current || !hasOverride(current)" @click="reset">恢复默认</button>
    </div>
    <p class="hint mb-4">无基线时用于判定功能与上下文边界。仅登记有官方出处的能力；未登记不等于不支持。修改后点击上方“保存质检设置”生效。</p>
    <label class="label" for="capability-model">型号（内置 {{ defaults.length }} 项，覆盖 {{ overrides.length }} 项）</label>
    <SearchSelect id="capability-model" v-model="selected" :options="options" :allow-empty="false" placeholder="搜索平台或型号" />
    <template v-if="effective">
      <p class="hint my-3 break-words">内置来源：{{ builtin?.source || '未收录，需登记官方来源' }}</p>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div v-for="flag in flags" :key="flag.key">
          <label class="label" :for="`capability-${flag.key}`">{{ flag.label }}</label>
          <select :id="`capability-${flag.key}`" class="input" :value="effective[flag.key] === null ? 'unknown' : String(effective[flag.key])" @change="editFlag(flag.key, $event)">
            <option value="true">支持</option><option value="false">不支持</option><option value="unknown">未登记</option>
          </select>
        </div>
      </div>
      <div class="grid gap-4 mt-4 sm:grid-cols-2">
        <div><label class="label" for="capability-context_window">上下文窗口（token）</label><input id="capability-context_window" class="input" type="number" min="0" max="10000000" :value="effective.context_window" @change="editNumber('context_window', $event)" /></div>
        <div><label class="label" for="capability-max_output">最大输出（token）</label><input id="capability-max_output" class="input" type="number" min="0" max="10000000" :value="effective.max_output" @change="editNumber('max_output', $event)" /></div>
        <div><label class="label" for="capability-auth">按认证方式的上下文窗口（JSON）</label><textarea id="capability-auth" class="input font-mono text-xs" :value="JSON.stringify(effective.context_window_by_auth, null, 2)" @change="editAuth" /></div>
        <div><label class="label" for="capability-efforts">推理档位（逗号分隔）</label><input id="capability-efforts" class="input" :value="effective.efforts?.join(', ') ?? ''" @change="edit({ efforts: ($event.target as HTMLInputElement).value.split(/[,，\s]+/).filter(Boolean) })" /><p class="hint mt-2">窗口与最大输出填 0 表示未登记；没有有限输出上限时，请登记“输出无上限”。</p></div>
      </div>
      <label class="label mt-4" for="capability-source">当前官方来源</label>
      <input id="capability-source" class="input" :value="effective.source" @change="edit({ source: ($event.target as HTMLInputElement).value })" />
      <p class="hint mt-2">恢复默认会删除当前条目的管理员覆盖；平台默认或前缀规则仍会按匹配顺序生效。</p>
    </template>
    <p v-else class="hint my-4">尚无能力登记，可添加型号与官方来源。</p>
    <details class="mt-4">
      <summary class="cursor-pointer text-sm">登记其他型号或前缀</summary>
      <div class="grid gap-3 mt-3 sm:grid-cols-2">
        <div><label class="label" for="capability-new-platform">平台</label><SearchSelect id="capability-new-platform" v-model="newPlatform" :options="platforms" :allow-empty="false" /></div>
        <div><label class="label" for="capability-new-model">型号或前缀</label><input id="capability-new-model" v-model="newModel" class="input" placeholder="例如 gpt-6-*，平台默认填 *" /></div>
      </div>
      <button class="btn-secondary mt-3" @click="add">添加型号</button>
    </details>
    <p v-if="error" role="alert" class="callout mt-3">{{ error }}</p>
  </section>
</template>
