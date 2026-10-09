# 03 数据模型

本系统数据库（market PG）的表设计草案。金额统一用 `numeric(20,10)`，单位与 sub2api 的余额和 `usage_logs` 一致；现金进出按 1 元 = 1 个余额单位换算（决策 D16），所以账面金额可以直接当作人民币金额。敏感字段（凭证、密码、证件号）应用层加密后存储，字段名以 `_enc` 结尾。

以下 DDL 只表达结构，不是最终迁移脚本；**实际结构以 `migrations/0001–0007` 为准**。实现时的主要调整：

| 设计稿 | 实现 |
|---|---|
| `buyer_profiles` | `shadow_identities`：买家与探测用户共用的影子用户表（`kind` = buyer / probe），保存加密的密码与令牌、可用分组、退款锁定标记 |
| `pool_assignments` | 不单独建表：调度器每轮计算期望状态写入 `upstream_runtime_params.group_ids`，执行器与 sub2api 实际状态比对 |
| `reference_accounts` | 不单独建表：基线账号是 `upstreams.is_reference = true` 的上游，复用上架流程，不能挂单 |
| — | 新增 `vendor_configs`（厂商开关、寄售倍率区间、回收基础倍率、默认并发与限流）、`upstream_oauth_sessions`（授权会话）、`upstream_quality_state`（检测排期、恢复退避、手动复测计数）、`user_ip_events`（风控用的 IP 记录）、`refunds`（退款状态机） |

## 1. 用户与身份

```sql
users (
  id            bigserial primary key,
  email         text unique not null,
  password_hash text not null,
  status        text not null default 'active',   -- active / frozen / closed
  created_at    timestamptz not null default now()
);

kyc_profiles (                                     -- 卖家提现前必须实名
  user_id        bigint primary key references users,
  real_name_enc  bytea not null,
  id_number_enc  bytea not null,
  status         text not null,                    -- pending / verified / rejected
  verified_at    timestamptz
);

buyer_profiles (                                   -- 买家与 sub2api 影子用户的映射
  user_id                  bigint primary key references users,
  sub2api_user_id          bigint unique not null,
  sub2api_email            text not null,
  sub2api_password_enc     bytea not null,
  sub2api_refresh_token_enc bytea,
  concurrency              int not null,
  status                   text not null
);
```

## 2. 买家 Key 与路由

```sql
buyer_keys (
  id            bigserial primary key,
  user_id       bigint not null references users,
  name          text not null,
  key_hash      bytea unique not null,               -- 只存哈希，明文只在创建时返回一次
  key_prefix    text not null,                       -- 展示用，如 tm-ab12…
  route_type    text not null,                       -- default / chain
  status        text not null,                       -- active / disabled
  limits        jsonb not null default '{}',         -- 额度、5h/1d/7d 限额、IP 白名单、过期时间
  config_version bigint not null default 1,          -- 推送给 Edge 的版本号
  created_at    timestamptz not null default now()
);

buyer_key_hops (                                     -- route_type=default 时只有一跳
  key_id        bigint references buyer_keys,
  position      smallint check (position between 1 and 5),
  listing_id    bigint references listings,           -- 与 pool_id 二选一
  pool_id       bigint references pools,              -- 只允许出现在最后一跳
  primary key (key_id, position)
);

internal_keys (                                       -- 影子用户名下的 sub2api key，按"买家 Key × 分组"创建
  id               bigserial primary key,
  buyer_key_id     bigint not null references buyer_keys,
  buyer_user_id    bigint not null references users,
  sub2api_group_id bigint not null,
  sub2api_key_id   bigint unique not null,           -- 用量入账时据此把 usage_logs.api_key_id 对应回买家 Key
  key_enc          bytea not null,
  unique (buyer_key_id, sub2api_group_id)
);
```

## 3. 上游与挂单

