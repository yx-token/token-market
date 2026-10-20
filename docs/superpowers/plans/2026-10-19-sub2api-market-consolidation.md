# sub2api 底座吸收市场能力 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 先拿到**完整** sub2api（基线 v0.2.14）代码树作为改造宿主，再在其上增量叠加 token-market **全部**能力；不硬改社区计费/网关内核；**产品 UI 保持本仓前端风格**；前期保留出站队列与影子用户，便于跟社区更新。

**Architecture:** 宿主 = 全量 sub2api + `market/` 增量。**管理端 = 社区 `frontend/`（sub2api 原壳，`/admin/...`）**；闲猫是侧栏「闲猫」增量，用同一套管理员会话打 `/api/admin/*`。**C 端 = `market/web` 的 `/`**。

**Tech Stack:** Go、PostgreSQL、Redis、sub2api v0.2.14 全树、token-market 迁入模块、Vue 3（本产品风格）。

**宿主路径：** 本仓 `idlicat/`（产品名 Idlicat；完整 sub2api + `market/`；已提交到 GitHub）  
**过渡仓：** 本仓库根目录市场实现（满足退役条件前仍可发版）

**ADR:** [docs/adr/0001-sub2api-market-consolidation.md](../../adr/0001-sub2api-market-consolidation.md)  
**模块地图:** [docs/dev/consolidation-module-map.md](../../dev/consolidation-module-map.md)

---

## 已锁定决策（勿擅自推翻）

- [x] **先全量 sub2api，再叠加市场代码**（不裁剪社区树）
- [x] **管理端 = sub2api 原壳**：社区 `frontend/`（`/engine/admin/...`）；闲猫运营在「闲猫市场」
- [x] **C 端 = 闲置 TOKEN**：`market/web` 的市场/买家/卖家；不用社区用户门户替换
- [x] `/admin`、`/console` 进入上述管理端；已登录闲猫则换发引擎会话，不再二次登录
- [x] 保留双系统胶水至资金稳定且另立硬 fork 决策之后
- [x] 少改引擎计费/OAuth/usage 内核；管理端改 `frontend` + `market/` API
- [x] 资金域最后动；契约基线 v0.2.14

功能对照表：[docs/dev/admin-c-end-split.md](../../dev/admin-c-end-split.md)

---

## 二开约定（硬约束）

在 sub2api 代码上二开，闲猫全部功能做进这个产品。同一套用户、同一个管理端、一个端口。

| 层 | 落点 |
|---|---|
| 产品代码 | `backend/` + `frontend/`（sub2api 本体） |
| 闲猫功能 | 在上述代码里新增：市场、卖家、买家、质检、资金 |
| 用户 | sub2api `users`，不再另开一套登录 |
| 管理端 | `frontend/` 原管理壳 |
| C 端 | 同一个 `frontend/` 里的市场页面 |

`market/` 与替身、影子用户、`/engine` 跳转只是过渡胶水，不是终态。计费/网关内核按社区方式扩展，不平行再做一个引擎。

---

## 过渡仓退役条件（全部满足才停发 token-market）

- [ ] 宿主上 `market/cmd` 的 api / worker / edge 可独立构建、与现网等价配置启动
- [x] 单端口：`market-api` 同时托管 C 端 `web/dist` 与管理端 `admin-dist`（`/admin`）
- [ ] `market/web` 构建产物由 market-api（或等价静态托管）提供，主站入口已切
- [ ] 模块地图中 P0–P3 已迁入；P4 资金在宿主跑通 integration（含支付替身）
- [ ] CI：宿主仓对 `backend` + `market` 均有构建/测试；发版流水线以宿主为准
- [ ] 生产（或预发）连续稳定 ≥1 个迭代，无「必须回过渡仓发版」的阻塞
- [ ] `market/SOURCE.md` 已迁入清单与过渡仓 tag 对齐；本仓 README 改为指向宿主

