# 活动包资金链路代码审计

审计结论（修复前快照）：发现两项风险，其中 P1 会使正常活动包退款无法完成。**最终状态：两项代码修复与新增带 race 集成回归均已通过主代理验收。** P1现拒绝不支持8位的初始换算、按已记录消费及已完成资金操作可解释的量化/表示误差核对、撤销精确远端余额；P2仅活动钱包新建显式余额0，funding直接发送数据库换算结果。以下保留原始发现与证据，审计代理未修改实现或测试。

审计深度为高风险：人民币资金、美元面额换算、隔离钱包、跨服务 outbox、副作用重试及退款。审计基于 `main` / `ede5d3e` 上的未提交工作区，范围限定需求2/5后端与其真实运行时消费者；不扫描整个项目，不评价代码风格。技术栈为 Go 1.26、pgx/PostgreSQL、HTTP Edge、Redis 路由缓存和 sub2api v0.2.14。未进行部署、浏览器验收或全量 e2e，未触碰共享 Redis。

主代理后续验收补记（2026-10-15）：上述 P1/P2 修复后，billing、buyer、edge 三包全部 `-race -tags integration` 已实际执行通过，包含真实8位余额退款、不可表示换算拒绝和非零默认赠金隔离三项新增回归；HTTP购买/快照/资金隔离 e2e 亦通过。测试日志与浏览器结果统一记录于 `market-upgrade-acceptance.md`。前述“仅编译通过”保留为审计工人交接时点，已不代表最终测试状态。

## 阻断风险

### P1：本地10位活动余额与 sub2api 8位余额不一致，会卡住退款或产生必败的撤销操作

证据：

- 真实版本 `../github-fork/sub2api` 的 `v0.2.14:backend/migrations/001_init.sql:46` 定义 `users.balance DECIMAL(20,8)`；同文件157/158行定义 `total_cost/actual_cost DECIMAL(20,10)`。
- `migrations/0034_store_credit_packs.sql:26` 的 `remaining_balance_units` 为 `numeric(20,10)`。`internal/billing/store_credits.go:149` 允许面额乘汇率小至1e-10；168/169行按10位本地额度建快照。
- `internal/billing/store_credit_usage.go:44` 使用实际10位用量扣本地额度；68/69行保存本地剩额。
- `internal/billing/store_credits.go:302` 退款要求 `abs(remote-remaining)<1e-9`。311至316行随后用**本地** remaining 生成远端撤销金额。
- 真实 sub2api `backend/internal/repository/user_repo.go:943` 的余额操作要求 `balance + delta >= 0`，超出真实余额的撤销不会成功。
- 现资金集成夹具 `internal/billing/store_credits_integration_test.go:37` 把用户余额定义成 `numeric(20,10)`，与真实 sub2api 不同，无法发现本问题。

已在现有测试 PostgreSQL 执行纯 SELECT 实证：

1. 初始70、实际费用0.123456785：真实8位余额69.87654322，本地10位剩额69.8765432150，差5e-9，现退款闸返回false。无新调用后差额不会自行消失，包持续refunding。
2. 面额0.01、汇率1.00000001：本地剩额0.0100000001，真实余额0.01000000；现退款闸返回true，但真实余额减本地剩额小于0，撤销操作被上游拒绝，最终dead。故仅放宽容差不足以修复。
3. 面额0.01、汇率1e-8在当前允许范围内，初始可抵平台单位1e-10会在真实钱包舍入为0，包可被标active却无法消费。

影响：细粒度正常用量或合法小数汇率即可触发，不依赖恶意请求，也不是已经记录的异步入账限制。用户实付资金将无法通过正常退款状态机退回。

最小修复方向：明确活动初始可用单位与外送钱包金额的8位量化策略，禁止初始额度舍入为0；退款应在可解释的舍入边界内核对实际钱包，并撤销可由真实远端表示且不超过其余额的金额，避免用大容差绕过真正未入账消费。覆盖消费/补差/恢复等多次8位舍入的累积差异。归属资金后端与数据库/测试工程。

验收：真实 `numeric(20,8)` 用户余额夹具下，分别执行上述两个退款反例、小额度购买、重复退款与并发用量；要求退款结束、赠送额度不兑现、双边账本平衡、无永久残留debit操作。沿现有专项命令运行 `TM_TEST_DATABASE_DSN=... go test -race -tags=integration ./internal/billing -run 'TestStoreCredit' -count=1`，并在真实sub2api结构运行时专项中覆盖量化输入。

### P2：活动隔离钱包创建省略 balance，继承 sub2api 默认赠金会造成额外无账本余额及退款卡死

