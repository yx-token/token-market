// 与 market-api 的 JSON 字段一一对应（见 internal/*/ 中带 json tag 的结构体）。

export interface User {
  id: number
  email: string
  is_admin: boolean
  status: string
  created_at: string
  last_login_at?: string
  kyc_status: string
}

export interface LoginResult {
  token: string
  expires_at: string
  user: User
}

export interface MarketConfig {
  require_seller_proxy: boolean
  edge_url: string
  commission_rate: number
  freeze_days: number
  withdraw_min: number
  max_hops: number
  payment_channels: string[]
  recharge_min: number
  recharge_max: number
}

export interface MarketItem {
  listing_id: number
  platform: string
  auth_type: string
  tier?: string
  grade?: string
  health: string
  price: number
  success_rate_24h: number | null
  requests_24h: number
  ttft_p50_ms: number | null
  listed_since: string
}

export interface PoolItem {
  name: string
  platform: string
  price: number
}

export interface Price {
  model: string
  input_per_1m: number
  output_per_1m: number
  cache_write_per_1m: number
  cache_read_per_1m: number
}

export interface VendorView {
  platform: string
  auth_types: string[]
  consignment_min: number
  consignment_max: number
  self_reserve_supported: boolean
}

export interface PaymentInfo {
  channel: string
  kind: 'redirect' | 'qrcode' | 'manual'
  url?: string
  qr_code?: string
  instructions?: string
}

export interface RechargeOrder {
  id: number
  user_id: number
  amount: number
  provider: string
  channel: string
  out_trade_no?: string
  provider_ref?: string
  status: string
  close_reason?: string
  refunded_amount: number
  created_at: string
  expires_at?: string
  paid_at?: string
  credited_at?: string
  payment?: PaymentInfo
}

export interface RefundPayout {
  id: number
  refund_id: number
  recharge_order_id?: number
  channel: string
  amount: number
  out_refund_no?: string
  status: string
  provider_ref?: string
  error?: string
  attempts: number
  created_at: string
  finished_at?: string
}

export interface Refund {
  id: number
  user_id: number
  status: string
  amount?: number
  payout_ref?: string
  note?: string
  created_at: string
  finished_at?: string
  payouts: RefundPayout[] | null
}

export interface PaymentChannelStatus {
  channel: string
  provider: string
  configured: boolean
  enabled: boolean
  available: boolean
}

export interface PaymentNotification {
  id: number
  provider: string
  out_trade_no?: string
  trade_no?: string
  state?: string
  amount_fen?: number
  result: string
  error?: string
  created_at: string
}

export interface AlipayMerchant {
  app_id: string
  gateway: string
  mode: 'public_key' | 'cert'
  alipay_public_key: string
  app_cert: string
  alipay_cert: string
  alipay_root_cert: string
}

export interface WechatMerchant {
  app_id: string
  mch_id: string
  mch_serial_no: string
  public_key_id: string
  public_key: string
  base_url: string
}

export interface MerchantView {
  provider: string
  enabled: boolean
  alipay?: AlipayMerchant
  wechat?: WechatMerchant
  has_private_key: boolean
  has_api_v3_key?: boolean
  ready: boolean
  problem?: string
  updated_at?: string
}

export interface PaymentStatus {
  channels: PaymentChannelStatus[]
  public_url: string
  notifications: PaymentNotification[]
}

export interface Wallet {
  balance: number | null
  ledger_cash: number
  recent_recharges: RechargeOrder[] | null
  active_refund?: Refund
}

export interface HopInput {
  listing_id?: number
  pool?: string
}

export interface RouteInput {
  type: 'default' | 'chain'
  pool?: string
  hops?: HopInput[]
}

export interface Limits {
  quota_usd?: number
  rate_limit_5h_usd?: number
  rate_limit_1d_usd?: number
  rate_limit_7d_usd?: number
  ip_whitelist?: string[]
  expires_at?: string
}

export interface HopView {
  position: number
  listing_id?: number
  pool?: string
  state: string
}

export interface BuyerKey {
  id: number
  name: string
  key_prefix: string
  route_type: string
  status: string
  disabled_reason?: string
  limits: Limits
  limit_blocked_until?: string
  limit_block_reason?: string
  hops: HopView[] | null
  created_at: string
}

export interface CreatedKey {
  key: string
  note: string
  detail: BuyerKey
}

export interface UsageItem {
  usage_id: number
  request_id?: string
  created_at: string
  buyer_key_id?: number
  listing_id?: number
  channel: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  cost: number
  first_token_ms?: number
  duration_ms?: number
}

export interface AttemptView {
  hop: number
  listing_id?: number
  pool_id?: number
  http_status?: number
  end_reason: string
  committed: boolean
  sub2api_request_id?: string
  charged: boolean
  cost: number
  note: string
  started_at: string
}

export interface BillDay {
  day: string
  requests: number
  cost: number
}

