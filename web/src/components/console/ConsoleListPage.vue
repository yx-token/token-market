<script setup lang="ts">
/**
 * 控制台标准列表页（对齐 YourHub TablePageLayout）
 * Header → [page tabs] → filter-panel → glass table → footer
 * Tab 在页面级切换整页（筛选 / 表体 / 底栏），不嵌在表格卡片内。
 */
import '@/assets/styles/console-table-page.css'

defineProps<{
  id: string
  title: string
  description?: string
}>()
</script>

<template>
  <div :id="id" class="y-page table-page-layout">
    <div class="y-header" :class="{ 'has-page-tabs': !!$slots.tabs }">
      <div class="min-w-0">
        <h1 class="y-title">{{ title }}</h1>
        <p v-if="description" class="y-desc">{{ description }}</p>
      </div>
      <div v-if="$slots.actions" class="y-header-actions">
        <slot name="actions" />
      </div>
    </div>

    <div v-if="$slots.tabs" class="y-page-tabs">
      <slot name="tabs" />
    </div>

    <div class="y-container--tight">
      <div v-if="$slots.filters" class="filter-panel">
        <div class="filter-grid">
          <slot name="filters" />
        </div>
      </div>
      <div class="table-scroll-container">
        <slot />
      </div>
      <div v-if="$slots.footer" class="y-footer">
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>
