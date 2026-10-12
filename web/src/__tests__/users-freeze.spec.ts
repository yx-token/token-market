import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createPinia } from 'pinia'
import { admin } from '@/api'
import UsersView from '@/views/admin/UsersView.vue'

vi.mock('@/api', () => ({
  admin: { pendingKYC: vi.fn(), reviewKYC: vi.fn(), setUserStatus: vi.fn(), orgUsers: vi.fn() },
}))

const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
const listPage = defineComponent({ template: '<main><slot name="actions" /><slot name="filters" /><slot /><slot name="footer" /></main>' })

function mountUsers() {
  return mount(UsersView, { global: { plugins: [createPinia()], stubs: { ModalDialog: dialog, ConsoleListPage: listPage } } })
}

describe('用户冻结 — 目标用户用可搜索下拉', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.pendingKYC).mockResolvedValue([])
    vi.mocked(admin.orgUsers).mockResolvedValue([
      { id: 42, email: 'buyer@market.test', is_admin: false, status: 'active', created_at: '' },
    ])
    vi.mocked(admin.setUserStatus).mockResolvedValue({} as any)
  })

  it('远程搜索用户、选中后冻结携带用户 ID', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mountUsers()
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '冻结 / 解冻')!.trigger('click')
    await flushPromises()
    const trigger = wrapper.get('#uid')
    await trigger.trigger('click')
    await flushPromises()
    const panel = document.body.querySelector('#uid-listbox')!
    expect(panel).toBeTruthy()
    const opt = [...panel.querySelectorAll<HTMLElement>('.ss-opt')].find((li) => li.textContent?.includes('buyer@market.test'))!
    opt.click()
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '冻结')!.trigger('click')
    await flushPromises()
    expect(admin.setUserStatus).toHaveBeenCalledWith(42, 'frozen')
    confirmSpy.mockRestore()
    wrapper.unmount()
  })

  it('未选用户时冻结按钮不可生效（值为空）', async () => {
    const wrapper = mountUsers()
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '冻结 / 解冻')!.trigger('click')
    await flushPromises()
    // 未选择用户时，冻结按钮 disabled
    const freezeBtn = wrapper.findAll('button').find((b) => b.text() === '冻结')!
    expect(freezeBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
