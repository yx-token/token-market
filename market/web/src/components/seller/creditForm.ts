import { parseAmount } from '@/utils/format'

export interface CreditDraft {
  title: string
  description: string
  price: string
  /** 额度面值，人民币 */
  face: string
  platforms: string[]
  models: string[]
}

export function emptyCreditDraft(): CreditDraft {
  return { title: '', description: '', price: '', face: '', platforms: [], models: [] }
}

export function creditFormError(draft: CreditDraft): string {
  if (!draft.title.trim()) return '请填写活动标题'
  if (draft.title.trim().length > 80) return '标题不要超过 80 字'
  if (parseAmount(draft.price) === null) return '实付价须为大于 0 的人民币，最多两位小数'
  if (parseAmount(draft.face) === null) return '额度面值须为大于 0 的人民币，最多两位小数'
  return ''
}
