import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import HotSearchesView from '../HotSearchesView.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), send: vi.fn() }))
vi.mock('../api', () => ({
  MARKET_API_PREFIX: '/api/v1/admin/market',
  marketGet: api.get,
  marketSend: api.send,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const pagination = defineComponent({
  props: ['page'],
  emits: ['update:page', 'update:pageSize'],
  template: '<div data-test="pagination" :data-page="page"><button data-test="page-two" @click="$emit(\'update:page\', 2)">第 2 页</button></div>',
})

let hotSearches: { id: string; label: string; enabled: boolean; action: Record<string, string> }[]
let failDiscovery = false

async function page() {
  const wrapper = mount(HotSearchesView, {
    global: { stubs: { Icon: true, Pagination: pagination, Select: true } },
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
  failDiscovery = false
  hotSearches = Array.from({ length: 16 }, (_, index) => ({
    id: `hs_${index + 1}`,
    label: `热搜 ${index + 1}`,
    enabled: true,
    action: {},
  }))
  api.get.mockImplementation(async (path: string) => {
    if (path.endsWith('/market-discovery')) {
      if (failDiscovery) throw new Error('热搜读取失败')
      return { version: 1, hot_searches: structuredClone(hotSearches) }
    }
    if (path.endsWith('/market-categories')) return []
    throw new Error(`unexpected GET ${path}`)
  })
  api.send.mockImplementation(async (_method: string, _path: string, body: unknown) => structuredClone(body))
})

enableAutoUnmount(afterEach)

describe('热搜配置页', () => {
  it('首次加载失败显示错误和重试并禁止保存', async () => {
    failDiscovery = true
    const wrapper = await page()
    const save = wrapper.findAll('button').find((button) => button.text() === '保存')!
    expect(wrapper.text()).toContain('热搜读取失败')
    expect(wrapper.text()).toContain('重试')
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')
    expect(api.send).not.toHaveBeenCalled()
    failDiscovery = false
    await click(wrapper, '重试')
    expect(save.attributes('disabled')).toBeUndefined()
  })

  it('刷新失败保留最后成功数据但禁止保存', async () => {
    const wrapper = await page()
    expect((wrapper.get('td.col-label input').element as HTMLInputElement).value).toBe('热搜 1')
    failDiscovery = true
    await click(wrapper, '刷新')
    expect((wrapper.get('td.col-label input').element as HTMLInputElement).value).toBe('热搜 1')
    expect(wrapper.text()).toContain('热搜读取失败')
    expect(wrapper.findAll('button').find((button) => button.text() === '保存')!.attributes('disabled')).toBeDefined()
  })

  it('删除末页末条后回收页码', async () => {
    const wrapper = await page()
    await wrapper.get('[data-test="page-two"]').trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    await click(wrapper, '删除')
    expect(wrapper.get('[data-test="pagination"]').attributes('data-page')).toBe('1')
    expect(wrapper.findAll('tbody tr')).toHaveLength(15)
  })

  it('筛选激活时禁用调序并给出说明', async () => {
    const wrapper = await page()
    await wrapper.get('input.filter-input').setValue('热搜 2')
    expect(wrapper.text()).toContain('筛选激活时不可调序')
    expect(wrapper.findAll('button').filter((button) => button.text() === '上移')[0]!.attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').filter((button) => button.text() === '下移')[0]!.attributes('disabled')).toBeDefined()
  })

  it('新增时清理筛选落点并跳到新项所在页', async () => {
    const wrapper = await page()
    await wrapper.get('input.filter-input').setValue('不存在的热搜')
    await click(wrapper, '热搜')
    expect((wrapper.get('input.filter-input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-test="pagination"]').attributes('data-page')).toBe('2')
    expect(wrapper.findAll('td.col-label input').some((input) => (input.element as HTMLInputElement).value === '新热搜')).toBe(true)
  })

  it.each([
    ['   ', '热搜 ID 不能为空'],
    ['hs_2', '热搜 ID 必须唯一'],
  ])('拒绝空白或重复 ID：%s', async (value, message) => {
    const wrapper = await page()
    await wrapper.get('td.col-id input').setValue(value)
    await click(wrapper, '保存')
    expect(api.send).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain(message)
  })

  it('保存成功后继续编辑会清除成功提示', async () => {
    const wrapper = await page()
    await click(wrapper, '保存')
    expect(wrapper.text()).toContain('热搜已保存')
    await wrapper.get('td.col-label input').setValue('继续编辑')
    expect(wrapper.text()).not.toContain('热搜已保存')
  })

  it('行内文本输入显式声明 type=text', async () => {
    const wrapper = await page()
    const inputs = wrapper.findAll('td input.cell-input')
    expect(inputs.length).toBeGreaterThan(0)
    for (const input of inputs) expect(input.attributes('type')).toBe('text')
  })
})
