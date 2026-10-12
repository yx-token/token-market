<script setup lang="ts">
// 一次检测的完整结果（仅管理员可见）：结论概要、性能与智力指标、按套件汇总，以及每道题的结果与回答片段。
// 标准答案是私有内容，只在管理员上下文传 show-answer 展示；卖家/上游侧复用本组件时默认不出现。
import { computed } from 'vue'
import type { ProbeRunDetail, ProbeRunResult } from '@/api/types'
import MarkdownText from '@/components/MarkdownText.vue'
import ProbeErrorBody from '@/components/ProbeErrorBody.vue'
import ProbeExcerpt from '@/components/ProbeExcerpt.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { dateTime, ms, percent } from '@/utils/format'
import { probeResultKey, probeSuiteText } from '@/utils/inspection'

const props = withDefaults(defineProps<{ detail: ProbeRunDetail; showAnswer?: boolean }>(), { showAnswer: false })

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

// 逐题结果按套件分组，分组行横跨整表；行序保持返回顺序，不另排序
interface RowGroup {
  suite: string
  rows: { row: (typeof rows.value)[number]; index: number }[]
}
const rowGroups = computed<RowGroup[]>(() => {
  const groups: RowGroup[] = []
  const byKey = new Map<string, RowGroup>()
  rows.value.forEach((row, index) => {
    let g = byKey.get(row.suite)
    if (!g) {
      g = { suite: row.suite, rows: [] }
      byKey.set(row.suite, g)
      groups.push(g)
    }
    g.rows.push({ row, index })
  })
  return groups
})

// 分组行横跨的列数：题目 / 结果 /（基线）/ 耗时 / token / 输出与答案
const detailColspan = computed(() => (props.detail.reference ? 6 : 5))
// 未通过或报错的结果行加淡红底，方便在长表里定位
function rowTone(r: ProbeRunResult) {
  return r.passed === false || r.error_class ? 'bg-red-50/70' : ''
}

function passText(p: boolean | null | undefined) {
  return p === null || p === undefined ? '—' : p ? '通过' : '未通过'
}
</script>

