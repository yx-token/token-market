<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { seller } from '@/api'
import type { ListingCategoryOption, Report, SellerProbeRun, UpstreamDetail, VendorView } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import ReportDetail from '@/components/ReportDetail.vue'
import InspectionAppraisal from '@/components/InspectionAppraisal.vue'
import QuotaPanel from '@/components/QuotaPanel.vue'
import ResetCreditsPanel from '@/components/ResetCreditsPanel.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import UpstreamModelWhitelist from '@/components/UpstreamModelWhitelist.vue'
import { authTypeText, isKeyAuthType, label, upstreamName } from '@/utils/labels'
import { takesProjectID } from '@/utils/authGuides'
import { dateTime, money, ms, multiplier, percent } from '@/utils/format'
import { appealableReport, detectionFlowText, intelligenceStatusText } from '@/utils/inspection'
import { errorText } from '@/api/errors'

const props = defineProps<{ id: string }>()
const upstreamId = computed(() => Number(props.id))
const route = useRoute()
const router = useRouter()
const toast = useToast()
const cfg = useConfig()
const { busy, run } = useAction()

const detail = useLoader(() => seller.upstream(upstreamId.value), null as UpstreamDetail | null)
const reports = useLoader(() => seller.reports(upstreamId.value), [] as Report[] | null)
const probeRuns = useLoader(() => seller.probeRuns(upstreamId.value), [] as SellerProbeRun[] | null)
const vendors = ref<VendorView[]>([])
const vendor = computed(() => vendors.value.find((v) => v.platform === detail.data.value?.platform))
const u = computed(() => detail.data.value)
const isOAuth = computed(() => !!u.value && !isKeyAuthType(u.value.auth_type))
// 已在 sub2api 创建账号（授权完成）后才有额度与用量
const hasAccount = computed(() => !!u.value && !['draft', 'authorizing', 'failed', 'removing', 'removed'].includes(u.value.status))
/** 接入未完成：不能挂单/改价，只能继续授权或删除 */
const needsAuth = computed(() => !!u.value && ['draft', 'authorizing', 'failed'].includes(u.value.status))
const showWizard = ref(false)
const canCreateListing = computed(() => u.value?.status === 'ready' && (!u.value.listing || u.value.listing.status === 'closed'))

/** 按来源返回上一级，避免钻进去出不来 */
const backNav = computed(() => {
  const from = typeof route.query.from === 'string' ? route.query.from : ''
  if (from === 'listings') return { to: '/seller/listings', label: '我的闲置TOKEN' }
  if (from === 'usage') return { to: `/seller/usage?upstream=${upstreamId.value}`, label: '用量' }
  if (from === 'publish') return { to: '/seller/publish', label: '发布闲置TOKEN' }
  return { to: '/seller/upstreams', label: '我的上游' }
})

const runningStatuses = new Set(['queued', 'running', 'third_party'])
const hasRunningRuns = computed(() => probeRuns.data.value?.some((run) => runningStatuses.has(run.status)) ?? false)

async function reload() {
  await Promise.all([detail.load(), reports.load(), probeRuns.load()])
  if (!hasRunningRuns.value) stopPolling()
  else startPolling()
}

let timer: ReturnType<typeof setInterval> | null = null
function stopPolling() {
  if (timer) clearInterval(timer)
  timer = null
}
function startPolling() {
  if (timer || !hasRunningRuns.value) return
  timer = setInterval(async () => {
    if (!hasRunningRuns.value) {
      stopPolling()
      return
    }
    await probeRuns.load()
    // 上架检测期间上游状态会随检测推进变化；检测全部结束后再整页刷新一次，带出新报告与健康状态
    if (u.value?.status === 'verifying') await detail.load()
    if (!hasRunningRuns.value) {
      stopPolling()
      await Promise.all([detail.load(), reports.load()])
    }
  }, 10000)
}
onMounted(async () => {
  await reload()
  cfg.ensure()
  const [v, cats] = await Promise.all([
    seller.vendors().catch(() => null),
    seller.listingCategories().catch(() => null),
  ])
  vendors.value = v ?? []
  categoryOpts.value = cats ?? []
  categories.value = []
})
onUnmounted(stopPolling)