export interface Upstream {
  id: number
  proxy_id?: number
  seller_id: number
  platform: string
  auth_type: string
  is_reference: boolean
  status: string
  health: string
  health_reason?: string
  health_changed_at?: string
  tier?: string
  privacy_ok?: boolean
  self_reserve_percent: number
  grade?: string
  failure_reason?: string
  created_at: string
  updated_at: string
}

export interface Listing {
  id: number
  upstream_id: number
  seller_id: number
  mode: string
  status: string
  paused_by?: string
  multiplier?: number
  join_default_pool: boolean
  price_changed_at?: string
  created_at: string
  updated_at: string
  closed_at?: string
}

export interface Quote {
  id: number
  upstream_id: number
  listing_id?: number
  grade: string
  multiplier: number
  status: string
  valid_until: string
  created_at: string
  decided_at?: string
}

export interface UpstreamStats {
  requests: number
  probe_requests: number
  payout: number
  ttft_p50_ms: number | null
  success_rate: number | null
}

export interface UpstreamDetail extends Upstream {
  proxy?: Proxy
  listing?: Listing
  quote?: Quote
  stats_24h: UpstreamStats
}

export interface AuthorizationStart {
  auth_url: string
  expires_at: string
}

export interface AuthorizationOptions {
  /** Gemini Code Assist 的 GCP 项目 ID，留空时自动探测 */
  project_id?: string
}

export interface CodingKeyInput {
  api_key: string
  /** 智谱国内站团队版的组织 ID 与项目 ID */
  organization?: string
  project?: string
}

export interface PriceVersion {
  multiplier: number
  buyer_price?: number
  effective_from: string
  source: string
}

export interface Report {
  id: number
  upstream_id: number
  trigger: string
  verdict: string
  summary: ReportSummary
  impact: Record<string, unknown>
  created_at: string
}

export interface ReportSummary {
  verdict: string
  trigger: string
  model: string
  checked_at: string
  baseline: boolean
  attribution?: string
  grade?: string
  tier?: string
  suggestions?: string[]
  internal_verdict?: string
  third_party?: ThirdPartyResult[]
  suites: {
    availability?: { success: number; total: number }
    latency?: { ttft_p50_ms?: number; ttft_p95_ms?: number; output_tps?: number }
    iq?: { relative_score: number; samples: number; confidence?: number }
    reasoning?: { ratio_to_baseline: number }
  }
}

export interface ThirdPartySummary {
  run_status?: string
  identity_status?: string
  verdict_status?: string
  true_family?: string
  true_model?: string
  spoof_method?: string
  confidence?: string | number
  reasoning?: string
  risk_flags?: string[]
  warnings?: { id: string; label: string; severity: string }[]
  items?: Record<string, number>
  failed_probes?: { label: string; group: string; reason: string }[]
  tokens?: { input?: number | null; output?: number | null }
  wall_clock_ms?: number
}

export interface ThirdPartyResult {
  provider: string
  status: string
  outcome?: string
  verdict: string
  model: string
  report_url?: string
  error?: string
  summary?: ThirdPartySummary
}

export interface TrendPoint {
  day: string
  relative_score?: number
  checks: number
  degraded: number
  unavailable: number
}

export interface Appeal {
  id: number
  report_id: number
  seller_id: number
  reason: string
  status: string
  admin_note?: string
  created_at: string
  resolved_at?: string
}

export interface Earnings {
  pending: number
  available: number
  withdrawing: number
  today: number
  last_7_days: number
  total: number
}

export interface SettlementLine {
  upstream_id: number
  hour: string
  channel: string
  requests: number
  official_cost: number
  payout: number
  unfrozen: number
}

export interface Withdrawal {
  id: number
  seller_id: number
  amount: number
  channel: string
  account_info?: string
  status: string
  review_reason?: string
  payout_ref?: string
  created_at: string
  reviewed_at?: string
  paid_at?: string
}

// ---- 管理后台 ----

export interface Settings {
  require_seller_proxy: boolean
  commission_rate: number
  freeze_days: number
  withdraw_min: number
  withdraw_auto_approve_max: number
  manual_retest_daily_limit: number
  quote_validity_days: number
  grade_coefficients: Record<string, number>
  price_change_cooldown_minutes: number
  shadow_concurrency: number
  refund_lock_seconds: number
  new_seller_days: number
  new_seller_direct_cap: number
  scheduler_change_budget: number
  balance_mismatch_tolerance: number
  allow_no_proxy: boolean
  price_models: string[]
  payment_public_url: string
  payment_channels: Record<string, boolean>
  recharge_min: number
  recharge_max: number
  recharge_order_timeout_minutes: number
  refund_original_route: boolean
  refund_original_route_days: number
}

export interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types: string[]
  consignment_min: number
  consignment_max: number
  buyback_base: Record<string, number>
  default_concurrency: number
  extra_limits: Record<string, unknown>
  updated_at: string
}

export interface Proxy {
  id: number
  seller_id?: number
  name: string
  protocol: string
  host: string
  port: number
  username?: string
  region?: string
  sub2api_proxy_id?: number
  status: string
  assigned_upstream_id?: number
  created_at: string
}

export interface CreateProxyInput {
  name: string
  protocol: string
  host: string
  port: number
  username?: string
  password?: string
  region?: string
}

