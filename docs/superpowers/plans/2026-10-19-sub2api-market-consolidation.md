# sub2api 底座吸收市场能力 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 先拿到**完整** sub2api（基线 v0.2.14）代码树作为改造宿主，再在其上增量叠加 token-market **全部**能力；不硬改社区计费/网关内核；**产品 UI 保持本仓前端风格**；前期保留出站队列与影子用户，便于跟社区更新。

**Architecture:** 宿主仓库 = 全量社区 sub2api + 顶层 `market/` 增量。Strangler：业务从本仓按模块迁入宿主 `market/`。`gateway.Admin` 隔离 HTTP Admin 与未来进程内调用。社区 `backend/`/`frontend/` 永久保留且可独立构建；**对用户暴露的主 UI 仅为 `market/web`**。

**Tech Stack:** Go、PostgreSQL、Redis、sub2api v0.2.14 全树、token-market 迁入模块、Vue 3（本产品风格）。

**宿主路径（本地）：** `../github-fork/sub2api`（分支 `market/consolidation`）  
**过渡仓：** 本仓库 `token-market`（满足退役条件前仍可发版）

**ADR:** [docs/adr/0001-sub2api-market-consolidation.md](../../adr/0001-sub2api-market-consolidation.md)  
**模块地图:** [docs/dev/consolidation-module-map.md](../../dev/consolidation-module-map.md)

---

## 已锁定决策（勿擅自推翻）

- [x] **先全量 sub2api，再叠加市场代码**（不裁剪社区树）
- [x] **后台/市场 UI 保留本产品前端风格**（`market/web` ← `web/`；不用社区 `frontend/` 换皮）
- [x] 保留双系统胶水（出站队列、影子用户）至资金稳定且另立硬 fork 决策之后
- [x] 少改 sub2api 原文件，优先 `market/` 新目录
- [x] 资金域最后动
- [x] 契约测试基线仍为 v0.2.14
- [x] 社区 `frontend/` 可保留作对照/运维，**不是**卖家/买家/运营主入口

---

## 双前端部署约定（硬约束）

| 前端 | 路径 | 谁用 | 默认公网 |
|---|---|---|---|
| **本产品 UI** | 宿主 `market/web` | 买家、卖家、运营控制台 | **是**（`www.` / 主站） |
| 社区 sub2api UI | 宿主 `frontend/` | 对照、排障、可选内网运维 | **否**（仅内网或关掉） |

部署约定：