// ---- 挂单 ----
const mode = ref<'consignment' | 'pack' | 'buyback'>('consignment')
const mult = ref('')
const joinPool = ref(true)
const faceValue = ref('100')
const stock = ref('3')
const minAmount = ref('0')
const listTitle = ref('')
const listDesc = ref('')
const categories = ref<string[]>([])
const categoryOpts = ref<ListingCategoryOption[]>([])
const metaTitle = ref('')
const metaDesc = ref('')
const metaCategories = ref<string[]>([])
const showMeta = ref(false)
const buyerPrice = computed(() => {
  const m = Number(mult.value)
  return m > 0 ? m * (1 + cfg.config.commission_rate) : null
})

function toggleCategory(id: string, target: 'create' | 'meta' = 'create') {
  const cur = target === 'meta' ? metaCategories : categories
  const set = new Set(cur.value)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  cur.value = [...set]
}

function openMetaEdit() {
  const l = u.value?.listing
  if (!l) return
  metaTitle.value = l.title ?? ''
  metaDesc.value = l.description ?? ''
  metaCategories.value = [...(l.categories ?? [])]
  if (categoryOpts.value.length && !metaCategories.value.some((id) => categoryOpts.value.some((c) => c.id === id))) {
    metaCategories.value = categoryOpts.value[0] ? [categoryOpts.value[0].id] : []
  }
  showMeta.value = true
}

async function saveMeta() {
  const l = u.value?.listing
  if (!l) return
  if (!metaCategories.value.length) {
    toast.error('请至少选择一个市场分类')
    return
  }
  if (
    await run(
      () =>
        seller.updateListingMeta(l.id, {
          title: metaTitle.value.trim(),
          description: metaDesc.value.trim(),
          categories: [...metaCategories.value],
        }),
      '展示信息已更新',
    )
  ) {
    showMeta.value = false
    reload()
  }
}

async function createListing() {
  if ((mode.value === 'consignment' || mode.value === 'pack') && !(Number(mult.value) > 0)) {
    toast.error('请填写倍率')
    return
  }
  if ((mode.value === 'consignment' || mode.value === 'pack') && !categories.value.length) {
    toast.error('请至少选择一个市场分类')
    return
  }
  if (mode.value === 'pack') {
    if (!(Number(faceValue.value) >= 10)) {
      toast.error('额度包面额至少 10 元')
      return
    }
    if (!(Number(stock.value) >= 1)) {
      toast.error('可售份数至少 1')
      return
    }
  }
  const meta = {
    title: listTitle.value.trim() || undefined,
    description: listDesc.value.trim() || undefined,
    categories: mode.value === 'buyback' ? undefined : [...categories.value],
  }
  const body =
    mode.value === 'consignment'
      ? { mode: mode.value, multiplier: Number(mult.value), join_default_pool: joinPool.value, min_amount: Number(minAmount.value) || 0, ...meta }
      : mode.value === 'pack'
        ? { mode: mode.value, multiplier: Number(mult.value), face_value: Number(faceValue.value), stock: Number(stock.value), ...meta }
        : { mode: mode.value, ...meta }
  const okMsg = mode.value === 'buyback' ? '已生成回收报价，请确认' : mode.value === 'pack' ? '额度包已上架' : '挂单已生效'
  if (await run(() => seller.createListing(upstreamId.value, body), okMsg)) reload()
}

async function listingAction(fn: () => Promise<unknown>, msg: string) {
  if (await run(fn, msg)) reload()
}

const showPrice = ref(false)
const newPrice = ref('')
async function changePrice() {
  const l = u.value?.listing
  if (!l) return
  if (await run(() => seller.changePrice(l.id, Number(newPrice.value)), '价格已更新')) {
    showPrice.value = false
    reload()
  }
}

