import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { admin } from '@/api'
import type { InspectionSettings, ModelCapability } from '@/api/types'
import InspectionSettingsPanel from '@/components/InspectionSettingsPanel.vue'
import { effectiveCapability } from '@/components/inspectionCapabilities'

vi.mock('@/api', () => ({ admin: { inspectionSettings: vi.fn(), updateInspectionSettings: vi.fn() } }))

const capability: ModelCapability = {
  platform: 'openai', model: 'gpt-6-astra', context_window: 1050000, context_window_by_auth: { oauth: 272000 },
  max_output: 128000, max_output_unlimited: null, tools: true, json_schema: false, image: null, pdf: null,
  multiturn: null, thinking: true, prompt_cache: null, efforts: ['low', 'high', 'max'], source: '官方调研第2章',
}
const settings: InspectionSettings = {
  mode: 'trial', check_overrides: {}, model_capabilities: [], model_capability_defaults: [capability],
  grade_thresholds: { S: 1.02, A: .93, B: .82, C: .65 }, grade_hysteresis: .02, downgrade_confirmations: 2,
  s_requirements: { gate_free_days: 30, availability_days: 7, min_availability: .995 },
  scheduling: { enabled: false, minimum_grade: 'B', strategy: 'quality_adjusted' }, timeouts: { full_minutes: 75, quick_minutes: 20 },
  limits: { account_parallelism: 2, global_parallelism: 32 },
  token_budgets: { full: { input_tokens_per_side: 2000000, output_tokens_per_side: 640000 }, quick: { input_tokens_per_side: 80000, output_tokens_per_side: 160000 } },
}
async function panel(value = settings) {
  vi.mocked(admin.inspectionSettings).mockResolvedValue(structuredClone(value))
  const wrapper = mount(InspectionSettingsPanel, { global: { plugins: [createPinia()] } })
  await flushPromises()
  return wrapper
}
function edited(wrapper: Awaited<ReturnType<typeof panel>>): InspectionSettings {
  return JSON.parse(wrapper.get<HTMLTextAreaElement>('#inspection-advanced').element.value)
}

