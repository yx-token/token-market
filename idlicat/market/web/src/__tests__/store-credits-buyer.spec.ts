import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent } from 'vue'
import StoreCreditsView from '@/views/buyer/StoreCreditsView.vue'
import { storeCreditsBuyer, type CreditOffer, type CreditPack } from '@/api/storeCreditsBuyer'

vi.mock('@/api/storeCreditsBuyer', () => ({ storeCreditsBuyer: {
  offers: vi.fn(), packs: vi.fn(), merchants: vi.fn(), purchase: vi.fn(), refund: vi.fn(),
} }))

const offer: CreditOffer = {
  id: 11, seller_id: 7, title: '周末店铺活动', description: '写代码专享', price_cny: 80,
  face_value_usd: 100, allowed_platforms: ['openai'], allowed_models: ['gpt-6-astra'],
  status: 'active', created_at: '', updated_at: '',
}
const pack: CreditPack = {
  ...offer, id: 22, offer_id: 11, buyer_id: 3, remaining_usd: 60,
  remaining_balance_units: 60, prepaid_remaining_cny: 48, usd_to_balance_rate: 1, status: 'active',
}
const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot/><slot name="footer"/></section>' })
const header = defineComponent({ props: ['title', 'description'], template: '<header><h1>{{ title }}</h1><p>{{ description }}</p><slot/></header>' })
async function open(path = '/buyer/store-credits?seller_id=7') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: StoreCreditsView }] })
  await router.push(path)
  const wrapper = mount(StoreCreditsView, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: dialog, PageHeader: header } } })
  await flushPromises()
  return { wrapper, router }
}
function button(wrapper: ReturnType<typeof mount>, text: string) {
  const result = wrapper.findAll('button').find(b => b.text() === text)
  if (!result) throw new Error(`Missing button: ${text}`)
  return result
}

describe('买家店铺活动包', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(storeCreditsBuyer.merchants).mockResolvedValue([{ seller_id: 7, listings: [] }, { seller_id: 8, listings: [] }])
    vi.mocked(storeCreditsBuyer.offers).mockResolvedValue([offer])
    vi.mocked(storeCreditsBuyer.packs).mockResolvedValue([pack])
  })

  it('分别展示人民币售价、美元额度和购买快照中的模型范围', async () => {
    const { wrapper } = await open()
    expect(storeCreditsBuyer.offers).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('¥80.00')
    expect(wrapper.text()).toContain('$100.00')
    expect(wrapper.text()).toContain('$60.00')
    expect(wrapper.text()).toContain('gpt-6-astra')
    expect(wrapper.text()).toContain('用完后自动扣平台余额')
    wrapper.unmount()
  })

  it('购买先确认；网络失败重试沿用幂等键，成功后允许再次购买', async () => {
    vi.mocked(storeCreditsBuyer.purchase).mockRejectedValueOnce(new Error('network failed')).mockResolvedValue(pack)
    const { wrapper } = await open()
    await button(wrapper, '购买活动包').trigger('click')
    expect(storeCreditsBuyer.purchase).not.toHaveBeenCalled()
    await button(wrapper, '确认支付').trigger('click')
    await flushPromises()
    await button(wrapper, '确认支付').trigger('click')
    await flushPromises()
    const calls = vi.mocked(storeCreditsBuyer.purchase).mock.calls
    expect(calls).toHaveLength(2)
    expect(calls[0][0]).toBe(11)
    expect(calls[0][1]).toBeTruthy()
    expect(calls[1][1]).toBe(calls[0][1])
    expect(storeCreditsBuyer.packs).toHaveBeenCalledTimes(2)
    await button(wrapper, '购买活动包').trigger('click')
    await button(wrapper, '确认支付').trigger('click')
    await flushPromises()
    expect(vi.mocked(storeCreditsBuyer.purchase).mock.calls[2][1]).not.toBe(calls[0][1])
    wrapper.unmount()
  })

  it('切换店铺后不会把较慢的旧店铺响应显示为新店铺活动', async () => {
    let resolveOld!: (offers: CreditOffer[]) => void
    vi.mocked(storeCreditsBuyer.offers).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce([{ ...offer, id: 12, seller_id: 8, title: '另一家店铺' }])
    const { wrapper, router } = await open()
    await router.replace('/buyer/store-credits?seller_id=8')
    await flushPromises()
    resolveOld([offer])
    await flushPromises()
    expect(wrapper.find('.credit-grid').text()).toContain('另一家店铺')
    expect(wrapper.find('.credit-grid').text()).not.toContain('周末店铺活动')
    wrapper.unmount()
  })

  it('没有选定店铺时不请求全市场活动，已购包仍然可见', async () => {
    const { wrapper } = await open('/buyer/store-credits')
    expect(storeCreditsBuyer.offers).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('先选择一家店铺')
    expect(wrapper.text()).toContain('包 #22')
    wrapper.unmount()
  })
})
