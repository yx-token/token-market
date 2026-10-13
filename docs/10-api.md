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
| GET | `/api/market/listings` | 生效中的寄售 / 额度包（已排除降智、不可用、不面向买家的上游）。筛选：`platform`、`grade`、`max_price`、`kind`（`channel` / `pack`）；排序 `sort`：`price`（默认）/ `success` / `ttft` |
| GET | `/api/market/listings/:id` | 挂单摘要：厂商、档位、买家价 `price`（= m_s × (1 + c)）、`sku_kind`、额度包 `face_value`/`stock`、按量 `min_amount`、健康状态、等级、24h 成功率与请求数、首字延迟 |
| GET | `/api/market/pools` | 默认分组列表及售价 D |
| GET | `/api/market/discovery` | 首页运营配置（类目树、横幅、静态槽位等） |
| GET | `/api/market/recommend` | 首页四宫格推荐结果（按 `market_recommend` 因子打分） |
| GET | `/api/market/categories` | 公开分类树（仅启用节点） |
| GET | `/api/market/vendors` | 已开放接入的厂商、接入方式、寄售倍率区间、是否支持自留比例 |
| GET | `/api/market/config` | 前端需要的公开参数：Edge 调用地址、抽佣率、冻结天数、最低提现额、最大跳数、可用的支付方式 `payment_channels`、单笔充值上下限 |
| GET | `/api/market/prices` | 官方价目（美元 / 百万 token），倍率的计算基准；取自 sub2api 内置价目表，模型列表由系统设置 `price_models` 决定 |

