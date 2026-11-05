<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import QRCode from 'qrcode'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import PromoChannelDetails from './PromoChannelDetails.vue'
import {
  createPromoChannel, deletePromoChannel, getPromoChannels, setPromoChannelStatus,
  updatePromoChannel,
  type PromoChannel, type PromoDateRange, type PromoOrganicStats, type PromoStats,
} from './api'

defineOptions({ name: 'MarketAdminPromoChannels' })

const presets = [
  { days: 1, label: '今天' }, { days: 7, label: '近 7 天' },
  { days: 30, label: '近 30 天' }, { days: 0, label: '全部' },
]

function recentRange(days: number): PromoDateRange {
  if (!days) return {}
  const today = new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString().slice(0, 10)
  const first = new Date(`${today}T00:00:00Z`)
  first.setUTCDate(first.getUTCDate() - days + 1)
  return { from: first.toISOString().slice(0, 10), to: today }
}

const draftRange = reactive({ from: recentRange(30).from!, to: recentRange(30).to! })
const appliedRange = ref<PromoDateRange>(recentRange(30))
const rangeError = ref('')
const channels = ref<PromoChannel[]>([])
const totals = ref<PromoStats>({ pv: 0, uv: 0, signups: 0, paying_users: 0, recharge_amount: '0.00', buyer_spend: '0.00', seller_users: 0 })
const organic = ref<PromoOrganicStats>({ signups: 0, paying_users: 0, recharge_amount: '0.00', buyer_spend: '0.00', seller_users: 0 })
const loading = ref(false)
const loaded = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const page = ref(1)
const pageSize = ref(15)
const sortKey = ref('')
const sortOrder = ref<'asc' | 'desc'>('desc')
let listRequest: AbortController | undefined
let requestedRange: PromoDateRange = recentRange(30)

const showForm = ref(false)
const editing = ref<PromoChannel | null>(null)
const form = reactive({ name: '', code: '', landing_path: '/', note: '' })
const formError = ref('')
const pendingAction = ref<{ kind: 'disable' | 'delete'; channel: PromoChannel } | null>(null)
const actionError = ref('')
const detailsChannel = ref<PromoChannel | null>(null)
const qrChannel = ref<PromoChannel | null>(null)
const qrDataUrl = ref('')
const qrError = ref('')
const copyFeedback = ref('')
const copyFailed = ref(false)
let qrSequence = 0
let disposed = false

