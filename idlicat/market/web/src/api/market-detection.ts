export type DetectionStatus = 'untested' | 'tested'
export type DegradationStatus = 'untested' | 'passed' | 'failed' | 'inconclusive'

export interface ModelCheck {
  model: string
  detection_status: DetectionStatus
  degradation_status?: DegradationStatus
}

/** Missing or legacy fields never count as evidence that a check passed. */
export function isTested(status?: DetectionStatus): boolean {
  return status === 'tested'
}

export function detectionText(status?: DetectionStatus): string {
  return isTested(status) ? '已检测' : '未检测'
}

export function modelDetectionRows(models: string[] = [], checks: ModelCheck[] = []): ModelCheck[] {
  return models.map((model) => {
    const check = checks.find((row) => row.model === model)
    const tested = isTested(check?.detection_status)
    return {
      model,
      detection_status: tested ? 'tested' : 'untested',
      ...(model === 'gpt-6-astra' ? { degradation_status: tested ? check?.degradation_status ?? 'untested' : 'untested' } : {}),
    }
  })
}

export function degradationText(status?: DegradationStatus): string {
  const labels: Record<DegradationStatus, string> = {
    untested: '未检测', passed: '通过', failed: '未通过', inconclusive: '无法判定',
  }
  return status ? labels[status] ?? '未检测' : '未检测'
}
