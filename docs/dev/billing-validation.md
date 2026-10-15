# 结算与店铺活动包后端验证

## 最后收尾修复的验证边界

主代理报告全量 race integration 通过后，最后审计发现上游余额8位与本地10位、默认赠金两个问题，代码现已最小修复。资金fixture已从numeric(20,10)改为上游真实numeric(20,8)。新增测试 `TestStoreCreditsRefundReclaimsRemotePrecision` 覆盖三次细粒度消费累计舍入、旧路径/量化路径、真正不明差异继续阻断，以及真实numeric8非负条件下按outbox撤销余额；`TestStoreCreditsRejectUnrepresentableFunding` 覆盖低于上游最小精度和非8位精确换算，断言明确业务错误与未扣款。运行时另新增非零默认赠金初始化专项。

按主代理要求，最后修复之后**未执行集成测试**，新增专项仅编译通过，不计入下方既有已通过专项证据。普通相关包测试通过。后续主代理可统一运行上述专项，避免与正在进行的e2e共享环境并发。

状态：settlement 测试代理新增真实 PostgreSQL 集成覆盖；不修改活动包实现，不依赖 Redis 或远程 API。

## 活动包资金与报告

文件：`internal/billing/store_credits_integration_test.go`。

测试 fixture：独立市场数据库完整迁移、独立最小 sub2api 只读数据库（仅余额 users 表）；买家账面与实时余额初始 100。通过真实 `Funds.PurchaseCreditPack` 购买售价 30、美元面额 10、汇率 7、限制本店/openai/gpt-6-astra 的包。断言购买后现金 70、活动预付负债 30，且出站扣款仅 30；手动模拟购买出站完成和活动钱包开通。

- `TestStoreCreditsConsumeAndCashRemainder`：第一笔原始费用 3.5 / 卖家 2.8，抵 0.5 USD，释放预付款 1.5，卖家收益 1.2；第二笔费用 68 / 卖家 54.4，抵剩余 66.5 单位，释放 28.5 预付款，平台现金补差 1.5，卖家收益 24。最终包准确耗尽。重复入账不重复扣款，现金补扣与活动钱包恢复出站各恰好 1.5。逐笔复式账本平衡。
- 同测试覆盖 `BuyerUsage`、`BuyerBills`、`RequestDetail` 两种请求 ID：cost=实际现金+已付预款，分拆字段 `cash_cost / credit_prepaid_cny / credit_covered_usd / credit_covered_units / credit_pack_id` 正确。平台汇总包含活动预付负债，未使用优惠不计作已消费或全平台充值。
- `TestStoreCreditsScopeCannotCoverOtherSellerPlatformOrModel`：分别变更真实卖家、厂商、模型；三个场景均零抵扣、活动剩余和预付款完全不变，原费用全转平台现金，活动钱包占用金额全数恢复。
- `TestStoreCreditsRefundOnlyUnusedPaidAmountAndRetry`：实时钱包与账面未追平时禁止推进；追平后先收回隔离钱包剩余额度，再仅退未用实际付款 28.5。多次退款请求及各阶段 worker 重试不重复出站、不重复记账，最终现金 98.5。
- `TestStoreCreditsLedgerFailureRollsBackAllocation`：人为触发账本插入失败，确认活动余额、分摊、usage 认领、卖家收入和结算汇总整体回滚；移除故障后重试一次成功。
- `TestStoreCreditsConcurrentConsumptionNeverExceedsRemainder`：两事务争同一活动包，两笔各 50 的原费用总共最多抵 70 单位、10 USD、30 实付预款；现金只补 30，包余额不为负。最终现金 40、卖家收入 48、平台收入 12，保持逐笔借贷平衡。

验证命令（通过）：

```sh
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/billing -run '^TestStoreCredits' -count=1 -v
go vet -tags=integration ./internal/billing
```

覆盖边界：此文件验证 Funds、用量分摊、报告和数据库/outbox 资金一致性。购买鉴权/幂等/快照与真实 Edge 准入、远端出站发送由主代理的 API/E2E 验证负责。

## 普通用量与冻结

`ingest_concurrency_integration_test.go` 覆盖两个事务同批用量仅一次记账，以及主动回滚后重试、重复调用返回 0。`unfreeze_integration_test.go` 覆盖按完整小时成熟、动态冻结配置、旧 days 调用、并发单次释放、迟到增量与账本失败回滚。详细约定见 `settlement-contract.md`。
