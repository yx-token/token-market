<script setup lang="ts">
// 一次检测的完整结果（仅管理员可见）：结论概要、性能与智力指标、按套件汇总，以及每道题的结果与回答片段。
// 标准答案是私有内容，只在管理员上下文传 show-answer 展示；卖家/上游侧复用本组件时默认不出现。
import { computed, ref } from 'vue'
import type { ProbeRunDetail, ProbeRunResult } from '@/api/types'
import MarkdownText from '@/components/MarkdownText.vue'
import ProbeErrorBody from '@/components/ProbeErrorBody.vue'
import ProbeExcerpt from '@/components/ProbeExcerpt.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { dateTime, ms, percent } from '@/utils/format'
import { compareProbeSuites, detectionFlowText, identitySourceText, intelligenceStatusText, probeFlagText, probeResultKey, probeSuiteText } from '@/utils/inspection'

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
  const groups = new Map<string, { target: ProbeRunResult[]; ref: ProbeRunResult[] }>()
  for (const r of props.detail.results) {
    if (!groups.has(r.suite)) groups.set(r.suite, { target: [], ref: [] })
    groups.get(r.suite)!.target.push(r)
  }
  for (const r of props.detail.reference_results ?? []) groups.get(r.suite)?.ref.push(r)
  return [...groups.entries()]
    .sort(([a], [b]) => compareProbeSuites(a, b))
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

// 逐题结果按「套件 → 题 → 记录」分组。参数化题一道题会现场生成多个实例，多步题每步一条，
// D06 还会把 D07 实例用低推理档重答一次（实例号 +1000），所以同一道题在一次检测里有多条记录；
// 多条记录的题折叠成一行汇总，展开后看各条。套件按现行顺序排列，题目保留返回顺序。
type Row = (typeof rows.value)[number]
interface ItemGroup {
  key: string
  first: Row
  rows: Row[]
  passed: number
  errors: number
  refPassed: number
  refTotal: number
  instances: number
  steps: number
  lowEffort: number
  checkKeys: string[]
  avgTtft: number
  avgDuration: number
}
type Entry =
  | { type: 'suite'; key: string; suite: string; items: number; records: number }
  | { type: 'item'; key: string; group: ItemGroup }
  | { type: 'row'; key: string; row: Row; groupKey: string | null }

function isLowEffort(r: ProbeRunResult) {
  return r.check_key === 'D06' && (r.instance_idx ?? 0) >= 1000
}
function instanceLabel(r: ProbeRunResult) {
  const idx = r.instance_idx ?? 0
  return `${isLowEffort(r) ? `低推理档对照 · 实例 ${idx - 1000}` : `实例 ${idx}`} · 步骤 ${r.step ?? 0}`
}

const entries = computed<Entry[]>(() => {
  const suiteOrder: string[] = []
  const bySuite = new Map<string, Map<string, Row[]>>()
  rows.value.forEach((row, index) => {
    if (!bySuite.has(row.suite)) {
      bySuite.set(row.suite, new Map())
      suiteOrder.push(row.suite)
    }
    const items = bySuite.get(row.suite)!
    const itemKey = row.item_id !== undefined ? `item:${row.item_id}` : `row:${index}`
    if (!items.has(itemKey)) items.set(itemKey, [])
    items.get(itemKey)!.push(row)
  })
  const out: Entry[] = []
  for (const suite of suiteOrder.sort(compareProbeSuites)) {
    const items = bySuite.get(suite)!
    let records = 0
    for (const list of items.values()) records += list.length
    out.push({ type: 'suite', key: `suite:${suite}`, suite, items: items.size, records })
    for (const [itemKey, list] of items) {
      // 同一道题的各条记录按实例、步骤排列，返回顺序是完成先后
      list.sort((a, b) => (a.instance_idx ?? 0) - (b.instance_idx ?? 0) || (a.step ?? 0) - (b.step ?? 0))
      if (list.length === 1) {
        out.push({ type: 'row', key: `${suite}|${itemKey}`, row: list[0], groupKey: null })
        continue
      }
      const key = `${suite}|${itemKey}`
      const ok = list.filter((r) => !r.error_class)
      const refs = list.filter((r) => r.ref)
      out.push({
        type: 'item',
        key,
        group: {
          key,
          first: list[0],
          rows: list,
          passed: list.filter((r) => r.passed).length,
          errors: list.length - ok.length,
          refPassed: refs.filter((r) => r.ref?.passed).length,
          refTotal: refs.length,
          instances: new Set(list.map((r) => r.instance_idx ?? 0)).size,
          steps: new Set(list.map((r) => r.step ?? 0)).size,
          lowEffort: list.filter(isLowEffort).length,
          checkKeys: [...new Set(list.map((r) => r.check_key).filter((k): k is string => !!k))],
          avgTtft: avg(ok.map((r) => r.ttft_ms)),
          avgDuration: avg(ok.map((r) => r.duration_ms)),
        },
      })
      for (const row of list) out.push({ type: 'row', key: `${key}|${probeResultKey(row)}`, row, groupKey: key })
    }
  }
  return out
})

