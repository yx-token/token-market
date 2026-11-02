import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import RiskView from '../RiskView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const risk = {
  id: 41,
  kind: 'shared_device',
  severity: 'high',
  user_id: 7,
  detail: { evidence: 'a'.repeat(120) },
  status: 'open',
  created_at: '2026-01-01T00:00:00Z',
}
const anomaly = {
  id: 52,
  rule: 'negative_balance',
  buyer_user_id: 7,
  amount: -3,
  detail: { reason: 'duplicate' },
  status: 'open',
  created_at: '2026-01-01T00:00:00Z',
}

async function page() {
  const wrapper = mount(RiskView, {
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
    if (path.startsWith('/api/v1/admin/market/users')) return [{ id: 7, email: 'risk@example.com' }]
    if (path.startsWith('/api/v1/admin/market/risk-events')) return [structuredClone(risk)]
    if (path.startsWith('/api/v1/admin/market/anomalies')) return [structuredClone(anomaly)]
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('风控操作确认与详情', () => {
  it.each(['处理', '忽略'])('取消风控%s确认时不发送请求', async (action) => {
    const wrapper = await page()
    await click(wrapper, action)
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('risk@example.com')
    dialogButton('取消').click()
    await flushPromises()
    expect(api.send).not.toHaveBeenCalled()
  })

  it.each(['确认', '忽略'])('取消异常%s原因对话框时不发送请求', async (action) => {
    vi.spyOn(window, 'prompt').mockReturnValue(null)
    const wrapper = await page()
    await click(wrapper, '异常')
    await click(wrapper, action)
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('#52')
    expect(document.body.querySelector('[role="dialog"] textarea')).not.toBeNull()
    dialogButton('取消').click()
    await flushPromises()
    expect(api.send).not.toHaveBeenCalled()
  })

  it('异常确认后提交原因并显示已处理', async () => {
    const wrapper = await page()
    await click(wrapper, '异常')
    await click(wrapper, '确认')
    const textarea = document.body.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea')!
    textarea.value = '人工核对完成'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    dialogButton('确认处理').click()
    await flushPromises()
    expect(api.send).toHaveBeenCalledWith('POST', '/api/v1/admin/market/anomalies/52/resolve', {
      status: 'resolved',
      note: '人工核对完成',
    })
    expect(wrapper.text()).toContain('异常已处理')
  })

  it('长详情可通过按钮打开可聚焦对话框', async () => {
    const wrapper = await page()
    await click(wrapper, '查看详情')
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(dialog?.textContent).toContain(risk.detail.evidence)
    expect(dialog?.querySelector('.modal-content')).not.toBeNull()
  })
})