未满足前：过渡仓可继续打 `v*` 发版；宿主只合迁移 PR，不代替生产发版。

---

## 宿主仓库与社区同步

产品宿主为 **`idlicat/`**（已纳入本仓并推送）。社区上游 URL：`https://github.com/Wei-Shaw/sub2api`。

- [x] 已用社区树创建 `idlicat`（含 `backend/`/`frontend/` 等全量目录）
- [x] 已拷入闲置 TOKEN 市场环境：`market/deploy`、`market/scripts`、`market/Dockerfile`、`market/Makefile`、`.github/workflows/market-*.yml`
- [x] 宿主树已提交到本仓 `idlicat/`（单管理端壳、闲猫增量、`/shop`、返回 C 端入口）
- [ ] 升级社区版本：在 `idlicat` 上 fetch 社区 tag → merge → `s2a-compat` + market e2e → 再改 `market/`
- [ ] 闲猫功能写进 `backend/` 与 `frontend/`；升级社区时再解决冲突。不另维护一套用户和后台

---

## 文件与职责（现状）

| 路径 | 职责 |
|---|---|
| 本仓 `docs/adr/0001-*.md` | 架构决策 |
| 本仓 `docs/superpowers/plans/2026-10-19-*.md` | 本实施计划 |
| 本仓 `internal/sub2api/gateway/admin.go` | `Admin` 端口 + `HTTP(*adminclient.Client)` |
| 本仓 `internal/sub2api/ops` / `opshandlers` | 出站队列；`Register(..., gateway.Admin)` |
| 宿主 `backend/` `frontend/` `deploy/` … | **完整社区树，禁止裁剪** |
| 宿主 `market/` | 本产品增量（cmd/internal/web/migrations） |

迁入后 Go 模块建议：宿主根或 `market/go.mod` 独立 module（第一批迁包时定），用 `go.work` 联调本仓与宿主。

---

## 阶段总览

| 阶段 | 主题 | 可交付 | 预估 |
|---|---|---|---|
| 0 | ADR + 计划 | 文档合入 | 完成 |
| 1 | Gateway 端口 | 本仓 `gateway.Admin`；出站语义不变 | 完成 |
| 2 | 宿主骨架 | 全量树 + `market/` + 同步/退役/双前端约定 | 进行中 |
| 3 | 供给+调度+控制面骨架 | listing/scheduling/httpapi 迁入；写路径经 Port | 3–5 周 |
| 4 | 买家+Edge+产品前端 | buyer/edge/web 迁入；主站切到 market/web | 4–6 周 |
| 5 | 质检 | quality/inspection/thirdparty | 4–6 周 |
| 6 | 资金+支付 | billing/payment；最后迁 | 6–8 周 |
| 7 | 退役过渡仓 | 满足退役清单；文档切宿主 | 1–2 周 |
| 8 | 可选拆除胶水 | 仅硬 fork 后评估 | TBD |

---

## 阶段 0 — 文档

- [x] ADR 0001
- [x] 本实施计划（含本轮完善）
- [x] 模块地图

---

## 阶段 1 — Gateway 端口（本仓，已完成）

- [x] `internal/sub2api/gateway`：`Admin` + `HTTP`
- [x] `opshandlers.Register` 依赖 `gateway.Admin`
- [x] `go test ./internal/sub2api/...`；相关 cmd 可构建

---

## 阶段 2 — 宿主骨架（进行中）

### Task 2.1 基座（idlicat）

- [x] 创建本地仓 `../idlicat`，内含完整社区 sub2api 树
- [x] `MARKET.md`、`market/README.md`、`SOURCE.md`、`DEPLOY.md`
- [x] 拷入市场环境：`market/deploy`、`scripts`、`Dockerfile`、`Makefile`、`market-ci.yml`、`market-release.yml`
- [x] 远程不改名（保持现状）
- [x] 选定 module `github.com/yx-token/idlicat/market`，已加 `market/go.mod` + 根 `go.work`
- [ ] （可选）为 idlicat 配置可推送的 GitHub 远程后 `git push -u`

