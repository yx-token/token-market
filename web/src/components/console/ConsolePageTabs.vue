<script setup lang="ts">
/**
 * 对齐 YourHub AppTabBar：液态玻璃条 + 选中浅蓝底 + 底边指示线。
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { NavNode } from './navTypes'
import { useConsoleTabs, HOME_TAB } from '@/stores/consoleTabs'

const props = defineProps<{ navItems: NavNode[] }>()
const route = useRoute()
const router = useRouter()
const {
  items: openTabs,
  open,
  close,
  closeAll,
  closeOthers,
  closeToRight,
  closeToLeft,
  isHome,
} = useConsoleTabs()

const scrollRef = ref<HTMLElement | null>(null)
const menuVisible = ref(false)
const menuX = ref(0)
const menuY = ref(0)
const menuTarget = ref<string | null>(null)

function walk(nodes: NavNode[], acc: { path: string; title: string }[] = []) {
  for (const n of nodes) {
    if (n.to) acc.push({ path: n.to, title: n.text })
    if (n.children?.length) walk(n.children, acc)
  }
  return acc
}

const titleMap = computed(() => {
  const m = new Map<string, string>()
  for (const t of walk(props.navItems)) m.set(t.path, t.title)
  return m
})

function titleFor(path: string) {
  if (titleMap.value.has(path)) return titleMap.value.get(path)!
  let best = ''
  let title = path
  for (const [p, t] of titleMap.value) {
    if (path === p || path.startsWith(`${p}/`)) {
      if (p.length > best.length) {
        best = p
        title = t
      }
    }
  }
  return title
}

function normalize(path: string) {
  if (path.length > 1 && path.endsWith('/')) return path.slice(0, -1)
  return path
}

const currentPath = computed(() => normalize(route.path))

watch(
  () => route.path,
  (path) => {
    if (!path.startsWith('/console')) return
    const p = normalize(path)
    open(p, titleFor(p))
  },
  { immediate: true },
)

function activate(path: string) {
  if (currentPath.value === path) return
  router.push(path)
}

function closeTab(path: string) {
  if (isHome(path)) return
  const next = close(path)
  if (currentPath.value === path) router.push(next || HOME_TAB.path)
}

function handleWheel(e: WheelEvent) {
  if (scrollRef.value) scrollRef.value.scrollLeft += e.deltaY
}

function showMenu(e: MouseEvent, path: string) {
  menuTarget.value = path
  menuX.value = e.clientX
  menuY.value = e.clientY
  menuVisible.value = true
}

function hideMenu() {
  menuVisible.value = false
  menuTarget.value = null
}

function go(path: string) {
  if (currentPath.value !== path) router.push(path)
}

function handleCloseCurrent() {
  if (menuTarget.value) closeTab(menuTarget.value)
  hideMenu()
}

function handleCloseOthers() {
  if (!menuTarget.value) return
  go(closeOthers(menuTarget.value))
  hideMenu()
}

function handleCloseRight() {
  if (!menuTarget.value) return
  const p = closeToRight(menuTarget.value)
  if (p) go(p)
  hideMenu()
}

function handleCloseLeft() {
  if (!menuTarget.value) return
  const p = closeToLeft(menuTarget.value)
  if (p) go(p)
  hideMenu()
}

function handleCloseAll() {
  go(closeAll())
  hideMenu()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') hideMenu()
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

const canCloseRight = computed(() => {
  if (!menuTarget.value) return false
  const i = openTabs.value.findIndex((t) => t.path === menuTarget.value)
  return i >= 0 && i < openTabs.value.length - 1
})

const canCloseLeft = computed(() => {
  if (!menuTarget.value) return false
  const i = openTabs.value.findIndex((t) => t.path === menuTarget.value)
  // 首页占 index 0，其左侧无可关；i>1 表示左侧还有非首页
  return i > 1
})
</script>

<template>
  <div class="app-tab-bar">
    <div ref="scrollRef" class="tab-scroll" @wheel.prevent="handleWheel">
      <div
        v-for="t in openTabs"
        :key="t.path"
        class="tab-item"
        :class="{ active: currentPath === t.path }"
        @click="activate(t.path)"
        @contextmenu.prevent="showMenu($event, t.path)"
        @click.middle.prevent="closeTab(t.path)"
      >
        <span class="tab-title">{{ t.title }}</span>
        <button
          v-if="!isHome(t.path)"
          type="button"
          class="tab-close"
          aria-label="关闭"
          @click.stop="closeTab(t.path)"
        >
          &times;
        </button>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="menuVisible"
        class="tab-context-menu"
        :style="{ left: `${menuX}px`, top: `${menuY}px` }"
        @click.stop
      >
        <div
          v-if="menuTarget && !isHome(menuTarget)"
          class="menu-item"
          @click="handleCloseCurrent"
        >
          关闭当前
        </div>
        <div class="menu-item" @click="handleCloseOthers">关闭其他</div>
        <div
          class="menu-item"
          :class="{ disabled: !canCloseRight }"
          @click="canCloseRight && handleCloseRight()"
        >
          关闭右侧
        </div>
        <div
          class="menu-item"
          :class="{ disabled: !canCloseLeft }"
          @click="canCloseLeft && handleCloseLeft()"
        >
          关闭左侧
        </div>
        <div class="menu-separator" />
        <div class="menu-item" @click="handleCloseAll">全部关闭</div>
      </div>
      <div v-if="menuVisible" class="tab-menu-overlay" @click="hideMenu" />
    </Teleport>
  </div>
</template>

<style scoped>
/* 直接对齐 YourHub AppTabBar.vue */
.app-tab-bar {
  height: 38px;
  width: 100%;
  /* 背景由 .console-top 统一提供，避免双层色差 */
  background: transparent;
  border-bottom: none;
  box-shadow: none;
  display: flex;
  align-items: center;
  overflow: hidden;
  user-select: none;
}
.tab-scroll {
  display: flex;
  align-items: stretch;
  height: 100%;
  overflow-x: auto;
  overflow-y: hidden;
  flex: 1;
  scrollbar-width: none;
}
.tab-scroll::-webkit-scrollbar {
  display: none;
}
.tab-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 14px;
  height: 100%;
  font-size: 13px;
  color: #64748b;
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
  transition: color 0.2s, background 0.2s;
}
.tab-item:hover {
  background: rgba(5, 85, 206, 0.06);
  color: #0446a7;
}
/*
 * 选中页签不再套一层 backdrop-filter（顶栏已是毛玻璃），指示线也不做无限流动动画：
 * 它在 sticky 毛玻璃顶栏里逐帧重绘，Chrome 会跟着反复重算整片模糊，控制台各页都会一闪一闪。
 */
