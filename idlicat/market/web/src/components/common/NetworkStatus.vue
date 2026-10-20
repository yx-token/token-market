<script setup lang="ts">
/** 网络信号条：对齐 YourHub NetworkStatus，绿=良好 */
import { computed, onMounted, onUnmounted, ref } from 'vue'

const online = ref(typeof navigator !== 'undefined' ? navigator.onLine : true)
const latency = ref<number | null>(null)
let pingTimer: ReturnType<typeof setInterval> | null = null

const bars = computed(() => {
  if (!online.value) return 0
  if (latency.value === null) return 3
  if (latency.value < 200) return 4
  if (latency.value < 500) return 3
  if (latency.value < 1000) return 2
  return 1
})

const statusText = computed(() => {
  if (!online.value) return '网络离线'
  if (latency.value === null) return '正在检测网络…'
  if (latency.value < 200) return `网络良好（${latency.value}ms）`
  if (latency.value < 500) return `网络一般（${latency.value}ms）`
  if (latency.value < 1000) return `网络较差（${latency.value}ms）`
  return `网络很差（${latency.value}ms）`
})

const colorClass = computed(() => {
  if (!online.value) return 'text-red-500'
  if (bars.value >= 4) return 'text-emerald-500'
  if (bars.value >= 3) return 'text-amber-400'
  if (bars.value >= 2) return 'text-orange-500'
  return 'text-red-500'
})

function updateOnline() {
  online.value = navigator.onLine
  if (online.value) void pingServer()
}

async function pingServer() {
  if (!navigator.onLine) {
    online.value = false
    return
  }
  const start = performance.now()
  try {
    await fetch(`${window.location.origin}/`, { method: 'HEAD', cache: 'no-store' })
    latency.value = Math.round(performance.now() - start)
    online.value = true
  } catch {
    try {
      await fetch(`${window.location.origin}/favicon.ico`, { method: 'HEAD', cache: 'no-store' })
      latency.value = Math.round(performance.now() - start)
      online.value = true
    } catch {
      latency.value = null
      online.value = false
    }
  }
}

onMounted(() => {
  window.addEventListener('online', updateOnline)
  window.addEventListener('offline', updateOnline)
  void pingServer()
  pingTimer = setInterval(() => void pingServer(), 30000)
})

onUnmounted(() => {
  window.removeEventListener('online', updateOnline)
  window.removeEventListener('offline', updateOnline)
  if (pingTimer) clearInterval(pingTimer)
})
</script>

<template>
  <div
    class="network-status"
    :class="colorClass"
    :title="statusText"
    role="status"
    :aria-label="statusText"
  >
    <svg viewBox="0 0 24 24" class="h-5 w-5" fill="currentColor" aria-hidden="true">
      <rect x="2" y="18" width="3" height="4" rx="0.5" :opacity="bars >= 1 ? 1 : 0.25" />
      <rect x="7" y="14" width="3" height="8" rx="0.5" :opacity="bars >= 2 ? 1 : 0.25" />
      <rect x="12" y="10" width="3" height="12" rx="0.5" :opacity="bars >= 3 ? 1 : 0.25" />
      <rect x="17" y="5" width="3" height="17" rx="0.5" :opacity="bars >= 4 ? 1 : 0.25" />
    </svg>
  </div>
</template>

<style scoped>
.network-status {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 28px;
  height: 28px;
}
</style>
