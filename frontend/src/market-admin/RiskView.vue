<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { MARKET_API_PREFIX, marketGet, marketSend } from './api'

interface RiskEvent {
  id: number
  kind: string
  severity: string
  user_id?: number
  related_user_id?: number
  upstream_id?: number
  detail?: Record<string, unknown>
  status: string
  created_at: string
}

interface Anomaly {
  id: number
  rule: string
  buyer_user_id?: number
  amount?: number
  detail?: Record<string, unknown>
  status: string
  note?: string
  created_at: string
}

const tab = ref<'risk' | 'anomalies'>('risk')
const riskStatus = ref('open')
const anomalyStatus = ref('open')
const keyword = ref('')
const severity = ref('')
const risks = ref<RiskEvent[]>([])
const anomalies = ref<Anomaly[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const msg = ref('')

const filteredRisks = computed(() => {
  let rows = risks.value
  if (severity.value) rows = rows.filter((e) => e.severity === severity.value)
  const kw = keyword.value.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((e) =>
      [e.id, e.kind, e.user_id, e.related_user_id].map((v) => String(v ?? '').toLowerCase()).some((s) => s.includes(kw)),
    )
  }
  return rows
})

const filteredAnomalies = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return anomalies.value
  return anomalies.value.filter((a) =>
    [a.id, a.rule, a.buyer_user_id, a.note].map((v) => String(v ?? '').toLowerCase()).some((s) => s.includes(kw)),
  )
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (tab.value === 'risk') {
      const q = riskStatus.value ? `?status=${encodeURIComponent(riskStatus.value)}` : ''
      risks.value = (await marketGet<RiskEvent[] | null>(`${MARKET_API_PREFIX}/risk-events${q}`)) ?? []
    } else {
      const q = anomalyStatus.value ? `?status=${encodeURIComponent(anomalyStatus.value)}` : ''
      anomalies.value =
        (await marketGet<Anomaly[] | null>(`${MARKET_API_PREFIX}/anomalies${q}`)) ?? []
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    risks.value = []
    anomalies.value = []
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
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}

