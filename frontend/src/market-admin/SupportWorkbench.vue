<script setup lang="ts">
/**
 * 官方客服工作台：长轮询同步消息/已读/输入中，列表随未读变化刷新。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
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
const activeConversation = ref<Conversation | null>(null)
const messages = ref<Message[]>([])
const draft = ref('')
const drafts = new Map<number, string>()
const alerts = ref<Alerts | null>(null)
const loadingList = ref(false)
const loadingThread = ref(false)
const sending = ref(false)
const closing = ref(false)
const closeConfirmationOpen = ref(false)
const error = ref('')
const threadError = ref('')
const peerTyping = ref(false)
const peerReadId = ref(0)
const hasNewMessages = ref(false)
const newMessageAnnouncement = ref('')
const listEl = ref<HTMLElement | null>(null)
let typingTimer: ReturnType<typeof setTimeout> | null = null
let typingSent = false
let typingConversationId = 0
let openRequestId = 0
let openAbort: AbortController | null = null
let threadRunning = false
let threadLoopId = 0
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
  if (selectedId.value === c.id) {
    activeConversation.value = { ...activeConversation.value, ...c }
    peerTyping.value = !!c.peer_typing
    peerReadId.value = Number(c.user_last_read_msg_id ?? 0)
  }
  const idx = list.value.findIndex((x) => x.id === c.id)
  if (idx >= 0) list.value[idx] = { ...list.value[idx], ...c }
}

function isNearBottom() {
  const el = listEl.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 80
}

async function scrollBottom(conversationId = selectedId.value) {
  await nextTick()
  if (selectedId.value !== conversationId) return
  const el = listEl.value
  if (el) el.scrollTop = el.scrollHeight
  hasNewMessages.value = false
}

async function appendMessages(rows: Message[], announce = false) {
  const conversationId = selectedId.value
  const known = new Set(messages.value.map((m) => m.id))
  const add = rows.filter((m) => m.conversation_id === conversationId && !known.has(m.id))
  if (!add.length) return 0
  const shouldScroll = isNearBottom()
  messages.value = [...messages.value, ...add]
  if (announce) {
    newMessageAnnouncement.value = `收到 ${add.length} 条新消息，当前共 ${messages.value.length} 条`
  }
  if (shouldScroll) await scrollBottom(conversationId)
  else hasNewMessages.value = true
  return add.length
}

function onMessagesScroll() {
  if (isNearBottom()) hasNewMessages.value = false
}

async function jumpToLatest() {
  await scrollBottom()
  listEl.value?.focus({ preventScroll: true })
}

async function loadAlerts() {
  try {
    alerts.value = await marketGet<Alerts>(`${MARKET_API_PREFIX}/support/alerts`)
  } catch {
    /* ignore */
  }
}

function clearActiveConversation() {
  if (selectedId.value) drafts.set(selectedId.value, draft.value)
  openRequestId += 1
  openAbort?.abort()
  openAbort = null
  stopThreadLoop()
  selectedId.value = 0
  activeConversation.value = null
  messages.value = []
  draft.value = ''
  loadingThread.value = false
  threadError.value = ''
  peerTyping.value = false
  peerReadId.value = 0
  hasNewMessages.value = false
  newMessageAnnouncement.value = ''
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
    if (keepSelection && prevId) {
      selectedId.value = prevId
    } else if (list.value.length) {
      await openConversation(list.value[0].id)
    } else {
      clearActiveConversation()
    }
  } catch (e) {
    if (!silent) error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loadingList.value = false
  }
}

function isCurrentOpen(requestId: number, conversationId: number, ac: AbortController) {
  return requestId === openRequestId && selectedId.value === conversationId && !ac.signal.aborted
}

