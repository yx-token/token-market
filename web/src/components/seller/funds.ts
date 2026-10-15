/** 卖家资金说明。公开配置优先用 freeze_hours；旧响应才退回天数。 */
export function resolveFreezeHours(config: { freeze_hours?: number; freeze_days?: number }): number {
  if (typeof config.freeze_hours === 'number' && Number.isFinite(config.freeze_hours)) return config.freeze_hours
  const days = Number(config.freeze_days ?? 1)
  return Math.max(0, days) * 24
}

export function freezeTimingText(hours: number): string {
  if (hours <= 0) return '没有额外冻结。用量入账后仍要等到该小时结束才可提现，可能再等不足 1 小时。'
  return `冻结 ${hours} 小时。从该小时结束起算，不是从每一笔请求立刻起算。`
}
