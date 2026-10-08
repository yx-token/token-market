<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { buyer } from '@/api'
import type { BillDay } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatCard from '@/components/StatCard.vue'
import { money } from '@/utils/format'

const days = ref(30)
const bills = useLoader(() => buyer.bills(days.value), [] as BillDay[] | null)
onMounted(bills.load)
const total = computed(() => (bills.data.value ?? []).reduce((s, b) => s + b.cost, 0))
const requests = computed(() => (bills.data.value ?? []).reduce((s, b) => s + b.requests, 0))
</script>

<template>
  <div>
    <PageHeader title="账单" description="按日汇总的消费。">
      <select v-model.number="days" class="input w-32" aria-label="时间范围" @change="bills.load()">
        <option :value="7">近 7 天</option>
        <option :value="30">近 30 天</option>
        <option :value="90">近 90 天</option>
      </select>
    </PageHeader>
    <div class="mb-5 grid grid-cols-2 gap-4">
      <StatCard label="合计消费" :value="money(total)" />
      <StatCard label="请求数" :value="requests" />
    </div>
    <div class="card">
      <DataTable
        :columns="[
          { key: 'day', label: '日期' },
          { key: 'requests', label: '请求数', align: 'right' },
          { key: 'cost', label: '消费', align: 'right' },
        ]"
        :rows="bills.data.value"
        :loading="bills.loading.value"
        row-key="day"
      >
        <template #cell-cost="{ row }">{{ money(row.cost) }}</template>
      </DataTable>
    </div>
  </div>
</template>
