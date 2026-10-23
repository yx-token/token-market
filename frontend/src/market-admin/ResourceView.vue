<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

type Kind =
  | 'vendors'
  | 'pools'
  | 'proxies'
  | 'categories'
  | 'upstreams'
  | 'probe-items'
  | 'probe-runs'
  | 'policies'
  | 'providers'
  | 'appeals'
  | 'recharges'
  | 'refunds'
  | 'withdrawals'
  | 'hot-searches'
  | 'checks'

const props = defineProps<{ kind: Kind }>()
const { t } = useI18n()

const catalog = computed<Record<Kind, { title: string; path: string; pick?: (data: unknown) => unknown[] }>>(() => ({
  vendors: { title: t('nav.marketVendors'), path: `${MARKET_API_PREFIX}/vendors` },
  pools: { title: t('nav.marketPools'), path: `${MARKET_API_PREFIX}/pools` },
  proxies: { title: t('nav.marketProxies'), path: `${MARKET_API_PREFIX}/proxies` },
  categories: { title: t('nav.marketCategoriesFull'), path: `${MARKET_API_PREFIX}/market-categories` },
  upstreams: { title: t('nav.marketUpstreams'), path: `${MARKET_API_PREFIX}/upstreams` },
  'probe-items': { title: t('nav.marketProbeItems'), path: `${MARKET_API_PREFIX}/probe-items` },
  'probe-runs': { title: t('nav.marketProbeRuns'), path: `${MARKET_API_PREFIX}/probe-runs` },
  policies: { title: t('nav.marketPolicies'), path: `${MARKET_API_PREFIX}/detection-policies` },
  providers: { title: t('nav.marketProviders'), path: `${MARKET_API_PREFIX}/detection-providers` },
  appeals: { title: t('nav.marketAppeals'), path: `${MARKET_API_PREFIX}/appeals` },
  recharges: { title: t('nav.marketFundsRecharge'), path: `${MARKET_API_PREFIX}/recharges` },
  refunds: { title: t('nav.marketFundsRefund'), path: `${MARKET_API_PREFIX}/refunds` },
  withdrawals: { title: t('nav.marketFundsWithdraw'), path: `${MARKET_API_PREFIX}/withdrawals` },
  'hot-searches': {
    title: t('nav.marketHotSearches'),
    path: `${MARKET_API_PREFIX}/market-discovery`,
    pick: (data) =>
      data && typeof data === 'object' && Array.isArray((data as { hot_searches?: unknown }).hot_searches)
        ? (data as { hot_searches: unknown[] }).hot_searches
        : [],
  },
  checks: { title: t('nav.marketChecks'), path: `${MARKET_API_PREFIX}/inspection/checks` },
}))

const rows = ref<Record<string, unknown>[]>([])
const discovery = ref<Record<string, unknown> | null>(null)
const error = ref('')
const loading = ref(false)
const actionMsg = ref('')
const initQuery = { pageNum: 1, pageSize: 15, keyword: '' }
const query = ref({ ...initQuery })

const meta = computed(() => catalog.value[props.kind])

const columns = computed(() => {
  const first = rows.value[0]
  if (!first) return [] as string[]
  const preferred =
    props.kind === 'vendors'
      ? ['platform', 'enabled', 'consignment_min', 'consignment_max', 'default_concurrency']
      : props.kind === 'pools'
        ? ['id', 'name', 'platform', 'sell_multiplier', 'enabled', 'market_published']
        : props.kind === 'proxies'
          ? ['id', 'name', 'protocol', 'host', 'port', 'status']
          : props.kind === 'probe-items'
            ? ['id', 'suite', 'check_key', 'status', 'weight']
            : props.kind === 'policies'
              ? ['id', 'platform', 'auth_type', 'enabled']
              : props.kind === 'providers'
                ? ['name', 'label', 'enabled', 'base_url']
                : props.kind === 'hot-searches'
                  ? ['id', 'label', 'sort', 'enabled']
                  : props.kind === 'checks'
                    ? ['key', 'name', 'flow', 'enabled', 'pricing', 'calibrated']
                    : props.kind === 'recharges' || props.kind === 'refunds' || props.kind === 'withdrawals'
                      ? ['id', 'status', 'amount', 'user_id']
                      : props.kind === 'appeals'
                        ? ['id', 'status', 'report_id', 'seller_id']
                        : null
  if (preferred) return preferred.filter((k) => k in first)
  return Object.keys(first).filter((k) => typeof first[k] !== 'object').slice(0, 6)
})

