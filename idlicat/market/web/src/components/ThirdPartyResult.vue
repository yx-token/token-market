<script setup lang="ts">
// 一条第三方检测结果：检测站的身份判定、折算后的结论、探针统计、警告与报告链接。
import type { ThirdPartySummary } from '@/api/types'
import StatusBadge from '@/components/StatusBadge.vue'
import { tokens } from '@/utils/format'

defineProps<{
  provider: string
  status: string
  outcome?: string
  verdict?: string
  model?: string
  reportUrl?: string
  error?: string
  summary?: ThirdPartySummary | null
}>()

const providerNames: Record<string, string> = { bazaarlink: 'BazaarLink' }
const verdictStatusText: Record<string, string> = {
  clean_match: '家族与具体型号都与声明一致',
  clean_match_family_only: '家族一致，具体型号无法确定',
  clean_match_submodel_mismatch: '家族一致，但不是声明的具体型号',
  plain_mismatch: '证据指向其他模型家族',
  spoof_behavior_induced: '行为被诱导成声明的模型，指纹指向其他模型',
  spoof_selfclaim_forged: '自称是声明的模型，但行为指向其他模型',
  ambiguous: '证据相互矛盾，无法判定',
  insufficient_data: '有效证据不足',
}
const severityTone: Record<string, string> = { critical: 'text-red-600', warning: 'text-amber-600', info: 'text-slate-500' }
</script>

<template>
  <div class="rounded-xl border border-gray-200 border-l-[3px] border-l-[#0555ce] bg-white p-3 text-sm shadow-sm console-tp-card">
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-medium">{{ providerNames[provider] ?? provider }}</span>
      <StatusBadge kind="tpStatus" :value="status" />
      <StatusBadge v-if="outcome" kind="tpOutcome" :value="outcome" />
      <span v-if="verdict" class="text-slate-500">折算结论</span>
      <StatusBadge v-if="verdict" kind="verdict" :value="verdict" />
      <span v-if="model" class="text-slate-400">模型 {{ model }}</span>
      <a v-if="reportUrl" :href="reportUrl" target="_blank" rel="noopener noreferrer" class="link ml-auto">查看检测站报告</a>
    </div>
    <p v-if="error" class="mt-2 text-red-600">{{ error }}</p>
    <dl v-if="summary" class="kv mt-2">
      <template v-if="summary.verdict_status">
        <dt>身份判定</dt>
        <dd>{{ verdictStatusText[summary.verdict_status] ?? summary.verdict_status }}</dd>
      </template>
      <template v-if="summary.true_model || summary.true_family">
        <dt>检测站认为实际是</dt>
        <dd>{{ summary.true_model || summary.true_family }}</dd>
      </template>
      <template v-if="summary.confidence !== undefined">
        <dt>置信度</dt>
        <dd>{{ summary.confidence }}</dd>
      </template>
      <template v-if="summary.items">
        <dt>探针</dt>
        <dd>
          共 {{ summary.items.total ?? 0 }} 道：通过 {{ summary.items.passed ?? 0 }}，未通过 {{ summary.items.failed ?? 0 }}，警告
          {{ summary.items.warning ?? 0 }}，出错 {{ summary.items.error ?? 0 }}
        </dd>
      </template>
      <template v-if="summary.tokens">
        <dt>消耗 token</dt>
        <dd>输入 {{ tokens(summary.tokens.input) }} / 输出 {{ tokens(summary.tokens.output) }}</dd>
      </template>
      <template v-if="summary.risk_flags?.length">
        <dt>风险标记</dt>
        <dd>{{ summary.risk_flags.join('，') }}</dd>
      </template>
    </dl>
    <ul v-if="summary?.warnings?.length" class="mt-2 space-y-0.5">
      <li v-for="w in summary.warnings" :key="w.id" :class="severityTone[w.severity] ?? 'text-slate-500'">⚠ {{ w.label }}</li>
    </ul>
    <details v-if="summary?.failed_probes?.length" class="mt-2">
      <summary class="cursor-pointer text-slate-500">未通过的探针（{{ summary.failed_probes.length }}）</summary>
      <ul class="mt-1 list-disc space-y-0.5 pl-5 text-slate-600">
        <li v-for="(p, i) in summary.failed_probes" :key="i">{{ p.label }}<span v-if="p.reason" class="text-slate-400">：{{ p.reason }}</span></li>
      </ul>
    </details>
  </div>
</template>
