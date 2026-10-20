# 测试交付说明（2026-10-19）

宿主：`../idlicat`。站点 `http://127.0.0.1:8080`，引擎 `http://127.0.0.1:18080`。

自动化：`idlicat/market/scripts/flow-e2e.sh`（健康、登录桥、管理 API、货架 SPA、买卖身份对齐）。

## 账号

| 角色 | 邮箱 | 密码 |
|---|---|---|
| 管理员 | admin@dev.local | admin-password-1 |
| 卖家 | seller@dev.local | seller-password-1 |
| 买家 | buyer@dev.local | buyer-password-1 |

市场登录 `POST /api/auth/login` 签发 `tm_token`。卖家/买家响应应带 `engine_user_id` 与 `engine_token_ready=true`。管理端用同一邮箱走引擎登录。

## 建议测的路径

1. **C 端原站** `/`、`/login`、`/buyer/*`、`/seller/*`：市场首页标题含「闲猫」。
2. **宿主货架** `/shop`、`/shop/listings/:id`、`/shop/center`（Key、余额购买套餐、钱包、商家、账单、挂单、上游 OAuth 发起）。未登录买卖中心应提示去 `/login`。
3. **管理端** `/admin/dashboard`。标志和登录页是闲猫；侧栏「闲猫」按前台、供给、资金、质检、运营分组，不要再看成一长串平铺。深色模式为纯黑底。
4. **身份** `/admin/market/identities`：三账号已关联；卖家/买家影子为 `buyer` 且指向同一引擎用户。
5. **资金**：充值/退款/提现列表，以及确认、打款、审核，经 `/api/v1/admin/market/*` 打到引擎，账本仍是市场人民币。见 [billing-ledger-boundary.md](billing-ledger-boundary.md)。不要把市场人民币余额和引擎 `users.balance` 当成同一数字。站内升级 `system/*` 仍在 market-api。

## 不要当成缺陷

以下未作为本轮交付完成项，测到缺失请记「未交付」而不是回归：

- 充值/退款/提现/活动包 integration 全绿（写接口已在引擎，专项测试未重跑）
- Edge 默认池与最多 5 跳在真实引擎上的故障转移复验
- 质检 worker 探测与试用/正式开关对照迁前行为
- 品牌墙全视觉、Coding Plan 导入整页仍在 `/` 的 `market/web`。宿主买卖中心已有余额购买套餐和「发起 OAuth」（厂商未配置时接口会失败，属环境）
- 过渡仓退役、拆掉影子用户胶水

当前库内可能没有在售挂单。`/shop` 空列表、`/api/market/listings/1` 返回 404 是数据状态，不是页面挂载失败。页面本身应返回 200 HTML。
