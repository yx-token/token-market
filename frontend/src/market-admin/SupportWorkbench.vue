<script setup lang="ts">
/**
 * 官方客服工作台：长轮询同步消息/已读/输入中，列表随未读变化刷新。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminSupport' })

const router = useRouter()
const LAYOUT_KEY = 'market-support-layout-v1'
type DragKind = 'queue' | 'list' | 'metrics' | 'compose' | 'log' | 'queueRow' | 'listRow'

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n))
}

interface Conversation {
  id: number
  user_id: number
  user_email?: string
  status: string
  last_message_at: string
  last_preview: string
  last_sender: string
  user_unread_count: number
  staff_unread_count: number
  awaiting_staff_since?: string | null
  user_last_read_msg_id?: number
  staff_last_read_msg_id?: number
  user_typing?: boolean
  staff_typing?: boolean
  peer_typing?: boolean
  overdue?: boolean
  waiting_minutes?: number
}

interface Message {
  id: number
  conversation_id: number
  sender: string
  body: string
  created_at: string
}

interface Alerts {
  pending_count: number
  overdue_count: number
  staff_unread_total: number
  pending_alert_threshold: number
  reply_timeout_minutes: number
  pending_alert_fired: boolean
  overdue_conversations: Conversation[]
}

const WAIT_MS = 20000

const status = ref('pending')
const q = ref('')
const list = ref<Conversation[]>([])
const total = ref(0)
const selectedId = ref(0)
const messages = ref<Message[]>([])
const draft = ref('')
const alerts = ref<Alerts | null>(null)
const loadingList = ref(false)
const loadingThread = ref(false)
const sending = ref(false)
const error = ref('')
const peerTyping = ref(false)
const peerReadId = ref(0)
const listEl = ref<HTMLElement | null>(null)
let typingTimer: ReturnType<typeof setTimeout> | null = null
let typingSent = false
let threadAbort: AbortController | null = null
let threadGen = 0
let alertsAbort: AbortController | null = null
let alertsGen = 0

const QUEUE_GROUPS = [
  {
    title: '工作队列',
    items: [
      { value: 'pending', label: '待回复', countKey: 'pending' as const },
      { value: 'overdue', label: '已超时', countKey: 'overdue' as const },
      { value: 'open', label: '进行中', countKey: null },
      { value: 'closed', label: '已关闭', countKey: null },
      { value: '', label: '全部会话', countKey: null },
    ],
  },
]

const detailTab = ref<'thread' | 'customer'>('thread')
const queueOpen = ref(true)
const historyOpen = ref(false)
const historyEl = ref<HTMLElement | null>(null)

const queueW = ref(200)
const listW = ref(340)
const queueH = ref(160)
const listH = ref(220)
const metricsH = ref(108)
const composeH = ref(110)
const logH = ref(140)
const stacked = ref(false)
let dragCleanup: (() => void) | null = null
let stackMqCleanup: (() => void) | null = null

const boardStyle = computed(() => {
  if (stacked.value) {
    return {
      gridTemplateColumns: '1fr',
      gridTemplateRows: `${queueH.value}px 8px ${listH.value}px 8px minmax(0, 1fr)`,
      gap: '0',
    }
  }
  return {
    gridTemplateColumns: `${queueW.value}px 8px ${listW.value}px 8px minmax(0, 1fr)`,
    gridTemplateRows: 'minmax(0, 1fr)',
    gap: '0 8px',
  }
})

function loadLayout() {
  try {
    const raw = localStorage.getItem(LAYOUT_KEY)
    if (!raw) return
    const o = JSON.parse(raw) as Record<string, number>
    if (Number.isFinite(o.queueW)) queueW.value = clamp(o.queueW, 140, 420)
    if (Number.isFinite(o.listW)) listW.value = clamp(o.listW, 220, 720)
    if (Number.isFinite(o.queueH)) queueH.value = clamp(o.queueH, 100, 420)
    if (Number.isFinite(o.listH)) listH.value = clamp(o.listH, 120, 520)
    if (Number.isFinite(o.metricsH)) metricsH.value = clamp(o.metricsH, 52, 320)
    if (Number.isFinite(o.composeH)) composeH.value = clamp(o.composeH, 72, 360)
    if (Number.isFinite(o.logH)) logH.value = clamp(o.logH, 72, 420)
  } catch {
    /* ignore */
  }
}

function saveLayout() {
  localStorage.setItem(
    LAYOUT_KEY,
    JSON.stringify({
      queueW: queueW.value,
      listW: listW.value,
      queueH: queueH.value,
      listH: listH.value,
      metricsH: metricsH.value,
      composeH: composeH.value,
      logH: logH.value,
    }),
  )
}

function startResize(kind: DragKind, e: MouseEvent) {
  e.preventDefault()
  dragCleanup?.()
  const startX = e.clientX
  const startY = e.clientY
  const start = {
    queueW: queueW.value,
    listW: listW.value,
    queueH: queueH.value,
    listH: listH.value,
    metricsH: metricsH.value,
    composeH: composeH.value,
    logH: logH.value,
  }
  const vertical = kind === 'metrics' || kind === 'compose' || kind === 'log' || kind === 'queueRow' || kind === 'listRow'
  const onMove = (ev: MouseEvent) => {
    const dx = ev.clientX - startX
    const dy = ev.clientY - startY
    if (kind === 'queue') queueW.value = clamp(start.queueW + dx, 140, 420)
    else if (kind === 'list') listW.value = clamp(start.listW + dx, 220, 720)
    else if (kind === 'queueRow') queueH.value = clamp(start.queueH + dy, 100, 420)
    else if (kind === 'listRow') listH.value = clamp(start.listH + dy, 120, 520)
    else if (kind === 'metrics') metricsH.value = clamp(start.metricsH + dy, 52, 320)
    else if (kind === 'compose') composeH.value = clamp(start.composeH - dy, 72, 360)
    else if (kind === 'log') logH.value = clamp(start.logH - dy, 72, 420)
  }
  const onUp = () => {
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
    document.body.style.removeProperty('cursor')
    document.body.style.removeProperty('user-select')
    dragCleanup = null
    saveLayout()
  }
  document.body.style.cursor = vertical ? 'row-resize' : 'col-resize'
  document.body.style.userSelect = 'none'
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
  dragCleanup = onUp
}

