import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ListingsView from '../ListingsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn() }))
const route = vi.hoisted(() => ({ query: {} as Record<string, string> }))
const router = vi.hoisted(() => ({ push: vi.fn() }))

vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: vi.fn(),
}))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }))
vi.mock('@/stores/consoleTabs', () => ({ useConsoleTabs: () => ({ open: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('element-plus/es/components/date-picker/style/css', () => ({}))

const dateRange = defineComponent({
  props: ['startDate', 'endDate'],
  emits: ['update:startDate', 'update:endDate'],
  template: '<div class="date-range-stub"><input aria-label="开始时间" /><input aria-label="结束时间" /></div>',
})

const longTitle = '无空格长标题'.repeat(30)
const longEmail = `${'seller-'.repeat(20)}@example.test`
const listing = {
  id: 9,
  upstream_id: 3,
  seller_id: 5,
  seller_email: longEmail,
  mode: 'consignment',
  status: 'active',
  join_default_pool: true,
  title: longTitle,
  created_at: '2026-10-01 12:00:00',
}

async function page() {
  const wrapper = mount(ListingsView, {
    global: { stubs: { Icon: true, Pagination: true, DateRangePicker: dateRange } },
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockImplementation(async (path: string) => {
    if (path.startsWith('/api/v1/admin/market/listings?')) return [listing]
    if (path.startsWith('/api/v1/admin/market/users?')) return []
    throw new Error(`unexpected GET ${path}`)
  })
})

enableAutoUnmount(afterEach)

describe('挂单列表布局与筛选', () => {
  it('筛选标签关联控件，Select 使用明确中文名称，日期范围占满窄屏两行网格', async () => {
    const wrapper = await page()
    const expected = [
      ['创建时间', 'listing-filter-created-at'],
      ['挂单 ID', 'listing-filter-keyword'],
      ['用户', 'listing-filter-seller'],
      ['状态', 'listing-filter-status'],
    ] as const
    for (const [labelText, id] of expected) {
      const item = wrapper.findAll('.filter-item').find((node) => node.get('label').text() === labelText)
      expect(item, `找不到筛选：${labelText}`).toBeDefined()
      expect(item!.get('label').attributes('for')).toBe(id)
      expect(wrapper.find(`#${id}`).exists()).toBe(true)
    }
    const dateItem = wrapper.findAll('.filter-item').find((node) => node.get('label').text() === '创建时间')!
    expect(dateItem.classes()).toContain('filter-item--date-range')
    expect(dateItem.classes()).toContain('sm:col-span-2')
    expect(wrapper.get('#listing-filter-seller').attributes('aria-label')).toBe('按用户筛选挂单')
    expect(wrapper.get('#listing-filter-status').attributes('aria-label')).toBe('按状态筛选挂单')
  })

  it('长标题和邮箱保留全文提示与桌面截断类', async () => {
    const wrapper = await page()
    const title = wrapper.get('tbody td:nth-child(3) .data-table-cell-truncate')
    expect(title.attributes('title')).toBe(longTitle)
    const seller = wrapper.get('tbody td:nth-child(4) .data-table-cell-truncate')
    expect(seller.attributes('title')).toBe(longEmail)
  })
})
