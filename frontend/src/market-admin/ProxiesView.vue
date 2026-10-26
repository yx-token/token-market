<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface ProxyRow {
  id: number
  seller_id?: number | null
  name: string
  protocol: string
  host: string
  port: number
  username?: string
  region?: string
  sub2api_proxy_id?: number | null
  status: string
  assigned_upstream_id?: number | null
  created_at?: string
  connection_report?: { success?: boolean; latency_ms?: number; ip_address?: string }
  quality_report?: { score?: number; grade?: string; exit_ip?: string }
  connection_checked_at?: string
  quality_checked_at?: string
}

const STATUS_OPTIONS = [
  { value: 'ready', text: '就绪' },
  { value: 'disabled', text: '停用' },
  { value: 'error', text: '异常' },
  { value: 'pending', text: '待同步' },
]

const rows = ref<ProxyRow[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterStatus = ref('')
const filterOwner = ref('')
const pageNum = ref(1)
const pageSize = 15
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

const valid = computed(
  () =>
    !!form.name.trim() &&
    !!form.host.trim() &&
    Number.isInteger(Number(form.port)) &&
    Number(form.port) > 0 &&
    Number(form.port) <= 65535,
)

const filtered = computed(() => {
  let list = rows.value
  if (filterStatus.value) list = list.filter((p) => p.status === filterStatus.value)
  if (filterOwner.value === 'platform') list = list.filter((p) => !p.seller_id)
  if (filterOwner.value === 'seller') list = list.filter((p) => !!p.seller_id)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    list = list.filter((p) =>
      [p.id, p.name, p.host, p.region, p.seller_id, p.assigned_upstream_id]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageRows = computed(() => {
  const start = (pageNum.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function endpoint(p: ProxyRow) {
  const host = p.host.includes(':') ? `[${p.host}]` : p.host
  return `${p.protocol}://${host}:${p.port}`
}

function fmtTime(v?: string) {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
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
  const ok = await run(
    quality ? `代理 ${p.name} 质量检测完成` : `代理 ${p.name} 连接检测完成`,
    () =>
      marketSend(
        'POST',
        `${MARKET_API_PREFIX}/proxies/${p.id}/${quality ? 'quality-check' : 'test'}`,
      ),
  )
  checking.value = null
  if (!ok) return
}

function resetFilters() {
  keyword.value = ''
  filterStatus.value = ''
  filterOwner.value = ''
  pageNum.value = 1
}

onMounted(load)
</script>

<template>
  <section id="market-proxies-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">代理 IP</h2>
        <p class="y-page-desc">管理平台库存与卖家代理；账号独占绑定，移除后释放</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn" :disabled="busy" @click="openCreate">添加代理</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="checking" class="y-msg">
        正在{{ checking.quality ? '检测代理质量（可能 1–3 分钟）' : '检测连通性和出口地区' }}…
      </p>

      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input
              class="filter-input w-full"
              v-model="keyword"
              type="search"
              placeholder="名称 / IP / 地区"
              @keyup.enter="pageNum = 1"
            />
          </div>
        </div>
        <div class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="filterStatus" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">状态：全部</option>
            <option v-for="o in STATUS_OPTIONS" :key="o.value" :value="o.value">{{ o.text }}</option>
          </select>
        </div>
        <div class="filter-item">
          <label class="filter-label">来源</label>
          <select v-model="filterOwner" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">来源：全部</option>
            <option value="platform">平台</option>
            <option value="seller">卖家</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button type="button" class="btn btn-secondary" @click="resetFilters">重置</button>
          <button type="button" class="btn btn-primary" @click="pageNum = 1">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!pageRows.length" class="y-msg">还没有代理 IP，请先添加</p>
      <table v-else>
        <thead>
          <tr>
            <th>代理</th>
            <th>来源</th>
            <th>地区</th>
            <th>连接 / 质量</th>
            <th>状态</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in pageRows" :key="row.id">
            <td>
              <div>{{ row.name }}</div>
              <div class="row-mono">{{ endpoint(row) }}</div>
              <div class="row-sub">#{{ row.id }} · {{ fmtTime(row.created_at) }}</div>
            </td>
            <td>{{ row.seller_id ? `卖家 #${row.seller_id}` : '平台' }}</td>
            <td>
              <div>{{ row.region || '—' }}</div>
              <div class="row-mono">
                {{
                  row.quality_checked_at &&
                  (!row.connection_checked_at || row.quality_checked_at > row.connection_checked_at)
                    ? row.quality_report?.exit_ip
                    : row.connection_report?.ip_address
                }}
              </div>
            </td>
            <td>
              <div v-if="row.connection_report">
                {{
                  row.connection_report.success
                    ? `连接 ${row.connection_report.latency_ms ?? '—'} ms`
                    : '连接失败'
                }}
              </div>
              <div v-else class="row-sub">连接未检测</div>
              <div class="row-sub">
                <template v-if="row.quality_report">
                  {{ row.quality_report.grade }} · {{ row.quality_report.score }} 分
                </template>
                <template v-else>质量未检测</template>
              </div>
            </td>
            <td>
              <span :class="row.status === 'disabled' ? 'y-chip' : 'y-chip y-chip--ok'">{{ row.status }}</span>
              <div class="row-sub">
                {{ row.assigned_upstream_id ? `账号 #${row.assigned_upstream_id}` : '未绑定账号' }}
              </div>
              <div class="row-sub" :class="{ warn: !row.sub2api_proxy_id }">
                {{ row.sub2api_proxy_id ? '连接已就绪' : '连接配置待重试' }}
              </div>
            </td>
            <td class="y-col-op">
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
                :disabled="busy || (!!row.assigned_upstream_id && row.status !== 'disabled')"
                @click="toggle(row)"
              >
                {{ row.status === 'disabled' ? '启用' : '停用' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer v-if="total > 0" class="y-footer">
      <div class="y-pager">
        <span>共 {{ total }} 条</span>
        <button type="button" :disabled="pageNum <= 1" @click="pageNum -= 1">上一页</button>
        <button type="button" class="is-on">{{ pageNum }} / {{ pageCount }}</button>
        <button type="button" :disabled="pageNum >= pageCount" @click="pageNum += 1">下一页</button>
      </div>
    </footer>

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
          <button type="button" class="y-btn y-btn--ghost" @click="closeModal">取消</button>
          <button type="button" class="y-btn" :disabled="busy || !valid" @click="save">添加</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.row-sub {
  margin-top: 2px;
  font-size: 12px;
  opacity: 0.65;
}
.row-sub.warn {
  color: var(--color-warning, #faad14);
  opacity: 1;
}
.row-mono {
  margin-top: 2px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
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
@media (max-width: 720px) {
  .y-field-row {
    grid-template-columns: 1fr;
  }
}
</style>
