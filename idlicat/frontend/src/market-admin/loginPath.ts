import { authLoginPath } from '@/auth/storageKeys'

/**
 * vue-router 路径（不含 base）。引擎构建 base=/engine/ 时路由仍是 /login。
 * 整页跳转请用 authLoginPath()。
 */
export function adminLoginPath(): string {
  if (import.meta.env.VITE_ENGINE_HOST === '1') {
    return '/login'
  }
  if (import.meta.env.VITE_MARKET_HOST === '1') {
    return '/admin/login'
  }
  return '/login'
}

/** 登录成功后的默认落地页（vue-router 路径）。 */
export function adminHomePath(): string {
  if (import.meta.env.VITE_ENGINE_HOST === '1') {
    return '/admin/dashboard'
  }
  return import.meta.env.VITE_MARKET_HOST === '1' ? '/admin/market' : '/dashboard'
}

export { authLoginPath }
