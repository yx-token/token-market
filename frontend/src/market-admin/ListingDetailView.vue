<script setup lang="ts">
/**
 * 管理端挂单详情 / 新建。
 * 新建流程对齐 C 端 PublishView 七步：代理 → 接入 → 模型 → 检测 → 商品 → 定价 → 完成。
 * 管理端选「已就绪」上游时可从代理步直接跳到商品（同 C 端 resume）。
 */
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'
import { useConsoleTabs } from '@/stores/consoleTabs'

const route = useRoute()
const router = useRouter()
const { open: openTab, close: closeTab } = useConsoleTabs()

interface ListingRow {
  id: number
  upstream_id: number
  seller_id: number
  mode: string
  status: string
  paused_by?: string | null
  multiplier?: number | null
  join_default_pool: boolean
  sku_kind?: string
  face_value?: number | null
  stock?: number | null
  min_amount?: number
  title?: string | null
  description?: string | null
  categories?: string[]
  platform?: string
  created_at?: string
  updated_at?: string
}

interface UpstreamRow {
  id: number
  platform?: string
  status?: string
  account_email?: string | null
  seller_id?: number
  tier?: string | null
  grade?: string | null
}

/** 与 C 端 PublishView 一致 */
const CREATE_STEPS = ['代理', '接入', '模型', '检测', '商品', '定价', '完成'] as const
const STEP_PROXY = 0
const STEP_AUTH = 1
const STEP_MODELS = 2
const STEP_DETECT = 3
const STEP_SKU = 4
const STEP_PRICE = 5
const STEP_DONE = 6

const isNew = computed(() => route.params.id === 'new' || route.name === 'MarketAdminListingCreate')
const listingId = computed(() => {
  if (isNew.value) return 0
  const n = Number(route.params.id)
  return Number.isFinite(n) ? n : 0
})

