# GLM Coding Plan OAuth 接入说明

第二期应用适配位于 `internal/service/zhipu_*.go`、`internal/repository/zhipu_oauth_*.go`、`internal/handler/admin/zhipu_oauth_handler.go`。前端接口沿用约定，标准 response envelope 的 `data` 为下列响应。

## 管理接口

| POST 路径 | 请求 | data |
| --- | --- | --- |
| `/admin/zhipu/oauth/init` | `{provider:"bigmodel"\|"zai",proxy_id?}` | `{session_id,provider,authorize_url,expires_in,expires_at,interval}` |
| `/admin/zhipu/oauth/poll` | `{session_id,proxy_id?}` | `{status:"pending"\|"success"\|"expired"\|"failed",interval?,message?,result?}` |
| `/admin/zhipu/oauth/exchange-code` | `{session_id,callback_url?,code?,state?,proxy_id?}` | `{provider,credentials,display}` |
| `/admin/zhipu/accounts/:id/refresh` | 无 | Account DTO，直接在 data 下，不再包 account |

`expires_in`、`interval` 单位为秒，`expires_at` 为 Unix 秒。会话 ID 与 poll token 各自使用 32 字节随机数；poll token 不返回前端。Redis key 为 `zhipu:oauth:session:<session_id>`，TTL 不超过上游 expires_at。代理在 init 时固定，后续显式传入不同 proxy_id 返回 400。

CLI init 返回的授权地址在展示前按桌面客户端改写：BigModel 仅替换 `redirect`，Z.ai 仅替换 `redirect_uri`，目标均为当前 `DesktopRedirectURI()` 中间页。flow、state 和其他参数保留；Redis 存储及手动换码使用同一中间页 URI。中间页保留自动 CLI 轮询桥接，其“打开 ZCode”按钮的 `zcode://oauth/callback?...` 链接可直接复制到手动输入框；不要粘贴中间页本身的 HTTPS 地址。

Poll 的 pending/expired/failed 状态返回 HTTP 200；通信、存储或配置错误走标准错误响应。手动回调格式/state 错误为 400，正在被其他请求处理的会话为 409。poll/manual 用 Redis Lua 领取同一独占 owner；成功会话保存短期幂等结果，后续 poll/manual 返回同一结果，不再换 code 或派生。终态删除 poll token、state、flow ID、代理 URL 和 redirect URI。进程若在换码中崩溃，领取状态保留至 flow 自然过期；用户重新 init，防止不确定状态下再次消费单次 code。

poll 和 exchange-code 的请求体不写入审计日志，避免可取回成功结果的 OAuth flow ID 或完整回调长期留存；路由、操作人和处理结果仍正常审计。普通运行会话的 session_id 保留原有审计语义。

成功结果的 credentials 包含完整 `api_key=id.secret`、业务 `access_token`、`zcode_jwt`、provider、组织/项目 ID、client_version 和白名单 user_info。每次登录均返回完整生命周期快照：缺失的 `refresh_token`、不适用的 `zai_oauth_access_token`、未知的 `zcode_jwt_expires_at` / `business_expires_at` / `oauth_expires_at` 明确为 JSON null；Z.ai 业务 TTL 未知时同时返回 `business_expiry_unknown=true`。已知时间使用 UTC RFC3339；display.expires_at 使用 Unix 秒。display 中组织/项目名称目前省略，只保存 ID。

重新授权整体替换上述生命周期，只继承协议、端点、模型映射和错误策略等管理员配置。前端同样为遗漏的可选生命周期字段补 null；通用后台更新按字段是否显式存在来合并，因此 null 不会恢复成旧敏感值，读取端将其视为空或未知。这不改变普通账号编辑及其他平台的缺省字段保留语义。普通 Account DTO 仍隐去 key、JWT 和 OAuth token。

## 推理和续期