const filtered = computed(() => {
  const keyword = query.value.keyword.trim().toLowerCase()
  if (!keyword) return rows.value
  return rows.value.filter((row) =>
    columns.value.some((col) => String(row[col] ?? '').toLowerCase().includes(keyword)),
  )
})
const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / query.value.pageSize)))
const pageRows = computed(() => {
  const start = (query.value.pageNum - 1) * query.value.pageSize
  return filtered.value.slice(start, start + query.value.pageSize)
})

function handleQuery() {
  query.value.pageNum = 1
}

function resetQuery() {
  query.value = { ...initQuery }
}

function serial(index: number) {
  return (query.value.pageNum - 1) * query.value.pageSize + index + 1
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await marketGet<unknown>(meta.value.path)
    discovery.value = props.kind === 'hot-searches' && data && typeof data === 'object' ? (data as Record<string, unknown>) : null
    const picked = meta.value.pick ? meta.value.pick(data) : Array.isArray(data) ? data : data == null ? [] : [data]
    const list = (picked as Record<string, unknown>[]) || []
    rows.value = props.kind === 'categories' ? flatten(list) : list
  } catch (e) {
    error.value = e instanceof Error ? e.message : t('common.loadFailed')
    rows.value = []
  } finally {
    loading.value = false
  }
}

function flatten(nodes: Record<string, unknown>[], out: Record<string, unknown>[] = []): Record<string, unknown>[] {
  for (const n of nodes) {
    const { children, ...rest } = n
    out.push(rest)
    if (Array.isArray(children)) flatten(children as Record<string, unknown>[], out)
  }
  return out
}

const colLabel: Record<string, string> = {
  id: 'ID',
  platform: '平台',
  enabled: '启用',
  consignment_min: '寄售下限',
  consignment_max: '寄售上限',
  default_concurrency: '默认并发',
  name: '名称',
  sell_multiplier: '售价倍率',
  market_published: '市场上架',
  protocol: '协议',
  host: '主机',
  port: '端口',
  status: '状态',
  suite: '套件',
  check_key: '检查项',
  weight: '权重',
  auth_type: '接入方式',
  label: '标签',
  base_url: '地址',
  sort: '排序',
  key: '键',
  flow: '流程',
  pricing: '计价',
  calibrated: '已校准',
  amount: '金额',
  user_id: '用户',
  upstream_id: '上游',
  seller_id: '卖家',
  report_id: '报告',
  parent_id: '父级',
}

function labelOf(col: string) {
  return colLabel[col] || col
}

function cell(v: unknown) {
  if (v == null || v === '') return '—'
  if (typeof v === 'boolean') return v ? '是' : '否'
  return String(v)
}

function chipClass(col: string, v: unknown) {
  if (typeof v === 'boolean') return v ? 'y-chip y-chip--ok' : 'y-chip'
  const s = String(v || '')
  if (['enabled', 'active', 'paid', 'confirmed', 'ready', 'listed', 'normal'].includes(s)) return 'y-chip y-chip--ok'
  if (['pending', 'pending_review', 'paying', 'open', 'draft'].includes(s)) return 'y-chip y-chip--warn'
  if (['disabled', 'retired', 'rejected', 'failed', 'error'].includes(s)) return 'y-chip y-chip--err'
  if (col === 'status' || col === 'enabled') return 'y-chip'
  return ''
}

const fundsTabs = computed(() => [
  { kind: 'recharges' as const, to: '/admin/market/recharges', label: t('nav.marketRecharges') },
  { kind: 'refunds' as const, to: '/admin/market/refunds', label: t('nav.marketRefunds') },
  { kind: 'withdrawals' as const, to: '/admin/market/withdrawals', label: t('nav.marketWithdrawals') },
])

const showFundsTabs = computed(() =>
  ['recharges', 'refunds', 'withdrawals'].includes(props.kind)
)

const hasActions = computed(() =>
  ['vendors', 'proxies', 'pools', 'policies', 'probe-items', 'hot-searches', 'checks', 'appeals', 'recharges', 'refunds', 'withdrawals'].includes(props.kind)
)

