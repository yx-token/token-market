# 02 架构

## 1. 组件

| 组件 | 职责 | 是否在请求链路上 |
|---|---|---|
| **market-api** | 控制面 HTTP 接口：买家、卖家、管理后台 | 否 |
| **market-worker** | 后台任务：用量拉取、结算、探测调度、评分判定、回收报价、调度执行器、状态对账、兼容自检。多副本时用 PostgreSQL advisory lock 选主，只有持锁的副本运行任务 | 否 |
| **market-edge** | 买家 API 入口：鉴权买家 Key、解析卖家链、转发到 sub2api、故障转移 | **是** |
| **market-web** | 前端：市场、买家控制台、卖家门户、管理后台（第一期未实现，技术栈待定） | 否 |
| **s2a-compat** | 命令行工具：sub2api 升级前的兼容检查 | 否 |
| PostgreSQL（market） | 本系统数据库 | 否（Edge 不直接访问） |
| Redis（market） | 缓存、任务队列、Edge 路由配置分发 | Edge 读取 |
| **sub2api** | 网关、账号调度、实时计费与余额拦截、OAuth token 刷新 | 是 |
| PostgreSQL（sub2api 只读副本） | 用量与账号状态的数据来源 | 否 |

Go 单仓库，多个可执行文件：

```
token-market/
├── cmd/
│   ├── api/          # market-api
│   ├── worker/       # market-worker
│   ├── edge/         # market-edge
│   └── s2a-compat/   # 兼容闸门
├── internal/
│   ├── app/          # 按配置组装各进程共用的依赖与服务；同步写入 sub2api 的兼容门控
│   ├── httpapi/      # 控制面 HTTP 接口（路由与处理函数）
│   ├── sub2api/      # 适配层：apiwire、adminclient、userclient、replica、ops、opshandlers、compatguard、provision
│   ├── account/      # 平台用户、实名
│   ├── listing/      # 上游、挂单、上架与重新授权、代理库存、回收报价
│   ├── market/       # 市场列表、官方价目
│   ├── buyer/        # 影子用户、买家 Key 与内部 key、Edge 配置发布
│   ├── edgecfg/      # Edge 路由配置在 Redis 中的存取与通知
│   ├── edge/         # Edge 路由与转发
│   ├── quality/      # 检测策略、题库、探测、判定、报告、申诉
│   ├── scheduling/   # 默认分组、规划器、执行器、状态对账
│   ├── billing/      # 用量入账、账本、结算、充值退款提现、对账、Key 限额
│   ├── risk/         # 风控
│   ├── settings/     # 平台可调参数
│   ├── compat/       # s2a-compat 的检查逻辑
│   ├── platform/     # 配置、加密、数据库、鉴权、HTTP 工具、客户端 IP
│   ├── testutil/     # 临时数据库、sub2api 替身（fakesub2api）
│   └── e2e/          # 端到端测试
├── contract/         # sub2api 契约：contract.yaml、queries/*.sql
├── migrations/       # 本系统数据库迁移
├── scripts/          # 受控升级 sub2api 等运维脚本
└── docs/
```

## 2. 部署拓扑

```
                     公网
   买家 ──────────▶ api.<域名> ─▶ market-edge（≥2 实例）──┐
   卖家/买家/管理员 ▶ www.<域名> ─▶ market-web / market-api │ 内网
                                                           ▼
             market-worker ──Admin API / 用户 API──▶ sub2api（只在内网）──▶ 官方上游
                  │  ▲                                  │
                  │  └──────── 只读 ───── sub2api PG 只读副本
                  ▼
           market PG / Redis ◀── market-api
```

- sub2api 不暴露公网。买家流量只能经过 Edge 进入。
- Edge 和 sub2api 同机房部署，转发多出的延迟在毫秒级。
- 探测请求由 market-worker 直接发给 sub2api 网关，不经过 Edge（见 [05 质量检测](05-quality.md)）。
- 端口：market-api `http.addr`（默认 :8080）；market-edge `edge.addr`（:8090）与管理端口 `edge.admin_addr`（:8091）；market-worker 管理端口 `worker.admin_addr`（:8092，`/healthz` 显示是否持有主锁，`/debug/vars` 输出任务运行次数、失败次数、最近成功时间，以及出站队列积压、死信、待处理异常、入账延迟、兼容自检状态等积压指标）。管理端口只在内网开放。

## 3. 与 sub2api 的集成通道

| 通道 | 用途 | 说明 |
|---|---|---|
| Admin API（`x-api-key`） | 建删改账号、分组、用户、代理；调余额；OAuth 授权流程；查配额 | 写操作统一走 [09](09-sub2api-integration.md) 的操作出站表，保证可重试、可对账 |
| 用户 API（影子用户 JWT） | 创建、修改内部 key | Admin API 不能替用户建 key，只能用影子用户登录态调 `/api/v1/keys` |
| 只读数据库 | 用量（`usage_logs`）、账号状态、分组成员、迁移版本 | SQL 集中在 `contract/queries/`，受契约约束 |
| sub2api 配置 | 一次性基线配置 | 见第 6 节 |

## 4. 请求链路

### 4.1 买家走默认分组

```
买家 ─(tm-xxx key)→ Edge ─(内部 key，绑定 tm-pool-claude)→ sub2api
     sub2api：回收段（priority 1–19）→ 寄售段（50–79）→ 选中账号 → 官方上游
```