## 3. 买家

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/buyer/wallet` | `balance`（sub2api 实时余额）、`refundable`（可按充值单退款的余额：实时余额减去未用完的额度包）、`ledger_cash`（账面余额，用量入账有几分钟延迟）、近期充值、进行中的退款 |
| POST | `/api/buyer/recharge` | 创建充值订单：`amount`（两位小数）、`channel`（`alipay_page` / `alipay_f2f` / `wechat_native` / `manual`，默认 `manual`）。返回订单与 `payment`：`kind` 为 `redirect`（跳转 `url`，支付宝收银台）、`qrcode`（把 `qr_code` 渲染成二维码）或 `manual` |
| GET | `/api/buyer/recharge/:id` | 充值订单：状态 `pending` / `crediting` / `credited` / `failed` / `cancelled` / `expired`；待支付订单带 `payment` 便于继续支付。待支付的在线订单距上次查单超过 5 秒时先向渠道查单 |
| POST | `/api/buyer/recharge/:id/cancel` | 取消未支付订单（在线订单先向渠道关单，发现已支付返回 409 `ORDER_PAID`） |
| POST | `/api/buyer/recharge/:id/refund` | 按充值单退款：`amount`（两位小数，不超过该订单未退金额与 `refundable`）、可选 `note`。不暂停 Key，直接扣减 sub2api 余额，成功后原路退回该订单（人工充值或超过原路退回期限时人工打款）；扣减失败则退款失败。已有进行中的退款返回 409 `REFUND_IN_PROGRESS` |
| POST | `/api/buyer/refunds` | 申请退还全部剩余余额（退到分）：先暂停该买家全部 Key，等待 `refund_lock_seconds` 后扣减 sub2api 余额，再按打款明细原路退回或人工打款 |
| GET | `/api/buyer/refunds` | 退款记录：`kind`（`balance` 退全部余额 / `order` 按充值单，带 `recharge_order_id` / `pack` 额度包原路退回），含打款明细 `payouts`（渠道、金额、状态） |
| POST | `/api/buyer/packs` | 购买额度包：`listing_id`，可选 `channel`（第一期仅 `balance`）、`name`。校验可调用余额 ≥ 面额后账面预付、占库存并签发 Key（明文只返回一次）；调用抵包内 remaining，未用量可退回账面 |
| GET | `/api/buyer/packs` | 我的额度包：面额、剩余、状态 |
| POST | `/api/buyer/packs/:id/refund` | 按未用量退货：停用 Key、释放卖家库存。可选 `dest`（`balance` / `original`，第一期记状态） |
| POST | `/api/buyer/keys` | 创建 Key，返回明文 |
| GET | `/api/buyer/keys` | Key 列表；`recoverable` 表示能否再次查看明文（加密保存明文之前创建的 Key 不能） |
| GET | `/api/buyer/keys/:id/secret` | 再次查看 Key 明文（`no-store`）；不能查看时返回 409 `KEY_NOT_RECOVERABLE` |
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

校验：最多 5 跳；默认分组只能在最后一跳；同一挂单不能重复出现；只能指定生效中的寄售或额度包挂单（回收挂单只通过默认分组售卖）；链内各跳是同一平台，最后一跳的默认分组必须是同平台分组或全模型分组（`platform = composite`），避免同一请求在不同协议的上游之间转换。额度包购买走 `/api/buyer/packs`，不要手工把额度包挂进多跳链。

## 4. 卖家

### 4.1 上游

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/vendors` | 可接入的厂商与接入方式（由管理员启用的厂商决定） |
| GET | `/api/seller/proxies` | 自己配置的代理列表（含占用状态，不含密码），供接入账号时选择 |
| POST | `/api/seller/proxies` | 配置代理：`name`、`protocol`、`host`（IP / 域名）、`port`，可选 `username`、`password`、`region`；同步失败返回 `PROXY_SYNC_FAILED`，已保存的代理可从列表重试 |
| POST | `/api/seller/proxies/:id/sync` | 重试同步代理配置；未就绪的代理不能绑定账号 |
| POST | `/api/seller/proxies/:id/test` | 检测连接、出口 IP、国家 / 地区及延迟，返回更新后的代理与 `connection_report`、`connection_checked_at` |
| POST | `/api/seller/proxies/:id/quality-check` | 检测 AI 站点可达性，返回更新后的代理与 `quality_report`、`quality_checked_at`（评分、等级及逐项结果）；最长 3 分钟 |
| POST | `/api/seller/proxies/:id/disable`、`/enable` | 停用 / 启用自己的空闲代理；已绑定代理不能停用 |
| POST | `/api/seller/upstreams` | 创建上游草稿：`platform`、`auth_type`、`proxy_id`（默认必填，选择自己的空闲代理，且最近一次连通性检测须成功，否则返回 `PROXY_NOT_CONNECTED`）；`require_seller_proxy=false` 时可不传，由平台从平台库存关联。`proxy_from_import=true`（不能与 `proxy_id` 同时传）时暂不绑定代理，导入账号时使用导出文件中的代理；这类上游在绑定代理前不能走官方授权或提交 key |
| POST | `/api/seller/upstreams/:id/oauth/start` | 获取授权链接（30 分钟有效）；Gemini Code Assist 可选 `project_id`（GCP 项目 ID） |
| POST | `/api/seller/upstreams/:id/oauth/complete` | 提交授权码或完整回调地址：`code`。需要 state 的厂商（OpenAI、Gemini 等）请粘贴完整回调地址 |
| POST | `/api/seller/upstreams/:id/coding-key` | 提交 Coding Plan key：`api_key`；智谱国内站团队版另填 `organization`、可选 `project`；查不到官方窗口用量的 key 被拒绝 |
| POST | `/api/seller/upstreams/:id/import` | 导入授权信息：`data` 为 sub2api v1 导出文件、账号数组、单个账号或纯凭证对象；`account_index` 可选，默认 0。每次导入一个账号。上游已绑定代理时沿用该代理、忽略文件中的代理；未绑定时按账号的 `proxy_key` 取文件 `proxies` 中的代理，登记到自己的代理列表（同一地址复用原记录）并实测连通，失败返回 `IMPORT_PROXY_UNREACHABLE`，文件缺少引用的代理返回 `IMPORT_PROXY_NOT_FOUND`，代理已被占用或停用返回 `PROXY_IN_USE`。仍执行隐私设置、订阅额度校验与上架检测 |
| POST | `/api/seller/upstreams/:id/reauthorize` | 授权失效后重新授权：返回新的授权链接，之后同样用 `oauth/complete` 提交；必须是同一个官方账号；可选 `project_id` 同上 |
| POST | `/api/seller/upstreams/:id/retry-verification` | 上架检测失败后重新检测 |
| GET | `/api/seller/upstreams` | 上游列表。`account_email` 是授权或导入时识别的官方账号邮箱，控制台用它作为账号名称（Gemini 与 Coding Plan 没有邮箱，按上游编号展示）；不向买家展示 |
| GET | `/api/seller/upstreams/:id` | 详情：状态、健康状态、档位、等级、当前挂单与报价、近 24 小时服务情况（请求数、收益、首字延迟、成功率）、能否使用重置卡与授权自动用卡（`reset_credits_supported`、`auto_reset_supported`）及自动用卡设置 |
| PUT | `/api/seller/upstreams/:id/settings` | 修改自留比例：`self_reserve_percent`（0–90，仅支持窗口阈值的厂商） |
| GET | `/api/seller/upstreams/:id/quota` | 账号的官方订阅额度：各窗口（5 小时、7 天、Sonnet / Opus 7 天、30 天、Gemini 每日、Antigravity 各模型、Coding Plan 各窗口）的使用率、重置时间与窗口内经平台的请求数 / token / 官方价费用，以及授权失效、封禁等提示和自留比例。Claude 默认读 sub2api 的响应头采样快照（不访问官方）；`refresh=1` 向官方查询（同一上游至少间隔 2 分钟，其余 1 分钟内复用结果） |
| GET | `/api/seller/upstreams/:id/reset-credits` | 重置卡（仅 Codex 与 Claude 的 OAuth 账号，详情的 `reset_credits_supported` 为真）：`queried`（是否有官方数据）、`available_count`、各张卡的到期时间（Claude 另有恢复的窗口 `clears`、能否使用及原因）、官方冷却时间；Codex 另有自动用卡 `auto`（是否授权、是否已在底层生效、卖家设定与实际生效的阈值、暂停线、sub2api 的运行状态）；最近 20 条用卡记录 `events`（手动与平台自动）。Codex 默认读 sub2api 缓存的快照，Claude 默认返回最近一次查询结果；`refresh=1` 向官方查询（30 秒内复用结果） |
| POST | `/api/seller/upstreams/:id/reset-credits/redeem` | 使用一张重置卡：`request_id`（8–64 位字母、数字、`-`、`_`，每次确认生成；网络中断重试沿用同一个，返回首次结果）、`expected_available`（确认时看到的可用张数）。用卡前向官方确认张数：没有卡返回 `NO_RESET_CREDIT`，张数变化返回 `RESET_CREDITS_CHANGED`，同一上游正在用卡返回 `RESET_IN_PROGRESS`，这三种情况都不消耗卡。返回用卡记录，`status` 为 success / no_credit / failed / unknown（结果不确定，卡可能已用掉） |
| PUT | `/api/seller/upstreams/:id/auto-reset` | 授权或取消平台自动用卡（仅 Codex）：`enabled`，可选 `threshold_5h_percent`、`threshold_7d_percent`（1 到暂停线 100 − 自留比例，留空跟随暂停线，超出返回 `INVALID_AUTO_RESET_THRESHOLD`）。只在挂单面向买家时生效 |
| DELETE | `/api/seller/upstreams/:id` | 删除上游 |

