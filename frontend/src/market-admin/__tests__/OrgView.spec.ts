import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import OrgView from '../OrgView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const roles = [
  { id: 1, name: '审核员', code: 'reviewer', description: '审核角色' },
  { id: 2, name: '财务', code: 'finance', description: '财务角色' },
]
const resource = { id: 'root', parent_id: null, name: '市场管理', kind: 'menu', path: '/market', icon: '', sort: 1, enabled: true }

async function page(initialTab?: '角色' | '员工') {
  const wrapper = mount(OrgView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, Pagination: true } },
  })
  await flushPromises()
  if (initialTab) await click(wrapper, initialTab)
  return wrapper
}

async function click(wrapper: VueWrapper, text: string) {
  const buttons = wrapper.findAll('button')
  const button = buttons.find((item) => item.text() === text) ?? buttons.find((item) => item.text().includes(text))
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
    if (path === '/api/v1/admin/market/org/units') return []
    if (path === '/api/v1/admin/market/org/roles') return structuredClone(roles)
    if (path === '/api/v1/admin/market/org/resources') return [structuredClone(resource)]
    if (path === '/api/v1/admin/market/org/staff') return []
    if (path === '/api/v1/admin/market/org/users?q=') return []
    if (path.match(/\/org\/roles\/\d+\/resources$/)) return { resource_ids: [] }
    if (path.match(/\/org\/roles\/\d+\/members$/)) return { members: [] }
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockResolvedValue({})
})

enableAutoUnmount(afterEach)
afterEach(() => {
  document.body.innerHTML = ''
  document.body.classList.remove('modal-open')
})

describe('组织草稿与成员搜索', () => {
  it.each(['刷新', '员工', '成员'])('权限草稿存在时%s会先要求确认，取消后保留草稿', async (target) => {
    const wrapper = await page('角色')
    await wrapper.get('.perm-list input[type="checkbox"]').setValue(true)
    await click(wrapper, target)
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('未保存')
    dialogButton('取消').click()
    await flushPromises()
    expect(wrapper.get('.perm-list input[type="checkbox"]').element.checked).toBe(true)
  })

  it('切换角色前确认权限草稿，确认放弃后才加载新角色', async () => {
    const wrapper = await page('角色')
    await wrapper.get('.perm-list input[type="checkbox"]').setValue(true)
    await wrapper.findAll('.org-side__item')[1]!.trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('未保存')
    expect(api.get).not.toHaveBeenCalledWith('/api/v1/admin/market/org/roles/2/resources')
    dialogButton('放弃草稿').click()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/api/v1/admin/market/org/roles/2/resources')
  })

  it('成员搜索展示空态与错误，而不是静默吞掉失败', async () => {
    const wrapper = await page('角色')
    await click(wrapper, '成员')
    await click(wrapper, '查询')
    expect(wrapper.text()).toContain('未找到匹配用户')
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/org/users?q=') throw new Error('搜索服务不可用')
      if (path === '/api/v1/admin/market/org/roles') return structuredClone(roles)
      if (path === '/api/v1/admin/market/org/resources') return [structuredClone(resource)]
      if (path === '/api/v1/admin/market/org/units') return []
      if (path.match(/\/org\/roles\/\d+\/resources$/)) return { resource_ids: [] }
      if (path.match(/\/org\/roles\/\d+\/members$/)) return { members: [] }
      return []
    })
    await click(wrapper, '查询')
    expect(wrapper.get('[role="alert"]').text()).toContain('搜索服务不可用')
  })

  it('成员新增形成草稿，保存后更新基线且刷新不再警告', async () => {
    const wrapper = await page('角色')
    await click(wrapper, '成员')
    api.get.mockImplementation(async (path: string) => {
      if (path === '/api/v1/admin/market/org/users?q=') return [{ id: 99, email: 'member@example.com' }]
      if (path === '/api/v1/admin/market/org/roles') return structuredClone(roles)
      if (path === '/api/v1/admin/market/org/resources') return [structuredClone(resource)]
      if (path === '/api/v1/admin/market/org/units') return []
      if (path.match(/\/org\/roles\/\d+\/resources$/)) return { resource_ids: [] }
      if (path.match(/\/org\/roles\/\d+\/members$/)) return { members: [] }
      return []
    })
    await click(wrapper, '查询')
    await click(wrapper, '添加 member@example.com')
    await click(wrapper, '刷新')
    expect(document.body.querySelector('[role="dialog"]')?.textContent).toContain('未保存')
    dialogButton('取消').click()
    await flushPromises()
    expect(wrapper.text()).toContain('member@example.com')

    await click(wrapper, '保存成员')
    expect(api.send).toHaveBeenCalledWith('PUT', '/api/v1/admin/market/org/roles/1/members', { user_ids: [99] })
    await click(wrapper, '刷新')
    expect(document.body.querySelector('[role="dialog"]:not(.modal-leave-active)')).toBeNull()
  })

  it('组织表单错误显示在活动弹窗 body', async () => {
    const wrapper = await page()
    await click(wrapper, '新建组织')
    await click(wrapper, '保存')
    expect(document.body.querySelector('[role="dialog"] .modal-body [role="alert"]')?.textContent).toContain('请填写名称与编码')
  })
})
