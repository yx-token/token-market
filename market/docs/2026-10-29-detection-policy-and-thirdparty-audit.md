# 检测策略配置与第三方检测链路排查记录

- **状态**：已实施并经浏览器验收（含待决策事项）
- **日期**：2026-10-29
- **范围**：`backend/internal/server/routes`（引擎侧读取路由）、`market/internal/quality`（策略配置 + 第三方执行）、`frontend/src/market-admin`（线上后台检测策略页）、`market/web/src`（卖家接入指引）
- **动机**：后台检测策略的配置「还是会有问题」；并需确认「开启第三方检测后，是否真的走第三方检测」。

## 一、结论摘要

1. **根因是引擎侧的读取接口只返回 3 个字段**（见 §二）。后台编辑弹窗因此回显为空，管理员保存时用空值覆盖掉库里原有的全部配置。此前修的配置层问题都是下游症状。
2. **弹窗保存的第三方检测配置会被执行端真实使用**：浏览器里把检测站模型改成 `claude-opus-4-1`、处置改成 `suspected`，再触发检测，新生成的 `third_party_checks` 记录用的正是这两个值，协议按平台自动推断为 `anthropic`。
3. **第三方检测主链路是通的**，且原有 5 个「静默失败」出口已堵上并留下可见原因（见 §四）。
4. **本次最大的教训**：仓库有两套后台前端。`market/web/src/views/admin/` 下除 `OpenEngineView.vue` 外的页面（以及 `layouts/ConsoleLayout.vue`）没有接入路由，不进构建产物；线上后台是 `frontend/src/market-admin/`。第一轮前端修复改在了未接入路由的文件上，单测全绿却对线上完全无效，是在浏览器验收时才发现的。已写入仓库根 `AGENTS.md`。

## 二、根因：引擎侧列表接口被「瘦身」

> **名词：native 与 relay。** 引擎进程（`backend/`）对 `/api/v1/admin/market/*` 有两种实现，见 `backend/internal/server/routes/market.go`：
>
> - **native**：`registerNativeIdleCat` 里的 `bind(...)`。引擎自己用简化 SQL 查市场库（`backend/internal/idlecat/lists.go`），只返回少数几列。
> - **relay（转发）**：`registerIdleCatRelay` 清单里的接口，经 `idleCatRelay` 进入 `enginehost.ServeAdmin` → `market/internal/httpapi/engine_bridge.go`。引擎**不自己回答**，而是在进程内用 `httptest` 构造一次请求，调用 market 原来的管理处理函数，把响应原样返回。没有网络往返，字段与校验和 market 自己的接口完全一致。
>
> 代码注释把 relay 称为「尚未单独重写的接口」的承接方式，native 则是已重写的版本。本次问题出在 native 的列表是简表。relay 要求引擎进程里配好 market 的 `TM_*` 环境，否则返回 503；`PUT` 检测策略、`settings`、`users` 等本来就走 relay，所以这次没有新增依赖。

线上后台 `/admin/market/policies` 读取的是 `/api/v1/admin/market/detection-policies`，由引擎侧 `backend/internal/server/routes/market.go` 注册，原先绑定到 `idlecat.Store.ListPolicies`：

```sql
SELECT platform, auth_type, enabled FROM detection_policies
```

编辑弹窗需要的 `flows`、`third_party`、`thresholds`、`jitter_minutes`、`daily_budget_usd`、`token_budgets` 等二十余个字段全部缺失。

**后果是数据丢失型的**：

1. 点「编辑」，弹窗里抖动、预算、测智周期与模型、降智周期与模型、身份核验全是空/关；
2. 管理员只想改一个开关，保存时其余字段以空值或默认值提交；
3. 库里原有的完整配置被覆盖。

**修复**：把这两个 GET 加入 relay 清单（`registerIdleCatRelay`），交给 market 进程内的完整处理函数 `enginehost.ServeAdmin`；`PUT` 本来就走 relay，读写现在走同一套口径。同时删除已无调用方的 `Store.ListPolicies` / `Store.ListProviders`，避免后人误以为它们还在用。

**回归**（`backend/internal/server/routes/market_detection_relay_test.go`）：

- `TestDetectionReadsUseRelayNotThinStore`：用没有数据库句柄的 `Store` 注册路由，谁误走简表读取会 panic。**已验证红灯**：临时回退修复后该测试失败，恢复后转绿。
- `TestDetectionRelayCoversReadAndWrite`：读写四条路由都存在。

### 同类问题（未修，待决策）

对比引擎侧与 market 侧的列表接口，下列页面存在同样的字段缺失，编辑类弹窗有同样风险：