export interface Pool {
  id: number
  name: string
  platform: string
  sub2api_group_id?: number
  sell_multiplier: number
  min_margin: number
  safety_buffer: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface Thresholds {
  h0: number
  h1: number
  alpha: number
  beta: number
}

export interface Policy {
  id: number
  platform: string
  auth_type: string
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  jitter_minutes: number
  suites: string[]
  models: string[]
  daily_budget_usd: number
  items_per_run: number
  thresholds: Thresholds
  auto_pause: boolean
  default_grade: string
  third_party: ThirdPartyCheck[] | null
  version: number
  updated_at: string
}

export interface ThirdPartyCheck {
  provider: string
  triggers: string[]
  model?: string
  format?: string
  on_mismatch: string
  on_submodel_mismatch: string
}

export interface DetectionProvider {
  name: string
  label: string
  description: string
  docs_url: string
  default_base_url: string
  options: Record<string, string>
  runs_per_hour_limit: number
  requests_per_run: number
  enabled: boolean
  base_url: string
  has_api_key: boolean
  settings: Record<string, unknown>
  max_runs_per_hour: number
  timeout_minutes: number
  runs_last_hour: number
  updated_at?: string
}

export interface ThirdPartyCheckView {
  id: number
  run_id: number
  upstream_id: number
  provider: string
  model: string
  format: string
  status: string
  external_id?: string
  outcome?: string
  verdict?: string
  summary?: ThirdPartySummary
  report_url?: string
  error?: string
  created_at: string
  submitted_at?: string
  finished_at?: string
}

export interface Grader {
  type: string
  answer?: string
  number?: number
  tolerance?: number
  pattern?: string
  keys?: string[]
  case_insensitive?: boolean
}

export interface ProbeItem {
  id: number
  suite: string
  category: string
  prompt: string
  grader: Grader
  platforms: string[]
  max_tokens: number
  reasoning_effort?: string
  status: string
  version: number
}

export interface QualityOverview {
  platforms: { platform: string; health: Record<string, number>; grades: Record<string, number> }[] | null
  recent_vendor_wide_events: unknown[] | null
  degraded_upstreams: number[] | null
}

export interface KYCItem {
  user_id: number
  email: string
  real_name: string
  id_number_masked: string
  status: string
  submitted_at: string
}

export interface Anomaly {
  id: number
  rule: string
  sub2api_usage_id?: number
  edge_attempt_id?: number
  buyer_user_id?: number
  amount?: number
  detail?: Record<string, unknown>
  status: string
  note?: string
  created_at: string
}

export interface RiskEvent {
  id: number
  kind: string
  severity: string
  user_id?: number
  related_user_id?: number
  upstream_id?: number
  detail?: Record<string, unknown>
  status: string
  created_at: string
}

export interface PlatformLedger {
  cash: number
  revenue: number
  probe_cost: number
  adjustment: number
  buyer_balances: number
  seller_payable: number
}

export interface Sub2APIStatus {
  sub2api_version: string | null
  migration_fingerprint: string | null
  guard_state: string
  reason: string | null
  checked_at: string | null
  ops_by_status: Record<string, number>
  ingest_cursor: number
}

export interface AuditLog {
  id: number
  actor_type: string
  actor_id?: number
  action: string
  target?: string
  detail: unknown
  created_at: string
}

export interface UsageTotals {
  requests: number
  buyer_requests: number
  probe_requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_cost: number
  payout: number
}

export interface SellerUsage {
  from: string
  days: number
  totals: UsageTotals
  today: UsageTotals
  daily: ({ day: string } & UsageTotals)[]
  models: ({ model: string } & UsageTotals)[]
  upstreams: ({ upstream_id: number; platform: string } & UsageTotals)[]
}

export interface SellerUsageRecord {
  id: number
  created_at: string
  upstream_id: number
  listing_id?: number
  channel: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  first_token_ms?: number
  duration_ms?: number
  total_cost: number
  payout: number
}

export interface QuotaWindow {
  key: string
  label: string
  utilization: number
  resets_at?: string
  requests?: number
  tokens?: number
  cost?: number
  used_requests?: number
  limit_requests?: number
}

export interface AccountQuota {
  supported: boolean
  source?: string
  sampled_at?: string
  fetched_at: string
  plan_level?: string
  windows: QuotaWindow[]
  alerts?: string[]
  error?: string
  self_reserve_percent?: number
}
export interface UpdateJob {
  id: string
  version: string
  phase: 'queued' | 'downloading' | 'checking' | 'restarting' | 'succeeded' | 'failed' | 'rolling_back' | 'rolled_back' | 'rollback_failed'
  message: string
  started_at: string
  updated_at: string
}

export interface SystemVersion {
  version: string
  commit: string
  platform: string
  repository: string
  enabled: boolean
  reason?: string
  job?: UpdateJob
}

export interface SystemUpdateCheck {
  update_available: boolean
  release: {
    version: string
    name: string
    notes: string
    url: string
    published_at: string
    compatible: boolean
  }
}