证据：`internal/buyer/credit_runtime.go:84` 使用 `Shadow.Ensure(credit_pack, packID)`；`internal/buyer/shadow.go:139` 构造 CreateUserInput 时没有设置已有的 `Balance *float64` 字段（定义在 `internal/sub2api/adminclient/methods.go:69`）。真实固定版本 `v0.2.14:backend/internal/service/admin_user.go:120` 的 CreateUser 在 Balance 为nil时使用 `settingService.GetDefaultBalance`。随后 `internal/buyer/credit_runtime.go:102` 又充值完整活动面额。

静态复现：sub2api后台默认初始赠金设5，买入可抵70单位的活动包，创建的钱包先有5，再收到70，实际75但本地remaining=70。即使完全没使用，`store_credits.go:302` 退款闸也不会通过，资金一直停在refunding；独立对账只能记录差异，不能消除此差异。

影响：配置触发，当前默认赠金为0的环境不会出现；新功能不应依赖未在活动包契约中约束的外部赠金配置。尚未运行“非0默认赠金”接口复现，本结论为明确的生产方/消费方静态证明。

最小修复方向：至少对 `credit_pack` 影子身份创建显式传入0余额，并验证创建或复用身份的初始资金来源。不要仅为修复活动包而未经回归改变旧买家钱包语义。归属运行时钱包与测试工程。

验收：sub2api默认赠金非0的替身/真实服务场景中，活动钱包仍从0创建，funding只充值一次，未用包可完整退回实付人民币；重复开通不增加余额。

## 已审执行链与需求对账

| 验收点 | 生产方 → 消费方 / 持久化证据 | 验证证据与结论 |
| --- | --- | --- |
| D16不变，人民币售价/美元面额真实换算 | `store_credits.go:129` 读商品金额，168行快照汇率；`credit_runtime.go:104` 仅给隔离钱包充值face×rate；`store_credit_usage.go:44` 按含倍率actual_cost抵扣 | 核对contract与代码；`TestStoreCreditsConsumeAndCashRemainder`覆盖¥30/$10/rate7及现金尾差。已落地，精度边界见P1 |
| 购买幂等与实付托管 | `store_credits.go:85/113/119` 首次/锁后查幂等；168至186行插入包、转预付、排outbox同事务；迁移买家范围唯一键 | 静态核对锁后幂等检查、购买及转托管同事务与DB唯一约束。未看到独立重复购买并发测试，不能把消费并发测试当购买并发证据；本次未复跑全量测试 |
| 先扣款再开通、隔离钱包不泄漏到普通钱包 | `credit_runtime.go:24/81/102/120` purchase done →独立身份 → funding done → active；内部key密文 | `TestCreditRuntimeIndependentWalletScopeAndFallback`覆盖开通闸与多次开通；存在P2初始化缺口 |
| 本店及厂商/模型范围，额度优先、多包、现金回退 | `edge/credit.go:47` 实时SQL复查买家、商家清单、挂单、健康、退款锁、scope；`edge/proxy.go:255` 选择与不足后下一包；`store_credit_usage.go:45` 结算再次核scope | `TestStoreCreditsScopeCannotCoverOtherSellerPlatformOrModel`、运行时专项。已落地；默认池/WebSocket排除属已说明边界 |
| 分摊、欠款、不会重复结算 | `ingest.go:165` 全局事务advisory认领；`store_credit_usage.go:37` 包行锁、63行usage唯一分摊、91行双侧幂等outbox；`store_credit_reconcile.go:83` 重试原欠款键；`edge/credit.go:71` 未清欠款阻断活动 | 并发、失败整笔回滚、现金尾差专项已列交付记录。未发现独立双付路径；异步后扣限制仍存在 |
| 退款只退剩余实付且不退走欠款 | `store_credits.go:203/217` 同包锁后refunding；269行安全延迟、302行余额闸、339行只转剩余人民币预付；`order_refunds.go:148` 现金上界 | `TestStoreCreditsRefundOnlyUnusedPaidAmountAndRetry`、`TestStoreCreditCashDebtCannotBeRefundedAndRetriesAfterRecharge`。核心状态机已落地，受P1/P2阻断 |
| 卖家冻结24小时，重复/并发不双付 | `ingest.go:308` 截止时刻扣完整小时，341行行锁认领增量，与账本共事务；`cmd/worker/main.go:128` 每轮读动态FreezeHours | `TestUnfreezeHoursWaitsForWholeHourMaturity`、`TestUnfreezeConcurrentClaimsAndLateUsage`及失败回滚专项。未发现小时截断提前释放或重复认领 |
| 报表与对账覆盖活动预付 | `reports.go:46/100/149` 实付消费与活动分摊；228行平台总负债含活动预付；`store_credit_reconcile.go:11` 活动钱包独立核对 | 已读对应字段与余额对账专项；没有把活动美元当普通人民币现金。精度差异可能误报/阻塞，见P1 |

