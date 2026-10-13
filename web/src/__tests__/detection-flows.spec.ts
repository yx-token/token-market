import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { admin } from '@/api'
import type { DetectionProvider, Policy, Upstream } from '@/api/types'
import SearchSelect from '@/components/common/SearchSelect.vue'
import DetectionView from '@/views/admin/DetectionView.vue'

vi.mock('@/api', () => ({ admin: {
  policies: vi.fn(), providers: vi.fn(), settings: vi.fn(), vendors: vi.fn(), upstreams: vi.fn(), items: vi.fn(),
  upsertPolicy: vi.fn(), probeFlows: vi.fn(), setOverride: vi.fn(), deleteOverride: vi.fn(),
} }))
const policy: Policy = {
  id: 1, platform: 'openai', auth_type: 'apikey', enabled: true, scheduled_enabled: true,
  interval_minutes: 360, jitter_minutes: 30, suites: ['iq'], models: ['旧模型'], daily_budget_usd: 1,
  thresholds: { h0: 0.9, h1: 0.7, alpha: 0.05, beta: 0.1 }, auto_pause: true, default_grade: 'B',
  third_party: [{ provider: 'test', triggers: ['appeal'], model: '身份模型', format: 'openai', on_mismatch: 'degraded', on_submodel_mismatch: 'suspected' }], version: 1, updated_at: '',
  flows: {
    intelligence: { enabled: true, scheduled_enabled: true, interval_minutes: 360, triggers: ['scheduled', 'admin'], full_triggers: ['admin'], models: [{ model: 'gpt-6-astra', expected_pass_rate: null }, { model: 'gpt-6.1-sol', expected_pass_rate: 0.9 }] },
    identity: { enabled: true, source: 'third_party', scheduled_enabled: false, interval_minutes: 1440, triggers: ['onboarding', 'admin'], full_triggers: ['onboarding', 'admin'] },
  },
}
const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })
async function page() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/console/test', component: DetectionView }] })
  await router.push('/console/test'); await router.isReady()
  const wrapper = mount(DetectionView, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: dialog } } })
  await flushPromises()
  return wrapper
}
async function click(wrapper: VueWrapper, text: string) {
  await wrapper.findAll('button').find((b) => b.text() === text)!.trigger('click'); await flushPromises()
}
async function select(wrapper: VueWrapper, id: string, value: string | string[]) {
  wrapper.findAllComponents(SearchSelect).find((c) => c.props('id') === id)!.vm.$emit('update:modelValue', value); await flushPromises()
}
async function edit() { const wrapper = await page(); await click(wrapper, '编辑'); return wrapper }
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(admin.policies).mockResolvedValue([structuredClone(policy)])
  vi.mocked(admin.providers).mockResolvedValue([{ name: 'test', label: '测试检测站', enabled: true } as DetectionProvider])
  vi.mocked(admin.settings).mockResolvedValue({ price_models: [] } as unknown as Awaited<ReturnType<typeof admin.settings>>)
  vi.mocked(admin.vendors).mockResolvedValue([])
  vi.mocked(admin.upstreams).mockResolvedValue([{ id: 42, platform: 'openai' } as Upstream])
  vi.mocked(admin.items).mockResolvedValue([])
  vi.mocked(admin.upsertPolicy).mockResolvedValue(policy)
  vi.mocked(admin.probeFlows).mockResolvedValue({ run_id: 123 })
})

