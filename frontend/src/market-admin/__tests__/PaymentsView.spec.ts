import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PaymentsView from '../PaymentsView.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const confirmDialog = defineComponent({
  props: ['show'],
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show" data-test="confirm-dialog"><button data-test="confirm-cancel" @click="$emit(\'cancel\')">取消</button><button data-test="confirm-ok" @click="$emit(\'confirm\')">确认</button></div>',
})

const settings = {
  payment_public_url: 'https://pay.example.com',
  payment_channels: { alipay_page: true, alipay_f2f: false, wechat_native: true, manual: false },
  recharge_min: 1,
  recharge_max: 50000,
  recharge_order_timeout_minutes: 30,
  refund_original_route: true,
  refund_original_route_days: 90,
}

const merchants = {
  alipay: {
    provider: 'alipay',
    enabled: true,
    ready: true,
    updated_at: '2026-01-01T00:00:00Z',
    alipay: { app_id: 'ali-app-id', mode: 'public_key' },
  },
  wechat: {
    provider: 'wechat',
    enabled: true,
    ready: true,
    updated_at: '2026-01-01T00:00:00Z',
    wechat: { app_id: 'wx-app-id', mch_id: 'mch-id' },
  },
}

let failSettings = false
let failMerchants = false
let failStatus = false
let notifications: Record<string, unknown>[]

async function page() {
  const wrapper = mount(PaymentsView, {
    global: { stubs: { Icon: true, Pagination: true, Select: true, ConfirmDialog: confirmDialog } },
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

function numberInput(wrapper: VueWrapper, label: string) {
  const field = wrapper.findAll('.y-field').find((item) => item.find('span').text() === label)
  expect(field, `找不到字段：${label}`).toBeDefined()
  return field!.get('input')
}

beforeEach(() => {
  vi.clearAllMocks()
  failSettings = false
  failMerchants = false
  failStatus = false
  notifications = []
  api.get.mockImplementation(async (path: string) => {
    if (path.endsWith('/settings')) {
      if (failSettings) throw new Error('基础设置读取失败')
      return structuredClone(settings)
    }
    if (path.endsWith('/payments/merchants')) {
      if (failMerchants) throw new Error('商户配置读取失败')
      return structuredClone(merchants)
    }
    if (path.endsWith('/payments/status')) {
      if (failStatus) throw new Error('支付通知读取失败')
      return { notifications: structuredClone(notifications) }
    }
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

describe('支付配置页', () => {
  it('逐项标记商户加载失败并禁止保存商户表单，重试可恢复', async () => {
    failMerchants = true
    const wrapper = await page()
    await click(wrapper, '支付宝')
    expect(wrapper.text()).toContain('商户通道')
    expect(wrapper.text()).toContain('商户配置读取失败')
    expect(wrapper.get('.pay-actions .btn-primary').attributes('disabled')).toBeDefined()
    failMerchants = false
    await click(wrapper, '重试')
    expect(wrapper.get('.pay-actions .btn-primary').attributes('disabled')).toBeUndefined()
  })

  it('刷新商户配置失败保留最后成功数据但禁止保存', async () => {
    const wrapper = await page()
    await click(wrapper, '支付宝')
    expect(wrapper.get('input.input').element.value).toBe('ali-app-id')
    failMerchants = true
    await click(wrapper, '刷新')
    expect(wrapper.get('input.input').element.value).toBe('ali-app-id')
    expect(wrapper.get('.pay-actions .btn-primary').attributes('disabled')).toBeDefined()
  })

  it.each([
    ['单笔充值下限（元）', '', '单笔充值下限不能为空且必须是有限数字'],
    ['单笔充值下限（元）', '0', '单笔充值下限必须大于 0'],
    ['在线支付时限（分钟）', '4', '在线支付时限必须为 5–1440 分钟的整数'],
    ['在线支付时限（分钟）', '1441', '在线支付时限必须为 5–1440 分钟的整数'],
    ['原路退回期限（天）', '-1', '原路退回期限必须为不小于 0 的整数'],
  ])('拒绝非法基础设置：%s = %s', async (label, value, message) => {
    const wrapper = await page()
    await numberInput(wrapper, label).setValue(value)
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(message)
  })

  it('充值上限低于下限时拒绝保存', async () => {
    const wrapper = await page()
    await numberInput(wrapper, '单笔充值上限（元）').setValue('0.5')
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('单笔充值上限不得低于下限')
  })

  it('支付结果 chip 只放结果，长错误放在 row-sub，金额支持数字字符串', async () => {
    notifications = [{ id: 1, provider: 'alipay', result: 'fail', error: '很长的回调错误', amount_fen: '1234' }]
    const wrapper = await page()
    await click(wrapper, '最近通知')
    expect(wrapper.get('.y-chip').text()).toBe('fail')
    expect(wrapper.get('.row-sub').text()).toBe('很长的回调错误')
    expect(wrapper.get('.amount-cell').text()).toBe('12.34')
  })

  it('非列表 Tab 使用普通内容滚动模式', async () => {
    const wrapper = await page()
    expect(wrapper.findComponent(TablePageLayout).props('contentMode')).toBe('content')
    await click(wrapper, '通道状态')
    expect(wrapper.findComponent(TablePageLayout).props('contentMode')).toBe('table')
  })

  it('取消支付通道启停确认不发送请求', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = await page()
    await click(wrapper, '通道状态')
    await click(wrapper, '停用')
    expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(true)
    await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
    expect(api.send).not.toHaveBeenCalled()
  })
})
