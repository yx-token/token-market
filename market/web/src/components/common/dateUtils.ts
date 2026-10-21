/** 日期工具（YYYY-MM-DD，本地时区） */

export function formatYmd(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

export function parseYmd(s: string): Date | null {
  if (!s || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return null
  const [y, m, d] = s.split('-').map(Number)
  const dt = new Date(y, m - 1, d)
  if (dt.getFullYear() !== y || dt.getMonth() !== m - 1 || dt.getDate() !== d) return null
  return dt
}

export function addMonths(date: Date, n: number): Date {
  return new Date(date.getFullYear(), date.getMonth() + n, 1)
}

export function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1)
}

export function sameDay(a: string, b: string): boolean {
  return !!a && !!b && a === b
}

export function compareYmd(a: string, b: string): number {
  if (a === b) return 0
  return a < b ? -1 : 1
}

export function inRange(day: string, start: string, end: string): boolean {
  if (!day || !start || !end) return false
  const [lo, hi] = compareYmd(start, end) <= 0 ? [start, end] : [end, start]
  return day >= lo && day <= hi
}

export function displayZhDate(ymd: string): string {
  const d = parseYmd(ymd)
  if (!d) return ymd
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: 'short', day: 'numeric' })
}

export type CalendarCell = {
  ymd: string
  day: number
  inCurrentMonth: boolean
  isToday: boolean
}

/** 生成某月日历格（含上/下月填补，周日起始对齐 Element） */
export function buildMonthCells(view: Date, weekStartsOn: 0 | 1 = 1): CalendarCell[] {
  const year = view.getFullYear()
  const month = view.getMonth()
  const first = new Date(year, month, 1)
  const today = formatYmd(new Date())
  let startWeekday = first.getDay() // 0=Sun
  if (weekStartsOn === 1) startWeekday = (startWeekday + 6) % 7 // Mon=0
  const gridStart = new Date(year, month, 1 - startWeekday)
  const cells: CalendarCell[] = []
  for (let i = 0; i < 42; i++) {
    const d = new Date(gridStart.getFullYear(), gridStart.getMonth(), gridStart.getDate() + i)
    const ymd = formatYmd(d)
    cells.push({
      ymd,
      day: d.getDate(),
      inCurrentMonth: d.getMonth() === month,
      isToday: ymd === today,
    })
  }
  return cells
}

export const WEEKDAYS_ZH = ['一', '二', '三', '四', '五', '六', '日'] as const
