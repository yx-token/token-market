# 并仓模块地图（草稿）

对照 [ADR 0001](../adr/0001-sub2api-market-consolidation.md) 与 [实施计划](../superpowers/plans/2026-10-19-sub2api-market-consolidation.md)。

目标仓以 sub2api fork（基线 v0.2.14）为底座；市场代码可先留在本仓，经 `gateway.Admin` 访问引擎。

| token-market 包 | 并仓优先级 | 目标方向 | 备注 |
|---|---|---|---|
| `internal/sub2api/gateway` | P0 已建 | 永久端口 | 替换点：HTTP → 进程内 |
| `internal/sub2api/ops` + `opshandlers` | P0 保留 | 过渡期保留 | 拆除仅在硬 fork 后评估 |
| `internal/listing` | P1 | `market/listing` | 先搬只读与上架状态机，写账号仍走出站 |
| `internal/scheduling` | P1 | `market/scheduling` | 执行器继续调 Port |
| `internal/buyer` + `edgecfg` | P2 | `market/buyer` | 影子用户暂留 |
| `internal/edge` | P2 | 同仓旁路或网关插件 | 勿先改进 sub2api 扣费路径 |
| `internal/quality` + `inspection` | P3 | `market/quality` | worker 同仓 |
| `internal/billing` + `payment` | P4 最后 | `market/billing` | 独立账本边界设计后再动 |
| `internal/httpapi` + `web` | 贯穿 | `market/api` + `market/web` | API 兼容层 |
| `internal/account` + `iam` + `risk` | P2–P3 | `market/iam` | |

**禁止直接修改（社区合并敏感）：** sub2api `backend/internal` 下计费、余额、usage 入账、OAuth 刷新核心路径——变更应落在新 `market/` 目录或经 Admin 端口。
