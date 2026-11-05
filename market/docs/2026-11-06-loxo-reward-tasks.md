# loxo 任务中心对接（2026-11-06）

## 背景

loxo（https://loxo.cc）有一个面向其用户的「任务中心」：用户在 loxo 点「去完成」，带着票据跳到合作站点，完成浏览 / 注册 / 充值后由 loxo 给用户发奖励额度。闲猫作为**合作站点（partner）**接入，协议见 loxo 文档 https://loxo.cc/docs/reward-integration （页面是 SPA，正文在其前端包里；服务端实现见 loxo 仓库 `backend/internal/service/reward_task_open.go`）。

我们要做的三件事：

| 任务类型 | loxo 校验方式 | 闲猫要做的 |
| --- | --- | --- |
| browse（浏览） | sdk | 落地页加载 loxo 的 `reward-task-sdk.js`，SDK 自己心跳计时 |
| register（注册） | callback | 用户带票据**新注册**成功后，服务端签名回调 `events`（`event_type=register`） |
| recharge（充值） | callback | 绑定票据后，用户每笔到账的钱包充值签名回调 `events`（`event_type=recharge`，按票据累计） |

browse + timer 模式不需要合作站点做任何事；loxo 那边给闲猫配置浏览任务时请用 **sdk** 模式。

## 协议要点（摘自 loxo 文档与源码）

- 开放接口前缀 `{base}/api/v1/reward-open`：`ping`、`tickets/query`、`events` 需签名，`sdk/heartbeat` 由浏览器 SDK 调用、不签名。
- 签名：`X-Reward-App-Key` / `X-Reward-Timestamp`（Unix 秒，±300s）/ `X-Reward-Nonce`（16~64 位 `[A-Za-z0-9_-]`，10 分钟内不可重复）/ `X-Reward-Signature` = `hex_lower(HMAC_SHA256(app_secret, timestamp + "\n" + nonce + "\n" + raw_body))`。
- 响应信封：成功 `{"code":0,"message":"success","data":{...}}`；失败 `{"code":<HTTP 状态>,"message":"...","reason":"REWARD_..."}`。
- 票据 `rt_` 开头，最长 64 字符；跳转时 loxo 追加 `?reward_ticket=`（目标网址含 `{ticket}` 占位符时改为替换）。
- `events` 的 `event_id` 是幂等键：同一 `(app_key, event_id)` 重复上报直接返回首次结果（`duplicate: true`）；被拒（rejected）的事件可用同一 `event_id` 修正后重试。
- `amount` 单位元，可传字符串，正则 `^[0-9]+(\.[0-9]{1,2})?$`。名额已满（`REWARD_TASK_QUOTA_EXHAUSTED`）时 loxo **保留**本次累计，之后用同一 `event_id` 重试只尝试发奖、不重复累加。
- 同一 `external_user_id` 在同一任务同一周期只能绑定一个 loxo 用户；票据一经绑定 `external_user_id` 不能再换。

## 闲猫侧设计

### 配置（管理端「闲置市场 → 任务对接」）

存在 market 库 `settings` 表，key = `reward_partner`：

```json
{ "enabled": false, "base_url": "https://loxo.cc", "app_key": "rk_...", "app_secret_enc": "<base64，Keyring 加密>", "sdk_enabled": true }
```

- `app_secret` 只写不读：保存时传空串表示不修改；接口只返回 `secret_set: true|false`。
- `base_url` 必须是 `https://` 开头的站点根地址（仅允许 `http://localhost` / `http://127.0.0.1` 用于本地联调），保存时去掉末尾 `/`。
- `enabled=false` 时：落地不查票据、不加载 SDK、不绑定新票据、worker 不发请求（已有队列保留，重新开启后继续）。

### 票据流转

1. **落地（C 端，未登录也可）**：`market/web` 启动时同步读取 `?reward_ticket=`，格式合法（`^rt_[A-Za-z0-9_-]{8,61}$`）就写入 `localStorage.tm_reward_ticket = {"ticket","touched_at"}`，并用 `history.replaceState` 从地址栏去掉（必须赶在路由初始化之前，同推广渠道 `ch`）。然后异步 `POST /api/market/reward/landing {ticket}`：
   - 服务端用 `tickets/query` 查任务类型（按 IP 限流 30 次/分钟，全站 300 次/分钟：loxo 按我们的出口 IP 限 600 次/分钟，要给 worker 回调留余量）。
   - 返回 `browse` 且开启了 SDK：前端把票据写入 `sessionStorage.reward_task_ticket`（SDK 自己的存储键）、把 `{script_url, app_key}` 写入 `sessionStorage.tm_reward_sdk`，再动态插入 `<script src="{base}/reward-task-sdk.js" data-app-key=... data-lang="zh">`；浏览任务不需要绑定账号，同时清掉 `tm_reward_ticket`。
   - 返回 `valid: false`（票据不存在 / 不属于本站 / 已过期）：清掉 `tm_reward_ticket`。
   - 其它情况（注册 / 充值任务，或查询失败）：保留，等注册或登录后绑定。
   - 同一标签页整页刷新时，如果 `sessionStorage` 里还有 `reward_task_ticket` 与 `tm_reward_sdk`，直接重新插入 SDK（SDK 完成或终态出错时会自己删掉 `reward_task_ticket`）。
