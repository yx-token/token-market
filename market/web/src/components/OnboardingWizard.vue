<script setup lang="ts">
// 接入上游：选择接入方式 → 完成账号接入 → 选择挂售模型 → 可挂单，检测由卖家主动发起。
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { market, seller } from '@/api'
import type { Proxy, Upstream, VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import ModalDialog from '@/components/ModalDialog.vue'
import ModelWhitelistSelector from '@/components/ModelWhitelistSelector.vue'
import SellerOAuthPanel from '@/components/SellerOAuthPanel.vue'
import { authTypeText, isKeyAuthType, label } from '@/utils/labels'
import { authGuides, takesTeam } from '@/utils/authGuides'
import { useToast } from '@/stores/toast'
import { accountImportChoices } from '@/utils/accountImport'

const props = defineProps<{ vendors: VendorView[]; resume?: Upstream | null }>()
const emit = defineEmits<{ done: [] }>()
const open = defineModel<boolean>('open', { default: false })
const router = useRouter()
const { busy, run } = useAction()
const toast = useToast()

const step = ref<'choose' | 'authorize' | 'models' | 'done'>('choose')
const selectedModels = ref<string[]>([])
const modelsValid = ref(false)
const platform = ref('')
const authType = ref('')
const upstream = ref<Upstream | null>(null)
const entryMode = ref<'authorize' | 'import'>('authorize')
const importText = ref('')
const importIndex = ref(0)
const importPreview = computed(() => {
  if (!importText.value.trim()) return { data: undefined, choices: [], error: '' }
  try {
    if (new TextEncoder().encode(importText.value).length > 900000) throw new Error('导入内容过大，请使用小于 900 KB 的文件')
    const data: unknown = JSON.parse(importText.value)
    return { data, choices: accountImportChoices(data), error: '' }
  } catch (e) {
    return { data: undefined, choices: [], error: e instanceof SyntaxError ? '请输入有效的 JSON 账号数据' : (e as Error).message }
  }
})
watch(importText, () => { importIndex.value = 0 })
const selectedImport = computed(() => importPreview.value.choices[importIndex.value])
const importMismatch = computed(() => {
  const a = selectedImport.value
  const expected = isOAuth.value ? (currentType.value === 'setup-token' ? 'setup-token' : 'oauth') : 'apikey'
  return !!a && ((!!a.platform && a.platform !== platform.value) || (!!a.type && a.type !== expected))
})
// 上游未绑定代理时由导入文件带入代理；必须使用卖家代理时，所选账号在文件中必须有代理。
const pendingProxy = computed(() => !!upstream.value && !upstream.value.proxy_id)
const importOnly = computed(() => pendingProxy.value && requireSellerProxy.value)
const mode = computed(() => (importOnly.value ? 'import' : entryMode.value))
const importProxyBlocked = computed(() => {
  const a = selectedImport.value
  return pendingProxy.value && !!a && (!!a.proxyMissing || (importOnly.value && !a.proxy))
})
const canImport = computed(() => !!selectedImport.value && !importPreview.value.error && !importMismatch.value && !importProxyBlocked.value)
const proxyAddress = (p: { protocol: string; host: string; port: number }) => `${p.protocol}://${p.host.includes(':') ? `[${p.host}]` : p.host}:${p.port}`

async function readImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    if (file.size > 900000) throw new Error('导入文件需小于 900 KB')
    importText.value = await file.text()
  } catch (e) { toast.error(e) }
  finally { input.value = '' }
}
const apiKey = ref('')
const team = ref({ organization: '', project: '' })
const proxies = ref<Proxy[]>([])
// 'import' 表示暂不绑定代理，导入账号时使用导出文件中的代理。
const proxyID = ref<number | '' | 'import'>('')
const requireSellerProxy = ref(true)
const loadingProxies = ref(false)
const proxyLoadError = ref(false)
const idleProxies = computed(() => proxies.value.filter((p) => p.status === 'available' && !p.assigned_upstream_id && p.sub2api_proxy_id))
// 只有最近一次连通性检测成功的代理可以选择，后端同样校验。
const availableProxies = computed(() => idleProxies.value.filter((p) => p.connection_report?.success))
const untestedCount = computed(() => idleProxies.value.length - availableProxies.value.length)

