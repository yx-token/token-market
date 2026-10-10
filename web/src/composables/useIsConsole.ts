import { computed } from 'vue'
import { useRoute } from 'vue-router'

/** 是否处于 /console 运营台（与市场账本皮肤分离） */
export function useIsConsole() {
  const route = useRoute()
  return computed(() => route.path.startsWith('/console') || route.path.startsWith('/admin'))
}

export function documentIsConsole() {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('is-console')
}
