import { http } from './client'

export interface CreditOffer {
  id: number
  seller_id: number
  title: string
  description: string
  price_cny: number
  face_value_usd: number
  allowed_platforms: string[]
  allowed_models: string[]
  status: string
  created_at: string
  updated_at: string
}

export interface CreditPack {
  id: number
  offer_id: number
  buyer_id: number
  seller_id: number
  title: string
  price_cny: number
  face_value_usd: number
  remaining_usd: number
  remaining_balance_units: number
  prepaid_remaining_cny: number
  usd_to_balance_rate: number
  allowed_platforms: string[]
  allowed_models: string[]
  status: 'provisioning' | 'active' | 'depleted' | 'refunding' | 'refunded'
  created_at: string
}

export interface CreditMerchant {
  seller_id: number
  listings: { title?: string; platform: string }[]
}

export const storeCreditsBuyer = {
  offers: (sellerId: number) => http.get<CreditOffer[] | null>('/api/market/credit-offers', { seller_id: sellerId }),
  packs: () => http.get<CreditPack[] | null>('/api/buyer/credit-packs'),
  merchants: () => http.get<CreditMerchant[] | null>('/api/buyer/merchants'),
  purchase: (offerId: number, idempotencyKey: string) =>
    http.post<CreditPack>('/api/buyer/credit-packs', { offer_id: offerId, idempotency_key: idempotencyKey }),
  refund: (packId: number) => http.post<CreditPack>(`/api/buyer/credit-packs/${packId}/refund`),
}
