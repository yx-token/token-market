# 站内更新

管理后台「系统更新」提供当前版本、最新正式 Release、发布说明、确认更新和任务进度。更新包包含 api、worker、edge、s2a-compat 与前端，三个运行服务切换至同一版本；不会更新 sub2api。

## 现有部署接入

旧版本没有更新包及启动器，需要先通过镜像部署一次包含本功能的版本，并采用新的 `deploy/docker-compose.yml`。单机部署同步更新 `market-src/deploy/docker-compose.yml`，保留现有 `market.override.yml` 和密钥。

1. 固定 `TM_IMAGE` 为新版本，按原流程登录镜像仓库。
2. 在 `.env`（单机部署为 `market.env`）设置 `UPDATE_REPOSITORY=yx-token/token-market`。私有仓库设置 `UPDATE_GITHUB_TOKEN`，使用该仓库 Contents 读取权限的 fine-grained PAT，或具有私有仓库读取权限的 classic PAT。`docker login` 的凭据不能代替此令牌。
3. 使用新 Compose 执行 `pull`、`up -d`。三个服务均以 `market-runner` 启动，并挂载同一个 `runtime:/app/runtime` 卷。
4. 管理员访问 `/admin/system-update`。未来正式 Release 提供当前架构更新包后，可以在此检查并确认更新。

Release 工作流从镜像的 `release` 构建阶段导出 `token-market_linux_amd64.tar.gz`、`token-market_linux_arm64.tar.gz` 和 `checksums.txt`，无需运行另一种架构的程序。预发布版本不会作为站内更新目标。旧 Release 缺少更新附件时，界面禁用更新并提示通过镜像升级。

## 更新与恢复流程

- API 重新查询最新正式 Release，确认版本与页面检查结果一致，并拒绝降级、重复提交和并发更新。
- 更新任务在后台下载完整包及校验文件，必须通过 SHA-256 校验，再检查平台、版本、协议和必要文件。解压拒绝目录穿越、链接和特殊文件，并限制下载与解压大小。
- 包在共享卷内解压至独立版本目录，完成后提交版本切换任务。稳定启动器原子切换 `current` 符号链接，为旧进程发送 SIGTERM，等待退出后启动新进程。前端路径同时切换。
- api、worker、edge 各自检查本机健康接口并记录健康状态；api 启动器协调结果。三个服务在 90 秒内恢复健康才算成功，否则切回旧版本并再次检查。新版 API 启动失败时，独立启动器仍可完成回退。
- 任务存入 `job.json`，API 重启不会丢失状态。下载被重启中断时标记失败，可重新提交。浏览器遇到暂时断连会自动重试；成功后点击「刷新页面，加载新版本」。

这是一种短暂重启的在线升级，进行中的模型请求可能中断。升级前阅读发布说明并备份数据库。启动时仍按现有规则执行数据库迁移；程序回退不撤销迁移，因此有不兼容迁移的版本必须通过人工维护窗口升级。

## 持久化与镜像升级

共享卷保存完整版本目录、当前指针和最近任务。容器重建或宿主机重启后继续运行卷内已更新版本。部署更高版本镜像时，新镜像版本优先；重新部署旧镜像不会自动覆盖卷内更高版本。不要删除共享卷来回退，卷中还保留了可恢复版本。

自动回退失败时，检查 `docker compose logs api worker edge`，先恢复数据库、Redis 和配置；程序已尝试切回旧版本，恢复依赖后可重启服务。

需要人工指定版本时，先停止三个服务，再通过同镜像的临时容器切换共享指针。例如旧目录为 `releases/<旧版本目录>`：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env stop api worker edge
docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --no-deps --entrypoint /bin/sh api
# 在临时容器内执行；必须选择已经存在的完整版本目录。
cd /app/runtime
ls releases
ln -s releases/<旧版本目录> current.next
mv -f current.next current
# 清除最近事务，防止启动器继续未完成的切换；保留版本目录。
rm -f job.json
exit
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d api worker edge
```

手动回退时同时将 `TM_IMAGE` 固定到相应旧版本或更早的兼容版本，避免启动器优先采用更高版本镜像。`runtime/releases` 会保留历史目录；空间不足时先停止更新，确认当前指针和 `job.json` 中的 target/previous，只有不再用于运行或回退的目录才能清理。

## 不使用 Docker

直接运行 `bin/api`、`bin/worker`、`bin/edge` 仍然可用，但管理页显示站内更新未启用。启用需准备与更新包相同的安装布局：

```text
/opt/token-market/
  manifest.json       schema=1、version、commit、os=linux、arch=amd64 或 arm64
  bin/                api、worker、edge、s2a-compat、market-runner
  web/                前端构建产物
```

为三个进程配置同一个绝对路径 `TM_UPDATE_RUNTIME_DIR`（同一用户拥有读写权限），从进程管理器分别启动 `bin/market-runner api --config ...`、`bin/market-runner worker --config ...`、`bin/market-runner edge --config ...`。更新配置支持 YAML `update.repository/github_token/runtime_dir` 或 `TM_UPDATE_REPOSITORY`、`TM_UPDATE_GITHUB_TOKEN`、`TM_UPDATE_RUNTIME_DIR`；访问令牌只配置给 api。

当前协议用于同一宿主机上的一套 api/worker/edge。多副本或多宿主机部署应继续通过统一镜像滚动发布。稳定启动器与运行时基础镜像不在热更新包内，协议或系统依赖变化时使用镜像升级。
