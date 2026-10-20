import { computed, ref } from 'vue'

/** 页签只存 path；标题展示时再按 locale 解析，避免中英混开 */
export type ConsoleTab = { path: string; titleKey?: string; title?: string }

const STORAGE_KEY = 'console.open-tabs'
export const MAX_CONSOLE_TABS = 8
export const HOME_TAB_PATH = '/admin/dashboard'
export const HOME_TAB_TITLE_KEY = 'nav.overview'
export const HOME_TAB: ConsoleTab = { path: HOME_TAB_PATH, titleKey: HOME_TAB_TITLE_KEY }

function isHome(path: string) {
  return path === HOME_TAB_PATH
}

function asTab(raw: unknown): ConsoleTab | null {
  if (!raw || typeof raw !== 'object') return null
  const path = (raw as ConsoleTab).path
  if (typeof path !== 'string' || !path) return null
  const titleKey = (raw as ConsoleTab).titleKey
  const title = (raw as ConsoleTab).title
  return {
    path,
    ...(typeof titleKey === 'string' && titleKey ? { titleKey } : {}),
    ...(typeof title === 'string' && title ? { title } : {}),
  }
}

function normalizeList(list: ConsoleTab[]): ConsoleTab[] {
  const rest = list.filter((t) => t?.path && !isHome(t.path))
  const seen = new Set<string>()
  const unique: ConsoleTab[] = []
  for (const t of rest) {
    if (seen.has(t.path)) continue
    seen.add(t.path)
    unique.push({ path: t.path, ...(t.titleKey ? { titleKey: t.titleKey } : {}), ...(t.title ? { title: t.title } : {}) })
  }
  return [HOME_TAB, ...unique.slice(-(MAX_CONSOLE_TABS - 1))]
}

function load(): ConsoleTab[] {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return [HOME_TAB]
    const parsed = JSON.parse(raw) as unknown[]
    if (!Array.isArray(parsed)) return [HOME_TAB]
    return normalizeList(parsed.map(asTab).filter((t): t is ConsoleTab => !!t))
  } catch {
    return [HOME_TAB]
  }
}

function persist(list: ConsoleTab[]) {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(list))
  } catch {
    /* ignore */
  }
}

const items = ref<ConsoleTab[]>(load())

export function useConsoleTabs() {
  function open(path: string, meta?: { titleKey?: string; title?: string }) {
    if (isHome(path)) {
      items.value = normalizeList(items.value)
      persist(items.value)
      return
    }
    const titleKey = meta?.titleKey
    const title = meta?.title
    const i = items.value.findIndex((t) => t.path === path)
    if (i >= 0) {
      const cur = items.value[i]
      if (cur.titleKey !== titleKey || cur.title !== title) {
        const next = [...items.value]
        next[i] = { path, ...(titleKey ? { titleKey } : {}), ...(title ? { title } : {}) }
        items.value = normalizeList(next)
        persist(items.value)
      }
      return
    }
    items.value = normalizeList([
      ...items.value,
      { path, ...(titleKey ? { titleKey } : {}), ...(title ? { title } : {}) },
    ])
    persist(items.value)
  }

  function close(path: string): string | null {
    if (isHome(path)) return null
    const list = items.value
    const i = list.findIndex((t) => t.path === path)
    if (i < 0) return null
    const next = list[i + 1] || list[i - 1] || HOME_TAB
    items.value = normalizeList(list.filter((t) => t.path !== path))
    persist(items.value)
    return next.path
  }

  function closeAll(): string {
    items.value = [HOME_TAB]
    persist(items.value)
    return HOME_TAB.path
  }

  function closeOthers(keepPath: string): string {
    const keep = isHome(keepPath) ? HOME_TAB.path : keepPath
    const kept = items.value.find((t) => t.path === keep)
    items.value = normalizeList(kept && !isHome(keep) ? [HOME_TAB, kept] : [HOME_TAB])
    persist(items.value)
    return keep
  }

  function closeToRight(fromPath: string): string | null {
    const i = items.value.findIndex((t) => t.path === fromPath)
    if (i < 0) return null
    items.value = normalizeList(items.value.slice(0, i + 1))
    persist(items.value)
    return fromPath
  }

  function closeToLeft(fromPath: string): string | null {
    const i = items.value.findIndex((t) => t.path === fromPath)
    if (i < 0) return null
    items.value = normalizeList(items.value.slice(i))
    persist(items.value)
    return fromPath
  }

  return { items: computed(() => items.value), isHome, open, close, closeAll, closeOthers, closeToRight, closeToLeft }
}
