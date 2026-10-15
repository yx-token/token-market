<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { seller } from '@/api'
import { sellerStoreCredits, type CreditOffer, type CreditOfferInput } from '@/api/sellerStoreCredits'
import type { VendorView } from '@/api/types'
import { useAction } from '@/composables/useAsync'
import { useConfig } from '@/stores/config'
import { useToast } from '@/stores/toast'
import PageHeader from '@/components/PageHeader.vue'
import RefreshButton from '@/components/RefreshButton.vue'
import { coverageUnits, creditFormError, emptyCreditDraft, type CreditDraft } from '@/components/seller/creditForm'
import { label } from '@/utils/labels'
import { money, parseAmount, usd } from '@/utils/format'
import '@/styles/seller-center.css'

const cfg = useConfig()
const toast = useToast()
const { busy, run } = useAction()
const offers = ref<CreditOffer[] | null>(null)
const loading = ref(false)
const loadError = ref('')
const vendors = ref<VendorView[]>([])
const editingId = ref<number | null>(null)
const draft = ref<CreditDraft>(emptyCreditDraft())
const modelInput = ref('')
const formError = ref('')
const showPaused = ref(true)

const rate = computed(() => cfg.config.credit_usd_to_balance_rate ?? 1)
const face = computed(() => parseAmount(draft.value.face))
const price = computed(() => parseAmount(draft.value.price))
const coverage = computed(() => (face.value ? coverageUnits(face.value, rate.value) : null))
const visible = computed(() => (offers.value ?? []).filter((row) => showPaused.value || row.status === 'active'))

onMounted(() => {
  cfg.ensure()
  reload()
  seller.vendors().then((rows) => { vendors.value = rows ?? [] }).catch(() => { vendors.value = [] })
})

async function reload() {
  loading.value = true
  loadError.value = ''
  try {
    offers.value = (await sellerStoreCredits.list()) ?? []
  } catch (error) {
    offers.value = null
    loadError.value = error instanceof Error ? error.message : '活动额度没有载入'
    toast.error(error)
  } finally {
    loading.value = false
  }
}

function resetForm() {
  editingId.value = null
  draft.value = emptyCreditDraft()
  modelInput.value = ''
  formError.value = ''
}

function edit(row: CreditOffer) {
  editingId.value = row.id
  draft.value = {
    title: row.title,
    description: row.description ?? '',
    price: String(row.price_cny),
    face: String(row.face_value_usd),
    platforms: [...(row.allowed_platforms ?? [])],
    models: [...(row.allowed_models ?? [])],
  }
  formError.value = ''
}

function togglePlatform(id: string) {
  const set = new Set(draft.value.platforms)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  draft.value.platforms = [...set]
}

function addModel() {
  const name = modelInput.value.trim()
  if (!name || draft.value.models.includes(name)) {
    modelInput.value = ''
    return
  }
  draft.value.models = [...draft.value.models, name]
  modelInput.value = ''
}

function payload(status?: 'active' | 'paused'): CreditOfferInput {
  return {
    title: draft.value.title.trim(),
    description: draft.value.description.trim() || undefined,
    price_cny: parseAmount(draft.value.price) ?? 0,
    face_value_usd: parseAmount(draft.value.face) ?? 0,
    allowed_platforms: [...draft.value.platforms],
    allowed_models: [...draft.value.models],
    ...(status ? { status } : {}),
  }
}

async function save(status?: 'active' | 'paused') {
  formError.value = creditFormError(draft.value)
  if (formError.value) return
  const body = payload(status)
  const done = editingId.value
    ? await run(() => sellerStoreCredits.update(editingId.value!, body), '活动已更新')
    : await run(() => sellerStoreCredits.create(body), '活动已上架')
  if (!done) return
  resetForm()
  await reload()
}

async function setStatus(row: CreditOffer, status: 'active' | 'paused') {
  const body: CreditOfferInput = {
    title: row.title,
    description: row.description || undefined,
    price_cny: row.price_cny,
    face_value_usd: row.face_value_usd,
    allowed_platforms: row.allowed_platforms ?? [],
    allowed_models: row.allowed_models ?? [],
    status,
  }
  if (await run(() => sellerStoreCredits.update(row.id, body), status === 'paused' ? '已停售' : '已重新开售')) await reload()
}

function scopeText(platforms: string[], models: string[]) {
  const plats = platforms.length ? platforms.map((id) => label('platform', id).text).join('、') : '本店全部厂商'
  const mods = models.length ? models.join('、') : '全部模型'
  return `${plats} · ${mods}`
}
</script>

