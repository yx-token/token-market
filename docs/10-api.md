# 10 第一期接口

控制面接口由 market-api 提供，前缀 `/api`，JSON，平台用户 JWT 认证（`Authorization: Bearer <token>`）；管理后台接口额外要求管理员角色。买家调用模型的接口由 Edge 提供，见 [07](07-edge.md)。

响应格式：成功 `{"data": ...}`；失败 `{"error": {"code": "...", "message": "..."}}`，`code` 是稳定的错误码（如 `EMAIL_TAKEN`、`LISTING_UNAVAILABLE`、`SUB2API_SAFE_MODE`），前端按它显示文案。列表接口统一支持 `limit`（默认 50，最大 500）与 `offset`。

路由表在 `internal/httpapi/server.go`，端到端测试 `internal/e2e` 覆盖了下面全部接口。

## 1. 账号

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 注册：`email`、`password` |
| POST | `/api/auth/login` | 登录，返回 `token`、`expires_at` |
| POST | `/api/auth/refresh` | 用仍然有效的 token 换新 token（重新读取角色与状态） |
| GET | `/api/me` | 当前用户信息，含实名状态 |
| POST | `/api/me/kyc` | 提交实名：`real_name`、`id_number`（卖家提现前必需，证件号加密存储并按盲索引去重） |

防爆破：登录失败按 IP（30 次 / 15 分钟）和邮箱（10 次 / 15 分钟）计数，注册按 IP（20 次 / 小时）计数，超过返回 429 `TOO_MANY_ATTEMPTS`。客户端 IP 按 `http.trusted_proxies` 的受信任代理规则解析。

## 2. 市场（买家浏览，无需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/market/listings` | 生效中的寄售挂单（已排除降智、不可用、不面向买家的上游）。筛选：`platform`、`grade`、`max_price`；排序 `sort`：`price`（默认）/ `success` / `ttft` |
| GET | `/api/market/listings/:id` | 挂单摘要：厂商、档位、买家价 `price`（= m_s × (1 + c)）、健康状态、等级、24h 成功率与请求数、首字延迟 |
| GET | `/api/market/pools` | 默认分组列表及售价 D |
| GET | `/api/market/vendors` | 已开放接入的厂商、接入方式、寄售倍率区间、是否支持自留比例 |
| GET | `/api/market/config` | 前端需要的公开参数：Edge 调用地址、抽佣率、冻结天数、最低提现额、最大跳数、可用的支付方式 `payment_channels`、单笔充值上下限 |
| GET | `/api/market/prices` | 官方价目（美元 / 百万 token），倍率的计算基准；取自 sub2api 内置价目表，模型列表由系统设置 `price_models` 决定 |

