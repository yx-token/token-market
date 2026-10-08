<script setup lang="ts">
// 登录 / 注册版式：左侧墨色宣言页（印章、行情摘要），右侧纸面表单。
import { onMounted } from 'vue'
import { useMarketBoard } from '@/stores/marketBoard'
import SealMark from '@/components/SealMark.vue'
import { label } from '@/utils/labels'

defineProps<{ title: string; subtitle?: string }>()
const board = useMarketBoard()
onMounted(board.start)
</script>

<template>
  <div class="mx-auto grid grid-cols-1 max-w-5xl overflow-hidden border border-ink-900 bg-white shadow-hard md:grid-cols-[1.05fr_1fr]">
    <div class="bg-grid-ink relative hidden flex-col justify-between bg-ink-900 p-10 text-ink-200 md:flex">
      <div>
        <SealMark :size="64" animate />
        <p class="mt-10 font-display text-[30px] font-bold leading-[1.35] text-white">
          用不完的订阅，<br />有人正需要。
        </p>
        <p class="mt-4 max-w-xs text-[13.5px] leading-[1.85] text-ink-300">
          同一个账号既能买也能卖：按官方价打折调用大模型，或者把闲置额度挂出来按小时结算收益。
        </p>
      </div>
      <div class="mt-10 border-t border-ink-700 pt-5">
        <div class="mb-3 font-mono text-[10px] uppercase tracking-[0.2em] text-ink-400">此刻 · 最低买家价</div>
        <div v-if="board.board.length" class="space-y-2">
          <div v-for="r in board.board.slice(0, 4)" :key="r.platform" class="flex items-baseline justify-between border-b border-dashed border-ink-700 pb-2">
            <span class="text-[13.5px] text-white">{{ label('platform', r.platform).text }}</span>
            <span class="font-mono text-[15px] text-brand-400">{{ r.best.toFixed(2) }}×</span>
          </div>
        </div>
        <p v-else class="font-mono text-xs text-ink-400">等待第一笔挂单</p>
      </div>
    </div>
    <div class="p-8 md:p-12">
      <div class="kicker mb-3 flex items-center gap-2"><span class="h-px w-6 bg-brand-500" />闲置算力交易所</div>
      <h1 class="font-display text-[30px] font-bold tracking-[0.04em]">{{ title }}</h1>
      <p v-if="subtitle" class="mt-2 text-[13.5px] text-ink-500">{{ subtitle }}</p>
      <div class="mt-8"><slot /></div>
    </div>
  </div>
</template>
