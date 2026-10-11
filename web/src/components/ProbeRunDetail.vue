<script setup lang="ts">
// 一次检测的完整结果（仅管理员可见）：各项指标、按套件汇总，以及每道题的结果与回答片段。
import { computed } from 'vue'
import type { ProbeRunDetail, ProbeRunResult } from '@/api/types'
import StatusBadge from '@/components/StatusBadge.vue'
import { dateTime, ms, percent } from '@/utils/format'
import { probeResultKey } from '@/utils/inspection'

const props = defineProps<{ detail: ProbeRunDetail }>()

const run = computed(() => props.detail.run)
const metrics = computed(() => run.value.summary?.suites)

// 按题目、实例和步骤配对，不能把同模板的不同实例互相覆盖。
const rows = computed(() => {
  const ref = new Map((props.detail.reference_results ?? []).map((x) => [probeResultKey(x), x]))
  return props.detail.results.map((r) => ({ ...r, ref: r.item_id !== undefined ? ref.get(probeResultKey(r)) : undefined }))
})

const avg = (xs: number[]) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0)

// 按套件汇总：题数、通过数、错误数、平均耗时与平均推理 token；有基线时附基线通过数
const suites = computed(() => {
  const order = ['iq', 'reasoning', 'long_context']
  const groups = new Map<string, { target: ProbeRunResult[]; ref: ProbeRunResult[] }>()
  for (const r of props.detail.results) {
    if (!groups.has(r.suite)) groups.set(r.suite, { target: [], ref: [] })
    groups.get(r.suite)!.target.push(r)
  }
  for (const r of props.detail.reference_results ?? []) groups.get(r.suite)?.ref.push(r)
  return [...groups.entries()]
    .sort(([a], [b]) => (order.indexOf(a) + 1 || 99) - (order.indexOf(b) + 1 || 99))
    .map(([suite, g]) => {
      const ok = g.target.filter((r) => !r.error_class)
      return {
        suite,
        items: g.target.length,
        passed: g.target.filter((r) => r.passed).length,
        errors: g.target.length - ok.length,
        avgDuration: avg(ok.map((r) => r.duration_ms)),
        avgReasoning: avg(ok.map((r) => r.reasoning_tokens)),
        refItems: g.ref.length,
        refPassed: g.ref.filter((r) => r.passed).length,
      }
    })
})

function passText(p: boolean | null | undefined) {
  return p === null || p === undefined ? '—' : p ? '通过' : '未通过'
}
</script>

