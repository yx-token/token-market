<p align="center">
  <img src="docs/assets/logo.svg" width="96" height="96" alt="闲置算力交易所">
</p>

<h1 align="center">token-market · 闲置算力交易所</h1>

<p align="center">
  <a href="https://github.com/yx-token/token-market/actions/workflows/ci.yml"><img src="https://github.com/yx-token/token-market/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

Token 闲置交易平台。卖家把用不完的订阅额度（官方 OAuth 订阅账号、国产 Coding Plan）挂到平台，平台以两种方式变现：

- **寄售**：卖家自定倍率，平台加固定抽佣后在市场展示，买家可指定卖家购买；
- **平台回收**：平台按质量检测结果报出回收倍率，按实际用量结算。

买家在本平台注册、充值、创建 API Key，可以走平台默认分组，也可以指定最多 5 个卖家按顺序故障转移。

底层路由与实时计费完全复用 [sub2api](https://github.com/Wei-Shaw/sub2api)，**不修改 sub2api 的任何代码**：本系统通过 Admin API、影子用户的用户 API、只读数据库访问和 sub2api 自身配置来驱动它。

## 文档

| 文档 | 内容 |
|---|---|
| [01 总览](docs/01-overview.md) | 目标、第一期范围、角色、术语、决策记录 |
| [02 架构](docs/02-architecture.md) | 组件、部署拓扑、请求链路、与 sub2api 的映射 |
| [03 数据模型](docs/03-data-model.md) | 本系统数据库表设计 |
| [04 上游与挂单](docs/04-listing.md) | 支持的上游、上架流程、两种模式、状态机 |
| [05 质量检测](docs/05-quality.md) | 检测体系、降智判定、厂商检测开关、卖家测试报告 |
| [06 调度](docs/06-scheduling.md) | 默认分组结构、策略插件、执行器、状态对账 |
| [07 Edge 路由网关](docs/07-edge.md) | 买家 Key、指定卖家链、故障转移 |
| [08 计费与结算](docs/08-billing.md) | 定价、账本、结算、提现、资金风控 |
| [09 sub2api 集成与兼容闸门](docs/09-sub2api-integration.md) | 适配层、调用清单、s2a-compat |
| [10 第一期接口](docs/10-api.md) | 买家、卖家、管理后台接口 |
| [11 路线图与待定事项](docs/11-roadmap.md) | 分期计划、未决问题 |

## 技术栈

Go + PostgreSQL + Redis，独立仓库、独立数据库。对接的 sub2api 基线版本：**0.2.14**。

## 状态

第一期（M0–M5）与前端已完成：卖家上架、调度、买家 Key 与 Edge、结算与对账、质量检测、兼容闸门，在线支付（支付宝电脑网站支付与当面付、微信支付 Native，余额退款原路退回），第三方检测（BazaarLink 模型身份检测），以及市场、买家中心、卖家中心、管理后台的页面。详见 [11 路线图](docs/11-roadmap.md)，其中列出了需要运营方完成的事项（sub2api 管理员合规确认、真实厂商授权与支付商户联调、提现代付、题库、压测）。

## 快速体验（本地）

不需要真实的 sub2api、厂商账号与支付商户：`scripts/dev.sh` 启动 PostgreSQL / Redis 容器、sub2api 替身（`cmd/fake-sub2api`，在 sub2api v0.2.14 的真实表结构上模拟 Admin API、用户 API 与网关）、第三方服务替身（`cmd/fake-thirdparty`：支付宝、微信支付、BazaarLink，按真实协议签名验签，不产生真实扣款）和三个服务，并写入演示数据（开放厂商、代理、默认分组、本地可通过的题库、Claude 上架检测追加 BazaarLink 身份检测）。

```bash
make dev-up        # 构建前端与后端并启动；打开 http://127.0.0.1:8080
make dev-down
```

管理员 `admin@dev.local` / `admin-password-1`。可以注册一个卖家，先在卖家中心的“代理 IP”中添加代理（替身环境可填 `10.255.1.1:1080`），再接入 Claude 并选择该代理（授权码随便填一个词，同一个词代表同一个官方账号），等上架检测（含第三方身份检测，约 1–2 分钟）通过后挂单；再注册一个买家充值、创建 Key，用页面上给出的 curl 调用 `http://127.0.0.1:8090`。充值选支付宝网页支付会跳到替身收银台；选扫码支付时，二维码内容就是替身的"扫码付款"页面地址，在浏览器打开它并确认付款即可。前端开发用 `cd web && pnpm dev`（http://127.0.0.1:5173，/api 代理到 :8080）。

## 发布与部署

### 发布

推送 `v` 开头的标签即发布（`.github/workflows/release.yml`）：先跑完整 CI（Go 检查与单元、集成测试，前端检查与构建），通过后构建多架构镜像（linux/amd64、linux/arm64）推送到 GHCR，最后创建 GitHub Release（自动生成更新说明，附部署文件）。

```bash
git tag v0.1.0 && git push origin v0.1.0     # 正式版：镜像标签 v0.1.0、0.1.0、0.1、latest
git tag v0.2.0-rc.1 && git push origin v0.2.0-rc.1   # 带 - 的是预发布：只打完整版本号，不更新 latest
```

镜像地址 `ghcr.io/yx-token/token-market`。镜像内程序的版本可以在 `GET /healthz` 的 `version` 字段和启动日志中看到。推送到 main 与提交 PR 时会运行同样的 CI（`.github/workflows/ci.yml`，另外检查镜像能否构建）。

国内服务器拉取 GHCR 较慢时，可以让发布流程同时推送到一个镜像仓库副本（例如阿里云容器镜像服务）：在仓库 Settings → Secrets and variables → Actions 中添加变量 `MIRROR_IMAGE`（如 `registry.cn-hangzhou.aliyuncs.com/<命名空间>/token-market`）和密钥 `MIRROR_USERNAME`、`MIRROR_PASSWORD`。

### 部署

一个镜像包含全部组件，`deploy/docker-compose.yml` 是部署示例（本系统的 PostgreSQL、Redis、api、worker、edge；sub2api 单独部署）。仓库是私有的，镜像也是私有的，服务器需要先用一个有 `read:packages` 权限的 GitHub token 登录 GHCR：

```bash
echo "<token>" | docker login ghcr.io -u <GitHub 用户名> --password-stdin
cp deploy/.env.example deploy/.env    # 填写 TM_IMAGE（建议固定版本，如 ghcr.io/yx-token/token-market:v0.1.0）、密钥与 sub2api 连接信息
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

升级：把 `TM_IMAGE` 改成新版本后再执行 `pull` 与 `up -d`；数据库迁移在 api 启动时自动执行。每个 Release 的附件里也有对应版本的 `docker-compose.yml` 与 `.env.example`。

sub2api 与 token-market 部署在同一台服务器上的完整布局（含 sub2api 的 compose、对外入口与运维脚本）见 [deploy/single-host](deploy/single-host/README.md)。

只需对外暴露 api（控制面与前端，`:8080`）和 edge（买家调用模型，`:8090`），并把负载均衡地址配置为 `TRUSTED_PROXIES`。不使用容器时，三个服务共用一份配置（见 `config.example.yaml`），分别运行 `bin/api`、`bin/worker`、`bin/edge`。支付宝、微信支付商户在管理后台"支付配置"中填写，不需要写进配置文件。

依赖：PostgreSQL（本系统库）、Redis、sub2api（Admin API Key、只读库账号；sub2api 需关闭登录验证码，管理员需完成合规确认）。sub2api 配置要求见 [02 架构](docs/02-architecture.md) 第 6 节。

## 开发

需要 Go 1.26+、Node 22 与 pnpm、Docker（PostgreSQL 与 Redis）、本地 sub2api 仓库（兼容检查、集成测试与开发环境用）。

```bash
make build              # 构建 bin/api、bin/worker、bin/edge、bin/s2a-compat、bin/fake-sub2api、bin/fake-thirdparty
make test               # 单元测试
make web-test           # 前端单元测试（vitest）
make web-build          # 前端类型检查与构建（web/dist）
make test-db-up         # 启动测试用 PostgreSQL（55432）与 Redis（56379）
make test-integration   # 集成测试：本系统迁移、出站表、契约查询，以及基于 sub2api 替身的端到端测试
make lint
```

端到端测试（`internal/e2e`）在应用了 sub2api v0.2.14 全部迁移的临时库上运行 sub2api 替身（`internal/testutil/fakesub2api`），把上架、调度、Edge 故障转移、入账、对账、退款、提现、质量检测等流程完整跑一遍。

兼容检查（升级 sub2api 前，详见 [09](docs/09-sub2api-integration.md) 第 6、7 节）：

```bash
go run ./cmd/s2a-compat check --sub2api-repo ../github-fork/sub2api --current v0.2.14 --target v0.2.15 --target-version 0.2.15
go run ./cmd/s2a-compat routes --base-url http://staging-sub2api:8080
scripts/upgrade-sub2api.sh   # 受控升级：检查 → 预发探测 → 确认 → 升级 → 等待自检恢复
```

冒烟测试需要一个运行中的 sub2api，参数见 `internal/sub2api/smoke_test.go` 文件头：

```bash
TM_SMOKE_SUB2API_URL=http://127.0.0.1:58080 TM_SMOKE_USER_EMAIL=... TM_SMOKE_USER_PASSWORD=... \
TM_SMOKE_GROUP_ID=1 go test -tags smoke ./internal/sub2api/ -v
```

### 目录

```
cmd/            api、worker、edge、s2a-compat、fake-sub2api 与 fake-thirdparty（开发用替身）
web/            前端（Vue 3 + TypeScript + Vite + Tailwind）
deploy/         docker compose 部署示例
contract/       对 sub2api 的依赖契约：contract.yaml 与只读查询
internal/
  app/          组装各进程的依赖与服务
  httpapi/      控制面 HTTP 接口
  account/      用户与实名
  listing/      上游、挂单、代理、回收报价
  scheduling/   默认分组、规划器、执行器
  buyer/        影子用户、买家 Key、Edge 配置发布
  edge/         Edge 路由网关
  billing/      入账、账本、结算、充值与退款（含原路退回）、提现、对账
  payment/      支付渠道：支付宝、微信支付
  quality/      质量检测（thirdparty/：第三方检测站）
  market/       市场与官方价目
  risk/         风控
  settings/     平台参数
  compat/       s2a-compat 的检查逻辑
  platform/     配置、加密、数据库、鉴权、HTTP 工具
  sub2api/      sub2api 适配层
  testutil/     临时数据库、sub2api / 支付 / BazaarLink 替身
  e2e/          端到端测试
migrations/     本系统数据库迁移
scripts/        受控升级 sub2api、本地开发环境
docs/           设计文档
```
