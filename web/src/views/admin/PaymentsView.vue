<script setup lang="ts">
// 支付配置：通知地址与支付方式开关、支付宝应用、微信支付商户、渠道状态与通知记录。
// 商户私钥与 APIv3 密钥加密保存在数据库中，保存后不再显示；修改后约 5 秒内对全部服务生效。
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { MerchantView, PaymentStatus, Settings } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import KeyField from '@/components/KeyField.vue'
import { label } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const GATEWAY_PROD = 'https://openapi.alipay.com/gateway.do'
const GATEWAY_SANDBOX = 'https://openapi-sandbox.dl.alipaydev.com/gateway.do'

const { busy, run } = useAction()
const status = useLoader(() => admin.paymentStatus(), null as PaymentStatus | null)
const st = ref<Settings | null>(null)
const merchants = ref<Record<string, MerchantView>>({})

const channelNames: Record<string, string> = {
  alipay_page: '支付宝电脑网站支付',
  alipay_f2f: '支付宝当面付（扫码）',
  wechat_native: '微信支付 Native（扫码）',
  manual: '人工确认到账',
}

const ali = reactive({ enabled: false, appID: '', gatewayKind: 'prod', gateway: '', mode: 'public_key', publicKey: '', appCert: '', alipayCert: '', rootCert: '', privateKey: '' })
const wx = reactive({ enabled: false, appID: '', mchID: '', serial: '', verify: 'public_key', publicKeyID: '', publicKey: '', baseURL: '', privateKey: '', apiV3Key: '' })

function fillMerchants(m: Record<string, MerchantView>) {
  merchants.value = m
  const a = m.alipay?.alipay
  if (a) {
    const kind = !a.gateway || a.gateway === GATEWAY_PROD ? 'prod' : a.gateway === GATEWAY_SANDBOX ? 'sandbox' : 'custom'
    Object.assign(ali, { enabled: m.alipay.enabled, appID: a.app_id, gatewayKind: kind, gateway: kind === 'custom' ? a.gateway : '', mode: a.mode || 'public_key',
      publicKey: a.alipay_public_key, appCert: a.app_cert, alipayCert: a.alipay_cert, rootCert: a.alipay_root_cert, privateKey: '' })
  }
  const w = m.wechat?.wechat
  if (w) {
    Object.assign(wx, { enabled: m.wechat.enabled, appID: w.app_id, mchID: w.mch_id, serial: w.mch_serial_no, verify: w.public_key_id ? 'public_key' : 'platform_cert',
      publicKeyID: w.public_key_id, publicKey: w.public_key, baseURL: w.base_url, privateKey: '', apiV3Key: '' })
  }
}

async function load() {
  status.load()
  const [s, m] = await Promise.all([admin.settings().catch(() => null), admin.merchants().catch(() => null)])
  if (s) {
    s.payment_channels = { alipay_page: false, alipay_f2f: false, wechat_native: false, manual: false, ...s.payment_channels }
    st.value = s
  }
  if (m) fillMerchants(m)
}
onMounted(load)

const origin = window.location.origin
const notifyBase = computed(() => st.value?.payment_public_url || '（未填写）')

async function saveBasic() {
  if (!st.value) return
  const s = st.value
  const patch: Partial<Settings> = {
    payment_public_url: s.payment_public_url.trim(),
    payment_channels: s.payment_channels,
    recharge_min: Number(s.recharge_min),
    recharge_max: Number(s.recharge_max),
    recharge_order_timeout_minutes: Number(s.recharge_order_timeout_minutes),
    refund_original_route: s.refund_original_route,
    refund_original_route_days: Number(s.refund_original_route_days),
  }
  const res = await run(() => admin.updateSettings(patch), '已保存')
  if (res) load()
}

function alipayGateway() {
  if (ali.gatewayKind === 'sandbox') return GATEWAY_SANDBOX
  if (ali.gatewayKind === 'custom') return ali.gateway.trim()
  return ''
}

async function saveAlipay() {
  const body: Record<string, unknown> = {
    enabled: ali.enabled,
    alipay: { app_id: ali.appID, gateway: alipayGateway(), mode: ali.mode, alipay_public_key: ali.publicKey, app_cert: ali.appCert,
      alipay_cert: ali.alipayCert, alipay_root_cert: ali.rootCert },
  }
  if (ali.privateKey.trim()) body.private_key = ali.privateKey
  const res = await run(() => admin.updateMerchant('alipay', body), '支付宝配置已保存')
  if (res) load()
}