### Task 2.2 开工门禁

- [x] 功能全覆盖清单与模块地图已覆盖主要包
- [x] 宿主已迁入 `platform` + `sub2api` + `contract` + `migrations`，单测通过
- [x] 本仓对照：`go test ./internal/listing/...`（迁 listing 前再跑）

---

## 阶段 3 — 供给 / 调度 / 控制面（功能拆分）

执行以文末「功能全覆盖清单」为准；本节只规定本阶段允许动的包，不得漏迁清单中的对应项。

### 3.1 listing（P1）

- [x] 迁入 `market/internal/listing`（上游、挂单、代理、寄售/回收；**不含资金**）
- [x] 写账号/分组仍走出站 + `gateway.Admin`
- [x] 单测 + 相关 integration 在宿主或 go.work 下通过

### 3.2 scheduling（P1）

- [x] 迁入 `market/internal/scheduling`
- [x] 规划器/执行器/对账；禁止直改社区分组表以外的内核

### 3.3 httpapi 骨架 + settings/account 基础

- [x] 迁入 `settings` / `account` / `market` 包（API 接线仍待 httpapi）
- [x] 迁入卖家上架相关 API 与平台设置只读/必要写（`httpapi` 全量包已迁；单测通过）
- [x] 管理端路由已挂市场运营子集；供给融合账号/分组页可测

### 3.4 cmd 骨架

- [x] `market/cmd/api` `worker` `edge` 等已迁入且 `go build` 通过
- [x] 健康检查区分社区 server 与 market 进程（文档约定已写；运行时 probe 待预发）

**阶段 3 验收：** 卖家上架→入池期望态可对账；社区 backend 仍独立可启。

---

## 阶段 4 — 买家 / Edge / 产品前端

### 4.1 buyer + edgecfg + edge

- [x] 迁入影子用户、买家 Key、跳、内部 key、Edge 配置发布（`buyer` / `edgecfg` 包）
- [x] 迁入 Edge（鉴权、≤5 跳 failover、熔断）；**不改**社区扣费路径
- [x] Edge 公网入口文档化（与社区网关端口分离；见宿主 `market/DEPLOY.md`）

### 4.2 market/web（C 端硬约束）

- [x] 整体迁入本仓 `web/` → `market/web`；`pnpm test`/`pnpm build` 通过
- [x] 构建接入 market-api 单端口（C 端 `web/dist` + 管理端 `admin-dist`）
- [ ] 验收：市场首页、买家/卖家中心与迁移前一致（允许接口 host 变化）
- [ ] `/console` 保留过渡期；文档标明终态运营入口为社区 `frontend/`

### 4.3 管理端 = 闲猫风 sub2api（P0 · 纠正路线）

硬顺序：

- [x] **4.3.1** 闲猫 console CSS + AppLayout 管理壳（`xianmao-console` / bridge）；管理端走 console-app
- [x] **4.3.2** 侧栏按闲猫信息架构重排（值班 / 闲置 TOKEN / 检测 / 运营 / 治理 + 引擎能力）
- [x] **4.3.3a** 统一鉴权：market-api 登录兼容 `access_token`/`role`；`GET /api/auth/me`；去掉市场登录墙，共用 `auth_token`
- [x] **4.3.3b** 写操作首批：平台设置、厂商/分组/检测策略开关、代理启停、题库下线
- [x] **4.3.3c** 热搜上下架、质检项开关、充值确认 / 退款打款 / 提现审核 / 申诉维持或改判
- [x] **4.3.4** 退役闲猫 `/console`（整页跳到 `/admin`）；C 端只保留市场/买家/卖家 + 控制台入口

### 4.4 商家清单 / 市场浏览 API

- [ ] 迁入 market 列表、discovery、商家清单相关 buyer API

