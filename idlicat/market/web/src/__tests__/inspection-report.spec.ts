import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
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
    expect(wrapper.find('[data-flow]').exists()).toBe(false)
  })

  it('试运行按组显示候选结果与修复建议，保持原等级', () => {
    const value = report()
    Object.assign(value.summary.checks!.D06, { evidence: { answer: '秘密答案', threshold: 0.81, reference_ip: '内部地址' } })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('能力与智力')
    expect(wrapper.text()).toContain('推理强度兑现')
    expect(wrapper.text()).toContain('试运行，不影响价格')
    expect(wrapper.text()).not.toContain('生效系数')
    expect(wrapper.text()).not.toContain('候选系数')
    expect(wrapper.text()).toContain('试算等级')
    expect(wrapper.text()).toContain('修复后预计等级')
    expect(wrapper.text()).toContain('核对推理设置')
    expect(wrapper.text()).not.toContain('秘密答案')
    expect(wrapper.text()).not.toContain('内部地址')
    expect(wrapper.text()).not.toContain('0.81')
    expect(wrapper.find('[data-flow]').exists()).toBe(false)
  })

  it('正式报告显示生效扣价与确认门槛，扣价可申诉', () => {
    const value = report({ inspection_mode: 'formal', quality_factor: null, appraisal: { ...appraisal, factor: null, multiplier: null }, checks: { D06: { ...check, applied: true, impact: '质量系数降低，影响回收报价。' } }, gates: [{ check_key: 'A04', code: 'model_unavailable', confirmed: true, health: 'unavailable', occurred_at: appraisal.checked_at!, reason: '声明型号权限未兑现。' }] })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('生效系数 0.850')
    expect(wrapper.text()).toContain('质量系数降低')
    expect(wrapper.text()).toContain('已确认，按健康限制处置')
    expect(wrapper.text()).toContain('暂不估价')
    expect(wrapper.text()).toContain('本次检测覆盖不足')
    expect(appealableReport(value)).toBe(true)
    expect(appealableReport(report())).toBe(false)
  })

  it('新报告按流程归类，核验结果归入身份区块并标明沿用时间', () => {
    const result = (key: string, name = key): InspectionCheckResult => ({ ...check, key, name, status: 'pass', role: 'display', factor: null, impact: '' })
    const keys = ['D05', 'D10', 'D04', 'D13', 'D07', 'D07.honesty', 'D09', 'D06', 'A06', 'A01', 'A02', 'A03', 'A12', 'A15', 'A04', 'A05', 'A11.tools', 'D08', 'D12', 'third_party']
    const value = report({
      flows: ['intelligence', 'identity'], identity_source: 'third_party', models: ['gpt-6-astra'],
      intelligence: { status: 'warning', inherited_at: '2026-10-10T04:00:00Z', models: [{ model: 'gpt-6-astra', status: 'warning', target_pass_rate: 0.8, expected_pass_rate: 0.9, samples: 57 }] },
      identity: { source: 'third_party', status: 'pass', inherited_at: '2026-10-11T04:00:00Z' },
      checks: Object.fromEntries(keys.map((key) => [key, result(key)])),
      verification: { status: 'pass', verdict_text: '家族与具体型号都与声明一致', categories: [{ key: 'identity', name: '身份识别', total: 29, passed: 29, warning: 0, failed: 0, error: 0 }], risks: [] },
    })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.findAll('[data-flow]').map((section) => section.attributes('data-flow'))).toEqual(['identity', 'common', 'intelligence'])
    // 质量等级紧跟身份与功能核验，排在其他检测区块之前
    const blocks = wrapper.findAll('[data-flow], [data-testid="quality-grade"]').map((node) => node.attributes('data-flow') ?? 'quality-grade')
    expect(blocks.slice(0, 3)).toEqual(['identity', 'quality-grade', 'common'])
    const common = wrapper.get('[data-flow="common"]')
    expect(common.text()).toContain('可用与性能')
    expect(common.findAll('[data-check-key]').map((row) => row.attributes('data-check-key'))).toEqual(['D13', 'D04', 'D10', 'D05'])
    const intelligence = wrapper.get('[data-flow="intelligence"]')
    expect(intelligence.findAll('[data-check-key]').map((row) => row.attributes('data-check-key'))).toEqual(['A06', 'D06', 'D07', 'D07.honesty', 'D09'])
    expect(intelligence.text()).toContain('偏低（扣价）')
    expect(intelligence.text()).toContain('沿用 2026-10-10')
    expect(intelligence.text()).toContain('的结果')
    const identity = wrapper.get('[data-flow="identity"]')
    expect(identity.findAll('[data-check-key]')).toHaveLength(11)
    expect(identity.find('[data-testid="idle-checks"]').exists()).toBe(false)
    // 卖家不需要知道这次怎么检测的：不显示来源，也不出现「第三方」字样
    expect(identity.text()).not.toContain('来源')
    expect(identity.text()).not.toContain('第三方')
    expect(identity.text()).toContain('家族与具体型号都与声明一致')
    expect(identity.text()).toContain('沿用 2026-10-11')
    expect(wrapper.findAllComponents({ name: 'ThirdPartyResult' })).toHaveLength(0)
    expect(wrapper.text()).not.toContain('试运行，不影响价格')
  })

  it('核验结果展示各大类计数与风险项，并隐藏渠道名与站点链接', () => {
    const value = report({
      flows: ['intelligence', 'identity'], identity_source: 'third_party', models: ['gpt-6-astra'],
      intelligence: { status: 'pass', inherited_at: null, models: [{ model: 'gpt-6-astra', status: 'pass' }] },
      identity: { source: 'third_party', status: 'warning', inherited_at: null },
      checks: { third_party: { ...check, key: 'third_party', name: '身份核验', status: 'warning', role: 'gate', factor: null, impact: '' } },
      verification: {
        status: 'fail',
        verdict_text: '证据指向其他模型家族',
        true_model: 'gpt-6.1-sol',
        checked_at: '2026-10-14T02:00:00Z',
        categories: [
          { key: 'identity', name: '身份识别', total: 29, passed: 29, warning: 0, failed: 0, error: 0 },
          { key: 'integrity', name: '完整性检查', total: 12, passed: 4, warning: 5, failed: 1, error: 2 },
        ],
        risks: [
          { category_name: '完整性检查', label: 'Context 长度測試', severity: 'fail', reason: 'HTTP 503' },
          { category_name: '安全探针', label: '越狱探针', severity: 'warning', reason: '' },
        ],
      },
      // 管理端字段即使混进卖家报告也不该被渲染
      third_party: [{ provider: 'bazaarlink', status: 'completed', verdict: 'degraded', model: 'gpt-6-astra', report_url: 'https://vendor.example/report/1', summary: { verdict_status: 'plain_mismatch' } }],
    })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    const block = wrapper.get('[data-testid="identity-verification"]')
    expect(block.text()).toContain('核验结果')
    expect(block.text()).not.toContain('第三方')
    expect(block.text()).toContain('证据指向其他模型家族')
    expect(block.text()).toContain('结论型号：gpt-6.1-sol')
    const categories = block.get('[data-testid="verification-categories"]').findAll('[data-category]')
    expect(categories.map((c) => c.attributes('data-category'))).toEqual(['identity', 'integrity'])
    expect(categories[0].text()).toContain('身份识别')
    expect(categories[0].text()).toContain('29 通过 / 共 29 项')
    expect(categories[1].text()).toContain('4 通过 · 5 警告 · 1 未通过 · 2 出错 / 共 12 项')
    const risks = block.get('[data-testid="verification-risks"]').findAll('[data-risk-severity]')
    expect(risks).toHaveLength(2)
    expect(risks[0].text()).toContain('完整性检查')
    expect(risks[0].text()).toContain('Context 长度測試')
    expect(risks[0].text()).toContain('（未通过）')
    expect(risks[0].text()).toContain('HTTP 503')
    expect(risks[1].text()).toContain('（警告）')
    expect(wrapper.text()).not.toContain('BazaarLink')
    expect(wrapper.text()).not.toContain('bazaarlink')
    expect(wrapper.text()).not.toContain('vendor.example')
    expect(wrapper.text()).not.toContain('查看检测站报告')
    wrapper.unmount()
  })

  it('没有风险项时显示未发现风险项，没有核验结果时不渲染该区块', () => {
    const base: Partial<Report['summary']> = {
      flows: ['identity'], identity_source: 'third_party', models: ['gpt-6-astra'],
      identity: { source: 'third_party', status: 'pass', inherited_at: null },
      checks: { third_party: { ...check, key: 'third_party', name: '身份核验', status: 'pass', role: 'gate', factor: null, impact: '' } },
    }
    const clean = mount(ReportDetail, { props: { report: report({ ...base, verification: { status: 'pass', verdict_text: '家族与具体型号都与声明一致', categories: [], risks: [] } }) } })
    expect(clean.get('[data-testid="verification-risks"]').text()).toContain('未发现风险项')
    clean.unmount()
    const missing = mount(ReportDetail, { props: { report: report({ ...base }) } })
    expect(missing.find('[data-testid="identity-verification"]').exists()).toBe(false)
    expect(missing.find('[data-testid="verification-result"]').exists()).toBe(false)
    missing.unmount()
  })

  it('降智流程只在报告确实跑过时出现，D14 归入降智区块', () => {
    const withFlow = mount(ReportDetail, {
      props: {
        report: report({
          intelligence: { status: 'pass', models: [] },
          degradation: { status: 'warning', inherited_at: null, models: [{ model: 'gpt-6-astra', status: 'warning' }] },
          identity: { source: 'self', status: 'not_run' },
          checks: { D14: { ...check, key: 'D14', name: '降智检测', status: 'warning', factor: null, impact: '' }, D07: { ...check, key: 'D07', name: '智力测试', status: 'pass' } },
        }),
      },
    })
    expect(withFlow.findAll('[data-flow]').map((section) => section.attributes('data-flow'))).toEqual(['identity', 'common', 'intelligence', 'degradation'])
    const degradation = withFlow.get('[data-flow="degradation"]')
    expect(degradation.text()).toContain('降智检测')
    expect(degradation.text()).toContain('偏低（扣价）')
    expect(degradation.findAll('[data-check-key]').map((row) => row.attributes('data-check-key'))).toEqual(['D14'])
    expect(withFlow.get('[data-flow="intelligence"]').findAll('[data-check-key]').map((row) => row.attributes('data-check-key'))).toEqual(['D07'])
    withFlow.unmount()
    // 旧报告没有降智结论时不多出整块「未测」
    const legacy = mount(ReportDetail, { props: { report: report({ intelligence: { status: 'pass', models: [] }, identity: { source: 'self', status: 'not_run' } }) } })
    expect(legacy.findAll('[data-flow]').map((section) => section.attributes('data-flow'))).toEqual(['identity', 'common', 'intelligence'])
    legacy.unmount()
  })

  it('未执行的检测项只给管理员看，卖家只看到核验结论', () => {
    const idle = (key: string, name: string): InspectionCheckResult => ({ ...check, key, name, status: 'not_run', role: 'display', factor: null, impact: '' })
    const value = report({
      flows: ['intelligence', 'identity'], identity_source: 'third_party', models: ['gpt-6-astra'],
      intelligence: { status: 'pass', inherited_at: null, models: [{ model: 'gpt-6-astra', status: 'pass' }] },
      identity: { source: 'third_party', status: 'pass', inherited_at: null },
      checks: {
        D07: { ...check, key: 'D07', name: '智力测试', status: 'pass', role: 'factor', factor: null, impact: '' },
        A02: idle('A02', '响应型号一致'),
        A11_tools: idle('A11.tools', '工具调用'),
        third_party: { ...check, key: 'third_party', name: '身份核验', status: 'pass', role: 'gate', factor: null, impact: '' },
      },
      verification: { status: 'pass', verdict_text: '家族与具体型号都与声明一致', categories: [], risks: [] },
    })
    const seller = mount(ReportDetail, { props: { report: value } })
    const sellerIdentity = seller.get('[data-flow="identity"]')
    expect(sellerIdentity.find('[data-testid="idle-checks"]').exists()).toBe(false)
    expect(sellerIdentity.text()).not.toContain('未执行')
    expect(sellerIdentity.get('[data-testid="verification-result"]').text()).toContain('家族与具体型号都与声明一致')
    seller.unmount()
    const identity = mount(ReportDetail, { props: { report: value, admin: true } }).get('[data-flow="identity"]')
    expect(identity.findAll('[data-check-key]').map((row) => row.attributes('data-check-key'))).toEqual(['third_party'])
    const folded = identity.get('[data-testid="idle-checks"]')
    expect(folded.element.hasAttribute('open')).toBe(false)
    expect(folded.text()).toContain('本次未执行的检测项（2 项）')
    expect(folded.get('[data-idle-keys]').attributes('data-idle-keys')).toBe('A02,A11.tools')
  })

  it('自有题库报告展示身份与功能结果，只有已生效的扣价显示系数', () => {
    const value = report({
      intelligence: { status: 'fail', inherited_at: null, models: [{ model: 'gpt-6-astra', status: 'fail' }] },
      identity: { source: 'self', status: 'warning', inherited_at: null },
      checks: {
        D06: { ...check, applied: true, impact: '已用于扣价。' },
        A04: { ...check, key: 'A04', name: '型号权限', category: 'configuration', role: 'gate', applied: true, factor: 0.7 },
        A11: { ...check, key: 'A11.tools', name: '工具调用', category: 'function', factor: 0.95 },
      },
      third_party: [{ provider: 'bazaarlink', status: 'completed', verdict: 'normal', model: '旧第三方型号' }],
    })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.get('[data-flow="intelligence"]').text()).toContain('显著偏低（疑似）')
    const identity = wrapper.get('[data-flow="identity"]')
    expect(identity.text()).not.toContain('来源')
    expect(identity.text()).toContain('型号权限')
    expect(identity.text()).toContain('工具调用')
    expect(wrapper.get('[data-check-key="D06"]').text()).toContain('生效系数 0.850')
    expect(identity.get('[data-check-key="A04"]').text()).toContain('生效系数 0.700')
    expect(identity.get('[data-check-key="A11.tools"]').text()).not.toContain('0.950')
    expect(appealableReport(report({ checks: { A04: value.summary.checks!.A04 } }))).toBe(true)
    expect(wrapper.text()).not.toContain('候选系数')
    expect(wrapper.text()).not.toContain('旧第三方型号')
    expect(wrapper.text()).not.toContain('沿用')
  })

  it('卖家测智只显示模型状态，管理员额外显示通过率和期望通过率', () => {
    const value = report({
      intelligence: { status: 'pass', models: [{ model: 'gpt-6-astra', status: 'pass', target_pass_rate: 0.96, expected_pass_rate: 0.9 }, { model: 'gpt-6.1-sol', status: 'not_run' }] },
      identity: { source: 'self', status: 'not_run' },
      checks: { D07: { ...check, key: 'D07', metrics: { target_pass_rate: { value: 0.96, unit: 'ratio' } } } },
    })
    const sellerWrapper = mount(ReportDetail, { props: { report: value } })
    const sellerModels = sellerWrapper.get('[data-testid="intelligence-models"]')
    expect(sellerModels.text()).toContain('gpt-6-astra')
    expect(sellerModels.text()).toContain('正常')
    expect(sellerModels.text()).toContain('gpt-6.1-sol')
    expect(sellerModels.text()).toContain('未测')
    expect(sellerWrapper.get('[data-flow="intelligence"]').text()).not.toContain('通过率')
    const adminWrapper = mount(ReportDetail, { props: { report: value, admin: true } })
    const adminModels = adminWrapper.get('[data-testid="intelligence-models"]')
    expect(adminModels.text()).toContain('通过率 96.0%')
    expect(adminModels.text()).toContain('期望通过率 90.0%')
    expect(adminModels.get('[data-model="gpt-6.1-sol"]').text()).toContain('通过率 —')
  })

  it('脱敏报告和不适用测智保持完整区块，不要求模型率或检测项', () => {
    const value = report({ checks: undefined, intelligence: { status: 'not_applicable', models: [] }, identity: { source: 'self', status: 'not_run' } })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.get('[data-flow="intelligence"]').text()).toContain('不适用')
    expect(wrapper.get('[data-flow="intelligence"]').text()).toContain('挂售模型未配置测智')
    expect(wrapper.get('[data-flow="identity"]').text()).toContain('未测')
    expect(wrapper.get('[data-flow="common"]').text()).toContain('暂无可用与性能检测结果')
  })

  it.each<{ name: string; extra: Partial<Report['summary']> }>([
    { name: '空流程列表', extra: { flows: [] } },
    { name: '身份来源', extra: { identity_source: 'self' } },
    { name: '空测试模型列表', extra: { models: [] } },
  ])('只有$name新字段时仍采用新版报告', ({ extra }) => {
    const value = report({ checks: undefined, suites: { iq: { relative_score: 0.95, samples: 8 } }, ...extra })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.findAll('[data-flow]').map((section) => section.attributes('data-flow'))).toEqual(['identity', 'common', 'intelligence'])
    expect(wrapper.text()).not.toContain('智力相对分')
  })

  it('新版估价试算提示表示未启用扣价，不再表示检测覆盖不足', () => {
    const value = report({
      intelligence: { status: 'pass', models: [] },
      appraisal: { ...appraisal, factor: 1, multiplier: 1.5, provisional: true },
      checks: {},
    })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('未启用扣价，估价为试算')
    expect(wrapper.text()).not.toContain('本次检测覆盖不足')
    expect(wrapper.text()).not.toContain('试运行，不影响价格')
    expect(value.summary.appraisal?.provisional).toBe(true)
  })

  it('新版试运行已生效的扣价不显示旧估价和价格提示', () => {
    const value = report({
      intelligence: { status: 'warning', models: [] },
      inspection_mode: 'trial',
      appraisal: { ...appraisal, provisional: false },
      checks: { D06: { ...check, applied: true, impact: '质量系数降低，已用于扣价。' } },
    })
    const wrapper = mount(ReportDetail, { props: { report: value } })
    expect(wrapper.text()).toContain('生效系数 0.850')
    expect(wrapper.text()).toContain('质量系数降低，已用于扣价。')
    expect(wrapper.text()).not.toContain('未启用扣价，估价为试算')
    expect(wrapper.text()).not.toContain('本次检测覆盖不足')
    expect(wrapper.text()).not.toContain('试运行，不影响价格')
    expect(wrapper.text()).not.toContain('等级与回收报价沿用现有判定')
    value.summary.checks!.D06.impact = ''
    const fallback = mount(ReportDetail, { props: { report: value } })
    expect(fallback.get('[data-check-key="D06"]').text()).toContain('本项已用于扣价。')
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
    expect(wrapper.text()).toContain('等级与回收报价沿用现有判定')
  })

  it('已检测市场卡片展示等级与基础指标（质检明细在详情页）', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    await router.push('/')
    const wrapper = mount(MarketListingCard, { props: { sku: { id: 'l-1', listingId: 1, kind: 'channel', plat: 'Claude', tier: 'Max', title: '测试通道', img: 'card1.jpg', mult: 0.8, grade: 'B', success: 0.97, ttft: 400, detectionStatus: 'tested', qualityChecks: { D06: check }, inspectionMode: 'trial', lastCheckedAt: appraisal.checked_at! } }, global: { plugins: [router], stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
    expect(wrapper.text()).toContain('测试通道')
    expect(wrapper.text()).toContain('B 级')
    expect(wrapper.text()).toContain('97%')
    expect(wrapper.text()).toContain('400ms')
    expect(wrapper.classes()).toContain('lib-card')
    wrapper.unmount()
  })

  it.each([undefined, 'untested'] as const)('市场卡片检测状态 %s 不因残留等级推断已检测', async (status) => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    await router.push('/')
    const wrapper = mount(MarketListingCard, { props: { sku: { id: 'l-1', listingId: 1, kind: 'channel', plat: 'ChatGPT', tier: 'Plus', title: '测试通道', img: 'card1.jpg', mult: 0.8, grade: 'B', success: 0.97, ttft: 400, detectionStatus: status } }, global: { plugins: [router] } })
    expect(wrapper.text()).toContain('测试通道')
    expect(wrapper.text()).not.toContain('B 级')
    expect(wrapper.classes()).toContain('lib-card')
    wrapper.unmount()
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