async function saveWechat() {
  const body: Record<string, unknown> = {
    enabled: wx.enabled,
    wechat: { app_id: wx.appID, mch_id: wx.mchID, mch_serial_no: wx.serial, base_url: wx.baseURL,
      public_key_id: wx.verify === 'public_key' ? wx.publicKeyID : '', public_key: wx.verify === 'public_key' ? wx.publicKey : '' },
  }
  if (wx.privateKey.trim()) body.private_key = wx.privateKey
  if (wx.apiV3Key.trim()) body.api_v3_key = wx.apiV3Key
  const res = await run(() => admin.updateMerchant('wechat', body), '微信支付配置已保存')
  if (res) load()
}

async function test(provider: string) {
  await run(() => admin.testMerchant(provider), '连接正常：签名、密钥与验签均通过')
}

function stateText(m?: MerchantView) {
  if (!m || !m.updated_at) return { text: '未配置', cls: 'chip-gray' }
  if (!m.ready) return { text: '配置不完整', cls: 'chip-yellow' }
  if (!m.enabled) return { text: '已停用', cls: 'chip-gray' }
  return { text: '已启用', cls: 'chip-green' }
}
</script>

<template>
  <div>
    <PageHeader title="支付配置" description="支付宝、微信支付商户在这里配置并即时生效；私钥与 APIv3 密钥加密保存，保存后不再显示。" />

    <div v-if="st" class="card mb-5">
      <h2 class="card-title">基础设置</h2>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="md:col-span-2">
          <label class="label" for="pub">支付通知地址（控制面对外地址）</label>
          <input id="pub" v-model="st.payment_public_url" class="input" :placeholder="origin" />
          <p class="hint">
            支付宝、微信支付的异步通知发到 <code>{{ notifyBase }}/api/payments/alipay/notify</code> 与 <code>…/wechat/notify</code>，必须是公网可访问的 https
            地址；网页支付完成后也会跳回这个地址的钱包页。
            <button v-if="!st.payment_public_url" type="button" class="link" @click="st.payment_public_url = origin">使用当前地址</button>
          </p>
        </div>
        <div class="md:col-span-2">
          <div class="label">对买家开放的支付方式</div>
          <div class="flex flex-wrap gap-4">
            <label v-for="(name, ch) in channelNames" :key="ch" class="flex items-center gap-2 text-sm">
              <input v-model="st.payment_channels[ch]" type="checkbox" /> {{ name }}
            </label>
          </div>
          <p class="hint">在线支付方式还需要下方对应商户已启用才可用。</p>
        </div>
        <div>
          <label class="label" for="rmin">单笔充值下限（元）</label>
          <input id="rmin" v-model="st.recharge_min" class="input" type="number" step="0.01" min="0" />
        </div>
        <div>
          <label class="label" for="rmax">单笔充值上限（元）</label>
          <input id="rmax" v-model="st.recharge_max" class="input" type="number" step="0.01" min="0" />
        </div>
        <div>
          <label class="label" for="rto">在线支付时限（分钟）</label>
          <input id="rto" v-model="st.recharge_order_timeout_minutes" class="input" type="number" min="5" />
          <p class="hint">超时未支付的订单向渠道关单</p>
        </div>
        <div>
          <label class="label" for="rdays">原路退回期限（天）</label>
          <input id="rdays" v-model="st.refund_original_route_days" class="input" type="number" min="0" />
          <p class="hint">只原路退回这么多天内的充值单，需不超过渠道的可退款期限</p>
        </div>
        <label class="flex items-center gap-2 text-sm md:col-span-2">
          <input v-model="st.refund_original_route" type="checkbox" /> 余额退款优先原路退回（支付宝 / 微信支付），其余人工打款
        </label>
      </div>
      <div class="mt-3 flex justify-end"><button class="btn-primary" :disabled="busy" @click="saveBasic">保存</button></div>
    </div>

    <div class="card mb-5">
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <h2 class="card-title !mb-0">支付宝</h2>
        <span :class="['chip', stateText(merchants.alipay).cls]">{{ stateText(merchants.alipay).text }}</span>
        <span v-if="merchants.alipay?.problem && merchants.alipay?.updated_at" class="text-xs text-amber-700">{{ merchants.alipay.problem }}</span>
        <span v-if="merchants.alipay?.updated_at" class="ml-auto text-xs text-slate-400">更新于 {{ dateTime(merchants.alipay.updated_at) }}</span>
      </div>
      <p class="hint mb-3">在支付宝开放平台创建应用并签约"电脑网站支付"与"当面付"，接口加签方式选 RSA2。没有签约的产品请在上方关闭对应支付方式。</p>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <label class="flex items-center gap-2 text-sm md:col-span-2"><input v-model="ali.enabled" type="checkbox" /> 启用（停用后不再创建新订单，已有订单的通知、查单、退款照常处理）</label>
        <div>
          <label class="label" for="ali-app">应用 APPID</label>
          <input id="ali-app" v-model="ali.appID" class="input" />
        </div>
        <div>
          <label class="label" for="ali-gw">网关</label>
          <select id="ali-gw" v-model="ali.gatewayKind" class="input">
            <option value="prod">正式环境</option>
            <option value="sandbox">沙箱环境</option>
            <option value="custom">自定义</option>
          </select>
          <input v-if="ali.gatewayKind === 'custom'" v-model="ali.gateway" class="input mt-2" placeholder="https://…/gateway.do" aria-label="自定义网关" />
        </div>
        <div class="md:col-span-2">
          <div class="label">接口加签方式</div>
          <div class="flex gap-4 text-sm">
            <label class="flex items-center gap-1"><input v-model="ali.mode" type="radio" value="public_key" /> 公钥</label>
            <label class="flex items-center gap-1"><input v-model="ali.mode" type="radio" value="cert" /> 公钥证书</label>
          </div>
        </div>
        <KeyField
          id="ali-priv"
          v-model="ali.privateKey"
          class="md:col-span-2"
          label="应用私钥"
          :placeholder="merchants.alipay?.has_private_key ? '已设置，留空不修改' : '粘贴 PEM，或支付宝密钥工具生成的私钥字符串'"
        />
        <KeyField v-if="ali.mode === 'public_key'" id="ali-pub" v-model="ali.publicKey" class="md:col-span-2" label="支付宝公钥" placeholder="开放平台中的支付宝公钥（不是应用公钥）" />
        <template v-else>
          <KeyField id="ali-appcert" v-model="ali.appCert" label="应用公钥证书 appCertPublicKey_xxx.crt" :rows="3" />
          <KeyField id="ali-cert" v-model="ali.alipayCert" label="支付宝公钥证书 alipayCertPublicKey_RSA2.crt" :rows="3" />
          <KeyField id="ali-root" v-model="ali.rootCert" class="md:col-span-2" label="支付宝根证书 alipayRootCert.crt" :rows="3" />
        </template>
      </div>
      <div class="mt-3 flex justify-end gap-2">
        <button class="btn-secondary" :disabled="busy || !merchants.alipay?.ready" @click="test('alipay')">测试连接</button>
        <button class="btn-primary" :disabled="busy" @click="saveAlipay">保存</button>
      </div>
    </div>

    <div class="card mb-5">
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <h2 class="card-title !mb-0">微信支付</h2>
        <span :class="['chip', stateText(merchants.wechat).cls]">{{ stateText(merchants.wechat).text }}</span>
        <span v-if="merchants.wechat?.problem && merchants.wechat?.updated_at" class="text-xs text-amber-700">{{ merchants.wechat.problem }}</span>
        <span v-if="merchants.wechat?.updated_at" class="ml-auto text-xs text-slate-400">更新于 {{ dateTime(merchants.wechat.updated_at) }}</span>
      </div>
      <p class="hint mb-3">使用 APIv3 接口与 Native 支付。商户号需要开通 Native 支付，并与填写的 AppID（公众号或应用）绑定。</p>
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <label class="flex items-center gap-2 text-sm md:col-span-3"><input v-model="wx.enabled" type="checkbox" /> 启用（停用后不再创建新订单，已有订单的通知、查单、退款照常处理）</label>
        <div>
          <label class="label" for="wx-app">AppID</label>
          <input id="wx-app" v-model="wx.appID" class="input" />
        </div>
        <div>
          <label class="label" for="wx-mch">商户号</label>
          <input id="wx-mch" v-model="wx.mchID" class="input" />
        </div>
        <div>
          <label class="label" for="wx-serial">商户 API 证书序列号</label>
          <input id="wx-serial" v-model="wx.serial" class="input" />
        </div>
        <KeyField
          id="wx-priv"
          v-model="wx.privateKey"
          class="md:col-span-2"
          label="商户 API 私钥 apiclient_key.pem"
          :placeholder="merchants.wechat?.has_private_key ? '已设置，留空不修改' : '粘贴 apiclient_key.pem 的内容'"
        />
        <div>
          <label class="label" for="wx-v3">APIv3 密钥</label>
          <input id="wx-v3" v-model="wx.apiV3Key" class="input" type="password" autocomplete="off" :placeholder="merchants.wechat?.has_api_v3_key ? '已设置，留空不修改' : '32 个字符'" />
        </div>
        <div class="md:col-span-3">
          <div class="label">验签方式</div>
          <div class="flex gap-4 text-sm">
            <label class="flex items-center gap-1"><input v-model="wx.verify" type="radio" value="public_key" /> 微信支付公钥（新商户默认）</label>
            <label class="flex items-center gap-1"><input v-model="wx.verify" type="radio" value="platform_cert" /> 平台证书（自动下载）</label>
          </div>
        </div>
        <template v-if="wx.verify === 'public_key'">
          <div>
            <label class="label" for="wx-pkid">微信支付公钥 ID</label>
            <input id="wx-pkid" v-model="wx.publicKeyID" class="input" placeholder="PUB_KEY_ID_…" />
          </div>
          <KeyField id="wx-pk" v-model="wx.publicKey" class="md:col-span-2" label="微信支付公钥 pub_key.pem" :rows="3" />
        </template>
        <details class="md:col-span-3">
          <summary class="cursor-pointer text-sm text-slate-500">高级</summary>
          <label class="label mt-2" for="wx-base">接口地址</label>
          <input id="wx-base" v-model="wx.baseURL" class="input" placeholder="https://api.mch.weixin.qq.com（默认）" />
        </details>
      </div>
      <div class="mt-3 flex justify-end gap-2">
        <button class="btn-secondary" :disabled="busy || !merchants.wechat?.ready" @click="test('wechat')">测试连接</button>
        <button class="btn-primary" :disabled="busy" @click="saveWechat">保存</button>
      </div>
    </div>

    <div class="card mb-5">
      <h2 class="card-title">支付方式状态</h2>
      <DataTable
        :columns="[
          { key: 'channel', label: '支付方式' },
          { key: 'configured', label: '商户' },
          { key: 'enabled', label: '开关' },
          { key: 'available', label: '买家可用' },
        ]"
        :rows="status.data.value?.channels"
        row-key="channel"
      >
        <template #cell-channel="{ row }">{{ label('paymentChannel', row.channel).text }}</template>
        <template #cell-configured="{ row }">{{ row.channel === 'manual' ? '—' : row.configured ? '已启用' : '未启用' }}</template>
        <template #cell-enabled="{ row }">{{ row.enabled ? '开启' : '关闭' }}</template>
        <template #cell-available="{ row }">
          <span :class="row.available ? 'text-emerald-600' : 'text-slate-400'">{{ row.available ? '可用' : '不可用' }}</span>
        </template>
      </DataTable>
    </div>

    <div class="card">
      <h2 class="card-title">最近的支付通知</h2>
      <DataTable
        :columns="[
          { key: 'created_at', label: '时间' },
          { key: 'provider', label: '渠道' },
          { key: 'out_trade_no', label: '商户单号' },
          { key: 'amount_fen', label: '金额', align: 'right' },
          { key: 'result', label: '处理结果' },
        ]"
        :rows="status.data.value?.notifications"
        row-key="id"
      >
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-provider="{ row }">{{ row.provider === 'alipay' ? '支付宝' : row.provider === 'wechat' ? '微信支付' : row.provider }}</template>
        <template #cell-amount_fen="{ row }">{{ row.amount_fen != null ? money(row.amount_fen / 100) : '—' }}</template>
        <template #cell-result="{ row }">
          <StatusBadge kind="notifyResult" :value="row.result" />
          <span v-if="row.error" class="block text-xs text-red-600">{{ row.error }}</span>
        </template>
      </DataTable>
    </div>
  </div>
</template>
