# Edge ↔ 引擎契约（P1）

> 对应设计：`docs/superpowers/specs/2026-10-22-c2c-engine-edge-fusion-design.md` 方案 1。  
> 状态：契约已定；智能调度策略（保障池优先吃回收）在 P2 落地，本文件先固定边界与可观测字段。

## 1. 分工

| 组件 | 负责 | 不负责 |
|------|------|--------|
| **Edge** | 买家 Key 鉴权；**调度决策**（选哪一跳供给 / 是否走活动包 Key）；故障转移；记录 `edge_attempts`；对买家暴露统一 `/v1` | 上游协议细节；引擎账号池内选号；人民币账本入账 |
| **引擎** | 用内部 Key 鉴权；组内选上游账号；协议转换；打上游；引擎侧额度扣减；写 `usage_logs` | 买家 tm-Key；C2C 商品/回收业务规则 |
| **Worker** | `usage_logs` → `usage_records` → 市场账本；与 `edge_attempts` 对账 | 实时同步扣买家余额（热路径） |

```text
买家 ── tm-Key /v1 ──► Edge（SchedulePlan → 换内部/活动包 Key）──► 引擎 GatewayURL+同路径 ──► 上游
                         │                                      │
                         └─ edge_attempts                       └─ usage_logs
                                      └─ worker 对账 / 入账 ─┘
```

## 2. 调度策略（版本）

| Policy ID | 含义 | 阶段 |
|-----------|------|------|
| `ordered-failover` | 按买家 Key 上 hops 顺序尝试；跳过 skip-list / 熔断；可重试失败则下一跳 | 无 pool hop 时 |
| `pool-buyback-first` | 路由含保障池 hop 时声明此策略；**账号级**回收优先由引擎 `scheduling.Plan`（buyback priority 1–19）执行，Edge 不拆池内账号 | **P2 已接线** |

Edge 在成功进入转发循环前设置响应头（若尚未写 body）：

- `X-TM-Schedule-Policy: ordered-failover`

已有可观测头（保持）：

- `X-TM-Request-ID` — Edge 生成，贯穿 attempts
- `X-TM-Listing` / `X-TM-Hop` — 实际命中跳（由 forward 设置）

## 3. 转发约定

- **URL**：`Sub2API.GatewayURL` + 买家原始 path + query
- **鉴权改写**：同一位置（Bearer / x-api-key / …）把 `tm-*` 换成解密后的内部 Key 或活动包 Key
- **不**在买家热路径调用引擎 Admin API
- 账号级选择仍在引擎组内完成；Edge 选的是 **hop（listing/pool → 内部 Key）**，不是引擎 `accounts.id`

## 4. 用量回传

1. Edge：异步写 `edge_attempts`（含 `edge_request_id`、hop、listing/pool、`sub2api_request_id`、是否 billable）
2. 引擎：成功用量进 `usage_logs`
3. Worker Ingester：复制/归因后入市场账本；Reconciler 用 request id 对齐 attempts

Edge **不在**请求返回前写人民币账本。

## 5. 扩展点

- `BuildSchedulePlan` / `SelectSchedulePolicy` 是唯一计划入口。
- 池内回收优先依赖引擎调度写入账号优先级，而非 Edge 多 hop 展开（买家不可见回收商品）。

## 6. 验收

- [x] 本文档入库
- [x] `ordered-failover` 经 `BuildSchedulePlan` 产出，行为与改造前一致
- [x] 响应带 `X-TM-Schedule-Policy`
- [x] P2：含 pool hop 时 `X-TM-Schedule-Policy=pool-buyback-first`；处置互斥 `DISPOSITION_ACTIVE`
