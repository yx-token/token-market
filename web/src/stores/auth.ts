import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { auth as authApi } from '@/api'
import { getToken, setToken } from '@/api/client'
import type { User } from '@/api/types'

export const useAuth = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const loaded = ref(false)

  const loggedIn = computed(() => user.value !== null)
  const isAdmin = computed(() => user.value?.is_admin === true)

  async function load() {
    const token = getToken()
    if (!token) {
      loaded.value = true
      return
    }
    try {
      user.value = await authApi.me()
      const exp = jwtExpiry(token)
      if (exp) scheduleRefresh(exp)
    } catch {
      setToken(null)
      user.value = null
    } finally {
      loaded.value = true
    }
  }

  async function login(email: string, password: string) {
    const res = await authApi.login(email, password)
    setToken(res.token)
    user.value = res.user
    scheduleRefresh(res.expires_at)
  }

  function logout() {
    setToken(null)
    user.value = null
    if (refreshTimer) clearTimeout(refreshTimer)
  }

  // 令牌到期前 10 分钟自动续期
  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  function scheduleRefresh(expiresAt: string) {
    if (refreshTimer) clearTimeout(refreshTimer)
    const delay = new Date(expiresAt).getTime() - Date.now() - 10 * 60 * 1000
    if (delay <= 0) return
    refreshTimer = setTimeout(async () => {
      try {
        const res = await authApi.refresh()
        setToken(res.token)
        user.value = res.user
        scheduleRefresh(res.expires_at)
      } catch {
        logout()
      }
    }, Math.min(delay, 2 ** 31 - 1))
  }

  async function reloadUser() {
    user.value = await authApi.me()
  }

  return { user, loaded, loggedIn, isAdmin, load, login, logout, reloadUser }
})

/** 读取 JWT 的过期时间（不校验签名，只用于安排续期）。 */
function jwtExpiry(token: string): string | null {
  try {
    const payload = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
    return typeof payload.exp === 'number' ? new Date(payload.exp * 1000).toISOString() : null
  } catch {
    return null
  }
}
