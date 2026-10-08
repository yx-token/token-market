<script setup lang="ts" generic="Row extends Record<string, any>">
// 通用表格：columns 描述列，单元格可用插槽 #cell-<key>="{ row }" 自定义。
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
</script>

<template>
  <div class="overflow-x-auto">
    <table class="table">
      <thead>
        <tr>
          <th v-for="c in columns" :key="c.key" :class="c.align === 'right' ? '!text-right' : ''">{{ c.label }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-slate-100">
        <tr v-if="loading">
          <td :colspan="columns.length" class="py-6 text-center text-slate-400">加载中…</td>
        </tr>
        <tr v-else-if="!rows || rows.length === 0">
          <td :colspan="columns.length" class="py-6 text-center text-slate-400">{{ empty ?? '暂无数据' }}</td>
        </tr>
        <template v-else>
          <tr v-for="(row, i) in rows" :key="rowKey ? row[rowKey] : i" class="hover:bg-slate-50">
            <td v-for="c in columns" :key="c.key" :class="c.align === 'right' ? 'text-right tabular-nums' : ''">
              <slot :name="`cell-${c.key}`" :row="row">{{ row[c.key] ?? '—' }}</slot>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
