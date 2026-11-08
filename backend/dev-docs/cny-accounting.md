# 引擎人民币记账口径（2026-11-08）

引擎记账单位统一为 CNY。固定汇率定义在 `internal/pkg/currency/currency.go`：
`USDToCNY = 7`。本次不重命名任何数据库列或金额 JSON 字段。

## 金额分类和处理

| 类别 | 字段/来源 | 处理 |
| --- | --- | --- |
| A | LiteLLM remote/cache/fallback/override 的 input/output、priority、cache creation 5m/1h、cache read、image input/output/cache token 和 per-image 价格 | 原始文件及 SHA256 保持 USD；解析每个新内存条目后乘常量一次。长上下文阈值/倍率先从 USD 源数据推导。 |
| A | PricingService 的 openAI*/claude*FallbackPricing、BillingService token fallback（含 Bedrock 走 Claude 系列价）、DeepSeek 低谷价 | 初始化/常量定义时引用同一汇率。运行时优先级/峰谷/长上下文倍率不变。 |
| A | 广场 plaza_channel_catalog.json 扩展价格 | 原文件 USD，读取新结构时乘常量。catalog/GetModelPricing/official_pricing 返回 CNY，市场不得再次乘 7。 |
| A | channel_model_pricing、channel_pricing_intervals、channel_account_stats_model_pricing、channel_account_stats_pricing_intervals 的 input_price/output_price/cache_write_price/cache_write_1h_price/cache_read_price/image_input_price/image_output_price/per_request_price（各表实际存在列） | 242 迁移精确乘 7；管理端今后直接录入 CNY。 |
| A | groups.image_price_1k/2k/4k、video_price_480p/720p/1080p、web_search_price_per_call、search_price_per_1k、audio_realtime_price_per_min、audio_tts_price_per_million_chars、audio_stt_price_per_hour | 242 迁移乘 7。Sora 列已在 090 删除，不再存在。 |
| A | groups.model_pricing JSON 中上述绝对价格叶子、groups.video_model_prices 的 family/resolution 价格 | 242 递归乘 7，仅改价格，所有 multiplier、tokens、排序不动。 |
| A | accounts.extra.quota_limit/quota_used/quota_daily_limit/quota_daily_used/quota_weekly_limit/quota_weekly_used/window_cost_limit/window_cost_sticky_reserve | 账号 quota 由 TotalCost × AccountRateMultiplier 累加，窗口使用 StandardCost；限额与现存计数同时乘 7；默认 sticky reserve 10 USD 变 70 CNY。scheduler_outbox 发 account_changed 刷新持久缓存。 |
| A | accounts.extra.quota_notify_daily/weekly/total_threshold | fixed 阈值乘 7；percentage 阈值不变。 |
| B | users.balance/frozen_balance、redeem_codes.value、promo_codes/promo_code_usages.bonus_amount、payment_orders.amount/bonus_amount、余额/平台限额配置 | 数值不变，语义 CNY。充值倍率与优惠比例不变。 |
| B | 默认注册赠送余额、用户固定余额提醒阈值、累计充值额、计费在途预留金额上限和用户在途预扣额 | 数值不变，语义 CNY；按百分比的提醒阈值属于 D 类。 |
| B | groups.daily/weekly/monthly_limit_usd、user_subscriptions.*_usage_usd、user_platform_quotas.*_limit_usd/*_usage_usd、api_keys.quota/quota_used/rate_limit_5h/1d/7d/usage_5h/1d/7d | 数值不变，语义 CNY；保留旧列和 JSON 名。 |
| B | usage_logs 各 cost 字段、历史账单/仪表盘/rollup、批量图片任务估算/冻结/已结算金额和价格快照 | 数值不变，标记 CNY；在途批量任务按提交快照结算，不追溯加价。 |
| C | subscription_plans.price/original_price | 旧 SUBSCRIPTION_USD_TO_CNY_RATE > 0 时 round(price × rate, 2)；0/不存在/非法值不变。删除该设置。套餐 currency 包括空串统一 CNY。 |
| D | rate_multiplier、account/image/video/peak/fast/flex/ultrafast/reasoning/time/batch/hold multiplier、百分比与 token 数 | 完全不变；custom_channel_time_pricing 只含倍率，无价格。 |
| D | payment_orders.pay_amount/refund_amount、provider_snapshot.currency、支付实例凭证与币种、payment.Yuan、各平台探测得到的原生余额/额度 | 原币原数值保留，保证已收款订单验签、查单、关单、原路退款与精度。上游诊断余额是上游原币，不能伪装成平台 CNY 资产。 |

## 迁移与兼容

新增 `migrations/242_cny_accounting.sql`，由迁移 runner 事务+锁+checksum 执行一次。
禁止手工反复执行数据换算 SQL；runner 再启动会跳过已应用文件。渠道与分组倍率均不改变。
认证快照版本升为 25，淘汰旧分组美元价格。账号额度 JSON 的数字及合法数字字符串均换算。
市场源配置和引擎必须一起升级，不能让旧调度器写回美元 quota/window 配置。

B 类历史 usage 成本保持数值，因此跨切换时点的 5h 窗口按新记账语义累加旧记录原值，
不会重写 usage_logs；后续请求按人民币价格进入窗口。这是保留历史金额的既定约束。

套餐直接 price 人民币收款；新增订阅订单仅接受 CNY 支付方式，旧订单不经套餐币种校验，
仍按 provider_snapshot.currency 完成支付/退款。不存在 payment_orders.currency 数据列。

## API 契约

金额 JSON 字段改名：**无**。所有平台记账金额使用 CNY，包含名字仍以 `_usd` 结尾的兼容字段。

| 接口 | 保留字段（现在全部是 CNY） |
| --- | --- |
| /api/v1/admin/groups（GET/POST/PUT）、分组 DTO、套餐 group 信息 | daily_limit_usd, weekly_limit_usd, monthly_limit_usd |
| /api/v1/subscriptions（列表/active/summary）、/api/v1/admin/subscriptions、分组/用户订阅列表 | daily_usage_usd, weekly_usage_usd, monthly_usage_usd；嵌套 group 的 daily_limit_usd, weekly_limit_usd, monthly_limit_usd |
| /api/v1/subscriptions/summary | daily_used_usd, weekly_used_usd, monthly_used_usd, daily_limit_usd, weekly_limit_usd, monthly_limit_usd, total_used_usd |
| /api/v1/subscriptions/progress、/api/v1/admin/subscriptions/:id/progress | daily/weekly/monthly.limit_usd, used_usd, remaining_usd |
| /api/v1/admin/users/:id/platform-quotas（GET/PUT）、/api/v1/user/platform-quotas（GET） | daily_limit_usd, weekly_limit_usd, monthly_limit_usd, daily_usage_usd, weekly_usage_usd, monthly_usage_usd |
| /api/v1/payment/checkout-info、/api/v1/admin/payment/plans | 套餐 daily_limit_usd, weekly_limit_usd, monthly_limit_usd |
| /v1/usage 等网关用量响应 | subscription 内 daily/weekly/monthly_usage_usd 和 daily/weekly/monthly_limit_usd；unit 从 USD 改 CNY |

完整 `_usd` 数据列白名单（均 B 类，数值不变）：

| 表 | 列 |
| --- | --- |
| groups | daily_limit_usd, weekly_limit_usd, monthly_limit_usd |
| user_subscriptions | daily_usage_usd, weekly_usage_usd, monthly_usage_usd |
| user_platform_quotas | daily_limit_usd, weekly_limit_usd, monthly_limit_usd, daily_usage_usd, weekly_usage_usd, monthly_usage_usd |

完整 `_usd` JSON 字段名集合：`daily_limit_usd`, `weekly_limit_usd`, `monthly_limit_usd`,
`daily_usage_usd`, `weekly_usage_usd`, `monthly_usage_usd`, `daily_used_usd`, `weekly_used_usd`,
`monthly_used_usd`, `total_used_usd`, `limit_usd`, `used_usd`, `remaining_usd`。
管理审计记录的 JSON 元数据另保留 `before_daily_limit_usd`, `before_weekly_limit_usd`,
`before_monthly_limit_usd`，同为 CNY；它们出现在用户平台额度更新/重置的审计详情，不是新的用户侧额度字段。
认证缓存中的同名字段也保留。配置键 `billing.inflight_reservation.max_reservation_usd`
及内部 MaxReservationUSD 标识保留，其单位现在是 CNY，数值不变。

已删除而非改名的 API 字段：

| 接口 | 已删除字段 |
| --- | --- |
| /api/v1/admin/settings（GET/PUT） | payment_subscription_usd_to_cny_rate |
| /api/v1/admin/payment/config（GET/PUT）、/api/v1/payment/config、/api/v1/payment/checkout-info | subscription_usd_to_cny_rate |

`/api/v1/admin/channels/pricing/catalog`、`/api/v1/model-plaza` 的 `official_pricing`
和定价字段统一 CNY。市场 recharges/refunds/withdrawals 的读取与写入共用 relay，
避免 native 简表产生另一套金额字段/序列化形式。其他 native 数据只保留原 CNY 数值，倍率不变。

## 前端配套

- 删除订阅 USD→CNY 汇率输入、类型字段与客户端换算；套餐价格固定 CNY，币种标签输入收敛为 CNY。
- 上述 `_usd` 字段继续读写同名，但文案统一人民币；不要再对 catalog/official_pricing 乘 7。
- 分组/渠道/账号成本限额以 CNY 录入，搜索默认价格现在 0.07 CNY/次。
- 订单 amount/bonus_amount 展示 CNY 账户额度；pay_amount 和退款实付仍按订单返回的真实 currency 展示。

## 验证

- `go test ./...`：最终通过，service 139.729s，repository 10.674s，routes 和全部其它包通过（首次全量曾遇到 market 并行修改中的已移除字段，market 完成后复跑通过）。
- `go test ./internal/service ./internal/handler/... ./internal/server/routes ./internal/repository ./migrations`：通过。
- `go test -tags integration ./internal/repository -run '^TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate$' -count=1`：通过。
- `go test -tags integration ./internal/repository -run '^Test(CNYMigrationConvertsPricesAndPreservesAssets|MigrationsRunner_IsIdempotent_AndSchemaIsUpToDate)$' -count=1`：通过，rate 7.15/0/invalid/.5/7./1e1/-1/缺失，验证价格、数字字符串额度、资产、倍率、支付快照与旧价任务。
- `go test -tags unit ./internal/service ./internal/handler/... ./internal/server/routes ./internal/repository ./migrations`：通过，service 227.510s；handler、admin、dto、quotaview、routes、repository、migrations 全部通过。

早期失败和修正记录：普通全量首轮因 market 并行删除 `CreditUSDToBalanceRate` 引用而构建失败，market 修复后全绿；
unit 首轮的旧官方价/币种文案断言失败，已按来源更新，不缩放渠道/分组自定义人民币价格或倍率。
后续定向仅剩的三个旧断言也已修正，完整 unit 复跑全绿。日志保存在 `/tmp/engine-cny-*.log`，关键原始输出示例：

```text
--- FAIL: TestNewModelPricingChannelOverridesAndFamilyIsolation
expected: 5e-06
actual  : 3.5e-05
--- FAIL: TestCalculateOpenAIRecordUsageCost_SearchIsAdditiveToTokens
Max difference between 1.0105 and 1.0735000000000001 allowed is 1e-09
```

这些是已解决的旧 USD 预期，不是未修复的失败；搜索混合用例现在精确断言 `1 CNY + 0.0105 USD * 7`。

最终日志：`/tmp/engine-cny-all-tests-final.log`、`/tmp/engine-cny-unit-final.log`、
`/tmp/engine-cny-migration-final.log`。`git diff --check -- backend` 通过。
本次临时 PostgreSQL 容器已停止并自动清理；没有修改实际部署数据库，没有进行浏览器验收，没有提交 Git。
