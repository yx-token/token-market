# Zhipu / ZCode 协议包

本包实现 ZCode 3.11.2 的 BigModel、Z.ai CLI OAuth、手工 code 交换、业务凭证规范化、推理 key 派生、V4 请求签名和临时 Redis flow 存储。使用项目已有依赖；服务层、数据库、管理路由和网关的第二期接入见 [INTEGRATION.md](INTEGRATION.md)。

## 对外 API

| API | 作用 |
| --- | --- |
| `NewClient(Options) (*Client, error)` | 注入 `HTTPClient`，或通过 `ProxyURL` 获取项目共享代理客户端；版本默认 `ClientVersion=3.11.2`，origin 默认仅 HTTPS |
| `GeneratePollToken() (string, error)` | 32 字节安全随机数的小写 hex；手工 state 也可使用同一生成器 |
| `NewRedisOAuthSessionStore(redisClient)` | 临时 flow 的 Create/Acquire/Save，Lua 独占领取和终态幂等结果，保持上游 TTL |
| `Client.Init(ctx, provider, pollToken)` | 初始化一次 CLI flow，返回含 `State` 的 `InitResult`，校验 HTTPS 授权地址、state、过期时间和轮询间隔；展示 URL 的 provider 回调参数改写为 DesktopRedirectURI，其余参数保留 |
| `Client.Poll(ctx, provider, flowID, pollToken)` | 单次轮询，校验 ready 的 JWT、所选 provider 的 access token 和 user_id；`Tokens` 仍是原始数据 |
| `Client.NormalizeTokens(ctx, provider, TokenData)` | 读取 JWT exp、保留原 OAuth token、代换 Z.ai 业务 token；userinfo 失败回退默认资料 |
| `Client.ManualAuthorizationURL(provider, state)` | 返回授权 URL 和必须原样保存的中间页 `redirect_uri` |
| `Client.DesktopRedirectURI()` | 当前配置/版本的中间页 URL |
| `ParseAuthorizationInput(provider, raw)` | 解析完整 deep link、query 或 JSON `{code/authCode,state}`，返回 `Callback` |
| `Client.ExchangeCode(ctx, provider, callback, expectedState, redirectURI)` | 比较 state 后交换 code，返回规范化 `TokenSet` |
| `Client.ExchangeBusinessToken(ctx, originalOAuthToken)` | 重新代换 Z.ai 业务 token，返回 token 和独立过期时间 |
| `Client.UserInfo(ctx, provider, accessToken)` | 获取用户原始资料，兼容 Z.ai 无外层 data 的响应 |
| `ParseJWTExpiration(jwt)` | 仅解码 payload 的数值 exp；不执行 JWT 签名验证 |
| `TokenSet.JWTStatus(now)` / `JWTExpiring(now)` | 三态 `valid/expiring/unknown`，已知 exp 使用 30 秒提前量；unknown 不算已过期 |
| `TokenSet.BusinessExpiring(now)` | 300 秒提前量；Z.ai 业务 TTL 未知时为 true，下次使用前重新代换，BigModel 不受影响 |
| `Client.DeriveKey(ctx, provider, businessAccessToken)` | 选择非团队组织/项目，查找或创建同名 key，copy secret，返回 `DerivedKey` |
| `SplitCredential(credential)` | 校验并拆分完整 `id.secret` |
| `SourceHeaders(SourceOptions)` | 构造 ZCode 来源头；版本、语言、时区、平台、OS 版本和设备 ID 可配置 |
| `NewRemoteConfigCache(client, ...RemoteConfigOptions)` / `Get(ctx, credential)` | 合并运行时 headers，强制使用完整凭证的 X-Api-Key，不发送 Authorization；按凭证缓存成功快照一小时，失败退避一分钟 |
| `NormalizeEndpointMappings(mappings)` / `AgentConfig.MapEndpoint(originalURL)` | 仅允许无 userinfo/query/fragment 的 HTTPS 映射，去尾斜杠、拒归一化重复源；保留原请求 query |
| `NewSigner(client, SignerOptions)` | 绑定原始业务 origin、签名模式和配置缓存 |
| `Signer.Sign(ctx, req, credential, sessionID)` | 不读取 body，原地写来源头、V4 签名头和 PoW |
| `Signer.Invalidate(credential)` / `Clear()` | 清理进程内私钥和临时绕过状态 |
| `Signer.Bypass(credential)` | 丢弃私钥，并对该 credential/origin 临时绕过签名 `SignatureBypassTTL=10m` |
| `SigningTransport` | 遇到指定 401 时重新握手一次；再次拒签后开始临时绕过，并无签名重发一次 |
| `RoutingTransport` | 嵌套到 SigningTransport.Base，签名后应用远程 endpoint 映射，再交给账号代理 transport |
| `IsSigningError(status, body)` / `IsUnsignedPath(path)` | 供现有转发链路自己处理重试及签名豁免 |

