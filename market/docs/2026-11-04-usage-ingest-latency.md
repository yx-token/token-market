# 用量明细秒级入账修复与验收

核实日期：2026-11-04。

## 根因

嵌入模式合并了进程，但仍保留两套表：引擎写 `usage_logs`，市场 worker 导入
`usage_records`，同事务更新买家账本、卖家 pending、额度包与结算汇总。
买家明细、账单和卖家统计读取市场表。

原导入器只读 `created_at < now() - 2 minutes` 的日志，默认每 15 秒执行一次。
因此正常请求完成后也会等待约 120–135 秒。后端日志批处理窗口是普通写入 3ms、
best-effort 写入 20ms，不是分钟级延迟来源。引擎还有异步计费任务队列，拥塞时仍可能排队。

## 实现

- 移除按 `created_at` 延迟读取，直接读取 PostgreSQL 已提交、当前可见的日志。
- 默认入账间隔改为 1 秒；前向批次满 2000 条时连续追赶积压。
- 每轮重读前向游标前 2000 个 ID，下一轮即可补普通乱序提交。
- 另用 `usage_logs_replay`、`usage_logs_replay_end` 两个持久游标，循环分批回扫历史。
  每圈冻结上界，保证持续写入不会让回扫永远追尾；低 ID 超出短尾窗口也能被找回。
- 游标读取、导入、归属、认领、买家扣款、卖家 pending、结算及三游标更新共用市场事务
  和原 advisory lock。失败全部回滚；进程重启后继续已提交的游标。
- `sub2api_usage_id` 唯一键与 `ledger_txn_id IS NULL` 认领共同防重复。已存在的日志先批量
  查 ID，避免回扫逐条重复 INSERT；归属和记账只处理本轮 ID，避免每秒扫描历史 unknown 流量。
- 余额对账超过容差时再按源用量 ID 与已记账 ID 精确比对，涵盖游标以下晚提交。
  市场和源库各自使用只读 Repeatable Read 快照；复核不持入账全局锁，不阻塞正常入账。
  缺失或软删除的源用户按各自异常处理，不中断其他钱包。
- 活动包退款保留原 3 分钟在途计费宽限期，改为独立常量，未随导入间隔缩短。

没有引入进程内通知，也不依赖 `pg_current_snapshot()` 推断 sequence ID 水位。
sequence 的分配顺序不保证提交顺序；同一套读取与补偿机制适用于独立 worker 和嵌入 worker。
只读物理副本只需支持现有 SELECT；如果存在复制延迟，可见时间还会叠加副本延迟。

## 真实引擎前后实测

使用 `market/scripts/dev.sh` 的隔离临时副本 `/tmp/tm-usage-check/dev-isolated.sh`：
脚本原有 HTTP、Edge、支付替身端口为固定值且已被其他环境占用，临时副本将其分别改为
18878、18978、18378，并使用独立后台进程启动方式。仓库脚本未改。

PostgreSQL 55478（`tm-usage-pg`）、Redis 56478（`tm-usage-redis`）、
引擎 18278、数据目录 `/tmp/tm-usage-check/data`；真实 `MARKET_EMBED=1`。
Edge 在验收构建中通过 Go overlay 指向本次引擎 18278，防止固定 8080 转发至别人的环境。

修复前构建使用 `git show HEAD:<path>` 读取原 ingest/config/replica/query 至临时文件，
通过 Go overlay 构建；不切分支、不改回工作区。前后使用同一数据库、凭证、上游和请求。
本地假 Anthropic 上游在 18478 返回真实协议响应，固定输入 1000、输出 200 token。
客户端请求经过市场 Edge、真实引擎、上游、引擎计费与市场入账完整链路。
每 200ms 查询一次 `/api/buyer/usage`，以响应完成为计时起点。

| 版本 | 调用耗时 | 响应结束到买家明细可见 |
| --- | ---: | ---: |
| 修复前 | 0.0297s | 133.8342s |
| 修复后首次 | 0.0267s | 0.8299s |
| 收紧批次处理后 | 0.0256s | 0.6395s |
| 最终版本 | 0.0245s | 0.8339s |