const row = ref<ListingRow | null>(null)
const upstreams = ref<UpstreamRow[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')
const step = ref(0)
const createdId = ref(0)

const form = reactive({
  upstream_id: '' as string | number,
  mode: 'consignment' as 'consignment' | 'pack' | 'buyback',
  multiplier: '0.8',
  join_default_pool: true,
  title: '',
  description: '',
  categories: '',
  status: 'active',
  sku_kind: 'channel',
  face_value: '100',
  stock: '3',
  min_amount: '0',
})

const selectedUpstream = computed(() => {
  const id = Number(form.upstream_id)
  if (!id) return null
  return upstreams.value.find((u) => u.id === id) ?? null
})

/** 仅 ready 可挂单（与 CreateListing 门禁一致） */
const readyUpstreams = computed(() => upstreams.value.filter((u) => u.status === 'ready'))
const otherUpstreams = computed(() => upstreams.value.filter((u) => u.status !== 'ready'))
const isUpstreamReady = computed(() => selectedUpstream.value?.status === 'ready')

const isPack = computed(() => form.mode === 'pack' || row.value?.mode === 'pack')
const needMultiplier = computed(() => form.mode === 'consignment' || form.mode === 'pack')

const modeLabel = computed(() => {
  if (form.mode === 'consignment') return '按量通道（寄售）'
  if (form.mode === 'pack') return '订阅套餐'
  if (form.mode === 'buyback') return '平台回收'
  return form.mode
})

function statusHint(status?: string): string {
  switch (status) {
    case 'ready':
      return '已就绪，可挂单'
    case 'listed':
      return '已有挂单，不能再挂'
    case 'draft':
      return '草稿：尚未完成授权'
    case 'authorizing':
      return '授权中：请完成 OAuth / Key 接入'
    case 'selecting_models':
      return '待选模型：需在 C 端或上游页完成模型选择'
    case 'verifying':
      return '检测中：等待质量检测结束'
    case 'verify_failed':
    case 'failed':
      return '检测失败：需重试检测或重新接入'
    case 'suspended':
      return '已冻结'
    default:
      return status ? `状态 ${status}，不可挂单` : '状态未知'
  }
}

function statusLabel(status?: string): string {
  const map: Record<string, string> = {
    ready: 'ready（可挂单）',
    listed: 'listed（已挂单）',
    draft: 'draft（草稿）',
    authorizing: 'authorizing（授权中）',
    selecting_models: 'selecting_models（待选模型）',
    verifying: 'verifying（检测中）',
    verify_failed: 'verify_failed（检测失败）',
    failed: 'failed',
    suspended: 'suspended（已冻结）',
  }
  return status ? map[status] || status : '—'
}

function upstreamTitle(u: UpstreamRow): string {
  const parts = [`#${u.id}`]
  if (u.platform) parts.push(u.platform)
  if (u.account_email) parts.push(u.account_email)
  if (u.tier || u.grade) parts.push(String(u.tier || u.grade))
  return parts.join(' · ')
}

function selectUpstream(id: number) {
  form.upstream_id = id
  error.value = ''
}

function goUpstreams(opts?: { add?: boolean; focusId?: number }) {
  if (opts?.focusId) {
    const path = `/admin/market/upstreams/${opts.focusId}`
    openTab(path, { title: `上游 #${opts.focusId}` })
    router.push(path)
    return
  }
  const query: Record<string, string> = {}
  if (opts?.add) query.add = '1'
  const path = '/admin/market/upstreams'
  openTab(path, { title: opts?.add ? '添加上游' : '上游管理' })
  router.push({ path, query })
}

async function refreshUpstreams() {
  await loadUpstreams()
  if (form.upstream_id && !upstreams.value.some((u) => u.id === Number(form.upstream_id))) {
    form.upstream_id = ''
  }
}

function onVisibilityRefresh() {
  if (document.visibilityState === 'visible' && isNew.value && step.value === STEP_PROXY) {
    void refreshUpstreams()
  }
}

function mapCreateError(raw: string): string {
  const m = raw.toLowerCase()
  if (m.includes('valid authorization') || m.includes('ready before listing') || m.includes('upstream_not_ready')) {
    return '上游未就绪：须已完成授权且状态为 ready 才能挂单。请改选已就绪上游，或到「上游管理」完成接入/检测。'
  }
  if (m.includes('privacy_required') || m.includes('training data')) {
    return '隐私检查未通过，不能挂单。'
  }
  if (m.includes('models_required') || m.includes('select sale models')) {
    return '请先为上游选择挂售模型。'
  }
  if (m.includes('unhealthy')) {
    return '上游健康状态异常，不能挂单。'
  }
  if (m.includes('inspection_required')) {
    return '平台回收须先有有效质量检测。'
  }
  return raw
}

function fillForm(r: ListingRow) {
  Object.assign(form, {
    upstream_id: String(r.upstream_id),
    mode: (r.mode as typeof form.mode) || 'consignment',
    multiplier: r.multiplier != null ? String(r.multiplier) : '',
    join_default_pool: r.join_default_pool,
    title: r.title || '',
    description: r.description || '',
    categories: (r.categories || []).join(','),
    status: r.status,
    sku_kind: r.sku_kind || 'channel',
    face_value: r.face_value != null ? String(r.face_value) : '',
    stock: r.stock != null ? String(r.stock) : '',
    min_amount: String(r.min_amount ?? 0),
  })
}

function resetNewForm() {
  Object.assign(form, {
    upstream_id: '',
    mode: 'consignment',
    multiplier: '0.8',
    join_default_pool: true,
    title: '',
    description: '',
    categories: '',
    status: 'active',
    sku_kind: 'channel',
    face_value: '100',
    stock: '3',
    min_amount: '0',
  })
  step.value = 0
  createdId.value = 0
}

async function loadUpstreams() {
  try {
    upstreams.value = (await marketGet<UpstreamRow[] | null>(`${MARKET_API_PREFIX}/upstreams`)) ?? []
  } catch {
    upstreams.value = []
  }
}

async function load() {
  if (isNew.value) {
    row.value = null
    resetNewForm()
    openTab('/admin/market/listings/new', { title: '新建挂单' })
    await loadUpstreams()
    return
  }
  if (!listingId.value) {
    error.value = '无效挂单 ID'
    return
  }
  loading.value = true
  error.value = ''
  try {
    row.value = await marketGet<ListingRow>(`${MARKET_API_PREFIX}/listings/${listingId.value}`)
    fillForm(row.value)
    openTab(`/admin/market/listings/${listingId.value}`, { title: `挂单 #${listingId.value}` })
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    row.value = null
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
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
    return false
  } finally {
    busy.value = false
  }
}

function cats() {
  return form.categories
    .split(/[,，\s]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function validateStep(index: number): boolean {
  error.value = ''
  if (index === STEP_PROXY) {
    if (!Number(form.upstream_id)) {
      error.value = '请选择或填写上游 ID'
      return false
    }
    if (!isUpstreamReady.value) {
      error.value =
        '该上游状态不是 ready，不能挂单。请改选「已就绪」上游，或到上游管理完成授权/模型/检测。'
      return false
    }
  }
  if (index >= STEP_AUTH && index <= STEP_DETECT) {
    if (!isUpstreamReady.value) {
      error.value = '上游未就绪，无法继续。请返回代理步改选已就绪上游。'
      return false
    }
  }
  if (index === STEP_SKU) {
    if (!['consignment', 'pack', 'buyback'].includes(form.mode)) {
      error.value = '请选择商品形态'
      return false
    }
  }
  if (index === STEP_PRICE) {
    if (needMultiplier.value && !(Number(form.multiplier) > 0)) {
      error.value = '请填写有效倍率'
      return false
    }
    if (form.mode === 'pack') {
      if (!(Number(form.face_value) >= 10)) {
        error.value = '订阅套餐售价至少 10 元'
        return false
      }
      if (!(Number(form.stock) >= 1)) {
        error.value = '可售份数至少 1'
        return false
      }
    }
  }
  return true
}

function goNext() {
  if (!validateStep(step.value)) return
  // 代理步选中已就绪上游 → 跳到商品（对齐 C 端 resume）
  if (step.value === STEP_PROXY && isUpstreamReady.value) {
    step.value = STEP_SKU
    return
  }
  if (step.value >= STEP_AUTH && step.value <= STEP_DETECT && isUpstreamReady.value) {
    step.value = STEP_SKU
    return
  }
  if (step.value < STEP_PRICE) {
    step.value += 1
  }
}

function goPrev() {
  error.value = ''
  if (step.value === STEP_DONE) return
  if (step.value === STEP_SKU) {
    step.value = STEP_PROXY
    return
  }
  if (step.value > STEP_PROXY) step.value -= 1
}

function goStep(index: number) {
  if (index === step.value) return
  if (step.value === STEP_DONE) return
  if (index === STEP_DONE) return
  if (index < step.value) {
    // 从商品回到代理即可，中间接入/模型/检测对管理端选 ready 上游无独立页
    step.value = index <= STEP_DETECT ? STEP_PROXY : index
    error.value = ''
    return
  }
  if (index > step.value) {
    if (index === STEP_SKU || index === STEP_PRICE) {
      if (!validateStep(STEP_PROXY)) {
        step.value = STEP_PROXY
        return
      }
      if (index === STEP_PRICE && !validateStep(STEP_SKU)) {
        step.value = STEP_SKU
        return
      }
      step.value = index
      return
    }
    // 不允许点进未就绪的接入/模型/检测（除非上游 ready，则等同跳到商品）
    if (index >= STEP_AUTH && index <= STEP_DETECT) {
      if (!validateStep(STEP_PROXY)) {
        step.value = STEP_PROXY
        return
      }
      step.value = STEP_SKU
    }
  }
}

async function save() {
  if (isNew.value) {
    if (!validateStep(STEP_PROXY) || !validateStep(STEP_SKU) || !validateStep(STEP_PRICE)) {
      if (!isUpstreamReady.value || !Number(form.upstream_id)) step.value = STEP_PROXY
      else if (!validateStep(STEP_SKU)) step.value = STEP_SKU
      else step.value = STEP_PRICE
      return
    }
    const upstreamId = Number(form.upstream_id)
    const body: Record<string, unknown> = {
      upstream_id: upstreamId,
      mode: form.mode,
      join_default_pool: form.mode === 'consignment' ? form.join_default_pool : false,
      title: form.title.trim() || undefined,
      description: form.description.trim() || undefined,
      categories: cats(),
    }
    if (form.mode === 'consignment') {
      body.multiplier = Number(form.multiplier)
      body.min_amount = Number(form.min_amount) || 0
    } else if (form.mode === 'pack') {
      body.multiplier = Number(form.multiplier)
      body.face_value = Number(form.face_value)
      body.stock = Number(form.stock)
    }
    try {
      busy.value = true
      error.value = ''
      const created = await marketSend<{ listing?: ListingRow } & ListingRow>(
        'POST',
        `${MARKET_API_PREFIX}/listings`,
        body,
      )
      const id = created?.listing?.id ?? created?.id
      if (id) {
        createdId.value = id
        msg.value = '挂单已创建'
        step.value = STEP_DONE
        openTab(`/admin/market/listings/${id}`, { title: `挂单 #${id}` })
      }
    } catch (e) {
      error.value = mapCreateError(e instanceof Error ? e.message : '创建失败')
    } finally {
      busy.value = false
    }
    return
  }

  const body: Record<string, unknown> = {
    status: form.status,
    join_default_pool: form.join_default_pool,
    title: form.title,
    description: form.description,
    categories: cats(),
    sku_kind: form.sku_kind,
    min_amount: Number(form.min_amount) || 0,
  }
  if (form.multiplier !== '') body.multiplier = Number(form.multiplier)
  if (form.face_value !== '') body.face_value = Number(form.face_value)
  if (form.stock !== '') body.stock = Number(form.stock)
  if (
    await run(`挂单 #${listingId.value} 已保存`, () =>
      marketSend('PUT', `${MARKET_API_PREFIX}/listings/${listingId.value}`, body),
    )
  ) {
    await load()
  }
}

async function pause() {
  if (!window.confirm(`强制暂停挂单 #${listingId.value}？`)) return
  if (await run('已暂停', () => marketSend('POST', `${MARKET_API_PREFIX}/listings/${listingId.value}/pause`))) {
    await load()
  }
}
async function resume() {
  if (await run('已恢复', () => marketSend('POST', `${MARKET_API_PREFIX}/listings/${listingId.value}/resume`))) {
    await load()
  }
}
async function closeListing() {
  if (!window.confirm(`强制关闭挂单 #${listingId.value}？关闭后不可再售。`)) return
  if (await run('已关闭', () => marketSend('POST', `${MARKET_API_PREFIX}/listings/${listingId.value}/close`))) {
    await load()
  }
}

function backToList() {
  router.push('/admin/market/listings')
}

function openCreated() {
  if (!createdId.value) return
  closeTab('/admin/market/listings/new')
  router.replace(`/admin/market/listings/${createdId.value}`)
}

function resetAndAgain() {
  resetNewForm()
  router.replace('/admin/market/listings/new')
  load()
}

watch(
  () => route.params.id,
  () => load(),
)

onMounted(() => {
  void load()
  document.addEventListener('visibilitychange', onVisibilityRefresh)
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisibilityRefresh)
})
</script>

<template>
  <section id="market-listing-detail-page" class="detail-shell">
    <header class="detail-header">
      <div class="page-heading">
        <h2 class="y-title">{{ isNew ? '新建挂单' : `挂单详情 #${listingId}` }}</h2>
        <p class="y-page-desc">
          {{
            isNew
              ? '对齐卖家发布流程：选上游 → 商品形态 → 定价上架'
              : '编辑字段、强制暂停/恢复/关闭'
          }}
        </p>
      </div>
      <div class="detail-header__actions">
        <button type="button" class="btn btn-secondary" @click="backToList">返回总览</button>
        <button
          v-if="!isNew"
          type="button"
          class="btn btn-secondary"
          :disabled="loading || busy"
          @click="load"
        >
          刷新
        </button>
        <button v-if="!isNew" type="button" class="btn btn-primary" :disabled="busy" @click="save">
          保存
        </button>
      </div>
    </header>

    <div class="detail-body">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg && !isNew" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>

      <!-- ========== 新建：C 端步骤条 ========== -->
      <template v-else-if="isNew">
        <ol class="xm-stepper" aria-label="发布步骤">
          <li
            v-for="(label, i) in CREATE_STEPS"
            :key="label"
            class="xm-stepper__item"
            :class="{ 'is-on': step === i, 'is-done': step > i }"
            :aria-current="step === i ? 'step' : undefined"
            @click="goStep(i)"
          >
            <div class="xm-stepper__node">
              <span class="xm-stepper__dot" aria-hidden="true">
                <svg v-if="step > i" viewBox="0 0 16 16" width="14" height="14" fill="none">
                  <path
                    d="M3.5 8.5 6.5 11.5 12.5 4.5"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
                <template v-else>{{ i + 1 }}</template>
              </span>
              <span class="xm-stepper__label">{{ label }}</span>
            </div>
            <span
              v-if="i < CREATE_STEPS.length - 1"
              class="xm-stepper__line"
              :class="{ 'is-done': step > i }"
              aria-hidden="true"
            />
          </li>
        </ol>

        <!-- 0 代理：选已就绪上游（管理端不登记代理） -->
        <section v-if="step === STEP_PROXY" class="xm-pub-panel">
          <div class="upstream-step-head">
            <div>
              <h2>选择上游账号</h2>
              <p class="sub">
                仅 <b>ready</b> 可挂单。点选下方可选择项继续；没有可用项时点「添加上游」去推进接入。
              </p>
            </div>
            <div class="upstream-step-head__actions">
              <button type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="refreshUpstreams">
                刷新列表
              </button>
              <button type="button" class="xm-btn xm-btn-primary" @click="goUpstreams({ add: true })">
                添加上游
              </button>
            </div>
          </div>

          <div class="xm-callout" :class="readyUpstreams.length ? 'ok' : 'warn'">
            共 <b>{{ upstreams.length }}</b> 条上游 ·
            可选择 <b>{{ readyUpstreams.length }}</b> 条 ·
            未就绪 <b>{{ otherUpstreams.length }}</b> 条
          </div>

          <label class="xm-field-label">可选择的上游（ready）</label>
          <div v-if="readyUpstreams.length" class="xm-choice-list upstream-pick">
            <label
              v-for="u in readyUpstreams"
              :key="u.id"
              class="xm-choice"
              :class="{ on: Number(form.upstream_id) === u.id }"
            >
              <input
                type="radio"
                name="admin-listing-upstream"
                :value="u.id"
                :checked="Number(form.upstream_id) === u.id"
                @change="selectUpstream(u.id)"
              />
              <div>
                <strong>{{ upstreamTitle(u) }}</strong>
                <span>卖家 #{{ u.seller_id ?? '—' }} · {{ statusHint(u.status) }}</span>
              </div>
            </label>
          </div>
          <div v-else class="upstream-empty">
            <p>暂无可挂单上游。</p>
            <p class="upstream-empty__hint">
              现有账号若停在 draft / selecting_models，需卖家完成授权与选模型。点「添加上游」查看并推进，完成后回本页刷新。
            </p>
            <button type="button" class="xm-btn xm-btn-primary" @click="goUpstreams({ add: true })">
              去添加上游
            </button>
          </div>

          <div v-if="otherUpstreams.length" class="upstream-block">
            <div class="xm-field-label">未就绪（不可选，{{ otherUpstreams.length }}）</div>
            <ul class="upstream-list">
              <li v-for="u in otherUpstreams" :key="u.id" class="upstream-list__item">
                <div class="upstream-list__main">
                  <b>#{{ u.id }}</b>
                  <span>{{ u.platform || '—' }}</span>
                  <template v-if="u.account_email"><span>{{ u.account_email }}</span></template>
                  <span class="upstream-list__status">{{ statusLabel(u.status) }}</span>
                </div>
                <div class="upstream-list__hint">{{ statusHint(u.status) }}</div>
                <button
                  type="button"
                  class="upstream-list__go"
                  @click="goUpstreams({ focusId: u.id })"
                >
                  去完成接入
                </button>
              </li>
            </ul>
          </div>

          <div v-if="selectedUpstream && isUpstreamReady" class="xm-summary">
            已选 <b>{{ upstreamTitle(selectedUpstream) }}</b>
          </div>

          <div class="xm-pub-actions">
            <button type="button" class="xm-btn xm-btn-ghost" @click="backToList">取消</button>
            <button
              type="button"
              class="xm-btn xm-btn-primary"
              :disabled="busy || !isUpstreamReady"
              @click="goNext"
            >
              下一步（进入商品）
            </button>
          </div>
        </section>

        <!-- 1–3：管理端不重复做接入；仅提示 -->
        <section
          v-else-if="step === STEP_AUTH || step === STEP_MODELS || step === STEP_DETECT"
          class="xm-pub-panel"
        >
          <h2>{{ CREATE_STEPS[step] }}</h2>
          <p class="sub">
            卖家 C 端在此步完成授权 / 模型 / 检测。管理端请使用已 ready 的上游；选中后会自动跳到「商品」。
          </p>
          <div class="xm-summary">
            当前上游 <b>#{{ form.upstream_id || '—' }}</b>
            · {{ selectedUpstream?.status || '未选' }}
          </div>
          <div class="xm-pub-actions">
            <button type="button" class="xm-btn xm-btn-ghost" @click="goPrev">上一步</button>
            <button type="button" class="xm-btn xm-btn-primary" :disabled="!isUpstreamReady" @click="goNext">
              继续挂单
            </button>
          </div>
        </section>

        <!-- 4 商品形态 -->
        <section v-else-if="step === STEP_SKU" class="xm-pub-panel">
          <h2>选择商品形态</h2>
          <p class="sub">同一上游同时只能有一条有效挂单；形态决定买家怎么下单。上游 #{{ form.upstream_id }}</p>

          <div class="xm-choice-list">
            <label class="xm-choice" :class="{ on: form.mode === 'consignment' }">
              <input v-model="form.mode" type="radio" value="consignment" />
              <div>
                <strong>A. 按量通道（寄售）</strong>
                <span>买家付最低额度 M（可为 0），之后按量扣余额。可申请保障池。</span>
              </div>
            </label>
            <label class="xm-choice" :class="{ on: form.mode === 'pack' }">
              <input v-model="form.mode" type="radio" value="pack" />
              <div>
                <strong>B. 订阅套餐</strong>
                <span>买家付套餐价，获得周期内调用权；有可售份数。不进保障池。</span>
              </div>
            </label>
            <label class="xm-choice" :class="{ on: form.mode === 'buyback' }">
              <input v-model="form.mode" type="radio" value="buyback" />
              <div>
                <strong>C. 平台回收（按量）</strong>
                <span>接受平台报价，不进市场目录，只进保障池。倍率由平台定。</span>
              </div>
            </label>
          </div>

          <div class="xm-pub-actions">
            <button type="button" class="xm-btn xm-btn-ghost" @click="goPrev">上一步</button>
            <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="goNext">
              下一步
            </button>
          </div>
        </section>

        <!-- 5 定价 -->
        <section v-else-if="step === STEP_PRICE" class="xm-pub-panel">
          <h2>定价与上架内容</h2>
          <p class="sub">设置买家看到的价格与标题。上游 #{{ form.upstream_id }} · {{ modeLabel }}</p>

          <template v-if="form.mode === 'consignment'">
            <div class="xm-grid-2">
              <div>
                <label class="xm-field-label">卖家倍率 m_s</label>
                <input v-model="form.multiplier" class="xm-input" inputmode="decimal" placeholder="0.8" />
                <p class="xm-hint">买家价 ≈ 倍率 × (1 + 平台抽成)</p>
              </div>
              <div>
                <label class="xm-field-label">最低额度 M（元）</label>
                <input v-model="form.min_amount" class="xm-input" inputmode="decimal" />
                <p class="xm-hint">0 = 免费开通，之后按量扣余额</p>
              </div>
            </div>
            <label class="xm-check">
              <input v-model="form.join_default_pool" type="checkbox" />
              同时加入保障池
            </label>
          </template>

          <template v-else-if="form.mode === 'pack'">
            <div class="xm-grid-2">
              <div>
                <label class="xm-field-label">套餐售价（元）</label>
                <input v-model="form.face_value" class="xm-input" inputmode="decimal" />
                <p class="xm-hint">买家一口价，至少 10 元</p>
              </div>
              <div>
                <label class="xm-field-label">可售份数</label>
                <input v-model="form.stock" class="xm-input" inputmode="numeric" />
                <p class="xm-hint">同时生效的订阅数</p>
              </div>
            </div>
            <div class="mt-3">
              <label class="xm-field-label">期内计费倍率</label>
              <input v-model="form.multiplier" class="xm-input" inputmode="decimal" />
              <p class="xm-hint">订阅套餐不进保障池</p>
            </div>
          </template>

          <template v-else>
            <div class="xm-summary">
              平台回收：确认后按质量等级生成报价，不进市场目录。管理端创建时将直接写入回收形态。
            </div>
          </template>

          <div class="mt-4">
            <label class="xm-field-label">展示标题（可选）</label>
            <input
              v-model="form.title"
              class="xm-input"
              placeholder="留空则由系统生成"
            />
          </div>
          <div class="mt-3">
            <label class="xm-field-label">短描述（可选）</label>
            <textarea
              v-model="form.description"
              class="xm-input"
              rows="3"
              placeholder="给买家看的说明"
            />
          </div>
          <div class="mt-3">
            <label class="xm-field-label">分类（逗号分隔，可选）</label>
            <input v-model="form.categories" class="xm-input" placeholder="例如 channel,gpt" />
          </div>

          <div class="xm-pub-actions">
            <button type="button" class="xm-btn xm-btn-ghost" @click="goPrev">上一步</button>
            <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="save">
              {{ busy ? '创建中…' : '确认上架' }}
            </button>
          </div>
        </section>

        <!-- 6 完成 -->
        <section v-else-if="step === STEP_DONE" class="xm-pub-panel xm-pub-panel--center">
          <h2 class="!mb-2">已发布</h2>
          <p class="sub !mb-4">
            <template v-if="form.mode === 'pack'">订阅套餐已上架。</template>
            <template v-else-if="form.mode === 'buyback'">平台回收挂单已创建。</template>
            <template v-else>按量通道已生效。</template>
          </p>
          <div class="xm-summary text-left">
            <div>
              上游 <b>#{{ form.upstream_id }}</b> · 挂单 <b>#{{ createdId || '—' }}</b>
            </div>
            <div>形态：<b>{{ modeLabel }}</b></div>
            <div v-if="needMultiplier">倍率：<b>{{ form.multiplier }}</b></div>
            <div v-if="form.title">标题：<b>{{ form.title }}</b></div>
          </div>
          <div class="xm-pub-actions xm-pub-actions--center">
            <button type="button" class="xm-btn xm-btn-ghost" @click="resetAndAgain">再建一条</button>
            <button type="button" class="xm-btn xm-btn-primary" @click="openCreated">查看详情</button>
            <button type="button" class="xm-btn xm-btn-ghost" @click="backToList">返回总览</button>
          </div>
        </section>
      </template>

      <!-- ========== 编辑 ========== -->
      <template v-else>
        <div v-if="row" class="detail-meta">
          <div><span>卖家</span>#{{ row.seller_id }}</div>
          <div><span>上游</span>#{{ row.upstream_id }}</div>
          <div><span>平台</span>{{ row.platform || '—' }}</div>
          <div>
            <span>状态</span>{{ row.status
            }}<template v-if="row.paused_by"> · {{ row.paused_by }}</template>
          </div>
        </div>

        <div class="edit-sections">
          <section class="edit-card">
            <h3 class="edit-card__title">业务状态</h3>
            <div class="form-grid">
              <label class="y-field">
                <span>状态</span>
                <select v-model="form.status" class="input w-full">
                  <option value="active">active</option>
                  <option value="paused">paused</option>
                  <option value="pending_confirm">pending_confirm</option>
                </select>
              </label>
              <label class="y-field">
                <span>倍率</span>
                <input v-model="form.multiplier" class="input w-full" />
              </label>
              <label class="y-check form-grid__full">
                <input v-model="form.join_default_pool" type="checkbox" />
                加入保障池
              </label>
            </div>
          </section>

          <section class="edit-card">
            <h3 class="edit-card__title">展示信息</h3>
            <div class="form-grid form-grid--single">
              <label class="y-field">
                <span>标题</span>
                <input v-model="form.title" class="input w-full" />
              </label>
              <label class="y-field">
                <span>描述</span>
                <textarea v-model="form.description" class="input w-full" rows="3" />
              </label>
              <label class="y-field">
                <span>分类（逗号分隔）</span>
                <input v-model="form.categories" class="input w-full" />
              </label>
            </div>
          </section>

          <section class="edit-card">
            <h3 class="edit-card__title">售卖配置</h3>
            <div class="form-grid">
              <label class="y-field">
                <span>sku_kind</span>
                <input v-model="form.sku_kind" class="input w-full" />
              </label>
              <label class="y-field">
                <span>最低金额</span>
                <input v-model="form.min_amount" class="input w-full" />
              </label>
              <template v-if="isPack">
                <label class="y-field">
                  <span>面值</span>
                  <input v-model="form.face_value" class="input w-full" />
                </label>
                <label class="y-field">
                  <span>库存</span>
                  <input v-model="form.stock" class="input w-full" />
                </label>
              </template>
            </div>
          </section>
        </div>

        <div class="detail-actions">
          <button
            v-if="row?.status === 'active'"
            type="button"
            class="btn btn-secondary"
            :disabled="busy"
            @click="pause"
          >
            强制暂停
          </button>
          <button
            v-if="row?.status === 'paused'"
            type="button"
            class="btn btn-secondary"
            :disabled="busy"
            @click="resume"
          >
            恢复
          </button>
          <button
            v-if="row?.status !== 'closed'"
            type="button"
            class="btn btn-danger"
            :disabled="busy"
            @click="closeListing"
          >
            强制关闭
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.detail-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
  flex-shrink: 0;
}

.page-heading {
  min-width: 0;
}

.y-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  line-height: 26px;
  color: #1f1f1f;
}