**阶段 4 验收：** 公网 C 端只见闲猫；运营在社区管理壳内完成市场运营动作；Edge 替身链路可通。

---

## 阶段 5 — 质检

- [x] 迁入 `quality` / `inspection` / thirdparty（包级）
- [ ] worker 探测任务；临时令牌路径仍走 Edge
- [ ] trial/formal 开关行为与迁前一致

**阶段 5 验收：** 上架检测/定时检测在替身+真实表结构 integration 通过。

---

## 阶段 6 — 资金与支付（最后）

- [x] 设计文档：人民币账本 vs sub2api `users.balance` 边界（同实例 schema 可，禁止简单等同）→ [docs/dev/billing-ledger-boundary.md](../../dev/billing-ledger-boundary.md)
- [x] 迁入 `billing` / `payment`（包级；专项验收仍待）
- [ ] 充值/退款/提现/活动包 integration + 支付替身
- [ ] 精度与异步后扣费回归（对照现有 credit/billing 专项）

**阶段 6 验收：** 资金恒等式与现仓专项测试等价通过。

---

## 阶段 7 — 退役过渡仓

- [ ] 逐项勾选上文「退役条件」
- [ ] 本仓 README 改为「已迁入 xxx，请使用宿主仓库」
- [ ] 冻结本仓功能开发（仅紧急热修策略书面化）

---

## 阶段 8 — 可选拆除胶水

- [ ] 书面决策：接受硬 fork 后再做
- [ ] 评估进程内 `Admin`、弱化/删除 `sub2api_ops`、影子用户归一
- [ ] **默认不做**；与「方便搬社区代码」冲突时保持胶水

---

## 测试策略

| 层级 | 命令/范围 |
|---|---|
| 社区树不回归 | `cd backend && go build ./cmd/server`（及社区自有测试若可跑） |
| 市场单元 | `go test` 已迁 `market/internal/...`；前端 `pnpm test` / `pnpm build`（`market/web`） |
| 集成 | `-tags integration`，`TM_SUB2API_REPO` 指向宿主全量树 |
| 跟社区升级 | tag merge 后：`s2a-compat` + 关键 e2e + 资金专项（若已迁） |

禁止：未过 billing/edge integration 就删出站队列；未满足退役条件就停过渡仓发版。

---

## 风险与回滚

| 风险 | 缓解 |
|---|---|
| 浅克隆导致 merge 社区失败 | 阶段 2 unshallow；远程名保持现状 |
| 误把社区 frontend 当 C 端主站 | 双前端约定：社区壳仅管理域；www 仍是 market/web |
| C 端被换成社区皮肤 | 阶段 4 C 端视觉验收；禁止用 frontend 替换买家/卖家/市场 |
| 管理端双开永久化 | 4.3 清单与闲猫 `/console` 退役门禁 |
| 双仓发版混乱 | 退役条件清单；未退役前生产只认过渡仓 tag |
| 改坏社区内核 | 目录禁令；冲突往 `market/` 收 |

回滚：未退役前生产回滚过渡仓上一 tag；宿主迁移用分支/PR 回退。

---

## 功能全覆盖清单（执行时逐项勾选；缺一项即未完成）

迁入规则：源目录整包复制到宿主对应路径，改 import 前缀；**不改行为**；视觉文件随 `web/` 原样迁。每包迁完跑该包 `go test`；前端迁完跑 `pnpm test` 与 `pnpm build`。

### 后端包

