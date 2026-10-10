<script setup lang="ts">
/**
 * 发布供给：对齐 docs/prototypes/seller-publish.html
 * 代理 → 接入 → 检测 → 商品形态 → 定价上架 → 完成（全部走真实 API）
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { market, seller } from '@/api'
import type { Listing, ListingCategoryOption, Proxy, Quote, Upstream, VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import { authGuides, takesProjectID, takesTeam } from '@/utils/authGuides'
import { authTypeText, isKeyAuthType, label } from '@/utils/labels'
import { multiplier, percent } from '@/utils/format'

const route = useRoute()
const cfg = useConfig()
const toast = useToast()
const { busy, run } = useAction()

const step = ref(0)
const steps = ['代理', '接入', '检测', '商品', '定价', '完成'] as const

const vendors = ref<VendorView[]>([])
const proxies = ref<Proxy[]>([])
const requireSellerProxy = ref(true)
const proxyID = ref<number | ''>('')
const readyUpstreams = ref<Upstream[]>([])
const resumeUpstreamID = ref<number | ''>('')

const platform = ref('')
const authType = ref('')
const upstream = ref<Upstream | null>(null)
const authURL = ref('')
const code = ref('')
const apiKey = ref('')
const projectID = ref('')
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
const categories = ref<string[]>(['featured'])
const categoryOpts = ref<ListingCategoryOption[]>([])
const quote = ref<Quote | null>(null)
const listing = ref<Listing | null>(null)

const availableProxies = computed(() =>
  proxies.value.filter((p) => p.status === 'available' && !p.assigned_upstream_id && p.sub2api_proxy_id),
)
/** 已占用代理：有登记但不在空闲列表，避免用户以为「没代理」 */
const occupiedProxies = computed(() =>
  proxies.value.filter((p) => p.assigned_upstream_id || p.status === 'assigned'),
)
const vendor = computed(() => vendors.value.find((v) => v.platform === platform.value))
const isOAuth = computed(() => !isKeyAuthType(authType.value))
const withProject = computed(() => takesProjectID(platform.value, authType.value))
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
  if (categoryOpts.value.length && !categories.value.some((id) => categoryOpts.value.some((c) => c.id === id))) {
    categories.value = categoryOpts.value[0] ? [categoryOpts.value[0].id] : []
  }
  const busyUpstream = new Set(
    (ls ?? []).filter((l) => l.status !== 'closed').map((l) => l.upstream_id),
  )
  readyUpstreams.value = (us ?? []).filter((u) => u.status === 'ready' && !busyUpstream.has(u.id))
  platform.value = vendors.value[0]?.platform ?? ''
  authType.value = vendors.value[0]?.auth_types[0] ?? ''
  const resume = Number(route.query.upstream)
  if (resume) {
    const u = await seller.upstream(resume).catch(() => null)
    if (u && u.status === 'ready') {
      upstream.value = u
      platform.value = u.platform
      authType.value = u.auth_type
      step.value = 3
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
  if (n === 2) startProbePoll()
  else if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (n === 4 && sku.value === 'buyback') loadQuote()
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
      toast.error('该上游尚未通过检测，请走完整接入流程')
      return
    }
    upstream.value = u
    platform.value = u.platform
    authType.value = u.auth_type
    step.value = 3
    return
  }
  if (requireSellerProxy.value && !proxyID.value) {
    if (occupiedProxies.value.length && !availableProxies.value.length) {
      toast.error('现有代理都已绑定账号。新接入请再添加空闲代理；若给已有上游挂单，请选上方「已通过检测的上游」')
    } else {
      toast.error('请选择空闲代理，或先去「代理 IP」登记')
    }
    return
  }
  step.value = 1
}

