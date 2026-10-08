<script setup lang="ts">
import { onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { market } from '@/api'
import type { MarketItem, PoolItem, Price } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { label, options } from '@/utils/labels'
import { ms, multiplier, percent } from '@/utils/format'

const router = useRouter()
const auth = useAuth()
const filter = reactive({ platform: '', grade: '', max_price: '', sort: 'price' })
const listings = useLoader(() => market.listings({ ...filter }), [] as MarketItem[] | null)
const pools = useLoader(() => market.pools(), [] as PoolItem[] | null)
const prices = useLoader(() => market.prices(), [] as Price[] | null)

onMounted(() => {
  listings.load()
  pools.load()
  prices.load()
})

function buy(listingId: number) {
  router.push(auth.loggedIn ? { path: '/buyer/keys', query: { listing: listingId } } : { name: 'login', query: { redirect: `/buyer/keys?listing=${listingId}` } })
}
const platforms = options('platform').filter((o) => o.value !== 'composite')
</script>

<template>
  <div class="space-y-6">
    <section class="rounded-lg bg-gradient-to-r from-brand-700 to-brand-500 p-6 text-white">
      <h1 class="text-2xl font-semibold">把闲置的订阅额度变现，用更低的价格调用大模型</h1>
      <p class="mt-2 max-w-3xl text-sm text-brand-100">
        卖家挂出 Claude、ChatGPT、Gemini、Kimi 等订阅账号的闲置额度；买家按"官方价 × 倍率"付费，可以走平台默认分组，也可以指定最多 5 个卖家依次故障转移。
        所有上游都经过平台质量检测，发现降智立即停止调度。
      </p>
      <div class="mt-4 flex gap-3">
        <RouterLink to="/buyer/keys" class="btn bg-white text-brand-700 hover:bg-brand-50">创建 API Key</RouterLink>
        <RouterLink to="/seller/upstreams" class="btn border border-white/60 text-white hover:bg-white/10">挂出我的额度</RouterLink>
      </div>
    </section>

    <section class="card">
      <h2 class="card-title">默认分组</h2>
      <p class="mb-3 text-sm text-slate-500">平台优先调度回收的上游，其次是价格更低的寄售上游，由平台负责可用性。</p>
      <DataTable
        :columns="[
          { key: 'name', label: '分组' },
          { key: 'platform', label: '平台' },
          { key: 'price', label: '价格（官方价倍率）', align: 'right' },
        ]"
        :rows="pools.data.value"
        :loading="pools.loading.value"
        row-key="name"
      >
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-price="{ row }">{{ multiplier(row.price) }}</template>
      </DataTable>
    </section>

    <section class="card">
      <div class="mb-3 flex flex-wrap items-end gap-3">
        <h2 class="card-title mb-0 mr-auto">寄售挂单</h2>
        <select v-model="filter.platform" class="input w-40" aria-label="平台" @change="listings.load()">
          <option value="">全部平台</option>
          <option v-for="p in platforms" :key="p.value" :value="p.value">{{ p.text }}</option>
        </select>
        <select v-model="filter.grade" class="input w-28" aria-label="等级" @change="listings.load()">
          <option value="">全部等级</option>
          <option v-for="g in ['S', 'A', 'B', 'C']" :key="g" :value="g">{{ g }} 级</option>
        </select>
        <input v-model="filter.max_price" class="input w-32" placeholder="最高倍率" inputmode="decimal" aria-label="最高倍率" @keyup.enter="listings.load()" />
        <select v-model="filter.sort" class="input w-32" aria-label="排序" @change="listings.load()">
          <option value="price">价格从低到高</option>
          <option value="success">成功率</option>
          <option value="ttft">首字延迟</option>
        </select>
      </div>
      <DataTable
        :columns="[
          { key: 'listing_id', label: '挂单' },
          { key: 'platform', label: '平台' },
          { key: 'tier', label: '档位' },
          { key: 'grade', label: '等级' },
          { key: 'health', label: '状态' },
          { key: 'price', label: '价格', align: 'right' },
          { key: 'success_rate_24h', label: '24h 成功率', align: 'right' },
          { key: 'ttft_p50_ms', label: '首字延迟', align: 'right' },
          { key: 'actions', label: '' },
        ]"
        :rows="listings.data.value"
        :loading="listings.loading.value"
        row-key="listing_id"
        empty="暂无符合条件的挂单"
      >
        <template #cell-listing_id="{ row }">
          <RouterLink :to="`/market/listings/${row.listing_id}`" class="link">#{{ row.listing_id }}</RouterLink>
        </template>
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-grade="{ row }">{{ row.grade ? `${row.grade} 级` : '—' }}</template>
        <template #cell-health="{ row }"><StatusBadge kind="health" :value="row.health" /></template>
        <template #cell-price="{ row }">{{ multiplier(row.price) }}</template>
        <template #cell-success_rate_24h="{ row }">{{ percent(row.success_rate_24h) }}</template>
        <template #cell-ttft_p50_ms="{ row }">{{ ms(row.ttft_p50_ms) }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-primary btn-sm" @click="buy(row.listing_id)">指定购买</button>
        </template>
      </DataTable>
    </section>

    <section v-if="prices.data.value && prices.data.value.length" class="card">
      <h2 class="card-title">官方价目</h2>
      <p class="mb-3 text-sm text-slate-500">倍率以官方价为基准，例如 0.6× 表示按官方价的 60% 计费（单位：每百万 token，1:1 结算）。</p>
      <DataTable
        :columns="[
          { key: 'model', label: '模型' },
          { key: 'input_per_1m', label: '输入', align: 'right' },
          { key: 'output_per_1m', label: '输出', align: 'right' },
          { key: 'cache_read_per_1m', label: '缓存读取', align: 'right' },
        ]"
        :rows="prices.data.value"
        row-key="model"
      />
    </section>
  </div>
</template>
