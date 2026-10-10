<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { admin, seller } from '@/api'
import type { Proxy } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import MarketListPage from '@/components/MarketListPage.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import ProxyDiagnosticReport from '@/components/ProxyDiagnosticReport.vue'
import { options } from '@/utils/labels'
import { dateTime } from '@/utils/format'

const props = defineProps<{ scope: 'seller' | 'admin' }>()
const isSeller = computed(() => props.scope === 'seller')
const api = isSeller.value ? seller : admin

const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: '',
  owner: '',
})

const proxies = useLoader(() => api.proxies(), [] as Proxy[] | null)
const { busy, run } = useAction()
const show = ref(false)
const reportProxy = ref<Proxy | null>(null)
const reportOpen = ref(false)
const checking = ref<{ id: number; quality: boolean } | null>(null)
const form = reactive({ name: '', protocol: 'socks5', host: '', port: '', username: '', password: '', region: '' })
const valid = computed(() => !!form.name.trim() && !!form.host.trim() && Number.isInteger(Number(form.port)) && Number(form.port) > 0 && Number(form.port) <= 65535)
const columns = computed(() => [
  { key: 'name', label: '代理' },
  ...(!isSeller.value ? [{ key: 'owner', label: '来源' }] : []),
  { key: 'region', label: '出口地区' },
  { key: 'diagnostics', label: '连接 / 质量' },
  { key: 'status', label: '状态 / 绑定' },
  { key: 'actions', label: '操作' },
])
onMounted(proxies.load)

const filtered = computed(() => {
  let rows = proxies.data.value ?? []
  if (queryParams.status) rows = rows.filter((p) => p.status === queryParams.status)
  if (!isSeller.value && queryParams.owner === 'platform') rows = rows.filter((p) => !p.seller_id)
  if (!isSeller.value && queryParams.owner === 'seller') rows = rows.filter((p) => !!p.seller_id)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'name', 'host', 'region', 'seller_id', 'assigned_upstream_id'] as (keyof Proxy)[])
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  proxies.load()
}
function onReset() {
  resetQuery()
  resetPage()
  proxies.load()
}

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

function viewReport(p: Proxy) {
  reportProxy.value = p
  reportOpen.value = true
}

async function check(p: Proxy, quality: boolean) {
  if (busy.value) return
  checking.value = { id: p.id, quality }
  const updated = await run(() => quality ? api.checkProxyQuality(p.id) : api.testProxy(p.id))
  checking.value = null
  if (updated) {
    viewReport(updated)
    await proxies.load()
  }
}
</script>

