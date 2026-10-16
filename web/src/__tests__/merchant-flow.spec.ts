import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { buyer, market } from '@/api'
import { merchants } from '@/api/merchants'
import type { BuyerKey, MarketItem, User } from '@/api/types'
import { useAuth } from '@/stores/auth'
import { useToast } from '@/stores/toast'
import KeysView from '@/views/buyer/KeysView.vue'
import MerchantsView from '@/views/buyer/MerchantsView.vue'
import ListingView from '@/views/market/ListingView.vue'
import MarketView from '@/views/market/MarketView.vue'

vi.mock('@/api/merchants', () => ({ merchants: { list: vi.fn(), listings: vi.fn(), add: vi.fn(), remove: vi.fn() } }))
vi.mock('@/api', () => ({
  auth: {},
  buyer: { keys: vi.fn(), packs: vi.fn(), createKey: vi.fn(), updateKey: vi.fn(), purchasePack: vi.fn(), getRecharge: vi.fn() },
  market: {
    listings: vi.fn(), listing: vi.fn(), pools: vi.fn(), config: vi.fn(),
    categories: vi.fn().mockResolvedValue([]), recommend: vi.fn().mockResolvedValue(null), discovery: vi.fn().mockResolvedValue({ slots: [] }),
  },
}))

const first = { seller_id: 8, listing_id: 11, platform: 'openai', auth_type: 'oauth', price: 0.6, health: 'healthy', grade: 'S', detection_status: 'untested', title: '授权通道', models: ['gpt-6-astra', 'gpt-6.1-sol'] } as MarketItem
const second = { ...first, listing_id: 12, title: '备用通道' }
const dialog = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" /></div>' })

async function setup(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:path(.*)*', component: { template: '<div />' } }] })
  await router.push(path)
  const pinia = createPinia()
  useAuth(pinia).user = { id: 30, email: 'buyer@example.test' } as User
  return { router, pinia, global: { plugins: [pinia, router], stubs: { ModalDialog: dialog, DateField: true, QrCode: true } } }
}

function button(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find((element) => element.text() === text)!
}