async function createAndAuthorize() {
  if (!platform.value || !authType.value) {
    toast.error('请选择厂商与接入方式')
    return
  }
  if (requireSellerProxy.value && !proxyID.value) {
    toast.error('请先选择代理')
    return
  }
  let u: Upstream | null = upstream.value
  if (!u || u.platform !== platform.value) {
    const created = await run(() => seller.createUpstream(platform.value, authType.value, proxyID.value || undefined))
    if (!created) return
    u = created
    upstream.value = created
  }
  if (isOAuth.value) {
    if (!code.value.trim()) {
      toast.error('请先获取授权链接并回填授权码')
      return
    }
    const done = await run(() => seller.completeOAuth(u!.id, code.value.trim()))
    if (!done) return
    upstream.value = done
    u = done
  } else {
    if (!apiKey.value.trim()) {
      toast.error('请粘贴 Coding Plan Key')
      return
    }
    const body = {
      api_key: apiKey.value.trim(),
      ...(withTeam.value && team.value.organization.trim()
        ? { organization: team.value.organization.trim(), project: team.value.project.trim() || undefined }
        : {}),
    }
    const done = await run(() => seller.submitCodingKey(u!.id, body))
    if (!done) return
    upstream.value = done
    u = done
  }
  if (reserve.value >= 0) {
    await seller.setSelfReserve(u.id, Number(reserve.value)).catch(() => null)
  }
  step.value = 2
}

async function getOAuthLink() {
  if (!upstream.value) {
    const u = await run(() => seller.createUpstream(platform.value, authType.value, proxyID.value || undefined))
    if (!u) return
    upstream.value = u
  }
  const opts = withProject.value && projectID.value.trim() ? { project_id: projectID.value.trim() } : undefined
  const res = await run(() => seller.startOAuth(upstream.value!.id, opts))
  if (res) {
    authURL.value = res.auth_url
    window.open(res.auth_url, '_blank', 'noopener')
  }
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
  const u = await run(() => seller.retryVerification(upstream.value!.id), '已重新开始检测')
  if (u) upstream.value = u
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
      toast.error('额度包面额至少 10 元')
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
          join_default_pool: joinPool.value,
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
    sku.value === 'buyback' ? '已生成回收报价' : sku.value === 'pack' ? '额度包已上架' : '按量通道已上架',
  )
  if (!res) return
  listing.value = res.listing
  quote.value = res.quote ?? null
  if (sku.value === 'buyback' && res.listing.status === 'pending_confirm') {
    // 停留在完成页，提示去确认报价
  }
  step.value = 5
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
  code.value = ''
  apiKey.value = ''
  authURL.value = ''
  title.value = ''
  description.value = ''
  categories.value = ['featured']
  resumeUpstreamID.value = ''
}

const probeOk = computed(() => upstream.value?.status === 'ready')
const probeFail = computed(() => ['verify_failed', 'failed'].includes(upstream.value?.status ?? ''))
</script>

