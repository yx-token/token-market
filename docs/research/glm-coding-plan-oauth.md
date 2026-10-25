# GLM Coding Plan OAuth 上号与推理凭证逆向调研

- 调研日期：2026-10-25
- 目标：智谱 GLM Coding Plan（BigModel 国内线，兼顾 Z.ai 国际线）在 token-market 服务端上号
- 样本：本机 `/Applications/ZCode.app`，桌面端 3.11.2，bundle id `dev.zcode.app`
- 仓库分支：`feat/cn-coding-plan-oauth`
- 方法：只读静态逆向 + 少量无凭证公开端点探测；未运行 ZCode 或其任何二进制，未读取 `~/.zcode`、`~/.zcode-keysmith`，未登录真实账号
- 边界：本报告不验证“官方是否还允许发 API Key”这一商业/产品政策，只验证客户端当前技术链路

## 0. 结论先行

**选择 C：以标准 GLM Coding Plan 为口径，正式推理当前绕不开客户端签名与 PoW；但整条服务端方案仍然可实现，且不需要额外 app secret。**

更具体地说：

1. **OAuth 登录、code 换 token、Z.ai 业务 token 代换、派生 `id.secret` API Key，都不需要客户端签名、PoW 或 app secret。**置信度：高。
2. **标准 Coding Plan 的正式推理路径当前开启 `codingPlanSignature.enable=true`，官方客户端会执行签名与 PoW。**2026-10-25 的公开配置探测也返回 `enable: true`。要兼容官方客户端，服务端必须实现这套算法；不能把现有“拿到 `id.secret` 就直接转发”当作完整方案。置信度：高（客户端行为），中（服务端是否所有部署都强制拒绝无签名请求，未用真实账号做对照）。
3. **签名/PoW 不是黑盒阻断点。**算法、密钥派生、握手、Ed25519 私钥解密和 PoW 拼接均已从客户端还原，可用 Go 标准密码学能力及项目已有的 `golang.org/x/crypto` 实现，无需运行 ZCode、无需内置隐藏 app secret。置信度：高。
4. **不能选 A，也暂不选 B。**`zcode.z.ai/api/v1/zcode-plan/*` 确实有免签名路径，且官方 StartPlan 使用 zcode JWT 调这些路径；但 StartPlan 与标准 Coding Plan 是不同 provider 和权益路径。没有真实账号证据证明标准 Coding Plan 可直接用 JWT 走该路径，因此不能把它当成已验证的 B 类替代方案。若后续真实账号验证通过，可重新评估为 B。
5. **登录首选 `/oauth/cli/init + /oauth/cli/poll/{flow_id}`，手工粘贴回调作为备选。**两条路均已定位到客户端实现；CLI 初始化与 pending 轮询也做了无凭证实测。管理员只需在浏览器登录，服务端即可轮询取得 token，无需复制 code。置信度：高。

推荐实现不是“寻找隐藏 secret”，而是：

> CLI OAuth 轮询上号 → 保存 OAuth/JWT/业务 token → 用业务 token 派生并保存 `id.secret` → 按官方算法为 Coding Plan 推理请求签名并计算 PoW → 用既有额度接口监控；JWT/业务凭证失效时重新走 OAuth。

### 关键限制

- 未使用真实账号，因此没有验证成功 token 响应、真实 JWT 的全部 claims、refresh rotation、真实推理和真实额度响应。
- 客户端“会签名”与服务器“所有情况都必然拒绝无签名请求”不是同一命题。本报告按官方客户端兼容口径要求实现签名，不把未验证的免签名服务端行为当事实。
- `billing/current` 只找到 URL 构造，未找到实际消费方或响应结构；未授权探测为 HTTP 401 空响应。
- `/api/v2/releases/latest` 两次无凭证请求均为 HTTP 204，未能进一步取得 `client/configs` 的当前版本快照；静态默认端点与 `/agent/configs` 的实时结果仍可复核。

## 1. 样本与证据约定

### 1.1 样本身份

| 制品 | 版本/大小 | SHA-256 |
|---|---:|---|
| `Contents/Resources/app.asar` | 307,138,363 bytes | `0317216fc08f374162e5cc1b779bae5f94ef70c5eaa585cb71b8ffedc537b668` |
| `Contents/Resources/glm/zcode.cjs` | 12,615,227 bytes | `e9f1868c0fdb863537ed910ee3828b9be96b8c2fd805473f63b439e1113266b8` |
| asar 内 `package.json` | ZCode Desktop 3.11.2 | `fb587db4e6de0a4e9836d8edb293ffa42d746fd19eaf3fb07d3971b0b45e3657` |

asar 内构建信息还包含：`version=3.11.2`、build commit `89817f5b`、build time `2026-09-04T08:04:18.527Z`（`app.asar!out/host/chunk-RWMCBKS2.js@2` 附近）。

`Contents/Resources/config/default.json` 只有反馈和社区 URL，没有 OAuth secret 或推理配置。

### 1.2 偏移读法

下文使用零起始字节偏移：

- `app.asar!out/host/index.js@N`：asar 内该文件的 offset N；该文件在 asar 中的起始绝对偏移为 252,431,064。
- `app.asar!out/host/chunk-RWMCBKS2.js@N`：asar 内该文件的 offset N；其 asar 起始绝对偏移为 251,842,349。
- `glm/zcode.cjs@N`：独立文件 `Contents/Resources/glm/zcode.cjs` 的 offset N。

asar 数据区起点为 7,146,192；上面列出的两个“起始绝对偏移”已经包含它。需要 asar 绝对偏移时，直接计算：

`文件在 asar 中的起始绝对偏移 + 文件内偏移`。

格式化的临时副本仅用于阅读；所有报告的偏移均回到原始 minified 文件计算。

### 1.3 证据类型和置信度