const selected = computed(() => list.value.find((c) => c.id === selectedId.value) || null)
const lastMsgId = computed(() => messages.value.reduce((m, x) => Math.max(m, x.id), 0))
const lastOwnId = computed(() => {
  let id = 0
  for (const m of messages.value) {
    if (m.sender === 'staff') id = m.id
  }
  return id
})
const systemLogs = computed(() => messages.value.filter((m) => m.sender === 'system'))
const chatMessages = computed(() => messages.value.filter((m) => m.sender !== 'system'))

const stats = computed(() => {
  const a = alerts.value
  const overdueAvg = (() => {
    const rows = a?.overdue_conversations || []
    if (!rows.length) return '—'
    const mins = rows.reduce((s, c) => s + (c.waiting_minutes ?? 0), 0) / rows.length
    if (mins < 1) return '<1m'
    if (mins < 60) return `${Math.round(mins)}m`
    return `${(mins / 60).toFixed(1)}h`
  })()
  return [
    { key: 'pending', label: '待回复', value: a?.pending_count ?? '—', hint: '等待官方回复', tone: 'blue', icon: 'inbox' as const },
    { key: 'overdue', label: '已超时', value: a?.overdue_count ?? '—', hint: `${a?.reply_timeout_minutes ?? 15} 分钟阈值`, tone: 'red', icon: 'exclamationTriangle' as const },
    { key: 'unread', label: '未读消息', value: a?.staff_unread_total ?? '—', hint: '顶栏角标同步', tone: 'amber', icon: 'chatBubble' as const },
    { key: 'queue', label: '当前列表', value: total.value, hint: STATUS_LABEL[status.value] || '全部', tone: 'violet', icon: 'clipboard' as const },
    { key: 'threshold', label: '积压阈值', value: a?.pending_alert_threshold ?? 5, hint: a?.pending_alert_fired ? '已触发告警' : '未触发', tone: 'slate', icon: 'badge' as const },
    { key: 'avg', label: '超时均等待', value: overdueAvg, hint: '超时会话均值', tone: 'cyan', icon: 'clock' as const },
  ]
})

const STATUS_LABEL: Record<string, string> = {
  pending: '待回复',
  overdue: '已超时',
  open: '进行中',
  closed: '已关闭',
  '': '全部',
}

function queueCount(key: 'pending' | 'overdue' | null) {
  if (key === 'pending') return alerts.value?.pending_count
  if (key === 'overdue') return alerts.value?.overdue_count
  return undefined
}

function avatarText(c: Conversation) {
  const raw = (c.user_email || String(c.user_id || '')).trim()
  if (!raw) return '?'
  const name = raw.includes('@') ? raw.split('@')[0] : raw
  return name.slice(0, 1).toUpperCase()
}