```sql
upstreams (
  id                   bigserial primary key,
  seller_id            bigint not null references users,
  platform             text not null,      -- anthropic / openai / gemini / antigravity / grok / kimi / zhipu / minimax / opencode_go
  auth_type            text not null,      -- oauth / setup-token / google_one / coding / coding-intl / go
  status               text not null,      -- 见 04 状态机
  health               text not null default 'unknown',  -- healthy / suspected / degraded / unavailable / limited / unknown
  sub2api_account_id   bigint unique,
  proxy_id             bigint references proxies,
  probe_group_id       bigint,             -- sub2api tm-probe-<id>
  tier                 text,               -- 订阅档位，如 pro / max_5x / max_20x
  privacy_ok           boolean,
  self_reserve_percent smallint not null default 0,   -- 卖家自留比例，换算成 sub2api 窗口阈值
  created_at           timestamptz not null default now(),
  deleted_at           timestamptz
);

listings (
  id                 bigserial primary key,
  upstream_id        bigint not null references upstreams,
  seller_id          bigint not null references users,
  mode               text not null,      -- consignment / buyback
  status             text not null,      -- 见 04 状态机
  multiplier         numeric(10,4) not null,   -- 当前生效：寄售为 m_s，回收为 m_b
  join_default_pool  boolean not null default true,   -- 仅寄售有意义
  listing_group_id   bigint,             -- sub2api tm-l-<id>，按需创建
  created_at         timestamptz not null default now(),
  closed_at          timestamptz
);
-- 同一上游最多一个未关闭的挂单
create unique index on listings (upstream_id) where closed_at is null;

listing_price_versions (                -- 价格历史，用于对账与纠纷
  listing_id     bigint references listings,
  multiplier     numeric(10,4) not null,
  buyer_price    numeric(10,4),          -- 寄售：m_s × (1 + c)
  effective_from timestamptz not null,
  source         text not null           -- seller / quote / admin
);

buyback_quotes (
  id           bigserial primary key,
  upstream_id  bigint not null references upstreams,
  grade        text not null,
  multiplier   numeric(10,4) not null,
  status       text not null,            -- offered / accepted / rejected / expired / superseded
  valid_until  timestamptz not null,
  created_at   timestamptz not null default now()
);

proxies (                                -- 平台和卖家代理库存
  id               bigserial primary key,
  seller_id        bigint references users, -- NULL 表示平台库存，否则仅所属卖家可选
  name             text not null,
  protocol         text not null,          -- http / https / socks5 / socks5h
  host             text not null,
  port             int not null,
  username         text,
  password_enc     bytea,                  -- 加密保存，不通过 API 返回
  region           text,
  sub2api_proxy_id bigint unique,
  assigned_upstream_id bigint unique,
  status           text not null,          -- available / assigned / disabled
  created_at       timestamptz not null default now()
);
-- 卖家代理地址 (lower(host), port) 唯一；非 removed 上游的 proxy_id 唯一。
```

## 4. 默认分组与调度

```sql
pools (
  id               bigserial primary key,
  name             text unique not null,
  platform         text not null,        -- composite 表示全模型分组
  sub2api_group_id bigint unique not null,
  sell_multiplier  numeric(10,4) not null,   -- D
  min_margin       numeric(6,4) not null,
  safety_buffer    numeric(6,4) not null
);

pool_assignments (                       -- 期望状态，由调度策略产出
  pool_id       bigint references pools,
  upstream_id   bigint references upstreams,
  band          text not null,           -- buyback / consignment
  priority      int not null,
  load_factor   int,
  concurrency   int not null,
  reason        text,
  computed_at   timestamptz not null,
  primary key (pool_id, upstream_id)
);

upstream_runtime_params (                -- 账号级参数的期望值（与分组无关）
  upstream_id         bigint primary key references upstreams,
  priority            int not null,
  load_factor         int,
  concurrency         int not null,
  window_threshold    smallint,          -- credentials.account_scheduling_threshold
  extra_limits        jsonb not null default '{}',   -- window_cost_limit / base_rpm / max_sessions / 日额度
  buyer_facing        boolean not null,  -- false = 不调度（只保留在探测分组）
  updated_at          timestamptz not null
);
```

## 5. 质量检测

