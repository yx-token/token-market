<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { Upstream } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { label, options } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const status = ref('')
const offset = ref(0)
const pageSize = 100
const upstreams = useLoader(() => admin.upstreams({ status: status.value, limit: pageSize, offset: offset.value }), [] as Upstream[] | null)
const { busy, run } = useAction()
onMounted(upstreams.load)

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
function page(d: number) {
  offset.value = Math.max(0, offset.value + d * pageSize)
  upstreams.load()
}
</script>

<template>
  <div>
    <PageHeader title="上游">
      <select v-model="status" class="input w-36" aria-label="状态" @change="(offset = 0), upstreams.load()">
        <option value="">全部状态</option>
        <option v-for="o in options('upstreamStatus')" :key="o.value" :value="o.value">{{ o.text }}</option>
      </select>
    </PageHeader>
    <div class="card">
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
          { key: 'actions', label: '' },
        ]"
        :rows="upstreams.data.value"
        :loading="upstreams.loading.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}<span v-if="row.is_reference" class="ml-1 text-xs text-violet-600">基线</span></template>
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }} · {{ label('authType', row.auth_type).text }}</template>
        <template #cell-status="{ row }">
          <StatusBadge kind="upstreamStatus" :value="row.status" />
          <div v-if="row.failure_reason" class="max-w-xs truncate text-xs text-slate-500" :title="row.failure_reason">{{ row.failure_reason }}</div>
        </template>
        <template #cell-health="{ row }">
          <StatusBadge kind="health" :value="row.health" />
          <div v-if="row.health_reason" class="max-w-xs truncate text-xs text-slate-500" :title="row.health_reason">{{ row.health_reason }}</div>
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
      <div class="mt-3 flex justify-end gap-2">
        <button class="btn-secondary btn-sm" :disabled="offset === 0" @click="page(-1)">上一页</button>
        <button class="btn-secondary btn-sm" :disabled="(upstreams.data.value?.length ?? 0) < pageSize" @click="page(1)">下一页</button>
      </div>
    </div>
  </div>
</template>
