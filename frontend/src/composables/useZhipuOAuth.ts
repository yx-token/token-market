import { onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { ZhipuOAuthProvider, ZhipuOAuthResult, ZhipuOAuthSession, ZhipuPollResponse } from '@/api/admin/zhipu'
import { extractApiErrorMessage } from '@/utils/apiError'

export function useZhipuOAuth() {
  const { t } = useI18n()
  const session = ref<ZhipuOAuthSession | null>(null)
  const result = ref<ZhipuOAuthResult | null>(null)
  const status = ref<'idle' | 'loading' | 'error' | ZhipuPollResponse['status']>('idle')
  const loading = ref(false)
  const exchanging = ref(false)
  const error = ref('')
  const remainingSeconds = ref(0)
  let generation = 0
  let deadline = 0
  let interval = 2
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let countdownTimer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined
  let pollRequest: Promise<void> | undefined
  let schedulePoll: (() => void) | undefined
  let sessionProxy: { proxy_id?: number } = {}

  const stopPolling = () => {
    generation++
    clearTimeout(pollTimer)
    clearTimeout(expiryTimer)
    clearInterval(countdownTimer)
    controller?.abort()
    controller = undefined
    schedulePoll = undefined
    pollRequest = undefined
    loading.value = false
    exchanging.value = false
  }
  const resetState = () => {
    stopPolling()
    session.value = null
    result.value = null
    status.value = 'idle'
    remainingSeconds.value = 0
    error.value = ''
  }
  const expire = () => {
    stopPolling()
    remainingSeconds.value = 0
    status.value = 'expired'
  }
  const finish = (value: ZhipuOAuthResult | undefined) => {
    if (!value || value.provider !== session.value?.provider || !value.credentials ||
      typeof value.credentials.api_key !== 'string' || !value.credentials.api_key.trim()) {
      throw new Error(t('admin.accounts.oauth.zhipu.invalidResult'))
    }
    stopPolling()
    status.value = 'success'
    error.value = ''
    result.value = value
  }
  const validInterval = (value?: number) => typeof value === 'number' && Number.isFinite(value) && value > 0

  const initOAuth = async (provider: ZhipuOAuthProvider, proxyId?: number | null): Promise<boolean> => {
    if (loading.value) return false
    resetState()
    const run = generation
    sessionProxy = proxyId ? { proxy_id: proxyId } : {}
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    status.value = 'loading'
    try {
      const response = await adminAPI.zhipu.initOAuth({ provider, ...sessionProxy }, signal)
      if (run !== generation) return false
      session.value = response
      interval = validInterval(response.interval) ? response.interval : 2
      deadline = Date.now() + response.expires_in * 1000
      if (!Number.isFinite(response.expires_in) || response.expires_in <= 0) { expire(); return false }
      remainingSeconds.value = Math.ceil(response.expires_in)
      status.value = 'pending'
      expiryTimer = setTimeout(expire, response.expires_in * 1000)
      countdownTimer = setInterval(() => {
        remainingSeconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      }, 1000)
      const poll = async () => {
        if (run !== generation || exchanging.value) return
        if (Date.now() >= deadline) { expire(); return }
        try {
          const response = await adminAPI.zhipu.pollOAuth({ session_id: session.value!.session_id, ...sessionProxy }, signal)
          if (run !== generation) return
          if (Date.now() >= deadline) { expire(); return }
          if (response.status === 'success') {
            finish(response.result)
          } else if (response.status === 'expired' || response.status === 'failed') {
            stopPolling()
            status.value = response.status
            error.value = response.message || ''
            if (response.status === 'expired') remainingSeconds.value = 0
          } else {
            if (validInterval(response.interval)) interval = response.interval!
            schedulePoll?.()
          }
        } catch (err) {
          if (run !== generation) return
          stopPolling()
          status.value = 'error'
          error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.zhipu.failedToPoll'))
        }
      }
      schedulePoll = () => {
        if (run !== generation || exchanging.value) return
        clearTimeout(pollTimer)
        pollTimer = setTimeout(() => { pollRequest = poll() }, interval * 1000)
      }
      schedulePoll()
      return true
    } catch (err) {
      if (run !== generation) return false
      stopPolling()
      status.value = 'error'
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.zhipu.failedToInit'))
      return false
    }
  }

  const exchangeCode = async (callback: string): Promise<boolean> => {
    if (!callback.trim() || !session.value || status.value !== 'pending' || exchanging.value) return false
    const run = generation
    exchanging.value = true
    error.value = ''
    clearTimeout(pollTimer)
    // A poll may already be exchanging the same code on the server. Let it finish first.
    await pollRequest
    if (run !== generation) return false
    if (Date.now() >= deadline) { expire(); return false }
    try {
      const value = await adminAPI.zhipu.exchangeCode({
        session_id: session.value.session_id, callback_url: callback.trim(), ...sessionProxy
      }, controller?.signal)
      if (run !== generation) return false
      if (Date.now() >= deadline) { expire(); return false }
      finish(value)
      return true
    } catch (err) {
      if (run !== generation) return false
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.zhipu.failedToExchange'))
      return false
    } finally {
      if (run === generation) {
        exchanging.value = false
        schedulePoll?.()
      }
    }
  }
  onScopeDispose(stopPolling)
  return { session, result, status, loading, exchanging, error, remainingSeconds, initOAuth, exchangeCode, stopPolling, resetState }
}
