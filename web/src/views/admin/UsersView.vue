<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { KYCItem } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime } from '@/utils/format'

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  startDate: '',
  endDate: '',
})

const kyc = useLoader(() => admin.pendingKYC(), [] as KYCItem[] | null)
const { busy, run } = useAction()
const showFreeze = ref(false)
const userId = ref('')
onMounted(kyc.load)

const filtered = computed(() => {
  let rows = kyc.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['user_id', 'email', 'real_name', 'id_number_masked'] as (keyof KYCItem)[])
  rows = filterByDateField(rows, 'submitted_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  kyc.load()
}
function onReset() {
  resetQuery()
  resetPage()
  kyc.load()
}

function openFreeze(prefill?: number | string) {
  userId.value = prefill != null ? String(prefill) : ''
  showFreeze.value = true
}

async function review(item: KYCItem, approve: boolean) {
  const note = approve ? '' : (prompt('驳回原因') ?? '')
  if (await run(() => admin.reviewKYC(item.user_id, approve, note), approve ? '已通过' : '已驳回')) kyc.load()
}

async function setStatus(status: string) {
  const id = Number(userId.value)
  if (!id) return
  if (status === 'frozen' && !confirm(`冻结用户 #${id}？冻结后无法登录与调用。`)) return
  if (await run(() => admin.setUserStatus(id, status), status === 'frozen' ? '已冻结' : '已解冻')) {
    showFreeze.value = false
  }
}
</script>

<template>
  <ConsoleListPage id="console-users-page" title="用户与实名" description="审核实名申请；按用户 ID 冻结 / 解冻账号。">
    <template #actions>
      <button type="button" class="btn btn-secondary" @click="openFreeze()">冻结 / 解冻</button>
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="用户 / 邮箱 / 姓名" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">提交日</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-item" aria-hidden="true" />
      <div class="filter-item" aria-hidden="true" />
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      :columns="[
        { key: 'user_id', label: '用户' },
        { key: 'email', label: '邮箱' },
        { key: 'real_name', label: '姓名' },
        { key: 'id_number_masked', label: '证件号' },
        { key: 'submitted_at', label: '提交时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="kyc.loading.value"
      row-key="user_id"
      empty="暂无待审核实名"
    >
      <template #cell-user_id="{ row }">#{{ row.user_id }}</template>
      <template #cell-submitted_at="{ row }">{{ dateTime(row.submitted_at) }}</template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" :disabled="busy" @click="review(row, true)">通过</button>
        <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="review(row, false)">驳回</button>
        <button class="btn-ghost btn-sm" :disabled="busy" @click="openFreeze(row.user_id)">冻结/解冻</button>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog v-model:open="showFreeze" title="冻结 / 解冻账号">
    <p class="text-sm text-gray-500">输入用户 ID，冻结后无法登录与调用 API；解冻后恢复正常。</p>
    <div>
      <label class="label" for="uid">用户 ID</label>
      <input id="uid" v-model="userId" class="input" inputmode="numeric" placeholder="例如 42" @keyup.enter="setStatus('frozen')" />
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="showFreeze = false">取消</button>
      <button type="button" class="btn btn-secondary" :disabled="busy || !Number(userId)" @click="setStatus('active')">
        解冻
      </button>
      <button type="button" class="btn btn-danger" :disabled="busy || !Number(userId)" @click="setStatus('frozen')">
        冻结
      </button>
    </template>
  </ModalDialog>
</template>