function relativeTime(v?: string) {
  if (!v) return ''
  const t = new Date(v).getTime()
  if (Number.isNaN(t)) return v
  const diff = Date.now() - t
  if (diff < 60_000) return '刚刚'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)} 小时前`
  return fmtTime(v)
}

function priorityOf(c: Conversation) {
  if (c.overdue) return { label: '紧急', tone: 'danger' }
  if (c.awaiting_staff_since) return { label: '待回复', tone: 'warn' }
  if (c.status === 'closed') return { label: '已关闭', tone: 'muted' }
  return { label: '进行中', tone: 'info' }
}

function fmtTime(v?: string) {
  if (!v) return ''
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function selectQueue(value: string) {
  status.value = value
  detailTab.value = 'thread'
}

function senderLabel(sender: string) {
  if (sender === 'staff') return '官方'
  if (sender === 'user') return '用户'
  if (sender === 'system') return '系统'
  return sender
}

async function openHistory() {
  if (!selectedId.value) return
  historyOpen.value = true
  // 重新拉全量，避免侧栏截断后漏看早期消息
  try {
    const bundle = await marketGet<{ conversation: Conversation; messages: Message[] }>(
      `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}`,
    )
    messages.value = bundle.messages ?? []
    applyConv(bundle.conversation)
  } catch {
    /* 仍展示当前内存中的消息 */
  }
  await nextTick()
  const el = historyEl.value
  if (el) el.scrollTop = el.scrollHeight
}

function closeHistory() {
  historyOpen.value = false
}

function refreshAll() {
  void loadList()
  void loadAlerts()
}

function waitLabel(c: Conversation) {
  if (!c.awaiting_staff_since) return ''
  const mins = c.waiting_minutes ?? Math.floor((Date.now() - new Date(c.awaiting_staff_since).getTime()) / 60000)
  if (mins < 1) return '刚到'
  if (mins < 60) return `等 ${mins} 分`
  return `等 ${Math.floor(mins / 60)} 时`
}

function receiptOf(m: Message) {
  if (m.sender !== 'staff' || m.id !== lastOwnId.value) return ''
  return peerReadId.value >= m.id ? '已读' : '未读'
}

function applyConv(c: Conversation) {
  peerTyping.value = !!c.peer_typing
  peerReadId.value = Number(c.user_last_read_msg_id ?? 0)
  const idx = list.value.findIndex((x) => x.id === c.id)
  if (idx >= 0) list.value[idx] = { ...list.value[idx], ...c }
}

async function scrollBottom() {
  await nextTick()
  const el = listEl.value
  if (el) el.scrollTop = el.scrollHeight
}

async function loadAlerts() {
  try {
    alerts.value = await marketGet<Alerts>(`${MARKET_API_PREFIX}/support/alerts`)
  } catch {
    /* ignore */
  }
}

async function loadList(keepSelection = true, silent = false) {
  if (!silent) loadingList.value = true
  error.value = ''
  try {
    const params = new URLSearchParams()
    params.set('limit', '50')
    if (status.value) params.set('status', status.value)
    if (q.value.trim()) params.set('q', q.value.trim())
    const res = await marketGet<{ items: Conversation[] | null; total: number }>(
      `${MARKET_API_PREFIX}/support/conversations?${params}`,
    )
    const prevId = selectedId.value
    // 回复后会话会离开「待回复/已超时」筛选；钉住当前会话，方便连续回多条
    const pinned =
      keepSelection && prevId
        ? list.value.find((c) => c.id === prevId) || selected.value
        : null
    list.value = res.items ?? []
    total.value = Number(res.total ?? list.value.length)
    if (keepSelection && prevId) {
      if (!list.value.some((c) => c.id === prevId) && pinned && pinned.id === prevId) {
        list.value = [{ ...pinned, awaiting_staff_since: null, overdue: false }, ...list.value]
      }
      selectedId.value = prevId
    } else if (list.value.length) {
      await openConversation(list.value[0].id)
    } else {
      selectedId.value = 0
      messages.value = []
      peerTyping.value = false
    }
  } catch (e) {
    if (!silent) error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loadingList.value = false
  }
}

async function openConversation(id: number) {
  if (selectedId.value && selectedId.value !== id) await clearTyping()
  stopThreadLoop()
  if (selectedId.value !== id) historyOpen.value = false
  selectedId.value = id
  loadingThread.value = true
  error.value = ''
  try {
    const bundle = await marketGet<{ conversation: Conversation; messages: Message[] }>(
      `${MARKET_API_PREFIX}/support/conversations/${id}`,
    )
    messages.value = bundle.messages ?? []
    applyConv(bundle.conversation)
    await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
    window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
    const idx = list.value.findIndex((c) => c.id === id)
    if (idx >= 0) list.value[idx].staff_unread_count = 0
    await scrollBottom()
    void threadLoop()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载会话失败'
  } finally {
    loadingThread.value = false
  }
}

function stopThreadLoop() {
  threadGen += 1
  threadAbort?.abort()
  threadAbort = null
}

async function threadLoop() {
  const gen = ++threadGen
  while (gen === threadGen && selectedId.value) {
    const id = selectedId.value
    const ac = new AbortController()
    threadAbort = ac
    try {
      const q = new URLSearchParams({
        after_id: String(lastMsgId.value),
        peer_read: String(peerReadId.value),
        peer_typing: peerTyping.value ? '1' : '0',
        unread: String(list.value.find((c) => c.id === id)?.staff_unread_count ?? 0),
        status: selected.value?.status || 'open',
        wait_ms: String(WAIT_MS),
      })
      const bundle = await marketGet<{ conversation: Conversation; messages: Message[] }>(
        `${MARKET_API_PREFIX}/support/conversations/${id}?${q}`,
        { signal: ac.signal, timeoutMs: WAIT_MS + 8000 },
      )
      if (gen !== threadGen || selectedId.value !== id) break
      applyConv(bundle.conversation)
      const rows = bundle.messages ?? []
      if (rows.length) {
        const known = new Set(messages.value.map((m) => m.id))
        const add = rows.filter((m) => !known.has(m.id))
        if (add.length) {
          messages.value = [...messages.value, ...add]
          // 仅前台可见时标已读，避免后台页签把顶栏未读角标瞬间清掉
          if (document.visibilityState === 'visible') {
            await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
            window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
          }
          await scrollBottom()
        }
        void loadList(true, true)
      } else if (bundle.conversation.staff_unread_count !== undefined) {
        void loadList(true, true)
      }
    } catch {
      if (gen !== threadGen) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
}

function stopAlertsLoop() {
  alertsGen += 1
  alertsAbort?.abort()
  alertsAbort = null
}

async function alertsLoop() {
  const gen = ++alertsGen
  while (gen === alertsGen) {
    const ac = new AbortController()
    alertsAbort = ac
    try {
      const pending = alerts.value?.pending_count ?? 0
      const overdue = alerts.value?.overdue_count ?? 0
      const unread = alerts.value?.staff_unread_total ?? 0
      const q = new URLSearchParams({
        pending: String(pending),
        overdue: String(overdue),
        unread: String(unread),
        wait_ms: String(WAIT_MS),
      })
      const a = await marketGet<Alerts>(`${MARKET_API_PREFIX}/support/alerts?${q}`, {
        signal: ac.signal,
        timeoutMs: WAIT_MS + 8000,
      })
      if (gen !== alertsGen) break
      const changed =
        !alerts.value ||
        a.pending_count !== alerts.value.pending_count ||
        a.overdue_count !== alerts.value.overdue_count ||
        a.staff_unread_total !== alerts.value.staff_unread_total
      alerts.value = a
      if (changed) void loadList(true, true)
    } catch {
      if (gen !== alertsGen) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
}

async function send() {
  const body = draft.value.trim()
  if (!body || !selectedId.value || sending.value) return
  sending.value = true
  error.value = ''
  try {
    await clearTyping()
    const msg = await marketSend<Message>(
      'POST',
      `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/messages`,
      { body },
    )
    draft.value = ''
    if (!messages.value.some((m) => m.id === msg.id)) messages.value = [...messages.value, msg]
    const idx = list.value.findIndex((c) => c.id === selectedId.value)
    if (idx >= 0) {
      list.value[idx] = {
        ...list.value[idx],
        last_preview: body.slice(0, 80),
        last_sender: 'staff',
        awaiting_staff_since: null,
        overdue: false,
        staff_unread_count: 0,
        peer_typing: false,
      }
    }
    peerTyping.value = false
    await scrollBottom()
    void loadAlerts()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '发送失败'
  } finally {
    sending.value = false
  }
}

async function closeConversation() {
  if (!selectedId.value) return
  if (!confirm('确认关闭该会话？用户仍可再发消息重新打开。')) return
  await clearTyping()
  try {
    await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/close`)
    await loadList(false)
    void loadAlerts()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '关闭失败'
  }
}

