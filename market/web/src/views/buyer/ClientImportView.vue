<script setup lang="ts">
/**
 * 导入客户端：参考 aux-system 的 ClientImportWorkspace。
 * 选择 API Key → 选择导入目标 → 确认连接参数 → 一键导入 / 复制配置命令 / 下载配置文件。
 *
 * 明文只在选中时向 /api/buyer/keys/{id}/secret 取一次，留在内存里，不写浏览器存储；
 * 预览区一律显示打码后的 Key，复制 / 下载 / 唤起客户端时才用明文。
 */
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { buyer, market } from '@/api'
import type { BuyerKey } from '@/api/types'
import PageHeader from '@/components/PageHeader.vue'
import ApiCodeBlock from '@/components/docs/ApiCodeBlock.vue'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import { copyText } from '@/utils/clipboard'
import { normalizeCcSwitchPlatform } from '@/utils/ccswitchImport'
import { label } from '@/utils/labels'
import {
  CONFIG_FILE_NOTES,
  SETUP_COMMAND_NOTES,
  buildCCSwitchImportURL,
  buildClientConfigFiles,
  buildClientSetupPlan,
  buildConfigFile,
  buildDirectImportURL,
  fallbackClientModel,
  getClientImportTarget,
  maskAPIKey,
  normalizeImportBaseURL,
  resolveCCSwitchEndpoint,
  supportsConfigFile,
  supportsMultiModel,
  supportsSetupCommand,
  targetsForKind,
  type ClientConfigFile,
  type ClientImportKey,
  type ClientImportKind,
  type ClientImportTarget,
  type ClientImportTargetId,
  type ConfigFileTargetId,
  type DirectImportTargetId,
} from '@/utils/clientImport'
import { detectSetupPlatform, renderSetupCommand, setupPlanMergesJSON, setupPlanPaths, type SetupPlatform } from '@/utils/clientSetupCommand'
import { suggestKeyModels, type KeyModelSuggestions } from '@/utils/keyModelSuggestions'

type ConfigView = 'command' | 'file'

const route = useRoute()
const cfg = useConfig()
const toast = useToast()

const DEFAULT_PROVIDER = '闲猫'
const KINDS: Array<{ id: ClientImportKind; label: string; hint: string }> = [
  { id: 'cc-switch', label: 'CC Switch', hint: '一键导入' },
  { id: 'direct-import', label: 'Cherry / Chatbox', hint: '唤起客户端' },
  { id: 'config-file', label: '配置文件', hint: '命令或文件' },
]
const FILE_MIME: Record<ClientConfigFile['format'], string> = { JSON: 'application/json', TOML: 'application/toml', ENV: 'text/plain' }
const COPY_LABEL: Record<ClientConfigFile['format'], string> = { JSON: '复制 JSON', TOML: '复制 TOML', ENV: '复制 .env' }
const PREVIEW_LANG: Record<ClientConfigFile['format'], string> = { JSON: 'json', TOML: 'toml', ENV: 'shell' }

// ---- Key ----
const loadState = ref<'loading' | 'ready' | 'error'>('loading')
const loadError = ref('')
const keys = ref<BuyerKey[]>([])
const selectedKeyId = ref<number | null>(null)
/** 已取回的明文，按 Key id 缓存在内存里 */
const secrets = reactive<Record<number, string>>({})
const failed = reactive(new Set<number>())
const resolving = ref<number | null>(null)

// ---- 目标与参数 ----
const targetKind = ref<ClientImportKind>('cc-switch')
const targetId = ref<ClientImportTargetId>('claude-code')
const configView = ref<ConfigView>('command')
const setupPlatform = ref<SetupPlatform>(detectSetupPlatform())
const activeFileName = ref('')
const baseInput = ref('')
const providerName = ref(DEFAULT_PROVIDER)
const model = ref('')
const modelTouched = ref(false)
const candidates = ref<string[]>([])
const candidateInput = ref('')
const suggestions = reactive<Record<number, KeyModelSuggestions | 'loading'>>({})

async function loadKeys() {
  loadState.value = 'loading'
  try {
    const list = await buyer.keys()
    keys.value = (list ?? []).filter((k) => k.status === 'active')
    const wanted = Number(route.query.key)
    const pick =
      keys.value.find((k) => k.id === wanted) ?? keys.value.find((k) => k.recoverable !== false) ?? keys.value[0]
    if (!keys.value.some((k) => k.id === selectedKeyId.value)) selectedKeyId.value = pick?.id ?? null
    loadState.value = 'ready'
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '读取 API Key 失败'
    loadState.value = 'error'
  }
}

const selectedKey = computed(() => keys.value.find((k) => k.id === selectedKeyId.value) ?? null)
const plain = computed(() => (selectedKey.value ? secrets[selectedKey.value.id] ?? '' : ''))
const revealFailed = computed(() => !!selectedKey.value && failed.has(selectedKey.value.id))
const revealing = computed(() => !!selectedKey.value && resolving.value === selectedKey.value.id)
const maskedKey = computed(() => {
  if (plain.value) return maskAPIKey(plain.value)
  return selectedKey.value ? `${selectedKey.value.key_prefix}••••••••` : ''
})

function toImportKey(k: BuyerKey, secret: string): ClientImportKey {
  return { id: k.id, key: secret, name: k.name, status: k.status, platform: k.platform, created_at: k.created_at }
}
/** 真实明文的 Key；明文没取到时为 null（不能导入） */
const importKey = computed(() => (selectedKey.value && plain.value ? toImportKey(selectedKey.value, plain.value) : null))
/** 预览用的打码 Key，明文还没取到时也能先看配置长什么样 */
const previewKey = computed(() => (selectedKey.value ? toImportKey(selectedKey.value, maskedKey.value) : null))

