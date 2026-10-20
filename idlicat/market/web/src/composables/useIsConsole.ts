import { computed } from 'vue'
import { useRoute, type RouteLocationNormalizedLoaded } from 'vue-router'

/** 是否处于 /admin 运营台（与市场账本皮肤分离） */
export function useIsConsole() {
  let route: RouteLocationNormalizedLoaded | undefined
  try {
    route = useRoute()
  } catch {
    route = undefined
  }
  return computed(() => {
    const path = route?.path
    if (path) return path.startsWith('/admin')
    return documentIsConsole()
  })
}

export function documentIsConsole() {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('is-console')
}
