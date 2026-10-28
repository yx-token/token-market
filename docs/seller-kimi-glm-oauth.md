# 卖家接入端支持 Kimi / 智谱 Coding Plan OAuth 上号

- 状态：待实施
- 日期：2026-10-28
- 背景：Kimi 与智谱的 Coding Plan 订阅不再提供 API Key，引擎侧（管理端）已支持 OAuth 上号；卖家中心目前仍只支持 API Key。

## 一、现状（已核实）

| 侧 | 事实 | 位置 |
|---|---|---|
| 卖家前端 | 完全泛化：用 `isKeyAuthType(authType)` 二分，非 key 类走「授权链接 + 粘贴授权码」，不硬编码厂商 | `market/web/src/utils/authGuides.ts`、`market/web/src/views/seller/` |
| 市场后端 | `oauthSpecs` 只登记 anthropic/openai/gemini/antigravity/grok；kimi/zhipu 只有 `codingSpecs`（纯 API Key） | `market/internal/listing/vendors.go:31-118` |
| 市场后端 | 卖家建上游请求体 `{platform, auth_type, proxy_id}`；校验是 DB `vendor_configs.auth_types` + 代码 spec 双重门 | `handlers_seller.go:14-19`、`onboarding.go:90-93` |
| 市场后端 | 市场不存凭证，只存 `identity_hash` 与 `sub2api_account_id`；建号走 admin client `POST /api/v1/admin/accounts` | `onboarding.go:593-633`、`provision.go:64-78` |
| 授权会话 | `upstream_oauth_sessions(upstream_id PK, session_id, state, auth_type, auth_url NOT NULL, expires_at)`，固定 30 分钟 | `migrations/0003_listing.sql:72-80`、`onboarding.go:330-348` |
| 引擎 | accounts 表无 seller/owner 字段；`/admin` 组只挂 `adminAuth`，无权限键 | `backend/ent/schema/account.go:51-229`、`routes/admin.go:26-38` |
| 引擎 | Kimi 设备码 `POST /api/v1/admin/kimi/oauth/device-code` + `poll`；智谱 `POST /api/v1/admin/zhipu/oauth/init` + `poll` + `exchange-code` | `routes/admin.go:514-522`、`:919-925` |

## 二、方案选型

**复用引擎已实现的 OAuth 协议，把「放宽权限」严格限制在 market 进程的 HTTP 边界上，引擎鉴权一行不改。**

理由：
1. 引擎 accounts 表没有卖家归属字段，凭空给卖家开 admin 接口无法校验归属。
2. market 是引擎 Admin API 的唯一调用方（共享 x-api-key），卖家 JWT 到不了引擎。
3. 卖家「只能动自己的账号」由 `GetSellerUpstream(seller_id)` 结构性保证。
4. 协议（设备码 / init-poll / PKCE / 状态码）已经在引擎里测过，市场侧不再复刻一遍，避免漂移（复刻约 700-900 行且要自己做出站代理白名单）。

关键约束（不可协商）：
- **session_id 不下发浏览器、也不从请求体接收**。引擎 session 只存市场库 `upstream_oauth_sessions`，poll 时用已校验归属的 `upstream_id`（路径参数）取回。
- **凭证不出服务端**。poll 成功时市场在服务端把 token 转成账号凭证并调引擎建号，前端只看到 `status` 与 `upstream`。
- **代理归属**：`proxy_id` 不从请求体取，用 `upstream.proxy_id` 经 `proxySub2APIID` 换算，保持智谱 `PROXY_MISMATCH` 校验有效。

## 三、实施清单

### A. 接入方式注册表（`market/internal/listing/vendors.go`）

`oauthSpec` 增加 `flow` 字段：`link-code`（现有，粘贴授权码）、`device-code`（Kimi）、`init-poll`（智谱）。

新增：

- `kimi: oauth` → flow=device-code
  - start 调 `POST /api/v1/admin/kimi/oauth/device-code`，返回 `{session_id, user_code, verification_uri, verification_uri_complete, expires_in, interval}`
  - poll 调 `POST /api/v1/admin/kimi/oauth/poll`，`{status, token_info}`
  - credentials：`access_token, refresh_token, expires_at(RFC3339), token_type, scope, device_id, client_id, base_url=https://api.kimi.com/coding/v1, account_mode=coding`
  - identity：空（Kimi 无稳定主体标识，见风险）
