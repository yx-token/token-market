<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import type { Column } from '@/components/common/types'
import {
  getPromoChannelDaily,
  getPromoChannelUsers,
  type PromoChannel,
  type PromoChannelDaily,
  type PromoChannelUser,
  type PromoDateRange,
} from './api'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const props = defineProps<{ channel: PromoChannel | null; range: PromoDateRange }>()
const emit = defineEmits<{ close: [] }>()
const dailyRows = ref<PromoChannelDaily[]>([])
const dailyLoading = ref(false)
const dailyError = ref('')
const users = ref<PromoChannelUser[]>([])
const usersLoading = ref(false)
const usersError = ref('')
const usersTotal = ref(0)
const page = ref(1)
const pageSize = ref(20)
let dailyRequest: AbortController | undefined
let usersRequest: AbortController | undefined

const userColumns: Column[] = [
  { key: 'email', label: '邮箱' },
  { key: 'user_id', label: '用户 ID' },
  { key: 'registered_at', label: '注册时间（北京时间）' },
  { key: 'recharge_amount', label: '累计充值' },
  { key: 'buyer_spend', label: '累计消费' },
  { key: 'is_seller', label: '卖家身份' },
]

function beijingToday() {
  return new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
  }).format(new Date())
}

function shiftDate(date: string, days: number) {
  const value = new Date(`${date}T00:00:00Z`)
  value.setUTCDate(value.getUTCDate() + days)
  return value.toISOString().slice(0, 10)
}

const dailyRange = computed(() => {
  if (!props.range.from && !props.range.to) return { query: {} as PromoDateRange, limited: false }
  const to = props.range.to || beijingToday()
  const from = props.range.from || shiftDate(to, -29)
  const minFrom = shiftDate(to, -365)
  return { query: { from: from < minFrom ? minFrom : from, to }, limited: from < minFrom }
})

const dailyRangeLabel = computed(() => {
  const first = dailyRows.value[0]?.date
  const last = dailyRows.value[dailyRows.value.length - 1]?.date
  if (first && last) return `${first} 至 ${last}`
  const to = dailyRange.value.query.to || beijingToday()
  return `${dailyRange.value.query.from || shiftDate(to, -29)} 至 ${to}`
})

function numeric(value: unknown) {
  const number = Number(value)
  return Number.isFinite(number) ? number : 0
}

function money(value: unknown) {
  return new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' }).format(numeric(value))
}

function registeredTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).format(date)
}

const chartData = computed(() => ({
  labels: dailyRows.value.map((row) => row.date),
  datasets: [
    { label: '访客 UV', data: dailyRows.value.map((row) => numeric(row.uv)), borderColor: '#3b82f6', yAxisID: 'people' },
    { label: '注册人数', data: dailyRows.value.map((row) => numeric(row.signups)), borderColor: '#10b981', yAxisID: 'people' },
    { label: '充值金额（元）', data: dailyRows.value.map((row) => numeric(row.recharge_amount)), borderColor: '#f59e0b', yAxisID: 'amount' },
  ].map((dataset) => ({ ...dataset, tension: 0.2, pointRadius: dailyRows.value.length > 60 ? 0 : 2 })),
}))

const chartOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index', intersect: false },
  plugins: { legend: { position: 'top' } },
  scales: {
    x: { ticks: { maxTicksLimit: 10 } },
    people: { position: 'left', beginAtZero: true, title: { display: true, text: '人数' }, ticks: { precision: 0 } },
    amount: { position: 'right', beginAtZero: true, title: { display: true, text: '充值金额（元）' }, grid: { drawOnChartArea: false } },
  },
}

async function loadDaily() {
  dailyRequest?.abort()
  if (!props.channel) return
  const request = new AbortController()
  dailyRequest = request
  dailyLoading.value = true
  dailyError.value = ''
  dailyRows.value = []
  try {
    const result = await getPromoChannelDaily(props.channel.id, dailyRange.value.query, { signal: request.signal })
    if (request.signal.aborted) return
    dailyRows.value = Array.isArray(result.items) ? [...result.items].sort((a, b) => a.date.localeCompare(b.date)) : []
  } catch (error) {
    if (!request.signal.aborted) dailyError.value = error instanceof Error ? error.message : '每日趋势加载失败'
  } finally {
    if (dailyRequest === request) dailyLoading.value = false
  }
}

async function loadUsers() {
  usersRequest?.abort()
  if (!props.channel) return
  const request = new AbortController()
  usersRequest = request
  usersLoading.value = true
  usersError.value = ''
  try {
    const result = await getPromoChannelUsers(props.channel.id, page.value, pageSize.value, { signal: request.signal })
    if (request.signal.aborted) return
    users.value = Array.isArray(result.items) ? result.items : []
    usersTotal.value = numeric(result.total)
  } catch (error) {
    if (!request.signal.aborted) usersError.value = error instanceof Error ? error.message : '归因用户加载失败'
  } finally {
    if (usersRequest === request) usersLoading.value = false
  }
}

function changePage(value: number) {
  page.value = value
  void loadUsers()
}

function changePageSize(value: number) {
  pageSize.value = value
  changePage(1)
}