describe('买家商家清单与 Key 调用顺序', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(merchants.listings).mockResolvedValue([first, second])
    vi.mocked(merchants.list).mockResolvedValue([{ seller_id: 8, added_at: '2026-10-15T00:00:00Z', listings: [first] }])
    vi.mocked(merchants.add).mockResolvedValue({})
    vi.mocked(merchants.remove).mockResolvedValue({})
    vi.mocked(buyer.keys).mockResolvedValue([])
    vi.mocked(buyer.packs).mockResolvedValue([])
    vi.mocked(buyer.createKey).mockResolvedValue({ key: 'test-key', detail: {} as BuyerKey })
    vi.mocked(buyer.updateKey).mockResolvedValue({} as BuyerKey)
    vi.mocked(market.listing).mockResolvedValue(first)
    vi.mocked(market.listings).mockResolvedValue([first])
    vi.mocked(market.pools).mockResolvedValue([{ id: 1, name: 'tm-pool-all', platform: 'composite', price: 1 }])
    vi.mocked(market.config).mockResolvedValue({ require_seller_proxy: true, commission_rate: 0.2, freeze_days: 7, withdraw_min: 10, payment_channels: [], recharge_min: 1, recharge_max: 50000, max_hops: 5, edge_url: '', price_models: [] })
  })

  it('市场详情等待商家持久化成功，再去 Key 配置，直接加入不签发 Key', async () => {
    let finish!: () => void
    vi.mocked(merchants.add).mockReturnValue(new Promise((resolve) => { finish = () => resolve({}) }))
    const context = await setup('/market/listings/11')
    const wrapper = mount(ListingView, { props: { id: '11' }, global: context.global })
    await flushPromises()
    expect(wrapper.text()).toContain('质量检测：未检测')
    expect(wrapper.findAll('.spec')[2]!.text()).toContain('未检测')
    expect(wrapper.get('a[href="/buyer/store-credits?seller_id=8"]').text()).toBe('店铺活动额度包')
    await button(wrapper, '加入商家并配置 Key').trigger('click')
    await flushPromises()
    expect(merchants.add).toHaveBeenCalledWith(8)
    expect(context.router.currentRoute.value.path).toBe('/market/listings/11')
    expect(buyer.createKey).not.toHaveBeenCalled()
    finish()
    await flushPromises()
    expect(context.router.currentRoute.value.fullPath).toBe('/buyer/keys?listing=11')
    wrapper.unmount()
  })

  it('市场快速加入失败会保留原页，不绕过商家清单创建 Key', async () => {
    vi.mocked(merchants.add).mockRejectedValueOnce(new Error('商家已停用'))
    const context = await setup('/')
    const wrapper = mount(MarketView, { global: context.global })
    await flushPromises()
    await button(wrapper, '选用开通').trigger('click')
    await flushPromises()
    expect(merchants.add).toHaveBeenCalledWith(8)
    expect(buyer.createKey).not.toHaveBeenCalled()
    expect(context.router.currentRoute.value.path).toBe('/')
    expect(useToast(context.pinia).items.some((item) => item.text.includes('商家已停用'))).toBe(true)
    wrapper.unmount()
  })

  it('旧额度包购买也先持久化商家，失败时不下单', async () => {
    vi.mocked(market.listing).mockResolvedValue({ ...first, sku_kind: 'pack', face_value: 20, stock: 2, stock_sold: 0 })
    vi.mocked(merchants.add).mockRejectedValueOnce(new Error('商家已停用'))
    const context = await setup('/market/listings/11')
    const wrapper = mount(ListingView, { props: { id: '11' }, global: context.global })
    await flushPromises()
    await button(wrapper, '支付并订阅套餐').trigger('click')
    await flushPromises()
    expect(merchants.add).toHaveBeenCalledWith(8)
    expect(buyer.purchasePack).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('在线旧包不把此前同挂单的包当作本次到账，履约失败后结束等待并说明余额去向', async () => {
    vi.useFakeTimers()
    let wrapper: ReturnType<typeof mount> | undefined
    try {
      vi.mocked(market.listing).mockResolvedValue({ ...first, sku_kind: 'pack', face_value: 20, stock: 2, stock_sold: 0 })
      const recharge = {
        id: 77, user_id: 30, amount: 20, provider: 'alipay', channel: 'alipay_f2f', status: 'pending',
        refunded_amount: 0, created_at: '2026-10-15T00:00:00Z',
        payment: { channel: 'alipay_f2f', kind: 'qrcode' as const, qr_code: 'local-test-payment' },
      }
      vi.mocked(buyer.purchasePack).mockResolvedValue({ recharge })
      vi.mocked(buyer.getRecharge).mockResolvedValue({ ...recharge, status: 'credited', pack_checkout_status: 'pending' })
      vi.mocked(buyer.packs).mockResolvedValue([{
        id: 1, listing_id: 11, buyer_id: 30, buyer_key_id: 9, face_value: 20, remaining: 20,
        paid_amount: 20, status: 'active', recharge_id: 1,
      }])
      const context = await setup('/market/listings/11')
      wrapper = mount(ListingView, { props: { id: '11' }, global: context.global })
      await flushPromises()
      await button(wrapper, '支付并订阅套餐').trigger('click')

      await flushPromises()
      await vi.advanceTimersByTimeAsync(2000)
      await flushPromises()
      expect(context.router.currentRoute.value.path).toBe('/market/listings/11')
      vi.mocked(buyer.getRecharge).mockResolvedValue({ ...recharge, status: 'credited', pack_checkout_status: 'failed' })
      await vi.advanceTimersByTimeAsync(2000)
      await flushPromises()
      expect(context.router.currentRoute.value.path).toBe('/buyer/wallet')
      expect(useToast(context.pinia).items.some((item) => item.text.includes('已支付金额保留在平台余额'))).toBe(true)
      await vi.advanceTimersByTimeAsync(4000)
      expect(buyer.getRecharge).toHaveBeenCalledTimes(2)
    } finally {
      wrapper?.unmount()
      vi.useRealTimers()
    }
  })

  it('Key 只加载清单挂单，提交买家调整的优先顺序并保留平台兜底', async () => {
    const context = await setup('/buyer/keys')
    const wrapper = mount(KeysView, { global: context.global })
    await flushPromises()
    expect(merchants.listings).toHaveBeenCalledOnce()
    expect(market.listings).not.toHaveBeenCalled()
    await button(wrapper, '创建 Key').trigger('click')
    await wrapper.get('#kname').setValue('顺序测试')
    await wrapper.get('input[type="radio"][value="chain"]').setValue()
    await button(wrapper, '＋ 添加清单内挂单').trigger('click')
    await wrapper.get('select[aria-label="第 1 跳挂单"]').setValue('11')
    await button(wrapper, '＋ 添加清单内挂单').trigger('click')
    await wrapper.get('select[aria-label="第 2 跳挂单"]').setValue('12')
    await wrapper.findAll('button[aria-label="上移"]')[1]!.trigger('click')
    await button(wrapper, '＋ 以默认分组兜底').trigger('click')
    expect(wrapper.get('select[aria-label="第 1 跳挂单"]').text()).toContain('未检测')
    expect(wrapper.get('select[aria-label="第 1 跳挂单"]').text()).not.toContain('S 级')
    expect(wrapper.get('select[aria-label="第 3 跳默认分组"]').element).toBeDefined()
    await button(wrapper, '创建').trigger('click')
    await flushPromises()
    expect(buyer.createKey).toHaveBeenCalledWith(expect.objectContaining({
      name: '顺序测试', route: { type: 'chain', hops: [{ listing_id: 12 }, { listing_id: 11 }, { pool: 'tm-pool-all' }] },
    }))
    wrapper.unmount()
  })

  it('清单外的 URL 挂单不能预填；编辑旧 Key 的失效挂单不能原样保存', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([{ id: 1, name: '旧 Key', route_type: 'chain', limits: {}, hops: [{ position: 1, listing_id: 99 }] } as BuyerKey])
    const context = await setup('/buyer/keys?listing=99')
    const wrapper = mount(KeysView, { global: context.global })
    await flushPromises()
    expect(wrapper.find('#kname').exists()).toBe(false)
    await button(wrapper, '编辑').trigger('click')
    expect(wrapper.text()).toContain('挂单 #99 · 已移出清单或暂不可用')
    await button(wrapper, '保存').trigger('click')
    await flushPromises()
    expect(buyer.updateKey).not.toHaveBeenCalled()
    expect(useToast(context.pinia).items.some((item) => item.text.includes('已移出商家清单或暂不可用'))).toBe(true)
    wrapper.unmount()
  })

  it('Key 主列表明确标出移除商家后已跳过的挂单，保留后续兜底', async () => {
    vi.mocked(buyer.keys).mockResolvedValue([{
      id: 1, name: '原顺序', route_type: 'chain', limits: {},
      hops: [{ position: 1, listing_id: 11, state: 'skipped' }, { position: 2, pool: 'tm-pool-all', state: 'ready' }],
    } as BuyerKey])
    const context = await setup('/buyer/keys')
    const wrapper = mount(KeysView, { global: context.global })
    await flushPromises()
    const hops = wrapper.findAll('.xm-hop')
    expect(hops[0]!.text()).toContain('卖家挂单 #11')
    expect(hops[0]!.text()).toContain('已跳过')
    expect(hops[1]!.text()).toContain('默认分组 tm-pool-all')
    expect(hops[1]!.text()).not.toContain('已跳过')
    wrapper.unmount()
  })

  it('商家没有可用挂单仍保留，移除成功后更新页面，失败保留原数据', async () => {
    vi.mocked(merchants.list).mockResolvedValue([{ seller_id: 8, added_at: '2026-10-15T00:00:00Z', listings: [] }])
    vi.mocked(merchants.remove).mockRejectedValueOnce(new Error('网络暂不可用'))
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const context = await setup('/buyer/merchants')
    const wrapper = mount(MerchantsView, { global: context.global })
    await flushPromises()
    expect(wrapper.text()).toContain('商家 #8')
    expect(wrapper.text()).toContain('暂无可用挂单')
    await button(wrapper, '移除商家').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('商家 #8')
    await button(wrapper, '移除商家').trigger('click')
    await flushPromises()
    expect(merchants.remove).toHaveBeenCalledWith(8)
    expect(wrapper.text()).toContain('还没有加入商家')
    expect(confirm.mock.calls[0]?.[0]).toContain('其他挂单与平台兜底继续按原顺序调用')
    confirm.mockRestore()
    wrapper.unmount()
  })
})