- **【代码】**：ZCode 3.11.2 的一手静态代码事实，可定位到文件与偏移；一般为高置信度。
- **【探测】**：2026-10-25 对公开端点的无凭证实测；只证明当次响应，不外推为永久协议。
- **【推断】**：根据代码调用关系作出的工程判断，会明确标注。
- **【建议】**：交给 token-market 后续工人的实现方案，不是 ZCode 已有行为。

未使用搜索摘要或二手文章支撑关键结论。

## 2. 问题 1：授权 URL 的完整构造

### 2.1 共同的桌面中间页

两条线实际使用的 `redirectUri` 都不是简单的 `zcode://oauth/callback`，而是：

```text
https://zcode.z.ai/app/oauth/login?redirect=zcode%3A%2F%2Foauth%2Fcallback&app_version=3.11.2
```

代码原意（URL 解码后）：

```js
t = new URL("/app/oauth/login", zcodeOrigin)
t.searchParams.set("redirect", "zcode://oauth/callback")
t.searchParams.set("app_version", "3.11.2")
```

证据：`app.asar!out/host/index.js@1534003`（`buildDesktopOAuthRedirectUriFromEnv`）；常量 `zcode://oauth/callback` 在 `@1533729` 附近。

### 2.2 BigModel 国内线

**【代码，高置信度】**

```text
https://bigmodel.cn/login?redirect=<中间页 URL>&appId=zcode&state=<32 字节随机数的 64 位小写 hex>
```

查询参数只有：

| 参数 | 值 |
|---|---|
| `redirect` | 上述 `https://zcode.z.ai/app/oauth/login?...` 中间页 |
| `appId` | `zcode` |
| `state` | 客户端生成的 32 字节随机数 hex；常规手动流程本地保存并校验 |

没有 `client_id`、`redirect_uri`、`response_type`、`scope`、`code_challenge`、`code_challenge_method`，因此这条自定义 BigModel 流程不是 PKCE 流程。

原文：

```js
new URLSearchParams({
  redirect: t.redirectUri,
  appId: this.config.appId,
  state: t.state
})
```

证据：

- URL 构造：`app.asar!out/host/index.js@1518547`
- 默认 `authorizeUrl/tokenUrl/appId`：`app.asar!out/host/index.js@1534310` 附近
- 运行时配置 `createBigModelProviderRuntimeConfig`：`app.asar!out/host/index.js@1534497`

`BIGMODEL_OAUTH_APP_SECRET` 虽然被读进 `appSecret` 配置字段（`@1534831` 附近），但桌面 `BigModelProviderAdapter` 的授权和 token 请求均未使用该字段。

### 2.3 Z.ai 国际线

**【代码，高置信度】**

```text
https://chat.z.ai/api/oauth/authorize?redirect_uri=<中间页 URL>&response_type=code&client_id=client_P8X5CMWmlaRO9gyO-KSqtg&state=<state>
```

| 参数 | 值 |
|---|---|
| `redirect_uri` | 同一桌面中间页 |
| `response_type` | `code` |
| `client_id` | `client_P8X5CMWmlaRO9gyO-KSqtg` |
| `state` | 同上 |

没有 `scope` 或 PKCE 参数。

证据：URL 构造 `app.asar!out/host/index.js@1525293`；默认配置 `@1534977` 附近；`createZaiProviderRuntimeConfig` 在 `@1535259`。

**【探测】**该授权 URL 未登录时返回 HTTP 307，`Location: /auth?...`，并保留 `client_id/redirect_uri/state/response_type`。这与静态构造一致。

### 2.4 不要混用旧 CLI 的 BigModel 直连流程

`glm/zcode.cjs` 另有旧 `createBigmodelOAuthClient`：

```js
POST https://bigmodel.cn/api/auth/tokenByAuthCode
{ appId: "zcode", appSecret: r, authCode: i.code }
```

该路径会明确抛出 `BigModel OAuth appSecret is required.`。证据：`glm/zcode.cjs@7156471`（错误与请求体）、`@7157325`（endpoint）、`@7157515`（函数命名）。

**结论：后续实现应选择桌面 host 的 ZCode 后端代换或 CLI 轮询，不要选择这条需要 appSecret 的旧直连路径。**

## 3. 问题 2：回调参数、中间页行为与手工粘贴

### 3.1 回调参数

**【代码，高置信度】**

桌面接受完整 `zcode://oauth/callback?...` URL，并从 query 读取：

- BigModel：优先 `authCode`，退回 `code`；必须有 `state`。
- Z.ai：优先 `code`，退回 `authCode`；必须有 `state`。
- 可选归因参数：`channel_id`、`utm_source`、`utm_campaign`。

证据：

- BigModel `parseCallbackParams`：`app.asar!out/host/index.js@1518217`。
- Z.ai `parseCallbackParams`：`app.asar!out/host/index.js@1524963`。
- state 生成与 5 分钟 pending TTL：`app.asar!out/host/index.js@1535788,1547951`；state 校验和回调处理：`@1548503`。

手动流程中 state 必须和本地 pending session 相同；成功后消费 session，不能把它当可重放 code。

### 3.2 中间页做了什么

公开中间页脚本 `https://zcode.z.ai/_next/static/chunks/app/app/oauth/login/page-1098321b4da73487.js` 表明：

1. 校验目标必须是 `zcode://oauth/callback`。
2. 把 `attributionParams + passthroughParams` 写回 deep link query。
3. `window.location.assign(e)` 自动尝试打开 ZCode，也把同一地址放到“打开 ZCode”按钮的 `href`。
4. 若参数含 `authCode + state`，同时请求 `/api/v1/oauth/cli/callback/bigmodel`；若含 `code + state`，请求 `/api/v1/oauth/cli/callback/zai`，用于 CLI 轮询桥接。
5. 版本 `>=3.9.1` 时启用上述轮询桥接 UI。

偏移（该公开 JS 文件内）：deep link 合成 `@2726`；BigModel CLI callback `@3481`；Z.ai CLI callback `@3663`；自动打开 `@4563`。

**【探测】**直接 GET 中间页返回 HTTP 200 和“登录已完成/打开 ZCode”页面；HTML 的 props 中包含 `redirect`、`appVersion` 和 `passthroughParams`。脚本静态分析验证了上述行为，未执行它。