async function clearTyping() {
  if (typingTimer) {
    clearTimeout(typingTimer)
    typingTimer = null
  }
  if (!typingSent || !selectedId.value) return
  typingSent = false
  await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/typing`, {
    typing: false,
  }).catch(() => null)
}

function onDraftInput() {
  if (!selectedId.value || selected.value?.status === 'closed') return
  if (!typingSent) {
    typingSent = true
    void marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/typing`, {
      typing: true,
    }).catch(() => null)
  }
  if (typingTimer) clearTimeout(typingTimer)
  typingTimer = setTimeout(() => {
    typingSent = false
    if (!selectedId.value) return
    void marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/typing`, {
      typing: false,
    }).catch(() => null)
  }, 2500)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    void send()
  }
}

watch(status, () => void loadList(false))

onMounted(async () => {
  loadLayout()
  const mq = window.matchMedia('(max-width: 960px)')
  const applyStack = () => {
    stacked.value = mq.matches
  }
  applyStack()
  mq.addEventListener('change', applyStack)
  stackMqCleanup = () => mq.removeEventListener('change', applyStack)
  await Promise.all([loadList(false), loadAlerts()])
  void alertsLoop()
})

onUnmounted(() => {
  dragCleanup?.()
  dragCleanup = null
  stackMqCleanup?.()
  stackMqCleanup = null
  stopThreadLoop()
  stopAlertsLoop()
  void clearTyping()
})
</script>

<template>
  <section id="market-support-page" class="sw-shell">
    <header class="sw-top">
      <div class="sw-top-title">
        <h2>官方客服工作台</h2>
        <p>用户留言 · {{ alerts?.reply_timeout_minutes ?? 15 }} 分钟未回预警 · 10 分钟无消息自动结束</p>
      </div>
      <div class="sw-top-search">
        <Icon name="search" size="sm" class="sw-search-ico" />
        <input
          v-model="q"
          type="search"
          placeholder="搜索邮箱、内容预览、会话 ID"
          @keydown.enter="loadList(false)"
        />
        <button type="button" class="sw-link-btn" @click="loadList(false)">搜索</button>
      </div>
      <div class="sw-top-actions">
        <button type="button" class="sw-refresh" @click="router.push('/admin/market/support-h5')">
          手机版
        </button>
        <button type="button" class="sw-refresh" :disabled="loadingList" @click="refreshAll">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingList }" />
          刷新
        </button>
      </div>
    </header>

    <div
      v-if="alerts && (alerts.pending_alert_fired || alerts.overdue_count > 0)"
      class="sw-banner"
      :class="{ hot: alerts.pending_alert_fired }"
    >
      <template v-if="alerts.pending_alert_fired">
        积压告警：待回复 {{ alerts.pending_count }} 条（阈值 {{ alerts.pending_alert_threshold }}）
      </template>
      <template v-else>超时未回 {{ alerts.overdue_count }} 条</template>
    </div>

    <div class="sw-metrics-wrap" :style="{ height: `${metricsH}px` }">
      <div class="sw-metric-row">
        <article v-for="s in stats" :key="s.key" class="sw-metric" :class="s.tone">
          <div class="sw-metric-ico"><Icon :name="s.icon" size="sm" /></div>
          <div class="sw-metric-body">
            <span>{{ s.label }}</span>
            <strong>{{ s.value }}</strong>
            <em>{{ s.hint }}</em>
          </div>
        </article>
      </div>
    </div>
    <div
      class="sw-splitter sw-splitter--row"
      title="拖拽调整指标区高度"
      @mousedown="startResize('metrics', $event)"
    />

    <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>

    <div class="sw-board" :class="{ stacked }" :style="boardStyle">
      <aside class="sw-queue">
        <button type="button" class="sw-queue-head" @click="queueOpen = !queueOpen">
          <span>工作队列</span>
          <Icon :name="queueOpen ? 'chevronUp' : 'chevronDown'" size="sm" />
        </button>
        <div v-show="queueOpen" class="sw-queue-body">
          <template v-for="g in QUEUE_GROUPS" :key="g.title">
            <p class="sw-queue-label">{{ g.title }}</p>
            <button
              v-for="item in g.items"
              :key="item.value || 'all'"
              type="button"
              class="sw-queue-item"
              :class="{ on: status === item.value }"
              @click="selectQueue(item.value)"
            >
              <span>{{ item.label }}</span>
              <b v-if="queueCount(item.countKey) != null">{{ queueCount(item.countKey) }}</b>
            </button>
          </template>
          <div v-if="alerts?.overdue_conversations?.length" class="sw-queue-overdue">
            <p class="sw-queue-label">超时会话</p>
            <button
              v-for="c in alerts.overdue_conversations"
              :key="`od-${c.id}`"
              type="button"
              class="sw-queue-mini"
              @click="openConversation(c.id)"
            >
              <strong>{{ c.user_email || `#${c.user_id}` }}</strong>
              <em>{{ waitLabel(c) }}</em>
            </button>
          </div>
        </div>
      </aside>

      <div
        class="sw-splitter"
        :class="stacked ? 'sw-splitter--row' : 'sw-splitter--col'"
        :title="stacked ? '拖拽调整队列高度' : '拖拽调整队列宽度'"
        @mousedown="startResize(stacked ? 'queueRow' : 'queue', $event)"
      />

      <section class="sw-list-pane">
        <header class="sw-list-toolbar">
          <div class="sw-filters">
            <span class="sw-chip">{{ STATUS_LABEL[status] || '全部' }}</span>
            <span class="sw-chip muted">实时长轮询</span>
          </div>
          <span class="sw-list-total">共 {{ total }} 条</span>
        </header>
        <ul class="sw-cards">
          <li v-if="loadingList && !list.length" class="sw-empty">加载中…</li>
          <li v-else-if="!list.length" class="sw-empty">暂无会话</li>
          <li
            v-for="c in list"
            :key="c.id"
            class="sw-card"
            :class="{ on: c.id === selectedId, overdue: c.overdue }"
            @click="openConversation(c.id)"
          >
            <div class="sw-card-avatar" :data-tone="priorityOf(c).tone">{{ avatarText(c) }}</div>
            <div class="sw-card-main">
              <div class="sw-card-top">
                <strong>{{ c.user_email || `用户 #${c.user_id}` }}</strong>
                <span class="sw-tag" :data-tone="priorityOf(c).tone">{{ priorityOf(c).label }}</span>
                <span v-if="c.staff_unread_count" class="sw-dot">{{ c.staff_unread_count }}</span>
                <time>{{ relativeTime(c.last_message_at) }}</time>
              </div>
              <p v-if="c.peer_typing || c.user_typing" class="sw-typing-hint">对方正在输入…</p>
              <p v-else class="sw-card-preview">{{ c.last_preview || '（无内容）' }}</p>
              <div class="sw-card-meta">
                <span>#{{ c.id }}</span>
                <span>{{ c.last_sender === 'user' ? '用户留言' : c.last_sender === 'staff' ? '官方已回' : '系统消息' }}</span>
                <em v-if="c.awaiting_staff_since" :class="{ hot: c.overdue }">{{ waitLabel(c) }}</em>
              </div>
            </div>
          </li>
        </ul>
      </section>

      <div
        class="sw-splitter"
        :class="stacked ? 'sw-splitter--row' : 'sw-splitter--col'"
        :title="stacked ? '拖拽调整列表高度' : '拖拽调整列表宽度'"
        @mousedown="startResize(stacked ? 'listRow' : 'list', $event)"
      />

      <main class="sw-detail">
        <template v-if="selected">
          <header class="sw-detail-head">
            <div class="sw-detail-user">
              <div class="sw-card-avatar lg" :data-tone="priorityOf(selected).tone">{{ avatarText(selected) }}</div>
              <div>
                <strong>{{ selected.user_email || `用户 #${selected.user_id}` }}</strong>
                <div class="sw-detail-tags">
                  <span class="sw-tag" :data-tone="priorityOf(selected).tone">{{ priorityOf(selected).label }}</span>
                  <span class="sw-tag muted">#{{ selected.id }}</span>
                  <em v-if="peerTyping" class="sw-peer-typing">对方正在输入…</em>
                </div>
              </div>
            </div>
            <div class="sw-detail-actions">
              <button type="button" class="sw-btn ghost" @click="openHistory">
                全部聊天记录
              </button>
              <button
                v-if="selected.status !== 'closed'"
                type="button"
                class="sw-btn ghost"
                @click="closeConversation"
              >
                结束会话
              </button>
              <span v-else class="sw-closed-tag">已结束</span>
            </div>
          </header>

          <div class="sw-detail-tabs">
            <button type="button" :class="{ on: detailTab === 'thread' }" @click="detailTab = 'thread'">会话详情</button>
            <button type="button" :class="{ on: detailTab === 'customer' }" @click="detailTab = 'customer'">客户信息</button>
          </div>

          <div v-if="detailTab === 'thread'" class="sw-detail-body">
            <div ref="listEl" class="sw-msgs">
              <p v-if="loadingThread" class="sw-empty">加载中…</p>
              <div
                v-for="m in chatMessages"
                :key="m.id"
                class="sw-msg"
                :class="{ staff: m.sender === 'staff', user: m.sender === 'user' }"
              >
                <div class="sw-bubble">
                  <p>{{ m.body }}</p>
                  <div class="sw-meta">
                    <time>{{ fmtTime(m.created_at) }} · {{ m.sender === 'user' ? '用户' : '官方' }}</time>
                    <span v-if="receiptOf(m)" class="sw-read" :class="{ on: receiptOf(m) === '已读' }">{{ receiptOf(m) }}</span>
                  </div>
                </div>
              </div>
              <div v-if="peerTyping" class="sw-msg user">
                <div class="sw-bubble sw-typing-dots"><span /><span /><span /></div>
              </div>
            </div>

            <div
              class="sw-splitter sw-splitter--row sw-splitter--inpane"
              title="拖拽调整输入区高度"
              @mousedown="startResize('compose', $event)"
            />
            <footer class="sw-compose" :style="{ height: `${composeH}px` }">
              <textarea
                v-model="draft"
                maxlength="2000"
                placeholder="输入回复内容，Enter 发送，Shift+Enter 换行"
                :disabled="selected.status === 'closed'"
                @input="onDraftInput"
                @keydown="onKey"
              />
              <div class="sw-compose-bar">
                <span>{{ draft.length }}/2000</span>
                <button
                  type="button"
                  class="sw-btn primary"
                  :disabled="sending || !draft.trim() || selected.status === 'closed'"
                  @click="send"
                >
                  {{ sending ? '发送中…' : '回复' }}
                </button>
              </div>
            </footer>

            <template v-if="systemLogs.length">
              <div
                class="sw-splitter sw-splitter--row sw-splitter--inpane"
                title="拖拽调整处理记录高度"
                @mousedown="startResize('log', $event)"
              />
              <section class="sw-log" :style="{ height: `${logH}px` }">
                <h4>处理记录</h4>
                <ol>
                  <li v-for="m in systemLogs" :key="m.id">
                    <i />
                    <div>
                      <p>{{ m.body }}</p>
                      <time>{{ fmtTime(m.created_at) }}</time>
                    </div>
                  </li>
                </ol>
              </section>
            </template>
          </div>

          <div v-else class="sw-customer">
            <dl>
              <div><dt>邮箱</dt><dd>{{ selected.user_email || '—' }}</dd></div>
              <div><dt>用户 ID</dt><dd>{{ selected.user_id }}</dd></div>
              <div><dt>会话 ID</dt><dd>{{ selected.id }}</dd></div>
              <div><dt>状态</dt><dd>{{ priorityOf(selected).label }}</dd></div>
              <div><dt>最近消息</dt><dd>{{ fmtTime(selected.last_message_at) }}</dd></div>
              <div><dt>等待时长</dt><dd>{{ waitLabel(selected) || '—' }}</dd></div>
              <div><dt>未读</dt><dd>{{ selected.staff_unread_count }}</dd></div>
            </dl>
          </div>
        </template>
        <div v-else class="sw-empty tall">
          <p>从中间列表选择一条会话开始处理</p>
        </div>
      </main>
    </div>

    <Teleport to="body">
      <div
        v-if="historyOpen && selected"
        class="sw-history-mask"
        @click.self="closeHistory"
      >
        <div class="sw-history" role="dialog" aria-modal="true" aria-label="全部聊天记录">
          <header class="sw-history-head">
            <div>
              <strong>全部聊天记录</strong>
              <p>
                {{ selected.user_email || `用户 #${selected.user_id}` }}
                · #{{ selected.id }}
                · 共 {{ messages.length }} 条
              </p>
            </div>
            <button type="button" class="sw-history-x" aria-label="关闭" @click="closeHistory">×</button>
          </header>
          <div ref="historyEl" class="sw-history-body">
            <p v-if="!messages.length" class="sw-empty">暂无消息</p>
            <div
              v-for="m in messages"
              :key="`h-${m.id}`"
              class="sw-history-row"
              :class="m.sender"
            >
              <div class="sw-history-bubble">
                <div class="sw-history-meta">
                  <b>{{ senderLabel(m.sender) }}</b>
                  <time>{{ fmtTime(m.created_at) }}</time>
                  <span class="sw-history-id">#{{ m.id }}</span>
                </div>
                <p>{{ m.body }}</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.sw-shell {
  --sw-bg: #f3f5f9;
  --sw-card: #fff;
  --sw-line: #e8ecf2;
  --sw-text: #1f2937;
  --sw-muted: #8b95a7;
  --sw-primary: #3b82f6;
  --sw-primary-soft: #eff6ff;
  display: flex;
  flex-direction: column;
  gap: 10px;
  flex: 1;
  min-height: 0;
  height: 100%;
  max-height: 100%;
  overflow: hidden;
  color: var(--sw-text);
}
.sw-top {
  flex-shrink: 0;
  display: grid;
  grid-template-columns: minmax(180px, 240px) 1fr auto;
  gap: 14px;
  align-items: center;
}
.sw-top-title h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: -0.02em;
}
.sw-top-title p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--sw-muted);
}
.sw-top-search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 40px;
  padding: 0 12px;
  border: 1px solid var(--sw-line);
  border-radius: 10px;
  background: var(--sw-card);
}
.sw-search-ico { color: var(--sw-muted); flex-shrink: 0; }
.sw-top-search input {
  flex: 1;
  border: 0;
  outline: none;
  background: transparent;
  font-size: 13px;
  min-width: 0;
}
.sw-link-btn {
  border: 0;
  background: transparent;
  color: var(--sw-primary);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}
