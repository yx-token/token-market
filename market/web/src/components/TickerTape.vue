<script setup lang="ts">
// 顶部滚动行情条：各平台最低买家价与在售数量、默认分组价格、官方价目。悬停暂停。
import { computed, onMounted } from 'vue'
import { useMarketBoard } from '@/stores/marketBoard'
import { label } from '@/utils/labels'
import { multiplier, percent } from '@/utils/format'

const board = useMarketBoard()
onMounted(board.start)

interface Tick {
  key: string
  name: string
  value: string
  note?: string
  accent?: boolean
}
const ticks = computed<Tick[]>(() => {
  const out: Tick[] = []
  for (const r of board.board) {
    out.push({
      key: `b-${r.platform}`,
      name: label('platform', r.platform).text,
      value: multiplier(r.best),
      note: `${r.count} 家在售${r.successRate !== null ? ` · 成功率 ${percent(r.successRate, 0)}` : ''}`,
      accent: true,
    })
  }
  for (const p of board.pools) out.push({ key: `p-${p.name}`, name: `默认分组 ${label('platform', p.platform).text}`, value: multiplier(p.price) })
  for (const p of board.prices.slice(0, 8))
    out.push({ key: `o-${p.model}`, name: p.model, value: `$${p.input_per_1m.toFixed(2)} / $${p.output_per_1m.toFixed(2)}`, note: '官方价 每百万' })
  if (!out.length) out.push({ key: 'idle', name: '行情', value: '等待第一笔挂单', note: '卖家挂出闲置额度后在这里显示最低价' })
  return out
})
</script>

<template>
  <div class="group relative flex h-8 items-stretch overflow-hidden bg-ink-900 text-[12px] text-ink-200">
    <div class="z-10 flex shrink-0 items-center gap-2 border-r border-ink-700 bg-ink-900 px-3 font-mono text-[10.5px] uppercase tracking-[0.18em] text-white">
      <span class="h-1.5 w-1.5 animate-blink rounded-full bg-brand-500" />
      实时行情
    </div>
    <div class="relative min-w-0 flex-1 overflow-hidden">
      <div class="flex w-max animate-ticker items-center group-hover:[animation-play-state:paused]" :style="{ animationDuration: `${Math.max(30, ticks.length * 7)}s` }">
        <template v-for="copy in 2" :key="copy">
          <div v-for="t in ticks" :key="`${copy}-${t.key}`" class="flex items-center gap-2 whitespace-nowrap px-5" :aria-hidden="copy === 2">
            <span class="text-ink-300">{{ t.name }}</span>
            <span :class="['font-mono font-medium', t.accent ? 'text-brand-400' : 'text-white']">{{ t.value }}</span>
            <span v-if="t.note" class="font-mono text-[11px] text-ink-400">{{ t.note }}</span>
            <span class="ml-3 text-ink-600">／</span>
          </div>
        </template>
      </div>
      <div class="pointer-events-none absolute inset-y-0 left-0 w-8 bg-gradient-to-r from-ink-900" />
      <div class="pointer-events-none absolute inset-y-0 right-0 w-12 bg-gradient-to-l from-ink-900" />
    </div>
  </div>
</template>
