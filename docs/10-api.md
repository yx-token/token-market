# 10 第一期接口

控制面接口由 market-api 提供，前缀 `/api`，JSON，平台用户 JWT 认证；管理后台接口额外要求管理员角色。买家调用模型的接口由 Edge 提供，见 [07](07-edge.md)。

以下只列接口和关键字段，完整定义在实现时用 OpenAPI 描述。

## 1. 账号

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 注册 |
| POST | `/api/auth/login` | 登录 |
| POST | `/api/auth/refresh` | 刷新 token |
| GET | `/api/me` | 当前用户信息，含买家 / 卖家能力 |
| POST | `/api/me/kyc` | 提交实名（卖家提现前必需） |

## 2. 市场（买家浏览）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/market/listings` | 寄售挂单列表。筛选：厂商、模型族、智力分档位、价格区间、可用率；排序：价格、智力分、可用率 |
| GET | `/api/market/listings/:id` | 挂单摘要：厂商、档位、买家价 p、健康状态、智力分档位、24h / 7d 可用率、首字延迟、卖家信用 |
| GET | `/api/market/pools` | 默认分组列表及价格 D |
| GET | `/api/market/prices` | 官方价目表（倍率的计算基准） |

## 3. 买家

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/buyer/wallet` | 当前余额（读 sub2api 实时余额）、现金与赠送余额拆分、充值与退款记录 |
| POST | `/api/buyer/recharge` | 创建充值订单 |
| GET | `/api/buyer/recharge/:id` | 充值订单状态（待支付 / 到账中 / 已到账） |
| POST | `/api/buyer/refunds` | 申请退还剩余现金余额（期间暂停该买家所有 Key） |
| POST | `/api/buyer/keys` | 创建 Key，明文只返回一次 |
| GET | `/api/buyer/keys` | Key 列表 |
| GET | `/api/buyer/keys/:id` | Key 详情，含各跳的当前状态（正常 / 跳过 / 熔断） |
| PUT | `/api/buyer/keys/:id` | 修改名称、路由、限制 |
| POST | `/api/buyer/keys/:id/disable`、`/enable` | 停用 / 启用 |
| DELETE | `/api/buyer/keys/:id` | 删除 |
| GET | `/api/buyer/usage` | 用量明细，可按 Key、挂单、模型、时间筛选 |
| GET | `/api/buyer/usage/:edge_request_id` | 单次请求详情：每一跳的状态（失败的跳标注"未产生费用"）、实际服务的挂单、token、费用、中途断开标注 |
| GET | `/api/buyer/bills` | 账单（按日汇总，含人工调整） |

创建 Key 示例：

```json
{
  "name": "claude-code",
  "route": {
    "type": "chain",
    "hops": [
      { "listing_id": 101 },
      { "listing_id": 205 },
      { "pool": "tm-pool-claude" }
    ]
  },
  "limits": {
    "quota_usd": 50,
    "rate_limit_1d_usd": 10,
    "ip_whitelist": ["203.0.113.0/24"],
    "expires_at": "2026-12-31T00:00:00Z"
  }
}
```

校验：最多 5 跳；默认分组只能在最后一跳；同一挂单不能重复出现；第一期要求链内各跳是同一平台，最后一跳的默认分组必须是同平台分组或 `tm-pool-all`，避免同一请求在不同协议的上游之间转换。

## 4. 卖家

### 4.1 上游

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/vendors` | 可接入的厂商与接入方式（由管理员启用的厂商决定） |
| POST | `/api/seller/upstreams` | 创建上游草稿：`platform`、`auth_type` |
| POST | `/api/seller/upstreams/:id/oauth/start` | 获取授权链接 |
| POST | `/api/seller/upstreams/:id/oauth/complete` | 提交授权码 |
| POST | `/api/seller/upstreams/:id/coding-key` | 提交 Coding Plan key |
| GET | `/api/seller/upstreams` | 上游列表 |
| GET | `/api/seller/upstreams/:id` | 详情：状态、健康状态、档位、窗口用量、近期指标 |
| PUT | `/api/seller/upstreams/:id/settings` | 修改自留比例 |
| POST | `/api/seller/upstreams/:id/reauthorize` | 重新授权（授权失效时） |
| DELETE | `/api/seller/upstreams/:id` | 删除上游 |

