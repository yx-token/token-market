<script setup lang="ts">
/**
 * 产品内操作引导：贴在业务流程页，跟随真实步骤，而不是只读文案。
 *
 * 引导卡片固定在右下角，会盖住页面底部的按钮（发布向导的「下一步」就在面板右下角）。
 * 所以打开期间在页面末尾垫一段与卡片等高的留白，保证任何内容都能滚到卡片上方；
 * 切换步骤时把高亮区域滚到卡片上方。滚动时只重算高亮框，不再反向滚动页面。
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

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
  /** 点主按钮后收起成小胶囊，把页面让给用户操作；切换步骤时自动展开 */
  collapseOnPrimary?: boolean
}>()

const emit = defineEmits<{
  close: []
  primary: []
  secondary: []
}>()

const GAP = 12
const SAFE_TOP = 16

const coachEl = ref<HTMLElement | null>(null)
const collapsed = ref(false)
const spot = ref<{ top: number; left: number; width: number; height: number } | null>(null)
/** 页面末尾需要垫出的留白高度（卡片高度 + 间距） */
const reserve = ref(0)

const progress = computed(() => `${Math.min(props.stepIndex + 1, props.stepTotal)} / ${props.stepTotal}`)

function target(): HTMLElement | null {
  if (!props.spotlight) return null
  return document.querySelector(props.spotlight) as HTMLElement | null
}

function measure() {
  if (!props.open) {
    spot.value = null
    return
  }
  const coach = coachEl.value
  // 卡片叠在右下角「找官方」按钮上方，留白按卡片顶边到视口底部的距离算
  reserve.value = coach ? Math.max(0, Math.ceil(window.innerHeight - coach.getBoundingClientRect().top)) + GAP : 0
  const el = target()
  if (!el) {
    spot.value = null
    return
  }
  const r = el.getBoundingClientRect()
  // 只框出视口内可见的部分，避免元素超出视口时框的位置和高度对不上
  const top = Math.max(8, r.top - 6)
  const bottom = Math.min(window.innerHeight - 8, r.bottom + 6)
  const left = Math.max(8, r.left - 6)
  const right = Math.min(window.innerWidth - 8, r.right + 6)
  spot.value = bottom > top && right > left ? { top, left, width: right - left, height: bottom - top } : null
}

/** 把高亮区域滚到卡片上方：放得下就整个露出来，放不下就顶部对齐，剩下的由用户往下滚。 */
function reveal() {
  const el = target()
  if (!el) return
  const r = el.getBoundingClientRect()
  let safeBottom = window.innerHeight - GAP
  const c = coachEl.value?.getBoundingClientRect()
  if (c && c.left < r.right && c.right > r.left) safeBottom = Math.min(safeBottom, c.top - GAP)
  const room = safeBottom - SAFE_TOP
  let delta = 0
  if (r.height > room || r.top < SAFE_TOP) delta = r.top - SAFE_TOP
  else if (r.bottom > safeBottom) delta = r.bottom - safeBottom
  if (Math.abs(delta) < 1) return
  const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  window.scrollBy({ top: delta, behavior: reduce ? 'auto' : 'smooth' })
}

let raf = 0
function schedule() {
  if (raf) return
  raf = requestAnimationFrame(() => {
    raf = 0
    measure()
  })
}

// 内容异步加载（厂商列表、Key 列表）会改变目标尺寸和位置，跟着重算
let observer: ResizeObserver | null = null
let observed: HTMLElement | null = null
function observe() {
  if (typeof ResizeObserver === 'undefined') return
  if (!observer) {
    observer = new ResizeObserver(schedule)
    observer.observe(document.body)
  }
  const el = target()
  if (el === observed) return
  if (observed) observer.unobserve(observed)
  observed = el
  if (el) observer.observe(el)
  if (coachEl.value) observer.observe(coachEl.value)
}

function disconnect() {
  observer?.disconnect()
  observer = null
  observed = null
}

watch(
  () => [props.open, props.spotlight, props.stepIndex] as const,
  async ([open], prev) => {
    if (!open) {
      disconnect()
      spot.value = null
      return
    }
    if (!prev || prev[2] !== props.stepIndex || prev[1] !== props.spotlight) collapsed.value = false
    await nextTick()
    // 面板切换后 DOM 可能晚一拍
    requestAnimationFrame(() => {
      observe()
      measure()
      reveal()
    })
    setTimeout(() => {
      observe()
      measure()
    }, 120)
  },
  { immediate: true },
)

