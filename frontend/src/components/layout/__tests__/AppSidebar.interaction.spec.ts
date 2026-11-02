import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AppSidebar from '../AppSidebar.vue'

const sidebarState = await vi.hoisted(async () => {
  const { reactive } = await import('vue')
  return reactive({
    collapsed: true,
    mobileOpen: false,
    isMobile: false,
    setCollapsed: vi.fn(),
    setMobileOpen: vi.fn(),
  })
})

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/admin/dashboard' }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    get sidebarCollapsed() { return sidebarState.collapsed },
    get mobileOpen() { return sidebarState.mobileOpen },
    setSidebarCollapsed: sidebarState.setCollapsed,
    setMobileOpen: sidebarState.setMobileOpen,
    toggleSidebar: vi.fn(),
    siteName: '闲猫',
    siteLogo: '',
    siteVersion: '1',
    publicSettingsLoaded: true,
    cachedPublicSettings: {},
    backendModeEnabled: false,
    sidebarScrollTop: 0,
  }),
  useAuthStore: () => ({ isAdmin: true, isSimpleMode: false, user: { role: 'admin' } }),
  useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }),
  useAdminSettingsStore: () => ({
    customMenuItems: [],
    opsMonitoringEnabled: false,
    paymentEnabled: false,
    fetch: vi.fn(),
  }),
}))

vi.mock('@/utils/featureFlags', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/featureFlags')>()),
  makeSidebarFlag: () => () => true,
}))

vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: { value: false }, refreshBatchImageAccess: vi.fn() }),
}))

const RouterLinkStub = {
  props: ['to'],
  template: '<a :href="to"><slot /></a>',
}

let wrapper: ReturnType<typeof mount> | undefined

beforeEach(() => {
  sidebarState.collapsed = true
  sidebarState.mobileOpen = false
  sidebarState.isMobile = false
  sidebarState.setCollapsed.mockReset()
  sidebarState.setMobileOpen.mockReset()
  sidebarState.setCollapsed.mockImplementation((value: boolean) => { sidebarState.collapsed = value })
  sidebarState.setMobileOpen.mockImplementation((value: boolean) => { sidebarState.mobileOpen = value })
  window.matchMedia = ((query: string) => ({
    matches: query.includes('max-width') ? sidebarState.isMobile : false,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })) as unknown as typeof window.matchMedia
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

async function mountSidebar() {
  wrapper = mount(AppSidebar, {
    attachTo: document.body,
    global: { stubs: { 'router-link': RouterLinkStub, Icon: true, VersionBadge: true } },
  })
  await nextTick()
  await nextTick()
  return wrapper
}

describe('AppSidebar collapsed groups', () => {
  it('expands the sidebar and the clicked group instead of ignoring the click', async () => {
    await mountSidebar()
    const group = wrapper!.findAll('.sidebar-group > button')
      .find((button) => button.text().includes('nav.channelManagement'))!
    expect(group.attributes('aria-expanded')).toBe('false')
    expect(group.attributes('aria-controls')).toBeTruthy()

    await group.trigger('click')
    await nextTick()
    expect(sidebarState.setCollapsed).toHaveBeenCalledWith(false)
    expect(group.attributes('aria-expanded')).toBe('true')
    expect(wrapper!.find(`#${group.attributes('aria-controls')}`).exists()).toBe(true)
  })
})

describe('AppSidebar mobile drawer', () => {
  it('ignores desktop collapse, isolates the background, and restores focus on Escape', async () => {
    sidebarState.isMobile = true
    sidebarState.mobileOpen = true
    document.body.innerHTML = '<div class="console-main-col"><button id="console-mobile-menu-button">menu</button></div>'
    await mountSidebar()
    const aside = wrapper!.get('aside')
    expect(aside.classes()).toContain('is-mobile-open')
    expect(aside.classes()).not.toContain('is-collapsed')
    expect(aside.attributes('style') || '').not.toContain('72px')
    expect(aside.attributes('role')).toBe('dialog')
    expect(aside.attributes('aria-modal')).toBe('true')
    expect(document.querySelector('.console-main-col')!.hasAttribute('inert')).toBe(true)
    expect(document.activeElement!.closest('aside')).toBe(aside.element)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    await nextTick()
    expect(sidebarState.setMobileOpen).toHaveBeenCalledWith(false)
    expect(document.querySelector('.console-main-col')!.hasAttribute('inert')).toBe(false)
    expect(document.activeElement).toBe(document.querySelector('#console-mobile-menu-button'))
  })
})
