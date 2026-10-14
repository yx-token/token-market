import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import ProbeItemsView from '@/views/admin/ProbeItemsView.vue'
import { admin, seller } from '@/api'
import type { ProbeItem } from '@/api/types'
import { useToast } from '@/stores/toast'

vi.mock('@/api', () => ({
  admin: { items: vi.fn(), references: vi.fn(), vendors: vi.fn(), createItem: vi.fn(), updateItem: vi.fn(), retireItem: vi.fn(), inspectionChecks: vi.fn(), settings: vi.fn(), probeGeneratorTemplates: vi.fn() },
  seller: { vendors: vi.fn() },
}))

const legacy: ProbeItem = { id: 1, suite: 'iq', category: 'math', prompt: '旧算术题', grader: { type: 'numeric', number: 391 }, platforms: [], models: [], max_tokens: 1024, status: 'active', version: 1, irt_difficulty: 0.35, irt_discrimination: 1.75 }
const generated: ProbeItem = {
  ...legacy, id: 2, suite: 'inspection_capability', prompt: '寄存器模板', check_key: 'A09',
  request: { system: '只给最终答案', effort: 'high' },
  generator: { template_id: 'register_machine', version: '1', parameters: { operations: [30, 60] }, instances: { full: 8, quick: 4 } },
  modes: ['full', 'quick'], builtin_key: 'inspection.register.v1', weight: 1.5,
}
const secondVersion: ProbeItem = { ...generated, id: 3, prompt: '新版寄存器模板', generator: { ...generated.generator, version: '2' } }
const context: ProbeItem = { ...generated, id: 4, suite: 'inspection_context', check_key: 'D09', prompt: '上下文模板', generator: { template_id: 'long_context', version: '1' }, modes: ['full'] }

const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
const listPage = defineComponent({ template: '<main><slot name="tabs" /><slot name="actions" /><slot name="filters" /><slot /><slot name="footer" /></main>' })

async function page() {
  const pinia = createPinia()
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/console/probe-items', component: { template: '<div />' } }] })
  await router.push('/console/probe-items')
  await router.isReady()
  const wrapper = mount(ProbeItemsView, { global: { plugins: [pinia, router], stubs: { ModalDialog: dialog, ConsoleListPage: listPage, OnboardingWizard: true } } })
  await flushPromises()
  return { wrapper, pinia }
}

