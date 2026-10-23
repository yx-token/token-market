# 单机部署：单进程闲猫（宿主内嵌市场 + Edge）

同一台服务器上**一个控制面/数据面进程**（`sub2api` 镜像）：

- 管理端、C 端、市场 API、市场 worker、买家 Edge（8090）
- 另起 **postgres + redis**（市场库，供内嵌进程使用）

```
/root/idlecat/   （也可软链为 /root/token-market）
  tm.sh
  docker-compose.market.yml   仅 postgres / redis（api/worker/edge 在 legacy-split）
  market.override.yml
  market.env
  sub2api/
    docker-compose.yml        宿主（8080 + 8090）+ 引擎库/redis + 备用 nginx(8081)
    .env
```

## 入口与端口

| 端口 | 说明 |
|---|---|
| 8080 | C 端 `/`、管理端 `/admin`、网关与 `/api/v1`、市场 API |
| 8090 | 买家调用模型（内嵌 Edge） |
| 8081 | 备用管理入口（仅 127.0.0.1） |

## 常用命令

```bash
cd /root/idlecat
./tm.sh status
./tm.sh sub2api logs -f sub2api
./tm.sh market up -d          # 只起 postgres/redis
```

临时回到拆分进程：`./tm.sh market --profile legacy-split up -d`，并设 `MARKET_EMBED_EDGE=0` / `MARKET_EMBED_WORKER=0` 后重启宿主。

## 升级

```bash
# ENGINE_IMAGE 与 TM_IMAGE（仅用于市场库侧镜像拉取时）改为同一版本：
./tm.sh sub2api pull && ./tm.sh sub2api up -d
./tm.sh market pull && ./tm.sh market up -d
```

站内更新升宿主二进制（含管理端、C 端静态、内嵌 worker/edge）。详见 [站内更新](../UPDATES.md)。
