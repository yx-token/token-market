# 单机部署：单进程闲猫（宿主内嵌市场）

同一台服务器上：

- **宿主进程**（`sub2api` 镜像）：网关 + 管理端 + 市场 C 端 API/静态 + 市场 worker（`MARKET_EMBED=1`）
- **edge 进程**（`token-market` 镜像）：买家模型流量（另端口）；市场库与 redis 仍由 market compose 提供

```
/root/idlecat/
  tm.sh                     运维入口
  docker-compose.market.yml 复制自 market/deploy/docker-compose.yml
  market.override.yml       共享网络；默认停独立 api/worker（profile legacy-split）
  market.env                密钥（权限 600）
  sub2api/
    docker-compose.yml      宿主 + PostgreSQL + Redis + 备用 nginx(8081)
    nginx-admin.conf
    .env
```

## 入口与端口

| 端口 | 服务 | 说明 |
|---|---|---|
| 8080 | 宿主 | C 端 `/`、管理端 `/admin`、网关与 `/api/v1`；市场 API `/api/auth|me|buyer|…` |
| 8090 | edge | 买家调用模型 |
| 8081 | 宿主（nginx，仅 127.0.0.1） | 备用入口；在线升级/回滚仍返回 403 |

管理端随宿主镜像打包（`VITE_ENGINE_HOST=1` → `/admin-assets`）。C 端静态在镜像 `/app/market-web`（`TM_HTTP_WEB_DIR`）。

宿主管理员（`ADMIN_EMAIL` / `ADMIN_PASSWORD`）须与市场 bootstrap 账号一致，并关闭登录验证码。`CRYPTO_*`、`MARKET_JWT_SECRET`、`SUB2API_ADMIN_API_KEY`、`MARKET_DB_PASSWORD` 与 `market.env` 一致。

## 常用命令

```bash
cd /root/idlecat
./tm.sh status
./tm.sh sub2api logs -f sub2api
./tm.sh market logs -f edge
./tm.sh market up -d          # 只起 postgres/redis/edge（api/worker 在 legacy-split profile）
```

临时回到双进程（独立 api+worker）：

```bash
./tm.sh market --profile legacy-split up -d
# 并在 sub2api/.env 去掉 MARKET_EMBED 后重启宿主
```

## 首次部署

1. 复制本目录到服务器；把 `market/deploy/docker-compose.yml` 复制为 `docker-compose.market.yml`。
2. 按 `sub2api/.env.example` / `market/deploy/.env.example` 填密钥（权限 600）：
   - `ENGINE_IMAGE` 与 `TM_IMAGE` 同一发布版本（宿主标签不带 `v`）；
   - `EDGE_PUBLIC_URL` 指向公网 edge（如 `http://<服务器>:8090`），写入 `sub2api/.env` 与 `market.env`；
   - 宿主已默认 `MARKET_EMBED=1`、`TM_HTTP_WEB_DIR=/app/market-web`。
3. `./tm.sh sub2api up -d`，在宿主库创建 `tm_readonly`（SQL 见旧版说明）。
4. SSH 隧道打开 `http://127.0.0.1:8081`，确认合规承诺并生成 Admin API Key，写入 `market.env`。
5. `./tm.sh market up -d`（postgres + redis + edge）。迁移由宿主内嵌市场在首次请求/启动时经 `enginehost`/`app.Build` 执行。
6. 打开 `http://<服务器>:8080/admin` 完成运营配置。

## 升级

```bash
# 改 ENGINE_IMAGE / TM_IMAGE 为同一新版本后：
./tm.sh sub2api pull && ./tm.sh sub2api up -d
./tm.sh market pull && ./tm.sh market up -d
```

或在管理端「系统更新」：拉取本仓 `sub2api_*` 包，热换宿主二进制与 `market-web`；**edge 仍需**把 `TM_IMAGE` 升到同版本并 `market pull/up`。详见 [站内更新](../UPDATES.md)。升级前备份两套数据库。

## 注意

- 服务器时间须准确（支付回调验签）。
- Docker 日志建议限流；数据在 docker 卷中，需另行备份。