```sql
detection_policies (                     -- 管理员按厂商配置，可动态修改
  id                    bigserial primary key,
  platform              text not null,
  auth_type             text,            -- 为空表示该厂商所有接入方式
  enabled               boolean not null,          -- 是否检测（总开关）
  scheduled_enabled     boolean not null,          -- 是否定时检测，依赖 enabled
  interval_minutes      int not null,
  jitter_minutes        int not null,
  suites                jsonb not null,  -- availability / latency / iq / reasoning / long_context / tool_call
  models                jsonb not null,  -- 要测的模型
  daily_budget_usd      numeric(20,10) not null,   -- 每个上游每天的探测预算（按官方价计）
  thresholds            jsonb not null,  -- 判定阈值与置信度
  auto_pause            boolean not null,          -- 判定降智后是否自动不调度
  default_grade         text not null,             -- 未检测时的默认等级
  version               bigint not null,
  updated_by            bigint,
  updated_at            timestamptz not null,
  unique (platform, auth_type)
);

detection_overrides (                    -- 管理员对单个上游的覆盖
  upstream_id         bigint primary key references upstreams,
  enabled             boolean,           -- 为空表示沿用厂商策略
  scheduled_enabled   boolean,
  expires_at          timestamptz,
  reason              text
);

probe_items (                            -- 私有题库，卖家不可见
  id                   bigserial primary key,
  suite                text not null,
  category             text not null,
  prompt               jsonb not null,   -- 请求体模板
  grader               jsonb not null,   -- 判分方式：exact / numeric / regex / unit_test / json_schema
  platforms            text[] not null,
  irt_difficulty       double precision,
  irt_discrimination   double precision,
  status               text not null,    -- draft / active / retired
  version              int not null
);

reference_accounts (                     -- 平台自备的基线账号
  id                 bigserial primary key,
  platform           text not null,
  sub2api_account_id bigint unique not null,
  ref_group_id       bigint not null,
  status             text not null
);

system_keys (                            -- 平台探测用户名下的 sub2api key，每个探测分组 / 基线分组一把
  id               bigserial primary key,
  purpose          text not null,        -- probe / reference
  sub2api_group_id bigint unique not null,
  sub2api_key_id   bigint unique not null,
  key_enc          bytea not null
);
-- 探测用户的 sub2api 余额由 worker 定期补足；补足本身不记成本，探测成本在用量入账时按卖家收益记录（见 08）

probe_runs (
  id                 bigserial primary key,
  upstream_id        bigint references upstreams,
  reference_id       bigint references reference_accounts,   -- 与 upstream_id 二选一
  trigger            text not null,      -- onboarding / scheduled / recovery / seller_manual / admin / adaptive
  policy_version     bigint not null,
  suites             jsonb not null,
  status             text not null,      -- running / done / failed / skipped_budget
  cost_usd           numeric(20,10),
  started_at         timestamptz not null,
  finished_at        timestamptz
);

probe_results (
  run_id             bigint references probe_runs,
  item_id            bigint references probe_items,
  model              text not null,
  passed             boolean,
  score              double precision,
  ttft_ms            int,
  output_tps         double precision,
  reasoning_tokens   int,
  error_class        text,
  raw_ref            text                -- 原始响应存对象存储，仅管理员可读
);

quality_verdicts (
  id              bigserial primary key,
  upstream_id     bigint not null references upstreams,
  model           text not null,
  verdict         text not null,         -- normal / suspected / degraded / unavailable / inconclusive
  relative_score  double precision,      -- 相对基线
  confidence      double precision,
  attribution     text,                  -- account / proxy / vendor_wide / time_window / unknown
  run_ids         bigint[] not null,
  created_at      timestamptz not null default now()
);

detection_providers (                    -- 第三方检测站配置
  name               text primary key,     -- bazaarlink
  enabled            boolean not null,
  base_url           text not null,
  api_key_enc        bytea,                -- 加密存储
  options            jsonb not null,       -- 检测站可选参数；allow_insecure_target 仅开发环境
  max_runs_per_hour  int not null,
  timeout_minutes    int not null          -- 从排队到出结果的最长等待
);
-- detection_policies.third_party jsonb：[{provider, triggers, model, format, on_mismatch, on_submodel_mismatch}]

third_party_checks (                     -- 每次检测提交给检测站的记录
  id bigserial primary key,
  run_id          bigint not null references probe_runs,   -- 检测记录在等待期间处于 third_party 状态
  upstream_id     bigint not null,
  provider        text not null,
  model           text not null,
  format          text not null,           -- openai / anthropic
  config          jsonb not null,          -- 策略检测项快照
  status          text not null,           -- pending / running / completed / failed / timeout / skipped
  external_id     text,                    -- 检测站的检测编号
  token_hash      text,                    -- Edge 临时令牌哈希，结束后撤销
  outcome         text,                    -- pass / submodel_mismatch / mismatch / spoof / inconclusive
  verdict         text,                    -- 折算后的结论
  summary         jsonb,                   -- 脱敏摘要（不含被测端点的原始回答）
  report_url      text,
  deadline        timestamptz not null
);

test_reports (                           -- 卖家可见的报告（已脱敏）
  id           bigserial primary key,
  upstream_id  bigint not null references upstreams,
  run_id       bigint not null references probe_runs,
  verdict      text not null,
  summary      jsonb not null,           -- 见 05 第 7 节
  impact       jsonb not null,           -- 调度状态变化
  created_at   timestamptz not null default now()
);

appeals (
  id          bigserial primary key,
  report_id   bigint not null references test_reports,
  seller_id   bigint not null references users,
  reason      text not null,
  status      text not null,             -- open / retesting / upheld / overturned
  admin_note  text,
  created_at  timestamptz not null default now()
);

health_events (                          -- 被动信号：sub2api 账号状态变化、错误率、限流等
  id           bigserial primary key,
  upstream_id  bigint not null references upstreams,
  type         text not null,
  source       text not null,            -- sub2api_account / usage / edge
  detail       jsonb,
  created_at   timestamptz not null default now()
);
```

