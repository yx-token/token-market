<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { seller } from '@/api'
import type { Report, TrendPoint, UpstreamDetail, VendorView } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import StatCard from '@/components/StatCard.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import TrendChart from '@/components/TrendChart.vue'
import ReportDetail from '@/components/ReportDetail.vue'
import QuotaPanel from '@/components/QuotaPanel.vue'
import { label } from '@/utils/labels'
import { dateTime, money, ms, multiplier, percent } from '@/utils/format'

const props = defineProps<{ id: string }>()
const upstreamId = computed(() => Number(props.id))
const router = useRouter()
const toast = useToast()
const cfg = useConfig()
const { busy, run } = useAction()

const detail = useLoader(() => seller.upstream(upstreamId.value), null as UpstreamDetail | null)
const reports = useLoader(() => seller.reports(upstreamId.value), [] as Report[] | null)
const trend = useLoader(() => seller.trend(upstreamId.value), [] as TrendPoint[] | null)
const vendors = ref<VendorView[]>([])
const vendor = computed(() => vendors.value.find((v) => v.platform === detail.data.value?.platform))
const u = computed(() => detail.data.value)
const isOAuth = computed(() => !!u.value && !['coding', 'go'].includes(u.value.auth_type))
// 已在 sub2api 创建账号（授权完成）后才有额度与用量
const hasAccount = computed(() => !!u.value && !['draft', 'authorizing', 'failed', 'removing', 'removed'].includes(u.value.status))

function reload() {
  detail.load()
  reports.load()
  trend.load()
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  reload()
  cfg.ensure()
  vendors.value = (await seller.vendors().catch(() => null)) ?? []
  timer = setInterval(() => {
    if (u.value?.status === 'verifying') reload()
  }, 10000)
})
onUnmounted(() => timer && clearInterval(timer))

// ---- 挂单 ----
const mode = ref<'consignment' | 'buyback'>('consignment')
const mult = ref('')
const joinPool = ref(true)
const buyerPrice = computed(() => {
  const m = Number(mult.value)
  return m > 0 ? m * (1 + cfg.config.commission_rate) : null
})

