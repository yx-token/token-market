<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { buyer, market } from '@/api'
import { merchants } from '@/api/merchants'
import type { BuyerKey, Limits, MarketItem, PoolItem, QuotaPack } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import DateField from '@/components/common/DateField.vue'
import RouteEditor from '@/components/RouteEditor.vue'
import { dateTime, money } from '@/utils/format'
import { toRoute, validateChain, type HopChoice } from '@/utils/route'
import { copyText } from '@/utils/clipboard'
import { sampleRequest } from '@/utils/keySample'

const route = useRoute()
const toast = useToast()
const cfg = useConfig()
const keys = useLoader(() => buyer.keys(), [] as BuyerKey[] | null)
const packs = useLoader(() => buyer.packs(), [] as QuotaPack[] | null)
const listings = ref<MarketItem[]>([])
const listingsLoading = ref(false)
const listingsError = ref(false)
const pools = ref<PoolItem[]>([])
const { busy, run } = useAction()

// 创建 / 编辑表单
const editing = ref<BuyerKey | null>(null)
const showForm = ref(false)
const form = reactive({
  name: '',
  type: 'default' as 'default' | 'chain',
  pool: '',
  hops: [] as HopChoice[],
  quota: '',
  h5: '',
  d1: '',
  d7: '',
  ips: '',
  expires: '',
})
const created = ref<{ key: string; platform: string } | null>(null)
// 已取回的明文（点「查看」后按 Key id 缓存），刷新浏览器后再点一次即可重新取回
const revealed = reactive<Record<number, { key: string; platform: string }>>({})
const revealing = ref(0)

const baseURL = computed(() => cfg.config.edge_url || window.location.origin)

async function refundPack(p: QuotaPack) {
  if (!confirm(`按未用量退货 ¥${p.remaining.toFixed(2)}？Key 将停用，卖家库存加回。`)) return
  const online = !!(p.pay_channel && p.pay_channel !== 'balance' && p.pay_channel !== 'manual')
  const dest =
    online && confirm(`该包通过 ${p.pay_channel} 购买。确定 = 原路退回支付渠道；取消 = 退到平台余额。`)
      ? 'original'
      : 'balance'
  if (await run(() => buyer.refundPack(p.id, dest), dest === 'original' ? '已申请原路退款' : '已退货到余额')) {
    packs.load()
    keys.load()
  }
}

onMounted(async () => {
  keys.load()
  packs.load()
  await cfg.ensure()
  const [, ps] = await Promise.all([loadMerchantListings(), market.pools().catch(() => null)])
  pools.value = ps ?? []
  const pre = Number(route.query.listing)
  if (pre) {
    const l = listings.value.find((x) => x.listing_id === pre)
    if (!l) {
      toast.info('该挂单不在商家清单中或暂不可用，请先去市场加入商家。')
      return
    }
    openCreate()
    form.type = 'chain'
    form.hops = [{ listingId: pre, platform: l.platform }]
    form.name = `商家 #${l.seller_id} · 挂单 #${pre}`
  }
})

async function loadMerchantListings() {
  listingsLoading.value = true
  listingsError.value = false
  try {
    listings.value = await merchants.listings() ?? []
  } catch (e) {
    listings.value = []
    listingsError.value = true
    toast.error(e)
  } finally {
    listingsLoading.value = false
  }
}

function resetForm() {
  Object.assign(form, { name: '', type: 'default', pool: pools.value[0]?.name ?? '', hops: [], quota: '', h5: '', d1: '', d7: '', ips: '', expires: '' })
}

function openCreate() {
  editing.value = null
  resetForm()
  showForm.value = true
}

function openEdit(k: BuyerKey) {
  editing.value = k
  const hops: HopChoice[] = (k.hops ?? []).map((h) =>
    h.pool
      ? { pool: h.pool, platform: pools.value.find((p) => p.name === h.pool)?.platform ?? '' }
      : { listingId: h.listing_id, platform: listings.value.find((l) => l.listing_id === h.listing_id)?.platform ?? '' },
  )
  Object.assign(form, {
    name: k.name,
    type: k.route_type === 'chain' ? 'chain' : 'default',
    pool: k.route_type === 'default' ? (k.hops?.[0]?.pool ?? '') : (pools.value[0]?.name ?? ''),
    hops: k.route_type === 'chain' ? hops : [],
    quota: k.limits.quota_usd?.toString() ?? '',
    h5: k.limits.rate_limit_5h_usd?.toString() ?? '',
    d1: k.limits.rate_limit_1d_usd?.toString() ?? '',
    d7: k.limits.rate_limit_7d_usd?.toString() ?? '',
    ips: (k.limits.ip_whitelist ?? []).join(', '),
    expires: k.limits.expires_at ? k.limits.expires_at.slice(0, 10) : '',
  })
  showForm.value = true
}

