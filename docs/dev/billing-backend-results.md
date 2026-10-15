# 需求2/5后端交付记录

主代理后续验收：本文件交接之后，最后新增的三项回归及 billing/buyer/edge 三包全部带 race 集成测试已实际通过，最终 HTTP 购买 e2e 与浏览器购买、抵扣、结算、退款亦通过。以下“尚未执行”保留为工人交接时点，最终证据以 `market-upgrade-acceptance.md` 为准。

## 最终收尾与测试基线

主代理已报告全量 `go test -race -tags integration ./...` 通过（billing约63秒、edge约28秒、e2e约271秒），前端239项与build通过；随后由主代理单独重跑e2e与浏览器验收。该结果是下列最后精度/赠金修复**之前**的基线，不能当作其回归证据。本代理按要求未再执行集成测试。

最后代码变更：

- `store_credit_reconcile.go` 的 `time` 导入已补齐，构建恢复。
- `store_credits.go` 购买拒绝换算后小于1e-8、超过1e9或不能精确表示为8位小数的面额（400 `INVALID_CREDIT_CONVERSION`，事务不扣款）；防止上游舍入成零或暗改汇率。
- 退款闸仅接受已入账消费、已完成资金操作能解释的8位量化与payload表示差异；撤销实际远端余额，人民币退款仍按本地剩余实付预款。避免10位本地余量导致永等/撤销余额不足。
- `buyer/shadow.go` 仅新活动钱包显式初始余额0；`buyer/credit_runtime.go` funding发送数据库换算结果，移除Go浮点乘法。
- 资金fixture改为真实 `numeric(20,8)`；新增 `TestStoreCreditsRefundReclaimsRemotePrecision`、`TestStoreCreditsRejectUnrepresentableFunding` 和 `TestCreditShadowDoesNotInheritDefaultSignupBalance` 回归源码。

最终普通相关包测试与 integration 标签的仅编译检查通过（`-run '^$'`，没有运行集成用例）；上述三个新增专项尚未实际执行。修复前审计与修复状态见 `billing-audit.md`。未修改主代理e2e验收文件、前端区域或市场查询。

## 已完成

- `freeze_hours` 默认24，后台动态设置；兼容旧 `freeze_days`。worker每分钟解冻，小时桶结束后才开始计算冻结期，原子认领增量避免并发/重复双付。
- 新活动包独立于旧 `quota_packs`：卖家商品、买家购买/列表/退款API、人民币售价/美元面额、seller_id与可选厂商/模型快照、独立影子钱包与凭证、后台开通及Edge选择/多包FIFO/现金兜底。
- `credit_usd_to_balance_rate` 默认1，范围1e-8..1e6，最多8位小数，购买快照；只对活动面额转换，不改变D16钱包及旧按量费率。活动按包含店铺倍率的actual_cost扣减可抵平台单位。
- 购买及用量分摊、余额/预付资金/卖家收益/平台收入、最后一笔差额、outbox补扣/归还和退款都保留唯一业务键及精确numeric计算。优惠由卖家实收比例承担。退款只返剩余实付预付，不兑现赠送额度。
- 买家用量、日账单、请求明细返回实际人民币消费及分摊字段；平台买家总负债包含活动预付，单列buyer_store_credit；活动隔离钱包独立对账与R3冻结。
- 现金补差不足保留欠款并停活动准入，充值后重试原outbox键；退款上限同时受账面现金约束，避免趁远端补扣尚未执行时退走欠款。
- `postUsage`同事务全局认领锁与游标GREATEST防并发重复/倒退，扫描上限overlap+batch修复积压停滞。

## 文件与契约

- 总契约：billing-contract.md；冻结：settlement-contract.md；活动运行时：credit-runtime.md；卖家商品：credit-offers-progress.md。
- 迁移仅新增0034_store_credit_packs.sql。
- 后端新增核心文件：billing/store_credits.go、store_credit_usage.go、store_credit_reconcile.go；listing/credit_offers.go；buyer/credit_runtime.go；edge/credit.go；httpapi/handlers_credit_offers.go、handlers_credit_packs.go。
- 共享文件仅精准接入billing.ingest/ledger/reports/reconcile/funds/order_refunds、settings、worker/edge入口、HTTP路由、买家Publish的credit_candidate。保留其他工人所有改动。未修改买家/卖家/管理前端实现。

## 已验证

相关Go包单测通过。真实PostgreSQL billing专项 `-race -tags=integration` 已通过：

- postUsage并发一次认领、回滚重试与重复不双付。
- Unfreeze成熟边界0/1/24/25小时、动态配置、并发、迟到增量、失败回滚。
- ¥30/$10/rate7：首笔3.5平台单位抵$0.5并释放¥1.5，次笔68单位抵剩66.5并转现金¥1.5；卖家收益1.2/24，所有复式交易平衡。
- 跨卖家/厂商/模型零覆盖；并发总抵扣不超过余额；账本失败整笔回滚。
- 退款未追平不返现、追平后仅退剩余¥28.5、多阶段重复不双退。
- 用量/账单/逐跳报表分摊值与平台预付负债一致。
- 活动钱包与普通现金在补差outbox未发送/已完成时均正确对账；异常多余活动余额单列告警；欠款不扩大可退款余额，充值后原扣款幂等恢复。

运行时 `-race` 测试另验证真实sub2api结构+替身的零普通余额可消费、同店多上游、倍率、实时商家/健康/厂商/白名单、退款锁、同包互斥、首包耗尽优先第二包、最终现金兜底和不重复usage。测试用独立临时库/miniredis，未并行运行e2e或flush共享Redis。

## 主代理验收与明确边界

1. 主代理执行全量Go/race集成与浏览器验收，重点充值/购买/开通/真实调用/用量展示/退款全流程及旧包回归。
2. 原用量安全延迟约2分钟保留，不能宣称资金跨进程即时一致；24小时冻结按小时桶保守晚于真实24小时，小于1小时额外延迟加worker轮询。
3. sub2api是异步后扣费。单包请求有完整响应期互斥，已归集差额会阻断继续使用活动，但不能把Edge锁宣称为sub2api余额缓存/异步用量的原子确认；详见credit-runtime.md。
4. 活动路径为可确定本店的普通HTTP计费POST。平台默认池、WebSocket、模型列表与countTokens沿原钱包路径。支持普通Key引用同店多挂单，不生成前台包专用Key；原quota_packs专用Key不混用活动。
5. 购买/开通或退款远程操作进入dead时保留资金/状态等待现有outbox运营重试，不能把不确定的远程结果当作未扣款而自动返现。现金尾差dead在充值后自动重试；其他dead沿现有运维处理。
6. 活动使用完整响应锁占用数据库连接，Edge连接池使用database.max_conns，同包串行；高并发SSE需按实际并发设置连接池容量。
7. 最后精度/赠金修复的新增集成回归待主代理后续统一验证。显式零余额只作用于新建活动钱包；若本地/历史试验已产生继承赠金的活动身份，不会自动覆盖资金，对账仍会拦截其异常差异，需核实来源后按既有运维流程处理。

无git commit、部署或外部消息。
