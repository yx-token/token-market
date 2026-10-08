# token-market

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

第一期（M0–M5）后端已完成：卖家上架、调度、买家 Key 与 Edge、结算与对账、质量检测、兼容闸门。前端尚未实现。详见 [11 路线图](docs/11-roadmap.md)，其中列出了需要运营方完成的事项（sub2api 管理员合规确认、真实厂商授权联调、支付通道、题库、压测）。

## 运行

三个服务共用一份配置（见 `config.example.yaml`，敏感项用环境变量覆盖）：

```bash
make build
bin/api    -config config.yaml   # 控制面接口，启动时执行本系统迁移、创建初始管理员、写入内置题库
bin/worker -config config.yaml   # 后台任务（可多副本，自动选主）
bin/edge   -config config.yaml   # 买家 API 入口
```

依赖：PostgreSQL（本系统库）、Redis、sub2api（Admin API Key、只读库账号；sub2api 需关闭登录验证码，管理员需完成合规确认）。上线前把 sub2api 的分组与账号交由本系统管理（`tm-` 前缀），sub2api 配置要求见 [02 架构](docs/02-architecture.md) 第 6 节。

## 开发

需要 Go 1.26+、Docker（集成测试用的 PostgreSQL 与 Redis）、本地 sub2api 仓库（兼容检查与集成测试用）。

```bash
make build              # 构建 bin/api、bin/worker、bin/edge、bin/s2a-compat
make test               # 单元测试
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
cmd/            api、worker、edge、s2a-compat
contract/       对 sub2api 的依赖契约：contract.yaml 与只读查询
internal/
  app/          组装各进程的依赖与服务
  httpapi/      控制面 HTTP 接口
  account/      用户与实名
  listing/      上游、挂单、代理、回收报价
  scheduling/   默认分组、规划器、执行器
  buyer/        影子用户、买家 Key、Edge 配置发布
  edge/         Edge 路由网关
  billing/      入账、账本、结算、资金、对账
  quality/      质量检测
  market/       市场与官方价目
  risk/         风控
  settings/     平台参数
  compat/       s2a-compat 的检查逻辑
  platform/     配置、加密、数据库、鉴权、HTTP 工具
  sub2api/      sub2api 适配层
  testutil/     临时数据库、sub2api 替身
  e2e/          端到端测试
migrations/     本系统数据库迁移
scripts/        运维脚本
docs/           设计文档
```
