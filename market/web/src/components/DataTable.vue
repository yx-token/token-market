<script setup lang="ts" generic="Row extends Record<string, any>">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useIsConsole } from '@/composables/useIsConsole'

export interface Column {
  key: string
  label: string
  align?: 'left' | 'right'
}

const props = defineProps<{
  columns: Column[]
  rows: Row[] | null | undefined
  rowKey?: string
  loading?: boolean
  empty?: string
}>()

const isConsole = useIsConsole()

/** 只挂载一种布局，避免表格+卡片双份 DOM 拖垮测试与无障碍树 */
const isH5 = ref(false)
let mq: MediaQueryList | null = null
function syncH5() {
  isH5.value = !!mq?.matches
}
onMounted(() => {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
  mq = window.matchMedia('(max-width: 720px)')
  syncH5()
  mq.addEventListener('change', syncH5)
})
onUnmounted(() => {
  mq?.removeEventListener('change', syncH5)
  mq = null
})

/** H5 卡片：首列作标题，其余 label/value，操作列沉底 */
const titleCol = computed(() => props.columns.find((c) => c.key !== 'actions') ?? null)
const bodyCols = computed(() =>
  props.columns.filter((c) => c.key !== 'actions' && c.key !== titleCol.value?.key),
)
const actionCol = computed(() => props.columns.find((c) => c.key === 'actions') ?? null)

function rowId(row: Row, i: number) {
  return props.rowKey ? row[props.rowKey] : i
}
</script>

