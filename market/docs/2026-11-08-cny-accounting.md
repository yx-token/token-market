# 全系统改为人民币结算（2026-11-08）

整个系统（sub2api 引擎 + 闲猫市场 + 两套前端）的记账单位统一为人民币（CNY）。
引擎侧的字段清单、迁移细节和 API 契约见 `backend/dev-docs/cny-accounting.md`。

## 决定

1. **汇率固定 1 USD = 7 CNY**，是代码常量（`backend/internal/pkg/currency.USDToCNY`），不可配置。
2. **按真实汇率计费**：第三方 / 官方美元价 ×7 成人民币后计费，各种倍率一律不动（分组、账号、挂单、保障池、充值到账倍率）。同一次调用扣的人民币是改造前的 7 倍，这是预期。
3. **用户资产与用户侧额度：数值不变，只把单位改成 CNY / ¥**（不 ×7）。

## 金额分类

| 类 | 内容 | 处理 |
| --- | --- | --- |
| A：价格及按官方成本计量的量 | LiteLLM 远程价格表、fallback / override 文件、代码里的兜底价；管理员录入的渠道 / 分组定价；上游账号成本额度（`window_cost_limit`、`window_cost_sticky_reserve`、`quota_limit`、`quota_daily_limit`、`quota_weekly_limit` 及已用计数、固定金额通知阈值）；检测策略每日预算 | ×7。LiteLLM 原始文件和哈希保持美元，只在解析进内存时乘一次；库里的值由迁移乘 |
| B：用户资产与额度 | 引擎余额、兑换码、订阅用量额度 / 限额、用户及 API Key 限额、历史 usage 成本；市场钱包；买家 Key 限额；店铺活动包 | 数值不变，改标 CNY。活动包面值 = 原美元面值 × 购买时兑换率（精确乘积），之后兑换率恒为 1 |
| C：订阅套餐售价 | `subscription_plans.price / original_price` | 删除 `SUBSCRIPTION_USD_TO_CNY_RATE`；若原设置 > 0，迁移时 `round(price × rate, 2)`，用户实付不变 |
| D：不变 | 各种倍率；支付实付金额与网关币种（`provider_snapshot.currency`）；上游原生余额（如 Grok 美元预付） | 原样保留，保证验签、查单、原路退款 |

分账、退款、payout 仍按 `payment.Yuan` 和分的精度计算。

## 跨模块约束

- 引擎保留所有 `*_usd` 列名和 JSON 字段名，含义已是 CNY；白名单见引擎文档。前端照常读写，**不要再乘 7**。
- 市场的官方价目取自引擎 pricing catalog，引擎已换算，市场不再换算。
- 市场接口字段改名（旧名会被拒绝）：`face_value_usd → face_value_cny`、`remaining_usd → remaining_cny`、`credit_covered_usd → credit_covered_cny`、Key 限额 `quota_usd / rate_limit_*_usd → *_cny`（输入仍兼容旧键）、`/v1/usage` 的 `store_credit_remaining_usd → store_credit_remaining_cny`、检测策略 `daily_budget_usd → daily_budget_cny`；删除 `credit_usd_to_balance_rate`。
- **引擎迁移 242 与市场迁移 0054 必须同版本上线**，否则市场调度器会把旧的美元成本额度写回引擎账号。
- 引擎新增迁移改变了 `market/contract/contract.yaml` 的 `host_migration_fingerprints`；以后引擎再加或改名迁移，要重算该指纹（`contract_test` 会报出新值），否则市场进入安全模式。

## 上线影响

- 换算后面值小数超过 8 位或超出范围的在售活动包商品会被迁移暂停（它们换算前本来就买不了），卖家按人民币重填面值后再上架。
- `market/web/src/views/admin/` 旧控制台（线上不可达）仍残留 `credit_usd_to_balance_rate` 等旧字段，属已知例外。

## 验收（2026-11-08，本机真实引擎）

- 引擎实际加载的价格表 689 个价格字段与原始美元价之比全部为 7；C 端 `/api/market/prices` 与 catalog 一致，无重复换算。
- 固定用量 1000 / 500 token 调用 claude-haiku-4-5：`total_cost` = 0.0245（$1 / $5 每百万 × 7），`actual_cost` = 0.0147（× 分组倍率 0.6）；市场用量、卖家 0.01225 + 平台 0.00245 分账一致。
- 充值 ¥100 到账 ¥100；活动包「¥10 买 ¥20 额度」购买、抵扣、本金分摊正确；`/v1/usage` 单位 CNY。
- 买家侧退款被产品政策关闭（`BUYER_REFUNDS_DISABLED`），原路退款由 `market/internal/e2e` 的 `TestOnlinePaymentsAndOriginalRouteRefund`、`TestRechargeOrderRefund` 等集成测试覆盖。
