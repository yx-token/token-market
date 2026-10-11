<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { InspectionCheckDefinition } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import { inspectionCategories } from '@/utils/inspection'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'

const toast = useToast()
const checks = useLoader(() => admin.inspectionChecks(), [] as InspectionCheckDefinition[])
const query = ref('')
const category = ref('')
const rows = computed(() => checks.data.value.filter((check) => (!category.value || check.category === category.value) && `${check.key} ${check.name}`.includes(query.value.trim())))
const editing = ref<InspectionCheckDefinition | null>(null)
const form = reactive({ enabled: false, calibrated: false, calibrationNote: '', coefficients: '', thresholds: '' })
const { busy, run } = useAction()
onMounted(checks.load)

function edit(check: InspectionCheckDefinition) {
  editing.value = check
  Object.assign(form, { enabled: check.enabled, calibrated: check.calibrated, calibrationNote: check.calibration_note, coefficients: JSON.stringify(check.coefficients, null, 2), thresholds: JSON.stringify(check.thresholds, null, 2) })
}

function numberObject(text: string): Record<string, number> {
  const value = JSON.parse(text)
  if (!value || Array.isArray(value) || typeof value !== 'object' || Object.values(value).some((number) => typeof number !== 'number' || !Number.isFinite(number))) throw new Error()
  return value
}

async function save() {
  if (!editing.value) return
  let coefficients: Record<string, number>, thresholds: Record<string, number>
  try { coefficients = numberObject(form.coefficients); thresholds = numberObject(form.thresholds) } catch {
    toast.error('系数与阈值需要是数值 JSON 对象')
    return
  }
  if (await run(() => admin.updateInspectionCheck(editing.value!.key, { enabled: form.enabled, calibrated: form.calibrated, calibration_note: form.calibrationNote, coefficients, thresholds }), '质检项已保存')) {
    editing.value = null
    checks.load()
  }
}
</script>

<template>
  <ConsoleListPage id="console-inspection-checks-page" title="质检项" description="按质检目录启用规则、登记运营校准并调整系数与阈值。仅启用不能让未校准规则参与扣价。">
    <template #filters>
      <div class="filter-item"><label class="filter-label">关键字</label><input v-model="query" class="filter-input" placeholder="质检项键 / 名称" /></div>
      <div class="filter-item"><label class="filter-label">分组</label><select v-model="category" class="filter-input"><option value="">全部</option><option v-for="(name, key) in inspectionCategories" :key="key" :value="key">{{ name }}</option></select></div>
    </template>
    <DataTable :rows="rows" :loading="checks.loading.value" row-key="key" :columns="[{key:'key',label:'质检项'}, {key:'category',label:'分组'}, {key:'role',label:'作用'}, {key:'enabled',label:'启用'}, {key:'calibrated',label:'校准状态'}, {key:'coefficients',label:'系数'}, {key:'actions',label:'操作'}]">
      <template #cell-key="{row}"><span class="font-mono">{{ row.key }}</span> · {{ row.name }}</template>
      <template #cell-category="{row}">{{ inspectionCategories[row.category] ?? row.category }}</template>
      <template #cell-role="{row}">{{ row.role === 'gate' ? '门槛' : row.role === 'factor' ? '扣价因子' : '说明' }}</template>
      <template #cell-enabled="{row}">{{ row.enabled ? '开启' : '关闭' }}</template>
      <template #cell-calibrated="{row}"><span :class="row.calibrated ? 'text-emerald-700' : 'text-amber-700'">{{ row.calibrated ? '已校准' : '待运营校准，不扣价' }}</span></template>
      <template #cell-coefficients="{row}">{{ JSON.stringify(row.coefficients) }}</template>
      <template #cell-actions="{row}"><button class="btn-ghost btn-sm" @click="edit(row)">编辑</button></template>
    </DataTable>
  </ConsoleListPage>
  <ModalDialog :open="!!editing" :title="`编辑 ${editing?.key} · ${editing?.name}`" @update:open="(open: boolean) => !open && (editing = null)">
    <label class="flex items-center gap-2 text-sm"><input id="check-enabled" v-model="form.enabled" type="checkbox" />启用质检项</label>
    <label class="flex items-center gap-2 text-sm"><input id="check-calibrated" v-model="form.calibrated" type="checkbox" />已完成运营校准</label>
    <p class="hint">仅开启规则不会参与扣价；完成 A/A 和阳性对照等校准后，再登记校准状态。</p>
    <div><label class="label" for="check-calibration-note">校准记录</label><textarea id="check-calibration-note" v-model="form.calibrationNote" class="input" placeholder="型号、入口、网关版本、样本数量、误报情况与校准记录标识" /></div>
    <div><label class="label" for="check-coefficients">系数（JSON）</label><textarea id="check-coefficients" v-model="form.coefficients" class="input font-mono" /></div>
    <div><label class="label" for="check-thresholds">阈值（JSON）</label><textarea id="check-thresholds" v-model="form.thresholds" class="input font-mono" /></div>
    <p class="hint">仅可编辑目录已注册的参数。{{ editing?.description }}{{ editing?.advice }}</p>
    <template #footer><button class="btn-secondary" @click="editing = null">取消</button><button class="btn-primary" :disabled="busy" @click="save">保存</button></template>
  </ModalDialog>
</template>
