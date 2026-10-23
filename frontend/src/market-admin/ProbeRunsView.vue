<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { MARKET_API_PREFIX, marketGet } from './api'

interface SuiteStat {
  items: number
  passed: number
}

interface ProbeRunRecord {
  id: number
  upstream_id: number
  status: string
  trigger?: string
  verdict?: string | null
  platform?: string
  auth_type?: string
  seller_id?: number
  account_email?: string
  is_reference?: boolean
  model?: string
  models?: string[]
  queued_at?: string
  finished_at?: string | null
  items?: number
  passed?: number
  suites?: Record<string, SuiteStat>
  summary?: {
    intelligence?: { status?: string }
    suites?: Record<string, { relative_score?: number }>
  }
}

interface ProbeRunResult {
  item_id?: number
  suite: string
  check_key?: string
  model?: string
  passed?: boolean | null
  error_class?: string
  duration_ms?: number
  excerpt?: string
  instance_idx?: number
  step?: number
}

interface ProbeRunDetail {
  run: ProbeRunRecord
  results: ProbeRunResult[]
  reference?: ProbeRunRecord
  reference_results?: ProbeRunResult[]
}

const rows = ref<ProbeRunRecord[]>([])
const loading = ref(false)
const error = ref('')
const keyword = ref('')
const pageNum = ref(1)
const pageSize = 15

const detail = ref<ProbeRunDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const showDetail = ref(false)

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return rows.value
  return rows.value.filter((r) =>
    [r.id, r.upstream_id, r.account_email, r.platform, r.status, r.trigger, r.verdict, r.model]
      .map((v) => String(v ?? '').toLowerCase())
      .some((s) => s.includes(kw)),
  )
})

const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const pageRows = computed(() => {
  const start = (pageNum.value - 1) * pageSize
  return filtered.value.slice(start, start + pageSize)
})

function fmtTime(v?: string | null) {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}

function suiteText(r: ProbeRunRecord) {
  return Object.entries(r.suites ?? {})
    .map(([s, c]) => `${s} ${c.passed}/${c.items}`)
    .join(' · ')
}

