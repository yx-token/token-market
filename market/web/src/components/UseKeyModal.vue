<script setup lang="ts">
/**
 * 买家「使用密钥」：对齐引擎 UseKeyModal 的常用路径（Claude Code / Codex / curl），
 * 给出可复制的环境变量与 settings.json，而不是只贴一段 curl。
 */
import { computed, ref, watch } from 'vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { copyText } from '@/utils/clipboard'
import { normalizeCcSwitchPlatform } from '@/utils/ccswitchImport'
import { sampleRequest } from '@/utils/keySample'
import { useToast } from '@/stores/toast'

const props = defineProps<{
  open: boolean
  apiKey: string
  baseUrl: string
  platform?: string | null
  keyName?: string
}>()

const emit = defineEmits<{ 'update:open': [boolean] }>()

const toast = useToast()
type Client = 'claude' | 'codex' | 'gemini' | 'curl'
type Shell = 'unix' | 'powershell' | 'cmd'

const client = ref<Client>('claude')
const shell = ref<Shell>('unix')

const plat = computed(() => normalizeCcSwitchPlatform(props.platform))
const rootBase = computed(() => props.baseUrl.replace(/\/v1\/?$/, '').replace(/\/+$/, ''))

const clients = computed(() => {
  const list: { id: Client; label: string }[] = [
    { id: 'claude', label: 'Claude Code' },
    { id: 'codex', label: 'Codex / OpenAI' },
  ]
  if (plat.value === 'gemini' || plat.value === 'antigravity') {
    list.push({ id: 'gemini', label: 'Gemini CLI' })
  }
  list.push({ id: 'curl', label: 'curl 探测' })
  return list
})

watch(
  () => [props.open, plat.value] as const,
  ([open]) => {
    if (!open) return
    if (plat.value === 'openai') client.value = 'codex'
    else if (plat.value === 'gemini') client.value = 'gemini'
    else client.value = 'claude'
    shell.value = 'unix'
  },
)

const claudeEnv = computed(() => {
  const base = rootBase.value
  const key = props.apiKey
  if (shell.value === 'powershell') {
    return `$env:ANTHROPIC_BASE_URL="${base}"
$env:ANTHROPIC_AUTH_TOKEN="${key}"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
  }
  if (shell.value === 'cmd') {
    return `set ANTHROPIC_BASE_URL=${base}
set ANTHROPIC_AUTH_TOKEN=${key}
set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
  }
  return `export ANTHROPIC_BASE_URL="${base}"
export ANTHROPIC_AUTH_TOKEN="${key}"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`
})

const claudeSettingsPath = computed(() =>
  shell.value === 'unix' ? '~/.claude/settings.json' : '%USERPROFILE%\\.claude\\settings.json',
)

const claudeSettings = computed(
  () => `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "env": {
    "ANTHROPIC_BASE_URL": "${rootBase.value}",
    "ANTHROPIC_AUTH_TOKEN": "${props.apiKey}",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}`,
)

const codexEnv = computed(() => {
  const base = `${rootBase.value}/v1`
  const key = props.apiKey
  if (shell.value === 'powershell') {
    return `$env:OPENAI_BASE_URL="${base}"
$env:OPENAI_API_KEY="${key}"`
  }
  if (shell.value === 'cmd') {
    return `set OPENAI_BASE_URL=${base}
set OPENAI_API_KEY=${key}`
  }
  return `export OPENAI_BASE_URL="${base}"
export OPENAI_API_KEY="${key}"`
})

const geminiEnv = computed(() => {
  const base = rootBase.value
  const key = props.apiKey
  if (shell.value === 'powershell') {
    return `$env:GOOGLE_GEMINI_BASE_URL="${base}"
$env:GEMINI_API_KEY="${key}"
$env:GEMINI_MODEL="gemini-2.0-flash"`
  }
  if (shell.value === 'cmd') {
    return `set GOOGLE_GEMINI_BASE_URL=${base}
set GEMINI_API_KEY=${key}
set GEMINI_MODEL=gemini-2.0-flash`
  }
  return `export GOOGLE_GEMINI_BASE_URL="${base}"
export GEMINI_API_KEY="${key}"
export GEMINI_MODEL="gemini-2.0-flash"`
})

const curlSample = computed(() =>
  sampleRequest(plat.value === 'openai' ? 'openai' : plat.value === 'gemini' ? 'gemini' : 'anthropic', rootBase.value, props.apiKey),
)

const lead = computed(() => {
  if (client.value === 'claude') return '把 Base URL 与 Auth Token 指到闲猫，Claude Code 即可走 Anthropic 协议调用。'
  if (client.value === 'codex') return 'Codex CLI / OpenAI SDK：Base URL 需带 /v1，API Key 换成闲猫 Key。'
  if (client.value === 'gemini') return 'Gemini CLI：把官方 Base URL 换成闲猫地址，API Key 换成闲猫 Key。'
  return '用 curl 先探测通不通；通过后再接到客户端。'
})

