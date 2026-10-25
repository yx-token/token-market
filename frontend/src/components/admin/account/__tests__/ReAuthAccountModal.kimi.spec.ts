import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'
import { kimiDeviceCode, kimiToken } from '@/__tests__/fixtures/kimiOAuth'

const mocks = vi.hoisted(() => ({ generate: vi.fn(), poll: vi.fn(), update: vi.fn(), clearError: vi.fn(), copy: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  kimi: { generateDeviceCode: mocks.generate, pollDeviceCode: mocks.poll },
  accounts: { update: mocks.update, clearError: mocks.clearError }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const account = {
  id: 7, name: 'Kimi subscription', platform: 'kimi', type: 'oauth', proxy_id: 8,
  credentials: {
    access_token: 'old-access', refresh_token: 'old-refresh', account_mode: 'coding',
    api_protocol: 'anthropic', model_mapping: { public: 'kimi-k2.5' },
    base_url: 'https://relay.example/coding', custom_error_codes: [429]
  }, extra: { base_rpm: 10 }
} as unknown as Account

function mountModal() {
  return mount(ReAuthAccountModal, {
    props: { show: true, account },
    global: { stubs: {
      BaseDialog: defineComponent({ template: '<div><slot /><slot name="footer" /><button data-testid="close-dialog" @click="$emit(\'close\')">close</button></div>' }),
      Icon: true, OAuthAuthorizationFlow: true
    } }
  })
}

describe('ReAuthAccountModal Kimi OAuth', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mocks.copy.mockReset()
    mocks.generate.mockReset().mockResolvedValue(kimiDeviceCode)
    mocks.poll.mockReset().mockResolvedValue({ status: 'success', token_info: kimiToken })
    mocks.update.mockReset().mockResolvedValue(account)
    mocks.clearError.mockReset().mockResolvedValue({ ...account, status: 'active' })
  })
  afterEach(() => vi.useRealTimers())

  it('uses device authorization and merges credentials while clearing the error', async () => {
    const wrapper = mountModal()
    expect(wrapper.find('o-auth-authorization-flow-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.oauth.kimi.title')
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith({ proxy_id: 8 }, expect.any(AbortSignal))
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, {
      type: 'oauth', credentials: {
        ...account.credentials, access_token: kimiToken.access_token, refresh_token: kimiToken.refresh_token,
        expires_at: kimiToken.expires_at, token_type: 'Bearer', scope: 'coding', device_id: 'kimi-device'
      }
    })
    expect(mocks.clearError).toHaveBeenCalledWith(7)
    expect(wrapper.emitted('reauthorized')?.[0][0]).toMatchObject({ id: 7, status: 'active' })
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('copies the displayed code and complete authorization link', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await flushPromises()
    const link = wrapper.get('[data-testid="kimi-device-flow"] a')
    expect(link.attributes('href')).toBe(kimiDeviceCode.verification_uri_complete)
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toContain('noopener')
    await wrapper.get('[aria-label="admin.accounts.oauth.kimi.copyCode"]').trigger('click')
    await wrapper.get('[aria-label="admin.accounts.oauth.kimi.copyUrl"]').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith(kimiDeviceCode.user_code)
    expect(mocks.copy).toHaveBeenCalledWith(kimiDeviceCode.verification_uri_complete)
    wrapper.unmount()
  })

  it('ignores a late token response after switching to another account', async () => {
    let resolve!: (result: unknown) => void
    mocks.poll.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const wrapper = mountModal()
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await vi.advanceTimersByTimeAsync(5000)
    await wrapper.setProps({ account: { ...account, id: 99 } })
    resolve({ status: 'success', token_info: kimiToken })
    await flushPromises()
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['close', 'hide', 'unmount'])('stops polling on %s', async action => {
    mocks.poll.mockResolvedValue({ status: 'pending' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.poll).toHaveBeenCalledOnce()
    if (action === 'close') await wrapper.get('[data-testid="close-dialog"]').trigger('click')
    if (action === 'hide') await wrapper.setProps({ show: false })
    if (action === 'unmount') wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120000)
    expect(mocks.poll).toHaveBeenCalledOnce()
    expect(mocks.update).not.toHaveBeenCalled()
    if (action !== 'unmount') wrapper.unmount()
  })

  it('retains the tokens to retry saving after an update error', async () => {
    mocks.update.mockRejectedValueOnce({ message: 'save failed' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('save failed')
    expect(mocks.clearError).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="kimi-save-account"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledTimes(2)
    expect(mocks.generate).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('does not update the account when authorization is denied', async () => {
    mocks.poll.mockResolvedValue({ status: 'denied' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="kimi-generate-code"]').trigger('click')
    await vi.advanceTimersByTimeAsync(120000)
    expect(wrapper.get('[data-testid="kimi-status"]').text()).toContain('denied')
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
