# 卖家冻结与结算约定

状态：后端实现与测试完成，后端所有权代理 settlement。冻结设置无需 schema 迁移。

## 配置 API

沿用 `GET /api/admin/settings` 与 `PUT /api/admin/settings`（部分更新）。新增正式字段：

```json
{"freeze_hours":24}
```

- `freeze_hours`：非负整数小时，默认 **24**；上限为 `settings.MaxFreezeHours`（防止 duration 溢出）。后台修改后下一轮解冻直接读取最新值，无需重启。
- `freeze_hours: 0`：无额外冻结期；当前结算架构按小时汇总，因此仍等待该小时结束。
- 保留 `freeze_days` 兼容旧前端和既有数据。旧存储或更新仅包含 `freeze_days` 时按 `days × 24` 转换；两者同时给出时以 `freeze_hours` 为准。
- 返回同时包含两字段；`freeze_days` 为 `ceil(freeze_hours / 24)`，新 UI 必须读写 `freeze_hours`，避免精度丢失。
- 现有保存的冻结天数继续生效；未保存任何冻结项的默认值从 7 天改为 24 小时。

## 解冻与资金语义

- 买家扣费、卖家待结算收入仍在用量入账事务内及时记账；此配置只影响待结算转为可提现。
- Worker 每 1 分钟运行一次解冻，并在每轮动态读取设置。
- 按 `小时桶结束时间 + freeze_hours <= 当前时间` 判断成熟，避免当小时最后一笔用量提前释放。此保守方案可能额外等待不足 1 小时。
- 同一事务锁住成熟结算行，原子更新 `unfrozen_payout` 并只转移本轮认领的增量；并发、重试不会重复释放，迟到用量只释放新增部分。
- `billing.Unfreeze(..., freezeDays, ...)` 保留兼容，worker 使用新的 `billing.UnfreezeHours(..., freezeHours, ...)`。

## 活动包兑换率配置

同一设置接口新增 `credit_usd_to_balance_rate`，默认 **1**，范围 `0.00000001..1000000`，最多 8 位小数，必须为有限值。与活动包 `numeric(16,8)` 汇率快照一致，防止 SQL 四舍五入为 0。语义仅为活动包 1 USD 面额对应的平台余额单位数；不改变既有 D16 全局余额口径。购买、分摊及快照接口由 billing 主工人实现，详见 billing-contract.md。

`internal/httpapi/handlers_user.go` 的公开市场配置已精准补充 `freeze_hours` 与 `credit_usd_to_balance_rate`；后台设置响应使用 settings 结构，自动带新字段。

## 验证

- `go test ./internal/settings ./internal/billing ./cmd/worker` 通过。
- PostgreSQL 真实事务集成测试：`TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/billing -run '^TestUnfreeze' -count=1 -v` 通过（4 个顶层测试）。
- 覆盖 0/1/24/25 小时成熟边界，动态修改配置，旧天数接口兼容，同一结算行并发争领，迟到用量新增收益，重复运行幂等，以及账本失败时结算认领整体回滚。
- 设置单元测试覆盖默认与老值读取、hours/days 优先级、显式 0、保存后读取、非法输入与溢出、兑换率精度/范围/有限值。

## 现有限制

小时汇总架构下，为避免提前解冻，实际释放可能比该笔用量的严格冻结时长晚不足 1 小时，再加最多约 1 分钟 worker 调度间隔；这是当前批准的保守策略。未修改 postUsage/attribute/RunOnce、未新增迁移、未提交 commit。

## 追加：用量并发入账验证

新增测试文件 `internal/billing/ingest_concurrency_integration_test.go`，未修改入账实现。

- `TestPostUsageConcurrentTransactionsClaimOnce`：两个真实事务同时等待并争领相同普通用量，只一方返回 1，另一方返回 0。验证一笔 usage 交易、3 条借贷分录、结算 request_count=1、买家现金 10→8、卖家待结算 +1、平台收入 +1，以及所有交易逐笔借贷平衡。
- `TestPostUsageRollbackRetryAndReplay`：完成入账后主动回滚，验证用量认领、账本和结算汇总全部恢复；随后重试返回 1，连续两次重放均返回 0，余额和结算不再变化。
- 验证通过：`TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55432/tm?sslmode=disable' go test -tags=integration ./internal/billing -run '^TestPostUsage' -count=1 -v`。

### 重叠读取停滞（主代理已修复，回归通过）

现有配置 `ingestOverlap=2000`、`ingestBatch=2000`，查询 `id > cursor-overlap ORDER BY id LIMIT batch`。连续 ID 首次取 1..2000 后 cursor=2000；下一轮 from=0，再取相同 1..2000，Inserted/Posted=0 且 more=true，因而持续热循环且游标无法前进。advisory lock 与 `GREATEST(last_id,...)` 不解决该问题。

最小修复：读取 limit 改为 `ingestOverlap+ingestBatch`，more 同步比较该读取 limit。由于重叠区间最多含 2000 个整型 ID，4000 条窗口保证保留前向读取空间。更明确的架构是分别查询前向批次和有界回补区间，more 只由前向批次是否满决定。应补至少 4001 条连续 ID 的 RunOnce 回归测试，验证最终全部入账、cursor 到末尾且返回 more=false。此评估未自行修改共享实现。

主代理已采用读取 limit=overlap+batch 的修复。新增 `TestIngesterContinuousIDsAdvanceBeyondOverlapWindow` 使用独立最小 replica 表生成 4001 条连续 ID，经过真实 RunOnce 导入/归属/复式入账，验证全部 4001 条只入账一次、cursor=4001、最终 more=false、再次重放 Inserted/Posted=0，结算 request_count 与卖家收入均为 4001、探测费用与收益平衡。测试真实 PostgreSQL 通过（约 14 秒）。

活动包完整资金、退款、作用域、并发与报告测试结果见 `billing-validation.md`。
