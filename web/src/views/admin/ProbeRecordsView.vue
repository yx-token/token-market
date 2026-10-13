<script setup lang="ts">
// 全局检测记录：所有上游的检测（可看每次检测的各项指标与逐题结果），以及按题统计每道题在各次检测中的表现。
import { computed, onMounted, ref } from 'vue'
import { admin } from '@/api'
import type { ProbeItemResult, ProbeItemStat, ProbeRunDetail as Detail, ProbeRunRecord } from '@/api/types'
import { useLoader } from '@/composables/useAsync'
import { filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import DataTable from '@/components/DataTable.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import ProbeErrorBody from '@/components/ProbeErrorBody.vue'
import ProbeExcerpt from '@/components/ProbeExcerpt.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import TabBar from '@/components/TabBar.vue'
import { authTypeText, label, options } from '@/utils/labels'
import { compareProbeSuites, detectionFlowText, identitySourceText, probeFlagText, probeSuiteText } from '@/utils/inspection'
import { dateTime, ms, percent } from '@/utils/format'

const tab = ref<'runs' | 'items'>('runs')
const pageTitle = computed(() => (tab.value === 'runs' ? '检测记录' : '按题统计'))
const pageDescription = computed(() =>
  tab.value === 'runs'
    ? '所有上游的检测记录：各项指标、按套件的答题情况，点「明细」查看每道题的结果与回答片段。'
    : '每道题在筛选范围内的答题情况：被测上游与基线账号分开统计，通过率低的题更能区分降智。',
)
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  platform: '',
  model: '',
  trigger: '',
  verdict: '',
  startDate: '',
  endDate: '',
})
const serverQuery = () => ({
  platform: queryParams.platform || undefined,
  model: queryParams.model.trim() || undefined,
  from: queryParams.startDate || undefined,
  to: queryParams.endDate || undefined,
})

const records = useLoader(
  () => admin.probeRecords({ ...serverQuery(), trigger: queryParams.trigger || undefined, verdict: queryParams.verdict || undefined, limit: 500 }),
  [] as ProbeRunRecord[] | null,
)
const stats = useLoader(() => admin.probeItemStats({ ...serverQuery(), trigger: queryParams.trigger || undefined }), [] as ProbeItemStat[] | null)
onMounted(() => {
  records.load()
  stats.load()
})

const filteredRuns = computed(() =>
  filterByKeyword(records.data.value, queryParams.keyword, ['id', 'upstream_id', 'account_email', 'model', 'models'] as (keyof ProbeRunRecord)[]),
)
const runPage = useClientPage(filteredRuns)
const filteredStats = computed(() =>
  filterByKeyword(stats.data.value, queryParams.keyword, ['item_id', 'suite', 'category', 'prompt'] as (keyof ProbeItemStat)[]),
)
const statPage = useClientPage(filteredStats)
const pager = computed(() => (tab.value === 'runs' ? runPage : statPage))

function search() {
  handleQuery()
  runPage.resetPage()
  statPage.resetPage()
  records.load()
  stats.load()
}
function onReset() {
  resetQuery()
  search()
}

function suiteText(r: ProbeRunRecord) {
  return Object.entries(r.suites ?? {})
    .sort(([a], [b]) => compareProbeSuites(a, b))
    .map(([s, c]) => ({ suite: s, text: `${probeSuiteText(s)} ${c.passed}/${c.items}`, bad: c.passed < c.items }))
}
function rate(passed: number, asked: number) {
  return asked ? passed / asked : null
}

// 单次检测的明细
const runDetail = ref<Detail | null>(null)
const runFailed = ref(false)
const showRun = ref(false)
let lastRunId = 0
async function openRun(r: { id?: number; run_id?: number }) {
  lastRunId = (r.run_id ?? r.id)!
  runDetail.value = null
  runFailed.value = false
  showRun.value = true
  try {
    runDetail.value = await admin.probeRun(lastRunId)
  } catch {
    runFailed.value = true
  }
}

