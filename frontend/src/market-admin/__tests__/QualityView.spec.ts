import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import QualityView from '../QualityView.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockResolvedValue({
    upstream_health: [{ platform: 'openai', health: 'healthy', grade: 'A', n: 2 }],
    appeals_by_status: [{ status: 'resolved', n: 3 }],
  })
})

enableAutoUnmount(afterEach)

describe('质量概览', () => {
  it('双表位于内容滚动模式，第二张表保留数据且没有无作用搜索按钮', async () => {
    const wrapper = mount(QualityView, { global: { stubs: { Icon: true, Select: true } } })
    await flushPromises()

    expect(wrapper.getComponent(TablePageLayout).props('contentMode')).toBe('content')
    expect(wrapper.find('.tables-stack').exists()).toBe(true)
    const tables = wrapper.findAllComponents(DataTable)
    expect(tables).toHaveLength(2)
    expect(tables[1]!.props('data')).toEqual([{ status: 'resolved', n: 3 }])
    expect(wrapper.text()).toContain('resolved')
    expect(wrapper.findAll('button').some((button) => button.text() === '搜索')).toBe(false)
  })
})
