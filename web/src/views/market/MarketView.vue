<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { market } from '@/api'
import type { MarketItem } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { useAuth } from '@/stores/auth'
import { useMarketBoard } from '@/stores/marketBoard'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import FlapNumber from '@/components/FlapNumber.vue'
import GradeMark from '@/components/GradeMark.vue'
import { label, options } from '@/utils/labels'
import { ms, percent } from '@/utils/format'

const router = useRouter()
const auth = useAuth()
const board = useMarketBoard()
const filter = reactive({ platform: '', grade: '', max_price: '', sort: 'price' })
const listings = useLoader(() => market.listings({ ...filter }), [] as MarketItem[] | null)

onMounted(() => {
  listings.load()
  board.start()
})

function buy(listingId: number) {
  router.push(auth.loggedIn ? { path: '/buyer/keys', query: { listing: listingId } } : { name: 'login', query: { redirect: `/buyer/keys?listing=${listingId}` } })
}
const platforms = options('platform').filter((o) => o.value !== 'composite')
const price = (v: number) => v.toFixed(2)
const updated = computed(() => {
  const d = board.updatedAt
  if (!d) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
})
const totalListed = computed(() => board.listings.length)
const issue = computed(() => {
  const d = new Date()
  return `${d.getFullYear()}.${String(d.getMonth() + 1).padStart(2, '0')}.${String(d.getDate()).padStart(2, '0')}`
})
const steps = [
  { no: '壹', title: '买家：按官方价打折调用', body: '充值后创建 API Key，把 SDK 的 Base URL 换成平台地址即可。走默认分组由平台保障可用；也可以指定最多 5 个卖家，依次故障转移。' },
  { no: '贰', title: '卖家：挂出用不完的订阅', body: '授权官方订阅账号或 Coding Plan，通过上架检测后挂单：寄售自己定价，或接受平台回收报价。收益按小时结算，可提现。' },
  { no: '叁', title: '平台：检测质量，降智即停', body: '自有题库对照基线账号持续检测，并可接入第三方身份检测；判定降智或不可用的上游立即停止调度，失败的请求不计费。' },
]
</script>

