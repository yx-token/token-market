import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { seller } from '@/api'
import UpstreamView from '@/views/seller/UpstreamView.vue'
import type { Report, SellerProbeRun, UpstreamDetail } from '@/api/types'

vi.mock('@/api', () => ({
  market: { config: vi.fn().mockResolvedValue({}) },
  seller: {
    upstream: vi.fn(),
    reports: vi.fn().mockResolvedValue([]),
    probeRuns: vi.fn().mockResolvedValue([]),
    report: vi.fn(),
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
        ReportDetail: true,
        InspectionAppraisal: true,
        QuotaPanel: true,
        ResetCreditsPanel: true,
        OnboardingWizard: true,
        ModalDialog: true,
        RefreshButton: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('卖家手动复测：只在检测结果异常时可用', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(seller.reports).mockResolvedValue([])
    vi.mocked(seller.probeRuns).mockResolvedValue([])
  })
  afterEach(() => vi.useRealTimers())

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

  it('检测记录只剩时间、类型、状态、结论、操作五列，不再露出进度与第三方', async () => {
    vi.mocked(seller.probeRuns).mockResolvedValue([
      { id: 1, trigger: 'scheduled', status: 'done', queued_at: '', report_id: 3, verdict: 'normal' },
      { id: 2, trigger: 'admin', status: 'running', queued_at: '', report_id: null, verdict: null },
    ])
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false } })
    const table = wrapper.get('table')
    expect(table.findAll('thead th').map((cell) => cell.text())).toEqual(['时间', '类型', '状态', '结论', '操作'])
    const rows = table.findAll('tbody tr')
    expect(rows[0].text()).toContain('已完成')
    expect(rows[0].text()).toContain('查看报告')
    expect(rows[1].text()).toContain('检测中')
    for (const gone of ['进度', '已答', '测试模型', '流程', '第三方', 'BazaarLink']) expect(wrapper.text()).not.toContain(gone)
    expect(wrapper.text()).not.toContain('测智')
    wrapper.unmount()
  })

  it('进行中的记录每十秒刷新，全部结束后停止轮询，离开页面清理定时器', async () => {
    vi.useFakeTimers()
    const running: SellerProbeRun = {
      id: 10, trigger: 'onboarding', status: 'running', queued_at: '',
      started_at: '2026-10-13T00:00:00Z', report_id: null, verdict: null,
    }
    const done: SellerProbeRun = { ...running, status: 'done', finished_at: '2026-10-13T00:01:00Z' }
    vi.mocked(seller.probeRuns).mockResolvedValueOnce([running]).mockResolvedValueOnce([done])
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false } })
    expect(wrapper.text()).toContain('检测中')
    const calls = vi.mocked(seller.probeRuns).mock.calls.length
    const detailCalls = vi.mocked(seller.upstream).mock.calls.length
    const reportCalls = vi.mocked(seller.reports).mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(seller.probeRuns).toHaveBeenCalledTimes(calls + 1)
    expect(wrapper.text()).toContain('已完成')
    // 检测全部结束后整页刷新一次，带出新的上游状态与报告
    expect(seller.upstream).toHaveBeenCalledTimes(detailCalls + 1)
    expect(seller.reports).toHaveBeenCalledTimes(reportCalls + 1)
    await vi.advanceTimersByTimeAsync(20000)
    expect(seller.probeRuns).toHaveBeenCalledTimes(calls + 1)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('有报告的记录调用卖家报告接口打开报告弹窗', async () => {
    const report: Report = { id: 12, upstream_id: 5, trigger: 'admin', verdict: 'normal', created_at: '', impact: {}, summary: { verdict: 'normal', trigger: 'admin', model: 'gpt-6-astra', checked_at: '', baseline: false, suites: {} } }
    vi.mocked(seller.report).mockResolvedValue(report)
    vi.mocked(seller.probeRuns).mockResolvedValue([{
      id: 12, trigger: 'admin', status: 'done', queued_at: '', report_id: 12, verdict: 'normal',
    }])
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false } })
    await wrapper.findAll('button').find((button) => button.text().includes('查看报告'))!.trigger('click')
    await flushPromises()
    expect(seller.report).toHaveBeenCalledWith(12)
    wrapper.unmount()
  })

  it('已结束的记录显示结论徽标，未结束时显示中文状态', async () => {
    vi.mocked(seller.probeRuns).mockResolvedValue([{
      id: 30, trigger: 'admin', status: 'third_party', queued_at: '', report_id: null, verdict: null,
    }, {
      id: 29, trigger: 'admin', status: 'done', queued_at: '', report_id: 9, verdict: 'normal',
    }])
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false } })
    const rows = wrapper.get('table').findAll('tbody tr')
    expect(rows[0].text()).toContain('第三方核验中')
    expect(rows[0].text()).not.toContain('third_party')
    expect(rows[1].findComponent({ name: 'StatusBadge' }).exists()).toBe(true)
    expect(rows[1].text()).not.toContain('已完成已完成')
    wrapper.unmount()
  })

  it('检测记录状态使用卖家中文文案', async () => {
    const statuses = [
      ['queued', '排队中'], ['running', '检测中'], ['third_party', '第三方核验中'], ['done', '已完成'],
      ['failed', '未完成'], ['skipped_budget', '预算跳过'], ['skipped_disabled', '已停用跳过'],
    ] as const
    vi.mocked(seller.probeRuns).mockResolvedValue(statuses.map(([status], index) => ({
      id: index + 1, trigger: 'admin', status, queued_at: '', report_id: null, verdict: null,
    })))
    const wrapper = await mountView({ ...base, manual_retest: { allowed: false } })
    for (const [, text] of statuses) expect(wrapper.text()).toContain(text)
    wrapper.unmount()
  })
})
