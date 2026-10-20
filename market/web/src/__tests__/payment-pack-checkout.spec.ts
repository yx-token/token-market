import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { buyer } from '@/api'
import type { RechargeOrder } from '@/api/types'
import PaymentDialog from '@/components/PaymentDialog.vue'

vi.mock('@/api', () => ({ buyer: { getRecharge: vi.fn() } }))
vi.mock('@/stores/config', () => ({ useConfig: () => ({ config: { payment_channels: ['manual'] }, ensure: async () => {} }) }))
const dialog = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" /></div>' })
const order = (checkout?: RechargeOrder['pack_checkout_status']) => ({
  id: 12, amount: 20, status: 'credited', channel: 'manual', pack_checkout_status: checkout,
} as RechargeOrder)

async function open(checkout?: RechargeOrder['pack_checkout_status']) {
  const wrapper = mount(PaymentDialog, {
    props: { open: false, order: order(checkout) },
    global: { plugins: [createPinia()], stubs: { ModalDialog: dialog, StatusBadge: true, QrCode: true } },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('在线额度包支付到账后的履约结果', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.clearAllMocks() })
  afterEach(() => { vi.useRealTimers() })

  it('余额到账后继续等待履约，失败时说明钱仍在余额并停止轮询', async () => {
    vi.mocked(buyer.getRecharge).mockResolvedValueOnce(order('pending')).mockResolvedValue(order('failed'))
    const wrapper = await open('pending')
    expect(wrapper.text()).toContain('正在开通额度包')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.text()).toContain('额度包未能开通，已支付金额保留在平台余额')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(4000)
    expect(buyer.getRecharge).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('正常履约后停止轮询并提示 Key 已可查看', async () => {
    vi.mocked(buyer.getRecharge).mockResolvedValue(order('fulfilled'))
    const wrapper = await open('pending')
    expect(wrapper.text()).toContain('额度包已开通，可在 API Key 页面查看')
    await vi.advanceTimersByTimeAsync(4000)
    expect(buyer.getRecharge).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('普通充值到账后直接停止轮询', async () => {
    vi.mocked(buyer.getRecharge).mockResolvedValue(order())
    const wrapper = await open()
    expect(wrapper.text()).toContain('余额已到账')
    expect(wrapper.text()).not.toContain('额度包')
    await vi.advanceTimersByTimeAsync(4000)
    expect(buyer.getRecharge).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
