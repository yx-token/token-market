<script setup lang="ts">
/**
 * 支付配置：TablePageLayout；通道/通知用 DataTable + filter-grid，表单 Tab 保留原逻辑。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminPayments' })

interface MerchantView {
  provider: string
  enabled: boolean
  alipay?: Record<string, string>
  wechat?: Record<string, string>
  has_private_key?: boolean
  has_api_v3_key?: boolean
  ready?: boolean
  problem?: string
  updated_at?: string
}

interface Settings {
  payment_public_url: string
  payment_channels: Record<string, boolean>
  recharge_min: number
  recharge_max: number
  recharge_order_timeout_minutes: number
  refund_original_route: boolean
  refund_original_route_days: number
}

interface PaymentNotification {
  id: number
  provider: string
  out_trade_no?: string
  trade_no?: string
  state?: string
  amount_fen?: number
  result: string
  error?: string
  created_at: string
}

interface PaymentStatus {
  channels?: unknown[]
  public_url?: string
  notifications?: PaymentNotification[]
}

const GATEWAY_PROD = 'https://openapi.alipay.com/gateway.do'
const GATEWAY_SANDBOX = 'https://openapi-sandbox.dl.alipaydev.com/gateway.do'

const channelNames: Record<string, string> = {
  alipay_page: '支付宝电脑网站支付',
  alipay_f2f: '支付宝当面付（扫码）',
  wechat_native: '微信支付 Native（扫码）',
  manual: '人工确认到账',
}

const PROVIDER_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'alipay', label: '支付宝' },
  { value: 'wechat', label: '微信支付' },
]

const RESULT_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'ok', label: '成功' },
  { value: 'fail', label: '失败' },
]

const CHANNEL_STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'enabled', label: '启用' },
  { value: 'disabled', label: '停用' },
]

const CHANNEL_READY_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'ready', label: '已就绪' },
  { value: 'not_ready', label: '未就绪' },
]

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  provider: '',
  result: '',
  channel_status: '',
  channel_ready: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const tab = ref<'basic' | 'alipay' | 'wechat' | 'channels' | 'notifications'>('basic')
const st = ref<Settings | null>(null)
const merchants = ref<Record<string, MerchantView>>({})
const notifications = ref<PaymentNotification[]>([])
const busy = ref(false)
const loading = ref(false)
const error = ref('')
const msg = ref('')

const ali = reactive({
  enabled: false,
  appID: '',
  gatewayKind: 'prod',
  gateway: '',
  mode: 'public_key',
  publicKey: '',
  appCert: '',
  alipayCert: '',
  rootCert: '',
  privateKey: '',
})
const wx = reactive({
  enabled: false,
  appID: '',
  mchID: '',
  serial: '',
  verify: 'public_key',
  publicKeyID: '',
  publicKey: '',
  baseURL: '',
  privateKey: '',
  apiV3Key: '',
})

const origin = typeof window !== 'undefined' ? window.location.origin : ''
const notifyBase = computed(() => st.value?.payment_public_url || '（未填写）')
const isListTab = computed(() => tab.value === 'channels' || tab.value === 'notifications')

const channelColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'provider', label: '通道', class: 'col-provider' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'ready', label: '就绪', class: 'col-ready' },
  { key: 'updated_at', label: '更新时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const notifyColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'created_at', label: '时间', class: 'col-time' },
  { key: 'provider', label: '渠道', class: 'col-provider' },
  { key: 'out_trade_no', label: '商户单号', class: 'col-trade' },
  { key: 'trade_no', label: '渠道单号', class: 'col-trade' },
  { key: 'amount', label: '金额', class: 'col-amt' },
  { key: 'result', label: '结果', class: 'col-result' },
]

const columns = computed(() => (tab.value === 'channels' ? channelColumns : notifyColumns))

const merchantRows = computed(() =>
  Object.entries(merchants.value).map(([key, m]) => ({
    ...m,
    _key: key,
    provider: m.provider || key,
  })),
)

const filteredChannels = computed(() => {
  let list = merchantRows.value
  if (queryParams.value.channel_status === 'enabled') list = list.filter((m) => m.enabled)
  if (queryParams.value.channel_status === 'disabled') list = list.filter((m) => !m.enabled)
  if (queryParams.value.channel_ready === 'ready') list = list.filter((m) => !!m.ready)
  if (queryParams.value.channel_ready === 'not_ready') list = list.filter((m) => !m.ready)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((m) =>
      [m.provider, m._key, m.problem, m.enabled ? '启用' : '停用']
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const filteredNotifications = computed(() => {
  let list = notifications.value
  if (queryParams.value.provider) {
    list = list.filter((n) => n.provider === queryParams.value.provider)
  }
  if (queryParams.value.result === 'ok') {
    list = list.filter((n) => n.result === 'ok' || n.result === 'success')
  }
  if (queryParams.value.result === 'fail') {
    list = list.filter((n) => n.result !== 'ok' && n.result !== 'success')
  }
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((n) =>
      [n.id, n.provider, n.out_trade_no, n.trade_no, n.result, n.error, n.state]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const pageSource = computed(() =>
  tab.value === 'channels' ? filteredChannels.value : filteredNotifications.value,
)
const total = computed(() => (isListTab.value ? pageSource.value.length : 0))
const pageRows = computed(() => {
  if (!isListTab.value) return []
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return pageSource.value.slice(start, start + queryParams.value.pageSize)
})

const channelStats = computed(() => {
  const all = merchantRows.value
  return {
    total: all.length,
    enabled: all.filter((m) => m.enabled).length,
    ready: all.filter((m) => !!m.ready).length,
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.provider,
    queryParams.value.result,
    queryParams.value.channel_status,
    queryParams.value.channel_ready,
    queryParams.value.pageSize,
    tab.value,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function stateText(m?: MerchantView) {
  if (!m || !m.updated_at) return { text: '未配置', ok: false, warn: false }
  if (!m.ready) return { text: '配置不完整', ok: false, warn: true }
  if (!m.enabled) return { text: '已停用', ok: false, warn: false }
  return { text: '已启用', ok: true, warn: false }
}

function fillMerchants(m: Record<string, MerchantView>) {
  merchants.value = m
  const a = m.alipay?.alipay
  if (a) {
    const kind = !a.gateway || a.gateway === GATEWAY_PROD ? 'prod' : a.gateway === GATEWAY_SANDBOX ? 'sandbox' : 'custom'
    Object.assign(ali, {
      enabled: m.alipay.enabled,
      appID: a.app_id || '',
      gatewayKind: kind,
      gateway: kind === 'custom' ? a.gateway : '',
      mode: a.mode || 'public_key',
      publicKey: a.alipay_public_key || '',
      appCert: a.app_cert || '',
      alipayCert: a.alipay_cert || '',
      rootCert: a.alipay_root_cert || '',
      privateKey: '',
    })
  }
  const w = m.wechat?.wechat
  if (w) {
    Object.assign(wx, {
      enabled: m.wechat.enabled,
      appID: w.app_id || '',
      mchID: w.mch_id || '',
      serial: w.mch_serial_no || '',
      verify: w.public_key_id ? 'public_key' : 'platform_cert',
      publicKeyID: w.public_key_id || '',
      publicKey: w.public_key || '',
      baseURL: w.base_url || '',
      privateKey: '',
      apiV3Key: '',
    })
  }
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fenYuan(fen?: number) {
  if (fen == null || !Number.isFinite(fen)) return '—'
  return (fen / 100).toFixed(2)
}

function providerLabel(p: string) {
  if (p === 'alipay') return '支付宝'
  if (p === 'wechat') return '微信支付'
  return p || '—'
}

function resultOk(r: string) {
  return r === 'ok' || r === 'success'
}

function getSerialNumber(index: number) {
  return String((queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1).padStart(2, '0')
}

function rowKey(row: Record<string, unknown> | PaymentNotification | { _key?: string; id?: number }) {
  if (tab.value === 'channels') return String((row as { _key?: string })._key ?? '')
  return String((row as PaymentNotification).id ?? '')
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

function pageTitle() {
  return (
    {
      basic: '基础设置',
      alipay: '支付宝',
      wechat: '微信支付',
      channels: '通道状态',
      notifications: '最近通知',
    } as Record<string, string>
  )[tab.value]
}

function pageDesc() {
  return (
    {
      basic: '通知地址、充值限额与买家可见支付方式',
      alipay: '支付宝商户密钥与加签方式；私钥保存后不再回显',
      wechat: '微信商户证书与验签；私钥 / APIv3 密钥保存后不再回显',
      channels: '商户通道启停与连接测试',
      notifications: '支付回调通知记录',
    } as Record<string, string>
  )[tab.value]
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [s, m, status] = await Promise.all([
      marketGet<Settings>(`${MARKET_API_PREFIX}/settings`).catch(() => null),
      marketGet<Record<string, MerchantView>>(`${MARKET_API_PREFIX}/payments/merchants`).catch(() => null),
      marketGet<PaymentStatus>(`${MARKET_API_PREFIX}/payments/status`).catch(() => null),
    ])
    if (s) {
      st.value = {
        ...s,
        payment_channels: {
          alipay_page: false,
          alipay_f2f: false,
          wechat_native: false,
          manual: false,
          ...(s.payment_channels || {}),
        },
      }
    }
    if (m) fillMerchants(m)
    notifications.value = status?.notifications ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
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

async function saveBasic() {
  if (!st.value) return
  const s = st.value
  await run('基础设置已保存', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/settings`, {
      payment_public_url: s.payment_public_url.trim(),
      payment_channels: s.payment_channels,
      recharge_min: Number(s.recharge_min),
      recharge_max: Number(s.recharge_max),
      recharge_order_timeout_minutes: Number(s.recharge_order_timeout_minutes),
      refund_original_route: s.refund_original_route,
      refund_original_route_days: Number(s.refund_original_route_days),
    }),
  )
}

function alipayGateway() {
  if (ali.gatewayKind === 'sandbox') return GATEWAY_SANDBOX
  if (ali.gatewayKind === 'custom') return ali.gateway.trim()
  return ''
}

async function saveAlipay() {
  const body: Record<string, unknown> = {
    enabled: ali.enabled,
    alipay: {
      app_id: ali.appID,
      gateway: alipayGateway(),
      mode: ali.mode,
      alipay_public_key: ali.publicKey,
      app_cert: ali.appCert,
      alipay_cert: ali.alipayCert,
      alipay_root_cert: ali.rootCert,
    },
  }
  if (ali.privateKey.trim()) body.private_key = ali.privateKey
  await run('支付宝配置已保存', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/payments/merchants/alipay`, body),
  )
}

async function saveWechat() {
  const body: Record<string, unknown> = {
    enabled: wx.enabled,
    wechat: {
      app_id: wx.appID,
      mch_id: wx.mchID,
      mch_serial_no: wx.serial,
      base_url: wx.baseURL,
      public_key_id: wx.verify === 'public_key' ? wx.publicKeyID : '',
      public_key: wx.verify === 'public_key' ? wx.publicKey : '',
    },
  }
  if (wx.privateKey.trim()) body.private_key = wx.privateKey
  if (wx.apiV3Key.trim()) body.api_v3_key = wx.apiV3Key
  await run('微信支付配置已保存', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/payments/merchants/wechat`, body),
  )
}

async function test(provider: string) {
  await run('连接正常：签名、密钥与验签均通过', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/payments/merchants/${provider}/test`),
  )
}

async function toggleChannel(provider: string) {
  const m = merchants.value[provider]
  if (!m) return
  const next = !m.enabled
  if (!window.confirm(`${next ? '启用' : '停用'}支付通道 ${providerLabel(provider)}？`)) return
  await run(`${providerLabel(provider)} 已${next ? '启用' : '停用'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/payments/merchants/${provider}`, { enabled: next }),
  )
}

watch(tab, () => {
  Object.assign(queryParams.value, { ...initQueryParams })
})

onMounted(load)
</script>

<template>
  <section id="market-payments-page" class="payments-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">{{ pageTitle() }}</h2>
          <p class="y-page-desc">{{ pageDesc() }}</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>

        <nav class="y-tabs mb-3">
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'basic' }" @click="tab = 'basic'">
            基础设置
          </button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'alipay' }" @click="tab = 'alipay'">
            支付宝
          </button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'wechat' }" @click="tab = 'wechat'">
            微信支付
          </button>
          <button type="button" class="y-tab" :class="{ 'is-on': tab === 'channels' }" @click="tab = 'channels'">
            通道状态
          </button>
          <button
            type="button"
            class="y-tab"
            :class="{ 'is-on': tab === 'notifications' }"
            @click="tab = 'notifications'"
          >
            最近通知
          </button>
        </nav>

        <div v-if="tab === 'channels'" class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">通道数</div>
                <div class="stat-card__value">{{ channelStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">已启用</div>
                <div class="stat-card__value">{{ channelStats.enabled.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">已就绪</div>
                <div class="stat-card__value">{{ channelStats.ready.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div v-if="isListTab" class="section-block" :class="{ 'mt-3': tab === 'channels' }">
          <div class="section-title">筛选条件</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  :placeholder="tab === 'channels' ? '通道名称' : '单号 / 渠道 / 结果'"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div v-show="tab === 'channels'" class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.channel_status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="CHANNEL_STATUS_OPTIONS"
              />
            </div>
            <div v-show="tab === 'channels'" class="filter-item">
              <label class="filter-label">就绪</label>
              <Select
                v-model="queryParams.channel_ready"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="CHANNEL_READY_OPTIONS"
              />
            </div>
            <div v-show="tab === 'notifications'" class="filter-item">
              <label class="filter-label">渠道</label>
              <Select
                v-model="queryParams.provider"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PROVIDER_OPTIONS"
              />
            </div>
            <div v-show="tab === 'notifications'" class="filter-item">
              <label class="filter-label">结果</label>
              <Select
                v-model="queryParams.result"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="RESULT_OPTIONS"
              />
            </div>
            <div class="filter-actions lg:col-span-4 sm:col-span-2">
              <button type="button" class="btn btn-secondary" @click="resetQuery">
                <Icon name="refresh" size="sm" />
                重置
              </button>
              <button type="button" class="btn btn-primary" @click="handleQuery">
                <Icon name="search" size="sm" />
                搜索
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <div v-if="loading && !isListTab" class="y-msg">加载中…</div>

        <template v-else-if="tab === 'basic'">
          <div v-if="!st" class="y-msg">无法加载设置</div>
          <div v-else class="pay-form">
            <label class="y-field y-field--wide">
              <span>支付通知地址（对外公网 https）</span>
              <input v-model="st.payment_public_url" class="input w-full" :placeholder="origin" />
              <span class="hint">
                通知发到 {{ notifyBase }}/api/payments/alipay/notify 与 …/wechat/notify
                <button
                  v-if="!st.payment_public_url"
                  type="button"
                  class="y-link"
                  @click="st.payment_public_url = origin"
                >
                  使用当前地址
                </button>
              </span>
            </label>
            <div class="y-field y-field--wide">
              <span>对买家开放的支付方式</span>
              <div class="auth-list">
                <label v-for="(name, ch) in channelNames" :key="ch" class="y-check">
                  <input v-model="st.payment_channels[ch]" type="checkbox" /> {{ name }}
                </label>
              </div>
            </div>
            <label class="y-field">
              <span>单笔充值下限（元）</span>
              <input v-model.number="st.recharge_min" class="input w-full" type="number" step="0.01" min="0" />
            </label>
            <label class="y-field">
              <span>单笔充值上限（元）</span>
              <input v-model.number="st.recharge_max" class="input w-full" type="number" step="0.01" min="0" />
            </label>
            <label class="y-field">
              <span>在线支付时限（分钟）</span>
              <input v-model.number="st.recharge_order_timeout_minutes" class="input w-full" type="number" min="5" />
            </label>
            <label class="y-field">
              <span>原路退回期限（天）</span>
              <input v-model.number="st.refund_original_route_days" class="input w-full" type="number" min="0" />
            </label>
            <label class="y-check y-field--wide">
              <input v-model="st.refund_original_route" type="checkbox" />
              余额退款优先原路退回
            </label>
            <div class="pay-actions">
              <button type="button" class="btn btn-primary" :disabled="busy" @click="saveBasic">保存</button>
            </div>
          </div>
        </template>

        <template v-else-if="tab === 'alipay'">
          <div class="provider-head">
            <span
              :class="
                stateText(merchants.alipay).ok
                  ? 'y-chip y-chip--ok'
                  : stateText(merchants.alipay).warn
                    ? 'y-chip y-chip--warn'
                    : 'y-chip'
              "
            >
              {{ stateText(merchants.alipay).text }}
            </span>
            <span v-if="merchants.alipay?.problem" class="hint warn">{{ merchants.alipay.problem }}</span>
          </div>
          <div class="pay-form">
            <label class="y-check y-field--wide">
              <input v-model="ali.enabled" type="checkbox" /> 启用支付宝
            </label>
            <label class="y-field">
              <span>应用 APPID</span>
              <input v-model="ali.appID" class="input w-full" />
            </label>
            <label class="y-field">
              <span>网关</span>
              <select v-model="ali.gatewayKind" class="input w-full">
                <option value="prod">正式环境</option>
                <option value="sandbox">沙箱环境</option>
                <option value="custom">自定义</option>
              </select>
            </label>
            <label v-if="ali.gatewayKind === 'custom'" class="y-field y-field--wide">
              <span>自定义网关</span>
              <input v-model="ali.gateway" class="input w-full" placeholder="https://…/gateway.do" />
            </label>
            <div class="y-field y-field--wide">
              <span>接口加签方式</span>
              <div class="auth-list">
                <label class="y-check"><input v-model="ali.mode" type="radio" value="public_key" /> 公钥</label>
                <label class="y-check"><input v-model="ali.mode" type="radio" value="cert" /> 公钥证书</label>
              </div>
            </div>
            <label class="y-field y-field--wide">
              <span>应用私钥</span>
              <textarea
                v-model="ali.privateKey"
                class="input w-full y-textarea font-mono"
                rows="3"
                :placeholder="merchants.alipay?.has_private_key ? '已设置，留空不修改' : '粘贴 PEM'"
              />
            </label>
            <label v-if="ali.mode === 'public_key'" class="y-field y-field--wide">
              <span>支付宝公钥</span>
              <textarea v-model="ali.publicKey" class="input w-full y-textarea font-mono" rows="3" />
            </label>
            <template v-else>
              <label class="y-field y-field--wide">
                <span>应用公钥证书</span>
                <textarea v-model="ali.appCert" class="input w-full y-textarea font-mono" rows="2" />
              </label>
              <label class="y-field y-field--wide">
                <span>支付宝公钥证书</span>
                <textarea v-model="ali.alipayCert" class="input w-full y-textarea font-mono" rows="2" />
              </label>
              <label class="y-field y-field--wide">
                <span>支付宝根证书</span>
                <textarea v-model="ali.rootCert" class="input w-full y-textarea font-mono" rows="2" />
              </label>
            </template>
            <div class="pay-actions">
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="busy || !merchants.alipay?.ready"
                @click="test('alipay')"
              >
                测试连接
              </button>
              <button type="button" class="btn btn-primary" :disabled="busy" @click="saveAlipay">保存</button>
            </div>
          </div>
        </template>

        <template v-else-if="tab === 'wechat'">
          <div class="provider-head">
            <span
              :class="
                stateText(merchants.wechat).ok
                  ? 'y-chip y-chip--ok'
                  : stateText(merchants.wechat).warn
                    ? 'y-chip y-chip--warn'
                    : 'y-chip'
              "
            >
              {{ stateText(merchants.wechat).text }}
            </span>
            <span v-if="merchants.wechat?.problem" class="hint warn">{{ merchants.wechat.problem }}</span>
          </div>
          <div class="pay-form">
            <label class="y-check y-field--wide">
              <input v-model="wx.enabled" type="checkbox" /> 启用微信支付
            </label>
            <label class="y-field">
              <span>AppID</span>
              <input v-model="wx.appID" class="input w-full" />
            </label>
            <label class="y-field">
              <span>商户号</span>
              <input v-model="wx.mchID" class="input w-full" />
            </label>
            <label class="y-field">
              <span>商户 API 证书序列号</span>
              <input v-model="wx.serial" class="input w-full" />
            </label>
            <label class="y-field y-field--wide">
              <span>商户 API 私钥</span>
              <textarea
                v-model="wx.privateKey"
                class="input w-full y-textarea font-mono"
                rows="3"
                :placeholder="merchants.wechat?.has_private_key ? '已设置，留空不修改' : '粘贴 apiclient_key.pem'"
              />
            </label>
            <label class="y-field">
              <span>APIv3 密钥</span>
              <input
                v-model="wx.apiV3Key"
                class="input w-full"
                type="password"
                autocomplete="off"
                :placeholder="merchants.wechat?.has_api_v3_key ? '已设置，留空不修改' : '32 个字符'"
              />
            </label>
            <div class="y-field y-field--wide">
              <span>验签方式</span>
              <div class="auth-list">
                <label class="y-check">
                  <input v-model="wx.verify" type="radio" value="public_key" /> 微信支付公钥
                </label>
                <label class="y-check">
                  <input v-model="wx.verify" type="radio" value="platform_cert" /> 平台证书
                </label>
              </div>
            </div>
            <template v-if="wx.verify === 'public_key'">
              <label class="y-field">
                <span>公钥 ID</span>
                <input v-model="wx.publicKeyID" class="input w-full" placeholder="PUB_KEY_ID_…" />
              </label>
              <label class="y-field y-field--wide">
                <span>微信支付公钥</span>
                <textarea v-model="wx.publicKey" class="input w-full y-textarea font-mono" rows="3" />
              </label>
            </template>
            <label class="y-field y-field--wide">
              <span>接口地址（可选）</span>
              <input v-model="wx.baseURL" class="input w-full" placeholder="https://api.mch.weixin.qq.com" />
            </label>
            <div class="pay-actions">
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="busy || !merchants.wechat?.ready"
                @click="test('wechat')"
              >
                测试连接
              </button>
              <button type="button" class="btn btn-primary" :disabled="busy" @click="saveWechat">保存</button>
            </div>
          </div>
        </template>

        <DataTable
          v-else
          :columns="columns"
          :data="pageRows"
          :loading="loading"
          :expandable-actions="false"
        >
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => rowKey(r) === rowKey(row))) }}
          </template>
          <template #cell-provider="{ row }">{{ providerLabel(row.provider) }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.enabled ? 'ok' : 'idle'" />
              {{ row.enabled ? '启用' : '停用' }}
            </span>
          </template>
          <template #cell-ready="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="row.ready ? 'ok' : 'err'" />
              {{ row.ready ? '是' : '否' }}
            </span>
            <div v-if="row.problem" class="row-sub warn">{{ row.problem }}</div>
          </template>
          <template #cell-updated_at="{ row }">{{ fmtTime(row.updated_at) }}</template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-out_trade_no="{ row }">
            <span class="mono-cell" :title="row.out_trade_no || ''">{{ row.out_trade_no || '—' }}</span>
          </template>
          <template #cell-trade_no="{ row }">
            <span class="mono-cell" :title="row.trade_no || ''">{{ row.trade_no || '—' }}</span>
          </template>
          <template #cell-amount="{ row }">{{ fenYuan(row.amount_fen) }}</template>
          <template #cell-result="{ row }">
            <span :class="resultOk(row.result) ? 'y-chip y-chip--ok' : 'y-chip y-chip--err'">
              {{ row.result }}{{ row.error ? ` · ${row.error}` : '' }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="toggleChannel(String(row.provider || row._key))">
                {{ row.enabled ? '停用' : '启用' }}
              </button>
              <button
                type="button"
                class="y-link"
                :disabled="busy || !row.ready"
                @click="test(String(row.provider || row._key))"
              >
                测试
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              :title="tab === 'channels' ? '暂无商户通道' : '暂无支付回调通知'"
              description="调整筛选条件后重试，或点击刷新重新加载"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="isListTab && total > 0"
          :total="total"
          :page="queryParams.pageNum"
          :page-size="queryParams.pageSize"
          :page-size-options="[15, 20, 50]"
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>
  </section>
</template>

<style scoped>
#market-payments-page.payments-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.page-heading {
  min-width: 0;
}

.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.section-block {
  min-width: 0;
}

.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  color: var(--color-text-primary, #262626);
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.stat-card {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}

.stat-card--blue {
  background: linear-gradient(135deg, #eff6ff, #f8fafc);
}
.stat-card--green {
  background: linear-gradient(135deg, #ecfdf5, #f8fafc);
}
.stat-card--cyan {
  background: linear-gradient(135deg, #ecfeff, #f8fafc);
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.2;
}

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

.row-sub {
  margin-top: 2px;
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
}

.row-sub.warn {
  color: var(--color-warning, #faad14);
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}
.status-dot[data-tone='ok'] {
  background: #16a34a;
}
.status-dot[data-tone='err'] {
  background: #ef4444;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

.pay-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 12px;
  padding: 8px 0 16px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field > span:first-child {
  color: var(--color-text-secondary, #868686);
}
.y-field--wide {
  grid-column: 1 / -1;
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.hint {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.hint.warn {
  color: var(--color-warning, #faad14);
}
.pay-actions {
  grid-column: 1 / -1;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.provider-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}
.y-textarea {
  height: auto !important;
  min-height: 72px;
  padding: 8px 10px;
  resize: vertical;
}

#market-payments-page :deep(.filter-grid .select-trigger),
#market-payments-page :deep(.filter-grid .select-trigger-sm),
#market-payments-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-payments-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-payments-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-payments-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-payments-page :deep(th.col-provider),
#market-payments-page :deep(td.col-provider) {
  min-width: 7rem;
}
#market-payments-page :deep(th.col-status),
#market-payments-page :deep(td.col-status),
#market-payments-page :deep(th.col-ready),
#market-payments-page :deep(td.col-ready),
#market-payments-page :deep(th.col-amt),
#market-payments-page :deep(td.col-amt) {
  min-width: 5rem;
}
#market-payments-page :deep(th.col-trade),
#market-payments-page :deep(td.col-trade) {
  min-width: 10rem;
  max-width: 14rem;
}
#market-payments-page :deep(th.col-result),
#market-payments-page :deep(td.col-result) {
  min-width: 8rem;
}
#market-payments-page :deep(th.col-time),
#market-payments-page :deep(td.col-time) {
  min-width: 9rem;
}
#market-payments-page :deep(th.col-ops),
#market-payments-page :deep(td.col-ops) {
  min-width: 8rem;
  width: 8rem;
  box-sizing: border-box;
}
#market-payments-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-payments-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-payments-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

@media (max-width: 900px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
}
</style>
