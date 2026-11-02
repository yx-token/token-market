<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
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
const menuRef = ref<HTMLElement | null>(null)
const tabButtons = new Map<string, HTMLButtonElement>()
let menuReturnFocus: HTMLButtonElement | null = null
let dragPointerId: number | null = null
let dragStartY = 0
let dragStartHeight = DEFAULT_H
let dragResizer: HTMLElement | null = null

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

function setTabRef(path: string, element: unknown) {
  if (element instanceof HTMLButtonElement) tabButtons.set(path, element)
  else tabButtons.delete(path)
}

function focusTab(path: string) {
  void nextTick(() => tabButtons.get(path)?.focus())
}

function applyHeight() {
  const root = document.querySelector('.console-app') as HTMLElement | null
  root?.style.setProperty('--console-tab-height', `${tabHeight.value}px`)
}

function persistHeight() {
  try {
    localStorage.setItem(HEIGHT_KEY, String(tabHeight.value))
  } catch {
    /* ignore */
  }
}

watch(
  () => route.path,
  async (path) => {
    if (path.startsWith('/admin') || path.startsWith('/shop')) open(path, metaFor(path))
    await nextTick()
    tabButtons.get(path)?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  },
  { immediate: true, flush: 'post' },
)

function activate(path: string) {
  if (route.path !== path) router.push(path)
}

function closeTab(path: string) {
  if (isHome(path)) return
  const next = close(path)
  if (route.path === path) router.push(next || HOME_TAB.path)
}

function onTabKeydown(event: KeyboardEvent, path: string) {
  const tabs = displayTabs.value
  const index = tabs.findIndex((tab) => tab.path === path)
  if (index < 0) return
  let targetIndex: number | null = null
  if (event.key === 'ArrowRight') targetIndex = (index + 1) % tabs.length
  if (event.key === 'ArrowLeft') targetIndex = (index - 1 + tabs.length) % tabs.length
  if (event.key === 'Home') targetIndex = 0
  if (event.key === 'End') targetIndex = tabs.length - 1
  if (targetIndex !== null) {
    event.preventDefault()
    focusTab(tabs[targetIndex].path)
    return
  }
  if (event.key === 'Delete' && !isHome(path)) {
    event.preventDefault()
    closeTab(path)
    void nextTick(() => {
      const remaining = displayTabs.value
      remaining[Math.min(index, remaining.length - 1)]?.path && focusTab(remaining[Math.min(index, remaining.length - 1)].path)
    })
  }
}

function onResizeStart(event: PointerEvent) {
  if (event.button !== 0 || dragPointerId !== null) return
  event.preventDefault()
  dragPointerId = event.pointerId
  dragStartY = event.clientY
  dragStartHeight = tabHeight.value
  dragResizer = event.currentTarget as HTMLElement
  dragResizer.setPointerCapture?.(event.pointerId)
  window.addEventListener('pointermove', onResizeMove)
  window.addEventListener('pointerup', onResizeEnd)
  window.addEventListener('pointercancel', onResizeEnd)
}

function onResizeMove(event: PointerEvent) {
  if (event.pointerId !== dragPointerId) return
  tabHeight.value = Math.min(MAX_H, Math.max(MIN_H, dragStartHeight + event.clientY - dragStartY))
  applyHeight()
}

function stopResize() {
  if (dragPointerId !== null) persistHeight()
  if (dragResizer && dragPointerId !== null && dragResizer.hasPointerCapture?.(dragPointerId)) {
    dragResizer.releasePointerCapture(dragPointerId)
  }
  window.removeEventListener('pointermove', onResizeMove)
  window.removeEventListener('pointerup', onResizeEnd)
  window.removeEventListener('pointercancel', onResizeEnd)
  dragPointerId = null
  dragResizer = null
}

function onResizeEnd(event: PointerEvent) {
  if (event.pointerId !== dragPointerId) return
  stopResize()
}

function onResizeKeydown(event: KeyboardEvent) {
  let nextHeight: number | null = null
  if (event.key === 'ArrowUp') nextHeight = tabHeight.value - 2
  if (event.key === 'ArrowDown') nextHeight = tabHeight.value + 2
  if (event.key === 'Home') nextHeight = MIN_H
  if (event.key === 'End') nextHeight = MAX_H
  if (nextHeight === null) return
  event.preventDefault()
  tabHeight.value = Math.min(MAX_H, Math.max(MIN_H, nextHeight))
  applyHeight()
  persistHeight()
}