### 3.3 手工粘贴是否可行

**可行，置信度高。**管理员可以把浏览器最终跳转到 `zcode://oauth/callback` 的完整地址（含 query）复制出来；服务端解析 `code/authCode + state` 后，得到的参数与桌面 deep link 处理完全相同。中间页按钮本身也保留该地址，便于复制链接。

注意事项：

- 不能只粘贴中间页 URL；需要的是带 `code/authCode` 和 `state` 的最终 deep link，或至少粘贴这两个字段。
- token 交换所用的 `redirect_uri` 仍应是最初的中间页 URL，不是 `zcode://oauth/callback`。
- code 可能单次使用且很快过期；若轮询已经消费，重复手工交换应视为重复完成，而不是再次换 token。

### 3.4 更适合服务端的 CLI 轮询回调

**【代码】**桌面还支持：

```text
POST https://zcode.z.ai/api/v1/oauth/cli/init
Authorization: Bearer <本地 32 字节随机 poll token>
Content-Type: application/json

{"provider":"bigmodel"}  // 或 "zai"
```

随后：

```text
GET https://zcode.z.ai/api/v1/oauth/cli/poll/{flow_id}
Authorization: Bearer <同一个本地 poll token>
```

证据：`app.asar!out/host/index.js@1543916`（init）、`@1545918`（poll 与 ready 响应解析）。

初始化响应字段：`flow_id`、`authorize_url`、`poll_token`、`expires_at`、`poll_interval_sec`。桌面使用自己生成的 poll token，而不是依赖响应中的 `poll_token` 字段；这与实测一致。

**【探测】**分别用随机、无账号的 poll token 初始化两条线：

- 均返回 HTTP 200、`code:0`、`poll_interval_sec:2`，`expires_at` 比初始化时间约晚 300 秒。
- BigModel 返回 `bigmodel.cn/login?...redirect=https://zcode.z.ai/api/v1/oauth/cli/callback/bigmodel&state=...`。
- Z.ai 返回 `chat.z.ai/api/oauth/authorize?...redirect_uri=https://zcode.z.ai/api/v1/oauth/cli/callback/zai&response_type=code`。
- 各轮询一次均返回 HTTP 200、`code:0`、`data.status:"pending"`；未继续登录，也未高频轮询。

因此服务端可以完全不让管理员粘贴 code；桌面代码中“把 authorize_url 的 redirect 改回中间页”只是兼容 deep link 的附加行为，不是纯 CLI 轮询的必要条件。

## 4. 问题 3：code 换 token

### 4.1 共同的 ZCode token 代换端点

两条桌面线都调用：

```text
POST https://zcode.z.ai/api/v1/oauth/token
Content-Type: application/json
```

无 `X-Client-*` 签名头、无 PoW、无 PKCE verifier、无 app secret。

| 线 | JSON body | 代码读取的响应字段 |
|---|---|---|
| BigModel | `{provider:"bigmodel", code, redirect_uri:<中间页>, state}` | 必需 `data.token`、`data.bigmodel.access_token`（兼容 camelCase 和顶层 access token）；可选 `data.bigmodel.refresh_token/refreshToken` |
| Z.ai | `{provider:"zai", code, redirect_uri:<中间页>, state}` | 必需 `data.token`、`data.zai.access_token`；可选 `data.user`（缺失时回退 userinfo）、`data.expires_in` |

证据：

- BigModel 请求与解析：`app.asar!out/host/index.js@1518757`
- Z.ai 请求与解析：`app.asar!out/host/index.js@1525293`

**【探测】**两条线各发送一次明显无效 code，均为 HTTP 500、`{"code":2007,"msg":"http error"}`。这只能验证错误封装和端点可达，不能证明成功响应或长期错误码稳定性。

### 4.2 Z.ai 还要做一次业务 token 代换

Z.ai 并不直接把 chat OAuth access token 用于业务 API。客户端继续调用：

```text
POST https://api.z.ai/api/auth/z/login
Content-Type: application/json

{"token":"<data.zai.access_token>"}
```

返回 `data.access_token`（兼容 `accessToken`）和可选 `data.expires_in`。随后把这个**业务 access token** 存为账号的 `accessToken`，用于派生 key 和后续业务接口。缓存提前 300 秒视为过期。

证据：`app.asar!out/host/index.js@1521753`（`exchangeBusinessToken`）；Z.ai token 主流程 `@1525293` 同一片段。

BigModel 无此额外代换，直接使用 `data.bigmodel.access_token`。

### 4.3 userinfo

- BigModel：`GET https://bigmodel.cn/api/biz/customer/getCustomerInfo`，`Authorization: <accessToken>`，**不添加 Bearer**；响应 `data.customerNumber/customerName/nickName/avatar`。证据：`app.asar!out/host/index.js@1518757` 后半段。
- Z.ai：优先使用 token 响应的 `data.user`；回退 `GET https://chat.z.ai/api/oauth/userinfo`，`Authorization: Bearer <业务 accessToken>`。证据：同上的 Z.ai `fetchUserInfo` 片段。

### 4.4 zcode JWT 能确认的结构

`data.token` 在代码中作为 zcode JWT 保存。客户端只对第二段做 base64url JSON 解码，并读取数值 `exp`；到期判断预留 30 秒。证据：`app.asar!out/host/chunk-RWMCBKS2.js@517487`（`resolveJwtExpiration`）。

**未确认项：**没有真实 JWT，不能给出 header、`sub/iss/aud` 等其他 claims，也不能把 `expires_in` 当成 JWT 的实际 TTL；只能确认客户端依赖 payload 的 `exp`。

## 5. 问题 4：最终推理凭证、派生 Key、base URL 与请求头

### 5.1 三类凭证的职责

