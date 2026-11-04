# CC Switch 买家 Key 余额查询

核实日期：2026-11-04。

## 入口与认证

Edge 本地处理 `GET /v1/usage`，并兼容旧 Antigravity 客户端的
`GET /antigravity/v1/usage`。两个入口均不转发到 sub2api，不生成上游调用、
用量或 attempt 日志；主端口与 8090 使用同一个 Edge handler。
非 `tm-` Key 在宿主仍走原生网关。

复用 Edge 的 Key 读取、无效 Key 限流、状态、过期、花费封禁与 IP 白名单校验。
停用返回 403，吊销/无效/过期返回 401，花费封禁返回 429，IP 不符返回 403。
市场库再次验证 Key 所有者、Key/用户状态和影子钱包锁，避免陈旧路由配置误报可用。
未配置市场 DSN、数据库不可访问或影子钱包未就绪返回 503：
`{"error":{"type":"api_error","message":"usage balance unavailable"}}`。

## 金额口径

顶层 `remaining` 表示这个 Key 在当前路由范围内的 CNY 计价消费额度，不是可退款现金。

| Key 类型 | 基础消费额度 |
| --- | --- |
| 普通钱包 Key | `buyer_cash` 账本余额 + 当前路由可用的活动包 `remaining_balance_units` |
| 旧 `quota_packs` 专用 Key | 仅该 Key 的 active 包 `remaining`；不叠加现金或活动包 |

旧包只要存在任何状态的关联记录，就仍为专用 Key。包已 depleted/refunding/refunded
时余额为零，不回落钱包。普通钱包的 `buyer_cash` 为负债账户的负分录和。

活动包按包 ID 去重，只计资金开通完成、凭证有效、影子钱包未锁、店铺与当前路线匹配、
商品和上游可用、平台/模型范围有交集且无待处理补差债务的包。
一个包关联多个挂单不会重复计入。没有指定模型时表示路由范围内至少一种可用模型；
不能据此认为全部额度适用于任意模型。商品、路由或权限刚变更时，Redis 发布存在短暂窗口。

活动包购买快照中的 `remaining_balance_units` 与调用 `actual_cost` 使用同一计价单位，
包含赠送消费额度；`remaining_usd` 是活动美元额度，`prepaid_remaining_cny` 是剩余人民币本金。
例如实付 80 CNY 获得 100 USD、快照兑换率为 2，则初始消费额度为 200 个平台余额单位，
不表示可以退回 200 元。

最终 `remaining = max(0, min(基础消费额度, 各已配置限额的剩余额度))`。
总限额和 5 小时/1 天/7 天窗口均按现有 LimitEnforcer 的
`usage_records.actual_cost` 合计计算。虽然历史字段名带 `_usd`，现有执行逻辑
直接与平台消费额比较，不进行美元换算，本次保持该语义。

响应包含 `remaining`、`unit: CNY`、`currency: CNY`、`is_active`、`isValid`，并兼容
原生常见的 `mode`、`status`、`balance`、`quota`、`rate_limits`。
顶层 `balance`、`quota_remaining` 是 `remaining` 的兼容别名；`quota.remaining`
单独表示 Key 总限额余量。`wallet_remaining`、`quota_pack_remaining`、
`store_credit_remaining_balance_units` 提供限额裁剪前的分项，活动 USD/本金另列，
不暴露内部 Key、账号或卖家信息。余额或任一限额耗尽时 `remaining=0`、`is_active=false`。

数据来自市场已归集的账本与用量，`source=market_ledger`、`scope=key_routes`、
`amount_kind=consumption_credit`。`as_of` 是读取快照的时间，不是所有上游用量已归集的水位。
进行中、尚未归集的用量和未完成的余额出站操作可能造成短暂差异。

## 部署与查询成本

不增加环境变量或迁移。嵌入部署已配置 `TM_DATABASE_DSN`，更新镜像即可；
沿用 `EDGE_PUBLIC_URL=https://idlicat.com`，8090 兼容入口继续保留。
独立 `market-edge` 要使用本功能，需配置指向市场库的 `TM_DATABASE_DSN`。
未配置时只对本地余额查询返回 503，模型调用保持原配置行为。

每次余额查询只执行一个只读 SQL，使用同一语句快照、现有 owner/Key 索引和 2 秒超时。
CC Switch 默认每 30 秒查询。本次不增加成功缓存，以便断库立即报错并避免缓存已撤销状态；
HTTP 响应使用 `Cache-Control: no-store`。单个 Key 的历史用量聚合成本随记录数增长，
高频或大用量场景仍需观测查询延迟，必要时另行引入有明确失效策略的短缓存/汇总。

新版导入脚本把 `/v1`、`/antigravity`、`/antigravity/v1` 结尾归一至站点 `/v1/usage`，
默认币种改为 CNY。返回错误或余额不是有限数值时显示查询无效，不伪造零余额。
已导入的脚本不会自动更新；旧脚本对这两个本地入口也可工作，读取服务端的 CNY。
要获得新版脚本的路径归一和异常解析，请重新导入对应服务商。

## 验证与真实环境验收

受影响的 market/backend Go 包 test、vet、race 通过；真实 PostgreSQL 集成覆盖
48 个子场景，包括只读连接、关闭连接池、旧包状态、活动包去重/准入、窗口限额和身份失效。
前端 Vitest 35 个文件共 306 项测试、类型检查通过。未启动 `dev.sh` 或登录线上。

真实环境建议按以下顺序验收：

1. 使用有效普通买家 Key，对站点 443 和旧 8090 各请求 `/v1/usage`，确认金额/币种一致，
   没有内部账号字段。再次请求 `/antigravity/v1/usage`，确认同一结果。
2. 记录买家余额、用量条数与 attempt 条数，仅轮询余额接口，确认余额和条数都不变，
   上游没有请求；普通 sub2api Key 请求 `/v1/usage` 仍返回原生结果。
3. 钱包发起一次模型调用，归集完成后核对余额下降；分别验证旧包专用 Key、
   可用活动包、同包多挂单不翻倍，以及活动包被暂停/退款/移除路线后不再计入。
4. 验证总限额和窗口限额取最小值、耗尽显示零且不活跃；停用/吊销/过期/IP拒绝/
   花费封禁保留相应 HTTP 错误码。
5. 在隔离测试部署中断开市场数据库，确认明确 503 且客户端显示查询失败；
   恢复连接后恢复金额，不能显示伪造零余额。
6. 从 C 端分别导入 Claude、OpenAI、Gemini、Grok、Antigravity，确认 CC Switch 的
   查询 URL、CNY、正常金额、零额度与错误状态，等待一个 30 秒轮询周期确认刷新。

回滚仅需恢复上一镜像，不涉及数据变更；回滚后新本地余额入口重新不可用，
旧 Antigravity 入口也不再具备本次的本地处理保护，需同步禁用余额轮询。
