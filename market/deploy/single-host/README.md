# 单机部署：闲猫宿主（sub2api）+ token-market

同一台服务器上用两套 docker compose 运行闲猫宿主（引擎：网关与管理 API）和 token-market（市场 api / worker / edge）。
两者来自同一仓库、同一次发布：宿主镜像 `ghcr.io/yx-token/sub2api:<版本>`，市场镜像 `ghcr.io/yx-token/token-market:v<版本>`。

```
/root/idlecat/
  tm.sh                     运维入口（见下文）
  docker-compose.market.yml token-market（复制自仓库 market/deploy/docker-compose.yml）
  market.override.yml       token-market 加入共享网络 token-market；市场库以 market-postgres 供宿主直连
  market.env                token-market 配置与密钥（权限 600，不进仓库）
  sub2api/
    docker-compose.yml      闲猫宿主 + PostgreSQL + Redis + 对外入口 nginx
    nginx-admin.conf        对外入口：禁用在线升级 / 回滚接口
    .env                    宿主配置与密钥（权限 600，不进仓库）
```

## 入口与端口

| 端口 | 服务 | 说明 |
|---|---|---|
| 8080 | token-market api | C 端 `/`；管理端 `/admin`（同端口，管理接口经 `/api/v1` 反代到宿主） |
| 8090 | token-market edge | 买家调用模型 |
| 8081 | 宿主（经 nginx，仅绑定 127.0.0.1） | 备用管理入口，不对公网开放，经 SSH 隧道访问（`ssh -L 8081:127.0.0.1:8081 root@<服务器>`）；`/api/v1/admin/system/update`、`/rollback` 返回 403 |

管理端打包在市场镜像的 `web/admin-spa`，随站内更新一起切换。`/admin` 用当前管理员邮箱和 `BOOTSTRAP_ADMIN_PASSWORD`
登录宿主换发会话，所以宿主管理员（`sub2api/.env` 的 `ADMIN_EMAIL` / `ADMIN_PASSWORD`）必须与市场的
`BOOTSTRAP_ADMIN_EMAIL` / `BOOTSTRAP_ADMIN_PASSWORD` 一致，宿主需关闭登录验证码。宿主的在线升级会换成上游社区版本，
在 8081 和 `/api/v1` 两个入口都已禁用。

两套服务的 PostgreSQL、Redis 都不映射端口。经共享 docker 网络 `token-market`：

- token-market 直连 `sub2api:8080`，并用只读账号 `tm_readonly` 读 `sub2api-postgres`；
- 宿主的闲猫运营接口（`/api/v1/admin/market/*`）用 `MARKET_DATABASE_DSN` 直连 `market-postgres`。市场库晚于宿主启动时，
  宿主按需重连，不需要重启。

## 常用命令

```bash
cd /root/idlecat
./tm.sh status                          # 两套服务的运行状态
./tm.sh market logs -f api worker edge  # token-market 日志
./tm.sh sub2api logs -f sub2api         # 宿主日志
./tm.sh market restart worker
```

## 首次部署

1. 在服务器上建立上面的目录：复制仓库 `market/deploy/single-host/` 的内容，并把 `market/deploy/docker-compose.yml`
   复制为 `docker-compose.market.yml`。
2. 按 `sub2api/.env.example` 生成 `sub2api/.env`，按 `market/deploy/.env.example` 生成 `market.env`（均为权限 600）：
   - `ENGINE_IMAGE` 与 `TM_IMAGE` 使用同一个发布版本（宿主标签不带 `v`，例如 `0.17.2` 与 `v0.17.2`）；
   - 两边的 `MARKET_DB_PASSWORD` 相同，管理员邮箱和密码相同；
   - `SUB2API_BASE_URL=http://sub2api:8080`，`SUB2API_REPLICA_DSN` 指向 `sub2api-postgres` 的 `tm_readonly`。
3. `./tm.sh sub2api up -d`（同时创建共享网络）。在宿主库中创建只读账号：

   ```sql
   CREATE ROLE tm_readonly LOGIN PASSWORD '<READONLY_PASSWORD>';
   GRANT CONNECT ON DATABASE sub2api TO tm_readonly;
   GRANT USAGE ON SCHEMA public TO tm_readonly;
   GRANT SELECT ON ALL TABLES IN SCHEMA public TO tm_readonly;
   ALTER DEFAULT PRIVILEGES FOR ROLE sub2api IN SCHEMA public GRANT SELECT ON TABLES TO tm_readonly;
   ALTER ROLE tm_readonly SET default_transaction_read_only = on;
   ```

4. 管理员经 SSH 隧道（`ssh -L 8081:127.0.0.1:8081 root@<服务器>`，然后访问 `http://127.0.0.1:8081`）登录宿主后台，
   **本人**阅读并确认《部署与运营合规承诺》，然后生成 Admin API Key，
   填入 `market.env` 的 `SUB2API_ADMIN_API_KEY`。
5. `./tm.sh market up -d`。数据库迁移在 api 启动时自动执行；worker 完成兼容自检后
   （`GET /api/admin/sub2api/status` 的 `guard_state` 为 `normal`）才允许写入宿主。
6. 打开 `http://<服务器>:8080/admin`，在管理端配置代理、默认分组、厂商、支付等。

## 升级

宿主与市场同仓发布，一起升级：把 `sub2api/.env` 的 `ENGINE_IMAGE` 和 `market.env` 的 `TM_IMAGE` 改成同一个新版本，然后：

```bash
./tm.sh sub2api pull && ./tm.sh sub2api up -d
./tm.sh market pull && ./tm.sh market up -d
```

管理后台「系统更新」（站内更新）只更新 token-market（含 C 端与管理端页面），不更新宿主，详见 [站内更新](../UPDATES.md)。
运行时自检要求宿主的迁移集合与市场契约登记的一致（`contract.yaml` 的 `host_migration_fingerprints`）；
宿主带新迁移的版本必须两边一起换镜像，只更新一边时市场会进入安全模式（暂停写宿主和结算入账），两边对齐后自动恢复。
数据库迁移在启动时执行，程序回退不撤销迁移，升级前先备份两套数据库。

## 注意

- 服务器时间必须准确（支付回调验签有时间窗口）。该机房屏蔽了大部分 NTP 服务器，
  `/etc/systemd/timesyncd.conf.d/token-market.conf` 使用 `ntp.aliyun.com`。
- Docker 日志已在 `/etc/docker/daemon.json` 中限制为每个容器 5 × 50 MB。
- 数据都在 docker 卷中（`sub2api_postgres_data`、`token-market_pgdata` 等），需要另行定期备份。
