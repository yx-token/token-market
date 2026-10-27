<script setup lang="ts">
/**
 * 管理端上游详情 / 编辑：代卖家完成授权、选模型，把 draft / selecting_models 推进到 ready。
 */
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Select from '@/components/common/Select.vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { useConsoleTabs } from '@/stores/consoleTabs'

defineOptions({ name: 'MarketAdminUpstreamDetail' })

const route = useRoute()
const router = useRouter()
const { open: openTab, close: closeTab } = useConsoleTabs()

interface Upstream {
  id: number
  seller_id: number
  seller_email?: string
  platform: string
  auth_type?: string
  account_email?: string | null
  code?: string
  name?: string | null
  note?: string | null
  status: string
  health?: string
  health_reason?: string
  failure_reason?: string
  grade?: string | null
  tier?: string | null
  privacy_ok?: boolean | null
  is_reference?: boolean
  model_whitelist?: string[] | null
  created_at?: string
}

interface UpstreamModels {
  catalog: string[]
  whitelist: string[]
  detection_status?: string
}

interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types?: string[]
}

const STATUS_LABEL: Record<string, string> = {
  ready: '就绪',
  listed: '已挂单',
  selecting_models: '待选模型',
  verifying: '检测中',
  suspended: '已冻结',
  verify_failed: '检测失败',
  draft: '草稿',
  authorizing: '授权中',
  failed: '失败',
  removing: '移除中',
}

const PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: '智谱' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'opencode_go', label: 'OpenCode Go' },
]

const AUTH_FALLBACK: Record<string, string[]> = {
  anthropic: ['oauth', 'setup-token'],
  openai: ['oauth'],
  gemini: ['oauth', 'google_one'],
  antigravity: ['oauth'],
  grok: ['oauth'],
  kimi: ['coding'],
  zhipu: ['coding', 'coding-intl'],
  minimax: ['coding', 'coding-intl'],
  opencode_go: ['go'],
}

const AUTH_LABEL: Record<string, string> = {
  oauth: 'OAuth 授权',
  'setup-token': 'Setup Token',
  google_one: 'Google One',
  coding: 'Coding Plan',
  'coding-intl': 'Coding Plan 国际站',
  go: 'OpenCode Go',
}

const upstreamId = computed(() => {
  const n = Number(route.params.id)
  return Number.isFinite(n) ? n : 0
})

const row = ref<Upstream | null>(null)
const models = ref<UpstreamModels | null>(null)
const selected = ref<string[]>([])
const customModel = ref('')
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const authURL = ref('')
const oauthCode = ref('')
const projectID = ref('')
const apiKey = ref('')
const organization = ref('')
const project = ref('')

const vendors = ref<VendorConfig[]>([])
const switchPlatform = ref('')
const switchAuthType = ref('')

const needsAuth = computed(() =>
  !!row.value && ['draft', 'authorizing', 'failed'].includes(row.value.status),
)
const needsModels = computed(() =>
  !!row.value &&
  ['selecting_models', 'ready', 'listed', 'verify_failed', 'verifying', 'suspended'].includes(
    row.value.status,
  ),
)
const isCoding = computed(() => {
  const t = (row.value?.auth_type || '').toLowerCase()
  return t.includes('coding') || t.includes('apikey') || t === 'api_key' || t === 'key' || t === 'go'
})
const isOAuth = computed(() => !isCoding.value)

function authTypesForPlatform(platform: string): string[] {
  if (!platform) return []
  const v = vendors.value.find((x) => x.platform === platform)
  const supported = Array.isArray(v?.supported_auth_types) ? v!.supported_auth_types.filter(Boolean) : []
  const opened = Array.isArray(v?.auth_types) ? v!.auth_types.filter(Boolean) : []
  const merged = [...new Set([...supported, ...opened])]
  if (merged.length) return merged
  return AUTH_FALLBACK[platform] || ['oauth']
}

const switchPlatformOptions = computed(() => {
  const fromApi = vendors.value
    .filter((v) => v.enabled)
    .map((v) => ({
      value: v.platform,
      label: PLATFORM_OPTIONS.find((o) => o.value === v.platform)?.label || v.platform,
    }))
  if (fromApi.length) return fromApi
  return Object.keys(AUTH_FALLBACK).map((p) => ({
    value: p,
    label: PLATFORM_OPTIONS.find((o) => o.value === p)?.label || p,
  }))
})

const switchAuthOptions = computed(() =>
  authTypesForPlatform(switchPlatform.value).map((t) => ({
    value: t,
    label: AUTH_LABEL[t] || t,
  })),
)