- `zhipu: oauth`（国内 BigModel）与 `zhipu: oauth-intl`（国际 Z.ai）→ flow=init-poll
  - init 调 `POST /api/v1/admin/zhipu/oauth/init`，`{provider: bigmodel|zai, proxy_id}`
  - poll 调 `POST /api/v1/admin/zhipu/oauth/poll`
  - 成功结果 `result.credentials` 整体作为凭证，另补 `account_mode=coding`、按站点补 `base_url`、`api_protocol`
  - identity：`display.email / display.username`

同时：`normalizeCredentials` 按 flow 分派；`startOAuth` 不再硬性要求 `auth_url`。

### B. 授权会话（迁移 + `onboarding.go`）

新迁移 `market/migrations/0042_upstream_oauth_flows.sql`：

```
ALTER TABLE upstream_oauth_sessions
  ALTER COLUMN auth_url DROP NOT NULL,
  ADD COLUMN kind text NOT NULL DEFAULT 'link',
  ADD COLUMN provider text,
  ADD COLUMN user_code text,
  ADD COLUMN verification_uri text,
  ADD COLUMN interval_seconds int,
  ADD COLUMN next_poll_at timestamptz,
  ADD COLUMN last_status text;
```

`beginOAuth` 按 flow 分派；`expires_at` 用引擎返回的 `expires_in`，不要再用固定 30 分钟（Kimi 约 15 分钟、智谱约 5 分钟）。

### C. 卖家接口（`market/internal/listing/onboarding.go` + `httpapi/server.go`）

- `POST /api/seller/upstreams/{id}/oauth/start` 保留，返回体按 flow 增加可选字段：`flow, user_code, verification_uri, verification_uri_complete, interval`（`auth_url` 对 device-code 为空）。
- **新增** `POST /api/seller/upstreams/{id}/oauth/poll`，卖家会话鉴权 + `GetSellerUpstream` 校验归属，按 `next_poll_at` 节流，回 `{status: pending|slow_down|ready|expired|denied, interval}`；ready 时在服务端完成建号并返回 `upstream`。
- `complete` 仅对 `link-code` / `init-poll`（智谱可选手动粘贴回调）有效；`device-code` 走 poll。

### D. 引擎侧加固（可选但建议同批）

- `backend/internal/service/zhipu_oauth_service.go` 的 `BuildResult` 补 `account_mode: "coding"`（现在依赖管理端前端补，绕过前端的调用方会掉进 payg 计费）。
- `backend/internal/server/middleware/audit_log.go` 的整体丢弃清单补上 `/kimi/oauth/poll` 与 `/api/v1/admin/kimi/oauth/device-code`。

### E. 卖家前端（`market/web`）

- 新增设备码面板组件：展示 `user_code`（可复制）、`verification_uri_complete`（可复制/新窗口打开）、倒计时、状态；按 `interval` 轮询 `/oauth/poll`，`slow_down` 加大间隔，`expired/denied` 给明确提示。
- 智谱：展示授权链接 + 可选「手动粘贴回调地址」折叠块。
- 接入方式下拉：kimi 增加「OAuth 授权」，zhipu 增加「OAuth 授权（国内站 / 国际站）」。
- 复用现有 `AuthorizationStart` / `Upstream` 类型，不要 import `frontend/` 的组件（两个前端独立构建，且管理端组件依赖 vue-i18n 与 admin store）。

### F. 测试与验收

- 后端单测：flow 分派、poll 状态机（pending/slow_down/ready/expired/denied）、`next_poll_at` 节流、归属校验（A 卖家动 B 卖家的 upstream 必须 404）、session_id 不出现在响应体、凭证字段完整性。
- 前端单测：设备码面板的倒计时/停止轮询/过期提示，接入方式按平台显示。
- 手工验收：真实 Kimi 会员走一次设备码；智谱 BigModel 与 Z.ai 各走一次。

## 四、风险

1. **Kimi 无稳定身份标识**：无法做官方账号去重，同一订阅可被重复接入。需产品确认是否接受（可在后续用 `device_id` 作为弱标识）。
2. **会话 TTL 短**：Kimi 15 分钟、智谱 5 分钟。必须按引擎返回值设置，前端要显示倒计时。
3. **凭证轮换归引擎**：市场只交付一次完整凭证快照，不自己刷新，避免双写。
4. **引擎加固可延后**：`account_mode` 由市场侧自行补齐时可不动引擎，但建议同批修掉。
