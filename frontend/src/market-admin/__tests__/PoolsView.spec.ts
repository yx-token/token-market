import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PoolsView from '../PoolsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const pool = {
  id: 1,
  name: 'tm-pool-openai',
  platform: 'openai',
  sub2api_group_id: 8,
  sell_multiplier: 1.2,
  min_margin: 0.1,
  safety_buffer: 0.02,
  enabled: true,
  market_published: true,
  market_title: 'OpenAI 保障',
  market_description: '原始介绍',
}

async function page() {
  const wrapper = mount(PoolsView, {
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

describe('保障池编辑弹窗', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.get.mockResolvedValue([structuredClone(pool)])
    api.send.mockRejectedValue(new Error('保障池保存失败'))
  })

  it('保存失败时在活动弹窗内播报错误并保留表单', async () => {
    const wrapper = await page()
    await click(wrapper, '编辑')

    const description = dialog().querySelector<HTMLTextAreaElement>('textarea')!
    description.value = '失败重试时不能丢失的介绍'
    description.dispatchEvent(new Event('input', { bubbles: true }))
    await clickDialog('保存')

    expect(dialog().getAttribute('aria-modal')).toBe('true')
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('保障池保存失败')
    expect(dialog().querySelector<HTMLTextAreaElement>('textarea')?.value).toBe('失败重试时不能丢失的介绍')
    expect(api.get).toHaveBeenCalledOnce()
  })
})