每次 `total_cost=0.006`、买家倍率 0.8、卖家倍率 0.5：买家扣款 0.0048、卖家 pending 0.003。
累计 4 次调用后：4 条唯一用量、4 笔 usage 交易、结算 request_count=4，
买家现金余额从 100 变为 99.9808（与引擎余额相等），卖家 pending=0.012，平台收入=0.0072。
所有交易借贷和为零。经历重启、多个轮询和重放后再次核对，金额未增加。

使用 ego-browser 登录真实买家页面并刷新 `/buyer/usage`，确认记录、token 数、费用可见，
页面提示改为调用结束后通常几秒内可查。原始测量 JSON、账本断言、日志保存在
`/tmp/tm-usage-check/`。验收后执行隔离脚本 `down`，停止假上游与自建容器。

## 测试

以下命令均通过；集成测试使用上述独立 PostgreSQL，E2E 使用独立 Redis 的 DB 9。

```sh
cd market
go test ./...
go vet ./...
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55478/tm?sslmode=disable' \
  go test -race -tags integration ./internal/billing ./internal/sub2api/replica -count=1
go vet -tags integration ./internal/billing ./internal/sub2api/replica ./internal/compat ./internal/e2e
TM_TEST_DATABASE_DSN='postgres://tm:tm@127.0.0.1:55478/tm?sslmode=disable' \
TM_TEST_REDIS_ADDR=127.0.0.1:56478 TM_SUB2API_REPO=/Users/duegin/project/token-market \
  go test -tags integration ./internal/e2e \
  -run 'TestAPISurface|TestSpendingLimitAndRiskControls' -count=1 -v
cd ../backend
go test ./internal/service ./internal/repository ./internal/server/routes
go vet ./internal/service ./internal/repository ./internal/server/routes
cd ../market/web
node node_modules/vite/bin/vite.js build
```

新增测试覆盖新鲜用量立即可见、低 ID 延迟提交超过 2000 ID 后跨重启恢复、短尾下一轮补齐、
并发入账一次、账本失败后三游标和用量一起回滚、限定本轮 ID、普通与活动包精确对账、
2001 条费用分页、源记录清理、缺失源用户、单连接池以及复核不阻塞正常入账。

## 边界

- 正常前向读取及短尾补偿为秒级。低 ID 延迟提交超过短尾窗口时由全历史循环恢复，
  不能保证秒级；恢复周期随源历史行数增长，每轮回扫 2000 条。
- 最终补齐以源记录在停机及一次完整恢复周期内没有被 retention 或人工删除为前提。
  没有引入跨库原子 outbox；极长停机后源日志已删除的记录不能凭空恢复。
- 异常钱包的精确对账会分页扫描该用户仍保留的源历史，成本随历史增长；只对疑似异常执行，
  不持全局入账锁。当前未新增引擎 `(user_id,id)` 索引。
- 引擎扣款与 usage log 写入仍是两个事务。恰好在两者之间运行余额检查，仍有既有的短暂
  假异常窗口；这次精确复核针对晚提交/回扫引起的差异，没有改引擎计费事务模型。
- 独立进程显式配置 `worker.ingest_interval: 15s` 的部署需改为 `1s`；
  嵌入模式读取默认配置，本次默认值直接生效。

## 文件清单

- 入账与资金：`internal/billing/ingest.go`、`reports.go`、`reconcile.go`、
  `store_credit_reconcile.go`、`reconcile_pending.go`、`store_credits.go`。
- 查询与迁移：`internal/sub2api/replica/replica.go`、`usage_pending.go`、
  `contract/queries/usage_logs_after.sql`、`usage_logs_range.sql`、`usage_costs_by_user.sql`、
  `migrations/0047_usage_ingest_replay.sql`。
- 配置与 UI：`internal/platform/config/config.go`、`config_test.go`、`config.example.yaml`、
  `web/src/views/buyer/UsageView.vue`。
- 测试：`internal/billing/ingest_replay_integration_test.go`、`reconcile_pending_integration_test.go`、
  `internal/compat/schema_integration_test.go`；删除四个 `internal/e2e/*integration_test.go`
  中原来绕过两分钟门槛的时钟调整。

本次没有修改版本号、提交或推送。工作区同时存在另一位协作者的 Edge 同端口任务；
其 backend 路由、main、enginehost Edge 和部署文件不属于本次用量修复。
