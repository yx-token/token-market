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
  /** 市场展示官方价格的模型，用于按平台挑调用示例里的模型名 */
  price_models: string[]
}

export interface MarketItem {
  models?: string[]
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
  mode?: string
  sku_kind?: string
  face_value?: number
  stock?: number
  stock_sold?: number
  min_amount?: number
  title?: string
  description?: string
  categories?: string[]
  inspection_mode?: 'trial' | 'formal'
  quality_factor?: number | null
  appraisal?: InspectionAppraisal
  quality_checks?: Record<string, InspectionCheckResult>
  last_checked_at?: string | null
}

export interface PoolItem {
  id: number
  name: string
  platform: string
  price: number
  market_published?: boolean
  market_title?: string
  market_description?: string
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
  /** balance 退全部余额 / order 按充值单 / pack 额度包原路退回 */
  kind: string
  recharge_order_id?: number
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
  /** 可按充值单退款的余额：实时余额减去未用完的额度包 */
  refundable?: number
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
  /** 首跳平台，用于给出对应协议的调用示例 */
  platform?: string
  /** 能否再次查看明文；加密保存明文之前创建的 Key 不能 */
  recoverable: boolean
  created_at: string
}

export interface CreatedKey {
  key: string
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
  model_whitelist?: string[] | null
  model_catalog?: string[]
  model_catalog_at?: string | null
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
  /** 官方账号邮箱，授权或导入时识别；Gemini 与 Coding Plan 没有 */
  account_email?: string
  privacy_ok?: boolean
  self_reserve_percent: number
  /** 已授权平台自动用卡（仅 Codex） */
  auto_reset_enabled: boolean
  /** 自动用卡阈值（百分比），未设定时跟随暂停线 */
  auto_reset_5h_percent?: number
  auto_reset_7d_percent?: number
  auto_reset_authorized_at?: string
  grade?: string
  inspection_mode?: 'trial' | 'formal'
  quality_factor?: number | null
  appraisal?: InspectionAppraisal
  quality_checks?: Record<string, InspectionCheckResult>
  last_checked_at?: string | null
  failure_reason?: string
  created_at: string
  updated_at: string
}

export interface UpstreamModels {
  catalog: string[]
  whitelist: string[] | null
  fetched_at: string | null
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
  sku_kind?: string
  face_value?: number
  stock?: number
  stock_sold?: number
  min_amount?: number
  title?: string
  description?: string
  categories?: string[]
  created_at: string
  updated_at: string
  closed_at?: string
}

export interface ListingCategoryOption {
  id: string
  name: string
  desc: string
  parent_id?: string
  path?: string
}

export interface RecommendFactor {
  id: string
  name: string
  desc: string
  group: string
  enabled: boolean
  default_weight: number
  reserved?: boolean
}

export interface RecommendProfile {
  id: string
  name: string
  desc: string
  enabled: boolean
  filter_kind: string
  filter_val: string
  action_tag?: string
  action_kind?: string
  action_plat?: string
  weights: Record<string, number>
  low_rate_max_multiplier: number
  per_tile_sku: number
  fresh_days: number
  diversity_per_plat: number
  min_sku_to_show_tile: number
}

export interface HomeTile {
  id: string
  title: string
  desc: string
  eng: string
  theme: string
  enabled: boolean
  sort: number
  boost: number
  profile_id: string
}

/** 三层推荐运营配置：因子 → 推荐类型 → 首页四格 */
export interface RecommendConfig {
  version: number
  factors: RecommendFactor[]
  profiles: RecommendProfile[]
  tiles: HomeTile[]
  max_tiles: number
  hide_empty_tiles: boolean
}

export interface RecommendTileItem {
  listing_id?: number
  key: string
  title: string
  plat: string
  price: string
  score: number
  pool?: boolean
  kind: string
  face_value?: number
  mult?: number
}

