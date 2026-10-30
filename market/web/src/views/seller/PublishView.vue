<script setup lang="ts">
/**
 * 发布闲置TOKEN：对齐 docs/prototypes/seller-publish.html
 * 代理 → 接入 → 选择模型 → 检测 → 商品形态 → 定价上架 → 完成（全部走真实 API）
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { market, seller } from '@/api'
import type { Listing, ListingCategoryOption, Proxy, Quote, Upstream, VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import { authGuides, takesTeam } from '@/utils/authGuides'
import { authTypeText, isKeyAuthType, label } from '@/utils/labels'
import { multiplier, percent } from '@/utils/format'
import InspectionAppraisal from '@/components/InspectionAppraisal.vue'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'
import ModelWhitelistSelector from '@/components/ModelWhitelistSelector.vue'
import SellerOAuthPanel from '@/components/SellerOAuthPanel.vue'
import { privacyBlocksListing, sellerUntested } from '@/components/seller/listingGate'
import { assignedUpstreamCount, canSelectSellerProxy } from '@/utils/proxyBindings'
import '@/styles/seller-center.css'

const route = useRoute()
const cfg = useConfig()
const toast = useToast()
const { busy, run } = useAction()

const step = ref(0)
const selectedModels = ref<string[]>([])
const modelsValid = ref(false)
const steps = ['代理', '接入', '模型', '检测', '商品', '定价', '完成'] as const

const vendors = ref<VendorView[]>([])
const proxies = ref<Proxy[]>([])
const requireSellerProxy = ref(true)
const proxyID = ref<number | ''>('')
const readyUpstreams = ref<Upstream[]>([])
const resumeUpstreamID = ref<number | ''>('')

const platform = ref('')
const authType = ref('')
const upstream = ref<Upstream | null>(null)
const apiKey = ref('')
const team = ref({ organization: '', project: '' })
const reserve = ref(20)

const sku = ref<'consignment' | 'pack' | 'buyback'>('consignment')
const mult = ref('0.55')
const minAmount = ref('0')
const joinPool = ref(true)
const faceValue = ref('100')
const stock = ref('3')
const title = ref('')
const description = ref('')
const categories = ref<string[]>([])
const categoryOpts = ref<ListingCategoryOption[]>([])
const quote = ref<Quote | null>(null)
const listing = ref<Listing | null>(null)

const availableProxies = computed(() =>
  proxies.value.filter((p) => canSelectSellerProxy(p) && p.connection_report?.success),
)
const vendor = computed(() => vendors.value.find((v) => v.platform === platform.value))
const isOAuth = computed(() => !isKeyAuthType(authType.value))
const withTeam = computed(() => takesTeam(platform.value, authType.value))
const guide = computed(() => authGuides[`${platform.value}:${authType.value}`])
const buyerPrice = computed(() => {
  const m = Number(mult.value)
  return m > 0 ? m * (1 + cfg.config.commission_rate) : null
})

let pollTimer: ReturnType<typeof setInterval> | null = null

onMounted(async () => {
  await cfg.ensure()
  const [vs, ps, conf, us, ls, cats] = await Promise.all([
    seller.vendors().catch(() => null),
    seller.proxies().catch(() => null),
    market.config().catch(() => null),
    seller.upstreams().catch(() => null),
    seller.listings().catch(() => null),
    seller.listingCategories().catch(() => null),
  ])
  vendors.value = vs ?? []
  proxies.value = ps ?? []
  requireSellerProxy.value = conf?.require_seller_proxy ?? true
  categoryOpts.value = cats ?? []
  categories.value = []
  const busyUpstream = new Set(
    (ls ?? []).filter((l) => l.status !== 'closed').map((l) => l.upstream_id),
  )
  readyUpstreams.value = (us ?? []).filter((u) => u.status === 'ready' && !busyUpstream.has(u.id))
  platform.value = vendors.value[0]?.platform ?? ''
  authType.value = vendors.value[0]?.auth_types[0] ?? ''
  const resume = Number(route.query.upstream)
  if (resume) {
    const u = await seller.upstream(resume).catch(() => null)
    if (u && ['ready', 'selecting_models', 'verifying', 'verify_failed'].includes(u.status)) {
      upstream.value = u
      platform.value = u.platform
      authType.value = u.auth_type
      step.value = u.status === 'ready' ? 4 : u.status === 'selecting_models' ? 2 : 3
    }
  }
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})

watch(platform, () => {
  const v = vendors.value.find((x) => x.platform === platform.value)
  if (v) authType.value = v.auth_types[0] ?? ''
})

watch(step, (n) => {
  if (n === 3) startProbePoll()
  else if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (n === 5 && sku.value === 'buyback') loadQuote()
})

function pickVendor(v: VendorView) {
  platform.value = v.platform
  authType.value = v.auth_types[0] ?? ''
}

async function nextFromProxy() {
  if (resumeUpstreamID.value) {
    const u = await run(() => seller.upstream(Number(resumeUpstreamID.value)))
    if (!u) return
    if (u.status !== 'ready') {
      toast.error('该上游尚未就绪，请先完成授权、模型选择与隐私检查')
      return
    }
    upstream.value = u
    platform.value = u.platform
    authType.value = u.auth_type
    step.value = 4
    return
  }
  if (requireSellerProxy.value && !proxyID.value) {
    toast.error('请选择连接成功的卖家自有代理，或先去「代理 IP」登记并检测连接')
    return
  }
  step.value = 1
}

async function createAndAuthorize() {
  if (busy.value) return
  if (!platform.value || !authType.value) {
    toast.error('请选择厂商与接入方式')
    return
  }
  if (requireSellerProxy.value && !proxyID.value) {
    toast.error('请先选择代理')
    return
  }
  if (!isOAuth.value && !apiKey.value.trim()) {
    toast.error('请粘贴 Coding Plan Key')
    return
  }
  const done = await run(async () => {
    let u = upstream.value
    if (!u) {
      u = await seller.createUpstream(platform.value, authType.value, proxyID.value || undefined)
      upstream.value = u
    }
    if (u.platform !== platform.value || u.auth_type !== authType.value) throw new Error('接入方式与已创建的上游不一致，请重新开始接入。')
    if (isOAuth.value) return null
    const body = {
      api_key: apiKey.value.trim(),
      ...(withTeam.value && team.value.organization.trim()
        ? { organization: team.value.organization.trim(), project: team.value.project.trim() || undefined }
        : {}),
    }
    return seller.submitCodingKey(u.id, body)
  })
  if (done) await authorized(done)
}

async function authorized(u: Upstream) {
  upstream.value = u
  if (reserve.value >= 0) {
    await seller.setSelfReserve(u.id, Number(reserve.value)).catch(() => null)
  }
  toast.success(`接入成功，当前状态：${label('upstreamStatus', u.status).text}`)
  step.value = u.status === 'selecting_models' ? 2 : 3
}

async function submitModels(detect = false) {
  if (!upstream.value || !modelsValid.value) return
  const updated = await run(() => seller.setUpstreamModels(upstream.value!.id, [...selectedModels.value]))
  if (!updated) return
  upstream.value = updated
  if (privacyBlocksListing(updated)) {
    toast.error('隐私检查未通过，不能挂单')
    return
  }
  if (detect || updated.status === 'verifying') {
    if (detect && (updated.status === 'ready' || updated.status === 'listed')) {
      const started = await run(() => seller.retryVerification(updated.id), '已开始质量检测')
      if (started) upstream.value = started
    }
    step.value = 3
    return
  }
  if (updated.status === 'ready' || updated.status === 'listed') {
    step.value = 4
    return
  }
  step.value = updated.status === 'selecting_models' ? 2 : 3
}

async function startDetection() {
  if (!upstream.value) return
  const started = await run(() => seller.retryVerification(upstream.value!.id), '已开始质量检测')
  if (!started) return
  upstream.value = started
  step.value = 3
}

function startProbePoll() {
  if (pollTimer) clearInterval(pollTimer)
  const tick = async () => {
    if (!upstream.value) return
    const u = await seller.upstream(upstream.value.id).catch(() => null)
    if (u) upstream.value = u
  }
  void tick()
  pollTimer = setInterval(tick, 4000)
}

async function retryProbe() {
  if (!upstream.value) return
  const u = await run(() => seller.retryVerification(upstream.value!.id))
  if (u) {
    upstream.value = u
    if (u.status === 'selecting_models') step.value = 2
    else if (u.status === 'verifying') toast.success('已重新开始检测')
  }
}

async function loadQuote() {
  if (!upstream.value) return
  quote.value = await seller.quote(upstream.value.id).catch(() => null)
}

function toggleCategory(id: string) {
  const set = new Set(categories.value)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  categories.value = [...set]
}

async function publish() {
  if (!upstream.value) return
  if (privacyBlocked.value) {
    toast.error('隐私检查未通过，不能挂单')
    return
  }
  if (sku.value === 'buyback' && untested.value) {
    toast.error('平台回收必须先有有效检测')
    return
  }
  if (sku.value === 'consignment' || sku.value === 'pack') {
    if (!(Number(mult.value) > 0)) {
      toast.error('请填写倍率')
      return
    }
    if (!categories.value.length) {
      toast.error('请至少选择一个市场分类')
      return
    }
  }
  if (sku.value === 'pack') {
    if (!(Number(faceValue.value) >= 10)) {
      toast.error('订阅套餐售价至少 10 元')
      return
    }
    if (!(Number(stock.value) >= 1)) {
      toast.error('可售份数至少 1')
      return
    }
  }
  const body =
    sku.value === 'consignment'
      ? {
          mode: 'consignment',
          multiplier: Number(mult.value),
          join_default_pool: untested.value ? false : joinPool.value,
          min_amount: Number(minAmount.value) || 0,
          title: title.value.trim() || undefined,
          description: description.value.trim() || undefined,
          categories: [...categories.value],
        }
      : sku.value === 'pack'
        ? {
            mode: 'pack',
            multiplier: Number(mult.value),
            face_value: Number(faceValue.value),
            stock: Number(stock.value),
            title: title.value.trim() || undefined,
            description: description.value.trim() || undefined,
            categories: [...categories.value],
          }
        : {
            mode: 'buyback',
            title: title.value.trim() || undefined,
            description: description.value.trim() || undefined,
          }
  const res = await run(
    () => seller.createListing(upstream.value!.id, body),
    sku.value === 'buyback' ? '已生成回收报价' : sku.value === 'pack' ? '订阅套餐已上架' : '按量通道已上架',
  )
  if (!res) return
  listing.value = res.listing
  quote.value = res.quote ?? null
  if (sku.value === 'buyback' && res.listing.status === 'pending_confirm') {
    // 停留在完成页，提示去确认报价
  }
  step.value = 6
}

async function acceptBuyback() {
  if (!listing.value) return
  const l = await run(() => seller.acceptQuote(listing.value!.id), '已接受回收报价')
  if (l) listing.value = l
}

function resetWizard() {
  step.value = 0
  upstream.value = null
  listing.value = null
  quote.value = null
  apiKey.value = ''
  title.value = ''
  description.value = ''
  categories.value = []
  selectedModels.value = []
  modelsValid.value = false
  resumeUpstreamID.value = ''
}

const probeOk = computed(() => upstream.value?.status === 'ready')
const probeFail = computed(() => ['verify_failed', 'failed'].includes(upstream.value?.status ?? ''))
const untested = computed(() => sellerUntested(upstream.value))
const privacyBlocked = computed(() => privacyBlocksListing(upstream.value))
</script>

<template>
  <div class="xm-publish seller-page">
    <RouterLink
      :to="(resumeUpstreamID || upstream?.id) ? `/seller/upstreams/${resumeUpstreamID || upstream?.id}?from=publish` : '/seller/upstreams'"
      class="xm-page-back"
    >
      <span class="xm-page-back__ico" aria-hidden="true">←</span>
      {{ (resumeUpstreamID || upstream?.id) ? `上游 #${resumeUpstreamID || upstream?.id}` : '我的上游' }}
    </RouterLink>
    <h1 class="text-[28px] font-extrabold tracking-tight text-[#1f1f1f]">发布闲置TOKEN</h1>
    <p class="mt-2 mb-6 max-w-3xl text-[15px] leading-relaxed text-[#666]">
      授权订阅账号 → 选择挂售模型 → 可直接挂「按量通道」或「订阅套餐」。质量检测可选；未检测会标给买家，不能进保障池，也不能做平台回收。隐私检查不能跳过。
    </p>

    <ol class="xm-stepper" aria-label="发布步骤">
      <li
        v-for="(label, i) in steps"
        :key="label"
        class="xm-stepper__item"
        :class="{ 'is-on': step === i, 'is-done': step > i }"
        :aria-current="step === i ? 'step' : undefined"
      >
        <div class="xm-stepper__node">
          <span class="xm-stepper__dot" aria-hidden="true">
            <svg v-if="step > i" viewBox="0 0 16 16" width="14" height="14" fill="none">
              <path d="M3.5 8.5 6.5 11.5 12.5 4.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <template v-else>{{ i + 1 }}</template>
          </span>
          <span class="xm-stepper__label">{{ label }}</span>
        </div>
        <span v-if="i < steps.length - 1" class="xm-stepper__line" :class="{ 'is-done': step > i }" aria-hidden="true" />
      </li>
    </ol>

    <!-- 0 代理 -->
    <section v-if="step === 0" class="xm-pub-panel">
      <h2>配置出口代理</h2>
      <p class="sub">授权与调用走同一固定代理；卖家自有代理可同时绑定多个账号。</p>

      <div v-if="readyUpstreams.length" class="mb-4">
        <label class="xm-field-label">或从已就绪的上游继续挂单</label>
        <select v-model="resumeUpstreamID" class="xm-input">
          <option value="">— 新接入账号 —</option>
          <option v-for="u in readyUpstreams" :key="u.id" :value="u.id">
            #{{ u.id }} {{ label('platform', u.platform).text }}{{ u.account_email ? ` · ${u.account_email}` : '' }} · {{ u.tier || u.grade || u.status }}
          </option>
        </select>
      </div>

      <template v-if="!resumeUpstreamID">
        <label class="xm-field-label">选择代理{{ requireSellerProxy ? '（必选）' : '（可由平台关联）' }}</label>
        <select v-model="proxyID" class="xm-input" :disabled="!!upstream">
          <option value="">{{ requireSellerProxy ? '请选择连通性检测通过的代理' : '由平台自动关联' }}</option>
          <option v-for="p in availableProxies" :key="p.id" :value="p.id">
            {{ p.name }} · {{ p.protocol }}://{{ p.host.includes(':') ? `[${p.host}]` : p.host }}:{{ p.port }}{{ p.region ? ` · ${p.region}` : '' }} · 已绑定 {{ assignedUpstreamCount(p) }} 个账号
          </option>
        </select>
        <p v-if="availableProxies.length" class="xm-hint">
          仅显示你自己配置、已同步且连通性检测通过的代理，同一代理可绑定多个账号。平台代理由平台按账号独占关联。
        </p>
        <p v-else class="xm-hint">
          暂无可用代理，请先
          <RouterLink to="/seller/proxies" class="font-bold underline underline-offset-2">登记代理 IP 并检测连接</RouterLink>
        </p>
      </template>

      <div class="xm-pub-actions">
        <RouterLink to="/seller/upstreams" class="xm-btn xm-btn-ghost">取消</RouterLink>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="nextFromProxy">下一步</button>
      </div>
    </section>

    <!-- 1 接入 -->
    <section v-else-if="step === 1" class="xm-pub-panel">
      <h2>接入上游账号</h2>
      <p class="sub">只做授权，不交密码。选择 OAuth 可在官方页面登录订阅账号，无需 API Key；也可使用已有订阅 Key。</p>

      <label class="xm-field-label">厂商</label>
      <div class="mb-4 grid grid-cols-2 gap-2 md:grid-cols-3">
        <button
          v-for="v in vendors"
          :key="v.platform"
          type="button"
          class="xm-choice"
          :class="{ on: platform === v.platform }"
          :disabled="busy || !!upstream"
          @click="pickVendor(v)"
        >
          <strong>{{ label('platform', v.platform).text }}</strong>
          <span>{{ v.consignment_min }}–{{ v.consignment_max }}×</span>
        </button>
      </div>

      <label v-if="vendor" class="xm-field-label">接入方式</label>
      <div v-if="vendor" class="mb-4 grid gap-2 sm:grid-cols-2">
        <label
          v-for="t in vendor.auth_types"
          :key="t"
          class="xm-choice"
          :class="{ on: authType === t }"
        >
          <input v-model="authType" type="radio" :value="t" class="mt-0.5" :disabled="busy || !!upstream" />
          <div>
            <strong>{{ authTypeText(platform, t) }}</strong>
            <span>{{ authGuides[`${platform}:${t}`]?.summary }}</span>
          </div>
        </label>
      </div>

      <template v-if="isOAuth">
        <SellerOAuthPanel v-if="upstream" :upstream-id="upstream.id" :platform="platform" :auth-type="authType" :disabled="busy" @ready="authorized" />
        <template v-else>
          <p class="xm-hint">{{ guide?.how || '点击开始授权后，在官方页面登录要接入的订阅账号。' }}</p>
          <p v-if="guide?.guideURL" class="xm-hint mb-2">
            <a :href="guide.guideURL" target="_blank" rel="noopener noreferrer" class="link">{{ guide.guideText || '前往官方页面' }}</a>
          </p>
        </template>
      </template>
      <template v-else>
        <p class="xm-hint mb-2">{{ guide?.how || '粘贴官方 Coding Plan 订阅 Key，平台会校验窗口用量。' }}</p>
        <p v-if="guide?.guideURL" class="xm-hint mb-2">
          <a :href="guide.guideURL" target="_blank" rel="noopener noreferrer" class="link">{{ guide.guideText || '前往官方页面' }}</a>
        </p>
        <label class="xm-field-label">Coding Plan Key</label>
        <input v-model="apiKey" type="password" class="xm-input" autocomplete="off" />
        <div v-if="withTeam" class="mt-3 grid gap-3 sm:grid-cols-2">
          <div>
            <label class="xm-field-label">组织 ID</label>
            <input v-model="team.organization" class="xm-input" placeholder="org-…" />
          </div>
          <div>
            <label class="xm-field-label">项目 ID（可选）</label>
            <input v-model="team.project" class="xm-input" placeholder="proj_…" />
          </div>
        </div>
      </template>

      <label class="xm-field-label mt-3">自留比例（%）</label>
      <input v-model.number="reserve" type="number" min="0" max="80" class="xm-input" />
      <p class="xm-hint">窗口用量达到阈值即暂停调度。部分厂商可能不支持。</p>

      <div class="xm-pub-actions">
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = 0">上一步</button>
        <button v-if="!isOAuth || !upstream" type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="createAndAuthorize">{{ isOAuth ? '开始授权' : '完成接入，选择模型' }}</button>
      </div>
    </section>

    <!-- 2 选择挂售模型 -->
    <section v-else-if="step === 2" class="xm-pub-panel">
      <h2>选择挂售模型</h2>
      <p class="sub">账号接入已完成。选择可供买家调用的模型，可直接挂单，也可先做质量检测。</p>
      <ModelWhitelistSelector v-if="upstream" v-model="selectedModels" :upstream-id="upstream.id" :disabled="busy" @valid="modelsValid = $event" />
      <div class="xm-pub-actions">
        <RouterLink :to="`/seller/upstreams/${upstream?.id}`" class="xm-btn xm-btn-ghost">稍后继续</RouterLink>
        <button type="button" class="xm-btn xm-btn-ghost" :disabled="busy || !modelsValid" @click="submitModels(true)">确认模型并开始检测</button>
        <button type="button" class="xm-btn xm-btn-primary" data-testid="confirm-models-list" :disabled="busy || !modelsValid" @click="submitModels(false)">确认模型，直接挂单</button>
      </div>
    </section>

    <!-- 3 检测 -->
    <section v-else-if="step === 3" class="xm-pub-panel">
      <h2>上架检测</h2>
      <p class="sub">质量检测可选。未检测也可以挂寄售或额度包，买家会看到「未检测」。平台回收仍须有效检测。上游 #{{ upstream?.id }}{{ upstream?.account_email ? ` · ${upstream.account_email}` : '' }}</p>

      <div class="xm-probe" :class="{ ok: probeOk, fail: probeFail }">
        <div class="font-bold text-[13px]">
          <template v-if="probeOk">检测通过</template>
          <template v-else-if="probeFail">检测未通过</template>
          <template v-else>检测进行中…</template>
        </div>
        <div class="xm-probe-bar"><i :style="{ width: probeOk ? '100%' : probeFail ? '100%' : '55%' }" /></div>
        <div class="text-[12px] text-[#888]">
          状态：{{ label('upstreamStatus', upstream?.status ?? '').text }}
          <template v-if="upstream?.failure_reason"> · {{ upstream.failure_reason }}</template>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="xm-field-label">识别档位</label>
          <input class="xm-input" readonly :value="upstream?.tier || '—'" />
        </div>
        <div>
          <label class="xm-field-label">质量等级</label>
          <input class="xm-input" readonly :value="upstream?.grade || '—'" />
        </div>
      </div>

      <div class="xm-pub-actions">
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = 2">上一步</button>
        <button v-if="probeFail" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="retryProbe">重试检测</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="!probeOk || privacyBlocked" @click="step = 4">{{ untested ? '未检测，继续挂单' : '检测完成，继续挂单' }}</button>
      </div>
    </section>

    <!-- 4 商品形态 -->
    <section v-else-if="step === 4" class="xm-pub-panel">
      <h2>选择商品形态</h2>
      <p class="sub">同一上游同时只能有一条有效挂单；形态决定买家怎么下单。</p>
      <div v-if="privacyBlocked" class="seller-callout seller-callout-bad mb-3">隐私检查未通过，不能挂单。</div>
      <div v-else-if="untested" class="seller-callout seller-callout-warn mb-3" data-testid="untested-notice">尚未质量检测。寄售和额度包可以直接挂，买家会看到「未检测」。不能加入保障池，也不能选平台回收。</div>
      <MarketDetectionSummary v-if="upstream" :status="upstream.detection_status" :models="upstream.model_whitelist ?? []" :checks="upstream.model_checks" :last-checked-at="upstream.last_checked_at" />
      <div class="flex flex-col gap-2.5">
        <label class="xm-choice" :class="{ on: sku === 'consignment' }">
          <input v-model="sku" type="radio" value="consignment" />
          <div>
            <strong>A. 按量通道（寄售）</strong>
            <span>买家付最低额度 M（可为 0），之后按量扣余额。可在市场出售，检测合格后还可申请保障池。</span>
          </div>
        </label>
        <label class="xm-choice" :class="{ on: sku === 'pack' }">
          <input v-model="sku" type="radio" value="pack" />
          <div>
            <strong>B. 订阅套餐</strong>
            <span>买家付套餐价，获得周期内调用权与期内额度；到期或用尽即止。有可售份数。不进保障池。</span>
          </div>
        </label>
        <label class="xm-choice" :class="{ on: sku === 'buyback', 'seller-choice': untested }" :aria-disabled="untested">
          <input v-model="sku" type="radio" value="buyback" :disabled="untested" />
          <div>
            <strong>C. 平台回收（按量）</strong>
            <span>接受平台报价，不进市场目录，只进保障池。倍率由平台定。</span>
          </div>
        </label>
      </div>
      <div class="xm-pub-actions">
        <button v-if="untested && upstream" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="startDetection">先做质量检测</button>
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = upstream?.status === 'ready' && resumeUpstreamID ? 0 : 3">上一步</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="privacyBlocked" @click="step = 5">下一步</button>
      </div>
    </section>

    <!-- 5 定价 -->
    <section v-else-if="step === 5" class="xm-pub-panel">
      <h2>定价与上架内容</h2>
      <p class="sub">设置买家看到的价格与标题。</p>
      <InspectionAppraisal v-if="upstream && !untested" class="mb-4" :appraisal="upstream.appraisal" :grade="upstream.grade" :quality-factor="upstream.quality_factor" :inspection-mode="upstream.inspection_mode" :price="sku === 'buyback' ? undefined : Number(mult)" />

      <template v-if="sku === 'consignment'">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label class="xm-field-label">卖家倍率 m_s</label>
            <input v-model="mult" class="xm-input" inputmode="decimal" :placeholder="vendor ? `${vendor.consignment_min}–${vendor.consignment_max}` : ''" />
            <p class="xm-hint">
              买家价 ≈ m_s × (1 + {{ percent(cfg.config.commission_rate, 0) }})
              <template v-if="buyerPrice"> = {{ multiplier(buyerPrice) }}</template>
            </p>
          </div>
          <div>
            <label class="xm-field-label">最低额度 M（元）</label>
            <input v-model="minAmount" class="xm-input" inputmode="decimal" />
            <p class="xm-hint">0 = 免费开通，之后按量扣余额</p>
          </div>
        </div>
        <label class="mt-3 flex items-center gap-2 text-[13px] font-semibold">
          <input v-model="joinPool" type="checkbox" :disabled="untested" /> 同时加入保障池（倍率需足够低）<span v-if="untested">未检测不可加入</span>
        </label>
      </template>

      <template v-else-if="sku === 'pack'">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label class="xm-field-label">套餐售价（元）</label>
            <input v-model="faceValue" class="xm-input" inputmode="decimal" />
            <p class="xm-hint">买家一口价；当前亦作为期内额度口径（周期字段后续扩展）</p>
          </div>
          <div>
            <label class="xm-field-label">可售份数</label>
            <input v-model="stock" class="xm-input" inputmode="numeric" />
            <p class="xm-hint">同时生效的订阅数</p>
          </div>
        </div>
        <div class="mt-3">
          <label class="xm-field-label">期内计费倍率</label>
          <input v-model="mult" class="xm-input" inputmode="decimal" />
          <p class="xm-hint">订阅套餐不进保障池；调用按官方价 × 倍率抵扣期内额度。</p>
        </div>
      </template>

      <template v-else>
        <div class="rounded-[14px] bg-[#fafafa] p-4 text-[14px] leading-relaxed">
          <template v-if="quote">
            平台回收报价：<b>{{ multiplier(quote.multiplier) }}</b>（等级 {{ quote.grade }}）<br />
            有效期至 {{ quote.valid_until }}。接受后按实际用量结算，不进市场。
          </template>
          <template v-else>确认上架后将按质量等级生成回收报价，需你接受后生效。</template>
        </div>
      </template>

      <div v-if="sku === 'consignment' || sku === 'pack'" class="mt-4 rounded-[14px] border border-[#f0c000]/40 bg-[#fffbeb] p-4">
        <label class="xm-field-label !mb-1">市场分类（必选，至少选一个）</label>
        <p class="xm-hint !mt-0 !mb-3">决定市场分类与筛选归类；未选分类不能上架。</p>
        <div v-if="categoryOpts.length" class="flex flex-col gap-2">
          <label
            v-for="c in categoryOpts"
            :key="c.id"
            class="xm-choice"
            :class="{ on: categories.includes(c.id) }"
          >
            <input
              type="checkbox"
              :checked="categories.includes(c.id)"
              @change="toggleCategory(c.id)"
            />
            <div>
              <strong>{{ c.path || c.name }}</strong>
              <span>{{ c.desc || c.name }}</span>
            </div>
          </label>
        </div>
        <p v-else class="text-[13px] text-[#b45309]">暂无可选分类，请联系管理员在控制台开启「卖家可选」分类。</p>
        <p v-if="categories.length" class="mt-2 text-[12px] text-[#666]">已选 {{ categories.length }} 个</p>
      </div>

      <div class="mt-4">
        <label class="xm-field-label">展示标题（可选）</label>
        <input v-model="title" class="xm-input" placeholder="留空则自动生成：厂商 · 档位 · 形态" />
      </div>
      <div class="mt-3">
        <label class="xm-field-label">短描述（可选）</label>
        <textarea v-model="description" class="xm-input" rows="3" placeholder="给买家看的说明，不要写外链或账号信息" />
      </div>

      <div class="xm-pub-actions">
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = 4">上一步</button>
        <button
          type="button"
          class="xm-btn xm-btn-primary"
          :disabled="busy || ((sku === 'consignment' || sku === 'pack') && (!categories.length || !categoryOpts.length))"
          @click="publish"
        >
          确认上架
        </button>
      </div>
    </section>

    <!-- 6 完成 -->
    <section v-else class="xm-pub-panel text-center">
      <h2 class="!mb-2">已发布</h2>
      <p class="sub !mb-4">
        <template v-if="sku === 'pack'">订阅套餐已上架，买家可在市场订阅。</template>
        <template v-else-if="sku === 'buyback' && listing?.status === 'pending_confirm'">
          回收报价待确认。
          <button type="button" class="xm-btn xm-btn-primary xm-btn-sm ml-2" :disabled="busy" @click="acceptBuyback">接受报价</button>
        </template>
        <template v-else>按量通道已生效，可在市场被选用。</template>
      </p>
      <div class="rounded-[14px] bg-[#fafafa] p-4 text-left text-[14px] leading-relaxed">
        <div>上游 <b>#{{ upstream?.id }}</b><template v-if="upstream?.account_email">（{{ upstream.account_email }}）</template> · 挂单 <b>#{{ listing?.id }}</b></div>
        <div>
          形态：
          <b>{{ sku === 'pack' ? '订阅套餐' : sku === 'buyback' ? '平台回收' : '按量通道' }}</b>
        </div>
        <div v-if="sku === 'pack'">面额 ¥{{ faceValue }} · 库存 {{ stock }} · 倍率 {{ multiplier(Number(mult)) }}</div>
        <div v-else-if="sku === 'consignment'">
          倍率 {{ multiplier(Number(mult)) }} · 买家价 {{ buyerPrice ? multiplier(buyerPrice) : '—' }} · M=¥{{ minAmount || 0 }}
        </div>
        <div v-if="categories.length">
          分类：
          <b>{{ categories.map((id) => categoryOpts.find((c) => c.id === id)?.name || id).join('、') }}</b>
        </div>
        <div v-if="title">标题：{{ title }}</div>
      </div>
      <div class="mt-5 flex flex-wrap justify-center gap-2.5">
        <RouterLink v-if="listing && sku !== 'buyback'" class="xm-btn xm-btn-primary" :to="`/market/listings/${listing.id}`">去市场看看</RouterLink>
        <RouterLink class="xm-btn xm-btn-ghost" to="/seller/listings">回我的闲置TOKEN</RouterLink>
        <button type="button" class="xm-btn xm-btn-ghost" @click="resetWizard">再发一条</button>
      </div>
    </section>
  </div>
</template>
