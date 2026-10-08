<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { AuditLog } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import { dateTime } from '@/utils/format'

const target = ref('')
const offset = ref(0)
const pageSize = 50
const logs = useLoader(() => admin.auditLogs({ target: target.value, limit: pageSize, offset: offset.value }), [] as AuditLog[] | null)
onMounted(logs.load)
function search() {
  offset.value = 0
  logs.load()
}
function page(d: number) {
  offset.value = Math.max(0, offset.value + d * pageSize)
  logs.load()
}
</script>

<template>
  <div>
    <PageHeader title="审计日志" description="管理员的所有写操作。">
      <input v-model="target" class="input w-56" placeholder="按对象筛选，例如 upstream:12" aria-label="对象" @keyup.enter="search" />
      <button class="btn-secondary" @click="search">查询</button>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'created_at', label: '时间' },
          { key: 'actor_id', label: '管理员' },
          { key: 'action', label: '操作' },
          { key: 'target', label: '对象' },
          { key: 'detail', label: '详情' },
        ]"
        :rows="logs.data.value"
        :loading="logs.loading.value"
        row-key="id"
      >
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actor_id="{ row }">#{{ row.actor_id }}</template>
        <template #cell-detail="{ row }"><code class="block max-w-md truncate text-xs" :title="JSON.stringify(row.detail)">{{ JSON.stringify(row.detail) }}</code></template>
      </DataTable>
      <div class="mt-3 flex justify-end gap-2">
        <button class="btn-secondary btn-sm" :disabled="offset === 0" @click="page(-1)">上一页</button>
        <button class="btn-secondary btn-sm" :disabled="(logs.data.value?.length ?? 0) < pageSize" @click="page(1)">下一页</button>
      </div>
    </div>
  </div>
</template>