// ---- 上游操作 ----
const showReauth = ref(false)
const reauthURL = ref('')
const reauthCode = ref('')
const reauthProject = ref('')
const reauthWithProject = computed(() => !!u.value && takesProjectID(u.value.platform, u.value.auth_type))
function startReauth() {
  reauthURL.value = ''
  reauthCode.value = ''
  reauthProject.value = ''
  showReauth.value = true
}
async function getReauthLink() {
  const opts = reauthWithProject.value && reauthProject.value.trim() ? { project_id: reauthProject.value.trim() } : undefined
  const res = await run(() => seller.reauthorize(upstreamId.value, opts))
  if (res) {
    reauthURL.value = res.auth_url
    window.open(res.auth_url, '_blank', 'noopener')
  }
}
async function completeReauth() {
  if (await run(() => seller.completeOAuth(upstreamId.value, reauthCode.value.trim()), '重新授权成功')) {
    showReauth.value = false
    reload()
  }
}

// 用卡后额度窗口会变化，重新挂载额度面板以重新读取
const quotaKey = ref(0)

const showReserve = ref(false)
const reserve = ref(0)
async function saveReserve() {
  if (await run(() => seller.setSelfReserve(upstreamId.value, reserve.value), '已保存')) {
    showReserve.value = false
    reload()
  }
}

/** 不能手动复测时的原因；检测正常时后端拒绝复测，按钮置灰 */
const retestBlockedReason = computed(() => {
  const r = u.value?.manual_retest
  return r && !r.allowed ? errorText(r.code ?? '', '当前不能手动复测') : ''
})
async function retest() {
  if (!confirm('复测会消耗你账号的额度，这部分检测不结算收益。只有检测结果异常时才能复测，确定发起？')) return
  await listingAction(() => seller.retest(upstreamId.value), '已排队复测')
}

async function remove() {
  if (!confirm('删除这个上游？挂单会关闭，账号在进行中的请求结束后从平台移除，未结算的收益照常结算。')) return
  if (await run(() => seller.removeUpstream(upstreamId.value), '正在删除')) router.push('/seller/upstreams')
}

// ---- 报告 ----
const openReport = ref<Report | null>(null)
const reportLoading = ref(false)
async function openReportDetail(reportID: number) {
  reportLoading.value = true
  try {
    openReport.value = await seller.report(reportID)
  } catch (error) {
    toast.error(error)
  } finally {
    reportLoading.value = false
  }
}
const showAppeal = ref(false)
const appealReason = ref('')
async function submitAppeal() {
  if (!openReport.value) return
  if (await run(() => seller.appeal(openReport.value!.id, appealReason.value), '申诉已提交，平台会安排复测')) {
    showAppeal.value = false
    appealReason.value = ''
  }
}
const appealable = appealableReport

function thirdPartyStatusText(status: string) {
  const names: Record<string, string> = { pending: '排队中', running: '检测中', completed: '已完成', failed: '失败', timeout: '超时', skipped: '已跳过' }
  return names[status] ?? status
}

function providerText(provider: string) {
  return provider === 'bazaarlink' ? 'BazaarLink' : provider
}

function runStatusText(status: string) {
  const names: Record<string, string> = {
    queued: '排队中', running: '检测中', third_party: '第三方核验中', done: '已完成',
    failed: '未完成', skipped_budget: '预算跳过', skipped_disabled: '已停用跳过',
  }
  return names[status] ?? status
}

function reportFor(run: SellerProbeRun) {
  return run.report_id ? reports.data.value?.find((report) => report.id === run.report_id) : undefined
}

function intelligenceText(run: SellerProbeRun) {
  const report = reportFor(run)
  return report?.summary?.intelligence?.status
    ? intelligenceStatusText(report.summary.intelligence.status)
    : report?.summary?.suites?.iq
      ? percent(report.summary.suites.iq.relative_score, 0)
      : '—'
}
</script>