async function reveal(k: BuyerKey) {
  if (secrets[k.id] || failed.has(k.id) || resolving.value === k.id) return
  if (k.recoverable === false) {
    failed.add(k.id)
    return
  }
  resolving.value = k.id
  try {
    const res = await buyer.keySecret(k.id)
    if (res.key) secrets[k.id] = res.key
    else failed.add(k.id)
  } catch {
    failed.add(k.id)
  } finally {
    if (resolving.value === k.id) resolving.value = null
  }
}

async function loadSuggestions(k: BuyerKey) {
  if (suggestions[k.id]) return
  suggestions[k.id] = 'loading'
  await cfg.ensure()
  const ids = [...new Set((k.hops ?? []).map((h) => h.listing_id).filter((id): id is number => !!id))]
  const lists = await Promise.all(ids.map((id) => market.listing(id).then((l) => l.models ?? []).catch(() => [] as string[])))
  const result = suggestKeyModels(lists.flat(), cfg.config.price_models ?? [], k.platform)
  suggestions[k.id] = result
  if (!modelTouched.value && selectedKeyId.value === k.id) model.value = result.models[0] ?? ''
}

watch(
  selectedKey,
  (k) => {
    if (!k) return
    void reveal(k)
    void loadSuggestions(k)
  },
  { immediate: true },
)

const keySuggestions = computed(() => {
  const s = selectedKey.value ? suggestions[selectedKey.value.id] : undefined
  return s && s !== 'loading' ? s : null
})
const modelsLoading = computed(() => !!selectedKey.value && suggestions[selectedKey.value.id] === 'loading')
const modelOptions = computed(() => keySuggestions.value?.models ?? [])
const keyHasListing = computed(() => (selectedKey.value?.hops ?? []).some((h) => !!h.listing_id))

// ---- 目标 ----
const target = computed<ClientImportTarget>(() => getClientImportTarget(targetId.value))
const kindTargets = computed(() => targetsForKind(targetKind.value))
const importMode = computed<ClientImportKind>(() =>
  kindTargets.value.includes(target.value) ? targetKind.value : target.value.kind,
)
const directTargetId = computed<DirectImportTargetId | null>(() =>
  targetId.value === 'cherry-studio' || targetId.value === 'chatbox' ? targetId.value : null,
)
const fileTargetId = computed<ConfigFileTargetId | DirectImportTargetId | null>(() => {
  if (importMode.value === 'direct-import') return directTargetId.value
  if (importMode.value === 'config-file' && supportsConfigFile(targetId.value)) return targetId.value as ConfigFileTargetId
  return null
})

function selectTarget(id: ClientImportTargetId, kind: ClientImportKind = targetKind.value) {
  const next = getClientImportTarget(id)
  targetId.value = id
  targetKind.value = targetsForKind(kind).includes(next) ? kind : next.kind
  activeFileName.value = ''
}

function selectKind(kind: ClientImportKind) {
  targetKind.value = kind
  const list = targetsForKind(kind)
  if (!list.includes(target.value)) selectTarget(list[0]!.id, kind)
}

function openSetupCommand() {
  targetKind.value = 'config-file'
  configView.value = 'command'
  activeFileName.value = ''
}

// ---- 参数 ----
const baseURL = computed(() => normalizeImportBaseURL(baseInput.value))
const provider = computed(() => providerName.value.trim() || DEFAULT_PROVIDER)
const multiModel = computed(() => supportsMultiModel(targetId.value))
const defaultModel = computed(() => model.value.trim())
const activeCandidates = computed(() => (multiModel.value ? candidates.value.filter((m) => m !== defaultModel.value) : []))
const candidateOptions = computed(() => modelOptions.value.filter((m) => m !== defaultModel.value && !candidates.value.includes(m)))

function onModelInput(v: string) {
  modelTouched.value = true
  model.value = v
}

function addCandidate(v = candidateInput.value) {
  const m = v.trim()
  if (!m || m.length > 160 || candidates.value.includes(m) || m === defaultModel.value) {
    candidateInput.value = ''
    return
  }
  candidates.value = [...candidates.value, m]
  candidateInput.value = ''
}

function removeCandidate(m: string) {
  candidates.value = candidates.value.filter((x) => x !== m)
}

const modelHelp = computed(() => {
  const t = target.value
  if (t.id === 'workbuddy') return 'WorkBuddy 会把模型写入 models.json 的 models 和 availableModels。'
  if (t.id === 'pi') return 'Pi 至少需要一个模型 ID，才能在 /model 里选到这个供应商。'
  if (t.id === 'zcode') return 'ZCode 配置文件用于快速填写，官方没有 JSON 导入协议。'
  if (importMode.value === 'direct-import') return '请确认客户端要用的模型 ID；Chatbox 会把它加入导入的模型列表。'
  if (importMode.value === 'config-file') {
    if (t.id === 'opencode' || t.id === 'openclaw') return `${t.name} 至少需要一个模型 ID，才能选中该供应商下的模型。`
    const fb = fallbackClientModel(t.id, selectedKey.value?.platform)
    return fb ? `命令和配置文件会写入这个模型 ID；留空时与 CC Switch 导入一致，使用 ${fb}。` : `命令和配置文件会写入这个模型 ID；留空时 ${t.name} 使用自己的默认模型。`
  }
  return '留空则由客户端决定默认模型。Claude Desktop 用非 Claude 模型时，可能需要在 CC Switch 里配置模型映射。'
})

