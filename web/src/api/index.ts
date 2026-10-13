// 控制面接口（路径与 docs/10-api.md、internal/httpapi/server.go 一致）。
import { http, type Query } from './client'
import type * as T from './types'

type Ok = { status: string }

export const auth = {
  register: (email: string, password: string) => http.post<T.User>('/api/auth/register', { email, password }),
  login: (email: string, password: string) => http.post<T.LoginResult>('/api/auth/login', { email, password }),
  refresh: () => http.post<T.LoginResult>('/api/auth/refresh'),
  me: () => http.get<T.User>('/api/me'),
  consoleAccess: () => http.get<T.ConsoleAccess>('/api/me/console-access'),
  submitKYC: (real_name: string, id_number: string) => http.post<Ok>('/api/me/kyc', { real_name, id_number }),
}

export const market = {
  config: () => http.get<T.MarketConfig>('/api/market/config'),
  discovery: () => http.get<Record<string, unknown>>('/api/market/discovery'),
  recommend: () => http.get<T.RecommendResult>('/api/market/recommend'),
  categories: () => http.get<T.MarketCategory[] | null>('/api/market/categories'),
  listings: (q?: Query) => http.get<T.MarketItem[] | null>('/api/market/listings', q),
  listing: (id: number) => http.get<T.MarketItem>(`/api/market/listings/${id}`),
  pools: () => http.get<T.PoolItem[] | null>('/api/market/pools'),
  vendors: () => http.get<T.VendorView[] | null>('/api/market/vendors'),
  prices: () => http.get<T.Price[] | null>('/api/market/prices'),
}

export const buyer = {
  wallet: () => http.get<T.Wallet>('/api/buyer/wallet'),
  recharge: (amount: number, channel: string) =>
    http.post<{ order: T.RechargeOrder; payment?: T.PaymentInfo }>('/api/buyer/recharge', { amount, channel }),
  getRecharge: (id: number) => http.get<T.RechargeOrder>(`/api/buyer/recharge/${id}`),
  cancelRecharge: (id: number) => http.post<Ok>(`/api/buyer/recharge/${id}/cancel`),
  refundRecharge: (id: number, amount: number, note: string) => http.post<T.Refund>(`/api/buyer/recharge/${id}/refund`, { amount, note }),
  requestRefund: (note: string) => http.post<T.Refund>('/api/buyer/refunds', { note }),
  refunds: () => http.get<T.Refund[] | null>('/api/buyer/refunds'),
  purchasePack: (listing_id: number, channel = 'balance', name = '') =>
    http.post<{
      pack?: T.QuotaPack
      key?: string
      note?: string
      recharge?: T.RechargeOrder
    }>('/api/buyer/packs', { listing_id, channel, name }),
  packs: () => http.get<T.QuotaPack[] | null>('/api/buyer/packs'),
  refundPack: (id: number, dest = 'balance') => http.post<T.QuotaPack>(`/api/buyer/packs/${id}/refund`, { dest }),
  keys: () => http.get<T.BuyerKey[] | null>('/api/buyer/keys'),
  key: (id: number) => http.get<T.BuyerKey>(`/api/buyer/keys/${id}`),
  keySecret: (id: number) => http.get<{ key: string }>(`/api/buyer/keys/${id}/secret`),
  createKey: (body: { name: string; route: T.RouteInput; limits: T.Limits }) => http.post<T.CreatedKey>('/api/buyer/keys', body),
  updateKey: (id: number, body: { name?: string; route?: T.RouteInput; limits?: T.Limits }) =>
    http.put<T.BuyerKey>(`/api/buyer/keys/${id}`, body),
  setKeyEnabled: (id: number, enabled: boolean) => http.post<T.BuyerKey>(`/api/buyer/keys/${id}/${enabled ? 'enable' : 'disable'}`),
  deleteKey: (id: number) => http.del<Ok>(`/api/buyer/keys/${id}`),
  usage: (q?: Query) => http.get<T.UsageItem[] | null>('/api/buyer/usage', q),
  requestDetail: (rid: string) => http.get<T.AttemptView[] | null>(`/api/buyer/usage/${encodeURIComponent(rid)}`),
  bills: (days = 30) => http.get<T.BillDay[] | null>('/api/buyer/bills', { days }),
}

