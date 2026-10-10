<script setup lang="ts">
// 卖家用量：上游经本平台产生的请求、token、官方价费用与收益（汇总、每日、按模型、按上游、逐次记录）。
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { seller } from '@/api'
import type { SellerUsage, SellerUsageRecord, Upstream } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import StatCard from '@/components/StatCard.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import UsageChart from '@/components/UsageChart.vue'
import QuotaPanel from '@/components/QuotaPanel.vue'
import { label } from '@/utils/labels'
import { dateTime, money, ms, tokens, usd } from '@/utils/format'

const route = useRoute()
const router = useRouter()
const upstreamID = ref(Number(route.query.upstream) || 0)
const days = ref(30)
const channel = ref('')
const model = ref('')
const upstreams = useLoader(() => seller.upstreams(), [] as Upstream[] | null)
const usage = useLoader(() => seller.usage({ upstream_id: upstreamID.value || undefined, days: days.value }), null as SellerUsage | null)
const records = ref<SellerUsageRecord[]>([])
const more = ref(false)
const loadingRecords = ref(false)
const PAGE = 50

async function loadRecords(reset = true) {
  loadingRecords.value = true
  try {
    const rows =
      (await seller.usageRecords({
        upstream_id: upstreamID.value || undefined,
        channel: channel.value || undefined,
        model: model.value || undefined,
        limit: PAGE,
        offset: reset ? 0 : records.value.length,
      })) ?? []
    records.value = reset ? rows : [...records.value, ...rows]
    more.value = rows.length === PAGE
  } finally {
    loadingRecords.value = false
  }
}

onMounted(() => {
  upstreams.load()
  usage.load()
  loadRecords()
})
watch([upstreamID, days], () => {
  router.replace({ query: upstreamID.value ? { upstream: String(upstreamID.value) } : {} })
  usage.load()
})
watch([upstreamID, channel, model], () => loadRecords())

function reload() {
  upstreams.load()
  usage.load()
  loadRecords()
}

const connected = computed(() => (upstreams.data.value ?? []).filter((u) => !['draft', 'authorizing', 'removed'].includes(u.status)))
const t = computed(() => usage.data.value?.totals)
const today = computed(() => usage.data.value?.today)
const allTokens = (x?: { input_tokens: number; output_tokens: number; cache_creation_tokens: number; cache_read_tokens: number }) =>
  x ? x.input_tokens + x.output_tokens + x.cache_creation_tokens + x.cache_read_tokens : 0
</script>

