<script setup lang="ts">
// 卖家测试报告（脱敏）：只包含结论、各项指标与建议，不包含题目、答案和基线信息。
import type { Report } from '@/api/types'
import StatusBadge from '@/components/StatusBadge.vue'
import ThirdPartyResult from '@/components/ThirdPartyResult.vue'
import { dateTime, ms, percent } from '@/utils/format'

defineProps<{ report: Report }>()

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
    <dl class="grid grid-cols-2 gap-x-6 gap-y-2">
      <dt class="text-slate-500">可用性</dt>
      <dd>
        <template v-if="report.summary.suites.availability">
          {{ report.summary.suites.availability.success }} / {{ report.summary.suites.availability.total }} 次成功
        </template>
        <template v-else>—</template>
      </dd>
      <dt class="text-slate-500">首字延迟（中位 / P95）</dt>
      <dd>{{ ms(report.summary.suites.latency?.ttft_p50_ms) }} / {{ ms(report.summary.suites.latency?.ttft_p95_ms) }}</dd>
      <dt class="text-slate-500">输出速度</dt>
      <dd>{{ report.summary.suites.latency?.output_tps ? `${report.summary.suites.latency.output_tps.toFixed(1)} token/s` : '—' }}</dd>
      <dt class="text-slate-500">智力相对分</dt>
      <dd>
        <template v-if="report.summary.suites.iq">
          {{ percent(report.summary.suites.iq.relative_score, 0) }}（{{ report.summary.suites.iq.samples }} 题{{
            report.summary.suites.iq.confidence ? `，置信度 ${percent(report.summary.suites.iq.confidence, 0)}` : ''
          }}）
        </template>
        <template v-else>—</template>
      </dd>
      <dt class="text-slate-500">推理强度（相对基线）</dt>
      <dd>{{ report.summary.suites.reasoning ? percent(report.summary.suites.reasoning.ratio_to_baseline, 0) : '—' }}</dd>
      <dt class="text-slate-500">对照基线</dt>
      <dd>{{ report.summary.baseline ? '有（同批次对照平台基线账号）' : '无（按绝对标准判定）' }}</dd>
      <template v-if="report.summary.internal_verdict">
        <dt class="text-slate-500">平台探测结论</dt>
        <dd><StatusBadge kind="verdict" :value="report.summary.internal_verdict" />（最终结论取平台探测与第三方检测中更严重的一项）</dd>
      </template>
      <template v-if="report.summary.attribution">
        <dt class="text-slate-500">原因</dt>
        <dd><StatusBadge kind="attribution" :value="report.summary.attribution" /></dd>
      </template>
      <template v-if="report.summary.grade">
        <dt class="text-slate-500">等级</dt>
        <dd>{{ report.summary.grade }}</dd>
      </template>
      <template v-if="report.impact?.buyer_facing_paused !== undefined">
        <dt class="text-slate-500">处置</dt>
        <dd>{{ report.impact.buyer_facing_paused ? '已暂停调度，等待复测恢复' : '正常调度' }}</dd>
      </template>
      <template v-if="report.impact?.next_check_at">
        <dt class="text-slate-500">下次复测</dt>
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
    <div v-if="report.summary.suggestions?.length" class="rounded bg-slate-50 p-3">
      <div class="mb-1 font-medium text-slate-700">建议</div>
      <ul class="list-disc space-y-1 pl-5 text-slate-600">
        <li v-for="s in report.summary.suggestions" :key="s">{{ suggestionText[s] ?? s }}</li>
      </ul>
    </div>
  </div>
</template>
