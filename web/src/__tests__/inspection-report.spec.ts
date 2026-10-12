import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ReportDetail from '@/components/ReportDetail.vue'
import InspectionAppraisal from '@/components/InspectionAppraisal.vue'
import ProbeRunDetail from '@/components/ProbeRunDetail.vue'
import MarketListingCard from '@/components/MarketListingCard.vue'
import { appealableReport, appraisalDeviation, appraisalText, probeResultKey } from '@/utils/inspection'
import type { InspectionAppraisal as Appraisal, InspectionCheckResult, ProbeRunDetail as RunDetail, Report } from '@/api/types'

const appraisal: Appraisal = { base: 1.5, factor: 0.85, multiplier: 1.275, tier: 'max', checked_at: '2026-10-12T04:00:00Z', configured: true, provisional: true }
const check: InspectionCheckResult = { key: 'D06', name: '推理强度兑现', category: 'capability', status: 'warning', role: 'factor', factor_group: 'capability', factor: 0.85, applied: false, reason: '配对观测不足，继续复测。', advice: '核对推理设置。', impact: '试运行，不影响价格。', checked_at: appraisal.checked_at! }

function report(extra: Partial<Report['summary']> = {}): Report {
  return { id: 1, upstream_id: 2, trigger: 'scheduled', verdict: 'normal', created_at: appraisal.checked_at!, impact: {}, summary: { verdict: 'normal', trigger: 'scheduled', model: '测试型号', checked_at: appraisal.checked_at!, baseline: true, suites: {}, inspection_mode: 'trial', grade: 'A', checks: { D06: check }, quality_factor: 0.85, appraisal, proposed_grade: 'B', repaired_grade: 'A', ...extra } }
}

