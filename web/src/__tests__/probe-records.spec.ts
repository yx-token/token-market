import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { admin } from '@/api'
import MarkdownText from '@/components/MarkdownText.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import ProbeRecordsView from '@/views/admin/ProbeRecordsView.vue'
import { renderMarkdown } from '@/utils/markdown'
import { detectionFlowText, identitySourceText, probeSuiteOrder, probeSuiteText } from '@/utils/inspection'
import type { ProbeRunDetail as RunDetail, ProbeRunRecord, ProbeRunResult } from '@/api/types'

vi.mock('@/api', () => ({ admin: { probeRecords: vi.fn(), probeItemStats: vi.fn(), probeRun: vi.fn(), probeItemResults: vi.fn() } }))

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
    expect(wrapper.text()).toContain('测智')
    expect(wrapper.text()).toContain('（2 题）')
    expect(wrapper.text()).toContain('旧智力题（已停用）')
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

describe('检测明细：同一道题的多条记录折叠', () => {
  const zebra = (instance_idx: number, extra: Partial<ProbeRunResult> = {}): ProbeRunResult => ({
    ...base,
    item_id: 12,
    category: '斑马谜题',
    prompt: '斑马谜题（现场参数化生成，仅输出最终答案）',
    check_key: 'D07',
    generator_version: 'zebra/2',
    instance_idx,
    step: 0,
    instance_prompt: `第 ${instance_idx} 份谜题`,
    ...extra,
  })
  const results = [
    zebra(0),
    zebra(1, { passed: false }),
    zebra(2),
    zebra(1000, { check_key: 'D06', effort: 'low', instance_prompt: '第 0 份谜题' }),
    { ...base, item_id: 30, category: '工具', instance_idx: 0, step: 0 },
  ]

  it('多条记录的题折叠成一行汇总，展开后显示各条', async () => {
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail(results), showAnswer: true } })
    const groups = wrapper.findAll('[data-item-group]')
    expect(groups).toHaveLength(1)
    expect(groups[0].text()).toContain('4 条记录：4 个实例')
    expect(groups[0].text()).toContain('含低推理档对照 1 条')
    expect(groups[0].text()).toContain('D07 / D06')
    expect(groups[0].text()).toContain('通过 3 / 4')
    expect(wrapper.text()).toContain('（2 题 · 5 条记录）')

    const nested = () => wrapper.findAll('[data-result-key^="12:"]')
    expect(nested()).toHaveLength(4)
    expect(nested().every((tr) => (tr.element as HTMLElement).style.display === 'none')).toBe(true)
    // 只有一条记录的题直接显示，不折叠
    expect((wrapper.get('[data-result-key="30:0:0"]').element as HTMLElement).style.display).toBe('')

    await groups[0].get('button').trigger('click')
    expect(groups[0].get('button').attributes('aria-expanded')).toBe('true')
    expect(nested().every((tr) => (tr.element as HTMLElement).style.display === '')).toBe(true)
    expect(wrapper.get('[data-result-key="12:1:0"]').text()).toContain('第 1 份谜题')
    expect(wrapper.get('[data-result-key="12:1000:0"]').text()).toContain('低推理档对照 · 实例 0')
  })

  it('全部展开 / 全部收起', async () => {
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail(results) } })
    const toggle = wrapper.findAll('button').find((b) => b.text() === '全部展开')!
    await toggle.trigger('click')
    expect((wrapper.get('[data-result-key="12:2:0"]').element as HTMLElement).style.display).toBe('')
    expect(toggle.text()).toBe('全部收起')
    await toggle.trigger('click')
    expect((wrapper.get('[data-result-key="12:2:0"]').element as HTMLElement).style.display).toBe('none')
  })

  it('实际题面与标准答案一样，只在管理员上下文显示', async () => {
    const sellerSide = mount(ProbeRunDetail, { props: { detail: detail(results) } })
    await sellerSide.findAll('button').find((b) => b.text() === '全部展开')!.trigger('click')
    expect(sellerSide.text()).not.toContain('第 1 份谜题')
    expect(sellerSide.text()).not.toContain('实际题面')
  })
})