function num(s: string): number | undefined {
  const n = Number(s)
  return s.trim() && n > 0 ? n : undefined
}

const limits = computed<Limits>(() => ({
  quota_usd: num(form.quota),
  rate_limit_5h_usd: num(form.h5),
  rate_limit_1d_usd: num(form.d1),
  rate_limit_7d_usd: num(form.d7),
  ip_whitelist: form.ips.split(/[,\s]+/).filter(Boolean),
  expires_at: form.expires ? new Date(`${form.expires}T23:59:59`).toISOString() : undefined,
}))

async function save() {
  if (!form.name.trim()) {
    toast.error('请填写名称')
    return
  }
  const err = form.type === 'chain' ? validateChain(form.hops, cfg.config.max_hops) : form.pool ? null : '请选择默认分组'
  if (err) {
    toast.error(err)
    return
  }
  if (form.type === 'chain' && form.hops.some((h) => !h.pool && !listings.value.some((l) => l.listing_id === h.listingId))) {
    toast.error('所选挂单已移出商家清单或暂不可用，请重新选择。')
    return
  }
  const routeInput = toRoute(form.type, form.pool, form.hops)
  if (editing.value) {
    const ok = await run(() => buyer.updateKey(editing.value!.id, { name: form.name, route: routeInput, limits: limits.value }), '已保存')
    if (ok) {
      showForm.value = false
      keys.load()
    }
    return
  }
  const res = await run(() => buyer.createKey({ name: form.name, route: routeInput, limits: limits.value }))
  if (res) {
    showForm.value = false
    created.value = { key: res.key, platform: res.detail.platform ?? '' }
    keys.load()
  }
}

async function toggle(k: BuyerKey) {
  if (await run(() => buyer.setKeyEnabled(k.id, k.status !== 'active'), k.status === 'active' ? '已停用' : '已启用')) keys.load()
}

async function remove(k: BuyerKey) {
  if (!confirm(`删除 Key「${k.name}」？删除后立即失效，不能恢复。`)) return
  if (await run(() => buyer.deleteKey(k.id), '已删除')) keys.load()
}

function hopText(h: { listing_id?: number; pool?: string }) {
  return h.pool ? `默认分组 ${h.pool}` : `卖家挂单 #${h.listing_id}`
}

function packForKey(keyId: number): QuotaPack | undefined {
  return (packs.data.value ?? []).find((p) => p.buyer_key_id === keyId && (p.status === 'active' || p.status === 'depleted'))
}

async function copy(text: string) {
  if (await copyText(text)) toast.success('已复制')
  else toast.error('复制失败，请手动选择后按 Ctrl/⌘+C')
}

function keyPlain(k: BuyerKey): string {
  return revealed[k.id]?.key ?? ''
}

/** 取回明文：优先用缓存，没有就向控制面要一次（接口返回 no-store，明文只留在内存里）。 */
async function plainOf(k: BuyerKey): Promise<string> {
  const cached = revealed[k.id]
  if (cached) return cached.key
  if (revealing.value) return ''
  revealing.value = k.id
  try {
    const res = await buyer.keySecret(k.id)
    revealed[k.id] = { key: res.key, platform: k.platform ?? '' }
    return res.key
  } catch (e) {
    toast.error(e)
    return ''
  } finally {
    revealing.value = 0
  }
}

function hide(k: BuyerKey) {
  delete revealed[k.id]
}

async function copyKey(k: BuyerKey) {
  const text = await plainOf(k)
  if (text) await copy(text)
}

/** 该 Key 的调用示例：按首跳平台取协议与模型名。 */
function sample(platform: string, key: string) {
  return sampleRequest(platform, baseURL.value, key, cfg.config.price_models ?? [])
}

const createdSample = computed(() =>
  created.value ? sample(created.value.platform, created.value.key) : null,
)

</script>

