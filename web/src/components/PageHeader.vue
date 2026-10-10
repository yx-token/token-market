<script setup lang="ts">
// 控制台：标题上收到顶栏；市场/中心：闲猫简洁标题 + 可选返回
import { onActivated, onUnmounted, watch } from 'vue'
import { useIsConsole } from '@/composables/useIsConsole'
import { useConsolePage } from '@/stores/consolePage'

const props = defineProps<{
  title: string
  description?: string
  kicker?: string
  /** 返回上一级路径（买卖中心详情页用） */
  backTo?: string
  backLabel?: string
}>()
const isConsole = useIsConsole()
const { setMeta, clearMeta } = useConsolePage()

function syncConsoleMeta() {
  if (isConsole.value) setMeta(props.title, props.description || '')
}

watch(
  () => [isConsole.value, props.title, props.description] as const,
  () => syncConsoleMeta(),
  { immediate: true },
)
onActivated(syncConsoleMeta)
onUnmounted(() => {
  if (isConsole.value) clearMeta()
})
</script>

<template>
  <!-- 控制台：标题已在顶栏，这里只保留操作按钮 -->
  <div
    v-if="isConsole && (backTo || $slots.default)"
    class="mb-4 flex flex-wrap items-center justify-end gap-2"
  >
    <RouterLink
      v-if="backTo"
      :to="backTo"
      class="mr-auto inline-flex items-center gap-1 text-[13px] font-medium text-[var(--color-text-secondary,#868686)] hover:text-[var(--color-text-primary,#262626)]"
    >
      <span aria-hidden="true">←</span>
      {{ backLabel || '返回' }}
    </RouterLink>
    <slot />
  </div>
  <div v-else class="mb-6">
    <RouterLink
      v-if="backTo"
      :to="backTo"
      class="xm-page-back"
    >
      <span class="xm-page-back__ico" aria-hidden="true">←</span>
      {{ backLabel || '返回上一级' }}
    </RouterLink>
    <div class="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
      <div class="min-w-0">
        <p v-if="kicker" class="mb-1.5 text-[12px] font-bold text-[#888]">{{ kicker }}</p>
        <h1 class="text-[24px] font-extrabold leading-tight tracking-tight text-[#1f1f1f] md:text-[28px]">{{ title }}</h1>
        <p v-if="description" class="mt-1.5 max-w-3xl text-[14px] leading-relaxed text-[#666]">{{ description }}</p>
      </div>
      <div v-if="$slots.default" class="flex flex-wrap items-center gap-2"><slot /></div>
    </div>
  </div>
</template>
