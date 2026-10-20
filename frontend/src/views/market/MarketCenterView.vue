<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { marketJson, marketSessionToken } from './marketApi'

type KeyRow = { id: number; name: string; key_prefix: string; status: string; route_type: string }
type Wallet = { balance: number | null; refundable?: number; ledger_cash: number }
type MerchantRow = { seller_id: number; added_at: string; listings?: { listing_id: number; title?: string; platform: string }[] }
type SellerListing = { id: number; title?: string; status: string; platform?: string; mode?: string; sku_kind?: string }
type Upstream = { id: number; platform: string; status: string; health?: string; account_email?: string }
type Bill = { day: string; requests: number; cost: number; cash_cost: number; credit_prepaid_cny: number }

const route = useRoute()
const router = useRouter()
const tabs = [
  { id: 'keys', label: '买家 Key' },
  { id: 'wallet', label: '钱包' },
  { id: 'merchants', label: '商家清单' },
  { id: 'bills', label: '账单' },
  { id: 'listings', label: '卖家挂单' },
  { id: 'upstreams', label: '卖家上游' },
] as const
type Tab = (typeof tabs)[number]['id']

const tab = computed(() => (tabs.some((t) => t.id === route.query.tab) ? (route.query.tab as Tab) : 'keys'))
const loggedIn = computed(() => !!marketSessionToken())
const error = ref('')
const msg = ref('')
const loading = ref(false)
const keys = ref<KeyRow[]>([])
const wallet = ref<Wallet | null>(null)
const merchants = ref<MerchantRow[]>([])
const listings = ref<SellerListing[]>([])
const upstreams = ref<Upstream[]>([])
const bills = ref<Bill[]>([])
const keyName = ref('默认 Key')
const listingID = ref('')
const packListingID = ref('')
const amount = ref('10')
const issued = ref('')

function showTab(id: Tab) {
  router.replace({ path: '/shop/center', query: { tab: id } })
}