<template>
  <div v-if="u">
    <PageHeader
      :title="u.account_email ? upstreamName(u) : `上游 #${u.id}`"
      :description="`${u.account_email ? `上游 #${u.id} · ` : ''}${label('platform', u.platform).text} · ${authTypeText(u.platform, u.auth_type)}`"
      :back-to="backNav.to"
      :back-label="backNav.label"
    >
      <RefreshButton :loading="detail.loading.value || reports.loading.value || probeRuns.loading.value" @click="reload" />
      <RouterLink v-if="hasAccount" :to="`/seller/usage?upstream=${u.id}`" class="xm-btn xm-btn-ghost xm-btn-sm">用量明细 →</RouterLink>
      <button v-if="needsAuth" type="button" class="xm-btn xm-btn-primary xm-btn-sm" @click="showWizard = true">继续授权</button>
      <button v-if="u.status === 'verify_failed'" class="btn-secondary" :disabled="busy" @click="listingAction(() => seller.retryVerification(u!.id), '已重新开始检测')">
        重新检测
      </button>
      <button
        v-if="['ready', 'listed'].includes(u.status)"
        class="btn-secondary"
        :disabled="busy || !!retestBlockedReason"
        :title="retestBlockedReason || '检测结果异常时可发起复测；复测消耗你自己的额度，不结算收益'"
        data-testid="manual-retest"
        @click="retest"
      >
        手动复测
      </button>
      <button v-if="isOAuth && !['draft', 'authorizing', 'removing', 'removed'].includes(u.status)" class="btn-secondary" :disabled="busy" @click="startReauth">
        重新授权
      </button>
      <button v-if="vendor?.self_reserve_supported && hasAccount" class="btn-secondary" @click="(reserve = u.self_reserve_percent), (showReserve = true)">自留比例</button>
      <button v-if="!['removing', 'removed'].includes(u.status)" class="btn-ghost text-red-600" :disabled="busy" @click="remove">删除</button>
    </PageHeader>

    <div v-if="needsAuth" class="mb-4 callout callout-warn">
      这个上游还停在「{{ label('upstreamStatus', u.status).text }}」，授权没做完，所以不能挂单、也不能改价。
      厂商 / 接入方式 / 已绑代理在创建后不可改——要换代理请先删除本上游再重新接入。
      <div class="mt-2">
        <button type="button" class="xm-btn xm-btn-primary xm-btn-sm" @click="showWizard = true">继续完成授权 →</button>
      </div>
    </div>
    <div v-if="u.status === 'verifying'" class="mb-4 callout callout-info">正在进行上架检测（平台探测之后可能还有第三方身份检测，通常几分钟），完成后页面会自动刷新。检测通过前不能挂单。</div>
    <div v-if="u.failure_reason && ['verify_failed', 'suspended', 'failed'].includes(u.status)" class="mb-4 callout callout-err">
      {{ u.failure_reason }}
    </div>
    <div v-if="u.health === 'unavailable'" class="mb-4 callout callout-warn">
      账号当前不可用（{{ u.health_reason }}）。如果是授权失效，请点击"重新授权"。
    </div>

    <div class="xm-stats" style="grid-template-columns: repeat(5, 1fr)">
      <div class="xm-stat"><b>状态</b><div class="mt-1"><StatusBadge kind="upstreamStatus" :value="u.status" /></div></div>
      <div class="xm-stat"><b>健康</b><div class="mt-1"><StatusBadge kind="health" :value="u.health" /></div></div>
      <div class="xm-stat"><b>质量等级</b><strong>{{ u.grade ?? '—' }}</strong><em v-if="u.tier">档位 {{ u.tier }}</em></div>
      <div class="xm-stat">
        <b>24h 买家请求</b><strong>{{ u.stats_24h.requests }}</strong>
        <em>成功率 {{ percent(u.stats_24h.success_rate) }} · {{ ms(u.stats_24h.ttft_p50_ms) }}</em>
      </div>
      <div class="xm-stat">
        <b>24h 收益</b><strong>{{ money(u.stats_24h.payout) }}</strong>
        <em>检测 {{ u.stats_24h.probe_requests }} 次</em>
      </div>
    </div>

    <InspectionAppraisal class="mt-5" :appraisal="u.appraisal" :grade="u.grade" :quality-factor="u.quality_factor" :inspection-mode="u.inspection_mode" :price="u.listing?.multiplier" />
    <UpstreamModelWhitelist :upstream="u" class="mt-5" @changed="reload" />
    <QuotaPanel v-if="hasAccount" :key="quotaKey" :upstream-id="u.id" class="mt-5" />
    <ResetCreditsPanel v-if="hasAccount && u.reset_credits_supported" :upstream-id="u.id" class="mt-5" @changed="quotaKey++" />

    <div v-if="u.proxy" class="card mt-5">
      <h2 class="card-title">绑定的代理 IP</h2>
      <div class="flex flex-wrap items-center gap-3 text-sm">
        <span>{{ u.proxy.name }}</span>
        <span class="break-all font-mono text-xs">{{ u.proxy.protocol }}://{{ u.proxy.host.includes(':') ? `[${u.proxy.host}]` : u.proxy.host }}:{{ u.proxy.port }}</span>
        <span class="text-ink-500">{{ u.proxy.seller_id ? '由你提供' : '由平台关联' }}{{ u.proxy.region ? ` · ${u.proxy.region}` : '' }}</span>
        <RouterLink v-if="u.proxy.seller_id" to="/seller/proxies" class="xm-btn xm-btn-ghost xm-btn-sm ml-auto">代理列表 →</RouterLink>
      </div>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">商品</h2>
      <template v-if="u.listing && u.listing.status !== 'closed'">
        <div class="flex flex-wrap items-center gap-3 text-sm">
          <StatusBadge kind="mode" :value="u.listing.mode" />
          <StatusBadge kind="listingStatus" :value="u.listing.status" />
          <span v-if="u.listing.multiplier">你的倍率 <b>{{ multiplier(u.listing.multiplier) }}</b></span>
          <span v-if="(u.listing.mode === 'consignment' || u.listing.mode === 'pack') && u.listing.multiplier" class="text-slate-500">
            买家价 {{ multiplier(u.listing.multiplier * (1 + cfg.config.commission_rate)) }}
          </span>
          <span v-if="u.listing.mode === 'pack' && u.listing.face_value" class="text-slate-500">面额 ¥{{ u.listing.face_value }} · 库存 {{ (u.listing.stock ?? 0) - (u.listing.stock_sold ?? 0) }}/{{ u.listing.stock }}</span>
          <span v-if="u.listing.mode === 'consignment'" class="text-slate-500">{{ u.listing.join_default_pool ? '已加入默认分组' : '未加入默认分组' }}</span>
          <RouterLink :to="`/seller/listings`" class="link ml-auto">价格历史</RouterLink>
        </div>
        <div v-if="u.listing.status === 'pending_confirm' && u.quote" class="mt-3 callout callout-warn">
          平台回收报价：<b>{{ multiplier(u.quote.multiplier) }}</b>（等级 {{ u.quote.grade }}，有效期至 {{ dateTime(u.quote.valid_until) }}）。
          接受后按实际用量 × 回收倍率结算。
          <div class="mt-2 flex gap-2">
            <button type="button" class="xm-btn xm-btn-primary xm-btn-sm" :disabled="busy" @click="listingAction(() => seller.acceptQuote(u!.listing!.id), '已接受报价')">接受</button>
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy" @click="listingAction(() => seller.rejectQuote(u!.listing!.id), '已拒绝')">拒绝</button>
          </div>
        </div>
        <p v-if="u.listing.categories?.length" class="mt-2 text-sm text-slate-500">
          分类：{{ u.listing.categories.join(' · ') }}
          <span v-if="u.listing.title"> · {{ u.listing.title }}</span>
        </p>
        <div class="mt-3 flex flex-wrap gap-2">
          <template v-if="(u.listing.mode === 'consignment' || u.listing.mode === 'pack') && ['active', 'paused'].includes(u.listing.status)">
            <button class="btn-secondary btn-sm" @click="(newPrice = String(u.listing.multiplier ?? '')), (showPrice = true)">改价</button>
            <button class="btn-secondary btn-sm" @click="openMetaEdit">改分类/标题</button>
            <button
              v-if="u.listing.mode === 'consignment'"
              class="btn-secondary btn-sm"
              :disabled="busy"
              @click="listingAction(() => seller.setDefaultPool(u!.listing!.id, !u!.listing!.join_default_pool), '已更新')"
            >
              {{ u.listing.join_default_pool ? '退出默认分组' : '加入默认分组' }}
            </button>
          </template>
          <button v-if="u.listing.status === 'active'" class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.pause(u!.listing!.id), '已暂停')">暂停</button>
          <button v-if="u.listing.status === 'paused'" class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.resume(u!.listing!.id), '已恢复')">恢复</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="listingAction(() => seller.close(u!.listing!.id), '商品已下架')">下架商品</button>
        </div>
      </template>
      <template v-else-if="needsAuth">
        <p class="text-sm text-slate-600">完成授权并通过检测后，才能在这里挂单。请点上方「继续授权」。</p>
      </template>
      <template v-else-if="canCreateListing">
        <div class="flex flex-wrap gap-4 text-sm">
          <label class="flex items-center gap-1"><input v-model="mode" type="radio" value="consignment" /> 按量通道（寄售）</label>
          <label class="flex items-center gap-1"><input v-model="mode" type="radio" value="pack" /> 额度包</label>
          <label class="flex items-center gap-1"><input v-model="mode" type="radio" value="buyback" /> 平台回收</label>
        </div>
        <div v-if="mode === 'consignment'" class="mt-3 grid max-w-xl grid-cols-2 gap-3">
          <div>
            <label class="label" for="mult">倍率（相对官方价）</label>
            <input id="mult" v-model="mult" class="input" inputmode="decimal" :placeholder="vendor ? `${vendor.consignment_min}–${vendor.consignment_max}` : ''" />
            <p class="hint">买家价 = 倍率 × (1 + {{ percent(cfg.config.commission_rate, 0) }} 平台抽佣){{ buyerPrice ? ` = ${multiplier(buyerPrice)}` : '' }}</p>
          </div>
          <div>
            <label class="label" for="min-m">起步金（元）</label>
            <input id="min-m" v-model="minAmount" class="input" inputmode="decimal" />
            <p class="hint">0 = 免费开通，之后按量扣余额</p>
          </div>
          <label class="col-span-2 flex items-center gap-2 text-sm"><input v-model="joinPool" type="checkbox" /> 加入平台默认分组 / 保障池（推荐）</label>
        </div>
        <div v-else-if="mode === 'pack'" class="mt-3 grid max-w-xl grid-cols-2 gap-3">
          <div>
            <label class="label" for="face">包面额（元）</label>
            <input id="face" v-model="faceValue" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="stock">可售份数</label>
            <input id="stock" v-model="stock" class="input" inputmode="numeric" />
          </div>
          <div class="col-span-2">
            <label class="label" for="pack-mult">包内计费倍率</label>
            <input id="pack-mult" v-model="mult" class="input" inputmode="decimal" :placeholder="vendor ? `${vendor.consignment_min}–${vendor.consignment_max}` : ''" />
            <p class="hint">额度包不进保障池；买家整包购买，未用完可退货。{{ buyerPrice ? `买家看到的倍率约 ${multiplier(buyerPrice)}` : '' }}</p>
          </div>
        </div>
        <p v-else class="mt-3 text-sm text-slate-500">平台按质量等级和订阅档位报出回收倍率，你确认后生效，按实际用量结算。不进市场目录，只进保障池。</p>
        <div v-if="mode === 'consignment' || mode === 'pack'" class="mt-3 rounded-xl border border-amber-200 bg-amber-50 p-3">
          <p class="label mb-1">市场分类（必选，至少选一个）</p>
          <p class="mb-2 text-xs text-amber-800">未选分类不能上架；分类决定市场专场与侧栏归类。</p>
          <div class="flex flex-wrap gap-3 text-sm">
            <label v-for="c in categoryOpts" :key="c.id" class="flex items-center gap-1">
              <input type="checkbox" :checked="categories.includes(c.id)" @change="toggleCategory(c.id)" />
              {{ c.path || c.name }}
            </label>
            <span v-if="!categoryOpts.length" class="text-amber-700">暂无可选分类，请联系管理员在控制台开启「卖家可选」</span>
          </div>
        </div>
        <div class="mt-3 grid max-w-xl gap-3">
          <div>
            <label class="label" for="list-title">展示标题（可选）</label>
            <input id="list-title" v-model="listTitle" class="input" placeholder="留空则自动生成" />
          </div>
          <div>
            <label class="label" for="list-desc">短描述（可选）</label>
            <textarea id="list-desc" v-model="listDesc" class="input" rows="2" placeholder="给买家看的说明" />
          </div>
        </div>
        <button
          type="button"
          class="xm-btn xm-btn-primary mt-3"
          :disabled="busy || ((mode === 'consignment' || mode === 'pack') && (!categories.length || !categoryOpts.length))"
          @click="createListing"
        >
          {{ mode === 'buyback' ? '获取报价' : mode === 'pack' ? '上架额度包' : '上架商品' }}
        </button>
        <p class="mt-2 text-[12px] text-[#888]">
          推荐走完整向导：
          <RouterLink class="xm-btn xm-btn-primary xm-btn-sm" :to="`/seller/publish?upstream=${u.id}`">发布闲置TOKEN →</RouterLink>
        </p>
      </template>
      <p v-else class="text-sm text-slate-500">上游通过上架检测后才能发布商品。</p>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">检测记录</h2>
      <DataTable
        :columns="[
          { key: 'queued_at', label: '时间' },
          { key: 'trigger', label: '类型' },
          { key: 'status', label: '状态 / 结论' },
          { key: 'flows', label: '流程' },
          { key: 'models', label: '测试模型' },
          { key: 'progress', label: '进度' },
          { key: 'score', label: '测智', align: 'right' },
          { key: 'third_party', label: '第三方检测' },
          { key: 'actions', label: '' },
        ]"
        :rows="probeRuns.data.value"
        :loading="probeRuns.loading.value"
        row-key="id"
      >
        <template #cell-queued_at="{ row }">
          <span class="num">{{ dateTime(row.queued_at) }}</span>
          <div v-if="row.started_at" class="text-xs text-slate-500">开始时间 {{ dateTime(row.started_at) }}</div>
        </template>
        <template #cell-trigger="{ row }"><StatusBadge kind="trigger" :value="row.trigger" /></template>
        <template #cell-status="{ row }">
          <StatusBadge v-if="row.verdict" kind="verdict" :value="row.verdict" />
          <span v-else>{{ runStatusText(row.status) }}</span>
        </template>
        <template #cell-flows="{ row }">{{ row.flows?.length ? row.flows.map(detectionFlowText).join(' / ') : '—' }}</template>
        <template #cell-models="{ row }">{{ row.models?.length ? row.models.join(' / ') : '—' }}</template>
        <template #cell-progress="{ row }">
          已答 <span class="num">{{ row.progress.answered }}</span>
          <template v-if="row.progress.total !== null"> / 共 <span class="num">{{ row.progress.total }}</span> 题</template>
          <template v-else> 题</template>
        </template>
        <template #cell-score="{ row }">{{ intelligenceText(row) }}</template>
        <template #cell-third_party="{ row }">
          <span v-if="!row.third_party?.length">—</span>
          <span v-for="site in row.third_party" :key="site.provider" class="mr-2 text-xs">{{ providerText(site.provider) }}：{{ thirdPartyStatusText(site.status) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <button v-if="row.report_id" class="btn-ghost btn-sm" :disabled="reportLoading" @click="openReportDetail(row.report_id)">查看报告</button>
        </template>
      </DataTable>
    </div>

    <ModalDialog :open="!!openReport" title="质检报告" wide @update:open="(v: boolean) => !v && (openReport = null)">
      <ReportDetail v-if="openReport" :report="openReport" />
      <template #footer>
        <button v-if="openReport && appealable(openReport)" class="btn-secondary" @click="showAppeal = true">申诉</button>
        <button class="btn-primary" @click="openReport = null">关闭</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showAppeal" title="申诉">
      <p class="text-sm text-slate-600">对这次判定有异议可以申诉，平台会用更多题目重新检测。</p>
      <textarea v-model="appealReason" class="input" rows="3" placeholder="说明理由，例如账号最近未在其他地方使用" />
      <template #footer>
        <button class="btn-secondary" @click="showAppeal = false">取消</button>
        <button class="btn-primary" :disabled="busy || !appealReason.trim()" @click="submitAppeal">提交</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showPrice" title="改价">
      <div>
        <label class="label" for="np">新倍率</label>
        <input id="np" v-model="newPrice" class="input" inputmode="decimal" />
        <p class="hint">改价有冷却时间。买家价 = 倍率 × (1 + 抽佣率)。</p>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showPrice = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="changePrice">保存</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showMeta" title="分类与展示">
      <div class="space-y-3">
        <div>
          <label class="label" for="meta-title">展示标题</label>
          <input id="meta-title" v-model="metaTitle" class="input" placeholder="留空则自动生成" />
        </div>
        <div>
          <label class="label" for="meta-desc">短描述</label>
          <textarea id="meta-desc" v-model="metaDesc" class="input" rows="2" placeholder="给买家看的说明" />
        </div>
        <div>
          <p class="label mb-1">市场分类（至少选一个）</p>
          <div class="flex flex-wrap gap-3 text-sm">
            <label v-for="c in categoryOpts" :key="c.id" class="flex items-center gap-1">
              <input type="checkbox" :checked="metaCategories.includes(c.id)" @change="toggleCategory(c.id, 'meta')" />
              {{ c.path || c.name }}
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showMeta = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="saveMeta">保存</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showReauth" title="重新授权">
      <p class="text-sm text-ink-600">请用<b>同一个官方账号</b>登录授权，然后粘贴授权码或回调地址。</p>
      <div v-if="reauthWithProject">
        <label class="label" for="reauth-project">GCP 项目 ID（可选）</label>
        <input id="reauth-project" v-model="reauthProject" class="input font-mono" placeholder="与原来使用的项目相同" autocomplete="off" />
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="btn-secondary" :disabled="busy" @click="getReauthLink">{{ reauthURL ? '重新获取授权链接' : '获取授权链接' }}</button>
        <a v-if="reauthURL" :href="reauthURL" target="_blank" rel="noopener" class="link break-all text-xs">{{ reauthURL }}</a>
      </div>
      <div>
        <label class="label" for="reauth-code">授权码或回调地址</label>
        <textarea id="reauth-code" v-model="reauthCode" class="input" rows="3" />
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showReauth = false">取消</button>
        <button class="btn-primary" :disabled="busy || !reauthCode.trim()" @click="completeReauth">提交</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="showReserve" title="自留比例">
      <p class="text-sm text-slate-600">为自己保留一部分订阅窗口：窗口用量达到 (100 − 自留比例)% 时平台暂停调度，窗口重置后自动恢复。</p>
      <div class="flex items-center gap-3">
        <input v-model.number="reserve" type="range" min="0" max="90" step="5" class="flex-1" aria-label="自留比例" />
        <span class="w-12 text-right text-sm">{{ reserve }}%</span>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="showReserve = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="saveReserve">保存</button>
      </template>
    </ModalDialog>

    <OnboardingWizard
      v-if="u"
      v-model:open="showWizard"
      :vendors="vendors"
      :resume="u"
      @done="reload()"
    />
  </div>
</template>
