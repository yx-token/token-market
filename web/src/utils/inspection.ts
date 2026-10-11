import type { InspectionAppraisal, InspectionCheckResult, ProbeRunResult, Report } from '@/api/types'

export const inspectionCategories: Record<string, string> = {
  configuration: '配置与权限', authenticity: '真伪与一致性', capability: '能力与智力',
  function: '功能兑现', availability: '可用性', performance: '性能', display: '说明项',
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
  return report.verdict === 'degraded' || report.verdict === 'unavailable' || Object.values(report.summary.checks ?? {}).some((check) => check.applied && check.role === 'factor' && check.factor !== null && check.factor < 1)
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
