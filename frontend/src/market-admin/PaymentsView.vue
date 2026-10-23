<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

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
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}

function fenYuan(fen?: number) {
  if (fen == null || !Number.isFinite(fen)) return '—'
  return (fen / 100).toFixed(2)
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
  if (!window.confirm(`${next ? '启用' : '停用'}支付通道 ${provider}？`)) return
  await run(`${provider} 已${next ? '启用' : '停用'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/payments/merchants/${provider}`, { enabled: next }),
  )
}

onMounted(load)
</script>

<template>
  <section id="market-payments-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">支付配置</h2>
        <p class="y-page-desc">通知地址、商户密钥与通道开关；私钥保存后不再回显</p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'basic' }" @click="tab = 'basic'">基础设置</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'alipay' }" @click="tab = 'alipay'">支付宝</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'wechat' }" @click="tab = 'wechat'">微信支付</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'channels' }" @click="tab = 'channels'">通道状态</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'notifications' }" @click="tab = 'notifications'">
        最近通知
      </button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'basic'">
        <div v-if="!st" class="y-msg">无法加载设置</div>
        <div v-else class="pay-form">
          <label class="y-field y-field--wide">
            <span>支付通知地址（对外公网 https）</span>
            <input v-model="st.payment_public_url" class="input w-full" :placeholder="origin" />
            <span class="hint">
              通知发到 {{ notifyBase }}/api/payments/alipay/notify 与 …/wechat/notify
              <button v-if="!st.payment_public_url" type="button" class="y-link" @click="st.payment_public_url = origin">
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
            <button type="button" class="y-btn" :disabled="busy" @click="saveBasic">保存</button>
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
            <button type="button" class="y-btn y-btn--ghost" :disabled="busy || !merchants.alipay?.ready" @click="test('alipay')">
              测试连接
            </button>
            <button type="button" class="y-btn" :disabled="busy" @click="saveAlipay">保存</button>
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
              <label class="y-check"><input v-model="wx.verify" type="radio" value="public_key" /> 微信支付公钥</label>
              <label class="y-check"><input v-model="wx.verify" type="radio" value="platform_cert" /> 平台证书</label>
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
            <button type="button" class="y-btn y-btn--ghost" :disabled="busy || !merchants.wechat?.ready" @click="test('wechat')">
              测试连接
            </button>
            <button type="button" class="y-btn" :disabled="busy" @click="saveWechat">保存</button>
          </div>
        </div>
      </template>

      <template v-else-if="tab === 'channels'">
        <table>
          <thead>
            <tr>
              <th>通道</th>
              <th>状态</th>
              <th>就绪</th>
              <th>更新时间</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(m, key) in merchants" :key="key">
              <td>{{ m.provider || key }}</td>
              <td>
                <span :class="m.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ m.enabled ? '启用' : '停用' }}</span>
              </td>
              <td>{{ m.ready ? '是' : '否' }}</td>
              <td>{{ m.updated_at || '—' }}</td>
              <td class="y-col-op">
                <button type="button" class="y-link" :disabled="busy" @click="toggleChannel(String(m.provider || key))">
                  {{ m.enabled ? '停用' : '启用' }}
                </button>
                <button
                  type="button"
                  class="y-link"
                  :disabled="busy || !m.ready"
                  @click="test(String(m.provider || key))"
                >
                  测试
                </button>
              </td>
            </tr>
            <tr v-if="!Object.keys(merchants).length">
              <td colspan="5">暂无商户</td>
            </tr>
          </tbody>
        </table>
      </template>

      <template v-else>
        <p v-if="!notifications.length" class="y-msg">暂无支付回调通知</p>
        <table v-else>
          <thead>
            <tr>
              <th>时间</th>
              <th>渠道</th>
              <th>商户单号</th>
              <th>渠道单号</th>
              <th>金额</th>
              <th>结果</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in notifications" :key="n.id">
              <td>{{ fmtTime(n.created_at) }}</td>
              <td>{{ n.provider }}</td>
              <td class="mono">{{ n.out_trade_no || '—' }}</td>
              <td class="mono">{{ n.trade_no || '—' }}</td>
              <td>{{ fenYuan(n.amount_fen) }}</td>
              <td>
                <span :class="n.result === 'ok' || n.result === 'success' ? 'y-chip y-chip--ok' : 'y-chip y-chip--err'">
                  {{ n.result }}{{ n.error ? ` · ${n.error}` : '' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </template>
    </div>
  </section>
</template>

<style scoped>
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
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
</style>
