<script setup lang="ts">
import { onMounted } from 'vue'
import { market } from '@/api'
import type { MarketItem } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import StatCard from '@/components/StatCard.vue'
import FlapNumber from '@/components/FlapNumber.vue'
import GradeMark from '@/components/GradeMark.vue'
import { authTypeText, label } from '@/utils/labels'
import { dateTime, ms, percent } from '@/utils/format'

const props = defineProps<{ id: string }>()
const item = useLoader(() => market.listing(Number(props.id)), null as MarketItem | null)
onMounted(item.load)
</script>

<template>
  <div v-if="item.data.value" class="mx-auto max-w-4xl">
    <PageHeader
      :kicker="`挂单 · No.${String(item.data.value.listing_id).padStart(4, '0')}`"
      :title="label('platform', item.data.value.platform).text"
      :description="`${authTypeText(item.data.value.platform, item.data.value.auth_type)}${item.data.value.tier ? ` · ${item.data.value.tier}` : ''} · 上架于 ${dateTime(item.data.value.listed_since)}`"
    >
      <RouterLink :to="{ path: '/buyer/keys', query: { listing: item.data.value.listing_id } }" class="btn-primary">用这个卖家创建 Key<span aria-hidden="true">→</span></RouterLink>
    </PageHeader>

    <div class="grid grid-cols-1 gap-5 md:grid-cols-[1.1fr_1fr]">
      <div class="bg-grid-ink flex flex-col justify-between border border-ink-900 bg-ink-900 p-6 text-ink-200 shadow-hard-red">
        <div class="flex items-center justify-between">
          <span class="font-mono text-[10.5px] uppercase tracking-[0.2em] text-ink-400">买家价 · 官方价倍率</span>
          <StatusBadge kind="health" :value="item.data.value.health" />
        </div>
        <div class="my-8 text-[44px]"><FlapNumber :value="`${item.data.value.price.toFixed(2)}×`" /></div>
        <p class="font-mono text-[11.5px] text-ink-400">按官方价的 {{ Math.round(item.data.value.price * 100) }}% 计费 · 失败的请求不计费</p>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <StatCard label="24h 成功率" :value="percent(item.data.value.success_rate_24h)" :hint="`${item.data.value.requests_24h} 次请求`" />
        <StatCard label="首字延迟 · 中位" :value="ms(item.data.value.ttft_p50_ms)" />
        <div class="stat">
          <div class="stat-label">质量等级</div>
          <div class="mt-2"><GradeMark :grade="item.data.value.grade" /></div>
          <div class="hint">按平台检测的智力相对分定级</div>
        </div>
        <StatCard label="订阅档位" :value="item.data.value.tier ?? '—'" />
      </div>
    </div>

    <div class="callout mt-6">
      指定卖家时可以再追加其他卖家或默认分组作为备用：当前卖家不可用时，请求会按顺序切换，失败的请求不计费。
    </div>
  </div>
</template>
