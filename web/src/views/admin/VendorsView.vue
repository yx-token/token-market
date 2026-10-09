<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { admin } from '@/api'
import type { VendorConfig } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { authTypeText, label } from '@/utils/labels'

const toast = useToast()
const vendors = useLoader(() => admin.vendors(), [] as VendorConfig[] | null)
const { busy, run } = useAction()
const editing = ref<VendorConfig | null>(null)
const form = reactive({ enabled: false, authTypes: [] as string[], min: '', max: '', base: '', concurrency: '', extra: '' })

onMounted(vendors.load)

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
  <div>
    <PageHeader title="厂商" description="开放哪些厂商接入、寄售倍率区间、平台回收的基础倍率，以及账号在 sub2api 中的默认并发与限流参数。" />
    <div class="card">
      <DataTable
        :columns="[
          { key: 'platform', label: '厂商' },
          { key: 'enabled', label: '接入' },
          { key: 'auth_types', label: '接入方式' },
          { key: 'range', label: '寄售倍率区间' },
          { key: 'buyback_base', label: '回收基础倍率' },
          { key: 'default_concurrency', label: '默认并发', align: 'right' },
          { key: 'actions', label: '' },
        ]"
        :rows="vendors.data.value"
        :loading="vendors.loading.value"
        row-key="platform"
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
    </div>
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
  </div>
</template>
