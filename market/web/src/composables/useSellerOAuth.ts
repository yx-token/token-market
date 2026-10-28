import { computed, onScopeDispose, ref } from 'vue'
import { seller } from '@/api'
import { ApiError } from '@/api/client'
import type { AuthorizationComplete, AuthorizationOptions, AuthorizationStart, Upstream } from '@/api/types'

export function useSellerOAuth(upstreamID: () => number, reauthorize: () => boolean, onReady: (upstream: Upstream) => void) {
  const session = ref<AuthorizationStart | null>(null)
  const busy = ref(false)
  const active = ref(false)
  const error = ref('')
  const status = ref('')
  const remaining = ref(0)
  const countdown = computed(() => `${Math.floor(remaining.value / 60)}:${String(remaining.value % 60).padStart(2, '0')}`)
  let controller: AbortController | undefined
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let clockTimer: ReturnType<typeof setInterval> | undefined
  let generation = 0

  function stop() {
    generation++
    controller?.abort()
    controller = undefined
    clearTimeout(pollTimer)
    clearInterval(clockTimer)
    active.value = false
    busy.value = false
  }

  function fail(message: string) {
    stop()
    error.value = message
  }

  function checkExpiry() {
    remaining.value = Math.max(0, Math.ceil((Date.parse(session.value?.expires_at ?? '') - Date.now()) / 1000))
    if (!Number.isFinite(remaining.value) || remaining.value <= 0) {
      remaining.value = 0
      fail('授权已过期，请重新发起授权。')
      return true
    }
    return false
  }

  function ready(upstream: Upstream) {
    stop()
    status.value = '授权成功，账号状态已更新。'
    onReady(upstream)
  }

  function message(cause: unknown) {
    return cause instanceof ApiError ? cause.text : cause instanceof Error ? cause.message : '授权失败，请稍后重试。'
  }

  function schedule(interval?: number) {
    const seconds = interval && Number.isFinite(interval) && interval > 0 ? interval : 5
    pollTimer = setTimeout(poll, seconds * 1000)
  }

  async function poll() {
    if (!active.value || !controller || checkExpiry()) return
    const current = generation
    try {
      const result = await seller.oauthPoll(upstreamID(), controller.signal)
      if (current !== generation) return
      if (checkExpiry()) return
      if (result.status === 'ready') {
        if (!result.upstream) throw new Error('授权结果缺少账号状态，请刷新上游列表后重试。')
        ready(result.upstream)
      } else if (result.status === 'expired') {
        fail('授权已过期，请重新发起授权。')
      } else if (result.status === 'denied') {
        fail('官方账号拒绝了授权，请确认订阅与登录账号后重新发起。')
      } else {
        status.value = result.status === 'slow_down' ? '官方要求降低查询频率，已延长等待间隔。' : '等待你在官方页面确认授权，完成后将自动接入。'
        schedule(result.interval)
      }
    } catch (cause) {
      if (current === generation) fail(message(cause))
    }
  }

  async function start(options?: AuthorizationOptions) {
    if (busy.value || active.value) return
    stop()
    const current = generation
    controller = new AbortController()
    busy.value = true
    error.value = ''
    status.value = ''
    session.value = null
    try {
      const result = await (reauthorize() ? seller.reauthorize : seller.startOAuth)(upstreamID(), options, controller.signal)
      if (current !== generation) return
      session.value = result
      if (checkExpiry()) return
      active.value = true
      clockTimer = setInterval(checkExpiry, 1000)
      if (result.flow !== 'link-code') {
        status.value = '等待你在官方页面确认授权，完成后将自动接入。'
        schedule(result.interval)
      }
    } catch (cause) {
      if (current === generation) fail(message(cause))
    } finally {
      if (current === generation) busy.value = false
    }
  }

  async function complete(input: AuthorizationComplete) {
    if (busy.value || session.value?.flow === 'device-code') return
    if (session.value && checkExpiry()) return
    stop()
    const current = generation
    controller = new AbortController()
    busy.value = true
    error.value = ''
    try {
      const upstream = await seller.completeOAuth(upstreamID(), input, controller.signal)
      if (current === generation) ready(upstream)
    } catch (cause) {
      if (current === generation) fail(message(cause))
    } finally {
      if (current === generation) busy.value = false
    }
  }

  onScopeDispose(stop)
  return { session, busy, active, error, status, remaining, countdown, start, complete, stop }
}
