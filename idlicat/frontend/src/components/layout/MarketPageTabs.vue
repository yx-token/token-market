<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { HOME_TAB, HOME_TAB_TITLE_KEY, useConsoleTabs } from '@/stores/consoleTabs'
import { resolveRouteMetaKeys } from '@/router/title'

const HEIGHT_KEY = 'console.tabs.height'
const MIN_H = 28
const MAX_H = 72
const DEFAULT_H = 38

const route = useRoute()
const router = useRouter()
const { t, te, locale } = useI18n()
const { items: openTabs, open, close, closeAll, closeOthers, closeToRight, closeToLeft, isHome } = useConsoleTabs()

const tabHeight = ref(DEFAULT_H)
const menuVisible = ref(false)
const menuX = ref(0)
const menuY = ref(0)
const menuTarget = ref<string | null>(null)

function metaFor(path: string) {
  if (isHome(path)) return { titleKey: HOME_TAB_TITLE_KEY }
  try {
    const resolved = router.resolve(path)
    const { titleKey } = resolveRouteMetaKeys({
      name: resolved.name ?? undefined,
      meta: resolved.meta,
    })
    const title = typeof resolved.meta.title === 'string' ? resolved.meta.title : undefined
    return { titleKey, title }
  } catch {
    return {}
  }
}

function titleFor(path: string) {
  void locale.value
  const meta = metaFor(path)
  if (meta.titleKey) {
    const translated = t(meta.titleKey)
    if (translated && translated !== meta.titleKey) return translated
    if (te(meta.titleKey)) return translated
  }
  if (meta.title) return meta.title
  return path.split('/').filter(Boolean).pop() || t('nav.pageFallback')
}

const displayTabs = computed(() =>
  openTabs.value.map((tab) => ({
    path: tab.path,
    title: titleFor(tab.path),
  })),
)

function applyHeight() {
  const root = document.querySelector('.console-app') as HTMLElement | null
  root?.style.setProperty('--console-tab-height', `${tabHeight.value}px`)
}

watch(
  () => route.path,
  (path) => {
    if (!path.startsWith('/admin') && !path.startsWith('/shop')) return
    open(path, metaFor(path))
  },
  { immediate: true },
)

function activate(path: string) {
  if (route.path !== path) router.push(path)
}

function closeTab(path: string) {
  if (isHome(path)) return
  const next = close(path)
  if (route.path === path) router.push(next || HOME_TAB.path)
}

function onResizeStart(event: MouseEvent) {
  event.preventDefault()
  const startY = event.clientY
  const startH = tabHeight.value
  const move = (ev: MouseEvent) => {
    tabHeight.value = Math.min(MAX_H, Math.max(MIN_H, startH + ev.clientY - startY) )
    applyHeight()
  }
  const up = () => {
    try {
      localStorage.setItem(HEIGHT_KEY, String(tabHeight.value))
    } catch {
      /* ignore */
    }
    window.removeEventListener('mousemove', move)
    window.removeEventListener('mouseup', up)
  }
  window.addEventListener('mousemove', move)
  window.addEventListener('mouseup', up)
}

function showMenu(event: MouseEvent, path: string) {
  menuTarget.value = path
  menuX.value = event.clientX
  menuY.value = event.clientY
  menuVisible.value = true
}

function hideMenu() {
  menuVisible.value = false
  menuTarget.value = null
}

const canCloseRight = computed(() => {
  if (!menuTarget.value) return false
  const i = openTabs.value.findIndex((tab) => tab.path === menuTarget.value)
  return i >= 0 && i < openTabs.value.length - 1
})

const canCloseLeft = computed(() => {
  if (!menuTarget.value) return false
  const i = openTabs.value.findIndex((tab) => tab.path === menuTarget.value)
  return i > 1
})

function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') hideMenu()
}

onMounted(() => {
  try {
    const saved = Number(localStorage.getItem(HEIGHT_KEY))
    if (saved >= MIN_H && saved <= MAX_H) tabHeight.value = saved
  } catch {
    /* ignore */
  }
  applyHeight()
  window.addEventListener('keydown', onKey)
})
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="app-tab-bar" :style="{ height: `${tabHeight}px` }">
    <div class="tab-scroll">
      <div
        v-for="tab in displayTabs"
        :key="tab.path"
        class="tab-item"
        :class="{ active: route.path === tab.path }"
        @click="activate(tab.path)"
        @contextmenu.prevent="showMenu($event, tab.path)"
        @click.middle.prevent="closeTab(tab.path)"
      >
        <span class="tab-title">{{ tab.title }}</span>
        <button v-if="!isHome(tab.path)" type="button" class="tab-close" :aria-label="t('common.close')" @click.stop="closeTab(tab.path)">
          &times;
        </button>
      </div>
    </div>
    <div class="tab-resizer" :title="t('nav.tabResizeHint')" @mousedown="onResizeStart" />

    <Teleport to="body">
      <div v-if="menuVisible" class="tab-context-menu" :style="{ left: `${menuX}px`, top: `${menuY}px` }" @click.stop>
        <div v-if="menuTarget && !isHome(menuTarget)" class="menu-item" @click="closeTab(menuTarget); hideMenu()">{{ t('nav.tabCloseCurrent') }}</div>
        <div class="menu-item" @click="menuTarget && activate(closeOthers(menuTarget)); hideMenu()">{{ t('nav.tabCloseOthers') }}</div>
        <div class="menu-item" :class="{ disabled: !canCloseRight }" @click="canCloseRight && menuTarget && (closeToRight(menuTarget), hideMenu())">{{ t('nav.tabCloseRight') }}</div>
        <div class="menu-item" :class="{ disabled: !canCloseLeft }" @click="canCloseLeft && menuTarget && (closeToLeft(menuTarget), hideMenu())">{{ t('nav.tabCloseLeft') }}</div>
        <div class="menu-separator" />
        <div class="menu-item" @click="activate(closeAll()); hideMenu()">{{ t('nav.tabCloseAll') }}</div>
      </div>
      <div v-if="menuVisible" class="tab-menu-overlay" @click="hideMenu" />
    </Teleport>
  </div>
