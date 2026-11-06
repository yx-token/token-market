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
  profit_control_enabled: true,
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

function profitControlBox() {
  const label = Array.from(dialog().querySelectorAll('label')).find((item) => item.textContent?.includes('开启利润控制'))
  expect(label, '找不到利润控制开关').toBeDefined()
  return label!.querySelector<HTMLInputElement>('input[type="checkbox"]')!
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

  it('回显最低毛利与安全缓冲，保存时原样提交', async () => {
    api.send.mockResolvedValue({})
    const wrapper = await page()
    await click(wrapper, '编辑')

    const values = Array.from(dialog().querySelectorAll<HTMLInputElement>('input')).map((item) => item.value)
    expect(values).toEqual(expect.arrayContaining(['1.2', '0.1', '0.02']))
    expect(values).not.toContain('undefined')
    await clickDialog('保存')

    expect(api.send).toHaveBeenCalledWith(
      'PUT',
      '/api/v1/admin/market/pools/1',
      expect.objectContaining({
        sell_multiplier: 1.2,
        min_margin: 0.1,
        safety_buffer: 0.02,
        profit_control_enabled: true,
        market_description: '原始介绍',
      }),
    )
  })

  it('关闭利润控制时保留最低毛利与安全缓冲一起提交', async () => {
    api.send.mockResolvedValue({})
    const wrapper = await page()
    await click(wrapper, '编辑')

    const box = profitControlBox()
    expect(box.checked).toBe(true)
    box.click()
    await flushPromises()
    await clickDialog('保存')

    expect(api.send).toHaveBeenCalledWith(
      'PUT',
      '/api/v1/admin/market/pools/1',
      expect.objectContaining({ profit_control_enabled: false, min_margin: 0.1, safety_buffer: 0.02 }),
    )
  })

  it('新建分组默认不开启利润控制', async () => {
    api.get.mockResolvedValue([])
    api.send.mockResolvedValue({})
    const wrapper = await page()
    await click(wrapper, '新建分组')

    expect(profitControlBox().checked).toBe(false)
    const [name, sell] = Array.from(dialog().querySelectorAll<HTMLInputElement>('input.input'))
    name.value = 'tm-pool-new'
    name.dispatchEvent(new Event('input', { bubbles: true }))
    sell.value = '0.8'
    sell.dispatchEvent(new Event('input', { bubbles: true }))
    await clickDialog('保存')

    expect(api.send).toHaveBeenCalledWith(
      'POST',
      '/api/v1/admin/market/pools',
      expect.objectContaining({ name: 'tm-pool-new', sell_multiplier: 0.8, profit_control_enabled: false }),
    )
  })

  it('列表缺少毛利/缓冲字段时不回显 undefined，也不静默提交', async () => {
    // 引擎侧曾用简表返回保障池，只有 id/name/platform/enabled/sell_multiplier 等几列。
    const {
      min_margin: _m,
      safety_buffer: _b,
      market_description: _d,
      profit_control_enabled: _p,
      ...thin
    } = structuredClone(pool)
    api.get.mockResolvedValue([thin])
    const wrapper = await page()
    await click(wrapper, '编辑')

    const values = Array.from(dialog().querySelectorAll<HTMLInputElement>('input')).map((item) => item.value)
    expect(values).not.toContain('undefined')
    await clickDialog('保存')

    expect(api.send).not.toHaveBeenCalled()
    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('最低毛利')
  })
})

describe('保障池列表的利润控制', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('开启时显示毛利/缓冲与寄售上限，关闭时显示「关闭」与「不限」', async () => {
    api.get.mockResolvedValue([
      structuredClone(pool),
      { ...structuredClone(pool), id: 2, name: 'tm-pool-free', profit_control_enabled: false },
    ])
    const wrapper = await page()
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('10% / 2%')
    expect(rows[0].text()).toContain('1.056')
    expect(rows[1].text()).toContain('关闭')
    expect(rows[1].text()).toContain('不限')
    expect(rows[1].text()).not.toContain('1.056')
  })

  it('启停分组时原样带回利润控制开关', async () => {
    api.get.mockResolvedValue([{ ...structuredClone(pool), profit_control_enabled: false }])
    api.send.mockResolvedValue({})
    const wrapper = await page()
    await click(wrapper, '关闭')

    expect(api.send).toHaveBeenCalledWith(
      'PUT',
      '/api/v1/admin/market/pools/1',
      expect.objectContaining({ enabled: false, profit_control_enabled: false, min_margin: 0.1, safety_buffer: 0.02 }),
    )
  })
})

describe('保障池删除', () => {
  function deleteButton(wrapper: VueWrapper) {
    const button = wrapper.findAll('button').find((item) => item.text() === '删除')
    expect(button, '找不到删除按钮').toBeDefined()
    return button!
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('启用中的分组不能直接删除，提示先关闭', async () => {
    api.get.mockResolvedValue([structuredClone(pool)])
    const wrapper = await page()

    const button = deleteButton(wrapper)
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toBe('需先关闭分组')
  })

  it('关闭后确认删除，调用 DELETE 并刷新列表', async () => {
    api.get.mockResolvedValueOnce([{ ...structuredClone(pool), enabled: false, market_published: false }])
    api.get.mockResolvedValueOnce([])
    api.send.mockResolvedValue({ status: 'deleted' })
    const wrapper = await page()

    await deleteButton(wrapper).trigger('click')
    await flushPromises()
    expect(dialog().textContent).toContain('tm-pool-openai')
    await clickDialog('确认删除')

    expect(api.send).toHaveBeenCalledWith('DELETE', '/api/v1/admin/market/pools/1')
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(document.querySelector('[role="dialog"]:not(.modal-leave-active)')).toBeNull()
    expect(wrapper.text()).toContain('分组 tm-pool-openai 已删除')
  })

  it('删除失败时在确认框内显示错误', async () => {
    api.get.mockResolvedValue([{ ...structuredClone(pool), enabled: false, market_published: false }])
    api.send.mockRejectedValue(new Error('disable the pool before deleting it'))
    const wrapper = await page()

    await deleteButton(wrapper).trigger('click')
    await flushPromises()
    await clickDialog('确认删除')

    expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('disable the pool before deleting it')
    expect(api.get).toHaveBeenCalledOnce()
  })
})
