import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PromoChannelsView from '../PromoChannelsView.vue'
import type { PromoChannel, PromoChannelList } from '../api'

const api = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), status: vi.fn(), remove: vi.fn(),
  clipboard: vi.fn(), qr: vi.fn(),
}))
vi.mock('../api', () => ({
  getPromoChannels: api.list,
  createPromoChannel: api.create,
  updatePromoChannel: api.update,
  setPromoChannelStatus: api.status,
  deletePromoChannel: api.remove,
}))
vi.mock('qrcode', () => ({ default: { toDataURL: api.qr } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const stats = { pv: 10, uv: 8, signups: 2, paying_users: 1, recharge_amount: '128.50', buyer_spend: '30.00', seller_users: 1 }
function channel(overrides: Partial<PromoChannel> = {}): PromoChannel {
  return {
    id: 1, name: 'V2EX 帖子', code: 'v2ex-post', landing_path: '/', note: '首轮推广',
    status: 'active', created_at: '2026-11-01T08:00:00Z', updated_at: '2026-11-01T08:00:00Z',
    stats: { ...stats }, ...overrides,
  }
}
let result: PromoChannelList

async function page() {
  const wrapper = mount(PromoChannelsView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, PromoChannelDetails: true, teleport: true, transition: false } },
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

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-05T18:00:00Z'))
  result = {
    from: '2026-10-08', to: '2026-11-06', items: [channel()], totals: { ...stats },
    organic: { ...stats, signups: 3 },
  }
  api.list.mockImplementation(async () => structuredClone(result))
  api.create.mockResolvedValue(channel())
  api.update.mockResolvedValue(channel())
  api.status.mockResolvedValue(channel())
  api.remove.mockResolvedValue(undefined)
  api.clipboard.mockResolvedValue(undefined)
  api.qr.mockResolvedValue('data:image/png;base64,promo')
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: api.clipboard } })
})
enableAutoUnmount(afterEach)
afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('推广渠道页面', () => {
  it('按北京时间默认近30天，展示字符串金额、转化率和自然流量对比', async () => {
    const wrapper = await page()
    expect(api.list).toHaveBeenCalledWith({ from: '2026-10-08', to: '2026-11-06' }, { signal: expect.any(AbortSignal) })
    expect(wrapper.text()).toContain('128.50')
    expect(wrapper.text()).toContain('25.0%')
    expect(wrapper.text()).toContain('50.0%')
    expect(wrapper.get('[data-test="organic-comparison"]').text()).toContain('渠道注册 2 / 自然注册 3')
  })

  it.each([
    ['/', '/?ch=v2ex-post'],
    ['/?tab=market', '/?tab=market&ch=v2ex-post'],
    ['/market?tab=all#offers', '/market?tab=all&ch=v2ex-post#offers'],
  ])('复制专属链接：%s', async (path, suffix) => {
    result.items = [channel({ landing_path: path })]
    const wrapper = await page()
    await click(wrapper, '复制链接')
    expect(api.clipboard).toHaveBeenCalledWith(`${window.location.origin}${suffix}`)
    expect(wrapper.text()).toContain('已复制「V2EX 帖子」的专属链接')
  })

  it('零分母显示破折号，已有数据的渠道不显示删除', async () => {
    result.items = [channel({ stats: { ...stats, uv: 0, signups: 0 } })]
    const wrapper = await page()
    const cells = wrapper.findAll('tbody td')
    expect(cells.filter((cell) => cell.text() === '—')).toHaveLength(2)
    expect(wrapper.findAll('button').some((button) => button.text() === '删除')).toBe(false)
  })

  it('items 为 null 时显示空态，仍可新建', async () => {
    api.list.mockResolvedValue({ ...result, items: null })
    const wrapper = await page()
    expect(wrapper.text()).toContain('暂无推广渠道')
    await click(wrapper, '新建渠道')
    expect(wrapper.get('[data-test="landing-path"]').element).toHaveProperty('value', '/')
  })

  it('停用先确认，取消不请求，确认后发 disabled', async () => {
    const wrapper = await page()
    await click(wrapper, '停用')
    expect(wrapper.get('[role="dialog"]').text()).toContain('停用后专属链接不再计数和归因')
    expect(api.status).not.toHaveBeenCalled()
    await click(wrapper, '取消')
    expect(api.status).not.toHaveBeenCalled()
    await click(wrapper, '停用')
    await click(wrapper, '确认停用')
    expect(api.status).toHaveBeenCalledWith(1, 'disabled')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('启用直接提交 active', async () => {
    result.items = [channel({ status: 'disabled' })]
    const wrapper = await page()
    await click(wrapper, '启用')
    expect(api.status).toHaveBeenCalledWith(1, 'active')
  })

  it('创建自动生成渠道码，409 保留输入并明确提示', async () => {
    api.create.mockRejectedValueOnce(Object.assign(new Error('conflict'), { code: 'PROMO_CODE_TAKEN', status: 409 }))
    const wrapper = await page()
    await click(wrapper, '新建渠道')
    await wrapper.get('[data-test="name"]').setValue('线下活动')
    await wrapper.get('[data-test="code"]').setValue('offline')
    await wrapper.get('#promo-channel-form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('渠道码已被占用')
    expect(wrapper.get('[data-test="name"]').element).toHaveProperty('value', '线下活动')
    await wrapper.get('[data-test="code"]').setValue('')
    await wrapper.get('#promo-channel-form').trigger('submit')
    await flushPromises()
    expect(api.create).toHaveBeenLastCalledWith({ name: '线下活动', landing_path: '/', note: '' })
  })

  it('编辑渠道码只读且不提交 code，完整回填名称路径备注', async () => {
    const wrapper = await page()
    await click(wrapper, '编辑')
    expect(wrapper.get('[data-test="code"]').attributes('readonly')).toBeDefined()
    expect(wrapper.get('[data-test="note"]').element).toHaveProperty('value', '首轮推广')
    await wrapper.get('[data-test="name"]').setValue('更新名称')
    await wrapper.get('#promo-channel-form').trigger('submit')
    await flushPromises()
    expect(api.update).toHaveBeenCalledWith(1, { name: '更新名称', landing_path: '/', note: '首轮推广' })
  })

  it.each(['https://example.com', '//example.com', '/\\evil', '/foo http:', '/%2fexample.com', '/%5cevil', '/HTTP:evil', '/%0aevil'])('拒绝非法落地页 %s', async (path) => {
    const wrapper = await page()
    await click(wrapper, '新建渠道')
    await wrapper.get('[data-test="name"]').setValue('测试渠道')
    await wrapper.get('[data-test="landing-path"]').setValue(path)
    await wrapper.get('#promo-channel-form').trigger('submit')
    await flushPromises()
    expect(api.create).not.toHaveBeenCalled()
    expect(wrapper.get('[role="dialog"]').text()).toContain('落地页路径需以单个 / 开头')
  })

  it('无数据才可删除，服务端409提示全量历史限制', async () => {
    result.items = [channel({ stats: { ...stats, pv: 0, signups: 0 } })]
    api.remove.mockRejectedValue(Object.assign(new Error('conflict'), { code: 'PROMO_CHANNEL_HAS_DATA', status: 409 }))
    const wrapper = await page()
    await click(wrapper, '删除')
    await click(wrapper, '确认删除')
    expect(api.remove).toHaveBeenCalledWith(1)
    expect(wrapper.get('[role="dialog"]').text()).toContain('已有访问或注册数据，无法删除')
  })

  it('排序跨分页使用数字金额', async () => {
    result.items = [
      ...Array.from({ length: 15 }, (_, index) => channel({ id: index + 1 })),
      channel({ id: 16, name: '金额更高', stats: { ...stats, recharge_amount: '200.00' } }),
    ]
    const wrapper = await page()
    expect(wrapper.text()).not.toContain('金额更高')
    const th = wrapper.findAll('th').find((item) => item.text() === '充值金额（元）')!
    await th.trigger('click')
    expect(wrapper.findAll('tbody tr')[0]!.text()).toContain('V2EX 帖子')
    await th.trigger('click')
    expect(wrapper.findAll('tbody tr')[0]!.text()).toContain('金额更高')
  })

  it('全部区间不传日期，自定义区间倒置时不请求', async () => {
    const wrapper = await page()
    await click(wrapper, '全部')
    expect(api.list).toHaveBeenLastCalledWith({}, { signal: expect.any(AbortSignal) })
    const count = api.list.mock.calls.length
    await wrapper.get('[data-test="from"]').setValue('2026-11-06')
    await wrapper.get('[data-test="to"]').setValue('2026-11-01')
    await wrapper.get('form.date-fields').trigger('submit')
    expect(api.list).toHaveBeenCalledTimes(count)
    expect(wrapper.text()).toContain('开始日期不能晚于结束日期')
  })

  it('加载失败显示重试，刷新失败保留上次数据', async () => {
    api.list.mockRejectedValueOnce(new Error('渠道读取失败'))
    const wrapper = await page()
    expect(wrapper.text()).toContain('渠道读取失败')
    expect(wrapper.text()).not.toContain('暂无推广渠道')
    await click(wrapper, '重试')
    expect(wrapper.text()).toContain('V2EX 帖子')
    api.list.mockRejectedValueOnce(new Error('刷新失败'))
    await click(wrapper, '刷新')
    expect(wrapper.text()).toContain('当前显示上次成功加载的数据')
    expect(wrapper.text()).toContain('V2EX 帖子')
  })

  it('二维码可查看和下载，不新增接口请求', async () => {
    const wrapper = await page()
    await click(wrapper, '二维码')
    expect(api.qr).toHaveBeenCalledWith(`${window.location.origin}/?ch=v2ex-post`, expect.objectContaining({ width: 640 }))
    expect(wrapper.get('[role="dialog"] img').attributes('src')).toBe('data:image/png;base64,promo')
    expect(wrapper.get('a[download]').attributes('download')).toBe('promo-v2ex-post.png')
    await wrapper.get('[role="dialog"] button.btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('已复制「V2EX 帖子」的专属链接')
  })

  it('二维码弹窗内复制失败提供可恢复提示', async () => {
    api.clipboard.mockRejectedValue(new Error('clipboard blocked'))
    const wrapper = await page()
    await click(wrapper, '二维码')
    await wrapper.get('[role="dialog"] button.btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('复制失败')
    expect(wrapper.get('[role="dialog"] input[readonly]').element).toHaveProperty('value', `${window.location.origin}/?ch=v2ex-post`)
  })

  it('筛选请求失败后，重试保留请求的日期条件', async () => {
    const wrapper = await page()
    api.list.mockRejectedValueOnce(new Error('读取失败'))
    await click(wrapper, '近 7 天')
    await click(wrapper, '重试')
    expect(api.list).toHaveBeenLastCalledWith({ from: '2026-10-31', to: '2026-11-06' }, { signal: expect.any(AbortSignal) })
  })

  it('保存中阻止重复请求和关闭表单，失败后可继续编辑', async () => {
    let rejectSave!: (error: Error) => void
    api.create.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectSave = reject }))
    const wrapper = await page()
    await click(wrapper, '新建渠道')
    await wrapper.get('[data-test="name"]').setValue('待保存')
    await wrapper.get('#promo-channel-form').trigger('submit')
    await wrapper.get('#promo-channel-form').trigger('submit')
    expect(api.create).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[role="dialog"] button.btn-secondary').attributes('disabled')).toBeDefined()
    rejectSave(new Error('保存失败，可重试'))
    await flushPromises()
    expect(wrapper.get('[role="dialog"]').text()).toContain('保存失败，可重试')
    expect(wrapper.get('[data-test="name"]').attributes('disabled')).toBeUndefined()
  })

  it('快速切换日期中止旧请求，旧响应不能覆盖新结果', async () => {
    let resolveOld!: (value: PromoChannelList) => void
    api.list.mockImplementationOnce(() => new Promise<PromoChannelList>((resolve) => { resolveOld = resolve }))
    const wrapper = await page()
    const signal = api.list.mock.calls[0][1].signal as AbortSignal
    await click(wrapper, '今天')
    expect(signal.aborted).toBe(true)
    resolveOld({ ...result, items: [channel({ name: '旧结果' })] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('旧结果')
    expect(wrapper.text()).toContain('V2EX 帖子')
  })
})
