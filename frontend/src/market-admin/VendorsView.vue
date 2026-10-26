<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types: string[]
  consignment_min: number
  consignment_max: number
  buyback_base: Record<string, number>
  appraisal_base?: Record<string, number>
  default_concurrency: number
  extra_limits: Record<string, unknown>
}

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

const vendors = ref<VendorConfig[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterEnabled = ref('')
const pageNum = ref(1)
const pageSize = 15

const editing = ref<VendorConfig | null>(null)
const form = reactive({
  enabled: false,
  authTypes: [] as string[],
  min: '',
  max: '',
  base: '',
  appraisal: '',
  concurrency: '',
  extra: '',
})

const filtered = computed(() => {
  let rows = vendors.value
  if (filterEnabled.value === 'true') rows = rows.filter((v) => v.enabled)
  if (filterEnabled.value === 'false') rows = rows.filter((v) => !v.enabled)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((v) => v.platform.toLowerCase().includes(kw) || (PLATFORM_LABEL[v.platform] || '').toLowerCase().includes(kw))
  }
  return rows
})

const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageRows = computed(() => {
  const start = (pageNum.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function platLabel(p: string) {
  return PLATFORM_LABEL[p] || p
}

function authLabel(_platform: string, t: string) {
  if (t === 'oauth') return 'OAuth'
  if (t === 'api_key') return 'API Key'
  if (t === 'cookie') return 'Cookie'
  return t
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    vendors.value = (await marketGet<VendorConfig[] | null>(`${MARKET_API_PREFIX}/vendors`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    vendors.value = []
  } finally {
    loading.value = false
  }
}

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await fn()
    msg.value = label
    await load()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function openEdit(v: VendorConfig) {
  editing.value = v
  Object.assign(form, {
    enabled: v.enabled,
    authTypes: [...(v.auth_types || [])],
    min: String(v.consignment_min),
    max: String(v.consignment_max),
    base: JSON.stringify(v.buyback_base ?? {}),
    appraisal: JSON.stringify(v.appraisal_base ?? {}),
    concurrency: String(v.default_concurrency),
    extra: JSON.stringify(v.extra_limits ?? {}),
  })
}

function closeModal() {
  editing.value = null
}

async function quickToggle(v: VendorConfig) {
  await run(v.enabled ? '已关闭接入' : '已开放接入', () =>
    marketSend('PUT', `${MARKET_API_PREFIX}/vendors/${v.platform}`, { enabled: !v.enabled }),
  )
}

async function save() {
  if (!editing.value) return
  let base: Record<string, number>
  let appraisal: Record<string, number>
  let extra: Record<string, unknown>
  try {
    base = JSON.parse(form.base)
    appraisal = JSON.parse(form.appraisal)
    extra = JSON.parse(form.extra)
    if (
      !appraisal ||
      Array.isArray(appraisal) ||
      typeof appraisal !== 'object' ||
      Object.values(appraisal).some((value) => typeof value !== 'number' || !Number.isFinite(value) || value <= 0)
    ) {
      throw new Error('估价基准无效')
    }
  } catch {
    error.value = '回收基础倍率、质检估价基准与限流参数需要是 JSON 对象；估价基准须为正数'
    return
  }
  if (!form.authTypes.length) {
    error.value = '至少开放一种接入方式'
    return
  }
  const body = {
    enabled: form.enabled,
    auth_types: form.authTypes,
    consignment_min: Number(form.min),
    consignment_max: Number(form.max),
    buyback_base: base,
    appraisal_base: appraisal,
    default_concurrency: Number(form.concurrency),
    extra_limits: extra,
  }
  if (await run('已保存', () => marketSend('PUT', `${MARKET_API_PREFIX}/vendors/${editing.value!.platform}`, body))) {
    closeModal()
  }
}

onMounted(load)
</script>

<template>
  <section id="market-vendors-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">厂商</h2>
        <p class="y-page-desc">开放接入、寄售倍率区间、回收/估价基准与默认并发</p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input
              v-model="keyword"
              class="filter-input w-full"
              type="search"
              placeholder="厂商"
              @keyup.enter="pageNum = 1"
            />
          </div>
        </div>
        <div class="filter-item">
          <label class="filter-label">接入</label>
          <select v-model="filterEnabled" class="filter-input flex-1" @change="pageNum = 1">
            <option value="">全部</option>
            <option value="true">开放</option>
            <option value="false">关闭</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button
            type="button"
            class="btn btn-secondary"
            @click="
              keyword = '';
              filterEnabled = '';
              pageNum = 1
            "
          >
            重置
          </button>
          <button type="button" class="btn btn-primary" @click="pageNum = 1">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <div v-else-if="!pageRows.length" class="y-empty">没有符合条件的厂商</div>
      <table v-else>
        <thead>
          <tr>
            <th>厂商</th>
            <th>接入</th>
            <th>接入方式</th>
            <th>寄售区间</th>
            <th>默认并发</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in pageRows" :key="row.platform">
            <td>{{ platLabel(row.platform) }}</td>
            <td>
              <span :class="row.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ row.enabled ? '开放' : '关闭' }}</span>
            </td>
            <td>{{ (row.auth_types || []).map((t) => authLabel(row.platform, t)).join('、') || '—' }}</td>
            <td>{{ row.consignment_min }}–{{ row.consignment_max }}×</td>
            <td>{{ row.default_concurrency }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="quickToggle(row)">
                {{ row.enabled ? '关闭' : '开放' }}
              </button>
              <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
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

    <div v-if="editing" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="`编辑 ${platLabel(editing.platform)}`">
        <header class="y-modal__head">
          <h3>编辑 {{ platLabel(editing.platform) }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 开放卖家接入</label>
          <div class="y-field">
            <span>开放的接入方式</span>
            <div class="auth-list">
              <label v-for="t in editing.supported_auth_types || []" :key="t" class="y-check">
                <input v-model="form.authTypes" type="checkbox" :value="t" />
                {{ authLabel(editing.platform, t) }}
              </label>
            </div>
          </div>
          <div class="y-field-row">
            <label class="y-field">
              <span>寄售最低倍率</span>
              <input v-model="form.min" class="input w-full" inputmode="decimal" />
            </label>
            <label class="y-field">
              <span>寄售最高倍率</span>
              <input v-model="form.max" class="input w-full" inputmode="decimal" />
            </label>
          </div>
          <label class="y-field">
            <span>回收基础倍率（JSON）</span>
            <input v-model="form.base" class="input w-full font-mono" />
            <span class="hint">须含 default，如 {"default":0.15,"max":0.2}</span>
          </label>
          <label class="y-field">
            <span>质检估价基准（JSON，正数）</span>
            <input v-model="form.appraisal" class="input w-full font-mono" />
          </label>
          <label class="y-field">
            <span>默认并发</span>
            <input v-model="form.concurrency" class="input w-full" inputmode="numeric" />
          </label>
          <label class="y-field">
            <span>限流参数 extra_limits（JSON）</span>
            <textarea v-model="form.extra" class="input w-full y-textarea font-mono" rows="3" />
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
.auth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.hint {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
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
  width: min(560px, 100%);
  max-height: min(92vh, 800px);
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
.y-field > span:first-child {
  color: var(--color-text-secondary, #868686);
}
.y-field-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.y-textarea {
  height: auto !important;
  min-height: 72px;
  padding: 8px 10px;
  resize: vertical;
}
</style>
