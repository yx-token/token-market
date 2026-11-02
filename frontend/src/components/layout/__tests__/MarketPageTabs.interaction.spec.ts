import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive, ref } from 'vue'
import MarketPageTabs from '../MarketPageTabs.vue'

const routeState = reactive({ path: '/admin/market/policies' })
const tabsState = vi.hoisted(() => ({
  items: [
    { path: '/admin/dashboard' },
    { path: '/admin/market/policies' },
    { path: '/admin/market/checks' },
  ] as Array<{ path: string; title?: string }>,
  push: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({
    push: tabsState.push,
    resolve: (path: string) => ({ name: undefined, meta: { title: path } }),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string) => key,
    te: () => false,
    locale: ref('zh'),
  }),
}))

vi.mock('@/stores/consoleTabs', async () => {
  const { computed } = await import('vue')
  const isHome = (path: string) => path === '/admin/dashboard'
  return {
    HOME_TAB: { path: '/admin/dashboard' },
    HOME_TAB_TITLE_KEY: 'nav.overview',
    useConsoleTabs: () => ({
      items: computed(() => tabsState.items),
      isHome,
      open: vi.fn(),
      close: (path: string) => {
        const index = tabsState.items.findIndex((tab) => tab.path === path)
        const next = tabsState.items[index + 1] || tabsState.items[index - 1]
        tabsState.items = tabsState.items.filter((tab) => tab.path !== path)
        return next?.path || '/admin/dashboard'
      },
      closeAll: () => '/admin/dashboard',
      closeOthers: (path: string) => path,
      closeToRight: vi.fn(),
      closeToLeft: vi.fn(),
    }),
  }
})

let wrapper: ReturnType<typeof mount> | undefined
const scrollIntoView = vi.fn()

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: scrollIntoView,
  })
  localStorage.clear()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

async function mountTabs() {
  wrapper = mount(MarketPageTabs, { attachTo: document.body })
  await nextTick()
  await nextTick()
  return wrapper
}

describe('MarketPageTabs keyboard interaction', () => {
  it('uses a roving tab stop and moves focus with arrow keys', async () => {
    await mountTabs()
    const tabs = wrapper!.findAll<HTMLButtonElement>('[role="tab"]')
    expect(tabs).toHaveLength(3)
    expect(tabs.map((tab) => tab.attributes('tabindex'))).toEqual(['-1', '0', '-1'])
    expect(tabs[1].attributes('aria-selected')).toBe('true')

    await tabs[1].trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(tabs[2].element)
    await tabs[2].trigger('click')
    expect(tabsState.push).toHaveBeenCalledWith('/admin/market/checks')
  })

  it('scrolls the active tab into view when the route changes', async () => {
    await mountTabs()
    scrollIntoView.mockClear()
    routeState.path = '/admin/market/checks'
    await nextTick()
    await nextTick()
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' })
    routeState.path = '/admin/market/policies'
  })

  it('supports keyboard resizing and Pointer Events dragging', async () => {
    await mountTabs()
    const resizer = wrapper!.get('[role="separator"]')
    expect(resizer.attributes('aria-valuenow')).toBe('38')
    await resizer.trigger('keydown', { key: 'ArrowDown' })
    expect(resizer.attributes('aria-valuenow')).toBe('40')

    await resizer.trigger('pointerdown', { pointerId: 1, button: 0, clientY: 100, bubbles: true })
    const move = new MouseEvent('pointermove', { clientY: 106 })
    Object.defineProperty(move, 'pointerId', { value: 1 })
    window.dispatchEvent(move)
    await nextTick()
    expect(resizer.attributes('aria-valuenow')).toBe('46')
    const up = new MouseEvent('pointerup')
    Object.defineProperty(up, 'pointerId', { value: 1 })
    window.dispatchEvent(up)
  })
})

describe('MarketPageTabs context menu', () => {
  it('uses buttons, clamps to the viewport, and restores focus on Escape', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
      return {
        x: 0, y: 0, top: 0, left: 0, right: 160, bottom: 200,
        width: 160, height: 200, toJSON: () => ({}),
      } as DOMRect
    })
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 400 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 300 })
    await mountTabs()
    const source = wrapper!.findAll('[role="tab"]')[1]
    await source.trigger('contextmenu', { clientX: 390, clientY: 290 })
    await nextTick()
    await nextTick()

    const menu = document.querySelector<HTMLElement>('.tab-context-menu')!
    const items = menu.querySelectorAll<HTMLButtonElement>('button.menu-item')
    expect(items.length).toBeGreaterThan(0)
    expect(menu.style.left).toBe('232px')
    expect(menu.style.top).toBe('92px')
    expect(document.activeElement).toBe(items[0])

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.querySelector('.tab-context-menu')).toBeNull()
    expect(document.activeElement).toBe(source.element)
  })
})
