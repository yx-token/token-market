import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import CategoriesView from '../CategoriesView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const categories = [
  {
    id: 'disabled_root',
    parent_id: null,
    name: '已停用分类',
    description: '停用说明',
    sort: 10,
    enabled: false,
    seller_pickable: false,
    children: [
      {
        id: 'enabled_child',
        parent_id: 'disabled_root',
        name: '启用子分类',
        description: '',
        sort: 20,
        enabled: true,
        seller_pickable: true,
      },
    ],
  },
]

const defaultMatchMedia = window.matchMedia

function stubViewport(desktop: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: desktop,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
    })),
  })
}

async function page() {
  const wrapper = mount(CategoriesView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, Pagination: true, Select: true } },
  })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function dialog() {
  const element = document.querySelector<HTMLElement>('[role="dialog"]')
  expect(element).not.toBeNull()
  return element!
}

async function clickDialog(text: string) {
  const button = Array.from(dialog().querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent?.trim() === text,
  )
  expect(button, `找不到弹窗按钮：${text}`).toBeDefined()
  button!.click()
  await flushPromises()
}

enableAutoUnmount(afterEach)
afterEach(() => {
  Object.defineProperty(window, 'matchMedia', { configurable: true, value: defaultMatchMedia })
  document.body.innerHTML = ''
})

describe('市场分类页面', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubViewport(true)
    api.get.mockResolvedValue(structuredClone(categories))
    api.send.mockRejectedValue(new Error('分类保存失败'))
  })

  it('停用分类的桌面行使用 is-disabled', async () => {
    const wrapper = await page()
    expect(wrapper.get('tr[data-row-id="disabled_root"]').classes()).toContain('is-disabled')
    expect(wrapper.get('tr[data-row-id="enabled_child"]').classes()).not.toContain('is-disabled')
  })

  it('停用分类的移动卡片使用 is-disabled', async () => {
    stubViewport(false)
    const wrapper = await page()
    const cards = wrapper.findAll('[data-test="data-table-mobile-card"]')
    expect(cards[0]!.classes()).toContain('is-disabled')
    expect(cards[1]!.classes()).not.toContain('is-disabled')
  })

  it('保存失败时在活动弹窗内播报错误并保留表单', async () => {
    const wrapper = await page()
    await click(wrapper, '编辑')

    const description = dialog().querySelector<HTMLTextAreaElement>('textarea.y-textarea')
    expect(description).not.toBeNull()
    description!.value = '失败重试时不能丢失的说明'
    description!.dispatchEvent(new Event('input', { bubbles: true }))
    await clickDialog('保存')

    expect(dialog().getAttribute('aria-modal')).toBe('true')
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('分类保存失败')
    expect(dialog().querySelector<HTMLTextAreaElement>('textarea.y-textarea')?.value).toBe(
      '失败重试时不能丢失的说明',
    )
    expect(api.get).toHaveBeenCalledOnce()
  })

  it('取消删除分类时不发送请求', async () => {
    const wrapper = await page()
    const deleteButtons = wrapper.findAll('button').filter((button) => button.text() === '删除')
    await deleteButtons[1]!.trigger('click')
    await flushPromises()

    expect(dialog().textContent).toContain('启用子分类')
    await clickDialog('取消')

    expect(api.send).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')?.className).toContain('modal-leave')
  })
})
