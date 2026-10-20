<script setup lang="ts">
/**
 * 控制台标准列表页（对齐 YourHub TablePageLayout）
 * 标题在顶栏 AppHeader；页内只保留 actions / filters / table。
 */
import { onActivated, onUnmounted, watch } from 'vue'
import '@/assets/styles/console-table-page.css'
import { useConsolePage } from '@/stores/consolePage'

const props = withDefaults(
  defineProps<{
    id: string
    title: string
    description?: string
    /** 内容区不加外层白卡片（运营/分类树自带底时用） */
    plain?: boolean
  }>(),
  { plain: false },
)

const { setMeta, clearMeta } = useConsolePage()

function syncMeta() {
  setMeta(props.title, props.description || '')
}

watch(() => [props.title, props.description] as const, syncMeta, { immediate: true })
onActivated(syncMeta)
onUnmounted(clearMeta)
</script>

<template>
  <div :id="id" class="y-page table-page-layout">
    <div v-if="$slots.actions" class="y-header y-header--actions-only">
      <div class="y-header-actions">
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
      <div :class="plain ? 'content-scroll-plain' : 'table-scroll-container'">
        <slot />
      </div>
      <div v-if="$slots.footer" class="y-footer">
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>