<template>
  <div class="seller-page">
    <PageHeader title="店铺活动额度" description="只在本店生效。买家付人民币，获得美元额度；本店消费优先扣这份额度，用完后继续用钱包余额。">
      <RefreshButton :loading="loading" @click="reload" />
    </PageHeader>

    <section class="card" aria-labelledby="credit-form-title">
      <div class="seller-section">
        <div>
          <p class="seller-kicker">{{ editingId ? `编辑活动 #${editingId}` : '上架新活动' }}</p>
          <h2 id="credit-form-title">实付价和美元额度要同时写清</h2>
        </div>
        <button v-if="editingId" type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="resetForm">取消编辑</button>
      </div>
      <div class="mt-4 grid gap-3 md:grid-cols-2">
        <div class="md:col-span-2">
          <label class="label" for="credit-title">活动标题</label>
          <input id="credit-title" v-model="draft.title" class="input" maxlength="80" placeholder="例如：本店 Claude 10 美元额度" />
        </div>
        <div>
          <label class="label" for="credit-price">买家实付（人民币）</label>
          <input id="credit-price" v-model="draft.price" class="input" inputmode="decimal" placeholder="30.00" />
        </div>
        <div>
          <label class="label" for="credit-face">美元额度</label>
          <input id="credit-face" v-model="draft.face" class="input" inputmode="decimal" placeholder="10.00" />
        </div>
        <div class="md:col-span-2">
          <label class="label" for="credit-desc">给买家的说明</label>
          <textarea id="credit-desc" v-model="draft.description" class="input" rows="2" placeholder="额度用完后会继续用钱包余额，不会中断正在使用的 Key。" />
        </div>
      </div>

      <fieldset class="mt-4">
        <legend class="label">适用厂商</legend>
        <p class="hint">不勾选表示本店全部厂商。和模型限制同时存在时，调用必须同时匹配。</p>
        <div class="seller-filters">
          <button
            v-for="v in vendors"
            :key="v.platform"
            type="button"
            class="seller-chip"
            :class="{ 'is-on': draft.platforms.includes(v.platform) }"
            :aria-pressed="draft.platforms.includes(v.platform)"
            @click="togglePlatform(v.platform)"
          >{{ label('platform', v.platform).text }}</button>
          <span v-if="!vendors.length" class="hint">厂商列表暂不可用，留空即本店全部。</span>
        </div>
      </fieldset>

      <div class="mt-4">
        <label class="label" for="credit-model">适用模型</label>
        <div class="seller-toolbar">
          <input id="credit-model" v-model="modelInput" class="input" placeholder="输入模型名后添加，留空表示全部" @keydown.enter.prevent="addModel" />
          <button type="button" class="xm-btn xm-btn-ghost" @click="addModel">添加模型</button>
        </div>
        <div v-if="draft.models.length" class="seller-filters mt-2">
          <button v-for="name in draft.models" :key="name" type="button" class="seller-chip is-on" @click="draft.models = draft.models.filter((item) => item !== name)">{{ name }} · 移除</button>
        </div>
      </div>

      <aside class="seller-callout mt-4" data-testid="credit-preview">
        <p class="seller-kicker">买家会看到</p>
        <p>实付 <strong>{{ price ? money(price) : '—' }}</strong>，获得 <strong>{{ face ? usd(face) : '—' }}</strong> 本店额度。</p>
        <p>按当前兑换率，$1 额度可抵 ¥{{ rate }} 店铺消费；购买后按购买时兑换率使用，后续调整不影响已购包。</p>
        <p v-if="coverage !== null">这份额度大约可抵 {{ money(coverage) }} 本店消费。额度用完，或这一笔不够时，继续用钱包余额。</p>
        <p>范围：{{ scopeText(draft.platforms, draft.models) }}。不能用于其他店。</p>
      </aside>
      <p v-if="formError" class="seller-form-error mt-3" role="alert">{{ formError }}</p>
      <div class="seller-sticky-actions mt-4">
        <button type="button" class="xm-btn xm-btn-primary" :disabled="busy" @click="save(editingId ? undefined : 'active')">{{ editingId ? '保存修改' : '上架活动' }}</button>
        <button v-if="editingId" type="button" class="xm-btn xm-btn-ghost" :disabled="busy" @click="save('paused')">保存并停售</button>
      </div>
    </section>

    <section>
      <div class="seller-section">
        <h2>已上架活动</h2>
        <button type="button" class="seller-chip" :class="{ 'is-on': showPaused }" @click="showPaused = !showPaused">{{ showPaused ? '含停售' : '只看在售' }}</button>
      </div>
      <p v-if="loading && !offers" class="seller-lead">正在读取活动…</p>
      <div v-else-if="loadError" class="seller-empty">
        <strong>活动没有载入</strong>
        <p>{{ loadError }}</p>
        <button type="button" class="xm-btn xm-btn-primary" @click="reload">重试</button>
      </div>
      <div v-else-if="!visible.length" class="seller-empty">
        <strong>还没有{{ showPaused ? '' : '在售' }}活动</strong>
        <p>上架后只出现在本店。买家在本店消费，并且厂商、模型都匹配时，会优先用这份额度。</p>
      </div>
      <div v-else class="grid gap-3">
        <article v-for="row in visible" :key="row.id" class="card">
          <div class="seller-section">
            <div>
              <p class="seller-kicker">#{{ row.id }} · {{ row.status === 'paused' ? '停售' : '在售' }}</p>
              <h3 class="text-[16px] font-extrabold">{{ row.title }}</h3>
            </div>
            <div class="seller-toolbar">
              <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" @click="edit(row)">编辑</button>
              <button v-if="row.status !== 'paused'" type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="busy" @click="setStatus(row, 'paused')">停售</button>
              <button v-else type="button" class="xm-btn xm-btn-primary xm-btn-sm" :disabled="busy" @click="setStatus(row, 'active')">重新开售</button>
            </div>
          </div>
          <p v-if="row.description" class="mt-2 text-[13px] text-[#555]">{{ row.description }}</p>
          <p class="mt-2 text-[14px]">实付 <b>{{ money(row.price_cny) }}</b> · 额度 <b>{{ usd(row.face_value_usd) }}</b></p>
          <p class="mt-1 text-[12px] text-[#666]">{{ scopeText(row.allowed_platforms ?? [], row.allowed_models ?? []) }}。用完后继续用钱包余额。</p>
        </article>
      </div>
    </section>
  </div>
</template>
