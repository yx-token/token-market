import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ChecksView from '../ChecksView.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function inspectionCheck(id: number) {
  return {
    key: `D${id}`,
    name: `检查 ${id}`,
    flow: 'intelligence',
    category: 'capability',
    enabled: true,
    pricing: null,
    default_pricing: false,
    calibrated: false,
    coefficients: { warning: 0.9, severe: 0.7 },
    thresholds: {},
  }
}

let checks: ReturnType<typeof inspectionCheck>[]

async function page() {
  const wrapper = mount(ChecksView, {
    global: { stubs: { Icon: true, Select: true, Teleport: true } },
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
  window.localStorage.setItem('table-page-size', '10')
  checks = [inspectionCheck(1)]
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/inspection/checks') return structuredClone(checks)
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

afterEach(() => {
  window.localStorage.removeItem('table-page-size')
})
enableAutoUnmount(afterEach)

describe('质检项页', () => {
  it('保存失败在弹窗内播报错误并保留编辑状态', async () => {
    api.send.mockRejectedValue(new Error('系数保存失败'))
    const wrapper = await page()
    await click(wrapper, '编辑')
    await click(wrapper, '保存')

    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.get('[role="alert"]').text()).toBe('系数保存失败')
    expect(dialog.text()).toContain('编辑 D1')
    expect(api.send).toHaveBeenCalledOnce()
  })

  it('刷新后数据减少会回收页码，避免总数非零却显示空页', async () => {
    checks = Array.from({ length: 11 }, (_, index) => inspectionCheck(index + 1))
    const wrapper = await page()
    const pagination = wrapper.getComponent(Pagination)
    expect(pagination.props('pageSize')).toBe(10)

    pagination.vm.$emit('update:page', 2)
    await flushPromises()
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([inspectionCheck(11)])

    checks = [inspectionCheck(1)]
    await click(wrapper, '刷新')
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([inspectionCheck(1)])
  })
})