async function loadProxyChoices() {
  loadingProxies.value = true
  proxyLoadError.value = false
  try {
    const [list, config] = await Promise.all([seller.proxies(), market.config()])
    proxies.value = list ?? []
    requireSellerProxy.value = config.require_seller_proxy
    if (proxyID.value !== 'import' && !availableProxies.value.some((p) => p.id === proxyID.value)) proxyID.value = ''
  } catch (e) {
    proxyLoadError.value = true
    toast.error(e)
  } finally {
    loadingProxies.value = false
  }
}

const vendor = computed(() => props.vendors.find((v) => v.platform === platform.value))
const currentType = computed(() => upstream.value?.auth_type ?? authType.value)
const isOAuth = computed(() => !isKeyAuthType(currentType.value))
const guide = computed(() => authGuides[`${platform.value}:${currentType.value}`])
const withTeam = computed(() => takesTeam(platform.value, currentType.value))

watch(open, (v) => {
  if (!v) return
  entryMode.value = 'authorize'
  importText.value = ''
  importIndex.value = 0
  apiKey.value = ''
  team.value = { organization: '', project: '' }
  selectedModels.value = []
  modelsValid.value = false
  if (props.resume) {
    upstream.value = props.resume
    platform.value = props.resume.platform
    authType.value = props.resume.auth_type
    step.value = props.resume.status === 'selecting_models' ? 'models' : ['draft', 'authorizing', 'failed'].includes(props.resume.status) ? 'authorize' : 'done'
    // 读取代理策略，判断未绑定代理的上游是否只能通过导入接入。
    if (step.value === 'authorize') void loadProxyChoices()
  } else {
    proxyID.value = ''
    void loadProxyChoices()
    upstream.value = null
    platform.value = props.vendors[0]?.platform ?? ''
    authType.value = props.vendors[0]?.auth_types[0] ?? ''
    step.value = 'choose'
  }
})

// 换厂商时选中它的第一种接入方式
function pick(v: VendorView) {
  platform.value = v.platform
  authType.value = v.auth_types[0] ?? ''
}

async function create() {
  const fromImport = proxyID.value === 'import'
  const selected = typeof proxyID.value === 'number' ? proxyID.value : undefined
  const u = await run(() => fromImport
    ? seller.createUpstream(platform.value, authType.value, undefined, true)
    : seller.createUpstream(platform.value, authType.value, selected))
  if (!u) return
  upstream.value = u
  if (fromImport) entryMode.value = 'import'
  step.value = 'authorize'
  emit('done')
}

function authorized(u: Upstream) {
  upstream.value = u
  step.value = u.status === 'selecting_models' ? 'models' : 'done'
  emit('done')
}

async function submit() {
  if (!upstream.value) return
  const id = upstream.value.id
  const body = {
    api_key: apiKey.value.trim(),
    ...(withTeam.value && team.value.organization.trim()
      ? { organization: team.value.organization.trim(), project: team.value.project.trim() || undefined }
      : {}),
  }
  const importing = mode.value === 'import'
  const u = await run(() => importing
    ? seller.importAccount(id, importPreview.value.data, importIndex.value)
    : seller.submitCodingKey(id, body))
  if (u) {
    authorized(u)
    importText.value = ''
  } else if (importing && pendingProxy.value) {
    // 代理检测通过后账号创建仍可能失败，此时代理已绑定，刷新以显示正确的代理提示。
    seller.upstream(id).then((d) => { if (upstream.value?.id === id) upstream.value = d }).catch(() => {})
  }
}

async function submitModels() {
  if (!upstream.value || !modelsValid.value) return
  const u = await run(() => seller.setUpstreamModels(upstream.value!.id, [...selectedModels.value]))
  if (!u) return
  upstream.value = u
  step.value = 'done'
  emit('done')
}

function goDetail() {
  open.value = false
  if (upstream.value) router.push(`/seller/upstreams/${upstream.value.id}`)
}

