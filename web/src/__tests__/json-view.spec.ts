import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import JsonTree from '@/components/JsonTree.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import { detectJson } from '@/utils/jsonDetect'

describe('JSON 识别', () => {
  it('整段对象、数组与 ```json 代码块识别为 JSON', () => {
    expect(detectJson('{"a":1}')).toEqual({ kind: 'json', value: { a: 1 }, fenced: false })
    expect(detectJson('  [1, 2]\n')).toEqual({ kind: 'json', value: [1, 2], fenced: false })
    expect(detectJson('```json\n{"house": [1, 2]}\n```')).toEqual({ kind: 'json', value: { house: [1, 2] }, fenced: true })
    expect(detectJson('```\n[{"x": true}]\n```')).toMatchObject({ kind: 'json', fenced: true })
  })

  it('普通文本、单个标量和混排文本不当作 JSON', () => {
    expect(detectJson('答案是 42')).toBeNull()
    expect(detectJson('42')).toBeNull()
    expect(detectJson('"文本"')).toBeNull()
    expect(detectJson('结果如下：\n```json\n{"a":1}\n```')).toBeNull()
    expect(detectJson('')).toBeNull()
    expect(detectJson(null)).toBeNull()
  })

  it('形似 JSON 但解析失败（截断）标记为 broken', () => {
    expect(detectJson('{"houses": [{"color": "red", "pet": "zeb')).toEqual({ kind: 'broken', fenced: false })
    expect(detectJson('```json\n{"a": 1, "b": [1, 2')).toEqual({ kind: 'broken', fenced: true })
  })
})

describe('JSON 结构视图', () => {
  it('默认展开两层，更深层折叠并显示项数，点击后展开', async () => {
    const wrapper = mount(JsonTree, { props: { value: { a: { b: { c: [1, 2, 3] } }, n: null, ok: true } } })
    expect(wrapper.text()).toContain('"a":')
    expect(wrapper.text()).toContain('"b":')
    expect(wrapper.text()).toContain('… 1 项')
    expect(wrapper.text()).not.toContain('"c":')
    const collapsed = wrapper.findAll('button[aria-expanded="false"]')
    expect(collapsed).toHaveLength(1)
    await collapsed[0].trigger('click')
    expect(wrapper.text()).toContain('"c":')
    expect(wrapper.text()).toContain('null')
    expect(wrapper.text()).toContain('true')
  })

  it('大数组只渲染前 200 项并提示剩余数量', () => {
    const wrapper = mount(JsonTree, { props: { value: Array.from({ length: 205 }, (_, i) => i) } })
    expect(wrapper.text()).toContain('另有 5 项未显示')
  })

  it('字符串值按文本显示，不形成 HTML 节点', () => {
    const wrapper = mount(JsonTree, { props: { value: { x: '<img src=x onerror=alert(1)>' } } })
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
  })
})

describe('MarkdownText 的 JSON 回答', () => {
  it('JSON 回答渲染成结构视图，可切换原文', async () => {
    const wrapper = mount(MarkdownText, { props: { text: '{"answer": 21, "steps": ["a", "b"]}' } })
    expect(wrapper.find('[data-testid="json-view"]').exists()).toBe(true)
    expect(wrapper.find('.markdown-body').exists()).toBe(false)
    expect(wrapper.text()).toContain('结构')
    expect(wrapper.text()).toContain('JSON')
    expect(wrapper.text()).toContain('"answer":')
    await wrapper.findAll('button').find((b) => b.text() === '原文')!.trigger('click')
    expect(wrapper.find('[data-testid="json-view"]').exists()).toBe(false)
    expect(wrapper.find('pre').text()).toBe('{"answer": 21, "steps": ["a", "b"]}')
  })

  it('被截断的 JSON 按原文等宽显示并说明原因，不走 Markdown', () => {
    const wrapper = mount(MarkdownText, { props: { text: '{"houses": [{"color": "red", "pet": "zeb', truncated: true } })
    expect(wrapper.find('[data-testid="json-view"]').exists()).toBe(false)
    expect(wrapper.find('.markdown-body').exists()).toBe(false)
    expect(wrapper.find('pre').exists()).toBe(true)
    expect(wrapper.text()).toContain('JSON 不完整')
    expect(wrapper.text()).toContain('已截断')
  })

  it('非 JSON 文本仍按 Markdown 渲染', () => {
    const wrapper = mount(MarkdownText, { props: { text: '**加粗** 答案' } })
    expect(wrapper.find('.markdown-body strong').exists()).toBe(true)
    expect(wrapper.text()).toContain('渲染')
  })
})
