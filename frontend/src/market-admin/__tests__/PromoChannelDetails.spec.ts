import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import PromoChannelDetails from '../PromoChannelDetails.vue'
import type { PromoChannel, PromoChannelDaily, PromoDateRange } from '../api'

const api = vi.hoisted(() => ({ daily: vi.fn(), users: vi.fn() }))
vi.mock('../api', () => ({ getPromoChannelDaily: api.daily, getPromoChannelUsers: api.users }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-chartjs', () => ({
  Line: { name: 'Line', props: ['data', 'options'], template: '<div data-test="daily-chart" />' },
}))

const stats = { pv: 10, uv: 8, signups: 3, paying_users: 1, recharge_amount: '125.50', buyer_spend: '12.00', seller_users: 0 }
const channel: PromoChannel = {
  id: 7, code: 'v2ex', name: 'V2EX 帖子', landing_path: '/', note: '', status: 'active',
  created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', stats,
}
const daily: PromoChannelDaily = { date: '2026-11-05', ...stats }
const user = { user_id: 42, email: 'channel@example.com', registered_at: '2026-11-04T17:30:00Z', recharge_amount: '128.50', buyer_spend: '12.20', is_seller: true }

function page(range: PromoDateRange = { from: '2026-10-07', to: '2026-11-05' }, selected: PromoChannel | null = channel) {
  return mount(PromoChannelDetails, {
    props: { channel: selected, range },
    global: { stubs: { teleport: true, Icon: true, Pagination: true } },
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((resolveValue, rejectValue) => { resolve = resolveValue; reject = rejectValue })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.clearAllMocks()
  api.daily.mockResolvedValue({ items: [daily] })
  api.users.mockResolvedValue({ items: [user], total: 45 })
})

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('推广渠道详情', () => {
  it('未选择渠道时不请求，打开时独立加载每日趋势和归因用户', async () => {
    const wrapper = page({}, null)
    expect(api.daily).not.toHaveBeenCalled()
    expect(api.users).not.toHaveBeenCalled()
    await wrapper.setProps({ channel })
    await flushPromises()

    expect(api.daily).toHaveBeenCalledWith(7, {}, { signal: expect.any(AbortSignal) })
    expect(api.users).toHaveBeenCalledWith(7, 1, 20, { signal: expect.any(AbortSignal) })
    expect(wrapper.text()).toContain('V2EX 帖子')
    expect(wrapper.text()).toContain('全部时间模式下，每日趋势默认展示近 30 天')
    expect(wrapper.text()).toContain('用户全部时间累计金额')
    expect(wrapper.text()).toContain('channel@example.com')
    expect(wrapper.text()).toContain('2026/11/05 01:30')
    expect(wrapper.text()).toContain('¥128.50')
    expect(wrapper.text()).toContain('卖家')
    expect(wrapper.findComponent({ name: 'Line' }).props('data')).toMatchObject({
      labels: ['2026-11-05'],
      datasets: [{ data: [8] }, { data: [3] }, { data: [125.5] }],
    })
  })

  it('分页请求服务器数据并在调整每页数量后回到第一页', async () => {
    const wrapper = page()
    await flushPromises()
    wrapper.findComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
    await flushPromises()
    expect(api.users).toHaveBeenLastCalledWith(7, 2, 20, { signal: expect.any(AbortSignal) })

    wrapper.findComponent({ name: 'Pagination' }).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(api.users).toHaveBeenLastCalledWith(7, 1, 50, { signal: expect.any(AbortSignal) })
    expect(api.daily).toHaveBeenCalledTimes(1)
  })

  it('items 为 null 时显示独立空状态且不渲染图表或分页', async () => {
    api.daily.mockResolvedValue({ items: null })
    api.users.mockResolvedValue({ items: null, total: 0 })
    const wrapper = page()
    await flushPromises()
    expect(wrapper.text()).toContain('暂无每日趋势')
    expect(wrapper.text()).toContain('暂无归因用户')
    expect(wrapper.find('[data-test="daily-chart"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'Pagination' }).exists()).toBe(false)
  })

  it('两个区块分别显示加载状态，用户先返回时不阻塞用户列表', async () => {
    const pendingDaily = deferred<{ items: PromoChannelDaily[] }>()
    const pendingUsers = deferred<{ items: typeof user[]; total: number }>()
    api.daily.mockReturnValue(pendingDaily.promise)
    api.users.mockReturnValue(pendingUsers.promise)
    const wrapper = page()
    expect(wrapper.text()).toContain('每日趋势加载中')
    expect(wrapper.findAll('section').every((section) => section.attributes('aria-busy') === 'true')).toBe(true)

    pendingUsers.resolve({ items: [user], total: 1 })
    await flushPromises()
    expect(wrapper.text()).toContain('channel@example.com')
    expect(wrapper.text()).toContain('每日趋势加载中')
    pendingDaily.resolve({ items: [daily] })
    await flushPromises()
    expect(wrapper.find('[data-test="daily-chart"]').exists()).toBe(true)
  })

  it('趋势失败不影响用户列表，重试仅重新请求趋势', async () => {
    api.daily.mockRejectedValueOnce(new Error('趋势服务暂不可用'))
    const wrapper = page()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('趋势服务暂不可用')
    expect(wrapper.text()).toContain('channel@example.com')
    await wrapper.findAll('button').find((button) => button.text() === '重试每日趋势')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="daily-chart"]').exists()).toBe(true)
    expect(api.users).toHaveBeenCalledTimes(1)
  })

  it('用户失败不影响趋势图，重试仅重新请求用户', async () => {
    api.users.mockRejectedValueOnce(new Error('用户列表超时'))
    const wrapper = page()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('用户列表超时')
    expect(wrapper.find('[data-test="daily-chart"]').exists()).toBe(true)
    await wrapper.findAll('button').find((button) => button.text() === '重试归因用户')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('channel@example.com')
    expect(api.daily).toHaveBeenCalledTimes(1)
  })

  it('超长区间取截止日倒推 366 个自然日并提示，改日期不重载用户', async () => {
    api.daily.mockResolvedValue({ items: [] })
    const wrapper = page({ from: '2024-01-01', to: '2026-11-05' })
    await flushPromises()
    expect(api.daily).toHaveBeenCalledWith(7, { from: '2025-11-05', to: '2026-11-05' }, { signal: expect.any(AbortSignal) })
    expect(wrapper.text()).toContain('最多展示 366 天')
    expect(wrapper.text()).toContain('2025-11-05 至 2026-11-05（北京时间）')

    await wrapper.setProps({ range: { from: '2025-11-05', to: '2026-11-05' } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('最多展示 366 天')
    expect(api.users).toHaveBeenCalledTimes(1)
  })

  it('仅传起始日期时以北京时间今天为截止日', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-11-04T17:00:00Z'))
    page({ from: '2024-01-01' })
    await flushPromises()
    expect(api.daily).toHaveBeenCalledWith(7, { from: '2025-11-05', to: '2026-11-05' }, { signal: expect.any(AbortSignal) })
  })

  it('切换渠道中止旧请求，旧响应不能覆盖新渠道详情', async () => {
    const staleDaily = deferred<{ items: PromoChannelDaily[] }>()
    const staleUsers = deferred<{ items: typeof user[]; total: number }>()
    api.daily.mockReturnValueOnce(staleDaily.promise)
    api.users.mockReturnValueOnce(staleUsers.promise)
    const wrapper = page()
    const dailySignal = api.daily.mock.calls[0]![2].signal as AbortSignal
    const usersSignal = api.users.mock.calls[0]![3].signal as AbortSignal
    await wrapper.setProps({ channel: { ...channel, id: 8, name: '新渠道' } })
    await flushPromises()
    expect(dailySignal.aborted).toBe(true)
    expect(usersSignal.aborted).toBe(true)
    staleDaily.resolve({ items: [{ ...daily, date: '2020-01-01', uv: 999 }] })
    staleUsers.resolve({ items: [{ ...user, email: 'stale@example.com' }], total: 1 })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale@example.com')
    expect(wrapper.findComponent({ name: 'Line' }).props('data').labels).toEqual(['2026-11-05'])
    expect(api.users).toHaveBeenLastCalledWith(8, 1, 20, { signal: expect.any(AbortSignal) })
  })

  it('关闭中止两个在途请求并忽略其失败，卸载也中止请求', async () => {
    const pendingDaily = deferred<{ items: PromoChannelDaily[] }>()
    const pendingUsers = deferred<{ items: typeof user[]; total: number }>()
    api.daily.mockReturnValue(pendingDaily.promise)
    api.users.mockReturnValue(pendingUsers.promise)
    const wrapper = page()
    const dailySignal = api.daily.mock.calls[0]![2].signal as AbortSignal
    const usersSignal = api.users.mock.calls[0]![3].signal as AbortSignal
    await wrapper.findAll('button').find((button) => button.text() === '关闭')!.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(dailySignal.aborted).toBe(true)
    expect(usersSignal.aborted).toBe(true)
    await wrapper.setProps({ channel: null })
    pendingDaily.reject(new Error('aborted'))
    pendingUsers.reject(new Error('aborted'))
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)

    const other = page()
    const otherDailySignal = api.daily.mock.calls.at(-1)![2].signal as AbortSignal
    const otherUsersSignal = api.users.mock.calls.at(-1)![3].signal as AbortSignal
    other.unmount()
    expect(otherDailySignal.aborted).toBe(true)
    expect(otherUsersSignal.aborted).toBe(true)
  })
})