| 凭证 | 用途 | 是否直接作为标准 Coding Plan 推理 key |
|---|---|---|
| OAuth/业务 access token | 调 `getCustomerInfo` 和项目 API Key 管理接口，派生 key | 否，官方 Coding Plan 不直接用它推理 |
| `data.token`（zcode JWT） | ZCode 会话、StartPlan 推理、`zcode-plan/billing/*`、off-peak 外层鉴权 | 标准 Coding Plan 未找到这种官方用法，不能替代下述派生 key |
| 派生 `apiKey.secretKey`（`id.secret`） | 标准 BigModel/Z.ai Coding Plan 推理及传统额度接口；也是客户端签名密钥来源 | **是** |

“corrupt OAuth session 后清理 derived provider keys”对应的是：JWT/会话失效时，客户端同时清理上述派生 key，而不是说 OAuth access token 本身就是推理 key。证据：清理逻辑 `app.asar!out/host/index.js@1531697` 附近；provider 预设派生逻辑见下。

### 5.2 派生 `id.secret` 的完整接口序列

BigModel host 为 `https://bigmodel.cn`；Z.ai host 为 `https://api.z.ai`。

1. `GET {host}/api/biz/customer/getCustomerInfo`
2. 从 `data.organizations[]` 选择机构/项目：优先名称含“默认机构”“默认项目”，否则第一项；排除 `projectType == "2"` 的团队项目。
3. `GET {host}/api/biz/v1/organization/{organizationId}/projects/{projectId}/api_keys`
4. 在返回数组中找 `name == "zcode-api-key"`；没有则 `POST` 同一路径，body `{"name":"zcode-api-key"}`。
5. 读取结果中的 `apiKey`，再调用 `GET {api_keys}/copy/{apiKey}`，读取 `secretKey`。
6. 最终 credential 为 **`${apiKey}.${secretKey}`**。

请求鉴权：

- BigModel：上述业务请求均为 `Authorization: <bigmodel access token>`，不加 Bearer。
- Z.ai：上述业务请求均为 `Authorization: Bearer <上一步业务 access token>`。
- 另有 `Content-Type: application/json`；没有客户端签名、PoW 或 app secret。

证据：完整实现 `app.asar!out/host/index.js@1626109`；组织/项目选择 `@1570170`；Z.ai `requireSecretKey:true` 在同一 `resolveBizApiKey` 片段。`glm/zcode.cjs@7159291` 还有一份独立实现，交叉一致。

**实现建议：两条线都要求完整 `id.secret`。**BigModel 老代码在缺 secret 时可退回裸 apiKey，但这种值无法完成 V4 客户端签名，不应作为服务端可用完成态。

### 5.3 标准 Coding Plan 推理端点与鉴权头

| 协议 | BigModel | Z.ai | 鉴权 |
|---|---|---|---|
| OpenAI Chat Completions | `https://open.bigmodel.cn/api/coding/paas/v4/chat/completions` | `https://api.z.ai/api/coding/paas/v4/chat/completions` | `Authorization: Bearer <id.secret>` |
| Anthropic Messages | `https://open.bigmodel.cn/api/anthropic/v1/messages` | `https://api.z.ai/api/anthropic/v1/messages` | `Authorization: Bearer <id.secret>`、`x-api-key: <id.secret>`、`anthropic-version: 2023-06-01` |

证据：

- BigModel Anthropic 默认 base：`app.asar!out/host/index.js@1563944`；构造 `/v1/messages`：`@1568509`。
- Z.ai Anthropic 默认 base：`app.asar!out/host/index.js@1564337`；构造 `/v1/messages`：`@1568692`。
- 模型目录中旧 Coding Plan OpenAI base：`glm/zcode.cjs@6157292`（BigModel）与 `@4997902`（Z.ai）。
- Anthropic SDK 同时补 `Authorization: Bearer` 和 `x-api-key`：`glm/zcode.cjs@4482126` 的 `withAnthropicAuthorizationHeader` 加上 `@4179432` 的 SDK header 构造。

ZCode 运行期还会发送以下来源头：

```text
User-Agent: ZCode/3.11.2
X-ZCode-App-Version: 3.11.2
X-ZCode-Agent: glm
X-Title: Z Code@cli            // 桌面为 Z Code@electron
X-Release-Channel: production
X-Client-Language: <locale>
X-Client-Timezone: <timezone>
X-Platform: <platform-arch>
X-Os-Category: linux|macos|windows
X-Os-Version: <os version>
HTTP-Referer: https://zcode.z.ai
X-Device-Mid: <如已有>
```

证据：`glm/zcode.cjs@11775288`（运行时 source headers）、`app.asar!out/host/chunk-RWMCBKS2.js@540076`（完整 header builder）。

**必需性边界：**`X-Session-Id` 是签名算法必需输入；`X-App-Id/X-Client-*` 是签名请求头。上述 ZCode 来源头是客户端实际行为，但静态代码不能证明每一项都被服务端强制，实施时先完整模仿，避免自行删减。

### 5.4 `zcode-plan` 免签名路径的准确边界

以下三个 path 被 `isUnsignedModelRequestPath` 明确豁免：

```text
/api/v1/zcode-plan/anthropic/v1/messages
/api/v1/zcode-plan/chat/completions
/api/v1/off-peak/anthropic/v1/messages
```

证据：`app.asar!out/host/chunk-RWMCBKS2.js@510396`；独立副本 `glm/zcode.cjs@504847`。

官方 **StartPlan** provider 使用：

- `https://zcode.z.ai/api/v1/zcode-plan/anthropic` + `/v1/messages`（如配置 path 为空则由运行时拼路径）；
- `Authorization: Bearer <zcode JWT>`；
- `apiKey` 字段也是 zcode JWT，而不是派生 `id.secret`。

证据：`app.asar!out/host/index.js@1631845`（`Uae=zcodePlanAnthropicBaseUrl`）；Z.ai StartPlan 构造 `@1634913,1635116`，BigModel StartPlan 构造 `@1635396,1635607`（其中 `apiKey:p` 来自 zcode JWT）。

