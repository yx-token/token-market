<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Pool } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { label, options } from '@/utils/labels'
import { multiplier, percent } from '@/utils/format'

const pools = useLoader(() => admin.pools(), [] as Pool[] | null)
const { busy, run } = useAction()
const editing = ref<Pool | null>(null)
const show = ref(false)
const form = reactive({ name: 'tm-pool-', platform: 'anthropic', sell: '', margin: '0.10', buffer: '0.02', enabled: true })
onMounted(pools.load)

function openNew() {
  editing.value = null
  Object.assign(form, { name: 'tm-pool-', platform: 'anthropic', sell: '', margin: '0.10', buffer: '0.02', enabled: true })
  show.value = true
}
function openEdit(p: Pool) {
  editing.value = p
  Object.assign(form, { name: p.name, platform: p.platform, sell: String(p.sell_multiplier), margin: String(p.min_margin), buffer: String(p.safety_buffer), enabled: p.enabled })
  show.value = true
}
async function save() {
  const body = {
    name: form.name,
    platform: form.platform,
    sell_multiplier: Number(form.sell),
    min_margin: Number(form.margin),
    safety_buffer: Number(form.buffer),
    enabled: form.enabled,
  }
  const ok = editing.value ? await run(() => admin.updatePool(editing.value!.id, body), '已保存') : await run(() => admin.createPool(body), '已创建')
  if (ok) {
    show.value = false
    pools.load()
  }
}
async function sync(p: Pool) {
  if (await run(() => admin.syncPool(p.id), '已同步')) pools.load()
}
const cap = (p: Pool) => p.sell_multiplier * (1 - p.min_margin - p.safety_buffer)
</script>

<template>
  <div>
    <PageHeader title="默认分组" description="买家走平台默认分组时按售价 D 计费；分组内先调度回收上游，再调度倍率不超过上限的寄售上游。">
      <button class="btn-primary" @click="openNew">新建分组</button>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'name', label: '分组' },
          { key: 'platform', label: '平台' },
          { key: 'sell_multiplier', label: '售价 D', align: 'right' },
          { key: 'margin', label: '最低毛利 / 安全缓冲', align: 'right' },
          { key: 'cap', label: '寄售倍率上限', align: 'right' },
          { key: 'enabled', label: '状态' },
          { key: 'sub2api_group_id', label: 'sub2api 分组' },
          { key: 'actions', label: '' },
        ]"
        :rows="pools.data.value"
        :loading="pools.loading.value"
        row-key="id"
      >
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-sell_multiplier="{ row }">{{ multiplier(row.sell_multiplier) }}</template>
        <template #cell-margin="{ row }">{{ percent(row.min_margin, 0) }} / {{ percent(row.safety_buffer, 0) }}</template>
        <template #cell-cap="{ row }">{{ multiplier(cap(row)) }}</template>
        <template #cell-enabled="{ row }">{{ row.enabled ? '启用' : '停用' }}</template>
        <template #cell-sub2api_group_id="{ row }">{{ row.sub2api_group_id ? `#${row.sub2api_group_id}` : '未同步' }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-ghost btn-sm" @click="openEdit(row)">编辑</button>
          <button class="btn-ghost btn-sm" :disabled="busy" @click="sync(row)">同步</button>
        </template>
      </DataTable>
    </div>
    <ModalDialog v-model:open="show" :title="editing ? '编辑默认分组' : '新建默认分组'">
      <div>
        <label class="label" for="gname">名称（同时是 sub2api 分组名，以 tm-pool- 开头）</label>
        <input id="gname" v-model="form.name" class="input" :disabled="!!editing" />
      </div>
      <div>
        <label class="label" for="gplat">平台</label>
        <select id="gplat" v-model="form.platform" class="input" :disabled="!!editing">
          <option v-for="p in options('platform')" :key="p.value" :value="p.value">{{ p.text }}</option>
        </select>
      </div>
      <div class="grid grid-cols-3 gap-3">
        <div>
          <label class="label" for="gsell">售价 D</label>
          <input id="gsell" v-model="form.sell" class="input" inputmode="decimal" />
        </div>
        <div>
          <label class="label" for="gmargin">最低毛利</label>
          <input id="gmargin" v-model="form.margin" class="input" inputmode="decimal" />
        </div>
        <div>
          <label class="label" for="gbuf">安全缓冲</label>
          <input id="gbuf" v-model="form.buffer" class="input" inputmode="decimal" />
        </div>
      </div>
      <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" /> 启用</label>
      <template #footer>
        <button class="btn-secondary" @click="show = false">取消</button>
        <button class="btn-primary" :disabled="busy" @click="save">保存</button>
      </template>
    </ModalDialog>
  </div>
</template>
