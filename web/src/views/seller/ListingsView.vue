<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { seller } from '@/api'
import type { Listing, PriceVersion } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import { dateTime, multiplier } from '@/utils/format'

const cfg = useConfig()
const listings = useLoader(() => seller.listings(), [] as Listing[] | null)
const history = ref<PriceVersion[] | null>(null)
const historyFor = ref(0)
const { run } = useAction()

onMounted(() => {
  listings.load()
  cfg.ensure()
})

async function showHistory(id: number) {
  const res = await run(() => seller.listing(id))
  if (res) {
    historyFor.value = id
    history.value = res.price_history ?? []
  }
}
</script>

<template>
  <div>
    <PageHeader title="我的供给" description="在上游详情页创建挂单、改价、暂停或关闭。可挂「按量通道」或「额度包」。">
      <RefreshButton :loading="listings.loading.value" @click="listings.load()" />
      <RouterLink to="/seller/publish" class="xm-btn xm-btn-primary">发布供给 →</RouterLink>
      <RouterLink to="/seller/upstreams" class="xm-btn xm-btn-ghost">我的上游 →</RouterLink>
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '挂单' },
          { key: 'upstream_id', label: '上游' },
          { key: 'mode', label: '形态' },
          { key: 'price', label: '价格' },
          { key: 'status', label: '状态' },
          { key: 'created_at', label: '创建时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="listings.data.value"
        :loading="listings.loading.value"
        row-key="id"
        empty="还没有挂单，去「我的上游」或「发布供给」"
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
            {{ row.mode === 'pack' ? '额度包' : row.mode === 'buyback' ? '回收' : '按量' }}
          </span>
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
          <RouterLink :to="`/seller/upstreams/${row.upstream_id}?from=listings`" class="xm-btn xm-btn-ghost xm-btn-sm">管理 →</RouterLink>
          <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="showHistory(row.id)">价格历史</button>
        </template>
      </DataTable>
    </div>
    <ModalDialog :open="history !== null" :title="`挂单 #${historyFor} 价格历史`" @update:open="(v: boolean) => !v && (history = null)">
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