async function run(label: string, fn: () => Promise<unknown>) {
  actionMsg.value = ''
  try {
    await fn()
    actionMsg.value = label
    await load()
  } catch (e) {
    actionMsg.value = e instanceof Error ? e.message : '操作失败'
  }
}

function toggleVendor(row: Record<string, unknown>) {
  const platform = String(row.platform || '')
  if (!platform) return
  return run(`${platform} 已${row.enabled ? '关闭' : '开启'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/vendors/${platform}`, { enabled: !row.enabled })
  )
}

function toggleProxy(row: Record<string, unknown>) {
  const id = row.id
  const off = row.status === 'disabled'
  return run(`代理 ${id} 已${off ? '启用' : '停用'}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/proxies/${id}/${off ? 'enable' : 'disable'}`)
  )
}

function togglePool(row: Record<string, unknown>) {
  return run(`分组 ${row.name} 已${row.enabled ? '关闭' : '开启'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/pools/${row.id}`, {
      name: row.name,
      platform: row.platform,
      enabled: !row.enabled,
    })
  )
}

function togglePolicy(row: Record<string, unknown>) {
  const q = row.auth_type ? `?auth_type=${encodeURIComponent(String(row.auth_type))}` : ''
  return run(`${row.platform} 策略已${row.enabled ? '关闭' : '开启'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/detection-policies/${row.platform}${q}`, { enabled: !row.enabled })
  )
}

function retireItem(row: Record<string, unknown>) {
  return run(`题目 ${row.id} 已下线`, () => marketSend('POST', `${MARKET_API_PREFIX}/probe-items/${row.id}/retire`))
}

function toggleHot(row: Record<string, unknown>) {
  const cfg = discovery.value
  if (!cfg) return
  const list = ((cfg.hot_searches as Record<string, unknown>[]) || []).map((item) =>
    item.id === row.id ? { ...item, enabled: !row.enabled } : item
  )
  return run(`${row.label || row.id} 已${row.enabled ? '下架' : '上架'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/market-discovery`, { ...cfg, hot_searches: list })
  )
}

function toggleCheck(row: Record<string, unknown>) {
  return run(`${row.key} 已${row.enabled ? '关闭' : '开启'}`, () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/inspection/checks/${row.key}`, { enabled: !row.enabled })
  )
}

function confirmRecharge(row: Record<string, unknown>) {
  if (row.status !== 'pending') return
  if (!window.confirm(`确认充值单 ${row.id} 已到账？在线支付单只能由支付渠道确认。`)) return
  const providerRef = window.prompt('到账流水号', 'manual')
  if (providerRef == null || !providerRef.trim()) return
  return run(`充值 ${row.id} 已确认`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/recharges/${row.id}/confirm`, { provider_ref: providerRef.trim() })
  )
}

function markRefundPaid(row: Record<string, unknown>) {
  if (row.status !== 'paying') return
  const payoutRef = window.prompt(`退款 ${row.id} 的打款流水号`)
  if (payoutRef == null || !payoutRef.trim()) return
  return run(`退款 ${row.id} 已标记打款`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/refunds/${row.id}/paid`, { payout_ref: payoutRef.trim() })
  )
}

function reviewWithdrawal(row: Record<string, unknown>, approve: boolean) {
  if (row.status !== 'pending_review') return
  let reason = ''
  if (approve) {
    if (!window.confirm(`通过提现 ${row.id}？`)) return
  } else {
    const input = window.prompt(`拒绝提现 ${row.id} 的原因`)
    if (input == null || !input.trim()) return
    reason = input.trim()
  }
  return run(`提现 ${row.id} 已${approve ? '通过' : '拒绝'}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/withdrawals/${row.id}/review`, { approve, reason })
  )
}

function resolveAppeal(row: Record<string, unknown>, action: 'uphold' | 'overturn' | 'retest') {
  if (row.status !== 'open') return
  const label = action === 'uphold' ? '维持原判' : action === 'overturn' ? '推翻并恢复质量' : '安排复测'
  if (!window.confirm(`申诉 ${row.id}：${label}？`)) return
  const note = window.prompt('处理说明，可留空', '') ?? ''
  return run(`申诉 ${row.id}：${label}`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/appeals/${row.id}/resolve`, { action, note })
  )
}

onMounted(load)
watch(() => props.kind, load)
</script>