describe('独立检测流程策略', () => {
  it('回填两个流程与模型，列表展示来源和独立周期', async () => {
    const wrapper = await edit()
    expect(wrapper.text()).toContain('每 360 分钟')
    expect(wrapper.text()).toContain('第三方检测 · 定时关（1440 分钟）')
    expect(wrapper.get<HTMLInputElement>('#intelligence-enabled').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('#identity-scheduled').element.checked).toBe(false)
    expect(wrapper.get<HTMLInputElement>('#identity-interval').element.value).toBe('1440')
    expect(wrapper.get<HTMLInputElement>('#intelligence-rate-0').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('#intelligence-rate-1').element.value).toBe('0.9')
    for (const id of ['ps', 'pm', 'policy-full-triggers']) expect(wrapper.find(`#${id}`).exists()).toBe(false)
  })
  it('来源切换收起检测站，再切回保留配置', async () => {
    const wrapper = await edit()
    expect(wrapper.find('[data-testid="third-party-config"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="third-party-config"] input[type="checkbox"]')).toHaveLength(1)
    await wrapper.get('#identity-source-self').setValue()
    expect(wrapper.find('[data-testid="third-party-config"]').exists()).toBe(false)
    await wrapper.get('#identity-source-third-party').setValue()
    expect(wrapper.get<HTMLSelectElement>('#tp-format-test').element.value).toBe('openai')
    expect(wrapper.findAllComponents(SearchSelect).find((c) => c.props('id') === 'tp-model-test')!.props('modelValue')).toBe('身份模型')
  })
  it('序列化 flows，空通过率为 null，废弃字段不再提交', async () => {
    const wrapper = await edit()
    await wrapper.get('#intelligence-interval').setValue(720)
    await wrapper.get('#intelligence-rate-0').setValue(0.95)
    await wrapper.get('#intelligence-rate-1').setValue('')
    await wrapper.get('#identity-scheduled').setValue(true)
    await wrapper.get('#policy-parallelism').setValue(4)
    await wrapper.get('#policy-token-budgets').setValue('{"quick":{"input_tokens_per_side":80000,"output_tokens_per_side":160000}}')
    await select(wrapper, 'intelligence-triggers', ['admin', 'appeal'])
    await select(wrapper, 'identity-full-triggers', ['admin'])
    await click(wrapper, '保存')
    const body = vi.mocked(admin.upsertPolicy).mock.calls[0]![2]
    expect(body.flows).toEqual({
      intelligence: { ...policy.flows!.intelligence, interval_minutes: 720, triggers: ['admin', 'appeal'], models: [{ model: 'gpt-6-astra', expected_pass_rate: 0.95 }, { model: 'gpt-6.1-sol', expected_pass_rate: null }] },
      identity: { ...policy.flows!.identity, scheduled_enabled: true, full_triggers: ['admin'] },
    })
    for (const field of ['suites', 'models', 'interval_minutes', 'full_triggers']) expect(body).not.toHaveProperty(field)
    expect(body.item_parallelism).toBe(4)
    expect(body.token_budgets).toEqual({ quick: { input_tokens_per_side: 80000, output_tokens_per_side: 160000 } })
    expect(body.third_party).toEqual([expect.objectContaining({ triggers: ['onboarding', 'admin'], model: '身份模型' })])
    expect(policy.flows!.intelligence.interval_minutes).toBe(360)
  })
  it('保存自有题库时保留收起的检测站配置', async () => {
    const wrapper = await edit(); await wrapper.get('#identity-source-self').setValue(); await click(wrapper, '保存')
    expect(admin.upsertPolicy).toHaveBeenCalledWith('openai', 'apikey', expect.objectContaining({
      flows: expect.objectContaining({ identity: expect.objectContaining({ source: 'self' }) }),
      third_party: [expect.objectContaining({ provider: 'test', model: '身份模型', format: 'openai' })],
    }))
  })
  it.each([0, 1.01])('期望通过率 %s 超出契约范围时不提交', async (value) => {
    const wrapper = await edit(); await wrapper.get('#intelligence-rate-0').setValue(value); await click(wrapper, '保存')
    expect(admin.upsertPolicy).not.toHaveBeenCalled()
  })
  it('第三方需选已启用检测站，自有题库不受此限制', async () => {
    vi.mocked(admin.providers).mockResolvedValue([{ name: 'test', label: '测试检测站', enabled: false } as DetectionProvider])
    const wrapper = await edit(); await click(wrapper, '保存'); expect(admin.upsertPolicy).not.toHaveBeenCalled()
    await wrapper.get('#identity-source-self').setValue(); await click(wrapper, '保存'); expect(admin.upsertPolicy).toHaveBeenCalledOnce()
  })
  it('旧策略兼容回填后保存新 flows 字段', async () => {
    vi.mocked(admin.policies).mockResolvedValue([{ ...policy, flows: undefined }])
    const wrapper = await edit()
    expect(wrapper.findAllComponents(SearchSelect).find((c) => c.props('id') === 'intelligence-model-0')!.props('modelValue')).toBe('旧模型')
    await click(wrapper, '保存')
    expect(admin.upsertPolicy).toHaveBeenCalledWith('openai', 'apikey', expect.objectContaining({ flows: expect.objectContaining({ intelligence: expect.objectContaining({ models: [{ model: '旧模型', expected_pass_rate: null }] }) }) }))
  })
  it('立即检测默认全部流程，也能只选择身份流程，拒绝空选择', async () => {
    const wrapper = await page(); await click(wrapper, '单个上游'); await select(wrapper, 'ou', '42'); await click(wrapper, '立即检测')
    expect(admin.probeFlows).toHaveBeenLastCalledWith(42, ['intelligence', 'identity'])
    await select(wrapper, 'probe-flows', ['identity']); await click(wrapper, '立即检测')
    expect(admin.probeFlows).toHaveBeenLastCalledWith(42, ['identity'])
    await select(wrapper, 'probe-flows', []); await click(wrapper, '立即检测'); expect(admin.probeFlows).toHaveBeenCalledTimes(2)
  })
})
