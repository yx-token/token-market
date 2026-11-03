<script setup lang="ts">
/**
 * 产品内操作引导：贴在业务流程页，跟随真实步骤，而不是只读文案。
 */
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  open: boolean
  stepIndex: number
  stepTotal: number
  title: string
  body: string
  primaryLabel?: string
  secondaryLabel?: string
  /** 高亮目标选择器，如 [data-guide="edge"] */
  spotlight?: string
}>()

const emit = defineEmits<{
  close: []
  primary: []
  secondary: []
}>()

const spot = ref<{ top: number; left: number; width: number; height: number } | null>(null)

function measure() {
  if (!props.open || !props.spotlight) {
    spot.value = null
    return
  }
  const el = document.querySelector(props.spotlight) as HTMLElement | null
  if (!el) {
    spot.value = null
    return
  }
  const r = el.getBoundingClientRect()
  spot.value = {
    top: Math.max(8, r.top - 6),
    left: Math.max(8, r.left - 6),
    width: Math.min(window.innerWidth - 16, r.width + 12),
    height: Math.min(window.innerHeight - 16, r.height + 12),
  }
  el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
}

const progress = computed(() => `${Math.min(props.stepIndex + 1, props.stepTotal)} / ${props.stepTotal}`)

watch(
  () => [props.open, props.spotlight, props.stepIndex] as const,
  async () => {
    await nextTick()
    requestAnimationFrame(measure)
    // 面板切换后 DOM 可能晚一拍
    setTimeout(measure, 80)
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('resize', measure)
  window.addEventListener('scroll', measure, true)
})
onUnmounted(() => {
  window.removeEventListener('resize', measure)
  window.removeEventListener('scroll', measure, true)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="pg-root" aria-live="polite">
      <div v-if="spot" class="pg-mask" aria-hidden="true">
        <div
          class="pg-hole"
          :style="{
            top: `${spot.top}px`,
            left: `${spot.left}px`,
            width: `${spot.width}px`,
            height: `${spot.height}px`,
          }"
        />
      </div>
      <aside class="pg-coach" role="dialog" aria-label="操作引导">
        <div class="pg-coach-top">
          <span class="pg-progress">{{ progress }}</span>
          <button type="button" class="pg-close" aria-label="结束引导" @click="emit('close')">结束</button>
        </div>
        <strong class="pg-title">{{ title }}</strong>
        <p class="pg-body">{{ body }}</p>
        <div class="pg-actions">
          <button v-if="secondaryLabel" type="button" class="pg-btn ghost" @click="emit('secondary')">{{ secondaryLabel }}</button>
          <button v-if="primaryLabel" type="button" class="pg-btn primary" @click="emit('primary')">{{ primaryLabel }}</button>
        </div>
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.pg-root {
  position: fixed;
  inset: 0;
  z-index: 80;
  pointer-events: none;
}
.pg-mask {
  position: absolute;
  inset: 0;
  pointer-events: none;
}
.pg-hole {
  position: absolute;
  border-radius: 14px;
  box-shadow: 0 0 0 9999px rgba(20, 20, 20, 0.42);
  border: 2px solid #ffe60f;
  pointer-events: none;
  transition: top 0.2s, left 0.2s, width 0.2s, height 0.2s;
}
.pg-coach {
  pointer-events: auto;
  position: fixed;
  right: max(16px, env(safe-area-inset-right));
  bottom: max(16px, env(safe-area-inset-bottom));
  width: min(360px, calc(100vw - 24px));
  padding: 14px 16px 16px;
  border-radius: 16px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.18);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.pg-coach-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.pg-progress {
  font-size: 11px;
  font-weight: 800;
  color: #8a6a00;
  background: #fff3a8;
  border-radius: 999px;
  padding: 2px 8px;
}
.pg-close {
  border: none;
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  color: #888;
  cursor: pointer;
}
.pg-title {
  font-size: 15px;
  font-weight: 800;
  color: #1f1f1f;
}
.pg-body {
  margin: 0;
  font-size: 13px;
  line-height: 1.55;
  color: #555;
}
.pg-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 4px;
}
.pg-btn {
  border: none;
  border-radius: 999px;
  padding: 8px 14px;
  font: inherit;
  font-size: 12px;
  font-weight: 800;
  cursor: pointer;
}
.pg-btn.primary {
  background: #ffe60f;
  color: #1f1f1f;
}
.pg-btn.ghost {
  background: #f3f3f3;
  color: #333;
}
@media (max-width: 560px) {
  .pg-coach {
    left: 12px;
    right: 12px;
    width: auto;
    bottom: 12px;
  }
}
</style>
