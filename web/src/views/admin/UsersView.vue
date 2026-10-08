<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { KYCItem } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import { dateTime } from '@/utils/format'

const kyc = useLoader(() => admin.pendingKYC(), [] as KYCItem[] | null)
const { busy, run } = useAction()
const userId = ref('')
onMounted(kyc.load)

async function review(item: KYCItem, approve: boolean) {
  const note = approve ? '' : (prompt('驳回原因') ?? '')
  if (await run(() => admin.reviewKYC(item.user_id, approve, note), approve ? '已通过' : '已驳回')) kyc.load()
}
async function setStatus(status: string) {
  const id = Number(userId.value)
  if (!id) return
  if (status === 'frozen' && !confirm(`冻结用户 #${id}？冻结后无法登录与调用。`)) return
  await run(() => admin.setUserStatus(id, status), status === 'frozen' ? '已冻结' : '已解冻')
}
</script>

<template>
  <div>
    <PageHeader title="用户与实名" />
    <div class="card mb-5">
      <h2 class="card-title">待审核实名</h2>
      <DataTable
        :columns="[
          { key: 'user_id', label: '用户' },
          { key: 'email', label: '邮箱' },
          { key: 'real_name', label: '姓名' },
          { key: 'id_number_masked', label: '证件号' },
          { key: 'submitted_at', label: '提交时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="kyc.data.value"
        :loading="kyc.loading.value"
        row-key="user_id"
      >
        <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
        <template #cell-submitted_at="{ row }">{{ dateTime(row.submitted_at) }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-ghost btn-sm" :disabled="busy" @click="review(row, true)">通过</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="review(row, false)">驳回</button>
        </template>
      </DataTable>
    </div>
    <div class="card">
      <h2 class="card-title">冻结 / 解冻用户</h2>
      <div class="flex items-end gap-2">
        <div>
          <label class="label" for="uid">用户 ID</label>
          <input id="uid" v-model="userId" class="input w-32" inputmode="numeric" />
        </div>
        <button class="btn-danger" :disabled="busy" @click="setStatus('frozen')">冻结</button>
        <button class="btn-secondary" :disabled="busy" @click="setStatus('active')">解冻</button>
      </div>
    </div>
  </div>
</template>
