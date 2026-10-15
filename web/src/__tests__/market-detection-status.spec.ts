import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MarketDetectionSummary from '@/components/MarketDetectionSummary.vue'
import { detectionText, modelDetectionRows } from '@/api/market-detection'

describe('市场与模型检测证据展示', () => {
  it('缺失字段视为未检测，声明模型无证据不能推断通过', () => {
    expect(detectionText()).toBe('未检测')
    expect(modelDetectionRows(['gpt-6-astra', 'gpt-6.1-sol'])).toEqual([
      { model: 'gpt-6-astra', detection_status: 'untested', degradation_status: 'untested' },
      { model: 'gpt-6.1-sol', detection_status: 'untested' },
    ])
  })

  it('未检测模型的过期 passed 字段不显示为通过，其他模型不展示降智结果', () => {
    const wrapper = mount(MarketDetectionSummary, { props: {
      status: 'untested', models: ['gpt-6-astra', 'gpt-6.1-sol'],
      checks: [
        { model: 'gpt-6-astra', detection_status: 'untested', degradation_status: 'passed' },
        { model: 'gpt-6.1-sol', detection_status: 'tested', degradation_status: 'failed' },
      ],
    } })
    expect(wrapper.text()).toContain('gpt-6-astra · 未检测 · 降智检测：未检测')
    expect(wrapper.text()).toContain('gpt-6.1-sol · 已检测')
    expect(wrapper.text()).not.toContain('通过')
    expect(wrapper.text().match(/降智检测/g)).toHaveLength(1)
    wrapper.unmount()
  })

  it.each([
    ['passed', '通过'], ['failed', '未通过'], ['inconclusive', '无法判定'], ['untested', '未检测'],
  ] as const)('gpt-6-astra 明确呈现 %s 结果', (status, text) => {
    const wrapper = mount(MarketDetectionSummary, { props: {
      status: 'tested', models: ['gpt-6-astra'], checks: [{ model: 'gpt-6-astra', detection_status: 'tested', degradation_status: status }],
    } })
    expect(wrapper.text()).toContain(`降智检测：${text}`)
    wrapper.unmount()
  })
})
