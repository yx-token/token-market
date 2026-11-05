<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import {
  getRewardEvents, getRewardTaskConfig, getRewardTickets, pingRewardTasks, retryRewardEvent, saveRewardTaskConfig,
  REWARD_EVENT_STATES, REWARD_TICKET_STATES,
  type RewardEvent, type RewardEventState, type RewardPingResult, type RewardTaskConfig, type RewardTicket, type RewardTicketState,
} from './api'

defineOptions({ name: 'MarketAdminRewardTasks' })

const TICKET_LABELS: Record<RewardTicketState, string> = {
  pending: '待查询', active: '进行中', completed: '已完成', skipped: '无需上报', invalid: '已失效',
}
const EVENT_LABELS: Record<RewardEventState, string> = {
  pending: '待发送', accepted: '已计入', completed: '已发奖', ignored: '已忽略', rejected: '被拒绝', failed: '重试用尽',
}
const TASK_TYPES: Record<string, string> = { browse: '浏览', register: '注册', recharge: '充值' }
const REASONS: Record<string, string> = {
  NOT_NEW_USER: '老用户登录，不计注册',
  BROWSE_TASK: '浏览任务由 SDK 计时',
  REWARD_SIGNATURE_MISSING: '签名请求头缺失',
  REWARD_SIGNATURE_INVALID: '签名错误（检查 App Secret）',
  REWARD_TIMESTAMP_EXPIRED: '时间戳超窗（检查服务器时钟）',
  REWARD_NONCE_REPLAYED: 'nonce 重复',
  REWARD_PARTNER_INVALID: 'App Key 无效或站点被停用',
  REWARD_TASKS_DISABLED: 'loxo 任务中心未开启',
  REWARD_TICKET_NOT_FOUND: '票据不存在',
  REWARD_TICKET_EXPIRED: '票据已过期',
  REWARD_TICKET_PARTNER_MISMATCH: '票据不属于本站',
  REWARD_EVENT_INVALID: '回调参数不合法',
  REWARD_EVENT_TYPE_MISMATCH: '事件类型与任务不符',
  REWARD_EXTERNAL_USER_USED: '该账号本周期已绑定过其他 loxo 用户',
  REWARD_EXTERNAL_USER_MISMATCH: '票据已绑定其他账号',
  REWARD_TASK_UNAVAILABLE: '任务未启用或已结束',
  REWARD_TASK_ALREADY_COMPLETED: '本周期已完成',
  REWARD_TASK_QUOTA_EXHAUSTED: '任务名额已满（稍后自动重试）',
}

const config = ref<RewardTaskConfig | null>(null)
const form = reactive({ enabled: false, base_url: 'https://loxo.cc', app_key: '', app_secret: '', sdk_enabled: true })
const loading = ref(false)
const saving = ref(false)
const pinging = ref(false)
const error = ref('')
const msg = ref('')
const formError = ref('')
const ping = ref<RewardPingResult | null>(null)
const pingError = ref('')

const tab = ref<'tickets' | 'events'>('events')
const stateFilter = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const tickets = ref<RewardTicket[]>([])
const events = ref<RewardEvent[]>([])
const listLoading = ref(false)
const listError = ref('')
const retrying = ref<number | null>(null)
let listRequest: AbortController | undefined
let disposed = false

const siteOrigin = window.location.origin

const ticketColumns: Column[] = [
  { key: 'id', label: 'ID' },
  { key: 'user', label: '用户' },
  { key: 'task', label: '任务' },
  { key: 'state', label: '状态', class: 'col-state' },
  { key: 'new_user', label: '新注册' },
  { key: 'expires_at', label: '票据过期' },
  { key: 'created_at', label: '绑定时间' },
]
const eventColumns: Column[] = [
  { key: 'id', label: 'ID' },
  { key: 'user', label: '用户' },
  { key: 'event_type', label: '类型' },
  { key: 'amount', label: '金额（元）' },
  { key: 'state', label: '状态', class: 'col-state' },
  { key: 'attempts', label: '发送次数' },
  { key: 'when', label: '发送 / 下次重试' },
  { key: 'actions', label: '操作' },
]

