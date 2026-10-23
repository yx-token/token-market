# 管理端市场 API 规范路径（P4）

> 前端 `market-admin` 视图一律使用 `MARKET_API_PREFIX`（`/api/v1/admin/market`）。  
> `api.ts` 的 `resolvePath` 仅保留 `/api/admin/*` 兼容 shim（含 `sub2api/status` → `status`）。  
> Native IdleCat（`backend/internal/idlecat` + `routes/market.go`）只挂**规范路径**，不再注册同义别名。

## 读接口（规范）

| 能力 | 规范后缀（接在 `/api/v1/admin/market/`） |
|------|------------------------------------------|
| 状态 | `status` |
| 厂商 | `vendors` |
| 融合账号/分组 | `supply/accounts`, `supply/groups` |
| 池 / 代理 / 上游 | `pools`, `proxies`, `upstreams` |
| 资金列表 | `recharges`, `refunds`, `withdrawals` |
| 申诉 | `appeals` |
| 检测策略/第三方 | `detection-policies`, `detection-providers` |
| 题库/记录 | `probe-items`, `probe-runs` |
| 分类 | `market-categories` |
| 质量 | `quality/overview` |
| 支付 | `payments/status`, `payments/merchants` |
| 设置 / 热搜 | `settings`, `market-discovery` |
| 身份 | `identities` |
| 质检项 | `inspection/checks`（relay） |

## 已删除的别名（勿再用）

`policies` · `providers` · `categories` · `quality` · `payments` · `hot-searches`

## 双实现说明（尚未物理合并）

| 层 | 职责 |
|----|------|
| **引擎进程 + idlecat** | 管理端列表/开关的主读路径；设置扁平化；部分写 native，复杂写 relay 到 market 包 |
| **market 进程 / 包** | C 端、Edge、Worker、完整 httpapi；standalone 部署仍可用旧路径 |
| **收敛方向** | 管理端只认上表；目录合并属后续，不阻塞产品面 |

相关：`docs/dev/edge-engine-contract.md`、设计 `docs/superpowers/specs/2026-10-22-c2c-engine-edge-fusion-design.md`。