<template>
  <!-- 控制台：对齐 YourHub DataTable（sticky 表头 + 规范字号） -->
  <div v-if="isConsole" class="table-wrapper">
    <table class="console-data-table">
      <thead class="table-header">
        <tr>
          <th
            v-for="c in columns"
            :key="c.key"
            scope="col"
            :class="[
              'sticky-header-cell',
              c.align === 'right' ? 'text-right' : 'text-left',
              c.key === 'actions' && 'sticky-col sticky-col-right',
            ]"
          >
            {{ c.label }}
          </th>
        </tr>
      </thead>
      <tbody class="table-body">
        <tr v-if="loading">
          <td :colspan="columns.length" class="console-empty-cell">
            <span class="inline-flex items-center gap-1.5 text-xs text-gray-400">
              <span
                v-for="i in 3"
                :key="i"
                class="h-1.5 w-1.5 animate-pulse rounded-full bg-[#0555ce]/50"
                :style="{ animationDelay: `${i * 160}ms` }"
              />
              载入中
            </span>
          </td>
        </tr>
        <tr v-else-if="!rows || rows.length === 0">
          <td :colspan="columns.length" class="console-empty-cell">
            {{ empty ?? '暂无数据' }}
          </td>
        </tr>
        <template v-else>
          <tr v-for="(row, i) in rows" :key="rowId(row, i)">
            <td
              v-for="c in columns"
              :key="c.key"
              :class="[
                c.align === 'right' && 'text-right tabular-nums',
                c.key === 'actions' && 'sticky-col sticky-col-right',
              ]"
            >
              <slot :name="`cell-${c.key}`" :row="row">{{ row[c.key] ?? '—' }}</slot>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>

  <!-- 市场 / 买卖中心：桌面表格；H5 卡片（按媒体查询二选一挂载） -->
  <div v-else class="xm-data">
    <div v-if="!isH5" class="xm-table-wrap xm-data-table overflow-x-auto">
      <table class="xm-table">
        <thead>
          <tr>
            <th v-for="c in columns" :key="c.key" :class="c.align === 'right' ? 'text-right' : ''">{{ c.label }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td :colspan="columns.length" class="py-8 text-center text-[#888]">载入中…</td>
          </tr>
          <tr v-else-if="!rows || rows.length === 0">
            <td :colspan="columns.length" class="py-8 text-center text-[#888]">{{ empty ?? '暂无数据' }}</td>
          </tr>
          <template v-else>
            <tr v-for="(row, i) in rows" :key="rowId(row, i)">
              <td v-for="c in columns" :key="c.key" :class="c.align === 'right' ? 'num text-right' : ''">
                <slot :name="`cell-${c.key}`" :row="row">{{ row[c.key] ?? '—' }}</slot>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <div v-else class="xm-data-cards" aria-live="polite">
      <div v-if="loading" class="xm-data-empty">载入中…</div>
      <div v-else-if="!rows || rows.length === 0" class="xm-data-empty">{{ empty ?? '暂无数据' }}</div>
      <template v-else>
        <article v-for="(row, i) in rows" :key="rowId(row, i)" class="xm-data-card">
          <header v-if="titleCol" class="xm-data-card__head">
            <span v-if="titleCol.label" class="xm-data-card__kicker">{{ titleCol.label }}</span>
            <div class="xm-data-card__title" :class="titleCol.align === 'right' ? 'is-right' : ''">
              <slot :name="`cell-${titleCol.key}`" :row="row">{{ row[titleCol.key] ?? '—' }}</slot>
            </div>
          </header>
          <dl v-if="bodyCols.length" class="xm-data-card__dl">
            <div
              v-for="c in bodyCols"
              :key="c.key"
              class="xm-data-card__row"
              :class="c.align === 'right' ? 'is-right' : ''"
            >
              <dt>{{ c.label || c.key }}</dt>
              <dd>
                <slot :name="`cell-${c.key}`" :row="row">{{ row[c.key] ?? '—' }}</slot>
              </dd>
            </div>
          </dl>
          <footer v-if="actionCol" class="xm-data-card__actions">
            <slot :name="`cell-${actionCol.key}`" :row="row">{{ row[actionCol.key] ?? '' }}</slot>
          </footer>
        </article>
      </template>
    </div>
  </div>
</template>

<style scoped>
.table-wrapper {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow-x: auto;
  overflow-y: auto;
  isolation: isolate;
  scrollbar-gutter: stable;
}

.console-data-table {
  width: 100%;
  min-width: max-content;
  border-collapse: separate;
  border-spacing: 0;
}

.table-header {
  position: sticky;
  top: 0;
  z-index: 200;
  background-color: rgb(249 250 251);
}

.sticky-header-cell {
  position: sticky;
  top: 0;
  z-index: 210;
  padding: 0.75rem 1rem;
  font-size: 0.75rem;
  font-weight: 500;
  line-height: 1.25rem;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: #6b7280;
  background-color: rgb(249 250 251);
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  white-space: nowrap;
}

.table-body {
  position: relative;
  z-index: 0;
}

.table-body td {
  padding: 0.625rem 1rem;
  font-size: 0.75rem;
  line-height: 1.25rem;
  color: #374151;
  background-color: #fff;
  border-bottom: 1px solid rgba(15, 23, 42, 0.05);
  white-space: nowrap;
  vertical-align: middle;
}

.table-body tr:hover td {
  background-color: rgba(5, 85, 206, 0.03);
}

.table-body tr:last-child td {
  border-bottom: none;
}

.console-empty-cell {
  padding: 2.5rem 1rem !important;
  text-align: center;
  color: #9ca3af;
  white-space: normal !important;
}

.sticky-col {
  position: sticky;
  z-index: 20;
}

.sticky-col-right {
  right: 0;
}

.sticky-header-cell.sticky-col {
  z-index: 220;
}

.table-body .sticky-col {
  background-color: #fff;
}

.table-body tr:hover .sticky-col {
  background-color: #f8fafc;
}

/* ---- 市场 H5 卡片 ---- */
.xm-data-cards {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.xm-data-empty {
  padding: 28px 16px;
  text-align: center;
  font-size: 13px;
  color: #888;
  background: #fafafa;
  border: 1px dashed rgba(0, 0, 0, 0.1);
  border-radius: 14px;
}

.xm-data-card {
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 14px;
  padding: 14px 14px 12px;
}

.xm-data-card__head {
  margin-bottom: 10px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
}

.xm-data-card__kicker {
  display: block;
  margin-bottom: 4px;
  font-size: 11px;
  font-weight: 700;
  color: #999;
}

.xm-data-card__title {
  font-size: 15px;
  font-weight: 800;
  color: #1f1f1f;
  line-height: 1.4;
  word-break: break-word;
}

.xm-data-card__title.is-right {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.xm-data-card__dl {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.xm-data-card__row {
  display: grid;
  grid-template-columns: minmax(72px, 34%) minmax(0, 1fr);
  gap: 8px 12px;
  align-items: start;
}

.xm-data-card__row dt {
  margin: 0;
  font-size: 12px;
  font-weight: 700;
  color: #888;
  line-height: 1.45;
}

.xm-data-card__row dd {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: #1f1f1f;
  line-height: 1.45;
  word-break: break-word;
  min-width: 0;
}

.xm-data-card__row.is-right dd {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.xm-data-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
}

/* 无按钮时不占位（slot 条件渲染为空） */
.xm-data-card__actions:not(:has(button, a, .xm-btn, .btn-ghost, .btn-primary, .btn-secondary)) {
  display: none;
}

.xm-data-card__actions :deep(.xm-btn),
.xm-data-card__actions :deep(.btn-ghost),
.xm-data-card__actions :deep(.btn-secondary),
.xm-data-card__actions :deep(.btn-primary),
.xm-data-card__actions :deep(button) {
  flex: 1 1 auto;
  min-height: 36px;
  justify-content: center;
}
</style>
