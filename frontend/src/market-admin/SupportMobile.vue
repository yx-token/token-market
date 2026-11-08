<script setup lang="ts">
/**
 * 管理端客服快捷 H5：手机上快速看待回复并回复，不进桌面工作台壳。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminSupportMobile' })

interface Conversation {
  id: number
  user_id: number
  user_email?: string
  status: string
  last_message_at: string
  last_preview: string
  last_sender: string
  staff_unread_count: number
  awaiting_staff_since?: string | null
  user_last_read_msg_id?: number
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
  reply_timeout_minutes: number
  pending_alert_fired: boolean
}

const WAIT_MS = 20000
const FILTERS = [
  { value: 'pending', label: '待回复' },
  { value: 'overdue', label: '超时' },
  { value: 'open', label: '进行中' },
  { value: '', label: '全部' },
] as const

const router = useRouter()
const filter = ref('pending')
const list = ref<Conversation[]>([])
const selectedId = ref(0)
const messages = ref<Message[]>([])
const draft = ref('')
const alerts = ref<Alerts | null>(null)
const loadingList = ref(false)
const loadingThread = ref(false)
const sending = ref(false)
const ending = ref(false)
const error = ref('')
const peerTyping = ref(false)
const peerReadId = ref(0)
const listEl = ref<HTMLElement | null>(null)

let threadAbort: AbortController | null = null
let threadGen = 0
let alertsAbort: AbortController | null = null
let alertsGen = 0
let typingTimer: ReturnType<typeof setTimeout> | null = null
let typingSent = false

const selected = computed(() => list.value.find((c) => c.id === selectedId.value) || null)
const chatMessages = computed(() => messages.value.filter((m) => m.sender !== 'system'))
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

function relativeTime(v?: string) {
  if (!v) return ''
  const t = new Date(v).getTime()
  if (Number.isNaN(t)) return v
  const diff = Date.now() - t
  if (diff < 60_000) return '刚刚'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分前`
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)} 小时前`
  return fmtTime(v)
}

function waitLabel(c: Conversation) {
  if (!c.awaiting_staff_since) return ''
  const mins = c.waiting_minutes ?? Math.floor((Date.now() - new Date(c.awaiting_staff_since).getTime()) / 60000)
  if (mins < 1) return '刚到'
  if (mins < 60) return `等 ${mins} 分`
  return `等 ${Math.floor(mins / 60)} 时`
}

function avatarText(c: Conversation) {
  const raw = (c.user_email || String(c.user_id || '')).trim()
  if (!raw) return '?'
  const name = raw.includes('@') ? raw.split('@')[0] : raw
  return name.slice(0, 1).toUpperCase()
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

async function loadList(silent = false) {
  if (!silent) loadingList.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ limit: '50' })
    if (filter.value) params.set('status', filter.value)
    const res = await marketGet<{ items: Conversation[] | null; total: number }>(
      `${MARKET_API_PREFIX}/support/conversations?${params}`,
    )
    const prevId = selectedId.value
    const pinned = prevId ? list.value.find((c) => c.id === prevId) || selected.value : null
    list.value = res.items ?? []
    if (prevId && !list.value.some((c) => c.id === prevId) && pinned && pinned.id === prevId) {
      list.value = [{ ...pinned, awaiting_staff_since: null, overdue: false }, ...list.value]
    }
  } catch (e) {
    if (!silent) error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loadingList.value = false
  }
}

function stopThreadLoop() {
  threadGen += 1
  threadAbort?.abort()
  threadAbort = null
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
    if (document.visibilityState === 'visible') {
      await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
      window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
      const idx = list.value.findIndex((c) => c.id === id)
      if (idx >= 0) list.value[idx].staff_unread_count = 0
    }
    await scrollBottom()
    void threadLoop()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载会话失败'
  } finally {
    loadingThread.value = false
  }
}

function backToList() {
  void clearTyping()
  stopThreadLoop()
  selectedId.value = 0
  messages.value = []
  peerTyping.value = false
  draft.value = ''
  void loadList(true)
  void loadAlerts()
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
          await scrollBottom()
          if (document.visibilityState === 'visible') {
            await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${id}/read`).catch(() => null)
            window.dispatchEvent(new CustomEvent('market-support-alerts-refresh'))
          }
        }
        void loadList(true)
      }
    } catch {
      if (gen !== threadGen) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
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
      if (changed && !selectedId.value) void loadList(true)
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
  if (!selectedId.value || ending.value) return
  if (!confirm('确认结束该会话？')) return
  ending.value = true
  try {
    await clearTyping()
    await marketSend('POST', `${MARKET_API_PREFIX}/support/conversations/${selectedId.value}/close`)
    backToList()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '结束失败'
  } finally {
    ending.value = false
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
  }, 1800)
}

watch(filter, () => {
  selectedId.value = 0
  messages.value = []
  void loadList()
})

onMounted(async () => {
  await Promise.all([loadList(), loadAlerts()])
  void alertsLoop()
})

onUnmounted(() => {
  stopThreadLoop()
  alertsGen += 1
  alertsAbort?.abort()
  alertsAbort = null
  void clearTyping()
})
</script>

<template>
  <div class="sm-root">
    <header class="sm-top">
      <template v-if="!selected">
        <div class="sm-top-main">
          <strong>客服快捷回复</strong>
          <p>
            待回 {{ alerts?.pending_count ?? 0 }}
            · 未读 {{ alerts?.staff_unread_total ?? 0 }}
            <template v-if="alerts?.overdue_count"> · 超时 {{ alerts.overdue_count }}</template>
          </p>
        </div>
        <button type="button" class="sm-link" @click="router.push('/admin/market/support')">
          桌面版 / 预警设置
        </button>
      </template>
      <template v-else>
        <button type="button" class="sm-back" @click="backToList">‹</button>
        <div class="sm-top-main">
          <strong>{{ selected.user_email || `用户 #${selected.user_id}` }}</strong>
          <p>
            #{{ selected.id }}
            <template v-if="peerTyping"> · 对方输入中</template>
            <template v-else-if="selected.overdue"> · 已超时</template>
            <template v-else-if="selected.awaiting_staff_since"> · {{ waitLabel(selected) }}</template>
          </p>
        </div>
        <button
          v-if="selected.status !== 'closed'"
          type="button"
          class="sm-link danger"
          :disabled="ending"
          @click="closeConversation"
        >
          结束
        </button>
        <span v-else class="sm-closed">已结束</span>
      </template>
    </header>

    <p v-if="error" class="sm-err">{{ error }}</p>

    <section v-if="!selected" class="sm-list-pane">
      <div class="sm-filters">
        <button
          v-for="f in FILTERS"
          :key="f.value || 'all'"
          type="button"
          class="sm-filter"
          :class="{ on: filter === f.value }"
          @click="filter = f.value"
        >
          {{ f.label }}
          <i v-if="f.value === 'pending' && alerts?.pending_count">{{ alerts.pending_count }}</i>
          <i v-else-if="f.value === 'overdue' && alerts?.overdue_count">{{ alerts.overdue_count }}</i>
        </button>
      </div>
      <ul class="sm-cards">
        <li v-if="loadingList && !list.length" class="sm-empty">加载中…</li>
        <li v-else-if="!list.length" class="sm-empty">暂无会话</li>
        <li
          v-for="c in list"
          :key="c.id"
          class="sm-card"
          :class="{ overdue: c.overdue }"
          @click="openConversation(c.id)"
        >
          <div class="sm-avatar">{{ avatarText(c) }}</div>
          <div class="sm-card-main">
            <div class="sm-card-top">
              <strong>{{ c.user_email || `用户 #${c.user_id}` }}</strong>
              <time>{{ relativeTime(c.last_message_at) }}</time>
            </div>
            <p>{{ c.last_preview || '（无内容）' }}</p>
            <div class="sm-card-meta">
              <span v-if="c.staff_unread_count" class="sm-dot">{{ c.staff_unread_count }}</span>
              <em v-if="c.awaiting_staff_since" :class="{ hot: c.overdue }">{{ waitLabel(c) }}</em>
              <span>{{ c.last_sender === 'user' ? '用户留言' : c.last_sender === 'staff' ? '已回复' : '系统' }}</span>
            </div>
          </div>
        </li>
      </ul>
    </section>

    <section v-else class="sm-thread">
      <div ref="listEl" class="sm-msgs">
        <p v-if="loadingThread" class="sm-empty">加载中…</p>
        <div
          v-for="m in chatMessages"
          :key="m.id"
          class="sm-msg"
          :class="{ staff: m.sender === 'staff', user: m.sender === 'user' }"
        >
          <div class="sm-bubble">
            <p>{{ m.body }}</p>
            <div class="sm-meta">
              <time>{{ fmtTime(m.created_at) }}</time>
              <span v-if="receiptOf(m)" :class="{ on: receiptOf(m) === '已读' }">{{ receiptOf(m) }}</span>
            </div>
          </div>
        </div>
        <div v-if="peerTyping" class="sm-msg user">
          <div class="sm-bubble typing"><span /><span /><span /></div>
        </div>
      </div>

      <footer class="sm-compose">
        <textarea
          v-model="draft"
          rows="2"
          maxlength="2000"
          :placeholder="selected.status === 'closed' ? '会话已结束' : '输入回复…'"
          :disabled="selected.status === 'closed'"
          @input="onDraftInput"
        />
        <button
          type="button"
          class="sm-send"
          :disabled="sending || !draft.trim() || selected.status === 'closed'"
          @click="send"
        >
          {{ sending ? '…' : '发送' }}
        </button>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.sm-root {
  --sm-bg: #f3f5f9;
  --sm-card: #fff;
  --sm-line: #e8ecf2;
  --sm-text: #1f2937;
  --sm-muted: #8b95a7;
  --sm-primary: #3b82f6;
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  flex-direction: column;
  background: var(--sm-bg);
  color: var(--sm-text);
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  padding:
    env(safe-area-inset-top, 0)
    env(safe-area-inset-right, 0)
    env(safe-area-inset-bottom, 0)
    env(safe-area-inset-left, 0);
}
.sm-top {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  background: var(--sm-card);
  border-bottom: 1px solid var(--sm-line);
}
.sm-top-main {
  flex: 1;
  min-width: 0;
}
.sm-top-main strong {
  display: block;
  font-size: 16px;
  font-weight: 700;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sm-top-main p {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--sm-muted);
}
.sm-back {
  width: 36px;
  height: 36px;
  border: 0;
  border-radius: 10px;
  background: #f1f5f9;
  font-size: 26px;
  line-height: 1;
  color: #334155;
  cursor: pointer;
  flex-shrink: 0;
}
.sm-link {
  border: 0;
  background: transparent;
  color: var(--sm-primary);
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  white-space: nowrap;
  padding: 6px 4px;
}
.sm-link.danger { color: #dc2626; }
.sm-link:disabled { opacity: 0.5; }
.sm-closed {
  font-size: 12px;
  color: var(--sm-muted);
  font-weight: 600;
}
.sm-err {
  margin: 0;
  padding: 8px 14px;
  background: #fef2f2;
  color: #b91c1c;
  font-size: 12px;
  font-weight: 600;
}
.sm-list-pane {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.sm-filters {
  flex-shrink: 0;
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.sm-filter {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--sm-line);
  border-radius: 999px;
  background: var(--sm-card);
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
}
.sm-filter.on {
  background: #eff6ff;
  border-color: #bfdbfe;
  color: var(--sm-primary);
}
.sm-filter i {
  font-style: normal;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #fee2e2;
  color: #dc2626;
  font-size: 11px;
  font-weight: 800;
  display: inline-grid;
  place-items: center;
}
.sm-cards {
  list-style: none;
  margin: 0;
  padding: 0 10px 16px;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  -webkit-overflow-scrolling: touch;
}
.sm-card {
  display: flex;
  gap: 10px;
  padding: 12px;
  border-radius: 14px;
  background: var(--sm-card);
  border: 1px solid var(--sm-line);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
}
.sm-card.overdue { box-shadow: inset 3px 0 0 #ef4444; }
.sm-avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #60a5fa;
  color: #fff;
  font-weight: 800;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.sm-card-main { flex: 1; min-width: 0; }
.sm-card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.sm-card-top strong {
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sm-card-top time {
  font-size: 11px;
  color: var(--sm-muted);
  flex-shrink: 0;
}
.sm-card-main > p {
  margin: 4px 0 0;
  font-size: 13px;
  color: #475569;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sm-card-meta {
  margin-top: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  color: var(--sm-muted);
}
.sm-dot {
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #ef4444;
  color: #fff;
  font-size: 11px;
  font-weight: 800;
  display: inline-grid;
  place-items: center;
}
.sm-card-meta em {
  font-style: normal;
  font-weight: 700;
  color: #d97706;
}
.sm-card-meta em.hot { color: #dc2626; }
.sm-empty {
  padding: 40px 16px;
  text-align: center;
  color: var(--sm-muted);
  font-size: 13px;
}
.sm-thread {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: #eef2f7;
}
.sm-msgs {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 12px 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  -webkit-overflow-scrolling: touch;
}
.sm-msg { display: flex; }
.sm-msg.staff { justify-content: flex-end; }
.sm-msg.user { justify-content: flex-start; }
.sm-bubble {
  max-width: 82%;
  padding: 9px 11px;
  border-radius: 14px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.05);
}
.sm-msg.staff .sm-bubble {
  background: var(--sm-primary);
  border-color: transparent;
  color: #fff;
}
.sm-bubble p {
  margin: 0;
  font-size: 14px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
.sm-meta {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 4px;
  font-size: 10px;
  opacity: 0.7;
}
.sm-meta .on { opacity: 1; font-weight: 700; }
.sm-msg.staff .sm-meta .on { color: #bbf7d0; }
.sm-msg.user .sm-meta .on { color: #16a34a; }
.sm-bubble.typing {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 12px;
  min-width: 48px;
}
.sm-bubble.typing span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #94a3b8;
  animation: sm-bounce 1.2s infinite ease-in-out;
}
.sm-bubble.typing span:nth-child(2) { animation-delay: 0.15s; }
.sm-bubble.typing span:nth-child(3) { animation-delay: 0.3s; }
@keyframes sm-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-3px); opacity: 1; }
}
.sm-compose {
  flex-shrink: 0;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 8px;
  padding: 10px 12px calc(10px + env(safe-area-inset-bottom, 0));
  background: var(--sm-card);
  border-top: 1px solid var(--sm-line);
}
.sm-compose textarea {
  resize: none;
  border: 1px solid var(--sm-line);
  border-radius: 12px;
  padding: 10px 12px;
  font: inherit;
  font-size: 15px;
  outline: none;
  min-height: 44px;
  max-height: 120px;
}
.sm-compose textarea:focus { border-color: #93c5fd; }
.sm-send {
  align-self: end;
  height: 44px;
  min-width: 64px;
  border: 0;
  border-radius: 12px;
  background: var(--sm-primary);
  color: #fff;
  font-weight: 700;
  font-size: 14px;
  cursor: pointer;
}
.sm-send:disabled { opacity: 0.45; cursor: not-allowed; }
</style>