`ProviderBigModel`、`ProviderZai` 对应 `bigmodel`、`zai`；`PollStatus` 为 pending/ready/failed/expired。`APIError` 保留 HTTP 状态和脱敏后的业务 code，不泄漏原始响应。派生失败可用 `errors.Is` 检查 `ErrNoProject`、`ErrSecretUnavailable`。

## 第二期使用约定

1. 使用账号绑定的代理和出站 URL 策略创建 `Client`。`Options` 的 endpoint overrides 和签名 origin 均为可信服务端配置；不得直接使用未经校验的管理请求字段。注入客户端时由调用方禁止跨源重定向；默认客户端已经禁用重定向。签名握手客户端必须是未包装业务签名器的客户端，避免递归。
2. 应用保存 provider、flow ID、InitResult.State、本地 poll token、上游到期时间和轮询间隔，不使用上游返回的 poll token 替换本地值。按 `poll_interval_sec` 调用 `Poll(ctx, provider, flowID, pollToken)`；ready 后 NormalizeTokens，再 DeriveKey。包不启动轮询协程；第二期服务使用 RedisOAuthSessionStore 存储并领取 pending flow。
3. 手工回调复用 CLI flow 的上游 state 和 TTL，原样保存最初中间页 URL。调用 ExchangeCode 前由服务层校验到期时间和独占消费 session；poll 与手工回调共享完成状态。包比较 state，并要求 redirect_uri 与当前 `DesktopRedirectURI()` 相等（HTTPS、实际 ZCode origin、路径、deep link 和版本均一致；query 编码/顺序可以不同）。第二期 Redis flow 实现跨实例防重放和短期幂等结果。
4. 保存完整派生 `Credential` 到 `api_key`。`AccessToken` 是业务凭证；`OAuthAccessToken` 在 Z.ai 线上用于再次代换；JWT 和各类过期时间独立保存。JWT 无法解析或缺 exp 时为 unknown，NormalizeTokens 仍成功；unknown 只能用于提示，不能阻止派生 key 的推理调度。`ParseJWTExpiration` 辅助函数仍返回解析错误。Z.ai 未提供业务 expires_in 时不复用业务 token，下次调用业务接口前应重新代换；这不表示派生 key 过期。BigModel refresh token 仅保留，不存在已确认的 refresh grant。userinfo 查询失败时返回 `{id:"unknown",username:"user",displayName:"User"}`，token 交换或业务 token 代换失败仍中止。
5. 每个代理策略和原始业务 origin 复用一个 `Signer`，再按请求/终端会话构造包装器。必须按终端用户会话提供独立 `SessionID`，禁止用 OAuth flow ID 或全站固定值。调用方设置 `Authorization: Bearer <id.secret>`；Anthropic 同时设置 `x-api-key` 和 `anthropic-version`。
6. `SignatureAlways` 为默认值，always/never 的签名决策不读取远程开关；`SignatureAuto` 跟随远程开关，读取失败/缺 enable 时按未开启签名处理。三个 StartPlan/off-peak 特殊路径、跨 origin 请求，以及临时绕过中的请求，均先清理全部 V4 签名头，再无签名放行。远程配置无后台定时器，按需刷新，成功按凭证缓存一小时；失败不保留旧快照，退避一分钟后可重新拉取。调用方 context 取消仍返回错误，不进入失败退避。`RemoteConfigOptions.Headers` / `SignerOptions.ConfigHeaders` 对应原版运行时 `codingPlanSignature.headers`，不是远程响应字段；构造时复制，始终覆盖 X-Api-Key 并删除 Authorization。
7. 推理请求先对原始 business origin 签名，再在 `RoutingTransport` 中执行映射；底层 Base 必须对目标执行项目 URL 安全策略。`SigningTransport.Base` 必须与握手客户端使用一致的账号代理策略；未设置时复用注入的 `*http.Client.Transport`，其他自定义 HTTPClient 必须显式提供 Base。嵌套路由器后，每次重试都从原始 URL 重新签名、重新映射，原请求 URL/Host 不变。远程配置故障时路由器使用原始地址发送，不阻断 always/never 模式。
8. `SigningTransport` 仅缓冲 HTTP 401 错误体，不缓冲成功的流式响应；有 body 的请求只有提供 `GetBody` 才能重试。首次签名拒绝作废对应旧私钥并重新握手；第二次拒绝再作废私钥，按 credential/origin 开始十分钟绕过，并立即无签名重发一次（最多三次业务请求）。绕过期间的请求保持无签名，十分钟后恢复签名和握手；Invalidate/Clear 可提前结束。无签名响应不会触发签名重试。直接使用 Sign 的转发器需自己实现相同的有界响应处理，推荐直接组合下面两个 transport。
9. 私钥仅在内存缓存，按 credential/origin 隔离，最多保留 256 个条目；失败握手不缓存；并发握手合并。删除账号、换 key 或停用代理时调用 Invalidate/Clear，重启后自然重新握手。

