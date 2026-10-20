<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { admin } from '@/api'
import type { RiskEvent } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { riskKinds } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: 'open',
  severity: '',
  startDate: '',
  endDate: '',
})

const events = useLoader(() => admin.riskEvents(queryParams.status), [] as RiskEvent[] | null)
const { busy, run } = useAction()
onMounted(events.load)

const filtered = computed(() => {
  let rows = events.data.value ?? []
  if (queryParams.severity) rows = rows.filter((e) => e.severity === queryParams.severity)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'kind', 'user_id', 'related_user_id'] as (keyof RiskEvent)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  events.load()
}
function onReset() {
  resetQuery()
  resetPage()
  events.load()
}
async function resolve(e: RiskEvent, st: string) {
  if (await run(() => admin.resolveRisk(e.id, st), '已处理')) events.load()
}
</script>

<template>
  <ConsoleListPage
    id="console-risk-page"
    title="风控"
    description="有未处理风险事件的卖家，提现自动转人工审核。必要时在「用户与实名」中冻结账号。"
  >
    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="编号 / 类型 / 用户" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="open">待处理</option>
          <option value="resolved">已处理</option>
          <option value="ignored">已忽略</option>
          <option value="">全部</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">级别</label>
        <select v-model="queryParams.severity" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="high">高</option>
          <option value="medium">中</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">发现日</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      :columns="[
        { key: 'id', label: '编号' },
        { key: 'kind', label: '类型' },
        { key: 'severity', label: '级别' },
        { key: 'users', label: '涉及用户' },
        { key: 'detail', label: '详情' },
        { key: 'status', label: '状态' },
        { key: 'created_at', label: '发现时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="events.loading.value"
      row-key="id"
      empty="没有符合条件的风险事件"
    >
      <template #cell-kind="{ row }">{{ riskKinds[row.kind] ?? row.kind }}</template>
      <template #cell-severity="{ row }">
        <span :class="row.severity === 'high' ? 'text-red-700' : 'text-amber-700'">{{ row.severity === 'high' ? '高' : '中' }}</span>
      </template>
      <template #cell-users="{ row }">{{ [row.user_id, row.related_user_id].filter(Boolean).map((x) => `#${x}`).join('、') }}</template>
      <template #cell-detail="{ row }"><code class="text-xs">{{ row.detail ? JSON.stringify(row.detail) : '—' }}</code></template>
      <template #cell-status="{ row }"><StatusBadge kind="riskStatus" :value="row.status" /></template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <template v-if="row.status === 'open'">
          <button class="btn-ghost btn-sm" :disabled="busy" @click="resolve(row, 'resolved')">已处理</button>
          <button class="btn-ghost btn-sm" :disabled="busy" @click="resolve(row, 'ignored')">忽略</button>
        </template>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>
</template>
