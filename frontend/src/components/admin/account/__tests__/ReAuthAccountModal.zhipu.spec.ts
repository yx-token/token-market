import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'
import { zhipuSession, zhipuResult, zhipuCallback } from '@/__tests__/fixtures/zhipuOAuth'
const mocks = vi.hoisted(() => ({ init: vi.fn(), poll: vi.fn(), exchange: vi.fn(), update: vi.fn(), clearError: vi.fn(), copy: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  zhipu: { initOAuth: mocks.init, pollOAuth: mocks.poll, exchangeCode: mocks.exchange },
  accounts: { update: mocks.update, clearError: mocks.clearError }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import ReAuthAccountModal from '../ReAuthAccountModal.vue'
import LegacyReAuthAccountModal from '@/components/account/ReAuthAccountModal.vue'

const account = {
  id: 7, name: 'GLM subscription', platform: 'zhipu', type: 'oauth', proxy_id: 8,
  credentials: {
    access_token: 'old-access', api_key: 'old.key', zcode_jwt: 'old-jwt', account_mode: 'coding',
    oauth_provider: 'zai', api_protocol: 'adaptive', model_mapping: { public: 'glm-5' },
    api_base_urls: { chat_completions: 'https://relay.example/v4', anthropic: 'https://relay.example/anthropic' },
    base_url: 'https://relay.example/v4', custom_error_codes: [429]
  }, extra: { base_rpm: 10 }
} as unknown as Account
const result = { ...zhipuResult, provider: 'zai' as const, credentials: { ...zhipuResult.credentials, oauth_provider: 'zai', business_expiry_unknown: true } }
function mountModal(legacy = false) {
  return mount(legacy ? LegacyReAuthAccountModal : ReAuthAccountModal, {
    props: { show: true, account },
    global: { stubs: {
      BaseDialog: defineComponent({ template: '<div><slot /><slot name="footer" /><button data-testid="close-dialog" @click="$emit(\'close\')">close</button></div>' }),
      Icon: true, OAuthAuthorizationFlow: true
    } }
  })
}
describe('ReAuthAccountModal GLM OAuth', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mocks.copy.mockReset()
    mocks.init.mockReset().mockResolvedValue({ ...zhipuSession, provider: 'zai' })
    mocks.poll.mockReset().mockResolvedValue({ status: 'success', result })
    mocks.exchange.mockReset().mockResolvedValue(result)
    mocks.update.mockReset().mockResolvedValue(account)
    mocks.clearError.mockReset().mockResolvedValue({ ...account, status: 'active' })
  })
  afterEach(() => vi.useRealTimers())
  it.each([false, true])('merges all credentials and preserves routing (legacy entry=%s)', async legacy => {
    const wrapper = mountModal(legacy)
    expect(wrapper.find('o-auth-authorization-flow-stub').exists()).toBe(false)
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(mocks.init).toHaveBeenCalledWith({ provider: 'zai', proxy_id: 8 }, expect.any(AbortSignal))
    expect(mocks.update).toHaveBeenCalledWith(7, { type: 'oauth', credentials: { ...account.credentials, ...result.credentials } })
    expect(mocks.clearError).toHaveBeenCalledWith(7)
    if (legacy) expect(wrapper.emitted('reauthorized')).toHaveLength(1)
    else expect(wrapper.emitted('reauthorized')?.[0][0]).toMatchObject({ id: 7, status: 'active' })
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.html()).not.toContain('derived-secret')
    wrapper.unmount()
  })
  it('copies and opens the authorization link safely and supports manual exchange', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await flushPromises()
    const link = wrapper.get('[data-testid="zhipu-oauth-flow"] a')
    expect(link.attributes('href')).toBe(zhipuSession.authorize_url)
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toContain('noopener')
    await wrapper.get('[aria-label="admin.accounts.oauth.zhipu.copyUrl"]').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith(zhipuSession.authorize_url)
    await wrapper.get('[data-testid="zhipu-callback"]').setValue(zhipuCallback)
    await wrapper.get('[data-testid="zhipu-exchange"]').trigger('click')
    await flushPromises()
    expect(mocks.exchange).toHaveBeenCalledWith({ session_id: 'glm-session', callback_url: zhipuCallback, proxy_id: 8 }, expect.any(AbortSignal))
    expect(mocks.update).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
  it.each([false, true])('replaces previous lifecycle values during reauthorization (legacy entry=%s)', async legacy => {
    const wrapper = mountModal(legacy)
    const future = '2099-01-01T00:00:00Z'
    await wrapper.setProps({ account: { ...account, credentials: {
      ...account.credentials, refresh_token: 'old-refresh', zai_oauth_access_token: 'old-oauth',
      business_expires_at: future, zcode_jwt_expires_at: future, oauth_expires_at: future,
      business_expiry_unknown: false, _token_version: 123
    } } })
    // A fresh login without TTL or refresh token must replace the old lifecycle,
    // including when talking to a server version that omitted those fields.
    mocks.poll.mockResolvedValueOnce({ status: 'success', result: {
      provider: 'zai', credentials: { api_key: 'new.secret', access_token: 'new-business', zai_oauth_access_token: 'new-oauth', oauth_provider: 'zai' }, display: {}
    } })
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, { type: 'oauth', credentials: {
      api_key: 'new.secret', access_token: 'new-business', zai_oauth_access_token: 'new-oauth', oauth_provider: 'zai',
      refresh_token: null, business_expires_at: null, zcode_jwt_expires_at: null, oauth_expires_at: null,
      business_expiry_unknown: true, account_mode: 'coding', api_protocol: 'adaptive',
      base_url: account.credentials.base_url, api_base_urls: account.credentials.api_base_urls,
      model_mapping: account.credentials.model_mapping, custom_error_codes: [429]
    } })
    expect(mocks.clearError).toHaveBeenCalledWith(7)
    wrapper.unmount()
  })
  it.each(['close', 'hide', 'unmount'])('stops on %s', async action => {
    mocks.poll.mockResolvedValue({ status: 'pending' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    if (action === 'close') await wrapper.get('[data-testid="close-dialog"]').trigger('click')
    if (action === 'hide') await wrapper.setProps({ show: false })
    if (action === 'unmount') wrapper.unmount()
    await vi.advanceTimersByTimeAsync(400000)
    expect(mocks.poll).toHaveBeenCalledOnce()
    expect(mocks.update).not.toHaveBeenCalled()
    if (action !== 'unmount') wrapper.unmount()
  })
  it('ignores late authorization after changing account', async () => {
    let resolve!: (result: unknown) => void
    mocks.poll.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const wrapper = mountModal()
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    await wrapper.setProps({ account: { ...account, id: 99 } })
    resolve({ status: 'success', result })
    await flushPromises()
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('retries saving without another login after an update error', async () => {
    mocks.update.mockRejectedValueOnce({ message: 'save failed' })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('save failed')
    await wrapper.get('[data-testid="zhipu-save-account"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledTimes(2)
    expect(mocks.init).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
  it.each(['failed', 'expired'])('does not update on %s', async status => {
    mocks.poll.mockResolvedValue({ status })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="zhipu-init"]').trigger('click')
    await vi.advanceTimersByTimeAsync(400000)
    expect(wrapper.get('[data-testid="zhipu-status"]').text()).toContain(status)
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
