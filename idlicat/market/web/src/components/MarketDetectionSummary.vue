<script setup lang="ts">
import { computed } from 'vue'
import { degradationText, detectionText, isTested, modelDetectionRows, type DetectionStatus, type ModelCheck } from '@/api/market-detection'
import { dateTime } from '@/utils/format'

const props = defineProps<{
  status?: DetectionStatus
  models?: string[]
  checks?: ModelCheck[]
  lastCheckedAt?: string | null
}>()
const rows = computed(() => modelDetectionRows(props.models, props.checks))
</script>

<template>
  <div class="text-xs leading-relaxed text-slate-600">
    <p class="font-semibold" :class="isTested(status) ? 'text-slate-700' : 'text-amber-800'">质量检测：{{ detectionText(status) }}</p>
    <p v-for="row in rows" :key="row.model" class="break-all">
      {{ row.model }} · {{ detectionText(row.detection_status) }}<template v-if="row.model === 'gpt-6-astra'"> · 降智检测：{{ degradationText(row.degradation_status) }}</template>
    </p>
    <p v-if="isTested(status) && lastCheckedAt">上次检测：{{ dateTime(lastCheckedAt) }}</p>
    <p v-if="!isTested(status)">卖家已授权上架，尚无有效检测结果。</p>
  </div>
</template>