async function copy(text: string) {
  if (await copyText(text)) toast.success('已复制')
  else toast.error('复制失败，请手动选择后按 Ctrl/⌘+C')
}

function close() {
  emit('update:open', false)
}
</script>

<template>
  <ModalDialog :open="open" title="使用密钥" size="wide" @update:open="(v: boolean) => !v && close()">
    <div class="ukm">
      <p class="ukm-lead">
        <template v-if="keyName">Key「{{ keyName }}」· </template>{{ lead }}
      </p>

      <div class="ukm-tabs" role="tablist" aria-label="客户端">
        <button
          v-for="c in clients"
          :key="c.id"
          type="button"
          role="tab"
          class="ukm-tab"
          :class="{ on: client === c.id }"
          :aria-selected="client === c.id"
          @click="client = c.id"
        >{{ c.label }}</button>
      </div>

      <div v-if="client !== 'curl'" class="ukm-shells" role="tablist" aria-label="终端">
        <button type="button" class="ukm-shell" :class="{ on: shell === 'unix' }" @click="shell = 'unix'">Terminal</button>
        <button type="button" class="ukm-shell" :class="{ on: shell === 'powershell' }" @click="shell = 'powershell'">PowerShell</button>
        <button type="button" class="ukm-shell" :class="{ on: shell === 'cmd' }" @click="shell = 'cmd'">CMD</button>
      </div>

      <template v-if="client === 'claude'">
        <div class="ukm-block">
          <div class="ukm-head">
            <strong>环境变量（当前会话）</strong>
            <button type="button" class="ukm-copy" @click="copy(claudeEnv)">复制</button>
          </div>
          <pre>{{ claudeEnv }}</pre>
        </div>
        <div class="ukm-block">
          <div class="ukm-head">
            <strong>{{ claudeSettingsPath }}</strong>
            <button type="button" class="ukm-copy" @click="copy(claudeSettings)">复制</button>
          </div>
          <pre>{{ claudeSettings }}</pre>
          <p class="ukm-note">持久化推荐写 settings.json；不要把含 Key 的文件提交到仓库。</p>
        </div>
      </template>

      <template v-else-if="client === 'codex'">
        <div class="ukm-block">
          <div class="ukm-head">
            <strong>环境变量</strong>
            <button type="button" class="ukm-copy" @click="copy(codexEnv)">复制</button>
          </div>
          <pre>{{ codexEnv }}</pre>
          <p class="ukm-note">OPENAI_BASE_URL 必须是 <code>{{ rootBase }}/v1</code>。</p>
        </div>
      </template>

      <template v-else-if="client === 'gemini'">
        <div class="ukm-block">
          <div class="ukm-head">
            <strong>环境变量</strong>
            <button type="button" class="ukm-copy" @click="copy(geminiEnv)">复制</button>
          </div>
          <pre>{{ geminiEnv }}</pre>
        </div>
      </template>

      <template v-else>
        <div class="ukm-block">
          <div class="ukm-head">
            <strong>curl · {{ curlSample.label }}</strong>
            <button type="button" class="ukm-copy" @click="copy(curlSample.code)">复制</button>
          </div>
          <pre>{{ curlSample.code }}</pre>
          <p class="ukm-note">{{ curlSample.note }}</p>
        </div>
      </template>
    </div>
    <template #footer>
      <button type="button" class="btn-primary" @click="close">关闭</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.ukm { display: flex; flex-direction: column; gap: 12px; }
.ukm-lead { margin: 0; font-size: 13px; line-height: 1.55; color: #555; }
.ukm-tabs, .ukm-shells { display: flex; flex-wrap: wrap; gap: 6px; }
.ukm-tab, .ukm-shell {
  border: 1px solid rgba(0,0,0,.08);
  background: #f7f7f7;
  border-radius: 999px;
  padding: 6px 12px;
  font: inherit;
  font-size: 12px;
  font-weight: 750;
  color: #555;
  cursor: pointer;
}
.ukm-tab.on, .ukm-shell.on { background: #ffe60f; color: #1f1f1f; border-color: transparent; }
.ukm-block {
  border: 1px solid rgba(0,0,0,.08);
  border-radius: 12px;
  background: #fafafa;
  padding: 10px 12px;
}
.ukm-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;
}
.ukm-head strong { font-size: 12px; font-weight: 800; color: #333; }
.ukm-copy {
  border: none;
  background: #fff;
  border-radius: 8px;
  padding: 4px 10px;
  font: inherit;
  font-size: 12px;
  font-weight: 750;
  cursor: pointer;
  box-shadow: 0 0 0 1px rgba(0,0,0,.06);
}
.ukm-block pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
  line-height: 1.5;
  color: #222;
}
.ukm-note { margin: 8px 0 0; font-size: 12px; color: #777; line-height: 1.45; }
.ukm-note code { font-size: 11px; background: #f0f0f0; padding: 1px 4px; border-radius: 4px; }
</style>