.y-page-desc {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 20px;
  color: #8c8c8c;
}

.detail-header__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.detail-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-bottom: 8px;
}

/* —— C 端步骤条 —— */
.xm-stepper {
  display: flex;
  align-items: flex-start;
  list-style: none;
  margin: 0;
  padding: 18px 20px 16px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 16px;
  flex-shrink: 0;
}

.xm-stepper__item {
  display: flex;
  align-items: flex-start;
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

.xm-stepper__item:last-child {
  flex: 0 0 auto;
}

.xm-stepper__node {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  min-width: 44px;
}

.xm-stepper__dot {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 999px;
  border: 2px solid rgba(0, 0, 0, 0.12);
  background: #fafafa;
  color: #999;
  font-size: 13px;
  font-weight: 800;
  line-height: 1;
}

.xm-stepper__label {
  font-size: 12px;
  font-weight: 700;
  color: #999;
  white-space: nowrap;
}

.xm-stepper__line {
  flex: 1;
  height: 2px;
  margin: 15px 8px 0;
  background: rgba(0, 0, 0, 0.1);
  border-radius: 999px;
}

.xm-stepper__line.is-done {
  background: var(--color-primary, #0555ce);
}

.xm-stepper__item.is-done .xm-stepper__dot {
  background: var(--color-primary, #0555ce);
  border-color: var(--color-primary, #0555ce);
  color: #fff;
}

.xm-stepper__item.is-done .xm-stepper__label {
  color: #1f1f1f;
}

.xm-stepper__item.is-on .xm-stepper__dot {
  background: var(--color-primary, #0555ce);
  border-color: var(--color-primary, #0555ce);
  color: #fff;
  box-shadow: 0 0 0 3px rgba(5, 85, 206, 0.2);
}

.xm-stepper__item.is-on .xm-stepper__label {
  color: var(--color-primary, #0555ce);
}

/* —— 面板 —— */
.xm-pub-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 280px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 16px;
  padding: 24px 28px 20px;
}

.xm-pub-panel--center {
  text-align: center;
  align-items: center;
}

.xm-pub-panel h2 {
  margin: 0 0 6px;
  font-size: 20px;
  font-weight: 800;
  color: #1f1f1f;
  align-self: stretch;
  text-align: left;
}

.xm-pub-panel--center h2 {
  text-align: center;
}

.xm-pub-panel .sub {
  margin: 0 0 20px;
  font-size: 13px;
  color: #666;
  line-height: 1.55;
  align-self: stretch;
  text-align: left;
}

.xm-pub-panel--center .sub {
  text-align: center;
}

.xm-field-label {
  display: block;
  font-size: 12px;
  font-weight: 700;
  color: #666;
  margin: 12px 0 6px;
}

.xm-field-label:first-of-type {
  margin-top: 0;
}

.xm-input {
  width: 100%;
  box-sizing: border-box;
  border: 1.5px solid rgba(0, 0, 0, 0.1);
  border-radius: 12px;
  padding: 12px 14px;
  font: inherit;
  font-size: 14px;
  background: #fff;
  color: #1f1f1f;
}

.xm-input:focus {
  outline: none;
  border-color: var(--color-primary, #0555ce);
  box-shadow: 0 0 0 3px rgba(5, 85, 206, 0.12);
}

textarea.xm-input {
  min-height: 72px;
  resize: vertical;
}

.xm-hint {
  font-size: 12px;
  color: #888;
  line-height: 1.5;
  margin-top: 6px;
}

.xm-hint.warn {
  color: #b45309;
}

.xm-callout {
  margin: 0 0 16px;
  padding: 12px 14px;
  border-radius: 12px;
  font-size: 13px;
  line-height: 1.55;
  border: 1px solid transparent;
}

.xm-callout.warn {
  background: #fff7ed;
  border-color: #fdba74;
  color: #9a3412;
}

.xm-callout.ok {
  background: #f0f9ff;
  border-color: #bae6fd;
  color: #0c4a6e;
}

.xm-link {
  color: var(--color-primary, #0555ce);
  font-weight: 700;
  text-decoration: underline;
}

.upstream-step-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px 16px;
  margin-bottom: 4px;
}

.upstream-step-head h2 {
  margin: 0;
}

.upstream-step-head .sub {
  margin-top: 6px;
  margin-bottom: 0;
}

.upstream-step-head__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  flex-shrink: 0;
}

.upstream-pick {
  margin-bottom: 4px;
  max-height: 280px;
  overflow-y: auto;
  padding-right: 2px;
}

.upstream-empty {
  margin: 0 0 8px;
  padding: 20px 16px;
  border: 1.5px dashed rgba(5, 85, 206, 0.35);
  border-radius: 14px;
  background: rgba(5, 85, 206, 0.04);
  text-align: center;
}

.upstream-empty p {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: #1f1f1f;
}

.upstream-empty__hint {
  margin: 8px auto 14px !important;
  max-width: 36em;
  font-size: 12px !important;
  font-weight: 500 !important;
  color: #666 !important;
  line-height: 1.5;
}

.upstream-block {
  margin-top: 16px;
}

.upstream-list {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 12px;
  overflow: hidden;
  max-height: 220px;
  overflow-y: auto;
}

.upstream-list__item {
  position: relative;
  padding: 10px 14px;
  padding-right: 96px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  background: #fafafa;
}

.upstream-list__item:last-child {
  border-bottom: none;
}

.upstream-list__main {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  font-size: 13px;
  color: #1f1f1f;
}

.upstream-list__status {
  padding: 1px 8px;
  border-radius: 999px;
  background: #f3f4f6;
  color: #4b5563;
  font-size: 12px;
  font-weight: 600;
}

.upstream-list__hint {
  margin-top: 4px;
  font-size: 12px;
  color: #b45309;
}

.upstream-list__go {
  position: absolute;
  top: 12px;
  right: 12px;
  border: none;
  background: transparent;
  color: var(--color-primary, #0555ce);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  padding: 0;
}

.upstream-list__go:hover {
  text-decoration: underline;
}

.xm-summary.bad {
  background: #fef2f2;
  border-color: #fecaca;
  color: #991b1b;
}

.xm-choice-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.xm-choice {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 14px;
  border: 1.5px solid rgba(0, 0, 0, 0.1);
  border-radius: 14px;
  cursor: pointer;
  background: #fafafa;
  text-align: left;
}

.xm-choice.on {
  border-color: var(--color-primary, #0555ce);
  background: rgba(5, 85, 206, 0.06);
}

.xm-choice strong {
  display: block;
  font-size: 14px;
  margin-bottom: 2px;
  color: #1f1f1f;
}

.xm-choice span {
  font-size: 12px;
  color: #777;
  line-height: 1.45;
  font-weight: 500;
}

.xm-choice input {
  accent-color: var(--color-primary, #0555ce);
  margin-top: 3px;
}

.xm-grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px 16px;
}

.xm-check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  font-size: 13px;
  font-weight: 600;
  color: #1f1f1f;
}

.xm-summary {
  margin-top: 14px;
  padding: 14px 16px;
  border-radius: 14px;
  background: #fafafa;
  border: 1px solid rgba(0, 0, 0, 0.06);
  font-size: 14px;
  line-height: 1.7;
  color: #1f1f1f;
  align-self: stretch;
}

.xm-pub-actions {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-top: auto;
  padding-top: 20px;
  border-top: 1px solid rgba(0, 0, 0, 0.08);
  flex-shrink: 0;
}

.xm-pub-actions--center {
  justify-content: center;
  flex-wrap: wrap;
  width: 100%;
}

.xm-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 40px;
  padding: 0 18px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 700;
  border: none;
  cursor: pointer;
  font: inherit;
}

.xm-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.xm-btn-primary {
  background: var(--color-primary, #0555ce);
  color: #fff;
}

.xm-btn-primary:hover:not(:disabled) {
  filter: brightness(1.05);
}

.xm-btn-ghost {
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  color: #1f1f1f;
}

.mt-3 {
  margin-top: 12px;
}

.mt-4 {
  margin-top: 16px;
}

/* —— 编辑态 —— */
.detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 18px;
  padding: 12px 16px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
  font-size: 13px;
}

.detail-meta span {
  margin-right: 6px;
  color: #8c8c8c;
}

.edit-sections {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.edit-card {
  padding: 16px 20px;
  background: #fff;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 12px;
}

.edit-card__title {
  margin: 0 0 14px;
  font-size: 14px;
  font-weight: 600;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px 20px;
  max-width: 720px;
}

.form-grid--single {
  grid-template-columns: 1fr;
  max-width: 640px;
}

.form-grid__full {
  grid-column: 1 / -1;
}

.y-field {
  display: grid;
  gap: 6px;
}

.y-field > span:first-child {
  font-size: 13px;
  font-weight: 500;
  color: #606266;
}

.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 0 12px;
}

@media (max-width: 768px) {
  .xm-grid-2,
  .form-grid {
    grid-template-columns: 1fr;
  }

  .xm-stepper {
    overflow-x: auto;
  }

  .xm-stepper__item {
    flex: 0 0 auto;
  }

  .xm-stepper__line {
    min-width: 28px;
  }

  .detail-header {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
