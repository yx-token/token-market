# 管理端挂单与用户全量管理 Design

> 日期：2026-10-26  
> 状态：待评审  
> 前置：闲猫管理端 CRUD 迁移已齐；挂单无 admin API；用户页仅 KYC + 冻解。

## Goal

运营在宿主 `/admin/market` 内可对**市场商品（挂单）**与**用户相关数据**做业务级全字段管理；引擎用户/余额/Key 复用现有引擎 Admin API，不在闲猫页重写社区用户后台。

## Non-goals

- 通用「任意表任意列」SQL 后台
- 在 `market-admin` 重做完整 `frontend` UsersView（分组批量、属性矩阵等）
- 绕过账本直接改 `ledger_cash` 数字（调账仍走 funds / adjustments）
- 改买家调用扣费内核 / Edge 调度算法

## Scope（已确认：方案 2 / 「所有」）

| 域 | 能力 |
|---|---|
| 挂单 Listing | 列表、详情、创建（指定 upstream）、全业务字段更新、强制 pause/resume/close |
| 市场用户 | 列表、创建、详情编辑（email/status/is_admin/密码/KYC）、连带上游/挂单/资金单/风控 |
| 引擎用户 | 详情展示绑定；跳转或内嵌调用 `/api/v1/admin/users/:id` 改余额/并发/分组等 |
| 身份 | 未绑定可 ensure/bind（复用现有 identities） |

## Architecture

```
Admin SPA (market-admin)
  ListingsView ──► /api/v1/admin/market/listings*
  UsersView    ──► /api/v1/admin/market/users*
               ──► /api/v1/admin/users/:engineId   (引擎，已有)
               ──► /api/v1/admin/market/identities* (已有)
               ──► 连带：upstreams / listings?seller_id= / recharges?user_id= / risk-events

Engine routes/market.go
  relay 新增 listings*、users*（及扩展 GET users）
  ServeAdmin → market httpapi admin handlers
```

写操作一律 `audit(...)`；人民币入账不改本设计边界（见 `docs/dev/billing-ledger-boundary.md`）。

## API

### Listings

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/admin/listings` | 全站列表；query: `status`, `seller_id`, `upstream_id`, `platform`, `q`, `limit`, `offset` |
| GET | `/api/admin/listings/{id}` | 详情（含上游摘要） |
| POST | `/api/admin/listings` | 代创建；body 含 `upstream_id` + 卖家创建同构字段 |
| PUT | `/api/admin/listings/{id}` | 更新业务字段（见下） |
| POST | `/api/admin/listings/{id}/pause` | 强制暂停；`paused_by=admin` |
| POST | `/api/admin/listings/{id}/resume` | 强制恢复 |
| POST | `/api/admin/listings/{id}/close` | 强制关闭 |

规范前缀经引擎：`/api/v1/admin/market/listings*`。

**PUT 可写字段（对齐 `listing.Listing` 业务列）：**  
`status`（受限枚举）、`multiplier`、`join_default_pool`、`title`、`description`、`categories`、`sku_kind`、`face_value`、`stock`、`min_amount`。  
不可经本接口改：`id`、`seller_id`、`upstream_id`、`created_at`（换上游视为新挂单）。

**实现要点：** 在 `listing.Service` 增加 Admin 路径（跳过 sellerID 属主校验）；pause/resume/close 复用现有状态机，记录 `paused_by`。

### Users（市场）

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/admin/users` | 列表；query: `q`, `status`, `kyc_status`, `limit`, `offset` |
| GET | `/api/admin/users/{id}` | 详情：市场用户 + KYC + `engine_user_id`（若有） |
| POST | `/api/admin/users` | 创建（email/password）；可选随后 bind |
| PUT | `/api/admin/users/{id}` | 更新：`email`, `status`, `is_admin`, `password`（可选）, `kyc_status`（运营纠偏） |
| POST | `/api/admin/users/{id}/status` | 保留现有冻解 |

已有：`GET/POST kyc`、`POST kyc/{id}/review`。详情页继续用。

**连带只读（可筛）：** 现有 upstreams / listings / recharges / refunds / withdrawals / risk-events 增加或使用 `seller_id` / `user_id` query；不足则在 users 详情 handler 聚合返回 `related` 块。

### Engine users

不新增市场代理层。前端持引擎 Bearer 调：

- `GET/PUT /api/v1/admin/users/:id`
- `POST /api/v1/admin/users/:id/balance`
- `GET .../api-keys` 等（与现社区管理一致）

市场详情用 identities / shadow 表解析 `engine_user_id`。

## UI

### `/admin/market/listings`（新）

- y-page 列表 + 筛选 + 分页
- 弹层/侧栏编辑全可写字段
- 行内：暂停 / 恢复 / 关闭
- 「新建」：选 upstream（仅 ready）+ 模式/倍率等

侧栏：供给组或运营组挂「挂单」。

### `/admin/market/users`（扩展）

页签：

1. **列表** — 搜邮箱/ID，创建用户  
2. **KYC** — 保留现审核流  
3. **详情**（点行进入）  
   - 市场字段表单 + 保存  
   - 引擎卡片：未绑定 → ensure/bind；已绑定 → 摘要 + 链接 `/admin/users/:engineId`（或同页简单余额调账表单调引擎 API）  
   - 连带：上游、挂单、资金单、风控（只读表 + 链到对应管理页）

## Permissions

沿用 admin 会话 + 现有 `console.govern.users` 等；新路由挂 `adminRoutes` 与 `admin_perms`（listings 可用 `console.ops` 或新建 `console.market.listings`，与现网一致优先复用宽权限）。

## Testing

- Go：admin listings list/update/force-close；admin users get/put（含密码重置）  
- 前端：`vue-tsc` + i18n keys；可选组件 smoke  
- 手工：A-B-C 用例（创建挂单、强制下架、改用户状态、跳转引擎用户）

## Rollout

1. Backend handlers + `listing`/`account` Admin 方法 + `market.go` relay  
2. ListingsView + 路由/侧栏/i18n  
3. UsersView 扩展  
4. 文档：更新 `admin-c-end-split.md` 能力清单  

## Open questions（实现时可默认）

1. Admin 创建挂单是否跳过质检门禁？**默认：不跳过**（upstream 须已 tested/ready）。  
2. 改 `kyc_status` 是否写 KYC 审核流水？**默认：写一条 admin 纠偏 audit + 可选 note。**  
3. 引擎余额调账是否在闲猫详情内嵌？**默认：链到 `/admin/users/:id`；若一页内嵌则仅 amount/operation 两字段。**
