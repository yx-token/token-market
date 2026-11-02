import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import UpstreamsView from '../UpstreamsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
const route = vi.hoisted(() => ({ query: {} as Record<string, string> }))
const router = vi.hoisted(() => ({ push: vi.fn() }))

vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
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

const longName = '上游名称'.repeat(40)
const longEmail = `${'account-'.repeat(20)}@example.test`
const upstream = {
  id: 7,
  code: 'upstream-code-that-is-long-'.repeat(8),
  seller_id: 5,
  seller_email: longEmail,
  account_email: longEmail,
  name: longName,
  note: '无空格备注'.repeat(40),
  platform: 'openai',
  auth_type: 'oauth',
  status: 'ready',
  created_at: '2026-10-01 12:00:00',
}

async function page() {
  const wrapper = mount(UpstreamsView, {
    global: { stubs: { Icon: true, Pagination: true, DateRangePicker: dateRange } },
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
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/upstreams') return [structuredClone(upstream)]
    if (path === '/api/v1/admin/market/vendors') return [{ platform: 'openai', enabled: true, auth_types: ['oauth'] }]
    if (path.startsWith('/api/v1/admin/market/users?')) return [{ id: 5, email: longEmail, status: 'active' }]
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
  vi.spyOn(window, 'prompt').mockReturnValue(null)
  vi.spyOn(window, 'confirm').mockReturnValue(false)
})

afterEach(() => {
  vi.restoreAllMocks()
})

enableAutoUnmount(afterEach)

describe('上游列表筛选、长文本与弹窗', () => {
  it('筛选标签关联控件，Select 有中文名称，日期范围在双列网格占整行', async () => {
    const wrapper = await page()
    const expected = [
      ['接入时间', 'upstream-filter-created-at'],
      ['用户', 'upstream-filter-seller'],
      ['状态', 'upstream-filter-status'],
      ['厂商', 'upstream-filter-platform'],
      ['关键词', 'upstream-filter-keyword'],
    ] as const
    for (const [labelText, id] of expected) {
      const item = wrapper.findAll('.filter-item').find((node) => node.get('label').text() === labelText)
      expect(item, `找不到筛选：${labelText}`).toBeDefined()
      expect(item!.get('label').attributes('for')).toBe(id)
      expect(wrapper.find(`#${id}`).exists()).toBe(true)
    }
    const dateItem = wrapper.findAll('.filter-item').find((node) => node.get('label').text() === '接入时间')!
    expect(dateItem.classes()).toContain('filter-item--date-range')
    expect(dateItem.classes()).toContain('sm:col-span-2')
    expect(wrapper.get('#upstream-filter-seller').attributes('aria-label')).toBe('按用户筛选上游')
    expect(wrapper.get('#upstream-filter-status').attributes('aria-label')).toBe('按状态筛选上游')
    expect(wrapper.get('#upstream-filter-platform').attributes('aria-label')).toBe('按厂商筛选上游')
  })

  it('长名称、编号和卖家邮箱提供截断类与全文提示', async () => {
    const wrapper = await page()
    expect(wrapper.get('td.col-name .data-table-cell-truncate').attributes('title')).toBe(longName)
    expect(wrapper.get('td.col-code .data-table-cell-truncate').attributes('title')).toBe(upstream.code)
    expect(wrapper.get('td.col-seller .data-table-cell-truncate').attributes('title')).toBe(longEmail)
  })

  it('创建弹窗具备 aria-modal，校验错误位于弹窗 body 的 alert 中', async () => {
    const wrapper = await page()
    await click(wrapper, '添加上游')
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(dialog.querySelector('textarea')?.classList).toContain('y-textarea')
    const submit = Array.from(dialog.querySelectorAll('button')).find((button) => button.textContent === '创建并开始接入')!
    submit.click()
    await flushPromises()
    const alert = dialog.querySelector<HTMLElement>('[role="alert"]')!
    expect(alert.textContent).toContain('请选择卖家用户')
    expect(alert.closest('.modal-body')).not.toBeNull()
  })

  it.each(['冻结', '删除'])('%s必须经确认对话框，取消不发送请求', async (label) => {
    const wrapper = await page()
    await click(wrapper, label)
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
})
