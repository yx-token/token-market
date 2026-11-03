# 收款配置改用 sub2api 支付配置（2026-11-03）

## 结论（用户已拍板）

1. **只借用商户凭证**：C 端钱包充值的下单、异步通知、查单、关单、原路退款**仍由市场自己完成**
   （`market/internal/payment/{alipay,wechat}` 协议实现 + `market/internal/billing` 订单状态机保持不变）。
   变化只在于：商户凭证、充值限额、支付时限不再存在市场里，改为读取 sub2api「设置 → 支付设置」里的
   **支付宝官方（provider_key=alipay）/ 微信官方（provider_key=wxpay）** 服务商实例与全局支付配置。
   易支付、Stripe、Airwallex 实例不用于 C 端（市场没有这些协议实现）。
2. **移除人工转账**：C 端不再能创建 `manual` 充值单。历史上待确认的人工单，管理端「充值」页仍可「确认到账」。
3. **移除闲置市场下的收款配置**：管理端 `/admin/market/payments`（`frontend/src/market-admin/PaymentsView.vue`）整页删除，
   相关接口 `GET payments/status`、`GET payments/merchants`、`PUT payments/merchants/:provider`、
   `POST payments/merchants/:provider/test` 在市场与引擎 relay 两侧都删除。

## 数据来源：`market/hostpay`

契约包 `market/hostpay/hostpay.go`（已写好，叶子包）：

- 宿主（`backend/`）启动时 `hostpay.SetSource(fn)` 注册；市场通过 `hostpay.Load(ctx)` 取快照。
- 未注册（市场独立进程 / 替身引擎）→ `ErrNoSource` → 市场视为没有在线支付，C 端充值按钮显示「暂无可用的支付方式」。
- `Config.Enabled` = sub2api `PaymentConfig.Enabled && !BalanceDisabled`。
- `MinAmount/MaxAmount/OrderTimeoutMinutes` 取 sub2api `PaymentConfig.MinAmount/MaxAmount/OrderTimeoutMin`；<=0 时市场用默认值（1 / 50000 / 30），时限再夹到 [5, 1440]。
- `Instances` 含**已停用**的 alipay/wxpay 实例，`Config` 为解密后的明文（复用 sub2api `PaymentConfigService` 的解密逻辑，兼容旧密文）。

## 市场侧改动（market/）

### 渠道注册表

- 删除 `market/internal/payment/merchants`（站内商户表读写、加密、测试按钮）。
  新建一个基于 `hostpay` 的实现，满足 `billing.PaymentRegistry` 接口，秒级缓存，按 `Instance.ID + UpdatedAt` 指纹决定是否重建客户端
  （微信平台证书等缓存得以保留的语义不变）。
- 注册表从「按渠道名（alipay/wechat）各一个」改为「**按实例 ID**」：
  - 新订单使用该渠道下 **启用中** 的实例里 `SortOrder` 最小（再按 ID）的那个；
  - 订单记住所用实例：`recharge_orders` 新增可空列 `provider_instance_id bigint`（新迁移）；
  - 回调、查单、关单、退款按订单上的实例 ID 找客户端；**实例 ID 为空的历史订单**回退到该渠道当前的默认实例
    （运营把同一个支付宝应用迁进 sub2api 后，历史订单仍能对上）；
  - 已停用的实例不接新单，但其已有订单照常处理（与原 `Registry.Disable` 语义一致）。
- 字段映射：
  - 支付宝 → `alipay.Config{AppID: appId, PrivateKey: privateKey, AlipayPublicKey: publicKey||alipayPublicKey, 公钥模式}`。
    sub2api 只有公钥模式、只有正式网关。
  - 微信 → `wechat.Config{AppID: appId, MchID: mchId, MchSerialNo: certSerial, PublicKeyID: publicKeyId, PublicKey: publicKey, PrivateKey: privateKey, APIv3Key: apiV3Key}`。
  - 本地 / 测试需要把网关指向 fake-thirdparty：**用市场进程的配置项（环境变量）覆盖**支付宝网关与微信 API 地址，
    不从实例 config 读——避免管理员在线上 UI 里误配网关。线上不设置这两个配置项。
  - 渠道：支付宝实例提供 `alipay_page`，`payment_mode`（或 config `paymentMode`）不是 `redirect` 时再提供 `alipay_f2f`；
    微信实例提供 `wechat_native`。`supported_types` 非空且不含对应方式（alipay / wxpay）的实例不用于市场，规则与
    sub2api `enabledVisibleMethodsForProvider` 一致。
  - 配置缺字段、密钥解析失败的实例：不进注册表，记录原因（日志即可，后台不再有展示页）。
- 回调地址：`<origin>/api/payments/{alipay|wechat}/{instanceID}/notify`。
  `origin` 取该实例 config `notifyUrl` 的 scheme+host（sub2api 自己的回调就靠它，可达性已被验证）。
  `notifyUrl` 为空或无法解析 → 该实例不可用。
  旧路由 `POST /api/payments/{provider}/notify` 保留，走该渠道默认实例，兜底历史在途订单。
- 电脑网站支付的跳回地址：优先用买家发起充值请求的 origin（按 `TrustedProxies` 规则取 `X-Forwarded-*`），
  拿不到时用上面的 notify origin；路径仍是 `/buyer/wallet?order=<id>`。

### 设置与人工单

- `settings.Settings` 删除 `PaymentPublicURL`、`PaymentChannels`、`RechargeMin`、`RechargeMax`、`RechargeOrderTimeoutMinutes`
  及其默认值、校验、归一化。库里已存的旧 JSON 字段要能被忽略（确认反序列化不报错）。
