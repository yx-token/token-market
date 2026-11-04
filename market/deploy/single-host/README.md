# 单机部署：单进程闲猫（宿主内嵌市场 + Edge）

同一台服务器上**一个控制面/数据面进程**（`sub2api` 镜像）：

- 管理端、C 端、市场 API、市场 worker，以及挂在主端口的买家 Edge
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
| 8080 | C 端 `/`、管理端 `/admin`、网关与 `/api/v1`、`tm-` 买家模型请求（内嵌 Edge） |
| 8090 | 内嵌 Edge 兼容入口，仅供尚未迁移的旧客户端使用 |
| 8081 | 备用管理入口（仅 127.0.0.1） |

买家 Base URL 推荐使用站点 origin（例如 `https://market.example.com`），不要附加 `:8090`。TLS 反向代理应将站点模型路径转发到宿主 `8080`，并关闭响应缓冲以保留 SSE 流式输出；普通 sub2api Key 在相同路径仍由原网关处理。`sub2api/.env` 中将 `EDGE_PUBLIC_URL` 设为该 HTTPS origin，供 C 端显示及第三方检测 worker 探测使用；此值需公网可达。留空时 C 端虽回退到当前 origin，但第三方检测不可用。迁移后暂时保留 8090 监听和 nginx 入口，老客户端仍可继续使用。

主端口仅把带 `tm-` Key 的网关请求交给同一个 Edge，不扩大 Edge 原有白名单。不受支持的接口（如 `/v1/images/generations`、`/v1/custom-voices`、`/v1/usage`）与直接访问 8090 一样被拒绝，不会回落到宿主原网关。8090 兼容监听失败会记错误日志，但共享 handler 仍继续在主端口服务。

Edge 的买家 IP 白名单和无效 Key 限流在两个入口均使用 `TM_EDGE_TRUSTED_PROXIES`，不读取宿主的 `SERVER_TRUSTED_PROXIES` 或控制面的 `TM_HTTP_TRUSTED_PROXIES`。宿主挂载保留原始 `RemoteAddr` 与 `X-Forwarded-For`。compose 默认不信任任何代理；nginx 经 docker-proxy 的源地址若为 `172.31.250.1`，显式设置 `EDGE_TRUSTED_PROXIES=172.31.250.1/32`（compose 传为 `TM_EDGE_TRUSTED_PROXIES`），nginx 用 `proxy_set_header X-Forwarded-For $remote_addr` 覆盖外来头，即可继续取得真实买家 IP；其他拓扑应填实际直连代理地址。`.env.example` 的域名和代理地址均为注释示例，应按实际部署取消注释并替换。

现有 `idlicat.com` 拓扑将 `/opt/token-market/.env` 中 `EDGE_PUBLIC_URL=https://idlicat.com:8090` 改为 `EDGE_PUBLIC_URL=https://idlicat.com`，换新宿主镜像并重新创建容器。现有 443 的 `location /` 已转发至 `127.0.0.1:8080`，无需为模型路径另配反代；8090 的 TLS nginx server 暂时保留给旧客户端。保留当前容器的 `TM_EDGE_TRUSTED_PROXIES=172.31.250.1/32`；采用本仓 compose 时须在 `.env` 显式配置 `EDGE_TRUSTED_PROXIES=172.31.250.1/32`，否则默认空值会关闭转发 IP 信任。

宿主主端口不设置响应写超时，Edge 仍按自身首字节超时控制回源并逐块 flush；请求体同时受到 nginx、宿主全局上限与 Edge 上限约束。当前 Edge 默认上限是 32 MiB，宿主默认全局上限为 256 MiB，实际取较小值；不要把 nginx 的 `client_max_body_size 256m` 理解为买家可上传 256 MiB。

本 compose 给宿主 `stop_grace_period: 45s`，留给 Edge 最多 30 秒等待在途请求并排空 attempt 日志。沿用旧 compose 时 Docker 默认停机宽限可能只有 10 秒，升级时应同步此项，避免长请求在重启时被容器强制终止。

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