## 6. 用量、账本与结算

```sql
usage_records (                          -- 从 sub2api usage_logs 拉取的子集，按月分区
  sub2api_usage_id        bigint primary key,
  sub2api_request_id      text not null,   -- 形如 client:<UUID>，与 edge_attempts 关联
  created_at              timestamptz not null,
  sub2api_account_id      bigint not null,
  sub2api_group_id        bigint,
  sub2api_user_id         bigint not null,
  sub2api_api_key_id      bigint not null,
  model                   text not null,
  input_tokens            int, output_tokens int, cache_creation_tokens int, cache_read_tokens int,
  total_cost              numeric(20,10) not null,
  actual_cost             numeric(20,10) not null,
  account_rate_multiplier numeric(10,4),
  first_token_ms          int,
  duration_ms             int,
  -- 入账时解析出的业务归属
  upstream_id             bigint,
  listing_id              bigint,
  buyer_user_id           bigint,        -- 探测流量为空
  buyer_key_id            bigint,
  channel                 text not null  -- pool / direct / probe / reference / unknown
);

edge_attempts (                          -- Edge 每一跳一条，按天分区，保留 90 天
  id                   bigserial,
  edge_request_id      text not null,    -- Edge 为买家请求生成的 ID
  buyer_key_id         bigint not null,
  hop                  smallint not null,
  listing_id           bigint,
  pool_id              bigint,
  sub2api_request_id   text,             -- 来自 X-Client-Request-ID；连接失败时为空
  http_status          int,
  committed            boolean not null, -- 是否已向买家输出内容
  end_reason           text not null,    -- completed / client_cancel / upstream_error / timeout / skipped
  ttfb_ms              int,
  started_at           timestamptz not null,
  ended_at             timestamptz not null,
  primary key (id, started_at)
);
create index on edge_attempts (sub2api_request_id);

billing_anomalies (                      -- 逐次尝试对账发现的不一致，见 08 第 6.2 节；只记录与跟踪，不自动调账
  id                 bigserial primary key,
  rule               text not null,      -- R1_failed_with_charge / R2_success_without_charge / R3_orphan_charge / R4_duplicate
  sub2api_usage_id   bigint,
  edge_attempt_id    bigint,
  amount             numeric(20,10),
  status             text not null,      -- open / explained / adjusted / ignored
  ledger_txn_id      bigint,             -- 管理员人工调整时关联的账本交易
  note               text,
  created_at         timestamptz not null default now(),
  resolved_at        timestamptz
);

ingest_cursors (
  source      text primary key,          -- usage_logs
  last_id     bigint not null,
  updated_at  timestamptz not null
);

ledger_accounts (
  id          bigserial primary key,
  owner_type  text not null,             -- buyer / seller / platform
  owner_id    bigint,
  kind        text not null,             -- buyer_cash / buyer_bonus（买家余额的账面镜像，可为负）
                                         -- seller_pending / seller_available / seller_withdrawing
                                         -- platform_revenue / platform_probe_cost / cash_in / cash_out
  unique (owner_type, owner_id, kind)
);

ledger_txns (
  id          bigserial primary key,
  type        text not null,             -- recharge / usage / settle / unfreeze / withdraw / refund / adjust
  ref         text not null,             -- 业务单号，唯一，防重复记账
  created_at  timestamptz not null default now(),
  unique (type, ref)
);

ledger_entries (                         -- 复式记账，每笔交易借贷平衡
  id          bigserial primary key,
  txn_id      bigint not null references ledger_txns,
  account_id  bigint not null references ledger_accounts,
  amount      numeric(20,10) not null,   -- 正为借，负为贷
  created_at  timestamptz not null default now()
);

settlement_lines (                       -- 按小时聚合的卖家应收
  seller_id      bigint not null,
  upstream_id    bigint not null,
  listing_id     bigint,
  hour           timestamptz not null,
  channel        text not null,
  request_count  int not null,
  total_cost     numeric(20,10) not null,
  payout         numeric(20,10) not null,   -- Σ total_cost × account_rate_multiplier
  buyer_paid     numeric(20,10) not null,   -- Σ actual_cost
  primary key (upstream_id, hour, channel)
);

recharge_orders (                        -- 充值单
  id             bigserial primary key,
  user_id        bigint not null,
  amount         numeric(20,10) not null,
  provider       text not null,          -- manual / alipay / wechat
  channel        text not null,          -- manual / alipay_page / alipay_f2f / wechat_native
  out_trade_no   text unique,            -- 发给渠道的商户订单号
  provider_ref   text,                   -- 渠道交易号或人工流水号
  status         text not null,          -- pending / crediting / credited / cancelled / expired / failed
  pay_kind       text,                   -- redirect / qrcode
  pay_payload    text,                   -- 收银台地址或二维码内容
  expires_at     timestamptz,            -- 支付时限
  close_reason   text,
  last_synced_at timestamptz,            -- 最近一次主动查单
  refunded_amount numeric(20,10) not null default 0,   -- 已原路退回
  op_id          bigint,                 -- 给影子用户加余额的出站操作
  created_at, paid_at, credited_at
);

payment_merchants (                      -- 站内配置的支付商户
  provider     text primary key,           -- alipay / wechat
  enabled      boolean not null,           -- 停用后不再创建新订单，已有订单照常处理
  config       jsonb not null,             -- AppID、商户号、网关、加签方式、公钥与证书
  secrets_enc  bytea,                      -- 加密的私钥、APIv3 密钥
  updated_by bigint, updated_at timestamptz
);

payment_notifications (                  -- 渠道异步通知的处理记录
  id bigserial primary key, provider text, out_trade_no text, trade_no text, state text, amount_fen bigint,
  result text not null,                  -- credited / duplicate / expired / ignored / amount_mismatch / unknown_order / invalid / error
  error text, created_at timestamptz
);

refund_payouts (                         -- 余额退款的打款明细
  id                bigserial primary key,
  refund_id         bigint not null references refunds,
  recharge_order_id bigint references recharge_orders,  -- 原路退回的充值单；人工打款为空
  channel           text not null,       -- 原充值单的支付方式 / manual
  amount            numeric(20,10) not null,
  out_refund_no     text unique,         -- 渠道退款单号（幂等）
  status            text not null,       -- pending / processing / success / failed / manual
  provider_ref      text, error text, attempts int, next_attempt_at timestamptz, finished_at timestamptz
);

withdrawals (
  id           bigserial primary key,
  seller_id    bigint not null references users,
  amount       numeric(20,10) not null,
  status       text not null,            -- pending_review / approved / paying / paid / rejected / failed
  channel      text not null,
  reviewed_by  bigint,
  created_at   timestamptz not null default now()
);
```

