<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Anomaly } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByDateField, filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import JsonView from '@/components/JsonView.vue'
import SearchSelect from '@/components/common/SearchSelect.vue'
import type { SelectOption } from '@/components/common/searchSelect'
import { anomalyRules } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const toast = useToast()
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: 'open',
  startDate: '',
  endDate: '',
})

const anomalies = useLoader(() => admin.anomalies(queryParams.status), [] as Anomaly[] | null)
const { busy, run } = useAction()
const viewing = ref<Anomaly | null>(null)
const showAdjust = ref(false)
const adj = reactive({ user: '', amount: '', reason: '', anomaly: '' })
// 调账目标买家：按邮箱 / ID 远程搜索，失败时降级为空，SearchSelect 仍可选中已预填值
const userLoading = ref(false)
async function searchUsers(q: string): Promise<SelectOption[]> {
  userLoading.value = true
  try {
    const list = (await admin.orgUsers(q)) ?? []
    return list.map((u) => ({
      value: String(u.id),
      text: `#${u.id} · ${u.email}${u.status && u.status !== 'active' ? ` · ${u.status}` : ''}`,
      keywords: `${u.id} ${u.email}`,
    }))
  } finally {
    userLoading.value = false
  }
}
onMounted(anomalies.load)

const filtered = computed(() => {
  let rows = anomalies.data.value ?? []
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'rule', 'buyer_user_id'] as (keyof Anomaly)[])
  rows = filterByDateField(rows, 'created_at', queryParams.startDate, queryParams.endDate)
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  anomalies.load()
}
function onReset() {
  resetQuery()
  resetPage()
  anomalies.load()
}

async function resolve(a: Anomaly, st: string) {
  const note = prompt(st === 'explained' ? '解释说明' : '忽略原因')
  if (note === null) return
  if (await run(() => admin.resolveAnomaly(a.id, st, note), '已处理')) {
    viewing.value = null
    anomalies.load()
  }
}
function openAdjust(a?: Anomaly) {
  Object.assign(adj, { user: a?.buyer_user_id ? String(a.buyer_user_id) : '', amount: '', reason: '', anomaly: a ? String(a.id) : '' })
  viewing.value = null
  showAdjust.value = true
}
async function adjust() {
  const amount = Number(adj.amount)
  if (!Number(adj.user) || !amount || !adj.reason.trim()) {
    toast.error('请填写用户、金额（正数加余额，负数扣余额）与原因')
    return
  }
  const body = { buyer_user_id: Number(adj.user), amount, reason: adj.reason, anomaly_id: adj.anomaly ? Number(adj.anomaly) : undefined }
  if (await run(() => admin.adjust(body), '已调账，余额同步到 sub2api')) {
    showAdjust.value = false
    anomalies.load()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-billing-page"
    title="计费异常与调账"
    description="计费以 sub2api 实际费用为准；对账不一致只记录，由人工解释或调账。"
  >
    <template #actions>
      <button type="button" class="btn btn-secondary" @click="openAdjust()">人工调账</button>
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="编号 / 规则 / 买家" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="open">待处理</option>
          <option value="explained">已解释</option>
          <option value="adjusted">已调账</option>
          <option value="ignored">已忽略</option>
          <option value="">全部</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">发现日</label>
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
        { key: 'id', label: '编号' },
        { key: 'rule', label: '规则' },
        { key: 'buyer_user_id', label: '买家' },
        { key: 'amount', label: '金额', align: 'right' },
        { key: 'status', label: '状态' },
        { key: 'created_at', label: '发现时间' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="anomalies.loading.value"
      row-key="id"
      empty="没有符合条件的异常"
    >
      <template #cell-rule="{ row }">{{ anomalyRules[row.rule] ?? row.rule }}</template>
      <template #cell-buyer_user_id="{ row }">{{ row.buyer_user_id ? `#${row.buyer_user_id}` : '—' }}</template>
      <template #cell-amount="{ row }">{{ money(row.amount, 4) }}</template>
      <template #cell-status="{ row }"><StatusBadge kind="anomalyStatus" :value="row.status" /></template>
      <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
      <template #cell-actions="{ row }"><button class="btn-ghost btn-sm" @click="viewing = row">处理</button></template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog :open="!!viewing" :title="`异常 #${viewing?.id}`" wide @update:open="(v: boolean) => !v && (viewing = null)">
    <p class="text-sm">{{ anomalyRules[viewing?.rule ?? ''] ?? viewing?.rule }}</p>
    <JsonView :value="viewing" />
    <template #footer>
      <template v-if="viewing?.status === 'open'">
        <button class="btn-secondary" :disabled="busy" @click="resolve(viewing!, 'ignored')">忽略</button>
        <button class="btn-secondary" :disabled="busy" @click="resolve(viewing!, 'explained')">已解释</button>
        <button class="btn-primary" @click="openAdjust(viewing!)">调账</button>
      </template>
      <button v-else class="btn-primary" @click="viewing = null">关闭</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showAdjust" title="人工调账">
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="label" for="au">买家用户</label>
        <SearchSelect
          id="au"
          v-model="adj.user"
          :options="[]"
          :remote="searchUsers"
          :loading="userLoading"
          placeholder="搜索邮箱或用户 ID"
        />
      </div>
      <div>
        <label class="label" for="aa">金额（正数加、负数扣）</label>
        <input id="aa" v-model="adj.amount" class="input" inputmode="decimal" />
      </div>
    </div>
    <div>
      <label class="label" for="ar">原因（必填，记入审计）</label>
      <input id="ar" v-model="adj.reason" class="input" />
    </div>
    <div v-if="adj.anomaly" class="text-sm text-ink-500">关联异常 #{{ adj.anomaly }}</div>
    <template #footer>
      <button class="btn-secondary" @click="showAdjust = false">取消</button>
      <button class="btn-primary" :disabled="busy" @click="adjust">确认调账</button>
    </template>
  </ModalDialog>
</template>
