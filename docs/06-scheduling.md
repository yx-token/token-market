# 06 调度

调度分两层：

- **sub2api 负责秒级调度**：选账号、负载均衡、粘性会话、遇到 429 / 5xx 熔断和故障转移。
- **本系统负责分钟级调度**：决定每个上游进哪些分组、在哪个优先级段、账号级参数取什么值，并通过 Admin API 下发。

本系统不改 sub2api 代码，所以只能使用 sub2api 已有的参数来表达调度意图。

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

策略只负责计算期望状态，不关心如何落地到 sub2api：

```go
type Strategy interface {
    Name() string
    // Assign 为一个默认分组计算期望的成员和参数
    Assign(ctx context.Context, pool PoolState, candidates []UpstreamState) ([]Assignment, error)
}

type UpstreamState struct {
    UpstreamID   int64
    Mode         ListingMode // consignment / buyback
    Multiplier   float64
    Grade        string
    Health       Health
    Tier         string
    WindowUsage  WindowUsage // 5h / 周用量与重置时间
    Recent       Metrics     // 成功率、首字延迟、429 率
}

type Assignment struct {
    UpstreamID  int64
    Band        Band   // buyback / consignment
    Priority    int
    LoadFactor  *int
    Concurrency int
    Reason      string // 写入审计，便于解释"为什么这个账号排在这里"
}
```

第一期实现两个策略：`grade_first`（回收段）和 `price_bucket`（寄售段）。后续可以增加探索-利用（给新上游保底流量）、公平分配、按目标份额的闭环控制等策略，每个默认分组单独配置使用哪个策略。

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