### 4.2 挂单

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/seller/upstreams/:id/listing` | 创建挂单：`mode`（consignment / buyback）；寄售需填 `multiplier`、`join_default_pool` |
| GET | `/api/seller/listings/:id` | 挂单详情，含价格历史 |
| GET | `/api/seller/upstreams/:id/quote` | 查看回收报价 |
| POST | `/api/seller/listings/:id/accept-quote` | 接受回收报价 |
| PUT | `/api/seller/listings/:id/price` | 寄售改价（有冷却时间） |
| PUT | `/api/seller/listings/:id/default-pool` | 加入 / 退出默认分组（仅寄售） |
| POST | `/api/seller/listings/:id/pause`、`/resume`、`/close` | 暂停、恢复、关闭 |

### 4.3 测试报告

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/upstreams/:id/reports` | 报告列表 |
| GET | `/api/seller/reports/:rid` | 报告详情（脱敏，见 [05](05-quality.md) 第 8 节） |
| GET | `/api/seller/upstreams/:id/quality-trend` | 近 30 天智力相对分与可用率趋势 |
| POST | `/api/seller/upstreams/:id/retest` | 手动复测（每日限次，仅该厂商开启检测时可用） |
| POST | `/api/seller/reports/:rid/appeal` | 申诉 |

### 4.4 收益与提现

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/seller/earnings` | 待结算、可提现、提现中、累计收益 |
| GET | `/api/seller/earnings/lines` | 按上游、小时、渠道的结算明细 |
| POST | `/api/seller/withdrawals` | 申请提现 |
| GET | `/api/seller/withdrawals` | 提现记录 |

## 5. 管理后台

| 模块 | 接口 |
|---|---|
| 检测策略 | `GET /api/admin/detection-policies`、`PUT /api/admin/detection-policies/:platform`（含 `enabled`、`scheduled_enabled` 等，见 [05](05-quality.md) 第 3 节） |
| 单上游检测覆盖 | `PUT /api/admin/upstreams/:id/detection-override`、`DELETE …` |
| 手动检测 | `POST /api/admin/upstreams/:id/probe` |
| 题库 | `GET/POST/PUT /api/admin/probe-items`、`POST /api/admin/probe-items/:id/retire` |
| 基线账号 | `GET/POST/DELETE /api/admin/reference-accounts`（复用卖家的授权流程） |
| 质量看板 | `GET /api/admin/quality/overview`（各厂商健康分布、官方全局事件、降智上游列表） |
| 申诉 | `GET /api/admin/appeals`、`POST /api/admin/appeals/:id/resolve` |
| 上游与挂单 | `GET /api/admin/upstreams`、`POST /api/admin/upstreams/:id/suspend`、`/unsuspend` |
| 默认分组 | `GET/POST/PUT /api/admin/pools`（售价 D、最低毛利、安全缓冲、段内策略） |
| 价格设置 | `GET/PUT /api/admin/pricing`（抽佣率 c、各厂商倍率区间、回收基础倍率表、等级系数、报价有效期） |
| 厂商启用 | `GET/PUT /api/admin/vendors`（是否允许卖家接入、默认并发与限流参数） |
| 代理库存 | `GET/POST/PUT/DELETE /api/admin/proxies` |
| 提现审核 | `GET /api/admin/withdrawals`、`POST /api/admin/withdrawals/:id/approve`、`/reject` |
| 风控 | `GET /api/admin/risk-events`、关联关系查询 |
| 账本 | `GET /api/admin/ledger`、`POST /api/admin/ledger/adjustments`（需审批） |
| sub2api 状态 | `GET /api/admin/sub2api/status`（版本、兼容自检结果、安全模式、出站队列积压、对账漂移） |
| 审计 | `GET /api/admin/audit-logs` |
| 系统设置 | `GET/PUT /api/admin/settings`（冻结天数、最低提现额等） |
