<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

const route = useRoute()

interface Upstream {
  id: number
  seller_id: number
  platform: string
  auth_type?: string
  account_email?: string
  status: string
  health?: string
  health_reason?: string
  failure_reason?: string
  grade?: string
  privacy_ok?: boolean | null
  is_reference?: boolean
  created_at?: string
}

const PLATFORM_OPTIONS = [
  { value: 'anthropic', text: 'Anthropic' },
  { value: 'openai', text: 'OpenAI' },
  { value: 'gemini', text: 'Gemini' },
  { value: 'antigravity', text: 'Antigravity' },
  { value: 'grok', text: 'Grok' },
  { value: 'kimi', text: 'Kimi' },
  { value: 'zhipu', text: '智谱' },
  { value: 'minimax', text: 'MiniMax' },
  { value: 'opencode_go', text: 'OpenCode Go' },
]

const STATUS_OPTIONS = [
  { value: 'ready', text: '就绪' },
  { value: 'suspended', text: '已冻结' },
  { value: 'verify_failed', text: '检测失败' },
  { value: 'draft', text: '草稿' },
  { value: 'authorizing', text: '授权中' },
  { value: 'removing', text: '移除中' },
]

const rows = ref<Upstream[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterStatus = ref('')
const filterPlatform = ref('')
const pageNum = ref(1)
const pageSize = 15

const filtered = computed(() => {
  let list = rows.value
  if (filterStatus.value) list = list.filter((u) => u.status === filterStatus.value)
  if (filterPlatform.value) list = list.filter((u) => u.platform === filterPlatform.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    list = list.filter((u) =>
      [u.id, u.seller_id, u.account_email, u.platform, u.failure_reason, u.health_reason]
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

function platformLabel(v: string) {
  return PLATFORM_OPTIONS.find((o) => o.value === v)?.text || v
}

function statusChip(status: string) {
  if (status === 'ready') return 'y-chip y-chip--ok'
  if (status === 'suspended' || status === 'verify_failed') return 'y-chip y-chip--err'
  if (['draft', 'authorizing', 'removing'].includes(status)) return 'y-chip y-chip--warn'
  return 'y-chip'
}

function displayName(u: Upstream) {
  return u.account_email || `上游 #${u.id}`
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
    rows.value = (await marketGet<Upstream[] | null>(`${MARKET_API_PREFIX}/upstreams`)) ?? []
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
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function probe(u: Upstream) {
  return run(`上游 #${u.id} 已排队检测`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/probe`),
  )
}

function suspend(u: Upstream) {
  const reason = window.prompt('冻结原因')
  if (reason === null) return
  return run(`上游 #${u.id} 已冻结`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/suspend`, { reason }),
  )
}

function unsuspend(u: Upstream) {
  return run(`上游 #${u.id} 已解冻`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/upstreams/${u.id}/unsuspend`),
  )
}

function remove(u: Upstream) {
  if (!window.confirm(`强制删除上游 #${u.id}？`)) return
  return run(`上游 #${u.id} 已删除`, () =>
    marketSend('DELETE', `${MARKET_API_PREFIX}/upstreams/${u.id}`),
  )
}

function resetFilters() {
  keyword.value = ''
  filterStatus.value = ''
  filterPlatform.value = ''
  pageNum.value = 1
}

const focusId = computed(() => {
  const raw = route.query.focus ?? route.query.q
  const n = Number(Array.isArray(raw) ? raw[0] : raw)
  return Number.isFinite(n) && n > 0 ? n : 0
})

function applyRouteQuery() {
  const q = route.query.q
  if (typeof q === 'string' && q.trim()) {
    keyword.value = q.trim()
    pageNum.value = 1
  }
}

watch(
  () => route.query,
  () => applyRouteQuery(),
)

onMounted(() => {
  applyRouteQuery()
  load()
})
</script>

<template>
  <section id="market-upstreams-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">上游</h2>
        <p class="y-page-desc">平台全部上游账号：检测、冻结与强制删除</p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="focusId" class="y-msg">
        已定位上游 #{{ focusId }}。草稿/授权中账号需在卖家端完成 OAuth 或密钥导入后，才能作为基线对照。
      </p>

      <div class="y-search">
        <input v-model="keyword" type="search" placeholder="ID / 邮箱 / 卖家 / 原因" @keyup.enter="pageNum = 1" />
        <select v-model="filterStatus" class="input" @change="pageNum = 1">
          <option value="">状态：全部</option>
          <option v-for="o in STATUS_OPTIONS" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
        <select v-model="filterPlatform" class="input" @change="pageNum = 1">
          <option value="">厂商：全部</option>
          <option v-for="o in PLATFORM_OPTIONS" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
        <button type="button" class="y-btn y-btn--ghost" @click="resetFilters">重置</button>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!pageRows.length" class="y-msg">没有符合条件的上游</p>
      <table v-else>
        <thead>
          <tr>
            <th>账号</th>
            <th>卖家</th>
            <th>厂商</th>
            <th>状态</th>
            <th>健康</th>
            <th>等级</th>
            <th>隐私</th>
            <th>接入时间</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in pageRows" :key="row.id" :class="{ 'is-focus': focusId === row.id }">
            <td>
              <div>
                {{ displayName(row) }}
                <span v-if="row.is_reference" class="y-chip">基线</span>
              </div>
              <div v-if="row.account_email" class="row-sub">#{{ row.id }}</div>
            </td>
            <td>#{{ row.seller_id }}</td>
            <td>{{ platformLabel(row.platform) }}<span v-if="row.auth_type"> · {{ row.auth_type }}</span></td>
            <td>
              <span :class="statusChip(row.status)">{{ row.status }}</span>
              <div v-if="row.failure_reason" class="row-sub" :title="row.failure_reason">{{ row.failure_reason }}</div>
            </td>
            <td>
              {{ row.health || '—' }}
              <div v-if="row.health_reason" class="row-sub" :title="row.health_reason">{{ row.health_reason }}</div>
            </td>
            <td>{{ row.grade || '—' }}</td>
            <td>
              {{
                row.privacy_ok === undefined || row.privacy_ok === null
                  ? '—'
                  : row.privacy_ok
                    ? '已关闭训练'
                    : '未关闭'
              }}
            </td>
            <td>{{ fmtTime(row.created_at) }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="probe(row)">检测</button>
              <button
                v-if="row.status !== 'suspended'"
                type="button"
                class="y-link"
                :disabled="busy"
                @click="suspend(row)"
              >
                冻结
              </button>
              <button v-else type="button" class="y-link" :disabled="busy" @click="unsuspend(row)">解冻</button>
              <button type="button" class="y-link y-link--danger" :disabled="busy" @click="remove(row)">删除</button>
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
  </section>
</template>

<style scoped>
.row-sub {
  margin-top: 2px;
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  opacity: 0.65;
}
.y-link--danger {
  color: var(--color-error, #ff4d4f);
}
tr.is-focus td {
  background: color-mix(in srgb, #0ea5e9 12%, transparent);
}
</style>