function syncSwitchAuthType() {
  const opts = authTypesForPlatform(switchPlatform.value)
  if (!opts.length) {
    switchAuthType.value = ''
    return
  }
  if (!opts.includes(switchAuthType.value)) {
    switchAuthType.value = opts[0]
  }
}

function syncSwitchFormFromRow() {
  if (!row.value) return
  switchPlatform.value = row.value.platform || ''
  switchAuthType.value = row.value.auth_type || ''
  syncSwitchAuthType()
}

watch(switchPlatform, () => {
  syncSwitchAuthType()
})

function statusLabel(s?: string) {
  return (s && STATUS_LABEL[s]) || s || '—'
}

function backToList() {
  const path = '/admin/market/upstreams'
  closeTab(`/admin/market/upstreams/${upstreamId.value}`)
  router.push(path)
}

async function loadVendors() {
  try {
    vendors.value = (await marketGet<VendorConfig[] | null>(`${MARKET_API_PREFIX}/vendors`)) ?? []
  } catch {
    vendors.value = []
  }
}

async function load() {
  if (!upstreamId.value) {
    error.value = '无效上游 ID'
    return
  }
  loading.value = true
  error.value = ''
  msg.value = ''
  try {
    row.value = await marketGet<Upstream>(`${MARKET_API_PREFIX}/upstreams/${upstreamId.value}`)
    openTab(`/admin/market/upstreams/${upstreamId.value}`, {
      title:
        row.value?.name?.trim() ||
        row.value?.account_email?.trim() ||
        row.value?.seller_email?.trim() ||
        '上游详情',
    })
    syncSwitchFormFromRow()
    if (needsAuth.value) await loadVendors()
    if (needsModels.value) {
      await loadModels()
    } else {
      models.value = null
      selected.value = []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    row.value = null
  } finally {
    loading.value = false
  }
}

async function loadModels() {
  models.value = await marketGet<UpstreamModels>(
    `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/models`,
  )
  selected.value = [...(models.value.whitelist || [])]
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

async function startOAuth() {
  const body: Record<string, string> = {}
  if (projectID.value.trim()) body.project_id = projectID.value.trim()
  const st = await marketSend<{ auth_url?: string }>(
    'POST',
    `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/oauth/start`,
    Object.keys(body).length ? body : {},
  )
  authURL.value = st?.auth_url || ''
  if (authURL.value) {
    window.open(authURL.value, '_blank', 'noopener')
    msg.value = '已打开授权页，完成后粘贴回调地址或授权码'
  }
}

async function completeOAuth() {
  if (!oauthCode.value.trim()) {
    error.value = '请粘贴授权码或回调地址'
    return
  }
  await run('授权完成', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/oauth/complete`, {
      code: oauthCode.value.trim(),
    }),
  )
}

async function submitCodingKey() {
  if (!apiKey.value.trim() || apiKey.value.trim().length < 16) {
    error.value = '请填写有效 API Key'
    return
  }
  const body: Record<string, string> = { api_key: apiKey.value.trim() }
  if (organization.value.trim()) body.organization = organization.value.trim()
  if (project.value.trim()) body.project = project.value.trim()
  await run('Key 已接入', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/coding-key`, body),
  )
}

async function refreshModels() {
  await run('模型目录已刷新', async () => {
    models.value = await marketSend<UpstreamModels>(
      'POST',
      `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/models/refresh`,
    )
    selected.value = [...(models.value?.whitelist || selected.value)]
  })
}

function toggleModel(m: string) {
  const i = selected.value.indexOf(m)
  if (i >= 0) selected.value.splice(i, 1)
  else selected.value.push(m)
}

function addCustomModel() {
  const m = customModel.value.trim()
  if (!m) return
  if (!selected.value.includes(m)) selected.value.push(m)
  customModel.value = ''
}

async function saveModels() {
  if (!selected.value.length) {
    error.value = '请至少选择一个挂售模型'
    return
  }
  await run('挂售模型已保存', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/models`, {
      models: selected.value,
    }),
  )
}

const accountMissing = computed(
  () =>
    !!error.value &&
    (error.value.includes('引擎侧账号') || error.value.includes('ENGINE_ACCOUNT_MISSING')),
)

async function clearDeadAccount() {
  await run('已清除失效引擎账号，请重新授权', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/clear-dead-account`),
  )
}

