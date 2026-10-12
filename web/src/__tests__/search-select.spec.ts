import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import SearchSelect from '@/components/common/SearchSelect.vue'
import type { SelectOption } from '@/components/common/searchSelect'

const opts: SelectOption[] = [
  { value: 'anthropic', text: 'Claude' },
  { value: 'openai', text: 'ChatGPT' },
  { value: 'gemini', text: 'Gemini' },
]

/** 面板 Teleport 到 body，按文档查询而不是 wrapper 内查询 */
const panel = () => document.body.querySelector<HTMLElement>('.ss-panel')
const panelItems = () => [...(panel()?.querySelectorAll<HTMLElement>('.ss-opt') ?? [])]

// Teleport 的面板挂在 body 上，用例之间必须清干净，否则会命中上一个用例的残留节点
afterEach(() => {
  document.body.innerHTML = ''
})

function select(props: {
  modelValue?: string | string[]
  options: SelectOption[]
  multiple?: boolean
  placeholder?: string
  disabled?: boolean
  loading?: boolean
  id?: string
  remote?: (q: string) => Promise<SelectOption[]>
  remoteDebounce?: number
}) {
  return mount(SearchSelect, { props, attachTo: document.body })
}

async function open(wrapper: ReturnType<typeof select>) {
  await wrapper.get('.ss-trigger').trigger('click')
  await flushPromises()
  expect(panel()).toBeTruthy()
}