const stateOptions = computed(() =>
  tab.value === 'tickets'
    ? REWARD_TICKET_STATES.map((s) => ({ value: s, label: TICKET_LABELS[s] }))
    : REWARD_EVENT_STATES.map((s) => ({ value: s, label: EVENT_LABELS[s] })),
)

const summary = computed(() => {
  const s = config.value?.summary
  if (!s) return []
  return [
    { label: '待查询票据', value: s.tickets.pending, hint: '新绑定、等待向 loxo 确认任务类型' },
    { label: '进行中票据', value: s.tickets.active, hint: '注册待上报 / 充值累计中' },
    { label: '已完成票据', value: s.tickets.completed, hint: 'loxo 已发奖或本周期已完成' },
    { label: '待发送事件', value: s.events.pending, hint: '含退避重试中的事件' },
    { label: '已发奖事件', value: s.events.completed, hint: '本次回调促成任务完成' },
    { label: '被拒 / 失败事件', value: s.events.rejected + s.events.failed, hint: '可在下方「回调事件」里重试' },
  ]
})

const statusText = computed(() => {
  const c = config.value
  if (!c) return ''
  if (!c.enabled) return '未启用：不查票据、不绑定、不回调'
  return c.sdk_enabled ? '已启用（含浏览任务 SDK）' : '已启用（未加载浏览任务 SDK）'
})

function fill(c: RewardTaskConfig) {
  config.value = c
  form.enabled = c.enabled
  form.base_url = c.base_url
  form.app_key = c.app_key
  form.app_secret = ''
  form.sdk_enabled = c.sdk_enabled
}

function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

function reasonText(reason: string): string {
  if (!reason) return ''
  return REASONS[reason] ? `${REASONS[reason]}（${reason}）` : reason
}

function time(value: string | null | undefined): string {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
}

function maskTicket(t: string): string {
  return t.length > 14 ? `${t.slice(0, 9)}…${t.slice(-4)}` : t
}

async function loadConfig() {
  loading.value = true
  error.value = ''
  try {
    fill(await getRewardTaskConfig())
  } catch (e) {
    error.value = `加载配置失败：${messageOf(e)}`
  } finally {
    loading.value = false
  }
}

async function loadList() {
  listRequest?.abort()
  const ac = new AbortController()
  listRequest = ac
  listLoading.value = true
  listError.value = ''
  try {
    if (tab.value === 'tickets') {
      const r = await getRewardTickets(stateFilter.value, page.value, pageSize.value, { signal: ac.signal })
      if (ac.signal.aborted || disposed) return
      tickets.value = r.items
      total.value = r.total
    } else {
      const r = await getRewardEvents(stateFilter.value, page.value, pageSize.value, { signal: ac.signal })
      if (ac.signal.aborted || disposed) return
      events.value = r.items
      total.value = r.total
    }
  } catch (e) {
    if (ac.signal.aborted || disposed) return
    listError.value = `加载失败：${messageOf(e)}`
  } finally {
    if (listRequest === ac) listLoading.value = false
  }
}

function refresh() {
  msg.value = ''
  void loadConfig()
  void loadList()
}

async function save() {
  formError.value = ''
  msg.value = ''
  if (form.enabled && !form.app_key.trim()) {
    formError.value = '启用前请填写 App Key'
    return
  }
  if (form.enabled && !config.value?.secret_set && !form.app_secret.trim()) {
    formError.value = '启用前请填写 App Secret'
    return
  }
  saving.value = true
  try {
    fill(await saveRewardTaskConfig({ ...form, base_url: form.base_url.trim(), app_key: form.app_key.trim(), app_secret: form.app_secret.trim() }))
    msg.value = '配置已保存'
    ping.value = null
  } catch (e) {
    formError.value = `保存失败：${messageOf(e)}`
  } finally {
    saving.value = false
  }
}

async function testConnection() {
  pinging.value = true
  ping.value = null
  pingError.value = ''
  try {
    ping.value = await pingRewardTasks()
  } catch (e) {
    pingError.value = `连接失败：${reasonText(messageOf(e)) || messageOf(e)}`
  } finally {
    pinging.value = false
  }
}

