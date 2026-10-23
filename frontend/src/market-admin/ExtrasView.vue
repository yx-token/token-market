<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface Discovery {
  hot_searches?: unknown
  hot_bar?: { enabled?: boolean }
  feed?: { pin_pool_first?: boolean }
  version?: number | string
}

const form = reactive({ pin_pool_first: true })
const base = ref<Discovery>({})
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const remote = await marketGet<Discovery>(`${MARKET_API_PREFIX}/market-discovery`)
    base.value = remote ?? {}
    form.pin_pool_first = !!remote?.feed?.pin_pool_first
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  busy.value = true
  error.value = ''
  msg.value = ''
  try {
    const body = {
      hot_searches: base.value.hot_searches,
      hot_bar: { enabled: false, ...(base.value.hot_bar || {}) },
      feed: { pin_pool_first: form.pin_pool_first },
      version: base.value.version,
    }
    const res = await marketSend<Discovery>('PUT', `${MARKET_API_PREFIX}/market-discovery`, body)
    base.value = res ?? body
    form.pin_pool_first = !!base.value.feed?.pin_pool_first
    msg.value = '配置已保存'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-extras-page" class="y-page max-w-lg">
    <header class="y-header">
      <div>
        <h2 class="y-title">货架开关</h2>
        <p class="y-page-desc">控制市场信息流的默认排序</p>
      </div>
      <button type="button" class="y-btn" :disabled="busy || loading" @click="save">
        {{ busy ? '保存中…' : '保存' }}
      </button>
    </header>
    <div class="y-container--tight py-4">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>
      <label v-else class="y-check">
        <input v-model="form.pin_pool_first" type="checkbox" />
        信息流默认优先置顶保障池
      </label>
    </div>
  </section>
</template>

<style scoped>
.y-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}
</style>
