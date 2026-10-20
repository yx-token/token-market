# 并仓模块地图

对照 [ADR 0001](../adr/0001-sub2api-market-consolidation.md) 与 [实施计划](../superpowers/plans/2026-10-19-sub2api-market-consolidation.md)。

**宿主：** `../idlicat`（完整社区 sub2api + 顶层 `market/`；含市场 deploy/CI）  
**过渡仓：** 本仓库；迁入完成并满足退役条件前，本仓仍是生产权威实现。

| 来源 | 优先级 | 宿主路径 | 说明 |
|---|---|---|---|
| 社区全树 | P0 | `backend/` `frontend/` `deploy/` `docs/` … | 已在宿主，禁止裁剪 |
| `internal/sub2api/**` | P0 | `market/internal/sub2api/**` | 含 gateway/ops/opshandlers/adminclient/userclient/replica/provision/compatguard/apiwire |
| `internal/platform/**` | P0 | `market/internal/platform/**` | 配置、加密、DB、鉴权 |
| `migrations/` | P0 | `market/migrations/` | 市场库 |
| `internal/listing` | P1 | `market/internal/listing` | 上游、挂单、代理、厂商、活动包商品、分类 |
| `internal/scheduling` | P1 | `market/internal/scheduling` | 默认池规划与对账 |
| `internal/market` | P1 | `market/internal/market` | 市场列表、discovery、官方价 |
| `internal/settings` | P1 | `market/internal/settings` | 抽佣、冻结等 |
| `internal/account` | P1 | `market/internal/account` | 用户、实名 |
| `cmd/api` `cmd/worker` `cmd/edge` | P1 | `market/cmd/...` | 与社区 `backend/cmd` 并列 |
| `internal/buyer` `edgecfg` | P2 | `market/internal/buyer` `edgecfg` | 影子用户、Key、商家清单 |
| `internal/edge` | P2 | `market/internal/edge` | 链式路由；不改社区扣费 |
| `web/` | P2 | `market/web` | **主 UI**，风格原样保留 |
| `internal/iam` | P2 | `market/internal/iam` | 组织/角色/菜单 |
| `internal/quality` `inspection` | P3 | 同名路径 under `market/internal/` | 质检与估价 |
| `internal/risk` | P3 | `market/internal/risk` | |
| `internal/compat` `cmd/s2a-compat` | P3 | `market/internal/compat` `market/cmd/s2a-compat` | 跟社区升级闸门 |
| `internal/update` | P3 | `market/internal/update` | 站内更新 |
| `internal/billing` `payment` | P4 | `market/internal/billing` `payment` | 最后迁 |
| `cmd/fake-sub2api` `fake-thirdparty` `internal/testutil` `internal/e2e` | P4 | `market/cmd/...` `market/internal/...` | 开发与回归 |
| `deploy/` `scripts/` | P4 | `market/deploy` `market/scripts` | 市场进程部署；社区 deploy 保留 |

**禁止：** 为迁功能而删改 `backend/internal` 计费、余额、usage、OAuth 刷新。  
**双前端：** 主站 = `market/web`；社区 `frontend/` 内网。见宿主 `market/DEPLOY.md`。  
**退役：** 实施计划「功能全覆盖清单」全部勾选，且退役条件满足，才停本仓发版。
