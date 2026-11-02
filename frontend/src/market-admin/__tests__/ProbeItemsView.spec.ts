import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProbeItemsView from '../ProbeItemsView.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
const router = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-router', () => ({ useRouter: () => router }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function item(id: number) {
  return {
    id,
    suite: 'inspection_capability',
    category: 'identity',
    prompt: `题目 ${id}`,
    grader: { type: 'exact', answer: 'yes' },
    platforms: [],
    models: [],
    max_tokens: 1024,
    status: 'active',
    request: {},
    generator: {},
  }
}

let items: ReturnType<typeof item>[]

async function page() {
  const wrapper = mount(ProbeItemsView, {
    global: { stubs: { Icon: true, Select: true, Teleport: true } },
  })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.setItem('table-page-size', '10')
  items = [item(1)]
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/probe-items?status=active') return structuredClone(items)
    if (path === '/api/v1/admin/market/probe-items') return structuredClone(items)
    if (path === '/api/v1/admin/market/reference-accounts') return []
    if (path === '/api/v1/admin/market/vendors') {
      return [{ platform: 'anthropic', supported_auth_types: ['oauth'] }]
    }
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})

afterEach(() => {
  window.localStorage.removeItem('table-page-size')
  vi.restoreAllMocks()
})
enableAutoUnmount(afterEach)

describe('题库与基线账号页', () => {
  it('新建题目校验失败在弹窗内播报，并保留弹窗', async () => {
    const wrapper = await page()
    await click(wrapper, '新建题目')
    await click(wrapper, '保存')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.get('[role="alert"]').text()).toBe('请填写题目内容')
    expect(dialog.text()).toContain('新建题目')
    expect(api.send).not.toHaveBeenCalled()
  })

  it('基线创建 API 失败在弹窗内展示错误', async () => {
    api.send.mockRejectedValue(new Error('基线创建被拒绝'))
    const wrapper = await page()
    await click(wrapper, '基线账号')
    await click(wrapper, '添加基线')
    await click(wrapper, '创建')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.get('[role="alert"]').text()).toBe('基线创建被拒绝')
    expect(dialog.text()).toContain('添加基线账号')
    expect(router.push).not.toHaveBeenCalled()
  })

  it('统计标明当前查询/加载口径，下线末页数据后回收页码', async () => {
    items = Array.from({ length: 11 }, (_, index) => item(index + 1))
    const wrapper = await page()
    expect(wrapper.text()).toContain('当前查询题目')
    expect(wrapper.text()).toContain('当前加载启用')
    expect(wrapper.text()).toContain('当前加载已下线')
    expect(wrapper.text()).not.toContain('题目总数')

    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([item(11)])

    items = [item(1)]
    await click(wrapper, '下线')
    await click(wrapper, '确认下线')
    expect(api.send).toHaveBeenCalledWith('POST', '/api/v1/admin/market/probe-items/11/retire')
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([item(1)])
  })

  it('取消下线题目时不发送请求', async () => {
    const wrapper = await page()
    await click(wrapper, '下线')
    expect(wrapper.get('[role="dialog"]').text()).toContain('题目 #1')
    await click(wrapper, '取消')
    expect(api.send).not.toHaveBeenCalled()
  })

  it('取消移除基线账号时不发送请求', async () => {
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/reference-accounts') {
        return [{ id: 7, platform: 'anthropic', status: 'ready', account_email: 'base@example.com' }]
      }
      if (path === '/api/v1/admin/market/vendors') return []
      return []
    })
    const wrapper = await page()
    await click(wrapper, '基线账号')
    await click(wrapper, '移除')
    expect(wrapper.get('[role="dialog"]').text()).toContain('基线账号 #7')
    await click(wrapper, '取消')
    expect(api.send).not.toHaveBeenCalled()
  })
})
