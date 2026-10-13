<script setup lang="ts">
// 检测报告默认按卖家视角展示；管理员上下文可额外查看测智通过率。
import { computed } from 'vue'
import type { InspectionCheckResult, InspectionFlow, Report } from '@/api/types'
import InspectionAppraisal from '@/components/InspectionAppraisal.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ThirdPartyResult from '@/components/ThirdPartyResult.vue'
import { dateTime, ms, percent } from '@/utils/format'
import { identitySourceText, inspectionCategories, inspectionStatuses, intelligenceStatusText } from '@/utils/inspection'

const props = withDefaults(defineProps<{ report: Report; admin?: boolean }>(), { admin: false })
const hasFlows = computed(() => props.report.summary.intelligence !== undefined || props.report.summary.identity !== undefined || props.report.summary.flows !== undefined || props.report.summary.identity_source !== undefined || props.report.summary.models !== undefined)
// 新报告的试算标记表示未启用扣价，使用副本避免复用旧组件的覆盖不足文案。
const flowAppraisal = computed(() => props.report.summary.appraisal ? { ...props.report.summary.appraisal, provisional: false } : undefined)
const identitySource = computed(() => props.report.summary.identity?.source ?? props.report.summary.identity_source)
const commonOrder = ['D13', 'D04', 'D10', 'D05']

// 报告不返回完整 registry，使用约定的检测项键推导展示归属。
function checkFlow(check: InspectionCheckResult): InspectionFlow {
  if (commonOrder.includes(check.key)) return 'common'
  if (check.key.startsWith('D07') || ['D09', 'D06', 'A06'].includes(check.key)) return 'intelligence'
  return 'identity'
}

const flowSections = computed(() => {
  const checks = Object.values(props.report.summary.checks ?? {})
  return [
    { key: 'common', name: '可用与性能', status: undefined, inheritedAt: undefined },
    { key: 'intelligence', name: '测智', status: props.report.summary.intelligence?.status ?? 'not_run', inheritedAt: props.report.summary.intelligence?.inherited_at },
    { key: 'identity', name: '身份与功能核验', status: props.report.summary.identity?.status ?? 'not_run', inheritedAt: props.report.summary.identity?.inherited_at },
  ].map((section) => ({
    ...section,
    checks: checks.filter((check) => checkFlow(check) === section.key).sort((a, b) => section.key === 'common' ? commonOrder.indexOf(a.key) - commonOrder.indexOf(b.key) : a.key.localeCompare(b.key)),
  }))
})

function visibleMetrics(check: InspectionCheckResult) {
  if (!props.admin && checkFlow(check) === 'intelligence') return {}
  return check.metrics ?? {}
}

