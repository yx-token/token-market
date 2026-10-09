# 单机部署：sub2api + token-market

同一台服务器上用 docker compose 运行 sub2api（底层网关）与 token-market。生产服务器上的目录
`/root/token-market` 就是按本目录的结构部署的：

```
/root/token-market/
  tm.sh                 运维入口（见下文）
  market.override.yml   token-market 加入共享网络 token-market
  market.env            token-market 配置与密钥（权限 600，不进仓库）
  market-src/           token-market 源码快照（服务器上构建镜像用）
  sub2api/
    docker-compose.yml  sub2api 0.2.14 + PostgreSQL + Redis + 对外入口 nginx
    nginx-admin.conf    对外入口：禁用在线升级 / 回滚接口
    .env                sub2api 密钥（权限 600，不进仓库）
```

## 端口

| 端口 | 服务 | 说明 |
|---|---|---|
| 8080 | token-market api | 前端与控制面 |
| 8090 | token-market edge | 买家调用模型 |
| 8081 | sub2api（经 nginx） | 管理后台与网关；`/api/v1/admin/system/update`、`/rollback` 返回 403 |

两套服务的 PostgreSQL、Redis 都不映射端口。token-market 经共享 docker 网络 `token-market`
直连 `sub2api:8080`，并用只读账号 `tm_readonly` 读 sub2api 库。

## 常用命令

```bash
cd /root/token-market
./tm.sh status                          # 两套服务的运行状态
./tm.sh market logs -f api worker edge  # token-market 日志
./tm.sh sub2api logs -f sub2api         # sub2api 日志
./tm.sh market restart worker
```

## 升级 token-market

用发布的镜像（推荐）：把 `market.env` 的 `TM_IMAGE` 改成新版本（例如 `ghcr.io/yx-token/token-market:v0.2.0`，
需要先 `docker login ghcr.io`），然后：

```bash
./tm.sh market pull && ./tm.sh market up -d
```

在服务器上用源码构建：把新的源码放到 `market-src/`（并更新其中的 `VERSION`、`COMMIT`），然后：

```bash
./tm.sh build && ./tm.sh market up -d
```

数据库迁移在 api 启动时自动执行。

## 升级 sub2api

镜像版本固定在 `sub2api/docker-compose.yml`。升级前必须在开发机上用 s2a-compat 检查新版本
（见仓库 docs/09 第 6、7 节），并在 `contract/contract.yaml` 登记新版本后再改镜像版本号。
直接升级到未登记的版本会让 token-market 进入安全模式（停止写 sub2api、暂停结算入账）。

## 首次部署

1. 复制本目录到服务器，生成 `sub2api/.env`（`POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY`、
   `ADMIN_EMAIL`、`ADMIN_PASSWORD`、`READONLY_PASSWORD`，均用 `openssl rand -hex 24` 之类的随机值）。
2. `./tm.sh sub2api up -d`，在 sub2api 库中创建只读账号 `tm_readonly`（`SELECT` 权限、`default_transaction_read_only = on`）。
3. sub2api 管理员登录后台，**本人**阅读并确认《部署与运营合规承诺》，然后生成 Admin API Key。
4. 生成 `market.env`（参考仓库 `deploy/.env.example`，`SUB2API_BASE_URL=http://sub2api:8080`，
   `SUB2API_REPLICA_DSN` 指向 `sub2api-postgres` 的 `tm_readonly`），`./tm.sh market up -d`。
5. 在 token-market 管理后台配置代理、默认分组、厂商、支付等。

## 注意

- 服务器时间必须准确（支付回调验签有时间窗口）。该机房屏蔽了大部分 NTP 服务器，
  `/etc/systemd/timesyncd.conf.d/token-market.conf` 使用 `ntp.aliyun.com`。
- Docker 日志已在 `/etc/docker/daemon.json` 中限制为每个容器 5 × 50 MB。
- 数据都在 docker 卷中（`sub2api_postgres_data`、`token-market_pgdata` 等），需要另行定期备份。