// ---- 生成内容 ----
const configFiles = computed(() =>
  importKey.value && fileTargetId.value && baseURL.value
    ? buildClientConfigFiles(fileTargetId.value, importKey.value, baseURL.value, model.value, provider.value, activeCandidates.value)
    : null,
)
const previewFiles = computed(() =>
  previewKey.value && fileTargetId.value && baseURL.value
    ? buildClientConfigFiles(fileTargetId.value, previewKey.value, baseURL.value, model.value, provider.value, activeCandidates.value)
    : null,
)
const activeFileIndex = computed(() => Math.max(0, previewFiles.value?.findIndex((f) => f.fileName === activeFileName.value) ?? 0))
const activeFile = computed(() => configFiles.value?.[activeFileIndex.value] ?? null)
const activePreviewFile = computed(() => previewFiles.value?.[activeFileIndex.value] ?? null)
const multiFile = computed(() => (previewFiles.value?.length ?? 0) > 1)

const commandAvailable = computed(() => importMode.value === 'config-file' && supportsSetupCommand(targetId.value))
const showCommand = computed(() => commandAvailable.value && configView.value === 'command')
function planFor(k: ClientImportKey | null) {
  const id = targetId.value
  if (!showCommand.value || !k || !baseURL.value || !supportsSetupCommand(id)) return null
  return buildClientSetupPlan(id, k, baseURL.value, model.value, provider.value, activeCandidates.value)
}
const setupPlan = computed(() => planFor(importKey.value))
const previewPlan = computed(() => planFor(previewKey.value))
const previewCommand = computed(() => (previewPlan.value ? renderSetupCommand(previewPlan.value, setupPlatform.value) : ''))
const commandNote = computed(() => {
  const id = targetId.value
  return supportsSetupCommand(id) ? SETUP_COMMAND_NOTES[id] : ''
})
const fileNote = computed(() => {
  const id = targetId.value
  return supportsConfigFile(id) ? CONFIG_FILE_NOTES[id] : ''
})

const ccSwitchURL = computed(() =>
  importKey.value && importMode.value === 'cc-switch' && baseURL.value
    ? buildCCSwitchImportURL({ key: importKey.value, target: target.value, baseURL: baseURL.value, providerName: provider.value, model: model.value })
    : null,
)
const directImportURL = computed(() =>
  importKey.value && directTargetId.value && importMode.value === 'direct-import' && baseURL.value
    ? buildDirectImportURL(
        directTargetId.value,
        buildConfigFile(directTargetId.value, importKey.value, baseURL.value, model.value, provider.value, activeCandidates.value),
      )
    : null,
)
const canImport = computed(() => !!importKey.value && !!baseURL.value)
const blockedLabel = computed(() => {
  if (!selectedKey.value) return '请先选择 API Key'
  if (revealing.value) return '正在读取密钥…'
  if (revealFailed.value) return '该 Key 无法用于导入'
  if (!baseURL.value) return '请填写正确的 API 地址'
  return '填写完整参数后继续'
})
const previewPlaceholder = computed(() => (!baseURL.value ? '请先填写正确的 API 基础地址' : '选择 API Key 后显示配置预览'))
const ccsEndpoint = computed(() =>
  baseURL.value ? resolveCCSwitchEndpoint(targetId.value, normalizeCcSwitchPlatform(selectedKey.value?.platform), baseURL.value).endpoint : '等待填写',
)
const stepFourTitle = computed(() => (showCommand.value ? '一键配置' : importMode.value === 'config-file' ? '下载配置' : '导入配置'))
const stepFourDesc = computed(() => {
  if (importMode.value === 'cc-switch') return '点击后会唤起 CC Switch，应用里还会再弹一次确认。'
  if (showCommand.value) return '复制命令到终端运行，自动写入当前 Key、地址和模型；也可以切到「手动配置」下载文件。'
  if (importMode.value === 'config-file') return '生成包含当前 Key、地址和模型 ID 的配置文件，可先预览再下载或复制。'
  return `点击后会唤起 ${target.value.name}，请在客户端里检查并确认导入。`
})

// ---- 动作 ----
function launch(url: string | null, name: string) {
  if (!url) return
  window.location.href = url
  toast.info(`已尝试唤起 ${name}，请在应用里确认导入；没有反应说明本机还没安装`)
}

async function copyCommand() {
  if (!setupPlan.value) return
  if (await copyText(renderSetupCommand(setupPlan.value, setupPlatform.value))) {
    toast.success(setupPlatform.value === 'windows' ? '配置命令已复制，粘到 PowerShell 运行' : '配置命令已复制，粘到终端运行')
  } else {
    toast.error('复制失败，请切到「手动配置」下载配置文件')
  }
}

async function copyConfig() {
  const f = activeFile.value
  if (!f) return
  if (await copyText(f.content)) toast.success(f.path ? `${f.fileName} 已复制，粘到 ${f.path} 并保存` : `${f.fileName} 已复制，可在客户端的自定义供应商里粘贴`)
  else toast.error('复制失败，请改用下载')
}

