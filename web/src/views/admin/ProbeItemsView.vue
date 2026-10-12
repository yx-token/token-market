<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { admin, seller } from '@/api'
import type { GeneratorTemplate, Grader, InspectionCheckDefinition, ProbeGenerator, ProbeItem, ProbeRequest, Upstream, VendorConfig, VendorView } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { filterByKeyword, useClientPage, useListQuery } from '@/composables/useListQuery'
import { useToast } from '@/stores/toast'
import ConsoleListPage from '@/components/console/ConsoleListPage.vue'
import DataTable from '@/components/DataTable.vue'
import SearchSelect from '@/components/common/SearchSelect.vue'
import type { SelectOption } from '@/components/common/searchSelect'
import TabBar from '@/components/TabBar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { authTypeText, label, options } from '@/utils/labels'

const toast = useToast()
const tab = ref<'items' | 'refs'>('items')
const pageTitle = computed(() => (tab.value === 'items' ? '题库' : '基线账号'))
const pageDescription = computed(() =>
  tab.value === 'items'
    ? '题库私有、定期轮换；试运行只记录新题，正式模式将新题接入判定。'
    : '平台自备的干净官方账号，与被测上游同批次答题作对照。',
)
const { queryParams, resetQuery, handleQuery } = useListQuery({
  keyword: '',
  status: 'active',
  suite: '',
  platform: '',
  template: '',
  generatorVersion: '',
})

const items = useLoader(() => admin.items(queryParams.status), [] as ProbeItem[] | null)
const refs = useLoader(() => admin.references(), [] as Upstream[] | null)
const checks = ref<InspectionCheckDefinition[]>([])
// 质检项来自后端注册表；已存题目若引用了未注册的键，仍保留为选项以便看清并改正。
const checkOptions = computed(() => {
  const list = checks.value.map((c) => ({ value: c.key, text: `${c.key} · ${c.name}` }))
  if (form.checkKey && !list.some((o) => o.value === form.checkKey)) list.unshift({ value: form.checkKey, text: `${form.checkKey}（未注册）` })
  return list
})
const { busy, run } = useAction()
const editing = ref<ProbeItem | null>(null)
const show = ref(false)
function initialForm() {
  return {
    suite: 'iq', category: '', prompt: '', grader: '{"type": "exact", "answer": ""}',
    platforms: [] as string[], models: [] as string[], maxTokens: 1024, effort: '', checkKey: '',
    request: '{}', generator: '{}', modes: [] as ('full' | 'quick')[], weight: 1,
  }
}
const form = reactive(initialForm())
const suiteOptions = [
  { value: 'iq', text: '旧智力题' },
  { value: 'reasoning', text: '旧推理题' },
  { value: 'long_context', text: '旧长上下文题' },
  { value: 'inspection_authenticity', text: '真伪核验' },
  { value: 'inspection_capability', text: '能力质检' },
  { value: 'inspection_context', text: '上下文质检' },
  { value: 'inspection_function', text: '功能质检' },
  { value: 'inspection_output', text: '输出质检' },
  { value: 'inspection_performance', text: '性能与可用观测' },
]
const templateOptions = computed(() => [...new Set((items.data.value ?? []).map((it) => it.generator?.template_id).filter((id): id is string => !!id))].sort())
const versionOptions = computed(() => [...new Set((items.data.value ?? []).filter((it) => !queryParams.template || it.generator?.template_id === queryParams.template).map((it) => it.generator?.version).filter((version): version is string => !!version))].sort())
const newRef = reactive({ platform: 'anthropic', authType: 'oauth' })
const wizard = ref(false)
const resume = ref<Upstream | null>(null)
const vendors = ref<VendorView[]>([])
// 基线账号不受厂商开放状态限制，接入方式从本系统已实现的全部方式中选
const allVendors = ref<VendorConfig[]>([])
// 模型与厂商选项来自注册表；已存数据里不在表中的值由 SearchSelect 保留并标注
const priceModels = ref<string[]>([])
const templates = ref<GeneratorTemplate[]>([])
const platformOptions = computed<SelectOption[]>(() =>
  allVendors.value.map((v) => ({ value: v.platform, text: `${label('platform', v.platform).text}（${v.platform}）` })),
)
const modelOptions = computed<SelectOption[]>(() => priceModels.value.map((m) => ({ value: m, text: m })))
const refAuthTypes = computed(() => allVendors.value.find((v) => v.platform === newRef.platform)?.supported_auth_types ?? [])
watch(refAuthTypes, (ts) => {
  if (ts.length && !ts.includes(newRef.authType)) newRef.authType = ts[0]
})
const haystackHint = '{{HAYSTACK:5000}}'
const needleHint = '{{NEEDLE}}'