2. **注册**：`POST /api/auth/register` 新增可选字段 `reward_ticket`。注册成功后服务端写一条 `new_user = true` 的票据记录；失败只记日志，**不得**让注册失败。注册成功后前端清掉 `tm_reward_ticket`。
3. **已有账号登录 / 已登录时到达**：前端在登录成功、或启动时已登录的情况下调用 `POST /api/me/reward-ticket {ticket}`（`new_user = false`），成功或 4xx 后清掉本地票据。
4. **worker（`reward.sync`，每 10 秒）**：
   - 查询 `pending` 票据（`tickets/query`），按结果：
     - 已完成 → `completed`；已过期 / 不存在 / 不属于本站 / 已绑定别的 `external_user_id` → `invalid`；
     - `register`：`new_user` → `active` 并入队一条注册事件；否则 → `skipped`（`NOT_NEW_USER`，老用户登录不算注册）；
     - `recharge` → `active`；`browse` → `skipped`（`BROWSE_TASK`，由 SDK 负责）。
   - `active` 的充值票据：把该用户**绑定票据之后下单**（`created_at >= 绑定时间`）、**票据过期之前支付**（`paid_at < expires_at`）、状态为 `credited` 的充值单入队为充值事件，金额 = `amount - refunded_amount`（> 0 才报）。起点不用 `paid_at`：它取自支付渠道，只精确到秒且是对方时钟，绑定后马上付款时会早于绑定时间（本地联调实测踩到）。
   - 过期的 `active` 票据 → `invalid`（`REWARD_TICKET_EXPIRED`），但已入队的事件照常发送。
   - 发送 `pending` 事件。

`external_user_id` 用闲猫用户 ID 的十进制字符串。

### 事件 `event_id`

- 注册：`tm-reg-{票据记录 id}`
- 充值：`tm-rc-{充值单 id}-{票据记录 id}`（同一笔充值可同时计入用户手上不同任务的票据）

### 状态

`reward_tickets.state`：`pending`（待查询）→ `active` / `completed` / `skipped` / `invalid`；`active` → `completed` / `invalid`。

`reward_events.state`：`pending` → `accepted`（已计入未达标）/ `completed`（本次促成完成，已发奖）/ `ignored`（早已完成）/ `rejected`（业务拒绝，终态）/ `failed`（重试用尽）。

发送结果处理：

| 情况 | 处理 |
| --- | --- |
| 网络错误、5xx、429、`REWARD_SIGNATURE_*` / `REWARD_TIMESTAMP_EXPIRED` / `REWARD_NONCE_REPLAYED` / `REWARD_PARTNER_INVALID`（凭证配错）、`REWARD_TASKS_DISABLED` | 保持 `pending`，退避重试（30s 起翻倍，封顶 1 小时），48 次后 `failed` |
| `REWARD_TASK_QUOTA_EXHAUSTED`、`REWARD_TASK_UNAVAILABLE` | 保持 `pending`，每 6 小时重试一次，直到票据过期后 `rejected` |
| `REWARD_TASK_ALREADY_COMPLETED` | 事件 `ignored`，票据 `completed` |
| 其它 `REWARD_*`（票据过期 / 不存在 / 不属于本站、`external_user_id` 冲突、事件类型不符、参数不合法） | 事件 `rejected`；票据类原因同时把票据置 `invalid` |

管理员可以对 `rejected` / `failed` 的事件点「重试」，状态回到 `pending`，用同一个 `event_id` 重发。

已知限制：loxo 没有「撤销 / 负数」事件，充值上报后用户再申请原路退款，loxo 侧的累计不会扣减（目前买家退款被产品政策整体关闭，影响有限）。

## 数据表（market 库，迁移 0051）

