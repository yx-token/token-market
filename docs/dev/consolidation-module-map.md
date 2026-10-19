# 并仓模块地图（草稿）

对照 [ADR 0001](../adr/0001-sub2api-market-consolidation.md) 与 [实施计划](../superpowers/plans/2026-10-19-sub2api-market-consolidation.md)。

**宿主：** `../github-fork/sub2api` @ `market/consolidation`（完整社区树 + 顶层 `market/`）  
**过渡仓：** 本仓库；模块迁入宿主前仍以本仓为权威实现。

| token-market 包 | 优先级 | 迁入宿主路径 | 备注 |
|---|---|---|---|
| （社区全树） | P0 | `backend/` `frontend/` `deploy/` … | **已在宿主，禁止裁剪** |
| `internal/sub2api/gateway` | P0 | `market/internal/sub2api/gateway` | 替换点：HTTP → 进程内 |
| `internal/sub2api/ops` + `opshandlers` | P0 | `market/internal/sub2api/...` | 过渡期保留 |
| `internal/listing` | P1 | `market/internal/listing` | 先搬只读与上架；写账号仍走出站 |
| `internal/scheduling` | P1 | `market/internal/scheduling` | 执行器继续调 Port |
| `cmd/api|worker|edge` | P1 | `market/cmd/...` | 与社区 `backend/cmd` 并列 |
| `internal/buyer` + `edgecfg` | P2 | `market/internal/buyer` 等 | 影子用户暂留 |
| `internal/edge` | P2 | `market/internal/edge` | 勿先改进社区扣费路径 |
| `internal/quality` + `inspection` | P3 | `market/internal/quality` 等 | |
| `internal/billing` + `payment` | P4 最后 | `market/internal/billing` 等 | |
| `web/` | 贯穿 | `market/web` | **主 UI**：保留闲置TOKEN/控制台风格；社区 `frontend/` 并列保留但不替换本产品后台 |
| `migrations/` | 贯穿 | `market/migrations` | 市场库，可同实例不同 schema |

**禁止直接修改（社区合并敏感）：** `backend/internal` 下计费、余额、usage 入账、OAuth 刷新——变更落在 `market/` 或经 Admin 端口。

**双前端：** 主站 = `market/web`；社区 `frontend/` 内网对照。详见宿主 `market/DEPLOY.md`。  
**退役：** 实施计划「过渡仓退役条件」全部勾选前，生产发版仍以本仓为准。
