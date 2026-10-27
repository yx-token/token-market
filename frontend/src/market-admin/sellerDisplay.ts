/** 管理端卖家展示：优先邮箱账号，不展示裸编号。 */
export function sellerAccount(
  email?: string | null,
  _id?: number | string | null,
): string {
  const e = String(email ?? '').trim()
  return e || '—'
}

/** 筛选项 label：邮箱（无邮箱时才退回编号，避免下拉里不可辨）。 */
export function sellerFilterLabel(
  email?: string | null,
  id?: number | string | null,
): string {
  const e = String(email ?? '').trim()
  if (e) return e
  const sid = String(id ?? '').trim()
  return sid ? `用户 ${sid}` : '—'
}
