<script setup lang="ts">
/**
 * 右下角「找官方」：长轮询同步（消息/已读/输入中/未读角标）。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { support } from '@/api'
import type { SupportConversation, SupportMessage } from '@/api/types'
import { useAuth } from '@/stores/auth'
import { useLoginModal } from '@/stores/loginModal'

const auth = useAuth()
const loginModal = useLoginModal()

const open = ref(false)
const loading = ref(false)
const sending = ref(false)
const draft = ref('')
const error = ref('')
const messages = ref<SupportMessage[]>([])
const unread = ref(0)
const conversationId = ref(0)
const peerTyping = ref(false)
const peerReadId = ref(0)
const status = ref('open')
const ending = ref(false)
let syncAbort: AbortController | null = null
let syncRunning = false
let typingTimer: ReturnType<typeof setTimeout> | null = null
let typingSent = false
const listEl = ref<HTMLElement | null>(null)

const closed = computed(() => status.value === 'closed')

const WAIT_MS = 20000

const lastId = computed(() => messages.value.reduce((m, x) => Math.max(m, x.id), 0))
const lastOwnId = computed(() => {
  let id = 0
  for (const m of messages.value) {
    if (m.sender === 'user') id = m.id
  }
  return id
})

function fmtTime(v: string) {
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function receiptOf(m: SupportMessage) {
  if (m.sender !== 'user' || m.id !== lastOwnId.value) return ''
  return peerReadId.value >= m.id ? '已读' : '未读'
}

function applyConv(c?: SupportConversation | null) {
  if (!c) return
  conversationId.value = c.id
  peerTyping.value = !!c.peer_typing
  peerReadId.value = Number(c.staff_last_read_msg_id ?? 0)
  status.value = c.status || 'open'
  if (!open.value) unread.value = c.user_unread_count ?? 0
}

async function scrollBottom() {
  await nextTick()
  const el = listEl.value
  if (el) el.scrollTop = el.scrollHeight
}

function stopSync() {
  syncRunning = false
  syncAbort?.abort()
  syncAbort = null
}

async function syncLoop() {
  if (syncRunning) return
  syncRunning = true
  while (syncRunning && auth.loggedIn) {
    const ac = new AbortController()
    syncAbort = ac
    try {
      const bundle = await support.sync(
        {
          after_id: lastId.value,
          peer_read: peerReadId.value,
          peer_typing: peerTyping.value,
          unread: unread.value,
          status: status.value,
          wait_ms: WAIT_MS,
        },
        ac.signal,
      )
      if (!syncRunning) break
      applyConv(bundle.conversation)
      const rows = bundle.messages ?? []
      if (open.value && rows.length) {
        const known = new Set(messages.value.map((m) => m.id))
        const add = rows.filter((m) => !known.has(m.id))
        if (add.length) {
          messages.value = [...messages.value, ...add]
          await scrollBottom()
        }
        await support.markRead().catch(() => null)
        unread.value = 0
      } else if (!open.value) {
        unread.value = bundle.conversation?.user_unread_count ?? unread.value
      }
    } catch (e) {
      if (!syncRunning || ac.signal.aborted) break
      await new Promise((r) => setTimeout(r, 600))
    }
  }
  syncRunning = false
}

function startSync() {
  stopSync()
  void syncLoop()
}

async function ensureLoaded() {
  if (!auth.loggedIn) return
  loading.value = true
  error.value = ''
  try {
    const bundle = await support.conversation()
    applyConv(bundle.conversation)
    messages.value = bundle.messages ?? []
    await support.markRead().catch(() => null)
    unread.value = 0
    await scrollBottom()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function toggle() {
  if (!auth.loggedIn) {
    loginModal.show({ redirect: '/' })
    return
  }
  open.value = !open.value
  if (open.value) {
    await ensureLoaded()
    startSync()
  } else {
    void clearTyping()
    startSync()
  }
}

async function send() {
  const body = draft.value.trim()
  if (!body || sending.value) return
  if (!auth.loggedIn) {
    loginModal.show({ redirect: '/' })
    return
  }
  sending.value = true
  error.value = ''
  try {
    await clearTyping()
    if (!conversationId.value) await ensureLoaded()
    const wasClosed = closed.value
    const msg = await support.send(body)
    draft.value = ''
    status.value = 'open'
    if (wasClosed) {
      // 重新打开后拉全量，带上系统提示
      await ensureLoaded()
    } else if (!messages.value.some((m) => m.id === msg.id)) {
      messages.value = [...messages.value, msg]
      await scrollBottom()
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '发送失败'
  } finally {
    sending.value = false
  }
}

async function endSession() {
  if (ending.value || closed.value) return
  if (!confirm('确认结束本次会话？之后再发消息可重新联系官方。')) return
  ending.value = true
  error.value = ''
  try {
    await clearTyping()
    await support.close()
    status.value = 'closed'
    peerTyping.value = false
    await ensureLoaded()
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
  if (!typingSent) return
  typingSent = false
  await support.typing(false).catch(() => null)
}

function onDraftInput() {
  if (!auth.loggedIn || !open.value) return
  if (!typingSent) {
    typingSent = true
    void support.typing(true).catch(() => null)
  }
  if (typingTimer) clearTimeout(typingTimer)
  typingTimer = setTimeout(() => {
    typingSent = false
    void support.typing(false).catch(() => null)
  }, 1800)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    void send()
  }
}

function closePanel() {
  open.value = false
  void clearTyping()
  startSync()
}

watch(
  () => auth.loggedIn,
  (ok) => {
    if (!ok) {
      open.value = false
      messages.value = []
      unread.value = 0
      peerTyping.value = false
      stopSync()
      void clearTyping()
      return
    }
    startSync()
  },
)

onMounted(() => {
  if (auth.loggedIn) startSync()
})

onUnmounted(() => {
  stopSync()
  void clearTyping()
})
</script>

<template>
  <div class="sc-root">
    <button type="button" class="sc-fab" :class="{ on: open }" aria-label="找官方" @click="toggle">
      <span class="sc-fab-ico" aria-hidden="true">官</span>
      <span class="sc-fab-txt">找官方</span>
      <i v-if="unread > 0" class="sc-badge">{{ unread > 99 ? '99+' : unread }}</i>
    </button>

    <div v-if="open" class="sc-panel" role="dialog" aria-label="官方客服">
      <header class="sc-head">
        <div>
          <strong>闲猫官方</strong>
          <p v-if="closed">会话已结束 · 发送消息可重新咨询</p>
          <p v-else-if="peerTyping">对方正在输入…</p>
          <p v-else>有事直接说，看到就回 · 10 分钟无消息自动结束</p>
        </div>
        <div class="sc-head-actions">
          <button
            v-if="!closed"
            type="button"
            class="sc-end"
            :disabled="ending"
            @click="endSession"
          >
            {{ ending ? '…' : '结束会话' }}
          </button>
          <button type="button" class="sc-x" aria-label="关闭窗口" @click="closePanel">×</button>
        </div>
      </header>

      <div ref="listEl" class="sc-list">
        <p v-if="loading" class="sc-hint">加载中…</p>
        <p v-else-if="error" class="sc-err">{{ error }}</p>
        <div
          v-for="m in messages"
          :key="m.id"
          class="sc-row"
          :class="{
            me: m.sender === 'user',
            staff: m.sender === 'staff',
            sys: m.sender === 'system',
          }"
        >
          <div class="sc-bubble">
            <p>{{ m.body }}</p>
            <div class="sc-meta">
              <time>{{ fmtTime(m.created_at) }}</time>
              <span v-if="receiptOf(m)" class="sc-read" :class="{ on: receiptOf(m) === '已读' }">{{ receiptOf(m) }}</span>
            </div>
          </div>
        </div>
        <div v-if="peerTyping" class="sc-row staff">
          <div class="sc-bubble sc-typing">
            <span /><span /><span />
          </div>
        </div>
      </div>

      <footer class="sc-foot">
        <textarea
          v-model="draft"
          rows="2"
          maxlength="2000"
          :placeholder="closed ? '发送消息重新咨询官方…' : '输入问题，Enter 发送'"
          @input="onDraftInput"
          @keydown="onKey"
        />
        <button type="button" class="sc-send" :disabled="sending || !draft.trim()" @click="send">
          {{ sending ? '…' : closed ? '重新咨询' : '发送' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.sc-root {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 80;
  font-family: inherit;
}
.sc-fab {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: none;
  border-radius: 999px;
  padding: 10px 16px 10px 12px;
  background: #ffe60f;
  color: #1a1a1a;
  font-weight: 800;
  font-size: 13px;
  cursor: pointer;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
}
.sc-fab.on { filter: brightness(0.97); }
.sc-fab-ico {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: #fff;
  display: grid;
  place-items: center;
  font-size: 12px;
}
.sc-badge {
  position: absolute;
  top: -4px;
  right: -2px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #e11d48;
  color: #fff;
  font-size: 11px;
  font-style: normal;
  font-weight: 800;
  display: grid;
  place-items: center;
}
.sc-panel {
  position: absolute;
  right: 0;
  bottom: 56px;
  width: min(360px, calc(100vw - 24px));
  height: min(520px, calc(100vh - 100px));
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 16px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.14);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.sc-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  padding: 14px 14px 10px;
  background: linear-gradient(180deg, #fff8c4, #fff);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
}
.sc-head strong { font-size: 15px; color: #1a1a1a; }
.sc-head p { margin: 2px 0 0; font-size: 12px; color: #888; }
.sc-head-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.sc-end {
  border: 1px solid rgba(0, 0, 0, 0.12);
  background: #fff;
  border-radius: 8px;
  padding: 4px 8px;
  font-size: 12px;
  font-weight: 600;
  color: #666;
  cursor: pointer;
}
.sc-end:hover { color: #1a1a1a; border-color: rgba(0, 0, 0, 0.25); }
.sc-end:disabled { opacity: 0.5; cursor: not-allowed; }
.sc-x {
  border: none;
  background: transparent;
  font-size: 22px;
  line-height: 1;
  color: #999;
  cursor: pointer;
}
.sc-list {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: #f7f8fa;
  scrollbar-width: none;
}
.sc-list::-webkit-scrollbar { display: none; }
.sc-hint, .sc-err { font-size: 12px; color: #999; text-align: center; margin: 12px 0; }
.sc-err { color: #e11d48; }
.sc-row { display: flex; }
.sc-row.me { justify-content: flex-end; }
.sc-row.staff, .sc-row.sys { justify-content: flex-start; }
.sc-bubble {
  max-width: 82%;
  padding: 8px 10px;
  border-radius: 12px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.06);
}
.sc-row.me .sc-bubble {
  background: #ffe60f;
  border-color: transparent;
}
.sc-row.sys .sc-bubble {
  background: #eef2ff;
  border-color: transparent;
  color: #4338ca;
}
.sc-bubble p {
  margin: 0;
  font-size: 13px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
  color: #1a1a1a;
}
.sc-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 4px;
}
.sc-meta time {
  font-size: 10px;
  color: #999;
}
.sc-read {
  font-size: 10px;
  color: #b0b0b0;
}
.sc-read.on { color: #16a34a; font-weight: 700; }
.sc-typing {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 10px 12px;
  min-width: 48px;
}
.sc-typing span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #94a3b8;
  animation: sc-bounce 1.2s infinite ease-in-out;
}
.sc-typing span:nth-child(2) { animation-delay: 0.15s; }
.sc-typing span:nth-child(3) { animation-delay: 0.3s; }
@keyframes sc-bounce {
  0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
  40% { transform: translateY(-3px); opacity: 1; }
}
.sc-foot {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 8px;
  padding: 10px;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  background: #fff;
}
.sc-foot textarea {
  resize: none;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 10px;
  padding: 8px 10px;
  font: inherit;
  font-size: 13px;
  outline: none;
}
.sc-foot textarea:focus { border-color: #f5c400; }
.sc-send {
  align-self: end;
  border: none;
  border-radius: 10px;
  padding: 0 14px;
  height: 36px;
  background: #1a1a1a;
  color: #fff;
  font-weight: 700;
  font-size: 13px;
  cursor: pointer;
}
.sc-send:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
