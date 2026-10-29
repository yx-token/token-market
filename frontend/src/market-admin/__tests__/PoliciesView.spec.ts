import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PoliciesView from '../PoliciesView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

type Flow = {
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  triggers: string[]
  full_triggers: string[]
  models?: { model: string; expected_pass_rate: number | null }[]
  source?: 'third_party' | 'self'
}

type Policy = {
  id: number
  platform: string
  auth_type: string
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  jitter_minutes: number
  models: string[]
  daily_budget_usd: number
  thresholds: { h0: number; h1: number; alpha: number; beta: number }
  auto_pause: boolean
  default_grade: string
  third_party: { provider: string; triggers: string[]; model?: string; format?: string; on_mismatch: string; on_submodel_mismatch: string }[] | null
  item_parallelism: number
  full_triggers: string[]
  token_budgets: Record<string, unknown>
  flows?: { intelligence: Flow; degradation: Flow; identity: Flow }
}

const policy: Policy = {
  id: 1,
  platform: 'openai',
  auth_type: 'apikey',
  enabled: true,
  scheduled_enabled: true,
  interval_minutes: 360,
  jitter_minutes: 30,
  models: ['旧模型'],
  daily_budget_usd: 1,
  thresholds: { h0: 0.9, h1: 0.7, alpha: 0.05, beta: 0.1 },
  auto_pause: true,
  default_grade: 'B',
  third_party: [{ provider: 'test', triggers: ['appeal'], model: '身份模型', format: 'openai', on_mismatch: 'degraded', on_submodel_mismatch: 'suspected' }],
  item_parallelism: 2,
  full_triggers: ['onboarding', 'admin'],
  token_budgets: {},
  flows: {
    intelligence: { enabled: true, scheduled_enabled: true, interval_minutes: 360, triggers: ['scheduled', 'admin'], full_triggers: ['admin'], models: [{ model: 'gpt-6-astra', expected_pass_rate: null }] },
    degradation: { enabled: true, scheduled_enabled: false, interval_minutes: 720, triggers: ['scheduled'], full_triggers: ['onboarding'], models: [{ model: 'degrade-model', expected_pass_rate: null }] },
    identity: { enabled: true, scheduled_enabled: false, interval_minutes: 1440, triggers: ['onboarding', 'admin'], full_triggers: ['onboarding', 'admin'], source: 'third_party' },
  },
}

let policies: Policy[]
let providers: { name: string; label: string; enabled: boolean }[]

async function page() {
  const wrapper = mount(PoliciesView, {
    global: { stubs: { Icon: true, Pagination: true, Select: true } },
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

async function edit() {
  const wrapper = await page()
  await click(wrapper, '编辑')
  return wrapper
}

function flowCheckbox(wrapper: VueWrapper, flow: number, scheduled = false) {
  const checkbox = wrapper.findAll('.flow-block')[flow]!.findAll('input[type="checkbox"]')[scheduled ? 1 : 0]
  expect(checkbox, `找不到流程 ${flow} 的复选框`).toBeDefined()
  return checkbox!
}

function flowInterval(wrapper: VueWrapper, flow: number) {
  return wrapper.findAll('.flow-block')[flow]!.get('input[type="number"]')
}

function policyRequests() {
  return api.get.mock.calls.filter(([path]) => path === '/api/v1/admin/market/detection-policies')
}

beforeEach(() => {
  vi.clearAllMocks()
  policies = [structuredClone(policy)]
  providers = [{ name: 'test', label: '测试检测站', enabled: true }]
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/detection-policies') return structuredClone(policies)
    if (path === '/api/v1/admin/market/detection-providers') return structuredClone(providers)
    if (path === '/api/v1/admin/market/vendors') return []
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)

describe('生产检测策略页', () => {
  it('保存体保留独立 flows 与第三方配置，成功后刷新列表并关闭弹窗', async () => {
    const wrapper = await edit()
    await click(wrapper, '保存')
    expect(api.send).toHaveBeenCalledOnce()
    const [method, path, body] = api.send.mock.calls[0]!
    expect(method).toBe('PUT')
    expect(path).toBe('/api/v1/admin/market/detection-policies/openai?auth_type=apikey')
    expect(body.flows).toEqual(policy.flows)
    expect(body.third_party).toEqual([expect.objectContaining({
      provider: 'test',
      triggers: ['onboarding', 'admin'],
      model: '身份模型',
      format: 'openai',
    })])
    expect(policyRequests()).toHaveLength(2)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('旧策略总检测关闭时仍按后端 legacy 口径开启测智与降智流程', async () => {
    policies = [{ ...structuredClone(policy), enabled: false, scheduled_enabled: false, flows: undefined, third_party: [] }]
    const wrapper = await edit()
    expect(flowCheckbox(wrapper, 0).element.checked).toBe(true)
    expect(flowCheckbox(wrapper, 1).element.checked).toBe(true)
  })

  it('总检测或总定时关闭时摘要不再显示每 N 分钟，并禁用流程定时', async () => {
    policies = [
      { ...structuredClone(policy), scheduled_enabled: false },
      { ...structuredClone(policy), id: 2, enabled: false, scheduled_enabled: false },
    ]
    const wrapper = await page()
    expect(wrapper.text()).toContain('总定时关')
    expect(wrapper.text()).toContain('总检测关')
    await click(wrapper, '编辑')
    for (const flow of [0, 1, 2]) expect(flowCheckbox(wrapper, flow, true).element.disabled).toBe(true)
    expect(wrapper.text()).toContain('定时执行需同时开启总检测、总定时检测，以及对应流程和流程定时')
  })

  it('流程定时开启但流程未启用时拒绝保存并展示错误', async () => {
    const wrapper = await edit()
    await flowCheckbox(wrapper, 0).setValue(false)
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('流程定时检测需要先启用该流程')
  })

  it.each([0, 9, 10.5])('周期 %s 非法时拒绝保存', async (interval) => {
    const wrapper = await edit()
    await flowInterval(wrapper, 0).setValue(interval)
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('检测周期需为至少 10 分钟的整数')
  })

  it('列表与编辑页标出停用检测站，没有已启用站时阻止保存', async () => {
    providers = [{ name: 'test', label: '测试检测站', enabled: false }]
    const wrapper = await page()
    expect(wrapper.text()).toContain('测试检测站（已停用）')
    await click(wrapper, '编辑')
    expect(wrapper.get('[data-testid="disabled-provider-warning"]').text()).toContain('测试检测站')
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('第三方身份核验需要选择至少一个已启用的检测站')
  })

  it('保存失败在弹窗内展示错误并保留编辑状态', async () => {
    api.send.mockRejectedValue(new Error('后端拒绝保存'))
    const wrapper = await edit()
    await click(wrapper, '保存')
    expect(wrapper.get('[role="dialog"]').text()).toContain('后端拒绝保存')
    expect(policyRequests()).toHaveLength(1)
  })

  it('自有题库隐藏第三方区块，保存仍保留原检测站配置', async () => {
    const wrapper = await edit()
    await wrapper.findAll('.flow-block')[2]!.get('select').setValue('self')
    expect(wrapper.find('[data-testid="third-party-config"]').exists()).toBe(false)
    await click(wrapper, '保存')
    const body = api.send.mock.calls[0]![2]
    expect(body.flows.identity.source).toBe('self')
    expect(body.third_party).toEqual([expect.objectContaining({ provider: 'test', model: '身份模型' })])
  })
})