async function openConversation(id: number) {
  if (!id) return
  const requestId = ++openRequestId
  openAbort?.abort()
  const ac = new AbortController()
  openAbort = ac
  stopThreadLoop()
  const previousId = selectedId.value
  if (previousId && previousId !== id) {
    drafts.set(previousId, draft.value)
    void clearTyping(previousId)
  }
  selectedId.value = id
  if (previousId !== id) draft.value = drafts.get(id) ?? ''
  activeConversation.value = null
  messages.value = []
  loadingThread.value = true
  threadError.value = ''
  peerTyping.value = false
  peerReadId.value = 0
  hasNewMessages.value = false
  newMessageAnnouncement.value = ''
  error.value = ''
  try {
    const bundle = await marketGet<{ conversation: Conversation; messages: Message[] }>(
      `${MARKET_API_PREFIX}/support/conversations/${id}`,
      { signal: ac.signal },
    )
    if (!isCurrentOpen(requestId, id, ac)) return
    if (bundle.conversation.id !== id) throw new Error('会话响应不匹配，请重试')
    activeConversation.value = bundle.conversation
    messages.value = (bundle.messages ?? []).filter((m) => m.conversation_id === id)
    applyConv(bundle.conversation)
    await scrollBottom(id)
    await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
    if (!isCurrentOpen(requestId, id, ac)) return
    window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
    const idx = list.value.findIndex((c) => c.id === id)
    if (idx >= 0) list.value[idx].staff_unread_count = 0
    void threadLoop()
  } catch (e) {
    if (!isCurrentOpen(requestId, id, ac)) return
    threadError.value = e instanceof Error ? e.message : '加载会话失败'
  } finally {
    if (requestId === openRequestId) {
      loadingThread.value = false
      if (openAbort === ac) openAbort = null
    }
  }
}

function stopThreadLoop() {
  threadRunning = false
  threadLoopId += 1
  threadAbort?.abort()
  threadAbort = null
}