### 4.2 买家指定卖家链

```
买家 ─→ Edge 查到链 [挂单A, 挂单B, 默认分组]
        第 1 跳：内部 key（tm-l-A 分组，倍率 p_A）→ sub2api → A 的账号
        失败且未开始输出 → 第 2 跳：内部 key（tm-l-B）→ sub2api → B 的账号
        仍失败 → 第 3 跳：内部 key（tm-pool-xxx）→ 默认分组
```

每一跳按该分组自己的倍率计费，所以买家为每个卖家付的就是该卖家的价格。

### 4.3 探测

```
market-worker ─(探测用户 key，绑定 tm-probe-<上游>)→ sub2api → 被测账号 → 官方上游
```

探测分组只包含被测账号，因此探测请求在 sub2api 之后走的路径（代理、TLS 指纹、请求头处理）与买家请求完全相同。

## 5. 本系统概念在 sub2api 中的映射

| 本系统 | sub2api 资源 | 命名 |
|---|---|---|
| 上游 | 账号（1:1）+ 专用代理 | 账号名 `tm-u-<upstream_id>`，notes 写 `tm:upstream:<id>` |
| 探测分组 | 独占分组，只含该上游账号 | `tm-probe-<upstream_id>` |
| 寄售挂单 | 独占分组，只含该上游账号，倍率 = p；第一次被买家指定时才创建，无 key 绑定时回收 | `tm-l-<listing_id>` |
| 默认分组 | 按平台建的分组，开启利润控制；另建一个 composite 全模型分组 | `tm-pool-<family>`、`tm-pool-all` |
| 基线账号 | 账号 + 基线分组 | `tm-ref-<id>`、`tm-refg-<family>` |
| 买家 | 影子用户 | 邮箱 `b<user_id>@shadow.<内部域名>` |
| 买家余额 | 影子用户余额 | — |
| 内部 key | 影子用户名下的 key，按"买家 Key × 分组"创建，`custom_key` 由本系统生成 | 名称 `tm-ik-<id>` |
| 探测方 | 一个探测用户 + 每个探测分组一把 key | `probe@shadow.<内部域名>` |

**所有权约定**：以 `tm-` 开头的 sub2api 资源由本系统独占管理，状态对账器会把人工修改改回期望值。管理员不要在 sub2api 后台手动改这些资源。

**几个由 sub2api 结构决定的约束**：

- priority、load_factor、concurrency 是账号级参数，同一账号在所有分组里取值相同。
- 一个分组只有一个售价倍率，所以不同价格的卖家不能放进同一个买家分组按各自价格计费，这是需要 Edge 的根本原因之一。
- OAuth 账号不能拆成多个 sub2api 账号（会争抢同一个 refresh token），所以一个上游只有一个倍率。

## 6. sub2api 基线配置

| 配置项 | 值 | 原因 |
|---|---|---|
| 管理员合规确认 | 由平台运营方的管理员本人在 sub2api 后台阅读并确认 | sub2api 要求管理员确认《部署与运营合规承诺》后才能使用管理接口（包括 Admin API Key 调用），未确认时返回 HTTP 423 `ADMIN_COMPLIANCE_ACK_REQUIRED`。这是运营方的法律承诺，本系统不代为确认 |
| 开放注册 | 关闭 | 只有本系统创建用户 |
| 登录验证码（Turnstile 等） | 关闭 | 影子用户需要程序化登录；sub2api 不对公网开放 |
| 公网访问 | 禁止 | 只允许 Edge 和 market-worker 访问 |
| `/api/v1/admin/system/update`、`/rollback` | 在反向代理层禁用 | 防止绕过兼容闸门在线升级 |
| `gateway.scheduling.prefer_soonest_reset` | `true` | 同一优先级内优先用窗口最快重置的账号，减少额度作废 |
| OpenAI 高级调度 | 关闭 | 开启后 priority 只是打分项，无法保证"回收优先" |
| 默认分组利润控制 | 开启，毛利与安全缓冲按平台设定 | 运行时排除倍率过高的寄售账号 |
| `gateway.usage_record.overflow_policy` | `sync`（sub2api 默认值，不得改为 `drop` / `sample`） | 队列满时同步写用量，保证成功的请求不漏计费 |
| Admin API Key | 生成后存入本系统密钥存储 | 限制来源 IP |

部署方式固定镜像版本号，升级只能通过带 s2a-compat 检查的流水线进行。

## 7. 故障影响

| 故障 | 影响 | 处理 |
|---|---|---|
| market-api / worker 宕机 | 买家调用不受影响；无法注册、充值、改配置；结算延后 | Edge 使用已缓存的路由配置继续服务 |
| market Redis 宕机 | Edge 用本地缓存继续服务；新建的 key 暂时不可用 | Redis 恢复后自动同步 |
| Edge 全部宕机 | 买家调用中断 | Edge 多实例、无状态，优先保障 |
| sub2api 宕机 | 买家调用中断 | sub2api 自身高可用部署 |
| sub2api 只读副本延迟 | 结算和被动指标延后 | 用量拉取带安全延迟和重扫窗口 |
| sub2api 版本与契约不符 | 新系统进入安全模式：停止对 sub2api 写操作、暂停结算入账 | 见 [09](09-sub2api-integration.md) |
