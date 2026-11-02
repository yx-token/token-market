import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import FusedSupply from '../FusedSupply.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
const router = vi.hoisted(() => ({ push: vi.fn() }))
const tabs = vi.hoisted(() => ({ open: vi.fn() }))

vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-router', () => ({ useRouter: () => router }))
vi.mock('@/stores/consoleTabs', () => ({ useConsoleTabs: () => ({ open: tabs.open }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const account = {
  upstream_id: 11,
  upstream_name: '长上游账号',
  account_email: 'owner@example.test',
  seller_id: 3,
  platform: 'openai',
  market_status: 'ready',
  engine_account_id: 22,
  engine_name: 'engine-account',
  linked: true,
  link_state: 'orphan',
}

const group = {
  pool_id: 33,
  name: '默认保障池',
  platform: 'openai',
  enabled: true,
  engine_group_id: 44,
  engine_name: 'engine-group',
  market_status: 'ready',
  linked: true,
  link_state: 'linked',
}

async function page(kind: 'accounts' | 'groups') {
  api.get.mockResolvedValue([kind === 'accounts' ? account : group])
  const wrapper = mount(FusedSupply, {
    props: { kind },
    global: { stubs: { Icon: true, Pagination: true } },
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

beforeEach(() => {
  vi.clearAllMocks()
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)

describe('融合供给导航与确认', () => {
  it.each([
    ['accounts' as const, '上游', '/admin/market/upstreams/11'],
    ['accounts' as const, '引擎账号', '/admin/accounts'],
    ['groups' as const, '保障池', '/admin/market/pools'],
    ['groups' as const, '引擎分组', '/admin/groups'],
  ])('%s 页的“%s”会先打开页签再跳转路由', async (kind, label, path) => {
    const wrapper = await page(kind)
    await click(wrapper, label)
    expect(tabs.open).toHaveBeenCalledWith(path, expect.any(Object))
    expect(router.push).toHaveBeenCalledWith(path)
    expect(tabs.open.mock.invocationCallOrder[0]).toBeLessThan(router.push.mock.invocationCallOrder[0]!)
  })

  it('清除失效绑定必须经确认对话框，取消不发送请求', async () => {
    const wrapper = await page('accounts')
    await click(wrapper, '清除绑定')
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

  it('同步成功提示在列表刷新完成后仍然保留', async () => {
    const wrapper = await page('groups')
    await click(wrapper, '同步引擎')
    expect(api.send).toHaveBeenCalledOnce()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('已同步分组 默认保障池')
  })
})