async function createListing() {
  const body = mode.value === 'consignment' ? { mode: mode.value, multiplier: Number(mult.value), join_default_pool: joinPool.value } : { mode: mode.value }
  if (mode.value === 'consignment' && !(Number(mult.value) > 0)) {
    toast.error('请填写倍率')
    return
  }
  if (await run(() => seller.createListing(upstreamId.value, body), mode.value === 'consignment' ? '挂单已生效' : '已生成回收报价，请确认')) reload()
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
async function startReauth() {
  const res = await run(() => seller.reauthorize(upstreamId.value))
  if (res) {
    reauthURL.value = res.auth_url
    reauthCode.value = ''
    showReauth.value = true
    window.open(res.auth_url, '_blank', 'noopener')
  }
}
async function completeReauth() {
  if (await run(() => seller.completeOAuth(upstreamId.value, reauthCode.value.trim()), '重新授权成功')) {
    showReauth.value = false
    reload()
  }
}

const showReserve = ref(false)
const reserve = ref(0)
async function saveReserve() {
  if (await run(() => seller.setSelfReserve(upstreamId.value, reserve.value), '已保存')) {
    showReserve.value = false
    reload()
  }
}

async function remove() {
  if (!confirm('删除这个上游？挂单会关闭，账号在进行中的请求结束后从平台移除，未结算的收益照常结算。')) return
  if (await run(() => seller.removeUpstream(upstreamId.value), '正在删除')) router.push('/seller/upstreams')
}

// ---- 报告 ----
const openReport = ref<Report | null>(null)
const showAppeal = ref(false)
const appealReason = ref('')
async function submitAppeal() {
  if (!openReport.value) return
  if (await run(() => seller.appeal(openReport.value!.id, appealReason.value), '申诉已提交，平台会安排复测')) {
    showAppeal.value = false
    appealReason.value = ''
  }
}
const appealable = (r: Report) => r.verdict === 'degraded' || r.verdict === 'unavailable'
</script>

<template>
  <div v-if="u">
    <PageHeader :title="`上游 #${u.id}`" :description="`${label('platform', u.platform).text} · ${label('authType', u.auth_type).text}`">
      <RouterLink v-if="hasAccount" :to="`/seller/usage?upstream=${u.id}`" class="btn-secondary">用量明细</RouterLink>
      <button v-if="u.status === 'verify_failed'" class="btn-secondary" :disabled="busy" @click="listingAction(() => seller.retryVerification(u!.id), '已重新开始检测')">
        重新检测
      </button>
      <button v-if="['ready', 'listed'].includes(u.status)" class="btn-secondary" :disabled="busy" @click="listingAction(() => seller.retest(u!.id), '已排队复测')">
        手动复测
      </button>
      <button v-if="isOAuth && !['draft', 'authorizing', 'removing', 'removed'].includes(u.status)" class="btn-secondary" :disabled="busy" @click="startReauth">
        重新授权
      </button>
      <button v-if="vendor?.self_reserve_supported" class="btn-secondary" @click="(reserve = u.self_reserve_percent), (showReserve = true)">自留比例</button>
      <button v-if="!['removing', 'removed'].includes(u.status)" class="btn-ghost text-red-600" :disabled="busy" @click="remove">删除</button>
    </PageHeader>

    <div v-if="u.status === 'verifying'" class="mb-4 callout callout-info">正在进行上架检测（平台探测之后可能还有第三方身份检测，通常几分钟），完成后页面会自动刷新。检测通过前不能挂单。</div>
    <div v-if="u.failure_reason && ['verify_failed', 'suspended', 'failed'].includes(u.status)" class="mb-4 callout callout-err">
      {{ u.failure_reason }}
    </div>
    <div v-if="u.health === 'unavailable'" class="mb-4 callout callout-warn">
      账号当前不可用（{{ u.health_reason }}）。如果是授权失效，请点击"重新授权"。
    </div>

    <div class="grid grid-cols-2 gap-4 md:grid-cols-5">
      <div class="stat">
        <div class="stat-label">状态</div>
        <div class="mt-2"><StatusBadge kind="upstreamStatus" :value="u.status" /></div>
      </div>
      <div class="stat">
        <div class="stat-label">健康</div>
        <div class="mt-2"><StatusBadge kind="health" :value="u.health" /></div>
      </div>
      <StatCard label="质量等级" :value="u.grade ?? '—'" :hint="u.tier ? `档位 ${u.tier}` : undefined" />
      <StatCard label="24h 买家请求" :value="u.stats_24h.requests" :hint="`成功率 ${percent(u.stats_24h.success_rate)} · 首字 ${ms(u.stats_24h.ttft_p50_ms)}`" />
      <StatCard label="24h 收益" :value="money(u.stats_24h.payout)" :hint="`检测请求 ${u.stats_24h.probe_requests} 次`" />
    </div>

    <QuotaPanel v-if="hasAccount" :upstream-id="u.id" class="mt-5" />

    <div class="card mt-5">
      <h2 class="card-title">挂单</h2>
      <template v-if="u.listing && u.listing.status !== 'closed'">
        <div class="flex flex-wrap items-center gap-3 text-sm">
          <StatusBadge kind="mode" :value="u.listing.mode" />
          <StatusBadge kind="listingStatus" :value="u.listing.status" />
          <span v-if="u.listing.multiplier">你的倍率 <b>{{ multiplier(u.listing.multiplier) }}</b></span>
          <span v-if="u.listing.mode === 'consignment' && u.listing.multiplier" class="text-slate-500">
            买家价 {{ multiplier(u.listing.multiplier * (1 + cfg.config.commission_rate)) }}
          </span>
          <span v-if="u.listing.mode === 'consignment'" class="text-slate-500">{{ u.listing.join_default_pool ? '已加入默认分组' : '未加入默认分组' }}</span>
          <RouterLink :to="`/seller/listings`" class="link ml-auto">价格历史</RouterLink>
        </div>
        <div v-if="u.listing.status === 'pending_confirm' && u.quote" class="mt-3 callout callout-warn">
          平台回收报价：<b>{{ multiplier(u.quote.multiplier) }}</b>（等级 {{ u.quote.grade }}，有效期至 {{ dateTime(u.quote.valid_until) }}）。
          接受后按实际用量 × 回收倍率结算。
          <div class="mt-2 flex gap-2">
            <button class="btn-primary btn-sm" :disabled="busy" @click="listingAction(() => seller.acceptQuote(u!.listing!.id), '已接受报价')">接受</button>
            <button class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.rejectQuote(u!.listing!.id), '已拒绝')">拒绝</button>
          </div>
        </div>
        <div class="mt-3 flex flex-wrap gap-2">
          <template v-if="u.listing.mode === 'consignment' && ['active', 'paused'].includes(u.listing.status)">
            <button class="btn-secondary btn-sm" @click="(newPrice = String(u.listing.multiplier ?? '')), (showPrice = true)">改价</button>
            <button class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.setDefaultPool(u!.listing!.id, !u!.listing!.join_default_pool), '已更新')">
              {{ u.listing.join_default_pool ? '退出默认分组' : '加入默认分组' }}
            </button>
          </template>
          <button v-if="u.listing.status === 'active'" class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.pause(u!.listing!.id), '已暂停')">暂停</button>
          <button v-if="u.listing.status === 'paused'" class="btn-secondary btn-sm" :disabled="busy" @click="listingAction(() => seller.resume(u!.listing!.id), '已恢复')">恢复</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="listingAction(() => seller.close(u!.listing!.id), '挂单已关闭')">关闭挂单</button>
        </div>
      </template>
      <template v-else-if="u.status === 'ready'">
        <div class="flex gap-4 text-sm">
          <label class="flex items-center gap-1"><input v-model="mode" type="radio" value="consignment" /> 寄售（自己定价）</label>
          <label class="flex items-center gap-1"><input v-model="mode" type="radio" value="buyback" /> 平台回收（平台报价，省心）</label>
        </div>
        <div v-if="mode === 'consignment'" class="mt-3 grid max-w-xl grid-cols-2 gap-3">
          <div>
            <label class="label" for="mult">倍率（相对官方价）</label>
            <input id="mult" v-model="mult" class="input" inputmode="decimal" :placeholder="vendor ? `${vendor.consignment_min}–${vendor.consignment_max}` : ''" />
            <p class="hint">买家价 = 倍率 × (1 + {{ percent(cfg.config.commission_rate, 0) }} 平台抽佣){{ buyerPrice ? ` = ${multiplier(buyerPrice)}` : '' }}</p>
          </div>
          <label class="mt-6 flex items-center gap-2 text-sm"><input v-model="joinPool" type="checkbox" /> 加入平台默认分组（推荐）</label>
        </div>
        <p v-else class="mt-3 text-sm text-slate-500">平台按质量等级和订阅档位报出回收倍率，你确认后生效，按实际用量结算。</p>
        <button class="btn-primary mt-3" :disabled="busy" @click="createListing">{{ mode === 'consignment' ? '挂单' : '获取报价' }}</button>
      </template>
      <p v-else class="text-sm text-slate-500">上游通过上架检测后才能挂单。</p>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">质量趋势（近 30 天）</h2>
      <TrendChart v-if="trend.data.value && trend.data.value.length" :points="trend.data.value" />
      <p v-else class="text-sm text-slate-400">暂无检测数据</p>
    </div>

    <div class="card mt-5">
      <h2 class="card-title">测试报告</h2>
      <DataTable
        :columns="[
          { key: 'created_at', label: '时间' },
          { key: 'trigger', label: '类型' },
          { key: 'verdict', label: '结论' },
          { key: 'score', label: '智力相对分', align: 'right' },
          { key: 'actions', label: '' },
        ]"
        :rows="reports.data.value"
        :loading="reports.loading.value"
        row-key="id"
      >
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-trigger="{ row }"><StatusBadge kind="trigger" :value="row.trigger" /></template>
        <template #cell-verdict="{ row }"><StatusBadge kind="verdict" :value="row.verdict" /></template>
        <template #cell-score="{ row }">{{ row.summary?.suites?.iq ? percent(row.summary.suites.iq.relative_score, 0) : '—' }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-ghost btn-sm" @click="openReport = row">查看</button>
        </template>
      </DataTable>
    </div>

    <ModalDialog :open="!!openReport" title="测试报告" wide @update:open="(v: boolean) => !v && (openReport = null)">
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

    <ModalDialog v-model:open="showReauth" title="重新授权">
      <p class="text-sm text-slate-600">已打开授权页面，请用<b>同一个官方账号</b>登录授权，然后粘贴授权码或回调地址。</p>
      <a :href="reauthURL" target="_blank" rel="noopener" class="link break-all text-xs">{{ reauthURL }}</a>
      <textarea v-model="reauthCode" class="input" rows="3" />
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
  </div>
</template>
