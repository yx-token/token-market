<script setup lang="ts">
// 卖家测试报告（脱敏）：只包含结论、各项指标与建议，不包含题目、答案和基线信息。
import { computed } from 'vue'
import type { Report } from '@/api/types'
import InspectionAppraisal from '@/components/InspectionAppraisal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ThirdPartyResult from '@/components/ThirdPartyResult.vue'
import { dateTime, ms, percent } from '@/utils/format'
import { inspectionCategories, inspectionStatuses } from '@/utils/inspection'

const props = defineProps<{ report: Report }>()
const groups = computed(() => {
  const checks = Object.values(props.report.summary.checks ?? {})
  return [...new Set(checks.map((check) => check.category))].map((category) => ({ category, checks: checks.filter((check) => check.category === category) }))
})
const trial = computed(() => props.report.summary.inspection_mode === 'trial')
const metricUnits: Record<string, string> = { ms: '毫秒', tokens: '词元', tokens_per_second: '词元/秒', count: '个', ratio: '比例', fraction: '比例' }
const metricNames: Record<string, string> = { ttft_ratio: '首字延迟相对值', output_tps_ratio: '输出速度相对值', mismatches: '不一致观测', mismatch_rate: '不一致比例', success_rate: '成功率', relative_score: '智力相对分', mcnemar_p: '配对显著性', target_pass_rate: '通过率', reasoning_ratio: '推理量相对值', high_low_ratio: '高低档推理量之比' }

const suggestionText: Record<string, string> = {
  check_authorization: '账号没有正常响应：请检查授权是否被撤销、订阅是否到期，然后重新授权。',
  check_shared_usage: '请检查这个账号是否在其他地方被大量使用或与他人共用。',
  reauthorize: '可以尝试重新授权这个账号。',
  proxy_rotation: '疑似代理线路问题，平台会更换代理，无需操作。',
  vendor_wide: '官方服务整体下降，所有账号都受影响，不影响你的评级。',
  disable_training: '请关闭这个账号的训练数据使用设置。',
  third_party_mismatch: '第三方检测站判定这个端点背后的模型与声明不一致。请确认账号是官方订阅、没有经过其他中转，可以申诉复测。',
}
</script>

