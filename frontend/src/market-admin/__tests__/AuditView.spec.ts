import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AuditView from '../AuditView.vue'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../api', () => ({ MARKET_API_PREFIX: '/api/v1/admin/market', marketGet: api.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const ledger = { cash: 1, revenue: 2, probe_cost: 3, adjustment: 0, buyer_balances: 4, seller_payable: 5 }
const log = {
  id: 9,
  actor_type: 'admin',
  actor_id: 1,
  action: 'update_'.concat('x'.repeat(100)),
  target: 'user:'.concat('y'.repeat(100)),
  detail: { before: 'a'.repeat(100), after: 'b'.repeat(100) },
  created_at: '2026-01-01T00:00:00Z',
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
    if (path === '/api/v1/admin/market/ledger') return ledger
    if (path.startsWith('/api/v1/admin/market/audit-logs')) return [structuredClone(log)]
    throw new Error(`unexpected GET ${path}`)
  })
})

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('账本与审计详情', () => {
  it('账本页不渲染空表卡', async () => {
    const wrapper = mount(AuditView, { global: { stubs: { Icon: true, Pagination: true } } })
    await flushPromises()
    expect(wrapper.find('.table-scroll-container').exists()).toBe(false)
    expect(wrapper.text()).toContain('平台现金')
  })

  it('长动作、目标和 JSON 都能打开详情对话框', async () => {
    const wrapper = mount(AuditView, { attachTo: document.body, global: { stubs: { Icon: true, Pagination: true } } })
    await flushPromises()
    await click(wrapper, '审计')
    const detailButtons = wrapper.findAll('button').filter((button) => button.text() === '查看详情')
    expect(detailButtons.length).toBeGreaterThanOrEqual(3)
    const expectations = [
      { title: '动作', values: [log.action] },
      { title: '目标', values: [log.target] },
      { title: 'JSON 详情', values: [log.detail.before, log.detail.after] },
    ]
    for (const [index, expectation] of expectations.entries()) {
      await detailButtons[index]!.trigger('click')
      await flushPromises()
      const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
      expect(dialog?.getAttribute('aria-modal')).toBe('true')
      expect(dialog?.getAttribute('aria-labelledby')).toContain('modal-title-')
      expect(dialog?.textContent).toContain(expectation.title)
      for (const value of expectation.values) expect(dialog?.textContent).toContain(value)
      dialog!.querySelector<HTMLButtonElement>('.modal-header button')!.click()
      await flushPromises()
    }
  })
})
