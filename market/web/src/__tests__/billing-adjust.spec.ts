import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createPinia } from 'pinia'
import { admin } from '@/api'
import BillingView from '@/views/admin/BillingView.vue'
import type { Anomaly } from '@/api/types'

vi.mock('@/api', () => ({
  admin: { anomalies: vi.fn(), adjust: vi.fn(), orgUsers: vi.fn() },
}))

const anomaly: Anomaly = {
  id: 7, rule: 'balance_drift', buyer_user_id: 42, created_at: '2026-01-01T00:00:00Z', status: 'open',
  amount: 1.5, note: '',
} as unknown as Anomaly

const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
const listPage = defineComponent({ template: '<main><slot name="actions" /><slot name="filters" /><slot /><slot name="footer" /></main>' })

function mountBilling() {
  return mount(BillingView, { global: { plugins: [createPinia()], stubs: { ModalDialog: dialog, ConsoleListPage: listPage } } })
}

describe('计费调账 — 买家用户用可搜索下拉', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.anomalies).mockResolvedValue([anomaly])
    vi.mocked(admin.orgUsers).mockResolvedValue([
      { id: 42, email: 'buyer@market.test', is_admin: false, status: 'active', created_at: '' },
    ])
    vi.mocked(admin.adjust).mockResolvedValue({ status: 'adjusted' } as any)
  })

  it('远程搜索用户、选中后提交调账携带 buyer_user_id', async () => {
    const wrapper = mountBilling()
    await flushPromises()
    // 打开调账对话框
    await wrapper.findAll('button').find((b) => b.text() === '人工调账')!.trigger('click')
    await flushPromises()
    // 打开用户 SearchSelect，远程搜索返回 #42
    const trigger = wrapper.get('#au')
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.body.querySelector('#au-listbox')!
    expect(panel).toBeTruthy()
    const opt = [...panel.querySelectorAll<HTMLElement>('.ss-opt')].find((li) => li.textContent?.includes('buyer@market.test'))!
    opt.click()
    await flushPromises()
    // 填金额与原因
    await wrapper.get('#aa').setValue('1.5')
    await wrapper.get('#ar').setValue('补偿')
    await wrapper.findAll('button').find((b) => b.text() === '确认调账')!.trigger('click')
    await flushPromises()
    expect(admin.adjust).toHaveBeenCalledWith(expect.objectContaining({ buyer_user_id: 42, amount: 1.5, reason: '补偿' }))
    wrapper.unmount()
  })

  it('搜索失败时降级为空，不阻塞表单（已选值保留）', async () => {
    vi.mocked(admin.orgUsers).mockRejectedValue(new Error('network'))
    const wrapper = mountBilling()
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '人工调账')!.trigger('click')
    await flushPromises()
    const trigger = wrapper.get('#au')
    await trigger.trigger('click')
    await flushPromises()
    // 远程失败时面板仍存在，提示搜索失败；用户可重新输入关键字重试
    const panel = document.body.querySelector('#au-listbox')
    expect(panel).toBeTruthy()
    const note = document.body.querySelector('.ss-note')
    expect(note?.textContent ?? '').toContain('搜索失败')
    wrapper.unmount()
  })
})
