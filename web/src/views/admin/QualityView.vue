<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { admin } from '@/api'
import type { Appeal, QualityOverview } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import TabBar from '@/components/TabBar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { distribution, label } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const tab = ref<'overview' | 'appeals'>('appeals')
const pageTitle = computed(() => (tab.value === 'overview' ? '健康分布' : '申诉'))
const pageDescription = computed(() =>
  tab.value === 'overview' ? '各厂商上游健康与等级分布。' : '处理卖家质量申诉：复测、改判或维持。',
)

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: 'open',
  startDate: '',
  endDate: '',
})

const overview = useLoader(() => admin.qualityOverview(), null as QualityOverview | null)
const appeals = useLoader(() => admin.appeals(queryParams.status), [] as Appeal[] | null)
const { busy, run } = useAction()
onMounted(() => {
  overview.load()
  appeals.load()
})

const filtered = computed(() => {
  let rows = appeals.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'report_id', 'seller_id', 'reason', 'status'] as (keyof Appeal)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

watch(tab, () => resetPage())

function search() {
  handleQuery()
  resetPage()
  appeals.load()
}
function onReset() {
  resetQuery()
  resetPage()
  appeals.load()
}

async function resolve(a: Appeal, action: string) {
  const note = action === 'uphold' || action === 'overturn' ? (prompt('备注（可选）') ?? '') : ''
  const msg = { retest: '已安排复测', overturn: '已改判为正常', uphold: '已维持原判' }[action]
  if (await run(() => admin.resolveAppeal(a.id, action, note), msg)) appeals.load()
}
</script>

<template>
  <ConsoleListPage id="console-quality-page" :title="pageTitle" :description="pageDescription">
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'appeals', text: '申诉' },
          { key: 'overview', text: '健康分布' },
        ]"
      />
    </template>

    <template v-if="tab === 'appeals'" #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="编号 / 卖家 / 理由" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="open">待处理</option>
          <option value="retesting">复测中</option>
          <option value="">全部</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">提交日</label>
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

    <template v-if="tab === 'overview'">
      <table class="table">
        <thead>
          <tr>
            <th>厂商</th>
            <th>健康</th>
            <th>等级</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!overview.data.value?.platforms?.length">
            <td colspan="3" class="!py-8 text-center text-xs text-gray-400">暂无上游</td>
          </tr>
          <tr v-for="p in overview.data.value?.platforms ?? []" :key="p.platform">
            <td>{{ label('platform', p.platform).text }}</td>
            <td>
              <span v-for="(n, h) in p.health" :key="h" class="mr-2"><StatusBadge kind="health" :value="String(h)" /> {{ n }}</span>
            </td>
            <td>{{ distribution('grade', p.grades) }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="overview.data.value?.degraded_upstreams?.length" class="mt-3 text-sm text-gray-600">
        降智 / 不可用的上游：
        <span v-for="id in overview.data.value.degraded_upstreams" :key="id" class="mr-2">#{{ id }}</span>
        <RouterLink to="/console/upstreams" class="link">去处理</RouterLink>
      </p>
    </template>

    <DataTable
      v-else
      :columns="[
        { key: 'id', label: '编号' },
        { key: 'report_id', label: '报告' },
        { key: 'seller_id', label: '卖家' },
        { key: 'reason', label: '理由' },
        { key: 'status', label: '状态' },
        { key: 'created_at', label: '提交时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="appeals.loading.value"
      row-key="id"
      empty="没有符合条件的申诉"
    >
      <template #cell-reason="{ row }"><span class="block max-w-xs truncate" :title="row.reason">{{ row.reason }}</span></template>
      <template #cell-status="{ row }"><StatusBadge kind="appealStatus" :value="row.status" /></template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }">
        <template v-if="row.status === 'open'">
          <button class="btn-ghost btn-sm" :disabled="busy" @click="resolve(row, 'retest')">复测</button>
          <button class="btn-ghost btn-sm" :disabled="busy" @click="resolve(row, 'overturn')">改判正常</button>
          <button class="btn-ghost btn-sm" :disabled="busy" @click="resolve(row, 'uphold')">维持</button>
        </template>
      </template>
    </DataTable>

    <template v-if="tab === 'appeals'" #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>
</template>
