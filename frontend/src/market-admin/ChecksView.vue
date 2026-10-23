<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface InspectionCheck {
  key: string
  name: string
  flow?: string
  category?: string
  role?: string
  enabled: boolean
  pricing?: boolean | null
  default_pricing?: boolean
  calibrated: boolean
  calibration_note?: string
  coefficients: Record<string, number>
  thresholds: Record<string, number>
  description?: string
  advice?: string
}

const CATEGORY_LABEL: Record<string, string> = {
  capability: '能力',
  identity: '身份',
  quality: '质量',
  safety: '安全',
  other: '其他',
}

const FLOW_LABEL: Record<string, string> = {
  intelligence: '测智',
  degradation: '降智',
  identity: '身份',
}

const checks = ref<InspectionCheck[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const keyword = ref('')
const filterCategory = ref('')

const editing = ref<InspectionCheck | null>(null)
const form = reactive({
  enabled: false,
  pricing: null as boolean | null,
  calibrated: false,
  calibrationNote: '',
  warning: 1 as number | string,
  severe: 1 as number | string,
  otherCoefficients: '',
  thresholds: '',
})

const hasOtherCoefficients = computed(
  () => !!editing.value && Object.keys(editing.value.coefficients || {}).some((k) => k !== 'warning' && k !== 'severe'),
)

const filtered = computed(() => {
  let rows = checks.value
  if (filterCategory.value) rows = rows.filter((c) => c.category === filterCategory.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((c) => `${c.key} ${c.name}`.toLowerCase().includes(kw))
  }
  return rows
})

const categories = computed(() => {
  const set = new Set(checks.value.map((c) => c.category).filter(Boolean) as string[])
  return [...set].sort()
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    checks.value = (await marketGet<InspectionCheck[] | null>(`${MARKET_API_PREFIX}/inspection/checks`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    checks.value = []
  } finally {
    loading.value = false
  }
}

function openEdit(check: InspectionCheck) {
  editing.value = check
  const other = Object.fromEntries(
    Object.entries(check.coefficients || {}).filter(([key]) => key !== 'warning' && key !== 'severe'),
  )
  Object.assign(form, {
    enabled: check.enabled,
    pricing: check.pricing ?? null,
    calibrated: check.calibrated,
    calibrationNote: check.calibration_note || '',
    warning: check.coefficients?.warning ?? 1,
    severe: check.coefficients?.severe ?? 1,
    otherCoefficients: JSON.stringify(other, null, 2),
    thresholds: JSON.stringify(check.thresholds || {}, null, 2),
  })
}

function closeModal() {
  editing.value = null
}

function numberObject(text: string): Record<string, number> {
  const value = JSON.parse(text)
  if (
    !value ||
    Array.isArray(value) ||
    typeof value !== 'object' ||
    Object.values(value).some((n) => typeof n !== 'number' || !Number.isFinite(n))
  ) {
    throw new Error('invalid')
  }
  return value
}

async function save() {
  if (!editing.value) return
  const warning = Number(form.warning)
  const severe = Number(form.severe)
  if ([warning, severe].some((v) => !Number.isFinite(v) || v <= 0 || v > 1.05)) {
    error.value = '系数须为大于 0 且不超过 1.05 的数值'
    return
  }
  let otherCoefficients: Record<string, number>
  let thresholds: Record<string, number>
  try {
    otherCoefficients = numberObject(form.otherCoefficients || '{}')
    thresholds = numberObject(form.thresholds || '{}')
  } catch {
    error.value = '其他系数与阈值需要是数值 JSON 对象'
    return
  }
  const coefficients = { ...otherCoefficients, warning, severe }
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/inspection/checks/${editing.value.key}`, {
      enabled: form.enabled,
      pricing: form.pricing,
      calibrated: form.calibrated,
      calibration_note: form.calibrationNote,
      coefficients,
      thresholds,
    })
    msg.value = '质检项已保存'
    closeModal()
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

async function quickToggle(check: InspectionCheck) {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/inspection/checks/${check.key}`, {
      enabled: !check.enabled,
    })
    msg.value = `${check.key} 已${check.enabled ? '关闭' : '开启'}`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-checks-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">质检项</h2>
        <p class="y-page-desc">启用规则、扣价开关、校准状态与系数阈值</p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>

      <div class="y-search">
        <input v-model="keyword" type="search" placeholder="质检项键 / 名称" />
        <select v-model="filterCategory" class="input">
          <option value="">分组：全部</option>
          <option v-for="c in categories" :key="c" :value="c">{{ CATEGORY_LABEL[c] || c }}</option>
        </select>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!filtered.length" class="y-msg">没有符合条件的质检项</p>
      <table v-else>
        <thead>
          <tr>
            <th>质检项</th>
            <th>流程</th>
            <th>分组</th>
            <th>启用</th>
            <th>扣价</th>
            <th>校准</th>
            <th>系数</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in filtered" :key="row.key">
            <td>
              <span class="mono">{{ row.key }}</span>
              <span class="row-sub">{{ row.name }}</span>
            </td>
            <td>{{ row.flow ? FLOW_LABEL[row.flow] || row.flow : '—' }}</td>
            <td>{{ CATEGORY_LABEL[row.category || ''] || row.category || '—' }}</td>
            <td>
              <span :class="row.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ row.enabled ? '开启' : '关闭' }}</span>
            </td>
            <td>{{ (row.pricing ?? row.default_pricing ?? false) ? '开' : '关' }}</td>
            <td>
              <span :class="row.calibrated ? 'y-chip y-chip--ok' : 'y-chip y-chip--warn'">
                {{ row.calibrated ? '已完成' : '待校准' }}
              </span>
            </td>
            <td>
              关注 {{ row.coefficients?.warning ?? '—' }} · 严重 {{ row.coefficients?.severe ?? '—' }}
            </td>
            <td class="y-col-op">
              <button type="button" class="y-link" :disabled="busy" @click="quickToggle(row)">
                {{ row.enabled ? '关闭' : '开启' }}
              </button>
              <button type="button" class="y-link" @click="openEdit(row)">编辑</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="!loading" class="y-footer">共 {{ filtered.length }} 条</footer>

    <div v-if="editing" class="y-modal-mask" @click.self="closeModal">
      <div class="y-modal" role="dialog" :aria-label="`编辑 ${editing.key}`">
        <header class="y-modal__head">
          <h3>编辑 {{ editing.key }} · {{ editing.name }}</h3>
          <button type="button" class="y-link" @click="closeModal">关闭</button>
        </header>
        <div class="y-modal__body">
          <label class="y-check"><input v-model="form.enabled" type="checkbox" /> 启用质检项</label>
          <label class="y-field">
            <span>用于扣价</span>
            <select v-model="form.pricing" class="input w-full">
              <option :value="true">开</option>
              <option :value="false">关</option>
              <option :value="null">恢复默认</option>
            </select>
            <span class="hint">恢复默认后：{{ editing.default_pricing ? '开' : '关' }}</span>
          </label>
          <label class="y-check"><input v-model="form.calibrated" type="checkbox" /> 已完成运营校准</label>
          <p class="hint">校准只影响门槛（是否暂停），不影响扣价。</p>
          <label class="y-field">
            <span>校准记录</span>
            <textarea v-model="form.calibrationNote" class="input w-full y-textarea" rows="2" />
          </label>
          <div class="y-field-row">
            <label class="y-field">
              <span>需关注系数</span>
              <input v-model.number="form.warning" type="number" min="0.01" max="1.05" step="0.01" class="input w-full" />
            </label>
            <label class="y-field">
              <span>严重异常系数</span>
              <input v-model.number="form.severe" type="number" min="0.01" max="1.05" step="0.01" class="input w-full" />
            </label>
          </div>
          <label v-if="hasOtherCoefficients" class="y-field">
            <span>其他系数（JSON）</span>
            <textarea v-model="form.otherCoefficients" class="input w-full y-textarea mono" rows="2" />
          </label>
          <label class="y-field">
            <span>阈值（JSON）</span>
            <textarea v-model="form.thresholds" class="input w-full y-textarea mono" rows="2" />
          </label>
          <p v-if="editing.description || editing.advice" class="hint">
            {{ editing.description }}{{ editing.advice }}
          </p>
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
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.row-sub {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  opacity: 0.65;
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
  min-height: 64px;
  padding: 8px 10px;
  resize: vertical;
}
</style>