<template>
  <div class="space-y-4 text-sm">
    <!-- 概要：结论徽章 + 关键标识（弹窗内滚动时粘在顶部） -->
    <div class="sticky top-0 z-10 -mx-1 space-y-1.5 bg-white/95 px-1 py-2 backdrop-blur">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <StatusBadge v-if="run.summary?.verdict" kind="verdict" :value="run.summary.verdict" />
        <StatusBadge kind="runTrigger" :value="run.trigger" />
        <StatusBadge kind="probeRunStatus" :value="run.status" />
        <span class="text-slate-600"
          >检测 <span class="num">#{{ run.id }}</span> · 上游 <span class="num">#{{ run.upstream_id }}</span> · 模型 {{ run.model }}</span
        >
        <span class="text-xs text-slate-500">{{ dateTime(run.queued_at) }}</span>
      </div>
      <p v-if="run.summary?.error" class="rounded border-l-2 border-red-500 bg-red-50 px-3 py-2 text-xs text-red-800">
        {{ run.summary.error }}
      </p>
    </div>

    <!-- 指标分组：结论 / 性能 / 智力与推理 -->
    <div class="grid gap-x-6 gap-y-4 lg:grid-cols-3">
      <section aria-label="结论与可用性">
        <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">结论</h4>
        <dl class="kv">
          <dt>答题</dt>
          <dd>
            共 <span class="num">{{ run.items }}</span> 题，通过 <span class="num">{{ run.passed }}</span> 题
            <template v-if="detail.reference"
              >；基线 <span class="num">#{{ detail.reference.upstream_id }}</span> 通过
              <span class="num">{{ detail.reference.passed }} / {{ detail.reference.items }}</span></template
            >
          </dd>
          <dt>可用性</dt>
          <dd>
            <template v-if="metrics?.availability"
              ><span class="num">{{ metrics.availability.success }} / {{ metrics.availability.total }}</span> 次成功</template
            >
            <template v-else>—</template>
          </dd>
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
      </section>
      <section aria-label="性能指标">
        <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">性能</h4>
        <dl class="kv">
          <dt>首字延迟</dt>
          <dd>
            <template v-if="metrics?.latency?.ttft_p50_ms !== undefined">
              中位 <span class="num">{{ ms(metrics.latency.ttft_p50_ms) }}</span> · P95
              <span class="num">{{ ms(metrics.latency.ttft_p95_ms) }}</span>
            </template>
            <template v-else>—</template>
          </dd>
          <dt>输出速度</dt>
          <dd>
            <template v-if="metrics?.latency?.output_tps"
              ><span class="num">{{ metrics.latency.output_tps.toFixed(1) }}</span> token/s</template
            >
            <template v-else>—</template>
          </dd>
        </dl>
      </section>
      <section aria-label="智力与推理指标">
        <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">智力与推理</h4>
        <dl class="kv">
          <dt>智力相对分</dt>
          <dd>
            <template v-if="metrics?.iq">
              <span class="num">{{ percent(metrics.iq.relative_score, 0) }}</span
              >（计分 <span class="num">{{ metrics.iq.samples }}</span> 题<template v-if="metrics.iq.confidence"
                >，置信度 <span class="num">{{ percent(metrics.iq.confidence, 0) }}</span></template
              >）
            </template>
            <template v-else>—</template>
          </dd>
          <dt>推理强度</dt>
          <dd>
            <template v-if="metrics?.reasoning"
              >相对基线 <span class="num">{{ percent(metrics.reasoning.ratio_to_baseline, 0) }}</span></template
            >
            <template v-else>—</template>
          </dd>
        </dl>
      </section>
    </div>

    <div v-if="suites.length">
      <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">按套件汇总</h4>
      <table class="w-full text-left text-xs">
        <thead class="text-slate-500">
          <tr>
            <th scope="col" class="p-2 font-medium">套件</th>
            <th scope="col" class="p-2 text-right font-medium">通过 / 题数</th>
            <th scope="col" class="p-2 text-right font-medium">错误</th>
            <th scope="col" class="p-2 text-right font-medium">平均耗时</th>
            <th scope="col" class="p-2 text-right font-medium">平均推理 token</th>
            <th v-if="detail.reference" scope="col" class="p-2 text-right font-medium">基线通过</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in suites" :key="s.suite" class="border-t border-slate-100">
            <td class="p-2 font-medium">
              {{ probeSuiteText(s.suite) }}
              <span v-if="probeSuiteText(s.suite) !== s.suite" class="ml-1 font-normal text-slate-400">{{ s.suite }}</span>
            </td>
            <td class="num p-2 text-right" :class="s.passed < s.items ? 'text-red-600' : 'text-emerald-700'">{{ s.passed }} / {{ s.items }}</td>
            <td class="num p-2 text-right" :class="s.errors ? 'text-red-600' : ''">{{ s.errors }}</td>
            <td class="num p-2 text-right">{{ ms(s.avgDuration) }}</td>
            <td class="num p-2 text-right">{{ s.avgReasoning.toFixed(0) }}</td>
            <td v-if="detail.reference" class="num p-2 text-right">{{ s.refPassed }} / {{ s.refItems }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div>
      <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">逐题结果</h4>
      <div v-if="rowGroups.length" class="max-h-[50vh] overflow-auto rounded border border-slate-100">
        <table class="w-full text-left text-xs" data-testid="probe-run-results">
          <thead class="sticky top-0 z-10 bg-white text-slate-500">
            <tr>
              <th scope="col" class="p-2 font-medium">题目</th>
              <th scope="col" class="p-2 font-medium">结果</th>
              <th v-if="detail.reference" scope="col" class="p-2 font-medium">基线</th>
              <th scope="col" class="p-2 text-right font-medium">首字 / 总时长</th>
              <th scope="col" class="p-2 text-right font-medium">输入 / 输出 / 推理 token</th>
              <th scope="col" class="p-2 font-medium">{{ showAnswer ? '模型输出 / 标准答案' : '模型输出' }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="g in rowGroups" :key="g.suite">
              <tr class="border-t border-slate-200 bg-slate-50">
                <td :colspan="detailColspan" class="px-2 py-1.5 font-medium text-slate-600">
                  {{ probeSuiteText(g.suite) }}
                  <span v-if="probeSuiteText(g.suite) !== g.suite" class="ml-1 font-normal text-slate-400">{{ g.suite }}</span>
                  <span class="ml-1 font-normal text-slate-400">（{{ g.rows.length }} 题）</span>
                </td>
              </tr>
              <tr
                v-for="{ row: r, index: i } in g.rows"
                :key="r.item_id !== undefined ? probeResultKey(r) : i"
                class="border-t border-slate-100 align-top"
                :class="rowTone(r)"
                :data-result-key="probeResultKey(r)"
              >
                <td class="p-2">
                  <div class="font-medium">#{{ r.item_id ?? '—' }} <span class="font-normal text-slate-500">{{ r.category }}</span></div>
                  <div class="text-slate-500">实例 {{ r.instance_idx ?? 0 }} · 步骤 {{ r.step ?? 0 }}<template v-if="r.check_key"> · {{ r.check_key }}</template></div>
                  <div v-if="r.generator_version" class="text-slate-500">{{ r.generator_version }}</div>
                  <div v-if="r.instance_hash" class="max-w-xs truncate font-mono text-slate-500" :title="r.instance_hash">哈希 {{ r.instance_hash }}</div>
                  <div class="max-w-xs truncate text-slate-500" :title="r.prompt">{{ r.prompt }}</div>
                  <div v-if="r.models.length" class="text-slate-500">限 {{ r.models.join(', ') }}</div>
                </td>
                <td class="p-2" :class="r.passed ? 'text-emerald-700' : 'text-red-600'">
                  {{ passText(r.passed) }}
                  <div v-if="r.error_class" class="text-slate-500">{{ r.error_class }}{{ r.http_status ? ` · ${r.http_status}` : '' }}</div>
                  <div v-if="r.response_model && r.response_model !== run.model" class="text-amber-700">返回 {{ r.response_model }}</div>
                  <div v-if="r.stop_reason || r.effort" class="text-slate-500">结束 {{ r.stop_reason || '—' }} · 强度 {{ r.effort || '—' }}</div>
                  <div v-if="r.flags?.length" class="text-slate-500">{{ r.flags.join('、') }}</div>
                </td>
                <td v-if="detail.reference" class="p-2" :class="r.ref?.passed ? 'text-emerald-700' : 'text-red-600'">{{ passText(r.ref?.passed) }}</td>
                <td class="num p-2 text-right whitespace-nowrap">{{ ms(r.ttft_ms) }} / {{ ms(r.duration_ms) }}</td>
                <td class="num p-2 text-right whitespace-nowrap">
                  {{ r.input_tokens ?? '未取得' }} / {{ r.output_tokens }} / {{ r.reasoning_tokens }}
                  <div class="text-slate-500">缓存读 {{ r.cache_read_tokens ?? '未取得' }} / 写 {{ r.cache_creation_tokens ?? '未取得' }}</div>
                </td>
                <td class="min-w-0 p-2">
                  <div :class="showAnswer ? 'grid gap-3 lg:grid-cols-2 lg:gap-4' : ''">
                    <div class="min-w-0">
                      <div v-if="showAnswer" class="mb-0.5 font-mono text-[10px] uppercase tracking-wide text-slate-400">模型输出</div>
                      <ProbeExcerpt :text="r.excerpt" />
                      <ProbeErrorBody :body="r.error_body" />
                    </div>
                    <div v-if="showAnswer" class="min-w-0">
                      <div class="mb-0.5 font-mono text-[10px] uppercase tracking-wide text-slate-400">标准答案</div>
                      <MarkdownText v-if="r.expected_answer" :text="r.expected_answer" :collapsed-lines="4" max-height-class="max-h-48" />
                      <span v-else class="text-slate-400">—</span>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <p v-else class="rounded border border-dashed border-slate-200 p-4 text-center text-xs text-slate-500">
        {{ run.status === 'done' ? '这次检测没有记录逐题结果。' : '检测尚未完成，逐题结果会在结束后显示。' }}
      </p>
      <p class="hint">回答片段只保存前 500 个字符；返回的模型名与请求不一致时标黄。</p>
    </div>
  </div>
</template>