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
    <PageHeader title="挂单" description="在上游详情页创建挂单、改价、暂停或关闭。">
      <RefreshButton :loading="listings.loading.value" @click="listings.load()" />
    </PageHeader>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'id', label: '挂单' },
          { key: 'upstream_id', label: '上游' },
          { key: 'mode', label: '模式' },
          { key: 'status', label: '状态' },
          { key: 'multiplier', label: '你的倍率', align: 'right' },
          { key: 'buyer', label: '买家价', align: 'right' },
          { key: 'join_default_pool', label: '默认分组' },
          { key: 'created_at', label: '创建时间' },
          { key: 'actions', label: '' },
        ]"
        :rows="listings.data.value"
        :loading="listings.loading.value"
        row-key="id"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-upstream_id="{ row }">
          <RouterLink :to="`/seller/upstreams/${row.upstream_id}`" class="link">#{{ row.upstream_id }}</RouterLink>
        </template>
        <template #cell-mode="{ row }"><StatusBadge kind="mode" :value="row.mode" /></template>
        <template #cell-status="{ row }"><StatusBadge kind="listingStatus" :value="row.status" /></template>
        <template #cell-multiplier="{ row }">{{ multiplier(row.multiplier) }}</template>
        <template #cell-buyer="{ row }">{{ row.mode === 'consignment' && row.multiplier ? multiplier(row.multiplier * (1 + cfg.config.commission_rate)) : '—' }}</template>
        <template #cell-join_default_pool="{ row }">{{ row.mode === 'buyback' ? '是' : row.join_default_pool ? '是' : '否' }}</template>
        <template #cell-created_at="{ row }">{{ dateTime(row.created_at) }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-ghost btn-sm" @click="showHistory(row.id)">价格历史</button>
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