onMounted(async () => {
  items.load()
  refs.load()
  vendors.value = (await seller.vendors().catch(() => null)) ?? []
  allVendors.value = (await admin.vendors().catch(() => null)) ?? []
  checks.value = (await admin.inspectionChecks().catch(() => null)) ?? []
  // 选项接口失败时降级为空列表，SearchSelect 允许手动录入，不阻塞保存
  const st = await admin.settings().catch(() => null)
  priceModels.value = st?.price_models ?? []
  templates.value = (await admin.probeGeneratorTemplates().catch(() => null)) ?? []
})

const filtered = computed(() => {
  let rows = items.data.value ?? []
  if (queryParams.suite) rows = rows.filter((it) => it.suite === queryParams.suite)
  if (queryParams.platform) {
    rows = rows.filter((it) => !it.platforms.length || it.platforms.includes(queryParams.platform))
  }
  if (queryParams.template) rows = rows.filter((it) => it.generator?.template_id === queryParams.template)
  if (queryParams.generatorVersion) rows = rows.filter((it) => it.generator?.version === queryParams.generatorVersion)
  rows = filterByKeyword(rows, queryParams.keyword, ['id', 'suite', 'category', 'prompt', 'models', 'status', 'check_key', 'builtin_key'] as (keyof ProbeItem)[])
  return rows
})
const { pageNum, pageSize, total, pageRows, resetPage } = useClientPage(filtered)

function search() {
  handleQuery()
  resetPage()
  items.load()
}
function onReset() {
  resetQuery()
  resetPage()
  items.load()
}