export interface RecommendTile {
  id: string
  title: string
  desc: string
  eng: string
  theme: string
  score: number
  profile_id?: string
  action: { tag?: string; kindTab?: string; plat?: string }
  items: RecommendTileItem[]
}

export interface RecommendResult {
  config_version: number
  generated_at: string
  tiles: RecommendTile[]
}

export interface MarketCategory {
  id: string
  parent_id?: string
  name: string
  description: string
  sort: number
  enabled: boolean
  seller_pickable: boolean
  icon?: string
  children?: MarketCategory[]
}

export interface QuotaPack {
  id: number
  listing_id: number
  buyer_id: number
  buyer_key_id?: number
  face_value: number
  remaining: number
  status: string
  paid_amount: number
  pay_channel?: string
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
  /** 能否使用重置卡（Codex、Claude OAuth） */
  reset_credits_supported: boolean
  /** 能否授权平台自动用卡（Codex） */
  auto_reset_supported: boolean
  /** 能否手动复测：检测正常时不允许，code 为拒绝原因的错误码 */
  manual_retest?: { allowed: boolean; code?: string }
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
  flows?: DetectionFlow[]
  identity_source?: IdentitySource | null
  models?: string[]
  intelligence?: IntelligenceSummary
  degradation?: IntelligenceSummary
  identity?: IdentitySummary
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
  /** 卖家报告里的核验结果（脱敏，无渠道与站点链接） */
  verification?: VerificationResult
  inspection_mode?: 'trial' | 'formal'
  mode?: 'legacy' | 'full' | 'quick'
  checks?: Record<string, InspectionCheckResult>
  quality_factor?: number | null
  group_factors?: Record<string, number | null>
  gates?: InspectionGate[]
  appraisal?: InspectionAppraisal
  proposed_grade?: string
  repaired_grade?: string
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
  probe_request_timeout_seconds: number
  probe_batch_timeout_minutes: number
}

export interface VendorConfig {
  platform: string
  enabled: boolean
  auth_types: string[]
  supported_auth_types: string[]
  consignment_min: number
  consignment_max: number
  buyback_base: Record<string, number>
  appraisal_base?: Record<string, number>
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
  connection_report?: ProxyTestResult
  quality_report?: ProxyQualityResult
  connection_checked_at?: string
  quality_checked_at?: string
}

export interface ProxyTestResult {
  success: boolean
  message: string
  latency_ms: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
  country_code?: string
}

export interface ProxyQualityResult {
  score: number
  grade: string
  summary: string
  exit_ip?: string
  country?: string
  country_code?: string
  base_latency_ms: number
  passed_count: number
  warn_count: number
  failed_count: number
  challenge_count: number
  checked_at: number
  items: { target: string; status: string; http_status?: number; latency_ms: number; message?: string; cf_ray?: string }[]
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
  market_published: boolean
  market_title: string
  market_description: string
  created_at: string
  updated_at: string
}

export type DetectionFlow = 'intelligence' | 'degradation' | 'identity'
export type InspectionFlow = DetectionFlow | 'common'
export type IdentitySource = 'third_party' | 'self'

export interface DetectionFlowPolicy {
  enabled: boolean
  scheduled_enabled: boolean
  interval_minutes: number
  triggers: string[]
  full_triggers: string[]
}

export interface IntelligenceModelPolicy {
  model: string
  expected_pass_rate: number | null
}

export interface DetectionFlows {
  intelligence: DetectionFlowPolicy & { models: IntelligenceModelPolicy[] }
  // 降智检测与测智同构，但模型列表独立配置
  degradation: DetectionFlowPolicy & { models: IntelligenceModelPolicy[] }
  identity: DetectionFlowPolicy & { source: IdentitySource }
}

export interface IntelligenceSummary {
  status: InspectionCheckResult['status']
  inherited_at?: string | null
  models: {
    model: string
    status: InspectionCheckResult['status']
    target_pass_rate?: number | null
    expected_pass_rate?: number | null
    samples?: number
  }[]
}

