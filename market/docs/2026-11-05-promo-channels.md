# 推广渠道：专属链接与归因统计（2026-11-05）

## 目标

推广阶段要知道哪个渠道有效果。管理员在后台新建渠道，拿到专属链接；用户经链接到达、注册、充值、消费，都能按渠道统计。

## 术语

- **推广渠道（promo channel）**：一个推广位，比如「V2EX 帖子」「某 UP 主视频」。与 sub2api 的上游「渠道」（channel）、`usage_records.channel`、`recharge_orders.channel`（支付方式）无关。代码一律用 `promo` 前缀，避免混淆。
- **渠道码（code）**：链接里的 `ch` 参数，`^[a-z0-9_-]{2,32}$`，统一小写，创建后不可修改（链接已经发出去了）。

## 专属链接

`{站点 origin}{landing_path}?ch={code}`（`landing_path` 已带查询串时用 `&ch=`）。

- `landing_path` 默认 `/`，必须以单个 `/` 开头（拒绝 `//`、`http:`、反斜杠），长度不超过 512，防止被拿来做开放跳转。
- 链接由管理端前端用 `window.location.origin` 拼出（嵌入模式下管理端与 C 端同域，线上即 `https://idlicat.com`）。

## 归因规则

- **最后一次点击，30 天有效**：浏览器每次带 `ch` 到达都会覆盖本地记录；注册时若记录未超过 30 天，用户归到该渠道。
- 归因只在**注册**时发生一次，之后不再改变。已登录用户再点链接只记访问，不改归属。
- 停用的渠道：访问不记录，注册不归因。
- 注册后该用户的充值、消费、上架都算到所属渠道（终身归属），统计时按事件发生时间筛选区间。
- 没有渠道的注册用户算「自然流量」，在汇总里单独给出，用于对比。

## 数据表（market 库）

```sql
promo_channels (
  id bigserial PK, code text UNIQUE NOT NULL, name text NOT NULL,
  landing_path text NOT NULL DEFAULT '/', note text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
  created_by bigint, created_at timestamptz, updated_at timestamptz
)
promo_channel_visits (
  id bigserial PK, channel_id bigint NOT NULL REFERENCES promo_channels,
  visitor_id text NOT NULL, ip text, user_agent text, referrer text, landing_path text,
  created_at timestamptz NOT NULL DEFAULT now()
)  -- 索引 (channel_id, created_at)、(channel_id, visitor_id)
promo_channel_users (
  user_id bigint PRIMARY KEY REFERENCES users, channel_id bigint NOT NULL REFERENCES promo_channels,
  visitor_id text, touched_at timestamptz,          -- 前端记录的最后一次点击时间
  created_at timestamptz NOT NULL DEFAULT now()     -- 归因时间 = 注册时间
)  -- 索引 (channel_id, created_at)
```

## 统计口径

区间参数 `from` / `to` 为 `YYYY-MM-DD`，按北京时间（Asia/Shanghai）自然日、两端都含；都不传表示全部时间。

| 字段 | 含义 |
| --- | --- |
| `pv` | 区间内访问次数（`promo_channel_visits` 行数） |
| `uv` | 区间内去重访客数（`visitor_id` 去重） |
| `signups` | 区间内归因到该渠道的注册数 |
| `paying_users` | 该渠道用户中，区间内有到账充值（`recharge_orders.status='credited'`，按 `credited_at`，没有则 `paid_at`）的人数 |
| `recharge_amount` | 上述充值的金额合计，扣除已原路退回部分（`amount - refunded_amount`） |
| `buyer_spend` | 该渠道用户在区间内的 `usage_records` 消费合计，按 `buyer_user_id` 归属、`created_at` 筛选，金额为 `SUM(COALESCE(cash_cost, actual_cost) + credit_prepaid_cny)`（现金消费 + 已消耗的预付额度成本）；复用买家中心日账单 `market/internal/billing/reports.go` 的 `BuyerBills` 口径 |
| `seller_users` | 该渠道用户中，区间内新建过挂单的人数 |

金额字段一律是保留 2 位小数的字符串，例如 `"128.50"`。

## 接口

### C 端（公开，无需登录）

`POST /api/market/promo/visit`

```json
{ "code": "v2ex-post", "visitor_id": "<8~64 位 [A-Za-z0-9_-]>", "landing_path": "/", "referrer": "https://..." }
```