- [x] `internal/platform` → `market/internal/platform`
- [x] `internal/sub2api`（全部子包，含 `gateway`）→ `market/internal/sub2api`
- [x] `contract/` → `market/contract`
- [x] `migrations/` → `market/migrations`
- [x] `internal/account` → `market/internal/account`
- [x] `internal/settings` → `market/internal/settings`
- [x] `internal/listing`（上游、挂单、代理、厂商、额度包商品、分类、模型白名单、导入）→ `market/internal/listing`
- [x] `internal/market`（列表、热搜 discovery、官方价）→ `market/internal/market`
- [x] `internal/scheduling` → `market/internal/scheduling`
- [x] `internal/buyer`（影子用户、Key、商家清单、活动包购买）→ `market/internal/buyer`
- [x] `internal/edgecfg` → `market/internal/edgecfg`
- [x] `internal/edge`（链式 failover、活动额度运行时）→ `market/internal/edge`
- [x] `internal/iam` → `market/internal/iam`
- [x] `internal/quality`（含 `thirdparty`）→ `market/internal/quality`
- [x] `internal/inspection` → `market/internal/inspection`
- [x] `internal/risk` → `market/internal/risk`
- [x] `internal/compat` → `market/internal/compat`（`cmd/s2a-compat` 仍待）
- [x] `internal/update` → `market/internal/update`
- [x] `internal/billing`（账本、结算、退款、提现、店铺活动额度）→ `market/internal/billing`（包级；资金专项验收仍属阶段 6）
- [x] `internal/payment`（支付宝、微信、商户配置）→ `market/internal/payment`
- [x] `internal/httpapi` → `market/internal/httpapi`
- [x] `internal/app` → `market/internal/app`
- [x] `cmd/api` `cmd/worker` `cmd/edge` `cmd/market-runner` → `market/cmd`
- [x] `internal/testutil` → `market/internal/testutil`
- [x] `cmd/fake-sub2api` `cmd/fake-thirdparty` `cmd/s2a-compat` → `market/cmd`
- [x] `internal/e2e` → `market/internal/e2e`

### 前端页面（`web/` → `market/web`，风格禁止改）

- [x] 市场：`MarketView` `ListingView` 及品牌墙/筛选/热搜（源码已迁；视觉验收待勾）
- [x] 买家：`KeysView` `WalletView` `MerchantsView` `StoreCreditsView` `UsageView` `BillsView` `ProfileView`
- [x] 卖家：`PublishView` `UpstreamView` `UpstreamsView` `ListingsView` `ProxiesView` `UsageView` `EarningsView` `StoreCreditsView`
- [x] 运营：`Dashboard` `Users` `Vendors` `Pools` `Proxies` `Quality` `Detection` `ProbeItems` `ProbeRecords` `InspectionChecks` `Categories` `MarketHotSearch` `MarketExtras` `Payments` `Billing` `Funds` `Risk` `Audit` `Settings` `ThirdParty` `SystemUpdate` `OrgUnits` `OrgRoles` `OrgStaff` `Upstreams`
- [x] 登录注册：`LoginView` `RegisterView`
- [x] 样式与静态资源：`styles/`、`public/assets/brands/`、控制台布局

### 产品能力验收（与上表对应，执行结束前必须全勾）

- [ ] 卖家：OAuth/Coding Plan/导入、代理、模型白名单、寄售、订阅套餐、回收、改价下架
- [ ] 市场：品牌墙、热搜、分类筛选、详情购买/加入商家
- [ ] 买家：商家清单、Key 顺序、钱包充值退款、店铺活动包、订阅套餐退货、账单用量
- [ ] 运营：质检题库与检测、第三方检测、支付商户、资金调账、IAM、系统更新、热搜配置
- [ ] Edge：默认池与 ≤5 跳故障转移；社区 backend 扣费路径未被改写
- [ ] 公网只有 sub2api 这一套：C 端与管理端都在 `frontend/`，用户是 sub2api `users`

---

## 当前进度

