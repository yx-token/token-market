# 单进程合并：宿主内嵌市场（P5）

## 目标

闲猫对外是**一个系统、一个控制面进程**：点站内更新升全站控制面；不再依赖独立 `market-api` / `market-worker` 容器。

Edge（买家模型流量，另端口）可暂时同镜像独立进程；后续可再并。

## 分期

| 阶段 | Done |
|------|------|
| **P5a** | ✅ 宿主挂载 `HandlerEmbed`；`enginehost.PublicHandler`；frontend bypass `/admin*`；镜像含 `/app/market-web` + `VITE_ENGINE_HOST=1` |
| **P5b** | ✅ `workerloop` + `enginehost.StartWorker`（`MARKET_EMBED_WORKER=0` 可关） |
| **P5c** | ✅ single-host：宿主 `MARKET_EMBED=1` 暴露 8080；`api`/`worker` 进 `legacy-split` profile；只留 edge |
| **P5d** | ✅ `UPDATE_GITHUB_REPO` 默认本仓；优先 `sub2api_*` 包；热更新可解压 `market-web/`；release 构建 ENGINE_HOST 管理端 + market-web 入镜像；nginx 放开 update |

## P5a 约定

- 复用 `market/enginehost` 已有 `app.Build` 单例。
- `httpapi.HandlerEmbed`：不反代 `/api/v1`、不挂市场 `/admin` SPA（宿主 frontend 继续管管理端）。
- 管理端市场运营仍走已有 `/api/v1/admin/market/*`（idlecat + relay）。
- 环境：宿主已有 `MARKET_DATABASE_DSN` + 与 market 一致的 `TM_*` / `CRYPTO_*` / `JWT` 等（见 single-host README）。

## 非目标（本片）

- 不把 Edge 并进同端口。
- 不物理删除 `market/cmd/api`（standalone 部署仍可用）。
