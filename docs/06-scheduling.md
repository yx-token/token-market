# 06 调度

调度分两层：

- **sub2api 负责秒级调度**：选账号、负载均衡、粘性会话、遇到 429 / 5xx 熔断和故障转移。
- **本系统负责分钟级调度**：决定每个上游进哪些分组、在哪个优先级段、账号级参数取什么值，并通过 Admin API 下发。

本系统不改 sub2api 代码，所以只能使用 sub2api 已有的参数来表达调度意图。全部调度功能在 token-market 侧实现，通过现有 Admin API、只读数据库与配置驱动 sub2api；现有接口无法表达的能力记为“依赖 sub2api，后续处理”。

质检调度已实现，默认仍采用原策略。只有 `inspection.mode=formal` 且独立调度开关开启时才使用新增准入与排序；详细字段、回退规则与校准要求见[质检数据契约](research/inspection/data-contract.md)。

## 1. 默认分组结构

每个模型族一个默认分组，另有一个 composite 全模型分组：

| 默认分组 | sub2api 平台 | 成员 |
|---|---|---|
| `tm-pool-claude` | anthropic | Claude 上游 |
| `tm-pool-openai` | openai | ChatGPT / Codex 上游 |
| `tm-pool-gemini` | gemini | Gemini 上游 |
| `tm-pool-antigravity` | antigravity | Antigravity 上游 |
| `tm-pool-grok` | grok | Grok 上游 |
| `tm-pool-kimi` / `-zhipu` / `-minimax` / `-opencode` | 对应平台 | Coding Plan 上游 |
| `tm-pool-all` | composite | 以上全部，按请求模型路由到具体平台 |

每个默认分组：售价倍率 D 由管理员设定，开启 sub2api 分组利润控制（最低毛利、安全缓冲与本系统 `pools` 表一致）。

## 2. 成员规则与优先级分段

| 条件 | 进入默认分组 | 优先级段 |
|---|---|---|
| 回收挂单 `active`，健康状态允许调度 | 是 | **回收段 1–19** |
| 寄售挂单 `active`，健康状态允许调度，卖家未退出默认分组，且 m_s ≤ D × (1 − 最低毛利 − 安全缓冲) | 是 | **寄售段 50–79** |
| 其他 | 否 | — |

"健康状态允许调度"指 `healthy`、`suspected`、`limited`；`unknown`（尚未通过上架检测）一律不调度（见 [04](04-listing.md) 第 4.2 节、决策 D18）。

sub2api 的 Anthropic 网关和 OpenAI 普通调度都是严格按优先级分层的：回收段的账号全部不可用或满载，才会用到寄售段。这就是"默认分组优先走回收卖家，再走倍率更低的寄售卖家"。

倍率条件由本系统在维护成员时检查，sub2api 的利润控制在运行时再兜底一次，防止改价和调度之间的时间差。

**段内排序（第一期默认策略）**

- 回收段：按质量等级，S → 1、A → 5、B → 10、C → 15。
- 寄售段：按倍率分档，便宜的档位优先，例如每 0.05 一档。

不要给每个账号设不同的优先级，否则会把排在最前的账号用到满才换下一个。同档内由 sub2api 按负载率和"窗口最快重置优先"（`prefer_soonest_reset`）自然分摊。

**寄售专属分组**（`tm-l-<listing_id>`）只有一个账号，优先级在其中没有作用。

### 2.1 保障池准入

正式启用质检调度后，默认分组作为保障池，在现有挂单状态、健康状态、卖家加入意愿和毛利上限条件上，再要求**等级达到 `inspection.scheduling.minimum_grade`（默认 B）且无已确认的门槛事件**。准入与回收段排序使用同一个有效等级：优先读取 `upstreams.grade`，为空时取按接入方式匹配的策略 `default_grade`；默认等级也不能绕过健康限制或确认门槛。这是兼容旧未检测记录的兜底，不表示已经取得有效 Q 或质检估价。此检查位于 `Plan` 的默认分组成员循环，不用于限制卖家设定寄售倍率。

买家指定的寄售专属分组不受这一新增等级准入限制；账号原有健康限制仍适用，`degraded` / `unavailable` 等不能继续面向买家。不能因专属分组不检查最低等级，就绕过门槛事件引起的健康暂停。

| `inspection.mode` | `inspection.scheduling.enabled` | 等级、默认分组准入与排序 |
|---|---|---|
| `trial`（默认试运行） | false（默认）或 true | 新质检只展示；等级、调度继续现有逻辑，规划器输出与旧版一致 |
| `formal`（正式） | false | 等级由 Q 决定；新增准入和策略关闭，规划器对相同输入的输出与现状完全一致 |
| `formal`（正式） | true | 在现有条件上启用保障池准入及 `quality_adjusted` 策略 |

正式模式的等级变化属于质检输入变化；“调度开关关闭时输出一致”指规划器没有额外读取 Q 改排序或施加新准入条件。开关关闭不豁免既有健康暂停。两个开关均放在 settings 的独立 `inspection` 键，不加入旧的 `platform` 整包设置。

## 3. 账号级参数

以下参数是账号级的，在该账号所属的所有分组里取值相同：