watch(collapsed, async () => {
  await nextTick()
  if (observer && coachEl.value) observer.observe(coachEl.value)
  measure()
})

function onPrimary() {
  emit('primary')
  if (props.collapseOnPrimary) {
    collapsed.value = true
    nextTick(() => {
      measure()
      reveal()
    })
  }
}

function expand() {
  collapsed.value = false
}

onMounted(() => {
  window.addEventListener('resize', schedule)
  window.addEventListener('scroll', schedule, true)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', schedule)
  window.removeEventListener('scroll', schedule, true)
  if (raf) cancelAnimationFrame(raf)
  disconnect()
})
</script>

<template>
  <Teleport to="body">
    <!-- 页面末尾的留白：保证卡片盖住的底部按钮也能滚上来 -->
    <div v-if="open" class="pg-reserve" :style="{ height: `${reserve}px` }" aria-hidden="true" data-testid="guide-reserve" />
    <div v-if="open" class="pg-root" aria-live="polite">
      <div v-if="spot" class="pg-mask" aria-hidden="true">
        <div
          class="pg-hole"
          :class="{ 'is-soft': collapsed }"
          :style="{
            top: `${spot.top}px`,
            left: `${spot.left}px`,
            width: `${spot.width}px`,
            height: `${spot.height}px`,
          }"
        />
      </div>
      <button
        v-if="collapsed"
        ref="coachEl"
        type="button"
        class="pg-pill"
        :aria-label="`展开操作引导：${title}`"
        data-testid="guide-pill"
        @click="expand"
      >
        <span class="pg-progress">{{ progress }}</span>
        <span class="pg-pill-title">{{ title }}</span>
        <span class="pg-pill-more" aria-hidden="true">展开</span>
      </button>
      <aside v-else ref="coachEl" class="pg-coach" role="dialog" aria-label="操作引导">
        <div class="pg-coach-top">
          <span class="pg-progress">{{ progress }}</span>
          <button type="button" class="pg-close" aria-label="结束引导" @click="emit('close')">结束</button>
        </div>
        <strong class="pg-title">{{ title }}</strong>
        <p class="pg-body">{{ body }}</p>
        <div class="pg-actions">
          <button v-if="secondaryLabel" type="button" class="pg-btn ghost" @click="emit('secondary')">{{ secondaryLabel }}</button>
          <button v-if="primaryLabel" type="button" class="pg-btn primary" @click="onPrimary">{{ primaryLabel }}</button>
        </div>
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.pg-reserve {
  width: 1px;
  pointer-events: none;
}
.pg-root {
  /* 右下角「找官方」按钮约 44px 高、距底 20px，引导卡片叠在它上方，不挡客服入口 */
  --pg-bottom: calc(max(20px, env(safe-area-inset-bottom)) + 56px);
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
  transition: top 0.2s, left 0.2s, width 0.2s, height 0.2s, box-shadow 0.2s;
}
.pg-hole.is-soft {
  box-shadow: 0 0 0 4px rgba(255, 230, 15, 0.28);
}
.pg-coach {
  pointer-events: auto;
  position: fixed;
  right: max(16px, env(safe-area-inset-right));
  bottom: var(--pg-bottom);
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
.pg-pill {
  pointer-events: auto;
  position: fixed;
  right: max(16px, env(safe-area-inset-right));
  bottom: var(--pg-bottom);
  max-width: min(320px, calc(100vw - 24px));
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 8px 7px 8px;
  border-radius: 999px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: #fff;
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.16);
  font: inherit;
  cursor: pointer;
}
.pg-pill:focus-visible,
.pg-btn:focus-visible,
.pg-close:focus-visible {
  outline: 2px solid #1f1f1f;
  outline-offset: 2px;
}
.pg-pill-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  font-weight: 800;
  color: #1f1f1f;
}
.pg-pill-more {
  flex-shrink: 0;
  border-radius: 999px;
  background: #ffe60f;
  padding: 3px 9px;
  font-size: 11px;
  font-weight: 800;
  color: #1f1f1f;
}
.pg-coach-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.pg-progress {
  flex-shrink: 0;
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
  }
}
@media (prefers-reduced-motion: reduce) {
  .pg-hole {
    transition: none;
  }
}
</style>
