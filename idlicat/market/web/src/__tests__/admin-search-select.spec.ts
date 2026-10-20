import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, type Component } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { admin, seller } from '@/api'
import type { DetectionProvider, Policy, ProbeItem, Upstream, VendorConfig } from '@/api/types'
import SearchSelect from '@/components/common/SearchSelect.vue'
import DetectionView from '@/views/admin/DetectionView.vue'
import ProbeItemsView from '@/views/admin/ProbeItemsView.vue'
import UsersView from '@/views/admin/UsersView.vue'
import BillingView from '@/views/admin/BillingView.vue'

vi.mock('@/api', () => ({
  admin: {
    policies: vi.fn(), providers: vi.fn(), settings: vi.fn(), vendors: vi.fn(), upstreams: vi.fn(), items: vi.fn(),
    upsertPolicy: vi.fn(), references: vi.fn(), inspectionChecks: vi.fn(), probeGeneratorTemplates: vi.fn(),
    pendingKYC: vi.fn(), orgUsers: vi.fn(), anomalies: vi.fn(),
  },
  seller: { vendors: vi.fn() },
}))

const policy: Policy = {
  id: 1, platform: 'openai', auth_type: 'apikey', enabled: true, scheduled_enabled: true,
  interval_minutes: 360, jitter_minutes: 30, suites: ['availability', 'latency', 'iq', 'reasoning', 'long_context'],
  models: ['gpt-6-astra'], daily_budget_usd: 1, thresholds: { h0: 0.9, h1: 0.7, alpha: 0.05, beta: 0.1 },
  auto_pause: true, default_grade: 'B', version: 1, updated_at: '',
  third_party: [{ provider: 'test', triggers: ['admin'], model: 'third-party-model', on_mismatch: 'degraded', on_submodel_mismatch: 'suspected' }],
}
const item: ProbeItem = {
  id: 1, suite: 'iq', category: 'math', prompt: '测试题', grader: { type: 'exact', answer: '1' },
  platforms: ['openai'], models: ['item-model'], max_tokens: 1024, status: 'active', version: 1,
}
const vendor: VendorConfig = {
  platform: 'openai', enabled: true, auth_types: ['apikey'], supported_auth_types: ['apikey'],
  consignment_min: 0.1, consignment_max: 2, buyback_base: {}, appraisal_base: {}, default_concurrency: 2,
  extra_limits: {}, updated_at: '',
}

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

async function page(component: Component) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/test', component }] })
  await router.push('/admin/test')
  await router.isReady()
  const wrapper = mount(component, { attachTo: document.body, global: { plugins: [createPinia(), router] } })
  await flushPromises()
  return wrapper
}

async function button(wrapper: VueWrapper, text: string) {
  await wrapper.findAll('button').find((candidate) => candidate.text() === text)!.trigger('click')
  await flushPromises()
}

function selectComponent(wrapper: VueWrapper, id: string) {
  return wrapper.findAllComponents(SearchSelect).find((component) => component.props('id') === id)!
}

async function open(id: string) {
  const trigger = document.getElementById(id)!
  vi.spyOn(trigger, 'getBoundingClientRect').mockReturnValue(new DOMRect(100, 100, 320, 36))
  trigger.click()
  await flushPromises()
  const panel = document.getElementById(`${id}-listbox`)!
  expect(panel.style.width).toBe('320px')
  return panel
}

async function outside(id: string) {
  document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  await flushPromises()
  expect(document.getElementById(`${id}-listbox`)).toBeNull()
  // 点击外部只关闭浮层，页面弹窗仍在。
  expect(document.querySelector('[role="dialog"]')).toBeTruthy()
}

