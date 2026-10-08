<script setup lang="ts">
// 账号的官方订阅额度：各窗口的使用率、重置时间与窗口内经平台的用量；支持自留比例的平台标出暂停调度线。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { seller } from '@/api'
import type { AccountQuota } from '@/api/types'
import { useToast } from '@/stores/toast'
import { dateTime, tokens, usd } from '@/utils/format'

const props = defineProps<{ upstreamId: number }>()
const toast = useToast()
const q = ref<AccountQuota | null>(null)
const loading = ref(false)
const now = ref(Date.now())
let clock: ReturnType<typeof setInterval> | null = null

async function load(refresh = false) {
  loading.value = true
  try {
    q.value = await seller.quota(props.upstreamId, refresh)
    if (refresh) toast.success('已刷新')
  } catch (e) {
    toast.error(e)
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  load()
  clock = setInterval(() => (now.value = Date.now()), 30000)
})
onUnmounted(() => clock && clearInterval(clock))

const stopAt = computed(() => (q.value?.self_reserve_percent ? 100 - q.value.self_reserve_percent : null))
const alertText: Record<string, string> = {
  needs_reauth: '授权已失效，请重新授权',
  banned: '官方提示账号已被封禁',
  needs_verify: '官方要求人工验证账号',
  credential_invalid: 'Key 已失效',
}
const sourceText: Record<string, string> = {
  passive: '来自最近一次请求的官方响应',
  active: '向官方查询',
  coding_plan: '向官方查询',
}

function resetsIn(v?: string) {
  if (!v) return ''
  const s = Math.max(0, Math.floor((new Date(v).getTime() - now.value) / 1000))
  const d = Math.floor(s / 86400)
  const hh = Math.floor((s % 86400) / 3600)
  const mm = Math.floor((s % 3600) / 60)
  return d > 0 ? `${d} 天 ${hh} 小时后重置` : hh > 0 ? `${hh} 小时 ${mm} 分后重置` : `${mm} 分钟后重置`
}
function barClass(u: number) {
  if (stopAt.value !== null && u >= stopAt.value) return 'bg-amber-500'
  if (u >= 90) return 'bg-red-500'
  return 'bg-brand-500'
}
</script>

<template>
  <div class="card">
    <div class="mb-3 flex flex-wrap items-center gap-3">
      <h2 class="text-base font-semibold">账号额度</h2>
      <span v-if="q?.plan_level" class="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-600">{{ q.plan_level }}</span>
      <span v-if="q?.source" class="text-xs text-slate-400">
        {{ sourceText[q.source] ?? q.source }} · {{ dateTime(q.sampled_at || q.fetched_at) }}
      </span>
      <button class="btn-ghost btn-sm ml-auto" :disabled="loading" @click="load(true)">{{ loading ? '查询中…' : '刷新' }}</button>
    </div>
    <div v-for="a in q?.alerts ?? []" :key="a" class="mb-2 rounded bg-red-50 p-2 text-sm text-red-700">{{ alertText[a] ?? a }}</div>
    <p v-if="q?.error" class="mb-2 text-sm text-amber-700">{{ q.error }}</p>
    <p v-if="q && !q.windows.length && !q.error" class="text-sm text-slate-400">官方暂未提供这个账号的窗口用量。</p>
    <div class="space-y-4">
      <div v-for="w in q?.windows ?? []" :key="w.key">
        <div class="mb-1 flex flex-wrap items-baseline gap-2 text-sm">
          <span class="font-medium">{{ w.label }}</span>
          <span class="text-slate-600">已用 {{ w.utilization.toFixed(1) }}%</span>
          <span v-if="w.limit_requests" class="text-xs text-slate-400">（{{ w.used_requests }} / {{ w.limit_requests }} 次）</span>
          <span class="ml-auto text-xs text-slate-400">{{ resetsIn(w.resets_at) }}</span>
        </div>
        <div class="relative h-2 overflow-hidden rounded bg-slate-100">
          <div :class="['h-full rounded', barClass(w.utilization)]" :style="{ width: `${Math.min(100, w.utilization)}%` }" />
          <div v-if="stopAt !== null" class="absolute top-0 h-full w-0.5 bg-slate-500" :style="{ left: `${stopAt}%` }" :title="`用量达到 ${stopAt}% 时暂停调度`" />
        </div>
        <div v-if="w.requests !== undefined" class="mt-1 text-xs text-slate-500">
          窗口内经本平台 {{ w.requests }} 次请求 · {{ tokens(w.tokens) }} token · 官方价 {{ usd(w.cost) }}
        </div>
      </div>
    </div>
    <p class="hint mt-3">
      使用率由官方统计，包含你自己在其他地方的使用。<template v-if="stopAt !== null">按你设置的自留比例 {{ q?.self_reserve_percent }}%，窗口用量达到 {{ stopAt }}%（竖线）时平台暂停调度，窗口重置后自动恢复。</template>
    </p>
  </div>
</template>
