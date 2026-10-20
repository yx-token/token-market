import { http } from './client'
import type { CreditOffer } from './storeCreditsBuyer'

export type { CreditOffer }
export type CreditOfferStatus = 'active' | 'paused'

export interface CreditOfferInput {
  title: string
  description?: string
  price_cny: number
  face_value_usd: number
  allowed_platforms?: string[]
  allowed_models?: string[]
  status?: CreditOfferStatus
}

/** 卖家店铺活动额度。买家购买接口在 storeCreditsBuyer.ts，不要写到这里。 */
export const sellerStoreCredits = {
  list: () => http.get<CreditOffer[] | null>('/api/seller/credit-offers'),
  create: (body: CreditOfferInput) => http.post<CreditOffer>('/api/seller/credit-offers', body),
  update: (id: number, body: CreditOfferInput) => http.put<CreditOffer>(`/api/seller/credit-offers/${id}`, body),
}
