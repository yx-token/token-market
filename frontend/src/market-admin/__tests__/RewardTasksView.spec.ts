import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import RewardTasksView from '../RewardTasksView.vue'
import type { RewardEvent, RewardTaskConfig, RewardTicket } from '../api'

const api = vi.hoisted(() => ({
  config: vi.fn(), save: vi.fn(), ping: vi.fn(), tickets: vi.fn(), events: vi.fn(), retry: vi.fn(),
}))
vi.mock('../api', () => ({
  getRewardTaskConfig: api.config,
  saveRewardTaskConfig: api.save,
  pingRewardTasks: api.ping,
  getRewardTickets: api.tickets,
  getRewardEvents: api.events,
  retryRewardEvent: api.retry,
  REWARD_TICKET_STATES: ['pending', 'active', 'completed', 'skipped', 'invalid'],
  REWARD_EVENT_STATES: ['pending', 'accepted', 'completed', 'ignored', 'rejected', 'failed'],
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

function config(overrides: Partial<RewardTaskConfig> = {}): RewardTaskConfig {
  return {
    enabled: false, base_url: 'https://loxo.cc', app_key: '', secret_set: false, sdk_enabled: true, updated_at: null,
    summary: {
      tickets: { pending: 1, active: 2, completed: 3, skipped: 0, invalid: 0 },
      events: { pending: 4, accepted: 0, completed: 5, ignored: 0, rejected: 1, failed: 1 },
    },
    ...overrides,
  }
}

const event: RewardEvent = {
  id: 7, ticket_id: 2, ticket: 'rt_0123456789abcdef0123456789abcdef01234567', user_id: 9, user_email: 'buyer@example.com',
  task_title: '充值满 100', event_id: 'tm-rc-3-2', event_type: 'recharge', recharge_order_id: 3, amount: '30.00',
  state: 'rejected', reason: 'REWARD_TICKET_EXPIRED', rewarded: false, attempts: 1,
  next_attempt_at: '2026-11-06T00:00:00Z', sent_at: null, created_at: '2026-11-06T00:00:00Z',
}
const ticket: RewardTicket = {
  id: 2, ticket: event.ticket, user_id: 9, user_email: 'buyer@example.com', new_user: false, state: 'skipped', reason: 'NOT_NEW_USER',
  task_id: 1, task_title: '注册闲猫', task_type: 'register', period_key: 'once', expires_at: null, attempts: 1,
  created_at: '2026-11-06T00:00:00Z', updated_at: '2026-11-06T00:00:00Z',
}

async function page() {
  const wrapper = mount(RewardTasksView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, teleport: true, transition: false } },
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  api.config.mockResolvedValue(config())
  api.save.mockImplementation(async (input) => config({ ...input, secret_set: true }))
  api.ping.mockResolvedValue({ partner_name: '闲猫', app_key: 'rk_live', server_time: 1, clock_skew_seconds: 2 })
  api.tickets.mockResolvedValue({ items: [ticket], total: 1 })
  api.events.mockResolvedValue({ items: [event], total: 1 })
  api.retry.mockResolvedValue({ ...event, state: 'pending' })
})
enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
})

describe('任务对接页面', () => {
  it('加载配置、汇总与回调事件，不回显 secret', async () => {
    const wrapper = await page()
    expect(api.events).toHaveBeenCalledWith('', 1, 20, expect.anything())
    expect(wrapper.text()).toContain('未启用')
    expect(wrapper.text()).toContain('被拒 / 失败事件')
    expect(wrapper.text()).toContain('票据已过期（REWARD_TICKET_EXPIRED）')
    expect(wrapper.text()).toContain('buyer@example.com')
    expect((wrapper.get('[data-test="app-secret"]').element as HTMLInputElement).value).toBe('')
    // 没有已保存的 secret 时不能测试连接
    expect(wrapper.get('[data-test="ping"]').attributes('disabled')).toBeDefined()
  })

  it('启用前必须填凭证；保存只提交表单字段，空 secret 表示不改', async () => {
    const wrapper = await page()
    await wrapper.get('[data-test="enabled"]').setValue(true)
    await wrapper.get('form#reward-config-form').trigger('submit')
    await flushPromises()
    expect(api.save).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('启用前请填写 App Key')

    await wrapper.get('[data-test="app-key"]').setValue(' rk_live ')
    await wrapper.get('[data-test="app-secret"]').setValue('rs_live_secret')
    await wrapper.get('form#reward-config-form').trigger('submit')
    await flushPromises()
    expect(api.save).toHaveBeenCalledWith({ enabled: true, base_url: 'https://loxo.cc', app_key: 'rk_live', app_secret: 'rs_live_secret', sdk_enabled: true })
    expect(wrapper.text()).toContain('配置已保存')
    expect((wrapper.get('[data-test="app-secret"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-test="app-secret"]').attributes('placeholder')).toContain('已设置')
  })

  it('测试连接展示 loxo 登记的站点名与时钟差', async () => {
    api.config.mockResolvedValue(config({ enabled: true, app_key: 'rk_live', secret_set: true }))
    const wrapper = await page()
    await wrapper.get('[data-test="ping"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="ping-result"]').text()).toContain('「闲猫」')
    expect(wrapper.get('[data-test="ping-result"]').text()).toContain('相差 2 秒')

    api.ping.mockRejectedValueOnce(new Error('REWARD_SIGNATURE_INVALID'))
    await wrapper.get('[data-test="ping"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('签名错误（检查 App Secret）')
  })

  it('被拒事件可以重试，切到票据页按状态筛选', async () => {
    const wrapper = await page()
    await wrapper.get('[data-test="retry-7"]').trigger('click')
    await flushPromises()
    expect(api.retry).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('事件 #7 已重新排队')

    await wrapper.get('[data-test="tab-tickets"]').trigger('click')
    await flushPromises()
    expect(api.tickets).toHaveBeenLastCalledWith('', 1, 20, expect.anything())
    expect(wrapper.text()).toContain('老用户登录，不计注册')
    await wrapper.get('[data-test="state-filter"]').setValue('skipped')
    await flushPromises()
    expect(api.tickets).toHaveBeenLastCalledWith('skipped', 1, 20, expect.anything())
  })
})