<template>
  <component
    :is="isSeller ? MarketListPage : ConsoleListPage"
    :id="isSeller ? 'seller-proxies-page' : 'console-proxies-page'"
    :title="isSeller ? '我的代理 IP' : '代理 IP'"
    :description="isSeller ? '先配置代理列表，接入账号时选择一个空闲代理。每个账号独占一个代理，账号移除完成后释放。' : '管理平台库存与卖家提供的代理。由平台关联时仅分配平台的空闲代理，每个账号独占一个。'"
  >
    <template #actions>
      <button type="button" class="btn btn-primary" @click="show = true">添加代理 IP</button>
    </template>

    <template #filters>
      <div :class="isSeller ? 'min-w-0' : 'filter-item'">
        <label for="proxy-keyword" :class="isSeller ? 'label' : 'filter-label'">关键字</label>
        <input id="proxy-keyword" v-model="queryParams.keyword" :class="isSeller ? 'input' : 'filter-input'" placeholder="名称 / IP / 地区" @keyup.enter="search" />
      </div>
      <div :class="isSeller ? 'min-w-0' : 'filter-item'">
        <label for="proxy-status" :class="isSeller ? 'label' : 'filter-label'">状态</label>
        <select id="proxy-status" v-model="queryParams.status" :class="isSeller ? 'input' : 'filter-input'">
          <option value="">全部</option>
          <option v-for="o in options('proxyStatus')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div v-if="!isSeller" class="filter-item">
        <label for="proxy-owner" class="filter-label">来源</label>
        <select id="proxy-owner" v-model="queryParams.owner" class="filter-input">
          <option value="">全部</option>
          <option value="platform">平台</option>
          <option value="seller">卖家</option>
        </select>
      </div>
      <div :class="isSeller ? 'flex items-center justify-end gap-2' : 'filter-actions'">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <p v-if="isSeller" class="mb-5 text-xs leading-relaxed text-ink-500">请使用固定出口 IP。已绑定的代理需先移除对应账号，再更换代理。</p>
    <p v-if="checking" :class="isSeller ? 'callout mb-4' : 'console-notice'" role="status">正在{{ checking.quality ? '检测代理质量，逐项探测 AI 站点，可能需要 1–3 分钟' : '检测连通性和出口地区' }}…</p>

    <DataTable class="proxy-table" :columns="columns" :rows="pageRows" :loading="proxies.loading.value" row-key="id" empty="还没有代理 IP，请先添加">
      <template #cell-name="{ row }">
        <div class="proxy-identity">
          <div class="font-medium">{{ row.name }}</div>
          <div class="mt-1 break-all font-mono text-xs text-ink-600">{{ row.protocol }}://{{ row.host.includes(':') ? `[${row.host}]` : row.host }}:{{ row.port }}</div>
          <div class="mt-1 text-[11px] text-ink-500">#{{ row.id }} · {{ dateTime(row.created_at) }}</div>
        </div>
      </template>
      <template #cell-owner="{ row }">{{ row.seller_id ? `卖家 #${row.seller_id}` : '平台' }}</template>
      <template #cell-region="{ row }">
        <div>{{ row.region || '—' }}</div>
        <div class="font-mono text-xs text-ink-500">{{ row.quality_checked_at && (!row.connection_checked_at || row.quality_checked_at > row.connection_checked_at) ? row.quality_report?.exit_ip : row.connection_report?.ip_address }}</div>
      </template>
      <template #cell-diagnostics="{ row }">
        <div v-if="row.connection_report" :class="row.connection_report.success ? 'text-emerald-700' : 'text-red-600'">{{ row.connection_report.success ? `连接 ${row.connection_report.latency_ms} ms` : '连接失败' }}</div>
        <div v-else class="text-xs text-ink-500">连接未检测</div>
        <div class="mt-1 text-xs">
          <button v-if="row.quality_report" class="link" @click="viewReport(row)">{{ row.quality_report.grade }} · {{ row.quality_report.score }} 分</button>
          <span v-else class="text-ink-500">质量未检测</span>
        </div>
      </template>
      <template #cell-status="{ row }">
        <StatusBadge kind="proxyStatus" :value="row.status" />
        <div class="mt-1.5 text-xs">
          <RouterLink v-if="row.assigned_upstream_id && isSeller" :to="`/seller/upstreams/${row.assigned_upstream_id}`" class="link">账号 #{{ row.assigned_upstream_id }}</RouterLink>
          <span v-else class="text-ink-500">{{ row.assigned_upstream_id ? `账号 #${row.assigned_upstream_id}` : '未绑定账号' }}</span>
        </div>
        <div class="mt-1 text-[11px]" :class="row.sub2api_proxy_id ? 'text-ink-500' : 'text-amber-700'">{{ row.sub2api_proxy_id ? '连接已就绪' : '连接配置待重试' }}</div>
      </template>
      <template #cell-actions="{ row }">
        <div class="grid grid-cols-2 gap-x-1 gap-y-1">
          <button class="btn-ghost btn-sm" :disabled="busy || !row.sub2api_proxy_id" @click="check(row, false)">检测连接</button>
          <button class="btn-ghost btn-sm" :disabled="busy || !row.sub2api_proxy_id" @click="check(row, true)">质量检测</button>
          <button v-if="row.connection_report || row.quality_report" class="btn-ghost btn-sm" @click="viewReport(row)">查看报告</button>
          <button v-if="!row.sub2api_proxy_id" class="btn-ghost btn-sm" :disabled="busy" @click="act(() => api.syncProxy(row.id), '连接配置已就绪')">重试连接</button>
          <button v-if="row.status !== 'disabled'" class="btn-ghost btn-sm" :disabled="busy || !!row.assigned_upstream_id" @click="act(() => api.setProxyDisabled(row.id, true), '已停用')">停用</button>
          <button v-else class="btn-ghost btn-sm" :disabled="busy" @click="act(() => api.setProxyDisabled(row.id, false), '已启用')">启用</button>
        </div>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-ink-500">共 {{ total }} 条</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="font-mono text-xs text-ink-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </component>

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
  <ModalDialog v-model:open="reportOpen" :title="`代理检测报告 · ${reportProxy?.name ?? ''}`" wide>
    <ProxyDiagnosticReport v-if="reportProxy" :proxy="reportProxy" />
    <template #footer><button class="btn-secondary" @click="reportOpen = false">关闭</button></template>
  </ModalDialog>
</template>

<style scoped>
.proxy-identity {
  min-width: 11rem;
  max-width: 18rem;
  white-space: normal;
}
.proxy-table :deep(td) {
  vertical-align: middle;
}
.console-notice {
  margin: 1rem 1.25rem;
  font-size: 0.8125rem;
}
</style>
