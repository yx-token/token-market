<script setup lang="ts">
// 卖家报告里的核验结果区块：只展示结论、各项测试大类与风险项，不暴露检测渠道与站点链接。
import type { VerificationResult } from '@/api/types'
import { dateTime } from '@/utils/format'
import { verificationVerdictText } from '@/utils/inspection'

const props = defineProps<{ verification: VerificationResult }>()

// 后端下发的是平台折算结论（normal / suspected / degraded / inconclusive），按严重程度映射到三档配色。
const statusTone: Record<string, string> = {
  normal: 'bg-emerald-50 text-emerald-700',
  suspected: 'bg-amber-50 text-amber-800',
  degraded: 'bg-red-50 text-red-700',
  unavailable: 'bg-red-50 text-red-700',
  inconclusive: 'bg-slate-100 text-slate-600',
}
const severityTone: Record<string, string> = { fail: 'text-red-600', warning: 'text-amber-600' }
const severityText: Record<string, string> = { fail: '未通过', warning: '警告' }

// 平台折算结论的中文文案；后端给的 verdict_text 是检测站判定码的文案，缺失时才退回这里。
const statusText: Record<string, string> = {
  normal: '核验通过',
  suspected: '疑似不符',
  degraded: '核验不符',
  unavailable: '核验不可用',
  inconclusive: '无法判定',
}

/** 结论文案：优先用后端给定文案，其次由折算结论映射 */
function verdictText(v: VerificationResult): string {
  return v.verdict_text || statusText[v.status] || verificationVerdictText(v.status)
}

function resultSummary(category: NonNullable<VerificationResult['categories']>[number]): string {
  const parts: string[] = []
  if (category.passed) parts.push(`${category.passed} 通过`)
  if (category.warning) parts.push(`${category.warning} 警告`)
  if (category.failed) parts.push(`${category.failed} 未通过`)
  if (category.error) parts.push(`${category.error} 出错`)
  if (!parts.length) return `共 ${category.total} 项，暂无结论`
  return `${parts.join(' · ')} / 共 ${category.total} 项`
}

function riskReason(risk: NonNullable<VerificationResult['risks']>[number]): string {
  return risk.reason || severityText[risk.severity] || risk.severity
}

const categories = () => props.verification.categories ?? []
const risks = () => props.verification.risks ?? []
</script>

<template>
  <section class="rounded-xl border border-slate-200 bg-white/70 p-3 text-sm" aria-label="核验结果" data-testid="verification-result">
    <div class="flex flex-wrap items-center gap-2">
      <h4 class="font-medium text-slate-700">核验结果</h4>
      <span class="rounded px-2 py-0.5 text-xs" :class="statusTone[verification.status] ?? 'bg-slate-100 text-slate-600'">{{ verdictText(verification) }}</span>
      <span v-if="verification.checked_at" class="text-xs text-slate-400">核验时间 {{ dateTime(verification.checked_at) }}</span>
    </div>
    <p v-if="verification.true_model || verification.true_family" class="mt-1 text-slate-600">
      结论型号：{{ verification.true_model || verification.true_family }}
    </p>
    <div v-if="categories().length" class="mt-3" data-testid="verification-categories">
      <p class="label mb-1">各项测试大类</p>
      <ul class="space-y-1">
        <li v-for="category in categories()" :key="category.key" class="flex flex-wrap items-baseline gap-x-2" :data-category="category.key">
          <span class="text-slate-700">{{ category.name }}</span>
          <span class="text-xs text-slate-500">{{ resultSummary(category) }}</span>
        </li>
      </ul>
    </div>
    <div class="mt-3" data-testid="verification-risks">
      <p class="label mb-1">风险项</p>
      <p v-if="!risks().length" class="text-slate-500">未发现风险项</p>
      <ul v-else class="space-y-1">
        <li v-for="(risk, index) in risks()" :key="index" class="text-slate-600" :data-risk-severity="risk.severity">
          <span class="text-slate-700">{{ risk.category_name }}</span> · {{ risk.label }}
          <span :class="severityTone[risk.severity] ?? 'text-slate-500'">（{{ severityText[risk.severity] ?? risk.severity }}）</span>
          <span v-if="riskReason(risk)" class="text-slate-500">{{ riskReason(risk) }}</span>
        </li>
      </ul>
    </div>
  </section>
</template>
