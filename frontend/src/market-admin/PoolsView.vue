<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface Pool {
  id: number
  name: string
  platform: string
  sub2api_group_id?: number | null
  sell_multiplier: number
  min_margin: number
  safety_buffer: number
  enabled: boolean
  market_published: boolean
  market_title?: string
  market_description?: string
}

const PLATFORM_OPTIONS = [
  { value: 'anthropic', text: 'Anthropic' },
  { value: 'openai', text: 'OpenAI' },
  { value: 'gemini', text: 'Gemini' },
  { value: 'antigravity', text: 'Antigravity' },
  { value: 'grok', text: 'Grok' },
  { value: 'kimi', text: 'Kimi' },
  { value: 'zhipu', text: '智谱' },
  { value: 'minimax', text: 'MiniMax' },
  { value: 'opencode_go', text: 'OpenCode Go' },
  { value: 'composite', text: '综合池' },
]

const pools = ref<Pool[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterPlatform = ref('')
const filterEnabled = ref('')
const filterMarket = ref('')
const pageNum = ref(1)
const pageSize = 15

const show = ref(false)
const editing = ref<Pool | null>(null)
const form = reactive({
  name: 'tm-pool-',
  platform: 'anthropic',
  sell: '',
  margin: '0.10',
  buffer: '0.02',
  enabled: true,
  market_published: false,
  market_title: '',
  market_description: '',
})

const filtered = computed(() => {
  let rows = pools.value
  if (filterPlatform.value) rows = rows.filter((p) => p.platform === filterPlatform.value)
  if (filterEnabled.value === 'true') rows = rows.filter((p) => p.enabled)
  if (filterEnabled.value === 'false') rows = rows.filter((p) => !p.enabled)
  if (filterMarket.value === 'true') rows = rows.filter((p) => p.market_published)
  if (filterMarket.value === 'false') rows = rows.filter((p) => !p.market_published)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((p) =>
      [p.id, p.name, p.platform, p.sub2api_group_id, p.market_title]
        .map((v) => String(v ?? '').toLowerCase())
        .some((s) => s.includes(kw)),
    )
  }
  return rows
})