function resolveRisk(e: RiskEvent, status: string) {
  return run('已处理', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/risk-events/${e.id}/resolve`, { status }),
  )
}

function resolveAnomaly(a: Anomaly, status: string) {
  const note = window.prompt('处理说明，可留空', '') ?? ''
  return run('异常已处理', () =>
    marketSend('POST', `${MARKET_API_PREFIX}/anomalies/${a.id}/resolve`, { status, note }),
  )
}

function detailText(d?: Record<string, unknown>) {
  if (!d) return '—'
  try {
    return JSON.stringify(d)
  } catch {
    return '—'
  }
}

watch([tab, riskStatus, anomalyStatus], load)
onMounted(load)
</script>

<template>
  <section id="market-risk-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">{{ tab === 'risk' ? '风控事件' : '计费异常' }}</h2>
        <p class="y-page-desc">
          {{
            tab === 'risk'
              ? '未处理风险会影响提现审核；必要时到「用户与实名」冻结账号'
              : '用量/计费异常待确认'
          }}
        </p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <nav class="y-tabs">
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'risk' }" @click="tab = 'risk'">风控</button>
      <button type="button" class="y-tab" :class="{ 'is-on': tab === 'anomalies' }" @click="tab = 'anomalies'">异常</button>
    </nav>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="msg" class="y-msg y-msg--ok">{{ msg }}</p>
      <div class="filter-grid">
        <div class="filter-item">
          <label class="filter-label">关键词</label>
          <div class="relative flex-1 w-full">
            <input class="filter-input w-full" v-model="keyword" type="search" placeholder="编号 / 类型 / 用户" />
          </div>
        </div>
        <div v-show="tab === 'risk'" class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="riskStatus" class="filter-input flex-1">
            <option value="open">待处理</option>
            <option value="resolved">已处理</option>
            <option value="ignored">已忽略</option>
            <option value="">全部</option>
          </select>
        </div>
        <div v-show="tab !== 'risk'" class="filter-item">
          <label class="filter-label">状态</label>
          <select v-model="anomalyStatus" class="filter-input flex-1">
            <option value="open">待处理</option>
            <option value="resolved">已处理</option>
            <option value="ignored">已忽略</option>
            <option value="">全部</option>
          </select>
        </div>
        <div v-show="tab === 'risk'" class="filter-item">
          <label class="filter-label">级别</label>
          <select v-model="severity" class="filter-input flex-1">
            <option value="">级别：全部</option>
            <option value="high">高</option>
            <option value="medium">中</option>
          </select>
        </div>
        <div class="filter-actions lg:col-span-4">
          <button
            type="button"
            class="btn btn-secondary"
            @click="
              keyword = '';
              riskStatus = 'open';
              anomalyStatus = 'open';
              severity = '';
              load()
            "
          >
            重置
          </button>
          <button type="button" class="btn btn-primary" @click="load">查询</button>
        </div>
      </div>

      <p v-if="loading" class="y-msg">加载中…</p>

      <template v-else-if="tab === 'risk'">
        <p v-if="!filteredRisks.length" class="y-msg">暂无风险事件</p>
        <table v-else>
          <thead>
            <tr>
              <th>编号</th>
              <th>类型</th>
              <th>级别</th>
              <th>用户</th>
              <th>详情</th>
              <th>状态</th>
              <th>时间</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in filteredRisks" :key="e.id">
              <td>{{ e.id }}</td>
              <td>{{ e.kind }}</td>
              <td>
                <span :class="e.severity === 'high' ? 'y-chip y-chip--err' : 'y-chip y-chip--warn'">{{ e.severity }}</span>
              </td>
              <td>
                <span v-if="e.user_id">#{{ e.user_id }}</span>
                <span v-if="e.related_user_id"> / #{{ e.related_user_id }}</span>
                <span v-if="!e.user_id && !e.related_user_id">—</span>
              </td>
              <td class="detail">{{ detailText(e.detail) }}</td>
              <td>{{ e.status }}</td>
              <td>{{ e.created_at || '—' }}</td>
              <td class="y-col-op">
                <template v-if="e.status === 'open'">
                  <button type="button" class="y-link" :disabled="busy" @click="resolveRisk(e, 'resolved')">处理</button>
                  <button type="button" class="y-link y-link--danger" :disabled="busy" @click="resolveRisk(e, 'ignored')">忽略</button>
                </template>
                <span v-else>—</span>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ filteredRisks.length }} 条</footer>
      </template>

      <template v-else>
        <p v-if="!filteredAnomalies.length" class="y-msg">暂无异常</p>
        <table v-else>
          <thead>
            <tr>
              <th>编号</th>
              <th>规则</th>
              <th>买家</th>
              <th>金额</th>
              <th>详情</th>
              <th>状态</th>
              <th>时间</th>
              <th class="y-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in filteredAnomalies" :key="a.id">
              <td>{{ a.id }}</td>
              <td>{{ a.rule }}</td>
              <td>{{ a.buyer_user_id ? `#${a.buyer_user_id}` : '—' }}</td>
              <td>{{ a.amount ?? '—' }}</td>
              <td class="detail">{{ detailText(a.detail) }}</td>
              <td>{{ a.status }}</td>
              <td>{{ a.created_at || '—' }}</td>
              <td class="y-col-op">
                <template v-if="a.status === 'open'">
                  <button type="button" class="y-link" :disabled="busy" @click="resolveAnomaly(a, 'resolved')">确认</button>
                  <button type="button" class="y-link y-link--danger" :disabled="busy" @click="resolveAnomaly(a, 'ignored')">忽略</button>
                </template>
                <span v-else>{{ a.note || '—' }}</span>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="y-footer">共 {{ filteredAnomalies.length }} 条</footer>
      </template>
    </div>
  </section>
</template>

<style scoped>
.detail {
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
</style>
