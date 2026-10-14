import type { InspectionAppraisal, InspectionCheckResult, ProbeRunResult, Report } from '@/api/types'

export const inspectionCategories: Record<string, string> = {
  configuration: '配置与权限', authenticity: '真伪与一致性', capability: '能力与智力',
  identity: '身份核验', function: '功能兑现', availability: '可用性', performance: '性能', display: '说明项',
}

export const inspectionStatuses: Record<string, string> = {
  pass: '通过', warning: '需关注', fail: '未通过', inconclusive: '无法判定', not_applicable: '不适用', not_run: '未测',
}

export function appraisalText(appraisal?: InspectionAppraisal): string {
  if (!appraisal?.configured) return '未配置'
  if (appraisal.multiplier === null || appraisal.factor === null) return '暂不估价'
  return `${appraisal.multiplier.toFixed(3)}×`
}

export function appraisalDeviation(price: number | undefined, appraisal?: InspectionAppraisal): string {
  if (price === undefined || !Number.isFinite(price) || !appraisal?.configured || appraisal.multiplier === null || appraisal.factor === null || appraisal.multiplier <= 0) return '—'
  const deviation = (price / appraisal.multiplier - 1) * 100
  return `${deviation > 0 ? '+' : ''}${deviation.toFixed(1)}%`
}

export function appealableReport(report: Report): boolean {
  return report.verdict === 'degraded' || report.verdict === 'unavailable' || Object.values(report.summary.checks ?? {}).some((check) => check.applied && check.factor !== null && check.factor < 1)
}

export function inspectionSummary(checks?: Record<string, InspectionCheckResult>): string[] {
  return Object.values(checks ?? {}).sort((a, b) => {
    const rank = (check: InspectionCheckResult) => check.status === 'fail' ? 0 : check.status === 'warning' ? 1 : check.status === 'inconclusive' ? 2 : 3
    return rank(a) - rank(b) || a.key.localeCompare(b.key)
  }).slice(0, 3).map((check) => `${check.name}：${inspectionStatuses[check.status] ?? check.status}`)
}

export function probeResultKey(result: Pick<ProbeRunResult, 'item_id' | 'instance_idx' | 'step'>): string {
  return `${result.item_id ?? ''}:${result.instance_idx ?? 0}:${result.step ?? 0}`
}

// 检测套件代码的中文名；未知套件保留原值
export const probeSuiteNames: Record<string, string> = {
  inspection_capability: '测智',
  inspection_context: '测智·上下文深度',
  inspection_authenticity: '身份核验',
  inspection_function: '功能核验',
  inspection_output: '输出完整性',
  inspection_performance: '性能',
  iq: '旧智力题（已停用）',
  reasoning: '旧智力题（已停用）',
  long_context: '旧智力题（已停用）',
  availability: '可用率',
  latency: '延迟',
}

export const probeSuiteOrder = [
  'inspection_capability', 'inspection_context', 'inspection_authenticity',
  'inspection_function', 'inspection_output', 'inspection_performance',
  'availability', 'latency', 'iq', 'reasoning', 'long_context',
]

export function probeSuiteText(suite: string): string {
  return probeSuiteNames[suite] ?? suite
}

export function compareProbeSuites(a: string, b: string): number {
  const rank = (suite: string) => {
    const index = probeSuiteOrder.indexOf(suite)
    return index < 0 ? probeSuiteOrder.length : index
  }
  return rank(a) - rank(b) || a.localeCompare(b)
}

export const detectionFlowNames: Record<string, string> = {
  intelligence: '测智', degradation: '降智检测', identity: '身份与功能核验', common: '通用',
}

export function detectionFlowText(flow: string): string {
  return detectionFlowNames[flow] ?? flow
}

export const identitySourceNames: Record<string, string> = {
  third_party: '第三方检测', self: '自有题库',
}

// 核验结论文案；未知代码保留原值
export const verificationVerdictNames: Record<string, string> = {
  clean_match: '家族与具体型号都与声明一致',
  clean_match_family_only: '家族一致，具体型号无法确定',
  clean_match_submodel_mismatch: '家族一致，但不是声明的具体型号',
  plain_mismatch: '证据指向其他模型家族',
  spoof_behavior_induced: '行为被诱导成声明的模型，指纹指向其他模型',
  spoof_selfclaim_forged: '自称是声明的模型，但行为指向其他模型',
  ambiguous: '证据相互矛盾，无法判定',
  insufficient_data: '有效证据不足',
}

export function verificationVerdictText(code?: string): string {
  if (!code) return ''
  return verificationVerdictNames[code] ?? code
}

export function identitySourceText(source: string): string {
  return identitySourceNames[source] ?? source
}

export function intelligenceStatusText(status: string): string {
  const names: Record<string, string> = {
    pass: '正常', warning: '偏低（扣价）', fail: '显著偏低（疑似）',
    not_run: '未测', not_applicable: '不适用', inconclusive: '无法判定',
  }
  return names[status] ?? status
}

// 逐题结果标记的中文名；未知标记保留原值
export const probeFlagNames: Record<string, string> = {
  stop_normal: '正常结束',
  stop_error: '异常结束',
  unknown_stop_reason: '结束原因未知',
  truncated: '输出被截断',
  refusal: '拒答',
  reasoning_estimated: '推理量为估算',
  platform: '平台侧错误',
  rate_limited: '限流',
  abnormal_rate_limited: '异常限流',
  configuration: '配置错误',
  expected_reject: '预期拒绝',
  window_available: '额度窗口可用',
  window_unknown: '额度窗口未知',
}

export function probeFlagText(flag: string): string {
  return probeFlagNames[flag] ?? flag
}
