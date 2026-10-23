<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { MARKET_API_PREFIX, marketGet } from './api'

interface PlatformLedger {
  cash: number
  revenue: number
  probe_cost: number
  adjustment: number
  buyer_balances: number
  buyer_store_credit?: number
  seller_payable: number
}

interface AuditLog {
  id: number
  actor_type: string
  actor_id?: number
  action: string
  target?: string
  detail: unknown
  created_at: string
}

const tab = ref<'ledger' | 'audit'>('ledger')
const ledger = ref<PlatformLedger | null>(null)
const logs = ref<AuditLog[]>([])
const loading = ref(false)
const error = ref('')
const keyword = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'ledger') {
      ledger.value = await marketGet<PlatformLedger>(`${MARKET_API_PREFIX}/ledger`)
    } else {
      const q = keyword.value.trim() ? `?q=${encodeURIComponent(keyword.value.trim())}` : ''
      logs.value = (await marketGet<AuditLog[] | null>(`${MARKET_API_PREFIX}/audit-logs${q}`)) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    ledger.value = null
    logs.value = []
  } finally {
    loading.value = false
  }
}

function detailText(d: unknown) {
  if (d == null) return '—'
  if (typeof d === 'string') return d
  try {
    return JSON.stringify(d)
  } catch {
    return '—'
  }
}

watch(tab, load)
onMounted(load)
</script>

<template>
  <section id="market-audit-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">{{ tab === 'ledger' ? '平台账本' : '审计日志' }}</h2>
        <p class="y-page-desc">
          {{ tab === 'ledger' ? '平台现金、收入与应付概览' : '管理操作留痕' }}
        </p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'ledger' }" @click="tab = 'ledger'">账本</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'audit' }" @click="tab = 'audit'">审计</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'ledger'">
        <p v-if="!ledger" class="y-msg">暂无账本数据</p>
        <div v-else class="ledger-grid">
          <div class="ledger-card"><span>现金</span><strong>{{ ledger.cash }}</strong></div>
          <div class="ledger-card"><span>收入</span><strong>{{ ledger.revenue }}</strong></div>
          <div class="ledger-card"><span>检测成本</span><strong>{{ ledger.probe_cost }}</strong></div>
          <div class="ledger-card"><span>调整</span><strong>{{ ledger.adjustment }}</strong></div>
          <div class="ledger-card"><span>买家余额</span><strong>{{ ledger.buyer_balances }}</strong></div>
          <div class="ledger-card"><span>店铺抵扣</span><strong>{{ ledger.buyer_store_credit ?? 0 }}</strong></div>
          <div class="ledger-card"><span>卖家应付</span><strong>{{ ledger.seller_payable }}</strong></div>
        </div>
      </template>

      <template v-else>
        <div class="y-search">
          <input v-model="keyword" type="search" placeholder="动作 / 目标" @keyup.enter="load" />
          <button type="button" class="y-btn" @click="load">查询</button>
        </div>
        <p v-if="!logs.length" class="y-msg">暂无审计记录</p>
        <table v-else>
          <thead>
            <tr>
              <th>ID</th>
              <th>操作者</th>
              <th>动作</th>
              <th>目标</th>
              <th>详情</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="l in logs" :key="l.id">
              <td>{{ l.id }}</td>
              <td>{{ l.actor_type }}{{ l.actor_id != null ? ` #${l.actor_id}` : '' }}</td>
              <td>{{ l.action }}</td>
              <td>{{ l.target || '—' }}</td>
              <td class="detail">{{ detailText(l.detail) }}</td>
              <td>{{ l.created_at || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ logs.length }} 条</footer>
      </template>
    </div>
  </section>
</template>

<style scoped>
.ledger-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
  padding: 8px 0 16px;
}
.ledger-card {
  display: grid;
  gap: 6px;
  padding: 14px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.55);
  border: 1px solid rgb(0 0 0 / 6%);
}
.ledger-card span {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.ledger-card strong {
  font-size: 18px;
  font-weight: 650;
}
.detail {
  max-width: 18rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
</style>