<template>
  <div class="space-y-4 text-sm">
    <div class="flex flex-wrap items-center gap-2">
      <StatusBadge v-if="run.summary?.verdict" kind="verdict" :value="run.summary.verdict" />
      <StatusBadge kind="runTrigger" :value="run.trigger" />
      <StatusBadge kind="probeRunStatus" :value="run.status" />
      <span class="text-slate-500">检测 #{{ run.id }} · 上游 #{{ run.upstream_id }} · 模型 {{ run.model }}</span>
      <span class="text-slate-500">{{ dateTime(run.queued_at) }}</span>
    </div>
    <p v-if="run.summary?.error" class="text-red-600">{{ run.summary.error }}</p>

    <div>
      <div class="mb-1 font-medium text-slate-700">各项指标</div>
      <dl class="kv">
        <dt>答题</dt>
        <dd>
          共 {{ run.items }} 题，通过 {{ run.passed }} 题
          <template v-if="detail.reference">；基线 #{{ detail.reference.upstream_id }} 通过 {{ detail.reference.passed }} / {{ detail.reference.items }}</template>
        </dd>
        <dt>可用性</dt>
        <dd>{{ metrics?.availability ? `${metrics.availability.success} / ${metrics.availability.total} 次成功` : '—' }}</dd>
        <dt>首字延迟（中位 / P95）</dt>
        <dd>{{ ms(metrics?.latency?.ttft_p50_ms) }} / {{ ms(metrics?.latency?.ttft_p95_ms) }}</dd>
        <dt>输出速度</dt>
        <dd>{{ metrics?.latency?.output_tps ? `${metrics.latency.output_tps.toFixed(1)} token/s` : '—' }}</dd>
        <dt>智力相对分</dt>
        <dd>
          <template v-if="metrics?.iq">
            {{ percent(metrics.iq.relative_score, 0) }}（计分 {{ metrics.iq.samples }} 题{{ metrics.iq.confidence ? `，置信度 ${percent(metrics.iq.confidence, 0)}` : '' }}）
          </template>
          <template v-else>—</template>
        </dd>
        <dt>推理强度（相对基线）</dt>
        <dd>{{ metrics?.reasoning ? percent(metrics.reasoning.ratio_to_baseline, 0) : '—' }}</dd>
        <dt>对照基线</dt>
        <dd>{{ run.summary?.baseline ? '有' : run.summary ? '无（按绝对标准判定）' : '—' }}</dd>
        <template v-if="run.summary?.attribution">
          <dt>原因</dt>
          <dd><StatusBadge kind="attribution" :value="run.summary.attribution" /></dd>
        </template>
        <template v-if="run.summary?.grade">
          <dt>等级</dt>
          <dd>{{ run.summary.grade }}</dd>
        </template>
      </dl>
    </div>

    <div v-if="suites.length">
      <div class="mb-1 font-medium text-slate-700">按套件</div>
      <table class="w-full text-left text-xs">
        <thead class="text-slate-500">
          <tr>
            <th class="p-2">套件</th>
            <th class="p-2">通过 / 题数</th>
            <th class="p-2">错误</th>
            <th class="p-2">平均耗时</th>
            <th class="p-2">平均推理 token</th>
            <th v-if="detail.reference" class="p-2">基线通过</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in suites" :key="s.suite" class="border-t border-slate-100">
            <td class="p-2 font-medium">{{ s.suite }}</td>
            <td class="p-2" :class="s.passed < s.items ? 'text-red-600' : 'text-emerald-700'">{{ s.passed }} / {{ s.items }}</td>
            <td class="p-2">{{ s.errors }}</td>
            <td class="p-2">{{ ms(s.avgDuration) }}</td>
            <td class="p-2">{{ s.avgReasoning.toFixed(0) }}</td>
            <td v-if="detail.reference" class="p-2">{{ s.refPassed }} / {{ s.refItems }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div>
      <div class="mb-1 font-medium text-slate-700">逐题结果</div>
      <div class="max-h-[50vh] overflow-auto">
        <table class="w-full text-left text-xs">
          <thead class="sticky top-0 bg-white text-slate-500">
            <tr>
              <th class="p-2">题目</th>
              <th class="p-2">套件 / 类别</th>
              <th class="p-2">结果</th>
              <th v-if="detail.reference" class="p-2">基线</th>
              <th class="p-2">首字 / 总时长</th>
              <th class="p-2">输入 / 输出 / 推理词元</th>
              <th class="p-2">回答片段</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in rows" :key="r.item_id !== undefined ? probeResultKey(r) : i" class="border-t border-slate-100 align-top" :data-result-key="probeResultKey(r)">
              <td class="p-2">
                <div class="font-medium">#{{ r.item_id ?? '—' }}</div>
                <div class="text-slate-500">实例 {{ r.instance_idx ?? 0 }} · 步骤 {{ r.step ?? 0 }}</div>
                <div v-if="r.generator_version" class="text-slate-500">{{ r.generator_version }}</div>
                <div v-if="r.instance_hash" class="max-w-xs truncate font-mono text-slate-500" :title="r.instance_hash">哈希 {{ r.instance_hash }}</div>
                <div class="max-w-xs truncate text-slate-500" :title="r.prompt">{{ r.prompt }}</div>
                <div v-if="r.models.length" class="text-slate-500">限 {{ r.models.join(', ') }}</div>
              </td>
              <td class="p-2">{{ r.suite }}<div class="text-slate-500">{{ r.category }}{{ r.check_key ? ` · ${r.check_key}` : '' }}</div></td>
              <td class="p-2" :class="r.passed ? 'text-emerald-700' : 'text-red-600'">
                {{ passText(r.passed) }}
                <div v-if="r.error_class" class="text-slate-500">{{ r.error_class }}{{ r.http_status ? ` · ${r.http_status}` : '' }}</div>
                <div v-if="r.response_model && r.response_model !== run.model" class="text-amber-700">返回 {{ r.response_model }}</div>
                <div v-if="r.stop_reason || r.effort" class="text-slate-500">结束 {{ r.stop_reason || '—' }} · 强度 {{ r.effort || '—' }}</div>
                <div v-if="r.flags?.length" class="text-slate-500">{{ r.flags.join('、') }}</div>
              </td>
              <td v-if="detail.reference" class="p-2" :class="r.ref?.passed ? 'text-emerald-700' : 'text-red-600'">{{ passText(r.ref?.passed) }}</td>
              <td class="p-2 whitespace-nowrap">{{ ms(r.ttft_ms) }} / {{ ms(r.duration_ms) }}</td>
              <td class="p-2 whitespace-nowrap">{{ r.input_tokens ?? '未取得' }} / {{ r.output_tokens }} / {{ r.reasoning_tokens }}<div class="text-slate-500">缓存读 {{ r.cache_read_tokens ?? '未取得' }} / 写 {{ r.cache_creation_tokens ?? '未取得' }}</div></td>
              <td class="p-2">
                <div class="max-w-sm whitespace-pre-wrap break-words text-slate-600">{{ r.excerpt || '—' }}</div>
                <details v-if="r.error_body" class="mt-1 text-red-700"><summary>脱敏错误体</summary><pre class="max-w-sm whitespace-pre-wrap break-words">{{ r.error_body }}</pre></details>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="hint">回答片段只保存前 500 个字符；返回的模型名与请求不一致时标黄。</p>
    </div>
  </div>
</template>
