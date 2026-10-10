import { computed } from 'vue'
import { useRoute, type RouteLocationNormalizedLoaded } from 'vue-router'

/** 是否处于 /console 运营台（与市场账本皮肤分离） */
export function useIsConsole() {
  // 单测等无 Router 上下文时 inject 不到 route
  let route: RouteLocationNormalizedLoaded | undefined
  try {
    route = useRoute()
  } catch {
    route = undefined
  }
  return computed(() => {
    const path = route?.path
    if (path) return path.startsWith('/console') || path.startsWith('/admin')
    return documentIsConsole()
  })
}

export function documentIsConsole() {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('is-console')
}
