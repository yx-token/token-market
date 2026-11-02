import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AppHeader from '../AppHeader.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/admin/dashboard', name: 'AdminDashboard', meta: {}, params: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ contactInfo: '', docUrl: '', cachedPublicSettings: {} }),
  useAuthStore: () => ({
    isAdmin: true,
    isSimpleMode: false,
    user: {
      role: 'admin',
      username: 'Admin',
      email: 'admin@example.com',
      balance: 12,
      frozen_balance: 2,
      avatar_url: '',
    },
    logout: vi.fn(),
  }),
  useOnboardingStore: () => ({ replay: vi.fn() }),
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/utils/featureFlags', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/featureFlags')>()),
  isFeatureFlagEnabled: () => false,
  resolveFeatureFlag: () => true,
}))

vi.mock('@/router/title', () => ({
  resolveRouteMetaKeys: () => ({ titleKey: 'nav.dashboard', descriptionKey: undefined }),
}))

let wrapper: ReturnType<typeof mount> | undefined

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

async function mountHeader() {
  wrapper = mount(AppHeader, {
    attachTo: document.body,
    global: {
      mocks: { $t: (key: string) => key },
      stubs: {
        Icon: true,
        LocaleSwitcher: true,
        SubscriptionProgressMini: true,
        AnnouncementBell: true,
        SupportBell: true,
        'router-link': { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })
  await nextTick()
  return wrapper
}

describe('AppHeader keyboard menus', () => {
  it('exposes user menu state, focuses the first item, and restores focus on Escape', async () => {
    await mountHeader()
    const trigger = wrapper!.get<HTMLButtonElement>('[aria-label="common.userMenu"]')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper!.find('#console-mobile-menu-button').exists()).toBe(true)

    await trigger.trigger('click')
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(document.querySelector('.dropdown .dropdown-item'))

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })

  it('opens the user menu on focus-in without stealing focus', async () => {
    await mountHeader()
    const trigger = wrapper!.get<HTMLButtonElement>('[aria-label="common.userMenu"]')
    trigger.element.focus()
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(trigger.element)
    expect(wrapper!.find('#header-user-menu').exists()).toBe(true)
  })

  it('opens balance details by click and returns focus on Escape', async () => {
    await mountHeader()
    const trigger = wrapper!.get<HTMLButtonElement>('.balance-trigger')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper!.find('.balance-details').exists()).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })
})