- 阶段 0–1：完成
- 阶段 2–5（包级）：业务代码与闲猫 `web` 已进宿主；本地 `dev.sh` 可起市场 + 替身
- **决策再修订：** 在 sub2api 上二开。闲猫的市场、卖家、买家、质检、资金写进 `backend/` 和 `frontend/`。同一套用户、同一个管理端、一个端口。
- **已推进：** 真实 `backend/cmd/server` 已在 `127.0.0.1:18080` 起服（库 `tm_sub2api_live`）。`backend/internal/idlecat` 直连市场库，`/api/v1/admin/market/vendors|status|supply/*` 为原生实现（厂商 9、供给账号 4、分组 4）。仪表盘/账号/分组走真实引擎。
- **已切换：** 站点 :8080 的 `/api/v1` 指向真实引擎 `127.0.0.1:18080`。控制台用同一管理员邮箱向 `/api/v1/auth/login` 换会话，不再走替身的 `/admin/auth/session`。`market/scripts/flow-e2e.sh` 72 项通过（含 `/shop` 与卖家/买家引擎关联）。测试说明见 [docs/dev/qa-handoff.md](../../dev/qa-handoff.md)。
- **已迁原生读接口：** 质检概览、充值/退款/提现列表、检测策略/题库、分类、支付商户、市场设置、热搜等均在 `idlecat`（经 `/api/v1/admin/market/*`）。
- **已迁开关类写接口：** 厂商、代理、分组、检测策略、题库下线、热搜配置写入 `idlecat`。站点 :8080 对这些路径不再本地注册，反代到真实引擎。
- **运营页加固：** 资金/申诉操作按状态显示并二次确认；质检项列表回退 market-api 完整目录；支付商户可开关。
- **本地栈切真实引擎：** `dev.sh` 默认 `USE_REAL_ENGINE=1`（:18080 / `tm_sub2api_live`）；worker/edge 用引擎 Admin API Key；替身 :18081 仅 `USE_REAL_ENGINE=0`。
- **兼容契约：** `contract.yaml` 已收录宿主 `0.2.13`（指纹与 `0.2.14` 相同）；worker `compat_state=normal`，出站写恢复。
- **深色模式：** `html.dark` 下页面为纯黑（`dark-950/900` = `#000`），卡片 `#111`、边框 `#262626`。浅色令牌与按钮白字不变；管理壳用 `--color-surface` 切换，不改 class 名。
- **身份：** dev 上 admin/seller/buyer 已按邮箱写入引擎 `users`。`POST /api/v1/admin/market/identities/bind` 把卖家/买家绑成 `buyer` 影子，`sub2api_user_id` 指向同一引擎用户（seller `#2`、buyer `#3`），不再另开 `buyer-{id}@domain`。
- **影子令牌：** 绑定成功后立即用同一邮箱密码登录引擎，把 access/refresh 加密写入 `shadow_identities`。dev 上 seller/buyer 均为 `active` 且 `token_ready`。
- **登录对齐：** `POST /api/auth/login` 与 `/api/auth/register` 成功后按同一邮箱复用或创建引擎用户；买家/卖家绑定影子并写入令牌。管理员只回传 `engine_user_id`，不建买家影子。引擎失败不回滚市场会话，响应带 `engine_link_error`。
- **C 端首迁：** `/shop`、`/shop/listings/:id`、`/shop/center`（Key / 钱包 / 商家 / 账单 / 挂单 / 上游）。`/` 仍是 `market/web`。
- **管理接口：** 除 `system/*`（站内升级仍在 market-api）外，`/api/v1/admin/market/*` 由引擎执行。资金确认/退款/提现/申诉走引擎进程里的原 `billing.Funds` / `quality.Engine`，仍写市场人民币账本，不写 `users.balance`。题库、账本、组织角色已在 :18080 返回 200。
- **资金边界：** [docs/dev/billing-ledger-boundary.md](../../dev/billing-ledger-boundary.md)。充值/退款/提现/活动包 integration 尚未在宿主重跑。
- **仍未完成：** 资金 integration 全绿；Edge ≤5 跳复验；质检 worker 验收；OAuth/套餐购买/完整视觉迁入；过渡仓退役；`system/*` 站内升级。
