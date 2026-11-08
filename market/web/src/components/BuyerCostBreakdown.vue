<script setup lang="ts">
import type { BuyerCostAllocation } from '@/api/types'
import { money } from '@/utils/format'

defineProps<{ value: BuyerCostAllocation & { cost: number }; digits?: number }>()
</script>

<template>
  <div class="tabular-nums">
    <div>{{ money(value.cost, digits ?? 4) }}</div>
    <div v-if="(value.credit_covered_cny ?? 0) > 0" class="mt-1 whitespace-nowrap text-xs leading-relaxed text-[#777]">
      <div>活动额度 {{ money(value.credit_covered_cny, 4) }} · 预付 {{ money(value.credit_prepaid_cny, 4) }}</div>
      <div>平台余额 {{ money(value.cash_cost, 4) }}</div>
    </div>
  </div>
</template>
