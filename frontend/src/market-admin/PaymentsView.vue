<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

const merchants = ref<Record<string, unknown>[]>([])
const error = ref('')
const msg = ref('')
const loading = ref(false)

const rows = computed(() => merchants.value || [])

async function load() {
  loading.value = true
  error.value = ''
  try {
    const mc = await marketGet<unknown>(`${MARKET_API_PREFIX}/payments/merchants`)
    merchants.value = Array.isArray(mc) ? (mc as Record<string, unknown>[]) : []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    merchants.value = []
  } finally {
    loading.value = false
  }
}

async function toggle(row: Record<string, unknown>) {
  const provider = String(row.provider || '')
  if (!provider) return
  const next = !row.enabled
  if (!window.confirm(`${next ? '启用' : '停用'}支付通道 ${provider}？`)) return
  msg.value = ''
  error.value = ''
  try {
    await marketSend('PUT', `${MARKET_API_PREFIX}/payments/merchants/${provider}`, { enabled: next })
    msg.value = `${provider} 已${next ? '启用' : '停用'}`
    await load()
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  }
}

onMounted(load)
</script>

<template>
  <section class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">支付配置</h2>
        <p class="y-page-desc">商户通道启用状态；密钥配置仍走原校验</p>
      </div>
      <button type="button" class="y-btn" :disabled="loading" @click="load">刷新</button>
    </header>
    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>
      <table v-else>
        <thead>
          <tr>
            <th>通道</th>
            <th>启用</th>
            <th>更新时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!rows.length">
            <td colspan="4">暂无商户</td>
          </tr>
          <tr v-for="(row, i) in rows" :key="i">
            <td>{{ row.provider }}</td>
            <td>
              <span :class="row.enabled ? 'y-chip y-chip--ok' : 'y-chip'">{{ row.enabled ? '启用' : '停用' }}</span>
            </td>
            <td>{{ row.updated_at || '—' }}</td>
            <td>
              <button type="button" class="y-link" @click="toggle(row)">
                {{ row.enabled ? '停用' : '启用' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="!loading" class="y-footer">共 {{ rows.length }} 条</footer>
  </section>
</template>