async function showMenu(event: MouseEvent, path: string) {
  event.preventDefault()
  menuTarget.value = path
  menuX.value = event.clientX
  menuY.value = event.clientY
  menuReturnFocus = tabButtons.get(path) || null
  menuVisible.value = true
  await nextTick()
  const menu = menuRef.value
  if (!menu) return
  const rect = menu.getBoundingClientRect()
  const margin = 8
  menuX.value = Math.min(Math.max(margin, menuX.value), Math.max(margin, window.innerWidth - rect.width - margin))
  menuY.value = Math.min(Math.max(margin, menuY.value), Math.max(margin, window.innerHeight - rect.height - margin))
  menu.querySelector<HTMLButtonElement>('button.menu-item:not(:disabled)')?.focus()
}

function hideMenu(restoreFocus = true) {
  const returnFocus = menuReturnFocus
  menuVisible.value = false
  menuTarget.value = null
  menuReturnFocus = null
  if (restoreFocus) void nextTick(() => returnFocus?.focus())
}

function closeMenuTarget() {
  if (menuTarget.value) closeTab(menuTarget.value)
  hideMenu()
}

function closeMenuOthers() {
  if (menuTarget.value) activate(closeOthers(menuTarget.value))
  hideMenu()
}

function closeMenuRight() {
  if (canCloseRight.value && menuTarget.value) closeToRight(menuTarget.value)
  hideMenu()
}

function closeMenuLeft() {
  if (canCloseLeft.value && menuTarget.value) closeToLeft(menuTarget.value)
  hideMenu()
}

function closeMenuAll() {
  activate(closeAll())
  hideMenu()
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
  if (event.key === 'Escape' && menuVisible.value) {
    event.preventDefault()
    hideMenu()
  }
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
onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  stopResize()
})
</script>

<template>
  <div class="app-tab-bar" :style="{ height: `${tabHeight}px` }">
    <div class="tab-scroll" role="tablist">
      <div
        v-for="tab in displayTabs"
        :key="tab.path"
        class="tab-item"
        :class="{ active: route.path === tab.path }"
      >
        <button
          :id="route.path === tab.path ? 'console-tab-active' : undefined"
          :ref="(element) => setTabRef(tab.path, element)"
          type="button"
          role="tab"
          class="tab-activate"
          :aria-selected="route.path === tab.path"
          :aria-controls="'console-tab-panel'"
          :tabindex="route.path === tab.path ? 0 : -1"
          @click="activate(tab.path)"
          @keydown="onTabKeydown($event, tab.path)"
          @contextmenu="showMenu($event, tab.path)"
          @click.middle.prevent="closeTab(tab.path)"
        >
          <span class="tab-title">{{ tab.title }}</span>
        </button>
        <button
          v-if="!isHome(tab.path)"
          type="button"
          class="tab-close"
          :aria-label="`${t('common.close')} ${tab.title}`"
          @click.stop="closeTab(tab.path)"
        >
          &times;
        </button>
      </div>
    </div>
    <div
      class="tab-resizer"
      role="separator"
      tabindex="0"
      aria-orientation="horizontal"
      :aria-label="t('nav.tabResizeHint')"
      :aria-valuemin="MIN_H"
      :aria-valuemax="MAX_H"
      :aria-valuenow="tabHeight"
      :title="t('nav.tabResizeHint')"
      @pointerdown="onResizeStart"
      @keydown="onResizeKeydown"
    />

    <Teleport to="body">
      <div
        v-if="menuVisible"
        ref="menuRef"
        class="tab-context-menu"
        :style="{ left: `${menuX}px`, top: `${menuY}px` }"
        @click.stop
      >
        <button v-if="menuTarget && !isHome(menuTarget)" type="button" class="menu-item" @click="closeMenuTarget">{{ t('nav.tabCloseCurrent') }}</button>
        <button type="button" class="menu-item" @click="closeMenuOthers">{{ t('nav.tabCloseOthers') }}</button>
        <button type="button" class="menu-item" :disabled="!canCloseRight" @click="closeMenuRight">{{ t('nav.tabCloseRight') }}</button>
        <button type="button" class="menu-item" :disabled="!canCloseLeft" @click="closeMenuLeft">{{ t('nav.tabCloseLeft') }}</button>
        <div class="menu-separator" />
        <button type="button" class="menu-item" @click="closeMenuAll">{{ t('nav.tabCloseAll') }}</button>
      </div>
      <div v-if="menuVisible" class="tab-menu-overlay" @click="hideMenu()" />
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
.tab-activate {
  display: flex;
  align-items: center;
  align-self: stretch;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: pointer;
}
.tab-activate:focus-visible,
.tab-close:focus-visible,
.tab-resizer:focus-visible {
  outline: 2px solid #0555ce;
  outline-offset: -2px;
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
.menu-item {
  display: block;
  width: 100%;
  padding: 7px 18px;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  color: #262626;
}
.menu-item:hover,
.menu-item:focus-visible { background: rgba(230, 238, 250, 0.9); color: #0555ce; outline: none; }
.menu-item:disabled { opacity: 0.4; cursor: not-allowed; }
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