const columns: Column[] = [
  { key: 'name', label: '渠道名', class: 'col-name' },
  { key: 'code', label: '渠道码' },
  { key: 'status', label: '状态' },
  { key: 'pv', label: 'PV', sortable: true },
  { key: 'uv', label: 'UV', sortable: true },
  { key: 'signups', label: '注册数', sortable: true },
  { key: 'signup_rate', label: '注册转化率', sortable: true },
  { key: 'paying_users', label: '付费人数', sortable: true },
  { key: 'paying_rate', label: '付费转化率', sortable: true },
  { key: 'recharge_amount', label: '充值金额（元）', sortable: true },
  { key: 'buyer_spend', label: '消费金额（元）', sortable: true },
  { key: 'seller_users', label: '上架卖家数', sortable: true },
  { key: 'created_at', label: '创建时间', sortable: true },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

function number(value: unknown): number {
  const n = Number(value)
  return Number.isFinite(n) ? n : 0
}

function money(value: unknown): string {
  return number(value).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function rate(numerator: number, denominator: number): number | null {
  return denominator > 0 ? numerator / denominator : null
}

function percent(value: number | null): string {
  return value === null ? '—' : `${(value * 100).toFixed(1)}%`
}

function time(value: string): string {
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
}

function link(channel: PromoChannel): string {
  // 保留落地页 hash，并把归因参数放在 hash 之前，确保 C 端能从 location.search 读取。
  const [path, ...hash] = (channel.landing_path || '/').split('#')
  return `${window.location.origin}${path}${path.includes('?') ? '&' : '?'}ch=${encodeURIComponent(channel.code)}${hash.length ? `#${hash.join('#')}` : ''}`
}

const rows = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  const result = channels.value
    .filter((channel) => !kw || [channel.name, channel.code, channel.note].some((v) => v.toLowerCase().includes(kw)))
    .map((channel) => ({
      ...channel,
      ...channel.stats,
      recharge_amount: number(channel.stats.recharge_amount),
      buyer_spend: number(channel.stats.buyer_spend),
      signup_rate: rate(channel.stats.signups, channel.stats.uv),
      paying_rate: rate(channel.stats.paying_users, channel.stats.signups),
    }))
  const key = sortKey.value as keyof (typeof result)[number]
  if (key) result.sort((a, b) => {
    const av = a[key]
    const bv = b[key]
    if (av === null || bv === null) return av === bv ? 0 : av === null ? 1 : -1
    const delta = key === 'created_at'
      ? new Date(String(av)).getTime() - new Date(String(bv)).getTime()
      : number(av) - number(bv)
    return sortOrder.value === 'asc' ? delta : -delta
  })
  return result
})
const pageRows = computed(() => rows.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
const rangeLabel = computed(() => appliedRange.value.from ? `${appliedRange.value.from} 至 ${appliedRange.value.to}` : '全部时间')
const summary = computed(() => [
  { label: '总访客 UV', value: totals.value.uv.toLocaleString('zh-CN'), hint: '各渠道 UV 之和' },
  { label: '渠道注册', value: totals.value.signups.toLocaleString('zh-CN'), hint: `自然注册 ${organic.value.signups.toLocaleString('zh-CN')}` },
  { label: '付费人数', value: totals.value.paying_users.toLocaleString('zh-CN'), hint: `自然付费 ${organic.value.paying_users.toLocaleString('zh-CN')}` },
  { label: '充值金额（元）', value: money(totals.value.recharge_amount), hint: `自然充值 ${money(organic.value.recharge_amount)} 元` },
  { label: '消费金额（元）', value: money(totals.value.buyer_spend), hint: `自然消费 ${money(organic.value.buyer_spend)} 元` },
])

watch([keyword, pageSize], () => { page.value = 1 })
watch(() => rows.value.length, (total) => {
  page.value = Math.min(page.value, Math.max(1, Math.ceil(total / pageSize.value)))
})

async function load(range: PromoDateRange = requestedRange) {
  requestedRange = { ...range }
  listRequest?.abort()
  const request = new AbortController()
  listRequest = request
  loading.value = true
  error.value = ''
  try {
    const result = await getPromoChannels(range, { signal: request.signal })
    if (request.signal.aborted) return
    channels.value = result.items ?? []
    totals.value = result.totals
    organic.value = result.organic
    appliedRange.value = { ...range }
    loaded.value = true
  } catch (e) {
    if (!request.signal.aborted) error.value = errorMessage(e, '加载推广渠道失败')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

function chooseRange(days: number) {
  const range = recentRange(days)
  Object.assign(draftRange, { from: range.from || '', to: range.to || '' })
  rangeError.value = ''
  page.value = 1
  void load(range)
}

function query() {
  rangeError.value = ''
  if (Boolean(draftRange.from) !== Boolean(draftRange.to)) {
    rangeError.value = '请选择完整的开始和结束日期，或清空两项查询全部时间'
    return
  }
  if (draftRange.from > draftRange.to) {
    rangeError.value = '开始日期不能晚于结束日期'
    return
  }
  page.value = 1
  void load(draftRange.from ? { ...draftRange } : {})
}

function sort(key: string, order: 'asc' | 'desc') {
  sortKey.value = key
  sortOrder.value = order
  page.value = 1
}

function errorMessage(e: unknown, fallback: string): string {
  const code = e && typeof e === 'object' && 'code' in e ? e.code : ''
  if (code === 'PROMO_CODE_TAKEN') return '渠道码已被占用，请更换后重试'
  if (code === 'PROMO_CHANNEL_HAS_DATA') return '该渠道已有访问或注册数据，无法删除。可以停用该渠道。'
  return e instanceof Error ? e.message : fallback
}

function openForm(channel: PromoChannel | null = null) {
  editing.value = channel
  formError.value = ''
  msg.value = ''
  Object.assign(form, channel
    ? { name: channel.name, code: channel.code, landing_path: channel.landing_path, note: channel.note }
    : { name: '', code: '', landing_path: '/', note: '' })
  showForm.value = true
}

function closeForm() {
  if (!busy.value) showForm.value = false
}

function validLandingPath(path: string): boolean {
  try {
    return [path, decodeURIComponent(path)].every((candidate) =>
      candidate.startsWith('/') && !candidate.startsWith('//') &&
      !candidate.includes('\\') && !/https?:/i.test(candidate) &&
      ![...candidate].some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127),
    ) && [...path].length <= 512
  } catch {
    return false
  }
}

async function save() {
  if (busy.value) return
  formError.value = ''
  const name = form.name.trim()
  const code = form.code.trim().toLowerCase()
  const path = form.landing_path.trim() || '/'
  if (!name || name.length > 64) formError.value = '名称必填，且不能超过 64 个字符'
  else if (!editing.value && code && !/^[a-z0-9_-]{2,32}$/.test(code)) formError.value = '渠道码需为 2–32 位小写字母、数字、下划线或连字符'
  else if (!validLandingPath(path)) formError.value = '落地页路径需以单个 / 开头，不含反斜杠、控制字符或外部网址，且不超过 512 个字符'
  else if (form.note.length > 500) formError.value = '备注不能超过 500 个字符'
  if (formError.value) return
  busy.value = true
  try {
    const input = { name, landing_path: path, note: form.note }
    if (editing.value) await updatePromoChannel(editing.value.id, input)
    else await createPromoChannel({ ...input, ...(code ? { code } : {}) })
    if (disposed) return
    showForm.value = false
    msg.value = editing.value ? '渠道已保存' : '渠道已创建，可复制专属链接开始推广'
    await load()
  } catch (e) {
    if (!disposed) formError.value = errorMessage(e, '保存失败')
  } finally {
    busy.value = false
  }
}

function confirmAction(kind: 'disable' | 'delete', channel: PromoChannel) {
  actionError.value = ''
  msg.value = ''
  pendingAction.value = { kind, channel }
}

async function runAction(channel: PromoChannel, kind: 'disable' | 'delete' | 'enable') {
  if (busy.value) return
  busy.value = true
  actionError.value = ''
  error.value = ''
  msg.value = ''
  try {
    if (kind === 'delete') await deletePromoChannel(channel.id)
    else await setPromoChannelStatus(channel.id, kind === 'disable' ? 'disabled' : 'active')
    if (disposed) return
    pendingAction.value = null
    msg.value = kind === 'delete' ? '渠道已删除' : kind === 'disable' ? '渠道已停用' : '渠道已启用'
    await load()
  } catch (e) {
    if (pendingAction.value) actionError.value = errorMessage(e, '操作失败')
    else error.value = errorMessage(e, '操作失败')
  } finally {
    busy.value = false
  }
}

function submitAction() {
  if (pendingAction.value) void runAction(pendingAction.value.channel, pendingAction.value.kind)
}

async function copy(channel: PromoChannel) {
  msg.value = ''
  copyFeedback.value = ''
  copyFailed.value = false
  try {
    await navigator.clipboard.writeText(link(channel))
    msg.value = `已复制「${channel.name}」的专属链接`
    copyFeedback.value = msg.value
  } catch {
    error.value = '复制失败，请在二维码弹窗中选中专属链接后手动复制'
    copyFeedback.value = error.value
    copyFailed.value = true
  }
}

async function showQr(channel: PromoChannel) {
  qrChannel.value = channel
  qrDataUrl.value = ''
  qrError.value = ''
  copyFeedback.value = ''
  const sequence = ++qrSequence
  try {
    const url = await QRCode.toDataURL(link(channel), { width: 640, margin: 2, errorCorrectionLevel: 'M' })
    if (sequence === qrSequence && !disposed) qrDataUrl.value = url
  } catch {
    if (sequence === qrSequence && !disposed) qrError.value = '二维码生成失败，请重试或复制专属链接'
  }
}

function closeQr() {
  qrChannel.value = null
  qrSequence++
}

onMounted(() => { void load() })
onBeforeUnmount(() => {
  disposed = true
  listRequest?.abort()
  qrSequence++
})
</script>

<template>
  <div id="market-promo-channels-page" class="market-promo-page h-full min-h-0">
    <TablePageLayout content-mode="content">
      <template #title>
        <div class="page-heading">
          <h1 class="y-title">推广渠道</h1>
          <p class="y-hint">创建专属链接，了解哪些推广带来注册、充值与消费。</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy" @click="openForm()">
          <Icon name="plus" size="sm" />新建渠道
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading || busy" @click="load()">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />刷新
        </button>
      </template>
      <template #filters>
        <div v-if="error" class="y-msg y-msg--error mb-3" role="alert">
          {{ error }}<span v-if="loaded">（当前显示上次成功加载的数据）</span>
          <button type="button" class="y-link ml-2" :disabled="loading" @click="load()">重试</button>
        </div>
        <p v-if="msg" class="y-msg y-msg--ok mb-3" role="status">{{ msg }}</p>
        <div class="range-toolbar">
          <div class="range-presets" aria-label="日期快捷筛选">
            <button v-for="preset in presets" :key="preset.days" type="button" class="btn btn-secondary btn-sm"
              :disabled="busy" :aria-pressed="draftRange.from === (recentRange(preset.days).from || '') && draftRange.to === (recentRange(preset.days).to || '')"
              @click="chooseRange(preset.days)">{{ preset.label }}</button>
          </div>
          <form class="date-fields" @submit.prevent="query">
            <label class="date-field"><span>开始日期</span><input v-model="draftRange.from" data-test="from" type="date" class="filter-input" :disabled="busy" /></label>
            <label class="date-field"><span>结束日期</span><input v-model="draftRange.to" data-test="to" type="date" class="filter-input" :disabled="busy" /></label>
            <button type="submit" class="btn btn-primary btn-sm" :disabled="busy">查询</button>
          </form>
        </div>
        <p v-if="rangeError" class="y-msg y-msg--error mt-2" role="alert">{{ rangeError }}</p>
        <div class="section-heading mt-4">
          <h2>渠道效果</h2><span class="y-hint">{{ rangeLabel }} · 北京时间（含首尾日期）</span>
        </div>
        <div class="stats-row" :aria-busy="loading">
          <article v-for="card in summary" :key="card.label" class="stat-card">
            <div class="stat-card__label">{{ card.label }}</div>
            <div class="stat-card__value">{{ loaded ? card.value : '—' }}</div>
            <div class="y-hint">{{ loaded ? card.hint : '等待加载' }}</div>
          </article>
        </div>
        <p v-if="loaded" class="organic-comparison" data-test="organic-comparison">
          渠道注册 {{ totals.signups }} / 自然注册 {{ organic.signups }}
          <span class="y-hint"> · 注册转化率 = 注册数 / UV；付费转化率 = 区间付费人数 / 区间注册数</span>
        </p>
        <p class="y-hint mt-2">充值、消费和上架按事件发生时间统计，付费人数可能来自更早注册的用户，付费转化率可能超过 100%。</p>
        <div class="search-row mt-3">
          <label for="promo-keyword" class="filter-label">搜索渠道</label>
          <input id="promo-keyword" v-model="keyword" type="search" class="filter-input" placeholder="渠道名 / 渠道码 / 备注" />
          <span class="y-hint">点击指标列标题排序</span>
        </div>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="pageRows" :loading="loading" row-key="id" :expandable-actions="false" server-side-sort @sort="sort">
          <template #cell-name="{ row }"><span class="channel-name" :title="row.note || row.name">{{ row.name }}</span></template>
          <template #cell-code="{ row }"><code>{{ row.code }}</code></template>
          <template #cell-status="{ row }"><span class="status-cell"><i class="status-dot" :data-tone="row.status === 'active' ? 'ok' : 'idle'" />{{ row.status === 'active' ? '启用' : '停用' }}</span></template>
          <template #cell-signup_rate="{ row }">{{ percent(row.signup_rate) }}</template>
          <template #cell-paying_rate="{ row }">{{ percent(row.paying_rate) }}</template>
          <template #cell-recharge_amount="{ row }">{{ money(row.recharge_amount) }}</template>
          <template #cell-buyer_spend="{ row }">{{ money(row.buyer_spend) }}</template>
          <template #cell-created_at="{ row }">{{ time(row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button type="button" class="y-link" :disabled="busy" @click="detailsChannel = row">详情</button>
              <button type="button" class="y-link" :disabled="busy || loading" @click="openForm(row)">编辑</button>
              <button type="button" class="y-link" @click="copy(row)">复制链接</button>
              <button type="button" class="y-link" @click="showQr(row)">二维码</button>
              <button type="button" class="y-link" :disabled="busy || loading" @click="row.status === 'active' ? confirmAction('disable', row) : runAction(row, 'enable')">{{ row.status === 'active' ? '停用' : '启用' }}</button>
              <button v-if="row.stats.pv === 0 && row.stats.signups === 0" type="button" class="y-link y-link--danger" :disabled="busy || loading" @click="confirmAction('delete', row)">删除</button>
            </div>
          </template>
          <template #empty>
            <EmptyState v-if="!error" :title="keyword ? '没有匹配的渠道' : '暂无推广渠道'" :description="keyword ? '试试其他关键词' : '新建渠道，获得专属链接并开始统计推广效果'" :action-text="keyword ? undefined : '新建渠道'" @action="openForm()" />
            <p v-else class="y-hint">渠道加载失败，请重试</p>
          </template>
        </DataTable>
        <Pagination v-if="rows.length" :total="rows.length" :page="page" :page-size="pageSize" :page-size-options="[15, 20, 50]" @update:page="page = $event" @update:page-size="pageSize = $event" />
      </template>
    </TablePageLayout>

    <BaseDialog :show="showForm" :title="editing ? '编辑推广渠道' : '新建推广渠道'" :close-on-escape="!busy" :show-close-button="!busy" @close="closeForm">
      <form id="promo-channel-form" class="dialog-form" @submit.prevent="save">
        <p v-if="formError" class="y-msg y-msg--error" role="alert">{{ formError }}</p>
        <label class="y-field"><span>名称（必填）</span><input v-model="form.name" data-test="name" type="text" class="input w-full" maxlength="64" required :disabled="busy" /></label>
        <label class="y-field"><span>渠道码{{ editing ? '' : '（可选）' }}</span><input v-model="form.code" data-test="code" type="text" class="input w-full" maxlength="32" :readonly="!!editing" :disabled="busy" placeholder="留空自动生成" /><span class="y-hint">创建后不可修改；2–32 位小写字母、数字、下划线或连字符。</span></label>
        <label class="y-field"><span>落地页路径</span><input v-model="form.landing_path" data-test="landing-path" type="text" class="input w-full" maxlength="512" placeholder="/" :disabled="busy" /><span class="y-hint">以单个 / 开头，例如 /?tab=market；留空使用 /。</span></label>
        <label class="y-field"><span>备注</span><textarea v-model="form.note" data-test="note" class="input y-textarea w-full" rows="3" maxlength="500" :disabled="busy"></textarea></label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="busy" @click="closeForm">取消</button><button type="submit" form="promo-channel-form" class="btn btn-primary" :disabled="busy">{{ busy ? '保存中…' : '保存' }}</button></div></template>
    </BaseDialog>

    <ConfirmDialog :show="!!pendingAction" :title="pendingAction?.kind === 'disable' ? '停用推广渠道' : '删除推广渠道'"
      :message="pendingAction?.kind === 'disable' ? `确定停用「${pendingAction.channel.name}」？停用后专属链接不再计数和归因，已有用户的渠道归属保留。` : `确定删除「${pendingAction?.channel.name || ''}」？仅没有任何访问和注册的渠道可以删除。`"
      :confirm-text="pendingAction?.kind === 'disable' ? '确认停用' : '确认删除'" cancel-text="取消" busy-text="处理中…" danger :busy="busy"
      @confirm="submitAction" @cancel="pendingAction = null">
      <p v-if="actionError" class="y-msg y-msg--error" role="alert">{{ actionError }}</p>
    </ConfirmDialog>

    <BaseDialog :show="!!qrChannel" :title="`推广二维码 · ${qrChannel?.name || ''}`" @close="closeQr">
      <div v-if="qrChannel" class="qr-content">
        <p v-if="qrError" class="y-msg y-msg--error" role="alert">{{ qrError }}<button type="button" class="y-link ml-2" @click="showQr(qrChannel)">重试</button></p>
        <img v-else-if="qrDataUrl" :src="qrDataUrl" :alt="`${qrChannel.name} 的推广二维码`" width="280" height="280" />
        <p v-else class="y-hint" role="status">正在生成二维码…</p>
        <label class="y-field w-full"><span>专属链接</span><input type="text" class="input w-full" :value="link(qrChannel)" readonly @focus="($event.target as HTMLInputElement).select()" /></label>
        <p v-if="copyFeedback" class="y-msg" :class="copyFailed ? 'y-msg--error' : 'y-msg--ok'" :role="copyFailed ? 'alert' : 'status'">{{ copyFeedback }}</p>
        <p v-if="qrChannel.status !== 'active'" class="y-hint">渠道已停用，当前链接不计数和归因。</p>
      </div>
      <template #footer><div class="flex justify-end gap-3"><button v-if="qrChannel" type="button" class="btn btn-secondary" @click="copy(qrChannel)">复制链接</button><a v-if="qrDataUrl && qrChannel" :href="qrDataUrl" :download="`promo-${qrChannel.code}.png`" class="btn btn-primary">下载二维码</a></div></template>
    </BaseDialog>
    <PromoChannelDetails :channel="detailsChannel" :range="appliedRange" @close="detailsChannel = null" />
  </div>
</template>

<style scoped>
.page-heading { min-width: 0; }
.page-action-btn { height: 32px; padding: 0 14px; font-size: 13px; border-radius: 8px; display: inline-flex; align-items: center; gap: 6px; }
.y-hint { margin: 0; font-size: 12px; line-height: 1.6; color: var(--color-text-secondary, #64748b); }
.range-toolbar, .range-presets, .date-fields, .date-field, .search-row, .section-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.range-toolbar { justify-content: space-between; }
.range-presets [aria-pressed='true'] { color: var(--color-primary, #2563eb); border-color: currentColor; }
.date-field { font-size: 12px; color: var(--color-text-secondary, #64748b); }
.date-field input { width: 142px; min-width: 0; }
.section-heading { justify-content: space-between; margin-bottom: 10px; }
.section-heading h2 { font-size: 13px; font-weight: 600; color: var(--color-text-primary, #262626); }
.stats-row { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 12px; }
.stat-card { padding: 14px 16px; border-radius: 12px; border: 1px solid var(--color-border, #e2e8f0); background: var(--color-bg-secondary, #f8fafc); min-width: 0; }
.stat-card__label { font-size: 12px; color: var(--color-text-secondary, #64748b); }
.stat-card__value { margin: 4px 0; font-size: 22px; font-weight: 700; line-height: 1.3; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.organic-comparison { margin-top: 12px; font-size: 13px; }
.search-row input { width: 260px; max-width: 100%; }
.table-ops { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; }
.status-cell { display: inline-flex; align-items: center; gap: 6px; }
.status-dot { width: 7px; height: 7px; border-radius: 50%; background: #94a3b8; }
.status-dot[data-tone='ok'] { background: #16a34a; }
.channel-name { display: block; max-width: 16rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.market-promo-page :deep(th.col-ops), .market-promo-page :deep(td.col-ops) { min-width: 18rem; width: 18rem; white-space: normal; }
.market-promo-page :deep(td), .market-promo-page :deep(th) { font-size: 13px; }
.dialog-form, .y-field { display: grid; gap: 12px; }
.y-field { gap: 4px; font-size: 13px; }
.y-textarea { height: auto !important; min-height: 88px; resize: vertical; padding: 8px 10px; }
.qr-content { display: flex; flex-direction: column; align-items: center; gap: 16px; }
.qr-content img { max-width: 100%; height: auto; }
@media (max-width: 1200px) { .stats-row { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 767px) {
  .stats-row { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .stat-card { padding: 10px; }
  .stat-card__value { font-size: 20px; }
  .date-fields { width: 100%; align-items: flex-end; }
  .date-field { flex: 1; flex-direction: column; align-items: flex-start; }
  .date-field input { width: 100%; }
  .channel-name { white-space: normal; overflow-wrap: anywhere; }
  .page-heading .y-hint { display: none; }
}
</style>