export const seller = {
  upstreamModels: (id: number) => http.get<T.UpstreamModels>(`/api/seller/upstreams/${id}/models`),
  refreshUpstreamModels: (id: number) => http.post<T.UpstreamModels>(`/api/seller/upstreams/${id}/models/refresh`, {}, 30000),
  setUpstreamModels: (id: number, models: string[]) => http.put<T.Upstream>(`/api/seller/upstreams/${id}/models`, { models }),
  proxies: () => http.get<T.Proxy[] | null>('/api/seller/proxies'),
  createProxy: (body: T.CreateProxyInput) => http.post<T.Proxy>('/api/seller/proxies', body),
  updateProxy: (id: number, body: T.CreateProxyInput) => http.put<T.Proxy>(`/api/seller/proxies/${id}`, body),
  deleteProxy: (id: number) => http.del<Ok>(`/api/seller/proxies/${id}`),
  syncProxy: (id: number) => http.post<T.Proxy>(`/api/seller/proxies/${id}/sync`),
  testProxy: (id: number) => http.post<T.Proxy>(`/api/seller/proxies/${id}/test`),
  checkProxyQuality: (id: number) => http.post<T.Proxy>(`/api/seller/proxies/${id}/quality-check`, {}, 190000),
  setProxyDisabled: (id: number, disabled: boolean) => http.post<T.Proxy>(`/api/seller/proxies/${id}/${disabled ? 'disable' : 'enable'}`),
  vendors: () => http.get<T.VendorView[] | null>('/api/seller/vendors'),
  upstreams: () => http.get<T.Upstream[] | null>('/api/seller/upstreams'),
  upstream: (id: number) => http.get<T.UpstreamDetail>(`/api/seller/upstreams/${id}`),
  createUpstream: (platform: string, auth_type: string, proxy_id?: number, proxy_from_import?: boolean) =>
    http.post<T.Upstream>('/api/seller/upstreams', { platform, auth_type, proxy_id, ...(proxy_from_import ? { proxy_from_import } : {}) }),
  importAccount: (id: number, data: unknown, account_index: number) => http.post<T.Upstream>(`/api/seller/upstreams/${id}/import`, { data, account_index }),
  startOAuth: (id: number, opts?: T.AuthorizationOptions) => http.post<T.AuthorizationStart>(`/api/seller/upstreams/${id}/oauth/start`, opts),
  completeOAuth: (id: number, code: string) => http.post<T.Upstream>(`/api/seller/upstreams/${id}/oauth/complete`, { code }),
  reauthorize: (id: number, opts?: T.AuthorizationOptions) => http.post<T.AuthorizationStart>(`/api/seller/upstreams/${id}/reauthorize`, opts),
  submitCodingKey: (id: number, body: T.CodingKeyInput) => http.post<T.Upstream>(`/api/seller/upstreams/${id}/coding-key`, body),
  retryVerification: (id: number) => http.post<T.Upstream>(`/api/seller/upstreams/${id}/retry-verification`),
  setSelfReserve: (id: number, self_reserve_percent: number) =>
    http.put<T.Upstream>(`/api/seller/upstreams/${id}/settings`, { self_reserve_percent }),
  removeUpstream: (id: number) => http.del<Ok>(`/api/seller/upstreams/${id}`),
  listingCategories: () => http.get<T.ListingCategoryOption[] | null>('/api/seller/listing-categories'),
  createListing: (id: number, body: {
    mode: string
    multiplier?: number
    join_default_pool?: boolean
    face_value?: number
    stock?: number
    min_amount?: number
    title?: string
    description?: string
    categories?: string[]
  }) => http.post<{ listing: T.Listing; quote?: T.Quote }>(`/api/seller/upstreams/${id}/listing`, body),
  quote: (id: number) => http.get<T.Quote | null>(`/api/seller/upstreams/${id}/quote`),
  reports: (id: number) => http.get<T.Report[] | null>(`/api/seller/upstreams/${id}/reports`),
  report: (id: number) => http.get<T.Report>(`/api/seller/reports/${id}`),
  trend: (id: number, days = 30) => http.get<T.TrendPoint[] | null>(`/api/seller/upstreams/${id}/quality-trend`, { days }),
  retest: (id: number) => http.post<{ run_id: number; note: string }>(`/api/seller/upstreams/${id}/retest`),
  appeal: (reportId: number, reason: string) => http.post<T.Appeal>(`/api/seller/reports/${reportId}/appeal`, { reason }),
  listings: () => http.get<T.Listing[] | null>('/api/seller/listings'),
  listing: (id: number) => http.get<{ listing: T.Listing; price_history: T.PriceVersion[] | null }>(`/api/seller/listings/${id}`),
  acceptQuote: (id: number) => http.post<T.Listing>(`/api/seller/listings/${id}/accept-quote`),
  rejectQuote: (id: number) => http.post<Ok>(`/api/seller/listings/${id}/reject-quote`),
  changePrice: (id: number, multiplier: number) => http.put<T.Listing>(`/api/seller/listings/${id}/price`, { multiplier }),
  updateListingMeta: (id: number, body: { title?: string; description?: string; categories: string[] }) =>
    http.put<T.Listing>(`/api/seller/listings/${id}/meta`, body),
  setDefaultPool: (id: number, join: boolean) => http.put<T.Listing>(`/api/seller/listings/${id}/default-pool`, { join }),
  pause: (id: number) => http.post<T.Listing>(`/api/seller/listings/${id}/pause`),
  resume: (id: number) => http.post<T.Listing>(`/api/seller/listings/${id}/resume`),
  close: (id: number) => http.post<Ok>(`/api/seller/listings/${id}/close`),
  earnings: () => http.get<T.Earnings>('/api/seller/earnings'),
  lines: (q?: Query) => http.get<T.SettlementLine[] | null>('/api/seller/earnings/lines', q),
  withdraw: (amount: number, channel: string, account_info: string) =>
    http.post<T.Withdrawal>('/api/seller/withdrawals', { amount, channel, account_info }),
  withdrawals: () => http.get<T.Withdrawal[] | null>('/api/seller/withdrawals'),
  usage: (q?: Query) => http.get<T.SellerUsage>('/api/seller/usage', q),
  usageRecords: (q?: Query) => http.get<T.SellerUsageRecord[] | null>('/api/seller/usage/records', q),
  quota: (id: number, refresh = false) => http.get<T.AccountQuota>(`/api/seller/upstreams/${id}/quota`, refresh ? { refresh: 1 } : undefined),
  resetCredits: (id: number, refresh = false) =>
    http.get<T.ResetCredits>(`/api/seller/upstreams/${id}/reset-credits`, refresh ? { refresh: 1 } : undefined, 30000),
  // 用卡包含一次官方请求与底层的后续处理，最长约 100 秒
  redeemResetCredit: (id: number, request_id: string, expected_available: number) =>
    http.post<T.ResetCreditEvent>(`/api/seller/upstreams/${id}/reset-credits/redeem`, { request_id, expected_available }, 120000),
  setAutoReset: (id: number, body: T.AutoResetInput) => http.put<T.Upstream>(`/api/seller/upstreams/${id}/auto-reset`, body),
}