.tab-item.active {
  color: #0555ce;
  background: rgba(230, 238, 250, 0.85);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.06);
  border-radius: 6px 6px 0 0;
  margin: 0 2px;
  padding: 0 12px;
}
.tab-item.active::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 10%;
  right: 10%;
  height: 2px;
  background: linear-gradient(90deg, transparent, #0555ce, #3878e6, #0555ce, transparent);
  border-radius: 1px;
}
.tab-title {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tab-close {
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  font-size: 14px;
  line-height: 1;
  color: #94a3b8;
  cursor: pointer;
  border: none;
  background: transparent;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
  padding: 0;
}
.tab-close:hover {
  background: rgba(100, 116, 139, 0.15);
  color: #475569;
}
.tab-context-menu {
  position: fixed;
  z-index: 9999;
  background: rgba(255, 255, 255, 0.72);
  backdrop-filter: blur(32px) saturate(1.8);
  -webkit-backdrop-filter: blur(32px) saturate(1.8);
  border: 1px solid rgba(255, 255, 255, 0.45);
  border-radius: 10px;
  box-shadow:
    0 4px 16px rgba(0, 0, 0, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.25);
  padding: 6px 0;
  min-width: 130px;
}
.menu-item {
  padding: 7px 18px;
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
  color: #334155;
  transition: background 0.12s;
}
.menu-item:hover {
  background: #e6eefa;
  color: #0555ce;
}
.menu-item.disabled {
  opacity: 0.4;
  cursor: not-allowed;
  pointer-events: none;
}
.menu-separator {
  height: 1px;
  background: rgba(0, 0, 0, 0.06);
  margin: 4px 0;
}
.tab-menu-overlay {
  position: fixed;
  inset: 0;
  z-index: 9998;
}
</style>
