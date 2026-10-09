<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { admin, seller } from '@/api'
import type { ProbeItem, Upstream, VendorConfig, VendorView } from '@/api/types'
import { useAction, useLoader } from '@/composables/useAsync'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import OnboardingWizard from '@/components/OnboardingWizard.vue'
import { authTypeText, label, options } from '@/utils/labels'

const toast = useToast()
const status = ref('active')
const items = useLoader(() => admin.items(status.value), [] as ProbeItem[] | null)
const refs = useLoader(() => admin.references(), [] as Upstream[] | null)
const { busy, run } = useAction()
const editing = ref<ProbeItem | null>(null)
const show = ref(false)
const form = reactive({ suite: 'iq', category: '', prompt: '', grader: '{"type": "exact", "answer": ""}', platforms: '', maxTokens: 1024, effort: '' })
const newRef = reactive({ platform: 'anthropic', authType: 'oauth' })
const wizard = ref(false)
const resume = ref<Upstream | null>(null)
const vendors = ref<VendorView[]>([])
// 基线账号不受厂商开放状态限制，接入方式从本系统已实现的全部方式中选
const allVendors = ref<VendorConfig[]>([])
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
})

function openNew() {
  editing.value = null
  Object.assign(form, { suite: 'iq', category: '', prompt: '', grader: '{"type": "exact", "answer": ""}', platforms: '', maxTokens: 1024, effort: '' })
  show.value = true
}
function openEdit(it: ProbeItem) {
  editing.value = it
  Object.assign(form, {
    suite: it.suite,
    category: it.category,
    prompt: it.prompt,
    grader: JSON.stringify(it.grader),
    platforms: it.platforms.join(', '),
    maxTokens: it.max_tokens,
    effort: it.reasoning_effort ?? '',
  })
  show.value = true
}
async function save() {
  let grader
  try {
    grader = JSON.parse(form.grader)
  } catch {
    toast.error('判分规则需要是 JSON')
    return
  }
  const body: Partial<ProbeItem> = {
    suite: form.suite,
    category: form.category,
    prompt: form.prompt,
    grader,
    platforms: form.platforms.split(/[,\s]+/).filter(Boolean),
    max_tokens: Number(form.maxTokens),
    reasoning_effort: form.effort || undefined,
    status: editing.value?.status ?? 'active',
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
  <div>
    <PageHeader title="题库与基线账号" description="题库私有、定期轮换；基线账号是平台自备的干净官方账号，与被测上游同批次答题作对照。">
      <button class="btn-primary" @click="openNew">添加题目</button>
    </PageHeader>

    <div class="card mb-5">
      <div class="mb-3 flex items-center gap-3">
        <h2 class="card-title mb-0 mr-auto">题目</h2>
        <select v-model="status" class="input w-32" aria-label="状态" @change="items.load()">
          <option value="active">使用中</option>
          <option value="draft">草稿</option>
          <option value="retired">已下线</option>
          <option value="">全部</option>
        </select>
      </div>
      <DataTable
        :columns="[
          { key: 'id', label: 'ID' },
          { key: 'suite', label: '套件' },
          { key: 'category', label: '类别' },
          { key: 'prompt', label: '题目' },
          { key: 'grader', label: '判分' },
          { key: 'platforms', label: '平台' },
          { key: 'status', label: '状态' },
          { key: 'actions', label: '' },
        ]"
        :rows="items.data.value"
        :loading="items.loading.value"
        row-key="id"
      >
        <template #cell-prompt="{ row }"><span class="block max-w-md truncate" :title="row.prompt">{{ row.prompt }}</span></template>
        <template #cell-grader="{ row }">{{ row.grader.type }}</template>
        <template #cell-platforms="{ row }">{{ row.platforms.length ? row.platforms.join(', ') : '全部' }}</template>
        <template #cell-status="{ row }">{{ ({ active: '使用中', draft: '草稿', retired: '已下线' } as Record<string, string>)[row.status] }}</template>
        <template #cell-actions="{ row }">
          <button class="btn-ghost btn-sm" @click="openEdit(row)">编辑</button>
          <button v-if="row.status !== 'retired'" class="btn-ghost btn-sm text-red-600" :disabled="busy" @click="retire(row)">下线</button>
        </template>
      </DataTable>
    </div>

    <div class="card">
      <div class="mb-3 flex flex-wrap items-end gap-2">
        <h2 class="card-title mb-0 mr-auto">基线账号</h2>
        <select v-model="newRef.platform" class="input w-40" aria-label="厂商">
          <option v-for="p in options('platform').filter((o) => o.value !== 'composite')" :key="p.value" :value="p.value">{{ p.text }}</option>
        </select>
        <select v-model="newRef.authType" class="input w-48" aria-label="接入方式">
          <option v-for="t in refAuthTypes" :key="t" :value="t">{{ authTypeText(newRef.platform, t) }}</option>
        </select>
        <button class="btn-secondary" :disabled="busy" @click="createRef">新增并授权</button>
      </div>
      <DataTable
        :columns="[
          { key: 'id', label: '上游' },
          { key: 'platform', label: '厂商' },
          { key: 'auth_type', label: '接入方式' },
          { key: 'status', label: '状态' },
          { key: 'health', label: '健康' },
          { key: 'actions', label: '' },
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
    </div>

    <ModalDialog v-model:open="show" :title="editing ? `编辑题目 #${editing.id}` : '添加题目'" wide>
      <div class="grid grid-cols-3 gap-3">
        <div>
          <label class="label" for="isuite">套件</label>
          <select id="isuite" v-model="form.suite" class="input">
            <option v-for="s in ['iq', 'reasoning', 'long_context', 'tool_call']" :key="s" :value="s">{{ s }}</option>
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
      <div>
        <label class="label" for="iprompt">题目</label>
        <textarea id="iprompt" v-model="form.prompt" class="input" rows="5" />
        <p class="hint">长上下文题可以用 <code v-text="haystackHint" /> 插入干扰文本，答案写 <code v-text="needleHint" />。</p>
      </div>
      <div>
        <label class="label" for="igrader">判分规则（JSON）</label>
        <input id="igrader" v-model="form.grader" class="input font-mono" />
        <p class="hint">type：exact / contains / regex / numeric / choice / json_keys，例如 {"type":"numeric","number":391}</p>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="label" for="iplat">适用平台（留空为全部）</label>
          <input id="iplat" v-model="form.platforms" class="input" placeholder="anthropic, openai" />
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
  </div>
</template>