function goPublish() {
  open.value = false
  if (upstream.value?.status === 'ready') router.push({ path: '/seller/publish', query: { upstream: upstream.value.id } })
}

async function startVerification() {
  if (!upstream.value || !['ready', 'verify_failed'].includes(upstream.value.status)) return
  const u = await run(() => seller.retryVerification(upstream.value!.id))
  if (!u) return
  upstream.value = u
  emit('done')
}
</script>

<template>
  <ModalDialog v-model:open="open" title="接入上游" wide>
    <template v-if="step === 'choose'">
      <p class="text-sm text-ink-500">只接受官方订阅账号（OAuth 授权）与国产 Coding Plan 订阅 key。每个账号独占一个代理 IP，账号凭证由底层网关保管。</p>
      <div v-if="vendors.length === 0" class="callout callout-warn">平台暂未开放任何厂商接入。</div>
      <div>
        <div class="label">厂商</div>
        <div class="grid grid-cols-2 gap-2 md:grid-cols-3">
          <button v-for="v in vendors" :key="v.platform" type="button" :class="['choice', platform === v.platform && 'choice-on']" @click="pick(v)">
            <div class="font-medium">{{ label('platform', v.platform).text }}</div>
            <div class="text-xs text-ink-500">寄售倍率 {{ v.consignment_min }}–{{ v.consignment_max }}×</div>
          </button>
        </div>
      </div>
      <div v-if="vendor">
        <div class="label">接入方式</div>
        <div class="grid gap-2 sm:grid-cols-2">
          <label
            v-for="t in vendor.auth_types"
            :key="t"
            :class="['choice flex cursor-pointer items-start gap-2.5', authType === t && 'choice-on']"
          >
            <input v-model="authType" type="radio" :value="t" class="mt-0.5" :aria-label="authTypeText(platform, t)" />
            <span class="min-w-0">
              <span class="font-medium">{{ authTypeText(platform, t) }}</span>
              <span class="block text-xs leading-relaxed text-ink-500">{{ authGuides[`${platform}:${t}`]?.summary }}</span>
            </span>
          </label>
        </div>
      </div>
      <div>
        <label class="label" for="onboarding-proxy">代理 IP{{ requireSellerProxy ? '（必选）' : '（可由平台关联）' }}</label>
        <select id="onboarding-proxy" v-model="proxyID" class="input" :disabled="loadingProxies || proxyLoadError">
          <option value="">{{ requireSellerProxy ? '请选择连通性检测通过的空闲代理' : '由平台自动关联专用代理' }}</option>
          <option value="import">使用 sub2api 导入文件中的代理（导入时检测连通性）</option>
          <option v-for="p in availableProxies" :key="p.id" :value="p.id">{{ p.name }} · {{ proxyAddress(p) }}{{ p.region ? ` · ${p.region}` : '' }}</option>
        </select>
        <p v-if="loadingProxies" class="hint" role="status">正在加载代理列表…</p>
        <p v-else-if="proxyLoadError" class="hint text-red-600" role="alert">代理列表加载失败，请重试。</p>
        <p v-else-if="proxyID === 'import'" class="hint">接入时只能导入 sub2api 账号：导入前会登记并检测文件中该账号的代理，连通性检测通过才会导入。</p>
        <p v-else-if="availableProxies.length === 0" class="hint">暂无连通性检测通过的空闲代理，请先添加代理并在列表中检测连接，或使用导入文件中的代理。</p>
        <p v-else class="hint">仅显示你自己配置、连通性检测通过的空闲代理，绑定后其他账号无法选择。</p>
        <p v-if="!loadingProxies && !proxyLoadError && untestedCount > 0" class="hint">另有 {{ untestedCount }} 个空闲代理尚未检测或检测未通过，在代理列表检测连接成功后才能选择。</p>
        <div class="mt-2 flex items-center gap-3 text-sm">
          <RouterLink to="/seller/proxies" class="link" @click="open = false">配置代理 IP 列表</RouterLink>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="loadingProxies || busy" @click="loadProxyChoices">刷新列表</button>
        </div>
      </div>
    </template>

    <template v-else-if="step === 'authorize'">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-dashed border-ink-200 pb-3">
        <span class="font-mono text-xs text-ink-500">上游 #{{ upstream?.id }}</span>
        <span class="font-display text-[17px] font-bold">{{ label('platform', platform).text }}</span>
        <span class="text-sm text-ink-600">{{ authTypeText(platform, currentType) }}</span>
      </div>
      <p v-if="guide" class="text-sm text-ink-600">{{ guide.summary }}</p>

      <div class="flex gap-2" role="group" aria-label="账号接入方式">
        <button type="button" :class="['choice', mode === 'authorize' && 'choice-on']" :disabled="busy || importOnly" :title="importOnly ? '本上游未绑定代理，只能导入带代理的 sub2api 账号' : undefined" @click="entryMode = 'authorize'">{{ isOAuth ? '官方授权' : '填写 API Key' }}</button>
        <button type="button" :class="['choice', mode === 'import' && 'choice-on']" :disabled="busy" @click="entryMode = 'import'">导入 sub2api 账号</button>
      </div>

      <template v-if="mode === 'import'">
        <p class="text-sm text-ink-600">只有授权信息也可以接入，无需账号密码。支持 sub2api 导出的 JSON 文件、账号数组、单个账号或 credentials 对象；多账号文件每次选择一个账号导入。</p>
        <div>
          <label class="label" for="account-import-file">选择 JSON 文件</label>
          <input id="account-import-file" type="file" accept=".json,application/json" class="input" :disabled="busy" @change="readImportFile" />
        </div>
        <div>
          <label class="label" for="account-import-json">或粘贴账号 JSON</label>
          <textarea id="account-import-json" v-model="importText" class="input font-mono text-xs" rows="6" :disabled="busy" spellcheck="false" autocomplete="off" placeholder='{"platform":"anthropic","type":"oauth","credentials":{"access_token":"…","refresh_token":"…"}}' />
          <p v-if="importPreview.error" class="hint text-red-600" role="alert">{{ importPreview.error }}</p>
        </div>
        <div v-if="importPreview.choices.length">
          <label class="label" for="account-import-selection">本次导入的账号（共 {{ importPreview.choices.length }} 个）</label>
          <select id="account-import-selection" v-model="importIndex" class="input" :disabled="busy">
            <option v-for="(a, i) in importPreview.choices" :key="i" :value="i">{{ a.name }}{{ a.platform ? ` · ${label('platform', a.platform).text}` : '' }}{{ a.type ? ` · ${a.type}` : '' }}</option>
          </select>
          <p v-if="importMismatch" class="hint text-red-600" role="alert">所选账号的厂商或接入方式与当前上游不一致，请选择匹配的账号。</p>
        </div>
        <template v-if="pendingProxy && selectedImport">
          <p v-if="selectedImport.proxy" class="hint">将使用导入文件中该账号的代理 {{ selectedImport.proxy.name ? `${selectedImport.proxy.name} · ` : '' }}{{ proxyAddress(selectedImport.proxy) }}：导入前登记到你的代理列表并检测连通性，检测通过才会绑定并导入。</p>
          <p v-else-if="selectedImport.proxyMissing" class="hint text-red-600" role="alert">导入文件中缺少该账号引用的代理，请导出时一并包含代理。</p>
          <p v-else-if="importOnly" class="hint text-red-600" role="alert">所选账号在导入文件中没有代理，无法导入。请选择带代理的账号，或重新接入并选择已配置的代理。</p>
          <p v-else class="hint">所选账号在导入文件中没有代理，将按平台设置接入。</p>
        </template>
        <p class="hint">{{ pendingProxy ? '' : '导入账号沿用本上游已绑定的代理，导出文件中的代理不会被使用。' }}分组和调度配置不会被导入；导入后仍需通过隐私设置检查并选择挂售模型，Coding Plan 会校验官方订阅额度。</p>
      </template>

      <SellerOAuthPanel v-else-if="isOAuth && open && upstream" :upstream-id="upstream.id" :platform="platform" :auth-type="currentType" :disabled="busy" @ready="authorized" />

      <template v-else>
        <dl v-if="guide" class="kv">
          <template v-if="guide.how">
            <dt>获取方式</dt>
            <dd>{{ guide.how }}</dd>
          </template>
          <template v-if="guide.endpoint">
            <dt>官方地址</dt>
            <dd class="font-mono text-[12.5px]">{{ guide.endpoint }}</dd>
          </template>
        </dl>
        <p class="text-sm text-ink-600">平台会向官方查询订阅窗口用量来确认这是订阅 key（不接受按量付费 key 或中转 key），请求固定发往上面的官方地址。</p>
        <div>
          <label class="label" for="key">API Key</label>
          <input id="key" v-model="apiKey" class="input font-mono" autocomplete="off" />
        </div>
        <details v-if="withTeam" class="border border-ink-200 bg-paper/60 p-3">
          <summary class="cursor-pointer text-sm font-medium text-ink-700">团队版（个人版不用填）</summary>
          <div class="mt-3 grid gap-3 sm:grid-cols-2">
            <div>
              <label class="label" for="org">组织 ID</label>
              <input id="org" v-model="team.organization" class="input font-mono" placeholder="org-…" autocomplete="off" />
            </div>
            <div>
              <label class="label" for="proj">项目 ID（可选）</label>
              <input id="proj" v-model="team.project" class="input font-mono" placeholder="proj_…" autocomplete="off" />
            </div>
          </div>
          <p class="hint">
            团队版 Coding Plan 要带上组织 ID 才能查询额度。用团队版账号登录 bigmodel.cn 的团队版套餐页面，在浏览器开发者工具的网络请求中找到 /api/biz/v1/organization，地址里 org- 开头的是组织 ID，proj_ 开头的是项目 ID。
          </p>
        </details>
      </template>
    </template>

    <template v-else-if="step === 'models'">
      <h2 class="font-display text-lg font-bold">选择挂售模型</h2>
      <p class="text-sm text-ink-600">账号接入已完成。至少选择一个挂售模型并保存后即可发布商品，无需先运行质量检测。未检测状态会向买家明确展示。</p>
      <ModelWhitelistSelector v-if="upstream" v-model="selectedModels" :upstream-id="upstream.id" :disabled="busy" @valid="modelsValid = $event" />
    </template>

    <template v-else>
      <div v-if="upstream?.status === 'ready'" class="callout callout-ok">
        授权与模型白名单已保存，可以直接发布挂单。尚未运行质量检测时，商品与模型会标注“未检测”。你也可以主动发起检测。
      </div>
      <div v-else-if="upstream?.status === 'verifying'" class="callout callout-ok">
        授权完成，正在进行上架检测（通常几分钟内完成）。检测通过后就可以挂单了，检测报告可以在上游详情中查看。
      </div>
      <div v-else class="callout callout-warn">
        当前状态：{{ label('upstreamStatus', upstream?.status).text }}。{{ upstream?.failure_reason ?? '' }}
      </div>
    </template>

    <template #footer>
      <button type="button" class="xm-btn xm-btn-ghost" @click="open = false">关闭</button>
      <button v-if="step === 'choose'" type="button" class="xm-btn xm-btn-primary" :disabled="busy || loadingProxies || proxyLoadError || !platform || !authType || (requireSellerProxy && !proxyID)" @click="create">下一步</button>
      <button v-else-if="step === 'authorize' && (mode === 'import' || !isOAuth)" type="button" class="xm-btn xm-btn-primary" :disabled="busy || (mode === 'import' ? !canImport : !apiKey.trim())" @click="submit">{{ mode === 'import' ? '导入账号' : '提交' }}</button>
      <button v-else-if="step === 'models'" type="button" class="xm-btn xm-btn-primary" :disabled="busy || !modelsValid" @click="submitModels">保存挂售模型</button>
      <template v-else>
        <button v-if="upstream?.status === 'ready' || upstream?.status === 'verify_failed'" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="startVerification">主动发起检测</button>
        <button type="button" class="xm-btn xm-btn-ghost" @click="goDetail">查看上游</button>
        <button v-if="upstream?.status === 'ready'" type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="goPublish">去发布挂单</button>
      </template>
    </template>
  </ModalDialog>
</template>