function downloadConfig() {
  const f = activeFile.value
  if (!f) return
  const href = URL.createObjectURL(new Blob([f.content], { type: FILE_MIME[f.format] }))
  const a = document.createElement('a')
  a.href = href
  a.download = f.fileName
  a.click()
  setTimeout(() => URL.revokeObjectURL(href), 0)
  toast.success(f.path ? `${f.fileName} 已下载，请移到 ${f.path}` : `${f.fileName} 已下载，只在可信设备上保存`)
}

function platformText(k: BuyerKey) {
  return k.platform ? label('platform', k.platform).text : '未识别平台'
}

onMounted(async () => {
  await cfg.ensure()
  baseInput.value = typeof route.query.api_base === 'string' ? route.query.api_base : cfg.config.edge_url || window.location.origin
  await loadKeys()
})
</script>

<template>
  <div class="ci-page seller-page">
    <PageHeader title="导入客户端" description="把一个闲猫 API Key 带到你正在用的客户端：CC Switch、Cherry Studio、Chatbox 一键导入，Claude Code、Codex、OpenCode 等复制一条命令即可。">
      <RouterLink to="/docs" class="xm-btn xm-btn-ghost xm-btn-sm">API 文档</RouterLink>
    </PageHeader>

    <div class="ci-safe" role="note">
      <span class="ci-safe__ico" aria-hidden="true">
        <svg viewBox="0 0 16 16" width="15" height="15" fill="none"><rect x="3" y="7" width="10" height="7" rx="2" stroke="currentColor" stroke-width="1.6" /><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" stroke="currentColor" stroke-width="1.6" /></svg>
      </span>
      <p>密钥明文只在这个页面的内存里，不写浏览器存储；预览一律打码，复制、下载或唤起客户端时才会带上完整 Key。</p>
    </div>

    <div v-if="loadState === 'loading'" class="ci-state" role="status">
      <span class="ci-spin" aria-hidden="true" />正在读取你的 API Key…
    </div>

    <div v-else-if="loadState === 'error'" class="ci-state is-error">
      <p>{{ loadError }}</p>
      <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="loadKeys">重新读取</button>
    </div>

    <div v-else-if="!keys.length" class="ci-empty">
      <strong>还没有启用中的 API Key</strong>
      <p>先去市场选一个通道，或在「API Key」里创建一个 Key，再回来导入客户端。</p>
      <div class="ci-empty__actions">
        <RouterLink to="/buyer/keys" class="xm-btn xm-btn-primary">去创建 Key</RouterLink>
        <RouterLink to="/" class="xm-btn xm-btn-ghost">去市场看看</RouterLink>
      </div>
    </div>

    <div v-else class="ci-flow">
      <!-- 01 选择 Key -->
      <section class="ci-step" aria-labelledby="ci-step-key">
        <header class="ci-step__head">
          <span class="ci-step__no">01</span>
          <div>
            <h2 id="ci-step-key">选择 API Key</h2>
            <p>只列出启用中的 Key；导入时会自动读取明文，页面上不显示完整 Key。</p>
          </div>
        </header>
        <label class="xm-field-label" for="ci-key">API Key</label>
        <select id="ci-key" v-model="selectedKeyId" class="xm-input">
          <option v-for="k in keys" :key="k.id" :value="k.id">
            {{ k.name || `API Key #${k.id}` }} · {{ platformText(k) }}{{ k.recoverable === false ? ' · 无法导入' : '' }}
          </option>
        </select>
        <div v-if="selectedKey" class="ci-keycard">
          <div>
            <span>当前选择</span>
            <strong>{{ selectedKey.name || `API Key #${selectedKey.id}` }}</strong>
          </div>
          <code>{{ maskedKey }}</code>
          <small>{{ platformText(selectedKey) }} · {{ (selectedKey.hops ?? []).length }} 跳路线</small>
        </div>
        <p v-if="revealing" class="ci-inline-status" role="status"><span class="ci-spin" aria-hidden="true" />正在安全读取密钥明文…</p>
        <div v-if="revealFailed" class="seller-callout seller-callout-warn ci-callout">
          这个 Key 无法再次读取明文（多半创建于加密保存明文之前），不能用于导入。请到「API Key」新建一个，或改选其他 Key。
        </div>
      </section>

      <!-- 02 选择目标 -->
      <section class="ci-step" aria-labelledby="ci-step-target">
        <header class="ci-step__head">
          <span class="ci-step__no">02</span>
          <div>
            <h2 id="ci-step-target">选择导入目标</h2>
            <p>CC Switch、Cherry Studio、Chatbox 可以一键导入；Claude Code、Codex、OpenCode、Pi 等也能复制命令或下载配置文件。</p>
          </div>
        </header>
        <div class="ci-kinds" role="tablist" aria-label="导入方式">
          <button
            v-for="k in KINDS"
            :key="k.id"
            type="button"
            role="tab"
            class="ci-kind"
            :class="{ on: targetKind === k.id }"
            :aria-selected="targetKind === k.id"
            @click="selectKind(k.id)"
          >
            <strong>{{ k.label }}</strong><span>{{ k.hint }}</span>
          </button>
        </div>
        <div class="ci-targets" role="radiogroup" aria-label="选择客户端">
          <button
            v-for="t in kindTargets"
            :key="t.id"
            type="button"
            role="radio"
            class="ci-target"
            :class="{ on: t.id === targetId }"
            :aria-checked="t.id === targetId"
            @click="selectTarget(t.id)"
          >
            <span class="ci-target__ico">{{ t.icon }}</span>
            <span class="ci-target__copy">
              <strong>{{ t.name }}</strong>
              <small>{{ targetKind === 'config-file' && t.configDescription ? t.configDescription : t.description }}</small>
            </span>
            <svg v-if="t.id === targetId" class="ci-target__check" viewBox="0 0 16 16" width="16" height="16" fill="none" aria-hidden="true">
              <path d="M3.5 8.5 6.5 11.5 12.5 4.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </section>

      <!-- 03 连接参数 -->
      <section class="ci-step" aria-labelledby="ci-step-config">
        <header class="ci-step__head">
          <span class="ci-step__no">03</span>
          <div>
            <h2 id="ci-step-config">确认连接参数</h2>
            <p>API 地址默认是平台 Edge 地址；只有在你走自己的转发时才需要改。</p>
          </div>
        </header>
        <div class="ci-fields">
          <div class="ci-field">
            <label class="xm-field-label" for="ci-base">API 基础地址</label>
            <input id="ci-base" v-model="baseInput" type="url" class="xm-input" :class="{ 'is-invalid': !baseURL }" spellcheck="false" autocomplete="off" placeholder="https://api.example.com" :aria-invalid="!baseURL" />
            <small class="xm-hint" :class="{ 'is-error': !baseURL }">{{ baseURL ? '导入时会按客户端协议自动补 /v1 等路径。' : '请输入不带账号、参数和接口路径的 http(s) 地址。' }}</small>
          </div>
          <div class="ci-field">
            <label class="xm-field-label" for="ci-provider">供应商名称</label>
            <input id="ci-provider" v-model="providerName" class="xm-input" maxlength="80" :placeholder="DEFAULT_PROVIDER" />
            <small class="xm-hint">会显示在客户端的供应商列表里。</small>
          </div>
          <div class="ci-field ci-field--wide">
            <label class="xm-field-label" for="ci-model">默认模型 ID <span class="ci-optional">可选</span></label>
            <div class="ci-model">
              <input
                id="ci-model"
                :value="model"
                class="xm-input"
                list="ci-model-options"
                spellcheck="false"
                autocomplete="off"
                :placeholder="modelsLoading ? '正在读取可用模型…' : '留空则用客户端默认模型'"
                aria-describedby="ci-model-help"
                @input="onModelInput(($event.target as HTMLInputElement).value)"
              />
              <button v-if="model" type="button" class="ci-model__clear" aria-label="清空默认模型" @click="onModelInput('')">清空</button>
              <datalist id="ci-model-options">
                <option v-for="m in modelOptions" :key="m" :value="m" />
              </datalist>
            </div>
            <div v-if="modelOptions.length" class="ci-chips" role="group" aria-label="可选模型">
              <button
                v-for="m in modelOptions.slice(0, 12)"
                :key="m"
                type="button"
                class="ci-chip"
                :class="{ on: m === defaultModel }"
                @click="onModelInput(m)"
              >{{ m }}</button>
            </div>
            <small id="ci-model-help" class="xm-hint">
              <template v-if="keySuggestions?.source === 'catalog'">{{ keyHasListing ? '没读到路线上卖家开放的模型' : '这个 Key 走保障池' }}，下面是平台同类模型，仅供参考；</template>
              <template v-else-if="keySuggestions?.source === 'listing'">建议来自这个 Key 路线上卖家开放的模型；</template>
              {{ modelHelp }}
            </small>
          </div>
          <div v-if="multiModel" class="ci-field ci-field--wide">
            <label class="xm-field-label" for="ci-candidate">候选模型 ID <span class="ci-optional">可选，可多个</span></label>
            <div class="ci-model">
              <input
                id="ci-candidate"
                v-model="candidateInput"
                class="xm-input"
                list="ci-candidate-options"
                spellcheck="false"
                autocomplete="off"
                placeholder="输入或选择模型 ID，回车添加"
                @keydown.enter.prevent="addCandidate()"
              />
              <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="!candidateInput.trim()" @click="addCandidate()">添加</button>
              <datalist id="ci-candidate-options">
                <option v-for="m in candidateOptions" :key="m" :value="m" />
              </datalist>
            </div>
            <div v-if="activeCandidates.length" class="ci-chips" role="list" aria-label="已选候选模型">
              <span v-for="m in activeCandidates" :key="m" class="ci-chip on" role="listitem">
                {{ m }}
                <button type="button" class="ci-chip__x" :aria-label="`移除 ${m}`" @click="removeCandidate(m)">×</button>
              </span>
            </div>
            <small class="xm-hint">{{ target.name }} 支持写入多个模型：默认模型排第一，候选模型按添加顺序追加{{ defaultModel ? '' : '；没填默认模型时，第一个候选就是默认' }}。</small>
          </div>
        </div>
      </section>

      <!-- 04 导入 -->
      <section class="ci-step ci-step--final" aria-labelledby="ci-step-import">
        <header class="ci-step__head">
          <span class="ci-step__no">04</span>
          <div>
            <h2 id="ci-step-import">{{ stepFourTitle }}</h2>
            <p>{{ stepFourDesc }}</p>
          </div>
        </header>

        <div class="ci-preview">
          <div class="ci-preview__head">
            <div class="ci-preview__who">
              <span class="ci-target__ico">{{ target.icon }}</span>
              <div>
                <strong>{{ target.name }}</strong>
                <small>{{ selectedKey ? `${selectedKey.name || 'API Key'} · ${maskedKey}` : '未选择 API Key' }}</small>
              </div>
            </div>
            <span class="ci-status" :class="{ 'is-blocked': !canImport }">{{ canImport ? '可以导入' : revealing ? '读取中' : '不可导入' }}</span>
          </div>

          <dl v-if="importMode === 'cc-switch'" class="ci-dl">
            <div><dt>导入方式</dt><dd>CC Switch 深度链接</dd></div>
            <div><dt>接口地址</dt><dd><code>{{ ccsEndpoint }}</code></dd></div>
            <div><dt>默认模型</dt><dd>{{ defaultModel || '由客户端决定' }}</dd></div>
            <div><dt>余额查询</dt><dd>导入后 CC Switch 每 30 分钟读取一次 /v1/usage，余额按人民币（CNY）显示</dd></div>
          </dl>

          <template v-else>
            <div v-if="importMode === 'config-file'" class="ci-filebar">
              <div class="ci-filebar__controls">
                <div v-if="commandAvailable" class="ci-seg" role="tablist" aria-label="配置方式">
                  <button type="button" role="tab" :class="{ on: configView === 'command' }" :aria-selected="configView === 'command'" @click="configView = 'command'">一键命令</button>
                  <button type="button" role="tab" :class="{ on: configView === 'file' }" :aria-selected="configView === 'file'" @click="configView = 'file'; activeFileName = ''">手动配置</button>
                </div>
                <div v-if="showCommand" class="ci-seg" role="tablist" aria-label="运行环境">
                  <button type="button" role="tab" :class="{ on: setupPlatform === 'unix' }" :aria-selected="setupPlatform === 'unix'" @click="setupPlatform = 'unix'">macOS / Linux</button>
                  <button type="button" role="tab" :class="{ on: setupPlatform === 'windows' }" :aria-selected="setupPlatform === 'windows'" @click="setupPlatform = 'windows'">Windows</button>
                </div>
                <div v-else-if="multiFile && previewFiles" class="ci-seg" role="tablist" :aria-label="`${target.name} 配置文件`">
                  <button
                    v-for="f in previewFiles"
                    :key="f.fileName"
                    type="button"
                    role="tab"
                    :class="{ on: activePreviewFile?.fileName === f.fileName }"
                    :aria-selected="activePreviewFile?.fileName === f.fileName"
                    @click="activeFileName = f.fileName"
                  >{{ f.fileName }}</button>
                </div>
              </div>
              <small v-if="showCommand && previewPlan">
                {{ setupPlatform === 'windows' ? '在 PowerShell 中' : '在终端中' }}运行，写入
                <template v-for="(p, i) in setupPlanPaths(previewPlan)" :key="p">{{ i > 0 ? '、' : '' }}<code>{{ p }}</code></template>
              </small>
              <small v-else-if="!showCommand && activePreviewFile?.path">保存到 <code>{{ activePreviewFile.path }}</code></small>
            </div>
            <ApiCodeBlock
              v-if="showCommand"
              :code="previewCommand"
              :language="setupPlatform === 'windows' ? 'powershell' : 'shell'"
              :placeholder="previewPlaceholder"
              :copyable="false"
              :aria-label="`${target.name} 配置命令预览（已打码）`"
            />
            <ApiCodeBlock
              v-else
              :code="activePreviewFile?.content ?? ''"
              :language="activePreviewFile ? PREVIEW_LANG[activePreviewFile.format] : 'json'"
              :placeholder="previewPlaceholder"
              :copyable="false"
              :aria-label="`${target.name} 配置预览（已打码）`"
            />
            <p class="ci-masked-note">预览中的 Key 已打码，用下面的按钮复制或下载会带上完整 Key。</p>
          </template>
        </div>

        <div class="ci-actions">
          <template v-if="importMode === 'cc-switch'">
            <button type="button" class="xm-btn xm-btn-primary" :disabled="!ccSwitchURL" @click="launch(ccSwitchURL, 'CC Switch')">
              {{ ccSwitchURL ? '一键导入 CC Switch' : blockedLabel }}
            </button>
            <button v-if="supportsSetupCommand(target.id)" type="button" class="xm-btn xm-btn-ghost" @click="openSetupCommand">改用一键配置命令</button>
          </template>
          <template v-else-if="importMode === 'direct-import'">
            <button type="button" class="xm-btn xm-btn-primary" :disabled="!directImportURL" @click="launch(directImportURL, target.name)">
              {{ directImportURL ? `一键导入 ${target.name}` : blockedLabel }}
            </button>
            <button type="button" class="xm-btn xm-btn-ghost" :disabled="!activeFile" @click="downloadConfig">下载配置文件</button>
            <button type="button" class="xm-btn xm-btn-ghost" :disabled="!activeFile" @click="copyConfig">复制 JSON</button>
          </template>
          <template v-else-if="showCommand">
            <button type="button" class="xm-btn xm-btn-primary" :disabled="!setupPlan" @click="copyCommand">
              {{ setupPlan ? '复制配置命令' : blockedLabel }}
            </button>
          </template>
          <template v-else>
            <button type="button" class="xm-btn xm-btn-primary" :disabled="!activeFile" @click="downloadConfig">
              {{ activeFile ? (multiFile ? `下载 ${activeFile.fileName}` : `下载 ${target.name} 配置`) : blockedLabel }}
            </button>
            <button type="button" class="xm-btn xm-btn-ghost" :disabled="!activeFile" @click="copyConfig">{{ activeFile ? COPY_LABEL[activeFile.format] : '复制配置' }}</button>
          </template>
        </div>

        <ul class="ci-notes">
          <li v-if="importMode === 'cc-switch'">没装 CC Switch 时，可以在「配置文件」里复制一键配置命令，或下载 Claude Code、Codex、OpenCode 等的原生配置。CC Switch 暂不支持通过链接导入 Pi，用 Pi 请走「配置文件」。</li>
          <li v-if="importMode === 'direct-import'">请先安装目标客户端。链接只会交给本机应用；浏览器没唤起应用时，可下载配置文件或复制 JSON 后在客户端里导入。不要把带 Key 的链接发给别人。</li>
          <template v-if="showCommand && commandNote">
            <li>{{ commandNote }}{{ setupPlatform === 'windows' ? '请在 Windows PowerShell 或 PowerShell 7 里运行，不支持 cmd。' : `请在 bash 或 zsh 里运行${previewPlan && setupPlanMergesJSON(previewPlan) ? '；已有 JSON 配置时需要本机装有 Node.js 或 Python 3 来合并' : ''}。` }}</li>
            <li>命令里有完整 API Key，会写进本机配置文件，也可能留在终端历史里，别截图或外发。已有配置会先备份成同目录下的 .bak-时间戳 文件。</li>
          </template>
          <li v-if="importMode === 'config-file' && !showCommand && fileNote">{{ fileNote }}</li>
          <li>不再使用时，到「API Key」里停用或删除对应的 Key 即可让已导入的配置失效。</li>
        </ul>
      </section>
    </div>
  </div>