**不要把 StartPlan 当成标准 Coding Plan 的替代证据。**off-peak 还需要 JWT + `X-Coding-Plan-Api-Key` + ticket，亦不是通用替代。

### 5.5 当前代理映射

**【探测】**`GET https://zcode.z.ai/api/v1/agent/configs?app_version=3.11.2&platform=darwin-arm64`（带明显无效的假 `x-api-key`，无任何真实凭证）返回 HTTP 200、`code:0`，并含：

```json
{
  "codingPlanSignature": {"enable": true},
  "proxyEndpoint": {
    "mapping": [
      {"from": "https://open.bigmodel.cn/api/anthropic/v1/messages", "to": "https://zcode.z.ai/api/v1/ultra/anthropic/v1/messages"},
      {"from": "https://api.z.ai/api/anthropic/v1/messages", "to": "https://zcode.z.ai/api/v1/ultra-zai/anthropic/v1/messages"}
    ]
  }
}
```

静态代码会先按原 base URL 做签名，再由 transport 做 endpoint routing；因此实现代理映射时，不能误以为改成 zcode host 就可以丢掉原签名上下文。

## 6. 问题 5：刷新、过期与轮换

### 6.1 当前客户端没有可用的通用 refresh-token grant

桌面 `OAuthService.refreshToken` 会调用 adapter 的 `refreshToken` 方法；但本次样本中的 BigModel/Z.ai adapter 均未实现该方法，于是报“暂未提供 refresh token 交换接口，请重新登录”。证据：`app.asar!out/host/index.js@1549978`。

具体差异：

- BigModel 会返回并保存 `refresh_token`（如有），但当前 adapter 没有消费它。**无法确认刷新 URL、请求字段、TTL 或是否轮换。**
- Z.ai 主 token 流程未读取或保存 refresh token；`data.expires_in` 只用于计算过期时间。
- Z.ai 业务 token 可用原 chat OAuth access token 再调 `/api/auth/z/login` 重新代换；客户端在调用时提前 300 秒判定缓存过期并重新代换。原 chat token 失效后，此接口不能生成新的 zcode JWT。
- zcode JWT 到期时，客户端清除 active session 和派生 provider keys，而不是自动 refresh。证据：`app.asar!out/host/index.js@1538240`（`cached session invalidated because zcode JWT expired` 及其清理段）；JWT `exp` 检查见 `app.asar!out/host/chunk-RWMCBKS2.js@517487`。

**因此不能照搬 Grok/OpenAI 的 `grant_type=refresh_token`，也不能声称 refresh 会轮换。**

### 6.2 推荐的续期语义【建议】

- 保存 JWT 的 `exp`，至少提前 30 秒标记 `reauthentication_required`；若账户只有 OAuth/JWT 而没有有效派生 key，直接阻止调度。
- Z.ai 额外保存原始 chat OAuth access token；业务 token 到期前 5 分钟用 `/api/auth/z/login` 再代换，再重新派生/复制 key。
- 派生 key 失效或 `VERIFY_APIKEY_EXPIRED` 时，先用仍有效的业务 access token 重跑 `getCustomerInfo → api_keys → copy`，不要重复创建同名 key。
- BigModel `refresh_token` 只作为未知能力保留，不写未证实的刷新请求；OAuth/JWT 失效后重新走 CLI 登录。
- 刷新时不以空响应覆盖已有 refresh token 或派生 key，沿用项目 `MergeCredentials` 的保守合并方式（`backend/internal/service/oauth_refresh_api.go:438`）。

## 7. 问题 6：额度与用量查询

### 7.1 `zcode-plan/billing/balance`

请求：

```text
GET https://zcode.z.ai/api/v1/zcode-plan/billing/balance?app_version=3.11.2
Authorization: Bearer <zcode JWT>
```

证据：`app.asar!out/host/index.js@1631376`（`fetchZaiStartPlanBalanceEnvelope`），URL 还由 `buildZCodeEndpointUrls` 在 `app.asar!out/host/chunk-RWMCBKS2.js@386589` 构造。

代码可确认的响应结构：

```text
{
  code: 0,
  msg: "...",
  data: {
    server_time: <秒或客户端可解析的时间>,
    plans: [{
      plan_id, name, status, starts_at, ends_at,
      entitlements: [{ entitlement_id, show_name, effective_at, period }]
    }],
    balances: [{
      entitlement_id, show_name, plan_id, meter,
      capabilities: ["model:<model-id>", ...],
      total_units, used_units, remaining_units,
      available_units, reserved_units, expires_at
    }]
  }
}
```

解析证据：`app.asar!out/host/index.js@1747402`（balances 归一化），同文件 `buildZaiStartPlanSubscription` 段（格式化副本行 79905-79934）用于 plans 字段。

无凭证探测为 HTTP 401 空响应，符合必须带 JWT 的代码路径；没有真实响应样本，所以字段是否可空、数值单位仍需真实账号验证。

### 7.2 `zcode-plan/billing/current`

只找到 URL 构造：`app.asar!out/host/chunk-RWMCBKS2.js@386521`。在当前检索范围内未找到消费方，无法从代码给出返回结构。无凭证探测同样为 HTTP 401 空响应。

**结论：不能据此实现 current 解析；优先使用下述已有传统额度接口或 balance。**

### 7.3 与 token-market 现有 zhipu 接入最匹配的传统额度接口

```text
GET https://open.bigmodel.cn/api/monitor/usage/quota/limit
Authorization: <id.secret>       // 不加 Bearer
```

Z.ai 则使用 `https://api.z.ai/api/monitor/usage/quota/limit`。团队场景可加 `?type=2` 和 `bigmodel-organization/bigmodel-project` 头。

ZCode 侧对应路径与 `data.level/data.limits` 结构见 `app.asar!out/host/index.js` 的 usage 代码（格式化副本行 79185-79232、79834-79842）。token-market 已有完全相同的接口与解析：`backend/internal/service/cn_provider_quota_service.go:138-159,186-197,321-355,500-612`。