<template>
  <div class="space-y-14">
    <!-- 报头：标题 + 行情板 -->
    <section class="grid grid-cols-1 gap-10 lg:grid-cols-12 lg:gap-12">
      <div class="lg:col-span-7">
        <div class="kicker mb-5 flex items-center gap-3">
          <span class="bg-ink-900 px-2 py-0.5 text-white">今日行情</span>
          <span>{{ issue }}</span>
          <span class="h-px flex-1 bg-ink-300" />
        </div>
        <h1 class="font-display text-[44px] font-bold leading-[1.12] tracking-[0.02em] md:text-[64px]">
          闲置的订阅，<br />
          流动的<span class="relative inline-block text-brand-500">算力<span class="absolute -bottom-1 left-0 h-[6px] w-full bg-brand-500/15" /></span>。
        </h1>
        <p class="mt-6 max-w-xl text-[15px] leading-[1.85] text-ink-700">
          卖家把 Claude、ChatGPT、Gemini、Kimi 等订阅账号用不完的额度挂到平台；买家按<b class="font-semibold text-ink-900">「官方价 × 倍率」</b>付费调用，
          同一个 Key 兼容官方协议。每个上游都经过质量检测，发现降智立即停止调度。
        </p>
        <div class="mt-8 flex flex-wrap items-center gap-3">
          <RouterLink to="/buyer/keys" class="btn-primary !px-5 !py-2.5 !text-sm">创建 API Key<span aria-hidden="true">→</span></RouterLink>
          <RouterLink to="/seller/upstreams" class="btn-secondary !px-5 !py-2.5 !text-sm">挂出我的额度</RouterLink>
        </div>
        <dl class="mt-10 grid max-w-xl grid-cols-3 border-y border-ink-900">
          <div class="border-r border-ink-200 py-3 pr-4">
            <dt class="kicker text-[10px]">在售挂单</dt>
            <dd class="num mt-1 text-[26px] font-medium">{{ totalListed }}</dd>
          </div>
          <div class="border-r border-ink-200 px-4 py-3">
            <dt class="kicker text-[10px]">覆盖平台</dt>
            <dd class="num mt-1 text-[26px] font-medium">{{ board.board.length }}</dd>
          </div>
          <div class="py-3 pl-4">
            <dt class="kicker text-[10px]">默认分组</dt>
            <dd class="num mt-1 text-[26px] font-medium">{{ board.pools.length }}</dd>
          </div>
        </dl>
      </div>

      <aside class="lg:col-span-5">
        <div class="bg-grid-ink relative overflow-hidden border border-ink-900 bg-ink-900 text-ink-200 shadow-hard-red">
          <div class="flex items-center justify-between border-b border-ink-700 px-5 py-3">
            <div class="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.2em] text-white">
              <span class="h-1.5 w-1.5 animate-blink rounded-full bg-brand-500" />行情板
            </div>
            <span class="font-mono text-[10.5px] text-ink-400">更新 {{ updated }}</span>
          </div>
          <div class="grid grid-cols-[1fr_auto_auto] gap-x-5 border-b border-ink-700 px-5 py-2 font-mono text-[10px] uppercase tracking-[0.16em] text-ink-400">
            <span>平台</span><span class="text-right">最低买家价</span><span class="w-12 text-right">在售</span>
          </div>
          <div v-if="board.board.length" class="divide-y divide-ink-800">
            <div v-for="r in board.board" :key="r.platform" class="grid grid-cols-[1fr_auto_auto] items-center gap-x-5 px-5 py-3">
              <div class="min-w-0">
                <div class="truncate text-[14px] font-medium text-white">{{ label('platform', r.platform).text }}</div>
                <div class="mt-0.5 font-mono text-[10.5px] text-ink-400">
                  成功率 {{ percent(r.successRate, 0) }} · 首字 {{ ms(r.ttft) }}
                </div>
              </div>
              <div class="text-right text-[21px]"><FlapNumber :value="`${price(r.best)}×`" /></div>
              <div class="num w-12 text-right text-[15px] text-white">{{ r.count }}</div>
            </div>
          </div>
          <div v-else class="px-5 py-8 text-center font-mono text-xs text-ink-400">暂无在售挂单 · 卖家挂单后实时显示</div>
          <div v-if="board.pools.length" class="border-t border-ink-700 bg-ink-950/40 px-5 py-3">
            <div class="mb-2 font-mono text-[10px] uppercase tracking-[0.16em] text-ink-400">默认分组 · 平台保障可用</div>
            <div class="flex flex-wrap gap-x-6 gap-y-2">
              <div v-for="p in board.pools" :key="p.name" class="flex items-baseline gap-2">
                <span class="text-[13px] text-ink-200">{{ label('platform', p.platform).text }}</span>
                <span class="num text-[15px] text-white">{{ price(p.price) }}×</span>
              </div>
            </div>
          </div>
        </div>
        <p class="mt-3 font-mono text-[11px] leading-relaxed text-ink-500">倍率以官方价为基准：0.60× 即按官方价的 60% 计费。</p>
      </aside>
    </section>

    <!-- 默认分组：票据 -->
    <section v-if="board.pools.length">
      <div class="mb-5 flex items-end justify-between border-b border-ink-900 pb-3">
        <h2 class="font-display text-[22px] font-bold tracking-wide">默认分组</h2>
        <span class="kicker">优先调度平台回收的上游，其次是价格更低的寄售上游</span>
      </div>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="p in board.pools"
          :key="p.name"
          class="relative flex items-stretch border border-ink-900 bg-white transition-transform duration-200 hover:-translate-y-0.5 hover:shadow-hard"
        >
          <div class="flex-1 px-5 py-4">
            <div class="font-mono text-[11px] text-ink-500">{{ p.name }}</div>
            <div class="mt-1 font-display text-[18px] font-bold">{{ label('platform', p.platform).text }}</div>
            <div class="mt-3 text-[12px] text-ink-500">官方价的 {{ Math.round(p.price * 100) }}%</div>
          </div>
          <div class="relative flex w-28 flex-col items-center justify-center border-l border-dashed border-ink-300 bg-paper px-3">
            <span class="absolute -left-[7px] -top-[7px] h-3.5 w-3.5 rounded-full border border-ink-900 bg-paper" />
            <span class="absolute -bottom-[7px] -left-[7px] h-3.5 w-3.5 rounded-full border border-ink-900 bg-paper" />
            <span class="num text-[26px] font-medium leading-none text-brand-600">{{ price(p.price) }}</span>
            <span class="kicker mt-1 text-[9.5px]">倍率</span>
          </div>
        </div>
      </div>
    </section>

    <!-- 寄售挂单：委托簿 -->
    <section>
      <div class="mb-4 flex flex-wrap items-end gap-3 border-b border-ink-900 pb-3">
        <div class="mr-auto">
          <h2 class="font-display text-[22px] font-bold tracking-wide">寄售挂单</h2>
          <p class="mt-1 text-[13px] text-ink-500">卖家自己定价；可以指定一个或多个卖家组成故障转移链。</p>
        </div>
        <select v-model="filter.platform" class="input !w-36" aria-label="平台" @change="listings.load()">
          <option value="">全部平台</option>
          <option v-for="p in platforms" :key="p.value" :value="p.value">{{ p.text }}</option>
        </select>
        <select v-model="filter.grade" class="input !w-28" aria-label="等级" @change="listings.load()">
          <option value="">全部等级</option>
          <option v-for="g in ['S', 'A', 'B', 'C']" :key="g" :value="g">{{ g }} 级</option>
        </select>
        <input v-model="filter.max_price" class="input !w-28" placeholder="最高倍率" inputmode="decimal" aria-label="最高倍率" @keyup.enter="listings.load()" />
        <select v-model="filter.sort" class="input !w-36" aria-label="排序" @change="listings.load()">
          <option value="price">价格从低到高</option>
          <option value="success">成功率</option>
          <option value="ttft">首字延迟</option>
        </select>
      </div>
      <div class="card !p-3 md:!p-4">
        <DataTable
          :columns="[
            { key: 'listing_id', label: '挂单' },
            { key: 'platform', label: '平台' },
            { key: 'grade', label: '等级' },
            { key: 'health', label: '状态' },
            { key: 'price', label: '买家价', align: 'right' },
            { key: 'success_rate_24h', label: '24h 成功率', align: 'right' },
            { key: 'ttft_p50_ms', label: '首字延迟', align: 'right' },
            { key: 'requests_24h', label: '24h 请求', align: 'right' },
            { key: 'actions', label: '' },
          ]"
          :rows="listings.data.value"
          :loading="listings.loading.value"
          row-key="listing_id"
          empty="暂无符合条件的挂单"
        >
          <template #cell-listing_id="{ row }">
            <RouterLink :to="`/market/listings/${row.listing_id}`" class="link font-mono">#{{ String(row.listing_id).padStart(4, '0') }}</RouterLink>
          </template>
          <template #cell-platform="{ row }">
            <span class="font-medium text-ink-900">{{ label('platform', row.platform).text }}</span>
            <span v-if="row.tier" class="ml-1.5 font-mono text-[11px] text-ink-500">{{ row.tier }}</span>
          </template>
          <template #cell-grade="{ row }"><GradeMark :grade="row.grade" /></template>
          <template #cell-health="{ row }"><StatusBadge kind="health" :value="row.health" /></template>
          <template #cell-price="{ row }"><span class="text-[17px] font-medium text-brand-600">{{ price(row.price) }}×</span></template>
          <template #cell-success_rate_24h="{ row }">
            <div class="inline-flex flex-col items-end gap-1">
              <span>{{ percent(row.success_rate_24h) }}</span>
              <span class="block h-[3px] w-16 bg-ink-100">
                <span class="block h-full bg-emerald-600" :style="{ width: `${(row.success_rate_24h ?? 0) * 100}%` }" />
              </span>
            </div>
          </template>
          <template #cell-ttft_p50_ms="{ row }">{{ ms(row.ttft_p50_ms) }}</template>
          <template #cell-actions="{ row }">
            <button class="btn-primary btn-sm" @click="buy(row.listing_id)">指定购买<span aria-hidden="true">→</span></button>
          </template>
        </DataTable>
      </div>
    </section>

    <!-- 怎么运作 -->
    <section class="grid grid-cols-1 gap-px overflow-hidden border border-ink-900 bg-ink-900 md:grid-cols-3">
      <div v-for="s in steps" :key="s.no" class="bg-white p-6 md:p-7">
        <div class="font-display text-[44px] font-bold leading-none text-transparent [-webkit-text-stroke:1.2px_var(--vermilion)]">{{ s.no }}</div>
        <h3 class="mt-4 font-display text-[17px] font-bold">{{ s.title }}</h3>
        <p class="mt-2 text-[13.5px] leading-[1.8] text-ink-600">{{ s.body }}</p>
      </div>
    </section>

    <!-- 官方价目 -->
    <section v-if="board.prices.length" class="grid grid-cols-1 gap-8 lg:grid-cols-12">
      <div class="lg:col-span-4">
        <h2 class="font-display text-[22px] font-bold tracking-wide">官方价目</h2>
        <p class="mt-2 text-[13.5px] leading-[1.8] text-ink-600">倍率的计算基准，取自底层网关内置的官方价目表，单位为美元 / 百万 token，按 1:1 结算。</p>
        <div class="mt-5 border-l-[3px] border-brand-500 bg-white px-4 py-3">
          <div class="kicker text-[10px]">计费公式</div>
          <div class="mt-1.5 font-display text-[17px] font-bold">买家价 = 官方价 × 倍率</div>
          <div class="mt-1 font-mono text-[11.5px] text-ink-500">失败的请求不产生费用</div>
        </div>
      </div>
      <div class="card !p-3 md:!p-4 lg:col-span-8">
        <DataTable
          :columns="[
            { key: 'model', label: '模型' },
            { key: 'input_per_1m', label: '输入', align: 'right' },
            { key: 'output_per_1m', label: '输出', align: 'right' },
            { key: 'cache_write_per_1m', label: '缓存写入', align: 'right' },
            { key: 'cache_read_per_1m', label: '缓存读取', align: 'right' },
          ]"
          :rows="board.prices"
          row-key="model"
        >
          <template #cell-model="{ row }"><span class="font-mono text-[12.5px] text-ink-900">{{ row.model }}</span></template>
          <template #cell-input_per_1m="{ row }">${{ row.input_per_1m.toFixed(2) }}</template>
          <template #cell-output_per_1m="{ row }">${{ row.output_per_1m.toFixed(2) }}</template>
          <template #cell-cache_write_per_1m="{ row }">${{ row.cache_write_per_1m.toFixed(2) }}</template>
          <template #cell-cache_read_per_1m="{ row }">${{ row.cache_read_per_1m.toFixed(2) }}</template>
        </DataTable>
      </div>
    </section>
  </div>
</template>