1. 前端用 OAuth result 创建 `platform=zhipu,type=oauth` 账号，并增加 `account_mode=coding`、协议和国内/国际 base URL。
2. Chat Completions、Responses 转 Chat、Anthropic 原生直通和 adaptive 共用 `ZhipuGateway.Do`，使用派生 api_key。Anthropic 同时带 Bearer 和 x-api-key。账号测试/渠道推理探测共用这一发送层。
3. `SigningTransport → RoutingTransport → HTTPUpstream`：先在原始 URL 签名，再应用远程精确端点映射，最后通过账号代理和现有 URL 策略发送。握手/远程配置也使用同一账号代理；重试通过 GetBody 重放，重新从原始 URL 签名和映射。禁止重定向和私网目的地。
4. X-Session-Id 从真实调用 API Key ID 与现有显式会话标识/内容会话种子派生。不同 API Key 隔离；缺少身份或会话种子时随机生成。OAuth session_id 不参与推理会话。
5. Z.ai 业务 TTL 进入五分钟窗口或未知时重新代换并派生；未知 TTL 标记 `business_expiry_unknown=true`，下一次发送/检查前再次代换。强制重新派生优先复用仍有效的业务 token；业务 token 提前撤销时，尝试原 OAuth token 再代换一次。
6. 推理/握手拒绝失效 key 时，通过 OAuthRefreshAPI 的账户分布式锁重读账号，重新 list/copy 同名 zcode-api-key，最多重新派生和重放一次。普通 WAF 403/额度错误不认定为凭证失效。仍失败或业务/OAuth 明确失效时，以精确 credentials + proxy CAS 标记 error、schedulable=false，提示重新授权。新授权和管理员状态不会被旧请求覆盖。
7. 后台续期成功使用 CAS 原子保存合并后的凭证并发出 scheduler outbox；局部刷新响应遗漏字段时不清除旧 refresh token、JWT 等。该局部更新策略与重新登录的完整快照替换不同。后台候选包含没有 refresh_token 的 GLM OAuth。未证实的 BigModel refresh grant 不实现。JWT 到期信息保留供 UI 提示，实际推理凭证由上游校验。
8. 传统 Coding Plan 额度仍用派生 key（无 Bearer），OAuth 的组织 ID 不强制解释为团队套餐。channel monitor 复用原额度入口；billing probe 支持 GLM OAuth 的派生 key，官方域名仍按现有规则标记 unsupported，兼容中转才调用 `/v1/sub2api/billing`。

V4 的二次拒签仍遵循协议包策略：重新握手后再被 `VERIFY_SIGNATURE_INVALID` 拒绝，暂时绕过签名十分钟并无签名重发一次。`VERIFY_APIKEY_EXPIRED` 在应用发送层优先触发重新派生，避免用无签名绕过掩盖 key 失效。

## 与原版的有意差异

1. 手动回调 trim 外层输入及 code/state，拒绝重复的 code/authCode/state query 参数，避免歧义输入。
2. 签名模式默认 always；只有显式选择 auto 才跟随远程开关，不能把客户端放行降级当成服务端免签承诺。
3. 二次拒签后的绕过按 `(credential, origin)` 限时十分钟，过期恢复握手和签名，不永久绕过。
4. 远程配置失败不缓存成功快照，也不缓存失败一小时；使用一分钟失败退避，避免故障时频繁拉取。auto 仍按未开启签名处理。
5. 原版会因已知 zcode JWT 到期清除 active session 和派生 key；本项目推理使用派生 `api_key`，JWT 到期不等于 key 失效，因此 JWT 已知到期及 unknown 都不单独触发重新授权或停调。由上游凭证错误驱动重新派生；业务/OAuth 无法恢复或重新派生后仍拒绝，才标记需要重新授权。

## 配置

部署配置示例在仓库 `deploy/config.example.yaml`：

```yaml
gateway:
  zhipu:
    signature_mode: always # auto / always / never
    client_version: "3.11.2"
    config_headers: {}
```

默认 always；auto 跟随远程开关，远程失败按未开启处理并短退避；never 禁签。远程映射在三个模式均可执行。成功配置按账号/代理/凭证缓存一小时，失败一分钟退避。配置需按现有服务器配置流程重启生效。生产没有 AllowInsecureHTTP 配置入口。

启用出站白名单且自行维护固定列表的部署，需要加入 `bigmodel.cn`、`open.bigmodel.cn`、`zcode.z.ai`、`api.z.ai`、`chat.z.ai`，以及经审查的远程映射目标；默认列表和示例已增加 OAuth 所需域名。

## 可复验检查