可复用字段：`data.level`、`limits[].type`、`percentage`、`nextResetTime`、`unit`；项目已有 5h/weekly 分类和 `TOKENS_LIMIT/CREDIT_LIMIT` 处理，无需改为不透明的 `billing/current`。

## 8. 问题 7：签名/PoW 的路径、算法与阻断点

### 8.1 哪些请求会签名

模型工厂会为 BigModel/Z.ai 域名创建 `ClientRequestSigningV4` transport。规则：

1. 请求 origin 必须与签名器由 base URL 得出的 business origin 相同；否则直接不签。
2. 三个 `isUnsignedModelRequestPath` path 直接不签。
3. `codingPlanSignature.enable` 为 true 时，其余请求进入握手和签名。
4. feature gate 取不到或失败时客户端 fail-open 为不签；两次特定签名错误后也会降级不签。此容错不能当成服务端永远接受无签名的依据。

证据：

- 模型工厂接入签名：`glm/zcode.cjs@4492921`
- origin 判定：`app.asar!out/host/chunk-RWMCBKS2.js@521764`；精确 unsigned 集合 `@510396`
- feature gate：`app.asar!out/host/chunk-RWMCBKS2.js@512856`
- 实时 `enable:true`：见 §5.5 的公开探测

**标准 Coding Plan 的 `/api/anthropic/v1/messages` 与 `/api/coding/paas/v4/*` 不在免签名集合内，因此在当前 gate 下官方客户端会签名。**

OAuth、token 代换和派生 key 的代码并未经过这个模型签名 transport，所以这些步骤不需要 V4 签名/PoW。签名握手本身使用 HMAC，但不计算业务 PoW。

### 8.2 credential 拆分

`parseClientSigningCredential` 只在字符串含**恰好一个点号**且两侧非空时接受：

```text
credential = apiKeyId + "." + apiKeySecret
```

证据：`app.asar!out/host/chunk-RWMCBKS2.js@510530`。

### 8.3 共用 HKDF 参数

所有派生均为 HKDF-SHA256、32 字节输出：

```text
salt = "WD_CLIENT_SIGN_KDF_SALT"
ikm  = apiKeySecret（点号后的部分）
```

info 分别为 `getSignKey_hmac` 和 `ed25519_priv`。证据：`app.asar!out/host/chunk-RWMCBKS2.js@514327`、`@516297`（`deriveBytes`）。

### 8.4 握手：取得 Ed25519 私钥

握手地址：

```text
POST {businessOrigin}/api/paas/c1f3a7e2/v2/client
Authorization: <完整 id.secret>
Content-Type: application/json
```

生成毫秒时间戳 `ts` 和 16 字节随机数的小写 hex `nonce`（32 字符），然后：

```text
handshakeKey = HKDF-SHA256(apiKeySecret, salt, info="getSignKey_hmac", L=32)
message      = "get_sign_key\n" + apiKeyId + "\n" + ts + "\n" + nonce
sig          = Base64(HMAC-SHA256(handshakeKey, message))
```

body：

```json
{"apiKey":"<完整 id.secret>","nonce":"<nonce>","sig":"<sig>","ts":"<毫秒时间戳>"}
```

成功要求 HTTP 200 且业务 `code == 200`，读取 `data.privateCipher`。证据：`app.asar!out/host/chunk-RWMCBKS2.js@519190`（路径和常量）、`@523930`（请求）、`@514397`（HMAC）。

解密：

```text
privateKeyBytes = Base64Decode(privateCipher)
iv              = privateKeyBytes[0:12]
ciphertextTag   = privateKeyBytes[12:]
aesKey          = HKDF-SHA256(apiKeySecret, salt, info="ed25519_priv", L=32)
plaintext       = AES-GCM-Decrypt(aesKey, iv, ciphertextTag, AAD=apiKeyId, tagLength=128)
pkcs8           = Base64Decode(UTF8(plaintext))
privateKey      = Ed25519 PKCS8 private key
```

证据：`app.asar!out/host/chunk-RWMCBKS2.js@515164`。

### 8.5 业务签名头

每次签名请求需要非空 `X-Session-Id`，另生成新的毫秒 `ts` 和 16 字节随机 hex `nonce`：

```text
message = apiKeyId + "\n" + ts + "\n" + clientVersion + "\n" + sessionId + "\n" + nonce
X-Client-Sig = Base64(Ed25519.Sign(privateKey, message))
```

发送：

```text
X-Client-Ts: <ts>
X-Client-Version: 3.11.2       // 或服务端声明的兼容版本
X-Client-Sig: <上述签名>
X-Session-Id: <会话 ID>
X-Client-Nonce: <nonce>
X-App-Id: zcode
X-Client-Pow: <下一节结果>
```

证据：`app.asar!out/host/chunk-RWMCBKS2.js@522592`。

**path、HTTP method、query 和 body 均不进入这段签名文本。**因此接入现有转发器时不需要重写字段，但必须保证五个输入与 headers 完全一致。

### 8.6 PoW 算法

常量：`appId="zcode"`、`powBits=8`；业务签名 nonce 为 16 字节随机数 hex，PoW 随机前缀则为 12 字节随机数 hex。证据：`app.asar!out/host/chunk-RWMCBKS2.js@519238-519262` 与 `@515932`。

计算：

```text
digest  = SHA256(apiKeyId + "\n" + "zcode" + "\n" + sessionId + "\n" + ts)
seedHex = lowercaseHex(digest)[0:32]
prefix  = 12 个随机字节的小写 hex（24 字符）

for counter = 0 .. 2^32-1:
    candidate = prefix + lowercaseHex(counter).padStart(8, "0")
    if SHA256(seedHex + "\n" + candidate) 的前 8 个 bit 全为 0:
        return candidate   // 共 32 个 hex 字符
```

因为难度只有 8 bit，期望约 256 次 SHA-256；不是高成本 PoW。证据：`app.asar!out/host/chunk-RWMCBKS2.js@515932`。

### 8.7 失败与缓存

