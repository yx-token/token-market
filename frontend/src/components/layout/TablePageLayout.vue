<template>
  <div class="y-page table-page-layout" :class="{ 'mobile-mode': isMobile }">
    <!-- 头部：标题 + 操作 -->
    <div v-if="$slots.title || $slots.actions || $slots.header" class="y-header">
      <slot name="header">
        <slot name="title" />
      </slot>
      <div v-if="$slots.actions" class="y-header-actions">
        <slot name="actions" />
      </div>
    </div>

    <!-- 内容：筛选 + 表格 -->
    <div class="y-container--tight" :class="{ 'content-scroll-mode': props.contentMode === 'content' }">
      <div v-if="$slots.filters" class="layout-section-fixed">
        <div class="filter-panel">
          <slot name="filters" />
        </div>
      </div>

      <div v-if="hasTableContent" class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 底部分页 -->
    <div v-if="$slots.pagination" class="y-footer">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, useSlots } from 'vue'

interface Props {
  /**
   * 内容滚动模式：`table`（默认）保留单表页的内部表格滚动；
   * `content` 用于支付、检测站、质量、Org 等多区块页面，让整个内容区自行纵向滚动。
   */
  contentMode?: 'table' | 'content'
}

const props = withDefaults(defineProps<Props>(), {
  contentMode: 'table'
})

const slots = useSlots()
const hasTableContent = computed(() => Boolean(slots.table))

const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth < 1024
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})
</script>

<style scoped>
/* 对齐 YourHub：y-page 三段式 + filter-panel */
.table-page-layout {
  @apply flex flex-col;
  gap: 0;
  height: 100%;
  min-height: 0;
}

.y-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: var(--spacing-sm, 8px) 0;
  flex-shrink: 0;
}

.y-header:empty {
  display: none;
}

.y-header-actions {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 12px);
  margin-left: auto;
  flex-shrink: 0;
}

.y-header > :not(.y-header-actions) {
  flex: 1;
  min-width: 0;
}

.y-container--tight {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  padding-bottom: 0;
}

.y-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: var(--spacing-md, 12px) 0;
  flex-shrink: 0;
}

.layout-section-fixed {
  @apply flex-shrink-0;
}

.filter-panel {
  padding: 12px 16px;
  background: var(--color-bg-elevated, rgba(255, 255, 255, 0.96));
  border: 1px solid var(--color-border, rgba(15, 23, 42, 0.08));
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
  color: var(--color-text-primary, #262626);
}

.table-scroll-container {
  @apply flex flex-col overflow-hidden flex-1 min-h-0 bg-white dark:bg-dark-800 rounded-2xl border border-gray-200 dark:border-dark-700 shadow-sm;
}

.table-scroll-container :deep(.table-wrapper) {
  @apply flex-1 overflow-x-auto overflow-y-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  @apply bg-gray-50/80 dark:bg-dark-800/80 backdrop-blur-sm;
}

.table-scroll-container :deep(tbody) {
  /* 保持默认 table-row-group 显示，不使用 block */
}

.table-scroll-container :deep(th) {
  @apply px-5 py-4 text-left text-sm font-medium text-gray-600 dark:text-dark-300 border-b border-gray-200 dark:border-dark-700;
}

.table-scroll-container :deep(td) {
  @apply px-5 py-4 text-sm text-gray-700 dark:text-gray-300 border-b border-gray-100 dark:border-dark-800;
}

/* 多区块内容页：筛选 + 自定义内容 + 分页共用一条纵向滚动链 */
.table-page-layout .y-container--tight.content-scroll-mode {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.table-page-layout .y-container--tight.content-scroll-mode .table-scroll-container {
  flex: none;
  min-height: 0;
}

/* 移动端：恢复正常滚动 */
.table-page-layout.mobile-mode {
  height: auto;
  min-height: 100%;
  overflow: visible;
}

.table-page-layout.mobile-mode .table-scroll-container {
  @apply h-auto overflow-visible border-none shadow-none bg-transparent;
}

.table-page-layout.mobile-mode .y-container--tight {
  @apply flex-none min-h-fit overflow-visible;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply flex-none;
  display: table;
  min-width: 100%;
}
</style>