function openNew() {
  editing.value = null
  Object.assign(form, initialForm())
  show.value = true
}
function openEdit(it: ProbeItem) {
  editing.value = it
  Object.assign(form, {
    suite: it.suite,
    category: it.category,
    prompt: it.prompt,
    grader: JSON.stringify(it.grader),
    platforms: [...it.platforms],
    models: [...it.models],
    maxTokens: it.max_tokens,
    effort: it.reasoning_effort ?? '',
    checkKey: it.check_key ?? '',
    request: JSON.stringify(it.request ?? {}, null, 2),
    generator: JSON.stringify(it.generator ?? {}, null, 2),
    modes: [...(it.modes ?? [])],
    weight: it.weight ?? 1,
  })
  show.value = true
}
async function save() {
  let grader: Grader, request: ProbeRequest, generator: ProbeGenerator
  try {
    const object = (raw: string, name: string): Record<string, unknown> => {
      let value: unknown
      try { value = JSON.parse(raw) } catch { throw new Error(`${name}需要是 JSON 对象`) }
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name}需要是 JSON 对象`)
      return value as Record<string, unknown>
    }
    const parsedGrader = object(form.grader, '判分规则')
    if (typeof parsedGrader.type !== 'string' || !parsedGrader.type) throw new Error('判分规则需要非空 type')
    grader = parsedGrader as Grader
    request = object(form.request, '请求模板') as ProbeRequest
    generator = object(form.generator, '生成器定义') as ProbeGenerator
    if (Object.keys(generator).length && (typeof generator.template_id !== 'string' || !generator.template_id || typeof generator.version !== 'string' || !generator.version)) {
      throw new Error('生成器定义需要非空 template_id 和字符串 version')
    }
    if (!Number.isFinite(form.weight) || form.weight < 0) throw new Error('题族权重需要是非负数')
  } catch (err) {
    toast.error(err)
    return
  }
  const body: Partial<ProbeItem> = {
    suite: form.suite,
    category: form.category,
    prompt: form.prompt,
    grader,
    platforms: [...form.platforms],
    models: [...form.models],
    max_tokens: Number(form.maxTokens),
    reasoning_effort: form.effort || undefined,
    status: editing.value?.status ?? 'active',
    check_key: form.checkKey.trim(),
    request,
    generator,
    modes: [...form.modes],
    builtin_key: editing.value?.builtin_key,
    weight: Number(form.weight),
    irt_difficulty: editing.value?.irt_difficulty,
    irt_discrimination: editing.value?.irt_discrimination,
  }
  const ok = editing.value ? await run(() => admin.updateItem(editing.value!.id, body), '已保存') : await run(() => admin.createItem(body), '已添加')
  if (ok) {
    show.value = false
    items.load()
  }
}
async function retire(it: ProbeItem) {
  if (await run(() => admin.retireItem(it.id), '已下线')) items.load()
}

async function createRef() {
  const res = await run(() => admin.createReference(newRef.platform, newRef.authType))
  if (res) {
    refs.load()
    resume.value = res.upstream
    wizard.value = true
  }
}
function authorize(u: Upstream) {
  resume.value = u
  wizard.value = true
}
async function removeRef(u: Upstream) {
  if (!confirm(`移除基线账号 #${u.id}？`)) return
  if (await run(() => admin.deleteReference(u.id), '正在移除')) refs.load()
}
</script>

<template>
  <ConsoleListPage
    id="console-probe-items-page"
    :title="pageTitle"
    :description="pageDescription"
  >
    <template #tabs>
      <TabBar
        v-model="tab"
        :tabs="[
          { key: 'items', text: '题目' },
          { key: 'refs', text: '基线账号' },
        ]"
      />
    </template>

    <template v-if="tab === 'items'" #actions>
      <button type="button" class="btn btn-primary" @click="openNew">添加题目</button>
    </template>

    <template v-if="tab === 'items'" #filters>
      <div class="filter-item">
        <label class="filter-label">关键字</label>
        <div class="relative w-full flex-1">
          <input v-model="queryParams.keyword" class="filter-input w-full" placeholder="ID / 质检项 / 类别 / 题目 / 模型" @keyup.enter="search" />
        </div>
      </div>
      <div class="filter-item">
        <label class="filter-label">状态</label>
        <select v-model="queryParams.status" class="filter-input flex-1">
          <option value="active">使用中</option>
          <option value="draft">草稿</option>
          <option value="retired">已下线</option>
          <option value="">全部</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">套件</label>
        <select v-model="queryParams.suite" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="s in suiteOptions" :key="s.value" :value="s.value">{{ s.text }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label" for="item-template">模板</label>
        <select id="item-template" v-model="queryParams.template" class="filter-input flex-1" @change="queryParams.generatorVersion = ''">
          <option value="">全部</option>
          <option v-for="id in templateOptions" :key="id" :value="id">{{ id }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label" for="item-generator-version">模板版本</label>
        <select id="item-generator-version" v-model="queryParams.generatorVersion" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="version in versionOptions" :key="version" :value="version">{{ version }}</option>
        </select>
      </div>
      <div class="filter-item">
        <label class="filter-label">平台</label>
        <select v-model="queryParams.platform" class="filter-input flex-1">
          <option value="">全部</option>
          <option v-for="o in options('platform')" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div class="filter-actions lg:col-span-4">
        <button type="button" class="btn btn-secondary" @click="onReset">重置</button>
        <button type="button" class="btn btn-primary" @click="search">查询</button>
      </div>
    </template>

    <DataTable
      v-if="tab === 'items'"
      :columns="[
        { key: 'id', label: 'ID' },
        { key: 'suite', label: '套件' },
        { key: 'check_key', label: '质检项' },
        { key: 'template', label: '模板 / 版本' },
        { key: 'modes', label: '模式' },
        { key: 'category', label: '类别' },
        { key: 'prompt', label: '题目' },
        { key: 'grader', label: '判分' },
        { key: 'platforms', label: '平台' },
        { key: 'models', label: '模型' },
        { key: 'status', label: '状态' },
        { key: 'actions', label: '操作' },
      ]"
      :rows="pageRows"
      :loading="items.loading.value"
      row-key="id"
      empty="没有符合条件的题目"
    >
      <template #cell-suite="{ row }">{{ suiteOptions.find((s) => s.value === row.suite)?.text ?? row.suite }}</template>
      <template #cell-check_key="{ row }">{{ row.check_key || '旧题' }}</template>
      <template #cell-template="{ row }">{{ row.generator?.template_id ? `${row.generator.template_id} / ${row.generator.version}` : '静态题' }}</template>
      <template #cell-modes="{ row }">{{ row.modes?.length ? row.modes.map((mode: string) => mode === 'full' ? '完整质检' : '快检').join(' / ') : '旧模式' }}</template>
      <template #cell-prompt="{ row }"><span class="block max-w-md truncate" :title="row.prompt">{{ row.prompt }}</span></template>
      <template #cell-grader="{ row }">{{ row.grader.type }}</template>
      <template #cell-platforms="{ row }">{{ row.platforms.length ? row.platforms.join(', ') : '全部' }}</template>
      <template #cell-models="{ row }">{{ row.models.length ? row.models.join(', ') : '全部' }}</template>
      <template #cell-status="{ row }">{{ ({ active: '使用中', draft: '草稿', retired: '已下线' } as Record<string, string>)[row.status] }}</template>
      <template #cell-actions="{ row }">
        <button class="btn-ghost btn-sm" @click="openEdit(row)">编辑</button>
        <button v-if="row.status !== 'retired'" class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="retire(row)">下线</button>
      </template>
    </DataTable>

    <template v-else>
      <div class="console-toolbar">
        <select v-model="newRef.platform" class="input w-40" aria-label="厂商">
          <option v-for="p in options('platform').filter((o) => o.value !== 'composite')" :key="p.value" :value="p.value">{{ p.text }}</option>
        </select>
        <select v-model="newRef.authType" class="input w-48" aria-label="接入方式">
          <option v-for="t in refAuthTypes" :key="t" :value="t">{{ authTypeText(newRef.platform, t) }}</option>
        </select>
        <button class="btn btn-secondary" :disabled="busy" @click="createRef">新增并授权</button>
      </div>
      <DataTable
        :columns="[
          { key: 'id', label: '上游' },
          { key: 'platform', label: '厂商' },
          { key: 'auth_type', label: '接入方式' },
          { key: 'status', label: '状态' },
          { key: 'health', label: '健康' },
          { key: 'actions', label: '操作' },
        ]"
        :rows="refs.data.value"
        row-key="id"
        empty="还没有基线账号。没有基线时按绝对标准判定，不给出等级。"
      >
        <template #cell-id="{ row }">#{{ row.id }}</template>
        <template #cell-platform="{ row }">{{ label('platform', row.platform).text }}</template>
        <template #cell-auth_type="{ row }">{{ authTypeText(row.platform, row.auth_type) }}</template>
        <template #cell-status="{ row }"><StatusBadge kind="upstreamStatus" :value="row.status" /></template>
        <template #cell-health="{ row }"><StatusBadge kind="health" :value="row.health" /></template>
        <template #cell-actions="{ row }">
          <button v-if="['draft', 'authorizing', 'failed'].includes(row.status)" class="btn-ghost btn-sm" @click="authorize(row)">授权</button>
          <button class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="removeRef(row)">移除</button>
        </template>
      </DataTable>
    </template>

    <template v-if="tab === 'items'" #footer>
      <span class="mr-auto text-xs text-gray-500">共 {{ total }} 条题目</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum <= 1" @click="pageNum--">上一页</button>
      <span class="text-xs text-gray-500">{{ pageNum }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="pageNum * pageSize >= total" @click="pageNum++">下一页</button>
    </template>
  </ConsoleListPage>

  <ModalDialog v-model:open="show" :title="editing ? `编辑题目 #${editing.id}` : '添加题目'" wide>
    <div class="grid grid-cols-3 gap-3">
      <div>
        <label class="label" for="isuite">套件</label>
        <select id="isuite" v-model="form.suite" class="input">
          <option v-for="s in suiteOptions" :key="s.value" :value="s.value">{{ s.text }}</option>
        </select>
      </div>
      <div>
        <label class="label" for="icat">类别</label>
        <input id="icat" v-model="form.category" class="input" placeholder="math / logic / format …" />
      </div>
      <div>
        <label class="label" for="imax">最大输出 token</label>
        <input id="imax" v-model.number="form.maxTokens" class="input" type="number" min="1" />
      </div>
    </div>
    <div class="grid grid-cols-3 gap-3">
      <div>
        <label class="label" for="icheck">质检项键</label>
        <select id="icheck" v-model="form.checkKey" class="input">
          <option value="">不关联（旧题）</option>
          <option v-for="o in checkOptions" :key="o.value" :value="o.value">{{ o.text }}</option>
        </select>
      </div>
      <div>
        <label class="label" for="iweight">题族权重</label>
        <input id="iweight" v-model.number="form.weight" class="input" type="number" min="0" step="0.1" />
      </div>
      <div>
        <span class="label">检测模式</span>
        <label class="mr-3 inline-flex items-center gap-1"><input v-model="form.modes" type="checkbox" value="full" />完整质检</label>
        <label class="inline-flex items-center gap-1"><input v-model="form.modes" type="checkbox" value="quick" />快检</label>
      </div>
    </div>
    <p v-if="editing?.builtin_key" class="hint">内置题标识：{{ editing.builtin_key }}；题目版本：{{ editing.version }}。编辑后启动同步会保留修改。</p>
    <div>
      <label class="label" for="iprompt">题目</label>
      <textarea id="iprompt" v-model="form.prompt" class="input" rows="5" />
      <p class="hint">长上下文题可以用 <code v-text="haystackHint" /> 插入干扰文本，答案写 <code v-text="needleHint" />。</p>
    </div>
    <div>
      <label class="label" for="igrader">判分规则（JSON）</label>
      <textarea id="igrader" v-model="form.grader" class="input font-mono" rows="3" />
      <p class="hint">旧规则：exact / contains / regex / numeric / choice / json_keys；新规则：answer_tag / fields_exact / set_exact / lines_alias / constraints / json_schema / tool_calls / abstain / full_output / num_list / accept_reject。</p>
    </div>
    <div>
      <label class="label" for="irequest">请求模板（JSON）</label>
      <textarea id="irequest" v-model="form.request" class="input font-mono" rows="5" />
      <p class="hint">支持 system、messages、tools、attachments、effort 和 steps；空对象沿用题面与推理强度。本地虚拟工具只使用确定性结果。</p>
    </div>
    <div>
      <label class="label" for="igenerator">生成器定义（JSON）</label>
      <textarea id="igenerator" v-model="form.generator" class="input font-mono" rows="5" />
      <p class="hint">静态题填 {}；生成题包含 template_id、version、parameters 和 instances，例如 {"template_id":"register_machine","version":"1","parameters":{"operations":[30,60]},"instances":{"full":8,"quick":4}}。模板版本不可省略；参数域不接受代码。</p>
      <p v-if="templates.length" class="hint">
        已注册模板：<code v-for="t in templates" :key="t.id" class="mr-2">{{ t.id }}（版本 {{ t.versions.join(' / ') }}）</code>
      </p>
    </div>
    <div class="grid grid-cols-3 gap-3">
      <div>
        <label class="label" for="iplat">适用平台（留空为全部）</label>
        <SearchSelect id="iplat" v-model="form.platforms" :options="platformOptions" multiple placeholder="选择厂商" />
      </div>
      <div>
        <label class="label" for="imodels">适用模型（留空为全部）</label>
        <SearchSelect id="imodels" v-model="form.models" :options="modelOptions" multiple placeholder="选择模型" />
      </div>
      <div>
        <label class="label" for="ieff">推理强度（可选）</label>
        <input id="ieff" v-model="form.effort" class="input" placeholder="low / medium / high" />
      </div>
    </div>
    <template #footer>
      <button class="btn-secondary" @click="show = false">取消</button>
      <button class="btn-primary" :disabled="busy" @click="save">保存</button>
    </template>
  </ModalDialog>

  <OnboardingWizard v-model:open="wizard" :vendors="vendors" :resume="resume" @done="refs.load()" />
</template>
