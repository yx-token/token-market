<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { admin } from '@/api'
import type { AuditLog } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { filterByDateField, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import { dateTime } from '@/utils/format'

const { queryParams, resetQuery, handleQuery } = useListQuery({
  target: '',
  action: '',
  startDate: '',
  endDate: '',
})

const logs = useLoader(
  () => admin.auditLogs({ target: queryParams.target.trim() || undefined, limit: 200, offset: 0 }),
  [] as AuditLog[] | null,
)
onMounted(logs.load)

const filtered = computed(() => {
  let rows = logs.data.value ?? []
  if (queryParams.action.trim()) {
    const a = queryParams.action.trim().toLowerCase()
    rows = rows.filter((r) => String(r.action).toLowerCase().includes(a))
  }
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  logs.load()
}
function onReset() {
  resetQuery()
  resetPage()
  logs.load()
}
</script>

<template>
  <ConsoleListPage id="console-audit-page" title="审计日志" description="管理员的所有写操作，可按对象、动作与时间筛选。">
    <template #filters>
      <div class="filter-item">
        <label class="filter-label">对象</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.target" class="filter-input w-full" placeholder="如 upstream:12" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">动作</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.action" class="filter-input w-full" placeholder="动作关键字" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">时间</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-item" aria-hidden="true" />
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      :columns="[
        { key: 'created_at', label: '时间' },
        { key: 'actor_id', label: '管理员' },
        { key: 'action', label: '操作' },
        { key: 'target', label: '对象' },
        { key: 'detail', label: '详情' },
      ]"
      :rows="pageRows"
      :loading="logs.loading.value"
      row-key="id"
      empty="没有符合条件的审计记录"
    >
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actor_id="{ row }">#{{ row.actor_id }}</template>
      <template #cell-detail="{ row }">
        <code class="block max-w-md truncate text-xs" :title="JSON.stringify(row.detail)">{{ JSON.stringify(row.detail) }}</code>
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
