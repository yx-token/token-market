# 店铺活动额度运行时

接口/币种以 `billing-contract.md` 为准。本文件描述独立钱包及 Edge 实现；不改变旧 `quota_packs`。

- `KeyService.ProvisionCreditPacks(ctx)` 可每 5 秒调用。等待 `purchase_op_id` 对应 outbox `status='done'` 后才开通 `Shadow.Ensure("credit_pack", packID)`；独立充值 `face_value_usd * usd_to_balance_rate`，幂等键 `tm-credit-fund-{packID}`，保存 `funding_op_id`，等充值 `done` 再将 provisioning 设 active。**不会充值普通买家影子钱包。**
- 创建 `credit_pack` 影子用户显式发送 `balance: 0`，禁止继承 sub2api 后台默认赠金；buyer/probe 保持原有省略字段行为。专项回归 `TestCreditShadowDoesNotInheritDefaultSignupBalance` 用非零默认赠金 37.5 验证活动钱包为 0、其他身份保持默认赠金，且创建重试不重复建用户。
- 充值 payload 直接读取购买事务用 PostgreSQL numeric 换算后保存的 `remaining_balance_units`，不在 Go 中重新以 float 相乘 face/rate；未开通包尚无消费，此值即权威初始充值额。购买服务负责 sub2api 8 位精度与最小额度校验。
- 每个包、普通买家 Key、本店挂单生成一条 `store_credit_keys`；密文 AAD `store-credit-key:{id}`。仅给该包影子身份授权对应本店专用 sub2api group；沿用原 group 倍率，额度扣费是含实际倍率的 `actual_cost`。凭证不向前端或 Redis 路由发布。
- `KeyConfig.credit_candidate` 仅对确有匹配本店活动包的普通 Key 发布，开通完成后 worker 刷新路由。未购匹配活动的普通 Key 不查询活动数据库，维持原有 Redis 缓存转发能力。
- Edge 在原 Key 的状态/到期/IP/花费限额检查之后，仅对计费普通 POST 选包；WebSocket、模型列表、countTokens 保持原路径。数据库重查当前 buyer、key、真实路由、普通 internal_key、店铺 seller_id、包 active、钱包 funding done、退款锁，以及可选平台和模型范围。开通与选择都复用 `listing.MarketAvailableSQL`（挂单 active、上游 listed、有授权账号、允许的健康状态、隐私、厂商 enabled、buyer_facing），并核查实时商家清单与请求模型白名单。普通 Key 可引用本店 consignment/pack 挂单；原 `quota_packs` 专用 Key 排除。包按 ID FIFO。
- Gemini 模型取 URL 的 `models/{model}:action`，其余取 `body.model`；模型无法确定时受模型限制的包不参与。未知/停用/未开通/跨店包不使用；查库或解密出错返回 503，避免在活动状态不明时悄悄改扣现金。
- 选包时使用 `pg_advisory_xact_lock(hashtextextended('store-credit-pack:' || packID, 0))`，锁覆盖完整 HTTP/SSE 响应；取得锁后重查包状态。开通用同名 session advisory lock，退款需要相同锁。**不持有 pack 行锁**，因此 postUsage 可行锁入账。Edge 池连接数沿用 `database.max_conns`，每个执行中活动请求占用一条连接且同包串行；锁等待受首次响应时限约束。
- 隔离钱包准入返回 HTTP 403 且准确的 `INSUFFICIENT_BALANCE` 码（顶层 code、error.code 或 error.type）时，本次不计卖家熔断，记录 platform 重试，按包 ID 继续找下一匹配活动包；全部候选耗尽后才用该店原普通内部 Key 回退。不对不确定网络失败或已开始响应的请求重放。同一请求超包额度的分摊由 billing 按包剩余与现金差额完成，Edge 不把活动面额补到普通钱包。
- 买家普通钱包仍有 `tm-credit-cash-*` 补差出站操作未完成（包含 dead）时，不再准入任何活动包，回到普通现金准入，防止未清偿差额继续透支活动额度。
- sub2api 用量入库可能延迟，因此 advisory lock 不能单独证明可以退款。退款应先切 `refunding` 禁止新请求，再等待用量安全间隔、确认隔离钱包余额与相关已入账用量，最后回收隔离余额并返剩余实收预付。不能依据本地 `remaining_usd` 单独立即退。

并发边界：同包单飞覆盖完整响应，但真实 sub2api 可能在 HTTP 响应完成后异步持久化余额和 usage，准入余额缓存也可能短暂滞后。现有架构是后扣费，不能把上述锁描述成对 sub2api 异步扣费的原子确认；结算 worker 归集后通过现金补差债务拦截并核对隔离余额。本次未修改 sub2api 核心或引入永久未确认请求屏障。

验证：`go test -race ./internal/edge ./internal/buyer ./internal/edgecfg ./cmd/edge` 通过。真实 PostgreSQL + sub2api v0.2.14 迁移 + Admin/User/网关替身的 `go test -race -tags integration ./internal/edge` 验证购买扣款/充值完成闸门、重复开通不重复充值、独立钱包、零普通余额可消费活动包、同店多挂单共享、倍率 2 实扣 1.2、跨店/平台/模型不可用、商家移除/健康异常/暂停阻断、普通退款锁和旧包隔离、同包锁互斥、首包耗尽后优先下一包、最终回退现金且 usage 不重复、未完成/已 dead 补差债务阻断、退款状态不被开通 worker 复活。测试使用独立临时数据库和 miniredis，不清空共享 Redis。

最终赠金修复后按主代理收尾要求不再执行集成测试；新增专项集成回归仅编译检查，需纳入下一次集成运行。普通 buyer 包测试已重跑。
