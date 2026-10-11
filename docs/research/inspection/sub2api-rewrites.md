# sub2api 探测请求改写：当前 HEAD 与 v0.2.14 对照

核实日期：2026-10-12。只读源码位置固定为 `/Users/duegin/project/github-fork/sub2api`。先读取当前 HEAD `3a6fd1c9d`（`v0.2.15-1-g3a6fd1c9d`），再用 `git diff v0.2.14..HEAD` 与 `git show v0.2.14:<路径>` 对照。没有编译、执行、修改源码、切分支或更改其工作区。token-market 的契约和集成测试迁移基线仍为 **v0.2.14**，本次对照不构成新版本兼容认证。

第 1–7 节保留原 v0.2.14 核实结果及该标签行号；当前 HEAD 的差异集中在第 8 节。路径缩写：`S/` = `backend/internal/service/`，`I/` = `backend/internal/`。[模型资料第 6 章](model-landscape.md#第-6-章-sub2api-v0214模型目录定价表与网关改写)属于标签快照，不代替当前差异说明。

本任务所有阶段仅在 token-market 一侧通过现有 Admin API、用户 API、网关、只读库与配置实现功能；不可取得的字段记“无法判定；依赖 sub2api，后续处理”，不提出或实施 sub2api 修改。
## 1. Anthropic OAuth / Setup Token 账号

只要客户端不是真 Claude Code（UA 为 claude-cli 且带合法 user_id），就会触发以下改写（`S/gateway_forward.go:201-268`）。

- **system 整体替换为 3 块**（`S/gateway_service.go:41-57`，`S/gateway_claude_oauth_body.go:788-810`）：
  1. 计费块 `x-anthropic-billing-header: cc_version=…; cc_entrypoint=cli;`
  2. `You are Claude Code, Anthropic's official CLI for Claude.`
  3. 一段通用扩展提示，带 5m 的 `cache_control`

  注入开关默认开启（`S/setting_gateway_runtime.go:936`）。
- **买家的 system 被挪进 messages 开头**：变成 user `[System Instructions]\n…` + assistant `Understood. I will follow these instructions.`（`oauth_body.go:940-981`）。
- **缺省补字段**：缺什么补什么——`temperature=1`、`max_tokens=128000`、`tools=[]`、`metadata.user_id`；无 tools 时删除 `tool_choice`；thinking 为 enabled/adaptive 时补 `context_management`（`:248-313`）。
- **不改的字段**：`thinking`、`budget_tokens`、`top_p`、`output_config.effort` 原样透传。
- **例外——网关本地拒绝**：Opus 5.5 / Sonnet 5.5 若用 `thinking.type` 为 enabled/disabled 或强制 `tool_choice`，sub2api 在本地直接返回 400，请求不会到达官方（`S/gateway_request.go:578-626`）。
  - **对参数指纹（A15）的影响**：这几项在 5.5 型号上由网关拦截，被测与基线结果必然相同，无法区分底层型号，不能作为指纹使用；也不能把这类 400 当成官方行为写进规则表。
- **请求头**：客户端请求头一律不透传；`anthropic-beta` 换成固定 9 项（claude-code、oauth、interleaved-thinking、effort、context-management 等），客户端的 beta 只保留 structured-outputs（`S/gateway_upstream_request.go:150,546-556`；`I/pkg/claude/constants.go:98-110`）。
- **image / document**：原样透传，但没有 files-api beta。
- **cache_control**：
  - 默认透传；
  - tools 最后一项会被补一个 5m 断点；
  - 工具超过 5 个时，工具名会被改成假名，响应里再还原；
  - 断点总数超过 4 个时，先删 tools 上的，再删 messages 上的（`S/gateway_tool_rewrite.go:64-92,266-305`；`oauth_body.go:1057-1138`）。

## 2. Anthropic 响应

- **usage**：原样返回，只有两个例外（`S/gateway_upstream_response.go:944-973,1416-1447`）：
  - `cache_read` 为 0 时用 `cached_tokens` 补上；
  - 账号开启 `cache_ttl_override` 时改写 `cache_creation` 的 5m/1h 拆分（此开关默认关）。
- **stop_reason**：不改。被 sub2api 映射过的 model 会改回请求时的名字（官方回退返回的型号名不受影响）。
- **响应头**：走白名单（`I/util/responseheaders/responseheaders.go:15-38`），只有 content-type、date、x-request-id、x-ratelimit-*、retry-after 等。`anthropic-ratelimit-*` 与 `request-id` **不会透传**，探针读不到额度利用率头。

## 3. OpenAI Codex OAuth（/v1/responses）

- **模型映射**（`S/openai_codex_transform.go:13-80`，`S/openai_model_alias.go:50-91`）：
  - gpt-5、gpt-5-mini、gpt-5-nano、gpt-5.1 → gpt-5.4
  - 含 codex 的 → gpt-5.3-codex
  - `-high` 等后缀会被去掉（并解析成 effort）
  - 检测策略必须写规范 ID。
- **instructions**：为空时才填 Codex 官方 base 指令；input 里的 system 会被并到 instructions 前面（`:1308-1361,1494-1502`）。
- **stream / store**：强制 `store=false`、`stream=true`；客户端要非流式时由网关聚合 SSE 后返回。删除 `temperature`、`top_p`、`max_output_tokens`、`metadata`、`user` 等（`:150-226`）。
- **reasoning.effort**：透传，只把 minimal 改成 none。
- **tools 与图片**：function 工具和 `input_image` 都支持（gpt-5.3-codex-spark 除外）。
- **usage**：原样返回，但 model 会改回请求名（`S/openai_gateway_request_body.go:978`）。
- **`OpenAI-Model` 头**：不透传，也不记录。
- **x-codex-\***：只写进账号 extra（`S/openai_gateway_forward.go:1296-1301`）；只有开了 passthrough 的账号才转发给客户端（`S/openai_gateway_passthrough.go:2472-2490`）。

## 4. Chat Completions 网关（kimi / zhipu / minimax 等 Coding Plan）

协议为 chat_completions 或 adaptive 时原样直转（`S/openai_gateway_chat_completions_raw.go:57-270`），只做这几处改动：

- 改 model；
- **GLM 的 reasoning_effort 归一**：low/medium → high，xhigh → max（`S/gateway_request.go:1614-1667`），所以 GLM 的"档位响应度"只能比较 high 与 max；
- developer 角色改成 system（`S/openai_chat_roles.go:10-29`）；
- 流式时强制 `include_usage=true`，usage chunk 也会下发给客户端。

tools、tool_choice、usage 明细、reasoning_content 都原样。如果账号配置的是 anthropic 协议，会先做协议转换，usage 是重新拼出来的（`S/openai_gateway_chat_completions.go:183`）。

## 5. Gemini / Antigravity

- **Antigravity**：
  - `thinkingConfig` 原样透传，同时用来挑选 -low/-medium/-high 模型变体（`S/antigravity_gemini_thinking_variant.go:52-86`）；
  - `systemInstruction` 前面插入 "You are Antigravity…"（`S/antigravity_gateway_service.go:576-619`）；
  - `usageMetadata` 解包后原样返回（`S/antigravity_gateway_streaming.go:261-292`）。
- **Gemini 平台原生路径**：不注入、不改，响应原样（`S/gemini_messages_compat_service.go:1161-1330,2696-2742`）。

## 6. usage_logs 的型号字段

- **upstream_response_model** 取自响应体：
  - Anthropic 用 `message.model` 或 `model`；
  - OpenAI 用 `response.model` 或 `model`，终态事件优先；
  - Gemini 用 `modelVersion`。
- **upstream_model_mismatch**：上面的值与 sent_model（映射后的上游模型，没有则用请求模型）做忽略大小写比较；响应里没有 model 时为 NULL（`S/upstream_response_model.go:71-172,274-315`；`S/gateway_usage_billing.go:1186-1206`）。A02 应读 `usage_records.upstream_response_model`（原始上游值），并自己做型号名归一化与官方回退白名单。

## 7. 订阅档位与额度利用率

- **订阅档位**：Claude 没有存档位的字段；OpenAI 在 `credentials.plan_type`。
- **Claude 利用率**：成功响应的 `anthropic-ratelimit-unified-5h/7d/7d_oi` 头被写进账号 extra，如 `session_window_utilization`、`passive_usage_7d_utilization`（`S/ratelimit_service.go:1983-2103`）。
- **Codex 利用率**：`x-codex-*` 头写进 extra，如 `codex_5h_used_percent`、`codex_7d_used_percent`（`S/openai_gateway_usage.go:965-1118`）。
- **Admin API**：`GET /api/v1/admin/accounts/:id/usage?source=passive|active&force=true`（`I/server/routes/admin.go:402`），返回 five_hour、seven_day 利用率。开跑前读窗口余量（方案中的"窗口不够就顺延"）优先用只读库里的 extra 或这个接口，`source=active` 会向官方发请求，慎用。


## 8. 当前 HEAD 的差异与实施影响

以下是源码一手事实，非真实厂商请求的实测。行号对应当前 HEAD；文件/函数比行号更稳定。

| 当前 HEAD 的变化 | v0.2.14 对照 | 对 token-market 的影响 | 当前源码定位 |
|---|---|---|---|
| OAuth 模拟 Claude Code 的 beta 白名单额外允许客户端显式传 mid-conversation-tool-changes 与 inline-tools，仍不推断缺失的配套 token | 仅保留 structured-outputs | 第 1 节“只保留 structured-outputs”仅对标签成立；工具功能比较必须使用相同请求头与通道，不把此版本差异归因卖家 | `S/gateway_upstream_request.go:548`、`I/pkg/claude/constants.go:23` |
| cache_control 限制四块后，遇到后续 1h 断点会把更早的 ephemeral 断点 TTL 提至 1h，修正顺序 | 仅限制块数，不修 TTL 顺序 | D12 默认选择简单、相同的 5m 前缀；若测试 1h，记录网关版本与实际语义，不能声称所有 cache_control 都原样透传 | `S/gateway_claude_oauth_body.go:1057`、`normalizeCacheControlTTLOrder:1155` |
| Codex OAuth 重放历史含 web_search_call 而未声明搜索工具时，补 web_search 声明；Responses Lite 用 additional_tools 项；已有声明时不补 | 无此补齐 | 本期工具题用本地 function、不用联网搜索；多轮输入中不掺其他真实买家历史。跨版本注入差异不得当作 A03 账号异常 | `S/openai_codex_transform.go:354`、`S/openai_oauth_web_search_history.go:80` |
| 多协议/按模型路由统一由 resolveUpstreamProtocol 决定，参考账号 protocol_rules、模型目录与厂商 profile；adaptive 优先判断入站协议是否被支持 | 分散判断 OpenCode Go、CN adaptive 与固定协议 | 第 4 节“adaptive 原样直转”不能作为当前所有入口的通则；基线匹配须包括入口、账号协议配置、型号映射，固定 CC 原生路径仍按该路径核对 | `S/upstream_protocol_routing.go:32`、`S/openai_gateway_chat_completions.go:115`、`S/openai_gateway_messages.go:56` |
| 新增 Command Code/Cline 等平台、钱包错误与 profile 分流；HTML 403 豁免范围扩展到多协议 API key | 原平台范围更小 | 本期不新增厂商支持，不把未知平台直接纳入检测；现有契约闸门和未知平台“不支持挂单”规则继续生效 | `S/ratelimit_service.go:402`、`S/provider_profile.go`、`S/platform_catalog.go` |

已对照且相关实现未变化：`gateway_forward.go`、`gateway_request.go`（5.5 本地参数校验与 GLM effort 归一）、`openai_model_alias.go`、`openai_gateway_chat_completions_raw.go`、`gateway_upstream_response.go`、`upstream_response_model.go`、`gemini_messages_compat_service.go`、`antigravity_gateway_service.go`、`util/responseheaders/responseheaders.go`。Codex transform 虽新增历史工具补齐，删除 max_output_tokens/temperature、模型归一与默认 instructions 的相关段未改变；窗口采集相关代码也无影响本期语义的变化。此结论限于列出的行为，不是对全部 105 个 service 相关变更的兼容认证。

## 9. 可用证据与依赖缺口

- A02 优先读取现有 usage_records 的 upstream_response_model，自己归一化与应用官方路由白名单；不能直接将 sub2api 的严格 mismatch 布尔值当作违规。响应回显已改回请求名时，不能冒充原始上游证据。
- `OpenAI-Model` 头仍不由现有网关透传/记录。缺少它的回退证据记“依赖 sub2api，后续处理”，不得通过修改网关补取。
- Claude 订阅档位仍没有可用的现有核档字段。只展示令牌可读声明，缺失标“未核验”；不能由利用率猜测 Max 5x/20x。官方核档须等待现有接口提供，平台不持厂商凭据直连官方。
- Admin usage 的 source=active 会由 sub2api 访问官方。本次实现和离线测试不调用它；开跑优先读取 source=passive 或只读 extra，未知余量应保守顺延。
- Antigravity 协议转换可能生成 Claude 风格 id/model；只有现有原始 modelVersion/只读用量记录足够时才判定，否则无法判定。
- 本期扩展只读查询须登记在 token-market 的 contract/queries 并核对 v0.2.14 表结构；不会新增、修改 sub2api 迁移、源码、测试或脚本。新 HEAD 的兼容登记不随文档自动变化。