始终返回 `200 {"accepted": true|false}`，不因渠道不存在、已停用、限流而报错。服务端按 IP 限流；同一渠道同一访客 10 分钟内重复上报只记一次。`referrer` / `user_agent` 截断到 512 字符。

`POST /api/auth/register` 新增三个可选字段（缺省时行为与现在完全一致）：

```json
{ "email": "...", "password": "...", "channel_code": "v2ex-post", "visitor_id": "...", "channel_touched_at": "2026-11-05T08:00:00Z" }
```

渠道存在且启用、`channel_touched_at` 在 30 天内（未传则视为有效）时写入 `promo_channel_users`。归因失败只记日志，**不得**让注册失败。

### 管理端

market 原生路径 `/api/admin/promo-channels...`；引擎侧 `/api/v1/admin/market/promo-channels...` 全部走 relay（`registerIdleCatRelay`），不做 native 简表。响应体即 market 原样 JSON。

1. `GET promo-channels?from=&to=`

   ```json
   {
     "from": "2026-10-07", "to": "2026-11-05",
     "items": [{
       "id": 1, "code": "v2ex-post", "name": "V2EX 帖子", "landing_path": "/", "note": "",
       "status": "active", "created_at": "...", "updated_at": "...",
       "stats": { "pv": 0, "uv": 0, "signups": 0, "paying_users": 0,
                  "recharge_amount": "0.00", "buyer_spend": "0.00", "seller_users": 0 }
     }],
     "totals": { "...": "所有渠道 stats 求和，字段同 stats" },
     "organic": { "signups": 0, "paying_users": 0, "recharge_amount": "0.00", "buyer_spend": "0.00", "seller_users": 0 }
   }
   ```

   `from`/`to` 未传时返回 `null`。`items` 按 `created_at` 倒序。

2. `POST promo-channels`，body `{ "name": "必填，≤64", "code": "可选，留空自动生成 6 位小写字母数字", "landing_path": "可选", "note": "可选，≤500" }` → `201` + 渠道对象（含 `stats` 全 0）。渠道码重复 → `409 PROMO_CODE_TAKEN`；校验失败 → `400`。
3. `PUT promo-channels/{id}`，body `{ "name", "landing_path", "note" }`，渠道码不可改 → `200` + 渠道对象。
4. `POST promo-channels/{id}/status`，body `{ "status": "active" | "disabled" }` → `200` + 渠道对象。
5. `DELETE promo-channels/{id}`：只有没有任何访问和注册的渠道能删（用于删错建的），否则 `409 PROMO_CHANNEL_HAS_DATA`。
6. `GET promo-channels/{id}/daily?from=&to=`（默认最近 30 天，最长 366 天）→ `{ "items": [{ "date": "2026-11-05", "pv", "uv", "signups", "paying_users", "recharge_amount", "buyer_spend", "seller_users" }] }`，没有数据的日期补 0，按日期升序。
7. `GET promo-channels/{id}/users?page=1&page_size=20` → `{ "items": [{ "user_id", "email", "registered_at", "recharge_amount", "buyer_spend", "is_seller" }], "total": 0 }`，金额为该用户全部时间合计，按注册时间倒序。

错误体沿用 market 现有格式。

## C 端采集（market/web）

- 应用启动时读 `location.search` 的 `ch`，合法则：取或生成 `localStorage.tm_visitor_id`；写 `localStorage.tm_promo = {"code","touched_at"}`（覆盖旧值）；fire-and-forget 调 `/api/market/promo/visit`；用 `history.replaceState` 去掉 URL 里的 `ch`，其余参数保留。
- 注册（`LoginModal` 注册模式与 `RegisterView`）时，若 `tm_promo` 未超过 30 天，带上 `channel_code` / `visitor_id` / `channel_touched_at`；注册成功后清除 `tm_promo`（保留 `tm_visitor_id`）。
- 任何采集失败都不能影响页面和注册。

## 管理端页面（frontend/src/market-admin）

路由 `/admin/market/promo-channels`，侧边栏「推广渠道」。列表 + 日期区间筛选 + 汇总卡片（含自然流量对比）；新建/编辑弹窗；复制链接；启用/停用；无数据可删除；详情抽屉（每日趋势 + 用户列表）。