/** 卖家报告里的一项核验大类（渠道信息已脱敏） */
export interface VerificationCategory {
  key: string
  name: string
  total: number
  passed: number
  warning: number
  failed: number
  error: number
}

/** 卖家报告里的一条风险项 */
export interface VerificationRisk {
  category_name: string
  label: string
  severity: string
  reason: string
}

/** 卖家报告里的第三方核验结论；不含渠道名与站点链接 */
export interface VerificationResult {
  status: string
  categories?: VerificationCategory[]
  risks?: VerificationRisk[]
  verdict_text?: string
  true_model?: string
  true_family?: string
  checked_at?: string
}

export interface IdentitySummary {
  source: IdentitySource
  status: InspectionCheckResult['status']
  inherited_at?: string | null
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
  thresholds: Thresholds
  auto_pause: boolean
  default_grade: string
  third_party: ThirdPartyCheck[] | null
  item_parallelism?: number
  full_triggers?: string[]
  token_budgets?: Partial<Record<'full' | 'quick', InspectionTokenBudget>>
  version: number
  updated_at: string
  flows?: DetectionFlows
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
  [rule: string]: unknown
}

/** 管理员查看的一次检测记录 */
export interface ProbeRun {
  id: number
  upstream_id: number
  batch_id: string
  trigger: string
  model: string
  status: string
  summary?: Partial<ReportSummary> & { error?: string }
  items: number
  passed: number
  planned?: number | null
  queued_at: string
  finished_at?: string
  flows?: DetectionFlow[]
  identity_source?: IdentitySource | null
  models?: string[]
}

/** 一次检测中一道题的结果（含题目与回答片段，仅管理员可见） */
export interface ProbeRunResult {
  item_id?: number
  suite: string
  category: string
  prompt: string
  models: string[]
  passed: boolean | null
  http_status?: number
  error_class?: string
  ttft_ms: number
  duration_ms: number
  output_tokens: number
  reasoning_tokens: number
  response_model?: string
  excerpt?: string
  check_key?: string
  instance_idx?: number
  step?: number
  instance_hash?: string
  generator_version?: string
  input_tokens?: number | null
  cache_read_tokens?: number | null
  cache_creation_tokens?: number | null
  stop_reason?: string
  effort?: string
  flags?: string[]
  error_body?: string
  /** 标准答案，仅管理员明细接口返回；卖家 / 买家侧响应不携带 */
  expected_answer?: string
  /** 参数化实例实际发出的题面（超长截断），仅管理员明细接口返回 */
  instance_prompt?: string
}

export interface InspectionAppraisal {
  base: number | null
  factor: number | null
  multiplier: number | null
  tier: string
  checked_at: string | null
  configured: boolean
  provisional: boolean
}

/** 卖家与市场使用脱敏后的结果，不读取内部 evidence 或精确阈值。 */
export interface InspectionCheckResult {
  key: string
  name: string
  category: string
  status: 'pass' | 'warning' | 'fail' | 'inconclusive' | 'not_applicable' | 'not_run'
  role: 'gate' | 'factor' | 'display'
  factor_group: string | null
  factor: number | null
  applied: boolean
  metrics?: Record<string, { value: number | null; unit: string; ci?: [number, number] | null; confidence?: number | null }>
  samples?: { observed: number; paired: number; excluded: number }
  reason?: string
  advice?: string
  impact?: string
  checked_at?: string
}

export interface InspectionGate {
  check_key: string
  code: string
  health: 'degraded' | 'unavailable'
  confirmed: boolean
  occurred_at: string
  reason: string
}

export interface InspectionCheckDefinition {
  key: string
  name: string
  category: string
  role: 'gate' | 'factor' | 'display'
  factor_group: string | null
  enabled: boolean
  calibrated: boolean
  coefficients: Record<string, number>
  thresholds: Record<string, number>
  advice: string
  calibration_note: string
  description?: string
  flow?: InspectionFlow
  pricing?: boolean
  default_pricing?: boolean
}

