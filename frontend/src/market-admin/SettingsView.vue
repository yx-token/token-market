<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

defineOptions({ name: 'MarketAdminSettings' })

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
const loading = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await marketGet<Record<string, unknown>>(`${MARKET_API_PREFIX}/settings`)
    for (const f of fields) form[f.key] = data[f.key] == null ? '' : String(data[f.key])
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
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

onMounted(load)
</script>

<template>
  <section id="market-settings-page" class="settings-shell">
    <header class="y-header">
      <div class="page-heading">
        <h2 class="y-title">市场设置</h2>
        <p class="y-page-desc">抽佣、冻结与提现等平台参数</p>
      </div>
      <div class="y-header-actions">
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
        <button type="button" class="btn btn-primary page-action-btn" :disabled="busy || loading" @click="save">
          {{ busy ? '保存中…' : '保存' }}
        </button>
      </div>
    </header>

    <div class="form-panel">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>
      <form v-else class="form-grid" @submit.prevent="save">
        <label v-for="f in fields" :key="f.key" class="form-field">
          <span>{{ f.label }}</span>
          <input v-model="form[f.key]" class="input w-full" inputmode="decimal" />
        </label>
      </form>
    </div>
  </section>
</template>

<style scoped>
#market-settings-page.settings-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  gap: 12px;
}
.page-heading { min-width: 0; }
.y-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}
.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.form-panel {
  max-width: 40rem;
  padding: 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.08);
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
}
.form-grid {
  display: grid;
  gap: 12px;
}
.form-field {
  display: grid;
  gap: 4px;
  font-size: 13px;
}
.form-field > span {
  color: var(--color-text-secondary, #868686);
}
</style>