function cancelRequests() {
  dailyRequest?.abort()
  usersRequest?.abort()
}

function close() {
  cancelRequests()
  emit('close')
}

watch(() => [props.channel?.id, props.range.from, props.range.to], () => {
  dailyRequest?.abort()
  dailyRows.value = []
  dailyError.value = ''
  dailyLoading.value = false
  if (props.channel) void loadDaily()
}, { immediate: true })

watch(() => props.channel?.id, () => {
  usersRequest?.abort()
  page.value = 1
  users.value = []
  usersTotal.value = 0
  usersError.value = ''
  usersLoading.value = false
  if (props.channel) void loadUsers()
}, { immediate: true })

onBeforeUnmount(cancelRequests)
</script>

<template>
  <BaseDialog :show="!!channel" :title="`推广渠道详情 · ${channel?.name || ''}`" width="extra-wide" @close="close">
    <div class="space-y-6">
      <section aria-labelledby="promo-daily-title" :aria-busy="dailyLoading">
        <div class="mb-3 flex flex-wrap items-baseline justify-between gap-2">
          <h4 id="promo-daily-title" class="font-semibold text-gray-900 dark:text-gray-100">每日趋势</h4>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ dailyRangeLabel }}（北京时间）</p>
        </div>
        <p v-if="dailyRange.limited" role="status" class="mb-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
          每日趋势最多展示 366 天，已截取所选截止日期前的 366 天。渠道汇总仍按原筛选区间统计。
        </p>
        <p v-else-if="!range.from && !range.to" class="mb-3 text-sm text-gray-500 dark:text-dark-400">全部时间模式下，每日趋势默认展示近 30 天。</p>
        <div v-if="dailyError" role="alert" class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
          <span>每日趋势加载失败：{{ dailyError }}</span>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadDaily">重试每日趋势</button>
        </div>
        <div v-else-if="dailyLoading" role="status" class="flex h-64 animate-pulse items-center justify-center rounded-lg bg-gray-50 text-sm text-gray-500 dark:bg-dark-800 dark:text-dark-400">每日趋势加载中…</div>
        <template v-else-if="dailyRows.length">
          <div class="h-64"><Line :data="chartData" :options="chartOptions" role="img" aria-label="每日访客、注册人数和充值金额趋势；具体数值可查看每日明细" /></div>
          <details class="mt-3 text-sm">
            <summary class="cursor-pointer text-primary-600 dark:text-primary-400">查看每日明细</summary>
            <div class="mt-3 max-h-64 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
              <table class="w-full min-w-max text-left text-gray-700 dark:text-gray-300">
                <caption class="sr-only">每日渠道数据（北京时间）</caption>
                <thead class="bg-gray-50 dark:bg-dark-800"><tr><th scope="col" class="px-4 py-2">日期</th><th scope="col" class="px-4 py-2">访客 UV</th><th scope="col" class="px-4 py-2">注册人数</th><th scope="col" class="px-4 py-2">充值金额</th></tr></thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="row in dailyRows" :key="row.date"><td class="px-4 py-2">{{ row.date }}</td><td class="px-4 py-2 tabular-nums">{{ row.uv }}</td><td class="px-4 py-2 tabular-nums">{{ row.signups }}</td><td class="px-4 py-2 tabular-nums">{{ money(row.recharge_amount) }}</td></tr></tbody>
              </table>
            </div>
          </details>
        </template>
        <EmptyState v-else title="暂无每日趋势" description="当前区间没有可展示的数据" />
      </section>

      <section aria-labelledby="promo-users-title" :aria-busy="usersLoading">
        <h4 id="promo-users-title" class="font-semibold text-gray-900 dark:text-gray-100">归因用户</h4>
        <p class="mb-3 mt-1 text-sm text-gray-500 dark:text-dark-400">展示该渠道的全部归因用户，不受顶部日期筛选影响。充值、消费为用户全部时间累计金额。</p>
        <div v-if="usersError" role="alert" class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">
          <span>归因用户加载失败：{{ usersError }}</span>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadUsers">重试归因用户</button>
        </div>
        <div v-else class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
          <DataTable :columns="userColumns" :data="users" :loading="usersLoading" row-key="user_id" :expandable-actions="false">
            <template #cell-email="{ row }"><span class="break-all">{{ row.email || '—' }}</span></template>
            <template #cell-registered_at="{ row }">{{ registeredTime(row.registered_at) }}</template>
            <template #cell-recharge_amount="{ row }">{{ money(row.recharge_amount) }}</template>
            <template #cell-buyer_spend="{ row }">{{ money(row.buyer_spend) }}</template>
            <template #cell-is_seller="{ row }">{{ row.is_seller ? '卖家' : '普通用户' }}</template>
            <template #empty><EmptyState title="暂无归因用户" description="用户通过此渠道链接注册后会显示在这里" /></template>
          </DataTable>
          <Pagination v-if="usersTotal > 0" :total="usersTotal" :page="page" :page-size="pageSize" :page-size-options="[20, 50, 100]" @update:page="changePage" @update:page-size="changePageSize" />
        </div>
      </section>
    </div>
    <template #footer><button type="button" class="btn btn-secondary" @click="close">关闭</button></template>
  </BaseDialog>
</template>
