// 显示格式。金额按 1:1 结算（1 个余额单位 = 1 元，决策 D16）。

export function money(v: number | null | undefined, digits = 2): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  const abs = Math.abs(v)
  const d = abs > 0 && abs < 0.01 ? 4 : digits
  return `¥${v.toFixed(d)}`
}

export function multiplier(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—'
  return `${Number(v.toFixed(4))}×`
}

export function percent(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined) return '—'
  return `${(v * 100).toFixed(digits)}%`
}

export function dateTime(v: string | null | undefined): string {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function ms(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—'
  return v >= 1000 ? `${(v / 1000).toFixed(2)} s` : `${Math.round(v)} ms`
}

export function tokens(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—'
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`
  return String(v)
}

/** 把用户输入的金额解析成最多两位小数的数字，非法返回 null */
export function parseAmount(s: string): number | null {
  const t = s.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(t)) return null
  const n = Number(t)
  return n > 0 ? n : null
}