- Compose / 单机示例中，**反向代理默认只把用户流量指到 `market` 的 api+web+edge`**。
- 社区 `frontend` 若启动，绑定内网端口或独立 host（如 `s2a-admin.internal`），文档标明「非产品后台」。
- 迁移 `web/` 时原样保留样式体系（xianmao / 闲置TOKEN 控制台），禁止为「统一成社区皮肤」而改视觉。

---

## 过渡仓退役条件（全部满足才停发 token-market）

- [ ] 宿主上 `market/cmd` 的 api / worker / edge 可独立构建、与现网等价配置启动
- [ ] `market/web` 构建产物由 market-api（或等价静态托管）提供，主站入口已切
- [ ] 模块地图中 P0–P3 已迁入；P4 资金在宿主跑通 integration（含支付替身）
- [ ] CI：宿主仓对 `backend` + `market` 均有构建/测试；发版流水线以宿主为准
- [ ] 生产（或预发）连续稳定 ≥1 个迭代，无「必须回过渡仓发版」的阻塞
- [ ] `market/SOURCE.md` 已迁入清单与过渡仓 tag 对齐；本仓 README 改为指向宿主

未满足前：过渡仓可继续打 `v*` 发版；宿主只合迁移 PR，不代替生产发版。

---

## 宿主仓库与社区同步

当前本地克隆为 **shallow**，且 `origin` 指向社区 `Wei-Shaw/sub2api`。完善动作：

- [ ] `git fetch --unshallow`（或重新全量 clone）补全历史，便于日后 merge 社区 tag
- [ ] 增加产品远程（如 `yx-token/sub2api-market` 或组织约定名）为 `product`；社区保持 `upstream`
- [ ] 升级社区版本流程：`upstream` 取 tag → merge 进 `market/consolidation`（或 release 分支）→ 跑 `s2a-compat` + market e2e → 再动 `market/` 适配
- [ ] 冲突优先落在 `market/` 或薄适配层；避免改 `backend/internal` 计费/OAuth/usage 内核

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

### Task 2.1 基座

- [x] 分支 `market/consolidation` @ v0.2.14
- [x] `market/README.md`、`market/SOURCE.md`
- [x] `backend` 可 `go build ./cmd/server`（已本地验证）
- [ ] unshallow / 全量历史
- [ ] 配置 `upstream` + 产品远程
- [ ] 宿主根增加简短 `MARKET.md` 指向 `market/README.md`（避免只看到社区 README）

### Task 2.2 约定落地到宿主文档

- [x] 双前端原则写入 `market/README.md`
- [ ] `market/DEPLOY.md`：主站=`market/web`；社区 frontend 内网-only 示例
- [ ] `market/SOURCE.md` 增加「退役检查清单」链接到本计划退役节

### Task 2.3 第一批迁包准备

- [ ] `go.work`（本仓 + 宿主 `backend` + 宿主 `market` module）
- [ ] 选定 `market` 的 module path（建议 `github.com/<org>/sub2api/market` 或暂用 replace）

---

## 阶段 3 — 供给 / 调度 / 控制面（功能拆分）

### 3.1 listing（P1）

- [ ] 迁入 `market/internal/listing`（上游、挂单、代理、寄售/回收；**不含资金**）
- [ ] 写账号/分组仍走出站 + `gateway.Admin`
- [ ] 单测 + 相关 integration 在宿主或 go.work 下通过

### 3.2 scheduling（P1）

- [ ] 迁入 `market/internal/scheduling`
- [ ] 规划器/执行器/对账；禁止直改社区分组表以外的内核

### 3.3 httpapi 骨架 + settings/account 基础

- [ ] 迁入卖家上架相关 API 与平台设置只读/必要写
- [ ] 管理端路由可先挂「子集」，完整运营台随 web 阶段 4

### 3.4 cmd 骨架

- [ ] `market/cmd/api` `worker` 先能启动（可先只注册已迁模块）
- [ ] 健康检查区分社区 server 与 market 进程

**阶段 3 验收：** 卖家上架→入池期望态可对账；社区 backend 仍独立可启。

---

## 阶段 4 — 买家 / Edge / 产品前端

### 4.1 buyer + edgecfg + edge

- [ ] 迁入影子用户、买家 Key、跳、内部 key、Edge 配置发布
- [ ] 迁入 Edge（鉴权、≤5 跳 failover、熔断）；**不改**社区扣费路径
- [ ] Edge 公网入口文档化（与社区网关端口分离）

### 4.2 market/web（风格硬约束）

- [ ] 整体迁入本仓 `web/` → `market/web`（含样式与控制台）
- [ ] 构建接入 market-api 或独立静态服务
- [ ] 验收截图/清单：市场首页、卖家中心、运营后台与迁移前一致（允许接口 host 变化）
- [ ] 确认社区 `frontend/` **未**被设为默认 www

### 4.3 商家清单 / 市场浏览 API

- [ ] 迁入 market 列表、discovery、商家清单相关 buyer API

**阶段 4 验收：** 主站用户只看到本产品 UI；可用 Edge 调通一条链路（替身环境即可）。

---

## 阶段 5 — 质检

- [ ] 迁入 `quality` / `inspection` / thirdparty
- [ ] worker 探测任务；临时令牌路径仍走 Edge
- [ ] trial/formal 开关行为与迁前一致

**阶段 5 验收：** 上架检测/定时检测在替身+真实表结构 integration 通过。

---

## 阶段 6 — 资金与支付（最后）

- [ ] 设计文档：人民币账本 vs sub2api `users.balance` 边界（同实例 schema 可，禁止简单等同）
- [ ] 迁入 `billing` / `payment`
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
| 浅克隆导致 merge 社区失败 | 阶段 2 unshallow + 产品远程 |
| 误把社区 frontend 当主站 | 双前端部署约定 + DEPLOY 文档 |
| 迁包后风格漂移 | 阶段 4 视觉验收；禁止换社区皮肤 |
| 双仓发版混乱 | 退役条件清单；未退役前生产只认过渡仓 tag |
| 改坏社区内核 | 目录禁令；冲突往 `market/` 收 |

回滚：未退役前生产回滚过渡仓上一 tag；宿主迁移用分支/PR 回退。

---

## 当前进度

- 阶段 0–1：完成（本仓）
- 阶段 2：宿主分支 + `market/` 骨架 + 本轮计划完善；待 unshallow、产品远程、`MARKET.md`/`DEPLOY.md`、开搬 `listing`