## 3. 买家

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/buyer/wallet` | `balance`（sub2api 实时余额）、`ledger_cash`（账面余额，用量入账有几分钟延迟）、近期充值、进行中的退款 |
| POST | `/api/buyer/recharge` | 创建充值订单：`amount`（两位小数）、`channel`（`alipay_page` / `alipay_f2f` / `wechat_native` / `manual`，默认 `manual`）。返回订单与 `payment`：`kind` 为 `redirect`（跳转 `url`，支付宝收银台）、`qrcode`（把 `qr_code` 渲染成二维码）或 `manual` |
| GET | `/api/buyer/recharge/:id` | 充值订单：状态 `pending` / `crediting` / `credited` / `failed` / `cancelled` / `expired`；待支付订单带 `payment` 便于继续支付。待支付的在线订单距上次查单超过 5 秒时先向渠道查单 |
| POST | `/api/buyer/recharge/:id/cancel` | 取消未支付订单（在线订单先向渠道关单，发现已支付返回 409 `ORDER_PAID`） |
| POST | `/api/buyer/refunds` | 申请退还全部剩余余额（退到分）：先暂停该买家全部 Key，等待 `refund_lock_seconds` 后扣减 sub2api 余额，再按打款明细原路退回或人工打款 |
| GET | `/api/buyer/refunds` | 退款记录，含打款明细 `payouts`（渠道、金额、状态） |
| POST | `/api/buyer/keys` | 创建 Key，明文只返回一次 |
| GET | `/api/buyer/keys` | Key 列表 |
| GET | `/api/buyer/keys/:id` | Key 详情，含各跳状态：`ready` / `provisioning`（内部资源开通中）/ `skipped`（挂单当前不接流量）；限额封禁原因 `limit_block_reason` |
| PUT | `/api/buyer/keys/:id` | 修改 `name`、`route`、`limits`（修改限额会立即解除限额封禁） |
| POST | `/api/buyer/keys/:id/disable`、`/enable` | 停用 / 启用 |
| DELETE | `/api/buyer/keys/:id` | 删除（立即从 Edge 移除；sub2api 中的内部 key 由后台清理） |
| GET | `/api/buyer/usage` | 用量明细，可按 `key_id`、`listing_id`、`model`、`from`、`to`（RFC 3339）筛选，默认近 7 天 |
| GET | `/api/buyer/usage/:edge_request_id` | 单次请求详情：每一跳的状态、是否产生费用（失败的跳标注"未产生费用"）、实际服务的挂单、费用 |
| GET | `/api/buyer/bills` | 按日汇总的账单：`days`（默认 30） |

创建 Key 示例：

```json
{
  "name": "claude-code",
  "route": {
    "type": "chain",
    "hops": [
      { "listing_id": 101 },
      { "listing_id": 205 },
      { "pool": "tm-pool-claude" }
    ]
  },
  "limits": {
    "quota_usd": 50,
    "rate_limit_1d_usd": 10,
    "ip_whitelist": ["203.0.113.0/24"],
    "expires_at": "2026-12-31T00:00:00Z"
  }
}
```

也可以只走默认分组：`{"route": {"type": "default", "pool": "tm-pool-claude"}}`。

校验：最多 5 跳；默认分组只能在最后一跳；同一挂单不能重复出现；只能指定生效中的**寄售**挂单（回收挂单只通过默认分组售卖）；链内各跳是同一平台，最后一跳的默认分组必须是同平台分组或全模型分组（`platform = composite`），避免同一请求在不同协议的上游之间转换。

## 4. 卖家

### 4.1 上游

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/vendors` | 可接入的厂商与接入方式（由管理员启用的厂商决定） |
| POST | `/api/seller/upstreams` | 创建上游草稿：`platform`、`auth_type`，同时分配专用代理 |
| POST | `/api/seller/upstreams/:id/oauth/start` | 获取授权链接（30 分钟有效） |
| POST | `/api/seller/upstreams/:id/oauth/complete` | 提交授权码或完整回调地址：`code`。需要 state 的厂商（OpenAI、Gemini 等）请粘贴完整回调地址 |
| POST | `/api/seller/upstreams/:id/coding-key` | 提交 Coding Plan key：`api_key`；查不到官方窗口用量的 key 被拒绝 |
| POST | `/api/seller/upstreams/:id/reauthorize` | 授权失效后重新授权：返回新的授权链接，之后同样用 `oauth/complete` 提交；必须是同一个官方账号 |
| POST | `/api/seller/upstreams/:id/retry-verification` | 上架检测失败后重新检测 |
| GET | `/api/seller/upstreams` | 上游列表 |
| GET | `/api/seller/upstreams/:id` | 详情：状态、健康状态、档位、等级、当前挂单与报价、近 24 小时服务情况（请求数、收益、首字延迟、成功率） |
| PUT | `/api/seller/upstreams/:id/settings` | 修改自留比例：`self_reserve_percent`（0–90，仅支持窗口阈值的厂商） |
| DELETE | `/api/seller/upstreams/:id` | 删除上游 |

