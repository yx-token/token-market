<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Anomaly } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import JsonView from '@/components/JsonView.vue'
import { anomalyRules } from '@/utils/labels'
import { dateTime, money } from '@/utils/format'

const toast = useToast()
const status = ref('open')
const anomalies = useLoader(() => admin.anomalies(status.value), [] as Anomaly[] | null)
const { busy, run } = useAction()
const viewing = ref<Anomaly | null>(null)
const showAdjust = ref(false)
const adj = reactive({ user: '', amount: '', reason: '', anomaly: '' })
onMounted(anomalies.load)

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
  <div>
    <PageHeader title="计费异常与调账" description="计费以 sub2api 实际产生的费用为准；对账发现的不一致只记录，由人工解释或调账，系统不自动退款或补扣。">
      <button class="btn-secondary" @click="openAdjust()">人工调账</button>
    </PageHeader>
    <div class="card">
      <select v-model="status" class="input mb-3 w-36" aria-label="状态" @change="anomalies.load()">
        <option value="open">待处理</option>
        <option value="explained">已解释</option>
        <option value="adjusted">已调账</option>
        <option value="ignored">已忽略</option>
        <option value="">全部</option>
      </select>
      <DataTable
        :columns="[
          { key: 'id', label: '编号' },
          { key: 'rule', label: '规则' },
          { key: 'buyer_user_id', label: '买家' },
          { key: 'amount', label: '金额', align: 'right' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '发现时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="anomalies.data.value"
        :loading="anomalies.loading.value"
        row-key="id"
      >
        <template #cell-rule="{ row }">{{ anomalyRules[row.rule] ?? row.rule }}</template>
        <template #cell-buyer_user_id="{ row }">{{ row.buyer_user_id ? `#${row.buyer_user_id}` : '—' }}</template>
        <template #cell-amount="{ row }">{{ money(row.amount, 4) }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="anomalyStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }"><button class="btn-ghost btn-sm" @click="viewing = row">处理</button></template>
      </DataTable>
    </div>

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
          <label class="label" for="au">买家用户 ID</label>
          <input id="au" v-model="adj.user" class="input" inputmode="numeric" />
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
      <div v-if="adj.anomaly" class="text-sm text-slate-500">关联异常 #{{ adj.anomaly }}</div>
      <template #footer>
        <button class="btn-secondary" @click="showAdjust = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="adjust">确认调账</button>
      </template>
    </ModalDialog>
  </div>
</template>
