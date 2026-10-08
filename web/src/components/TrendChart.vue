<script setup lang="ts">
// 质量趋势：柱高为当天检测次数，红色为降智或不可用次数，折线为智力相对分。
import { computed } from 'vue'
import type { TrendPoint } from '@/api/types'

const props = defineProps<{ points: TrendPoint[] }>()
const W = 600
const H = 140
const sorted = computed(() => [...props.points].sort((a, b) => a.day.localeCompare(b.day)))
const maxChecks = computed(() => Math.max(1, ...sorted.value.map((p) => p.checks)))
const step = computed(() => W / Math.max(1, sorted.value.length))
// 柱宽最多 24，天数少时居中
const barW = computed(() => Math.min(24, Math.max(2, step.value - 4)))
const barX = (i: number) => i * step.value + (step.value - barW.value) / 2
const line = computed(() =>
  sorted.value
    .map((p, i) => (p.relative_score === undefined ? null : `${i * step.value + step.value / 2},${H - Math.min(1.2, p.relative_score) / 1.2 * H}`))
    .filter(Boolean)
    .join(' '),
)
</script>

<template>
  <div>
    <svg :viewBox="`0 0 ${W} ${H}`" class="h-36 w-full" role="img" aria-label="质量趋势">
      <g v-for="(p, i) in sorted" :key="p.day">
        <rect :x="barX(i)" :y="H - (p.checks / maxChecks) * H * 0.6" :width="barW" :height="(p.checks / maxChecks) * H * 0.6" class="fill-slate-200">
          <title>{{ p.day }}：检测 {{ p.checks }} 次，异常 {{ p.degraded + p.unavailable }} 次</title>
        </rect>
        <rect
          v-if="p.degraded + p.unavailable > 0"
          :x="barX(i)"
          :y="H - ((p.degraded + p.unavailable) / maxChecks) * H * 0.6"
          :width="barW"
          :height="((p.degraded + p.unavailable) / maxChecks) * H * 0.6"
          class="fill-red-300"
        />
      </g>
      <polyline v-if="line" :points="line" fill="none" class="stroke-brand-600" stroke-width="2" />
    </svg>
    <div class="flex justify-between text-xs text-slate-400">
      <span>{{ sorted[0]?.day }}</span>
      <span>灰色：检测次数　红色：异常　蓝线：智力相对分</span>
      <span>{{ sorted[sorted.length - 1]?.day }}</span>
    </div>
  </div>
</template>