### 4.2 挂单

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/seller/upstreams/:id/listing` | 创建挂单：`mode`（consignment / buyback）；寄售需填 `multiplier`，可选 `join_default_pool`（默认加入）；回收返回报价 |
| GET | `/api/seller/listings` | 挂单列表 |
| GET | `/api/seller/listings/:id` | 挂单详情，含价格历史 |
| GET | `/api/seller/upstreams/:id/quote` | 当前回收报价 |
| POST | `/api/seller/listings/:id/accept-quote`、`/reject-quote` | 接受 / 拒绝回收报价 |
| PUT | `/api/seller/listings/:id/price` | 寄售改价：`multiplier`（有冷却时间 `price_change_cooldown_minutes`） |
| PUT | `/api/seller/listings/:id/default-pool` | 加入 / 退出默认分组：`join`（仅寄售） |
| POST | `/api/seller/listings/:id/pause`、`/resume`、`/close` | 暂停、恢复、关闭 |

### 4.3 测试报告

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/upstreams/:id/reports` | 报告列表 |
| GET | `/api/seller/reports/:id` | 报告详情（脱敏，不含题目、答案与基线信息，见 [05](05-quality.md) 第 8 节） |
| GET | `/api/seller/upstreams/:id/quality-trend` | 每日智力相对分与异常次数：`days`（默认 30） |
| POST | `/api/seller/upstreams/:id/retest` | 手动复测（每日限次，仅该厂商开启检测时可用） |
| POST | `/api/seller/reports/:id/appeal` | 对降智 / 不可用的判定申诉：`reason` |

### 4.4 收益与提现

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/earnings` | 待结算、可提现、提现中、今日、近 7 天、累计 |
| GET | `/api/seller/earnings/lines` | 按上游、小时、渠道的结算明细，可按 `upstream_id` 筛选 |
| POST | `/api/seller/withdrawals` | 申请提现：`amount`、`channel`、`account_info`（加密存储）。需实名；低于自动审核阈值且无风险时自动通过 |
| GET | `/api/seller/withdrawals` | 提现记录 |

## 5. 支付渠道通知

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/payments/alipay/notify` | 支付宝异步通知（表单），验签后推进订单，处理成功返回 `success` |
| POST | `/api/payments/wechat/notify` | 微信支付通知（APIv3，验签、防重放、AES-GCM 解密），成功返回 204 |

不需要登录，安全性完全依赖验签。处理结果写入 `payment_notifications`，在后台"支付渠道"中查看。

## 6. 管理后台

所有写操作记审计日志。