async function retry(row: RewardEvent) {
  retrying.value = row.id
  listError.value = ''
  try {
    await retryRewardEvent(row.id)
    msg.value = `事件 #${row.id} 已重新排队，worker 会在 10 秒内发送`
    await Promise.all([loadList(), loadConfig()])
  } catch (e) {
    listError.value = `重试失败：${messageOf(e)}`
  } finally {
    retrying.value = null
  }
}

function switchTab(next: 'tickets' | 'events') {
  if (tab.value === next) return
  tab.value = next
  stateFilter.value = ''
  page.value = 1
}

watch([tab, stateFilter, pageSize], () => {
  page.value = 1
  void loadList()
})
watch(page, () => void loadList())

onMounted(refresh)
onBeforeUnmount(() => {
  disposed = true
  listRequest?.abort()
})
</script>

<template>
  <div id="market-reward-tasks-page" class="market-reward-page h-full min-h-0">
    <TablePageLayout content-mode="content">
      <template #title>
        <div class="page-heading">
          <h1 class="y-title">任务对接</h1>
          <p class="y-hint">接入 loxo 任务中心：loxo 用户来闲猫浏览、注册、充值后，由 loxo 给用户发奖励。</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading || listLoading" @click="refresh">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading || listLoading }" />刷新
        </button>
      </template>
      <template #filters>
        <div v-if="error" class="y-msg y-msg--error mb-3" role="alert">
          {{ error }}<button type="button" class="y-link ml-2" :disabled="loading" @click="loadConfig">重试</button>
        </div>
        <p v-if="msg" class="y-msg y-msg--ok mb-3" role="status">{{ msg }}</p>

        <section class="config-card" aria-labelledby="reward-config-title">
          <div class="section-heading">
            <h2 id="reward-config-title">对接配置</h2>
            <span v-if="config" class="status-cell" data-test="reward-status">
              <i class="status-dot" :data-tone="config.enabled ? 'ok' : 'idle'" />{{ statusText }}
            </span>
          </div>
          <form id="reward-config-form" class="config-form" @submit.prevent="save">
            <p v-if="formError" class="y-msg y-msg--error full" role="alert">{{ formError }}</p>
            <label class="y-field">
              <span>loxo 站点地址</span>
              <input v-model="form.base_url" data-test="base-url" type="url" class="input w-full" placeholder="https://loxo.cc" :disabled="saving" />
              <span class="y-hint">只填站点根地址，例如 https://loxo.cc。</span>
            </label>
            <label class="y-field">
              <span>App Key</span>
              <input v-model="form.app_key" data-test="app-key" type="text" class="input w-full" maxlength="64" placeholder="rk_ 开头" autocomplete="off" :disabled="saving" />
              <span class="y-hint">loxo 管理员登记合作站点后给出，可公开。</span>
            </label>
            <label class="y-field">
              <span>App Secret</span>
              <input v-model="form.app_secret" data-test="app-secret" type="password" class="input w-full" maxlength="256" autocomplete="new-password"
                :placeholder="config?.secret_set ? '已设置，留空表示不修改' : 'rs_ 开头，只在 loxo 创建 / 重置时显示一次'" :disabled="saving" />
              <span class="y-hint">只保存在服务端并加密存储，页面不会回显。</span>
            </label>
            <div class="toggles">
              <label class="check-field"><input v-model="form.enabled" data-test="enabled" type="checkbox" :disabled="saving" /><span>启用对接</span></label>
              <label class="check-field"><input v-model="form.sdk_enabled" data-test="sdk-enabled" type="checkbox" :disabled="saving" /><span>浏览任务加载 loxo SDK</span></label>
              <span class="y-hint">浏览任务需要 loxo 把本站域名加入合作站点的「SDK 允许域名」，并使用 sdk 校验方式。</span>
            </div>
            <div class="form-actions full">
              <button type="submit" class="btn btn-primary" data-test="save" :disabled="saving || loading">{{ saving ? '保存中…' : '保存配置' }}</button>
              <button type="button" class="btn btn-secondary" data-test="ping" :disabled="pinging || saving || !config?.secret_set" @click="testConnection">{{ pinging ? '测试中…' : '测试连接' }}</button>
              <span class="y-hint">测试连接使用已保存的凭证，未启用也可以测。</span>
            </div>
            <p v-if="ping" class="y-msg y-msg--ok full" role="status" data-test="ping-result">
              连接成功：loxo 登记的站点名称「{{ ping.partner_name }}」，App Key {{ ping.app_key }}，与 loxo 时钟相差 {{ ping.clock_skew_seconds }} 秒。
            </p>
            <p v-if="pingError" class="y-msg y-msg--error full" role="alert">{{ pingError }}</p>
          </form>
          <p class="y-hint mt-3">
            loxo 侧任务目标网址填 <code>{{ siteOrigin }}/</code>（loxo 会自动追加 <code>reward_ticket</code> 参数）。
            注册任务只给带着票据<strong>新注册</strong>的账号上报；充值任务按绑定票据后到账的钱包充值累计上报。
          </p>
        </section>

        <div class="stats-row mt-4" :aria-busy="loading">
          <article v-for="card in summary" :key="card.label" class="reward-stat">
            <div class="reward-stat__label">{{ card.label }}</div>
            <div class="reward-stat__value">{{ card.value }}</div>
            <div class="y-hint">{{ card.hint }}</div>
          </article>
        </div>

        <div class="list-toolbar mt-4">
          <div class="tabs" role="tablist">
            <button type="button" role="tab" class="btn btn-secondary btn-sm" data-test="tab-events" :aria-selected="tab === 'events'" @click="switchTab('events')">回调事件</button>
            <button type="button" role="tab" class="btn btn-secondary btn-sm" data-test="tab-tickets" :aria-selected="tab === 'tickets'" @click="switchTab('tickets')">票据</button>
          </div>
          <label class="state-filter">状态
            <select v-model="stateFilter" data-test="state-filter" class="filter-input">
              <option value="">全部</option>
              <option v-for="o in stateOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </label>
        </div>
        <p v-if="listError" class="y-msg y-msg--error mt-2" role="alert">{{ listError }}<button type="button" class="y-link ml-2" @click="loadList">重试</button></p>
      </template>
      <template #table>
        <DataTable v-if="tab === 'tickets'" :columns="ticketColumns" :data="tickets" :loading="listLoading" row-key="id" :expandable-actions="false">
          <template #cell-user="{ row }"><span class="cell-main">{{ row.user_email || `#${row.user_id}` }}</span><span class="y-hint mono" :title="row.ticket">{{ maskTicket(row.ticket) }}</span></template>
          <template #cell-task="{ row }">
            <span class="cell-main">{{ row.task_title || '—' }}</span>
            <span class="y-hint">{{ TASK_TYPES[row.task_type] || row.task_type || '待查询' }}<template v-if="row.period_key"> · {{ row.period_key }}</template></span>
          </template>
          <template #cell-state="{ row }">
            <span class="cell-main">{{ TICKET_LABELS[row.state as RewardTicketState] || row.state }}</span>
            <span v-if="row.reason" class="y-hint">{{ reasonText(row.reason) }}</span>
          </template>
          <template #cell-new_user="{ row }">{{ row.new_user ? '是' : '否' }}</template>
          <template #cell-expires_at="{ row }">{{ time(row.expires_at) }}</template>
          <template #cell-created_at="{ row }">{{ time(row.created_at) }}</template>
          <template #empty>
            <EmptyState v-if="!listError" title="暂无票据" description="用户从 loxo 任务中心跳转过来并注册或登录后，这里会出现绑定记录" />
            <p v-else class="y-hint">加载失败，请重试</p>
          </template>
        </DataTable>
        <DataTable v-else :columns="eventColumns" :data="events" :loading="listLoading" row-key="id" :expandable-actions="false">
          <template #cell-user="{ row }"><span class="cell-main">{{ row.user_email || `#${row.user_id}` }}</span><span class="y-hint">{{ row.task_title || '—' }}</span></template>
          <template #cell-event_type="{ row }">
            <span class="cell-main">{{ TASK_TYPES[row.event_type] || row.event_type }}</span>
            <span v-if="row.recharge_order_id" class="y-hint">充值单 #{{ row.recharge_order_id }}</span>
          </template>
          <template #cell-amount="{ row }">{{ row.amount ?? '—' }}</template>
          <template #cell-state="{ row }">
            <span class="cell-main">{{ EVENT_LABELS[row.state as RewardEventState] || row.state }}</span>
            <span v-if="row.reason" class="y-hint">{{ reasonText(row.reason) }}</span>
          </template>
          <template #cell-when="{ row }">
            <span v-if="row.state === 'pending'">{{ time(row.next_attempt_at) }}</span>
            <span v-else>{{ time(row.sent_at) }}</span>
          </template>
          <template #cell-actions="{ row }">
            <button v-if="row.state === 'rejected' || row.state === 'failed'" type="button" class="y-link" :data-test="`retry-${row.id}`" :disabled="retrying !== null" @click="retry(row)">
              {{ retrying === row.id ? '重试中…' : '重试' }}
            </button>
            <span v-else class="y-hint">—</span>
          </template>
          <template #empty>
            <EmptyState v-if="!listError" title="暂无回调事件" description="注册 / 充值任务的回调会在这里显示发送结果" />
            <p v-else class="y-hint">加载失败，请重试</p>
          </template>
        </DataTable>
        <Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" :page-size-options="[20, 50, 100]" @update:page="page = $event" @update:page-size="pageSize = $event" />
      </template>
    </TablePageLayout>
  </div>
