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
  market: '',
})

const pools = useLoader(() => admin.pools(), [] as Pool[] | null)
const { busy, run } = useAction()
const editing = ref<Pool | null>(null)
const show = ref(false)
const form = reactive({
  name: 'tm-pool-',
  platform: 'anthropic',
  sell: '',
  margin: '0.10',
  buffer: '0.02',
  enabled: true,
  market_published: false,
  market_title: '',
  market_description: '',
})
onMounted(pools.load)

const filtered = computed(() => {
  let rows = pools.data.value ?? []
  if (queryParams.platform) rows = rows.filter((p) => p.platform === queryParams.platform)
  if (queryParams.enabled === 'true') rows = rows.filter((p) => p.enabled)
  if (queryParams.enabled === 'false') rows = rows.filter((p) => !p.enabled)
  if (queryParams.market === 'true') rows = rows.filter((p) => p.market_published)
  if (queryParams.market === 'false') rows = rows.filter((p) => !p.market_published)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'name', 'platform', 'sub2api_group_id', 'market_title'] as (keyof Pool)[])
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

function defaultTitle(p: Pick<Pool, 'name' | 'platform'>) {
  const base = p.name.replace(/^tm-pool-/, '') || p.name
  return p.platform === 'composite' ? `${base} · 平台保障池` : `${base} · 平台保障`
}

function openNew() {
  editing.value = null
  Object.assign(form, {
    name: 'tm-pool-',
    platform: 'anthropic',
    sell: '',
    margin: '0.10',
    buffer: '0.02',
    enabled: true,
    market_published: false,
    market_title: '',
    market_description: '',
  })
  show.value = true
}
function openEdit(p: Pool) {
  editing.value = p
  Object.assign(form, {
    name: p.name,
    platform: p.platform,
    sell: String(p.sell_multiplier),
    margin: String(p.min_margin),
    buffer: String(p.safety_buffer),
    enabled: p.enabled,
    market_published: p.market_published,
    market_title: p.market_title || defaultTitle(p),
    market_description: p.market_description || '',
  })
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
    market_published: form.market_published,
    market_title: form.market_title.trim(),
    market_description: form.market_description.trim(),
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
async function toggleMarket(p: Pool) {
  const next = !p.market_published
  const ok = await run(
    () =>
      admin.updatePool(p.id, {
        market_published: next,
        market_title: p.market_title || defaultTitle(p),
        market_description: p.market_description || '',
      }),
    next ? '已上架市场' : '已下架',
  )
  if (ok) pools.load()
}
const cap = (p: Pool) => p.sell_multiplier * (1 - p.min_margin - p.safety_buffer)
</script>

<template>
  <ConsoleListPage
    id="console-pools-page"
    title="默认分组"
    description="买家走平台默认分组时按售价 D 计费；可将分组以商品形式上架到市场推销。"
  >
    <template #actions>
      <button type="button" class="btn btn-primary" @click="openNew">新建分组</button>
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="名称 / 标题 / ID" @keyup.enter="search" />
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
      <div class="filter-item">
        <label class="filter-label">市场</label>
        <select v-model="queryParams.market" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">已上架</option>
          <option value="false">未上架</option>
        </select>
      </div>
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
        { key: 'market', label: '市场商品' },
        { key: 'sub2api_group_id', label: 'sub2api 分组' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="pools.loading.value"
      row-key="id"
      empty="没有符合条件的分组"
    >
      <template #cell-name="{ row }">
        <div>{{ row.name }}</div>
        <div v-if="row.market_published && row.market_title" class="text-xs text-gray-500">{{ row.market_title }}</div>
      </template>
      <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
      <template #cell-sell_multiplier="{ row }">{{ multiplier(row.sell_multiplier) }}</template>
      <template #cell-margin="{ row }">{{ percent(row.min_margin, 0) }} / {{ percent(row.safety_buffer, 0) }}</template>
      <template #cell-cap="{ row }">{{ multiplier(cap(row)) }}</template>
      <template #cell-enabled="{ row }">{{ row.enabled ? '启用' : '停用' }}</template>
      <template #cell-market="{ row }">
        <span :class="row.market_published ? 'text-emerald-700' : 'text-gray-400'">
          {{ row.market_published ? '已上架' : '未上架' }}
        </span>
      </template>
      <template #cell-sub2api_group_id="{ row }">{{ row.sub2api_group_id ? `#${row.sub2api_group_id}` : '未同步' }}</template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" @click="openEdit(row)">编辑</button>
        <button class="btn-ghost btn-sm" :disabled="busy" @click="sync(row)">同步</button>
        <button
          class="btn-ghost btn-sm"
          :disabled="busy || (!row.market_published && (!row.enabled || !row.sub2api_group_id))"
          :title="!row.enabled || !row.sub2api_group_id ? '需先启用并同步到 sub2api' : ''"
          @click="toggleMarket(row)"
        >
          {{ row.market_published ? '下架市场' : '上架市场' }}
        </button>
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

    <div class="mt-4 border-t border-gray-100 pt-4">
      <label class="flex items-center gap-2 text-sm font-medium">
        <input v-model="form.market_published" type="checkbox" :disabled="!editing" />
        作为商品上架到市场
      </label>
      <p class="mt-1 text-xs text-gray-500">
        {{ editing ? '上架后出现在市场信息流，买家点击后去开通保障池 Key。' : '新建后请先同步，再编辑上架。' }}
      </p>
      <div class="mt-3">
        <label class="label" for="gtitle">市场标题</label>
        <input id="gtitle" v-model="form.market_title" class="input" placeholder="如：Claude · 平台保障" :disabled="!editing" />
      </div>
      <div class="mt-3">
        <label class="label" for="gdesc">市场介绍</label>
        <textarea
          id="gdesc"
          v-model="form.market_description"
          class="input min-h-[88px]"
          rows="3"
          placeholder="一两句话介绍平台保障池优势、适用场景"
          :disabled="!editing"
        />
      </div>
    </div>

    <template #footer>
      <button class="btn-secondary" @click="show = false">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>
</template>
