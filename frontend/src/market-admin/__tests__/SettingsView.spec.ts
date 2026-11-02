import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import SettingsView from '../SettingsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))

const values = {
  commission_rate: 0.2,
  freeze_hours: 24,
  withdraw_min: 10,
  quote_validity_days: 7,
  manual_retest_daily_limit: 3,
  refund_lock_seconds: 5,
}

let failLoad = false

async function page() {
  const wrapper = mount(SettingsView, { global: { stubs: { Icon: true } } })
  await flushPromises()
  return wrapper
}

function saveButton(wrapper: VueWrapper) {
  return wrapper.findAll('button').find((button) => button.text() === '保存')!
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  failLoad = false
  api.get.mockImplementation(async () => {
    if (failLoad) throw new Error('市场设置读取失败')
    return structuredClone(values)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)

describe('市场设置页', () => {
  it('首次加载失败禁止保存并显示错误和重试', async () => {
    failLoad = true
    const wrapper = await page()
    expect(wrapper.text()).toContain('市场设置读取失败')
    expect(wrapper.text()).toContain('重试')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    await saveButton(wrapper).trigger('click')
    expect(api.send).not.toHaveBeenCalled()
  })

  it('刷新失败保留最后成功表单值但禁止保存', async () => {
    const wrapper = await page()
    const input = wrapper.get('input.input')
    expect(input.element.value).toBe('0.2')
    failLoad = true
    await click(wrapper, '刷新')
    expect(wrapper.get('input.input').element.value).toBe('0.2')
    expect(wrapper.text()).toContain('市场设置读取失败')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
  })
})