### 4.2 挂单

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/seller/upstreams/:id/listing` | 创建挂单：`mode`（consignment / buyback / pack）。寄售需 `multiplier`，可选 `join_default_pool`、`min_amount`；额度包需 `multiplier`、`face_value`（≥10）、`stock`（≥1），不进保障池；回收返回报价。可选 `title`、`description`、`categories`（须为卖家可选分类，至少一类） |
| GET | `/api/seller/listing-categories` | 卖家可勾选的市场分类（扁平，含路径名） |
| GET | `/api/seller/listings` | 挂单列表 |
| GET | `/api/seller/listings/:id` | 挂单详情，含价格历史 |
| GET | `/api/seller/upstreams/:id/quote` | 当前回收报价 |
| POST | `/api/seller/listings/:id/accept-quote`、`/reject-quote` | 接受 / 拒绝回收报价 |
| PUT | `/api/seller/listings/:id/price` | 寄售改价：`multiplier`（有冷却时间 `price_change_cooldown_minutes`） |
| PUT | `/api/seller/listings/:id/meta` | 更新展示：`title`、`description`、`categories`（寄售/额度包，未关闭） |
| PUT | `/api/seller/listings/:id/default-pool` | 加入 / 退出默认分组：`join`（仅寄售） |
| POST | `/api/seller/listings/:id/pause`、`/resume`、`/close` | 暂停、恢复、关闭 |

### 4.3 测试报告

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/upstreams/:id/reports` | 报告列表 |
| GET | `/api/seller/reports/:id` | 报告详情（脱敏，不含题目、答案与基线信息，见 [05](05-quality.md) 第 8 节） |
| GET | `/api/seller/upstreams/:id/quality-trend` | 每日智力相对分与异常次数：`days`（默认 30） |
| POST | `/api/seller/upstreams/:id/retest` | 手动复测（每日限次，厂商须开启检测；健康异常、最近检测失败/证据不足或已有生效扣价时可用，否则 409 `RETEST_NOT_NEEDED`）。检测异常流程，复测由卖家承担、不结算收益；详情 `manual_retest` 给出能否复测及原因 |
| POST | `/api/seller/reports/:id/appeal` | 对暂停结论或已生效且系数小于 1 的扣价申诉：`reason` |

