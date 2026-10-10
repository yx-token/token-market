<script setup lang="ts" generic="Row extends Record<string, any>">
import { useIsConsole } from '@/composables/useIsConsole'

export interface Column {
  key: string
  label: string
  align?: 'left' | 'right'
}

defineProps<{
  columns: Column[]
  rows: Row[] | null | undefined
  rowKey?: string
  loading?: boolean
  empty?: string
}>()

const isConsole = useIsConsole()
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
          <tr v-for="(row, i) in rows" :key="rowKey ? row[rowKey] : i">
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

  <!-- 市场 / 买卖中心：闲猫圆角表 -->
  <div v-else class="xm-table-wrap overflow-x-auto">
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
          <tr v-for="(row, i) in rows" :key="rowKey ? row[rowKey] : i">
            <td v-for="c in columns" :key="c.key" :class="c.align === 'right' ? 'num text-right' : ''">
              <slot :name="`cell-${c.key}`" :row="row">{{ row[c.key] ?? '—' }}</slot>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
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
</style>