```sql
reward_tickets (
  id bigserial PK, ticket text UNIQUE NOT NULL, user_id bigint NOT NULL REFERENCES users,
  new_user boolean NOT NULL DEFAULT false,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','active','completed','skipped','invalid')),
  reason text NOT NULL DEFAULT '',
  task_id bigint, task_title text NOT NULL DEFAULT '', task_type text NOT NULL DEFAULT '',
  period_key text NOT NULL DEFAULT '', expires_at timestamptz,
  attempts int NOT NULL DEFAULT 0, next_check_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
)
reward_events (
  id bigserial PK, ticket_id bigint NOT NULL REFERENCES reward_tickets,
  event_id text UNIQUE NOT NULL, event_type text NOT NULL CHECK (event_type IN ('register','recharge')),
  recharge_order_id bigint REFERENCES recharge_orders, amount numeric(18,2),
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','completed','ignored','rejected','failed')),
  reason text NOT NULL DEFAULT '', rewarded boolean NOT NULL DEFAULT false,
  attempts int NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
  response jsonb, sent_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (ticket_id, recharge_order_id)
)
```

## 接口

### C 端

- `POST /api/market/reward/landing`（公开）`{ "ticket": "rt_..." }` → 始终 `200`：
  `{ "valid": true|false|null, "task_type": "browse|register|recharge|", "sdk": { "script_url": "https://loxo.cc/reward-task-sdk.js", "app_key": "rk_..." } | null }`。
  `valid: null` 表示没查成（未启用、限流、网络问题），前端照常保留票据。
- `POST /api/auth/register` 新增可选字段 `reward_ticket`。
- `POST /api/me/reward-ticket`（需登录）`{ "ticket": "rt_..." }` → `200 {"accepted": true|false}`；格式非法 `400 INVALID_REWARD_TICKET`。票据已被其他闲猫账号绑定时 `accepted: false`。

### 管理端

market 原生路径 `/api/admin/reward-tasks/...`；引擎侧 `/api/v1/admin/market/reward-tasks/...` 全部走 relay。权限资源 `console.ops.reward`（「任务对接」，平台管理员与运营默认有）。

1. `GET reward-tasks/config` → `{ "enabled", "base_url", "app_key", "secret_set", "sdk_enabled", "updated_at", "summary": { "tickets": {"pending":0,...}, "events": {"pending":0,...} } }`
2. `PUT reward-tasks/config`，body `{ "enabled", "base_url", "app_key", "app_secret"（空串=不改）, "sdk_enabled" }` → 同上。启用时 `app_key` 与 `app_secret` 必须都已配置。
3. `POST reward-tasks/ping` → `{ "partner_name", "app_key", "server_time", "clock_skew_seconds" }`；loxo 返回错误时 `502 REWARD_PING_FAILED`，`message` 带 loxo 的 `reason`。
4. `GET reward-tasks/tickets?state=&page=1&page_size=20` → `{ "items": [{ id, ticket, user_id, user_email, new_user, state, reason, task_id, task_title, task_type, period_key, expires_at, attempts, created_at, updated_at }], "total" }`，按 id 倒序。
5. `GET reward-tasks/events?state=&page=1&page_size=20` → `{ "items": [{ id, ticket_id, ticket, user_id, user_email, task_title, event_id, event_type, recharge_order_id, amount, state, reason, rewarded, attempts, next_attempt_at, sent_at, created_at }], "total" }`。
6. `POST reward-tasks/events/{id}/retry` → 事件对象；只允许 `rejected` / `failed`，否则 `409 REWARD_EVENT_NOT_RETRYABLE`。

## CSP

引擎给 C 端页面下发的 CSP `script-src` 默认不含 loxo 域名，SDK 会被拦截。`SecurityHeaders` 新增可选的外部脚本来源：内嵌市场开启且配置了「启用 + SDK」时，把 `base_url` 的 origin 同时加进 `script-src`（加载 SDK）和 `connect-src`（SDK 心跳）。后者不依赖默认策略里的 `https:`：自定义了 `security.csp.policy` 或本地用 http 联调时心跳也不会被拦。在本进程保存配置立即生效，其它情况最多 1 分钟。

## 上线步骤

1. 请 loxo 管理员登记合作站点「闲猫」：SDK 允许域名填 `idlicat.com`（如有 www 再加 `www.idlicat.com`），拿到 `app_key` / `app_secret`。
2. 闲猫后台「任务对接」填入凭证，点「测试连接」确认 `partner_name` 正确，再打开「启用」。
3. loxo 侧任务目标网址填 `https://idlicat.com/`（loxo 会自动追加 `reward_ticket`）；浏览任务用 sdk 模式。
4. 用测试账号走一遍：loxo 点「去完成」→ 闲猫注册 / 充值 → 后台「任务对接」看事件变为 `completed`，loxo 侧额度到账。
