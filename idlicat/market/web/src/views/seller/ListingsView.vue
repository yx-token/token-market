<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { seller } from '@/api'
import type { Listing, ListingCategoryOption, PriceVersion, Upstream } from '@/api/types'
import { detectionText } from '@/api/market-detection'
import { sellerUntested } from '@/components/seller/listingGate'
import '@/styles/seller-center.css'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime, multiplier } from '@/utils/format'

const cfg = useConfig()
const listingsError = ref('')
const listings = useLoader(async () => {
  try {
    const rows = (await seller.listings()) ?? []
    listingsError.value = ''
    return rows
  } catch (error) {
    listingsError.value = '挂单没有载入'
    throw error
  }
}, [] as Listing[])
const upstreams = ref<Upstream[]>([])
type StatusFilter = 'open' | 'active' | 'paused' | 'closed' | 'all'
const statusFilter = ref<StatusFilter>('open')
const modeFilter = ref('')
const statusChoices: { id: StatusFilter; label: string }[] = [
  { id: 'open', label: '在售' },
  { id: 'active', label: '生效中' },
  { id: 'paused', label: '已暂停' },
  { id: 'closed', label: '已下架' },
  { id: 'all', label: '全部' },
]
const history = ref<PriceVersion[] | null>(null)
const historyFor = ref(0)
const { busy, run } = useAction()

const priceOpen = ref(false)
const priceTarget = ref<Listing | null>(null)
const newPrice = ref('')

const metaOpen = ref(false)
const metaTarget = ref<Listing | null>(null)
const metaTitle = ref('')
const metaDesc = ref('')
const metaCats = ref<string[]>([])
const categoryOpts = ref<ListingCategoryOption[]>([])

const visible = computed(() =>
  (listings.data.value ?? []).filter((row) => {
    if (modeFilter.value && row.mode !== modeFilter.value) return false
    if (statusFilter.value === 'open') return row.status !== 'closed'
    if (statusFilter.value === 'all') return true
    return row.status === statusFilter.value
  }),
)
function upstreamOf(id: number) {
  return upstreams.value.find((u) => u.id === id)
}

onMounted(async () => {
  listings.load()
  cfg.ensure()
  const [cats, ups] = await Promise.all([seller.listingCategories().catch(() => null), seller.upstreams().catch(() => null)])
  categoryOpts.value = cats ?? []
  upstreams.value = ups ?? []
})

async function reload() {
  await listings.load()
  upstreams.value = (await seller.upstreams().catch(() => null)) ?? []
}

async function showHistory(id: number) {
  const res = await run(() => seller.listing(id))
  if (res) {
    historyFor.value = id
    history.value = res.price_history ?? []
  }
}

async function act(fn: () => Promise<unknown>, msg: string) {
  if (await run(fn, msg)) await reload()
}

function openPrice(row: Listing) {
  priceTarget.value = row
  newPrice.value = String(row.multiplier ?? '')
  priceOpen.value = true
}

async function savePrice() {
  const row = priceTarget.value
  if (!row) return
  const m = Number(newPrice.value)
  if (!(m > 0)) return
  if (await run(() => seller.changePrice(row.id, m), '已改价')) {
    priceOpen.value = false
    await reload()
  }
}

function openMeta(row: Listing) {
  metaTarget.value = row
  metaTitle.value = row.title ?? ''
  metaDesc.value = row.description ?? ''
  metaCats.value = [...(row.categories ?? [])].filter((c) => c !== 'pack')
  metaOpen.value = true
}

function toggleCat(id: string) {
  if (metaCats.value.includes(id)) metaCats.value = metaCats.value.filter((x) => x !== id)
  else metaCats.value = [...metaCats.value, id]
}

async function saveMeta() {
  const row = metaTarget.value
  if (!row) return
  if (!metaCats.value.length) return
  if (
    await run(
      () =>
        seller.updateListingMeta(row.id, {
          title: metaTitle.value.trim() || undefined,
          description: metaDesc.value.trim() || undefined,
          categories: metaCats.value,
        }),
      '已更新',
    )
  ) {
    metaOpen.value = false
    await reload()
  }
}