const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageRows = computed(() => {
  const start = (pageNum.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function platformLabel(v: string) {
  return PLATFORM_OPTIONS.find((o) => o.value === v)?.text || v
}

function defaultTitle(p: Pick<Pool, 'name' | 'platform'>) {
  const base = p.name.replace(/^tm-pool-/, '') || p.name
  return p.platform === 'composite' ? `${base} · 平台保障池` : `${base} · 平台保障`
}

function cap(p: Pool) {
  return p.sell_multiplier * (1 - p.min_margin - p.safety_buffer)
}

function fmtMul(n: number) {
  return Number.isFinite(n) ? n.toFixed(3) : '—'
}

function fmtPct(n: number) {
  return Number.isFinite(n) ? `${(n * 100).toFixed(0)}%` : '—'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    pools.value = (await marketGet<Pool[] | null>(`${MARKET_API_PREFIX}/pools`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    pools.value = []
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  Object.assign(form, {
    name: 'tm-pool-',
    platform: 'anthropic',
    sell: '',
    margin: '0.10',
    buffer: '0.02',
    enabled: true,
    market_published: false,
    market_title: '',
    market_description: '',
  })
  show.value = true
}

function openEdit(p: Pool) {
  editing.value = p
  Object.assign(form, {
    name: p.name,
    platform: p.platform,
    sell: String(p.sell_multiplier),
    margin: String(p.min_margin),
    buffer: String(p.safety_buffer),
    enabled: p.enabled,
    market_published: p.market_published,
    market_title: p.market_title || defaultTitle(p),
    market_description: p.market_description || '',
  })
  show.value = true
}

function closeModal() {
  show.value = false
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

async function save() {
  const body = {
    name: form.name,
    platform: form.platform,
    sell_multiplier: Number(form.sell),
    min_margin: Number(form.margin),
    safety_buffer: Number(form.buffer),
    enabled: form.enabled,
    market_published: form.market_published,
    market_title: form.market_title.trim(),
    market_description: form.market_description.trim(),
  }
  if (!Number.isFinite(body.sell_multiplier) || body.sell_multiplier <= 0) {
    error.value = '请填写有效的售价 D'
    return
  }
  const ok = editing.value
    ? await run('已保存', () => marketSend('PUT', `${MARKET_API_PREFIX}/pools/${editing.value!.id}`, body))
    : await run('已创建', () => marketSend('POST', `${MARKET_API_PREFIX}/pools`, body))
  if (ok) {
    closeModal()
    await load()
  }
}

async function sync(p: Pool) {
  if (await run(`分组 ${p.name} 已同步`, () => marketSend('POST', `${MARKET_API_PREFIX}/pools/${p.id}/sync`))) {
    await load()
  }
}

async function toggleMarket(p: Pool) {
  const next = !p.market_published
  if (
    await run(next ? '已上架市场' : '已下架', () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/pools/${p.id}`, {
        market_published: next,
        market_title: p.market_title || defaultTitle(p),
        market_description: p.market_description || '',
      }),
    )
  ) {
    await load()
  }
}

async function toggleEnabled(p: Pool) {
  if (
    await run(`分组 ${p.name} 已${p.enabled ? '关闭' : '开启'}`, () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/pools/${p.id}`, {
        name: p.name,
        platform: p.platform,
        enabled: !p.enabled,
        sell_multiplier: p.sell_multiplier,
        min_margin: p.min_margin,
        safety_buffer: p.safety_buffer,
        market_published: p.market_published,
        market_title: p.market_title || '',
        market_description: p.market_description || '',
      }),
    )
  ) {
    await load()
  }
}

function resetFilters() {
  keyword.value = ''
  filterPlatform.value = ''
  filterEnabled.value = ''
  filterMarket.value = ''
  pageNum.value = 1
}

onMounted(load)
</script>

<template>
  <section id="market-pools-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">默认分组</h2>
        <p class="y-page-desc">买家走平台默认分组时按售价 D 计费；可将分组上架到市场推销</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="y-btn" :disabled="busy" @click="openNew">新建分组</button>
        <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input
              class="filter-input w-full"
              v-model="keyword"
              type="search"
              placeholder="名称 / 标题 / ID"
              @keyup.enter="pageNum = 1"
            />
          </div>
        </div>
        <div class="filter-item">
          <label class="filter-label">平台</label>
          <select v-model="filterPlatform" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">平台：全部</option>
            <option v-for="o in PLATFORM_OPTIONS" :key="o.value" :value="o.value">{{ o.text }}</option>
          </select>
        </div>
        <div class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="filterEnabled" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">状态：全部</option>
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </div>
        <div class="filter-item">
          <label class="filter-label">市场</label>
          <select v-model="filterMarket" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">市场：全部</option>
            <option value="true">已上架</option>
            <option value="false">未上架</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button type="button" class="btn btn-secondary" @click="resetFilters">重置</button>
          <button type="button" class="btn btn-primary" @click="pageNum = 1">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!pageRows.length" class="y-msg">没有符合条件的分组</p>
      <table v-else>
        <thead>
          <tr>
            <th>分组</th>
            <th>平台</th>
            <th>售价 D</th>
            <th>毛利 / 缓冲</th>
            <th>寄售上限</th>
            <th>状态</th>
            <th>市场</th>
            <th>sub2api</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in pageRows" :key="row.id">
            <td>
              <div>{{ row.name }}</div>
              <div v-if="row.market_published && row.market_title" class="row-sub">{{ row.market_title }}</div>
            </td>
            <td>{{ platformLabel(row.platform) }}</td>
            <td>{{ fmtMul(row.sell_multiplier) }}</td>
            <td>{{ fmtPct(row.min_margin) }} / {{ fmtPct(row.safety_buffer) }}</td>
            <td>{{ fmtMul(cap(row)) }}</td>
            <td>
              <span :class="row.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ row.enabled ? '启用' : '停用' }}</span>
            </td>
            <td>
              <span :class="row.market_published ? 'y-chip y-chip--ok' : 'y-chip'">
                {{ row.market_published ? '已上架' : '未上架' }}
              </span>
            </td>
            <td>{{ row.sub2api_group_id ? `#${row.sub2api_group_id}` : '未同步' }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="openEdit(row)">编辑</button>
              <button type="button" class="y-link" :disabled="busy" @click="sync(row)">同步</button>
              <button type="button" class="y-link" :disabled="busy" @click="toggleEnabled(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button
                type="button"
                class="y-link"
                :disabled="busy || (!row.market_published && (!row.enabled || !row.sub2api_group_id))"
                :title="!row.enabled || !row.sub2api_group_id ? '需先启用并同步到 sub2api' : ''"
                @click="toggleMarket(row)"
              >
                {{ row.market_published ? '下架' : '上架' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <footer v-if="total > 0" class="y-footer">
      <div class="y-pager">
        <span>共 {{ total }} 条</span>
        <button type="button" :disabled="pageNum <= 1" @click="pageNum -= 1">上一页</button>
        <button type="button" class="is-on">{{ pageNum }} / {{ pageCount }}</button>
        <button type="button" :disabled="pageNum >= pageCount" @click="pageNum += 1">下一页</button>
      </div>
    </footer>

    <div v-if="show" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="editing ? '编辑默认分组' : '新建默认分组'">
        <header class="y-modal__head">
          <h3>{{ editing ? '编辑默认分组' : '新建默认分组' }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-field">
            <span>名称（sub2api 分组名，以 tm-pool- 开头）</span>
            <input v-model="form.name" class="input w-full" :disabled="!!editing" />
          </label>
          <label class="y-field">
            <span>平台</span>
            <select v-model="form.platform" class="input w-full" :disabled="!!editing">
              <option v-for="o in PLATFORM_OPTIONS" :key="o.value" :value="o.value">{{ o.text }}</option>
            </select>
          </label>
          <div class="y-field-row y-field-row--3">
            <label class="y-field">
              <span>售价 D</span>
              <input v-model="form.sell" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>最低毛利</span>
              <input v-model="form.margin" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>安全缓冲</span>
              <input v-model="form.buffer" class="input w-full" inputmode="decimal" />
            </label>
          </div>
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用</label>
          <div class="y-sep">
            <label class="y-check">
              <input v-model="form.market_published" type="checkbox" :disabled="!editing" />
              作为商品上架到市场
            </label>
            <p class="y-hint">
              {{ editing ? '上架后出现在市场信息流，买家点击后去开通保障池 Key。' : '新建后请先同步，再编辑上架。' }}
            </p>
            <label class="y-field">
              <span>市场标题</span>
              <input
                v-model="form.market_title"
                class="input w-full"
                placeholder="如：Claude · 平台保障"
                :disabled="!editing"
              />
            </label>
            <label class="y-field">
              <span>市场介绍</span>
              <textarea
                v-model="form.market_description"
                class="input w-full y-textarea"
                rows="3"
                placeholder="一两句话介绍平台保障池优势、适用场景"
                :disabled="!editing"
              />
            </label>
          </div>
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
.row-sub {
  margin-top: 2px;
  font-size: 12px;
  opacity: 0.65;
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
  width: min(520px, 100%);
  max-height: min(90vh, 720px);
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
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.y-sep {
  display: grid;
  gap: 10px;
  padding-top: 8px;
  border-top: 1px solid rgb(0 0 0 / 6%);
}
.y-hint {
  margin: 0;
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.y-textarea {
  height: auto !important;
  min-height: 88px;
  padding: 8px 10px;
  resize: vertical;
}
@media (max-width: 720px) {
  .y-field-row--3 {
    grid-template-columns: 1fr;
  }
}
</style>