### 4.4 收益与提现

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/earnings` | 待结算、可提现、提现中、今日、近 7 天、累计 |
| GET | `/api/seller/earnings/lines` | 按上游、小时、渠道的结算明细，可按 `upstream_id` 筛选 |
| POST | `/api/seller/withdrawals` | 申请提现：`amount`、`channel`、`account_info`（加密存储）。需实名；低于自动审核阈值且无风险时自动通过 |
| GET | `/api/seller/withdrawals` | 提现记录 |

### 4.5 用量

数据来自已入账的用量（约 2 分钟延迟），只包含用量与收益，不含买家身份、Key 与请求内容。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/usage` | 用量汇总：`upstream_id`（可选，默认全部上游）、`days`（1–90，默认 30）。返回合计与今日（请求数，其中买家 / 平台检测；输入、输出、缓存 token；官方价费用；收益），每日序列（补齐无用量的日子），按模型、按上游的汇总 |
| GET | `/api/seller/usage/records` | 逐次记录：时间、上游、来源（`pool` 默认分组 / `direct` 指定卖家 / `probe` 平台检测）、模型、token、首字与总耗时、官方价费用、收益。筛选：`upstream_id`、`channel`（`buyer` / `probe` / `pool` / `direct`）、`model`、`from`、`to`；分页 `limit`、`offset` |

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
| 系统设置 | `GET/PUT /api/admin/settings`：抽佣率、冻结天数、最低提现额、自动审核阈值、等级系数、报价有效期、改价冷却、退款等待、新卖家期、价目模型、质量检测超时（`probe_request_timeout_seconds`、`probe_batch_timeout_minutes`）等（部分更新） |
| 市场运营 | `GET/PUT /api/admin/market-discovery`：首页类目/横幅等运营配置；`GET/PUT /api/admin/market-recommend`：三层推荐配置（`factors` 因子目录 → `profiles` 推荐类型 → `tiles` 首页四格选型）；`GET/POST /api/admin/market-categories`、`PUT/DELETE /api/admin/market-categories/:id`：分类树 CRUD |

