<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { Pool } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { label, options } from '@/utils/labels'
import { multiplier, percent } from '@/utils/format'

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  platform: '',
  enabled: '',
})

const pools = useLoader(() => admin.pools(), [] as Pool[] | null)
const { busy, run } = useAction()
const editing = ref<Pool | null>(null)
const show = ref(false)
const form = reactive({ name: 'tm-pool-', platform: 'anthropic', sell: '', margin: '0.10', buffer: '0.02', enabled: true })
onMounted(pools.load)

const filtered = computed(() => {
  let rows = pools.data.value ?? []
  if (queryParams.platform) rows = rows.filter((p) => p.platform === queryParams.platform)
  if (queryParams.enabled === 'true') rows = rows.filter((p) => p.enabled)
  if (queryParams.enabled === 'false') rows = rows.filter((p) => !p.enabled)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'name', 'platform', 'sub2api_group_id'] as (keyof Pool)[])
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  pools.load()
}
function onReset() {
  resetQuery()
  resetPage()
  pools.load()
}

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
  <ConsoleListPage
    id="console-pools-page"
    title="默认分组"
    description="买家走平台默认分组时按售价 D 计费；分组内先调度回收上游，再调度倍率不超过上限的寄售上游。"
  >
    <template #actions>
      <button type="button" class="btn btn-primary" @click="openNew">新建分组</button>
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="名称 / ID / 分组" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">平台</label>
        <select v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.enabled" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">启用</option>
          <option value="false">停用</option>
        </select>
      </div>
      <div class="filter-item" aria-hidden="true" />
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      :columns="[
        { key: 'name', label: '分组' },
        { key: 'platform', label: '平台' },
        { key: 'sell_multiplier', label: '售价 D', align: 'right' },
        { key: 'margin', label: '最低毛利 / 安全缓冲', align: 'right' },
        { key: 'cap', label: '寄售倍率上限', align: 'right' },
        { key: 'enabled', label: '状态' },
        { key: 'sub2api_group_id', label: 'sub2api 分组' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="pools.loading.value"
      row-key="id"
      empty="没有符合条件的分组"
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

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

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
</template>
