<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { merchants, type BuyerMerchant } from '@/api/merchants'
import { useToast } from '@/stores/toast'
import { dateTime, multiplier } from '@/utils/format'
import { label } from '@/utils/labels'
import PageHeader from '@/components/PageHeader.vue'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'

const toast = useToast()
const items = ref<BuyerMerchant[]>([])
const loading = ref(false)
const failed = ref(false)
const removing = ref<number | null>(null)

async function load() {
  loading.value = true
  failed.value = false
  try {
    items.value = await merchants.list() ?? []
  } catch (e) {
    failed.value = true
    toast.error(e)
  } finally {
    loading.value = false
  }
}

async function remove(item: BuyerMerchant) {
  if (removing.value !== null) return
  if (!confirm(`从清单移除商家 #${item.seller_id}？现有 Key 将不再调用该商家的挂单，其他挂单与平台兜底继续按原顺序调用。`)) return
  removing.value = item.seller_id
  try {
    await merchants.remove(item.seller_id)
    items.value = items.value.filter((entry) => entry.seller_id !== item.seller_id)
    toast.success('已移除商家，相关 Key 路由已更新')
  } catch (e) {
    toast.error(e)
  } finally {
    removing.value = null
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="商家清单" description="在市场挑选商家并加入清单，再到 API Key 页面选择挂单和调用顺序。商家暂时没有可用挂单时仍会保留在这里。">
      <RouterLink to="/" class="xm-btn xm-btn-primary">去市场挑商家</RouterLink>
      <RouterLink to="/buyer/keys" class="xm-btn xm-btn-ghost">配置 API Key</RouterLink>
    </PageHeader>

    <p class="xm-yellow-callout">移除商家后，现有 Key 会停止调用该商家的挂单，其他挂单与平台默认分组继续按原顺序调用。</p>
    <div v-if="loading && !items.length" class="xm-key-card py-10 text-center text-slate-500" role="status">正在加载商家清单…</div>
    <div v-else-if="failed" class="xm-key-card py-10 text-center" role="alert">
      <p>商家清单加载失败，请重试。</p>
      <button type="button" class="xm-btn xm-btn-primary mt-3" @click="load">重新加载</button>
    </div>
    <div v-else-if="!items.length" class="xm-key-card py-10 text-center">
      <h2 class="font-semibold">还没有加入商家</h2>
      <p class="mt-2 text-sm text-slate-600">先去市场挑选，加入后便能在 Key 中选择这家商家的可用挂单。</p>
      <RouterLink to="/" class="xm-btn xm-btn-primary mt-4">浏览市场</RouterLink>
    </div>
    <div v-else class="space-y-4" :aria-busy="loading">
      <section v-for="merchant in items" :key="merchant.seller_id" class="xm-key-card">
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-black/10 pb-4">
          <div>
            <h2 class="text-lg font-bold">商家 #{{ merchant.seller_id }}</h2>
            <p class="mt-1 text-xs text-slate-500">加入于 {{ dateTime(merchant.added_at) }} · {{ merchant.listings?.length ?? 0 }} 个可用挂单</p>
          </div>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm xm-btn-danger" :disabled="removing !== null" @click="remove(merchant)">
            {{ removing === merchant.seller_id ? '移除中…' : '移除商家' }}
          </button>
        </div>
        <p v-if="!merchant.listings?.length" class="py-5 text-sm text-slate-600">该商家暂无可用挂单。上架恢复后可在此选择，也可继续去市场挑选其他商家。</p>
        <ul v-else class="divide-y divide-black/10">
          <li v-for="listing in merchant.listings" :key="listing.listing_id" class="flex flex-wrap items-start justify-between gap-4 py-4">
            <div class="min-w-0 flex-1">
              <h3 class="font-semibold"><RouterLink :to="`/market/listings/${listing.listing_id}`" class="underline underline-offset-4">{{ listing.title || `挂单 #${listing.listing_id}` }}</RouterLink></h3>
              <p class="my-2 text-sm text-slate-600">#{{ listing.listing_id }} · {{ label('platform', listing.platform).text }} · {{ multiplier(listing.price) }} 官方价</p>
              <MarketDetectionSummary :status="listing.detection_status" :models="listing.models" :checks="listing.model_checks" :last-checked-at="listing.last_checked_at" />
            </div>
            <RouterLink :to="{ path: '/buyer/keys', query: { listing: listing.listing_id } }" class="xm-btn xm-btn-primary xm-btn-sm">用于 API Key</RouterLink>
          </li>
        </ul>
      </section>
    </div>
  </div>
</template>
