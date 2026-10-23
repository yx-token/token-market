<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

const router = useRouter()

interface ProbeItem {
  id: number
  suite: string
  category: string
  prompt: string
  grader: Record<string, unknown>
  platforms: string[]
  models: string[]
  max_tokens: number
  reasoning_effort?: string
  status: string
  check_key?: string
  request?: Record<string, unknown>
  generator?: Record<string, unknown>
  modes?: string[]
  weight?: number
  builtin_key?: string
}

interface RefUpstream {
  id: number
  platform: string
  auth_type?: string
  account_email?: string
  status: string
  is_reference?: boolean
}

interface Vendor {
  platform: string
  supported_auth_types: string[]
}

const SUITES = [
  { value: 'inspection_capability', text: '能力检测' },
  { value: 'inspection_identity', text: '身份核验' },
  { value: 'inspection_quality', text: '质量抽检' },
  { value: 'iq', text: '旧题库 (iq)' },
]

const PLATFORM_LABEL: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  kimi: 'Kimi',
  zhipu: '智谱',
  minimax: 'MiniMax',
  opencode_go: 'OpenCode Go',
}

const tab = ref<'items' | 'refs'>('items')
const rows = ref<ProbeItem[]>([])
const refs = ref<RefUpstream[]>([])
const vendors = ref<Vendor[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterStatus = ref('active')
const filterSuite = ref('')
const pageNum = ref(1)
const pageSize = 15
const newRef = reactive({ platform: 'anthropic', authType: 'oauth' })
const showCreateRef = ref(false)

const refAuthTypes = computed(
  () => vendors.value.find((v) => v.platform === newRef.platform)?.supported_auth_types ?? ['oauth', 'api_key'],
)

const show = ref(false)
const editing = ref<ProbeItem | null>(null)
const form = reactive({
  suite: 'inspection_capability',
  category: '',
  prompt: '',
  grader: '{"type":"exact","answer":""}',
  platforms: '',
  models: '',
  max_tokens: 1024,
  check_key: '',
  modes: 'full,quick',
  weight: 1,
  request: '{}',
  generator: '{}',
})

const filtered = computed(() => {
  let list = rows.value
  if (filterStatus.value) list = list.filter((it) => it.status === filterStatus.value)
  if (filterSuite.value) list = list.filter((it) => it.suite === filterSuite.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    list = list.filter((it) =>
      [it.id, it.suite, it.category, it.prompt, it.check_key, it.status, it.builtin_key]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return list
})

const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageRows = computed(() => {
  const start = (pageNum.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function suiteText(v: string) {
  return SUITES.find((s) => s.value === v)?.text || v
}

function splitCSV(s: string) {
  return s
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'items') {
      const q = filterStatus.value ? `?status=${encodeURIComponent(filterStatus.value)}` : ''
      rows.value = (await marketGet<ProbeItem[] | null>(`${MARKET_API_PREFIX}/probe-items${q}`)) ?? []
    } else {
      const [rs, vs] = await Promise.all([
        marketGet<RefUpstream[] | null>(`${MARKET_API_PREFIX}/reference-accounts`),
        marketGet<Vendor[] | null>(`${MARKET_API_PREFIX}/vendors`).catch(() => []),
      ])
      refs.value = rs ?? []
      vendors.value = vs ?? []
      if (refAuthTypes.value.length && !refAuthTypes.value.includes(newRef.authType)) {
        newRef.authType = refAuthTypes.value[0]
      }
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
    refs.value = []
  } finally {
    loading.value = false
  }
}

async function run<T = unknown>(label: string, fn: () => Promise<T>): Promise<T | false> {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const result = await fn()
    msg.value = label
    await load()
    return result
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openNew() {
  editing.value = null
  Object.assign(form, {
    suite: 'inspection_capability',
    category: '',
    prompt: '',
    grader: '{"type":"exact","answer":""}',
    platforms: '',
    models: '',
    max_tokens: 1024,
    check_key: '',
    modes: 'full,quick',
    weight: 1,
    request: '{}',
    generator: '{}',
  })
  show.value = true
}

function openEdit(it: ProbeItem) {
  editing.value = it
  Object.assign(form, {
    suite: it.suite,
    category: it.category,
    prompt: it.prompt,
    grader: JSON.stringify(it.grader ?? {}, null, 2),
    platforms: (it.platforms || []).join(', '),
    models: (it.models || []).join(', '),
    max_tokens: it.max_tokens || 1024,
    check_key: it.check_key || '',
    modes: (it.modes || []).join(','),
    weight: it.weight ?? 1,
    request: JSON.stringify(it.request ?? {}, null, 2),
    generator: JSON.stringify(it.generator ?? {}, null, 2),
  })
  show.value = true
}

function closeModal() {
  show.value = false
}

function parseObject(raw: string, name: string): Record<string, unknown> {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    throw new Error(`${name}需要是 JSON 对象`)
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name}需要是 JSON 对象`)
  return value as Record<string, unknown>
}

async function save() {
  try {
    const grader = parseObject(form.grader, '判分规则')
    if (typeof grader.type !== 'string' || !grader.type) throw new Error('判分规则需要非空 type')
    const request = parseObject(form.request, '请求模板')
    const generator = parseObject(form.generator, '生成器定义')
    if (!form.prompt.trim()) throw new Error('请填写题目内容')
    if (!Number.isFinite(form.weight) || form.weight < 0) throw new Error('权重需要是非负数')

    const body = {
      suite: form.suite,
      category: form.category.trim(),
      prompt: form.prompt.trim(),
      grader,
      platforms: splitCSV(form.platforms),
      models: splitCSV(form.models),
      max_tokens: Number(form.max_tokens) || 1024,
      check_key: form.check_key.trim(),
      request,
      generator,
      modes: splitCSV(form.modes),
      weight: Number(form.weight) || 0,
      status: editing.value?.status ?? 'active',
    }

    const ok = editing.value
      ? await run(`题目 #${editing.value.id} 已保存`, () =>
          marketSend('PUT', `${MARKET_API_PREFIX}/probe-items/${editing.value!.id}`, body),
        )
      : await run('题目已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/probe-items`, body))
    if (ok) closeModal()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  }
}

function retire(it: ProbeItem) {
  if (it.status === 'retired') return
  if (!window.confirm(`下线题目 #${it.id}？`)) return
  return run(`题目 #${it.id} 已下线`, () =>
    marketSend('POST', `${MARKET_API_PREFIX}/probe-items/${it.id}/retire`),
  )
}

function resetFilters() {
  keyword.value = ''
  filterStatus.value = 'active'
  filterSuite.value = ''
  pageNum.value = 1
  load()
}

async function createRef() {
  const res = await run('基线账号草稿已创建', () =>
    marketSend<{ upstream?: RefUpstream }>('POST', `${MARKET_API_PREFIX}/reference-accounts`, {
      platform: newRef.platform,
      auth_type: newRef.authType,
    }),
  )
  if (res) {
    showCreateRef.value = false
    const id = res.upstream?.id
    if (id) {
      msg.value = `已创建草稿 #${id}，正在打开上游页…`
      await router.push({ path: '/admin/market/upstreams', query: { q: String(id), focus: String(id) } })
    } else {
      msg.value = '基线账号草稿已创建，请到上游页完成授权'
    }
  }
}

function goAuthorize(u: RefUpstream) {
  return router.push({ path: '/admin/market/upstreams', query: { q: String(u.id), focus: String(u.id) } })
}

async function removeRef(u: RefUpstream) {
  if (!window.confirm(`移除基线账号 #${u.id}？`)) return
  await run('正在移除', () => marketSend('DELETE', `${MARKET_API_PREFIX}/reference-accounts/${u.id}`))
}

function switchTab(next: 'items' | 'refs') {
  tab.value = next
  load()
}

onMounted(load)
</script>

<template>
  <section id="market-probe-items-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">{{ tab === 'items' ? '题库' : '基线账号' }}</h2>
        <p class="y-page-desc">
          {{
            tab === 'items'
              ? '测智与身份核验题库；可创建、编辑与下线'
              : '平台自备干净官方账号，与被测上游同批次答题作对照'
          }}
        </p>
      </div>
      <div class="y-header-actions">
        <button v-if="tab === 'items'" type="button" class="y-btn" :disabled="busy" @click="openNew">新建题目</button>
        <button v-else type="button" class="y-btn" :disabled="busy" @click="showCreateRef = true">添加基线</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'items' }" @click="switchTab('items')">题目</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'refs' }" @click="switchTab('refs')">基线账号</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <template v-if="tab === 'items'">
      <div class="y-search">
        <input v-model="keyword" type="search" placeholder="ID / 套件 / 题目 / check_key" @keyup.enter="pageNum = 1" />
        <select v-model="filterStatus" class="input" @change="pageNum = 1; load()">
          <option value="">状态：全部</option>
          <option value="active">启用</option>
          <option value="retired">已下线</option>
        </select>
        <select v-model="filterSuite" class="input" @change="pageNum = 1">
          <option value="">套件：全部</option>
          <option v-for="s in SUITES" :key="s.value" :value="s.value">{{ s.text }}</option>
        </select>
        <button type="button" class="y-btn y-btn--ghost" @click="resetFilters">重置</button>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!pageRows.length" class="y-msg">没有符合条件的题目</p>
      <table v-else>
        <thead>
          <tr>
            <th>ID</th>
            <th>套件</th>
            <th>分类</th>
            <th>题目</th>
            <th>check_key</th>
            <th>权重</th>
            <th>状态</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in pageRows" :key="row.id">
            <td>{{ row.id }}</td>
            <td>{{ suiteText(row.suite) }}</td>
            <td>{{ row.category || '—' }}</td>
            <td>
              <div class="prompt-cell" :title="row.prompt">{{ row.prompt }}</div>
            </td>
            <td class="mono">{{ row.check_key || '—' }}</td>
            <td>{{ row.weight ?? 1 }}</td>
            <td>
              <span :class="row.status === 'active' ? 'y-chip y-chip--ok' : 'y-chip'">{{ row.status }}</span>
            </td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="openEdit(row)">编辑</button>
              <button
                v-if="row.status !== 'retired'"
                type="button"
                class="y-link y-link--danger"
                :disabled="busy"
                @click="retire(row)"
              >
                下线
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <footer v-if="total > 0" class="y-footer">
        <div class="y-pager">
          <span>共 {{ total }} 条</span>
          <button type="button" :disabled="pageNum <= 1" @click="pageNum -= 1">上一页</button>
          <button type="button" class="is-on">{{ pageNum }} / {{ pageCount }}</button>
          <button type="button" :disabled="pageNum >= pageCount" @click="pageNum += 1">下一页</button>
        </div>
      </footer>
      </template>

      <template v-else>
        <p v-if="loading" class="y-msg">加载中…</p>
        <p v-else-if="!refs.length" class="y-msg">还没有基线账号</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>厂商</th>
              <th>接入</th>
              <th>邮箱</th>
              <th>状态</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in refs" :key="u.id">
              <td>#{{ u.id }}</td>
              <td>{{ PLATFORM_LABEL[u.platform] || u.platform }}</td>
              <td>{{ u.auth_type || '—' }}</td>
              <td>{{ u.account_email || '—' }}</td>
              <td><span class="y-chip">{{ u.status }}</span></td>
              <td class="y-col-op">
                <button
                  v-if="u.status === 'draft' || u.status === 'authorizing'"
                  type="button"
                  class="y-link"
                  @click="goAuthorize(u)"
                >
                  去授权
                </button>
                <button type="button" class="y-link" @click="goAuthorize(u)">打开上游</button>
                <button type="button" class="y-link y-link--danger" :disabled="busy" @click="removeRef(u)">移除</button>
              </td>
            </tr>
          </tbody>
        </table>
        <footer v-if="!loading" class="y-footer">共 {{ refs.length }} 条</footer>
      </template>
    </div>

    <div v-if="showCreateRef" class="y-modal-mask" @click.self="showCreateRef = false">
      <div class="y-modal" role="dialog" aria-label="添加基线账号">
        <header class="y-modal__head">
          <h3>添加基线账号</h3>
          <button type="button" class="y-link" @click="showCreateRef = false">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>厂商</span>
            <select v-model="newRef.platform" class="input w-full" @change="newRef.authType = refAuthTypes[0] || 'oauth'">
              <option v-for="(label, key) in PLATFORM_LABEL" :key="key" :value="key">{{ label }}</option>
            </select>
          </label>
          <label class="y-field">
            <span>接入方式</span>
            <select v-model="newRef.authType" class="input w-full">
              <option v-for="t in refAuthTypes" :key="t" :value="t">{{ t }}</option>
            </select>
          </label>
          <p class="hint">创建后会跳转到上游页，请在卖家端或上游详情完成 OAuth / 密钥导入。</p>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="showCreateRef = false">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="createRef">创建</button>
        </footer>
      </div>
    </div>

    <div v-if="show" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="editing ? '编辑题目' : '新建题目'">
        <header class="y-modal__head">
          <h3>{{ editing ? `编辑题目 #${editing.id}` : '新建题目' }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <div class="y-field-row">
            <label class="y-field">
              <span>套件</span>
              <select v-model="form.suite" class="input w-full">
                <option v-for="s in SUITES" :key="s.value" :value="s.value">{{ s.text }}</option>
              </select>
            </label>
            <label class="y-field">
              <span>分类</span>
              <input v-model="form.category" class="input w-full" />
            </label>
          </div>
          <label class="y-field">
            <span>题目内容</span>
            <textarea v-model="form.prompt" class="input w-full y-textarea" rows="4" />
          </label>
          <label class="y-field">
            <span>判分规则 (JSON)</span>
            <textarea v-model="form.grader" class="input w-full y-textarea mono" rows="3" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>平台（逗号分隔，空=全部）</span>
              <input v-model="form.platforms" class="input w-full" placeholder="anthropic, openai" />
            </label>
            <label class="y-field">
              <span>模型（逗号分隔）</span>
              <input v-model="form.models" class="input w-full" />
            </label>
          </div>
          <div class="y-field-row y-field-row--3">
            <label class="y-field">
              <span>max_tokens</span>
              <input v-model.number="form.max_tokens" type="number" class="input w-full" />
            </label>
            <label class="y-field">
              <span>check_key</span>
              <input v-model="form.check_key" class="input w-full" />
            </label>
            <label class="y-field">
              <span>权重</span>
              <input v-model.number="form.weight" type="number" class="input w-full" min="0" step="0.1" />
            </label>
          </div>
          <label class="y-field">
            <span>模式（逗号分隔：full / quick）</span>
            <input v-model="form.modes" class="input w-full" />
          </label>
          <label class="y-field">
            <span>请求模板 (JSON)</span>
            <textarea v-model="form.request" class="input w-full y-textarea mono" rows="2" />
          </label>
          <label class="y-field">
            <span>生成器定义 (JSON)</span>
            <textarea v-model="form.generator" class="input w-full y-textarea mono" rows="2" />
          </label>
        </div>
        <footer class="y-modal__foot">
          <button type="button" class="y-btn y-btn--ghost" @click="closeModal">取消</button>
          <button type="button" class="y-btn" :disabled="busy" @click="save">保存</button>
        </footer>
      </div>
    </div>
  </section>
</template>

<style scoped>
.y-header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.prompt-cell {
  max-width: 22rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.y-link--danger {
  color: var(--color-error, #ff4d4f);
}
.y-modal-mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(15 23 42 / 45%);
}
.y-modal {
  width: min(640px, 100%);
  max-height: min(92vh, 860px);
  overflow: auto;
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head,
.y-modal__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__foot {
  justify-content: flex-end;
}
.y-modal__body {
  display: grid;
  gap: 12px;
  padding: 0 16px 16px;
}
.y-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.y-field span {
  color: var(--color-text-secondary, #868686);
}
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.y-field-row--3 {
  grid-template-columns: 1fr 1fr 1fr;
}
.y-textarea {
  height: auto !important;
  min-height: 72px;
  padding: 8px 10px;
  resize: vertical;
}
.hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
@media (max-width: 720px) {
  .y-field-row,
  .y-field-row--3 {
    grid-template-columns: 1fr;
  }
}
</style>