async function threadLoop() {
  if (threadRunning) return
  threadRunning = true
  const loopId = ++threadLoopId
  while (threadRunning && loopId === threadLoopId && selectedId.value) {
    const id = selectedId.value
    const ac = new AbortController()
    threadAbort = ac
    try {
      const q = new URLSearchParams({
        after_id: String(lastMsgId.value),
        peer_read: String(peerReadId.value),
        peer_typing: peerTyping.value ? '1' : '0',
        unread: String(
          list.value.find((c) => c.id === id)?.staff_unread_count ?? activeConversation.value?.staff_unread_count ?? 0,
        ),
        status: activeConversation.value?.status || 'open',
        wait_ms: String(WAIT_MS),
      })
      const bundle = await marketGet<{ conversation: Conversation; messages: Message[] }>(
        `${MARKET_API_PREFIX}/support/conversations/${id}?${q}`,
        { signal: ac.signal, timeoutMs: WAIT_MS + 8000 },
      )
      if (!threadRunning || loopId !== threadLoopId || selectedId.value !== id) break
      if (bundle.conversation.id !== id) continue
      applyConv(bundle.conversation)
      const added = await appendMessages(bundle.messages ?? [], true)
      if (added) {
        // 仅前台可见时标已读，避免后台页签把顶栏未读角标瞬间清掉
        if (document.visibilityState === 'visible') {
          await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
          window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
        }
        void loadList(true, true)
      } else if (bundle.conversation.staff_unread_count !== undefined) {
        void loadList(true, true)
      }
    } catch {
      if (!threadRunning || loopId !== threadLoopId || selectedId.value !== id || ac.signal.aborted) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
  if (loopId === threadLoopId) threadRunning = false
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
  const conversationId = selectedId.value
  const draftSnapshot = draft.value
  const body = draftSnapshot.trim()
  if (!body || !conversationId || sending.value) return
  sending.value = true
  error.value = ''
  drafts.set(conversationId, draftSnapshot)
  try {
    await clearTyping(conversationId)
    const msg = await marketSend<Message>(
      'POST',
      `${MARKET_API_PREFIX}/support/conversations/${conversationId}/messages`,
      { body },
    )
    if (selectedId.value === conversationId && activeConversation.value?.id === conversationId) {
      if (draft.value === draftSnapshot) draft.value = ''
      activeConversation.value = {
        ...activeConversation.value,
        last_preview: body.slice(0, 80),
        last_sender: 'staff',
        awaiting_staff_since: null,
        overdue: false,
        staff_unread_count: 0,
        peer_typing: false,
      }
      peerTyping.value = false
      await appendMessages([msg])
    }
    if (drafts.get(conversationId) === draftSnapshot) drafts.set(conversationId, '')
    const idx = list.value.findIndex((c) => c.id === conversationId)
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
    void loadAlerts()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '发送失败'
  } finally {
    sending.value = false
  }
}

function closeConversation() {
  if (!selectedId.value || closing.value) return
  error.value = ''
  closeConfirmationOpen.value = true
}

async function confirmCloseConversation() {
  const conversationId = selectedId.value
  if (!conversationId || closing.value) return
  closing.value = true
  error.value = ''
  try {
    await clearTyping(conversationId)
    await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${conversationId}/close`)
    closeConfirmationOpen.value = false
    await loadList(false)
    void loadAlerts()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '关闭失败'
  } finally {
    closing.value = false
  }
}

async function clearTyping(conversationId = selectedId.value) {
  if (typingTimer) {
    clearTimeout(typingTimer)
    typingTimer = null
  }
  if (!typingSent || typingConversationId !== conversationId || !conversationId) return
  typingSent = false
  typingConversationId = 0
  await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${conversationId}/typing`, {
    typing: false,
  }).catch(() => null)
}

function onDraftInput() {
  const conversationId = selectedId.value
  if (!conversationId || activeConversation.value?.status === 'closed') return
  if (!typingSent) {
    typingSent = true
    typingConversationId = conversationId
    void marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${conversationId}/typing`, {
      typing: true,
    }).catch(() => null)
  }
  if (typingTimer) clearTimeout(typingTimer)
  typingTimer = setTimeout(() => {
    if (!typingSent || typingConversationId !== conversationId) return
    typingSent = false
    typingConversationId = 0
    void marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${conversationId}/typing`, {
      typing: false,
    }).catch(() => null)
  }, 2500)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
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
  openRequestId += 1
  openAbort?.abort()
  openAbort = null
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

    <p v-if="error" class="y-msg y-msg--error" role="alert">{{ error }}</p>

    <div class="sw-grid">
      <aside class="sw-pane sw-list">
        <div class="sw-tabs">
          <button
            v-for="t in STATUS_TABS"
            :key="t.value || 'all'"
            type="button"
            :class="{ on: status === t.value }"
            :aria-pressed="status === t.value"
            @click="status = t.value"
          >
            {{ t.label }}
          </button>
        </div>
        <div class="sw-search">
          <input
            v-model="q"
            type="search"
            aria-label="搜索会话"
            placeholder="邮箱 / 预览"
            @keydown.enter="loadList(false)"
          />
          <button type="button" class="btn btn-secondary btn-sm" @click="loadList(false)">搜</button>
        </div>
        <ul class="sw-convs">
          <li v-if="loadingList && !list.length" class="sw-empty" role="status">加载中…</li>
          <li v-else-if="!list.length" class="sw-empty">暂无会话</li>
          <li
            v-for="c in list"
            :key="c.id"
            :class="{ on: c.id === selectedId, overdue: c.overdue, typing: c.peer_typing || c.user_typing }"
          >
            <button
              type="button"
              class="sw-conv-button"
              :aria-current="c.id === selectedId ? 'true' : undefined"
              @click="openConversation(c.id)"
            >
              <div class="sw-conv-top">
                <strong :title="c.user_email || `#${c.user_id}`">{{ c.user_email || `#${c.user_id}` }}</strong>
                <span v-if="c.staff_unread_count" class="sw-dot">{{ c.staff_unread_count }}</span>
                <time>{{ fmtTime(c.last_message_at) }}</time>
              </div>
              <p v-if="c.peer_typing || c.user_typing" class="sw-typing-hint">正在输入…</p>
              <p v-else>{{ c.last_preview || '（无内容）' }}</p>
              <div class="sw-conv-meta">
                <em v-if="c.awaiting_staff_since" :class="{ hot: c.overdue }">{{ waitLabel(c) }}</em>
                <span>{{ c.status === 'closed' ? '已关闭' : '进行中' }}</span>
              </div>
            </button>
          </li>
        </ul>
        <footer class="sw-list-foot">共 {{ total }} 条</footer>
      </aside>

      <main class="sw-pane sw-thread">
        <template v-if="selectedId">
          <header class="sw-thread-head">
            <div class="sw-thread-title">
              <strong :title="activeConversation?.user_email || `会话 #${selectedId}`">
                {{ activeConversation?.user_email || `会话 #${selectedId}` }}
              </strong>
              <span>#{{ selectedId }}</span>
              <em v-if="peerTyping" class="sw-peer-typing">对方正在输入…</em>
            </div>
            <button
              v-if="activeConversation && activeConversation.status !== 'closed'"
              type="button"
              class="btn btn-secondary btn-sm"
              @click="closeConversation"
            >
              结束会话
            </button>
            <span v-else-if="activeConversation" class="sw-closed-tag">已结束</span>
          </header>
          <div
            ref="listEl"
            class="sw-msgs"
            tabindex="0"
            aria-label="会话消息"
            :aria-busy="loadingThread"
            @scroll="onMessagesScroll"
          >
            <p v-if="loadingThread" class="sw-empty" role="status">加载中…</p>
            <div v-else-if="threadError" class="sw-thread-error" role="alert">
              <p>{{ threadError }}</p>
              <button type="button" class="btn btn-secondary btn-sm" @click="openConversation(selectedId)">重试</button>
            </div>
            <p v-else-if="!messages.length" class="sw-empty">暂无消息</p>
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
            <div v-if="peerTyping && !loadingThread && !threadError" class="sw-msg user">
              <div class="sw-bubble sw-typing-dots"><span /><span /><span /></div>
            </div>
          </div>
          <div v-if="hasNewMessages" class="sw-new-messages">
            <button type="button" class="btn btn-secondary btn-sm" @click="jumpToLatest">新消息，跳到最新</button>
          </div>
          <p class="sw-sr-only" role="status" aria-live="polite" aria-atomic="true">{{ newMessageAnnouncement }}</p>
          <footer class="sw-compose">
            <div class="sw-compose-input">
              <textarea
                v-model="draft"
                rows="3"
                maxlength="2000"
                aria-label="回复用户"
                aria-describedby="sw-draft-meta"
                placeholder="回复用户，Enter 发送"
                :disabled="!activeConversation || loadingThread || !!threadError || activeConversation.status === 'closed'"
                @input="onDraftInput"
                @keydown="onKey"
              />
              <div id="sw-draft-meta" class="sw-draft-meta">草稿按会话保留 · {{ draft.length }} / 2000</div>
            </div>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="sending || !draft.trim() || !activeConversation || loadingThread || !!threadError || activeConversation.status === 'closed'"
              @click="send"
            >
              {{ sending ? '发送中…' : '发送' }}
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
          <li v-for="c in alerts?.overdue_conversations || []" :key="c.id">
            <button
              type="button"
              class="sw-overdue-button"
              :aria-current="c.id === selectedId ? 'true' : undefined"
              @click="openConversation(c.id)"
            >
              <strong :title="c.user_email || `#${c.user_id}`">{{ c.user_email || `#${c.user_id}` }}</strong>
              <span>{{ waitLabel(c) }}</span>
              <p>{{ c.last_preview }}</p>
            </button>
          </li>
        </ul>
      </aside>
    </div>

    <ConfirmDialog
      :show="closeConfirmationOpen"
      title="结束会话"
      :message="`确认结束与 ${activeConversation?.user_email || `会话 #${selectedId}`} 的会话？用户仍可再发消息重新打开。`"
      confirm-text="确认结束"
      cancel-text="取消"
      danger
      :busy="closing"
      @confirm="confirmCloseConversation"
      @cancel="closeConfirmationOpen = false"
    >
      <p v-if="closeConfirmationOpen && error" class="y-msg y-msg--error" role="alert">{{ error }}</p>
    </ConfirmDialog>
  </section>
</template>

<style scoped>
.sw-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  color: var(--color-text-primary, #0f172a);
}
.sw-shell > .y-header,
.sw-shell > .sw-banner,
.sw-shell > .y-msg {
  flex-shrink: 0;
}
.sw-shell > .y-header .page-heading {
  flex: 1;
  min-width: 0;
}
.sw-shell > .y-header .y-header-actions,
.sw-shell > .y-header .page-action-btn {
  flex-shrink: 0;
}
.sw-shell > .y-header .page-action-btn {
  white-space: nowrap;
}
.sw-banner {
  padding: 10px 14px;
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.08));
  border-radius: 10px;
  background: var(--color-bg-secondary, #fff7ed);
  color: var(--color-warning, #9a3412);
  font-size: 13px;
  font-weight: 600;
}
.sw-banner.hot { color: var(--color-error, #b91c1c); }
.sw-grid {
  flex: 1;
  display: grid;
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr) minmax(200px, 240px);
  gap: 12px;
  min-height: 0;
}
.sw-pane {
  background: var(--color-bg-elevated, var(--color-surface, #fff));
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.08));
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 0;
  min-height: 0;
}
.sw-tabs {
  display: flex;
  flex-wrap: wrap;
  flex-shrink: 0;
  gap: 4px;
  padding: 10px 10px 6px;
  border-bottom: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
}
.sw-tabs button {
  border: none;
  background: transparent;
  font-size: 12px;
  padding: 4px 8px;
  border-radius: 6px;
  color: var(--color-text-secondary, #64748b);
  cursor: pointer;
}
.sw-tabs button.on {
  background: var(--color-primary, #0555ce);
  color: #fff;
  font-weight: 700;
}
.sw-search {
  display: flex;
  flex-shrink: 0;
  gap: 6px;
  padding: 8px 10px;
}
.sw-search input {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.12));
  border-radius: 8px;
  background: var(--color-bg-elevated, #fff);
  color: var(--color-text-primary, #0f172a);
  padding: 6px 8px;
  font-size: 12px;
}
.sw-convs {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}
.sw-convs li { border-bottom: 1px solid var(--color-border, rgba(15, 23, 42, 0.05)); }
.sw-convs li:hover { background: var(--color-bg-secondary, #f8fafc); }
.sw-convs li.on {
  background: var(--color-bg-secondary, #eff6ff);
  box-shadow: inset 3px 0 0 var(--color-primary, #0555ce);
}
.sw-convs li.overdue { box-shadow: inset 3px 0 0 var(--color-error, #dc2626); }
.sw-convs li.typing { background: var(--color-bg-secondary, #fffbeb); }
.sw-conv-button,
.sw-overdue-button {
  display: block;
  width: 100%;
  margin: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.sw-conv-button { padding: 10px 12px; }
.sw-conv-button:focus-visible,
.sw-overdue-button:focus-visible {
  outline: 2px solid var(--color-primary, #0555ce);
  outline-offset: -2px;
}
.sw-conv-top {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}
.sw-conv-top strong {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-conv-top time {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--color-text-secondary, #64748b);
}
.sw-dot {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 999px;
  background: var(--color-error, #dc2626);
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  display: grid;
  place-items: center;
}
.sw-convs p {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-typing-hint { color: var(--color-warning, #b45309) !important; font-weight: 600; }
.sw-conv-meta {
  display: flex;
  justify-content: space-between;
  margin-top: 4px;
  font-size: 11px;
  color: var(--color-text-secondary, #64748b);
}
.sw-conv-meta em { font-style: normal; color: var(--color-warning, #c2410c); font-weight: 700; }
.sw-conv-meta em.hot { color: var(--color-error, #dc2626); }
.sw-list-foot {
  flex-shrink: 0;
  padding: 8px 12px;
  font-size: 11px;
  color: var(--color-text-secondary, #64748b);
  border-top: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
}
.sw-thread-head {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
}
.sw-thread-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 8px;
  min-width: 0;
}
.sw-thread-title strong {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}
.sw-thread-title span {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
}
.sw-thread-head > .btn { flex-shrink: 0; }
.sw-peer-typing {
  font-style: normal;
  font-size: 12px;
  color: var(--color-warning, #b45309);
  font-weight: 600;
}
.sw-closed-tag {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
  font-weight: 600;
}
.sw-msgs {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--color-bg-secondary, #f8fafc);
}
.sw-msgs:focus-visible {
  outline: 2px solid var(--color-primary, #0555ce);
  outline-offset: -2px;
}
.sw-msg { display: flex; }
.sw-msg.staff { justify-content: flex-end; }
.sw-msg.user, .sw-msg.sys { justify-content: flex-start; }
.sw-bubble {
  max-width: 78%;
  padding: 8px 11px;
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  color: var(--color-text-primary, #0f172a);
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
  overflow-wrap: anywhere;
}
.sw-msg.staff .sw-bubble {
  background: var(--color-primary, #0555ce);
  border-color: transparent;
  color: #fff;
}
.sw-msg.sys .sw-bubble {
  background: var(--color-bg-secondary, #eef2ff);
  color: var(--color-primary, #4338ca);
}
.sw-bubble p {
  margin: 0;
  font-size: 13px;
  line-height: 1.45;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.sw-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 4px;
}
.sw-meta time { font-size: 10px; opacity: 0.78; }
.sw-read { font-size: 10px; opacity: 0.78; }
.sw-read.on { opacity: 1; font-weight: 700; }
.sw-msg.user .sw-read.on { color: var(--color-success, #15803d); }
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
  background: var(--color-text-secondary, #64748b);
  animation: sw-bounce 1.2s infinite ease-in-out;
}
.sw-typing-dots span:nth-child(2) { animation-delay: 0.15s; }
.sw-typing-dots span:nth-child(3) { animation-delay: 0.3s; }
@keyframes sw-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-3px); opacity: 1; }
}
.sw-thread-error {
  margin: auto;
  padding: 20px;
  text-align: center;
  color: var(--color-error, #b91c1c);
}
.sw-thread-error p { margin: 0 0 12px; overflow-wrap: anywhere; }
.sw-new-messages {
  display: flex;
  flex-shrink: 0;
  justify-content: center;
  padding: 6px;
  border-top: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
  background: var(--color-bg-elevated, #fff);
}
.sw-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.sw-compose {
  display: grid;
  flex-shrink: 0;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 8px;
  padding: 10px 12px;
  border-top: 1px solid var(--color-border, rgba(15, 23, 42, 0.06));
  background: var(--color-bg-elevated, #fff);
}
.sw-compose-input { min-width: 0; }
.sw-compose textarea {
  display: block;
  width: 100%;
  min-height: 72px;
  max-height: 180px;
  resize: vertical;
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.12));
  border-radius: 10px;
  background: var(--color-bg-elevated, #fff);
  color: var(--color-text-primary, #0f172a);
  padding: 8px 10px;
  font: inherit;
  font-size: 13px;
}
.sw-draft-meta {
  margin-top: 4px;
  color: var(--color-text-secondary, #64748b);
  font-size: 11px;
  text-align: right;
}
.sw-alerts { padding: 14px; }
.sw-alerts h3 { margin: 0 0 10px; font-size: 14px; }
.sw-alerts h4 {
  margin: 16px 0 8px;
  font-size: 12px;
  color: var(--color-text-secondary, #64748b);
  font-weight: 700;
}
.sw-stats {
  display: grid;
  flex-shrink: 0;
  gap: 8px;
  margin: 0;
}
.sw-stats > div {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--color-bg-secondary, #f8fafc);
}
.sw-stats dt { font-size: 12px; color: var(--color-text-secondary, #64748b); }
.sw-stats dd { margin: 0; font-size: 18px; font-weight: 800; }
.sw-stats dd.hot { color: var(--color-error, #dc2626); }
.sw-overdue {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}
.sw-overdue li { border-bottom: 1px solid var(--color-border, rgba(15, 23, 42, 0.05)); }
.sw-overdue-button { padding: 8px 0; }
.sw-overdue strong {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
.sw-overdue span { float: right; font-size: 11px; color: var(--color-error, #dc2626); font-weight: 700; }
.sw-overdue p {
  margin: 4px 0 0;
  font-size: 11px;
  color: var(--color-text-secondary, #64748b);
  clear: both;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sw-empty {
  padding: 20px;
  text-align: center;
  font-size: 13px;
  color: var(--color-text-secondary, #64748b);
}
.sw-empty.tall {
  flex: 1;
  display: grid;
  place-items: center;
}
@media (max-width: 1100px) {
  .sw-shell {
    height: auto;
    min-height: 100%;
    overflow: visible;
  }
  .sw-grid {
    flex: none;
    grid-template-columns: minmax(0, 1fr);
    min-height: 0;
  }
  .sw-list { height: 320px; }
  .sw-thread { height: min(680px, 75dvh); min-height: 420px; }
  .sw-alerts { height: 300px; }
}
@media (min-width: 1101px) and (max-height: 700px) {
  .sw-shell { gap: 8px; }
  .sw-thread-head { padding: 8px 12px; }
  .sw-compose { padding: 8px 12px; }
  .sw-compose textarea { min-height: 56px; }
}
@media (prefers-reduced-motion: reduce) {
  .sw-typing-dots span,
  .sw-shell .animate-spin { animation: none; }
  .sw-msgs { scroll-behavior: auto; }
}
</style>
