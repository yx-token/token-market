import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MarkdownText from '@/components/MarkdownText.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import { renderMarkdown } from '@/utils/markdown'
import type { ProbeRunDetail as RunDetail, ProbeRunResult } from '@/api/types'

const run = { id: 1, upstream_id: 2, batch_id: 'batch', trigger: 'admin', model: '模型', status: 'done', items: 2, passed: 1, queued_at: '2026-10-12T04:00:00Z' }
const base: ProbeRunResult = { item_id: 7, suite: 'inspection_capability', category: 'logic', prompt: '题面', models: [], passed: true, ttft_ms: 12, duration_ms: 24, output_tokens: 3, reasoning_tokens: 4, expected_answer: '42' }

function detail(results: ProbeRunResult[], extra: Partial<RunDetail> = {}): RunDetail {
  return { run, results, ...extra }
}

describe('Markdown 渲染安全', () => {
  it('html:false 时 <script> 与 <img onerror> 一律转义，不形成可执行节点', () => {
    const hostile = '正文 <script>alert(1)</script> 与 <img src=x onerror=alert(2)> 结尾'
    const html = renderMarkdown(hostile)
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;img')

    const wrapper = mount(MarkdownText, { props: { text: hostile } })
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>alert(1)</script>')
  })

  it('截断的不完整 Markdown 降级为可读文本，并标注「已截断」', () => {
    const broken = '| 表头 | 值 |\n|---|---|\n| 甲 | **加粗的未闭合'
    const wrapper = mount(MarkdownText, { props: { text: broken, truncated: true } })
    expect(wrapper.text()).toContain('已截断')
    expect(wrapper.find('.markdown-body').exists()).toBe(true)
  })
})

describe('MarkdownText 渲染 / 原文切换与折叠', () => {
  it('默认渲染视图，可切换原文等宽显示', async () => {
    const wrapper = mount(MarkdownText, { props: { text: '**加粗** 与 `代码`' } })
    expect(wrapper.find('.markdown-body strong').exists()).toBe(true)
    expect(wrapper.find('pre').exists()).toBe(false)
    const raw = wrapper.findAll('button').find((b) => b.text() === '原文')!
    await raw.trigger('click')
    expect(wrapper.find('pre').exists()).toBe(true)
    expect(wrapper.find('pre').text()).toContain('**加粗**')
  })

  it('长文本折叠并提供「展开 / 收起」按钮，带 aria-expanded', async () => {
    const long = Array.from({ length: 8 }, (_, i) => `第 ${i} 行内容`.repeat(12)).join('\n')
    const wrapper = mount(MarkdownText, { props: { text: long, collapsedLines: 3 } })
    const buttons = wrapper.findAll('button')
    const toggle = buttons.find((b) => b.text().includes('展开'))!
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(wrapper.findAll('button').find((b) => b.text().includes('收起'))!.attributes('aria-expanded')).toBe('true')
  })

  it('短文本不显示展开按钮；空文本显示占位', () => {
    const short = mount(MarkdownText, { props: { text: '一行' } })
    expect(short.findAll('button').filter((b) => b.text().includes('展开'))).toHaveLength(0)
    const empty = mount(MarkdownText, { props: { text: '' } })
    expect(empty.text()).toContain('—')
  })
})

describe('检测明细：套件分组、答案列与失败行', () => {
  it('逐题结果按套件分组，分组行带题数', () => {
    const wrapper = mount(ProbeRunDetail, {
      props: {
        detail: detail([
          { ...base, item_id: 1, suite: 'inspection_capability', category: 'logic' },
          { ...base, item_id: 2, suite: 'inspection_capability', category: 'math' },
          { ...base, item_id: 3, suite: 'iq', category: 'word' },
        ]),
      },
    })
    const groups = wrapper.findAll('[data-testid="probe-run-results"] tbody tr.bg-slate-50')
    expect(groups).toHaveLength(2)
    expect(wrapper.text()).toContain('能力质检')
    expect(wrapper.text()).toContain('（2 题）')
    expect(wrapper.text()).toContain('智力')
  })

  it('showAnswer 默认关闭：不渲染标准答案；开启后管理员可见并与输出并排', () => {
    const sellerSide = mount(ProbeRunDetail, { props: { detail: detail([{ ...base }]) } })
    expect(sellerSide.text()).not.toContain('标准答案')
    expect(sellerSide.text()).not.toContain('42')

    const adminSide = mount(ProbeRunDetail, { props: { detail: detail([{ ...base }]), showAnswer: true } })
    expect(adminSide.text()).toContain('模型输出 / 标准答案')
    expect(adminSide.text()).toContain('标准答案')
    expect(adminSide.find('.lg\\:grid-cols-2').exists()).toBe(true)
    expect(adminSide.text()).toContain('42')
  })

  it('答案缺失时显示占位符而不报错', () => {
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail([{ ...base, expected_answer: undefined }]), showAnswer: true } })
    expect(wrapper.text()).toContain('标准答案')
    expect(wrapper.find('[data-testid="probe-run-results"]').text()).toContain('—')
  })

  it('未通过与报错行带红色底纹，通过行不带', () => {
    const wrapper = mount(ProbeRunDetail, {
      props: {
        detail: detail([
          { ...base, item_id: 1, passed: false },
          { ...base, item_id: 2, passed: true },
          { ...base, item_id: 3, passed: null, error_class: 'http_error' },
        ]),
      },
    })
    const rows = wrapper.findAll('[data-result-key]')
    expect(rows[0].classes()).toContain('bg-red-50/70')
    expect(rows[1].classes()).not.toContain('bg-red-50/70')
    expect(rows[2].classes()).toContain('bg-red-50/70')
  })

  it('没有逐题结果时显示空状态而不是空表', () => {
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail([]) } })
    expect(wrapper.find('[data-testid="probe-run-results"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('这次检测没有记录逐题结果')
    const running = mount(ProbeRunDetail, { props: { detail: detail([], { run: { ...run, status: 'running' } }) } })
    expect(running.text()).toContain('检测尚未完成')
  })
})
