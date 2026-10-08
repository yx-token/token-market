<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { RiskEvent } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { riskKinds } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const status = ref('open')
const events = useLoader(() => admin.riskEvents(status.value), [] as RiskEvent[] | null)
const { busy, run } = useAction()
onMounted(events.load)

async function resolve(e: RiskEvent, st: string) {
  if (await run(() => admin.resolveRisk(e.id, st), '已处理')) events.load()
}
</script>

<template>
  <div>
    <PageHeader title="风控" description="有未处理风险事件的卖家，提现自动转人工审核。必要时在“用户与实名”中冻结账号。">
      <select v-model="status" class="input w-32" aria-label="状态" @change="events.load()">
        <option value="open">待处理</option>
        <option value="resolved">已处理</option>
        <option value="ignored">已忽略</option>
        <option value="">全部</option>
      </select>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'kind', label: '类型' },
          { key: 'severity', label: '级别' },
          { key: 'users', label: '涉及用户' },
          { key: 'detail', label: '详情' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '发现时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="events.data.value"
        :loading="events.loading.value"
        row-key="id"
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
    </div>
  </div>
</template>