```sh
cd backend
go test -tags=unit ./...
go test -tags=unit -race ./internal/pkg/zhipu/... ./internal/service ./internal/repository ./internal/handler/admin ./internal/server/middleware -run 'TestZhipu|TestZCodeSigningVectors|TestRedisOAuth|TestKimi|TestRedactAuditBody'
golangci-lint run ./...
golangci-lint run --build-tags=unit ./...
```

测试只用 TLS httptest、miniredis 和 SQL mock，不访问真实账号、不运行 ZCode。只读原版向量仍由 `vectors_test.go` 逐项比较。

2026-10-25 首次接入本地验证（Go 1.27.3，golangci-lint 2.14.0；仓库 CI lint 使用 2.13）：

| 检查 | 结果 |
| --- | --- |
| 全量 `go test -tags=unit ./...`（含 Kimi） | 通过；service 204.952s |
| 上述 GLM/Kimi/审计目标竞态测试 | 五个包全部通过 |
| `TestZCodeSigningVectors` | 六组、17 个子例全部通过；向量文件未修改 |
| 普通全量 lint | 通过，`0 issues.` |
| unit 标签全量 lint | 未通过，65 条既有测试问题：depguard 6、errcheck 18、govet 3、staticcheck 30、unused 8 |
| unit 标签按本次 diff 检查（含未跟踪 GLM Go 文件） | 通过，`0 issues.` |

unit lint 报告位于已有的 `auth_wechat_oauth_test.go`、`api_key_service_cache_test.go`、`env_reachability_test.go` 等文件；未为本任务放宽全局 lint 规则或修改这些无关测试。全量单测日志 `/tmp/token-market-zhipu-unit-verified.log`，普通 lint 日志 `/tmp/token-market-zhipu-lint-final.log`，unit lint 完整输出 `/tmp/token-market-zhipu-unit-lint-final.log`。

同日第二期审查修正后，授权 URL 改写与生命周期整体替换的验证结果：

| 检查 | 结果 |
| --- | --- |
| 后端 `go test -tags=unit ./...` | 全部通过，含 Kimi 和只读签名向量；service 209.119s |
| 后端 `golangci-lint run ./...` | 通过，`0 issues.` |
| 前端 `pnpm typecheck` | 通过 |
| 前端 `pnpm lint:check` | 通过；使用无自动修复的全量 lint，避免修改其他平台 |
| 前端定向创建/重授权/builder 测试 | 72 项全部通过 |
| 前端 `pnpm test:run` | 2683 通过、8 项既有失败；345 个文件中 341 通过、4 失败 |

8 项既有失败分别是 `settings.authSourceDefaults.spec.ts` 3 项、`UseKeyModal.spec.ts` 3 项、`openaiFastPolicyLocales.spec.ts` 1 项、`feature-access.spec.ts` 1 项。GLM、Kimi 测试均通过。最新日志为 `/tmp/token-market-zhipu-review-unit.log`、`/tmp/token-market-zhipu-review-lint.log`、`/tmp/token-market-zhipu-review-typecheck-final.log`、`/tmp/token-market-zhipu-review-eslint-final.log`、`/tmp/token-market-zhipu-review-vitest-final.log`。

## 真实账号手工验收

BigModel、Z.ai 各使用一个测试账号，逐项低频检查，不记录 token/secret 到日志：

1. 管理页选择 provider/代理，完成 CLI 授权；确认前端只拿 session_id，没有 poll token，成功创建 OAuth 账号并带完整派生 api_key。
2. 新开会话，用手动粘贴回调完成登录；错误 state 被拒，完成后再次 poll/提交返回同一结果，不能重复换码。
3. 各做一次 Chat、Responses 和 Anthropic 最小请求；检查响应正常、会话隔离、动态映射、账号代理及签名模式。不要把所有请求改到 ChatGPT 或 zcode-plan 特殊路径。
4. 查询传统额度并在 channel monitor 查看相同窗口；核对套餐、组织权限和国际线的窗口字段。官方 billing probe 显示 unsupported 属现有行为。
5. Z.ai 到期/未知 TTL 后确认业务代换；点刷新确认复用既有同名 key。测试凭证撤销时确认停调和重新授权提示，再重新授权并清除错误恢复。BigModel 不应发出假定的 refresh grant。
6. 按调研文档第 10 节，在明确授权的测试环境比较正确签名与无签名、真实 JWT/业务 TTL、套餐权益和 copy 权限。静态向量与假上游通过不能替代这些验收。