describe('SearchSelect', () => {
  it('单选：输入过滤并回车选择', async () => {
    const wrapper = select({ modelValue: '', options: opts, placeholder: '选择厂商' })
    await open(wrapper)
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'gpt'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    const items = panelItems()
    expect(items).toHaveLength(1)
    expect(items[0].textContent).toContain('ChatGPT')
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['openai'])
    wrapper.unmount()
  })

  it('单选：点击选项即选中并收起面板', async () => {
    const wrapper = select({ modelValue: '', options: opts })
    await open(wrapper)
    panelItems()[1]!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['openai'])
    wrapper.unmount()
  })

  it('多选：勾选与取消，值保序', async () => {
    const wrapper = select({ modelValue: ['openai'], options: opts, multiple: true })
    expect(wrapper.get('.ss-trigger').text()).toContain('ChatGPT')
    await open(wrapper)
    panelItems()[0]!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai', 'anthropic']])
    // 再点一次取消刚添加的 anthropic，其余保留
    await wrapper.setProps({ modelValue: ['openai', 'anthropic'] })
    panelItems()[0]!.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['openai']])
    wrapper.unmount()
  })

  it('已存值不在选项中时保留并标注未注册', async () => {
    const wrapper = select({ modelValue: ['openai', 'deadbeef'], options: opts, multiple: true })
    expect(wrapper.get('.ss-trigger').text()).toContain('deadbeef（未注册）')
    await open(wrapper)
    const stale = panelItems().find((li) => li.textContent?.includes('deadbeef'))
    expect(stale).toBeTruthy()
    expect(stale!.classList.contains('is-stale')).toBe(true)
    expect(panel()!.querySelector('.ss-note')?.textContent).toContain('已按原样保留')
    wrapper.unmount()
  })

  it('选项为空时多选回车可直接录入，不阻塞表单', async () => {
    const wrapper = select({ modelValue: [], options: [], multiple: true })
    await open(wrapper)
    expect(panel()!.querySelector('.ss-empty')).toBeTruthy()
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'custom-model'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['custom-model']])
    wrapper.unmount()
  })

  it('Escape 收起面板，禁用时不展开', async () => {
    const wrapper = select({ modelValue: '', options: opts })
    await open(wrapper)
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'Escape' })
    await nextTick()
    await flushPromises()
    expect(panel()).toBeFalsy()
    wrapper.unmount()

    const disabled = select({ modelValue: '', options: opts, disabled: true })
    await disabled.get('.ss-trigger').trigger('click')
    expect(disabled.get('.ss-trigger').attributes('aria-expanded')).toBe('false')
    disabled.unmount()
  })

  it('方向键移动高亮，Enter 选中高亮项', async () => {
    const wrapper = select({ modelValue: '', options: opts })
    await open(wrapper)
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'ArrowDown' })
    await nextTick()
    expect(panel()!.querySelector('[data-active="true"]')!.textContent).toContain('ChatGPT')
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'ArrowUp' })
    await nextTick()
    expect(panel()!.querySelector('[data-active="true"]')!.textContent).toContain('Claude')
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['anthropic'])
    wrapper.unmount()
  })

  it('暴露 listbox / option 的 aria 关系', async () => {
    const wrapper = select({ id: 'vendor', modelValue: '', options: opts, multiple: true, placeholder: '厂商' })
    expect(wrapper.get('#vendor').attributes('aria-haspopup')).toBe('listbox')
    expect(wrapper.get('#vendor').attributes('aria-expanded')).toBe('false')
    await open(wrapper)
    expect(wrapper.get('#vendor').attributes('aria-expanded')).toBe('true')
    const list = document.body.querySelector('#vendor-listbox')!
    expect(list.getAttribute('role')).toBe('listbox')
    expect(list.getAttribute('aria-multiselectable')).toBe('true')
    expect(list.querySelector('.ss-opt')!.getAttribute('role')).toBe('option')
    expect(list.querySelector('.ss-opt')!.getAttribute('aria-selected')).toBe('false')
    wrapper.unmount()
  })

  it('远程搜索：打开时拉初始列表，输入关键字后替换结果', async () => {
    const calls: string[] = []
    const remote = async (q: string) => {
      calls.push(q)
      if (q === 'zzz') return []
      return [
        { value: '42', text: '#42 · buyer@market.test', keywords: '42 buyer@market.test' },
        { value: '7', text: '#7 · seller@market.test', keywords: '7 seller@market.test' },
      ]
    }
    const wrapper = select({ modelValue: '', options: [], remote })
    await open(wrapper)
    await flushPromises()
    // 打开时以空查询拉一次初始列表
    expect(calls[0]).toBe('')
    expect(panelItems().some((li) => li.textContent?.includes('buyer@market.test'))).toBe(true)
    // 输入关键字触发防抖后的远程搜索
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'zzz'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    // 防抖 250ms 后才发请求；快进定时器
    await new Promise((r) => setTimeout(r, 280))
    await flushPromises()
    expect(calls.at(-1)).toBe('zzz')
    // 无匹配且非多选，不提示可回车录入
    expect(panel()!.querySelector('.ss-empty')!.textContent).toContain('没有匹配「zzz」')
    wrapper.unmount()
  })

  it('远程搜索失败时提示错误，不阻塞已选值', async () => {
    let reject = false
    const remote = async () => {
      if (reject) throw new Error('boom')
      return [{ value: '42', text: '#42 · buyer@market.test' }]
    }
    const wrapper = select({ modelValue: '42', options: [], remote })
    // 预填已选值：远程结果加载前不应显示“未注册”，至少保留原值
    expect(wrapper.get('.ss-trigger').text()).toContain('42')
    expect(wrapper.get('.ss-trigger').text()).not.toContain('未注册')
    reject = true
    await open(wrapper)
    await flushPromises()
    // 远程失败后提示错误
    const note = document.body.querySelector('.ss-note')
    expect(note?.textContent ?? '').toContain('搜索失败')
    wrapper.unmount()
  })

  it('面板打开后，在 teleport 出去的 .ss-search 上派发键盘事件能选中值', async () => {
    const wrapper = select({ modelValue: '', options: opts })
    await open(wrapper)
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    // 無点已移到搜索框；在这里按方向键 + Enter 应能选中，不应因事件不冒泡而失效
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await nextTick()
    expect(panel()!.querySelector('[data-active="true"]')!.textContent).toContain('ChatGPT')
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['openai'])
    wrapper.unmount()
  })

  it('远程单选选中后，触发器显示远程返回的 text，重开 / 关闭后也不变成“未注册”', async () => {
    const remote = async () => [
      { value: '42', text: '#42 · buyer@market.test' },
    ]
    const wrapper = select({ modelValue: '', options: [], remote })
    await open(wrapper)
    await flushPromises()
    panelItems()[0]!.click()
    await flushPromises()
    // 模拟父组件回传 modelValue
    await wrapper.setProps({ modelValue: '42' })
    // 选中后触发器显示远程返回的可读文案
    expect(wrapper.get('.ss-trigger').text()).toContain('#42 · buyer@market.test')
    expect(wrapper.get('.ss-trigger').text()).not.toContain('未注册')
    // 重新打开面板（会再次拉远程）后关闭，已选值仍可读
    await open(wrapper)
    await flushPromises()
    await wrapper.get('.ss-trigger').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(wrapper.get('.ss-trigger').text()).toContain('#42 · buyer@market.test')
    expect(wrapper.get('.ss-trigger').text()).not.toContain('未注册')
    wrapper.unmount()
  })

  it('多选时不存在 button 嵌套 button，也不会重复显示已选值', async () => {
    const wrapper = select({ modelValue: ['anthropic', 'openai'], options: opts, multiple: true, placeholder: '厂商' })
    const triggerEl = wrapper.get('.ss-trigger').element as HTMLElement
    // 触发器不再是 button，避免标签删除按钮嵌在 button 里（非法 HTML）
    expect(triggerEl.tagName).toBe('DIV')
    // 不会出现 button 嵌套 button
    expect(triggerEl.querySelector('button:scope button')).toBeNull()
    // 已选值只以标签形式出现一次，不再同时输出 “A、B” 汇总文本
    const tags = wrapper.findAll('.ss-tag')
    expect(tags).toHaveLength(2)
    // 有已选值时不输出汇总 .ss-value 文本，只输出 placeholder 占位
    const valueSpan = wrapper.get('.ss-value')
    expect(valueSpan.text()).toBe('厂商')
    wrapper.unmount()
  })

  it('options 为空时单选可以回车录入输入文本', async () => {
    const wrapper = select({ modelValue: '', options: [], placeholder: '上游' })
    await open(wrapper)
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'upstream-9'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['upstream-9'])
    wrapper.unmount()
  })

  it('options 非空时输入不存在的值按回车不会录入，避免拼错写入脏数据', async () => {
    const wrapper = select({ modelValue: '', options: opts, placeholder: '选择厂商' })
    await open(wrapper)
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'typo'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    // 选项正常加载时拒绝录入不存在的值
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    wrapper.unmount()
  })

  it('loading 时即使 options 为空也不允许回车录入', async () => {
    const wrapper = select({ modelValue: '', options: [], loading: true, placeholder: '厂商' })
    await open(wrapper)
    const search = panel()!.querySelector<HTMLInputElement>('.ss-search')!
    search.value = 'x'
    search.dispatchEvent(new Event('input'))
    await flushPromises()
    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    wrapper.unmount()
  })
})