describe('检测明细：截图反馈', () => {
  it('同一道题的记录按实例、步骤排序，而不是完成先后', async () => {
    const order = [2, 3, 0, 5, 1].map((instance_idx) => ({ ...base, item_id: 23, instance_idx, step: 0 }))
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail(order) } })
    const keys = wrapper.findAll('[data-result-key^="23:"]').map((tr) => tr.attributes('data-result-key'))
    expect(keys).toEqual(['23:0:0', '23:1:0', '23:2:0', '23:3:0', '23:5:0'])
  })

  it('只有达到 500 字上限的模型输出才标「已截断」', () => {
    const short = mount(ProbeRunDetail, { props: { detail: detail([{ ...base, excerpt: '颜色：黄，白，蓝' }]) } })
    expect(short.text()).not.toContain('已截断')
    const full = mount(ProbeRunDetail, { props: { detail: detail([{ ...base, excerpt: '答'.repeat(500) }]) } })
    expect(full.text()).toContain('已截断')
  })

  it('套件与结果标记显示中文名，悬停可见原始代码', () => {
    const wrapper = mount(ProbeRunDetail, {
      props: { detail: detail([{ ...base, suite: 'inspection_authenticity', flags: ['stop_normal', 'window_available', 'mystery_flag'] }]) },
    })
    expect(wrapper.text()).toContain('身份核验')
    expect(wrapper.find('[title="inspection_authenticity"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('inspection_authenticity')
    const flags = wrapper.get('[title="stop_normal, window_available, mystery_flag"]')
    expect(flags.text()).toBe('正常结束、额度窗口可用、mystery_flag')
  })

  it('逐题表只随弹窗纵向滚动，不自带滚动，也不设最小宽度撑出横向滚动条', () => {
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail([{ ...base }]) } })
    const table = wrapper.get('[data-testid="probe-run-results"]')
    expect(table.element.parentElement!.className).not.toMatch(/max-h-|overflow-/)
    expect(table.classes()).toContain('table-fixed')
    expect(table.classes().some((c) => c.startsWith('min-w-'))).toBe(false)
  })
})

describe('检测名称与新套件排序', () => {
  it.each([
    ['inspection_capability', '测智'], ['inspection_context', '测智·上下文深度'],
    ['inspection_authenticity', '身份核验'], ['inspection_function', '功能核验'],
    ['inspection_output', '输出完整性'], ['inspection_performance', '性能'],
    ['iq', '旧智力题（已停用）'], ['reasoning', '旧智力题（已停用）'],
    ['long_context', '旧智力题（已停用）'], ['availability', '可用率'], ['latency', '延迟'],
  ])('套件 %s 使用约定中文名', (suite, text) => {
    expect(probeSuiteText(suite)).toBe(text)
  })

  it('流程、身份来源使用中文，未知代码保留原值', () => {
    expect(detectionFlowText('intelligence')).toBe('测智')
    expect(detectionFlowText('identity')).toBe('身份与功能核验')
    expect(detectionFlowText('common')).toBe('通用')
    expect(identitySourceText('third_party')).toBe('第三方检测')
    expect(identitySourceText('self')).toBe('自有题库')
    expect(probeSuiteText('new_suite')).toBe('new_suite')
    expect(detectionFlowText('new_flow')).toBe('new_flow')
    expect(identitySourceText('new_source')).toBe('new_source')
  })

  it('明细汇总与逐题结果按现行套件顺序排列，旧套件在末尾', () => {
    const results = [...probeSuiteOrder].reverse().map((suite, index) => ({ ...base, item_id: index + 1, suite }))
    const wrapper = mount(ProbeRunDetail, { props: { detail: detail(results) } })
    const titles = wrapper.findAll('[data-testid="probe-run-results"] tbody tr.bg-slate-50 td').map((cell) => cell.attributes('title'))
    expect(titles).toEqual(probeSuiteOrder)
    const summary = wrapper.findAll('table')[0].findAll('tbody tr td:first-child').map((cell) => cell.attributes('title'))
    expect(summary).toEqual(probeSuiteOrder)
  })

  it('管理员明细显示测智状态与通过率，旧智力指标继续兜底', () => {
    const summary = {
      intelligence: {
        status: 'warning' as const, inherited_at: '2026-10-12T04:00:00Z',
        models: [{ model: 'gpt-6-astra', status: 'warning' as const, target_pass_rate: .86, expected_pass_rate: .9 }],
      },
    }
    const adminSide = mount(ProbeRunDetail, { props: { detail: detail([], { run: { ...run, summary } }), showAnswer: true } })
    expect(adminSide.get('[aria-label="测智指标"]').text()).toContain('偏低（扣价）')
    expect(adminSide.get('[aria-label="测智指标"]').text()).toContain('通过率 86.0% · 期望通过率 90.0%')
    expect(adminSide.get('[aria-label="测智指标"]').text()).toContain('沿用')
    const sellerSide = mount(ProbeRunDetail, { props: { detail: detail([], { run: { ...run, summary } }) } })
    expect(sellerSide.text()).not.toContain('通过率')
    const legacySide = mount(ProbeRunDetail, { props: { detail: detail([]) } })
    expect(legacySide.find('[aria-label="智力与推理指标"]').exists()).toBe(true)
  })
})