describe('模型官方能力设置', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.updateInspectionSettings).mockImplementation(async (body) => ({ ...body, model_capability_defaults: [capability] }))
  })

  it('显示支持、不支持、未登记三态和内置来源', async () => {
    const wrapper = await panel()
    expect(wrapper.text()).toContain('模型官方能力')
    expect(wrapper.text()).toContain('内置来源：官方调研第2章')
    expect(wrapper.get<HTMLSelectElement>('#capability-tools').element.value).toBe('true')
    expect(wrapper.get<HTMLSelectElement>('#capability-json_schema').element.value).toBe('false')
    expect(wrapper.get<HTMLSelectElement>('#capability-pdf').element.value).toBe('unknown')
    expect(wrapper.findAll('button').find((b) => b.text() === '恢复默认')!.attributes('disabled')).toBeDefined()
    expect(edited(wrapper)).not.toHaveProperty('model_capability_defaults')
    wrapper.unmount()
  })

  it('编辑能力、认证窗口与来源，保存覆盖并保留内置默认', async () => {
    const wrapper = await panel()
    await wrapper.get('#capability-json_schema').setValue('true')
    await wrapper.get('#capability-tools').setValue('unknown')
    await wrapper.get('#capability-context_window').setValue(1000000)
    await wrapper.get('#capability-max_output').setValue(64000)
    await wrapper.get('#capability-auth').setValue('{"oauth":300000}')
    await wrapper.get('#capability-efforts').setValue('low，高档')
    await wrapper.get('#capability-efforts').setValue('low, high, max')
    await wrapper.get('#capability-source').setValue('官方模型页新登记')
    const expected = { platform: 'openai', model: 'gpt-6-astra', json_schema: true, tools: null,
      context_window: 1000000, max_output: 64000, context_window_by_auth: { oauth: 300000 }, efforts: ['low', 'high', 'max'], source: '官方模型页新登记' }
    expect(edited(wrapper).model_capabilities).toEqual([expected])
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith(expect.objectContaining({ model_capabilities: [expected] }))
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith(expect.not.objectContaining({ model_capability_defaults: expect.anything() }))
    expect(wrapper.get<HTMLSelectElement>('#capability-tools').element.value).toBe('unknown')
    expect(wrapper.text()).toContain('内置来源：官方调研第2章')
    wrapper.unmount()
  })

  it('恢复当前型号默认，保留其他型号覆盖且同步高级 JSON', async () => {
    const other = { platform: 'openai', model: 'gpt-6-luna', pdf: true }
    const wrapper = await panel({ ...settings, model_capabilities: [
      { platform: 'openai', model: 'gpt-6-astra', tools: false, thinking: null, context_window_by_auth: { oauth: 100000 }, source: '管理员覆盖' }, other,
    ] })
    expect(wrapper.get<HTMLSelectElement>('#capability-tools').element.value).toBe('false')
    await wrapper.findAll('button').find((b) => b.text() === '恢复默认')!.trigger('click')
    expect(edited(wrapper).model_capabilities).toEqual([other])
    expect(wrapper.get<HTMLSelectElement>('#capability-tools').element.value).toBe('true')
    expect(wrapper.get<HTMLSelectElement>('#capability-thinking').element.value).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#capability-auth').element.value).toContain('272000')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(admin.updateInspectionSettings).toHaveBeenCalledWith(expect.objectContaining({ model_capabilities: [other] }))
    wrapper.unmount()
  })

  it('新增前缀覆盖并选择编辑，未知能力保持未登记', async () => {
    const wrapper = await panel()
    await wrapper.get('#capability-new-model').setValue('gpt-6-*')
    await wrapper.findAll('button').find((b) => b.text() === '添加型号')!.trigger('click')
    expect(edited(wrapper).model_capabilities).toEqual([{ platform: 'openai', model: 'gpt-6-*' }])
    expect(wrapper.get<HTMLSelectElement>('#capability-thinking').element.value).toBe('unknown')
    await wrapper.get('#capability-tools').setValue('false')
    expect(edited(wrapper).model_capabilities?.[0]?.tools).toBe(false)
    wrapper.unmount()
  })

  it('认证窗口格式错误时显示说明，不写入错误覆盖', async () => {
    const wrapper = await panel()
    await wrapper.get('#capability-auth').setValue('[]')
    expect(wrapper.get('[role="alert"]').text()).toContain('认证窗口须为 JSON 对象')
    expect(edited(wrapper).model_capabilities).toEqual([])
    wrapper.unmount()
  })

  it('高级 JSON 覆盖同步三态控件', async () => {
    const wrapper = await panel()
    await wrapper.get('#inspection-advanced').setValue(JSON.stringify({ ...edited(wrapper), model_capabilities: [{ platform: 'openai', model: 'gpt-6-astra', tools: false, pdf: true, thinking: null }] }))
    expect(wrapper.get<HTMLSelectElement>('#capability-tools').element.value).toBe('false')
    expect(wrapper.get<HTMLSelectElement>('#capability-pdf').element.value).toBe('true')
    expect(wrapper.get<HTMLSelectElement>('#capability-thinking').element.value).toBe('unknown')
    wrapper.unmount()
  })
})

describe('能力覆盖与后端口径一致', () => {
  it('按平台、最长前缀、精确型号与认证窗口合并', () => {
    const actual = effectiveCapability('openai', capability.model, [capability], [
      { platform: 'openai', model: capability.model, tools: false, context_window_by_auth: { oauth: 300000 } },
      { platform: 'openai', model: '*', tools: true, pdf: false },
      { platform: 'openai', model: 'gpt-6-*', image: true },
      { platform: 'openai', model: 'gpt-6-a*', image: false, thinking: null },
      { platform: 'gemini', model: '*', tools: true },
    ])
    expect(actual.tools).toBe(false)
    expect(actual.pdf).toBe(false)
    expect(actual.image).toBe(false)
    expect(actual.thinking).toBeNull()
    expect(actual.context_window_by_auth).toEqual({ oauth: 300000 })
    expect(actual.max_output).toBe(128000)
    expect(capability.context_window_by_auth).toEqual({ oauth: 272000 })
    expect(effectiveCapability('openai', capability.model, [capability], [{ platform: 'openai', model: capability.model, context_window_by_auth: null }]).context_window_by_auth).toBeNull()
  })
})