- 限额 / 时限改从 `hostpay` 取（见上）。`/api/market/config` 返回的 `payment_channels`、`recharge_min`、`recharge_max`
  **字段名与类型不变**，前端无需改契约；`payment_channels` 不再出现 `manual`。
- `CreateRecharge`：`channel` 必填，不再默认 `manual`；`manual` 一律返回 `CHANNEL_UNAVAILABLE`。
  `ConfirmRecharge`（人工单确认到账）保留。
- `RechargeChannels`：`Config.Enabled` 为 false 或 `ErrNoSource` 时返回空列表。
- `payment_notifications` 表继续记录（排障用），只是不再有后台展示接口。
- **不删表**：`payment_merchants`、`payment_notifications` 本次都不 DROP（不可逆，留待确认线上无历史依赖后另行处理）。

### 其他

- `market/scripts/dev.sh`：原来 `PUT /api/admin/payments/merchants/*` 写入 fake-thirdparty 商户，改为在**真实引擎模式**下
  通过引擎 Admin API 创建 sub2api 支付宝官方 / 微信官方实例（`notifyUrl` 指向本地 8080），打开 sub2api 支付总开关，
  并给宿主进程设置上面的网关覆盖配置指向 fake-thirdparty。替身引擎模式下不配置在线支付。注意 dev.sh 有用户未提交的 `DEV_ENGINE_PORT` 改动，必须保留。
- `market/TEST-LOCAL.md` 等文档里「支付配置」的点检步骤同步更新。

## 宿主侧改动（backend/）

- 实现 `hostpay.Source`：读 `PaymentConfigService.GetPaymentConfig` 与服务商实例（只取 alipay/wxpay，含停用），复用现有解密。
  在 `backend/cmd/server` 启动时、`enginehost` 构建市场 app 之前注册。
- `backend/internal/server/routes/market.go` 的 relay 清单删掉 `payments/*` 四项，相应测试同步。
- 加单测：配置映射（Enabled 组合、含停用实例、过滤其它 provider、解密失败的处理）。

## 前端改动

- `frontend/`：删除 `PaymentsView.vue` 及其测试、路由 `payments`、侧栏菜单项（`AppSidebar.vue`）、i18n `nav.marketPayments`。
  在 sub2api 支付设置页（服务商实例相关位置）加一句提示：「支付宝官方、微信官方实例同时用于闲置市场 C 端钱包充值」。
- `market/web/`（C 端）：`PaymentDialog.vue` 去掉人工转账的选项文案；历史 `manual` 待支付单的展示可保留。
  注意 `market/web/src/views/admin/` 是不可达旧文件（见根 `AGENTS.md` §1），**不要改**。

## 验收

- `cd backend && go build ./... && go test ./internal/server/routes/... ./internal/service/...`（相关包）
- `cd market && go build ./... && go test ./...`（需要数据库的集成测试按仓库惯例跑）
- `cd frontend && pnpm vitest run src/market-admin` 与类型检查；`cd market/web && pnpm vitest run` 与类型检查
- 真实环境：本地真实引擎 + fake-thirdparty，在 sub2api 支付设置里配支付宝官方实例 → C 端钱包能看到支付宝、能下单、
  fake 回调后到账；管理端侧栏不再有「支付」。

## 审查后追加的改动（2026-11-03）

独立审查后补了三处，均已实测：

- **在途订单保护**：sub2api 原有「实例有在途订单时禁止删除、停用、修改受保护凭证、去掉支付方式」的保护只统计自己的订单。
  现在 `hostpay.SetInFlightCounter` 由市场注册（`market/enginehost/host.go`），统计绑定该实例的待支付单、
  支付时限后 10 分钟内仍会查单的已取消单、未结束的原路退款打款；sub2api 在上述操作前合并检查（只对 alipay / wxpay 实例），
  市场计数出错时保守拒绝。错误码仍是 `PENDING_ORDERS`，metadata 分别给出 `sub2api_count` / `market_count`。
  剩余风险：计数与实例变更之间不是跨库原子操作，存在很小的并发窗口。
- **订单绑定的实例找不到时**：查单、关单、退款打款打 Warn 日志（order_id、instance_id），不回退到其它实例。
- **跳回地址**：浏览器 `Origin` 头的 host 与本次请求的有效 host 一致时，采用 Origin 的 scheme——
  线上在代理处终止 TLS、又没配 `TM_HTTP_TRUSTED_PROXIES` 时，也不会生成 `http://` 跳回地址。
- **dev-proxy**：`/api/admin/*` 会被注入引擎 Admin Key，现拒绝跨站请求（`Sec-Fetch-Site` / `Origin` 检查），只用于本地。

## 本地开发环境的变化

真实引擎模式（`USE_REAL_ENGINE=1`）下，`dev.sh` 不再单独起市场 `api/worker/edge` 进程，而是 `MARKET_EMBED=1` 嵌进
sub2api 进程（与线上一致，否则 hostpay 没有来源、钱包没有在线支付），8080 由 `bin/dev-proxy` 统一转发：
`/api/admin/*` 改写到 `/api/v1/admin/market/*` 并换成引擎 Admin Key。嵌入模式不会创建市场引导管理员，
seed 里的管理员调用都走 dev-proxy。管理端用引擎账号登录（`admin@dev.local / admin-password-1`），登录态存在
`localStorage` 的 `engine_*` 键下。