| 系统更新 | `GET /api/admin/system/version`：当前版本、提交、运行平台、可用状态和最近更新任务；`GET /api/admin/system/check-update[?force=true]`：最新正式 Release 与更新包是否兼容；`POST /api/admin/system/update`：提交 `{"version":"vX.Y.Z"}`，成功返回 202 和任务，管理员操作写入审计日志 |
| 厂商 | `GET /api/admin/vendors`、`PUT /api/admin/vendors/:platform`：是否开放接入、开放的接入方式（`auth_types`，只能从 `supported_auth_types` 中选）、寄售倍率区间、回收基础倍率表（按档位）、默认并发、限流参数 |
| 代理库存 | `GET/POST /api/admin/proxies`、`POST /api/admin/proxies/:id/sync`（同步到 sub2api）、`/test`、`/quality-check`、`/disable`、`/enable`；检测响应与卖家接口相同。列表包含平台代理和卖家代理，`seller_id` 区分来源，后台新增代理归平台 |
| 默认分组 | `GET/POST /api/admin/pools`、`PUT /api/admin/pools/:id`（售价 D、最低毛利、安全缓冲、启用）、`POST /api/admin/pools/:id/sync` |
| 检测策略 | `GET /api/admin/detection-policies`、`PUT /api/admin/detection-policies/:platform[?auth_type=]`：总开关 enabled / scheduled_enabled、flows 的两条独立流程、预算、default_grade、third_party；旧 suites / models / interval_minutes / full_triggers 保留返回但已废弃，见 [05](05-quality.md) 第 3、9 节 |
| 质检设置 | `GET/PUT /api/admin/inspection/settings`：mode、预算、并发、恢复、等级、调度、模型能力等，部分更新 |
| 检测项 | `GET /api/admin/inspection/checks` 返回 flow、pricing、default_pricing=false、warning/severe 系数与校准；`PUT /api/admin/inspection/checks/:key` 覆盖 enabled、pricing（true/false/null）、calibrated、calibration_note、coefficients、thresholds |
| 第三方检测 | `GET /api/admin/detection-providers`（可接入的检测站、配置、最近 1 小时提交次数）、`PUT /api/admin/detection-providers/:name`（`enabled`、`base_url`、`api_key`、`settings`、`max_runs_per_hour`、`timeout_minutes`）、`GET /api/admin/third-party-checks[?upstream_id=]` |
| 单上游检测覆盖 | `PUT /api/admin/upstreams/:id/detection-override`、`DELETE …` |
| 手动检测 | `POST /api/admin/upstreams/:id/probe`：请求体可选 `{"flows":["intelligence","identity"]}`，省略检测全部已启用流程；对关闭检测总开关的厂商也可执行 |
| 检测明细 | `GET /api/admin/upstreams/:id/probe-runs`（最近的检测记录，含 flows / identity_source / models，不含基线对照检测，`limit` 默认 20）、`GET /api/admin/probe-runs/:id`（逐题结果：题目、是否通过、耗时、token、回答片段前 500 字符，附同批次基线账号的结果） |
| 检测记录 | `GET /api/admin/probe-runs`（全部上游的检测记录，含 flows / identity_source / models 及各套件题数与通过数；筛选 `platform`、`upstream_id`、`model`、`trigger`（为空时不含基线对照，`reference` 只看基线对照）、`status`、`verdict`、`from` / `to`（本地日期 YYYY-MM-DD，含当天）、`limit` / `offset`）、`GET /api/admin/probe-item-stats`（按题统计，被测上游与基线账号分开统计，筛选同上）、`GET /api/admin/probe-items/:id/results`（一道题最近的答题结果，含基线对照，`limit` 默认 50） |
| 题库 | `GET/POST /api/admin/probe-items`、`PUT /api/admin/probe-items/:id`、`POST /api/admin/probe-items/:id/retire`；新建拒绝旧 iq / reasoning / long_context 套件 |
| 基线账号 | `GET/POST /api/admin/reference-accounts`、`DELETE /api/admin/reference-accounts/:id`（授权复用卖家的授权接口） |
| 质量看板 | `GET /api/admin/quality/overview`（各厂商健康与等级分布、官方全局事件、降智上游列表） |
| 申诉 | `GET /api/admin/appeals`、`POST /api/admin/appeals/:id/resolve`（`action`：`retest` / `overturn` / `uphold`）；人工 overturn 将 Q 恢复为 1、清空估价时间并等待新检测，保留历史报告 |
| 上游 | `GET /api/admin/upstreams`、`POST /api/admin/upstreams/:id/suspend`、`/unsuspend`、`DELETE /api/admin/upstreams/:id` |
| 用户与实名 | `GET /api/admin/kyc`、`POST /api/admin/kyc/:id/review`、`POST /api/admin/users/:id/status`（冻结 / 解冻） |
| 充值 | `GET /api/admin/recharges`、`POST /api/admin/recharges/:id/confirm`（只用于人工充值单）、`POST /api/admin/recharges/:id/sync`（向渠道查单）、`POST /api/admin/recharges/:id/refund`（代买家按充值单退款，参数与规则同买家接口） |
| 支付配置 | `GET /api/admin/payments/merchants`（支付宝、微信支付商户配置，密钥只返回是否已设置；`ready` 与 `problem` 说明是否可用）、`PUT /api/admin/payments/merchants/:provider`（`enabled`、`alipay` / `wechat` 配置、`private_key`、`api_v3_key`，密钥留空表示不变）、`POST /api/admin/payments/merchants/:provider/test`（测试连接）、`GET /api/admin/payments/status`（各支付方式是否可用，最近 50 条通知处理记录）；通知地址、支付方式开关、充值上下限、支付时限、原路退回在系统设置中（`payment_public_url`、`payment_channels`、`recharge_min`、`recharge_max`、`recharge_order_timeout_minutes`、`refund_original_route`、`refund_original_route_days`） |
| 退款 | `GET /api/admin/refunds`、`POST /api/admin/refunds/:id/paid`（登记人工打款部分的流水号）、`POST /api/admin/refund-payouts/:id/retry`（重试失败的原路退款）、`POST /api/admin/refund-payouts/:id/manual`（失败的原路退款改为人工打款） |
| 提现 | `GET /api/admin/withdrawals`、`POST /api/admin/withdrawals/:id/review`（`approve` 为真通过，否则驳回并退回可提现）、`POST /api/admin/withdrawals/:id/paid`（`failed` 为真表示打款失败） |
| 计费异常与调账 | `GET /api/admin/anomalies`、`POST /api/admin/anomalies/:id/resolve`、`POST /api/admin/adjustments`（必须填写原因，可关联异常） |
| 风控 | `GET /api/admin/risk-events`、`POST /api/admin/risk-events/:id/resolve` |
| 账本 | `GET /api/admin/ledger`（平台现金、收入、探测成本、调整、买家余额合计、卖家应付合计） |
| sub2api 状态 | `GET /api/admin/sub2api/status`（版本、迁移指纹、兼容自检状态、出站队列积压、入账游标） |
| 审计 | `GET /api/admin/audit-logs`（可按 `target` 筛选） |