/** 实际页面绑定下，键盘选择更新 v-model，多选保持展开，单选收起。 */
async function choose(wrapper: VueWrapper, id: string, expectedValue: string) {
  const component = selectComponent(wrapper, id)
  const before = component.props('modelValue')
  const multiple = component.props('multiple')
  const panel = await open(id)
  const search = panel.querySelector<HTMLInputElement>('.ss-search')!
  search.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  search.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
  await nextTick()
  search.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }))
  search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  await flushPromises()
  const previous = Array.isArray(before) ? before : []
  const expected = multiple
    ? previous.includes(expectedValue) ? previous.filter((value) => value !== expectedValue) : [...previous, expectedValue]
    : expectedValue
  expect(component.props('modelValue')).toEqual(expected)
  expect(Boolean(document.getElementById(`${id}-listbox`))).toBe(Boolean(multiple))
  if (!multiple) await open(id)
  await outside(id)
}

async function enter(id: string, value: string) {
  const panel = await open(id)
  const search = panel.querySelector<HTMLInputElement>('.ss-search')!
  search.value = value
  search.dispatchEvent(new Event('input'))
  await flushPromises()
  search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  await flushPromises()
}

async function rejectUnknown(wrapper: VueWrapper, id: string) {
  const component = selectComponent(wrapper, id)
  const value = component.props('modelValue')
  const before = Array.isArray(value) ? [...value] : value
  expect(component.props('creatable')).toBe(false)
  await enter(id, 'unknown-id-987654')
  expect(component.props('modelValue')).toEqual(before)
  await outside(id)
}

