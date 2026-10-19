# sub2api 底座吸收市场能力 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不硬改社区计费/网关内核的前提下，分阶段把 token-market 能力迁到以 sub2api v0.2.14 为底座的统一产品上；前期保留出站队列与影子用户，便于跟社区更新。

**Architecture:** Strangler。市场代码继续以独立模块演进；通过 `Gateway` 端口抽象隔离「HTTP Admin」与未来「进程内调用」。sub2api 原文件尽量只读；市场表仍在 market PG（可同实例 `market` schema）。Edge / 挂单 / 质检 / 资金按阶段迁入或并仓。

**Tech Stack:** Go 1.26、PostgreSQL、Redis、现有 token-market 模块、sub2api v0.2.14（`../github-fork/sub2api`）、Vue 3 前端。

**ADR:** [docs/adr/0001-sub2api-market-consolidation.md](../../adr/0001-sub2api-market-consolidation.md)

---

## 已锁定决策（勿擅自推翻）

- [x] 保留双系统胶水（出站队列、影子用户）至资金与 Gateway 稳定之后
- [x] 少改 sub2api 原文件，优先新模块
- [x] 资金域最后动
- [x] 契约测试基线仍为 v0.2.14

---

## 文件与职责（阶段 1 起）

| 路径 | 职责 |
|---|---|
| `docs/adr/0001-*.md` | 架构决策 |
| `docs/superpowers/plans/2026-10-19-*.md` | 本实施计划 |
| `internal/sub2api/gateway/port.go` | 网关端口：账号/用户/分组/余额/代理等写操作抽象 |
| `internal/sub2api/gateway/http_adapter.go` | 现有 Admin/User HTTP 实现（委托 `adminclient`/`userclient`） |
| `internal/sub2api/gateway/doc.go` | 包说明与演进约束 |
| `internal/sub2api/ops` / `opshandlers` | 暂不删；handlers 逐步改为依赖 `gateway.Port` |
| 后续 `modules/market/` 或 sub2api 仓内 `market/` | 并仓落点（阶段 2） |

---

## 阶段总览

| 阶段 | 主题 | 可交付 | 预估 |
|---|---|---|---|
| 0 | ADR + 计划 | 文档合入 | 已开始 |
| 1 | Gateway 端口抽象 | HTTP 适配 + 单测；业务仍走原路径 | 1–2 周 |
| 2 | 并仓骨架 | sub2api fork 建 `market/` 模块或 go.work；CI 双测 | 1–2 周 |
| 3 | 供给内化准备 | 上架写路径经 Port；对账指标 | 3–4 周 |
| 4 | Gateway/Edge | 链式路由插件化或旁路同仓 | 4–6 周 |
| 5 | 质检 Worker | 探测走内部或同仓网关 | 4–6 周 |
| 6 | 资金 | 账本与余额边界设计 + 迁移 | 6–8 周 |
| 7 | 可选拆除胶水 | 仅当硬 fork 决策后 | TBD |

---

## 阶段 0 — 文档

### Task 0.1：ADR

- [x] 写入 `docs/adr/0001-sub2api-market-consolidation.md`
- [x] Commit：与阶段 1 同批

### Task 0.2：实施计划

- [x] 写入本文件
- [x] Commit：与阶段 1 同批

---

## 阶段 1 — Gateway 端口（当前执行）

### Task 1.1：定义 Port 接口

**Files:**
- Create: `internal/sub2api/gateway/doc.go`
- Create: `internal/sub2api/gateway/admin.go`

- [x] 定义 `Admin` 接口（覆盖出站 handlers 所用方法）
- [x] 接口方法签名复用 `adminclient` DTO
- [x] `go test` 编译通过

### Task 1.2：HTTP 适配器

**Files:**
- Create: `internal/sub2api/gateway/admin.go`（`HTTP(*adminclient.Client) Admin`）
- Create: `internal/sub2api/gateway/admin_test.go`

- [x] `HTTP` 委托：`*adminclient.Client` 直接满足 `Admin`
- [x] nil 防护 + 编译期断言单测

### Task 1.3：opshandlers 注入 Port

**Files:**
- Modify: `internal/sub2api/opshandlers/handlers.go`

- [x] `Register(s, c gateway.Admin)`；调用方仍可传 `*adminclient.Client`
- [x] 出站表语义不变；`go test ./internal/sub2api/...` 全绿

### Task 1.4：本地验证

- [x] `go test ./internal/sub2api/...`；`go build ./cmd/...`
- [ ] Commit：`refactor: 引入 sub2api Gateway 端口，保留出站路径`

---

## 阶段 2 — 并仓骨架（下一批）

### Task 2.1：sub2api fork 分支

- [ ] 在 `../github-fork/sub2api` 从 `v0.2.14` 建分支 `market/consolidation`
- [ ] 添加 `market/README.md`：说明模块边界与「禁止改 backend/internal 计费」清单
- [ ] 可选 `go.work` 同时包含 token-market 与 sub2api（仅本地开发）

### Task 2.2：迁移地图

- [ ] 维护 `docs/dev/consolidation-module-map.md`：`internal/listing|buyer|edge|...` → 目标仓路径
- [ ] 明确第一批搬迁包：`listing` 只读查询 / 健康检查类，不含资金

---

## 阶段 3+ — 摘要（详细任务在进入阶段时再拆）

3. 上架与调度写路径统一走 `gateway.Port`；增加同步延迟指标面板。  
4. Edge 与 sub2api 网关同仓部署；路由配置 schema 不变。  
5. 质检 worker 同仓；探测 key 仍用影子探测用户。  
6. 资金：独立设计「余额 vs 人民币账本」同库事务边界后再动。  
7. 仅当确认硬 fork：评估拆除 `sub2api_ops` 与影子用户。

---

## 测试策略

- 每阶段：`pnpm test`（若动前端）、`go test` 相关包、关键路径 `-tags integration`。
- 合并社区版本时：先升 sub2api tag，再跑 `s2a-compat` + e2e，最后改 HTTP 适配而非市场业务。
- 禁止：未过 billing/edge integration 就删出站队列。

---

## 风险与回滚

| 风险 | 缓解 |
|---|---|
| 接口抽象泄漏导致大面积改 handlers | 阶段 1 允许「只加 Port 不接线」 |
| 并仓后误改社区内核 | ADR + CODEOWNERS/目录禁令 |
| 社区大版本字段变更 | 变更收口在 `gateway/http_adapter` |

回滚：阶段 1 仅新增包时可直接删包；接线后回退单个 commit。

---

## 当前进度

- 阶段 0 文档：完成
- 阶段 1 代码：完成（待提交）
- 下一阶段：2.1 sub2api fork 分支 + market/README 边界说明
