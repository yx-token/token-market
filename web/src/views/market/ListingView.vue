<script setup lang="ts">
import { onMounted } from 'vue'
import { market } from '@/api'
import type { MarketItem } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import StatCard from '@/components/StatCard.vue'
import { label } from '@/utils/labels'
import { dateTime, ms, multiplier, percent } from '@/utils/format'

const props = defineProps<{ id: string }>()
const item = useLoader(() => market.listing(Number(props.id)), null as MarketItem | null)
onMounted(item.load)
</script>

<template>
  <div v-if="item.data.value" class="mx-auto max-w-3xl">
    <PageHeader :title="`挂单 #${item.data.value.listing_id}`" :description="`${label('platform', item.data.value.platform).text} · ${label('authType', item.data.value.auth_type).text}`">
      <RouterLink :to="{ path: '/buyer/keys', query: { listing: item.data.value.listing_id } }" class="btn-primary">用这个卖家创建 Key</RouterLink>
    </PageHeader>
    <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
      <StatCard label="价格（官方价倍率）" :value="multiplier(item.data.value.price)" />
      <StatCard label="24h 成功率" :value="percent(item.data.value.success_rate_24h)" :hint="`${item.data.value.requests_24h} 次请求`" />
      <StatCard label="首字延迟（中位数）" :value="ms(item.data.value.ttft_p50_ms)" />
      <StatCard label="质量等级" :value="item.data.value.grade ? `${item.data.value.grade} 级` : '—'" />
    </div>
    <div class="card mt-5 text-sm">
      <dl class="grid grid-cols-2 gap-3">
        <dt class="text-slate-500">状态</dt>
        <dd><StatusBadge kind="health" :value="item.data.value.health" /></dd>
        <dt class="text-slate-500">订阅档位</dt>
        <dd>{{ item.data.value.tier ?? '—' }}</dd>
        <dt class="text-slate-500">上架时间</dt>
        <dd>{{ dateTime(item.data.value.listed_since) }}</dd>
      </dl>
      <p class="mt-4 text-slate-500">指定卖家时可以再追加其他卖家或默认分组作为备用：当前卖家不可用时，请求会按顺序切换，失败的请求不计费。</p>
    </div>
  </div>
</template>
