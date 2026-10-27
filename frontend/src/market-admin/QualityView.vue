<script setup lang="ts">
/**
 * 质量概览：TablePageLayout + DataTable。
 */
import { computed, onMounted, reactive, ref, toRefs, watch } from 'vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { MARKET_API_PREFIX, marketGet } from './api'

defineOptions({ name: 'MarketAdminQuality' })

const overview = ref<Record<string, unknown> | null>(null)
const loading = ref(false)
const error = ref('')

const initQueryParams = { keyword: '', platform: '', health: '' }
const data = reactive({ queryParams: { ...initQueryParams } })
const { queryParams } = toRefs(data)

const healthRows = computed(() =>
  Array.isArray(overview.value?.upstream_health)
    ? (overview.value?.upstream_health as Record<string, unknown>[])
    : [],
)
const appealRows = computed(() =>
  Array.isArray(overview.value?.appeals_by_status)
    ? (overview.value?.appeals_by_status as Record<string, unknown>[])
    : [],
)

const platformOptions = computed(() => {
  const set = new Set<string>()
  for (const r of healthRows.value) {
    const p = String(r.platform ?? '').trim()
    if (p) set.add(p)
  }
  return [{ value: '', label: '全部' }, ...[...set].sort().map((v) => ({ value: v, label: v }))]
})

const healthOptions = computed(() => {
  const set = new Set<string>()
  for (const r of healthRows.value) {
    const h = String(r.health ?? '').trim()
    if (h) set.add(h)
  }
  return [{ value: '', label: '全部' }, ...[...set].sort().map((v) => ({ value: v, label: v }))]
})

const filteredHealth = computed(() => {
  let rows = healthRows.value
  if (queryParams.value.platform) rows = rows.filter((r) => String(r.platform) === queryParams.value.platform)
  if (queryParams.value.health) rows = rows.filter((r) => String(r.health) === queryParams.value.health)
  const kw = queryParams.value.keyword.trim().toLowerCase()
  if (kw) {
    rows = rows.filter((r) =>
      [r.platform, r.health, r.grade, r.n].map((v) => String(v ?? '').toLowerCase()).some((s) => s.includes(kw)),
    )
  }
  return rows
})

const healthColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'platform', label: '平台' },
  { key: 'health', label: '健康' },
  { key: 'grade', label: '等级' },
  { key: 'n', label: '数量' },
]

const appealColumns: Column[] = [
  { key: 'serial', label: '序号', class: 'w-16' },
  { key: 'status', label: '状态' },
  { key: 'n', label: '数量' },
]

const totalHealth = computed(() =>
  filteredHealth.value.reduce((sum, r) => sum + Number(r.n || 0), 0),
)
const totalAppeals = computed(() =>
  appealRows.value.reduce((sum, r) => sum + Number(r.n || 0), 0),
)

watch(
  () => [queryParams.value.keyword, queryParams.value.platform, queryParams.value.health],
  () => {},
)

function handleQuery() {
  /* client-side filters via computed */
}