async function load() {
  error.value = ''
  msg.value = ''
  if (!loggedIn.value) return
  loading.value = true
  try {
    if (tab.value === 'keys') {
      keys.value = (await marketJson<KeyRow[] | null>('/api/buyer/keys')) ?? []
    } else if (tab.value === 'wallet') {
      wallet.value = await marketJson<Wallet>('/api/buyer/wallet')
    } else if (tab.value === 'merchants') {
      merchants.value = (await marketJson<MerchantRow[] | null>('/api/buyer/merchants')) ?? []
    } else if (tab.value === 'bills') {
      bills.value = (await marketJson<Bill[] | null>('/api/buyer/bills')) ?? []
    } else if (tab.value === 'listings') {
      listings.value = (await marketJson<SellerListing[] | null>('/api/seller/listings')) ?? []
    } else {
      upstreams.value = (await marketJson<Upstream[] | null>('/api/seller/upstreams')) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function createKey() {
  error.value = ''
  issued.value = ''
  const id = Number(listingID.value)
  if (!keyName.value.trim() || !Number.isFinite(id) || id <= 0) {
    error.value = '填写 Key 名称和挂单 ID'
    return
  }
  try {
    const out = await marketJson<{ key: string }>('/api/buyer/keys', {
      method: 'POST',
      body: JSON.stringify({ name: keyName.value.trim(), route: { type: 'chain', hops: [{ listing_id: id }] }, limits: {} }),
    })
    issued.value = out.key
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '创建失败'
  }
}

async function toggleKey(row: KeyRow) {
  error.value = ''
  try {
    const action = row.status === 'active' ? 'disable' : 'enable'
    await marketJson(`/api/buyer/keys/${row.id}/${action}`, { method: 'POST' })
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  }
}

async function recharge() {
  error.value = ''
  const n = Number(amount.value)
  if (!Number.isFinite(n) || n <= 0) {
    error.value = '充值金额无效'
    return
  }
  try {
    await marketJson('/api/buyer/recharge', { method: 'POST', body: JSON.stringify({ amount: n, channel: 'balance' }) })
    msg.value = '充值单已创建'
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '充值失败'
  }
}

async function buyPack() {
  error.value = ''
  const id = Number(packListingID.value)
  if (!Number.isFinite(id) || id <= 0) {
    error.value = '填写套餐挂单 ID'
    return
  }
  try {
    const out = await marketJson<{ key?: string; note?: string }>('/api/buyer/packs', {
      method: 'POST',
      body: JSON.stringify({ listing_id: id, channel: 'balance', name: keyName.value.trim() || '套餐' }),
    })
    issued.value = out.key || ''
    msg.value = out.note || '套餐购买已提交'
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '购买失败'
  }
}

async function startOAuth(id: number) {
  error.value = ''
  try {
    const out = await marketJson<{ auth_url?: string }>(`/api/seller/upstreams/${id}/oauth/start`, { method: 'POST', body: '{}' })
    if (out.auth_url) {
      window.location.href = out.auth_url
      return
    }
    msg.value = `上游 #${id} 已发起授权`
  } catch (e) {
    error.value = e instanceof Error ? e.message : '授权失败'
  }
}

async function pauseListing(id: number) {
  error.value = ''
  try {
    await marketJson(`/api/seller/listings/${id}/pause`, { method: 'POST' })
    msg.value = `挂单 #${id} 已暂停`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '暂停失败'
  }
}

watch(tab, () => load())
onMounted(load)
</script>

<template>
  <AppLayout>
    <section class="space-y-4">
      <header>
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">买卖中心</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">使用市场登录态 tm_token。套餐购买走余额渠道；OAuth 会跳到厂商授权页。</p>
      </header>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="t in tabs"
          :key="t.id"
          type="button"
          class="rounded border px-3 py-1.5 text-sm"
          :class="tab === t.id ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-300' : 'border-gray-200 bg-white text-gray-600 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300'"
          @click="showTab(t.id)"
        >
          {{ t.label }}
        </button>
      </div>
      <p v-if="!loggedIn" class="text-sm text-gray-600 dark:text-gray-300">
        需要市场登录。<a class="text-primary-600 dark:text-primary-400" href="/login">去登录</a>
      </p>
      <template v-else>
        <p v-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <p v-else-if="msg" class="text-sm text-green-600 dark:text-green-400">{{ msg }}</p>
        <p v-if="loading" class="text-sm text-gray-500">加载中…</p>

        <div v-else-if="tab === 'keys'" class="space-y-3">
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="createKey">
            <label class="text-sm">
              <span class="mb-1 block text-gray-500">名称</span>
              <input v-model="keyName" class="rounded border border-gray-200 bg-white px-2 py-1 dark:border-dark-700 dark:bg-dark-900" />
            </label>
            <label class="text-sm">
              <span class="mb-1 block text-gray-500">挂单 ID</span>
              <input v-model="listingID" class="w-28 rounded border border-gray-200 bg-white px-2 py-1 dark:border-dark-700 dark:bg-dark-900" />
            </label>
            <button class="rounded bg-primary-600 px-3 py-1.5 text-sm text-white" type="submit">创建 Key</button>
          </form>
          <form class="flex flex-wrap items-end gap-2" @submit.prevent="buyPack">
            <label class="text-sm">
              <span class="mb-1 block text-gray-500">套餐挂单 ID</span>
              <input v-model="packListingID" class="w-28 rounded border border-gray-200 bg-white px-2 py-1 dark:border-dark-700 dark:bg-dark-900" />
            </label>
            <button class="rounded border border-primary-600 px-3 py-1.5 text-sm text-primary-700 dark:text-primary-300" type="submit">余额购买套餐</button>
          </form>
          <p v-if="issued" class="break-all font-mono text-xs text-gray-700 dark:text-gray-200">{{ issued }}</p>
          <ul class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
            <li v-for="k in keys" :key="k.id" class="flex items-center justify-between px-4 py-3 text-sm">
              <span>{{ k.name }} · {{ k.key_prefix }} · {{ k.status }}</span>
              <button class="text-primary-600 dark:text-primary-400" type="button" @click="toggleKey(k)">
                {{ k.status === 'active' ? '停用' : '启用' }}
              </button>
            </li>
            <li v-if="!keys.length" class="px-4 py-6 text-sm text-gray-500">还没有 Key</li>
          </ul>
        </div>

        <div v-else-if="tab === 'wallet' && wallet" class="space-y-3 text-sm">
          <div class="grid grid-cols-3 gap-3">
            <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500">余额</div>
              <div class="mt-1 text-lg">{{ wallet.balance ?? '—' }}</div>
            </div>
            <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500">可退</div>
              <div class="mt-1 text-lg">{{ wallet.refundable ?? '—' }}</div>
            </div>
            <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
              <div class="text-xs text-gray-500">账本现金</div>
              <div class="mt-1 text-lg">{{ wallet.ledger_cash }}</div>
            </div>
          </div>
          <form class="flex items-end gap-2" @submit.prevent="recharge">
            <label class="text-sm">
              <span class="mb-1 block text-gray-500">充值金额</span>
              <input v-model="amount" class="w-28 rounded border border-gray-200 bg-white px-2 py-1 dark:border-dark-700 dark:bg-dark-900" />
            </label>
            <button class="rounded bg-primary-600 px-3 py-1.5 text-sm text-white" type="submit">创建充值单</button>
          </form>
        </div>

        <ul v-else-if="tab === 'merchants'" class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
          <li v-for="m in merchants" :key="m.seller_id" class="px-4 py-3 text-sm">
            <div class="font-medium">卖家 #{{ m.seller_id }}</div>
            <div class="text-xs text-gray-500">挂单 {{ m.listings?.length ?? 0 }}</div>
          </li>
          <li v-if="!merchants.length" class="px-4 py-6 text-sm text-gray-500">还没有商家</li>
        </ul>

        <ul v-else-if="tab === 'bills'" class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
          <li v-for="b in bills" :key="b.day" class="px-4 py-3 text-sm">
            {{ b.day }} · {{ b.requests }} 次 · 合计 {{ b.cost }}
            <div class="text-xs text-gray-500">现金 {{ b.cash_cost }} · 活动预付 {{ b.credit_prepaid_cny }}</div>
          </li>
          <li v-if="!bills.length" class="px-4 py-6 text-sm text-gray-500">还没有账单</li>
        </ul>

        <ul v-else-if="tab === 'listings'" class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
          <li v-for="l in listings" :key="l.id" class="flex items-center justify-between px-4 py-3 text-sm">
            <span>{{ l.title || `#${l.id}` }} · {{ l.status }}</span>
            <button v-if="l.status === 'active'" class="text-primary-600 dark:text-primary-400" type="button" @click="pauseListing(l.id)">暂停</button>
          </li>
          <li v-if="!listings.length" class="px-4 py-6 text-sm text-gray-500">还没有挂单</li>
        </ul>

        <ul v-else class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white dark:divide-dark-800 dark:border-dark-700 dark:bg-dark-900">
          <li v-for="u in upstreams" :key="u.id" class="flex items-center justify-between px-4 py-3 text-sm">
            <span>
              #{{ u.id }} · {{ u.platform }} · {{ u.status }} · {{ u.health || '—' }}
              <div v-if="u.account_email" class="text-xs text-gray-500">{{ u.account_email }}</div>
            </span>
            <button class="text-primary-600 dark:text-primary-400" type="button" @click="startOAuth(u.id)">发起 OAuth</button>
          </li>
          <li v-if="!upstreams.length" class="px-4 py-6 text-sm text-gray-500">还没有上游</li>
        </ul>
      </template>
    </section>
  </AppLayout>
</template>
