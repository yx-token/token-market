import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ExtrasView from '../ExtrasView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))

let failLoad = false

async function page() {
  const wrapper = mount(ExtrasView, { global: { stubs: { Icon: true } } })
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
    if (failLoad) throw new Error('货架配置读取失败')
    return { hot_searches: [{ id: 'keep-me' }], feed: { pin_pool_first: true }, version: 3 }
  })
  api.send.mockImplementation(async (_method: string, _path: string, body: unknown) => structuredClone(body))
})

enableAutoUnmount(afterEach)

describe('货架开关页', () => {
  it('首次加载失败禁止保存并显示错误和重试', async () => {
    failLoad = true
    const wrapper = await page()
    expect(wrapper.text()).toContain('货架配置读取失败')
    expect(wrapper.text()).toContain('重试')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    await saveButton(wrapper).trigger('click')
    expect(api.send).not.toHaveBeenCalled()
  })

  it('刷新失败保留最后成功数据但禁止保存', async () => {
    const wrapper = await page()
    expect(wrapper.get('input[type="checkbox"]').element.checked).toBe(true)
    failLoad = true
    await click(wrapper, '刷新')
    expect(wrapper.get('input[type="checkbox"]').element.checked).toBe(true)
    expect(wrapper.text()).toContain('货架配置读取失败')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
  })
})