const groupKeys = computed(() => entries.value.flatMap((e) => (e.type === 'item' ? [e.key] : [])))
const expanded = ref(new Set<string>())
function toggleGroup(key: string) {
  const next = new Set(expanded.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expanded.value = next
}
const allExpanded = computed(() => groupKeys.value.length > 0 && groupKeys.value.every((k) => expanded.value.has(k)))
function toggleAll() {
  expanded.value = allExpanded.value ? new Set() : new Set(groupKeys.value)
}

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
    <div class="sticky -top-5 z-20 -mx-5 space-y-1.5 border-b border-slate-200/70 bg-white/80 px-5 py-2 backdrop-blur">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <StatusBadge v-if="run.summary?.verdict" kind="verdict" :value="run.summary.verdict" />
        <StatusBadge kind="runTrigger" :value="run.trigger" />
        <StatusBadge kind="probeRunStatus" :value="run.status" />
        <span class="text-slate-600"
          >检测 <span class="num">#{{ run.id }}</span> · 上游 <span class="num">#{{ run.upstream_id }}</span> · 测试模型 {{ run.models?.length ? run.models.join(' / ') : run.model }}</span
        >
        <span class="text-xs text-slate-500">{{ dateTime(run.queued_at) }}</span>
      </div>
      <p v-if="run.flows?.length || run.identity_source" class="text-xs text-slate-500">
        <span v-if="run.flows?.length">流程：{{ run.flows.map(detectionFlowText).join(' / ') }}</span>
        <span v-if="run.identity_source" class="ml-3">身份来源：{{ identitySourceText(run.identity_source) }}</span>
      </p>
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
            通过 <span class="num">{{ run.passed }}</span> / 已答 <span class="num">{{ run.items }}</span> / 共
            <span class="num">{{ run.planned ?? '—' }}</span> 题
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
      <section v-if="run.summary?.intelligence" aria-label="测智指标">
        <h4 class="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">测智</h4>
        <p>{{ intelligenceStatusText(run.summary.intelligence.status) }}</p>
        <p v-if="run.summary.intelligence.inherited_at" class="mt-1 text-xs text-slate-500">沿用 {{ dateTime(run.summary.intelligence.inherited_at) }} 的结果</p>
        <dl class="mt-2 space-y-2">
          <div v-for="model in run.summary.intelligence.models" :key="model.model">
            <dt class="break-all font-medium">{{ model.model }}</dt>
            <dd>
              {{ intelligenceStatusText(model.status) }}
              <span v-if="showAnswer" class="block text-xs text-slate-500">
                通过率 {{ percent(model.target_pass_rate) }} · 期望通过率 {{ percent(model.expected_pass_rate) }}
              </span>
            </dd>
          </div>
        </dl>
      </section>
      <section v-else aria-label="智力与推理指标">
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
            <th scope="col" class="p-2 text-right font-medium">通过 / 记录数</th>
            <th scope="col" class="p-2 text-right font-medium">错误</th>
            <th scope="col" class="p-2 text-right font-medium">平均耗时</th>
            <th scope="col" class="p-2 text-right font-medium">平均推理 token</th>
            <th v-if="detail.reference" scope="col" class="p-2 text-right font-medium">基线通过</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in suites" :key="s.suite" class="border-t border-slate-100">
            <td class="p-2 font-medium" :title="s.suite">
              {{ probeSuiteText(s.suite) }}
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
      <div class="mb-1 flex items-center justify-between gap-2">
        <h4 class="text-xs font-semibold uppercase tracking-wide text-slate-500">逐题结果</h4>
        <button v-if="groupKeys.length" type="button" class="text-xs text-sky-700 hover:underline" @click="toggleAll">
          {{ allExpanded ? '全部收起' : '全部展开' }}
        </button>
      </div>
      <div v-if="entries.length" class="rounded border border-slate-100">
        <!-- 固定列宽，把空间留给模型输出与标准答案对照；内容在单元格内折行，不横向滚动 -->
        <table class="w-full table-fixed text-left text-xs [overflow-wrap:anywhere]" data-testid="probe-run-results">
          <thead class="bg-slate-50/60 text-slate-500">
            <tr>
              <th scope="col" class="w-[20%] p-2 font-medium">题目</th>
              <th scope="col" class="w-[12%] p-2 font-medium">结果</th>
              <th v-if="detail.reference" scope="col" class="w-[6%] p-2 font-medium">基线</th>
              <th scope="col" class="w-[8%] p-2 text-right font-medium">首字 / 总时长</th>
              <th scope="col" class="w-[12%] p-2 text-right font-medium">输入 / 输出 / 推理 token</th>
              <th scope="col" class="p-2 font-medium">{{ showAnswer ? '模型输出 / 标准答案' : '模型输出' }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="e in entries" :key="e.key">
              <tr v-if="e.type === 'suite'" class="border-t border-slate-200 bg-slate-50">
                <td :colspan="detailColspan" class="px-2 py-1.5 font-medium text-slate-600" :title="e.suite">
                  {{ probeSuiteText(e.suite) }}
                  <span class="ml-1 font-normal text-slate-400"
                    >（{{ e.items }} 题<template v-if="e.records !== e.items"> · {{ e.records }} 条记录</template>）</span
                  >
                </td>
              </tr>

              <tr
                v-else-if="e.type === 'item'"
                class="border-t border-slate-100 align-top"
                :class="e.group.passed < e.group.rows.length ? 'bg-red-50/40' : ''"
                :data-item-group="e.key"
              >
                <td class="p-2">
                  <button
                    type="button"
                    class="flex w-full items-start gap-1.5 text-left"
                    :aria-expanded="expanded.has(e.key)"
                    @click="toggleGroup(e.key)"
                  >
                    <span aria-hidden="true" class="mt-0.5 inline-block text-[9px] text-slate-400 transition-transform" :class="expanded.has(e.key) ? 'rotate-90' : ''">▶</span>
                    <span class="min-w-0">
                      <span class="font-medium">#{{ e.group.first.item_id ?? '—' }}</span>
                      <span class="ml-1 text-slate-500">{{ e.group.first.category }}</span>
                      <span class="block text-slate-500">
                        {{ e.group.rows.length }} 条记录：{{ e.group.instances }} 个实例<template v-if="e.group.steps > 1"> × {{ e.group.steps }} 步</template
                        ><template v-if="e.group.lowEffort">，含低推理档对照 {{ e.group.lowEffort }} 条</template>
                      </span>
                      <span v-if="e.group.checkKeys.length || e.group.first.generator_version" class="block text-slate-500"
                        >{{ e.group.checkKeys.join(' / ') }}<template v-if="e.group.checkKeys.length && e.group.first.generator_version"> · </template
                        >{{ e.group.first.generator_version }}</span
                      >
                      <span class="block max-w-full truncate text-slate-500" :title="e.group.first.prompt">{{ e.group.first.prompt }}</span>
                    </span>
                  </button>
                </td>
                <td class="num p-2" :class="e.group.passed === e.group.rows.length ? 'text-emerald-700' : 'text-red-600'">
                  通过 {{ e.group.passed }} / {{ e.group.rows.length }}
                  <div v-if="e.group.errors" class="text-slate-500">错误 {{ e.group.errors }}</div>
                </td>
                <td v-if="detail.reference" class="num p-2 text-slate-600">
                  <template v-if="e.group.refTotal">{{ e.group.refPassed }} / {{ e.group.refTotal }}</template>
                  <template v-else>—</template>
                </td>
                <td class="num p-2 text-right text-slate-600">
                  <div>均 {{ ms(e.group.avgTtft) }}</div>
                  <div>/ {{ ms(e.group.avgDuration) }}</div>
                </td>
                <td class="p-2 text-right text-slate-400">—</td>
                <td class="p-2 text-slate-400">{{ expanded.has(e.key) ? '各条记录见下方' : '展开查看各条记录' }}</td>
              </tr>

              <tr
                v-else
                v-show="!e.groupKey || expanded.has(e.groupKey)"
                class="border-t border-slate-100 align-top"
                :class="rowTone(e.row)"
                :data-result-key="probeResultKey(e.row)"
              >
                <td class="p-2" :class="e.groupKey ? 'pl-7' : ''">
                  <template v-if="!e.groupKey">
                    <div class="font-medium">#{{ e.row.item_id ?? '—' }} <span class="font-normal text-slate-500">{{ e.row.category }}</span></div>
                    <div class="text-slate-500">{{ instanceLabel(e.row) }}<template v-if="e.row.check_key"> · {{ e.row.check_key }}</template></div>
                    <div v-if="e.row.generator_version" class="text-slate-500">{{ e.row.generator_version }}</div>
                  </template>
                  <div v-else class="text-slate-600">{{ instanceLabel(e.row) }}<template v-if="e.row.check_key"> · {{ e.row.check_key }}</template></div>
                  <div v-if="e.row.instance_hash" class="max-w-full truncate font-mono text-slate-500" :title="e.row.instance_hash">哈希 {{ e.row.instance_hash }}</div>
                  <div v-if="showAnswer && e.row.instance_prompt" class="mt-1 max-w-full">
                    <div class="mb-0.5 font-mono text-[10px] uppercase tracking-wide text-slate-400">实际题面</div>
                    <MarkdownText :text="e.row.instance_prompt" :collapsed-lines="2" max-height-class="max-h-40" />
                  </div>
                  <div v-else-if="!e.groupKey" class="max-w-full truncate text-slate-500" :title="e.row.prompt">{{ e.row.prompt }}</div>
                  <div v-if="!e.groupKey && e.row.models.length" class="text-slate-500">限 {{ e.row.models.join(', ') }}</div>
                </td>
                <td class="p-2" :class="e.row.passed ? 'text-emerald-700' : 'text-red-600'">
                  {{ passText(e.row.passed) }}
                  <div v-if="e.row.error_class" class="text-slate-500">{{ e.row.error_class }}{{ e.row.http_status ? ` · ${e.row.http_status}` : '' }}</div>
                  <div v-if="e.row.response_model && e.row.response_model !== run.model" class="text-amber-700">返回 {{ e.row.response_model }}</div>
                  <div v-if="e.row.stop_reason || e.row.effort" class="text-slate-500">结束 {{ e.row.stop_reason || '—' }} · 强度 {{ e.row.effort || '—' }}</div>
                  <div v-if="e.row.flags?.length" class="text-slate-500" :title="e.row.flags.join(', ')">{{ e.row.flags.map(probeFlagText).join('、') }}</div>
                </td>
                <td v-if="detail.reference" class="p-2" :class="e.row.ref?.passed ? 'text-emerald-700' : 'text-red-600'">{{ passText(e.row.ref?.passed) }}</td>
                <td class="num p-2 text-right">
                  <div>{{ ms(e.row.ttft_ms) }}</div>
                  <div>/ {{ ms(e.row.duration_ms) }}</div>
                </td>
                <td class="num p-2 text-right">
                  <div>{{ e.row.input_tokens ?? '未取得' }} / {{ e.row.output_tokens }} / {{ e.row.reasoning_tokens }}</div>
                  <div class="text-slate-500">缓存读 {{ e.row.cache_read_tokens ?? '未取得' }}</div>
                  <div class="text-slate-500">缓存写 {{ e.row.cache_creation_tokens ?? '未取得' }}</div>
                </td>
                <td class="min-w-0 p-2">
                  <div :class="showAnswer ? 'grid gap-3 lg:grid-cols-2 lg:gap-4' : ''">
                    <div class="min-w-0">
                      <div v-if="showAnswer" class="mb-0.5 font-mono text-[10px] uppercase tracking-wide text-slate-400">模型输出</div>
                      <ProbeExcerpt :text="e.row.excerpt" />
                      <ProbeErrorBody :body="e.row.error_body" />
                    </div>
                    <div v-if="showAnswer" class="min-w-0">
                      <div class="mb-0.5 font-mono text-[10px] uppercase tracking-wide text-slate-400">标准答案</div>
                      <MarkdownText v-if="e.row.expected_answer" :text="e.row.expected_answer" :collapsed-lines="4" max-height-class="max-h-48" />
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
