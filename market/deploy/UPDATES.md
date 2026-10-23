# 站内更新

## 单进程模式（推荐，`MARKET_EMBED=1`）

管理后台「系统更新」走宿主接口 `/api/v1/admin/system/*`，从本仓 Release（默认 `UPDATE_GITHUB_REPO=yx-token/token-market`）下载 **`sub2api_*` 宿主包**：

- 替换宿主二进制（内嵌管理端 `/admin` + `/admin-assets`）
- 若包内含 `market-web/` 且设置了 `TM_HTTP_WEB_DIR`，同步 C 端静态
- **不会**自动更新独立 edge 容器；请同时把 `market.env` 的 `TM_IMAGE` 升到同一版本并 `./tm.sh market pull && up -d`

Docker 部署也可不点站内更新，直接改 `ENGINE_IMAGE` / `TM_IMAGE` 后 pull + up。

更新完成后按提示重启宿主进程（或 `./tm.sh sub2api up -d`）。

## 双进程 / 独立 market-api（legacy）

管理页若仍走市场包更新（`/api/admin/system/update`），更新包含 api、worker、edge、s2a-compat 与前端（C 端和 `web/admin-spa`），**不**更新闲猫宿主。详见下文「现有部署接入」。

旧版单机若仍跑独立 `api`/`worker`，用 `./tm.sh market --profile legacy-split up -d`，并关闭宿主 `MARKET_EMBED`。

## 现有部署接入（市场包热更新）

旧版本没有更新包及启动器，需要先通过镜像部署一次包含本功能的版本，并采用新的 `deploy/docker-compose.yml`。单机部署同步更新部署目录中的 `docker-compose.market.yml`（见 [单机部署](single-host/README.md)），保留现有 `market.override.yml` 和密钥。

1. 固定 `TM_IMAGE` 为新版本，按原流程登录镜像仓库。
2. 在 `.env`（单机部署为 `market.env`）设置 `UPDATE_REPOSITORY=yx-token/token-market`。私有仓库设置 `UPDATE_GITHUB_TOKEN`。
3. 使用新 Compose 执行 `pull`、`up -d`。三个服务均以 `market-runner` 启动，并挂载同一个 `runtime:/app/runtime` 卷。
4. 管理员访问市场侧系统更新页。正式 Release 提供当前架构更新包后，可以在此检查并确认更新。

Release 工作流从镜像的 `release` 构建阶段导出 `token-market_linux_amd64.tar.gz`、`token-market_linux_arm64.tar.gz` 和 `token-market_checksums.txt`（与宿主 `sub2api_*` / `checksums.txt` 同在一个 Release）。预发布版本不会作为站内更新目标。

## 更新与恢复流程（市场包）

- API 重新查询最新正式 Release，确认版本与页面检查结果一致，并拒绝降级、重复提交和并发更新。
- 更新任务在后台下载完整包及校验文件，必须通过 SHA-256 校验。
- 包在共享卷内解压至独立版本目录，完成后提交版本切换任务。稳定启动器原子切换 `current` 符号链接。
- api、worker、edge 在 90 秒内恢复健康才算成功，否则切回旧版本。

这是一种短暂重启的在线升级。升级前阅读发布说明并备份数据库。程序回退不撤销迁移。

## 持久化与镜像升级

共享卷保存完整版本目录、当前指针和最近任务。部署更高版本镜像时，新镜像版本优先。

需要人工指定版本时，先停止服务再切换共享指针（见旧版操作说明）。手动回退时同时将 `TM_IMAGE` / `ENGINE_IMAGE` 固定到相应旧版本。