<template>
  <div class="space-y-4 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <StatusBadge kind="verdict" :value="report.verdict" />
      <StatusBadge kind="trigger" :value="report.trigger" />
      <span class="text-slate-500">模型 {{ report.summary.model }}</span>
      <span class="text-slate-500">{{ dateTime(report.summary.checked_at || report.created_at) }}</span>
    </div>
    <template v-if="groups.length">
      <InspectionAppraisal :appraisal="report.summary.appraisal" :grade="report.summary.grade" :quality-factor="report.summary.quality_factor" :inspection-mode="report.summary.inspection_mode" />
      <dl class="kv">
        <template v-for="(factor, group) in report.summary.group_factors ?? {}" :key="group"><dt>{{ inspectionCategories[group] ?? group }}组系数{{ trial ? '（试算）' : '' }}</dt><dd>{{ factor === null ? '无法判定' : factor.toFixed(3) }}</dd></template>
        <template v-if="report.summary.proposed_grade"><dt>{{ trial ? '试算等级' : '本次建议等级' }}</dt><dd>{{ report.summary.proposed_grade }}</dd></template>
        <template v-if="report.summary.repaired_grade"><dt>修复后预计等级</dt><dd>{{ report.summary.repaired_grade }}（需复测确认；S 仍需满足持续可用与无门槛历史条件）</dd></template>
        <template v-if="report.impact?.buyer_facing_paused !== undefined"><dt>调度状态</dt><dd>{{ report.impact.buyer_facing_paused ? '已暂停调度，等待复测恢复' : '正常调度' }}</dd></template>
        <template v-if="report.impact?.next_check_at"><dt>下次复测</dt><dd>{{ dateTime(String(report.impact.next_check_at)) }}</dd></template>
      </dl>
      <div v-if="report.summary.gates?.length" class="callout callout-warn">
        <div class="font-medium">门槛事件</div>
        <p v-for="(gate, index) in report.summary.gates" :key="index" class="mt-1">{{ gate.reason }} · {{ trial ? '试运行，仅展示' : gate.confirmed ? '已确认，按健康限制处置' : '待确认，不据此暂停' }}</p>
      </div>
      <section v-for="group in groups" :key="group.category" class="rounded-lg border border-slate-200">
        <h3 class="border-b border-slate-200 bg-slate-50 px-3 py-2 font-medium">{{ inspectionCategories[group.category] ?? group.category }}</h3>
        <div v-for="check in group.checks" :key="check.key" class="border-b border-slate-100 p-3 last:border-b-0" :data-check-key="check.key">
          <div class="flex flex-wrap items-center gap-2">
            <strong>{{ check.name }}</strong>
            <span class="rounded px-2 py-0.5 text-xs" :class="check.status === 'pass' ? 'bg-emerald-50 text-emerald-700' : check.status === 'fail' ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-800'">{{ inspectionStatuses[check.status] ?? check.status }}</span>
            <span v-if="check.role === 'factor' && check.factor !== null" class="text-xs text-slate-500">{{ check.applied ? '生效系数' : '候选系数，不生效' }} {{ check.factor.toFixed(3) }}</span>
            <span v-else-if="check.role === 'gate'" class="text-xs text-slate-500">门槛项</span>
          </div>
          <p v-if="check.reason" class="mt-1">{{ check.reason }}</p>
          <div v-if="check.metrics && Object.keys(check.metrics).length" class="mt-1 flex flex-wrap gap-x-4 text-xs text-slate-500"><span v-for="(metric, name) in check.metrics" :key="name">{{ metricNames[name] ?? name }}：{{ metric.value === null ? '未取得' : metric.value.toFixed(3) }} {{ metricUnits[metric.unit] ?? metric.unit }}</span></div>
          <p class="mt-1 text-slate-600">{{ trial ? '试运行，不影响价格与调度。' : check.impact || '本项未影响价格与调度。' }}</p>
          <p v-if="check.advice" class="mt-1 text-slate-600">建议：{{ check.advice }}</p>
        </div>
      </section>
      <p class="text-xs text-slate-500">检测是抽样，不能保证没有偷换；无法判定与未测项目不扣价。</p>
    </template>
    <dl v-else class="kv">
      <dt>可用性</dt>
      <dd>
        <template v-if="report.summary.suites.availability">
          {{ report.summary.suites.availability.success }} / {{ report.summary.suites.availability.total }} 次成功
        </template>
        <template v-else>—</template>
      </dd>
      <dt>首字延迟（中位 / P95）</dt>
      <dd>{{ ms(report.summary.suites.latency?.ttft_p50_ms) }} / {{ ms(report.summary.suites.latency?.ttft_p95_ms) }}</dd>
      <dt>输出速度</dt>
      <dd>{{ report.summary.suites.latency?.output_tps ? `${report.summary.suites.latency.output_tps.toFixed(1)} token/s` : '—' }}</dd>
      <dt>智力相对分</dt>
      <dd>
        <template v-if="report.summary.suites.iq">
          {{ percent(report.summary.suites.iq.relative_score, 0) }}（{{ report.summary.suites.iq.samples }} 题{{
            report.summary.suites.iq.confidence ? `，置信度 ${percent(report.summary.suites.iq.confidence, 0)}` : ''
          }}）
        </template>
        <template v-else>—</template>
      </dd>
      <dt>推理强度（相对基线）</dt>
      <dd>{{ report.summary.suites.reasoning ? percent(report.summary.suites.reasoning.ratio_to_baseline, 0) : '—' }}</dd>
      <dt>对照基线</dt>
      <dd>{{ report.summary.baseline ? '有（同批次对照平台基线账号）' : '无（按绝对标准判定）' }}</dd>
      <template v-if="report.summary.internal_verdict">
        <dt>平台探测结论</dt>
        <dd><StatusBadge kind="verdict" :value="report.summary.internal_verdict" />（最终结论取平台探测与第三方检测中更严重的一项）</dd>
      </template>
      <template v-if="report.summary.attribution">
        <dt>原因</dt>
        <dd><StatusBadge kind="attribution" :value="report.summary.attribution" /></dd>
      </template>
      <template v-if="report.summary.grade">
        <dt>等级</dt>
        <dd>{{ report.summary.grade }}</dd>
      </template>
      <template v-if="report.impact?.buyer_facing_paused !== undefined">
        <dt>处置</dt>
        <dd>{{ report.impact.buyer_facing_paused ? '已暂停调度，等待复测恢复' : '正常调度' }}</dd>
      </template>
      <template v-if="report.impact?.next_check_at">
        <dt>下次复测</dt>
        <dd>{{ dateTime(String(report.impact.next_check_at)) }}</dd>
      </template>
    </dl>
    <div v-if="report.summary.third_party?.length" class="space-y-2">
      <div class="font-medium text-slate-700">第三方检测</div>
      <ThirdPartyResult
        v-for="t in report.summary.third_party"
        :key="t.provider"
        :provider="t.provider"
        :status="t.status"
        :outcome="t.outcome"
        :verdict="t.verdict"
        :model="t.model"
        :report-url="t.report_url"
        :error="t.error"
        :summary="t.summary"
      />
    </div>
    <div v-if="report.summary.suggestions?.length" class="callout">
      <div class="mb-1 font-medium text-slate-700">建议</div>
      <ul class="list-disc space-y-1 pl-5 text-slate-600">
        <li v-for="s in report.summary.suggestions" :key="s">{{ suggestionText[s] ?? s }}</li>
      </ul>
    </div>
  </div>
</template>