</template>

<style scoped>
.app-tab-bar {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 28px;
  background: rgba(255, 255, 255, 0.55);
  backdrop-filter: blur(24px) saturate(1.8);
  -webkit-backdrop-filter: blur(24px) saturate(1.8);
  border-bottom: 1px solid rgba(255, 255, 255, 0.4);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.25);
  user-select: none;
}
.tab-scroll {
  display: flex;
  align-items: stretch;
  height: 100%;
  overflow-x: auto;
  flex: 1;
  min-width: 0;
  scrollbar-width: none;
}
.tab-scroll::-webkit-scrollbar { display: none; }
.tab-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 14px;
  height: 100%;
  font-size: 13px;
  color: #595959;
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
}
.tab-item:hover {
  background: rgba(5, 85, 206, 0.06);
  color: #0446a7;
}
.tab-item.active {
  color: #0555ce;
  background: rgba(230, 238, 250, 0.82);
  border-radius: 8px 8px 0 0;
  margin: 0 2px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55);
}
:global(html.dark) .tab-item,
:global(.dark) .tab-item {
  color: #e5e5e5 !important;
}
:global(html.dark) .tab-item:hover,
:global(.dark) .tab-item:hover {
  background: rgba(5, 85, 206, 0.18) !important;
  color: #93c5fd !important;
}
:global(html.dark) .tab-item.active,
:global(.dark) .tab-item.active {
  color: #93c5fd !important;
  background: rgba(5, 85, 206, 0.32) !important;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08) !important;
}
.tab-item.active::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 10%;
  right: 10%;
  height: 2px;
  background: #0555ce;
}
.tab-title {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tab-close {
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: #94a3b8;
  cursor: pointer;
}
.tab-close:hover { background: rgba(100, 116, 139, 0.15); color: #475569; }
.tab-resizer {
  position: absolute;
  left: 0;
  right: 0;
  bottom: -4px;
  z-index: 5;
  height: 8px;
  cursor: row-resize;
}
.tab-resizer::after {
  content: '';
  position: absolute;
  left: 40%;
  right: 40%;
  top: 3px;
  height: 2px;
  border-radius: 2px;
  background: var(--color-border, #e8e8e8);
}
.tab-resizer:hover::after {
  background: #0555ce;
}
.tab-context-menu {
  position: fixed;
  z-index: 9999;
  min-width: 130px;
  padding: 6px 0;
  background: rgba(255, 255, 255, 0.86);
  border: 1px solid rgba(255, 255, 255, 0.5);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.12);
  backdrop-filter: blur(18px) saturate(1.6);
  -webkit-backdrop-filter: blur(18px) saturate(1.6);
}
.menu-item { padding: 7px 18px; font-size: 13px; cursor: pointer; color: #262626; }
.menu-item:hover { background: rgba(230, 238, 250, 0.9); color: #0555ce; }
.menu-item.disabled { opacity: 0.4; pointer-events: none; }
.menu-separator { height: 1px; margin: 4px 0; background: rgba(15, 23, 42, 0.08); }
.tab-menu-overlay { position: fixed; inset: 0; z-index: 9998; }

:global(html.dark) .app-tab-bar,
:global(.dark) .app-tab-bar {
  background: rgba(0, 0, 0, 0.72);
  border-bottom-color: rgba(255, 255, 255, 0.1);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
}
:global(html.dark) .tab-context-menu,
:global(.dark) .tab-context-menu {
  background: rgba(17, 17, 17, 0.94);
  border-color: rgba(255, 255, 255, 0.12);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}
:global(html.dark) .menu-item,
:global(.dark) .menu-item { color: #f5f5f5; }
:global(html.dark) .menu-item:hover,
:global(.dark) .menu-item:hover { background: rgba(5, 85, 206, 0.28); color: #93c5fd; }
:global(html.dark) .menu-separator,
:global(.dark) .menu-separator { background: rgba(255, 255, 255, 0.12); }
</style>