export const admin = {
  systemVersion: () => http.get<T.SystemVersion>('/api/admin/system/version', undefined, 10000),
  checkSystemUpdate: (force = false) => http.get<T.SystemUpdateCheck>('/api/admin/system/check-update', { force }, 20000),
  startSystemUpdate: (version: string) => http.post<T.UpdateJob>('/api/admin/system/update', { version }, 20000),
  settings: () => http.get<T.Settings>('/api/admin/settings'),
  updateSettings: (patch: Partial<T.Settings>) => http.put<T.Settings>('/api/admin/settings', patch),
  inspectionSettings: () => http.get<T.InspectionSettings>('/api/admin/inspection/settings'),
  updateInspectionSettings: (body: T.InspectionSettings) => http.put<T.InspectionSettings>('/api/admin/inspection/settings', body),
  inspectionChecks: () => http.get<T.InspectionCheckDefinition[]>('/api/admin/inspection/checks'),
  updateInspectionCheck: (key: string, body: T.InspectionCheckOverride) =>
    http.put<T.InspectionCheckDefinition>(`/api/admin/inspection/checks/${encodeURIComponent(key)}`, body),
  marketDiscovery: () => http.get<Record<string, unknown>>('/api/admin/market-discovery'),
  updateMarketDiscovery: (body: Record<string, unknown>) =>
    http.put<Record<string, unknown>>('/api/admin/market-discovery', body),
  marketRecommend: () => http.get<T.RecommendConfig>('/api/admin/market-recommend'),
  updateMarketRecommend: (body: T.RecommendConfig) => http.put<T.RecommendConfig>('/api/admin/market-recommend', body),
  marketCategories: () => http.get<T.MarketCategory[] | null>('/api/admin/market-categories'),
  createMarketCategory: (body: Record<string, unknown>) => http.post<T.MarketCategory>('/api/admin/market-categories', body),
  updateMarketCategory: (id: string, body: Record<string, unknown>) =>
    http.put<T.MarketCategory>(`/api/admin/market-categories/${id}`, body),
  deleteMarketCategory: (id: string) => http.del<Ok>(`/api/admin/market-categories/${id}`),
  vendors: () => http.get<T.VendorConfig[] | null>('/api/admin/vendors'),
  updateVendor: (platform: string, body: Partial<T.VendorConfig>) => http.put<T.VendorConfig>(`/api/admin/vendors/${platform}`, body),
  proxies: () => http.get<T.Proxy[] | null>('/api/admin/proxies'),
  createProxy: (body: T.CreateProxyInput) => http.post<T.Proxy>('/api/admin/proxies', body),
  syncProxy: (id: number) => http.post<T.Proxy>(`/api/admin/proxies/${id}/sync`),
  testProxy: (id: number) => http.post<T.Proxy>(`/api/admin/proxies/${id}/test`),
  checkProxyQuality: (id: number) => http.post<T.Proxy>(`/api/admin/proxies/${id}/quality-check`, {}, 190000),
  setProxyDisabled: (id: number, disabled: boolean) => http.post<T.Proxy>(`/api/admin/proxies/${id}/${disabled ? 'disable' : 'enable'}`),
  pools: () => http.get<T.Pool[] | null>('/api/admin/pools'),
  createPool: (body: Record<string, unknown>) => http.post<T.Pool>('/api/admin/pools', body),
  updatePool: (id: number, body: Record<string, unknown>) => http.put<T.Pool>(`/api/admin/pools/${id}`, body),
  syncPool: (id: number) => http.post<T.Pool>(`/api/admin/pools/${id}/sync`),
  policies: () => http.get<T.Policy[] | null>('/api/admin/detection-policies'),
  upsertPolicy: (platform: string, authType: string, body: Record<string, unknown>) =>
    http.put<T.Policy>(`/api/admin/detection-policies/${platform}${authType ? `?auth_type=${encodeURIComponent(authType)}` : ''}`, body),
  providers: () => http.get<T.DetectionProvider[] | null>('/api/admin/detection-providers'),
  updateProvider: (name: string, body: Record<string, unknown>) => http.put<T.DetectionProvider>(`/api/admin/detection-providers/${name}`, body),
  thirdPartyChecks: (q?: Query) => http.get<T.ThirdPartyCheckView[] | null>('/api/admin/third-party-checks', q),
  setOverride: (upstreamId: number, body: Record<string, unknown>) => http.put<Ok>(`/api/admin/upstreams/${upstreamId}/detection-override`, body),
  deleteOverride: (upstreamId: number) => http.del<Ok>(`/api/admin/upstreams/${upstreamId}/detection-override`),
  probe: (upstreamId: number) => http.post<{ run_id: number }>(`/api/admin/upstreams/${upstreamId}/probe`),
  probeFlows: (upstreamId: number, flows: T.DetectionFlow[]) =>
    http.post<{ run_id: number }>(`/api/admin/upstreams/${upstreamId}/probe`, { flows }),
  probeRuns: (upstreamId: number) => http.get<T.ProbeRun[] | null>(`/api/admin/upstreams/${upstreamId}/probe-runs`),
  probeRun: (runId: number) => http.get<T.ProbeRunDetail>(`/api/admin/probe-runs/${runId}`),
  probeRecords: (q?: Query) => http.get<T.ProbeRunRecord[] | null>('/api/admin/probe-runs', q),
  probeItemStats: (q?: Query) => http.get<T.ProbeItemStat[] | null>('/api/admin/probe-item-stats', q),
  probeItemResults: (itemId: number, q?: Query) => http.get<T.ProbeItemResult[] | null>(`/api/admin/probe-items/${itemId}/results`, q),
  items: (status?: string) => http.get<T.ProbeItem[] | null>('/api/admin/probe-items', { status }),
  probeGeneratorTemplates: () => http.get<T.GeneratorTemplate[]>('/api/admin/probe-generator-templates'),
  createItem: (body: Partial<T.ProbeItem>) => http.post<T.ProbeItem>('/api/admin/probe-items', body),
  updateItem: (id: number, body: Partial<T.ProbeItem>) => http.put<T.ProbeItem>(`/api/admin/probe-items/${id}`, body),
  retireItem: (id: number) => http.post<Ok>(`/api/admin/probe-items/${id}/retire`),
  references: () => http.get<T.Upstream[] | null>('/api/admin/reference-accounts'),
  createReference: (platform: string, auth_type: string) =>
    http.post<{ upstream: T.Upstream; next: string }>('/api/admin/reference-accounts', { platform, auth_type }),
  deleteReference: (id: number) => http.del<Ok>(`/api/admin/reference-accounts/${id}`),
  qualityOverview: () => http.get<T.QualityOverview>('/api/admin/quality/overview'),
  appeals: (status?: string) => http.get<T.Appeal[] | null>('/api/admin/appeals', { status }),
  resolveAppeal: (id: number, action: string, note: string) => http.post<T.Appeal>(`/api/admin/appeals/${id}/resolve`, { action, note }),
  upstreams: (q?: Query) => http.get<T.Upstream[] | null>('/api/admin/upstreams', q),
  suspendUpstream: (id: number, suspend: boolean, reason = '') =>
    http.post<T.Upstream>(`/api/admin/upstreams/${id}/${suspend ? 'suspend' : 'unsuspend'}`, suspend ? { reason } : undefined),
  removeUpstream: (id: number) => http.del<Ok>(`/api/admin/upstreams/${id}`),
  pendingKYC: () => http.get<T.KYCItem[] | null>('/api/admin/kyc'),
  reviewKYC: (userId: number, approve: boolean, note: string) => http.post<Ok>(`/api/admin/kyc/${userId}/review`, { approve, note }),
  setUserStatus: (userId: number, status: string) => http.post<Ok>(`/api/admin/users/${userId}/status`, { status }),
  recharges: (status?: string) => http.get<T.RechargeOrder[] | null>('/api/admin/recharges', { status }),
  confirmRecharge: (id: number, provider_ref: string) => http.post<T.RechargeOrder>(`/api/admin/recharges/${id}/confirm`, { provider_ref }),
  syncRecharge: (id: number) => http.post<T.RechargeOrder>(`/api/admin/recharges/${id}/sync`),
  refundRecharge: (id: number, amount: number, note: string) => http.post<T.Refund>(`/api/admin/recharges/${id}/refund`, { amount, note }),
  paymentStatus: () => http.get<T.PaymentStatus>('/api/admin/payments/status'),
  merchants: () => http.get<Record<string, T.MerchantView>>('/api/admin/payments/merchants'),
  updateMerchant: (provider: string, body: Record<string, unknown>) => http.put<T.MerchantView>(`/api/admin/payments/merchants/${provider}`, body),
  testMerchant: (provider: string) => http.post<{ status: string }>(`/api/admin/payments/merchants/${provider}/test`),
  retryPayout: (id: number) => http.post<T.RefundPayout>(`/api/admin/refund-payouts/${id}/retry`),
  payoutToManual: (id: number) => http.post<T.RefundPayout>(`/api/admin/refund-payouts/${id}/manual`),
  refunds: (status?: string) => http.get<T.Refund[] | null>('/api/admin/refunds', { status }),
  refundPaid: (id: number, payout_ref: string) => http.post<T.Refund>(`/api/admin/refunds/${id}/paid`, { payout_ref }),
  withdrawals: (status?: string) => http.get<T.Withdrawal[] | null>('/api/admin/withdrawals', { status }),
  reviewWithdrawal: (id: number, approve: boolean, reason: string) =>
    http.post<T.Withdrawal>(`/api/admin/withdrawals/${id}/review`, { approve, reason }),
  withdrawalPaid: (id: number, body: { payout_ref?: string; failed?: boolean; reason?: string }) =>
    http.post<T.Withdrawal>(`/api/admin/withdrawals/${id}/paid`, body),
  anomalies: (status?: string) => http.get<T.Anomaly[] | null>('/api/admin/anomalies', { status }),
  resolveAnomaly: (id: number, status: string, note: string) => http.post<Ok>(`/api/admin/anomalies/${id}/resolve`, { status, note }),
  adjust: (body: { buyer_user_id: number; amount: number; reason: string; anomaly_id?: number }) =>
    http.post<Ok>('/api/admin/adjustments', body),
  riskEvents: (status?: string) => http.get<T.RiskEvent[] | null>('/api/admin/risk-events', { status }),
  resolveRisk: (id: number, status: string) => http.post<Ok>(`/api/admin/risk-events/${id}/resolve`, { status }),
  ledger: () => http.get<T.PlatformLedger>('/api/admin/ledger'),
  sub2apiStatus: () => http.get<T.Sub2APIStatus>('/api/admin/sub2api/status'),
  auditLogs: (q?: Query) => http.get<T.AuditLog[] | null>('/api/admin/audit-logs', q),
  orgUnits: () => http.get<T.OrgUnit[] | null>('/api/admin/org/units'),
  createOrgUnit: (body: Record<string, unknown>) => http.post<T.OrgUnit>('/api/admin/org/units', body),
  updateOrgUnit: (id: number, body: Record<string, unknown>) => http.put<T.OrgUnit>(`/api/admin/org/units/${id}`, body),
  deleteOrgUnit: (id: number) => http.del<Ok>(`/api/admin/org/units/${id}`),
  orgResources: () => http.get<T.IAMResource[] | null>('/api/admin/org/resources'),
  updateOrgResource: (id: string, body: Record<string, unknown>) =>
    http.put<T.IAMResource>(`/api/admin/org/resources/${encodeURIComponent(id)}`, body),
  orgRoles: () => http.get<T.IAMRole[] | null>('/api/admin/org/roles'),
  createOrgRole: (body: Record<string, unknown>) => http.post<T.IAMRole>('/api/admin/org/roles', body),
  updateOrgRole: (id: number, body: Record<string, unknown>) => http.put<T.IAMRole>(`/api/admin/org/roles/${id}`, body),
  deleteOrgRole: (id: number) => http.del<Ok>(`/api/admin/org/roles/${id}`),
  orgRoleResources: (id: number) => http.get<{ resource_ids: string[] }>(`/api/admin/org/roles/${id}/resources`),
  setOrgRoleResources: (id: number, resource_ids: string[]) =>
    http.put<Ok>(`/api/admin/org/roles/${id}/resources`, { resource_ids }),
  orgRoleMembers: (id: number) =>
    http.get<{ user_ids: number[]; members: T.IAMStaff[] }>(`/api/admin/org/roles/${id}/members`),
  setOrgRoleMembers: (id: number, user_ids: number[]) =>
    http.put<Ok>(`/api/admin/org/roles/${id}/members`, { user_ids }),
  orgStaff: (q?: string) => http.get<T.IAMStaff[] | null>('/api/admin/org/staff', q ? { q } : undefined),
  createOrgStaff: (body: {
    email: string
    password: string
    role_ids: number[]
    org_unit_ids: number[]
    primary_org_id?: number
  }) => http.post<T.IAMStaff>('/api/admin/org/staff', body),
  orgUsers: (q?: string) => http.get<T.IAMStaff[] | null>('/api/admin/org/users', q ? { q } : undefined),
  assignOrgStaff: (id: number, body: { role_ids: number[]; org_unit_ids: number[]; primary_org_id?: number }) =>
    http.put<T.IAMStaff>(`/api/admin/org/staff/${id}`, body),
}
