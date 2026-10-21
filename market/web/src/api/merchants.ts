import { http } from './client'
import type { MarketItem } from './types'

export interface BuyerMerchant {
  seller_id: number
  added_at: string
  listings: MarketItem[]
}

export const merchants = {
  list: () => http.get<BuyerMerchant[] | null>('/api/buyer/merchants'),
  listings: () => http.get<MarketItem[] | null>('/api/buyer/merchants/listings'),
  add: (sellerID: number) => http.put<unknown>(`/api/buyer/merchants/${sellerID}`),
  remove: (sellerID: number) => http.del<unknown>(`/api/buyer/merchants/${sellerID}`),
}
