<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { buyer } from '@/api'
import type { BillDay } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatCard from '@/components/StatCard.vue'
import BuyerCostBreakdown from '@/components/BuyerCostBreakdown.vue'
import { money, usd } from '@/utils/format'

const days = ref(30)
const bills = useLoader(() => buyer.bills(days.value), [] as BillDay[] | null)
onMounted(bills.load)
const total = computed(() => (bills.data.value ?? []).reduce((s, b) => s + b.cost, 0))
const requests = computed(() => (bills.data.value ?? []).reduce((s, b) => s + b.requests, 0))
const cash = computed(() => (bills.data.value ?? []).reduce((s, b) => s + (b.cash_cost ?? b.cost), 0))
const creditUSD = computed(() => (bills.data.value ?? []).reduce((s, b) => s + (b.credit_covered_usd ?? 0), 0))
const prepaid = computed(() => (bills.data.value ?? []).reduce((s, b) => s + (b.credit_prepaid_cny ?? 0), 0))
</script>

<template>
  <div>
    <PageHeader title="账单" description="按日汇总实际消费。购买活动包时预存的人民币，在使用时计入消费；美元抵扣额度单独展示。">
      <select v-model.number="days" class="input w-32" aria-label="时间范围" @change="bills.load()">
        <option :value="7">近 7 天</option>
        <option :value="30">近 30 天</option>
        <option :value="90">近 90 天</option>
      </select>
    </PageHeader>
    <div class="mb-5 grid grid-cols-2 gap-4 lg:grid-cols-4">
      <StatCard label="实际消费" :value="money(total)" hint="活动预付消耗 + 平台余额扣款" />
      <StatCard label="平台余额扣款" :value="money(cash)" />
      <StatCard label="活动额度抵扣" :value="usd(creditUSD)" :hint="`消耗预付款 ${money(prepaid)}`" />
      <StatCard label="请求数" :value="requests" />
    </div>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'day', label: '日期' },
          { key: 'requests', label: '请求数', align: 'right' },
          { key: 'cost', label: '实际消费', align: 'right' },
        ]"
        :rows="bills.data.value"
        :loading="bills.loading.value"
        row-key="day"
      >
        <template #cell-cost="{ row }"><BuyerCostBreakdown :value="row" :digits="2" /></template>
      </DataTable>
    </div>
  </div>
</template>