| 接口 | 引擎侧缺失的字段 |
|---|---|
| `detection-providers`（第三方检测站） | 已随本次一并修复 |
| `vendors` | `auth_types`、`supported_auth_types`、`appraisal_base`、`buyback_base`、`extra_limits` |
| `pools` | `min_margin`、`safety_buffer`、`market_description` |
| `probe-items` | `prompt`、`grader`、`request`、`generator`、`models`、`modes` 等 |
| `upstreams` | `appraisal`、`model_whitelist`、`quality_checks`、`proxy_id` 等 |
| `quality/overview` | 字段名完全不同（`platforms` / `degraded_upstreams` 对 `appeals_by_status`） |
| `market-categories` | 引擎侧 94 条拍扁的列表，market 侧 6 条树 |

## 三、配置层问题与修复

### 3.1 旧策略只改顶层字段，新值不进入执行配置（真 bug）

库里 `flows = {}` 的 legacy 策略，请求只带顶层 `models` / `full_triggers` / `scheduled_enabled`，保存成功但执行端跑的还是旧配置。`UpsertPolicy` 先用旧值合成 `flows`，之后才应用顶层补丁。

修复：更新路径改用 `scanPolicyRow` 拿到未经兼容回填的原始 flows 与 `policyFlowPresence`，顶层补丁应用后再对「确实没有配置 flows」的策略做 legacy 回填。回归：`TestUpsertPolicyLegacyTopLevelPatchUsesNewValues`、`TestUpsertPolicyTopLevelPatchPreservesConfiguredFlows`。

### 3.2 显式关闭的降智流程被自动重新启用（真 bug）

`backfillDegradation` 用 `IntervalMinutes == 0` 猜「没有配置降智流程」，分不清「缺失」与「显式关闭」。修复：`PolicyInput` 增加自定义 `UnmarshalJSON`，按 JSON 里是否存在 `flows.degradation` 键判断。显式 0 现在返回 `INVALID_FLOW_INTERVAL`。回归：`TestUpsertPolicyRejectsExplicitZeroDegradation`。

> 正确的「关闭流程」方式是 `enabled=false` 且周期仍 ≥ 10，不是把周期填 0。

### 3.3 库里全 0 的自定义 flows 被 legacy 整体覆盖（真 bug）

旧代码按「三条 interval 全 0」判定 legacy，与 `0033_degradation_flow.sql` 按 JSON 键判定的口径不一致。修复：更新路径按流程键判定并拒绝显式 0。回归：`TestUpsertPolicyRejectsStoredZeroIntervalsWithoutLegacyOverwrite`。

### 3.4 抖动校验口径与调度脱节，报错不可读（真 bug，浏览器验收时发现）

保存校验拿「顶层 `interval_minutes`」去比抖动，而弹窗里**没有这个输入框**；调度时（`engine.go` 的 `min(p.JitterMinutes, interval/2)`）却是按**流程周期的一半**钳位。后果：

- 抖动 5000 报英文 `interval must be >= 10 minutes and jitter smaller than interval`，管理员找不到叫 interval 的字段；
- 测智周期 10 + 抖动 30（默认）保存**成功**，调度时被静默钳位成 5，配置与实际不符。

修复：抖动上限改为「最短流程周期的一半」，与调度同口径；报错改为中文并点明上限与原因。顶层 `interval_minutes` 只保留库约束（≥ 10），不再参与抖动比较。新错误码 `INVALID_JITTER`。

回归（修复前红灯已验证）：`TestUpsertPolicyJitterMustFitShortestFlowInterval`、`TestUpsertPolicyJitterAtHalfOfShortestIntervalIsAccepted`、`TestUpsertPolicyIgnoresLegacyTopLevelIntervalForJitter`。

存量策略不受影响：默认抖动 30、默认最短周期 360（上限 180）。只有管理员主动把某条流程周期调到 60 以下又没同步调抖动时才会被拦，且提示会告诉他上限是多少。

### 3.5 检测站停用后没有可见提示（真 bug，展示层）

列表标「（已停用）」，编辑页给出 `disabled-provider-warning` 警告；后端 `TestUpsertPolicyRejectsDisabledThirdPartyProvider` 守住保存校验。

### 3.6 双层开关展示误导（非 bug）

调度语义是「总检测 + 总定时 + 流程启用 + 流程定时」四层共同决定，链路本身正确。但前端两组复选框互不相干，界面显示「开 · 每 N 分」而实际一轮不跑。现在列表显示「总检测关 / 总定时关」，三个流程的定时框在总开关未开时禁用并有说明文字。

### 3.7 `third_party[].triggers` 是废弃字段（非 bug）

