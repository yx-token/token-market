<script setup lang="ts">
/**
 * 卖家代理：TablePageLayout + filter-grid + DataTable；卖家显示邮箱，字段分列。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { sellerAccount, sellerFilterLabel } from './sellerDisplay'

defineOptions({ name: 'MarketAdminProxies' })

interface ProxyRow {
  id: number
  seller_id?: number | null
  seller_email?: string
  name: string
  protocol: string
  host: string
  port: number
  username?: string
  region?: string
  sub2api_proxy_id?: number | null
  status: string
  assigned_upstream_id?: number | null
  assigned_upstream_ids: number[]
  assigned_upstream_count: number
  created_at?: string
  connection_report?: { success?: boolean; latency_ms?: number; ip_address?: string }
  quality_report?: { score?: number; grade?: string; exit_ip?: string }
  connection_checked_at?: string
  quality_checked_at?: string
}

function assignedUpstreamIDs(proxy: ProxyRow): number[] {
  if (proxy.assigned_upstream_ids != null) return proxy.assigned_upstream_ids
  if (proxy.assigned_upstream_count != null) return []
  return proxy.assigned_upstream_id ? [proxy.assigned_upstream_id] : []
}

function assignedUpstreamCount(proxy: ProxyRow): number {
  return Math.max(proxy.assigned_upstream_count ?? 0, assignedUpstreamIDs(proxy).length)
}

const STATUS_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'ready', label: '就绪' },
  { value: 'disabled', label: '停用' },
  { value: 'error', label: '异常' },
  { value: 'pending', label: '待同步' },
]

const OWNER_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'platform', label: '平台' },
  { value: 'seller', label: '卖家' },
]

const BIND_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'bound', label: '已绑定上游' },
  { value: 'unbound', label: '未绑定' },
]

const PROTOCOL_OPTIONS = [
  { value: '', label: '全部' },
  { value: 'socks5', label: 'socks5' },
  { value: 'socks5h', label: 'socks5h' },
  { value: 'http', label: 'http' },
  { value: 'https', label: 'https' },
]

const STATUS_LABEL: Record<string, string> = {
  ready: '就绪',
  disabled: '停用',
  error: '异常',
  pending: '待同步',
}

const initQueryParams = {
  pageNum: 1,
  pageSize: 15,
  keyword: '',
  status: '',
  owner: '',
  seller_id: '',
  bind: '',
  protocol: '',
  region: '',
}

const data = reactive({
  queryParams: { ...initQueryParams },
})
const { queryParams } = toRefs(data)

const rows = ref<ProxyRow[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const checking = ref<{ id: number; quality: boolean } | null>(null)

const show = ref(false)
const form = reactive({
  name: '',
  protocol: 'socks5',
  host: '',
  port: '',
  username: '',
  password: '',
  region: '',
})

const columns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'name', label: '名称', class: 'col-name' },
  { key: 'endpoint', label: '地址', class: 'col-endpoint' },
  { key: 'owner', label: '来源/卖家', class: 'col-owner' },
  { key: 'region', label: '地区', class: 'col-region' },
  { key: 'connection', label: '连接', class: 'col-conn' },
  { key: 'quality', label: '质量', class: 'col-quality' },
  { key: 'status', label: '状态', class: 'col-status' },
  { key: 'bind', label: '绑定', class: 'col-bind' },
  { key: 'created_at', label: '创建时间', class: 'col-time' },
  { key: 'actions', label: '操作', class: 'col-ops' },
]

const valid = computed(
  () =>
    !!form.name.trim() &&
    !!form.host.trim() &&
    Number.isInteger(Number(form.port)) &&
    Number(form.port) > 0 &&
    Number(form.port) <= 65535,
)

const sellerFilterOptions = computed(() => {
  const map = new Map<string, string>()
  for (const r of rows.value) {
    const sid = String(r.seller_id || '').trim()
    if (!sid || map.has(sid)) continue
    map.set(sid, sellerFilterLabel(r.seller_email, r.seller_id))
  }
  return [
    { value: '', label: '全部' },
    ...[...map.entries()]
      .sort((a, b) => Number(a[0]) - Number(b[0]))
      .map(([value, label]) => ({ value, label })),
  ]
})

const regionFilterOptions = computed(() => {
  const set = new Set<string>()
  for (const r of rows.value) {
    const region = String(r.region || '').trim()
    if (region) set.add(region)
  }
  return [
    { value: '', label: '全部' },
    ...[...set].sort().map((value) => ({ value, label: value })),
  ]
})

const filtered = computed(() => {
  let list = rows.value
  if (queryParams.value.status) list = list.filter((p) => p.status === queryParams.value.status)
  if (queryParams.value.owner === 'platform') list = list.filter((p) => p.seller_id == null)
  if (queryParams.value.owner === 'seller') list = list.filter((p) => p.seller_id != null)
  if (queryParams.value.seller_id) {
    list = list.filter((p) => String(p.seller_id || '') === queryParams.value.seller_id)
  }
  if (queryParams.value.bind === 'bound') list = list.filter((p) => assignedUpstreamCount(p) > 0)
  if (queryParams.value.bind === 'unbound') list = list.filter((p) => assignedUpstreamCount(p) === 0)
  if (queryParams.value.protocol) list = list.filter((p) => p.protocol === queryParams.value.protocol)
  if (queryParams.value.region) {
    list = list.filter((p) => String(p.region || '').trim() === queryParams.value.region)
  }
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    list = list.filter((p) =>
      [p.id, p.name, p.host, p.region, p.seller_id, p.seller_email, p.protocol, ...assignedUpstreamIDs(p)]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageRows = computed(() => {
  const start = (queryParams.value.pageNum - 1) * queryParams.value.pageSize
  return filtered.value.slice(start, start + queryParams.value.pageSize)
})

const allStats = computed(() => {
  const all = rows.value
  return {
    total: all.length,
    ready: all.filter((p) => p.status === 'ready').length,
    seller: all.filter((p) => p.seller_id != null).length,
    unbound: all.filter((p) => assignedUpstreamCount(p) === 0).length,
    accounts: all.reduce((sum, p) => sum + assignedUpstreamCount(p), 0),
  }
})

watch(
  () => [
    queryParams.value.keyword,
    queryParams.value.status,
    queryParams.value.owner,
    queryParams.value.seller_id,
    queryParams.value.bind,
    queryParams.value.protocol,
    queryParams.value.region,
    queryParams.value.pageSize,
  ],
  () => {
    queryParams.value.pageNum = 1
  },
)

function endpoint(p: ProxyRow) {
  const host = p.host.includes(':') ? `[${p.host}]` : p.host
  return `${p.protocol}://${host}:${p.port}`
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function statusLabel(s: string) {
  return STATUS_LABEL[s] || s || '—'
}

function statusTone(s: string) {
  if (s === 'ready') return 'ok'
  if (s === 'error') return 'err'
  if (s === 'disabled') return 'idle'
  return 'progress'
}

function ownerLabel(p: ProxyRow) {
  if (p.seller_id == null) return '平台'
  return sellerAccount(p.seller_email, p.seller_id)
}

function exitIp(p: ProxyRow) {
  if (
    p.quality_checked_at &&
    (!p.connection_checked_at || p.quality_checked_at > p.connection_checked_at)
  ) {
    return p.quality_report?.exit_ip || ''
  }
  return p.connection_report?.ip_address || ''
}

function connectionText(p: ProxyRow) {
  if (!p.connection_report) return '未检测'
  if (p.connection_report.success) return `${p.connection_report.latency_ms ?? '—'} ms`
  return '失败'
}

function qualityText(p: ProxyRow) {
  if (!p.quality_report) return '未检测'
  const grade = p.quality_report.grade || '—'
  const score = p.quality_report.score
  return score == null ? grade : `${grade} · ${score} 分`
}

function getSerialNumber(index: number) {
  const n = (queryParams.value.pageNum - 1) * queryParams.value.pageSize + index + 1
  return String(n).padStart(2, '0')
}

function handleQuery() {
  queryParams.value.pageNum = 1
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = (await marketGet<ProxyRow[] | null>(`${MARKET_API_PREFIX}/proxies`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openCreate() {
  Object.assign(form, {
    name: '',
    protocol: 'socks5',
    host: '',
    port: '',
    username: '',
    password: '',
    region: '',
  })
  show.value = true
}

function closeModal() {
  show.value = false
}

async function save() {
  if (!valid.value) return
  const body = {
    name: form.name.trim(),
    protocol: form.protocol,
    host: form.host.trim(),
    port: Number(form.port),
    username: form.username.trim() || undefined,
    password: form.password || undefined,
    region: form.region.trim() || undefined,
  }
  if (await run('代理已添加', () => marketSend('POST', `${MARKET_API_PREFIX}/proxies`, body))) {
    closeModal()
  }
}

function toggle(p: ProxyRow) {
  const off = p.status === 'disabled'
  if (!off && assignedUpstreamCount(p)) return
  return run(`代理 ${p.name} 已${off ? '启用' : '停用'}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/proxies/${p.id}/${off ? 'enable' : 'disable'}`),
  )
}

function sync(p: ProxyRow) {
  return run(`代理 ${p.name} 连接配置已就绪`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/proxies/${p.id}/sync`),
  )
}

async function check(p: ProxyRow, quality: boolean) {
  if (busy.value) return
  checking.value = { id: p.id, quality }
  await run(
    quality ? `代理 ${p.name} 质量检测完成` : `代理 ${p.name} 连接检测完成`,
    () =>
      marketSend(
        'POST',
        `${MARKET_API_PREFIX}/proxies/${p.id}/${quality ? 'quality-check' : 'test'}`,
      ),
  )
  checking.value = null
}

onMounted(load)
</script>

<template>
  <section id="market-proxies-page" class="proxies-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">卖家代理</h2>
          <p class="y-page-desc">卖家自有代理可绑定多个账号，平台代理按账号独占；全部绑定账号移除完成后释放</p>
        </div>
      </template>

      <template #actions>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy" @click="openCreate">
          <Icon name="plus" size="sm" />
          添加代理
        </button>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>

      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <p v-else-if="msg" class="y-msg y-msg--ok mb-2">{{ msg }}</p>
        <p v-if="checking" class="y-msg mb-2">
          正在{{ checking.quality ? '检测代理质量（可能 1–3 分钟）' : '检测连通性和出口地区' }}…
        </p>

        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">代理总数</div>
                <div class="stat-card__value">{{ allStats.total.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--green">
              <div>
                <div class="stat-card__label">就绪</div>
                <div class="stat-card__value">{{ allStats.ready.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--cyan">
              <div>
                <div class="stat-card__label">卖家代理</div>
                <div class="stat-card__value">{{ allStats.seller.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--orange">
              <div>
                <div class="stat-card__label">未绑定</div>
                <div class="stat-card__value">{{ allStats.unbound.toLocaleString() }}</div>
              </div>
            </article>
            <article class="stat-card stat-card--blue">
              <div>
                <div class="stat-card__label">绑定账号总数</div>
                <div class="stat-card__value">{{ allStats.accounts.toLocaleString() }}</div>
              </div>
            </article>
          </div>
        </div>

        <div class="section-block mt-3">
          <div class="section-title">代理信息</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="名称 / IP / 卖家邮箱"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">状态</label>
              <Select
                v-model="queryParams.status"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="STATUS_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">来源</label>
              <Select
                v-model="queryParams.owner"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="OWNER_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">卖家</label>
              <Select
                v-model="queryParams.seller_id"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="sellerFilterOptions"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">绑定</label>
              <Select
                v-model="queryParams.bind"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="BIND_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">协议</label>
              <Select
                v-model="queryParams.protocol"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="PROTOCOL_OPTIONS"
              />
            </div>
            <div class="filter-item">
              <label class="filter-label">地区</label>
              <Select
                v-model="queryParams.region"
                class="flex-1 w-full min-w-0"
                size="sm"
                :options="regionFilterOptions"
              />
            </div>
            <div class="filter-actions lg:col-span-4 sm:col-span-2">
              <button type="button" class="btn btn-secondary" @click="resetQuery">
                <Icon name="refresh" size="sm" />
                重置
              </button>
              <button type="button" class="btn btn-primary" @click="handleQuery">
                <Icon name="search" size="sm" />
                搜索
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="pageRows"
          :loading="loading"
          :expandable-actions="false"
        >
          <template #cell-serial="{ row }">
            {{ getSerialNumber(pageRows.findIndex((r) => r.id === row.id)) }}
          </template>
          <template #cell-name="{ row }">{{ row.name || '—' }}</template>
          <template #cell-endpoint="{ row }">
            <span class="mono-cell" :title="endpoint(row)">{{ endpoint(row) }}</span>
          </template>
          <template #cell-owner="{ row }">{{ ownerLabel(row) }}</template>
          <template #cell-region="{ row }">
            <div>{{ row.region || '—' }}</div>
            <div v-if="exitIp(row)" class="row-sub mono-cell">{{ exitIp(row) }}</div>
          </template>
          <template #cell-connection="{ row }">{{ connectionText(row) }}</template>
          <template #cell-quality="{ row }">{{ qualityText(row) }}</template>
          <template #cell-status="{ row }">
            <span class="status-cell">
              <i class="status-dot" :data-tone="statusTone(row.status)" />
              {{ statusLabel(row.status) }}
            </span>
            <div class="row-sub" :class="{ warn: !row.sub2api_proxy_id }">
              {{ row.sub2api_proxy_id ? '连接已就绪' : '连接配置待重试' }}
            </div>
          </template>
          <template #cell-bind="{ row }">
            <div>{{ assignedUpstreamCount(row) ? `已绑定 ${assignedUpstreamCount(row)} 个账号` : '未绑定' }}</div>
            <div v-for="upstreamID in assignedUpstreamIDs(row)" :key="upstreamID" class="row-sub">账号 #{{ upstreamID }}</div>
          </template>
          <template #cell-created_at="{ row }">{{ fmtTime(row.created_at) }}</template>
          <template #cell-actions="{ row }">
            <div class="table-ops">
              <button
                type="button"
                class="y-link"
                :disabled="busy || !row.sub2api_proxy_id"
                @click="check(row, false)"
              >
                检测连接
              </button>
              <button
                type="button"
                class="y-link"
                :disabled="busy || !row.sub2api_proxy_id"
                @click="check(row, true)"
              >
                质量检测
              </button>
              <button
                v-if="!row.sub2api_proxy_id"
                type="button"
                class="y-link"
                :disabled="busy"
                @click="sync(row)"
              >
                重试连接
              </button>
              <button
                type="button"
                class="y-link"
                :disabled="busy || (assignedUpstreamCount(row) > 0 && row.status !== 'disabled')"
                @click="toggle(row)"
              >
                {{ row.status === 'disabled' ? '启用' : '停用' }}
              </button>
            </div>
          </template>
          <template #empty>
            <EmptyState
              title="暂无代理"
              description="点右上角「添加代理」录入平台库存，或调整筛选条件"
              action-text="添加代理"
              @action="openCreate"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="queryParams.pageNum"
          :page-size="queryParams.pageSize"
          :page-size-options="[15, 20, 50]"
          show-jump
          @update:page="queryParams.pageNum = $event"
          @update:page-size="queryParams.pageSize = $event"
        />
      </template>
    </TablePageLayout>

    <div v-if="show" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" aria-label="添加代理">
        <header class="y-modal__head">
          <h3>添加代理 IP</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>名称</span>
            <input v-model="form.name" class="input w-full" placeholder="例如 美国固定 IP 01" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>协议</span>
              <select v-model="form.protocol" class="input w-full">
                <option v-for="p in ['socks5', 'socks5h', 'http', 'https']" :key="p" :value="p">{{ p }}</option>
              </select>
            </label>
            <label class="y-field">
              <span>地区（可选）</span>
              <input v-model="form.region" class="input w-full" placeholder="us / jp …" />
            </label>
          </div>
          <div class="y-field-row">
            <label class="y-field">
              <span>IP 或域名</span>
              <input v-model="form.host" class="input w-full font-mono" placeholder="203.0.113.10" />
            </label>
            <label class="y-field">
              <span>端口</span>
              <input v-model="form.port" class="input w-full" type="number" min="1" max="65535" placeholder="1080" />
            </label>
          </div>
          <div class="y-field-row">
            <label class="y-field">
              <span>用户名（可选）</span>
              <input v-model="form.username" class="input w-full" autocomplete="off" />
            </label>
            <label class="y-field">
              <span>密码（可选）</span>
              <input v-model="form.password" class="input w-full" type="password" autocomplete="new-password" />
            </label>
          </div>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="btn btn-secondary" @click="closeModal">取消</button>
          <button type="button" class="btn btn-primary" :disabled="busy || !valid" @click="save">添加</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
#market-proxies-page.proxies-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.page-heading {
  min-width: 0;
}

.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.section-block {
  min-width: 0;
}

.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  color: var(--color-text-primary, #262626);
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}

.stat-card {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}

.stat-card--blue {
  background: linear-gradient(135deg, #eff6ff, #f8fafc);
}
.stat-card--green {
  background: linear-gradient(135deg, #ecfdf5, #f8fafc);
}
.stat-card--cyan {
  background: linear-gradient(135deg, #ecfeff, #f8fafc);
}
.stat-card--orange {
  background: linear-gradient(135deg, #fff7ed, #f8fafc);
}

.stat-card__label {
  font-size: 12px;
  color: #64748b;
}

.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.2;
}

.table-ops {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
  min-width: max-content;
}

.row-sub {
  margin-top: 2px;
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: #8c8c8c;
}

.row-sub.warn {
  color: var(--color-warning, #faad14);
}

.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #94a3b8;
}
.status-dot[data-tone='ok'] {
  background: #16a34a;
}
.status-dot[data-tone='err'] {
  background: #ef4444;
}
.status-dot[data-tone='progress'] {
  background: #0555ce;
}
.status-dot[data-tone='idle'] {
  background: #94a3b8;
}

#market-proxies-page :deep(.filter-grid .select-trigger),
#market-proxies-page :deep(.filter-grid .select-trigger-sm),
#market-proxies-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}

#market-proxies-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
  border-bottom: 1px solid #ebeef5;
}

#market-proxies-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #303133;
}

#market-proxies-page :deep(.table-scroll-container table tbody tr:hover td) {
  background: #f5f7fa;
}

#market-proxies-page :deep(th.col-name),
#market-proxies-page :deep(td.col-name) {
  min-width: 7rem;
}
#market-proxies-page :deep(th.col-endpoint),
#market-proxies-page :deep(td.col-endpoint) {
  min-width: 12rem;
  max-width: 16rem;
}
#market-proxies-page :deep(th.col-owner),
#market-proxies-page :deep(td.col-owner) {
  min-width: 10rem;
}
#market-proxies-page :deep(th.col-region),
#market-proxies-page :deep(td.col-region),
#market-proxies-page :deep(th.col-conn),
#market-proxies-page :deep(td.col-conn),
#market-proxies-page :deep(th.col-quality),
#market-proxies-page :deep(td.col-quality),
#market-proxies-page :deep(th.col-status),
#market-proxies-page :deep(td.col-status),
#market-proxies-page :deep(th.col-bind),
#market-proxies-page :deep(td.col-bind) {
  min-width: 5.5rem;
}
#market-proxies-page :deep(th.col-time),
#market-proxies-page :deep(td.col-time) {
  min-width: 9rem;
}
#market-proxies-page :deep(th.col-ops),
#market-proxies-page :deep(td.col-ops) {
  min-width: 18rem;
  width: 18rem;
  box-sizing: border-box;
}
#market-proxies-page :deep(thead th.col-ops) {
  background: #f5f7fa !important;
}
#market-proxies-page :deep(tbody td.col-ops) {
  background: #fff !important;
}
#market-proxies-page :deep(tbody tr:hover td.col-ops) {
  background: #f5f7fa !important;
}

.y-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}
.y-modal {
  width: min(520px, 100%);
  max-height: min(90vh, 720px);
  overflow: auto;
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head,
.y-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__foot {
  justify-content: flex-end;
}
.y-modal__body {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

@media (max-width: 1100px) {
  .stats-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .stats-row,
  .y-field-row {
    grid-template-columns: 1fr;
  }
}
</style>