<template>
  <div class="xm-publish">
    <RouterLink
      :to="(resumeUpstreamID || upstream?.id) ? `/seller/upstreams/${resumeUpstreamID || upstream?.id}?from=publish` : '/seller/upstreams'"
      class="xm-page-back"
    >
      <span class="xm-page-back__ico" aria-hidden="true">←</span>
      {{ (resumeUpstreamID || upstream?.id) ? `上游 #${resumeUpstreamID || upstream?.id}` : '我的上游' }}
    </RouterLink>
    <h1 class="text-[28px] font-extrabold tracking-tight text-[#1f1f1f]">发布闲置供给</h1>
    <p class="mt-2 mb-6 max-w-3xl text-[15px] leading-relaxed text-[#666]">
      授权订阅账号 → 上架检测 → 挂「按量通道」或「额度包」。不是上架实物，是开放可调度额度。
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
      <p class="sub">授权与调用走同一固定代理；每个代理同时只能绑一个账号。</p>

      <div v-if="readyUpstreams.length" class="mb-4">
        <label class="xm-field-label">或从已通过检测的上游继续挂单</label>
        <select v-model="resumeUpstreamID" class="xm-input">
          <option value="">— 新接入账号 —</option>
          <option v-for="u in readyUpstreams" :key="u.id" :value="u.id">
            #{{ u.id }} {{ label('platform', u.platform).text }} · {{ u.tier || u.grade || u.status }}
          </option>
        </select>
      </div>

      <template v-if="!resumeUpstreamID">
        <label class="xm-field-label">选择代理{{ requireSellerProxy ? '（必选）' : '（可由平台关联）' }}</label>
        <select v-model="proxyID" class="xm-input">
          <option value="">{{ requireSellerProxy ? '请选择空闲代理' : '由平台自动关联' }}</option>
          <option v-for="p in availableProxies" :key="p.id" :value="p.id">
            {{ p.name }} · {{ p.protocol }}://{{ p.host.includes(':') ? `[${p.host}]` : p.host }}:{{ p.port }}{{ p.region ? ` · ${p.region}` : '' }}
          </option>
        </select>
        <p v-if="!availableProxies.length && occupiedProxies.length" class="xm-hint !text-[#b45309]">
          你有 {{ occupiedProxies.length }} 条代理，但都已分配给账号
          <template v-for="(p, i) in occupiedProxies" :key="p.id">
            <template v-if="i">、</template>
            <RouterLink
              v-if="p.assigned_upstream_id"
              :to="`/seller/upstreams/${p.assigned_upstream_id}?from=publish`"
              class="font-bold underline underline-offset-2"
            >#{{ p.assigned_upstream_id }}</RouterLink>
            <span v-else>#?</span>
          </template>
          。每个代理同时只能绑一个号——新接入请
          <RouterLink to="/seller/proxies" class="font-bold underline underline-offset-2">再添加空闲代理</RouterLink>
          ；若要给已有上游挂单，用上方「已通过检测的上游」。
        </p>
        <p v-else class="xm-hint">
          暂无代理？
          <RouterLink to="/seller/proxies" class="font-bold underline underline-offset-2">去登记代理 IP</RouterLink>
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
      <p class="sub">只做授权，不交密码。OAuth 跳转官方页；Coding Plan 粘贴订阅 Key。</p>

      <label class="xm-field-label">厂商</label>
      <div class="mb-4 grid grid-cols-2 gap-2 md:grid-cols-3">
        <button
          v-for="v in vendors"
          :key="v.platform"
          type="button"
          class="xm-choice"
          :class="{ on: platform === v.platform }"
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
          <input v-model="authType" type="radio" :value="t" class="mt-0.5" />
          <div>
            <strong>{{ authTypeText(platform, t) }}</strong>
            <span>{{ authGuides[`${platform}:${t}`]?.summary }}</span>
          </div>
        </label>
      </div>

      <template v-if="isOAuth">
        <div v-if="withProject" class="mb-3">
          <label class="xm-field-label">GCP 项目 ID（可选）</label>
          <input v-model="projectID" class="xm-input" placeholder="my-project-123456" />
        </div>
        <button type="button" class="xm-btn xm-btn-ghost mb-2 w-full" :disabled="busy" @click="getOAuthLink">
          {{ authURL ? '重新获取授权链接' : '打开官方授权页' }}
        </button>
        <p v-if="authURL" class="xm-hint break-all">{{ authURL }}</p>
        <label class="xm-field-label">授权码或回调地址</label>
        <textarea v-model="code" class="xm-input" rows="3" placeholder="code#state 或完整回调 URL" />
      </template>
      <template v-else>
        <p class="xm-hint mb-2">{{ guide?.how || '粘贴官方 Coding Plan 订阅 Key，平台会校验窗口用量。' }}</p>
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
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="createAndAuthorize">创建上游并检测</button>
      </div>
    </section>

    <!-- 2 检测 -->
    <section v-else-if="step === 2" class="xm-pub-panel">
      <h2>上架检测</h2>
      <p class="sub">首次接入必做。通过前不能挂单。上游 #{{ upstream?.id }}</p>

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
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = 1">上一步</button>
        <button v-if="probeFail" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="retryProbe">重试检测</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="!probeOk" @click="step = 3">检测通过，继续</button>
      </div>
    </section>

    <!-- 3 商品形态 -->
    <section v-else-if="step === 3" class="xm-pub-panel">
      <h2>选择商品形态</h2>
      <p class="sub">同一上游同时只能有一条有效挂单；形态决定买家怎么下单。</p>
      <div class="flex flex-col gap-2.5">
        <label class="xm-choice" :class="{ on: sku === 'consignment' }">
          <input v-model="sku" type="radio" value="consignment" />
          <div>
            <strong>A. 按量通道（寄售）</strong>
            <span>买家付最低额度 M（可为 0），调用按官方价 × 你的倍率扣余额。可进市场，也可进保障池。</span>
          </div>
        </label>
        <label class="xm-choice" :class="{ on: sku === 'pack' }">
          <input v-model="sku" type="radio" value="pack" />
          <div>
            <strong>B. 额度包</strong>
            <span>固定面额商品（如 ¥100），买家整包购买；未用完可退货释放库存。有可售份数。</span>
          </div>
        </label>
        <label class="xm-choice" :class="{ on: sku === 'buyback' }">
          <input v-model="sku" type="radio" value="buyback" />
          <div>
            <strong>C. 平台回收（按量）</strong>
            <span>接受平台报价，不进市场目录，只进保障池。倍率由平台定。</span>
          </div>
        </label>
      </div>
      <div class="xm-pub-actions">
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = upstream?.status === 'ready' && resumeUpstreamID ? 0 : 2">上一步</button>
        <button type="button" class="xm-btn xm-btn-primary" @click="step = 4">下一步</button>
      </div>
    </section>

    <!-- 4 定价 -->
    <section v-else-if="step === 4" class="xm-pub-panel">
      <h2>定价与上架内容</h2>
      <p class="sub">设置买家看到的价格与标题。</p>

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
          <input v-model="joinPool" type="checkbox" /> 同时加入保障池（倍率需足够低）
        </label>
      </template>

      <template v-else-if="sku === 'pack'">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label class="xm-field-label">包面额（元）</label>
            <input v-model="faceValue" class="xm-input" inputmode="decimal" />
          </div>
          <div>
            <label class="xm-field-label">可售份数</label>
            <input v-model="stock" class="xm-input" inputmode="numeric" />
          </div>
        </div>
        <div class="mt-3">
          <label class="xm-field-label">包内计费倍率</label>
          <input v-model="mult" class="xm-input" inputmode="decimal" />
          <p class="xm-hint">额度包不进保障池；买家整包购买，未用完可退货。</p>
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

      <div v-if="sku === 'consignment' || sku === 'pack'" class="mt-4">
        <label class="xm-field-label">市场分类（至少选一个）</label>
        <div class="mt-2 flex flex-col gap-2">
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
        <button type="button" class="xm-btn xm-btn-ghost" @click="step = 3">上一步</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="publish">确认上架</button>
      </div>
    </section>

    <!-- 5 完成 -->
    <section v-else class="xm-pub-panel text-center">
      <h2 class="!mb-2">已发布</h2>
      <p class="sub !mb-4">
        <template v-if="sku === 'pack'">额度包已上架，买家可在市场整包购买。</template>
        <template v-else-if="sku === 'buyback' && listing?.status === 'pending_confirm'">
          回收报价待确认。
          <button type="button" class="xm-btn xm-btn-primary xm-btn-sm ml-2" :disabled="busy" @click="acceptBuyback">接受报价</button>
        </template>
        <template v-else>按量通道已生效，可在市场被选用。</template>
      </p>
      <div class="rounded-[14px] bg-[#fafafa] p-4 text-left text-[14px] leading-relaxed">
        <div>上游 <b>#{{ upstream?.id }}</b> · 挂单 <b>#{{ listing?.id }}</b></div>
        <div>
          形态：
          <b>{{ sku === 'pack' ? '额度包' : sku === 'buyback' ? '平台回收' : '按量通道' }}</b>
        </div>
        <div v-if="sku === 'pack'">面额 ¥{{ faceValue }} · 库存 {{ stock }} · 倍率 {{ multiplier(Number(mult)) }}</div>
        <div v-else-if="sku === 'consignment'">
          倍率 {{ multiplier(Number(mult)) }} · 买家价 {{ buyerPrice ? multiplier(buyerPrice) : '—' }} · M=¥{{ minAmount || 0 }}
        </div>
        <div v-if="title">标题：{{ title }}</div>
      </div>
      <div class="mt-5 flex flex-wrap justify-center gap-2.5">
        <RouterLink v-if="listing && sku !== 'buyback'" class="xm-btn xm-btn-primary" :to="`/market/listings/${listing.id}`">去市场看看</RouterLink>
        <RouterLink class="xm-btn xm-btn-ghost" to="/seller/listings">回挂单列表</RouterLink>
        <button type="button" class="xm-btn xm-btn-ghost" @click="resetWizard">再发一条</button>
      </div>
    </section>
  </div>
</template>
