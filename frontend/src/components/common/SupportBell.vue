<script setup lang="ts">
/**
 * 管理端顶栏：客服未读数长轮询，点击进入工作台。
 * 角标 = 未读消息数（打开会话已读即降）；超时/积压用颜色提示。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { MARKET_API_PREFIX, marketGet } from '@/market-admin/api'

const router = useRouter()
const { t } = useI18n()

interface Alerts {
  pending_count: number
  overdue_count: number
  staff_unread_total: number
  pending_alert_fired: boolean
}

const WAIT_MS = 20000

const unread = ref(0)
const pending = ref(0)
const overdue = ref(0)
const alertFired = ref(false)
let running = false
let abort: AbortController | null = null

const badge = computed(() => {
  const n = unread.value
  if (n <= 0) return ''
  return n > 99 ? '99+' : String(n)
})

const title = computed(() => {
  if (unread.value <= 0 && pending.value <= 0) return t('nav.marketSupport')
  const parts: string[] = []
  if (unread.value > 0) parts.push(`${unread.value} ${t('nav.marketSupportUnread')}`)
  if (pending.value > 0) parts.push(`${pending.value} ${t('nav.marketSupportPending')}`)
  if (overdue.value > 0) parts.push(`${overdue.value} ${t('nav.marketSupportOverdue')}`)
  return parts.join(' · ') || t('nav.marketSupport')
})

function apply(a: Alerts | null | undefined) {
  unread.value = Number(a?.staff_unread_total ?? 0)
  pending.value = Number(a?.pending_count ?? 0)
  overdue.value = Number(a?.overdue_count ?? 0)
  alertFired.value = !!a?.pending_alert_fired || overdue.value > 0
}

async function refreshOnce() {
  try {
    apply(await marketGet<Alerts>(`${MARKET_API_PREFIX}/support/alerts`))
  } catch {
    /* ignore */
  }
}

async function loop() {
  if (running) return
  running = true
  while (running) {
    abort?.abort()
    const ac = new AbortController()
    abort = ac
    try {
      const q = new URLSearchParams({
        pending: String(pending.value),
        overdue: String(overdue.value),
        unread: String(unread.value),
        wait_ms: String(WAIT_MS),
      })
      const a = await marketGet<Alerts>(`${MARKET_API_PREFIX}/support/alerts?${q}`, {
        signal: ac.signal,
        timeoutMs: WAIT_MS + 8000,
      })
      if (!running) break
      apply(a)
    } catch {
      if (!running) break
      await new Promise((r) => setTimeout(r, 1000))
    }
  }
  running = false
}

function openWorkbench() {
  void router.push('/admin/market/support')
}

function onVisible() {
  if (document.visibilityState !== 'visible') return
  void refreshOnce()
}

function onAlertsRefresh() {
  void refreshOnce()
}

onMounted(() => {
  void refreshOnce().finally(() => void loop())
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('market-support-alerts-refresh', onAlertsRefresh)
})

onUnmounted(() => {
  running = false
  abort?.abort()
  abort = null
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('market-support-alerts-refresh', onAlertsRefresh)
})
</script>

<template>
  <button
    type="button"
    class="relative flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-all hover:scale-105 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-800"
    :class="{
      'text-amber-600 dark:text-amber-400': (unread > 0 || pending > 0) && !alertFired,
      'text-red-600 dark:text-red-400': alertFired,
    }"
    :title="title"
    :aria-label="title"
    @click="openWorkbench"
  >
    <Icon name="chatBubble" size="md" />
    <span
      v-if="badge"
      class="absolute -right-0.5 -top-0.5 flex h-[18px] min-w-[18px] items-center justify-center rounded-full px-1 text-[10px] font-extrabold leading-none text-white"
      :class="alertFired ? 'bg-red-500' : 'bg-amber-500'"
    >
      {{ badge }}
    </span>
  </button>
</template>