<template>
  <div>
    <PageHeader title="用量" description="你的账号经本平台被调用的情况。数据来自已入账的用量记录，约有 2 分钟延迟；不包含买家身份与请求内容。">
      <select v-model.number="upstreamID" class="input w-48" aria-label="上游">
        <option :value="0">全部上游</option>
        <option v-for="u in connected" :key="u.id" :value="u.id">#{{ u.id }} {{ label('platform', u.platform).text }}</option>
      </select>
      <select v-model.number="days" class="input w-32" aria-label="时间范围">
        <option :value="7">近 7 天</option>
        <option :value="30">近 30 天</option>
        <option :value="90">近 90 天</option>
      </select>
      <RefreshButton :loading="usage.loading.value || loadingRecords" @click="reload" />
    </PageHeader>

    <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
      <StatCard label="请求数" :value="t?.requests ?? '—'" :hint="t ? `买家 ${t.buyer_requests} · 平台检测 ${t.probe_requests} · 今日 ${today?.requests ?? 0}` : undefined" />
      <StatCard label="Token" :value="tokens(allTokens(t))" :hint="t ? `输入 ${tokens(t.input_tokens)} · 输出 ${tokens(t.output_tokens)} · 缓存 ${tokens(t.cache_creation_tokens + t.cache_read_tokens)}` : undefined" />
      <StatCard label="官方价费用" :value="usd(t?.total_cost)" hint="按官方价计算的这些请求的价值" />
      <StatCard label="收益" :value="money(t?.payout)" :hint="`今日 ${money(today?.payout ?? 0)} · 结算后冻结期满可提现`" />
    </div>

    <QuotaPanel v-if="upstreamID" :key="upstreamID" :upstream-id="upstreamID" class="mt-5" />

    <div class="card mt-5">
      <h2 class="card-title">每日用量</h2>
      <UsageChart v-if="usage.data.value" :days="usage.data.value.daily" />
    </div>

    <div class="mt-5 grid grid-cols-1 gap-5" :class="!upstreamID ? 'lg:grid-cols-2' : ''">
      <div class="card">
        <h2 class="card-title">按模型</h2>
        <DataTable
          :columns="[
            { key: 'model', label: '模型' },
            { key: 'requests', label: '请求', align: 'right' },
            { key: 'tokens', label: 'Token', align: 'right' },
            { key: 'total_cost', label: '官方价', align: 'right' },
            { key: 'payout', label: '收益', align: 'right' },
          ]"
          :rows="usage.data.value?.models"
          :loading="usage.loading.value"
          row-key="model"
        >
          <template #cell-tokens="{ row }">{{ tokens(allTokens(row)) }}</template>
          <template #cell-total_cost="{ row }">{{ usd(row.total_cost) }}</template>
          <template #cell-payout="{ row }">{{ money(row.payout) }}</template>
        </DataTable>
      </div>
      <div v-if="!upstreamID" class="card">
        <h2 class="card-title">按上游</h2>
        <DataTable
          :columns="[
            { key: 'upstream_id', label: '上游' },
            { key: 'requests', label: '请求', align: 'right' },
            { key: 'tokens', label: 'Token', align: 'right' },
            { key: 'payout', label: '收益', align: 'right' },
            { key: 'actions', label: '' },
          ]"
          :rows="usage.data.value?.upstreams"
          :loading="usage.loading.value"
          row-key="upstream_id"
        >
          <template #cell-upstream_id="{ row }">#{{ row.upstream_id }} {{ label('platform', row.platform).text }}</template>
          <template #cell-tokens="{ row }">{{ tokens(allTokens(row)) }}</template>
          <template #cell-payout="{ row }">{{ money(row.payout) }}</template>
          <template #cell-actions="{ row }">
            <button class="btn-ghost btn-sm" @click="upstreamID = row.upstream_id">查看</button>
          </template>
        </DataTable>
      </div>
    </div>

    <div class="card mt-5">
      <div class="mb-3 flex flex-wrap items-center gap-3">
        <h2 class="card-title !mb-0">调用记录</h2>
        <select v-model="channel" class="input ml-auto w-40" aria-label="来源">
          <option value="">全部来源</option>
          <option value="buyer">买家</option>
          <option value="probe">平台检测</option>
        </select>
        <select v-model="model" class="input w-48" aria-label="模型">
          <option value="">全部模型</option>
          <option v-for="m in usage.data.value?.models ?? []" :key="m.model" :value="m.model">{{ m.model }}</option>
        </select>
      </div>
      <DataTable
        :columns="[
          { key: 'created_at', label: '时间' },
          { key: 'upstream_id', label: '上游' },
          { key: 'channel', label: '来源' },
          { key: 'model', label: '模型' },
          { key: 'tokens', label: '输入 / 输出 / 缓存', align: 'right' },
          { key: 'latency', label: '首字 / 总耗时', align: 'right' },
          { key: 'total_cost', label: '官方价', align: 'right' },
          { key: 'payout', label: '收益', align: 'right' },
        ]"
        :rows="records"
        :loading="loadingRecords && !records.length"
        row-key="id"
      >
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-upstream_id="{ row }">#{{ row.upstream_id }}</template>
        <template #cell-channel="{ row }"><StatusBadge kind="channel" :value="row.channel" /></template>
        <template #cell-tokens="{ row }">
          {{ tokens(row.input_tokens) }} / {{ tokens(row.output_tokens) }} / {{ tokens(row.cache_creation_tokens + row.cache_read_tokens) }}
        </template>
        <template #cell-latency="{ row }">{{ ms(row.first_token_ms) }} / {{ ms(row.duration_ms) }}</template>
        <template #cell-total_cost="{ row }">{{ usd(row.total_cost) }}</template>
        <template #cell-payout="{ row }">{{ money(row.payout) }}</template>
      </DataTable>
      <div v-if="more" class="mt-3 text-center">
        <button class="btn-secondary btn-sm" :disabled="loadingRecords" @click="loadRecords(false)">加载更多</button>
      </div>
    </div>
  </div>
</template>
