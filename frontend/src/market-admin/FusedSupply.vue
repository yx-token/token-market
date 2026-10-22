<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marketGet } from './api'

const props = defineProps<{ kind: 'accounts' | 'groups' }>()
const { t } = useI18n()

const rows = ref<Record<string, unknown>[]>([])
const error = ref('')
const loading = ref(false)
const initQuery = { pageNum: 1, pageSize: 15, keyword: '' }
const query = ref({ ...initQuery })

const title = computed(() =>
  props.kind === 'accounts' ? t('nav.marketSupplyAccounts') : t('nav.marketSupplyGroups'),
)
const hint = computed(() =>
  props.kind === 'accounts' ? t('nav.marketSupplyAccountsHint') : t('nav.marketSupplyGroupsHint'),
)
const path = computed(() => (props.kind === 'accounts' ? '/api/admin/supply/accounts' : '/api/admin/supply/groups'))
const columns = computed(() =>
  props.kind === 'accounts'
    ? ['upstream_id', 'engine_account_id', 'engine_name', 'platform', 'market_status', 'engine_status', 'health', 'schedulable', 'rate_multiplier', 'linked']
    : ['pool_id', 'engine_group_id', 'name', 'engine_name', 'platform', 'market_status', 'enabled', 'sell_multiplier', 'engine_rate', 'linked']
)
const columnLabel: Record<string, string> = {
  upstream_id: '市场上游',
  engine_account_id: '引擎账号',
  engine_name: '引擎名称',
  platform: '厂商',
  market_status: '市场状态',
  engine_status: '引擎状态',
  health: '健康',
  schedulable: '可调度',
  rate_multiplier: '引擎倍率',
  linked: '已关联',
  pool_id: '保障池',
  engine_group_id: '引擎分组',
  name: '名称',
  enabled: '启用',
  sell_multiplier: '池售价倍率',
  engine_rate: '引擎倍率',
}

const filtered = computed(() => {
  const keyword = query.value.keyword.trim().toLowerCase()
  if (!keyword) return rows.value
  return rows.value.filter((row) => columns.value.some((col) => String(row[col] ?? '').toLowerCase().includes(keyword)))
})
const total = computed(() => filtered.value.length)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / query.value.pageSize)))
const pageRows = computed(() => {
  const start = (query.value.pageNum - 1) * query.value.pageSize
  return filtered.value.slice(start, start + query.value.pageSize)
})

function handleQuery() {
  query.value.pageNum = 1
}

function resetQuery() {
  query.value = { ...initQuery }
}

function serial(index: number) {
  return (query.value.pageNum - 1) * query.value.pageSize + index + 1
}

function cell(v: unknown) {
  if (v == null || v === '') return '—'
  if (typeof v === 'boolean') return v ? t('common.yes') : t('common.no')
  return String(v)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await marketGet<unknown>(path.value)
    rows.value = Array.isArray(data) ? (data as Record<string, unknown>[]) : []
  } catch (e) {
    error.value = e instanceof Error ? e.message : t('common.loadFailed')
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => props.kind, load)
</script>

<template>
  <section id="market-supply-page" class="y-page">
    <header class="y-header">
      <div>
        <h2 class="y-title">{{ title }}</h2>
        <p class="y-page-desc">{{ hint }}</p>
      </div>
      <button type="button" class="y-btn" :disabled="loading" @click="load">{{ t('nav.refresh') }}</button>
    </header>
    <div class="y-container--tight">
      <div class="y-search">
        <input v-model="query.keyword" type="search" :placeholder="t('nav.searchCurrentColumns')" @keyup.enter="handleQuery" />
        <button type="button" class="y-btn" @click="handleQuery">{{ t('nav.query') }}</button>
        <button type="button" class="y-btn y-btn--ghost" @click="resetQuery">{{ t('nav.reset') }}</button>
      </div>
      <p v-if="error" class="y-msg y-msg--error">{{ error }}</p>
      <p v-else-if="loading" class="y-msg">{{ t('nav.loading') }}</p>
      <p v-else-if="!filtered.length" class="y-msg">{{ t('nav.noData') }}</p>
      <table v-else>
        <thead>
          <tr>
            <th>{{ t('nav.serialNo') }}</th>
            <th v-for="col in columns" :key="col">{{ columnLabel[col] || col }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in pageRows" :key="i">
            <td>{{ serial(i) }}</td>
            <td v-for="col in columns" :key="col">{{ cell(row[col]) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <footer v-if="total > 0" class="y-footer">
      <div class="y-pager">
        <span>共 {{ total }} 条</span>
        <button type="button" :disabled="query.pageNum <= 1" @click="query.pageNum -= 1">上一页</button>
        <button type="button" class="is-on">{{ query.pageNum }} / {{ pageCount }}</button>
        <button type="button" :disabled="query.pageNum >= pageCount" @click="query.pageNum += 1">下一页</button>
      </div>
    </footer>
  </section>
</template>