<template>
  <div>
    <PageHeader title="API Key" description="先在市场把商家加入清单，再选择清单内的挂单创建 Key。挂单按排列顺序优先调用，也可添加平台默认分组兜底。订阅套餐从期内额度扣，未用完可按标签退货。">
      <RouterLink to="/buyer/merchants" class="xm-btn xm-btn-ghost">商家清单</RouterLink>
      <button type="button" class="xm-btn xm-btn-primary" @click="openCreate">创建 Key</button>
    </PageHeader>

    <div v-if="(packs.data.value ?? []).length" class="xm-pack-list">
      <h2>我的订阅套餐</h2>
      <div v-for="p in packs.data.value" :key="p.id" class="xm-pack-row">
        <span class="xm-tag pack">订阅套餐</span>
        <span class="font-semibold">#{{ p.listing_id }}</span>
        <span>面额 ¥{{ p.face_value.toFixed(0) }} · 剩余 <b style="color:#ff5000">¥{{ p.remaining.toFixed(2) }}</b></span>
        <span style="color:#888">{{ p.status }}</span>
        <button
          v-if="p.status === 'active' && p.remaining > 0"
          type="button"
          class="xm-btn xm-btn-dark xm-btn-sm"
          :disabled="busy"
          @click="refundPack(p)"
        >
          退货
        </button>
      </div>
    </div>

    <div class="xm-edge-box">
      <div class="lab">调用地址 · Base URL</div>
      <code>{{ baseURL }}</code>
      <p>兼容官方协议。把 SDK Base URL 换成上面地址，Key 换成下方创建的 Key。失败不计费；余额不足时请求会被拒绝。</p>
      <button type="button" class="xm-btn xm-btn-sm xm-btn-primary" style="margin-top:12px" @click="copy(baseURL)">复制地址</button>
    </div>

    <div class="xm-yellow-callout">
      店铺活动包自动抵扣已有 Key 的适用调用，额度不足的部分扣平台余额。订阅套餐购买会账面预付套餐价；调用从期内额度扣，未用完可退货。
    </div>

    <div v-if="keys.loading.value && !keys.data.value?.length" class="py-10 text-center text-[#888]">加载中…</div>
    <div v-else-if="!keys.data.value?.length" class="xm-key-card py-10 text-center text-[#888]">还没有 Key，先创建一个或去市场选用闲置TOKEN。</div>
    <div v-else class="xm-card-list">
      <div v-for="k in keys.data.value ?? []" :key="k.id" class="xm-key-card">
        <div class="xm-key-top">
          <span class="name">{{ k.name }}</span>
          <span class="prefix">{{ k.key_prefix }}…</span>
          <span class="xm-tag" :class="k.status === 'active' ? 'on' : 'off'">{{ k.status === 'active' ? '使用中' : k.status }}</span>
          <span v-if="packForKey(k.id)" class="xm-tag pack">订阅套餐</span>
          <span v-if="k.limit_blocked_until" class="xm-tag warn">已限额</span>
          <div class="xm-key-actions">
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="revealing === k.id" @click="copyKey(k)">
              {{ keyPlain(k) ? '复制 Key' : '查看并复制' }}
            </button>
            <button v-if="keyPlain(k)" type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="hide(k)">隐藏</button>
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="openEdit(k)">编辑</button>
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy || k.status === 'provisioning'" @click="toggle(k)">
              {{ k.status === 'active' ? '停用' : '启用' }}
            </button>
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm xm-btn-danger" :disabled="busy" @click="remove(k)">删除</button>
          </div>
        </div>
        <div class="xm-hops">
          <template v-for="(h, i) in k.hops ?? []" :key="h.position">
            <span v-if="i > 0" class="xm-arrow">→</span>
            <span class="xm-hop">
              <span class="pos">{{ String(h.position).padStart(2, '0') }}</span>
              <span class="txt">{{ hopText(h) }}<template v-if="packForKey(k.id) && i === 0"> · 剩余 ¥{{ packForKey(k.id)!.remaining.toFixed(2) }}</template></span>
              <span v-if="h.state === 'skipped'" class="xm-tag warn" title="已移出商家清单或挂单暂不可用，不参与当前调用">已跳过</span>
              <span v-else-if="h.state === 'provisioning'" class="xm-tag warn">开通中</span>
            </span>
          </template>
        </div>
        <div v-if="keyPlain(k)" class="xm-key-plain">
          <code>{{ keyPlain(k) }}</code>
        </div>
        <div v-if="keyPlain(k)" class="xm-key-sample">
          <div class="lab">调用示例 · {{ sample(k.platform ?? '', keyPlain(k)).label }}</div>
          <pre class="codeblock">{{ sample(k.platform ?? '', keyPlain(k)).code }}</pre>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="copy(sample(k.platform ?? '', keyPlain(k)).code)">复制示例</button>
          <p class="hint">{{ sample(k.platform ?? '', keyPlain(k)).note }}</p>
        </div>
        <div v-else-if="k.recoverable === false" class="xm-meta">
          <span>这个 Key 创建较早，平台没有保存明文，无法再次查看；需要明文请新建一个 Key。</span>
        </div>
        <div class="xm-meta">
          <span v-if="packForKey(k.id)">
            已用 ¥{{ (packForKey(k.id)!.face_value - packForKey(k.id)!.remaining).toFixed(2) }}
            / 面额 ¥{{ packForKey(k.id)!.face_value.toFixed(0) }}
          </span>
          <span v-else-if="k.limits.quota_usd">总额度 {{ money(k.limits.quota_usd) }}</span>
          <span v-if="k.limits.rate_limit_5h_usd">5h {{ money(k.limits.rate_limit_5h_usd) }}</span>
          <span v-if="k.limit_blocked_until">限额至 {{ dateTime(k.limit_blocked_until) }}</span>
          <span>创建于 {{ dateTime(k.created_at) }}</span>
        </div>
      </div>
    </div>

    <ModalDialog v-model:open="showForm" :title="editing ? '编辑 Key' : '创建 Key'" wide>
      <div>
        <label class="label" for="kname">名称</label>
        <input id="kname" v-model="form.name" class="input" maxlength="100" placeholder="例如 claude-code" />
      </div>
      <div>
        <div class="label">路由</div>
        <p class="hint mb-3">只可选择商家清单内当前可用的挂单。<RouterLink to="/buyer/merchants" class="underline">管理商家清单</RouterLink> · <RouterLink to="/" class="underline">去市场挑商家</RouterLink></p>
        <p v-if="listingsLoading" role="status" class="hint mb-2">正在加载商家挂单…</p>
        <p v-else-if="listingsError" role="alert" class="mb-2 text-sm text-red-700">商家挂单加载失败。<button type="button" class="underline" @click="loadMerchantListings">重试</button></p>
        <p v-else-if="!listings.length" class="hint mb-2">清单里暂无可用挂单，可先加入商家或使用平台默认分组。</p>
        <RouteEditor v-model:type="form.type" v-model:pool="form.pool" v-model:hops="form.hops" :listings="listings" :pools="pools" :max-hops="cfg.config.max_hops" />
      </div>
      <details class="border border-ink-200 bg-paper/60 p-3">
        <summary class="cursor-pointer text-sm font-medium text-slate-700">限制（可选）</summary>
        <div class="mt-3 grid grid-cols-2 gap-3">
          <div>
            <label class="label" for="quota">总花费上限（元）</label>
            <input id="quota" v-model="form.quota" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="h5">每 5 小时上限</label>
            <input id="h5" v-model="form.h5" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="d1">每天上限</label>
            <input id="d1" v-model="form.d1" class="input" inputmode="decimal" />
          </div>
          <div>
            <label class="label" for="d7">每 7 天上限</label>
            <input id="d7" v-model="form.d7" class="input" inputmode="decimal" />
          </div>
          <div class="col-span-2">
            <label class="label" for="ips">IP 白名单（逗号分隔，支持 CIDR）</label>
            <input id="ips" v-model="form.ips" class="input" placeholder="203.0.113.0/24, 198.51.100.7" />
          </div>
          <div>
            <label class="label" for="exp">到期日</label>
            <DateField id="exp" v-model="form.expires" placeholder="可选到期日" />
          </div>
        </div>
        <p class="hint">花费限额按已入账用量统计，有几分钟延迟。</p>
      </details>
      <template #footer>
        <button class="btn-secondary" @click="showForm = false">取消</button>
        <button class="btn-primary" :disabled="busy || (form.type === 'chain' && listingsLoading)" @click="save">{{ editing ? '保存' : '创建' }}</button>
      </template>
    </ModalDialog>

    <ModalDialog :open="!!created" title="Key 已创建" @update:open="(v: boolean) => !v && (created = null)">
      <p class="text-sm text-amber-700">稍后也可以在买家中心再次查看和复制这个 Key。</p>
      <div class="flex items-center gap-2">
        <code class="flex-1 break-all rounded-xl border border-black/10 !bg-[#fafafa] !px-3 !py-2 text-sm">{{ created?.key }}</code>
        <button class="btn-secondary btn-sm" @click="copy(created!.key)">复制</button>
      </div>
      <div v-if="createdSample">
        <div class="label">调用示例 · {{ createdSample.label }}</div>
        <pre class="codeblock">{{ createdSample.code }}</pre>
        <button class="btn-secondary btn-sm" @click="copy(createdSample.code)">复制示例</button>
        <p class="hint">{{ createdSample.note }}</p>
      </div>
      <template #footer>
        <button class="btn-primary" @click="created = null">关闭</button>
      </template>
    </ModalDialog>
  </div>
</template>
