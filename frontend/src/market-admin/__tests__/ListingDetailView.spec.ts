import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ListingDetailView from '../ListingDetailView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
const route = vi.hoisted(() => ({ params: { id: 'new' }, name: 'MarketAdminListingCreate' }))
const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }))
const tabs = vi.hoisted(() => ({ open: vi.fn(), close: vi.fn() }))

vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }))
vi.mock('@/stores/consoleTabs', () => ({ useConsoleTabs: () => ({ open: tabs.open, close: tabs.close }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const longTitle = '挂单标题'.repeat(50)
const listing = {
  id: 9,
  upstream_id: 3,
  seller_id: 5,
  seller_email: 'seller@example.test',
  mode: 'consignment',
  status: 'active',
  join_default_pool: true,
  title: longTitle,
  description: '长描述',
  categories: [],
}
const upstream = {
  id: 3,
  platform: 'openai',
  status: 'ready',
  account_email: 'owner@example.test',
  seller_id: 5,
}

async function page(id = 'new') {
  route.params.id = id
  route.name = id === 'new' ? 'MarketAdminListingCreate' : 'MarketAdminListingDetail'
  const wrapper = mount(ListingDetailView, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/upstreams') return [upstream]
    if (path === '/api/v1/admin/market/listings/9') return structuredClone(listing)
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
  vi.spyOn(window, 'confirm').mockReturnValue(false)
})

afterEach(() => {
  vi.restoreAllMocks()
})

enableAutoUnmount(afterEach)

describe('挂单详情步骤与危险操作', () => {
  it('新建步骤条使用原生按钮并保留步骤跳转', async () => {
    const wrapper = await page()
    await wrapper.get('input[name="admin-listing-upstream"]').setValue(true)
    const skuStep = wrapper.findAll('.xm-stepper__item')[4]!
    const button = skuStep.get('button')
    expect(button.attributes('type')).toBe('button')
    await button.trigger('click')
    expect(wrapper.text()).toContain('选择商品形态')
  })

  it('强制暂停先显示可访问确认框，取消不调用 API', async () => {
    const wrapper = await page('9')
    await click(wrapper, '强制暂停')
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(api.send).not.toHaveBeenCalled()
    const cancel = Array.from(dialog!.querySelectorAll('button')).find((button) => button.textContent === '取消')!
    cancel.click()
    await flushPromises()
    await vi.waitFor(() => {
      expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    })
    expect(api.send).not.toHaveBeenCalled()
  })

  it('长标题提供全文提示，描述使用多行文本控件', async () => {
    const wrapper = await page('9')
    expect(wrapper.get('.y-title').attributes('title')).toBe(longTitle)
    const description = wrapper.get('textarea')
    expect(description.classes()).toContain('y-textarea')
  })
})