路由组合示例（`accountTransport` 是调用方已按账号代理和出站策略配置的 `http.RoundTripper`）：

```go
config := zhipu.NewRemoteConfigCache(client, zhipu.RemoteConfigOptions{
    Headers: runtimeConfigHeaders,
})
signer, err := zhipu.NewSigner(client, zhipu.SignerOptions{
    Origin: businessOrigin, Config: config, Mode: zhipu.SignatureAlways,
})
if err != nil { return err }
transport := &zhipu.SigningTransport{
    Signer: signer, Credential: credential, SessionID: endUserSessionID,
    Base: &zhipu.RoutingTransport{
        Config: config, Credential: credential, Base: accountTransport,
    },
}
// 给业务 http.Client 设置 Transport: transport；请求 URL 保留原始业务地址。
```

`SourceOptions.Timezone`、`OSVersion` 均可注入；Timezone 默认使用有效 IANA 时区，time.Local 名称为 Local 或无效时回退 Asia/Shanghai。OSVersion 默认 unknown，部署时应注入实际值。Client 来源头的 HTTP-Referer 始终跟随实际配置的 ZCode origin；单独使用 SourceHeaders 可通过 SourceOptions.ZCodeOrigin 指定。Options 和 SignerOptions 各自默认拒绝 HTTP，`AllowInsecureHTTP` 必须显式设置且仅用于测试；Client 的该选项不会自动开启 Signer 的选项，生产接入不得暴露此测试选项。

与原版手工解析的有意差异：本包 trim 外层输入和 code/state 值，拒绝重复的 code/authCode/state **query 参数**，而不是接受歧义值；JSON 输入按 Go 标准 JSON 解码。代码中保留了协议实现测试，不代表真实账号/权益已验收。

## 验证与外部标准向量

```sh
go test -tags=unit ./internal/pkg/zhipu/...
go test -tags=unit -race -cover ./internal/pkg/zhipu/...
golangci-lint run ./internal/pkg/zhipu/...
golangci-lint run --build-tags=unit ./internal/pkg/zhipu/...
```

`TestZCodeSigningVectors` 只读调研员维护的 `testdata/zcode_signing_vectors.json`，支持实际到位的 schema_version=1：凭证解析、HKDF、握手 HMAC、privateCipher、业务签名和 PoW 六组。文件缺失、格式不匹配或任何值不一致均失败。覆盖 0/8/12 bit 和非法 PoW 难度；正式签名始终使用 8 bit。凭证中有效空白按原版保留，不修剪参与签名的 id/secret；换行符仍明确拒绝，避免无效 HTTP 头。
