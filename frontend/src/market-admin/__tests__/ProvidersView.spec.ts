import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProvidersView from '../ProvidersView.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const provider = {
  name: 'test',
  label: '测试检测站',
  description: '检测模型身份',
  docs_url: '',
  default_base_url: 'https://example.test',
  options: {},
  runs_per_hour_limit: 10,
  requests_per_run: 1,
  enabled: true,
  base_url: 'https://example.test',
  has_api_key: false,
  settings: {},
  max_runs_per_hour: 5,
  timeout_minutes: 10,
  runs_last_hour: 0,
}

function check(id: number) {
  return { id, upstream_id: id, provider: 'test', model: `model-${id}`, status: 'done' }
}

let checks: ReturnType<typeof check>[]

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.setItem('table-page-size', '10')
  checks = Array.from({ length: 11 }, (_, index) => check(index + 1))
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/detection-providers') return [structuredClone(provider)]
    if (path === '/api/v1/admin/market/third-party-checks?limit=100') return structuredClone(checks)
    throw new Error(`unexpected GET ${path}`)
  })
})

afterEach(() => {
  window.localStorage.removeItem('table-page-size')
})
enableAutoUnmount(afterEach)

describe('检测站页', () => {
  it('配置卡位于内容槽，最近检测表仍可用，数据减少后回收页码', async () => {
    const wrapper = mount(ProvidersView, { global: { stubs: { Icon: true, Select: true } } })
    await flushPromises()

    expect(wrapper.getComponent(TablePageLayout).props('contentMode')).toBe('content')
    expect(wrapper.find('.filter-panel .provider-card').exists()).toBe(false)
    expect(wrapper.find('.table-scroll-container .provider-card').exists()).toBe(true)

    await click(wrapper, '最近检测')
    expect(wrapper.getComponent(DataTable).props('data')).toHaveLength(10)
    expect(wrapper.getComponent(Pagination).props('pageSize')).toBe(10)

    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    expect(wrapper.getComponent(Pagination).props('page')).toBe(2)
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([check(11)])

    checks = [check(1)]
    await click(wrapper, '刷新')
    expect(wrapper.getComponent(Pagination).props('page')).toBe(1)
    expect(wrapper.getComponent(DataTable).props('data')).toEqual([check(1)])
    expect(wrapper.text()).toContain('model-1')
  })
})
