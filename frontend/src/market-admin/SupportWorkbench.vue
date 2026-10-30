<script setup lang="ts">
/**
 * 官方客服工作台：长轮询同步消息/已读/输入中，列表随未读变化刷新。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminSupport' })

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
let threadRunning = false
let threadAbort: AbortController | null = null
let alertsRunning = false
let alertsAbort: AbortController | null = null

const STATUS_TABS = [
  { value: 'pending', label: '待回复' },
  { value: 'overdue', label: '已超时' },
  { value: 'open', label: '进行中' },
  { value: 'closed', label: '已关闭' },
  { value: '', label: '全部' },
]

const selected = computed(() => list.value.find((c) => c.id === selectedId.value) || null)
const lastMsgId = computed(() => messages.value.reduce((m, x) => Math.max(m, x.id), 0))
const lastOwnId = computed(() => {
  let id = 0
  for (const m of messages.value) {
    if (m.sender === 'staff') id = m.id
  }
  return id
})

function fmtTime(v?: string) {
  if (!v) return ''
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
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
    list.value = res.items ?? []
    total.value = Number(res.total ?? list.value.length)
    if (keepSelection && prevId && list.value.some((c) => c.id === prevId)) {
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
  threadRunning = false
  threadAbort?.abort()
  threadAbort = null
}

async function threadLoop() {
  if (threadRunning) return
  threadRunning = true
  while (threadRunning && selectedId.value) {
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
      if (!threadRunning || selectedId.value !== id) break
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
      if (!threadRunning) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
  threadRunning = false
}

function stopAlertsLoop() {
  alertsRunning = false
  alertsAbort?.abort()
  alertsAbort = null
}

async function alertsLoop() {
  if (alertsRunning) return
  alertsRunning = true
  while (alertsRunning) {
    alertsAbort?.abort()
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
      if (!alertsRunning) break
      const changed =
        !alerts.value ||
        a.pending_count !== alerts.value.pending_count ||
        a.overdue_count !== alerts.value.overdue_count ||
        a.staff_unread_total !== alerts.value.staff_unread_total
      alerts.value = a
      if (changed) void loadList(true, true)
    } catch {
      if (!alertsRunning) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
  alertsRunning = false
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
  await Promise.all([loadList(false), loadAlerts()])
  void alertsLoop()
})

onUnmounted(() => {
  stopThreadLoop()
  stopAlertsLoop()
  void clearTyping()
})
</script>

<template>
  <section id="market-support-page" class="sw-shell">
    <header class="y-header">
      <div class="page-heading">
        <h2 class="y-title">官方客服</h2>
        <p class="y-page-desc">用户留言工作台 · {{ alerts?.reply_timeout_minutes ?? 15 }} 分钟未回预警 · 10 分钟无消息自动结束 · 实时同步</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loadingList" @click="loadList(); loadAlerts()">
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

    <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>

    <div class="sw-grid">
      <aside class="sw-pane sw-list">
        <div class="sw-tabs">
          <button
            v-for="t in STATUS_TABS"
            :key="t.value || 'all'"
            type="button"
            :class="{ on: status === t.value }"
            @click="status = t.value"
          >
            {{ t.label }}
          </button>
        </div>
        <div class="sw-search">
          <input v-model="q" type="search" placeholder="邮箱 / 预览" @keydown.enter="loadList(false)" />
          <button type="button" class="btn btn-secondary btn-sm" @click="loadList(false)">搜</button>
        </div>
        <ul class="sw-convs">
          <li v-if="loadingList && !list.length" class="sw-empty">加载中…</li>
          <li v-else-if="!list.length" class="sw-empty">暂无会话</li>
          <li
            v-for="c in list"
            :key="c.id"
            :class="{ on: c.id === selectedId, overdue: c.overdue, typing: c.peer_typing || c.user_typing }"
            @click="openConversation(c.id)"
          >
            <div class="sw-conv-top">
              <strong>{{ c.user_email || `#${c.user_id}` }}</strong>
              <span v-if="c.staff_unread_count" class="sw-dot">{{ c.staff_unread_count }}</span>
              <time>{{ fmtTime(c.last_message_at) }}</time>
            </div>
            <p v-if="c.peer_typing || c.user_typing" class="sw-typing-hint">正在输入…</p>
            <p v-else>{{ c.last_preview || '（无内容）' }}</p>
            <div class="sw-conv-meta">
              <em v-if="c.awaiting_staff_since" :class="{ hot: c.overdue }">{{ waitLabel(c) }}</em>
              <span>{{ c.status === 'closed' ? '已关闭' : '进行中' }}</span>
            </div>
          </li>
        </ul>
        <footer class="sw-list-foot">共 {{ total }} 条</footer>
      </aside>

      <main class="sw-pane sw-thread">
        <template v-if="selected">
          <header class="sw-thread-head">
            <div>
              <strong>{{ selected.user_email || `用户 #${selected.user_id}` }}</strong>
              <span>#{{ selected.id }}</span>
              <em v-if="peerTyping" class="sw-peer-typing">对方正在输入…</em>
            </div>
            <button
              v-if="selected.status !== 'closed'"
              type="button"
              class="btn btn-secondary btn-sm"
              @click="closeConversation"
            >
              结束会话
            </button>
            <span v-else class="sw-closed-tag">已结束</span>
          </header>
          <div ref="listEl" class="sw-msgs">
            <p v-if="loadingThread" class="sw-empty">加载中…</p>
            <div
              v-for="m in messages"
              :key="m.id"
              class="sw-msg"
              :class="{
                staff: m.sender === 'staff',
                user: m.sender === 'user',
                sys: m.sender === 'system',
              }"
            >
              <div class="sw-bubble">
                <p>{{ m.body }}</p>
                <div class="sw-meta">
                  <time>{{ fmtTime(m.created_at) }} · {{ m.sender === 'user' ? '用户' : m.sender === 'staff' ? '官方' : '系统' }}</time>
                  <span v-if="receiptOf(m)" class="sw-read" :class="{ on: receiptOf(m) === '已读' }">{{ receiptOf(m) }}</span>
                </div>
              </div>
            </div>
            <div v-if="peerTyping" class="sw-msg user">
              <div class="sw-bubble sw-typing-dots"><span /><span /><span /></div>
            </div>
          </div>
          <footer class="sw-compose">
            <textarea
              v-model="draft"
              rows="3"
              maxlength="2000"
              placeholder="回复用户，Enter 发送"
              :disabled="selected.status === 'closed'"
              @input="onDraftInput"
              @keydown="onKey"
            />
            <button
              type="button"
              class="btn btn-primary"
              :disabled="sending || !draft.trim() || selected.status === 'closed'"
              @click="send"
            >
              {{ sending ? '…' : '发送' }}
            </button>
          </footer>
        </template>
        <div v-else class="sw-empty tall">选择左侧会话开始回复</div>
      </main>

      <aside class="sw-pane sw-alerts">
        <h3>预警</h3>
        <dl class="sw-stats">
          <div>
            <dt>待回复</dt>
            <dd :class="{ hot: alerts?.pending_alert_fired }">{{ alerts?.pending_count ?? '—' }}</dd>
          </div>
          <div>
            <dt>已超时</dt>
            <dd :class="{ hot: (alerts?.overdue_count ?? 0) > 0 }">{{ alerts?.overdue_count ?? '—' }}</dd>
          </div>
          <div>
            <dt>阈值</dt>
            <dd>{{ alerts?.pending_alert_threshold ?? 5 }} / {{ alerts?.reply_timeout_minutes ?? 15 }}分</dd>
          </div>
        </dl>
        <h4>超时会话</h4>
        <ul class="sw-overdue">
          <li v-if="!(alerts?.overdue_conversations?.length)" class="sw-empty">暂无</li>
          <li
            v-for="c in alerts?.overdue_conversations || []"
            :key="c.id"
            @click="openConversation(c.id)"
          >
            <strong>{{ c.user_email || `#${c.user_id}` }}</strong>
            <span>{{ waitLabel(c) }}</span>
            <p>{{ c.last_preview }}</p>
          </li>
        </ul>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.sw-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: calc(100vh - 140px);
}
.sw-banner {
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
.sw-grid {
  flex: 1;
  display: grid;
  grid-template-columns: minmax(240px, 280px) 1fr minmax(200px, 240px);
  gap: 12px;
  min-height: 520px;
}
.sw-pane {
  background: var(--color-surface, #fff);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}
.sw-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 10px 10px 6px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.06);
}
.sw-tabs button {
  border: none;
  background: transparent;
  font-size: 12px;
  padding: 4px 8px;
  border-radius: 6px;
  color: #64748b;
  cursor: pointer;
}
.sw-tabs button.on {
  background: #0f172a;
  color: #fff;
  font-weight: 700;
}
.sw-search {
  display: flex;
  gap: 6px;
  padding: 8px 10px;
}
.sw-search input {
  flex: 1;
  border: 1px solid rgba(15, 23, 42, 0.12);
  border-radius: 8px;
  padding: 6px 8px;
  font-size: 12px;
}
.sw-convs {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  flex: 1;
}
.sw-convs li {
  padding: 10px 12px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  cursor: pointer;
}
.sw-convs li:hover { background: #f8fafc; }
.sw-convs li.on { background: #eff6ff; }
.sw-convs li.overdue { box-shadow: inset 3px 0 0 #ef4444; }
.sw-convs li.typing { background: #fffbeb; }
.sw-conv-top {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}
.sw-conv-top strong { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sw-conv-top time { font-size: 11px; color: #94a3b8; }
.sw-dot {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 999px;
  background: #ef4444;
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  display: grid;
  place-items: center;
}
.sw-convs p {
  margin: 4px 0 0;
  font-size: 12px;
  color: #64748b;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-typing-hint { color: #d97706 !important; font-weight: 600; }
.sw-conv-meta {
  display: flex;
  justify-content: space-between;
  margin-top: 4px;
  font-size: 11px;
  color: #94a3b8;
}
.sw-conv-meta em { font-style: normal; color: #ea580c; font-weight: 700; }
.sw-conv-meta em.hot { color: #dc2626; }
.sw-list-foot {
  padding: 8px 12px;
  font-size: 11px;
  color: #94a3b8;
  border-top: 1px solid rgba(15, 23, 42, 0.06);
}
.sw-thread-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.06);
}
.sw-thread-head strong { font-size: 14px; margin-right: 8px; }
.sw-thread-head span { font-size: 12px; color: #94a3b8; }
.sw-peer-typing {
  margin-left: 8px;
  font-style: normal;
  font-size: 12px;
  color: #d97706;
  font-weight: 600;
}
.sw-closed-tag {
  font-size: 12px;
  color: #94a3b8;
  font-weight: 600;
}
.sw-msgs {
  flex: 1;
  overflow-y: auto;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: #f8fafc;
}
.sw-msg { display: flex; }
.sw-msg.staff { justify-content: flex-end; }
.sw-msg.user, .sw-msg.sys { justify-content: flex-start; }
.sw-bubble {
  max-width: 78%;
  padding: 8px 11px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.06);
}
.sw-msg.staff .sw-bubble {
  background: #0f172a;
  border-color: transparent;
  color: #fff;
}
.sw-msg.sys .sw-bubble {
  background: #eef2ff;
  border-color: transparent;
  color: #4338ca;
}
.sw-bubble p {
  margin: 0;
  font-size: 13px;
  line-height: 1.45;
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
.sw-meta time {
  font-size: 10px;
  opacity: 0.65;
}
.sw-read {
  font-size: 10px;
  opacity: 0.55;
}
.sw-read.on { opacity: 0.95; font-weight: 700; color: #4ade80; }
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
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid rgba(15, 23, 42, 0.06);
}
.sw-compose textarea {
  resize: none;
  border: 1px solid rgba(15, 23, 42, 0.12);
  border-radius: 10px;
  padding: 8px 10px;
  font: inherit;
  font-size: 13px;
}
.sw-alerts { padding: 14px; }
.sw-alerts h3 { margin: 0 0 10px; font-size: 14px; }
.sw-alerts h4 { margin: 16px 0 8px; font-size: 12px; color: #64748b; font-weight: 700; }
.sw-stats {
  display: grid;
  gap: 8px;
  margin: 0;
}
.sw-stats > div {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: 8px 10px;
  border-radius: 8px;
  background: #f8fafc;
}
.sw-stats dt { font-size: 12px; color: #64748b; }
.sw-stats dd { margin: 0; font-size: 18px; font-weight: 800; }
.sw-stats dd.hot { color: #dc2626; }
.sw-overdue {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  flex: 1;
}
.sw-overdue li {
  padding: 8px 0;
  border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  cursor: pointer;
}
.sw-overdue strong { font-size: 12px; }
.sw-overdue span { float: right; font-size: 11px; color: #dc2626; font-weight: 700; }
.sw-overdue p {
  margin: 4px 0 0;
  font-size: 11px;
  color: #64748b;
  clear: both;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-empty {
  padding: 20px;
  text-align: center;
  font-size: 13px;
  color: #94a3b8;
}
.sw-empty.tall {
  flex: 1;
  display: grid;
  place-items: center;
}
@media (max-width: 1100px) {
  .sw-grid { grid-template-columns: 1fr; }
  .sw-alerts { display: none; }
}
</style>