// 一道题在各次检测中的结果
const itemOf = ref<ProbeItemStat | null>(null)
const itemResults = ref<ProbeItemResult[]>([])
const itemLoading = ref(false)
const showItem = ref(false)
async function openItem(s: ProbeItemStat) {
  itemOf.value = s
  itemResults.value = []
  itemLoading.value = true
  showItem.value = true
  try {
    itemResults.value = (await admin.probeItemResults(s.item_id, { ...serverQuery(), limit: 100 })) ?? []
  } finally {
    itemLoading.value = false
  }
}
</script>

<template>
  <ConsoleListPage id="console-probe-records-page" :title="pageTitle" :description="pageDescription">
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'runs', text: '检测记录' },
          { key: 'items', text: '按题统计' },
        ]"
      />
    </template>

    <template #filters>
      <div class="filter-item">
        <label class="filter-label" for="pr-keyword">关键字</label>
        <div class="relative w-full flex-1">
          <input
            id="pr-keyword"
            v-model="queryParams.keyword"
            class="filter-input w-full"
            :placeholder="tab === 'runs' ? '检测 / 上游 / 邮箱 / 模型' : '题号 / 套件 / 类别 / 题目'"
            @keyup.enter="search"
          />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label" for="pr-platform">厂商</label>
        <select id="pr-platform" v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label" for="pr-model">模型</label>
        <input id="pr-model" v-model="queryParams.model" class="filter-input flex-1" placeholder="例如 gpt-6-astra" @keyup.enter="search" />
      </div>
      <div class="filter-item">
        <label class="filter-label" for="pr-trigger">触发</label>
        <select id="pr-trigger" v-model="queryParams.trigger" class="filter-input flex-1">
          <option value="">全部（不含基线对照）</option>
          <option v-for="o in options('runTrigger')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div v-if="tab === 'runs'" class="filter-item">
        <label class="filter-label" for="pr-verdict">结论</label>
        <select id="pr-verdict" v-model="queryParams.verdict" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('verdict')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">检测日</label>
        <div class="flex w-full flex-1 items-center gap-2">
          <DateRangePicker v-model:start-date="queryParams.startDate" v-model:end-date="queryParams.endDate" />
        </div>
      </div>
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      v-if="tab === 'runs'"
      :columns="[
        { key: 'queued_at', label: '时间' },
        { key: 'upstream', label: '上游' },
        { key: 'model', label: '厂商 / 模型' },
        { key: 'flows', label: '流程' },
        { key: 'identity_source', label: '身份来源' },
        { key: 'models', label: '测试模型' },
        { key: 'trigger', label: '触发' },
        { key: 'verdict', label: '结论' },
        { key: 'answers', label: '答题' },
        { key: 'iq', label: '智力相对分', align: 'right' },
        { key: 'latency', label: '首字延迟 / 输出速度', align: 'right' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="runPage.pageRows.value"
      :loading="records.loading.value"
      row-key="id"
      :empty="records.data.value === null ? '检测记录加载失败，请重试' : '没有符合条件的检测记录'"
    >
      <template #cell-queued_at="{ row }">
        <span class="num">{{ dateTime(row.queued_at) }}</span>
        <div class="num text-xs text-ink-500">#{{ row.id }}</div>
      </template>
      <template #cell-upstream="{ row }">
        <span class="num">#{{ row.upstream_id }}</span><span v-if="row.is_reference" class="ml-1 text-xs text-sky-700">基线</span>
        <div v-if="row.account_email" class="max-w-[12rem] truncate text-xs text-ink-500" :title="row.account_email">{{ row.account_email }}</div>
      </template>
      <template #cell-model="{ row }">
        {{ label('platform', row.platform).text }} · {{ authTypeText(row.platform, row.auth_type) }}
        <div class="text-xs text-ink-500">{{ row.model }}</div>
      </template>
      <template #cell-flows="{ row }">{{ row.flows?.length ? row.flows.map(detectionFlowText).join(' / ') : '—' }}</template>
      <template #cell-identity_source="{ row }">{{ row.identity_source ? identitySourceText(row.identity_source) : '—' }}</template>
      <template #cell-models="{ row }">{{ row.models?.length ? row.models.join(' / ') : row.model || '—' }}</template>
      <template #cell-trigger="{ row }"><StatusBadge kind="runTrigger" :value="row.trigger" /></template>
      <template #cell-verdict="{ row }">
        <StatusBadge v-if="row.summary?.verdict" kind="verdict" :value="row.summary.verdict" />
        <StatusBadge v-else kind="probeRunStatus" :value="row.status" />
        <div v-if="row.summary?.error" class="max-w-[14rem] truncate text-xs text-red-600" :title="row.summary.error">{{ row.summary.error }}</div>
      </template>
      <template #cell-answers="{ row }">
        <span class="num" :class="row.passed < row.items ? 'text-red-600' : ''">通过 {{ row.passed }} / 已答 {{ row.items }} / 共 {{ row.planned ?? '—' }}</span>
        <div class="num text-xs">
          <span v-for="s in suiteText(row)" :key="s.suite" class="mr-2" :class="s.bad ? 'text-red-600' : 'text-ink-500'">{{ s.text }}</span>
        </div>
      </template>
      <template #cell-iq="{ row }">
        <span class="num">{{ row.summary?.suites?.iq ? percent(row.summary.suites.iq.relative_score, 0) : '—' }}</span>
        <div v-if="row.summary?.suites?.reasoning" class="num text-xs text-ink-500">推理 {{ percent(row.summary.suites.reasoning.ratio_to_baseline, 0) }}</div>
      </template>
      <template #cell-latency="{ row }">
        <span class="num">{{ ms(row.summary?.suites?.latency?.ttft_p50_ms) }}</span>
        <div class="num text-xs text-ink-500">
          {{ row.summary?.suites?.latency?.output_tps ? `${row.summary.suites.latency.output_tps.toFixed(1)} token/s` : '—' }}
        </div>
      </template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" @click="openRun(row)">明细</button>
      </template>
    </DataTable>

    <DataTable
      v-else
      :columns="[
        { key: 'item', label: '题目' },
        { key: 'suite', label: '套件 / 类别' },
        { key: 'rate', label: '被测通过率', align: 'right' },
        { key: 'ref', label: '基线通过率', align: 'right' },
        { key: 'errors', label: '错误', align: 'right' },
        { key: 'avg', label: '平均耗时 / 推理 token', align: 'right' },
        { key: 'last', label: '最近测试' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="statPage.pageRows.value"
      :loading="stats.loading.value"
      row-key="item_id"
      :empty="stats.data.value === null ? '按题统计加载失败，请重试' : '筛选范围内还没有答题记录'"
    >
      <template #cell-item="{ row }">
        <div class="font-medium">#{{ row.item_id }}<span v-if="row.status !== 'active'" class="ml-1 text-xs text-ink-500">（已下线）</span></div>
        <div class="max-w-xs truncate text-xs text-ink-500" :title="row.prompt">{{ row.prompt }}</div>
        <div v-if="row.models.length" class="text-xs text-ink-500">限 {{ row.models.join(', ') }}</div>
      </template>
      <template #cell-suite="{ row }">
        {{ probeSuiteText(row.suite) }}<span v-if="probeSuiteText(row.suite) !== row.suite" class="ml-1 text-xs text-ink-400">{{ row.suite }}</span>
        <div class="text-xs text-ink-500">{{ row.category }}</div>
      </template>
      <template #cell-rate="{ row }">
        <span class="num" :class="row.passed < row.asked ? 'text-red-600' : 'text-emerald-700'">{{ percent(rate(row.passed, row.asked), 0) }}</span>
        <div class="num text-xs text-ink-500">{{ row.passed }} / {{ row.asked }}</div>
      </template>
      <template #cell-ref="{ row }">
        <template v-if="row.ref_asked">
          <span class="num">{{ percent(rate(row.ref_passed, row.ref_asked), 0) }}</span>
          <div class="num text-xs text-ink-500">{{ row.ref_passed }} / {{ row.ref_asked }}</div>
        </template>
        <template v-else>—</template>
      </template>
      <template #cell-errors="{ row }"><span class="num">{{ row.errors }}</span></template>
      <template #cell-avg="{ row }">
        <span class="num">{{ ms(row.avg_duration_ms) }}</span>
        <div class="num text-xs text-ink-500">{{ row.avg_reasoning_tokens.toFixed(0) }}</div>
      </template>
      <template #cell-last="{ row }"><span class="num">{{ dateTime(row.last_asked_at) }}</span></template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" @click="openItem(row)">各次结果</button>
      </template>
    </DataTable>

    <template #footer>
      <span class="mr-auto text-xs text-gray-500">共 <span class="num">{{ pager.total.value }}</span> 条{{ tab === 'runs' ? '（最多显示最近 500 次检测）' : '' }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pager.pageNum.value <= 1" @click="pager.pageNum.value--">上一页</button>
      <span class="num text-xs text-gray-500">{{ pager.pageNum.value }} / {{ Math.max(1, Math.ceil(pager.total.value / pager.pageSize.value)) }}</span>
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="pager.pageNum.value * pager.pageSize.value >= pager.total.value"
        @click="pager.pageNum.value++"
      >
        下一页
      </button>
    </template>
  </ConsoleListPage>

  <ModalDialog v-model:open="showRun" :title="runDetail ? `检测 #${runDetail.run.id}` : '检测明细'" size="xl">
    <ProbeRunDetail v-if="runDetail" :detail="runDetail" show-answer />
    <div v-else-if="runFailed" class="flex flex-col items-center gap-3 py-10 text-sm text-slate-500">
      <p>检测明细加载失败，请重试。</p>
      <button type="button" class="btn btn-secondary btn-sm" @click="openRun({ id: lastRunId })">重新加载</button>
    </div>
    <div v-else class="space-y-4 py-2" aria-busy="true" aria-label="正在加载检测明细">
      <div class="flex items-center gap-2">
        <span class="h-5 w-14 animate-pulse rounded-full bg-slate-200" />
        <span class="h-5 w-14 animate-pulse rounded-full bg-slate-200" />
        <span class="h-4 w-64 animate-pulse rounded bg-slate-100" />
      </div>
      <div class="grid gap-4 md:grid-cols-3">
        <div v-for="i in 3" :key="i" class="space-y-2">
          <div class="h-3 w-16 animate-pulse rounded bg-slate-200" />
          <div class="h-24 animate-pulse rounded bg-slate-100" />
        </div>
      </div>
      <div class="h-48 animate-pulse rounded bg-slate-100" />
      <p class="text-xs text-slate-400">加载中…</p>
    </div>
    <template #footer>
      <button class="btn-primary" @click="showRun = false">关闭</button>
    </template>
  </ModalDialog>

  <ModalDialog v-model:open="showItem" :title="itemOf ? `题目 #${itemOf.item_id} 的各次结果` : '各次结果'" wide>
    <div v-if="itemOf" class="space-y-3 text-sm">
      <div class="whitespace-pre-wrap rounded bg-slate-50 p-2 text-xs text-slate-700">{{ itemOf.prompt }}</div>
      <div class="max-h-[55vh] overflow-auto">
        <table class="w-full text-left text-xs">
          <thead class="sticky top-0 z-10 bg-white text-slate-500">
            <tr>
              <th scope="col" class="p-2 font-medium">时间</th>
              <th scope="col" class="p-2 font-medium">上游 / 模型</th>
              <th scope="col" class="p-2 font-medium">触发</th>
              <th scope="col" class="p-2 font-medium">结果</th>
              <th scope="col" class="p-2 text-right font-medium">首字 / 总时长</th>
              <th scope="col" class="p-2 text-right font-medium">输入 / 输出 / 推理 token</th>
              <th scope="col" class="p-2 font-medium">回答片段</th>
              <th scope="col" class="p-2"><span class="sr-only">操作</span></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in itemResults"
              :key="`${r.run_id}-${r.instance_idx ?? 0}-${r.step ?? 0}`"
              class="border-t border-slate-100 align-top"
              :class="r.passed === false || r.error_class ? 'bg-red-50/70' : ''"
            >
              <td class="num p-2 whitespace-nowrap">{{ dateTime(r.created_at) }}</td>
              <td class="p-2">
                <span class="num">#{{ r.upstream_id }}</span> · {{ label('platform', r.platform).text }}
                <div class="max-w-[12rem] truncate text-slate-500" :title="r.account_email">{{ r.account_email }}</div>
                <div class="text-slate-500">{{ r.model }}<span v-if="r.response_model && r.response_model !== r.model" class="text-amber-700">（返回 {{ r.response_model }}）</span></div>
                <div class="text-slate-500">{{ r.check_key || '旧题' }} · 实例 {{ r.instance_idx ?? 0 }} · 步骤 {{ r.step ?? 0 }}</div>
                <div v-if="r.generator_version" class="text-slate-500">{{ r.generator_version }}</div>
                <div v-if="r.instance_hash" class="max-w-[12rem] truncate font-mono text-slate-500" :title="r.instance_hash">{{ r.instance_hash }}</div>
              </td>
              <td class="p-2"><StatusBadge kind="runTrigger" :value="r.trigger" /></td>
              <td class="p-2" :class="r.passed ? 'text-emerald-700' : 'text-red-600'">
                {{ r.passed === null ? '—' : r.passed ? '通过' : '未通过' }}
                <div v-if="r.error_class" class="text-slate-500">{{ r.error_class }}{{ r.http_status ? ` · ${r.http_status}` : '' }}</div>
                <div v-if="r.stop_reason || r.effort" class="text-slate-500">{{ r.stop_reason || '—' }} · {{ r.effort || '—' }}</div>
                <div v-if="r.flags?.length" class="text-slate-500" :title="r.flags.join(', ')">{{ r.flags.map(probeFlagText).join('、') }}</div>
              </td>
              <td class="num p-2 text-right whitespace-nowrap">{{ ms(r.ttft_ms) }} / {{ ms(r.duration_ms) }}</td>
              <td class="num p-2 text-right whitespace-nowrap">
                {{ r.input_tokens ?? '未取得' }} / {{ r.output_tokens }} / {{ r.reasoning_tokens }}
                <div class="text-slate-500">缓存读 {{ r.cache_read_tokens ?? '未取得' }} / 写 {{ r.cache_creation_tokens ?? '未取得' }}</div>
              </td>
              <td class="min-w-0 p-2">
                <ProbeExcerpt :text="r.excerpt" />
                <ProbeErrorBody :body="r.error_body" />
              </td>
              <td class="p-2"><button class="btn-ghost btn-sm" @click="openRun(r)">整次检测</button></td>
            </tr>
          </tbody>
        </table>
        <div v-if="itemLoading" class="flex items-center gap-2 p-3 text-xs text-slate-400" aria-busy="true">
          <span v-for="i in 3" :key="i" class="h-1.5 w-1.5 animate-pulse rounded-full bg-slate-300" :style="{ animationDelay: `${i * 160}ms` }" />
          载入中
        </div>
        <div v-else-if="!itemResults.length" class="p-3 text-center text-xs text-slate-500">这道题在筛选范围内还没有答题记录。</div>
      </div>
      <p class="hint">最多显示最近 100 条，按上方的厂商、模型与日期筛选。</p>
    </div>
    <template #footer>
      <button class="btn-primary" @click="showItem = false">关闭</button>
    </template>
  </ModalDialog>
</template>