</template>

<style scoped>
.page-heading { min-width: 0; }
.page-action-btn { height: 32px; padding: 0 14px; font-size: 13px; border-radius: 8px; display: inline-flex; align-items: center; gap: 6px; }
.y-hint { margin: 0; font-size: 12px; line-height: 1.6; color: var(--color-text-secondary, #64748b); }
.config-card { padding: 16px; border-radius: 12px; border: 1px solid var(--color-border, #e2e8f0); background: var(--color-bg-secondary, #f8fafc); }
.section-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
.section-heading h2 { font-size: 13px; font-weight: 600; color: var(--color-text-primary, #262626); }
.config-form { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px 16px; }
.config-form .full { grid-column: 1 / -1; }
.y-field { display: grid; gap: 4px; font-size: 13px; align-content: start; }
.toggles { grid-column: 1 / -1; display: flex; align-items: center; flex-wrap: wrap; gap: 8px 20px; font-size: 13px; }
.check-field { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.form-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.status-cell { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; }
.status-dot { width: 7px; height: 7px; border-radius: 50%; background: #94a3b8; }
.status-dot[data-tone='ok'] { background: #16a34a; }
.stats-row { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 12px; }
.reward-stat { padding: 14px 16px; border-radius: 12px; border: 1px solid var(--color-border, #e2e8f0); background: var(--color-bg-secondary, #f8fafc); min-width: 0; }
.reward-stat__label { font-size: 12px; color: var(--color-text-secondary, #64748b); }
.reward-stat__value { margin: 4px 0; font-size: 22px; font-weight: 700; line-height: 1.3; font-variant-numeric: tabular-nums; }
.list-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.tabs { display: flex; gap: 8px; }
.tabs [aria-selected='true'] { color: var(--color-primary, #2563eb); border-color: currentColor; }
.state-filter { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; white-space: nowrap; }
.state-filter select { width: 8.5rem; flex: none; }
.cell-main { display: block; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
code { font-size: 12px; padding: 1px 4px; border-radius: 4px; background: var(--color-bg-tertiary, #eef2f7); }
.market-reward-page :deep(td), .market-reward-page :deep(th) { font-size: 13px; }
.market-reward-page :deep(td.col-state) { min-width: 14rem; white-space: normal; }
@media (max-width: 1200px) {
  .config-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .stats-row { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 767px) {
  .config-form { grid-template-columns: minmax(0, 1fr); }
  .stats-row { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .reward-stat { padding: 10px; }
  .page-heading .y-hint { display: none; }
}
</style>
