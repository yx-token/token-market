import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ResourceView from '../ResourceView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const confirmDialog = defineComponent({
  props: ['show', 'reason'],
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show" data-test="confirm-dialog"><button data-test="confirm-cancel" @click="$emit(\'cancel\')">取消</button><button data-test="confirm-ok" @click="$emit(\'confirm\', reason ? \'MANUAL-REF\' : undefined)">确认</button></div>',
})

let recharges: Record<string, unknown>[]
let refunds: Record<string, unknown>[]
let appeals: Record<string, unknown>[]

async function page(kind: 'recharges' | 'refunds' | 'appeals' = 'recharges') {
  const wrapper = mount(ResourceView, {
    props: { kind },
    global: { stubs: { Icon: true, Pagination: true, Select: true, RouterLink: true, ConfirmDialog: confirmDialog } },
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

beforeEach(() => {
  vi.clearAllMocks()
  recharges = []
  refunds = []
  appeals = []
  api.get.mockImplementation(async (path: string) => {
    // 资金三表走 relay，前端带 ?limit=200 一次拉满，本地分页
    if (path.startsWith('/api/v1/admin/market/recharges')) return structuredClone(recharges)
    if (path.startsWith('/api/v1/admin/market/refunds')) return structuredClone(refunds)
    if (path.endsWith('/appeals')) return structuredClone(appeals)
    return []
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

describe('资金与申诉资源页', () => {
  it('映射充值处理中、成功、取消和过期状态及 tone', async () => {
    recharges = [
      { id: 1, status: 'crediting', amount: 1 },
      { id: 2, status: 'credited', amount: 2 },
      { id: 3, status: 'cancelled', amount: 3 },
      { id: 4, status: 'expired', amount: 4 },
    ]
    const wrapper = await page()
    expect(wrapper.find('tbody').text()).toContain('到账中')
    expect(wrapper.find('tbody').text()).toContain('已到账')
    expect(wrapper.find('tbody').text()).toContain('已取消')
    expect(wrapper.find('tbody').text()).toContain('已过期')
    expect(wrapper.findAll('.status-dot').map((dot) => dot.attributes('data-tone'))).toEqual([
      'progress',
      'ok',
      'idle',
      'idle',
    ])
    expect(wrapper.find('.stats-row').text()).toContain('已加载')
  })

  it('映射退款处理状态，渠道使用 kind，金额接受数字字符串和空值', async () => {
    refunds = [
      { id: 1, status: 'locking', kind: 'balance', channel: '不应显示', amount: '1234.5' },
      { id: 2, status: 'subtracting', kind: 'order', amount: 20 },
      { id: 3, status: 'done', kind: 'pack', amount: null },
    ]
    const wrapper = await page('refunds')
    expect(wrapper.find('tbody').text()).toContain('暂停 Key 中')
    expect(wrapper.find('tbody').text()).toContain('扣减余额中')
    expect(wrapper.find('tbody').text()).toContain('已完成')
    expect(wrapper.find('tbody').text()).toContain('balance')
    expect(wrapper.find('tbody').text()).not.toContain('不应显示')
    expect(wrapper.findAll('.status-dot').map((dot) => dot.attributes('data-tone'))).toEqual([
      'progress',
      'progress',
      'ok',
    ])
    const amounts = wrapper.findAll('.amount-cell')
    expect(amounts.map((cell) => cell.text())).toEqual(['¥1,234.50', '¥20.00', '—'])
    expect(wrapper.find('tbody').text()).not.toContain('USD')
    expect(wrapper.find('tbody').text()).not.toContain('$')
  })

  it('将实际申诉状态 retesting 显示为复测中', async () => {
    appeals = [{ id: 1, status: 'retesting' }]
    const wrapper = await page('appeals')
    expect(wrapper.find('tbody').text()).toContain('复测中')
    expect(wrapper.get('.status-dot').attributes('data-tone')).toBe('progress')
  })

  it('切换资金页签时清除上一条操作成功提示', async () => {
    recharges = [{ id: 7, status: 'pending', channel: 'alipay', amount: 1 }]
    const wrapper = await page()
    await click(wrapper, '同步查单')
    expect(wrapper.text()).toContain('充值 7 已同步查单')
    await wrapper.setProps({ kind: 'refunds' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('充值 7 已同步查单')
  })

  it('资金列表按 relay 约定请求 limit=200，金额按人民币展示', async () => {
    recharges = [{ id: 11, status: 'credited', channel: 'alipay', amount: 12.5 }]
    const wrapper = await page()
    expect(api.get).toHaveBeenCalledWith('/api/v1/admin/market/recharges?limit=200')
    expect(wrapper.find('.amount-cell').text()).toBe('¥12.50')
  })

  it('取消确认充值对话框不发送请求', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.spyOn(window, 'prompt').mockReturnValue(null)
    recharges = [{ id: 9, status: 'pending', channel: 'manual', amount: 1 }]
    const wrapper = await page()
    await click(wrapper, '确认到账')
    expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(true)
    await wrapper.get('[data-test="confirm-cancel"]').trigger('click')
    expect(api.send).not.toHaveBeenCalled()
  })
})
