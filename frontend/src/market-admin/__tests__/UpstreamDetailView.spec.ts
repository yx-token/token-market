import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import UpstreamDetailView from '../UpstreamDetailView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
const route = vi.hoisted(() => ({ params: { id: '7' } }))
const router = vi.hoisted(() => ({ push: vi.fn() }))

vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => router }))
vi.mock('@/stores/consoleTabs', () => ({ useConsoleTabs: () => ({ open: vi.fn(), close: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const baseUpstream = {
  id: 7,
  seller_id: 5,
  seller_email: 'seller@example.test',
  account_email: `${'account-'.repeat(20)}@example.test`,
  name: '待接入上游',
  note: '备注'.repeat(100),
  platform: 'openai',
  auth_type: 'oauth',
  status: 'draft',
}

async function page() {
  const wrapper = mount(UpstreamDetailView)
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
  api.get.mockImplementation(async (path: string) => {
    if (path === '/api/v1/admin/market/upstreams/7') return structuredClone(baseUpstream)
    if (path === '/api/v1/admin/market/vendors') return []
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({ auth_url: 'https://example.test/oauth' })
  vi.spyOn(window, 'open').mockReturnValue(null)
})

afterEach(() => {
  vi.restoreAllMocks()
})

enableAutoUnmount(afterEach)

describe('上游详情 OAuth 与长内容', () => {
  it('OAuth 启动期间禁止重复点击，失败后恢复并显示 alert', async () => {
    let rejectStart!: (reason: Error) => void
    api.send.mockImplementation(() => new Promise((_, reject) => { rejectStart = reject }))
    const wrapper = await page()
    const button = wrapper.findAll('button').find((item) => item.text() === '打开授权页')!
    await button.trigger('click')
    await button.trigger('click')
    expect(api.send).toHaveBeenCalledOnce()
    expect(button.attributes('disabled')).toBeDefined()
    rejectStart(new Error('OAuth 启动失败'))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('OAuth 启动失败')
    expect(wrapper.findAll('button').find((item) => item.text() === '打开授权页')!.attributes('disabled')).toBeUndefined()
  })

  it('Key 接入成功提示在详情刷新完成后保留', async () => {
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/upstreams/7') return { ...baseUpstream, auth_type: 'coding', platform: 'kimi' }
      if (path === '/api/v1/admin/market/vendors') return []
      throw new Error(`unexpected GET ${path}`)
    })
    api.send.mockResolvedValue({})
    const wrapper = await page()
    await wrapper.get('input[type="password"]').setValue('a'.repeat(20))
    await click(wrapper, '提交 Key')
    expect(wrapper.text()).toContain('Key 已接入')
  })

  it('长模型 ID 提供全文提示与断行类', async () => {
    const model = 'model-without-spaces-'.repeat(20)
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/upstreams/7') return { ...baseUpstream, status: 'selecting_models' }
      if (path === '/api/v1/admin/market/upstreams/7/models') return { catalog: [model], whitelist: [] }
      throw new Error(`unexpected GET ${path}`)
    })
    const wrapper = await page()
    const modelLabel = wrapper.get('.model-item span')
    expect(modelLabel.attributes('title')).toBe(model)
    expect(modelLabel.classes()).toContain('break-anywhere')
  })
})