function resetQuery() {
  Object.assign(queryParams.value, { ...initQueryParams })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    overview.value = await marketGet<Record<string, unknown>>(`${MARKET_API_PREFIX}/quality/overview`)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
    overview.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <section id="market-quality-page" class="quality-shell">
    <TablePageLayout>
      <template #title>
        <div class="page-heading">
          <h2 class="y-title">质量概览</h2>
          <p class="y-page-desc">上游健康分布与申诉统计</p>
        </div>
      </template>
      <template #actions>
        <button type="button" class="btn btn-secondary page-action-btn" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新
        </button>
      </template>
      <template #filters>
        <p v-if="error" class="y-msg y-msg--error mb-2">{{ error }}</p>
        <div class="section-block">
          <div class="section-title">基本信息</div>
          <div class="stats-row">
            <article class="stat-card stat-card--blue">
              <div class="stat-card__label">健康分组</div>
              <div class="stat-card__value">{{ filteredHealth.length }}</div>
            </article>
            <article class="stat-card stat-card--green">
              <div class="stat-card__label">上游合计</div>
              <div class="stat-card__value">{{ totalHealth.toLocaleString() }}</div>
            </article>
            <article class="stat-card stat-card--orange">
              <div class="stat-card__label">申诉合计</div>
              <div class="stat-card__value">{{ totalAppeals.toLocaleString() }}</div>
            </article>
          </div>
        </div>
        <div class="section-block mt-3">
          <div class="section-title">健康筛选</div>
          <div class="filter-grid">
            <div class="filter-item">
              <label class="filter-label">关键词</label>
              <div class="relative flex-1 w-full min-w-0">
                <input
                  v-model="queryParams.keyword"
                  class="filter-input w-full"
                  type="search"
                  placeholder="平台 / 健康 / 等级"
                  @keyup.enter="handleQuery"
                />
              </div>
            </div>
            <div class="filter-item">
              <label class="filter-label">平台</label>
              <Select v-model="queryParams.platform" class="flex-1 w-full min-w-0" size="sm" :options="platformOptions" />
            </div>
            <div class="filter-item">
              <label class="filter-label">健康</label>
              <Select v-model="queryParams.health" class="flex-1 w-full min-w-0" size="sm" :options="healthOptions" />
            </div>
            <div class="filter-actions lg:col-span-4">
              <button type="button" class="btn btn-secondary" @click="resetQuery">
                <Icon name="refresh" size="sm" />
                重置
              </button>
              <button type="button" class="btn btn-primary" @click="handleQuery">
                <Icon name="search" size="sm" />
                搜索
              </button>
            </div>
          </div>
        </div>
      </template>
      <template #table>
        <div class="tables-stack">
          <div class="table-block">
            <div class="section-title">上游健康</div>
            <DataTable :columns="healthColumns" :data="filteredHealth" :loading="loading" :expandable-actions="false">
              <template #cell-serial="{ row }">
                {{ String(filteredHealth.findIndex((r) => r === row) + 1).padStart(2, '0') }}
              </template>
              <template #cell-platform="{ row }">{{ row.platform || '—' }}</template>
              <template #cell-health="{ row }">{{ row.health || '—' }}</template>
              <template #cell-grade="{ row }">{{ row.grade || '—' }}</template>
              <template #cell-n="{ row }">{{ row.n ?? '—' }}</template>
              <template #empty>
                <EmptyState title="暂无上游健康数据" description="接入上游并完成检测后会出现在这里" />
              </template>
            </DataTable>
          </div>
          <div class="table-block">
            <div class="section-title">申诉统计</div>
            <DataTable :columns="appealColumns" :data="appealRows" :loading="loading" :expandable-actions="false">
              <template #cell-serial="{ row }">
                {{ String(appealRows.findIndex((r) => r === row) + 1).padStart(2, '0') }}
              </template>
              <template #cell-status="{ row }">{{ row.status || '—' }}</template>
              <template #cell-n="{ row }">{{ row.n ?? '—' }}</template>
              <template #empty>
                <EmptyState title="暂无申诉" description="买家发起申诉后会汇总到这里" />
              </template>
            </DataTable>
          </div>
        </div>
      </template>
    </TablePageLayout>
  </section>
</template>

<style scoped>
#market-quality-page.quality-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
}
.page-heading { min-width: 0; }
.page-action-btn {
  height: 32px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.section-block { min-width: 0; }
.section-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
}
.stats-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.stat-card {
  padding: 14px 16px;
  border-radius: 12px;
  border: 1px solid rgba(15, 23, 42, 0.06);
  background: #f8fafc;
}
.stat-card--blue { background: linear-gradient(135deg, #eff6ff, #f8fafc); }
.stat-card--green { background: linear-gradient(135deg, #ecfdf5, #f8fafc); }
.stat-card--orange { background: linear-gradient(135deg, #fff7ed, #f8fafc); }
.stat-card__label { font-size: 12px; color: #64748b; }
.stat-card__value {
  margin-top: 4px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
}
.tables-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
}
.table-block {
  min-width: 0;
}
#market-quality-page :deep(.filter-grid .select-trigger),
#market-quality-page :deep(.filter-grid .select-trigger-sm),
#market-quality-page :deep(.filter-grid .filter-input) {
  height: 32px;
  min-height: 32px;
}
#market-quality-page :deep(.table-scroll-container table thead th) {
  background: #f5f7fa;
  color: #606266;
  font-weight: 500;
  font-size: 13px;
}
#market-quality-page :deep(.table-scroll-container table tbody td) {
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
}
@media (max-width: 900px) {
  .stats-row { grid-template-columns: 1fr; }
}
</style>
