import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { admin } from '@/api'
import InspectionSettingsPanel from '@/components/InspectionSettingsPanel.vue'
import InspectionChecksView from '@/views/admin/InspectionChecksView.vue'
import VendorsView from '@/views/admin/VendorsView.vue'
import type { InspectionCheckDefinition, InspectionSettings, VendorConfig } from '@/api/types'

vi.mock('@/api', () => ({ admin: { inspectionSettings: vi.fn(), updateInspectionSettings: vi.fn(), inspectionChecks: vi.fn(), updateInspectionCheck: vi.fn(), vendors: vi.fn(), updateVendor: vi.fn(), policies: vi.fn(), providers: vi.fn(), upsertPolicy: vi.fn(), settings: vi.fn(), upstreams: vi.fn(), items: vi.fn(), probeGeneratorTemplates: vi.fn() } }))
const settings: InspectionSettings = { mode: 'trial', check_overrides: {}, grade_thresholds: { S: 1.02, A: 0.93, B: 0.82, C: 0.65 }, grade_hysteresis: 0.02, downgrade_confirmations: 2, s_requirements: { gate_free_days: 30, availability_days: 7, min_availability: 0.995 }, scheduling: { enabled: false, minimum_grade: 'B', strategy: 'quality_adjusted' }, timeouts: { full_minutes: 75, quick_minutes: 20 }, limits: { account_parallelism: 2, global_parallelism: 32 }, token_budgets: { full: { input_tokens_per_side: 1300000, output_tokens_per_side: 640000 }, quick: { input_tokens_per_side: 80000, output_tokens_per_side: 160000 } }, future_setting: { preserved: true } }
const definition: InspectionCheckDefinition = { key: 'A15', name: '参数行为指纹', category: 'authenticity', role: 'gate', factor_group: null, enabled: false, calibrated: false, flow: 'identity', pricing: false, default_pricing: false, coefficients: { warning: 0.85, severe: 0.7 }, thresholds: { min_samples: 10 }, advice: '先做 A/A 校准。', calibration_note: '' }
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
    vi.mocked(admin.items).mockResolvedValue([])
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

  it('未校准规则可独立开启扣价，校准只影响门槛', async () => {
    const wrapper = await adminPage(InspectionChecksView)
    expect(wrapper.text()).toContain('待运营校准')
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.text()).toContain('校准只影响门槛（是否暂停），不影响扣价')
    await wrapper.get('#check-enabled').setValue(true)
    await wrapper.get('#check-pricing').setValue('true')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateInspectionCheck).toHaveBeenCalledWith('A15', { enabled: true, pricing: true, calibrated: false, calibration_note: '', coefficients: { warning: 0.85, severe: 0.7 }, thresholds: { min_samples: 10 } })
    wrapper.unmount()
  })

  it.each([
    { label: '开', pricing: true },
    { label: '关', pricing: false },
    { label: '恢复默认', pricing: null },
  ])('用于扣价选择 $label 时按三态契约保存', async ({ label, pricing }) => {
    const wrapper = await adminPage(InspectionChecksView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    const option = wrapper.get('#check-pricing').findAll('option').find((item) => item.text() === label)!
    await wrapper.get('#check-pricing').setValue((option.element as HTMLOptionElement).value)
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateInspectionCheck).toHaveBeenCalledWith('A15', expect.objectContaining({ pricing }))
    wrapper.unmount()
  })

  it('编辑回填生效扣价状态，并说明恢复默认后的状态', async () => {
    vi.mocked(admin.inspectionChecks).mockResolvedValue([{ ...definition, pricing: true }])
    const wrapper = await adminPage(InspectionChecksView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.get<HTMLSelectElement>('#check-pricing').element.value).toBe('true')
    expect(wrapper.text()).toContain('恢复默认后：关')
    wrapper.unmount()
  })

  it('按目录流程展示中文流程列', async () => {
    vi.mocked(admin.inspectionChecks).mockResolvedValue([
      { ...definition, key: 'D07', flow: 'intelligence' },
      definition,
      { ...definition, key: 'D13', flow: 'common' },
    ])
    const wrapper = await adminPage(InspectionChecksView)
    expect(wrapper.text()).toContain('所属流程')
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0]!.text()).toContain('测智')
    expect(rows[1]!.text()).toContain('身份与功能核验')
    expect(rows[2]!.text()).toContain('通用')
    wrapper.unmount()
  })

  it('数值输入回填并保存两档系数，同时保留已有其他系数', async () => {
    vi.mocked(admin.inspectionChecks).mockResolvedValue([{ ...definition, key: 'D10', coefficients: { warning: 0.97, severe: 0.93, worst: 0.85, bonus: 1.05 } }])
    const wrapper = await adminPage(InspectionChecksView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.get<HTMLInputElement>('#check-warning').element.value).toBe('0.97')
    expect(wrapper.get<HTMLInputElement>('#check-severe').element.value).toBe('0.93')
    await wrapper.get('#check-warning').setValue(0.9)
    await wrapper.get('#check-severe').setValue(0.75)
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateInspectionCheck).toHaveBeenCalledWith('D10', expect.objectContaining({ coefficients: { warning: 0.9, severe: 0.75, worst: 0.85, bonus: 1.05 } }))
    wrapper.unmount()
  })

  it.each(['', '0', '1.06'])('无效系数 %s 不能保存', async (value) => {
    const wrapper = await adminPage(InspectionChecksView)
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    await wrapper.get('#check-warning').setValue(value)
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    expect(admin.updateInspectionCheck).not.toHaveBeenCalled()
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

})