- `VERIFY_SIGNATURE_INVALID`、`VERIFY_APIKEY_EXPIRED` 会使客户端废弃当前私钥、重新握手并重试。
- 私钥在内存按 credential/origin 缓存；未发现可靠的过期字段。
- 建议【建议】进程内缓存即可，重启后重新握手，不持久化 Ed25519 私钥；失败时先作废并重新握手一次，仍失败则按账号认证错误处理，不把无签名降级当默认生产策略。

### 8.8 app secret 是否是绕不开的前置条件

**不是。**只有 §2.4 的旧 BigModel CLI `tokenByAuthCode` 需要 `appSecret`。本报告推荐的桌面/ZCode token 代换与 CLI 轮询均不需要它；派生 key 的 HMAC 密钥来自 `apiKeySecret`，不是 `BIGMODEL_OAUTH_APP_SECRET`。

## 9. 问题 8：可行性结论与推荐的服务端实现草图【建议】

### 9.1 登录：优先 CLI 轮询，保留手工粘贴

建议管理 API 抽象为：

1. `POST /admin/zhipu/oauth/init`
   - 输入：`provider=bigmodel|zai`、`proxy_id`。
   - 生成 32 字节随机 poll token，调用 `/api/v1/oauth/cli/init`。
   - 返回：内部 `session_id`、`authorize_url`、`state`、`expires_at`、`poll_interval_sec`。
2. `POST /admin/zhipu/oauth/poll`
   - 输入：内部 `session_id`。
   - 按服务端节奏调 `/oauth/cli/poll/{flow_id}`，返回 `pending|ready|failed|expired`；ready 时完成 token 规范化和派生 key。
3. `POST /admin/zhipu/oauth/exchange-code`（可选备选）
   - 输入：完整回调 URL，或 `code/authCode + state + session_id`。
   - 校验 state 后调用 `/api/v1/oauth/token`；若 poll 已成功则按幂等完成处理。
4. `POST /admin/zhipu/oauth/accounts/:id/refresh`
   - 走 §6.2 的重新代换/派生逻辑；不要伪造标准 refresh grant。

 pending session 至少保存：

```text
session_id, provider, flow_id, state,
poll_token（本地生成、按 secret 处理）,
authorize_url, redirect_uri,
expires_at, poll_interval_sec,
proxy_id, created_at, consumed/status
```

TTL 使用上游 `expires_at`（实测约 5 分钟），不要照搬 OpenAI/Grok 的 30 分钟。state/poll/session 单次消费；Redis 部署可参照 Grok 的跨实例 session store（`backend/internal/service/grok_oauth_service.go:37-47,147-198`）。

手工输入解析可复用 `xai.ParseAuthorizationInput` 的思路（`backend/internal/pkg/xai/oauth.go:599-630`），但必须补充 BigModel 的 `authCode` 和 custom scheme。

### 9.2 账号 credentials 字段

建议把**推理派生 key 与 OAuth 生命周期凭证分开**：

| 字段 | 内容/用途 |
|---|---|
| `api_key` | 完整派生 `id.secret`；标准 Coding Plan 推理与签名使用 |
| `access_token` | BigModel access token 或 Z.ai 业务 access token；用于派生 key、userinfo |
| `refresh_token` | BigModel 可选原值；当前无已证实刷新用途 |
| `zcode_jwt` | `data.token`；JWT 过期判断、StartPlan/billing/off-peak（如启用） |
| `zcode_jwt_expires_at` | 从 JWT payload `exp` 解码，统一 RFC3339/Unix 时间 |
| `zai_oauth_access_token` | Z.ai 原始 `data.zai.access_token`；业务 token 再代换使用，BigModel 为空 |
| `oauth_expires_at` | 可选：`/oauth/token` 响应 `data.expires_in` 的计算值，不与 JWT `exp` 混用 |
| `business_expires_at` | 可选：`/api/auth/z/login` 响应 `data.expires_in` 的计算值 |
| `oauth_provider` | `bigmodel` 或 `zai` |
| `account_mode` | `coding`，复用现有调度语义 |
| `base_url` / `api_protocol` | 沿用现有 zhipu 端点和协议配置；OAuth 初始化时设官方默认 |
| `zhipu_organization` / `zhipu_project` | 派生时选中的组织/项目；团队额度查询也可复用 |
| `client_version` | 初始兼容值 `3.11.2`；集中配置，勿散落硬编码 |
| `user_info` | 非敏感的账号显示信息；不要保存 callback/code/poll token 到最终账号 |

签名私钥、握手 nonce、PoW nonce 不入库；OAuth pending session 也不应成为永久账号 credentials。

### 9.3 推理与签名实现

建议新建独立的 Zhipu 签名组件，供 Chat Completions 与 Anthropic 原生路径共用：

1. 读取账号 `api_key` 并拆成 id/secret；缺任一部分按凭证错误处理。
2. 每小时读取一次 `/api/v1/agent/configs`，同时处理 `codingPlanSignature` 和 `proxyEndpoint.mapping`；生产实现默认按当前开启签名来实现，不依赖 fail-open。
3. 对需签名请求先做握手并缓存 Ed25519 私钥；握手 HTTP 使用同一账号 proxy 和出站 URL 安全策略，但不递归进入业务签名器。
4. 注入 `X-Session-Id` 后，按 §8.5/§8.6 生成其余 headers；签名之后再执行 endpoint routing。
5. 会话 ID 必须按终端用户会话隔离，禁止所有租户共享一个固定 ID；可复用项目 session 隔离思路，不能把 OAuth session_id 当长期推理 session。
6. 私钥失效时重新握手一次；错误按 `VERIFY_*` 和 HTTP 状态纳入现有 failover，不盲目高频重试。

Go 侧已有 `crypto/ed25519`、`crypto/hmac`、`crypto/sha256`、`crypto/aes/cipher` 与 `golang.org/x/crypto v0.58.0`，无需引入新框架。HKDF 可用现有 x/crypto 对应包实现。

