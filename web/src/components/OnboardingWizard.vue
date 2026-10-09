<script setup lang="ts">
// 接入上游：选择厂商与接入方式 → OAuth 授权或提交 Coding Plan key → 进入上架检测。
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { market, seller } from '@/api'
import type { Proxy, Upstream, VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import ModalDialog from '@/components/ModalDialog.vue'
import { authTypeText, isKeyAuthType, label } from '@/utils/labels'
import { authGuides, takesProjectID, takesTeam } from '@/utils/authGuides'
import { useToast } from '@/stores/toast'

const props = defineProps<{ vendors: VendorView[]; resume?: Upstream | null }>()
const emit = defineEmits<{ done: [] }>()
const open = defineModel<boolean>('open', { default: false })
const router = useRouter()
const { busy, run } = useAction()
const toast = useToast()

const step = ref<'choose' | 'authorize' | 'done'>('choose')
const platform = ref('')
const authType = ref('')
const upstream = ref<Upstream | null>(null)
const authURL = ref('')
const code = ref('')
const projectID = ref('')
const apiKey = ref('')
const team = ref({ organization: '', project: '' })
const proxies = ref<Proxy[]>([])
const proxyID = ref<number | ''>('')
const requireSellerProxy = ref(true)
const loadingProxies = ref(false)
const proxyLoadError = ref(false)
const availableProxies = computed(() => proxies.value.filter((p) => p.status === 'available' && !p.assigned_upstream_id && p.sub2api_proxy_id))

async function loadProxyChoices() {
  loadingProxies.value = true
  proxyLoadError.value = false
  try {
    const [list, config] = await Promise.all([seller.proxies(), market.config()])
    proxies.value = list ?? []
    requireSellerProxy.value = config.require_seller_proxy
    if (!availableProxies.value.some((p) => p.id === proxyID.value)) proxyID.value = ''
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
const withProject = computed(() => takesProjectID(platform.value, currentType.value))
const withTeam = computed(() => takesTeam(platform.value, currentType.value))

watch(open, (v) => {
  if (!v) return
  code.value = ''
  apiKey.value = ''
  authURL.value = ''
  projectID.value = ''
  team.value = { organization: '', project: '' }
  if (props.resume) {
    upstream.value = props.resume
    platform.value = props.resume.platform
    authType.value = props.resume.auth_type
    step.value = 'authorize'
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
  const u = await run(() => seller.createUpstream(platform.value, authType.value, proxyID.value || undefined))
  if (!u) return
  upstream.value = u
  step.value = 'authorize'
  emit('done')
}

async function getLink() {
  if (!upstream.value) return
  const opts = withProject.value && projectID.value.trim() ? { project_id: projectID.value.trim() } : undefined
  const res = await run(() => seller.startOAuth(upstream.value!.id, opts))
  if (res) {
    authURL.value = res.auth_url
    window.open(res.auth_url, '_blank', 'noopener')
  }
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
  const u = await run(() => (isOAuth.value ? seller.completeOAuth(id, code.value.trim()) : seller.submitCodingKey(id, body)))
  if (u) {
    upstream.value = u
    step.value = 'done'
    emit('done')
  }
}

function goDetail() {
  open.value = false
  if (upstream.value) router.push(`/seller/upstreams/${upstream.value.id}`)
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
          <option value="">{{ requireSellerProxy ? '请选择已配置的空闲代理' : '由平台自动关联专用代理' }}</option>
          <option v-for="p in availableProxies" :key="p.id" :value="p.id">{{ p.name }} · {{ p.protocol }}://{{ p.host.includes(':') ? `[${p.host}]` : p.host }}:{{ p.port }}{{ p.region ? ` · ${p.region}` : '' }}</option>
        </select>
        <p v-if="loadingProxies" class="hint" role="status">正在加载代理列表…</p>
        <p v-else-if="proxyLoadError" class="hint text-red-600" role="alert">代理列表加载失败，请重试。</p>
        <p v-else-if="availableProxies.length === 0" class="hint">暂无空闲且已就绪的代理，请先添加代理或在列表中重试连接。</p>
        <p v-else class="hint">仅显示你自己配置的空闲代理，绑定后其他账号无法选择。</p>
        <div class="mt-2 flex items-center gap-3 text-sm">
          <RouterLink to="/seller/proxies" class="link" @click="open = false">配置代理 IP 列表</RouterLink>
          <button class="btn-ghost btn-sm" :disabled="loadingProxies || busy" @click="loadProxyChoices">刷新列表</button>
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

      <template v-if="isOAuth">
        <div v-if="withProject">
          <label class="label" for="project">GCP 项目 ID（可选）</label>
          <input id="project" v-model="projectID" class="input font-mono" placeholder="例如 my-project-123456" autocomplete="off" />
          <p class="hint">{{ guide?.how }}修改后需要重新获取授权链接。</p>
        </div>
        <ol class="list-decimal space-y-2 pl-5 text-sm text-ink-600">
          <li>点击下面的按钮打开官方授权页面，用要挂出的账号登录并同意授权。</li>
          <li>授权完成后，把页面显示的授权码，或浏览器地址栏中的完整回调地址粘贴到下面。</li>
        </ol>
        <div class="flex flex-wrap items-center gap-2">
          <button class="btn-secondary" :disabled="busy" @click="getLink">{{ authURL ? '重新获取授权链接' : '获取授权链接' }}</button>
          <a v-if="authURL" :href="authURL" target="_blank" rel="noopener" class="link break-all text-xs">{{ authURL }}</a>
        </div>
        <div>
          <label class="label" for="code">授权码或回调地址</label>
          <textarea id="code" v-model="code" class="input" rows="3" placeholder="code#state 或 https://…?code=…&state=…" />
        </div>
      </template>

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

    <template v-else>
      <div v-if="upstream?.status === 'verifying'" class="callout callout-ok">
        授权完成，正在进行上架检测（通常几分钟内完成）。检测通过后就可以挂单了，检测报告可以在上游详情中查看。
      </div>
      <div v-else class="callout callout-warn">
        当前状态：{{ label('upstreamStatus', upstream?.status).text }}。{{ upstream?.failure_reason ?? '' }}
      </div>
    </template>

    <template #footer>
      <button class="btn-secondary" @click="open = false">关闭</button>
      <button v-if="step === 'choose'" class="btn-primary" :disabled="busy || loadingProxies || proxyLoadError || !platform || !authType || (requireSellerProxy && !proxyID)" @click="create">下一步</button>
      <button v-else-if="step === 'authorize'" class="btn-primary" :disabled="busy || (isOAuth ? !code.trim() : !apiKey.trim())" @click="submit">提交</button>
      <button v-else class="btn-primary" @click="goDetail">查看上游</button>
    </template>
  </ModalDialog>
</template>
