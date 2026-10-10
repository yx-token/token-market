<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { VendorConfig } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { authTypeText, label, options } from '@/utils/labels'

const toast = useToast()
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  platform: '',
  enabled: '',
})

const vendors = useLoader(() => admin.vendors(), [] as VendorConfig[] | null)
const { busy, run } = useAction()
const editing = ref<VendorConfig | null>(null)
const form = reactive({ enabled: false, authTypes: [] as string[], min: '', max: '', base: '', concurrency: '', extra: '' })

onMounted(vendors.load)

const filtered = computed(() => {
  let rows = vendors.data.value ?? []
  if (queryParams.platform) rows = rows.filter((v) => v.platform === queryParams.platform)
  if (queryParams.enabled === 'true') rows = rows.filter((v) => v.enabled)
  if (queryParams.enabled === 'false') rows = rows.filter((v) => !v.enabled)
  rows = filterByKeyword(rows, queryParams.keyword, ['platform'] as (keyof VendorConfig)[])
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  vendors.load()
}
function onReset() {
  resetQuery()
  resetPage()
  vendors.load()
}

function edit(v: VendorConfig) {
  editing.value = v
  Object.assign(form, {
    enabled: v.enabled,
    authTypes: [...v.auth_types],
    min: String(v.consignment_min),
    max: String(v.consignment_max),
    base: JSON.stringify(v.buyback_base),
    concurrency: String(v.default_concurrency),
    extra: JSON.stringify(v.extra_limits),
  })
}

async function quickToggle(v: VendorConfig) {
  if (await run(() => admin.updateVendor(v.platform, { enabled: !v.enabled }), v.enabled ? '已关闭接入' : '已开放接入')) vendors.load()
}

async function save() {
  if (!editing.value) return
  let base: Record<string, number>
  let extra: Record<string, unknown>
  try {
    base = JSON.parse(form.base)
    extra = JSON.parse(form.extra)
  } catch {
    toast.error('回收基础倍率与限流参数需要是 JSON 对象')
    return
  }
  if (!form.authTypes.length) {
    toast.error('至少开放一种接入方式')
    return
  }
  const body = {
    enabled: form.enabled,
    auth_types: form.authTypes,
    consignment_min: Number(form.min),
    consignment_max: Number(form.max),
    buyback_base: base,
    default_concurrency: Number(form.concurrency),
    extra_limits: extra,
  }
  if (await run(() => admin.updateVendor(editing.value!.platform, body), '已保存')) {
    editing.value = null
    vendors.load()
  }
}
</script>

<template>
  <ConsoleListPage
    id="console-vendors-page"
    title="厂商"
    description="开放哪些厂商接入、寄售倍率区间、平台回收的基础倍率，以及账号在 sub2api 中的默认并发与限流参数。"
  >
    <template #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="厂商" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">厂商</label>
        <select v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">接入</label>
        <select v-model="queryParams.enabled" class="filter-input flex-1">
          <option value="">全部</option>
          <option value="true">开放</option>
          <option value="false">关闭</option>
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
        { key: 'platform', label: '厂商' },
        { key: 'enabled', label: '接入' },
        { key: 'auth_types', label: '接入方式' },
        { key: 'range', label: '寄售倍率区间' },
        { key: 'buyback_base', label: '回收基础倍率' },
        { key: 'default_concurrency', label: '默认并发', align: 'right' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="vendors.loading.value"
      row-key="platform"
      empty="没有符合条件的厂商"
    >
      <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
      <template #cell-enabled="{ row }">
        <span :class="row.enabled ? 'text-emerald-700' : 'text-slate-400'">{{ row.enabled ? '开放' : '关闭' }}</span>
      </template>
      <template #cell-auth_types="{ row }">{{ row.auth_types.map((t: string) => authTypeText(row.platform, t)).join('、') }}</template>
      <template #cell-range="{ row }">{{ row.consignment_min }}–{{ row.consignment_max }}×</template>
      <template #cell-buyback_base="{ row }">{{ Object.entries(row.buyback_base).map(([k, v]) => `${k}: ${v}`).join('，') }}</template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" :disabled="busy" @click="quickToggle(row)">{{ row.enabled ? '关闭' : '开放' }}</button>
        <button class="btn-ghost btn-sm" @click="edit(row)">编辑</button>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog :open="!!editing" :title="`编辑 ${label('platform', editing?.platform).text}`" @update:open="(v: boolean) => !v && (editing = null)">
    <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" /> 开放卖家接入</label>
    <div>
      <div class="label">开放的接入方式</div>
      <div class="flex flex-wrap gap-x-5 gap-y-2">
        <label v-for="t in editing?.supported_auth_types ?? []" :key="t" class="flex items-center gap-2 text-sm">
          <input v-model="form.authTypes" type="checkbox" :value="t" /> {{ authTypeText(editing?.platform, t) }}
        </label>
      </div>
      <p class="hint">卖家只能用开放的方式接入；已接入的上游不受影响。</p>
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="label" for="vmin">寄售最低倍率</label>
        <input id="vmin" v-model="form.min" class="input" inputmode="decimal" />
      </div>
      <div>
        <label class="label" for="vmax">寄售最高倍率</label>
        <input id="vmax" v-model="form.max" class="input" inputmode="decimal" />
      </div>
    </div>
    <div>
      <label class="label" for="vbase">回收基础倍率（按订阅档位，JSON）</label>
      <input id="vbase" v-model="form.base" class="input font-mono" />
      <p class="hint">必须包含 default，例如 {"default": 0.15, "max": 0.2}</p>
    </div>
    <div>
      <label class="label" for="vcon">默认并发</label>
      <input id="vcon" v-model="form.concurrency" class="input" type="number" min="1" />
    </div>
    <div>
      <label class="label" for="vextra">限流参数（写入 sub2api 账号 extra，JSON）</label>
      <input id="vextra" v-model="form.extra" class="input font-mono" />
      <p class="hint">例如 {"max_sessions": 3, "base_rpm": 15}</p>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="editing = null">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>
</template>
