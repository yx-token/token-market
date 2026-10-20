import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import BillsView from '@/views/buyer/BillsView.vue'
import { buyer } from '@/api'

vi.mock('@/api', () => ({ buyer: { bills: vi.fn() } }))

describe('活动包账单', () => {
  it('实际消费合并人民币预付与余额补差，美元抵扣单列，普通用量仍计入总额', async () => {
    vi.mocked(buyer.bills).mockResolvedValue([
      { day: '2026-10-15', requests: 2, cost: 4.3, cash_cost: 2.8, credit_prepaid_cny: 1.5, credit_covered_usd: 0.5, credit_covered_units: 3.5 },
      { day: '2026-10-14', requests: 1, cost: 2 },
    ])
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: BillsView }] })
    await router.push('/')
    const wrapper = mount(BillsView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    const stats = wrapper.findAll('.stat')
    expect(stats[0]!.text()).toContain('¥6.30')
    expect(stats[1]!.text()).toContain('¥4.80')
    expect(stats[2]!.text()).toContain('$0.50')
    expect(stats[2]!.text()).toContain('¥1.50')
    expect(stats[3]!.text()).toContain('3')
    expect(wrapper.find('tbody').text()).toContain('平台余额 ¥2.8000')
    expect(wrapper.find('tbody').text()).toContain('预付 ¥1.5000')
    wrapper.unmount()
  })
})
