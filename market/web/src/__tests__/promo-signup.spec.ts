import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { auth as authApi } from '@/api'
import { useLoginModal } from '@/stores/loginModal'
import { PROMO_STORAGE_KEY, VISITOR_STORAGE_KEY } from '@/utils/promo'
import LoginModal from '@/components/LoginModal.vue'
import RegisterView from '@/views/auth/RegisterView.vue'

vi.mock('@/api', () => ({
  auth: {
    register: vi.fn(),
    login: vi.fn(),
    consoleAccess: vi.fn(),
  },
}))

const TOUCHED_AT = '2026-11-05T08:00:00.000Z'

function primePromo() {
  localStorage.setItem(PROMO_STORAGE_KEY, JSON.stringify({ code: 'v2ex-post', touched_at: TOUCHED_AT }))
  localStorage.setItem(VISITOR_STORAGE_KEY, 'visitor-abc123')
}

async function makeRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/')
  return router
}

describe('注册时带上推广渠道', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    vi.mocked(authApi.register).mockResolvedValue({ id: 1, email: 'buyer@market.test' } as never)
    vi.mocked(authApi.login).mockResolvedValue({
      token: 'header.payload.sig',
      user: { id: 1, email: 'buyer@market.test' },
      expires_at: new Date(Date.now() + 3600 * 1000).toISOString(),
    } as never)
    vi.mocked(authApi.consoleAccess).mockResolvedValue({ is_admin: false, menu_paths: [], resource_ids: [] } as never)
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('独立注册页：带上归因字段，注册成功后清掉 tm_promo', async () => {
    primePromo()
    const router = await makeRouter()
    const wrapper = mount(RegisterView, { global: { plugins: [createPinia(), router] } })

    await wrapper.get('#email').setValue('buyer@market.test')
    await wrapper.get('#password').setValue('pw12345678')
    await wrapper.get('#confirm').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(authApi.register).toHaveBeenCalledWith('buyer@market.test', 'pw12345678', {
      channel_code: 'v2ex-post',
      visitor_id: 'visitor-abc123',
      channel_touched_at: TOUCHED_AT,
    })
    expect(authApi.login).toHaveBeenCalledWith('buyer@market.test', 'pw12345678')
    expect(localStorage.getItem(PROMO_STORAGE_KEY)).toBeNull()
    // 访客标识保留，后续访问仍是同一访客
    expect(localStorage.getItem(VISITOR_STORAGE_KEY)).toBe('visitor-abc123')
    wrapper.unmount()
  })

  it('独立注册页：没有渠道记录时第三个参数是空对象，注册不受影响', async () => {
    const router = await makeRouter()
    const wrapper = mount(RegisterView, { global: { plugins: [createPinia(), router] } })

    await wrapper.get('#email').setValue('buyer@market.test')
    await wrapper.get('#password').setValue('pw12345678')
    await wrapper.get('#confirm').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(authApi.register).toHaveBeenCalledWith('buyer@market.test', 'pw12345678', {})
    wrapper.unmount()
  })

  it('独立注册页：渠道记录过期时不再归因', async () => {
    localStorage.setItem(
      PROMO_STORAGE_KEY,
      JSON.stringify({ code: 'v2ex-post', touched_at: new Date(Date.now() - 31 * 24 * 60 * 60 * 1000).toISOString() }),
    )
    const router = await makeRouter()
    const wrapper = mount(RegisterView, { global: { plugins: [createPinia(), router] } })

    await wrapper.get('#email').setValue('buyer@market.test')
    await wrapper.get('#password').setValue('pw12345678')
    await wrapper.get('#confirm').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(authApi.register).toHaveBeenCalledWith('buyer@market.test', 'pw12345678', {})
    wrapper.unmount()
  })

  it('注册页：注册失败时不清 tm_promo（换条路再试仍能归因）', async () => {
    primePromo()
    vi.mocked(authApi.register).mockRejectedValue(new Error('邮箱已被占用'))
    const router = await makeRouter()
    const wrapper = mount(RegisterView, { global: { plugins: [createPinia(), router] } })

    await wrapper.get('#email').setValue('buyer@market.test')
    await wrapper.get('#password').setValue('pw12345678')
    await wrapper.get('#confirm').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(localStorage.getItem(PROMO_STORAGE_KEY)).not.toBeNull()
    wrapper.unmount()
  })

  it('登录弹窗的注册模式：同样带上归因字段并在成功后清除', async () => {
    primePromo()
    const pinia = createPinia()
    const router = await makeRouter()
    const wrapper = mount(LoginModal, {
      global: { plugins: [pinia, router], stubs: { Teleport: true } },
    })
    const modal = useLoginModal(pinia)
    modal.show({ mode: 'register' })
    await nextTick()

    await wrapper.get('#xm-login-email').setValue('buyer@market.test')
    await wrapper.get('#xm-login-pass').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(authApi.register).toHaveBeenCalledWith('buyer@market.test', 'pw12345678', {
      channel_code: 'v2ex-post',
      visitor_id: 'visitor-abc123',
      channel_touched_at: TOUCHED_AT,
    })
    expect(localStorage.getItem(PROMO_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(VISITOR_STORAGE_KEY)).toBe('visitor-abc123')
    wrapper.unmount()
  })

  it('登录弹窗的登录模式：不调用注册，也不动 tm_promo', async () => {
    primePromo()
    const pinia = createPinia()
    const router = await makeRouter()
    const wrapper = mount(LoginModal, {
      global: { plugins: [pinia, router], stubs: { Teleport: true } },
    })
    const modal = useLoginModal(pinia)
    modal.show({ mode: 'login' })
    await nextTick()

    await wrapper.get('#xm-login-email').setValue('buyer@market.test')
    await wrapper.get('#xm-login-pass').setValue('pw12345678')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(authApi.register).not.toHaveBeenCalled()
    expect(authApi.login).toHaveBeenCalledWith('buyer@market.test', 'pw12345678')
    expect(localStorage.getItem(PROMO_STORAGE_KEY)).not.toBeNull()
    wrapper.unmount()
  })
})
