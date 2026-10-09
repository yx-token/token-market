<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin, seller } from '@/api'
import type { Proxy } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime } from '@/utils/format'

const props = defineProps<{ scope: 'seller' | 'admin' }>()
const isSeller = computed(() => props.scope === 'seller')
const api = isSeller.value ? seller : admin
const proxies = useLoader(() => api.proxies(), [] as Proxy[] | null)
const { busy, run } = useAction()
const show = ref(false)
const form = reactive({ name: '', protocol: 'socks5', host: '', port: '', username: '', password: '', region: '' })
const valid = computed(() => !!form.name.trim() && !!form.host.trim() && Number.isInteger(Number(form.port)) && Number(form.port) > 0 && Number(form.port) <= 65535)
const columns = computed(() => [
  { key: 'id', label: 'ID' },
  { key: 'name', label: '名称' },
  ...(!isSeller.value ? [{ key: 'owner', label: '来源' }] : []),
  { key: 'addr', label: '代理地址' },
  { key: 'region', label: '地区' },
  { key: 'status', label: '状态' },
  { key: 'assigned_upstream_id', label: '绑定账号' },
  { key: 'ready', label: '连接配置' },
  { key: 'created_at', label: '添加时间' },
  { key: 'actions', label: '' },
])
onMounted(proxies.load)

async function create() {
  if (!valid.value) return
  const saved = await run(() => api.createProxy({ ...form, port: Number(form.port) }), '代理 IP 已添加')
  if (saved) {
    show.value = false
    Object.assign(form, { name: '', host: '', port: '', username: '', password: '', region: '' })
  }
  // 同步失败时代理仍已保存，可在列表重试。
  await proxies.load()
}

async function act(fn: () => Promise<unknown>, msg: string) {
  if (await run(fn, msg)) await proxies.load()
}
</script>

<template>
  <div>
    <PageHeader
      :title="isSeller ? '我的代理 IP' : '代理 IP'"
      :description="isSeller ? '先配置代理列表，接入账号时选择一个空闲代理。每个账号独占一个代理，账号移除完成后释放。' : '管理平台库存与卖家提供的代理。由平台关联时仅分配平台的空闲代理，每个账号独占一个。'"
    >
      <button class="btn-primary" @click="show = true">添加代理 IP</button>
    </PageHeader>
    <p v-if="isSeller" class="mb-4 text-sm text-ink-600">请使用固定出口 IP 的代理。已绑定的代理不能停用；需要更换时，先移除对应账号，再用新的代理重新接入。</p>
    <div class="card">
      <DataTable :columns="columns" :rows="proxies.data.value" :loading="proxies.loading.value" row-key="id" empty="还没有代理 IP，请先添加">
        <template #cell-owner="{ row }">{{ row.seller_id ? `卖家 #${row.seller_id}` : '平台' }}</template>
        <template #cell-addr="{ row }"><span class="font-mono text-xs">{{ row.protocol }}://{{ row.host.includes(':') ? `[${row.host}]` : row.host }}:{{ row.port }}</span></template>
        <template #cell-status="{ row }"><StatusBadge kind="proxyStatus" :value="row.status" /></template>
        <template #cell-assigned_upstream_id="{ row }">
          <RouterLink v-if="row.assigned_upstream_id && isSeller" :to="`/seller/upstreams/${row.assigned_upstream_id}`" class="link">账号 #{{ row.assigned_upstream_id }}</RouterLink>
          <span v-else>{{ row.assigned_upstream_id ? `账号 #${row.assigned_upstream_id}` : '—' }}</span>
        </template>
        <template #cell-ready="{ row }">{{ row.sub2api_proxy_id ? '已就绪' : '待重试' }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <button v-if="!row.sub2api_proxy_id" class="btn-ghost btn-sm" :disabled="busy" @click="act(() => api.syncProxy(row.id), '连接配置已就绪')">重试连接</button>
          <button v-if="row.status !== 'disabled'" class="btn-ghost btn-sm" :disabled="busy || !!row.assigned_upstream_id" @click="act(() => api.setProxyDisabled(row.id, true), '已停用')">停用</button>
          <button v-else class="btn-ghost btn-sm" :disabled="busy" @click="act(() => api.setProxyDisabled(row.id, false), '已启用')">启用</button>
        </template>
      </DataTable>
    </div>
    <ModalDialog v-model:open="show" title="添加代理 IP">
      <form id="proxy-form" class="grid grid-cols-1 gap-3 sm:grid-cols-2" @submit.prevent="create">
        <div class="sm:col-span-2">
          <label class="label" for="proxy-name">名称</label>
          <input id="proxy-name" v-model="form.name" class="input" required placeholder="例如 美国固定 IP 01" />
        </div>
        <div>
          <label class="label" for="proxy-protocol">协议</label>
          <select id="proxy-protocol" v-model="form.protocol" class="input">
            <option v-for="p in ['socks5', 'socks5h', 'http', 'https']" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
        <div>
          <label class="label" for="proxy-region">地区（可选）</label>
          <input id="proxy-region" v-model="form.region" class="input" placeholder="us / jp …" />
        </div>
        <div>
          <label class="label" for="proxy-host">IP 或域名</label>
          <input id="proxy-host" v-model="form.host" class="input font-mono" required placeholder="203.0.113.10" autocomplete="off" />
          <p class="hint">只填写主机，不包含协议和端口</p>
        </div>
        <div>
          <label class="label" for="proxy-port">端口</label>
          <input id="proxy-port" v-model="form.port" class="input" type="number" min="1" max="65535" step="1" required placeholder="1080" />
        </div>
        <div>
          <label class="label" for="proxy-user">用户名（可选）</label>
          <input id="proxy-user" v-model="form.username" class="input" autocomplete="off" />
        </div>
        <div>
          <label class="label" for="proxy-password">密码（可选）</label>
          <input id="proxy-password" v-model="form.password" class="input" type="password" autocomplete="new-password" />
        </div>
      </form>
      <template #footer>
        <button class="btn-secondary" :disabled="busy" @click="show = false">取消</button>
        <button type="submit" form="proxy-form" class="btn-primary" :disabled="busy || !valid">添加</button>
      </template>
    </ModalDialog>
  </div>
</template>