.sw-top-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.sw-refresh {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 40px;
  padding: 0 14px;
  border: 1px solid var(--sw-line);
  border-radius: 10px;
  background: var(--sw-card);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}
.sw-refresh:disabled { opacity: 0.6; cursor: not-allowed; }
.sw-banner {
  flex-shrink: 0;
  padding: 10px 14px;
  border-radius: 10px;
  background: #fff7ed;
  color: #9a3412;
  font-size: 13px;
  font-weight: 600;
}
.sw-banner.hot {
  background: #fef2f2;
  color: #b91c1c;
}
.sw-metrics-wrap {
  flex-shrink: 0;
  min-height: 52px;
  overflow: hidden;
}
.sw-metric-row {
  height: 100%;
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
  overflow: auto;
}
.sw-splitter {
  position: relative;
  z-index: 2;
  flex-shrink: 0;
  touch-action: none;
}
.sw-splitter--col {
  width: 8px;
  margin: 0 -4px;
  cursor: col-resize;
}
.sw-splitter--row {
  height: 8px;
  margin: -2px 0;
  cursor: row-resize;
  flex-shrink: 0;
}
.sw-splitter--inpane {
  margin: 0;
  background: transparent;
  border-top: 1px solid var(--sw-line);
}
.sw-splitter::after {
  content: '';
  position: absolute;
  inset: 2px;
  border-radius: 999px;
  background: transparent;
  transition: background 0.15s ease;
}
.sw-splitter--col::after {
  top: 18%;
  bottom: 18%;
  left: 3px;
  right: 3px;
}
.sw-splitter--row::after {
  left: 28%;
  right: 28%;
  top: 3px;
  bottom: 3px;
}
.sw-splitter:hover::after,
.sw-splitter:active::after {
  background: #93c5fd;
}
.sw-metric {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 12px;
  border-radius: 12px;
  background: var(--sw-card);
  border: 1px solid var(--sw-line);
}
.sw-metric-ico {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: #f1f5f9;
  color: #64748b;
  flex-shrink: 0;
}
.sw-metric.blue .sw-metric-ico { background: #eff6ff; color: #2563eb; }
.sw-metric.red .sw-metric-ico { background: #fef2f2; color: #dc2626; }
.sw-metric.amber .sw-metric-ico { background: #fffbeb; color: #d97706; }
.sw-metric.violet .sw-metric-ico { background: #f5f3ff; color: #7c3aed; }
.sw-metric.slate .sw-metric-ico { background: #f8fafc; color: #475569; }
.sw-metric.cyan .sw-metric-ico { background: #ecfeff; color: #0891b2; }
.sw-metric-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.sw-metric-body span {
  font-size: 12px;
  color: var(--sw-muted);
}
.sw-metric-body strong {
  font-size: 22px;
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: -0.03em;
}
.sw-metric-body em {
  font-style: normal;
  font-size: 11px;
  color: #94a3b8;
}
.sw-board {
  flex: 1;
  min-height: 0;
  display: grid;
  overflow: hidden;
  align-items: stretch;
}
.sw-queue,
.sw-list-pane,
.sw-detail {
  background: var(--sw-card);
  border: 1px solid var(--sw-line);
  border-radius: 12px;
  min-width: 0;
  min-height: 0;
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.sw-queue-head {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 14px 14px 8px;
  border: 0;
  background: transparent;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
}
.sw-queue-body {
  flex: 1;
  min-height: 0;
  padding: 0 8px 12px;
  overflow-x: hidden;
  overflow-y: auto;
}
.sw-queue-label {
  margin: 10px 8px 6px;
  font-size: 11px;
  color: var(--sw-muted);
  font-weight: 600;
}
.sw-queue-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  border: 0;
  background: transparent;
  border-radius: 8px;
  padding: 9px 10px;
  font-size: 13px;
  color: #475569;
  cursor: pointer;
  text-align: left;
}
.sw-queue-item:hover { background: #f8fafc; }
.sw-queue-item.on {
  background: var(--sw-primary-soft);
  color: var(--sw-primary);
  font-weight: 700;
}
.sw-queue-item b {
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 999px;
  background: #eef2f7;
  color: #64748b;
  font-size: 11px;
  display: inline-grid;
  place-items: center;
}
.sw-queue-item.on b {
  background: #dbeafe;
  color: #2563eb;
}
.sw-queue-mini {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  border: 0;
  background: #fff7ed;
  border-radius: 8px;
  padding: 8px 10px;
  margin: 0 0 6px;
  cursor: pointer;
  text-align: left;
}
.sw-queue-mini strong {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-queue-mini em {
  font-style: normal;
  font-size: 11px;
  color: #dc2626;
  font-weight: 700;
}
.sw-list-toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--sw-line);
}
.sw-filters { display: flex; gap: 6px; flex-wrap: wrap; }
.sw-chip {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 10px;
  border-radius: 999px;
  background: #f1f5f9;
  color: #475569;
  font-size: 12px;
  font-weight: 600;
}
.sw-chip.muted { color: var(--sw-muted); font-weight: 500; }
.sw-list-total { font-size: 12px; color: var(--sw-muted); }
.sw-cards {
  list-style: none;
  margin: 0;
  padding: 8px;
  overflow-x: hidden;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.sw-card {
  display: flex;
  gap: 10px;
  padding: 12px;
  border-radius: 10px;
  border: 1px solid transparent;
  cursor: pointer;
}
.sw-card:hover { background: #f8fafc; }
.sw-card.on {
  background: var(--sw-primary-soft);
  border-color: #dbeafe;
}
.sw-card.overdue { box-shadow: inset 3px 0 0 #ef4444; }
.sw-card-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 13px;
  font-weight: 800;
  color: #fff;
  background: #60a5fa;
  flex-shrink: 0;
}
.sw-card-avatar.lg { width: 42px; height: 42px; font-size: 15px; }
.sw-card-avatar[data-tone='danger'] { background: #f87171; }
.sw-card-avatar[data-tone='warn'] { background: #fb923c; }
.sw-card-avatar[data-tone='info'] { background: #60a5fa; }
.sw-card-avatar[data-tone='muted'] { background: #94a3b8; }
.sw-card-main { min-width: 0; flex: 1; }
.sw-card-top {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.sw-card-top strong {
  font-size: 13px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-card-top time {
  margin-left: auto;
  font-size: 11px;
  color: var(--sw-muted);
  white-space: nowrap;
}
.sw-tag {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 7px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  background: #eef2ff;
  color: #4f46e5;
  white-space: nowrap;
}
.sw-tag[data-tone='danger'] { background: #fef2f2; color: #dc2626; }
.sw-tag[data-tone='warn'] { background: #fff7ed; color: #ea580c; }
.sw-tag[data-tone='info'] { background: #eff6ff; color: #2563eb; }
.sw-tag[data-tone='muted'],
.sw-tag.muted { background: #f1f5f9; color: #64748b; }
.sw-dot {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 999px;
  background: #ef4444;
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  display: inline-grid;
  place-items: center;
}
.sw-card-preview,
.sw-typing-hint {
  margin: 0;
  font-size: 13px;
  color: #64748b;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-typing-hint { color: #d97706; font-weight: 600; }
.sw-card-meta {
  display: flex;
  gap: 8px;
  margin-top: 6px;
  font-size: 11px;
  color: var(--sw-muted);
}
.sw-card-meta em {
  font-style: normal;
  color: #ea580c;
  font-weight: 700;
}
.sw-card-meta em.hot { color: #dc2626; }
.sw-detail-head {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--sw-line);
}
.sw-detail-user {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.sw-detail-user strong {
  display: block;
  font-size: 15px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-detail-tags {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  flex-wrap: wrap;
}
.sw-peer-typing {
  font-style: normal;
  font-size: 12px;
  color: #d97706;
  font-weight: 600;
}
.sw-closed-tag {
  font-size: 12px;
  color: var(--sw-muted);
  font-weight: 600;
}
.sw-btn {
  height: 32px;
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--sw-line);
  background: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}
.sw-history-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgba(15, 23, 42, 0.45);
  display: grid;
  place-items: center;
  padding: 24px;
}
.sw-history {
  width: min(720px, 100%);
  height: min(780px, calc(100vh - 48px));
  background: #fff;
  border-radius: 14px;
  border: 1px solid var(--sw-line);
  box-shadow: 0 20px 50px rgba(15, 23, 42, 0.22);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.sw-history-head {
  flex-shrink: 0;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--sw-line);
  background: #f8fafc;
}
.sw-history-head strong {
  font-size: 16px;
  font-weight: 700;
}
.sw-history-head p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--sw-muted);
}
.sw-history-x {
  border: 0;
  background: transparent;
  font-size: 24px;
  line-height: 1;
  color: #94a3b8;
  cursor: pointer;
  padding: 0 4px;
}
.sw-history-x:hover { color: #1f2937; }
.sw-history-body {
  flex: 1;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 16px 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: #f8fafc;
}
.sw-history-row {
  display: flex;
}
.sw-history-row.staff { justify-content: flex-end; }
.sw-history-row.user,
.sw-history-row.system { justify-content: flex-start; }
.sw-history-bubble {
  max-width: 86%;
  padding: 10px 12px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid var(--sw-line);
}
.sw-history-row.staff .sw-history-bubble {
  background: var(--sw-primary);
  border-color: transparent;
  color: #fff;
}
.sw-history-row.system .sw-history-bubble {
  background: #eef2ff;
  border-color: transparent;
  color: #4338ca;
}
.sw-history-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}
.sw-history-meta b {
  font-size: 12px;
  font-weight: 700;
}
.sw-history-meta time,
.sw-history-id {
  font-size: 11px;
  opacity: 0.7;
}
.sw-history-bubble p {
  margin: 0;
  font-size: 13px;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
}
.sw-btn.ghost:hover { background: #f8fafc; }
.sw-btn.primary {
  border-color: transparent;
  background: var(--sw-primary);
  color: #fff;
}
.sw-btn.primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.sw-detail-tabs {
  flex-shrink: 0;
  display: flex;
  gap: 4px;
  padding: 8px 12px 0;
  border-bottom: 1px solid var(--sw-line);
}
.sw-detail-tabs button {
  border: 0;
  background: transparent;
  padding: 8px 12px;
  font-size: 13px;
  color: var(--sw-muted);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}
.sw-detail-tabs button.on {
  color: var(--sw-primary);
  font-weight: 700;
  border-bottom-color: var(--sw-primary);
}
.sw-detail-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.sw-msgs {
  flex: 1;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: #f8fafc;
}
.sw-msg { display: flex; }
.sw-msg.staff { justify-content: flex-end; }
.sw-msg.user { justify-content: flex-start; }
.sw-bubble {
  max-width: 82%;
  padding: 9px 12px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid var(--sw-line);
}
.sw-msg.staff .sw-bubble {
  background: var(--sw-primary);
  border-color: transparent;
  color: #fff;
}
.sw-bubble p {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}
.sw-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 4px;
}
.sw-meta time { font-size: 10px; opacity: 0.65; }
.sw-read { font-size: 10px; opacity: 0.55; }
.sw-read.on { opacity: 0.95; font-weight: 700; color: #bbf7d0; }
.sw-msg.user .sw-read.on { color: #16a34a; }
.sw-typing-dots {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 10px 12px;
  min-width: 48px;
}
.sw-typing-dots span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #94a3b8;
  animation: sw-bounce 1.2s infinite ease-in-out;
}
.sw-typing-dots span:nth-child(2) { animation-delay: 0.15s; }
.sw-typing-dots span:nth-child(3) { animation-delay: 0.3s; }
@keyframes sw-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-3px); opacity: 1; }
}
.sw-compose {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: #fff;
  min-height: 72px;
  box-sizing: border-box;
  overflow: hidden;
}
.sw-compose textarea {
  flex: 1;
  min-height: 0;
  resize: none;
  border: 1px solid var(--sw-line);
  border-radius: 10px;
  padding: 10px 12px;
  font: inherit;
  font-size: 13px;
  outline: none;
}
.sw-compose textarea:focus { border-color: #93c5fd; box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.12); }
.sw-compose-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-shrink: 0;
}
.sw-compose-bar span { font-size: 11px; color: var(--sw-muted); }
.sw-log {
  flex-shrink: 0;
  padding: 12px 14px 14px;
  background: #fff;
  min-height: 72px;
  box-sizing: border-box;
  overflow-x: hidden;
  overflow-y: auto;
}
.sw-log h4 {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--sw-muted);
  font-weight: 700;
}
.sw-log ol {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.sw-log li {
  display: flex;
  gap: 10px;
}
.sw-log i {
  width: 8px;
  height: 8px;
  margin-top: 5px;
  border-radius: 50%;
  background: #93c5fd;
  flex-shrink: 0;
}
.sw-log p {
  margin: 0;
  font-size: 12px;
  color: #475569;
  line-height: 1.4;
}
.sw-log time {
  font-size: 11px;
  color: var(--sw-muted);
}
.sw-customer {
  flex: 1;
  min-height: 0;
  padding: 16px;
  overflow-x: hidden;
  overflow-y: auto;
}
.sw-customer dl {
  margin: 0;
  display: grid;
  gap: 10px;
}
.sw-customer dl > div {
  display: grid;
  grid-template-columns: 88px 1fr;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 8px;
  background: #f8fafc;
}
.sw-customer dt {
  font-size: 12px;
  color: var(--sw-muted);
}
.sw-customer dd {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  word-break: break-all;
}
.sw-empty {
  padding: 28px 16px;
  text-align: center;
  font-size: 13px;
  color: var(--sw-muted);
}
.sw-empty.tall {
  flex: 1;
  display: grid;
  place-items: center;
}
@media (max-width: 1200px) {
  .sw-metric-row { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 960px) {
  .sw-top { grid-template-columns: 1fr; }
  .sw-metric-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>

<style>
/* 本页占满内容区一屏，避免外层整页滚动 */
.console-body-slot:has(#market-support-page) {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.console-body-slot:has(#market-support-page) > * {
  flex: 1;
  min-height: 0;
}
</style>