| 模块 | 接口 |
|---|---|
| 系统设置 | `GET/PUT /api/admin/settings`：抽佣率、冻结天数、最低提现额、自动审核阈值、等级系数、报价有效期、改价冷却、退款等待、新卖家期、价目模型等（部分更新） |
| 厂商 | `GET /api/admin/vendors`、`PUT /api/admin/vendors/:platform`：是否开放接入、寄售倍率区间、回收基础倍率表（按档位）、默认并发、限流参数 |
| 代理库存 | `GET/POST /api/admin/proxies`、`POST /api/admin/proxies/:id/sync`（同步到 sub2api）、`/disable`、`/enable` |
| 默认分组 | `GET/POST /api/admin/pools`、`PUT /api/admin/pools/:id`（售价 D、最低毛利、安全缓冲、启用）、`POST /api/admin/pools/:id/sync` |
| 检测策略 | `GET /api/admin/detection-policies`、`PUT /api/admin/detection-policies/:platform[?auth_type=]`（`enabled`、`scheduled_enabled`、周期、套件、模型、预算、阈值、`auto_pause`、`default_grade`、`third_party`，见 [05](05-quality.md) 第 3、9 节） |
| 第三方检测 | `GET /api/admin/detection-providers`（可接入的检测站、配置、最近 1 小时提交次数）、`PUT /api/admin/detection-providers/:name`（`enabled`、`base_url`、`api_key`、`settings`、`max_runs_per_hour`、`timeout_minutes`）、`GET /api/admin/third-party-checks[?upstream_id=]` |
| 单上游检测覆盖 | `PUT /api/admin/upstreams/:id/detection-override`、`DELETE …` |
| 手动检测 | `POST /api/admin/upstreams/:id/probe`（对关闭检测的厂商也可执行） |
| 题库 | `GET/POST /api/admin/probe-items`、`PUT /api/admin/probe-items/:id`、`POST /api/admin/probe-items/:id/retire` |
| 基线账号 | `GET/POST /api/admin/reference-accounts`、`DELETE /api/admin/reference-accounts/:id`（授权复用卖家的授权接口） |
| 质量看板 | `GET /api/admin/quality/overview`（各厂商健康与等级分布、官方全局事件、降智上游列表） |
| 申诉 | `GET /api/admin/appeals`、`POST /api/admin/appeals/:id/resolve`（`action`：`retest` / `overturn` / `uphold`） |
| 上游 | `GET /api/admin/upstreams`、`POST /api/admin/upstreams/:id/suspend`、`/unsuspend`、`DELETE /api/admin/upstreams/:id` |
| 用户与实名 | `GET /api/admin/kyc`、`POST /api/admin/kyc/:id/review`、`POST /api/admin/users/:id/status`（冻结 / 解冻） |
| 充值 | `GET /api/admin/recharges`、`POST /api/admin/recharges/:id/confirm`（只用于人工充值单）、`POST /api/admin/recharges/:id/sync`（向渠道查单） |
| 支付配置 | `GET /api/admin/payments/merchants`（支付宝、微信支付商户配置，密钥只返回是否已设置；`ready` 与 `problem` 说明是否可用）、`PUT /api/admin/payments/merchants/:provider`（`enabled`、`alipay` / `wechat` 配置、`private_key`、`api_v3_key`，密钥留空表示不变）、`POST /api/admin/payments/merchants/:provider/test`（测试连接）、`GET /api/admin/payments/status`（各支付方式是否可用，最近 50 条通知处理记录）；通知地址、支付方式开关、充值上下限、支付时限、原路退回在系统设置中（`payment_public_url`、`payment_channels`、`recharge_min`、`recharge_max`、`recharge_order_timeout_minutes`、`refund_original_route`、`refund_original_route_days`） |
| 退款 | `GET /api/admin/refunds`、`POST /api/admin/refunds/:id/paid`（登记人工打款部分的流水号）、`POST /api/admin/refund-payouts/:id/retry`（重试失败的原路退款）、`POST /api/admin/refund-payouts/:id/manual`（失败的原路退款改为人工打款） |
| 提现 | `GET /api/admin/withdrawals`、`POST /api/admin/withdrawals/:id/review`（`approve` 为真通过，否则驳回并退回可提现）、`POST /api/admin/withdrawals/:id/paid`（`failed` 为真表示打款失败） |
| 计费异常与调账 | `GET /api/admin/anomalies`、`POST /api/admin/anomalies/:id/resolve`、`POST /api/admin/adjustments`（必须填写原因，可关联异常） |
| 风控 | `GET /api/admin/risk-events`、`POST /api/admin/risk-events/:id/resolve` |
| 账本 | `GET /api/admin/ledger`（平台现金、收入、探测成本、调整、买家余额合计、卖家应付合计） |
| sub2api 状态 | `GET /api/admin/sub2api/status`（版本、迁移指纹、兼容自检状态、出站队列积压、入账游标） |
| 审计 | `GET /api/admin/audit-logs`（可按 `target` 筛选） |

**与设计稿的差异**：价格类设置合并在"系统设置"与"厂商"中，没有单独的 `/pricing`；提现审核用一个 `review` 接口；调账不设审批流（记录审计日志与原因）；第一期没有赠送余额，钱包不拆分现金与赠送；Key 详情不显示熔断状态（熔断在各 Edge 实例内存中）。