function statusTone(status?: string) {
  return status === 'pass' ? 'bg-emerald-50 text-emerald-700' : status === 'fail' ? 'bg-red-50 text-red-700' : ['warning', 'inconclusive'].includes(status ?? '') ? 'bg-amber-50 text-amber-800' : 'bg-slate-100 text-slate-600'
}
const groups = computed(() => {
  const checks = Object.values(props.report.summary.checks ?? {})
  return [...new Set(checks.map((check) => check.category))].map((category) => ({ category, checks: checks.filter((check) => check.category === category) }))
})
const trial = computed(() => props.report.summary.inspection_mode === 'trial')
const metricUnits: Record<string, string> = { ms: '毫秒', tokens: 'token', tokens_per_second: 'token/s', count: '个', ratio: '比例', fraction: '比例' }
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
      <span class="text-slate-500">模型 {{ report.summary.models?.length ? report.summary.models.join('、') : report.summary.model }}</span>
      <span class="text-slate-500">{{ dateTime(report.summary.checked_at || report.created_at) }}</span>
    </div>
    <template v-if="hasFlows">
      <section v-for="section in flowSections" :key="section.key" class="rounded-lg border border-slate-200" :aria-label="section.name" :data-flow="section.key">
        <div class="border-b border-slate-200 bg-slate-50 px-3 py-2">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="font-medium">{{ section.name }}</h3>
            <span v-if="section.status" class="rounded px-2 py-0.5 text-xs" :class="statusTone(section.status)">{{ section.key === 'intelligence' ? intelligenceStatusText(section.status) : inspectionStatuses[section.status] ?? section.status }}</span>
            <span v-if="section.key === 'identity' && identitySource" class="text-xs text-slate-500">来源：{{ identitySourceText(identitySource) }}</span>
          </div>
          <p v-if="section.inheritedAt" class="mt-1 text-xs text-slate-500">沿用 {{ dateTime(section.inheritedAt) }} 的结果</p>
        </div>
        <div v-if="section.key === 'intelligence' && report.summary.intelligence?.models?.length" class="divide-y divide-slate-100 border-b border-slate-100" data-testid="intelligence-models">
          <div v-for="model in report.summary.intelligence.models" :key="model.model" class="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2" :data-model="model.model">
            <span class="min-w-0 break-all font-medium">{{ model.model }}</span>
            <span class="rounded px-2 py-0.5 text-xs" :class="statusTone(model.status)">{{ intelligenceStatusText(model.status) }}</span>
            <template v-if="admin">
              <span class="text-xs text-slate-500">通过率 {{ percent(model.target_pass_rate) }}</span>
              <span class="text-xs text-slate-500">期望通过率 {{ percent(model.expected_pass_rate) }}</span>
            </template>
          </div>
        </div>
        <div v-if="section.key === 'identity' && identitySource === 'third_party'" class="space-y-2 p-3" data-testid="identity-third-party">
          <ThirdPartyResult
            v-for="result in report.summary.third_party ?? []"
            :key="result.provider"
            :provider="result.provider"
            :status="result.status"
            :outcome="result.outcome"
            :verdict="result.verdict"
            :model="result.model"
            :report-url="result.report_url"
            :error="result.error"
            :summary="result.summary"
          />
          <p v-if="!report.summary.third_party?.length" class="text-slate-500">暂无第三方检测结果</p>
        </div>
        <div v-for="check in section.checks" :key="check.key" class="border-b border-slate-100 p-3 last:border-b-0" :data-check-key="check.key">
          <div class="flex flex-wrap items-center gap-2">
            <strong>{{ check.name }}</strong>
            <span class="rounded px-2 py-0.5 text-xs" :class="statusTone(check.status)">{{ inspectionStatuses[check.status] ?? check.status }}</span>
            <span v-if="check.applied && check.factor !== null && check.factor !== undefined" class="text-xs text-slate-500">生效系数 {{ check.factor.toFixed(3) }}</span>
            <span v-else-if="check.role === 'gate'" class="text-xs text-slate-500">门槛项</span>
          </div>
          <p v-if="check.reason" class="mt-1">{{ check.reason }}</p>
          <div v-if="Object.keys(visibleMetrics(check)).length" class="mt-1 flex flex-wrap gap-x-4 text-xs text-slate-500"><span v-for="(metric, name) in visibleMetrics(check)" :key="name">{{ metricNames[name] ?? name }}：{{ metric.value === null ? '未取得' : metric.value.toFixed(3) }} {{ metricUnits[metric.unit] ?? metric.unit }}</span></div>
          <p class="mt-1 text-slate-600">{{ check.impact || (check.applied && check.factor !== null && check.factor < 1 ? '本项已用于扣价。' : '本项未影响价格与调度。') }}</p>
          <p v-if="check.advice" class="mt-1 text-slate-600">建议：{{ check.advice }}</p>
        </div>
        <p v-if="!section.checks.length && section.key === 'common'" class="p-3 text-slate-500">暂无可用与性能检测结果</p>
        <p v-else-if="!section.checks.length && section.key === 'intelligence' && !report.summary.intelligence?.models?.length" class="p-3 text-slate-500">{{ section.status === 'not_applicable' ? '挂售模型未配置测智' : '暂无测智检测结果' }}</p>
        <p v-else-if="!section.checks.length && section.key === 'identity' && identitySource !== 'third_party'" class="p-3 text-slate-500">暂无身份与功能核验结果</p>
      </section>
      <InspectionAppraisal :appraisal="flowAppraisal" :grade="report.summary.grade" :quality-factor="report.summary.quality_factor" />
      <p v-if="report.summary.appraisal?.provisional" class="text-sm text-amber-800">未启用扣价，估价为试算</p>
      <dl class="kv">
        <template v-for="(factor, group) in report.summary.group_factors ?? {}" :key="group"><dt>{{ inspectionCategories[group] ?? group }}组系数</dt><dd>{{ factor === null ? '无法判定' : factor.toFixed(3) }}</dd></template>
        <template v-if="report.summary.proposed_grade"><dt>{{ trial ? '试算等级' : '本次建议等级' }}</dt><dd>{{ report.summary.proposed_grade }}</dd></template>
        <template v-if="report.summary.repaired_grade"><dt>修复后预计等级</dt><dd>{{ report.summary.repaired_grade }}（需复测确认）</dd></template>
        <template v-if="report.impact?.buyer_facing_paused !== undefined"><dt>调度状态</dt><dd>{{ report.impact.buyer_facing_paused ? '已暂停调度，等待复测恢复' : '正常调度' }}</dd></template>
        <template v-if="report.impact?.next_check_at"><dt>下次复测</dt><dd>{{ dateTime(String(report.impact.next_check_at)) }}</dd></template>
      </dl>
      <div v-if="report.summary.gates?.length" class="callout callout-warn">
        <div class="font-medium">门槛事件</div>
        <p v-for="(gate, index) in report.summary.gates" :key="index" class="mt-1">{{ gate.reason }} · {{ trial ? '试运行，仅展示门槛' : gate.confirmed ? '已确认，按健康限制处置' : '待确认，不据此暂停' }}</p>
      </div>
      <p class="text-xs text-slate-500">检测是抽样，不能保证没有偷换；仅开启扣价且已生效的项目影响质量系数。</p>
    </template>
    <template v-else-if="groups.length">
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
            <span v-if="check.applied && check.factor !== null && check.factor !== undefined" class="text-xs text-slate-500">生效系数 {{ check.factor.toFixed(3) }}</span>
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
    <div v-if="!hasFlows && report.summary.third_party?.length" class="space-y-2">
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