describe('全部 SearchSelect 使用处的交互回归', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.policies).mockResolvedValue([policy, { ...policy, id: 2, platform: 'anthropic', models: ['other-policy-model'] }])
    vi.mocked(admin.providers).mockResolvedValue([{ name: 'test', label: '测试检测站', enabled: true } as DetectionProvider])
    vi.mocked(admin.settings).mockResolvedValue({ price_models: ['price-model', 'gpt-6-astra', 'price-model'] } as Awaited<ReturnType<typeof admin.settings>>)
    vi.mocked(admin.vendors).mockResolvedValue([vendor])
    vi.mocked(admin.upstreams).mockResolvedValue([{ id: 42, platform: 'openai', account_email: 'upstream@test.example' } as Upstream])
    vi.mocked(admin.items).mockResolvedValue([item])
    vi.mocked(admin.upsertPolicy).mockResolvedValue(policy)
    vi.mocked(admin.references).mockResolvedValue([])
    vi.mocked(admin.inspectionChecks).mockResolvedValue([])
    vi.mocked(admin.probeGeneratorTemplates).mockResolvedValue([])
    vi.mocked(seller.vendors).mockResolvedValue([])
    vi.mocked(admin.pendingKYC).mockResolvedValue([])
    vi.mocked(admin.anomalies).mockResolvedValue([])
    vi.mocked(admin.orgUsers).mockResolvedValue([
      { id: 42, email: 'buyer@test.example', is_admin: false, status: 'active', created_at: '' },
      { id: 7, email: 'seller@test.example', is_admin: false, status: 'active', created_at: '' },
    ])
  })

  it('DetectionView：所有策略字段可用，模型来源去重合并，自定义模型能保存', async () => {
    const wrapper = await page(DetectionView)
    await button(wrapper, '编辑')
    const modelSelect = selectComponent(wrapper, 'intelligence-model-0')
    expect(modelSelect.props('options').map((option) => option.value)).toEqual([
      'price-model', 'gpt-6-astra', 'third-party-model', 'other-policy-model', 'item-model',
    ])
    expect(document.getElementById('intelligence-model-0')!.textContent).not.toContain('未注册')
    expect(admin.items).toHaveBeenCalledWith()
    await choose(wrapper, 'intelligence-model-0', 'price-model')
    await choose(wrapper, 'intelligence-full-triggers', 'onboarding')
    await rejectUnknown(wrapper, 'intelligence-full-triggers')
    await choose(wrapper, 'tp-model-test', 'price-model')
    await enter('intelligence-model-0', 'new-policy-model')
    expect(modelSelect.props('modelValue')).toBe('new-policy-model')
    await outside('intelligence-model-0')
    await enter('tp-model-test', 'new-third-party-model')
    expect(selectComponent(wrapper, 'tp-model-test').props('modelValue')).toBe('new-third-party-model')
    document.querySelector<HTMLElement>('.yh-modal-foot .btn-primary')!.click()
    await flushPromises()
    expect(admin.upsertPolicy).toHaveBeenCalledWith('openai', 'apikey', expect.objectContaining({
      flows: expect.objectContaining({ intelligence: expect.objectContaining({ models: [{ model: 'new-policy-model', expected_pass_rate: null }] }) }),
      third_party: [expect.objectContaining({ model: 'new-third-party-model' })],
    }))
  })

  it('DetectionView：厂商、接入方式与上游单选仍受选项约束', async () => {
    const wrapper = await page(DetectionView)
    await button(wrapper, '新建策略')
    expect(selectComponent(wrapper, 'na').props('disabled')).toBe(true)
    await choose(wrapper, 'np', 'openai')
    await rejectUnknown(wrapper, 'np')
    await choose(wrapper, 'na', 'apikey')
    await rejectUnknown(wrapper, 'na')
    document.querySelector<HTMLElement>('.yh-modal-close')!.click()
    await flushPromises()
    await button(wrapper, '单个上游')
    await open('ou')
    document.querySelector<HTMLElement>('#ou-listbox .ss-opt')!.click()
    await flushPromises()
    expect(selectComponent(wrapper, 'ou').props('modelValue')).toBe('42')
    await open('ou')
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await flushPromises()
    expect(document.getElementById('ou-listbox')).toBeNull()
    expect(selectComponent(wrapper, 'ou').props('creatable')).toBe(false)
  })

  it('DetectionView：题库或价目接口失败时仍保留策略中的模型', async () => {
    vi.mocked(admin.items).mockRejectedValue(new Error('无题库权限'))
    vi.mocked(admin.settings).mockRejectedValue(new Error('加载失败'))
    const wrapper = await page(DetectionView)
    await button(wrapper, '编辑')
    expect(selectComponent(wrapper, 'intelligence-model-0').props('options').map((option) => option.value)).toContain('gpt-6-astra')
    expect(document.getElementById('intelligence-model-0')!.textContent).not.toContain('未注册')
    await enter('intelligence-model-0', 'new-model')
    expect(selectComponent(wrapper, 'intelligence-model-0').props('modelValue')).toBe('new-model')
  })

  it('ProbeItemsView：厂商多选受约束，模型多选允许自定义，质检项仍用注册表', async () => {
    const wrapper = await page(ProbeItemsView)
    await button(wrapper, '编辑')
    expect(document.getElementById('icheck')!.tagName).toBe('SELECT')
    expect(document.getElementById('imodels')!.textContent).toContain('item-model（自定义）')
    await choose(wrapper, 'iplat', 'openai')
    await rejectUnknown(wrapper, 'iplat')
    await choose(wrapper, 'imodels', 'price-model')
    await enter('imodels', 'new-item-model')
    expect(selectComponent(wrapper, 'imodels').props('modelValue')).toContain('new-item-model')
    await outside('imodels')
  })

  it.each([
    [UsersView, '冻结 / 解冻', 'uid'],
    [BillingView, '人工调账', 'au'],
  ] as const)('%s：远程用户单选、键盘与外部关闭正常，拒绝任意 ID', async (component, action, id) => {
    const wrapper = await page(component)
    await button(wrapper, action)
    await choose(wrapper, id, '42')
    expect(admin.orgUsers).toHaveBeenCalledWith('')
    expect(document.getElementById(id)!.textContent).toContain('buyer@test.example')
    await rejectUnknown(wrapper, id)
    expect(selectComponent(wrapper, id).props('modelValue')).toBe('42')
  })
})
