<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { buyer } from '@/api'
import type { AttemptView, BuyerKey, UsageItem } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import BuyerCostBreakdown from '@/components/BuyerCostBreakdown.vue'
import { dateTime, ms, tokens } from '@/utils/format'

const filter = reactive({ key_id: '', model: '', from: '', to: '', offset: 0 })
const pageSize = 50
const usage = useLoader(
  () =>
    buyer.usage({
      key_id: filter.key_id,
      model: filter.model,
      from: filter.from ? new Date(filter.from).toISOString() : '',
      to: filter.to ? new Date(`${filter.to}T23:59:59`).toISOString() : '',
      limit: pageSize,
      offset: filter.offset,
    }),
  [] as UsageItem[] | null,
)
const keys = useLoader(() => buyer.keys(), [] as BuyerKey[] | null)
const detail = ref<AttemptView[] | null>(null)
const detailID = ref('')
const { run } = useAction()

onMounted(() => {
  usage.load()
  keys.load()
})

function search() {
  filter.offset = 0
  usage.load()
}
function page(d: number) {
  filter.offset = Math.max(0, filter.offset + d * pageSize)
  usage.load()
}
const notes: Record<string, string> = {
  'skipped (seller unavailable)': '卖家暂不可用，已跳过',
  'interrupted, charged by actual usage': '中途断开，按实际用量计费',
  charged: '已计费',
  'no charge': '未产生费用',
}

async function openDetail(requestID?: string) {
  if (!requestID) return
  // 列表中的 request_id 是 sub2api 的请求 ID（client:…），后端也接受 Edge 请求 ID（响应头 X-TM-Request-ID）
  const res = await run(() => buyer.requestDetail(requestID))
  if (res !== undefined) {
    detail.value = res ?? []
    detailID.value = requestID
  }
}
</script>

<template>
  <div>
    <PageHeader title="用量明细" description="已入账的实际消费，通常有几分钟延迟。使用活动包时，费用为消耗的人民币预付款与平台余额扣款之和。" />
    <div class="card mb-4 flex flex-wrap items-end gap-3">
      <div>
        <label class="label" for="key">Key</label>
        <select id="key" v-model="filter.key_id" class="input w-48">
          <option value="">全部</option>
          <option v-for="k in keys.data.value ?? []" :key="k.id" :value="k.id">{{ k.name }}</option>
        </select>
      </div>
      <div>
        <label class="label" for="model">模型</label>
        <input id="model" v-model="filter.model" class="input w-48" placeholder="例如 claude-sonnet-4-5" />
      </div>
      <div class="min-w-[220px] flex-1">
        <label class="label">日期范围</label>
        <DateRangePicker v-model:start-date="filter.from" v-model:end-date="filter.to" />
      </div>
      <button class="btn-primary" @click="search">查询</button>
    </div>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'created_at', label: '时间' },
          { key: 'model', label: '模型' },
          { key: 'channel', label: '渠道' },
          { key: 'listing_id', label: '服务的挂单' },
          { key: 'input_tokens', label: '输入', align: 'right' },
          { key: 'output_tokens', label: '输出', align: 'right' },
          { key: 'cache_tokens', label: '缓存', align: 'right' },
          { key: 'first_token_ms', label: '首字', align: 'right' },
          { key: 'cost', label: '实际消费', align: 'right' },
          { key: 'actions', label: '' },
        ]"
        :rows="usage.data.value"
        :loading="usage.loading.value"
        row-key="usage_id"
      >
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-channel="{ row }"><StatusBadge kind="channel" :value="row.channel" /></template>
        <template #cell-listing_id="{ row }">{{ row.listing_id ? `#${row.listing_id}` : '—' }}</template>
        <template #cell-input_tokens="{ row }">{{ tokens(row.input_tokens) }}</template>
        <template #cell-output_tokens="{ row }">{{ tokens(row.output_tokens) }}</template>
        <template #cell-cache_tokens="{ row }">{{ tokens(row.cache_tokens) }}</template>
        <template #cell-first_token_ms="{ row }">{{ ms(row.first_token_ms) }}</template>
        <template #cell-cost="{ row }"><BuyerCostBreakdown :value="row" /></template>
        <template #cell-actions="{ row }">
          <button v-if="row.request_id" class="btn-ghost btn-sm" @click="openDetail(row.request_id)">详情</button>
        </template>
      </DataTable>
      <div class="mt-3 flex justify-end gap-2">
        <button class="btn-secondary btn-sm" :disabled="filter.offset === 0" @click="page(-1)">上一页</button>
        <button class="btn-secondary btn-sm" :disabled="(usage.data.value?.length ?? 0) < pageSize" @click="page(1)">下一页</button>
      </div>
    </div>

    <ModalDialog :open="detail !== null" title="请求详情" wide @update:open="(v: boolean) => !v && (detail = null)">
      <p class="text-xs text-slate-500">请求 {{ detailID }}。失败后切换到下一跳的尝试不产生费用。</p>
      <DataTable
        :columns="[
          { key: 'hop', label: '跳' },
          { key: 'target', label: '目标' },
          { key: 'http_status', label: '状态码' },
          { key: 'end_reason', label: '结果' },
          { key: 'charged', label: '实际消费', align: 'right' },
          { key: 'note', label: '说明' },
        ]"
        :rows="detail"
        row-key="hop"
        empty="没有找到这次请求的 Edge 记录"
      >
        <template #cell-target="{ row }">{{ row.listing_id ? `挂单 #${row.listing_id}` : row.pool_id ? '默认分组' : '—' }}</template>
        <template #cell-end_reason="{ row }"><StatusBadge kind="endReason" :value="row.end_reason" /></template>
        <template #cell-charged="{ row }"><BuyerCostBreakdown v-if="row.charged" :value="row" /><span v-else>—</span></template>
        <template #cell-note="{ row }">{{ notes[row.note] ?? row.note }}</template>
      </DataTable>
    </ModalDialog>
  </div>
</template>
