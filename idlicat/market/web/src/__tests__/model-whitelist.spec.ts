import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { seller } from '@/api'
import type { Upstream } from '@/api/types'
import ModelWhitelistSelector from '@/components/ModelWhitelistSelector.vue'
import UpstreamModelWhitelist from '@/components/UpstreamModelWhitelist.vue'

vi.mock('@/api', () => ({ seller: { upstreamModels: vi.fn(), refreshUpstreamModels: vi.fn(), setUpstreamModels: vi.fn() } }))
const catalog = ['gpt-6-astra', 'gpt-6.1-sol']
const base = { id: 4, status: 'ready', model_whitelist: null } as Upstream
const button = (wrapper: ReturnType<typeof mount>, text: string) => wrapper.findAll('button').find((b) => b.text() === text)!

describe('挂售模型选择', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(seller.upstreamModels).mockResolvedValue({ catalog, whitelist: null, fetched_at: null })
    vi.mocked(seller.setUpstreamModels).mockImplementation(async (_, models) => ({ ...base, model_whitelist: models }))
  })

  it('默认全选，搜索后取消一项仍保留隐藏项，刷新不会重置选择', async () => {
    const wrapper = mount(ModelWhitelistSelector, { props: { upstreamId: 4 } })
    await flushPromises()
    expect(wrapper.findAll('input[type="checkbox"]').every((input) => (input.element as HTMLInputElement).checked)).toBe(true)
    await wrapper.get('input[type="search"]').setValue('astra')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    await wrapper.get('input[type="checkbox"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['gpt-6.1-sol']])
    vi.mocked(seller.refreshUpstreamModels).mockResolvedValue({ catalog: [...catalog, 'gpt-new'], whitelist: null, fetched_at: '2026-10-13T10:00:00Z' })
    await button(wrapper, '刷新目录').trigger('click')
    await flushPromises()
    expect(seller.refreshUpstreamModels).toHaveBeenCalledWith(4)
    await wrapper.get('input[type="search"]').setValue('')
    expect(wrapper.findAll('input[type="checkbox"]').map((input) => (input.element as HTMLInputElement).checked)).toEqual([false, true, false])
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([true])
    wrapper.unmount()
  })

  it('空目录允许手填模型并去重，空白输入不能提交', async () => {
    vi.mocked(seller.upstreamModels).mockResolvedValue({ catalog: [], whitelist: null, fetched_at: null })
    const wrapper = mount(ModelWhitelistSelector, { props: { upstreamId: 4 } })
    await flushPromises()
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([false])
    await wrapper.get('textarea').setValue(' custom-a，custom-b\ncustom-a ')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['custom-a', 'custom-b']])
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([true])
    await wrapper.get('textarea').setValue(' \n ')
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })

  it('目录加载失败可重试，刷新移除的已选模型必须重新确认', async () => {
    vi.mocked(seller.upstreamModels).mockRejectedValueOnce(new Error('目录暂不可用'))
    const wrapper = mount(ModelWhitelistSelector, { props: { upstreamId: 4 } })
    await flushPromises()
    expect(wrapper.text()).toContain('目录暂不可用')
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([false])
    await button(wrapper, '重试').trigger('click')
    await flushPromises()
    vi.mocked(seller.refreshUpstreamModels).mockResolvedValue({ catalog: ['gpt-6-astra'], whitelist: null, fetched_at: null })
    await button(wrapper, '刷新目录').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('不在当前目录中')
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([false])
    await button(wrapper, '全选').trigger('click')
    expect(wrapper.emitted('valid')?.at(-1)).toEqual([true])
    wrapper.unmount()
  })

  it('上游详情提示存量补填，保存选中的模型并通知详情刷新', async () => {
    const wrapper = mount(UpstreamModelWhitelist, { props: { upstream: base }, global: { plugins: [createPinia()] } })
    expect(wrapper.text()).toContain('目前不限制模型请求')
    await button(wrapper, '补填挂售模型').trigger('click')
    await flushPromises()
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(false)
    await button(wrapper, '保存白名单').trigger('click')
    await flushPromises()
    expect(seller.setUpstreamModels).toHaveBeenCalledWith(4, ['gpt-6.1-sol'])
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.text()).toContain('gpt-6.1-sol')
    expect(wrapper.text()).not.toContain('尚未声明')
    wrapper.unmount()
  })

  it('待选择状态自动打开选择器，保存失败保留编辑与选择', async () => {
    vi.mocked(seller.setUpstreamModels).mockRejectedValueOnce(new Error('写入失败'))
    const wrapper = mount(UpstreamModelWhitelist, { props: { upstream: { ...base, status: 'selecting_models' } }, global: { plugins: [createPinia()] } })
    await flushPromises()
    expect(wrapper.text()).toContain('待选择挂售模型')
    await button(wrapper, '保存白名单').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(2)
    expect(wrapper.emitted('changed')).toBeUndefined()
    await button(wrapper, '保存白名单').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })
})