</template>

<style scoped>
:where(.ci-page) code {
  border-radius: 0;
  background: transparent;
  padding: 0;
  font-size: inherit;
  color: inherit;
}
.ci-page {
  --ci-mono: 'Spline Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  --ci-line: rgba(0, 0, 0, 0.08);
}
.ci-safe {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 16px;
  border: 1px solid rgba(240, 194, 0, 0.45);
  border-radius: 14px;
  background: #fffbeb;
  padding: 12px 14px;
}
.ci-safe__ico {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  background: #ffd400;
  color: #1f1f1f;
}
.ci-safe p {
  margin: 3px 0 0;
  font-size: 13px;
  line-height: 1.6;
  color: #5b4a00;
}
.ci-state {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  border: 1px dashed var(--ci-line);
  border-radius: 16px;
  padding: 28px;
  font-size: 14px;
  color: #666;
}
.ci-state.is-error { color: #b91c1c; }
.ci-state p { margin: 0; }
.ci-spin {
  display: inline-block;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 2px solid rgba(0, 0, 0, 0.12);
  border-top-color: #1f1f1f;
  animation: ci-spin 0.8s linear infinite;
}
@keyframes ci-spin { to { transform: rotate(360deg); } }
.ci-empty {
  border: 1px solid var(--ci-line);
  border-radius: 18px;
  background: linear-gradient(180deg, #fffdf2, #fff);
  padding: 32px 24px;
  text-align: center;
}
.ci-empty strong { font-size: 17px; font-weight: 800; }
.ci-empty p { margin: 8px auto 18px; max-width: 420px; font-size: 13px; line-height: 1.6; color: #666; }
.ci-empty__actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 10px; }

.ci-flow { display: flex; flex-direction: column; gap: 14px; }
.ci-step {
  border: 1px solid var(--ci-line);
  border-radius: 18px;
  background: #fff;
  padding: 18px 20px 20px;
}
.ci-step--final { background: #fcfcfb; }
.ci-step__head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 16px;
}
.ci-step__no {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: #1f1f1f;
  color: #ffd400;
  font-family: var(--ci-mono);
  font-size: 13px;
  font-weight: 700;
}
.ci-step__head h2 { margin: 0; font-size: 17px; font-weight: 800; letter-spacing: -0.01em; }
.ci-step__head p { margin: 4px 0 0; font-size: 13px; line-height: 1.55; color: #777; }

.ci-keycard {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 4px 12px;
  align-items: center;
  margin-top: 12px;
  border-radius: 14px;
  background: #fafafa;
  border: 1px solid var(--ci-line);
  padding: 12px 14px;
}
.ci-keycard span { display: block; font-size: 11px; font-weight: 700; color: #999; }
.ci-keycard strong { display: block; font-size: 14px; font-weight: 800; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ci-keycard code {
  border-radius: 9px;
  background: #fffdf0;
  border: 1.5px solid rgba(255, 212, 0, 0.8);
  padding: 6px 10px;
  font-family: var(--ci-mono);
  font-size: 13px;
  font-weight: 600;
}
.ci-keycard small { grid-column: 1 / -1; font-size: 12px; color: #888; }
.ci-inline-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 10px 0 0;
  font-size: 12px;
  color: #777;
}
.ci-callout { margin-top: 12px; }

.ci-kinds {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
  margin-bottom: 14px;
  border-radius: 14px;
  background: #f3f3f3;
  padding: 4px;
}
.ci-kind {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  border: none;
  border-radius: 11px;
  background: transparent;
  padding: 8px 6px;
  font: inherit;
  color: #777;
  cursor: pointer;
}
.ci-kind strong { font-size: 13px; font-weight: 800; }
.ci-kind span { font-size: 11px; font-weight: 600; }
.ci-kind.on { background: #fff; color: #1f1f1f; box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08); }
.ci-kind.on span { color: #8a6a00; }
.ci-targets {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 8px;
}
.ci-target {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  border: 1.5px solid rgba(0, 0, 0, 0.1);
  border-radius: 14px;
  background: #fafafa;
  padding: 12px 34px 12px 12px;
  font: inherit;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.ci-target:hover { border-color: rgba(0, 0, 0, 0.3); }
.ci-target.on { border-color: #1f1f1f; background: rgba(255, 230, 15, 0.28); }
.ci-target__ico {
  flex-shrink: 0;
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: #1f1f1f;
  color: #ffd400;
  font-family: var(--ci-mono);
  font-size: 12px;
  font-weight: 700;
}
.ci-target__copy strong { display: block; font-size: 14px; font-weight: 800; }
.ci-target__copy small { display: block; margin-top: 2px; font-size: 12px; line-height: 1.45; color: #777; }
.ci-target__check { position: absolute; top: 12px; right: 12px; color: #1f1f1f; }

.ci-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.ci-field--wide { grid-column: 1 / -1; }
.ci-optional {
  margin-left: 4px;
  border-radius: 6px;
  background: #f1f1f1;
  padding: 1px 6px;
  font-size: 11px;
  font-weight: 700;
  color: #888;
}
.xm-input.is-invalid { border-color: #f07171; }
.xm-hint.is-error { color: #b91c1c; }
.ci-model { display: flex; gap: 8px; align-items: center; }
.ci-model .xm-input { flex: 1; min-width: 0; font-family: var(--ci-mono); font-size: 13px; }
.ci-model__clear {
  flex-shrink: 0;
  border: none;
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  color: #888;
  cursor: pointer;
}
.ci-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.ci-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 999px;
  background: #fff;
  padding: 4px 10px;
  font-family: var(--ci-mono);
  font-size: 12px;
  color: #444;
  cursor: pointer;
}
.ci-chip:hover { border-color: #1f1f1f; }
.ci-chip.on { border-color: #1f1f1f; background: #fff3a8; color: #1f1f1f; cursor: default; }
.ci-chip__x {
  border: none;
  background: transparent;
  padding: 0 0 0 2px;
  font-size: 14px;
  line-height: 1;
  color: #666;
  cursor: pointer;
}

.ci-preview {
  border: 1px solid var(--ci-line);
  border-radius: 16px;
  background: #fff;
  padding: 14px;
}
.ci-preview__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
}
.ci-preview__who { display: flex; align-items: center; gap: 10px; min-width: 0; }
.ci-preview__who strong { display: block; font-size: 14px; font-weight: 800; }
.ci-preview__who small { display: block; font-size: 12px; color: #888; font-family: var(--ci-mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ci-status {
  flex-shrink: 0;
  border-radius: 999px;
  background: #e5f6ec;
  padding: 3px 10px;
  font-size: 12px;
  font-weight: 800;
  color: #1a7a45;
}
.ci-status.is-blocked { background: #f3f3f3; color: #888; }
.ci-dl { margin: 0; display: grid; gap: 1px; border-radius: 12px; overflow: hidden; background: var(--ci-line); }
.ci-dl > div { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 12px; background: #fafafa; padding: 10px 12px; }
.ci-dl dt { font-size: 12px; font-weight: 700; color: #888; }
.ci-dl dd { margin: 0; font-size: 13px; color: #1f1f1f; overflow-wrap: anywhere; }
.ci-dl code { font-family: var(--ci-mono); font-size: 12.5px; }
.ci-filebar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
  margin-bottom: 10px;
}
.ci-filebar__controls { display: flex; flex-wrap: wrap; gap: 8px; }
.ci-filebar small { font-size: 12px; color: #777; }
.ci-filebar code { font-family: var(--ci-mono); font-size: 12px; color: #8a6a00; }
.ci-seg { display: inline-flex; gap: 3px; border-radius: 10px; background: #f3f3f3; padding: 3px; }
.ci-seg button {
  border: none;
  border-radius: 8px;
  background: transparent;
  padding: 5px 11px;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  color: #777;
  cursor: pointer;
  white-space: nowrap;
}
.ci-seg button.on { background: #fff; color: #1f1f1f; box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08); }
.ci-masked-note { margin: 8px 0 0; font-size: 12px; color: #999; }
.ci-actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 14px; }
.ci-notes { margin: 14px 0 0; padding: 0; list-style: none; display: grid; gap: 6px; }
.ci-notes li {
  position: relative;
  padding-left: 16px;
  font-size: 12px;
  line-height: 1.65;
  color: #666;
}
.ci-notes li::before {
  content: '';
  position: absolute;
  left: 3px;
  top: 0.65em;
  width: 5px;
  height: 5px;
  border-radius: 2px;
  background: #ffd400;
}
.ci-kind:focus-visible,
.ci-target:focus-visible,
.ci-chip:focus-visible,
.ci-seg button:focus-visible,
.ci-model__clear:focus-visible,
.ci-chip__x:focus-visible {
  outline: 2px solid #1f1f1f;
  outline-offset: 2px;
}
@media (max-width: 720px) {
  .ci-step { padding: 16px 14px; }
  .ci-fields { grid-template-columns: minmax(0, 1fr); }
  .ci-kinds { grid-template-columns: minmax(0, 1fr); }
  .ci-keycard { grid-template-columns: minmax(0, 1fr); }
  .ci-actions .xm-btn { flex: 1 1 auto; }
  .ci-dl > div { grid-template-columns: minmax(0, 1fr); gap: 2px; }
}
@media (prefers-reduced-motion: reduce) {
  .ci-spin { animation-duration: 2.4s; }
  .ci-target { transition: none; }
}
</style>