async function switchChannel() {
  if (!switchPlatform.value || !switchAuthType.value) {
    error.value = '请选择厂商与接入方式'
    return
  }
  const same =
    row.value?.platform === switchPlatform.value && row.value?.auth_type === switchAuthType.value
  if (same) {
    msg.value = '渠道未变化'
    return
  }
  const ok = await run('已切换渠道，请重新授权', async () => {
    await marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${upstreamId.value}/switch-channel`, {
      platform: switchPlatform.value,
      auth_type: switchAuthType.value,
    })
    authURL.value = ''
    oauthCode.value = ''
    apiKey.value = ''
  })
  if (ok) await nextTick()
}

watch(upstreamId, () => {
  void load()
})

onMounted(() => {
  void load()
})
</script>

<template>
  <section id="market-upstream-detail-page" class="detail-shell">
    <header class="detail-header">
      <div class="page-heading">
        <h2 class="y-title">
          {{
            (row?.name || '').trim() ||
            (row?.account_email || '').trim() ||
            (row?.seller_email || '').trim() ||
            '未命名上游'
          }}
        </h2>
        <p class="y-page-desc">管理端可代卖家完成授权与选模型；完成后状态变为就绪即可挂单</p>
      </div>
      <div class="detail-header__actions">
        <button type="button" class="btn btn-secondary" @click="backToList">返回列表</button>
        <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="load">
          刷新
        </button>
      </div>
    </header>

    <div class="detail-body">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <div v-if="accountMissing && !loading" class="dead-account-banner">
        <p>引擎侧账号已失效（常见于引擎库重建后）。需清除绑定并重新 OAuth / 填 Key，才能继续选模型。</p>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="clearDeadAccount">
          清除失效账号并重新授权
        </button>
      </div>
      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="row">
        <div class="summary-card">
          <div>
            <span>编号</span>
            {{ row.code || '—' }}
          </div>
          <div>
            <span>名称</span>
            {{ (row.name || '').trim() || '—' }}
          </div>
          <div>
            <span>卖家</span>
            {{ row.seller_email?.trim() || '—' }}
          </div>
          <div><span>厂商</span>{{ row.platform }} · {{ row.auth_type || '—' }}</div>
          <div><span>状态</span>{{ statusLabel(row.status) }}</div>
          <div><span>邮箱</span>{{ row.account_email || '—' }}</div>
          <div><span>健康</span>{{ row.health || '—' }}</div>
          <div><span>等级</span>{{ row.grade || row.tier || '—' }}</div>
          <div class="summary-note">
            <span>备注</span>
            {{ (row.note || '').trim() || '—' }}
          </div>
        </div>
        <p v-if="row.failure_reason" class="hint warn">失败原因：{{ row.failure_reason }}</p>

        <!-- 换渠道（仅未完成接入） -->
        <section v-if="needsAuth" class="edit-panel">
          <h3>切换渠道</h3>
          <p class="sub">
            草稿 / 授权中 / 失败时可更换厂商与接入方式；切换后需重新授权。已接入完成的上游请删除后新建。
          </p>
          <label class="field">
            <span>厂商</span>
            <Select
              v-model="switchPlatform"
              class="w-full"
              size="sm"
              :options="switchPlatformOptions"
            />
          </label>
          <label class="field">
            <span>接入方式</span>
            <Select
              v-model="switchAuthType"
              class="w-full"
              size="sm"
              :options="switchAuthOptions"
            />
          </label>
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="switchChannel">
            确认切换
          </button>
        </section>

        <!-- 授权 -->
        <section v-if="needsAuth" class="edit-panel">
          <h3>继续接入</h3>
          <p class="sub">当前为 {{ statusLabel(row.status) }}，需先完成授权才能选模型与挂单。</p>

          <template v-if="isOAuth">
            <label v-if="row.platform === 'gemini'" class="field">
              <span>GCP Project ID（可选）</span>
              <input v-model="projectID" class="input w-full" placeholder="付费版通常需要" />
            </label>
            <div class="actions-row">
              <button type="button" class="btn btn-primary" :disabled="busy" @click="startOAuth">
                打开授权页
              </button>
              <a
                v-if="authURL"
                class="y-link"
                :href="authURL"
                target="_blank"
                rel="noopener"
              >重新打开链接</a>
            </div>
            <label class="field">
              <span>授权码 / 回调地址</span>
              <textarea v-model="oauthCode" class="input w-full" rows="3" placeholder="授权完成后粘贴" />
            </label>
            <button type="button" class="btn btn-primary" :disabled="busy" @click="completeOAuth">
              提交授权
            </button>
          </template>

          <template v-else>
            <label class="field">
              <span>API Key</span>
              <input v-model="apiKey" class="input w-full" type="password" autocomplete="off" />
            </label>
            <label class="field">
              <span>Organization（智谱团队版可选）</span>
              <input v-model="organization" class="input w-full" />
            </label>
            <label class="field">
              <span>Project（智谱团队版可选）</span>
              <input v-model="project" class="input w-full" />
            </label>
            <button type="button" class="btn btn-primary" :disabled="busy" @click="submitCodingKey">
              提交 Key
            </button>
          </template>
        </section>

        <!-- 选模型 -->
        <section v-if="needsModels" class="edit-panel">
          <div class="panel-head">
            <h3>挂售模型</h3>
            <button type="button" class="btn btn-secondary" :disabled="busy" @click="refreshModels">
              刷新目录
            </button>
          </div>
          <p class="sub">
            <template v-if="row.status === 'selecting_models'">
              待选模型：勾选后保存，通常会进入就绪，即可挂单。
            </template>
            <template v-else>可调整白名单；保存后同步到引擎。</template>
          </p>

          <div v-if="!models?.catalog?.length" class="empty-models">
            目录为空。可点「刷新目录」，或下方手动填写模型 ID。
          </div>
          <div v-else class="model-grid">
            <label v-for="m in models.catalog" :key="m" class="model-item">
              <input type="checkbox" :checked="selected.includes(m)" @change="toggleModel(m)" />
              <span>{{ m }}</span>
            </label>
          </div>

          <div class="custom-row">
            <input
              v-model="customModel"
              class="input flex-1"
              placeholder="手动添加模型 ID"
              @keyup.enter="addCustomModel"
            />
            <button type="button" class="btn btn-secondary" @click="addCustomModel">添加</button>
          </div>
          <p v-if="selected.length" class="hint">已选 {{ selected.length }} 个</p>

          <div class="actions-row">
            <button type="button" class="btn btn-primary" :disabled="busy" @click="saveModels">
              保存模型
            </button>
            <button
              v-if="accountMissing"
              type="button"
              class="btn btn-secondary"
              :disabled="busy"
              @click="clearDeadAccount"
            >
              清除失效账号并重新授权
            </button>
          </div>
        </section>

        <section v-if="!needsAuth && !needsModels" class="edit-panel">
          <p class="sub">当前状态 {{ statusLabel(row.status) }}，无需在此编辑接入；可返回列表做检测 / 冻结。</p>
        </section>
      </template>
    </div>
  </section>
</template>

<style scoped>
.detail-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  gap: 12px;
}

.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
  flex-shrink: 0;
}

.page-heading {
  min-width: 0;
}

.y-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  line-height: 26px;
  color: #1f1f1f;
}

.y-page-desc {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 20px;
  color: #8c8c8c;
}

.detail-header__actions {
  display: flex;
  gap: 10px;
  flex-shrink: 0;
}

.detail-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 0 2px 24px;
}

.summary-card {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px 16px;
  padding: 14px 16px;
  margin-bottom: 14px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  font-size: 13px;
  color: #303133;
}

.summary-card span {
  display: block;
  margin-bottom: 2px;
  font-size: 12px;
  color: #8c8c8c;
}

.summary-note {
  grid-column: 1 / -1;
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 800px) {
  .summary-card {
    grid-template-columns: 1fr 1fr;
  }
}

.edit-panel {
  margin-bottom: 14px;
  padding: 16px 18px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
}

.edit-panel h3 {
  margin: 0 0 6px;
  font-size: 15px;
  font-weight: 600;
  color: #1f1f1f;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 4px;
}

.panel-head h3 {
  margin: 0;
}

.sub {
  margin: 0 0 14px;
  font-size: 13px;
  color: #666;
  line-height: 1.5;
}

.field {
  display: grid;
  gap: 6px;
  margin-bottom: 12px;
  font-size: 13px;
}

.field > span {
  color: #606266;
  font-weight: 500;
}

.actions-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
}

.dead-account-banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin: 0 0 12px;
  padding: 12px 14px;
  border: 1px solid #f0c2a0;
  border-radius: 8px;
  background: #fff7ed;
  color: #9a3412;
  font-size: 13px;
  line-height: 1.5;
}

.dead-account-banner p {
  margin: 0;
  flex: 1;
  min-width: 200px;
}

.model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 8px;
  margin-bottom: 12px;
  max-height: 320px;
  overflow-y: auto;
}

.model-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 10px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 8px;
  font-size: 12px;
  cursor: pointer;
  background: #fafafa;
}

.model-item input {
  margin-top: 2px;
  accent-color: var(--color-primary, #0555ce);
}

.empty-models {
  padding: 12px;
  margin-bottom: 12px;
  border-radius: 8px;
  background: #f5f7fa;
  font-size: 13px;
  color: #606266;
}

.custom-row {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.hint {
  margin: 0 0 8px;
  font-size: 12px;
  color: #8c8c8c;
}

.hint.warn {
  color: #b45309;
}

.flex-1 {
  flex: 1;
  min-width: 0;
}
</style>