## 7. sub2api 写操作出站表

所有改变 sub2api 状态的调用都先落这张表，再由执行器发送。用于重试、幂等和审计。

```sql
sub2api_ops (                            -- 已实现，见 migrations/0001_init.sql
  id               bigserial primary key,
  op_type          text not null,        -- user.balance_add / account.create / account.bulk_update / ...
  target           text not null,        -- 如 account:123；同一 target 严格按入队顺序执行
  payload          jsonb not null,
  idempotency_key  text unique not null, -- 同时作为调用 sub2api 时的 Idempotency-Key
  priority         smallint not null default 100,  -- 越小越先发送，不调度类操作用 10
  status           text not null,        -- pending / sending / done / failed / dead
  attempts         int not null default 0,
  max_attempts     int not null default 10,
  next_attempt_at  timestamptz not null, -- 失败后按指数退避
  locked_until     timestamptz,          -- sending 状态的租约，进程崩溃后可被重新领取
  last_error       text,
  result           jsonb,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  done_at          timestamptz
);
```

## 8. 其他

```sql
settings (key text primary key, value jsonb not null, updated_at timestamptz not null);
  -- commission_rate、multiplier_range、freeze_days、withdraw_min 等

compat_state (                           -- 兼容自检结果，单行
  id                    smallint primary key default 1,
  sub2api_version       text,
  migration_fingerprint text,
  guard_state           text not null,   -- normal / safe_mode
  checked_at            timestamptz
);

risk_events (...);
audit_logs (...);                        -- 管理员与系统对 sub2api 的所有写操作
```
