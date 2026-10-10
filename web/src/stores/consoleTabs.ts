import { computed, ref } from 'vue'

export type ConsoleTab = { path: string; title: string }

const STORAGE_KEY = 'console.open-tabs'
/** 顶栏多页签上限（含首页概览） */
export const MAX_CONSOLE_TABS = 8
/** 固定首页，始终最左、不可关闭 */
export const HOME_TAB: ConsoleTab = { path: '/console', title: '概览' }

function isHome(path: string) {
  return path === HOME_TAB.path
}

function normalizeList(list: ConsoleTab[]): ConsoleTab[] {
  const rest = list.filter((t) => t?.path && t?.title && !isHome(t.path))
  // 去重（保序）
  const seen = new Set<string>()
  const unique: ConsoleTab[] = []
  for (const t of rest) {
    if (seen.has(t.path)) continue
    seen.add(t.path)
    unique.push(t)
  }
  const capped = unique.slice(-(MAX_CONSOLE_TABS - 1))
  return [HOME_TAB, ...capped]
}

function load(): ConsoleTab[] {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return [HOME_TAB]
    const parsed = JSON.parse(raw) as ConsoleTab[]
    return normalizeList(Array.isArray(parsed) ? parsed : [])
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
  function open(path: string, title: string) {
    if (isHome(path)) {
      // 确保首页在位且标题正确
      items.value = normalizeList(items.value)
      persist(items.value)
      return
    }
    const i = items.value.findIndex((t) => t.path === path)
    if (i >= 0) {
      if (items.value[i].title !== title) {
        const next = [...items.value]
        next[i] = { path, title }
        items.value = normalizeList(next)
        persist(items.value)
      }
      return
    }
    items.value = normalizeList([...items.value, { path, title }])
    persist(items.value)
  }

  /** 关闭后返回应跳转的 path；首页不可关 */
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

  /** 只留首页 */
  function closeAll(): string {
    items.value = [HOME_TAB]
    persist(items.value)
    return HOME_TAB.path
  }

  /** 关闭除当前（及首页）外的页签；keepPath 为空则只留首页 */
  function closeOthers(keepPath: string): string {
    const keep = isHome(keepPath) ? HOME_TAB.path : keepPath
    const kept = items.value.find((t) => t.path === keep)
    items.value = normalizeList(kept && !isHome(keep) ? [HOME_TAB, kept] : [HOME_TAB])
    persist(items.value)
    return keep
  }

  /** 关闭当前页签右侧的全部（不含首页） */
  function closeToRight(fromPath: string): string | null {
    const list = items.value
    const i = list.findIndex((t) => t.path === fromPath)
    if (i < 0) return null
    items.value = normalizeList(list.slice(0, i + 1))
    persist(items.value)
    return fromPath
  }

  /** 关闭当前左侧（始终保留首页） */
  function closeToLeft(fromPath: string): string | null {
    const list = items.value
    const i = list.findIndex((t) => t.path === fromPath)
    if (i < 0) return null
    const keep = list.slice(i)
    items.value = normalizeList(keep)
    persist(items.value)
    return fromPath
  }

  const closableCount = computed(() => items.value.filter((t) => !isHome(t.path)).length)

  return {
    items: computed(() => items.value),
    closableCount,
    homePath: HOME_TAB.path,
    isHome,
    open,
    close,
    closeAll,
    closeOthers,
    closeToRight,
    closeToLeft,
  }
}
