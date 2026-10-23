<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

const fields = [
  { key: 'commission_rate', label: '抽佣率' },
  { key: 'freeze_hours', label: '冻结小时' },
  { key: 'withdraw_min', label: '最低提现' },
  { key: 'quote_validity_days', label: '报价有效天数' },
  { key: 'manual_retest_daily_limit', label: '每日手测上限' },
  { key: 'refund_lock_seconds', label: '退款锁定秒数' },
] as const

const form = reactive<Record<string, string>>({})
const error = ref('')
const msg = ref('')
const busy = ref(false)

async function load() {
  error.value = ''
  const data = await marketGet<Record<string, unknown>>(`${MARKET_API_PREFIX}/settings`)
  for (const f of fields) form[f.key] = data[f.key] == null ? '' : String(data[f.key])
}

async function save() {
  error.value = ''
  msg.value = ''
  busy.value = true
  try {
    const patch: Record<string, number> = {}
    for (const f of fields) {
      const n = Number(form[f.key])
      if (!Number.isFinite(n)) throw new Error(`${f.label} 不是数字`)
      patch[f.key] = n
    }
    await marketSend('PUT', `${MARKET_API_PREFIX}/settings`, patch)
    msg.value = '已保存'
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  try {
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  }
})
</script>

<template>
  <section class="y-page max-w-lg">
    <header class="y-header">
      <div>
        <h2 class="y-title">市场设置</h2>
        <p class="y-page-desc">抽佣、冻结与提现等平台参数</p>
      </div>
    </header>
    <form class="y-container--tight space-y-3 py-4" @submit.prevent="save">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <label v-for="f in fields" :key="f.key" class="block text-sm">
        <span class="mb-1 block text-gray-500">{{ f.label }}</span>
        <input v-model="form[f.key]" class="input w-full" inputmode="decimal" />
      </label>
      <button class="y-btn" type="submit" :disabled="busy">{{ busy ? '保存中…' : '保存' }}</button>
    </form>
  </section>
</template>
