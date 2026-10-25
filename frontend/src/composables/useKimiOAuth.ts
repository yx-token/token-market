import { onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { KimiDeviceCodeResponse, KimiPollResponse, KimiTokenInfo } from '@/api/admin/kimi'
import { extractApiErrorMessage } from '@/utils/apiError'

export function useKimiOAuth() {
  const { t } = useI18n()
  const deviceCode = ref<KimiDeviceCodeResponse | null>(null)
  const tokenInfo = ref<KimiTokenInfo | null>(null)
  const status = ref<'idle' | 'loading' | 'error' | KimiPollResponse['status']>('idle')
  const loading = ref(false)
  const error = ref('')
  const remainingSeconds = ref(0)
  let generation = 0
  let deadline = 0
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let countdownTimer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined

  // Invalidate in-flight responses as well as timers, including a pending code request.
  const stopPolling = () => {
    generation++
    clearTimeout(pollTimer)
    clearTimeout(expiryTimer)
    clearInterval(countdownTimer)
    controller?.abort()
    controller = undefined
    loading.value = false
  }

  const resetState = () => {
    stopPolling()
    deviceCode.value = null
    tokenInfo.value = null
    status.value = 'idle'
    remainingSeconds.value = 0
    error.value = ''
  }

  const expire = () => {
    stopPolling()
    remainingSeconds.value = 0
    status.value = 'expired'
  }

  const generateDeviceCode = async (proxyId?: number | null): Promise<boolean> => {
    if (loading.value) return false
    resetState()
    const run = generation
    const payload = proxyId ? { proxy_id: proxyId } : {}
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    status.value = 'loading'

    try {
      const response = await adminAPI.kimi.generateDeviceCode(payload, signal)
      if (run !== generation) return false
      deviceCode.value = response
      let interval = response.interval > 0 ? response.interval : 5
      deadline = Date.now() + response.expires_in * 1000
      if (response.expires_in <= 0) {
        expire()
        return false
      }
      status.value = 'pending'
      remainingSeconds.value = Math.ceil(response.expires_in)
      expiryTimer = setTimeout(expire, response.expires_in * 1000)
      countdownTimer = setInterval(() => {
        remainingSeconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      }, 1000)

      // Schedule only after each response to prevent overlapping requests.
      const poll = async () => {
        if (run !== generation) return
        if (Date.now() >= deadline) { expire(); return }
        try {
          const result = await adminAPI.kimi.pollDeviceCode({ session_id: response.session_id, ...payload }, signal)
          if (run !== generation) return
          if (Date.now() >= deadline) { expire(); return }
          status.value = result.status
          if (result.status === 'success') {
            if (!result.token_info?.access_token || !result.token_info.refresh_token || !(result.token_info.expires_in > 0)) {
              throw new Error(t('admin.accounts.oauth.kimi.invalidToken'))
            }
            stopPolling()
            tokenInfo.value = result.token_info
          } else if (result.status === 'expired' || result.status === 'denied' || result.status === 'failed') {
            if (result.status === 'failed') error.value = result.message || t('admin.accounts.oauth.kimi.invalidToken')
            stopPolling()
            if (result.status === 'expired') remainingSeconds.value = 0
          } else {
            if (result.status === 'slow_down') {
              interval = result.interval && result.interval > 0 ? result.interval : interval + 5
            }
            pollTimer = setTimeout(poll, interval * 1000)
          }
        } catch (err) {
          if (run !== generation) return
          stopPolling()
          status.value = 'error'
          error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.kimi.failedToPoll'))
        }
      }
      pollTimer = setTimeout(poll, interval * 1000)
      return true
    } catch (err) {
      if (run !== generation) return false
      stopPolling()
      status.value = 'error'
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.kimi.failedToGenerate'))
      return false
    }
  }

  onScopeDispose(stopPolling)
  return { deviceCode, tokenInfo, status, loading, error, remainingSeconds, generateDeviceCode, stopPolling, resetState }
}