检测策略的 flows 字段结构如下。每个流程分别保存开关、周期、触发类型与完整检测触发；身份来源第三方且开启时必须有已启用检测站。expected_pass_rate 为空或 `(0,1]`，为空沿用检测项阈值；third_party 内原 triggers 已废弃，执行以 identity.triggers 为准。

第三方 on_mismatch / on_submodel_mismatch 支持 degraded、suspected、ignore；ignore 折算为 normal 与身份 pass，不产生疑似或扣价。执行实例最多 `200 × max(1, 测智模型数)`，快照最多 32 MiB，超限运行明确失败，不执行删减计划。

```json
{
  "flows": {
    "intelligence": {
      "enabled": true, "scheduled_enabled": true, "interval_minutes": 360,
      "triggers": ["onboarding", "scheduled", "recovery", "seller_manual", "admin", "adaptive", "appeal"],
      "full_triggers": ["onboarding", "appeal", "admin"],
      "models": [{"model": "gpt-6-astra", "expected_pass_rate": null}]
    },
    "identity": {
      "enabled": true, "source": "third_party", "scheduled_enabled": false, "interval_minutes": 1440,
      "triggers": ["onboarding", "admin", "appeal"],
      "full_triggers": ["onboarding", "appeal", "admin", "scheduled", "recovery", "seller_manual", "adaptive"]
    }
  }
}
```

运行与报告 summary 新增 flows、identity_source、models，以及 intelligence / identity 两个区块（status、inherited_at；身份还有 source）。测智状态允许 pass / warning / fail / inconclusive / not_applicable / not_run，身份不含 not_applicable。管理员 intelligence.models[] 还包含 target_pass_rate、expected_pass_rate、samples；卖家只返回 model、status。卖家 checks 的 applied=true 项保留 factor。旧报告没有流程字段时继续用 summary.suites 展示，详见 [05 第 8 节](05-quality.md#8-卖家测试报告)。

测智历史只沿用当前模型集合的有效结果，模型期望或该检测项定义变化后须重测；其他项配置变化不使历史失效。申诉复测正常且无生效扣价时自动推翻，证据边界取本轮开始时间（未开始取入队时间），本轮通过结果继续用于估价；人工推翻以当前时间清旧证据，摘要等待新检测。

**与设计稿的差异**：价格类设置合并在"系统设置"与"厂商"中，没有单独的 `/pricing`；提现审核用一个 `review` 接口；调账不设审批流（记录审计日志与原因）；第一期没有赠送余额，钱包不拆分现金与赠送；Key 详情不显示熔断状态（熔断在各 Edge 实例内存中）。
