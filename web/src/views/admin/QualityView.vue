<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { Appeal, QualityOverview } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { distribution, label } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const overview = useLoader(() => admin.qualityOverview(), null as QualityOverview | null)
const status = ref('open')
const appeals = useLoader(() => admin.appeals(status.value), [] as Appeal[] | null)
const { busy, run } = useAction()
onMounted(() => {
  overview.load()
  appeals.load()
})

async function resolve(a: Appeal, action: string) {
  const note = action === 'uphold' || action === 'overturn' ? (prompt('备注（可选）') ?? '') : ''
  const msg = { retest: '已安排复测', overturn: '已改判为正常', uphold: '已维持原判' }[action]
  if (await run(() => admin.resolveAppeal(a.id, action, note), msg)) appeals.load()
}
</script>

<template>
  <div>
    <PageHeader title="质量与申诉" />
    <div class="card mb-5">
      <h2 class="card-title">各厂商健康与等级分布</h2>
      <table class="table">
        <thead>
          <tr>
            <th>厂商</th>
            <th>健康</th>
            <th>等级</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="p in overview.data.value?.platforms ?? []" :key="p.platform">
            <td>{{ label('platform', p.platform).text }}</td>
            <td>
              <span v-for="(n, h) in p.health" :key="h" class="mr-2"><StatusBadge kind="health" :value="String(h)" /> {{ n }}</span>
            </td>
            <td>{{ distribution('grade', p.grades) }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="overview.data.value?.degraded_upstreams?.length" class="mt-3 text-sm">
        降智 / 不可用的上游：
        <span v-for="id in overview.data.value.degraded_upstreams" :key="id" class="mr-2">#{{ id }}</span>
        <RouterLink to="/admin/upstreams" class="link">去处理</RouterLink>
      </p>
    </div>

    <div class="card">
      <div class="mb-3 flex items-center gap-3">
        <h2 class="card-title mb-0 mr-auto">申诉</h2>
        <select v-model="status" class="input w-32" aria-label="状态" @change="appeals.load()">
          <option value="open">待处理</option>
          <option value="retesting">复测中</option>
          <option value="">全部</option>
        </select>
      </div>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'report_id', label: '报告' },
          { key: 'seller_id', label: '卖家' },
          { key: 'reason', label: '理由' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '提交时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="appeals.data.value"
        :loading="appeals.loading.value"
        row-key="id"
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
    </div>
  </div>
</template>