function statusChip(status: string) {
  if (status === 'succeeded' || status === 'passed') return 'y-chip y-chip--ok'
  if (status === 'failed' || status === 'error') return 'y-chip y-chip--err'
  if (['queued', 'running', 'third_party'].includes(status)) return 'y-chip y-chip--warn'
  return 'y-chip'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    rows.value = (await marketGet<ProbeRunRecord[] | null>(`${MARKET_API_PREFIX}/probe-runs`)) ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function openDetail(row: ProbeRunRecord) {
  showDetail.value = true
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  try {
    detail.value = await marketGet<ProbeRunDetail>(`${MARKET_API_PREFIX}/probe-runs/${row.id}`)
  } catch (e) {
    detailError.value = e instanceof Error ? e.message : '加载明细失败'
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  showDetail.value = false
}

const detailResults = computed(() => detail.value?.results ?? [])

onMounted(load)
</script>

<template>
  <section id="market-probe-runs-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">检测记录</h2>
        <p class="y-page-desc">所有上游的检测；点「明细」查看逐题结果</p>
      </div>
      <button type="button" class="y-btn y-btn--ghost" :disabled="loading" @click="load">刷新</button>
    </header>

    <div class="y-container--tight">
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <div class="y-search">
        <input
          v-model="keyword"
          type="search"
          placeholder="ID / 上游 / 邮箱 / 平台 / 状态"
          @keyup.enter="pageNum = 1"
        />
        <button type="button" class="y-btn" @click="pageNum = 1">查询</button>
      </div>
      <p v-if="loading" class="y-msg">加载中…</p>
      <p v-else-if="!filtered.length" class="y-msg">暂无检测记录</p>
      <table v-else>
        <thead>
          <tr>
            <th>ID</th>
            <th>上游</th>
            <th>平台</th>
            <th>状态</th>
            <th>触发</th>
            <th>套件</th>
            <th>时间</th>
            <th class="y-col-op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in pageRows" :key="r.id">
            <td>#{{ r.id }}</td>
            <td>
              #{{ r.upstream_id }}
              <span v-if="r.is_reference" class="ref-tag">基线</span>
              <div class="sub">{{ r.account_email || '—' }}</div>
            </td>
            <td>{{ r.platform || '—' }}</td>
            <td><span :class="statusChip(r.status)">{{ r.status }}</span></td>
            <td>{{ r.trigger || '—' }}</td>
            <td class="suites">{{ suiteText(r) || '—' }}</td>
            <td>{{ fmtTime(r.finished_at || r.queued_at) }}</td>
            <td class="y-col-op">
              <button type="button" class="y-link" @click="openDetail(r)">明细</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="total > 0" class="y-footer">
      <div class="y-pager">
        <span>共 {{ total }} 条</span>
        <button type="button" :disabled="pageNum <= 1" @click="pageNum -= 1">上一页</button>
        <button type="button" class="is-on">{{ pageNum }} / {{ pageCount }}</button>
        <button type="button" :disabled="pageNum >= pageCount" @click="pageNum += 1">下一页</button>
      </div>
    </footer>

    <div v-if="showDetail" class="y-modal-mask" @click.self="closeDetail">
      <div class="y-modal y-modal--wide" role="dialog" aria-label="检测明细">
        <header class="y-modal__head">
          <h3>检测 #{{ detail?.run?.id ?? '…' }}</h3>
          <button type="button" class="y-link" @click="closeDetail">关闭</button>
        </header>
        <div class="y-modal__body">
          <p v-if="detailLoading" class="y-msg">加载明细…</p>
          <p v-else-if="detailError" class="y-msg y-msg--error">{{ detailError }}</p>
          <template v-else-if="detail">
            <dl class="kv">
              <div>
                <dt>上游</dt>
                <dd>#{{ detail.run.upstream_id }} {{ detail.run.account_email || '' }}</dd>
              </div>
              <div>
                <dt>状态</dt>
                <dd>{{ detail.run.status }} / {{ detail.run.verdict || '—' }}</dd>
              </div>
              <div>
                <dt>触发</dt>
                <dd>{{ detail.run.trigger || '—' }}</dd>
              </div>
              <div>
                <dt>基线对照</dt>
                <dd>
                  <template v-if="detail.reference">
                    #{{ detail.reference.upstream_id }} · {{ detail.reference.passed ?? '—' }}/{{
                      detail.reference.items ?? '—'
                    }}
                  </template>
                  <template v-else>—</template>
                </dd>
              </div>
            </dl>
            <p v-if="!detailResults.length" class="y-msg">无逐题结果</p>
            <table v-else class="detail-table">
              <thead>
                <tr>
                  <th>套件</th>
                  <th>题/检查</th>
                  <th>模型</th>
                  <th>通过</th>
                  <th>耗时</th>
                  <th>摘要</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(res, i) in detailResults" :key="i">
                  <td>{{ res.suite }}</td>
                  <td>
                    <span v-if="res.item_id">#{{ res.item_id }}</span>
                    <span v-if="res.check_key"> {{ res.check_key }}</span>
                    <span v-if="res.instance_idx != null" class="sub">
                      · i{{ res.instance_idx }}/s{{ res.step ?? 0 }}
                    </span>
                  </td>
                  <td>{{ res.model || '—' }}</td>
                  <td>
                    <span
                      :class="
                        res.error_class
                          ? 'y-chip y-chip--err'
                          : res.passed
                            ? 'y-chip y-chip--ok'
                            : 'y-chip'
                      "
                    >
                      {{ res.error_class || (res.passed ? '通过' : '未通过') }}
                    </span>
                  </td>
                  <td>{{ res.duration_ms != null ? `${res.duration_ms}ms` : '—' }}</td>
                  <td class="excerpt">{{ res.excerpt || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </template>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.ref-tag {
  margin-left: 4px;
  font-size: 11px;
  color: #0369a1;
}
.sub {
  font-size: 12px;
  color: var(--color-text-secondary, #868686);
}
.suites {
  max-width: 14rem;
  font-size: 12px;
}
.kv {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  margin: 0 0 12px;
  font-size: 13px;
}
.kv dt {
  color: var(--color-text-secondary, #868686);
}
.kv dd {
  margin: 2px 0 0;
}
.detail-table {
  width: 100%;
  font-size: 12px;
}
.excerpt {
  max-width: 16rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  width: min(920px, 100%);
  max-height: min(92vh, 900px);
  overflow: auto;
  border-radius: 12px;
  background: var(--color-bg-elevated, #fff);
  box-shadow: 0 16px 48px rgb(0 0 0 / 18%);
}
.y-modal__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  position: sticky;
  top: 0;
  background: inherit;
  z-index: 1;
}
.y-modal__head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.y-modal__body {
  padding: 0 16px 16px;
}
</style>