async function closeListing(row: Listing) {
  if (!confirm(`下架商品 #${row.id}？下架后买家将看不到，可再发布闲置TOKEN。`)) return
  await act(() => seller.close(row.id), '已下架')
}
</script>

<template>
  <div class="seller-page">
    <PageHeader title="我的闲置TOKEN" description="改价、改分类、暂停或下架。可挂「按量通道」或「订阅套餐」。未检测的寄售和订阅套餐可以挂，买家会看到未检测；它们不能进保障池。">
      <RefreshButton :loading="listings.loading.value" @click="reload()" />
      <RouterLink to="/seller/publish" class="xm-btn xm-btn-primary">＋ 发布闲置TOKEN</RouterLink>
      <RouterLink to="/seller/upstreams" class="xm-btn xm-btn-ghost">我的上游 →</RouterLink>
    </PageHeader>
    <div class="seller-filters" role="group" aria-label="按状态筛选">
      <button v-for="item in statusChoices" :key="item.id" type="button" class="seller-chip" :class="{ 'is-on': statusFilter === item.id }" @click="statusFilter = item.id">{{ item.label }}</button>
      <button v-for="item in [
        ['', '全部形态'],
        ['consignment', '按量'],
        ['pack', '订阅套餐'],
        ['buyback', '回收'],
      ]" :key="item[0] || 'all-mode'" type="button" class="seller-chip" :class="{ 'is-on': modeFilter === item[0] }" @click="modeFilter = item[0]">{{ item[1] }}</button>
    </div>
    <div v-if="listingsError && !listings.loading.value" class="seller-empty">
      <strong>挂单没有载入</strong>
      <p>请重试。空列表不是失败。</p>
      <button type="button" class="xm-btn xm-btn-primary" @click="reload">重试</button>
    </div>
    <div v-else class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '商品' },
          { key: 'upstream_id', label: '上游' },
          { key: 'mode', label: '形态' },
          { key: 'price', label: '价格' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '创建时间' },
          { key: 'actions', label: '操作' },
        ]"
        :rows="visible"
        :loading="listings.loading.value"
        row-key="id"
        :empty="listings.data.value.length ? '这个筛选下没有商品' : '还没有商品，去发布闲置TOKEN'"
      >
        <template #cell-id="{ row }">
          <span class="font-bold">#{{ row.id }}</span>
          <span v-if="row.title" class="ml-2 text-[#666]">{{ row.title }}</span>
        </template>
        <template #cell-upstream_id="{ row }">
          <RouterLink :to="`/seller/upstreams/${row.upstream_id}?from=listings`" class="font-bold underline underline-offset-2">#{{ row.upstream_id }} →</RouterLink>
        </template>
        <template #cell-mode="{ row }">
          <span class="xm-tag" :class="row.mode === 'pack' ? 'pack' : ''">
            {{ row.mode === 'pack' ? '订阅套餐' : row.mode === 'buyback' ? '回收' : '按量' }}
          </span>
          <span v-if="sellerUntested(upstreamOf(row.upstream_id) ?? row)" class="xm-tag warn">{{ detectionText(row.detection_status) }}</span>
        </template>
        <template #cell-price="{ row }">
          <template v-if="row.mode === 'pack'">
            ¥{{ Number(row.face_value ?? 0).toFixed(0) }} · 剩 {{ Math.max(0, (row.stock ?? 0) - (row.stock_sold ?? 0)) }} 份
            <span class="text-[#888]"> · {{ multiplier(row.multiplier) }}</span>
          </template>
          <template v-else-if="row.mode === 'consignment' && row.multiplier">
            {{ multiplier(row.multiplier) }} · 买家 {{ multiplier(row.multiplier * (1 + cfg.config.commission_rate)) }}
            <span v-if="(row.min_amount ?? 0) === 0" class="text-[#888]"> · M=0</span>
            <span v-else class="text-[#888]"> · M=¥{{ Number(row.min_amount).toFixed(0) }}</span>
          </template>
          <template v-else>{{ multiplier(row.multiplier) }}</template>
        </template>
        <template #cell-status="{ row }"><StatusBadge kind="listingStatus" :value="row.status" /></template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1.5">
            <RouterLink :to="`/seller/upstreams/${row.upstream_id}?from=listings`" class="xm-btn xm-btn-ghost xm-btn-sm">详情</RouterLink>
            <template v-if="(row.mode === 'consignment' || row.mode === 'pack') && ['active', 'paused'].includes(row.status)">
              <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy" @click="openPrice(row)">改价</button>
              <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy" @click="openMeta(row)">编辑</button>
            </template>
            <button
              v-if="row.status === 'active'"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm"
              :disabled="busy"
              @click="act(() => seller.pause(row.id), '已暂停')"
            >暂停</button>
            <button
              v-if="row.status === 'paused'"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm"
              :disabled="busy"
              @click="act(() => seller.resume(row.id), '已恢复')"
            >恢复</button>
            <button
              v-if="row.status !== 'closed'"
              type="button"
              class="xm-btn xm-btn-ghost xm-btn-sm text-red-600"
              :disabled="busy"
              @click="closeListing(row)"
            >下架</button>
            <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="showHistory(row.id)">价格历史</button>
          </div>
        </template>
      </DataTable>
    </div>

    <ModalDialog v-model:open="priceOpen" :title="`改价 · 商品 #${priceTarget?.id ?? ''}`">
      <div>
        <label class="label" for="listing-price">你的倍率</label>
        <input id="listing-price" v-model="newPrice" class="input" inputmode="decimal" />
        <p class="hint">买家价 = 倍率 × (1 + 平台抽佣)</p>
      </div>
      <template #footer>
        <button type="button" class="xm-btn xm-btn-ghost" @click="priceOpen = false">取消</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="savePrice">保存</button>
      </template>
    </ModalDialog>

    <ModalDialog v-model:open="metaOpen" :title="`编辑 · 商品 #${metaTarget?.id ?? ''}`">
      <div class="grid gap-3">
        <div>
          <label class="label" for="listing-title">展示标题</label>
          <input id="listing-title" v-model="metaTitle" class="input" placeholder="留空则自动生成" />
        </div>
        <div>
          <label class="label" for="listing-desc">短描述</label>
          <textarea id="listing-desc" v-model="metaDesc" class="input" rows="2" />
        </div>
        <div>
          <p class="label mb-1">市场分类（至少选一个）</p>
          <div class="flex flex-wrap gap-3 text-sm">
            <label v-for="c in categoryOpts" :key="c.id" class="flex items-center gap-1">
              <input type="checkbox" :checked="metaCats.includes(c.id)" @change="toggleCat(c.id)" />
              {{ c.path || c.name }}
            </label>
            <span v-if="!categoryOpts.length" class="text-[#888]">暂无可选分类</span>
          </div>
        </div>
      </div>
      <template #footer>
        <button type="button" class="xm-btn xm-btn-ghost" @click="metaOpen = false">取消</button>
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy || !metaCats.length" @click="saveMeta">保存</button>
      </template>
    </ModalDialog>

    <ModalDialog :open="history !== null" :title="`商品 #${historyFor} 价格历史`" @update:open="(v: boolean) => !v && (history = null)">
      <DataTable
        :columns="[
          { key: 'effective_from', label: '生效时间' },
          { key: 'multiplier', label: '倍率', align: 'right' },
          { key: 'buyer_price', label: '买家价', align: 'right' },
          { key: 'source', label: '来源' },
        ]"
        :rows="history"
      >
        <template #cell-effective_from="{ row }">{{ dateTime(row.effective_from) }}</template>
        <template #cell-multiplier="{ row }">{{ multiplier(row.multiplier) }}</template>
        <template #cell-buyer_price="{ row }">{{ multiplier(row.buyer_price) }}</template>
        <template #cell-source="{ row }">{{ ({ seller: '卖家定价', quote: '平台报价', requote: '平台续报价', admin: '平台调整' } as Record<string, string>)[row.source] ?? row.source }}</template>
      </DataTable>
    </ModalDialog>
  </div>
</template>
