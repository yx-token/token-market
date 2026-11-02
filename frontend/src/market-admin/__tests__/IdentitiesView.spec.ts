import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import IdentitiesView from '../IdentitiesView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const rows = [
  { market_user_id: 1, email: 'first@example.com', is_admin: true, market_status: 'active', linked: false },
  { market_user_id: 2, email: 'second@example.com', is_admin: true, market_status: 'active', linked: false },
]

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockResolvedValue(structuredClone(rows))
})

enableAutoUnmount(afterEach)

describe('身份同步忙碌状态', () => {
  it('一行同步时禁用其他行，完成后才恢复', async () => {
    const pending = deferred<{ email: string; engine_user_id: number }>()
    api.send.mockReturnValue(pending.promise)
    const wrapper = mount(IdentitiesView, { global: { stubs: { Icon: true, Pagination: true } } })
    await flushPromises()
    const buttons = wrapper.findAll('tbody button')
    expect(buttons).toHaveLength(2)
    expect(buttons[0]!.attributes('aria-label')).toContain('first@example.com')
    expect(buttons[1]!.attributes('aria-label')).toContain('second@example.com')

    await buttons[0]!.trigger('click')
    expect(api.send).toHaveBeenCalledTimes(1)
    expect(buttons[1]!.attributes('disabled')).toBeDefined()
    await buttons[1]!.trigger('click')
    expect(api.send).toHaveBeenCalledTimes(1)

    pending.resolve({ email: 'first@example.com', engine_user_id: 11 })
    await flushPromises()
    expect(wrapper.findAll('tbody button')[1]!.attributes('disabled')).toBeUndefined()
  })

  it('密码输入使用包含邮箱的可访问名称', async () => {
    api.get.mockResolvedValue([{ ...rows[0], is_admin: false }])
    const wrapper = mount(IdentitiesView, { global: { stubs: { Icon: true, Pagination: true } } })
    await flushPromises()
    expect(wrapper.get('input[type="password"]').attributes('aria-label')).toContain('first@example.com')
  })
})