export interface InspectionCheckOverride {
  enabled: boolean
  calibrated: boolean
  calibration_note: string
  coefficients: Record<string, number>
  thresholds: Record<string, number>
  pricing?: boolean | null
}

export interface InspectionTokenBudget {
  input_tokens_per_side: number
  output_tokens_per_side: number
}

export interface ModelCapability {
  platform: string
  model: string
  context_window: number
  context_window_by_auth: Record<string, number> | null
  max_output: number
  max_output_unlimited: boolean | null
  tools: boolean | null
  json_schema: boolean | null
  image: boolean | null
  pdf: boolean | null
  multiturn: boolean | null
  thinking: boolean | null
  prompt_cache: boolean | null
  efforts: string[] | null
  source: string
}

export type ModelCapabilityOverride = Pick<ModelCapability, 'platform' | 'model'> & Partial<ModelCapability>

export interface InspectionSettings {
  mode: 'trial' | 'formal'
  check_overrides: Record<string, Partial<InspectionCheckOverride>>
  model_capabilities?: ModelCapabilityOverride[]
  model_capability_defaults?: ModelCapability[]
  grade_thresholds: Record<string, number>
  grade_hysteresis: number
  downgrade_confirmations: number
  s_requirements: { gate_free_days: number; availability_days: number; min_availability: number }
  scheduling: { enabled: boolean; minimum_grade: string; strategy: string }
  timeouts: { full_minutes: number; quick_minutes: number }
  limits: { account_parallelism: number; global_parallelism: number }
  token_budgets: Record<'full' | 'quick', InspectionTokenBudget>
  [key: string]: unknown
}

export interface ProbeRunDetail {
  run: ProbeRun
  results: ProbeRunResult[]
  reference?: ProbeRun
  reference_results?: ProbeRunResult[]
}

/** 全局检测记录：检测本身、上游信息与各套件的题数和通过数 */
export interface ProbeRunRecord extends ProbeRun {
  platform: string
  auth_type: string
  seller_id: number
  account_email?: string
  is_reference: boolean
  suites: Record<string, { items: number; passed: number }>
}

/** 卖家查看的检测记录，只含状态与结论（不含进度、模型、流程与第三方渠道）。 */
export interface SellerProbeRun {
  id: number
  trigger: string
  status: string
  queued_at: string
  started_at?: string | null
  finished_at?: string | null
  report_id: number | null
  verdict: string | null
}

/** 一道题在筛选范围内的答题统计，被测上游与基线账号分开统计 */
export interface ProbeItemStat {
  item_id: number
  suite: string
  category: string
  prompt: string
  models: string[]
  status: string
  asked: number
  passed: number
  errors: number
  avg_duration_ms: number
  avg_output_tokens: number
  avg_reasoning_tokens: number
  last_asked_at?: string
  ref_asked: number
  ref_passed: number
}

/** 一道题在某次检测中的结果 */
export interface ProbeItemResult extends Pick<ProbeRunResult, 'check_key' | 'instance_idx' | 'step' | 'instance_hash' | 'generator_version' | 'input_tokens' | 'cache_read_tokens' | 'cache_creation_tokens' | 'stop_reason' | 'effort' | 'flags' | 'error_body'> {
  run_id: number
  upstream_id: number
  platform: string
  account_email?: string
  model: string
  trigger: string
  passed: boolean | null
  http_status?: number
  error_class?: string
  ttft_ms: number
  duration_ms: number
  output_tokens: number
  reasoning_tokens: number
  response_model?: string
  excerpt?: string
  created_at: string
}

