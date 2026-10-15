# 需求 1、4 实施结果

主代理后续验收：全包带 race 集成测试及独立完整 e2e 已通过；工人第二轮重叠 e2e 按要求中止，不作为通过证据。浏览器已走通未检测 Astra 挂单、加入商家清单、Key 顺序保存和重新进入后的持久化。最终证据与 Pi GLM 5.3 max 独立复核结论统一记录于 `market-upgrade-acceptance.md`。

## 前端

- 新增 `/buyer/merchants` 商家清单页和买家导航，按 `seller_id` 管理已加入商家。暂无可用挂单的商家继续保留，移除成功后更新列表，移除失败不清空原数据。
- 市场商品先 `PUT /api/buyer/merchants/{seller_id}`，成功后进入 API Key 配置，商家清单写入失败时不创建 Key、不跳转。旧额度包购买也先加入商家清单。
- API Key 页面仅加载 `/api/buyer/merchants/listings`，支持选择挂单、调整优先调用顺序及平台默认分组兜底。清单外挂单不会通过 URL 预填，旧 Key 中不可用的挂单明确提示且不能原样保存。
- 市场卡片、商品详情、商家清单和 Key 路由编辑统一使用检测状态组件。缺失或 `untested` 状态显示“未检测”，不通过旧 grade/health/质量检查推断通过；取消原有默认 B 等级、95% 成功率和 450 ms 数据填充。
- 每个声明模型独立显示检测状态，仅 `gpt-6-astra` 展示降智结果。无有效模型检测标记时，即使残留 `passed` 也显示未检测；通过、未通过、无法判定分别展示。
- 市场详情加入带 `seller_id` 的“店铺活动额度包”链接，活动包页面与买家功能由主代理实现。
- 通用接入向导和白名单组件改为保存非空模型白名单后按后端 `ready` 状态直接挂单，不主动运行检测。卖家可从 `ready` / `verify_failed` 主动检测；基线账号返回 `verifying` 时保留检测中流程。`suspended` / `removing` / `removed` 不提供跳过检测、恢复或挂单入口。

### 前端验证

- `cd web && pnpm typecheck`：通过。
- `cd web && pnpm test src/__tests__/merchant-flow.spec.ts src/__tests__/market-detection-status.spec.ts src/__tests__/market-models.spec.ts src/__tests__/route.spec.ts src/__tests__/onboarding.spec.ts src/__tests__/model-whitelist.spec.ts`：6 个文件、36 个测试通过。
- 权限/顺序/状态用例覆盖：清单持久化成功前不跳转；加入失败不签发 Key 或购买旧额度包；清单外 URL 和失效旧 Key 挂单被阻止；买家调整的挂单顺序与末尾平台兜底保留；无挂单商家保留、移除失败不丢失；空模型白名单不能保存；普通账号 ready 不自动检测；基线 verifying 与冻结/移除状态边界。
- `git diff --check -- web/src`：通过。
- 按协作约定未运行浏览器验收，由主代理统一执行。

## 后端

- 新增按 `(user_id, seller_id)` 保存的商家清单、本人清单读写路由和清单内可用挂单查询。无可用挂单仍保留商家身份。
- 创建/编辑 Key 强制检查商家范围，在事务内锁定清单项并复核；发布和开通时继续过滤已移除商家、停用挂单、冻结上游、禁用厂商和调度隔离账号。移除商家立即重发路由，保留剩余跳顺序及原有选择。
- Key 发布通过 Key 行锁串行化；移除商家先锁 Key 行再锁商家，避免与编辑反向等待。清单写入失败、缓存发布失败均向调用方返回错误，不假报成功。
- `0036_buyer_merchants.sql` 回填未删除存量 Key，以及升级前仍为 pending 的旧在线额度包订单；去重且排除已删除 Key、取消/失败订单。
- 普通卖家在有效授权、非空白名单成功同步及隐私保护完成后直接 ready，不创建检测记录，不改写默认健康状态为通过。基线选择模型仍走检测。主动检测重试保留白名单同步和上游锁。
- 挂单要求有效远端账号，继续阻止 reference、非 ready、无授权、禁用厂商、隐私失败、降智及不可用账号；未检测不能获取平台回收报价。
- 总体和逐模型检测状态共用可追溯证据：同上游已完成运行、冻结设置、当前检测项定义、有效期和重置时间。缺少 run_id、失败运行、缺少快照或定义变更时不能显示 Astra 通过；仅 Astra 下发降智结果。
- 调度器允许已授权未检测挂单进入专属分组，保持其在默认保障池之外；远端授权恢复后仍按有效检测证据保留这一边界。相关改动限定于 planner/runner 小段。

### 后端验证与交接状态

- `go test ./internal/listing ./internal/scheduling ./internal/buyer ./internal/market ./internal/httpapi`：5 包通过。
- 新增真实端到端专项：商家身份去重、跨买家隔离、清单外创建/编辑拒绝、保存调用顺序、移除立即失效、重新加入保留顺序、暂停挂单过滤、有效授权选模型后直接 ready、无自动检测记录、未检测回收拒绝、冻结/移除状态不可绕过。首轮两个专项通过（3.843s）。
- `go test -race -tags integration ./internal/buyer ./internal/listing -run 'TestMerchantMigration|TestDetectionViews' -count=1`：两包通过，分别验证旧 Key/旧待履约订单回填，以及无 run_id/失败运行/缺少快照/过期定义/证据重置不能显示通过。
- 首轮 `internal/e2e` 全包 `-race`：192.676s，仅导入 draft 已有白名单、检测重试重新同步白名单两个旧专项失败；对应生产逻辑已修复，两专项 `-race` 再跑通过（14.393s）。第二轮全包及基线模型选择专项结果待协作测试工人回报。
- 前端额外修正 `inspection-report.spec.ts` 的已检测夹具，并覆盖缺失状态/未检测时隐藏残留等级、时间和过期通过结果；相关 4 文件 37 测试通过，类型检查通过。
- `git diff --check`：通过。未提交、未部署，未回退其他工人代码。
- 用户已指定后续商家/未检测独立审阅交由 Pi GLM5.3 max；浏览器和全量最终验收由主代理统一完成。
