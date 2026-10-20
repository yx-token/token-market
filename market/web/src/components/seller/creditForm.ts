import { parseAmount } from '@/utils/format'

export interface CreditDraft {
  title: string
  description: string
  price: string
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
  if (parseAmount(draft.face) === null) return '美元额度须大于 0，最多两位小数'
  return ''
}

/** 可抵平台余额单位 = 美元额度 × 购买时汇率。不在调用时再乘店铺倍率。 */
export function coverageUnits(faceUsd: number, rate: number): number | null {
  if (!(faceUsd > 0) || !(rate > 0)) return null
  return faceUsd * rate
}
