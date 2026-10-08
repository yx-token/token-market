<script setup lang="ts">
// 每日用量柱状图：可切换收益、请求数、token；买家流量与平台检测分色显示（请求数）。
import { computed, ref } from 'vue'
import type { UsageTotals } from '@/api/types'
import { money, tokens } from '@/utils/format'

const props = defineProps<{ days: ({ day: string } & UsageTotals)[] }>()
const metric = ref<'payout' | 'requests' | 'tokens'>('payout')
const W = 720
const H = 160

const value = (d: UsageTotals) =>
  metric.value === 'payout' ? d.payout : metric.value === 'requests' ? d.requests : d.input_tokens + d.output_tokens + d.cache_creation_tokens + d.cache_read_tokens
const max = computed(() => Math.max(1e-9, ...props.days.map(value)))
const step = computed(() => W / Math.max(1, props.days.length))
const barW = computed(() => Math.min(28, Math.max(2, step.value - 4)))
const x = (i: number) => i * step.value + (step.value - barW.value) / 2
const h = (v: number) => (v / max.value) * (H - 8)
const fmt = (d: UsageTotals) =>
  metric.value === 'payout' ? money(d.payout) : metric.value === 'requests' ? `${d.requests} 次（买家 ${d.buyer_requests}，检测 ${d.probe_requests}）` : tokens(value(d))
</script>

<template>
  <div>
    <div class="mb-2 flex gap-1 text-xs">
      <button
        v-for="m in [
          { k: 'payout', t: '收益' },
          { k: 'requests', t: '请求数' },
          { k: 'tokens', t: 'Token' },
        ]"
        :key="m.k"
        type="button"
        :class="['rounded px-2 py-1', metric === m.k ? 'bg-slate-800 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200']"
        @click="metric = m.k as typeof metric"
      >
        {{ m.t }}
      </button>
    </div>
    <svg :viewBox="`0 0 ${W} ${H}`" class="h-40 w-full" role="img" aria-label="每日用量">
      <line :x1="0" :x2="W" :y1="H - 0.5" :y2="H - 0.5" class="stroke-slate-200" />
      <g v-for="(d, i) in days" :key="d.day">
        <template v-if="metric === 'requests'">
          <rect :x="x(i)" :y="H - h(d.requests)" :width="barW" :height="h(d.requests)" class="fill-slate-300" />
          <rect :x="x(i)" :y="H - h(d.buyer_requests)" :width="barW" :height="h(d.buyer_requests)" class="fill-brand-500" />
        </template>
        <rect v-else :x="x(i)" :y="H - h(value(d))" :width="barW" :height="h(value(d))" class="fill-brand-500" />
        <rect :x="i * step" y="0" :width="step" :height="H" fill="transparent">
          <title>{{ d.day }}：{{ fmt(d) }}</title>
        </rect>
      </g>
    </svg>
    <div class="flex justify-between text-xs text-slate-400">
      <span>{{ days[0]?.day }}</span>
      <span v-if="metric === 'requests'">蓝色：买家请求　灰色：平台检测</span>
      <span>{{ days[days.length - 1]?.day }}</span>
    </div>
  </div>
</template>