describe('检测记录流程字段', () => {
  const record: ProbeRunRecord = {
    ...run, platform: 'openai', auth_type: 'oauth', seller_id: 3, is_reference: false,
    suites: {
      iq: { items: 1, passed: 1 }, inspection_authenticity: { items: 2, passed: 2 },
      inspection_capability: { items: 3, passed: 2 }, inspection_context: { items: 1, passed: 1 },
    },
    flows: ['intelligence', 'identity'], identity_source: 'third_party', models: ['gpt-6-astra', 'gpt-6.1-sol'],
  }
  const dialog = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" /></section>' })
  const listPage = defineComponent({ template: '<main><slot name="tabs" /><slot name="filters" /><slot /><slot name="footer" /></main>' })

  async function page(records = [record]) {
    vi.mocked(admin.probeRecords).mockResolvedValue(records)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/console/probe-records', component: { template: '<div />' } }] })
    await router.push('/console/probe-records')
    await router.isReady()
    const wrapper = mount(ProbeRecordsView, { global: { plugins: [createPinia(), router], stubs: { ModalDialog: dialog, ConsoleListPage: listPage, DateRangePicker: true } } })
    await flushPromises()
    return wrapper
  }

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(admin.probeItemStats).mockResolvedValue([])
  })

  it('列表显示流程、身份来源、全部测试模型，套件汇总使用中文并排序', async () => {
    const wrapper = await page()
    const headers = wrapper.findAll('th').map((cell) => cell.text())
    const cells = wrapper.get('tbody tr').findAll('td')
    expect(cells[headers.indexOf('流程')].text()).toBe('测智 / 身份与功能核验')
    expect(cells[headers.indexOf('身份来源')].text()).toBe('第三方检测')
    expect(cells[headers.indexOf('测试模型')].text()).toBe('gpt-6-astra / gpt-6.1-sol')
    const suites = cells[headers.indexOf('答题')].get('div').findAll('span').map((span) => span.text())
    expect(suites).toEqual(['测智 2/3', '测智·上下文深度 1/1', '身份核验 2/2', '旧智力题（已停用） 1/1'])
    expect(wrapper.get('tbody').text()).not.toContain('inspection_capability')
    await wrapper.get('#pr-keyword').setValue('gpt-6.1-sol')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    wrapper.unmount()
  })

  it('旧记录缺少新字段时以主模型兜底，自有题库来源使用中文', async () => {
    const wrapper = await page([
      { ...record, id: 2, flows: undefined, identity_source: undefined, models: undefined },
      { ...record, id: 3, flows: ['identity'], identity_source: 'self' },
    ])
    const headers = wrapper.findAll('th').map((cell) => cell.text())
    const rows = wrapper.findAll('tbody tr')
    const legacy = rows[0].findAll('td')
    expect(legacy[headers.indexOf('流程')].text()).toBe('—')
    expect(legacy[headers.indexOf('身份来源')].text()).toBe('—')
    expect(legacy[headers.indexOf('测试模型')].text()).toBe('模型')
    const self = rows[1].findAll('td')
    expect(self[headers.indexOf('流程')].text()).toBe('身份与功能核验')
    expect(self[headers.indexOf('身份来源')].text()).toBe('自有题库')
    wrapper.unmount()
  })
})
