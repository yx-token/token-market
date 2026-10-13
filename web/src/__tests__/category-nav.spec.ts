import { describe, expect, it } from 'vitest'
import { buildCategoryNavGroups } from '@/market/config'

const tree = [
  {
    id: 'vendor',
    name: '厂商专场',
    sort: 10,
    children: [
      { id: 'claude', name: 'Claude', sort: 10 },
      { id: 'chatgpt', name: 'ChatGPT', sort: 20 },
      { id: 'gemini', name: 'Gemini', sort: 30 },
      { id: 'deepseek', name: 'DeepSeek', sort: 90 },
      { id: 'meta_llama', name: 'Llama / Meta', sort: 120 },
      { id: 'other_vendor', name: '其他厂商', sort: 80 },
    ],
  },
  {
    id: 'usecase',
    name: '用途场景',
    sort: 50,
    children: [
      { id: 'uc_programming', name: '编程开发', sort: 10 },
      { id: 'uc_agents', name: '智能体 / 工具', sort: 20 },
      { id: 'uc_legal', name: '法律合规', sort: 140 },
      { id: 'uc_marketing', name: '营销增长', sort: 60 },
    ],
  },
]

describe('buildCategoryNavGroups', () => {
  it('侧栏每行最多 3 个快捷入口，厂商默认 Claude/ChatGPT/Gemini', () => {
    const groups = buildCategoryNavGroups(tree)
    for (const g of groups) {
      expect(g.labels.length).toBeLessThanOrEqual(3)
    }
    expect(groups[0]!.labels.map((l) => l.text)).toEqual(['Claude', 'ChatGPT', 'Gemini'])
  })

  it('飞层收录该根下全部子类，不丢项', () => {
    const vendor = buildCategoryNavGroups(tree).find((g) => g.id === 'vendor')!
    const texts = vendor.sections.flatMap((s) => s.items.map((i) => i.text))
    expect(texts).toEqual(expect.arrayContaining(['Claude', 'DeepSeek', 'Llama / Meta', '其他厂商']))
    expect(texts).toHaveLength(6)
  })

  it('厂商飞层按国际 / 国内 / 云与开源切开', () => {
    const vendor = buildCategoryNavGroups(tree).find((g) => g.id === 'vendor')!
    expect(vendor.sections.map((s) => s.title)).toEqual(['国际头部', '国内头部', '云与开源'])
    expect(vendor.sections[0]!.items.map((i) => i.text)).toContain('Claude')
    expect(vendor.sections[1]!.items.map((i) => i.text)).toContain('DeepSeek')
    expect(vendor.sections[2]!.items.map((i) => i.text)).toContain('Llama / Meta')
  })

  it('用途飞层按主题切开，不再一整坨', () => {
    const uc = buildCategoryNavGroups(tree).find((g) => g.id === 'usecase')!
    expect(uc.sections.length).toBeGreaterThanOrEqual(2)
    expect(uc.sections.some((s) => s.title === '开发与对话')).toBe(true)
    expect(uc.sections.some((s) => s.title === '专业领域')).toBe(true)
  })
})
