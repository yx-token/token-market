import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { buyer, market } from '@/api'
import type { BuyerKey } from '@/api/types'
import ClientImportView from '@/views/buyer/ClientImportView.vue'
import { suggestKeyModels } from '@/utils/keyModelSuggestions'

vi.mock('@/api', () => ({
  buyer: { keys: vi.fn(), keySecret: vi.fn() },
  market: { listing: vi.fn(), config: vi.fn() },
}))

const SECRET = 'tm-901abcdefghijklmnopqrstuvwxyz0123456789'

function key(over: Partial<BuyerKey>): BuyerKey {
  return {
    id: 901,
    name: '写代码用',
    key_prefix: 'tm-ab12',
    route_type: 'default',
    status: 'active',
    limits: {},
    hops: [{ position: 1, listing_id: 7, state: 'ready' }],
    platform: 'anthropic',
    recoverable: true,
    created_at: '2026-11-01T10:00:00Z',
    ...over,
  } as BuyerKey
}

const copied: string[] = []

enableAutoUnmount(afterEach)
beforeEach(() => {
  copied.length = 0
  vi.mocked(market.config).mockResolvedValue({
    edge_url: 'https://edge.example.com',
    commission_rate: 0.2,
    freeze_days: 1,
    freeze_hours: 24,
    withdraw_min: 10,
    max_hops: 5,
    payment_channels: [],
    recharge_min: 1,
    recharge_max: 50000,
    price_models: ['claude-sonnet-4-5', 'gpt-5.5'],
  } as never)
  vi.mocked(market.listing).mockResolvedValue({ models: ['claude-opus-4-1', 'claude-sonnet-4-5'] } as never)
  vi.mocked(buyer.keySecret).mockResolvedValue({ key: SECRET })
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn(async (t: string) => void copied.push(t)) },
    configurable: true,
  })
})
afterEach(() => {
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

async function page(query = '') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/buyer/client-import', component: ClientImportView },
      { path: '/:other(.*)*', component: { render: () => null } },
    ] })
  await router.push(`/buyer/client-import${query}`)
  await router.isReady()
  const wrapper = mount(ClientImportView, { attachTo: document.body, global: { plugins: [createPinia(), router] } })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text().includes(text))
  expect(btn, `找不到按钮「${text}」`).toBeTruthy()
  await btn!.trigger('click')
  await flushPromises()
}

describe('导入客户端页', () => {
  it('只列启用中的 Key，选中后自动读取明文；预览打码、不出现完整 Key', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([key({}), key({ id: 902, name: '已停用', status: 'disabled' })])
    const w = await page()
    expect(w.findAll('#ci-key option')).toHaveLength(1)
    expect(buyer.keySecret).toHaveBeenCalledWith(901)
    expect(w.html()).not.toContain(SECRET)
    expect(w.find('.ci-keycard code').text()).toMatch(/^tm-9•+6789$/)
    // 默认模型取路线上卖家开放的第一个模型
    expect((w.find('#ci-model').element as HTMLInputElement).value).toBe('claude-opus-4-1')
  })

  it('配置文件 → 复制配置命令时带完整 Key 和 Edge 地址', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([key({ platform: 'openai' })])
    const w = await page()
    await click(w, '配置文件')
    await click(w, 'Codex')
    expect(w.find('pre').text()).not.toContain(SECRET)
    await click(w, '复制配置命令')
    expect(copied).toHaveLength(1)
    expect(copied[0]).toContain(SECRET)
    // Codex 原生配置的 base_url 要带 /v1
    expect(copied[0]).toContain('https://edge.example.com/v1')
  })

  it('?key= 预选指定 Key', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([key({}), key({ id: 905, name: '第二个' })])
    const w = await page('?key=905')
    expect((w.find('#ci-key').element as HTMLSelectElement).value).toBe('905')
    expect(buyer.keySecret).toHaveBeenCalledWith(905)
  })

  it('无法再次查看明文的 Key 不能导入，也不会去请求明文', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([key({ recoverable: false })])
    const w = await page()
    expect(buyer.keySecret).not.toHaveBeenCalled()
    expect(w.find('.ci-callout').exists()).toBe(true)
    const primary = w.find('.ci-actions .xm-btn-primary')
    expect(primary.attributes('disabled')).toBeDefined()
    expect(primary.text()).toContain('无法用于导入')
  })

  it('没有启用中的 Key 时显示空状态', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([])
    const w = await page()
    expect(w.text()).toContain('还没有启用中的 API Key')
  })
})

describe('suggestKeyModels', () => {
  it('优先用路线上挂单开放的模型', () => {
    expect(suggestKeyModels(['claude-opus-4-1', 'claude-opus-4-1', ''], ['gpt-5.5'], 'anthropic')).toEqual({
      models: ['claude-opus-4-1'],
      source: 'listing',
    })
  })
  it('全走保障池时退回价目表里同平台的模型', () => {
    expect(suggestKeyModels([], ['claude-sonnet-4-5', 'gpt-5.5', 'gemini-2.5-pro'], 'ChatGPT')).toEqual({
      models: ['gpt-5.5'],
      source: 'catalog',
    })
  })
  it('同平台过滤后为空时给全部价目表模型', () => {
    expect(suggestKeyModels([], ['kimi-for-coding'], 'anthropic').models).toEqual(['kimi-for-coding'])
  })
})
