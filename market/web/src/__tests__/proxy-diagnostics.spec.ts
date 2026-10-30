import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import ProxyInventory from '@/components/ProxyInventory.vue'
import { admin, seller } from '@/api'
import type { Proxy } from '@/api/types'

vi.mock('@/api', () => ({
  seller: { proxies: vi.fn(), testProxy: vi.fn(), checkProxyQuality: vi.fn() },
  admin: { proxies: vi.fn(), testProxy: vi.fn(), checkProxyQuality: vi.fn() },
}))

const proxy: Proxy = { id: 1, seller_id: 1, name: 'test proxy', protocol: 'http', host: 'proxy.test', port: 8080, status: 'available', sub2api_proxy_id: 1, created_at: '', assigned_upstream_ids: [], assigned_upstream_count: 0 }
const checked: Proxy = { ...proxy, region: 'US', quality_checked_at: '2026-10-10T00:00:00Z', quality_report: {
  grade: 'B', score: 80, summary: '部分站点触发人机验证', exit_ip: '203.0.113.25', country: 'United States', base_latency_ms: 100,
  passed_count: 1, failed_count: 0, warn_count: 0, challenge_count: 1, checked_at: 1791590400,
  items: [{ target: 'openai', status: 'challenge', http_status: 403, latency_ms: 200, message: 'Cloudflare challenge', cf_ray: 'test-ray' }],
} }

const dialog = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" /></div>' })
async function inventory(scope: 'seller' | 'admin') {
  const wrapper = mount(ProxyInventory, { props: { scope }, global: { plugins: [createPinia()], stubs: { ModalDialog: dialog, RouterLink: { template: '<a><slot /></a>' } } } })
  await flushPromises()
  return wrapper
}

describe('proxy diagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(seller.proxies).mockResolvedValue([proxy])
    vi.mocked(admin.proxies).mockResolvedValue([proxy])
    vi.mocked(seller.checkProxyQuality).mockResolvedValue(checked)
    vi.mocked(admin.checkProxyQuality).mockResolvedValue(checked)
  })

  it.each(['seller', 'admin'] as const)('runs %s diagnostics and shows per-target results', async (scope) => {
    const wrapper = await inventory(scope)
    await wrapper.findAll('button').find((b) => b.text() === '质量检测')!.trigger('click')
    await flushPromises()
    expect((scope === 'seller' ? seller : admin).checkProxyQuality).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('80 分')
    expect(wrapper.text()).toContain('人机验证')
    expect(wrapper.text()).toContain('HTTP 403')
    expect(wrapper.text()).toContain('test-ray')
    expect(wrapper.text()).toContain('203.0.113.25')
    wrapper.unmount()
  })

  it('disables diagnostics for proxies that are not synchronized', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([{ ...proxy, sub2api_proxy_id: undefined }])
    const wrapper = await inventory('seller')
    expect(wrapper.findAll('button').find((b) => b.text() === '质量检测')!.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('shows saved reports when the proxy list is loaded', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([checked])
    const wrapper = await inventory('seller')
    await wrapper.findAll('button').find((b) => b.text() === '查看报告')!.trigger('click')
    expect(wrapper.text()).toContain('Cloudflare challenge')
    expect(seller.checkProxyQuality).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reloads the proxy list from the refresh button', async () => {
    const wrapper = await inventory('seller')
    expect(seller.proxies).toHaveBeenCalledTimes(1)
    vi.mocked(seller.proxies).mockResolvedValue([checked])
    await wrapper.findAll('button').find((b) => b.text() === '刷新')!.trigger('click')
    await flushPromises()
    expect(seller.proxies).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('80 分')
    wrapper.unmount()
  })

  it.each([
    { assigned_upstream_ids: [71, 72], assigned_upstream_count: 2 },
    { assigned_upstream_ids: [], assigned_upstream_count: 2 },
    { assigned_upstream_ids: [71, 72], assigned_upstream_count: 0 },
  ])('blocks editing, disabling and deletion while any binding remains: %j', async (bindings) => {
    vi.mocked(seller.proxies).mockResolvedValue([{ ...proxy, ...bindings }])
    const wrapper = await inventory('seller')
    expect(wrapper.text()).toContain('已绑定 2 个账号')
    expect(wrapper.findAll('button').some((button) => ['编辑', '删除'].includes(button.text()))).toBe(false)
    expect(wrapper.findAll('button').find((button) => button.text() === '停用')!.attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('a').map((link) => link.text()).filter((text) => text.startsWith('账号 #'))).toEqual(bindings.assigned_upstream_ids.map((id) => `账号 #${id}`))
    wrapper.unmount()
  })

  it('ignores a stale legacy binding after all accounts are removed', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([{ ...proxy, assigned_upstream_id: 71 }])
    const wrapper = await inventory('seller')
    expect(wrapper.text()).toContain('未绑定账号')
    expect(wrapper.text()).not.toContain('账号 #71')
    expect(wrapper.findAll('button').filter((button) => ['编辑', '删除'].includes(button.text()))).toHaveLength(2)
    expect(wrapper.findAll('button').find((button) => button.text() === '停用')!.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('finds a proxy by its second account', async () => {
    vi.mocked(seller.proxies).mockResolvedValue([{ ...proxy, assigned_upstream_ids: [71, 98765], assigned_upstream_count: 2 }])
    const wrapper = await inventory('seller')
    await wrapper.get('#proxy-keyword').setValue('98765')
    expect(wrapper.text()).toContain('test proxy')
    await wrapper.get('#proxy-keyword').setValue('no-match')
    expect(wrapper.text()).not.toContain('test proxy')
    wrapper.unmount()
  })
})