`0030_detection_flows.sql` 已把各站 triggers 聚合进 identity 流程；执行端不读 `site.Applies`。后端字段标注废弃，前端保留回写并注明不参与执行。

### 3.8 真页面独有的 bug

`policyFlows()` 对 DataTable 传入的 Vue 响应式 Proxy 直接 `structuredClone(p.flows)` 会抛 `DataCloneError`，导致已有 flows 的表格行渲染中断。改为 `structuredClone(toRaw(p.flows))`。该问题在死代码里不存在，是首次在真页面上写测试时复现的。

## 四、第三方检测链路

### 4.1 逐环判定

| 环节 | 判定 | 说明 |
|---|---|---|
| 策略 → 运行 | 通 | 身份流程须启用且被本次运行选中，来源冻结为 `third_party` |
| 模型解析 | 有条件通 | 所有模型来源为空时无法提交，现在记录明确失败 |
| 预提交 | 已修复 | 停用/缺失检测站保留 `skipped` 记录；站点列表为空时摘要留下原因 |
| 提交 / 轮询 | 已修复 | 遵守持久化退避与截止时间；检测站级锁保护小时额度；轮询 429 遵守 Retry-After |
| 结论合并 | 通 | 纯第三方身份无内部题时不会被吞成 inconclusive；mismatch 按处置规则判 degraded |
| Provider / 幂等 / 令牌 | 通 | 重入复用同一记录；撤销失败不误标 `revoked` |

### 4.2 修复的 5 个静默失败出口

| 场景 | 原行为 | 现行为 |
|---|---|---|
| 检测站停用 / 缺失 | `continue` 静默跳过，无记录无提示 | 写 `skipped` + 原因，身份流程判 `inconclusive` |
| `third_party = []` | 运行摘要里什么都没有 | `finishRun` 合成一条可见的 skipped 结果 |
| 模型全空 | 空串发给检测站被拒，报错不可读 | `failCheck` 记录明确原因 |
| 每小时额度并发 | 多个 run 可同时越过 `max_runs_per_hour` | 按 provider 加 advisory lock 串行化 |
| 重入 | 绕过 `next_attempt_at` 持久化退避 | 提交前重读 status / attempts / deadline / next_attempt_at |

另外 `bazaarlink.Poll` 的 429 现在识别为 `RateLimitedError`，不再消耗普通失败次数。

### 4.3 会阻断或改变结果的配置组合

**不会提交第三方检测：** `flows.identity.enabled = false`；`flows.identity.source = self`；本次只选测智 / 降智；检测站停用、缺失或 `third_party` 为空（现在会留下可见原因）。

**提交会失败：** 模型来源（`third_party.model` / 运行模型 / 上游 `model_whitelist`）全空；`EdgePublicURL` 未配，或 http 且未开 `allow_insecure_target`；小时额度耗尽或远端 429（退避，超过截止时间后 `timeout`）。

**判定方式：** `on_mismatch` / `on_submodel_mismatch` 取 `degraded`（暂停调度）、`suspected`（标记疑似）、`ignore`（仅记录，折算 normal）。

### 4.4 两个容易误判的点

1. 第三方 degraded **无视 `AutoPause=false`** 直接暂停（`tpCause != ""` 分支）。
2. **health 降级与字母等级是两回事**：第三方失败门槛可能让质量系数为空，此时字母等级保留原值，「等级没变」不等于「第三方没生效」。

## 五、浏览器验收（真实引擎 + 真实后台页面）

环境：隔离端口起真实 sub2api 引擎（自动安装）+ market-api + worker + edge，第三方检测站用 `fake-thirdparty` 替身。

**配置回显**：修复前，编辑弹窗里抖动、预算、测智/降智周期与模型、身份核验全为空/关；修复后全部回显库里的真实值，列表新增「第三方」列显示 `BazaarLink`。

**保存**（以库里的真实数据核对，version 4 → 5）：

| 字段 | 保存前 | 保存后 |
|---|---|---|
| 开启检测 | f | t |
| 检测站处置 on_mismatch | degraded | suspected |
| 检测站模型 | 空 | claude-opus-4-1 |
| 测智模型 / 抖动 / 预算 / h0 | claude-sonnet-4-5 / 30 / 1 / 0.9 | **不变** |

**配置校验**（弹窗内逐条构造非法输入）：

