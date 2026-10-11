import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { admin } from '@/api'
import InspectionSettingsPanel from '@/components/InspectionSettingsPanel.vue'
import InspectionChecksView from '@/views/admin/InspectionChecksView.vue'
import VendorsView from '@/views/admin/VendorsView.vue'
import DetectionView from '@/views/admin/DetectionView.vue'
import type { InspectionCheckDefinition, InspectionSettings, Policy, VendorConfig } from '@/api/types'

vi.mock('@/api', () => ({ admin: { inspectionSettings: vi.fn(), updateInspectionSettings: vi.fn(), inspectionChecks: vi.fn(), updateInspectionCheck: vi.fn(), vendors: vi.fn(), updateVendor: vi.fn(), policies: vi.fn(), providers: vi.fn(), upsertPolicy: vi.fn() } }))
const settings: InspectionSettings = { mode: 'trial', check_overrides: {}, grade_thresholds: { S: 1.02, A: 0.93, B: 0.82, C: 0.65 }, grade_hysteresis: 0.02, downgrade_confirmations: 2, s_requirements: { gate_free_days: 30, availability_days: 7, min_availability: 0.995 }, scheduling: { enabled: false, minimum_grade: 'B', strategy: 'quality_adjusted' }, timeouts: { full_minutes: 75, quick_minutes: 20 }, limits: { account_parallelism: 2, global_parallelism: 32 }, token_budgets: { full: { input_tokens_per_side: 1300000, output_tokens_per_side: 640000 }, quick: { input_tokens_per_side: 80000, output_tokens_per_side: 160000 } }, future_setting: { preserved: true } }
const definition: InspectionCheckDefinition = { key: 'A15', name: '参数行为指纹', category: 'authenticity', role: 'gate', factor_group: null, enabled: false, calibrated: false, coefficients: {}, thresholds: { min_samples: 10 }, advice: '先做 A/A 校准。', calibration_note: '' }
const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
const listPage = defineComponent({ template: '<main><slot name="filters" /><slot /><slot name="footer" /></main>' })
async function adminPage(component: Parameters<typeof mount>[0]) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/console/test', component: { template: '<div />' } }] })
  await router.push('/console/test')
  await router.isReady()
  const wrapper = mount(component, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: dialog, ConsoleListPage: listPage } } })
  await flushPromises()
  return wrapper
}

describe('质检后台设置', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.inspectionSettings).mockResolvedValue(structuredClone(settings))
    vi.mocked(admin.updateInspectionSettings).mockImplementation(async (body) => body)
    vi.mocked(admin.inspectionChecks).mockResolvedValue([definition])
    vi.mocked(admin.updateInspectionCheck).mockResolvedValue({ ...definition, enabled: true })
  })

  it('独立保存 inspection，不丢预算、校准覆盖与兼容字段', async () => {
    const wrapper = mount(InspectionSettingsPanel, { global: { plugins: [createPinia()] } })
    await flushPromises()
    expect(wrapper.text()).toContain('即使开启质检调度也不生效')
    await wrapper.get('#inspection-mode').setValue('formal')
    await wrapper.get('#inspection-scheduling').setValue(true)
    await wrapper.get('#inspection-min-grade').setValue('A')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith({ ...settings, mode: 'formal', scheduling: { enabled: true, minimum_grade: 'A', strategy: 'quality_adjusted' } })
    wrapper.unmount()
  })

  it('无效高级 JSON 不能保存', async () => {
    const wrapper = mount(InspectionSettingsPanel, { global: { plugins: [createPinia()] } })
    await flushPromises()
    await wrapper.get('#inspection-advanced').setValue('[]')
    await wrapper.get('button').trigger('click')
    expect(admin.updateInspectionSettings).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('JSON 与常用控件同步，不引入契约之外的字段', async () => {
    const wrapper = mount(InspectionSettingsPanel, { global: { plugins: [createPinia()] } })
    await flushPromises()
    await wrapper.get('#inspection-advanced').setValue(JSON.stringify({ ...settings, mode: 'formal', scheduling: { ...settings.scheduling, enabled: true } }))
    expect(wrapper.get<HTMLSelectElement>('#inspection-mode').element.value).toBe('formal')
    expect(wrapper.get<HTMLInputElement>('#inspection-scheduling').element.checked).toBe(true)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith(expect.objectContaining({ mode: 'formal', scheduling: { ...settings.scheduling, enabled: true } }))
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith(expect.not.objectContaining({ scheduling_strategy: expect.anything() }))
    wrapper.unmount()
  })

  it('启用未校准规则保留 calibrated=false，并展示不扣价提示', async () => {
    const wrapper = await adminPage(InspectionChecksView)
    expect(wrapper.text()).toContain('待运营校准，不扣价')
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    await wrapper.get('#check-enabled').setValue(true)
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateInspectionCheck).toHaveBeenCalledWith('A15', { enabled: true, calibrated: false, calibration_note: '', coefficients: {}, thresholds: { min_samples: 10 } })
    wrapper.unmount()
  })

  it('厂商编辑分别保存质检基准和回收倍率', async () => {
    const vendor: VendorConfig = { platform: 'anthropic', enabled: true, auth_types: ['oauth'], supported_auth_types: ['oauth'], consignment_min: 0.1, consignment_max: 2, buyback_base: { default: 0.15 }, appraisal_base: {}, default_concurrency: 2, extra_limits: {}, updated_at: '' }
    vi.mocked(admin.vendors).mockResolvedValue([vendor])
    vi.mocked(admin.updateVendor).mockResolvedValue(vendor)
    const wrapper = await adminPage(VendorsView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    await wrapper.get('#vappraisal').setValue('{"default":1.5,"max":2}')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateVendor).toHaveBeenCalledWith('anthropic', expect.objectContaining({ buyback_base: { default: 0.15 }, appraisal_base: { default: 1.5, max: 2 } }))
    wrapper.unmount()
  })

  it('检测策略保存并行、完整触发与模式预算，并提示全部新套件', async () => {
    const policy: Policy = { id: 1, platform: 'anthropic', auth_type: 'oauth', enabled: true, scheduled_enabled: true, interval_minutes: 360, jitter_minutes: 30, suites: ['iq'], models: ['模型'], daily_budget_usd: 1, thresholds: { h0: 0.9, h1: 0.7, alpha: 0.05, beta: 0.1 }, auto_pause: true, default_grade: 'B', third_party: [], version: 1, updated_at: '' }
    vi.mocked(admin.policies).mockResolvedValue([policy])
    vi.mocked(admin.providers).mockResolvedValue([])
    vi.mocked(admin.upsertPolicy).mockResolvedValue(policy)
    const wrapper = await adminPage(DetectionView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.text()).toContain('inspection_context')
    expect(wrapper.text()).toContain('inspection_performance')
    await wrapper.get('#policy-parallelism').setValue(2)
    await wrapper.get('#policy-full-triggers').setValue('onboarding, appeal')
    await wrapper.get('#policy-token-budgets').setValue('{"quick":{"input_tokens_per_side":80000,"output_tokens_per_side":160000}}')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.upsertPolicy).toHaveBeenCalledWith('anthropic', 'oauth', expect.objectContaining({ item_parallelism: 2, full_triggers: ['onboarding', 'appeal'], token_budgets: { quick: { input_tokens_per_side: 80000, output_tokens_per_side: 160000 } }, suites: ['iq'] }))
    wrapper.unmount()
  })
})
