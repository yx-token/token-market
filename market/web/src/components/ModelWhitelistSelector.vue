<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { seller } from '@/api'
import { dateTime } from '@/utils/format'

const props = defineProps<{ upstreamId: number; disabled?: boolean }>()
const models = defineModel<string[]>({ default: () => [] })
const emit = defineEmits<{ valid: [valid: boolean] }>()
const fieldID = useId()
const catalog = ref<string[]>([])
const fetchedAt = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const search = ref('')
const manual = ref('')
const filtered = computed(() => catalog.value.filter((m) => m.toLowerCase().includes(search.value.trim().toLowerCase())))
const unavailable = computed(() => catalog.value.length ? models.value.filter((m) => !catalog.value.includes(m)) : [])
let request = 0

watch(() => props.upstreamId, () => {
  loaded.value = false
  models.value = []
  catalog.value = []
  search.value = ''
  manual.value = ''
  void load()
}, { immediate: true })

watch([models, loaded, loading, unavailable], () => {
  emit('valid', loaded.value && !loading.value && models.value.length > 0 && unavailable.value.length === 0)
}, { immediate: true })

async function load(refresh = false) {
  const serial = ++request
  const id = props.upstreamId
  loading.value = true
  error.value = ''
  try {
    const data = await (refresh ? seller.refreshUpstreamModels(id) : seller.upstreamModels(id))
    if (serial !== request) return
    catalog.value = [...new Set(data.catalog ?? [])]
    fetchedAt.value = data.fetched_at
    if (!loaded.value) models.value = [...(data.whitelist ?? catalog.value)]
    // 刷新目录保留卖家的当前选择，失效模型由卖家确认后移除。
    manual.value = models.value.join('\n')
    loaded.value = true
  } catch (e) {
    if (serial === request) error.value = e instanceof Error ? e.message : '模型目录加载失败，请重试'
  } finally {
    if (serial === request) loading.value = false
  }
}

function toggle(model: string) {
  models.value = models.value.includes(model) ? models.value.filter((m) => m !== model) : [...models.value, model]
}

function updateManual() {
  models.value = [...new Set(manual.value.split(/[\s,，]+/).map((m) => m.trim()).filter(Boolean))]
}
</script>

<template>
  <div class="space-y-3" :aria-busy="loading">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <p class="text-sm text-ink-600">只接收已选择模型的请求，至少选择一个挂售模型。</p>
      <button type="button" class="xm-btn xm-btn-ghost xm-btn-sm" :disabled="loading || disabled" @click="load(true)">{{ loading ? '加载中…' : '刷新目录' }}</button>
    </div>
    <p v-if="error" class="callout callout-err text-sm" role="alert">{{ error }}<button type="button" class="link ml-2" :disabled="loading || disabled" @click="load()">重试</button></p>
    <p v-if="loading && !loaded" class="hint" role="status">正在加载账号支持的模型…</p>
    <template v-if="loaded">
      <template v-if="catalog.length">
        <label class="label" :for="`${fieldID}-search`">搜索模型</label>
        <input :id="`${fieldID}-search`" v-model="search" type="search" class="input" placeholder="输入模型 ID 搜索" :disabled="disabled || loading" />
        <div class="flex flex-wrap items-center gap-3 text-sm">
          <button type="button" class="link" :disabled="disabled || loading" @click="models = [...catalog]">全选</button>
          <button type="button" class="link" :disabled="disabled || loading" @click="models = []">清空</button>
          <span class="text-ink-500" role="status">已选 {{ models.length }} / {{ catalog.length }} 个</span>
        </div>
        <div class="max-h-64 space-y-2 overflow-y-auto border border-ink-200 p-3" role="group" aria-label="挂售模型列表">
          <label v-for="model in filtered" :key="model" class="flex items-start gap-2 text-sm">
            <input type="checkbox" class="mt-0.5" :checked="models.includes(model)" :disabled="disabled || loading" @change="toggle(model)" />
            <span class="break-all font-mono text-xs">{{ model }}</span>
          </label>
          <p v-if="!filtered.length" class="hint">没有匹配的模型。</p>
        </div>
        <p v-if="unavailable.length" class="callout callout-warn text-sm">以下已选模型不在当前目录中：{{ unavailable.join('、') }}。请全选或重新选择目录内的模型。</p>
      </template>
      <div v-else>
        <p class="callout callout-warn mb-3 text-sm">当前没有可用模型目录，请填写账号支持的模型 ID。</p>
        <label class="label" :for="`${fieldID}-manual`">挂售模型 ID</label>
        <textarea :id="`${fieldID}-manual`" v-model="manual" rows="4" class="input font-mono text-xs" placeholder="每行一个模型 ID，也可用逗号分隔" :disabled="disabled || loading" spellcheck="false" @input="updateManual" />
        <p class="hint" role="status">已填 {{ models.length }} 个模型；请确认这些模型可用。</p>
      </div>
      <p v-if="!models.length" class="hint text-red-600">请至少选择或填写一个挂售模型。</p>
      <p class="hint">目录更新时间：{{ fetchedAt ? dateTime(fetchedAt) : '未提供' }}</p>
    </template>
  </div>
</template>