| 参数 | 来源 |
|---|---|
| `priority` | 所在优先级段和段内排序 |
| `concurrency` | 按厂商和订阅档位的默认值（管理员配置），再按质量评分调整 |
| `load_factor` | 默认不设；需要在同档内调整分摊比例时使用 |
| `credentials.account_scheduling_threshold` | 100 − 卖家自留比例 |
| `extra.window_cost_limit` / `base_rpm` / `max_sessions` | Claude 账号按档位的默认值，降低被官方风控的概率 |
| `extra.quota_daily_limit` 等 | Coding Plan 账号可选的每日花费上限 |
| 分组成员 | 第 2 节的规则；不调度时只保留探测分组 |

各厂商、各档位的默认并发和限流值放在管理后台配置，上线前按实际封号情况调整。

## 4. 调度策略插件

实际策略接口只负责段内优先级；默认分组准入、健康限制和期望账号状态由 `Plan` 统一计算，执行器负责落地到 sub2api：

```go
type Strategy interface {
    Name() string
    BuybackPriority(u UpstreamInput, grade string) int
    ConsignmentPriority(u UpstreamInput, multiplier float64) int
}
```

默认 `DefaultStrategy` 的名称为 `grade_first+price_bucket`：回收段按等级，寄售段按价格档；未提供策略时由 `Plan` 选择它。结果由规划器限制在回收段 1–19、寄售段 50–79。账号健康为 `suspected` 时，并发减半规则继续保留。

`quality_adjusted` 实现同一个接口。回收段继续按确认等级排序；寄售段使用有效价格 `m_s / Q`，每 0.05 一档，优先级为 `50 + floor((m_s / Q) / 0.05)`，仍限制在 50–79，不跨到回收段。计算时沿用现有浮点边界容差，避免整档倍率落入前一档。例如倍率均为 0.30，Q=1 的账号落在 56，Q=0.75 的账号落在 58；卖家实际倍率和买家费用均不因此改动。

`LoadPlanInputs` 读取 token-market 的 `upstreams.quality_factor` 与最近正式检测的门槛事件；`appraised_at` 为空的未检测账号按 1 兼容排序，无效 Q 也按 1 保护除法，不能把该值展示成“已通过质检”或有效估价。门槛按健康状态和保障池准入排除，不以 Q=0 参加除法；字段与历史缺失规则见[数据契约](research/inspection/data-contract.md#2-新增列)。新策略只在正式模式且调度开关启用时选择，关闭时仍用原策略。单元测试用相同输入逐字段比较全部规划输出，覆盖 trial 下两种开关值及 formal 下关闭开关的兼容性。

探索-利用（给新上游保底流量）、公平分配、按目标份额的闭环控制仍为后续方向，本次不增加按默认分组配置多个策略的机制。

## 5. 执行器

执行器比较期望状态（`pool_assignments`、`upstream_runtime_params`）与 sub2api 实际状态，生成写操作写入 `sub2api_ops` 出站表，由发送器按顺序调用 Admin API。

| 机制 | 说明 |
|---|---|
| 死区 | 参数变化小于阈值不下发，避免抖动 |
| 变更限速 | 每分钟最多改 N 个账号。每次账号变更都会触发 sub2api 重建调度快照，过于频繁会造成重建风暴 |
| 批量下发 | 多个账号要改成同样的值时，用 `/admin/accounts/bulk-update` 一次下发 |
| `extra` 修改 | 用 bulk-update（jsonb 合并写入）；`PUT /admin/accounts/:id` 会整体替换 extra，必须先读后写 |
| 优先级 | 不调度（安全相关）的变更插队优先发送，普通参数调整排后 |
| 幂等 | 创建类操作带 `Idempotency-Key`，失败重试不会重复创建 |

## 6. 状态对账

对账器每 1–5 分钟从 sub2api 只读库读取所有 `tm-` 资源（账号、分组、分组成员、用户、key）：

- **漂移**：实际值与期望值不一致（例如有人在 sub2api 后台手动改过），生成修正操作并记审计日志。
- **孤儿资源**：sub2api 中存在、本系统没有记录的 `tm-` 资源，告警，由管理员确认后清理，不自动删除。
- **丢失资源**：本系统有记录、sub2api 中不存在，告警并标记相关上游为 `unavailable`。
- **sub2api 自动状态**：账号被 sub2api 标为 error、限流、过载等，只读取作为健康信号，不去覆盖。这些状态由 sub2api 自己管理和恢复。

## 7. 安全模式

兼容自检发现 sub2api 版本不在已验证列表中时（见 [09](09-sub2api-integration.md)），执行器和对账器停止一切写操作，只保留读取。买家请求不受影响，sub2api 按最后一次下发的配置继续调度。

## 8. 已知限制

- 同一账号同时在默认分组和寄售专属分组里时，无法让"指定该卖家的买家"优先于默认分组流量，二者共享该账号的并发。
- 优先级段是严格分层，不是按比例分流。需要按比例分流时，只能在同一段内用 `load_factor` 粗略调节，高负载时近似成比例，低负载时近似轮询。
- OpenAI 高级调度必须保持关闭，否则优先级段失效；代价是 OpenAI 上游用不上 sub2api 高级调度里的错误率、首字延迟打分。
- `prefer_soonest_reset` 对 sub2api 通用网关路径生效；OpenAI 原生调度路径是否生效，实现时确认。
