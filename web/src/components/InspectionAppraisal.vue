<script setup lang="ts">
import type { InspectionAppraisal } from '@/api/types'
import { appraisalDeviation, appraisalText } from '@/utils/inspection'
import { dateTime } from '@/utils/format'

defineProps<{ appraisal?: InspectionAppraisal; grade?: string; qualityFactor?: number | null; inspectionMode?: string; price?: number }>()
</script>

<template>
  <div class="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm" data-testid="inspection-appraisal">
    <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
      <span>质量等级 <strong>{{ grade || '—' }}</strong></span>
      <span>质检估价 <strong>{{ appraisalText(appraisal) }}</strong></span>
      <span v-if="qualityFactor !== undefined">质量系数 Q {{ qualityFactor === null ? '无法判定' : qualityFactor.toFixed(3) }}</span>
      <span v-if="price !== undefined">你的倍率相对估价偏离 {{ appraisalDeviation(price, appraisal) }}</span>
    </div>
    <p v-if="inspectionMode === 'trial'" class="mt-2 text-amber-800">试运行，不影响价格。等级、回收报价与调度沿用现有判定。</p>
    <p v-else-if="appraisal?.provisional" class="mt-2 text-amber-800">估价仅供参考，本次检测覆盖不足。</p>
    <p class="mt-1 text-xs text-slate-500">估价为卖家侧倍率，不含抽佣；可自主定价。{{ appraisal?.checked_at ? `上次检测 ${dateTime(appraisal.checked_at)}` : '尚无估价检测时间' }}</p>
  </div>
</template>
