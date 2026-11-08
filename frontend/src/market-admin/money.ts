/** 市场管理端金额展示：平台金额统一为人民币（后端已完成换算，前端不再乘汇率）。 */

const cnyFormatter = new Intl.NumberFormat('zh-CN', {
  style: 'currency',
  currency: 'CNY',
  currencyDisplay: 'narrowSymbol',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

/** 金额（CNY）→ `¥1,234.00`；非数字返回 `—`。 */
export function money(value: unknown): string {
  if (value == null || (typeof value === 'string' && !value.trim())) return '—'
  const amount = Number(value)
  return Number.isFinite(amount) ? cnyFormatter.format(amount) : '—'
}