describe('质检报告与估价', () => {
  it('旧报告继续展示原套件指标', () => {
    const wrapper = mount(ReportDetail, { props: { report: report({ checks: undefined, inspection_mode: undefined, suites: { availability: { success: 3, total: 4 }, iq: { relative_score: 0.95, samples: 8 } } }) } })
    expect(wrapper.text()).toContain('3 / 4 次成功')
    expect(wrapper.text()).toContain('95%（8 题')
    expect(wrapper.find('[data-testid="inspection-appraisal"]').exists()).toBe(false)
  })

  it('试运行按组显示候选结果与修复建议，保持原等级', () => {
    const value = report()
    Object.assign(value.summary.checks!.D06, { evidence: { answer: '秘密答案', threshold: 0.81, reference_ip: '内部地址' } })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('能力与智力')
    expect(wrapper.text()).toContain('推理强度兑现')
    expect(wrapper.text()).toContain('试运行，不影响价格')
    expect(wrapper.text()).toContain('候选系数，不生效 0.850')
    expect(wrapper.text()).toContain('试算等级')
    expect(wrapper.text()).toContain('修复后预计等级')
    expect(wrapper.text()).toContain('核对推理设置')
    expect(wrapper.text()).not.toContain('秘密答案')
    expect(wrapper.text()).not.toContain('内部地址')
    expect(wrapper.text()).not.toContain('0.81')
  })

  it('正式报告显示生效扣价与确认门槛，扣价可申诉', () => {
    const value = report({ inspection_mode: 'formal', quality_factor: null, appraisal: { ...appraisal, factor: null, multiplier: null }, checks: { D06: { ...check, applied: true, impact: '质量系数降低，影响回收报价。' } }, gates: [{ check_key: 'A04', code: 'model_unavailable', confirmed: true, health: 'unavailable', occurred_at: appraisal.checked_at!, reason: '声明型号权限未兑现。' }] })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('生效系数 0.850')
    expect(wrapper.text()).toContain('质量系数降低')
    expect(wrapper.text()).toContain('已确认，按健康限制处置')
    expect(wrapper.text()).toContain('暂不估价')
    expect(appealableReport(value)).toBe(true)
    expect(appealableReport(report())).toBe(false)
  })

  it('未配置与没有有效系数严格区分，不使用回收价兜底', () => {
    expect(appraisalText()).toBe('未配置')
    expect(appraisalText({ ...appraisal, configured: false })).toBe('未配置')
    expect(appraisalText({ ...appraisal, factor: null, multiplier: null })).toBe('暂不估价')
    expect(appraisalText(appraisal)).toBe('1.275×')
    const wrapper = mount(InspectionAppraisal, { props: { grade: 'B', appraisal: { ...appraisal, factor: null, multiplier: null }, qualityFactor: null, price: 1.5 } })
    expect(wrapper.text()).toContain('暂不估价')
    expect(wrapper.text()).toContain('无法判定')
    expect(wrapper.text()).toContain('偏离 —')
  })

  it('按卖家侧倍率计算偏离，试运行标明不生效', () => {
    expect(appraisalDeviation(1.53, appraisal)).toBe('+20.0%')
    expect(appraisalDeviation(1.02, appraisal)).toBe('-20.0%')
    expect(appraisalDeviation(1, { ...appraisal, multiplier: null })).toBe('—')
    const wrapper = mount(InspectionAppraisal, { props: { grade: 'A', appraisal, qualityFactor: 0.85, inspectionMode: 'trial', price: 1.53 } })
    expect(wrapper.text()).toContain('+20.0%')
    expect(wrapper.text()).toContain('等级、回收报价与调度沿用现有判定')
  })

  it('市场卡片显示关键项、检测时间与抽样局限', () => {
    const wrapper = mount(MarketListingCard, { props: { sku: { id: 'l-1', listingId: 1, kind: 'channel', plat: 'Claude', tier: 'Max', title: '测试通道', img: 'card1.jpg', mult: 0.8, grade: 'B', success: 0.97, ttft: 400, qualityChecks: { D06: check }, inspectionMode: 'trial', lastCheckedAt: appraisal.checked_at! } }, global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
    expect(wrapper.text()).toContain('B 级')
    expect(wrapper.text()).toContain('推理强度兑现：需关注')
    expect(wrapper.text()).toContain('上次检测')
    expect(wrapper.text()).toContain('试运行，不影响价格')
    expect(wrapper.text()).toContain('检测是抽样，不能保证没有偷换')
  })
})

describe('逐题实例配对', () => {
  it('同题不同实例与步骤按三元键配对，不覆盖基线', () => {
    const run = { id: 1, upstream_id: 2, batch_id: 'batch', trigger: 'admin', model: '模型', status: 'completed', items: 3, passed: 2, queued_at: appraisal.checked_at! }
    const base = { item_id: 7, suite: 'inspection_function', category: 'tools', prompt: '题面', models: [], passed: true, ttft_ms: 12, duration_ms: 24, output_tokens: 3, reasoning_tokens: 4 }
    const detail: RunDetail = { run, reference: { ...run, id: 2, upstream_id: 3 }, results: [{ ...base, instance_idx: 0, step: 0 }, { ...base, instance_idx: 0, step: 1 }, { ...base, instance_idx: 1, step: 0 }], reference_results: [{ ...base, instance_idx: 1, step: 0, passed: true }, { ...base, instance_idx: 0, step: 1, passed: false }, { ...base, instance_idx: 0, step: 0, passed: true }] }
    const wrapper = mount(ProbeRunDetail, { props: { detail } })
    // 原断言依赖列下标，把「套件 / 类别」列并进「题目」列后就读到了耗时列；
    // 改为在逐题表内按表头文本定位「基线」列，断言强度不变（仍校验配对后的基线结果）。
    const detailTable = wrapper.get('[data-testid="probe-run-results"]')
    const headers = detailTable.findAll('thead th').map((th) => th.text())
    const refCol = headers.indexOf('基线')
    expect(refCol).toBeGreaterThan(0)
    const refText = (key: string) => detailTable.get(`[data-result-key="${key}"]`).findAll('td')[refCol].text()
    expect(refText('7:0:0')).toBe('通过')
    expect(refText('7:0:1')).toBe('未通过')
    expect(refText('7:1:0')).toBe('通过')
    expect(probeResultKey({ item_id: 7 })).toBe('7:0:0')
  })
})