/** 题目请求模板只保存协议无关数据，不执行工具或生成的代码。 */
export interface ProbeRequest {
  model?: string
  prompt?: string
  max_tokens?: number
  reasoning_effort?: string
  system?: string
  messages?: { role: string; content: Record<string, unknown>[] }[]
  tools?: { name: string; description?: string; input_schema: Record<string, unknown> }[]
  attachments?: { id: string; media_type: string; encoding: string; data: string }[]
  effort?: string
  extra?: Record<string, unknown>
  expected?: { outcome?: string; http_status?: number[]; error_codes?: string[] }
  steps?: { request?: ProbeRequest; expected?: Grader; use_previous?: boolean; tool_results?: Record<string, unknown> }[]
}

export interface ProbeGenerator {
  template_id?: string
  version?: string
  parameters?: Record<string, unknown>
  instances?: { full?: number; quick?: number }
}

/** 题库生成器模板的注册信息（GET /api/admin/probe-generator-templates） */
export interface GeneratorTemplate {
  id: string
  versions: string[]
  parameters: string[]
}

export interface ProbeItem {
  id: number
  suite: string
  category: string
  prompt: string
  grader: Grader
  platforms: string[]
  models: string[]
  max_tokens: number
  reasoning_effort?: string
  status: string
  version: number
  irt_difficulty?: number
  irt_discrimination?: number
  check_key?: string
  request?: ProbeRequest
  generator?: ProbeGenerator
  modes?: ('full' | 'quick')[]
  builtin_key?: string
  weight?: number
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
  /** 上架检测或卖家手动复测的探测：消耗卖家自己的额度，不结算收益 */
  seller_borne?: boolean
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
export interface ResetCreditItem {
  label?: string
  expires_at?: string
  resets_left: number
  /** Claude：用卡会恢复的窗口（five_hour / seven_day / seven_day_overage_included） */
  clears?: string[]
  redeemable: boolean
  /** Claude：暂时不能用的原因 */
  blocking?: string[]
}

export interface AutoResetState {
  status: 'checking' | 'available' | 'resetting' | 'success' | 'no_credit' | 'failed' | string
  trigger_window?: string
  available_count: number
  checked_at?: string
  last_result_at?: string
  error_code?: string
}

export interface AutoResetView {
  enabled: boolean
  /** 已在底层生效：授权且挂单正在面向买家时才开启 */
  active: boolean
  threshold_5h_percent?: number
  threshold_7d_percent?: number
  effective_5h_percent: number
  effective_7d_percent: number
  pause_line_percent: number
  authorized_at?: string
  state?: AutoResetState
}

export interface ResetCreditEvent {
  id: number
  trigger: 'manual' | 'auto'
  status: 'pending' | 'success' | 'no_credit' | 'failed' | 'unknown'
  windows: string[]
  error_code?: string
  detail?: Record<string, unknown>
  created_at: string
  finished_at?: string
}

export interface ResetCredits {
  platform: string
  /** 是否有官方数据（Claude 需要点"查询"才会向官方查询） */
  queried: boolean
  source?: 'snapshot' | 'live'
  available_count: number
  credits: ResetCreditItem[]
  cooldown_until?: string
  fetched_at?: string
  error?: string
  auto?: AutoResetView
  events: ResetCreditEvent[]
}

export interface AutoResetInput {
  enabled: boolean
  threshold_5h_percent?: number
  threshold_7d_percent?: number
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

/** 组织权限（IAM） */
export interface OrgUnit {
  id: number
  parent_id?: number | null
  name: string
  code: string
  sort: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface IAMResource {
  id: string
  parent_id?: string | null
  name: string
  kind: 'dir' | 'menu' | 'action'
  path: string
  icon: string
  sort: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface IAMRole {
  id: number
  code: string
  name: string
  description: string
  is_system: boolean
  member_count?: number
  created_at: string
  updated_at: string
}

export interface IAMStaff {
  id: number
  email: string
  is_admin: boolean
  status: string
  created_at: string
  roles?: IAMRole[]
  org_units?: OrgUnit[]
}

export interface ConsoleAccess {
  is_admin: boolean
  resource_ids: string[]
  menu_paths: string[]
}
