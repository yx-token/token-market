import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import UsersView from '../UsersView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const user = {
  id: 7,
  email: 'long-user@example.com',
  is_admin: false,
  status: 'active',
  kyc_status: 'verified',
  created_at: '2026-01-01T00:00:00Z',
}

async function page() {
  const wrapper = mount(UsersView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, Pagination: true, RouterLink: true } },
  })
  await flushPromises()
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  if (button) await button.trigger('click')
  else dialogButton(text).click()
  await flushPromises()
}

function dialogButton(text: string) {
  const matches = Array.from(document.body.querySelectorAll<HTMLButtonElement>('.modal-footer button'))
    .filter((item) => item.textContent?.trim() === text)
  expect(matches.length, `找不到对话框按钮：${text}`).toBeGreaterThan(0)
  return matches.at(-1)!
}

beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockImplementation(async (path: string) => {
    if (path.startsWith('/api/v1/admin/market/users')) return [structuredClone(user)]
    if (path === '/api/v1/admin/market/kyc') return []
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('用户危险变更与弹窗', () => {
  it('取消列表冻结确认时不发送请求', async () => {
    const wrapper = await page()
    await click(wrapper, '冻结')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain(user.email)
    dialogButton('取消').click()
    await flushPromises()
    expect(api.send).not.toHaveBeenCalled()
  })

  it('确认列表冻结后提交原状态字段', async () => {
    const wrapper = await page()
    await click(wrapper, '冻结')
    dialogButton('确认冻结').click()
    await flushPromises()
    expect(api.send).toHaveBeenCalledWith('POST', '/api/v1/admin/market/users/7/status', { status: 'frozen' })
  })

  it('冻结、提升超管和删除已有 KYC 前集中展示危险警告', async () => {
    const wrapper = await page()
    await click(wrapper, '编辑')
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    const status = dialog.querySelector<HTMLSelectElement>('#edit-user-status')!
    status.value = 'frozen'
    status.dispatchEvent(new Event('change', { bubbles: true }))
    const admin = dialog.querySelector<HTMLInputElement>('#edit-user-admin')!
    admin.click()
    const kyc = dialog.querySelector<HTMLSelectElement>('#edit-user-kyc')!
    kyc.value = 'none'
    kyc.dispatchEvent(new Event('change', { bubbles: true }))
    await flushPromises()
    await click(wrapper, '保存')

    const dialogs = document.body.querySelectorAll('[role="dialog"]')
    const warning = dialogs.item(dialogs.length - 1)
    expect(warning.textContent).toContain('正常改为冻结')
    expect(warning.textContent).toContain('普通用户提升为超管')
    expect(warning.textContent).toContain('已有 KYC 改为未实名')
    expect(api.send).not.toHaveBeenCalled()
    dialogButton('取消').click()
    await flushPromises()
    expect(api.send).not.toHaveBeenCalled()
  })

  it('创建失败显示在活动弹窗 body，表单保留语义和自动填充属性', async () => {
    api.send.mockRejectedValue(new Error('邮箱已存在'))
    const wrapper = await page()
    await click(wrapper, '新建用户')
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog.querySelector('form')).not.toBeNull()
    expect(dialog.querySelector('input[type="email"]')).not.toBeNull()
    expect(dialog.querySelector('input[type="password"]')?.getAttribute('autocomplete')).toBe('new-password')
    const email = dialog.querySelector<HTMLInputElement>('#create-user-email')!
    email.value = 'new@example.com'
    email.dispatchEvent(new Event('input', { bubbles: true }))
    const password = dialog.querySelector<HTMLInputElement>('#create-user-password')!
    password.value = 'secret1'
    password.dispatchEvent(new Event('input', { bubbles: true }))
    await click(wrapper, '创建')
    const alert = document.body.querySelector('[role="dialog"] .modal-body [role="alert"]')
    expect(alert?.textContent).toContain('邮箱已存在')
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
  })
})
