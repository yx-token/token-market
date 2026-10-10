<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { admin } from '@/api'
import type { Upstream } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { authTypeText, label, options } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: '',
  platform: '',
  startDate: '',
  endDate: '',
})

const upstreams = useLoader(
  () => admin.upstreams({ status: queryParams.status || undefined, limit: 500, offset: 0 }),
  [] as Upstream[] | null,
)
const { busy, run } = useAction()
onMounted(upstreams.load)

const filtered = computed(() => {
  let rows = upstreams.data.value ?? []
  if (queryParams.platform) rows = rows.filter((u) => u.platform === queryParams.platform)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'seller_id', 'platform', 'failure_reason', 'health_reason'] as (keyof Upstream)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  upstreams.load()
}
function onReset() {
  resetQuery()
  resetPage()
  upstreams.load()
}

async function act(fn: () => Promise<unknown>, msg: string) {
  if (await run(fn, msg)) upstreams.load()
}
function suspend(u: Upstream) {
  const reason = prompt('冻结原因')
  if (reason !== null) act(() => admin.suspendUpstream(u.id, true, reason), '已冻结')
}
function remove(u: Upstream) {
  if (confirm(`强制删除上游 #${u.id}？`)) act(() => admin.removeUpstream(u.id), '正在删除')
}
</script>

<template>
  <ConsoleListPage id="console-upstreams-page" title="上游" description="平台全部上游账号：检测、冻结与强制删除。">
    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="ID / 卖家 / 原因" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('upstreamStatus')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">厂商</label>
        <select v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">接入日</label>
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
        { key: 'id', label: '上游' },
        { key: 'seller_id', label: '卖家' },
        { key: 'platform', label: '厂商' },
        { key: 'status', label: '状态' },
        { key: 'health', label: '健康' },
        { key: 'grade', label: '等级' },
        { key: 'privacy_ok', label: '隐私' },
        { key: 'created_at', label: '接入时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="upstreams.loading.value"
      row-key="id"
      empty="没有符合条件的上游"
    >
      <template #cell-id="{ row }">#{{ row.id }}<span v-if="row.is_reference" class="ml-1 text-xs text-sky-700">基线</span></template>
      <template #cell-platform="{ row }">{{ label('platform', row.platform).text }} · {{ authTypeText(row.platform, row.auth_type) }}</template>
      <template #cell-status="{ row }">
        <StatusBadge kind="upstreamStatus" :value="row.status" />
        <div v-if="row.failure_reason" class="max-w-xs truncate text-xs text-ink-500" :title="row.failure_reason">{{ row.failure_reason }}</div>
      </template>
      <template #cell-health="{ row }">
        <StatusBadge kind="health" :value="row.health" />
        <div v-if="row.health_reason" class="max-w-xs truncate text-xs text-ink-500" :title="row.health_reason">{{ row.health_reason }}</div>
      </template>
      <template #cell-grade="{ row }">{{ row.grade ?? '—' }}</template>
      <template #cell-privacy_ok="{ row }">{{ row.privacy_ok === undefined || row.privacy_ok === null ? '—' : row.privacy_ok ? '已关闭训练' : '未关闭' }}</template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" :disabled="busy" @click="act(() => admin.probe(row.id), '已排队检测')">检测</button>
        <button v-if="row.status !== 'suspended'" class="btn-ghost btn-sm" :disabled="busy" @click="suspend(row)">冻结</button>
        <button v-else class="btn-ghost btn-sm" :disabled="busy" @click="act(() => admin.suspendUpstream(row.id, false), '已解冻')">解冻</button>
        <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="remove(row)">删除</button>
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
