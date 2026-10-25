import { beforeEach, describe, expect, it, vi } from 'vitest'
import { kimiDeviceCode, kimiToken } from '@/__tests__/fixtures/kimiOAuth'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))

import kimiAPI from '@/api/admin/kimi'
import { adminAPI } from '@/api/admin'

describe('admin Kimi OAuth API', () => {
  beforeEach(() => post.mockReset())

  it('registers the API and requests a device code with proxy and cancellation', async () => {
    post.mockResolvedValue({ data: kimiDeviceCode })
    const signal = new AbortController().signal
    expect(adminAPI.kimi).toBe(kimiAPI)
    expect(await kimiAPI.generateDeviceCode({ proxy_id: 8 }, signal)).toEqual(kimiDeviceCode)
    expect(post).toHaveBeenCalledWith('/admin/kimi/oauth/device-code', { proxy_id: 8 }, { signal })
  })

  it.each(['pending', 'slow_down', 'success', 'expired', 'denied'] as const)('preserves HTTP 200 status %s', async status => {
    const response = { status, interval: 10, token_info: status === 'success' ? kimiToken : undefined }
    post.mockResolvedValue({ data: response })
    expect(await kimiAPI.pollDeviceCode({ session_id: 'session', proxy_id: 8 })).toEqual(response)
    expect(post).toHaveBeenCalledWith('/admin/kimi/oauth/poll', { session_id: 'session', proxy_id: 8 }, { signal: undefined })
  })

  it('refreshes a token with the original device and proxy', async () => {
    post.mockResolvedValue({ data: kimiToken })
    const payload = { refresh_token: 'refresh', device_id: 'device', proxy_id: 8 }
    expect(await kimiAPI.refreshKimiToken(payload)).toEqual(kimiToken)
    expect(post).toHaveBeenCalledWith('/admin/kimi/oauth/refresh-token', payload)
  })

  it('refreshes an existing account through the Kimi endpoint', async () => {
    const account = { id: 7, platform: 'kimi', type: 'oauth' }
    post.mockResolvedValue({ data: account })
    expect(await kimiAPI.refreshAccount(7)).toEqual(account)
    expect(post).toHaveBeenCalledWith('/admin/kimi/accounts/7/refresh')
  })

  it('accepts the wrapped account response used by generic token refresh', async () => {
    const account = { id: 7, platform: 'kimi', type: 'oauth' }
    post.mockResolvedValue({ data: { account } })
    expect(await kimiAPI.refreshAccount(7)).toEqual(account)
  })
})