以上测试名称/主代理通过结果是既有证据，不是本次审计亲自重跑结果。审计亲自执行的验证仅是下面列出的读取与只读SQL。

## 字段、状态及影响面

| 字段 / 状态 | 来源、默认值、消费者与兼容 |
| --- | --- |
| price_cny / face_value_usd / usd_to_balance_rate | 商品 → 购买快照 → funding/使用/退款；rate默认1，最多8位，旧quota_packs不复用此字段。P1涉及本地/远端可表示精度 |
| remaining_usd / remaining_balance_units / prepaid_remaining_cny | 购买建立，分摊行锁更新，列表展示，退款读预付及平台单位；remaining_balance_units为精确主余额。P1涉及本地10位与上游8位 |
| credit_pack_id / credit_covered_units / credit_prepaid_cny / cash_cost | 活动内部key归属→usage分摊→买家用量/逐跳/日账单；旧数据新增字段为NULL或0，读取COALESCE保持原路径 |
| buyer_store_credit | 新负债账户；购买从cash转入，使用/退款释放；平台buyer_balances包含此账户，独立单列不能重复相加 |
| provisioning / active / depleted / refunding / refunded | purchase done/funding done才active；refunding禁止新活动准入，但允许之前usage结算；退款两侧outbox逐步推进。未发现worker复活refunding路径 |
| freeze_hours / freeze_days | 新配置默认24小时，旧days兼容；worker每分钟读取，小时桶保守延后小于1小时，无提前解冻 |

编辑链路：商品认证卖家身份绑定、SQL seller_id隔离，已购scope/价格/汇率快照不追改；商品测试已验证。删除链路：活动商品使用paused、包使用refunded，不删资金快照或usage，旧quota_packs保持原存续路径。本次没有执行真实生产数据迁移、旧版本混部或回滚验证，不能据此给发布批准。

最小威胁模型：买家可控制offer_id、幂等键、请求模型与并发；资产为普通现金、实付托管与卖家收益；可信边界为认证HTTP、Edge内部凭证、sub2api异步资金接口。已核对购买/退款买家归属、商品卖家归属、运行时seller/platform/model限制与凭证不向前端发布。敏感密钥保留既有加密AAD，未发现本范围新增明文返回路径。

## 实际验证与证据边界

- 读取 `billing-contract.md`、`billing-backend-results.md`、`credit-runtime.md`，审阅指定billing源文件、runtime/Edge生产方与调用点、0034迁移、冻结和活动包专项测试。
- 实际搜索范围为 `internal/billing`、`internal/buyer`、`internal/edge`、`internal/sub2api`、`cmd`、`migrations`。搜索词包含 `store.credit`、`StoreCredit`、`credit_pack`、`remaining_balance_units`、`cash_cost`、`refund`、`KindCreditPack`、`FreezeHours`、`CreditUSDToBalanceRate`、`quota_packs`、`dead`、`balance`、`DefaultBalance`、`Idempotency`。命中覆盖HTTP/worker/Edge可达路径、旧包排除、异步outbox、报表及夹具。
- `git diff --stat`、相关资金diff、`git branch --show-current`、`git rev-parse --short HEAD` 用于确认未提交变更边界；固定 `git -C ../github-fork/sub2api show v0.2.14:...` 复核真实余额精度与默认赠金语义。
- 实际执行两次 `docker exec tm-test-pg psql -U tm -d tm -Atc` 的纯SELECT，不写数据，分别得到 `69.87654322|69.8765432150|0.0000000050|f` 和 `0.01000000|0.0100000001|t|f`，映射P1两个失败分支。
- 未重新运行e2e、全量race、flushRedis；主代理已报告Go全量、vet、真实PG资金race、runtime真实PG-race通过。上述自动化不能覆盖P1，因为金额夹具采用了不同的余额精度，也未覆盖P2非0默认赠金。

审计排除了只看diff、只测happy path、后台/报表/异步消费者漏查及将工具绿灯当资金正确性的做法。仍保留外部异步用量晚提交、钱包缓存和完整响应锁之间的既有边界，不将其重复包装为新发现。后续仅需资金/运行时最小修复两个问题，并由数据库/测试专项验证真实精度与默认赠金配置，之后复审收口。