| 用例 | 拦截位置 | 提示 | 评价 |
|---|---|---|---|
| 流程周期 < 10 / 非整数 | 前端 | 检测周期需为至少 10 分钟的整数 | 合理 |
| 测智启用但无模型 | 前端 | 启用测智流程时须配置至少一个模型 | 合理 |
| 身份=第三方但不勾检测站 | 前端 | 第三方身份核验需要选择至少一个已启用的检测站 | 合理 |
| token 预算非法 JSON | 前端 | token 预算需要是 JSON 对象 | 合理 |
| 并发 = 99 | 后端 | 题目并发上限须为 1–8 | 提示合理，前端未拦（见 §七） |
| 抖动 = 5000 | 后端 | 原：英文且不指字段；现：中文、点明上限 | 已修（§3.4） |

**第三方检测执行**：触发只含身份流程的检测，运行 `queued → third_party/running → done`，请求确实提交到检测站并撤销了临时令牌。第二次触发时检测记录用的是弹窗里填的 `claude-opus-4-1 / suspected / anthropic`，对比第一次（种子值 `claude-sonnet-4-5 / degraded`）。

**未能在浏览器环境验证的部分**：本地演示上游没有真实账号（种子里 OAuth 演示完成返回 400），检测站探测请求无账号可用，4 道题全 `error`，结论 `inconclusive`。`mismatch → degraded → 暂停` 这条路径仅有集成测试 `TestThirdPartyPureIdentitySubmitsPollsAndPauses` 覆盖，未在浏览器环境亲见。

## 六、卖家接入指引链接

`web/src/utils/authGuides.ts` 增加 `guideURL` / `guideText`，渲染到接入向导、发布页、OAuth 授权面板。链接实测均返回 200：

| 接入方式 | 链接 |
|---|---|
| Kimi Coding Plan 订阅 | `https://www.kimi.com/code` |
| Kimi For Coding 创建 key | `https://www.kimi.com/code/console` |
| GLM 国内站订阅 | `https://bigmodel.cn/coding-plan/personal/overview` |
| GLM 国内站 API Keys | `https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys` |
| GLM 国际站订阅 | `https://z.ai/subscribe` |
| GLM 国际站 API Keys | `https://z.ai/manage-apikey/apikey-list` |

指引链接放在 OAuth 面板底部：放顶部会被 `wrapper.get('a')` 抢到，导致 3 个既有测试失败。

## 七、待决策与遗留

1. **同类瘦身列表**（§二表格）：`vendors`、`pools`、`probe-items`、`upstreams`、`quality/overview`、`market-categories` 存在同样的字段缺失，编辑类弹窗有同样的覆盖风险。建议逐页核对后统一改走 relay。**待决定**，已在 `AGENTS.md` 标注「尚未处理」。
2. **`market/web/src/views/admin/` 下的旧控制台文件**（27 个 `.vue` 中仅 `OpenEngineView.vue` 在用）：只有测试引用，不进构建产物，却有数十个测试在维护它。**已决定先记录、暂不删除**：说明写在仓库根 `AGENTS.md`、该目录的 `README.md` 和 `market/README.md`，供协作者与 agent 避免改错位置。删除与否之后再定。
3. **并发输入框**只有 `min="1"` 没有 `max="8"`，也不在前端校验，靠后端兜底。提示可读，不阻断。**已决定暂不加**。
4. **顶层总开关语义未改**：四层共同决定调度；若希望流程脱离总开关独立运行需改执行引擎。
5. **历史非法全 0 数据未清洗**：只阻止了被静默重写，读取侧仍保留兜底。
6. **`ThirdPartyView`（检测站页）仍允许停用正被策略引用的检测站**，目前只在策略页暴露风险。
7. **`Tick` 中 `scheduleDue` 失败会直接 return**，该轮 `advanceThirdParty` 不推进。

## 八、验证

| 命令 | 结果 |
|---|---|
| `cd market && go build ./... && go test ./internal/quality/...` | 通过 |
| `cd backend && go build ./... && go test ./internal/idlecat/ ./internal/server/routes/` | 通过 |
| `cd frontend && npx vitest run src/market-admin/__tests__/PoliciesView.spec.ts` | 10/10 通过 |
| `cd frontend && npx vue-tsc --noEmit` | 通过 |
| `cd market/web && npm run test -- --run` | 271/271 通过 |
| `cd market/web && npx vue-tsc --noEmit` | 通过 |
| 浏览器验收（真实引擎 + 真实后台页面） | 通过，见 §五 |

另：`market/scripts/dev.sh` 修了三处环境问题，使本地能起真实引擎做验收：端口与容器名可覆盖（`DEV_PG_PORT` 等，默认值不变）、真实引擎首次启动自动安装（`AUTO_SETUP`）、合规确认语句与引擎当前文案对齐、第 343 行全角括号紧跟变量导致的 `unbound variable`。
