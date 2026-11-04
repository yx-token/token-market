import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const select = defineComponent({
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
})

const proxy = { id: 1, seller_id: 5, name: 'shared-proxy', protocol: 'http', host: 'proxy.test', port: 8080, status: 'assigned', sub2api_proxy_id: 1, assigned_upstream_ids: [71, 98765], assigned_upstream_count: 2 }

async function page() {
  const wrapper = mount(ProxiesView, {
    attachTo: document.body,
    global: { stubs: { Icon: true, Pagination: true, Select: select } },
  })
  await flushPromises()
  return wrapper
}

function filter(wrapper: VueWrapper, label: string) {
  return wrapper.findAll('.filter-item').find((item) => item.find('label').text() === label)!.get('select')
}

function stat(wrapper: VueWrapper, label: string) {
  return wrapper.findAll('.stat-card').find((item) => item.get('.stat-card__label').text() === label)!.get('.stat-card__value').text()
}

enableAutoUnmount(afterEach)

describe('market admin proxy bindings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.get.mockResolvedValue([
      proxy,
      { ...proxy, id: 2, name: 'count-only', assigned_upstream_ids: [], assigned_upstream_count: 3 },
      { ...proxy, id: 3, name: 'released', status: 'available', assigned_upstream_id: 71, assigned_upstream_ids: [], assigned_upstream_count: 0 },
      { ...proxy, id: 4, name: 'platform-proxy', seller_id: null, assigned_upstream_ids: [81], assigned_upstream_count: 1 },
      { ...proxy, id: 5, name: 'legacy-proxy', assigned_upstream_ids: undefined, assigned_upstream_count: undefined, assigned_upstream_id: 91 },
    ])
  })

  it('shows all account IDs, counts bindings and protects proxies with remaining bindings', async () => {
    const wrapper = await page()
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0]!.text()).toContain('已绑定 2 个账号')
    expect(rows[0]!.text()).toContain('账号 #71')
    expect(rows[0]!.text()).toContain('账号 #98765')
    expect(rows[4]!.text()).toContain('账号 #91')
    expect(stat(wrapper, '绑定账号总数')).toBe('7')
    expect(stat(wrapper, '未绑定')).toBe('1')
    expect(stat(wrapper, '卖家代理')).toBe('4')
    for (const index of [0, 1, 3, 4]) {
      const disable = rows[index]!.findAll('button').find((button) => button.text() === '停用')!
      expect(disable.attributes('disabled')).toBeDefined()
      await disable.trigger('click')
    }
    expect(api.send).not.toHaveBeenCalled()
    expect(rows[2]!.text()).not.toContain('账号 #71')
    expect(rows[2]!.findAll('button').find((button) => button.text() === '停用')!.attributes('disabled')).toBeUndefined()
  })

  it('filters bindings by count and searches the second account ID', async () => {
    const wrapper = await page()
    await filter(wrapper, '绑定').setValue('bound')
    expect(wrapper.findAll('tbody tr')).toHaveLength(4)
    expect(wrapper.find('tbody').text()).not.toContain('released')
    await filter(wrapper, '绑定').setValue('unbound')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody').text()).toContain('released')
    await filter(wrapper, '绑定').setValue('')
    await wrapper.get('input.filter-input').setValue('98765')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.find('tbody').text()).toContain('shared-proxy')
    await wrapper.get('input.filter-input').setValue('71')
    expect(wrapper.find('tbody').text()).not.toContain('released')
  })

  it('默认折叠较多绑定账号并显示停用按钮的禁用原因', async () => {
    api.get.mockResolvedValue([
      { ...proxy, assigned_upstream_ids: [71, 72, 73, 74, 75], assigned_upstream_count: 5 },
    ])
    const wrapper = await page()
    const row = wrapper.get('tbody tr')
    const preview = row.get('.binding-account-preview')

    expect(preview.text()).toContain('账号 #71')
    expect(preview.text()).toContain('账号 #72')
    expect(preview.text()).toContain('账号 #73')
    expect(preview.text()).not.toContain('账号 #74')
    expect(preview.text()).not.toContain('账号 #75')

    const details = row.get('details.binding-accounts')
    expect(details.get('summary').text()).toBe('另有 2 个')
    expect(details.attributes('open')).toBeUndefined()
    await details.get('summary').trigger('click')
    expect((details.element as HTMLDetailsElement).open).toBe(true)
    expect(row.get('.disable-reason').text()).toContain('需全部解绑后才能停用')
  })

  it('仅平台代理可切换共享，发送专用字段并按返回列表回显', async () => {
    api.send.mockResolvedValue({})
    const wrapper = await page()
    const sellerRow = wrapper.findAll('tbody tr')[0]!
    expect(sellerRow.findAll('button').some((button) => button.text().includes('设为共享'))).toBe(false)
    const platformRow = wrapper.findAll('tbody tr')[3]!
    api.get.mockResolvedValue([{ ...proxy, id: 4, name: 'platform-proxy', seller_id: null, is_shared: true, assigned_upstream_ids: [81], assigned_upstream_count: 1 }])
    await platformRow.findAll('button').find((button) => button.text() === '设为共享')!.trigger('click')
    await flushPromises()
    expect(api.send).toHaveBeenCalledWith('PUT', '/api/v1/admin/market/proxies/4/sharing', { is_shared: true })
    expect(wrapper.get('tbody').text()).toContain('设为独占')
  })

  it('保存失败时在活动弹窗内播报错误并保留表单', async () => {
    api.send.mockRejectedValue(new Error('代理保存失败'))
    const wrapper = await page()
    const open = wrapper.findAll('button').find((button) => button.text() === '添加代理')!
    await open.trigger('click')
    await flushPromises()

    const currentDialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!
    const values: Array<[string, string]> = [
      ['input[placeholder="例如 美国固定 IP 01"]', '待保留的代理名称'],
      ['input[placeholder="203.0.113.10"]', '198.51.100.8'],
      ['input[placeholder="1080"]', '1080'],
    ]
    for (const [selector, value] of values) {
      const input = currentDialog().querySelector<HTMLInputElement>(selector)!
      input.value = value
      input.dispatchEvent(new Event('input', { bubbles: true }))
    }
    await flushPromises()
    const save = Array.from(currentDialog().querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.trim() === '添加',
    )!
    save.click()
    await flushPromises()

    expect(currentDialog().getAttribute('aria-modal')).toBe('true')
    expect(currentDialog().querySelector('[role="alert"]')?.textContent).toContain('代理保存失败')
    expect(
      currentDialog().querySelector<HTMLInputElement>('input[placeholder="例如 美国固定 IP 01"]')?.value,
    ).toBe('待保留的代理名称')
    expect(api.get).toHaveBeenCalledOnce()
  })
})
