<script setup lang="ts" generic="Row extends Record<string, any>">
// 通用表格（账本样式）：columns 描述列，单元格可用插槽 #cell-<key>="{ row }" 自定义；右对齐列使用等宽数字。
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
  <div class="-mx-1 overflow-x-auto px-1">
    <table class="table">
      <thead>
        <tr>
          <th v-for="c in columns" :key="c.key" :class="c.align === 'right' ? '!text-right' : ''">{{ c.label }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="loading">
          <td :colspan="columns.length" class="!py-8 text-center">
            <span class="inline-flex items-center gap-1.5 font-mono text-xs text-ink-400">
              <span v-for="i in 3" :key="i" class="h-1 w-1 animate-blink bg-ink-400" :style="{ animationDelay: `${i * 160}ms` }" />
              载入中
            </span>
          </td>
        </tr>
        <tr v-else-if="!rows || rows.length === 0">
          <td :colspan="columns.length" class="!py-8 text-center font-mono text-xs tracking-wide text-ink-400">— {{ empty ?? '暂无数据' }} —</td>
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