describe('质检题库编辑', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.items).mockResolvedValue([legacy, generated, secondVersion, context])
    vi.mocked(admin.references).mockResolvedValue([])
    vi.mocked(admin.vendors).mockResolvedValue([])
    vi.mocked(seller.vendors).mockResolvedValue([])
    vi.mocked(admin.updateItem).mockResolvedValue(generated)
    vi.mocked(admin.createItem).mockResolvedValue(generated)
    vi.mocked(admin.inspectionChecks).mockResolvedValue([
      { key: 'D07', name: '智力测试', category: 'capability', role: 'factor', factor_group: 'capability', enabled: true, calibrated: false, coefficients: {}, thresholds: {}, advice: '', calibration_note: '' },
      { key: 'D09', name: '上下文深度', category: 'capability', role: 'factor', factor_group: 'capability', enabled: true, calibrated: false, coefficients: {}, thresholds: {}, advice: '', calibration_note: '' },
    ])
    vi.mocked(admin.settings).mockResolvedValue({ price_models: [] } as any)
    vi.mocked(admin.probeGeneratorTemplates).mockResolvedValue([])
  })

  it('按模板与版本查看新题，保留旧题显示', async () => {
    const { wrapper } = await page()
    expect(wrapper.text()).toContain('旧算术题')
    expect(wrapper.text()).toContain('测智·上下文深度')
    expect(wrapper.find('tbody').text()).toContain('旧智力题（已停用）')
    expect(wrapper.text()).toContain('register_machine / 1')
    expect(wrapper.find('option[value="tool_call"]').exists()).toBe(false)
    await wrapper.get('#item-template').setValue('register_machine')
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    await wrapper.get('#item-generator-version').setValue('1')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody').text()).toContain('寄存器模板')
    expect(wrapper.find('tbody').text()).not.toContain('新版寄存器模板')
    wrapper.unmount()
  })

  it('编辑时保留请求、生成器、模式、内置键和权重', async () => {
    const { wrapper } = await page()
    const row = wrapper.findAll('tbody tr').find((tr) => tr.find('td').text() === '2')!
    await row.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('#irequest').element.value).toContain('只给最终答案')
    expect(wrapper.get<HTMLTextAreaElement>('#igenerator').element.value).toContain('register_machine')
    expect(wrapper.get<HTMLSelectElement>('#icheck').element.value).toBe('A09')
    expect(wrapper.get('#icheck').text()).toContain('A09（未注册）')
    expect(wrapper.get('#icheck').text()).toContain('D07 · 智力测试')
    await wrapper.get('#icat').setValue('logic')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateItem).toHaveBeenCalledWith(2, expect.objectContaining({
      category: 'logic', check_key: generated.check_key, request: generated.request, generator: generated.generator,
      modes: generated.modes, builtin_key: generated.builtin_key, weight: generated.weight,
    }))
    wrapper.unmount()
  })

  it('编辑旧题使用兼容默认值，保存原有判分规则', async () => {
    const { wrapper } = await page()
    const row = wrapper.findAll('tbody tr').find((tr) => tr.find('td').text() === '1')!
    await row.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    expect(wrapper.get<HTMLSelectElement>('#isuite').element.value).toBe('iq')
    expect(wrapper.get('#isuite').text()).toContain('旧智力题（已停用）')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.updateItem).toHaveBeenCalledWith(1, expect.objectContaining({ grader: legacy.grader, request: {}, generator: {}, check_key: '', modes: [], weight: 1, irt_difficulty: legacy.irt_difficulty, irt_discrimination: legacy.irt_discrimination }))
    wrapper.unmount()
  })

  it('新建题目默认测智，套件选项不再提供旧智力题', async () => {
    const { wrapper } = await page()
    await wrapper.findAll('button').find((button) => button.text() === '添加题目')!.trigger('click')
    expect(wrapper.get<HTMLSelectElement>('#isuite').element.value).toBe('inspection_capability')
    const options = wrapper.get('#isuite').findAll('option')
    expect(options.map((option) => option.element.getAttribute('value'))).toEqual([
      'inspection_capability', 'inspection_context', 'inspection_degradation', 'inspection_authenticity',
      'inspection_function', 'inspection_output', 'inspection_performance',
    ])
    // 每个套件选项都显示中文名，不能把套件代码直接露给管理员
    for (const option of options) expect(option.text()).not.toMatch(/inspection_/)
    expect(wrapper.get('#isuite').text()).toContain('降智检测')
    expect(wrapper.get('#isuite').text()).not.toContain('已停用')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.createItem).toHaveBeenCalledWith(expect.objectContaining({ suite: 'inspection_capability' }))
    wrapper.unmount()
  })

  it.each([
    ['igrader', '[]', '判分规则需要是 JSON 对象'],
    ['irequest', 'null', '请求模板需要是 JSON 对象'],
    ['igenerator', '{"template_id":"long_context","version":1}', '生成器定义需要非空 template_id 和字符串 version'],
    ['irequest', '{', '请求模板需要是 JSON 对象'],
  ])('拒绝 %s 的无效 JSON 配置', async (field, value, message) => {
    const { wrapper, pinia } = await page()
    await wrapper.findAll('button').find((button) => button.text() === '添加题目')!.trigger('click')
    await wrapper.get(`#${field}`).setValue(value)
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.createItem).not.toHaveBeenCalled()
    expect(useToast(pinia).items.at(-1)?.text).toBe(message)
    wrapper.unmount()
  })

  it('添加新题提交质检字段，并沿用现有 API 错误提示', async () => {
    const { wrapper, pinia } = await page()
    await wrapper.findAll('button').find((button) => button.text() === '添加题目')!.trigger('click')
    await wrapper.get('#isuite').setValue('inspection_context')
    await wrapper.get('#icheck').setValue('D09')
    await wrapper.get('#iprompt').setValue('新上下文模板')
    await wrapper.get('#igenerator').setValue('{"template_id":"long_context","version":"1","parameters":{"target_tokens":8192},"instances":{"full":1,"quick":0}}')
    await wrapper.get('input[type="checkbox"][value="full"]').setValue(true)
    vi.mocked(admin.createItem).mockRejectedValue(new Error('题目参数不合法'))
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(admin.createItem).toHaveBeenCalledWith(expect.objectContaining({ suite: 'inspection_context', check_key: 'D09', modes: ['full'], generator: expect.objectContaining({ template_id: 'long_context', version: '1' }) }))
    expect(useToast(pinia).items.at(-1)?.text).toBe('题目参数不合法')
    expect(wrapper.find('#igenerator').exists()).toBe(true)
    wrapper.unmount()
  })
})
