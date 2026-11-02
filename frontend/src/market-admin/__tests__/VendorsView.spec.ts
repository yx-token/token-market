import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import VendorsView from '../VendorsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const vendor = {
  platform: 'openai',
  enabled: true,
  auth_types: ['api_key'],
  supported_auth_types: ['api_key', 'oauth'],
  consignment_min: 0.1,
  consignment_max: 0.3,
  buyback_base: { default: 0.15 },
  appraisal_base: { default: 0.2 },
  default_concurrency: 2,
  extra_limits: { burst: 3 },
}

async function page() {
  const wrapper = mount(VendorsView, {
    attachTo: document.body,
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

function dialog() {
  const element = document.querySelector<HTMLElement>('[role="dialog"]')
  expect(element).not.toBeNull()
  return element!
}

async function clickDialog(text: string) {
  const button = Array.from(dialog().querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent?.trim() === text,
  )
  expect(button, `找不到弹窗按钮：${text}`).toBeDefined()
  button!.click()
  await flushPromises()
}

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
})

describe('厂商编辑弹窗', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.get.mockResolvedValue([structuredClone(vendor)])
    api.send.mockRejectedValue(new Error('厂商保存失败'))
  })

  it('保存失败时在活动弹窗内播报错误并保留表单', async () => {
    const wrapper = await page()
    await click(wrapper, '编辑')

    const minimum = dialog().querySelector<HTMLInputElement>('input[inputmode="decimal"]')!
    minimum.value = '0.17'
    minimum.dispatchEvent(new Event('input', { bubbles: true }))
    await clickDialog('保存')

    expect(dialog().getAttribute('aria-modal')).toBe('true')
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('厂商保存失败')
    expect(dialog().querySelector<HTMLInputElement>('input[inputmode="decimal"]')?.value).toBe('0.17')
    expect(api.get).toHaveBeenCalledOnce()
  })
})