### 9.4 token-market 必须同步调整的接入边界

仅新增 OAuth handler 不够，当前 zhipu OAuth 会被旧代码误路由或取不到正确 key：

- `backend/internal/service/openai_gateway_forward.go:1373-1383`：`AccountTypeOAuth` 默认进入 ChatGPT internal API，只有 Kimi 特判。必须为 Zhipu 增加官方 base URL 分支，不能让 Zhipu OAuth 走 `chatgptCodexURL`。
- `backend/internal/service/openai_gateway_service.go:1219-1264`：按平台选择 token provider。应增加 Zhipu provider，**返回派生 `api_key`，不是 OAuth access token**。
- `backend/internal/service/account.go:1637-1645`：`GetCNAPIKey` 对 Zhipu 已会读 `api_key`，适合额度查询；但 `@1790-1801` 的 `GetOpenAIProtocolAPIKey` 对非 APIKey 类型返回空，需要为 Zhipu OAuth 明确放行并读取派生 key。
- `backend/internal/service/anthropic_apikey_auth.go:20-33` 与 `openai_gateway_messages_anthropic_native.go:195-208`：当前非 APIKey 默认只写 `x-api-key`。Zhipu OAuth 需要官方的双头 `Authorization: Bearer` + `x-api-key`，并叠加 V4 签名头。
- `backend/internal/service/domain_constants.go:80-81,96`：默认 Coding/OpenAI 与 Anthropic base 已和 ZCode 对齐，应复用而非另造 endpoint。
- `backend/internal/service/cn_provider_quota_service.go:138-197`：可继续使用派生 key 查询传统额度；Zhipu OAuth 无需改为 JWT 额度查询。

管理 UI 可参照 Grok/OpenAI 的“授权 URL + state + 手工 code”流程（`frontend/src/composables/useGrokOAuth.ts:26-87`、`useOpenAIOAuth.ts:52-132`），或当前 Kimi worker 的 device/poll 展示方式；但 GLM 的 5 分钟 flow、provider 选择和派生 key 阶段需要独立状态，不要伪装成已有 refresh-token 生命周期。

### 9.5 推荐实施顺序

1. 先实现 CLI init/poll 与手工 code 两条上号路径，验证 state、单次消费、proxy 和 5 分钟 TTL。
2. 实现 token 规范化、Z.ai 业务 token 代换、完整派生 `id.secret`，并完成组织/项目选择。
3. 打通上述 Zhipu OAuth 路由与取 key 边界，先用测试上游验证不会误入 ChatGPT。
4. 实现签名握手、Ed25519、PoW 和失败重握手；覆盖 Chat Completions 与 Anthropic 两条真实转发路径。
5. 接入传统额度接口与现有阈值调度，最后补 JWT/业务 token 过期与重新上号状态。
6. 由后续有账号权限的工人各做一次 BigModel/Z.ai 真实账号验收；在此之前不宣称生产可用。

## 10. 后续真实账号最小验收清单

这些是静态调研无法替代的验证项：

1. 登录成功后核对 `data.token/data.bigmodel|zai/user/expires_in`，记录真实 JWT claims（只在安全环境，不写入公开文档）。
2. 完成派生 key，确认同名 `zcode-api-key` 的查找、创建、copy 权限及 `id.secret` 格式。
3. 分别用无签名、正确签名请求各做一次最小推理，确认服务端强制性与响应错误；不要以高频请求试探。
4. 验证 `X-Session-Id`、客户端版本与动态 `proxyEndpoint.mapping` 是否必须；验证 Chat Completions 和 Anthropic 两种协议。
5. 验证 Z.ai 业务 token 再代换、BigModel refresh_token 是否存在可用但未在客户端使用的续期能力；没有证据前不实现假定 grant。
6. 获取一次真实 `billing/balance` 与传统 `quota/limit`，确认字段单位、窗口和 full Coding Plan/StartPlan 权益差异。
7. **关键降级验证：**只在有明确授权和测试账号时，测试 full Coding Plan 能否直接用 zcode JWT 调 `zcode-plan`。若失败，维持本报告的 C；若成功且模型/权益一致，才把该路径评估为 B 类替代。

## 11. 最终证据索引（按问题）

| 问题 | 核心证据位置 |
|---|---|
| 授权 URL | `app.asar!out/host/index.js@1534003,1518547,1525293,1534497,1535259`；旧 appSecret 路径 `glm/zcode.cjs@7156471,7157325` |
| 回调/中间页 | 同文件 `@1518217,1524963,1548503`；公开中间页 JS `@2726,3481,3663,4563`；CLI `@1543916,1545918` |
| code 换 token | 同文件 `@1518757,1521753,1525293`；无效 code 实测 HTTP 500/code 2007 |
| 派生与推理 | 同文件 `@1626109,1570170,1563944,1564337,1568509,1568692`；`glm/zcode.cjs@4482126,6157292,4997902` |
| 刷新 | 同文件 `@1549978,1521753`；JWT `app.asar!out/host/chunk-RWMCBKS2.js@517487` |
| 额度 | `app.asar!out/host/index.js@1631376,1747402`；current URL `app.asar!out/host/chunk-RWMCBKS2.js@386521`；项目 `cn_provider_quota_service.go` |
| 签名/PoW | `app.asar!out/host/chunk-RWMCBKS2.js@510396,510530,512856,514327,514397,515164,515932,519190,522592,523930,526554`；`glm/zcode.cjs@4492921` |
| 项目落点 | `openai_gateway_forward.go:1373`、`openai_gateway_service.go:1219`、`account.go:1639,1790`、`anthropic_apikey_auth.go:20`、`domain_constants.go:80,96`、`cn_provider_quota_service.go:138` |

---

**一句话交接：**可以做，而且不必找 app secret；但按标准 Coding Plan 兼容口径，必须把“CLI OAuth 上号 + 派生 `id.secret` + V4 签名/PoW + 重新上号”作为完整交付，不能只实现 OAuth 登录后把 token 当普通 API Key 转发。
