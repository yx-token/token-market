import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ReviewsView from '../ReviewsView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const review = {
  id: 18,
  buyer_id: 2,
  seller_id: 3,
  rating: 5,
  content: `详细评价 ${'长文本'.repeat(40)}`,
  tags: ['可靠'],
  status: 'visible',
  created_at: '2026-01-01T00:00:00Z',
}

async function page() {
  const wrapper = mount(ReviewsView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, Pagination: true } },
  })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const buttons = wrapper.findAll('button')
  const button = buttons.find((item) => item.text() === text) ?? buttons.find((item) => item.text().includes(text))
  expect(button, `找不到按钮：${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

function dialogButton(text: string) {
  const button = Array.from(document.body.querySelectorAll<HTMLButtonElement>('.modal-footer button'))
    .find((item) => item.textContent?.trim() === text)
  expect(button, `找不到对话框按钮：${text}`).toBeDefined()
  return button!
}

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockImplementation(async (path: string) => {
    if (path.startsWith('/api/v1/admin/market/users')) return []
    if (path.startsWith('/api/v1/admin/market/reviews')) return { items: [structuredClone(review)], total: 1 }
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('评价危险操作与详情', () => {
  it.each(['隐藏', '删除'])('取消%s原因对话框时不发送请求', async (action) => {
    vi.spyOn(window, 'prompt').mockReturnValue(null)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await page()
    await click(wrapper, action)
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('#18')
    expect(document.body.querySelector('[role="dialog"] textarea')).not.toBeNull()
    dialogButton('取消').click()
    await flushPromises()
    expect(api.send).not.toHaveBeenCalled()
  })

  it('确认隐藏后提交原因', async () => {
    const wrapper = await page()
    await click(wrapper, '隐藏')
    const textarea = document.body.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!
    textarea.value = '包含违规信息'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    dialogButton('确认隐藏').click()
    await flushPromises()
    expect(api.send).toHaveBeenCalledWith('POST', '/api/v1/admin/market/reviews/18/hide', { reason: '包含违规信息' })
  })

  it('完整评价与隐藏原因可在详情对话框中查看', async () => {
    const wrapper = await page()
    await click(wrapper, '查看详情')
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(dialog?.textContent).toContain(review.content)
  })
})
