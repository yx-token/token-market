import { effectScope, type EffectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { kimiDeviceCode, kimiToken } from '@/__tests__/fixtures/kimiOAuth'

const { generate, poll } = vi.hoisted(() => ({ generate: vi.fn(), poll: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { kimi: { generateDeviceCode: generate, pollDeviceCode: poll } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import { useKimiOAuth } from '../useKimiOAuth'

describe('useKimiOAuth device authorization', () => {
  let scope: EffectScope
  let oauth: ReturnType<typeof useKimiOAuth>
  beforeEach(() => {
    vi.useFakeTimers()
    generate.mockReset().mockResolvedValue(kimiDeviceCode)
    poll.mockReset().mockResolvedValue({ status: 'pending' })
    scope = effectScope()
    oauth = scope.run(useKimiOAuth)!
  })
  afterEach(() => { scope.stop(); vi.useRealTimers() })

  it('waits for interval, preserves the proxy and returns tokens once', async () => {
    poll.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValueOnce({ status: 'success', token_info: kimiToken })
    await oauth.generateDeviceCode(8)
    expect(generate).toHaveBeenCalledWith({ proxy_id: 8 }, expect.any(AbortSignal))
    await vi.advanceTimersByTimeAsync(4999)
    expect(poll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(poll).toHaveBeenCalledWith({ session_id: 'kimi-session', proxy_id: 8 }, expect.any(AbortSignal))
    await vi.advanceTimersByTimeAsync(5000)
    expect(oauth.tokenInfo.value).toEqual(kimiToken)
    expect(oauth.status.value).toBe('success')
    expect(oauth.loading.value).toBe(false)
    await vi.advanceTimersByTimeAsync(120000)
    expect(poll).toHaveBeenCalledTimes(2)
    expect(oauth.status.value).toBe('success')
  })

  it.each([[7, 7000], [undefined, 10000]])('backs off for slow_down with interval %s', async (interval, delay) => {
    poll.mockResolvedValueOnce({ status: 'slow_down', interval })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(5000)
    expect(oauth.status.value).toBe('slow_down')
    await vi.advanceTimersByTimeAsync(delay - 1)
    expect(poll).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(poll).toHaveBeenCalledTimes(2)
  })

  it.each(['expired', 'denied', 'failed'] as const)('stops for upstream %s', async status => {
    poll.mockResolvedValue({ status })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(120000)
    expect(oauth.status.value).toBe(status)
    expect(poll).toHaveBeenCalledTimes(1)
    expect(oauth.tokenInfo.value).toBeNull()
    expect(oauth.loading.value).toBe(false)
  })

  it('shows the backend incomplete-token message and stops polling', async () => {
    poll.mockResolvedValue({ status: 'failed', message: 'Missing refresh_token; authorize again' })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(15000)
    expect(oauth.status.value).toBe('failed')
    expect(oauth.error.value).toBe('Missing refresh_token; authorize again')
    expect(oauth.tokenInfo.value).toBeNull()
    expect(poll).toHaveBeenCalledTimes(1)
  })

  it('rejects a success response without a positive expires_in', async () => {
    poll.mockResolvedValue({ status: 'success', token_info: { ...kimiToken, expires_in: 0 } })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(5000)
    expect(oauth.status.value).toBe('error')
    expect(oauth.tokenInfo.value).toBeNull()
  })

  it('expires locally during a pending request and ignores a late success', async () => {
    let resolve!: (result: unknown) => void
    poll.mockImplementation(() => new Promise(r => { resolve = r }))
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(60000)
    expect(poll).toHaveBeenCalledTimes(1)
    expect(oauth.status.value).toBe('expired')
    expect(oauth.remainingSeconds.value).toBe(0)
    expect(poll.mock.calls[0][1].aborted).toBe(true)
    resolve({ status: 'success', token_info: kimiToken })
    await flushPromises()
    expect(oauth.tokenInfo.value).toBeNull()
    expect(oauth.status.value).toBe('expired')
  })

  it('prevents duplicate code requests and overlapping polls', async () => {
    poll.mockImplementation(() => new Promise(() => {}))
    await Promise.all([oauth.generateDeviceCode(), oauth.generateDeviceCode()])
    expect(generate).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(15000)
    expect(poll).toHaveBeenCalledTimes(1)
  })

  it('reset cancels an in-flight device request and allows a fresh session', async () => {
    let resolve!: (result: unknown) => void
    generate.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const old = oauth.generateDeviceCode()
    oauth.resetState()
    await oauth.generateDeviceCode(9)
    resolve({ ...kimiDeviceCode, session_id: 'stale' })
    await old
    expect(oauth.deviceCode.value?.session_id).toBe('kimi-session')
    await vi.advanceTimersByTimeAsync(5000)
    expect(poll.mock.calls[0][0]).toEqual({ session_id: 'kimi-session', proxy_id: 9 })
  })

  it('stops on component unmount', async () => {
    let mountedOAuth!: ReturnType<typeof useKimiOAuth>
    const wrapper = mount({ setup() { mountedOAuth = useKimiOAuth(); return () => null } })
    await mountedOAuth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(5000)
    expect(poll).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120000)
    expect(poll).toHaveBeenCalledTimes(1)
    expect(generate.mock.calls[0][1].aborted).toBe(true)
  })

  it('shows ordinary API errors and stops polling', async () => {
    poll.mockRejectedValue({ message: 'session no longer exists' })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(120000)
    expect(oauth.status.value).toBe('error')
    expect(oauth.error.value).toBe('session no longer exists')
    expect(poll).toHaveBeenCalledTimes(1)
  })

  it('handles device-code request errors and invalid successful tokens', async () => {
    generate.mockRejectedValueOnce({ message: 'upstream unavailable' })
    expect(await oauth.generateDeviceCode()).toBe(false)
    expect(oauth.error.value).toBe('upstream unavailable')
    poll.mockResolvedValue({ status: 'success' })
    await oauth.generateDeviceCode()
    await vi.advanceTimersByTimeAsync(5000)
    expect(oauth.status.value).toBe('error')
    expect(oauth.tokenInfo.value).toBeNull()
  })
})
