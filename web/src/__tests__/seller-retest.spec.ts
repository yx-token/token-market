import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { seller } from '@/api'
import UpstreamView from '@/views/seller/UpstreamView.vue'
import type { UpstreamDetail } from '@/api/types'

vi.mock('@/api', () => ({
  market: { config: vi.fn().mockResolvedValue({}) },
  seller: {
    upstream: vi.fn(),
    reports: vi.fn().mockResolvedValue([]),
    trend: vi.fn().mockResolvedValue([]),
    vendors: vi.fn().mockResolvedValue([]),
    listingCategories: vi.fn().mockResolvedValue([]),
    retest: vi.fn().mockResolvedValue({ run_id: 9 }),
  },
}))

const base = {
  id: 5,
  platform: 'openai',
  auth_type: 'oauth',
  status: 'listed',
  health: 'healthy',
  is_reference: false,
  self_reserve_percent: 0,
  auto_reset_enabled: false,
  privacy_ok: true,
  created_at: '',
  updated_at: '',
  stats_24h: { requests: 0, probe_requests: 0, payout: 0, ttft_p50_ms: null, success_rate: null },
  reset_credits_supported: false,
  auto_reset_supported: false,
} as unknown as UpstreamDetail

async function mountView(detail: UpstreamDetail) {
  vi.mocked(seller.upstream).mockResolvedValue(detail)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  await router.push('/seller/upstreams/5')
  const wrapper = mount(UpstreamView, {
    props: { id: '5' },
    global: {
      plugins: [createPinia(), router],
      stubs: {
        TrendChart: true,
        ReportDetail: true,
        InspectionAppraisal: true,
        QuotaPanel: true,
        ResetCreditsPanel: true,
        OnboardingWizard: true,
        ModalDialog: true,
        DataTable: true,
        RefreshButton: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('卖家手动复测：只在检测结果异常时可用', () => {
  beforeEach(() => vi.clearAllMocks())

  it('最近一次检测正常时按钮置灰，并说明原因', async () => {
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false, code: 'RETEST_NOT_NEEDED' } })
    const button = wrapper.get('[data-testid="manual-retest"]')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toContain('最近一次检测正常')
    await button.trigger('click')
    expect(seller.retest).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('结果异常时可以复测，发起前提示消耗自己的额度且不结算收益', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await mountView({ ...base, health: 'suspected', manual_retest: { allowed: true } })
    const button = wrapper.get('[data-testid="manual-retest"]')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    await flushPromises()
    expect(confirmSpy.mock.calls[0][0]).toContain('不结算收益')
    expect(seller.retest).toHaveBeenCalledWith(5)
    confirmSpy.mockRestore()
    wrapper.unmount()
  })

  it('卖家取消确认时不发起复测', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = await mountView({ ...base, health: 'degraded', manual_retest: { allowed: true } })
    await wrapper.get('[data-testid="manual-retest"]').trigger('click')
    await flushPromises()
    expect(seller.retest).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
    wrapper.unmount()
  })
})