<template>
  <section id="market-resource-page" class="y-page">
    <header class="y-header">
      <h2 class="y-title">{{ meta.title }}</h2>
      <button type="button" class="y-btn" :disabled="loading" @click="load">{{ t('nav.refresh') }}</button>
    </header>
    <nav v-if="showFundsTabs" class="y-tabs">
      <RouterLink
        v-for="tab in fundsTabs"
        :key="tab.kind"
        :to="tab.to"
        class="y-tab"
        :class="{ 'is-on': kind === tab.kind }"
      >
        {{ tab.label }}
      </RouterLink>
    </nav>
    <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
    <p v-else-if="actionMsg" class="y-msg y-msg--ok">{{ actionMsg }}</p>
    <div class="y-container--tight">
      <div class="y-search">
        <input
          v-model="query.keyword"
          type="search"
          :placeholder="t('nav.searchCurrentColumns')"
          @keyup.enter="handleQuery"
        />
        <button type="button" class="y-btn" @click="handleQuery">{{ t('nav.query') }}</button>
        <button type="button" class="y-btn y-btn--ghost" @click="resetQuery">{{ t('nav.reset') }}</button>
      </div>
      <p v-if="loading" class="y-msg">{{ t('nav.loading') }}</p>
      <p v-else-if="!filtered.length" class="y-msg">{{ t('nav.noData') }}</p>
      <table v-else>
        <thead>
          <tr>
            <th>{{ t('nav.serialNo') }}</th>
            <th v-for="col in columns" :key="col">{{ labelOf(col) }}</th>
            <th v-if="hasActions" class="y-col-op">{{ t('nav.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in pageRows" :key="i">
            <td>{{ serial(i) }}</td>
            <td v-for="col in columns" :key="col">
              <span v-if="chipClass(col, row[col])" :class="chipClass(col, row[col])">{{ cell(row[col]) }}</span>
              <template v-else>{{ cell(row[col]) }}</template>
            </td>
            <td v-if="hasActions" class="y-col-op">
              <button v-if="kind === 'vendors'" type="button" class="y-link" @click="toggleVendor(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button v-else-if="kind === 'proxies'" type="button" class="y-link" @click="toggleProxy(row)">
                {{ row.status === 'disabled' ? '启用' : '停用' }}
              </button>
              <button v-else-if="kind === 'pools'" type="button" class="y-link" @click="togglePool(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button v-else-if="kind === 'policies'" type="button" class="y-link" @click="togglePolicy(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button
                v-else-if="kind === 'probe-items' && row.status !== 'retired'"
                type="button"
                class="y-link"
                @click="retireItem(row)"
              >
                下线
              </button>
              <button v-else-if="kind === 'hot-searches'" type="button" class="y-link" @click="toggleHot(row)">
                {{ row.enabled ? '下架' : '上架' }}
              </button>
              <button v-else-if="kind === 'checks'" type="button" class="y-link" @click="toggleCheck(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button
                v-else-if="kind === 'recharges' && row.status === 'pending'"
                type="button"
                class="y-link"
                @click="confirmRecharge(row)"
              >
                确认到账
              </button>
              <button
                v-else-if="kind === 'refunds' && row.status === 'paying'"
                type="button"
                class="y-link"
                @click="markRefundPaid(row)"
              >
                标记已打款
              </button>
              <template v-else-if="kind === 'withdrawals' && row.status === 'pending_review'">
                <button type="button" class="y-link" @click="reviewWithdrawal(row, true)">通过</button>
                <button type="button" class="y-link" @click="reviewWithdrawal(row, false)">拒绝</button>
              </template>
              <template v-else-if="kind === 'appeals' && row.status === 'open'">
                <button type="button" class="y-link" @click="resolveAppeal(row, 'uphold')">维持</button>
                <button type="button" class="y-link" @click="resolveAppeal(row, 'overturn')">推翻</button>
                <button type="button" class="y-link" @click="resolveAppeal(row, 'retest')">复测</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="total > 0" class="y-footer">
      <div class="y-pager">
        <span>共 {{ total }} 条</span>
        <button type="button" :disabled="query.pageNum <= 1" @click="query.pageNum -= 1">上一页</button>
        <button type="button" class="is-on">{{ query.pageNum }} / {{ pageCount }}</button>
        <button type="button" :disabled="query.pageNum >= pageCount" @click="query.pageNum += 1">下一页</button>
      </div>
    </footer>
  </section>
</template>
