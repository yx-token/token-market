# 闲置算力交易所 · 各厂商模型与订阅档位资料（出题用）

> 整理日期：2026-10-12。证据访问日期、数字与来源编号沿用原报告；来源编号仅在本报告内有效，不能与另三份报告混用。论文/官方页面的声明是该来源的一手事实，不等于对 token-market 真实账号的实测；【推断】与候选设计均未获真实账号验证。
>
> 本文保存 2026-10-11 访问的厂商型号、权限、档位与参数资料，部分资料跨到 10-12，以各段原日期为准。2026-10-12 是整理日期，不表示重新访问厂商网页；时效性强的事实须在上线前复核。第 6 章原核对标签为 v0.2.14；当前唯一有效源码位置为 /Users/duegin/project/github-fork/sub2api，HEAD 为 3a6fd1c9d（v0.2.15-1-g3a6fd1c9d）。本次只读对照与差异见[网关核对](sub2api-rewrites.md)，不能把标签行为一概当成当前行为。文中的自报身份与不确定厂商行为仅为研究背景，不作为认证或扣价判据。
>
> 实施优先级：[确认后的汇总](../quality-inspection-2026-10-12.md)与[数据契约](data-contract.md)高于本文的候选建议。所有业务、迁移、测试和脚本只能在 token-market 一侧实现，通过现有 Admin API、用户 API、网关、只读数据库与配置驱动 sub2api；能力缺口记为“依赖 sub2api，后续处理”，不修改 sub2api 的任何代码。


- 调研日期：2026-10-11。文中所有 URL 的访问日期均为 2026-10-11，除非另有标注。
- 用途：为 token-market 的质检题库（模型身份、知识截止、上下文长度、推理强度、档位权限）和检测策略的 `models` 配置提供依据。
- 证据等级标注：
  - **官方**：厂商官网、文档、帮助中心、官方博客或官方开源仓库。
  - **sub2api 源码**：`Wei-Shaw/sub2api` v0.2.14 的可读实现为源码事实；作者注释中的实测、来源识别或经验属于社区观察，不等于厂商声明。当前 HEAD 行为以[网关核对](sub2api-rewrites.md)为准。
  - **社区观察**：Reddit、X、HN、V2EX、知乎、Linux.do、GitHub issue、测评博客等，未经官方确认。
  - **未找到官方资料**：查过但官方没有写明。
- 方法：第 1–5 章由各厂商的官方页面整理（部分文档站拦截了自动抓取，改用 `.md` 版本或原始页面读取）；第 6 章来自只读克隆的 sub2api v0.2.14 源码，没有编译或运行任何下载的代码。
- 已有的降智检测方法论见仓库内 [2026-10-10 降智调研](../model-degradation-gpt-claude-2026-10-10.md)，本文只补充厂商事实与网关行为。

## 目录

- 第 0 章 出题速查（模型速查、档位速查、各通道自我介绍）
- 第 1 章 Anthropic Claude
- 第 2 章 OpenAI（ChatGPT 订阅 + Codex OAuth）
- 第 3 章 Google Gemini
- 第 4 章 xAI Grok（含 Google 与 xAI 共用的出题要点和存疑清单）
- 第 5 章 国产 Coding Plan 与 OpenCode Go
- 第 6 章 sub2api v0.2.14：模型目录、定价表与网关改写
- 第 7 章 出题与检测建议（跨厂商）

## 第 0 章 出题速查

> 本章汇总第 1–6 章的结论，逐条来源（URL 与访问日期 2026-10-11）见对应章节。"未找到"即"未找到官方资料"。

### 0.1 主力模型速查

| 厂商 | 模型（官方 ID） | 知识截止 | 上下文 / 最大输出 | 推理参数与默认值 | 关键行为与指纹 | 详见 |
|---|---|---|---|---|---|---|
| Anthropic | `claude-fable-5-1`（2026-09-01） | reliable 与训练截止均为 2026-06 | 1M / 128K | effort low–max，默认 high；thinking 常开，`disabled` 和 `budget_tokens` 都返回 400 | `tool_choice: any/tool` 返回 400；最小可缓存 512 token；缓存读 $0.25 | 1 章 2、3 节 |
| Anthropic | `claude-opus-5-5`（2026-09-22） | 2026-06 / 2026-06 | 1M / 128K | 默认 **medium**；thinking 在任何 effort 下都关不掉 | 非默认 `temperature` 返回 400；`tool_choice: any` 返回 400；最小可缓存 512 | 1 章 |
| Anthropic | `claude-sonnet-5-5`（2026-09-28） | 2026-06 / 2026-06 | 1M / 128K | API 默认 high，Claude 应用和 Claude Code 默认 medium；`disabled` 返回 400 | **只有它接受** `thinking: {type: "between_tools"}`（effort ≤ high 时） | 1 章 |
| Anthropic | `claude-haiku-5-5`（2026-10-07） | 2026-06 / 2026-06 | 1M / 128K；提示超过 10 万 token 时适用高价 | 默认 **medium**；effort ≤ high 时可以 `disabled` | 无服务端拒答回退；sub2api v0.2.14 未收录 | 1 章 |
| Anthropic | `claude-opus-5`（2026-07-24） | **2026-05** | 1M / 128K | 默认 high；effort ≤ high 时可以 `disabled` | — | 1 章 |
| Anthropic | `claude-fable-5`、`claude-sonnet-5`、`claude-opus-4-8`、`claude-opus-4-7` | 2026-01 | 1M / 128K | 默认 high（Claude Code 中 Opus 4.7 默认 xhigh）；Opus 4.8 / 4.7 不传 thinking 就不思考 | 最小可缓存：Fable 5 为 512，Sonnet 5 和 Opus 4.8 为 1,024，Opus 4.7 为 2,048 | 1 章 |
| Anthropic | `claude-opus-4-6` | 2025-05（训练截止 2025-08） | 1M / 128K | low / medium / high / max（无 xhigh）；`budget_tokens` 已弃用但仍可用 | 接受 `temperature`；最小可缓存 4,096 | 1 章 |
| Anthropic | `claude-sonnet-4-6` | 2025-08（训练截止 2026-01；透明度中心写 2025-05，有冲突） | 1M / 128K | 同 Opus 4.6 | 最小可缓存 1,024 | 1 章 |
| Anthropic | `claude-haiku-4-5-20251001` | 2025-02（训练截止 2025-07） | 200K / 64K | 只支持 `budget_tokens`，不支持 effort | 旧 tokenizer；最小可缓存 4,096 | 1 章 |
| OpenAI | `gpt-6-astra`（2026-09-03） | 2026-04-30 | API 总上下文 1,050,000（输入上限 922,000）/ 128,000；Codex 默认 272K，最多可配到 872K | API：low–max，`none` 返回 400；Codex 另有 `ultra`，默认 low | 只有 Plus 及以上能用；Plus 每 5 小时约 5–45 条 | 2 章 2 节 |
| OpenAI | `gpt-6.1-sol`（2026-09-29） | 2026-04-30 | 同上 | API 默认 medium，不支持 `none` / `minimal`；Codex 默认 low | **Codex 当前默认模型**；Free 和 Go 档不含 | 2 章 |
| OpenAI | `gpt-6-sol`（2026-09-22） | 2026-04-20 | 同上 | none–max，默认 medium | Chat 付费档的 GPT-6 由它驱动 | 2 章 |
| OpenAI | `gpt-6-luna`（2026-09-22） | 2026-05-18 | 同上 | none–max，默认 medium；Codex 最高到 max，没有 ultra | Free / Go 也能用（桌面端） | 2 章 |
| OpenAI | `gpt-5.6-sol` / `-terra` / `-luna` | 2026-02-16 | 同上 | none–max，默认 medium（Codex 中 Sol 默认 low） | GPT-5.6 Luna 是 Luna Reserve 的兜底模型 | 2 章 |
| OpenAI | `gpt-5.5` | 2025-12-01 | API 1,050,000 / 128,000；Codex 272K | none–xhigh，默认 medium | **2026-10-14 退出 ChatGPT / Work / Codex** | 2 章 |
| OpenAI | `gpt-5.4`、`gpt-5.3-codex`、`gpt-5.2` | 2025-08-31 | 5.4 为 1.05M；5.3-codex 和 5.2 为 400K | — | ChatGPT 登录的 Codex 已不可用或已弃用 | 2 章 |
| Google | `gemini-3.8-flash`（GA，2026-09-02） | 2026-03（官方称部分领域仍停在 2025-01） | 1,048,576 / 65,536 | `thinking_level` low / **medium（默认）** / high；`minimal` 报错；不能关闭思考 | `gemini-3.7-flash` 被自动路由到它 | 3 章 2 节 |
| Google | `gemini-3.6-flash` | 2026-03 | 同上 | minimal / low / **medium** / high | `gemini-3.5-flash` 被自动路由到它；temperature 被忽略 | 3 章 |
| Google | `gemini-3.5-flash-lite` | 2026-03 | 同上 | **minimal（默认）** / low / medium / high | Gemini App 免费档主力；订阅用户用完额度也回落到它 | 3 章 |
| Google | `gemini-3.1-pro-preview` | 2025-01 | 同上 | low / medium / **high（默认）**；不能关闭，不支持 minimal | `gemini-3-pro-preview` 被自动路由到它；API 无免费层 | 3 章 |
| Google | `gemini-2.5-pro` / `-flash` | 2025-01 | 同上 | `thinkingBudget`：Pro 128–32768 且不能关；Flash 0–24576，0 为关闭 | 2026-09-18 起只对用过的老账号开放 | 3 章 |
| xAI | `grok-4.7` | 2026-05 | 500,000 / 无输出上限 | `reasoning.effort` low / medium / **high（默认）** / xhigh；不能关闭 | 传 `stop`、presence / frequency penalty 会报错 | 4 章 7 节 |
| xAI | `grok-4.6`、`grok-4.5` | 未找到 | 500,000 | 同上（4.5 是否支持 xhigh，文档自相矛盾） | — | 4 章 |
| xAI | `grok-4.3` | 未找到 | 1,000,000 | none / **low（默认）** / medium / high / xhigh | 已退役旧 slug 的重定向目标 | 4 章 |
| xAI | `grok-build-0.1` | 未找到 | 256,000 | 会推理，文档未列档位 | `grok-code-fast-1` 被重定向到它 | 4 章 |
| Kimi | `k3`（Kimi Code）/ `kimi-k3`（开放平台） | 未找到 | 1,048,576；Kimi Code 的 Plus 档只给 262,144 | 始终思考；`reasoning_effort` low / high / max，Kimi Code 默认 high、开放平台默认 max | Kimi Code 中关闭思考后，改由 K2.8 Preview 无思考版作答；Plus 起可用 | 5 章 1 节 |
| Kimi | `kimi-for-coding`（2026-09-11 起指向 K2.8 Preview） | 未找到 | 1,048,576 | 默认 max | 所有付费档 | 5 章 |
| Kimi | `kimi-for-coding-highspeed`（K2.7 Code HighSpeed） | 未找到 | 262,144 | 强制思考 | Pro 起；ID 写错会**静默**回落到标准版 | 5 章 |
| 智谱 | `glm-5.3`、`glm-5.3-flash` | 未找到 | 1M / 128K | 始终思考；`reasoning_effort` low / high / max，默认 max；标准 API 关思考会报错，Coding 端点会改成 low 继续 | 套餐内只有这两个模型；Claude Code 默认把 Opus / Sonnet / Haiku 都映射到 Flash | 5 章 2 节 |
| MiniMax | `MiniMax-M3.1-Flash-Preview` | 未找到 | 1,000,000 / 524,288 | 思考关不掉，`disabled` 返回 400（错误码 2013）；effort low–max，默认 max | M Plan 全档唯一的文本模型 | 5 章 3 节 |
| MiniMax | `MiniMax-M3` | **2026-01**（写在官方 chat template 里） | 1,000,000 / 524,288 | Anthropic 兼容接口省略 thinking 时默认**关闭** | 裸调时自述 "Your model version is MiniMax-M3 … Knowledge cutoff: January 2026" | 5 章 |
| MiniMax | `MiniMax-M2.7` | 未找到 | 204,800 / 204,800 | 思考关不掉（传 `disabled` 被忽略） | 输入超过 204,800 必失败，可借此区分 M3 与 M2.7 | 5 章 |

### 0.2 订阅档位速查

| 厂商 | 档位与美国月价 | 对出题最有用的档位差异 | 详见 |
|---|---|---|---|
| Anthropic | Free $0；Pro $20；Max 5x $100；Max 20x $200；Team Standard $25（年付 $20）；Team Premium $125（年付 $100）；Enterprise 每席 $20，用量另按 API 价 | Free 没有 Claude Code、Opus 和 Fable；Pro 和 Team Standard 用 Fable 只能走 usage credits；Max 有单独的 Fable 周限额，Max 和 Team Premium 的 Fable 用量最多占周额度的 50%；Team Standard 每会话额度为 Pro 的 1.25 倍，Premium 为 6.25 倍；网页端和 Claude Code 共用额度。聊天上下文按模型不同：Fable 5、Opus 4.6–4.8、Sonnet 4.6 为 500K，其余 5.x 为 1M；Claude Code 全部 1M，例外是 Opus 4.6 和 Sonnet 4.6 需要 `[1m]` 后缀并受 usage credits 门槛限制 | 1 章 1 节 |
| OpenAI | Free $0；Go $8；Plus $20；Pro $100 / $200 / $500；Business Standard $25（年付 $20）；Business Premium $125（年付 $100）；Enterprise / Edu 联系销售 | Chat 快速模式上下文：Free 27K，Go / Plus / Business 54K，Pro / Enterprise 128K；推理模式 256K，Pro 为 400K。Codex 中各档用的是同一组模型，只差额度：Plus 每 5 小时 Astra 约 5–45 条；Pro 三档没有 5 小时限额；Ultrafast 只给 Pro $500 和合格的 Enterprise / Edu；Free / Go 在 Codex 中只能用 Luna | 2 章 1 节 |
| Google | Free $0；AI Plus $4.99；AI Pro $19.99；AI Ultra $99.99（5x）/ $199.99（20x） | Gemini App 上下文：Free 32K，Plus 128K，Pro 和 Ultra 1M。2026-05-17 起改为按算力计量，每 5 小时刷新，另有周上限。Deep Think 仅 Ultra；Plus 将失去 Pro 模型。Antigravity 中 Claude 5.5 只给 Pro（非试用）、Ultra 和企业版；个人账号的 Gemini CLI 登录已于 2026-06-18 停服 | 3 章 1 节 |
| xAI | Free；SuperGrok Lite（官方未标价）；SuperGrok $30；SuperGrok Plus $100；SuperGrok Heavy（官方未标价）；X Premium $8；Premium+ $40 | 所有档位都能用 Grok 4.7 和 Grok Build；自 2026-06 起所有产品共用一个每周用量池；**官方允许 OpenCode 等第三方工具用 SuperGrok 订阅 OAuth 登录** | 4 章 6 节 |
| Kimi | Go ¥49（不含 Kimi Code）；Plus ¥99 / $19；Pro ¥199 / $39；Max ¥699 / $99；Ultra $199（只在国际站） | `k3` 从 Plus 起可用；`k3` 的 1M 上下文和高速版从 Pro 起；新套餐取消周额度，只有 5 小时窗口和月度总额；权限不足时返回 401，报错文案固定 | 5 章 1 节 |
| 智谱 | Lite ¥118 / $18；Pro ¥538 / $80；Max ¥1078 / $168（2026-07-30 改为积分制） | 三档模型完全相同，只差额度：每 5 小时 2,000 / 12,000 / 28,000 积分，每周 10,000 / 60,000 / 140,000；并发 Max > Pro > Lite；只能在指定工具里用 | 5 章 2 节 |
| MiniMax | M Plan：Go ¥49 / $22；Explore ¥119 / $55；Build ¥469 / $132（Token Plan 已停售） | 三档文本模型相同；Go 不含视频；Build 的额度在国内站是 Go 的 12 倍，国际站是 7.5 倍 | 5 章 3 节 |
| OpenCode Go | Go $10；Go Plus $40 | 每个模型单独设月度美元额度（例如 Kimi K3 在 Go 为 $15、Go Plus 为 $60）；推理商不公开，可能多家轮换 | 5 章 4 节 |

### 0.3 经 sub2api 转发时各通道的典型自我介绍

> 本表记录网关/客户端身份污染的机制，**不提供可计分的身份金标**。所有通道（包括无 system 的 API Key 与 MiniMax 裸调）的自述均不用于认证、门槛或扣价；保留典型文本只为解释假异常。版本判据使用可取得的真实响应元数据、计数与确定参数行为，不能把注入的名字当作上游证明。

| 通道 | 网关或客户端注入的身份 | 预期自述 | 身份题能否计分 |
|---|---|---|---|
| Claude OAuth / Setup Token，买家不是 Claude Code 客户端 | sub2api 默认注入 `You are Claude Code, Anthropic's official CLI for Claude.`，买家原 system 被挪进第一条 user 消息 | "我是 Claude Code / Anthropic 的 Claude" | 不能计分，自述不能确认厂商或版本 |
| 真 Claude Code 客户端 | 环境段写有 "You are powered by the model named … The exact model ID is … Assistant knowledge cutoff is …"（社区观察） | 照抄环境段 | 不能计分，身份题和截止题都会被污染 |
| Codex OAuth，instructions 为空 | sub2api 注入官方 Codex 提示：Astra / 6.1 Sol 为 "You are Codex, an agent based on GPT-6"；**6 Sol、6 Luna、5.6 系回退为 GPT-5.5 的提示 "based on GPT-5"** | "Codex，基于 GPT-6 / GPT-5" | 分不出 GPT-6 的各个型号；"based on GPT-5" 可能是网关造成的 |
| Antigravity | sub2api 默认注入 "You are Antigravity …"，并对 Claude 模型追加 "You are Model <显示名>, ModelId is <ID>." | 照抄网关写入的模型名 | 不能计分 |
| Gemini CLI 官方客户端 | "You are Gemini CLI, an interactive CLI agent …"（Gemini 3）；sub2api 不注入 | "Gemini CLI" | 不能计分 |
| Grok Build | "You are Grok released by xAI. You are an interactive CLI tool …" | "Grok，由 xAI / SpaceXAI 开发"（公司已并入 SpaceX，保留两种典型说法） | 不能计分 |
| Kimi Code / OpenCode 客户端 | "You are ${product_name}, an interactive general AI agent …" / "You are OpenCode …" | Kimi Code / OpenCode；K3 有自称 Claude 的社区报告 | 不能计分 |
| GLM Coding Plan，在 Claude Code 中使用 | Claude Code 的系统提示 | 倾向自称 Claude Code（推断）；智谱没有官方身份文本 | 不能计分 |
| MiniMax M3 裸调 | 官方 chat template 默认 "Your model version is MiniMax-M3 … Knowledge cutoff: January 2026" | MiniMax-M3，截止 2026-01 | 不能计分，chat template 可决定自述 |
| API Key 请求，无系统提示（厂商机制资料） | 无 | 各厂商都没有官方资料 | 不能计分；token-market 不直连厂商 |

## 第 1 章 Anthropic Claude

> 调研日期：2026-10-11。下文所有来源的访问日期都是 **2026-10-11**。文中 `[A*]/[B*]/[C*]/[D*]` 为官方或 Anthropic 自有资料，`[E*]` 为社区或媒体资料。来源 URL 见文末“附：来源索引”。
> 抓取方式：platform.claude.com 的 WebFetch 被拦截，因此用 `curl -x 127.0.0.1:7890` 拉取文档站的 `.md` 原文后用 grep 逐字核对。support.claude.com 和 claude.com 页面拉取 HTML 后转成纯文本核对。
> 核实结论：Claude 5 家族的真实命名确认为 **Fable 5.1 / Opus 5.5 / Sonnet 5.5 / Haiku 5.5**，模型 ID 分别是 `claude-fable-5-1` / `claude-opus-5-5` / `claude-sonnet-5-5` / `claude-haiku-5-5`[A1]。Haiku 5.5 于 2026-10-07 发布，是真实存在的模型[A4]。Fable 是位于 Opus 之上的 Mythos 级别，与 Claude Mythos 5.1 是同一底模，Fable 5.1 额外加了双用途安全措施[A5][A31]。

---

### 1. 订阅档位对比

#### 1.1 主表

| 档位 | 月价（美国） | 可用模型 | 用量规则 | 上下文（聊天 / Claude Code） | Claude Code | 来源 |
|---|---|---|---|---|---|---|
| **Free** | $0 | Sonnet、Haiku 可用；Opus、Fable 不可用 | 5 小时会话限额，“可发送的消息数随需求变化”，Anthropic 还可能施加其他限制；没有 usage credits | 定价页写“Up to 1M（varies by model）”；各模型的具体数值**未找到官方资料**（上下文文章只覆盖付费档） | **不可用** | [B1][B20] |
| **Pro** | 月付 $20；年付折合 $17/月（一次性 $200） | Opus、Sonnet、Haiku 计入套餐；**Fable 5 / 5.1 不计入套餐额度**，只能用 usage credits 按 API 价付费（Fable 5 的“含在 Pro 内”促销已于 2026-07-19 23:59:59 PT 结束；Fable 5.1 从未包含） | 5 小时会话限额（定价页称至少是 Free 的 5 倍）；另有**跨全模型的周限额**，按账户固定时间重置；Anthropic “可酌情施加周/月上限或模型、功能限制”；可开 usage credits 按 API 价继续使用 | 聊天：见 1.2 表。Claude Code：除 Opus 4.6 / Sonnet 4.6 外均为 1M；`claude-opus-4-6[1m]`、`claude-sonnet-4-6[1m]` 在 Pro 上需要开启 usage credits | 可用，与网页端**共用同一额度池**；`default` 解析为 Opus 5.5（v2.1.280 起；更早版本 Pro 默认是 Sonnet 5） | [B1][B2][B9][B8][C1] |
| **Max 5x** | $100（仅月付） | 全部模型。Fable 5 / 5.1 计入套餐，但**最多用每周额度的 50%**，并有**单独的 Fable 周限额** | 每会话额度为 Pro 的 5 倍；全模型周限额加 Fable 周限额；可开 usage credits；2026-10-07 起每月附送 **$100 API credits**（只用于 Claude Platform、API、Agent SDK、Managed Agents，**不能用于交互式 Claude Code 或 usage credits**） | 同 Pro；Claude Code 中 Opus 4.6[1m] 已含在订阅内，Sonnet 4.6[1m] 仍需 usage credits | 可用，共享额度，默认 Opus 5.5 | [B1][B3][B6][B9][B11][C1] |
| **Max 20x** | $200（仅月付） | 同 Max 5x（Fable 同样最多 50% 周额度） | 每会话额度为 Pro 的 20 倍；每月附送 **$200 API credits**；其余同 5x | 同 Max 5x | 同上 | [B1][B3][B11] |
| **Team Standard 席位** | 月付 $25/人；年付 $20/人；每队 2–150 席 | “所有可用模型”；Fable 5 / 5.1 只能用 usage credits | 每会话额度为 Pro 的 **1.25 倍**，加全模型周限额，**按成员单独计算**；组织可开 usage credits；每席 $20 API credits，全队合并后**每月上限 $500** | Team 文章写“200k context window”，但上下文专文写最高 1M（见 1.2），**二者冲突** | 每个席位都含 Claude Code；默认 Opus 5.5（v2.1.280 前 Team Standard 默认是 Sonnet 5） | [B1][B4][B9][B11][C1] |
| **Team Premium 席位** | 月付 $125/人；年付 $100/人 | 全部模型；Fable 计入套餐，最多 50% 周额度 | 每会话额度为 Pro 的 **6.25 倍**（即 Standard 的 5 倍）；每席 $100 API credits（全队合并 ≤ $500/月） | 同上；Claude Code 中 Opus 4.6[1m] 已含在订阅内 | 同上 | [B1][B4][B9][C1] |
| **Enterprise（现行，按用量计费）** | 席位费 $20/人/月，年付；**所有用量另按 API 标准价计费**；自助版最少 20 席，销售版最少 50 席 | 全部模型，Fable 也按 API 价计费；管理员可按角色限制模型和 effort 上限 | **没有套餐或席位级用量上限**；管理员可设组织级和用户级消费上限 | 同付费档（见 1.2） | 席位含 Claude Code；默认 Opus 5.5，组织可设默认模型（需 v2.1.196+） | [B1][B5][B9][C1] |
| **Enterprise（旧版按席位计费：Standard / Premium）** | 合同价 | Premium 席位：Fable 最多 50% 周额度；Standard 席位：组织开启 usage credits 后才能用 Fable | 与 Team 类似，有会话和周限额（文中未给倍数）。旧的 Chat / Chat+Claude Code 席位将在续约后**不再保留** | — | Premium 席位或 Chat+Claude Code 席位才含 Claude Code | [B5][B9][B13] |

**跨档位通用规则**（均为官方说法）：
- 网页、桌面、移动端、Claude Code（包括 IDE 插件）共用同一个额度池[B1][B7][B12]。
- 会话限额按“滚动的 5 小时窗口”重置；付费档另有周限额[B1][B2][B3]。
- 定价页 FAQ 原文：“Pro gives you at least 5x more usage per 5-hour session than Free. Max gives you 5x or 20x more usage per 5-hour session than Pro. On Team plans, Standard seats give more than Pro and Premium seats give 5x more than Standard.”[B1]
- 2026-09-22 Opus 5.5 发布时，官方提高了 Pro、Max、Team 和按席位 Enterprise 的 5 小时限额，并发放一次可自选时间使用的 rate limit reset[A41]。Limit reset 可把 5 小时或周限额一次性重置为满[B21]。
- Claude Code 错误参考里存在按模型族的限额提示，“You've hit your Opus limit”和“You've hit your Sonnet limit”，只作用于对应模型族，切换到别的模型族可以继续用[C4][C5]。support 文章**没有写**哪些档位有这类限额、额度多大。
- **Usage credits（旧称 extra usage）**：Claude Code v2.1.144（2026-05-19）把界面里的 “extra usage” 改名为 “usage credits”[C8]。触发 5 小时、周或 Fable 周限额后，可切换为按 **API 标准价**的预付费计费，支持每月消费上限和自动充值[B6]。
- **Fast mode**（Opus 5.5 / 5 / 4.8）：订阅用户只能用 usage credits 支付，不计入套餐额度[C6]。

#### 1.2 付费档各模型上下文窗口（官方逐模型表）

| 模型 | 聊天（claude.ai） | Claude Code | Cowork |
|---|---|---|---|
| Fable 5.1 | 1M | 1M | 1M |
| Fable 5 | **500K** | 1M | 1M |
| Opus 5.5 | 1M | 1M | 1M |
| Opus 5 | 1M | 1M | 1M |
| Opus 4.8 / 4.7 | **500K** | 1M | 1M |
| Opus 4.6 | **500K** | 1M，需选 `claude-opus-4-6[1m]`；Pro 需开 usage credits | **200K** |
| Sonnet 5.5 | 1M | 1M | 1M |
| Sonnet 5 | 1M | 1M | 1M（到 500K 自动压缩） |
| Sonnet 4.6 | **500K** | 1M，需选 `claude-sonnet-4-6[1m]`；**所有订阅档**都需 usage credits（按用量计费的 Enterprise 除外） | **200K** |
| Haiku 5.5 | 1M | 1M | **500K** |
| 其他（如 Haiku 4.5） | 200K | — | 200K |

来源[B8]。Claude Code 文档的方案对照：Opus 4.6 的 1M 在 Max、Team、Enterprise 上“Included with subscription”，在 Pro 上需要 usage credits；Sonnet 4.6 的 1M 在所有订阅档都需要 usage credits[C1]。

---

### 2. 模型参数

价格单位均为 $/MTok。“缓存读”指缓存命中价。Batch 一律 5 折。

| 模型名 | API 模型 ID / 别名 | 上下文窗口 | 最大输出 | reliable knowledge cutoff | training data cutoff | thinking / effort 支持与默认值 | 价格（输入 / 输出；5m 写 / 1h 写 / 读） | 发布 · 状态 | 来源 |
|---|---|---|---|---|---|---|---|---|---|
| **Claude Fable 5.1** | `claude-fable-5-1`（别名同 ID；Bedrock 为 `anthropic.claude-fable-5-1`） | 1M（默认即最大，无需 beta 头） | 128K | Jun 2026 | Jun 2026 | Adaptive 常开，`disabled` 和 `budget_tokens` 都返回 400；effort 支持 low/medium/high/xhigh/max，**默认 high** | 10 / 50；12.50 / 20 / **0.25** | 2026-09-01 · Active，最早 2027-09-01 退役 | [A1][A5][A17][A20] |
| Claude Mythos 5.1（需验证准入） | `claude-mythos-5-1` | 1M | 128K | Jun 2026 | Jun 2026 | 同 Fable 5.1 | 同 Fable 5.1 | 2026-09-01 · 需经验证项目准入 | [A6][A17] |
| **Claude Opus 5.5** | `claude-opus-5-5` | 1M | 128K（Batch 用 `output-300k-2026-03-24` 可到 300K） | Jun 2026 | Jun 2026 | Adaptive 常开，`disabled` 和 `budget_tokens` 在任何 effort 下都返回 400；effort 5 档，**默认 medium** | 4 / 20；5 / 8 / **0.20**；fast mode 8 / 40 | 2026-09-22 · Active（latest） | [A2][A17][A19][A25] |
| **Claude Sonnet 5.5** | `claude-sonnet-5-5` | 1M | 128K（Batch 300K） | Jun 2026 | Jun 2026 | Adaptive 默认开；`disabled` 返回 400；可改用 `between_tools`（仅限 effort ≤ high）关掉前置思考；effort 5 档，**API 默认 high**，Claude Apps 和 Claude Code 默认 medium；非默认 sampling 参数返回 400 | 2 / 10；2.50 / 4 / **0.10** | 2026-09-28 · Active | [A3][A17][A26][A42][C1] |
| **Claude Haiku 5.5** | `claude-haiku-5-5` | 1M（提示超过 10 万 token 按高价计费） | 128K（Batch 300K） | Jun 2026 | Jun 2026 | Adaptive 默认开；effort ≤ high 时可以 `disabled`；effort 5 档，**默认 medium** | 提示 ≤ 100K：0.10 / 0.50，读 0.01；提示 > 100K：0.50 / 2.50，读 0.05 | 2026-10-07 · Active | [A4][A17][A27] |
| Claude Fable 5 | `claude-fable-5` | 1M | 128K | Jan 2026 | Jan 2026 | Adaptive 常开；effort 5 档，默认 high | 10 / 50；读 1 | 2026-06-09（6/12–7/1 曾因出口管制暂停访问）· Legacy | [A7][A17][A31] |
| Claude Opus 5 | `claude-opus-5` | 1M | 128K（Batch 300K） | **May 2026** | May 2026 | Adaptive 默认开；effort ≤ high 时可以 `disabled`，xhigh/max 下返回 400；effort 5 档，默认 high | 5 / 25；6.25 / 10 / 0.50；fast mode 10 / 50 | 2026-07-24 · Legacy | [A8][A17][A19] |
| Claude Sonnet 5 | `claude-sonnet-5` | 1M | 128K（Batch 300K） | Jan 2026 | Jan 2026 | Adaptive 默认开，可 `disabled`；effort 5 档，默认 high；非默认 sampling 参数返回 400 | 2 / 10（原为首发优惠价，现已转为标准价）；2.50 / 4 / 0.20 | 2026-06-30 · Legacy | [A9][A17][A24] |
| Claude Opus 4.8 | `claude-opus-4-8` | 1M | 128K（Batch 300K） | Jan 2026 | Jan 2026 | 不传 thinking 时**不思考**，需显式设 `adaptive`；effort 5 档，默认 high | 5 / 25；读 0.50；fast mode 10 / 50 | 2026-05-28 · Legacy | [A10][A17][A20] |
| Claude Opus 4.7 | `claude-opus-4-7` | 1M | 128K（Batch 300K） | Jan 2026 | Jan 2026 | 同 4.8；API 默认 high，**Claude Code 默认 xhigh**；fast mode 已移除 | 5 / 25 | 2026-04-16 · Legacy | [A11][A19][C1][C6] |
| Claude Opus 4.6 | `claude-opus-4-6` | 1M（2026-03-13 起 GA，按标准价） | 128K（Batch 300K） | May 2025 | Aug 2025 | 支持 Adaptive；`budget_tokens` 已弃用但仍可用；effort 支持 low/medium/high/max（**无 xhigh**），默认 high | 5 / 25 | 2026-02-05 · Legacy | [A12][A19][A24] |
| Claude Sonnet 4.6 | `claude-sonnet-4-6` | 1M（2026-03-13 起 GA） | 128K（Batch 300K） | **Aug 2025**（模型页）；透明度中心写 May 2025，**冲突** | Jan 2026 | 同 Opus 4.6 | 3 / 15；3.75 / 6 / 0.30 | 2026-02-17 · Legacy | [A14][A37] |
| Claude Opus 4.5 | `claude-opus-4-5-20251101`（别名 `claude-opus-4-5`） | 200K | 64K | May 2025 | Aug 2025 | 只支持手动 extended thinking（`budget_tokens` ≥ 1024）；effort 支持 low/medium/high，默认 high；interleaved 需加 beta 头 | 5 / 25 | 2025-11-24 · Legacy，最早 2026-11-24 退役 | [A13][A19][A21] |
| Claude Sonnet 4.5（已弃用） | `claude-sonnet-4-5-20250929`（别名 `claude-sonnet-4-5`） | 200K（1M beta 已于 2026-04-30 取消） | 64K | Jan 2025 | Jul 2025 | 只支持 extended thinking；不支持 effort | 3 / 15 | 2025-09-29 · 2026-09-30 宣布弃用，**2026-11-30 退役** | [A15][A18][A24] |
| Claude Haiku 4.5 | `claude-haiku-4-5-20251001`（别名 `claude-haiku-4-5`） | 200K | 64K | Feb 2025（模型页和透明度中心）；claude.ai 系统提示写“end of January 2025”，**冲突** | Jul 2025 | 只支持 extended thinking；不支持 effort；不支持 interleaved | 1 / 5；1.25 / 2 / 0.10 | 2025-10-15 · Legacy，最早 2026-10-15 退役 | [A16][A33b] |

补充说明：
- **已退役模型**：Opus 4.1（2026-08-05）、Opus 4 和 Sonnet 4（2026-06-15）、Sonnet 3.7 和 Haiku 3.5（2026-02-19）、Haiku 3（2026-04-20）、Opus 3（2026-01-05）。Mythos Preview 于 2026-06-09 宣布弃用，退役日期待定[A18]。
- **ID 规则**：从 4.6 代起，无日期 ID 本身就是固定快照，别名一栏只是重复 ID。4.6 之前的别名指向带日期的 ID[A1]。
- **Tokenizer**：Claude 4.7 及之后的模型（含 Haiku 5.5、Sonnet 5 / 5.5、Fable）使用新 tokenizer，同样文本大约多 30% token。1M token 在新 tokenizer 下约等于 55.5 万英文词，旧 tokenizer 约 75 万词[A1][A17][A27]。
- **数据驻留**：4.6 及以后模型指定 US-only（`inference_geo`）时价格 ×1.1[A17]。
- 官方“当前在售”的最新四款是 Fable 5.1、Opus 5.5、Sonnet 5.5、Haiku 5.5，官方推荐默认用 Opus 5.5[A1]。

---

### 3. 推理与缓存参数细节

#### 3.1 effort（`output_config.effort`，已 GA，无需 beta 头）[A19]

| 项目 | 内容 |
|---|---|
| 取值 | `low` / `medium` / `high` / `xhigh` / `max` |
| 支持 `max` 的模型 | Fable 5.1、Mythos 5.1、Fable 5、Mythos 5、Mythos Preview、Opus 5.5、Opus 5、Opus 4.8、Opus 4.7、Opus 4.6、Sonnet 5.5、Sonnet 5、Sonnet 4.6、Haiku 5.5 |
| 支持 `xhigh` 的模型 | 上表去掉 Mythos Preview、Opus 4.6、Sonnet 4.6。官方原文：“Not every model that supports `max` supports `xhigh`.” |
| Opus 4.5 | 只有 low/medium/high，与 `budget_tokens` 配合使用 |
| 不支持 effort | Sonnet 4.5、Haiku 4.5（模型页写“Not supported”） |
| API 默认值 | 所有支持 effort 的模型都默认 **high**，只有 **Opus 5.5 和 Haiku 5.5 默认 medium**。原文：“Setting `effort` to the model's default … produces exactly the same behavior as omitting the `effort` parameter entirely.” |
| Claude Code 默认值 | 默认 high，例外是 **Opus 5.5 / Sonnet 5.5 / Haiku 5.5 为 medium，Opus 4.7 为 xhigh**[C1]。设了模型不支持的档位时，降到“不高于设定值的最高支持档”，例如 xhigh 在 Opus 4.6 上按 high 跑。`max` 只对当前会话生效，除非通过环境变量设置。在提示里写 `ultrathink` 只追加一段上下文指令，**不改变**发给 API 的 effort |
| 管控 | Enterprise 管理员可按角色、按模型设 effort 上限；任何档位都可用托管设置 `maxEffortLevel` 封顶。若输出格式为 json/stream-json，超限时会被**静默钳制**[C1] |
| 按消息调整 effort（beta） | `mid-conversation-output-config-2026-07-01`。支持 Fable 5.1、Mythos 5.1、Opus 5.5、Opus 5、Sonnet 5.5、Haiku 5.5（Claude API 和 Google Cloud）；中途改 effort 不会让缓存失效 |
| 档位标尺按模型校准 | 原文：“The effort scale is calibrated per model, so the same level name does not represent the same underlying value across models.”[C1]；Sonnet 5.5 的档位相对 Sonnet 5 做了重新校准[A26] |
| 历史 | Claude Code 在 2026-03-04 把默认 effort 从 high 改为 medium，2026-04-07 撤回（影响 Sonnet 4.6 和 Opus 4.6）[A39] |

#### 3.2 thinking（extended / adaptive / interleaved）[A20][A21]

| 模型 | 不传 `thinking` | `adaptive` | `enabled` + `budget_tokens` | `between_tools` | `disabled` |
|---|---|---|---|---|---|
| Opus 5.5 | Adaptive | Adaptive | 400 | 400 | **400** |
| Sonnet 5.5 | Adaptive | Adaptive | 400 | effort ≤ high 时关掉前置思考 | **400** |
| Haiku 5.5 | Adaptive | Adaptive | 400 | 400 | effort ≤ high 时关闭思考 |
| Fable 5.1 / Mythos 5.1 / Fable 5 / Mythos 5 | Adaptive | Adaptive | 400 | 400 | 400 |
| Opus 5 | Adaptive | Adaptive | 400 | 400 | effort ≤ high 时关闭思考 |
| Sonnet 5 | Adaptive | Adaptive | 400 | 400 | 关闭思考 |
| Opus 4.8 / 4.7 | **不思考** | Adaptive | 400 | 400 | 关闭思考 |
| Opus 4.6 / Sonnet 4.6 | 不思考 | Adaptive | Extended（已弃用） | 400 | 关闭思考 |
| Opus 4.5 / Sonnet 4.5 / Haiku 4.5 | 不思考 | **400** | Extended | 400 | 关闭思考 |

- **display 默认值**：Fable 5.1 / 5、Mythos、Opus 5.5 / 5 / 4.8 / 4.7、Sonnet 5.5 / 5、Haiku 5.5 默认 `omitted`，thinking 块正文为空字符串，只带 signature。Opus 4.6、Sonnet 4.6 及更早模型默认 `summarized`。`display: "updates"` 需要 beta 头 `thinking-display-updates-2026-08-18`。官方说明：任何模型都不会返回原始思维链[A20]。
- **interleaved**：使用 adaptive 时自动开启。Opus 4.5、Sonnet 4.5 及更早的 Claude 4 模型需要 beta 头 `interleaved-thinking-2025-05-14`。Haiku 4.5 不支持，在 API 上该头会被接受但不起作用[A21][A23]。
- **budget_tokens**：最小 1,024，且必须小于 `max_tokens`（interleaved 时例外）[A21]。
- **sampling 参数**：在 Claude 4.7 及之后的模型和 Mythos Preview 上，`temperature`、`top_p`、`top_k` 设为非默认值会返回 400[A18]。
- **强制工具调用**：Fable 5.1、Mythos 5.1、Opus 5.5、Sonnet 5.5 上 `tool_choice` 设为 `any` 或 `tool` 都返回 400[A25][A26][A28]。4.6 及之后的模型都不支持 assistant prefill（返回 400）[D1]。
- **thinking 块与账号、模型绑定**（多账号池需注意）：
  - Sonnet 5.5 和 Haiku 5.5 的 thinking 块**只在产生它的账号或关联账号内有效**。换账号发送时，API **静默丢弃**这些块，请求本身成功。加上 beta 头 `thinking-binding-controls-2026-08-01` 后，丢弃会出现在 `input_transformations` 中，reason 为 `organization_binding_mismatch`[A26][A27]。
  - Opus 5.5 和 Fable 5.1 会检查 thinking 块之前的 system、tools 或历史消息是否被改动。对 2026-08-31 00:00 UTC 及之后创建的账号，改动后回放 thinking 块直接返回 400[A25]。
  - Opus 5.5 能读取 Opus 5、更早的 Opus/Sonnet/Haiku，以及（在 API 和 Google Cloud 上）Sonnet 5.5、Haiku 5.5 的 thinking 块，**读不了 Fable 和 Mythos 的**[A25]。

#### 3.3 prompt caching[A22][A17]

| 项目 | 内容 |
|---|---|
| TTL | 默认 5 分钟；`cache_control: {"type":"ephemeral","ttl":"1h"}` 为 1 小时。命中会刷新 TTL |
| 价格倍数（相对基础输入价） | 5 分钟写 1.25×；1 小时写 2×；读 0.1×。例外：Fable 5.1 和 Mythos 5.1 读为 0.025×，Opus 5.5 和 Sonnet 5.5 读为 0.05× |
| 断点 | 最多 4 个；每个断点向前回溯 20 个块；可在请求顶层用 `cache_control` 做自动缓存 |
| 最小可缓存长度：**512** | Fable 5.1、Mythos 5.1、Opus 5.5、Opus 5、Sonnet 5.5、Fable 5、Mythos 5、Haiku 5.5 |
| 最小可缓存长度：**1,024** | Opus 4.8、Sonnet 5、Sonnet 4.6、Sonnet 4.5，以及已退役的 Opus 4.1 / Opus 4 / Sonnet 4 |
| 最小可缓存长度：**2,048** | Opus 4.7、Mythos Preview（以及已退役的 Haiku 3.5） |
| 最小可缓存长度：**4,096** | Opus 4.6、Opus 4.5、Haiku 4.5 |
| 未达到最小长度时 | 不报错，按无缓存处理，`cache_creation_input_tokens` 和 `cache_read_input_tokens` 都为 0 |
| 隔离范围 | Claude API、Claude Platform on AWS、Foundry 按 **workspace** 隔离；Bedrock 和 Vertex 按组织隔离；不同组织之间从不共享 |

#### 3.4 1M 上下文与相关 beta 头（含历史）[A23][A24][C1]

- **现状**：所有 1M 模型默认就是 1M，**不需要 beta 头**，按标准价计费。唯一例外是 Haiku 5.5，提示超过 10 万 token 时适用更高价格。原文：“For every model with a 1M-token context window, 1M is the default: you don't need a beta header, and long-context requests are billed at standard pricing, except on Claude Haiku 5.5…”[A23]
- **1M 模型清单**：Fable 5.1 / 5、Mythos 5.1 / 5、Opus 5.5 / 5 / 4.8 / 4.7 / 4.6、Sonnet 5.5 / 5 / 4.6、Haiku 5.5、Mythos Preview。**其余模型（含 Sonnet 4.5）为 200K**[A23]。
- **历史**：
  - Sonnet 4 首先推出 1M beta（`context-1m-2025-08-07`）。
  - 2026-02-05：Opus 4.6 开放 1M beta，超过 200K 的部分按长上下文价计费。
  - **2026-03-13**：Opus 4.6 和 Sonnet 4.6 的 1M 转为 GA，按标准价，无需 beta 头；同时取消 1M 专属限流，单次可传图片和 PDF 页数提到 600。
  - **2026-04-30**：Sonnet 4.5 和 Sonnet 4 的 1M beta 下线，该头不再起作用，超过 200K 直接报错[A24]。
- **Claude Code**：Fable、Sonnet 5 及以后、Haiku 5.5、Opus 4.7 及以后原生 1M，不需要 `[1m]` 后缀；只有 Opus 4.6 和 Sonnet 4.6 需要 `[1m]`，并受 1.1 节所述的档位门槛限制。原生 1M 的会话约在 **967K** 时自动压缩。`CLAUDE_CODE_DISABLE_1M_CONTEXT=1` 会把会话限制在 200K[C1]。
- **上下文感知注入**：Sonnet 5、Sonnet 4.6、Sonnet 4.5、Haiku 4.5 会在系统提示里收到 `<budget:token_budget>…</budget:token_budget>`，工具调用后收到 `<system_warning>Token usage: …</system_warning>`。Opus 4.7 及之后的 Opus、Sonnet 5.5、Fable、Haiku 5.5 **不注入**这些标签[A23]。
- **超长行为**：输入本身超过窗口时一律返回 400 “prompt is too long”。4.5 及之后的模型如果在生成过程中触顶，以 `stop_reason: "model_context_window_exceeded"` 结束[A23]。

#### 3.5 官方“拒答后换模型”（server-side fallback）[A29]

- 开启方式：请求体加 `fallbacks: "default"`，同时带 beta 头 `server-side-fallback-2026-07-01`。旧版头 `2026-06-01` 只支持显式模型列表写法。
- 可识别的特征：
  - 响应顶层 `model` 字段是**实际作答的模型**。
  - `content` 里会插入 `{"type":"fallback","from":{"model":…},"to":{"model":…}}` 块。
  - `usage.iterations` 逐跳记录每次尝试，类型分为 `message` 和 `fallback_message`。
  - **sticky routing**：同一对话前缀约 1 小时内直接路由到回退模型，流式响应的 `message_start.model` 一开始就是回退模型。
- 拒答类别：`cyber`、`bio`、`frontier_llm`、`reasoning_extraction`、`general_harms`。其中 bio、frontier_llm、reasoning_extraction 即使在输出前就被拒，也照常计费（官方说明截至 2026 年 9 月）。
- Haiku 5.5 没有服务端回退。

---

### 4. 订阅 OAuth / Claude Code 渠道的特殊要求

#### 4.1 官方明文

| 主题 | 官方内容（逐字摘录） | 来源 |
|---|---|---|
| OAuth 用途 | “**OAuth authentication** is intended exclusively for purchasers of Claude Free, Pro, Max, Team, and Enterprise subscription plans and is designed to support ordinary use of Claude Code and other native Anthropic applications.” | [C2] |
| 第三方禁令 | “Anthropic does not permit third-party developers to offer Claude.ai login into their own applications, or to route requests through Free, Pro, or Max plan credentials on behalf of their users. Moreover, developers may not collect, store, or intermediate Claude.ai credentials or session tokens … Anthropic reserves the right to take measures to enforce these restrictions and may do so without prior notice.” | [C2] |
| 禁止转售 | “Customers may not pay for, resell, or intermediate Claude usage on their end users' behalf.” 另有：“Advertised usage limits for Pro and Max plans assume ordinary, individual usage of Claude Code and the Agent SDK.” | [C2] |
| 消费者条款（2025-10-08 生效） | “You may not share your Account login information, Anthropic API key, or Account credentials with anyone else. You also may not make your Account available to anyone else.” 禁止 “resell the Services”。另有：“Except when you are accessing our Services via an Anthropic API Key or where we otherwise explicitly permit it, to access the Services through automated or non-human means, whether through a bot, script, or otherwise.” | [A40] |
| 第三方应用与 Agent SDK 的现行口径 | 2026-06-15 更新：“We've paused the previously-announced changes to Claude Agent SDK usage… Claude Agent SDK, claude -p, and third-party app usage still draw from your subscription limits.” 2026-10-07 更新：“You can still use the Claude Agent SDK, claude -p, and third-party apps with your subscription limits.”（**与上面法律页的禁令存在张力，见第 8 节**） | [B10] |
| 必须透传 OAuth 能力头 | 原文：用 claude.ai 登录时，`anthropic-beta` 中带有 “an OAuth capability that the upstream requires, and stripping it fails those requests with `401`”。官方文档**没有写出具体头名** | [C3] |
| Claude Code 请求头 | `anthropic-version: 2023-06-01`、`anthropic-beta`（原文：“don't allowlist individual values”）、`x-claude-code-session-id`、`x-claude-code-agent-id`、`x-claude-code-parent-agent-id`。可选的提示头有 `x-claude-code-request-class` 等 | [C3] |
| 系统提示归因块 | 原文：“Claude Code prepends a short attribution block to the system prompt containing the client version and a fingerprint derived from the conversation.” 该块是 system 的首块时，`api.anthropic.com` 会把它剥离；可用 `CLAUDE_CODE_ATTRIBUTION_HEADER=0` 关闭。对 auto mode 的分类器请求，原文：“the block is the only marker in the request body that identifies them as Claude Code traffic”，并提到 v2.1.229 之前 “when the API declined the unidentified requests, auto mode failed” | [C3] |
| 服务端校验客户端版本 | 版本过低返回 400，错误码 `claude_code_version_too_old`，例：“Claude Code 2.1.219 does not support this model; version 2.1.255 or newer is required.” 官方说明这是服务端**按模型**检查的 | [C4] |
| 各模型的 Claude Code 最低版本 | Opus 5.5 需 v2.1.280+；Sonnet 5.5 需 v2.1.284+；Haiku 5.5 建议 v2.1.293+；Fable 5.1 需 v2.1.257+（Claude Code 文档），support 文章写 2.1.255+；Fable 5 需 2.1.170+ | [C1][B9] |
| Agent SDK 与 `-p` 的系统提示 | Agent SDK 不设 `systemPrompt` 时使用“minimal prompt”，不含 `claude_code` 预设的安全段落；`claude -p` 默认使用 Claude Code 系统提示。官方**不公开**提示原文 | [C7] |
| 订阅渠道的自动换模型 | Opus 5.5、Fable 5.1、Fable 5 遇到 bio 类拒答回退到 Opus 5，遇到 cyber 类回退到 Opus 4.8；Sonnet 5.5 的 cyber 回退到 Sonnet 5（bio 直接拒答）；Opus 5 的 cyber 回退到 Opus 4.8。回退后**会话继续使用回退模型**，effort 沿用原值。可用 `switchModelsOnFlag` 改为“每次询问”。Claude.ai 网页端同样默认开启自动换模型，并显示提示 | [C1][B16][B17] |
| OAuth Bearer 用法 | `ant` CLI 的 OAuth token 放在 `Authorization: Bearer` 中，并且 `/v1/messages` 必须带 `anthropic-beta: oauth-2025-04-20`（出自 Anthropic 随 Claude Code v2.1.296 分发的 claude-api skill，不是公开网页文档） | [D1] |
| 用量相关响应头 | 原文：Claude Code 会读取 `anthropic-ratelimit-unified-*`，“to show usage against plan limits”，并在 429 时区分是套餐上限还是临时限流。官方**没有列出具体头名** | [C3] |

#### 4.2 社区观察（**社区观察，未经官方确认**）

| 项目 | 观察内容 | 来源 |
|---|---|---|
| 系统提示首句 | 交互式 CLI：“You are Claude Code, Anthropic's official CLI for Claude.” 桌面端 Code 标签页：“You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.” headless / Agent SDK：“You are a Claude agent, built on Anthropic's Claude Agent SDK.”。环境段：“You are powered by the model named Opus 5.5 (1M context). The exact model ID is claude-opus-5-5[1m]. Assistant knowledge cutoff is June 2026.” | [E7]。本会话运行环境的系统提示开头也是 “…running within the Claude Agent SDK.”，并自报 Opus 5.5 / claude-opus-5-5 / June 2026，属**环境观察，非官方文档** |
| 请求头（v2.1.145，2026-05 抓包） | `anthropic-beta: claude-code-20250219,oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05`；`user-agent: claude-cli/2.1.145 (external, cli)` | [E1] |
| 请求头（v2.1.286，2026-10 抓包） | `POST /v1/messages?beta=true`；`Authorization: Bearer sk-ant-oat01-…`；`anthropic-beta: claude-code-20250219,oauth-2025-04-20,…`；`User-Agent: claude-cli/2.1.286 (external, sdk-cli, agent-sdk/0.3.284)`；`x-app: cli` | [E2] |
| 第三方仿冒做法 | pi / pigo 给 OAuth 请求补 `user-agent: claude-cli/<ver>`、`x-app: cli`、`anthropic-dangerous-direct-browser-access: true`，在 system 第一位放 “You are Claude Code…” 身份块，并把工具名大小写改成 Claude Code 规范；作者称“缺失时通常以‘该凭证只能用于 Claude Code’拒绝”（作者未用真实账号验证） | [E4] |
| 典型拒绝报文 | ① “This credential is only authorized for use with Claude Code and cannot be used for other API requests.”（2025-09-23 起在 Claude Code v1.0.120 内出现；约 2026-01-09 起大量出现在 OpenCode、Clawdbot、Roo Code 等第三方工具）[E9][E15]。② 400 invalid_request_error “Third-party apps now draw from your extra usage, not your plan limits. We've added a $200 credit…”（2026-04 起，有官方 CLI 被误判的报告）[E5]。③ “You're out of extra usage. Add more at claude.ai/settings/usage and keep going.”（Hermes 报告称带 tools 参数即触发）[E3]。④ `{"details":{"error_code":"claude_code_version_too_old"}}`，并有客户端版本被误报的案例[E6] | [E3][E5][E6][E9][E15] |
| 2026-04 政策收紧 | Boris Cherny（Claude Code 负责人）4/3 宣布：“Starting tomorrow at 12pm PT, Claude subscriptions will no longer cover usage on third-party tools like OpenClaw.” Anthropic 发言人对 The Register 说：“Using Claude subscriptions with third-party tools isn't permitted under our Terms of Service.” 作为补偿，发放一次性等额 extra usage 额度，并提供 extra usage 包 7 折 | [E8][E16] |
| 2026-05 / 06 | 媒体报道：5/13 宣布自 6/15 起 Agent SDK 和 `claude -p` 改为单独按月额度计费；6/15 当天暂停（暂停这一点有官方 support 文章佐证[B10]） | [E13] |
| `x-anthropic-billing-header` 三段式 system 改写等 sub2api 细节 | 由 sub2api 章节负责，本节不重复 | — |

---

### 5. 自我介绍

| 模型 | 典型自述 | 依据 | 官方 / 社区 |
|---|---|---|---|
| Opus 5.5（claude.ai） | “The currently selected version of Claude is Claude Opus 5.5.”；“Claude's reliable knowledge cutoff… is the end of Jun 2026.”；系统提示列出的最新公开模型为 Fable 5.1、Opus 5.5、Sonnet 5、Haiku 4.5（9/22 版本） | claude.ai 系统提示 2026-09-22 版 | 官方[A31] |
| Sonnet 5.5（claude.ai） | “This iteration of Claude is Claude Sonnet 5.5.”；截止“end of Jun 2026” | 2026-09-28 版 | 官方[A32] |
| Haiku 5.5（claude.ai） | “This iteration of Claude is Claude Haiku 5.5. Claude Haiku 5.5 is the fastest model for quick questions.”；截止“end of June 2026” | 2026-10-07 版 | 官方[A33] |
| Fable 5.1（claude.ai） | “This iteration of Claude is Claude Fable 5.1, the newest model in Anthropic's Claude 5 family and part of the Mythos-class model tier…”；截止“end of Jun 2026” | 2026-09-01 版 | 官方[A34] |
| Opus 5 / Sonnet 5 / Fable 5 / Opus 4.8 | Opus 5：“The currently selected version of Claude is Claude Opus 5.”，截止 May 2026。Sonnet 5：“This iteration of Claude is Claude Sonnet 5.”，截止 Jan 2026。Fable 5：“…the first model in Anthropic's new Claude 5 family…”，截止 Jan 2026。Opus 4.8：“…the newest Claude model…”，截止 Jan 2026 | 各模型系统提示页 | 官方[A35] |
| Opus 4.7 / 4.6、Sonnet 4.6 | Opus 4.7：“…Claude Opus 4.7 from the Claude 4.7 model family”，截止 end of January 2026。Opus 4.6：“…Claude Opus 4.6 from the **Claude 4.5** model family”（原文如此），截止 end of May 2025。Sonnet 4.6：“…from the Claude 4.6 model family”，截止 “beginning of August 2025” | 同上 | 官方[A35] |
| Haiku 4.5 | 2025-10-15 版开头为 “**The assistant is Claude, created by Anthropic.**”，随后 “The current date is {{currentDateTime}}.”，截止 “end of January 2025”。新一代（Claude 4.6 起）系统提示以 `<claude_behavior><product_information>` 开头，**不再使用**这句 | 同上 | 官方[A33b][A31] |
| 任意模型（API，无系统提示） | 官方原文：claude.ai 的系统提示 “do not apply to the Claude API”。模型在 API 上能否准确说出版本号，**官方没有资料**。新系统提示中特意加了一句：“The person can switch models mid-conversation, so earlier messages… that identify as a different model or report a different knowledge cutoff may still be accurate.” | [A30][A31] | 官方（只说明了机制） |
| 任意模型（API，无系统提示）的实际表现 | 较早有报告称加自定义系统提示后 Sonnet 4.5 自称 “Claude 3.5”，Cline 中各模型都自称 “Claude 3.5 Sonnet”。2026 年的新模型**没有检索到**类似报告 | [E14] | **社区观察** |
| 任意模型（Claude Code） | 自述由系统提示注入的环境段决定（“You are powered by the model named … The exact model ID is … Assistant knowledge cutoff is …”）。因此在 Claude Code 渠道，**身份题和截止日期题无法区分真实模型**：被替换的模型会照抄注入的文本 | [E7]、本会话环境 | **社区观察** |

---

### 6. 档位差异与降智 / 事故

#### 6.1 官方

| 时间 | 事件 | 要点 | 来源 |
|---|---|---|---|
| 2025-08-05 至 09-18 | 三个基础设施 bug | ① 上下文路由错误：Sonnet 4 的短请求被误路由到 1M 服务器，8/31 最严重的一小时影响 16% 请求，约 30% 的 Claude Code 用户至少有 1 条消息受影响，且路由是“sticky”的。② 输出损坏：英文回答中夹杂泰文或中文、代码出现语法错误，影响 Opus 4.1 / 4（8/25–28）和 Sonnet 4（8/25–9/2）。③ approximate top-k 在 XLA:TPU 上编译错误：影响 Haiku 3.5，疑似也影响 Opus 3 和 Sonnet 4。官方原文：“**We never reduce model quality due to demand, time of day, or server load.**” | [A38] |
| 2026-03-04 至 04-20 | Claude Code 质量问题（**API 未受影响**） | ① 3/4 把默认 effort 从 high 改为 medium，4/7 撤回（影响 Sonnet 4.6、Opus 4.6）。② 3/26 引入一个清理闲置会话 thinking 的 bug，结果每轮都在清，表现为健忘、重复、缓存 miss、额度消耗变快；4/10 在 v2.1.101 修复。③ 4/16 加入一条控制啰嗦程度的提示词，导致评测下降 3%，4/20 撤回（v2.1.116，影响 Sonnet 4.6、Opus 4.6、Opus 4.7）。**4/23 为所有订阅用户重置用量限额**。原文：“We never intentionally degrade our models.” | [A39] |
| 2026-06-12 至 07-01 | Fable 5 / Mythos 5 暂停访问 | 为遵守美国商务部出口管制而暂停，管制于 6/30 解除，7/1 恢复 | [A31][B19] |
| 2026-07-19 | Fable 5 促销结束 | Pro 和 Team Standard 从此只能通过 usage credits 使用 Fable | [B9] |
| 常态机制 | **官方的模型自动切换（并非故障）** | Opus 5.5 的 bio 和 frontier LLM 类请求回退到 Opus 5，cyber 类回退到 Opus 4.8；Fable 5 / 5.1 同理（回退到 Opus 5 或 Opus 4.8）；Sonnet 5.5 的 cyber 类回退到 Sonnet 5；读取外部内容（记忆、连接器、搜索结果、文件）也可能触发。网页端会显示提示并标注实际作答模型；Claude Code 中会话**之后都停留在回退模型上** | [B16][B17][C1] |
| 常态差异 | 同一模型在不同界面的上下文窗口不同 | 例如 Opus 4.8 在聊天中为 500K、在 Claude Code 中为 1M；Opus 4.6 在 Cowork 中只有 200K（见 1.2 表） | [B8] |
| 版本差异 | Claude Code 旧版本的 `default` 指向不同模型 | v2.1.280 之前，Pro 和 Team Standard 的 `default` 是 **Sonnet 5**；`opusplan` 在规划时用 Opus、执行时用 Sonnet | [C1] |
| 2026-09-22 / 10-07 | 额度利好 | Opus 5.5 发布时提高 5 小时限额并发放一次 reset；Max 和 Team 开始附送月度 API credits | [A41][B11] |

#### 6.2 社区与媒体（**社区观察，未经官方确认**）

| 时间 | 说法 | 来源 |
|---|---|---|
| 2026-03-26 起 | 工作日高峰（5am–11am PT）5 小时额度消耗更快。消息来自 Anthropic 员工 Thariq 的 X 帖子，称约 7% 用户会更早触顶、周总量不变。官方帮助中心**未见**相关说明 | [E10] |
| 2026-03 至 04 | Reddit r/ClaudeCode 出现大量“Opus 4.6 降智”帖，有第三方追踪称通过率从 56% 降到 50%。时间上与 [A39] 的 Claude Code 事故重合 | [E11] |
| 2026-01-26 至 28 | 有 Substack 文章称存在一次 harness 回归并获官方确认（**未能核实原始出处**） | [E11]（搜索摘要） |
| 2026-05-06 | 单一来源称 Claude Code 在 Pro 和 Max 上取消了高峰削减，并把 5 小时额度翻倍（**未核实**） | [E10]（usagemeter.app，搜索摘要） |
| 2026-09 至 10 | LessWrong 汇总的 Opus 5.5 用户反馈：“may be a bit lazier”，偶尔冒出一句不合逻辑的话，在复杂意图推断上相对吃力 | [E12] |
| 2025-09、2026-01、2026-04 | 第三方使用订阅 OAuth 被封堵的三波（见 4.2），包括官方 CLI 被误判为第三方应用 | [E5][E8][E9] |
| “Opus 被换成 Sonnet” | **未检索到 2026 年的官方事故说明**。能确认的“换模型”只有三种官方机制：安全分类器回退、`opusplan`、旧版 Claude Code 的 `default` 指向 Sonnet | — |

---

### 7. 对出题有用的要点

#### 7.1 最有效的一类：API 行为指纹（确定性强，不依赖模型自述）

| # | 探针 | 预期（官方行为） | 能区分什么 |
|---|---|---|---|
| 1 | 请求声称是 `claude-opus-5-5`，带 `temperature: 0.3` | Claude 4.7 及之后的模型返回 **400**；Opus 4.6、Sonnet 4.6、4.5 系列接受 | 冒充的 4.6 及更早模型 |
| 2 | `thinking: {"type":"disabled"}` | Opus 5.5、Sonnet 5.5、Fable 返回 400；Opus 5 和 Haiku 5.5 在 effort ≤ high 时接受、在 xhigh/max 时返回 400；Sonnet 5、Opus 4.8 / 4.7 / 4.6 接受 | 5.5 代与 5 / 4.x 代 |
| 3 | `thinking: {"type":"between_tools"}` | **只有 Sonnet 5.5**（effort ≤ high）接受 | Sonnet 5.5 是否真实 |
| 4 | `thinking: {"type":"enabled","budget_tokens":2048}` | 4.7 及之后和 5.x 返回 400；4.6 / 4.5 / Haiku 4.5 接受 | 冒充的旧模型 |
| 5 | `tool_choice: {"type":"any"}` | Opus 5.5、Sonnet 5.5、Fable 5.1 返回 400 | 5.5 代 |
| 6 | `max_tokens: 100000` | Opus 4.5、Sonnet 4.5、Haiku 4.5（上限 64K）报错；新模型接受（上限 128K） | 低档替换 |
| 7 | 输入约 300K token | 200K 窗口的模型返回“prompt is too long”；1M 模型正常 | 上下文缩水；1.2 表所示的界面和档位差异 |
| 8 | 带 `cache_control`、长约 800 / 1,500 / 3,000 token 的固定前缀，各发 2 次 | `cache_creation_input_tokens` 或 `cache_read_input_tokens` 是否大于 0 的组合可反推最小缓存长度：512（5.x）、1,024（Opus 4.8、Sonnet 5、Sonnet 4.6）、2,048（Opus 4.7）、4,096（Opus 4.6 / 4.5、Haiku 4.5） | 模型代际（前提：网关如实透传 usage） |
| 9 | 固定文本比较 `usage.input_tokens` | 新 tokenizer（4.7 及之后，含 Haiku 5.5）比旧 tokenizer（Sonnet 4.6 及更早、Haiku 4.5）多约 0–35% | Haiku 5.5 被换成 4.5；Opus 被换成 Sonnet 4.6 |
| 10 | 打开 thinking 但不设 display | 5.x、Opus 4.8 / 4.7 返回的 thinking 正文为空字符串；Opus 4.6 和 Sonnet 4.6 默认返回摘要 | 代际 |
| 11 | 难题、显式 `effort` 设为 low 和 max 各跑一次 | Opus 5.5 的 thinking 无法关闭，难题在 max 下 `output_tokens`（含 thinking）应显著更多；需要为每个模型分别采集基线（官方说明 effort 按模型校准） | effort 被网关剥离或降档；thinking 被裁 |
| 12 | 检查响应 `model` 字段、`fallback` 块和 `usage.iterations` | 官方回退时这些字段会如实标注 | 区分官方回退与卖家换模型 |

#### 7.2 知识截止题（只适合 API 或无注入的渠道）

- reliable cutoff 分层：
  - **Jun 2026**：Fable 5.1、Opus 5.5、Sonnet 5.5、Haiku 5.5
  - **May 2026**：Opus 5
  - **Jan 2026**：Fable 5、Sonnet 5、Opus 4.8、Opus 4.7
  - **Aug 2025**：Sonnet 4.6（训练数据截至 Jan 2026）
  - **May 2025**：Opus 4.6、Opus 4.5
  - **Feb 2025**：Haiku 4.5
  - **Jan 2025**：Sonnet 4.5
- 可用作截止期探针的官方时间线事件（2026 年上半年）：
  - 2/5 Opus 4.6 发布；2/17 Sonnet 4.6 发布
  - 4/9 Cowork 正式发布；4/16 Opus 4.7 发布；4/17 Claude Design 发布
  - 5/28 Opus 4.8 发布
  - 6/9 Fable 5 / Mythos 5 发布；6/12 因出口管制暂停访问；6/30 Sonnet 5 发布（临界）[B19][A24]
  - 截止日期为 Jun 2026 的模型应当“大致知道” 2 至 6 月的事件；Jan 2026 截止的模型不应知道
- 截止日期之后的事件：7/24 Opus 5、9/1 Fable 5.1、9/22 Opus 5.5、9/28 Sonnet 5.5、10/7 Haiku 5.5。没有系统提示也能准确说出这些事件，说明上下文被注入了信息，或者存在伪造。
- **注意事项**：
  - claude.ai 系统提示里写了 Fable 停供事件和最新模型列表；Claude Code 系统提示写了 “The most recent Claude models are … Fable 5.1 / Opus 5.5 / Sonnet 5.5 / Haiku 5.5” 以及模型名和截止日期[E7]。这些事实**在对应渠道里会被污染**，不能用来出题。
  - 模型对“本公司发布史”的记忆普遍不稳定，只宜作为弱信号，需要多题统计。

#### 7.3 档位与权限题

- 声称 Pro 或 Team Standard 的账号却能免费跑 Fable：按官方规则这类账号只能用 usage credits，说明卖家在额外付费，或者档位与申报不符[B9]。
- Opus 4.6[1m] 在 Pro 上需要 usage credits；Sonnet 4.6[1m] 在**所有订阅档**都需要 usage credits[C1]。
- Free 档没有 Claude Code，也没有 Opus[B1]。
- Fast mode 在订阅档只能走 usage credits[C6]。
- Claude Code 版本门槛：网关上报的版本低于 2.1.280 / 2.1.284 / 2.1.257 时，请求 Opus 5.5 / Sonnet 5.5 / Fable 5.1 会返回 400 `claude_code_version_too_old`[C1][C4]。
- Enterprise 账号可能被组织**静默钳制 effort 上限**或限制可用模型[C1]。

#### 7.4 出题时必须避开的话题（否则会触发官方换模型，被误判为卖家作弊）

- 不要出网络安全攻击类（exploit、渗透测试）、病毒学、毒理、分子设计类、前沿 LLM 训练或 kernel 开发类题目。
- **不要要求模型逐字复述思维链，或把推理过程写进输出**，否则触发 `reasoning_extraction`：直接拒答，在输出前就会计费，且没有回退模型[A29][B16]。
- 题目所在仓库或上下文中不要含有安全或生物资料，Claude Code 首个请求可能仅凭上下文就触发回退[C1]。

---

### 8. 未找到官方资料 / 存疑清单

1. **各档位的具体额度数字**：5 小时额度、周额度的 token 数或消息数均**未公开**；Fable 周额度的绝对值也未公开。
2. **按模型族的限额**（“Opus limit”和“Sonnet limit”）：只在 Claude Code 错误参考中出现，哪些档位适用、额度多大均**未找到官方资料**。sub2api 用量接口里的 `seven_day_sonnet` 等字段无官方对应说明。
3. **Free 档**：具体可用的 Sonnet / Haiku 版本、各模型上下文窗口、是否有周限额，均未找到官方资料。
4. **Team 文章与上下文专文冲突**：Team 文章写“200k context window”，上下文专文按模型写最高 1M。
5. **Pro 是否享有高峰期优先访问**：Pro 文章列为权益，定价页对比表 Pro 一栏写“No”，二者冲突。
6. **知识截止日期冲突**：Sonnet 4.6 在模型页和 claude.ai 系统提示中为 Aug 2025，透明度中心写 May 2025；Haiku 4.5 在模型页和透明度中心为 Feb 2025，claude.ai 系统提示写“end of January 2025”。
7. **Fable 5.1 所需 Claude Code 最低版本**：Claude Code 文档写 2.1.257，support 文章写 2.1.255，sub2api 注释写 2.1.251，三处不一致。
8. **第三方应用使用订阅的政策口径矛盾**：support 文章（2026-10-07 更新）称第三方应用仍可使用订阅额度；Claude Code 法律页禁止开发者代用户路由订阅凭证，消费者条款禁止共享账号和转售；2026-04 媒体引用的官方发言称第三方工具不被允许并改走 extra usage。**当前实际的执行标准不清楚**，转售模式与条款明显冲突，属合规高风险。
9. **OAuth 渠道的具体请求头**：`oauth-2025-04-20`、`claude-code-20250219`、`x-app: cli`、`user-agent: claude-cli/…`、`anthropic-dangerous-direct-browser-access` 都**不在**公开网页文档中，仅 `oauth-2025-04-20` 出现在 Anthropic 随 CLI 分发的 skill 文档里。
10. **不带 Claude Code 系统提示的 OAuth 请求是否被拒**：官方只间接提到（归因块是分类器请求“唯一的 Claude Code 标识”，v2.1.229 前未标识的请求会被拒）；“只认身份块”这一说法仅来自社区。
11. **`anthropic-ratelimit-unified-*` 的具体字段**（如 fallback-percentage、7d_oi）以及 `/api/oauth/usage` 接口：官方均无文档。
12. **高峰期加速扣额**（2026-03-26）以及 2026-05-06 额度翻倍：仅有媒体和员工 X 帖子，帮助中心无记载。
13. **effort 设为 xhigh 发给 Opus 4.6 / Sonnet 4.6 时 API 的行为**（报 400 还是静默降档）：未核实。Claude Code 客户端会自动降到 high。
14. **无系统提示时各模型自报版本的准确率**：官方没有资料，2026 年的新模型也没有社区实测。
15. **旧版按席位计费的 Enterprise**：Standard 和 Premium 席位的额度倍数未见公开。

---

### 附：来源索引（访问日期均为 2026-10-11）

**Anthropic 官方 · 开发者文档（platform.claude.com）**
- [A1] Models overview — https://platform.claude.com/docs/en/models/overview（旧地址 /docs/en/about-claude/models/overview 会 307 跳转到此）
- [A2] Claude Opus 5.5 — https://platform.claude.com/docs/en/models/opus-5-5/overview
- [A3] Claude Sonnet 5.5 — https://platform.claude.com/docs/en/models/sonnet-5-5/overview
- [A4] Claude Haiku 5.5 — https://platform.claude.com/docs/en/models/haiku-5-5/overview
- [A5] Claude Fable 5.1 — https://platform.claude.com/docs/en/models/fable-5-1/overview
- [A6] Claude Mythos 5.1 — https://platform.claude.com/docs/en/models/mythos-5-1/overview
- [A7] Claude Fable 5 — https://platform.claude.com/docs/en/models/fable-5/overview
- [A8] Claude Opus 5 — https://platform.claude.com/docs/en/models/opus-5/overview
- [A9] Claude Sonnet 5 — https://platform.claude.com/docs/en/models/sonnet-5/overview
- [A10] Claude Opus 4.8 — https://platform.claude.com/docs/en/models/opus-4-8/overview
- [A11] Claude Opus 4.7 — https://platform.claude.com/docs/en/models/opus-4-7/overview
- [A12] Claude Opus 4.6 — https://platform.claude.com/docs/en/models/opus-4-6/overview
- [A13] Claude Opus 4.5 — https://platform.claude.com/docs/en/models/opus-4-5/overview
- [A14] Claude Sonnet 4.6 — https://platform.claude.com/docs/en/models/sonnet-4-6/overview
- [A15] Claude Sonnet 4.5 — https://platform.claude.com/docs/en/models/sonnet-4-5/overview
- [A16] Claude Haiku 4.5 — https://platform.claude.com/docs/en/models/haiku-4-5/overview
- [A17] Pricing — https://platform.claude.com/docs/en/about-claude/pricing
- [A18] Model deprecations — https://platform.claude.com/docs/en/about-claude/model-deprecations
- [A19] Effort — https://platform.claude.com/docs/en/build-with-claude/effort
- [A20] Thinking — https://platform.claude.com/docs/en/build-with-claude/thinking
- [A21] Extended thinking — https://platform.claude.com/docs/en/build-with-claude/extended-thinking
- [A22] Prompt caching — https://platform.claude.com/docs/en/build-with-claude/prompt-caching
- [A23] Context windows — https://platform.claude.com/docs/en/build-with-claude/context-windows
- [A24] Platform release notes — https://platform.claude.com/docs/en/release-notes/overview
- [A25] What's new in Opus 5.5 — https://platform.claude.com/docs/en/models/opus-5-5/whats-new-opus-5-5
- [A26] What's new in Sonnet 5.5 — https://platform.claude.com/docs/en/models/sonnet-5-5/whats-new-sonnet-5-5
- [A27] What's new in Haiku 5.5 — https://platform.claude.com/docs/en/models/haiku-5-5/whats-new-haiku-5-5
- [A28] What's new in Fable 5.1 — https://platform.claude.com/docs/en/models/fable-5-1/whats-new-fable-5-1
- [A29] Refusals and fallback — https://platform.claude.com/docs/en/build-with-claude/refusals-and-fallback
- [A30] System prompts（总览）— https://platform.claude.com/docs/en/release-notes/system-prompts/overview
- [A31] Opus 5.5 system prompt — https://platform.claude.com/docs/en/release-notes/system-prompts/claude-opus-5-5
- [A32] Sonnet 5.5 system prompt — https://platform.claude.com/docs/en/release-notes/system-prompts/claude-sonnet-5-5
- [A33] Haiku 5.5 system prompt — https://platform.claude.com/docs/en/release-notes/system-prompts/claude-haiku-5-5
- [A33b] Haiku 4.5 system prompt — https://platform.claude.com/docs/en/release-notes/system-prompts/claude-haiku-4-5
- [A34] Fable 5.1 system prompt — https://platform.claude.com/docs/en/release-notes/system-prompts/claude-fable-5-1
- [A35] 其他模型 system prompt（同一 URL 模式）— https://platform.claude.com/docs/en/release-notes/system-prompts/claude-opus-5 ；…/claude-sonnet-5 ；…/claude-fable-5 ；…/claude-opus-4-8 ；…/claude-opus-4-7 ；…/claude-opus-4-6 ；…/claude-sonnet-4-6

**Anthropic 官方 · 官网**
- [A37] Transparency Hub — https://www.anthropic.com/transparency
- [A38] A postmortem of three recent issues（2025-09-17）— https://www.anthropic.com/engineering/a-postmortem-of-three-recent-issues
- [A39] An update on recent Claude Code quality reports（2026-04-23）— https://www.anthropic.com/engineering/april-23-postmortem
- [A40] Consumer Terms of Service（2025-10-08 生效）— https://www.anthropic.com/legal/consumer-terms
- [A41] Introducing Claude Opus 5.5 — https://www.anthropic.com/claude-opus-5-5
- [A42] Claude Sonnet 5.5 — https://www.anthropic.com/claude-sonnet-5-5
- [A43] Claude Haiku 5.5 — https://www.anthropic.com/claude-haiku-5-5
- [A44] 2026 Usage Policy update（2026-10-08 发布，11-12 生效；含 Supported Regions 执行口径）— https://www.anthropic.com/news/2026-usage-policy-update

**Claude 定价与帮助中心**
- [B1] https://claude.com/pricing
- [B2] What is the Pro plan? — https://support.claude.com/en/articles/8325606-what-is-the-pro-plan
- [B3] What is the Max plan? — https://support.claude.com/en/articles/11049741-what-is-the-max-plan
- [B4] What is the Team plan? — https://support.claude.com/en/articles/9266767-what-is-the-team-plan
- [B5] What is the Enterprise plan? — https://support.claude.com/en/articles/9797531-what-is-the-enterprise-plan
- [B6] Manage usage credits for paid Claude plans — https://support.claude.com/en/articles/12429409-manage-usage-credits-for-paid-claude-plans
- [B7] How do usage and length limits work? — https://support.claude.com/en/articles/11647753-how-do-usage-and-length-limits-work
- [B8] How large is the context window on paid Claude plans? — https://support.claude.com/en/articles/8606394-how-large-is-the-context-window-on-paid-claude-plans
- [B9] Claude Fable models on your plan — https://support.claude.com/en/articles/15424964-claude-fable-models-on-your-plan
- [B10] Use the Claude Agent SDK with your Claude plan — https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan
- [B11] Monthly API credits for Max and Team plans — https://support.claude.com/en/articles/17154008-monthly-api-credits-for-max-and-team-plans
- [B12] Use Claude Code with your Pro or Max plan — https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan
- [B13] Use Claude Code with your Team or Enterprise plan — https://support.claude.com/en/articles/11845131-use-claude-code-with-your-team-or-enterprise-plan
- [B14] Claude Code model configuration（support）— https://support.claude.com/en/articles/11940350-claude-code-model-configuration
- [B15] Usage limit best practices — https://support.claude.com/en/articles/9797557-usage-limit-best-practices
- [B16] Why Claude switched models…Opus 5 or Opus 5.5 — https://support.claude.com/en/articles/16049681-why-claude-switched-models-in-your-conversation-with-opus-5-or-opus-5-5
- [B17] Why Claude switched models…Fable 5 or Fable 5.1 — https://support.claude.com/en/articles/15363606-why-claude-switched-models-in-your-conversation-with-fable-5-or-fable-5-1
- [B18] Change the model, effort, and thinking settings — https://support.claude.com/en/articles/8664678-change-the-model-effort-and-thinking-settings
- [B19] Release notes（Claude apps）— https://support.claude.com/en/articles/12138966-release-notes
- [B20] Get started with Claude（Free 档）— https://support.claude.com/en/articles/8114491-get-started-with-claude
- [B21] What is a limit reset? — https://support.claude.com/en/articles/17007452-what-is-a-limit-reset

**Claude Code 官方文档（code.claude.com）**
- [C1] Model configuration — https://code.claude.com/docs/en/model-config
- [C2] Legal and compliance — https://code.claude.com/docs/en/legal-and-compliance
- [C3] Claude Code gateway compatibility guide — https://code.claude.com/docs/en/llm-gateway-protocol
- [C4] Error reference — https://code.claude.com/docs/en/errors
- [C5] Manage costs effectively — https://code.claude.com/docs/en/costs
- [C6] Fast mode — https://code.claude.com/docs/en/fast-mode
- [C7] Agent SDK：Modifying system prompts — https://code.claude.com/docs/en/agent-sdk/modifying-system-prompts
- [C8] Claude Code changelog — https://code.claude.com/docs/en/changelog

**Anthropic 随产品分发的资料（非公开网页）**
- [D1] Claude Code v2.1.296 内置 claude-api skill（`shared/anthropic-cli.md` 及 SKILL.md；由本机 Claude Code 解压得到，内容为 Anthropic 编写）

**社区 / 媒体（社区观察，未经官方确认）**
- [E1] anthropics/claude-code#60913（v2.1.145 请求头抓包）— https://github.com/anthropics/claude-code/issues/60913
- [E2] yetone/magpie#455（v2.1.286 请求头）— https://github.com/yetone/magpie/issues/455
- [E3] NousResearch/hermes-agent#15080 — https://github.com/NousResearch/hermes-agent/issues/15080
- [E4] Lowpower/pigo#242 — https://github.com/Lowpower/pigo/issues/242
- [E5] anthropics/claude-code#45134（官方 CLI 被误判为第三方应用）— https://github.com/anthropics/claude-code/issues/45134
- [E6] anthropics/claude-code#101152（`claude_code_version_too_old`）— https://github.com/anthropics/claude-code/issues/101152
- [E7] asgeirtj/system_prompts_leaks（Claude Code 各模型系统提示抓取）— https://github.com/asgeirtj/system_prompts_leaks/tree/main/Anthropic/claude-code（逐个查看了 claude-code-opus-5.5.md、claude-code-haiku-5.5.md、claude-code-headless-fable-5.1.md、claude-code-desktop-fable-5.1.md）
- [E8] The Register 2026-04-06 — https://www.theregister.com/software/2026/04/06/anthropic-closes-door-on-subscription-use-of-openclaw/5222854
- [E9] “credential is only authorized for use with Claude Code” — https://github.com/RooCodeInc/Roo-Code/issues/10566 ；https://github.com/anthropics/claude-code/issues/8052（仅读搜索摘要）
- [E10] 高峰期额度 — https://piunikaweb.com/2026/03/27/anthropic-explains-claude-usage-limits-peak-hours/ ；https://www.techradar.com/ai-platforms-assistants/claude/claude-is-limiting-usage-more-aggressively-during-peak-hours-heres-what-changed ；https://usagemeter.app/en/blog/claude-ai-peak-hours-surge-pricing-explained（仅读搜索摘要）
- [E11] Opus 4.6 降智讨论 — https://kingy.ai/news/was-claude-opus-4-6-nerfed-the-invisible-downgrade-when-the-ai-you-paid-for-stops-being-the-ai-you-get/ ；https://alphaguruai.substack.com/p/whats-going-on-with-claude-code（仅读搜索摘要）
- [E12] LessWrong：Claude Opus 5.5 Should Raise Your Ambitions — https://www.lesswrong.com/posts/rtPiip9igy3QvxYdM/claude-opus-5-5-should-raise-your-ambitions（仅读搜索摘要）
- [E13] Agent SDK 计费变更与暂停 — https://www.digitalapplied.com/blog/anthropic-claude-credit-overhaul-june-15-2026 ；https://7minai.com/news/anthropic-pauses-agent-sdk-billing/（仅读搜索摘要）
- [E14] 自报版本错误的旧报告 — https://github.com/anthropics/claude-code/issues/11105 ；https://github.com/cline/cline/issues/6646（仅读搜索摘要）
- [E15] 2026-01 第三方封堵 — https://x.com/theo/status/2009464346846621700 ；https://x.com/marcelpociot/status/2009521978345181578（仅读搜索摘要）
- [E16] TechRadar OpenClaw — https://www.techradar.com/pro/bad-news-claude-users-anthropic-says-youll-need-to-pay-to-use-openclaw-now（仅读搜索摘要）

## 第 2 章 OpenAI（ChatGPT 订阅 + Codex OAuth）

> 调研日期 / 全部来源访问日期：**2026-10-11**。
> 说明：developers.openai.com/codex/* 已整体重定向到 **learn.chatgpt.com/docs/***（"ChatGPT + Codex user docs"），表中统一引用新地址（原地址会 302 过去）。每页都有 `.md` 版（URL 加 `.md`）。
> 与训练记忆冲突处一律以官网为准：截至 2026-10，OpenAI 已上线 **GPT-6 家族（Astra / Sol / 6.1 Sol / Luna）**，GPT-5.6 家族改名为 Sol / Terra / Luna，Pro 拆成 $100/$200/$500 三档，Business 拆成 Standard/Premium 席位，Codex 已并入 "ChatGPT 桌面 App（Work / Codex 模式）"。
> 标注约定：**【官方】**＝OpenAI 官网/文档/帮助中心/官方开源仓库；**【源码】**＝github.com/openai/codex 开源代码（官方仓库，但属实现细节，不等于服务端承诺）；**【社区】**＝社区观察，未经官方确认。

### 0. 来源索引（下文用 [编号] 引用，访问日期均为 2026-10-11）

| 编号 | 来源 | URL |
|---|---|---|
| D1 | Codex/Work 模型页（含 GPT-5.5 退役、弃用列表） | https://learn.chatgpt.com/docs/models （.md 版同路径加 .md；原 https://developers.openai.com/codex/models ） |
| D2 | Codex/Work 定价与用量页 | https://learn.chatgpt.com/docs/pricing |
| D3 | Speed（Fast / Ultrafast） | https://learn.chatgpt.com/docs/agent-configuration/speed |
| D4 | Workspace model availability | https://learn.chatgpt.com/docs/enterprise/workspace-model-availability |
| D5 | Config Reference | https://learn.chatgpt.com/docs/config-file/config-reference |
| D6 | Models & Trusted Access（cyber） | https://learn.chatgpt.com/docs/cyber-safety |
| D7 | ChatGPT & Codex changelog | https://learn.chatgpt.com/docs/changelog |
| D8 | Agent approvals & security（Safety monitoring） | https://learn.chatgpt.com/docs/agent-approvals-security |
| D9 | Codex App Server 协议（`model/rerouted` 等事件） | https://learn.chatgpt.com/docs/app-server |
| D10 | Model selection（Codex/Work） | https://learn.chatgpt.com/docs/model-selection |
| A1 | API 模型页 GPT-6 Astra | https://developers.openai.com/api/docs/models/gpt-6-astra |
| A2 | API 模型页 GPT-6.1 Sol | https://developers.openai.com/api/docs/models/gpt-6.1-sol |
| A3 | API 模型页 GPT-6 Sol | https://developers.openai.com/api/docs/models/gpt-6-sol |
| A4 | API 模型页 GPT-6 Luna | https://developers.openai.com/api/docs/models/gpt-6-luna |
| A5 | API 模型页 GPT-5.6 Sol / Terra / Luna | https://developers.openai.com/api/docs/models/gpt-5.6-sol （-terra / -luna 同理） |
| A6 | API 模型页 GPT-5.5 / GPT-5.5 Pro / GPT-5.4 / GPT-5.3-Codex / GPT-5.2 | https://developers.openai.com/api/docs/models/gpt-5.5 （其余替换 slug） |
| A7 | API 模型页 chat-latest / gpt-5.3-chat-latest | https://developers.openai.com/api/docs/models/chat-latest |
| A8 | API Using GPT-6 指南 | https://developers.openai.com/api/docs/guides/latest-model |
| A9 | API Reasoning 指南 | https://developers.openai.com/api/docs/guides/reasoning |
| A10 | API Changelog | https://developers.openai.com/api/docs/changelog |
| A11 | API Cybersecurity checks | https://developers.openai.com/api/docs/guides/safety-checks/cybersecurity |
| A12 | API 全模型目录 | https://developers.openai.com/api/docs/models/all |
| H1 | GPT-6 and other models in ChatGPT（原 11909943 "GPT-5 in ChatGPT" 已重定向至此） | https://help.openai.com/en/articles/20001354-gpt-6-and-other-models-in-chatgpt |
| H2 | About ChatGPT Pro tiers | https://help.openai.com/en/articles/9793128-about-chatgpt-pro-tiers |
| H3 | What is ChatGPT Plus? | https://help.openai.com/en/articles/6950777-what-is-chatgpt-plus |
| H4 | What is ChatGPT Go? | https://help.openai.com/en/articles/11989085-what-is-chatgpt-go |
| H5 | ChatGPT Free Tier FAQ | https://help.openai.com/en/articles/9275245-chatgpt-free-tier-faq |
| H6 | ChatGPT Business models and limits | https://help.openai.com/en/articles/12003714-chatgpt-business-models-and-limits |
| H7 | ChatGPT Business - Overview（席位价格） | https://help.openai.com/en/articles/8792828-chatgpt-business-overview |
| H8 | ChatGPT Enterprise and Edu models and limits | https://help.openai.com/en/articles/11165333-chatgpt-enterprise-and-edu-models-and-limits |
| H9 | Managing usage with GPT-6 Astra in Work and Codex | https://help.openai.com/en/articles/20001516-managing-usage-with-gpt-6-astra-in-work-and-codex |
| H10 | ChatGPT Work and Codex | https://help.openai.com/en/articles/20001275-chatgpt-work-and-codex |
| H11 | Using Codex with your ChatGPT plan | https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan |
| H12 | Luna Reserve in Codex and ChatGPT Work | https://help.openai.com/en/articles/20001499-luna-reserve-in-codex-and-chatgpt-work |
| H13 | Why was my chat paused or ended as a precaution? | https://help.openai.com/en/articles/20001509-why-was-my-chat-paused-or-ended-as-a-precaution |
| H14 | ChatGPT release notes | https://help.openai.com/en/articles/6825453-chatgpt-release-notes |
| H15 | Model Release Notes | https://help.openai.com/en/articles/9624314-model-release-notes |
| H16 | Using Credits for Flexible Usage (Personal plans) | https://help.openai.com/en/articles/12642688 |
| P1 | ChatGPT 个人版定价页（登录态页面取不到对比表，使用 Wayback 2026-10-10 02:04 UTC 快照） | https://chatgpt.com/pricing/ ；快照 https://web.archive.org/web/20261010020431/https://chatgpt.com/pricing/ |
| P2 | Business / Enterprise 定价页 | https://openai.com/business/pricing/ |
| B1 | Introducing GPT-6 Astra（2026-09-03） | https://openai.com/index/gpt-6-astra/ |
| B2 | Path to Astra（2026-09-01，安全措施） | https://openai.com/index/path-to-astra/ |
| B3 | Introducing GPT-6 Sol and Luna（2026-09-22） | https://openai.com/index/introducing-gpt-6-sol-and-luna/ |
| B4 | Introducing GPT-6.1 Sol（2026-09-29） | https://openai.com/index/introducing-gpt-6-1-sol/ |
| B5 | GPT-6 and Intelligent UI for everyone（2026-10-07） | https://openai.com/index/gpt-6-for-everyone/ |
| G1 | Codex 模型目录 models.json（main，最近修改 commit d63a9b8 2026-10-06；GPT-6.1 Sol 设为默认的 commit b1e7296 2026-09-29） | https://raw.githubusercontent.com/openai/codex/main/codex-rs/models-manager/models.json |
| G2 | ChatGPT 后端 base URL 常量 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/model-provider-info/src/lib.rs |
| G3 | 鉴权头（ChatGPT-Account-ID / X-OpenAI-Fedramp） | https://raw.githubusercontent.com/openai/codex/main/codex-rs/model-provider/src/bearer_auth_provider.rs |
| G4 | originator / User-Agent | https://raw.githubusercontent.com/openai/codex/main/codex-rs/login/src/auth/default_client.rs |
| G5 | session-id / thread-id 头 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/codex-api/src/requests/headers.rs |
| G6 | 请求体构造（store/stream/include/指令） | https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/src/client.rs |
| G7 | 请求体结构 ResponsesApiRequest | https://raw.githubusercontent.com/openai/codex/main/codex-rs/codex-api/src/common.rs |
| G8 | "把 base instructions 改为 developer 消息" commit（#51156，2026-10-05，首发于 rust-v0.162.0） | https://github.com/openai/codex/commit/c9253c4977 |
| G9 | 响应头 openai-model / 限额头解析 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/codex-api/src/sse/responses.rs ；https://raw.githubusercontent.com/openai/codex/main/codex-rs/codex-api/src/rate_limits.rs |
| G10 | ModelRerouteReason 定义 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/protocol/src/protocol.rs ；https://raw.githubusercontent.com/openai/codex/main/codex-rs/app-server-protocol/schema/typescript/v2/ModelRerouteReason.ts |
| G11 | OAuth（issuer / client_id / scope） | https://raw.githubusercontent.com/openai/codex/main/codex-rs/login/src/server.rs ；https://raw.githubusercontent.com/openai/codex/main/codex-rs/login/src/auth/manager.rs |
| G12 | 旧版系统提示文件 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/gpt_5_codex_prompt.md 、…/gpt_5_1_prompt.md 、…/gpt-5.2-codex_prompt.md 、…/codex-rs/models-manager/prompt.md |
| G13 | 安全缓冲头 | https://raw.githubusercontent.com/openai/codex/main/codex-rs/codex-api/src/safety_buffering.rs |

---

### 1. ChatGPT 订阅档位对比

**Chat（普通对话）模型映射【官方】**：模型菜单里 "GPT-6 replaces Latest"；"When GPT-6 is selected, GPT-6 Sol powers Instant, Medium, High, and Extra High; Pro uses GPT-6 Pro, powered by GPT-6 Astra." [H1]；10-07 发布说明："Plus, Pro, Business, and Enterprise use GPT-6 Sol; Free and Go use GPT-6 Luna." [H14][B5]（与 H1 中 "Free and Go use GPT-5.6 Luna" 冲突，见 §8）。GPT-6.1 Sol / GPT-6 Luna / GPT-5.6 Terra 在 Chat 中不可选，只在 Work 与 Codex 中可选 [H1][H6][D1]。

| 档位 | 月价（美元，官方） | ChatGPT（Chat）可用模型 | 上下文（Chat，官方定价表） | 用量规则（Chat） | Codex 可用性与限额 | 来源 |
|---|---|---|---|---|---|---|
| Free | $0 | GPT-6（10-08 起向 Free 推送，由 GPT-6 Luna 驱动）；Think（帮助中心写 "Think uses GPT-5.6 Luna"）；定价表：GPT-6 Luna = Yes，GPT-5 Thinking Mini = Yes，GPT-6 Astra / 6 Sol / 6.1 Sol / 5.6 Sol = No；无 legacy | GPT Instant 总上下文 **27K**（输入上限约 12 页）；Reasoning："Varies" | "Free users have unlimited everyday text chats, subject to abuse-prevention safeguards"；上传/生图/语音/数据分析另有限额；部分国家有广告 | 定价表 Codex = "Limited"；Codex 定价卡："GPT-6 Luna at Standard speed in the desktop app, subject to rollout"；定价表 GPT-5.6 Terra = "Limited access in Work and Codex on desktop"；**Codex Cloud 不含 Free/Go**；无图像生成 | [P1][H1][H5][D2][H11][B5][H14] |
| Go | $8（Codex 定价卡；帮助中心称价格见定价页、按地区计价） | 同 Free + 更高工具额度；Think = GPT-5.6 Luna；"ChatGPT Go does not include GPT-5.6 Sol"；无 legacy | Instant **54K**（约 40 页）；Reasoning **256K**（约 320 页） | 文字聊天不限；工具额度高于 Free；"may include ads" | 同 Free（Limited；桌面端 GPT-6 Luna；无 Cloud） | [D2][P1][H4][H1] |
| Plus | $20（仅月付） | GPT-6（Sol）Instant / Medium / High（**无 Extra High、无 Pro**）；GPT-5.6 Sol 可选；legacy 可用。Work/Codex：GPT-6 Astra（"limited Astra usage"）、6.1 Sol、6 Sol、6 Luna、5.6 Sol/Terra/Luna | Instant **54K**（约 40 页）；Reasoning **256K**（约 320 页） | Thinking 限额"depend on your plan"，**未公布具体条数**；达 Thinking 限额 "ChatGPT may continue with another available model"；2026-09-14 起 Instant 不再自动升 Thinking（"ChatGPT can still switch automatically for safety"） | Codex 本地消息/5 小时估算：**GPT-6 Astra 5–45、GPT-6.1 Sol 15–160、GPT-6 Sol 15–150、GPT-6 Luna 350–3,000**，"Weekly limits may also apply"；可买 credits；含 Codex Cloud、代码审查、Slack；Fast 可用，Ultrafast 不可用 | [H3][H1][H14][D2][H9][H10][P1] |
| Pro（$100 / $200 / $500 三档） | $100 / $200 / $500（"Pro is billed monthly"，无年付） | GPT-6（Sol）Instant–Extra High + **Pro 档 = GPT-6 Pro（由 GPT-6 Astra 驱动）**（官方句子只列 "Pro $100, Pro $200, Business and Enterprise"）；GPT-5.6 Sol Pro；Work/Codex 全部模型（定价表 Astra / 6.1 Sol / 6 Sol / 6 Luna = "Expanded"） | Instant **128K**（约 250 页）；Reasoning **400K**（约 680 页） | GPT-6 Pro 有周限额（"does not include unlimited use"）；**Pro $200 用完 GPT-6 Pro 周额度后 "automatically switches to GPT-5.6 Thinking at Medium"**；Pro $100 两个 Pro 模型共用一个周额度；"Unlimited subject to abuse guardrails"（Instant 等） | "Pro 100, Pro 200 and Pro 500 currently have no five-hour usage limit in Work and Codex"（仍有总额度，看 Settings→Usage）；"Pro 200 includes more usage than Pro 100. Pro 500 offers the highest"；**Ultrafast 仅 Pro 500**（included 用量按 8×、credits 按 6× 计）；2026-09-29 起新订 Pro 200 额度下调，09-22~09-29 有效订阅者旧额度保留至 10-29 | [H2][H1][P1][H9][H10][D2][D3][H14] |
| Business Standard 席位（原 Team） | $25/席/月（月付）；$20/席/月（年付）；≥2 席 | GPT-6（Sol）Instant–Extra High + Pro（GPT-6 Pro/Astra）；可开 "Higher intelligence"（Instant 下自动加推理）；Work/Codex：6 Sol / 6.1 Sol / 6 Luna，视工作区可有 Astra 与 5.6 系 | Instant **54K**（约 40 页）；Reasoning **256K**（约 320 页） | Instant "virtually unlimited"；**Pro 消息 15 条/月**（GPT-6 Pro 与 GPT-5.6 Sol Pro 共用） | 本地消息/5h（Standard）：Astra 5–45；6.1 Sol、6 Sol 15–150（Codex 定价页写 6.1 Sol 15–160）；5.6 Sol 10–100；5.6 Terra 25–200；5.6 Luna 250–2,000；"Standard seats include limited Astra usage"；可购工作区 credits；无 Ultrafast | [H7][H6][P2][D2] |
| Business Premium 席位 | $125/席/月（月付）；$100/席/月（年付） | 同 Standard | 同 Business（定价页不区分席位） | **Pro 消息 50 条/周**（两 Pro 模型共用） | "Premium includes 5x more usage than Standard seats, no 5-hour usage limit"；可全额用于 Astra | [H7][H6] |
| Enterprise / Edu | 联系销售（无公开价） | GPT-6（Sol）+ Pro（GPT-6 Pro/Astra，需管理员开权限）；Astra、6 Sol/Luna、6.1 Sol 在 Ent/Edu **默认关闭**需管理员启用；可配置自动路由 | Enterprise：Instant **128K**（约 250 页）；Reasoning **256K**（约 320 页）；Edu 未单列 | "virtually unlimited messages for eligible base models"（token 计费协议下 Instant 也计费） | "Enterprise/Edu users with flexible pricing have no fixed rate limits"；无 flexible pricing 的 "same per-seat usage limits as Plus for most features"；"Priority request processing"；Ultrafast 限合格 Ent/Edu 且默认关 | [P2][H8][D2][D4][D3] |
| API Key 登录 Codex | 按 API 价 | — | — | — | CLI / SDK / IDE 可用，无云端功能；"Model availability follows the API models available to your key"；不受 GPT-5.4/5.5 退役影响 | [D2][D1] |

补充说明：
- **地区价**【官方页面观察】：调研用浏览器位于新加坡，定价弹窗显示 Go 11、Plus 30、Pro 起 138（同页 Business 显示 "SGD 25"，说明是 SGD 本地价）；Business 帮助页："pricing may vary depending on your country and currency type" [H7][P2]。卖家账号可能来自不同地区，**月价应以美国价为基准**。
- **Chat 的 GPT-5.6 Sol 与 API/Codex 版本不同**【官方】："GPT-5.6 Sol refers to the version available in our API, ChatGPT Codex, and ChatGPT Work. The version in ChatGPT Chat is slightly different." [B1 脚注]
- **Chat 与 Work/Codex 额度互相独立**【官方】："GPT-6 Pro in Chat is powered by Astra, but Chat has its own model availability and message limits. Work and Codex share a separate usage allowance." [H9]；"ChatGPT Work and Codex share usage." [D2]
- 历史上下文变化【官方】：2026-02-20 发布说明 "ChatGPT now has a total context window length of 256k tokens (128k input and 128k max output) when you manually select Thinking. Previously it was 196k total tokens." [H14]

---

### 2. 模型参数

#### 2.1 参数总表

API 数字来自各模型页 "Model details"（逐字："1,050,000 context window / Maximum input tokens: 922,000 / 128,000 max output tokens / … knowledge cutoff"）；Codex 列来自 models.json（`context_window` / `max_context_window` / `default_reasoning_level` / `supported_reasoning_levels`）[G1]。

| 模型名 | API / Codex 模型 ID | 上下文（API 总 / 最大输入；Codex 默认 / 可配上限） | 最大输出 | 知识截止 | reasoning effort 取值 / 默认 | 可用渠道与档位 | 来源 |
|---|---|---|---|---|---|---|---|
| GPT-6 Astra | `gpt-6-astra`（官方存在，**已核实**；Codex 显示名 "GPT-6-Astra"，UI 简称 "Astra"） | API 1,050,000 / 922,000；Codex 272,000 / 872,000 | 128,000 | **2026-04-30** | API：low / medium / high / xhigh / max（**不支持 none**，设 none 返回 HTTP 400；模型页未写默认值）；Codex：+ **ultra**，Codex 默认 **low**（文档："In configuration, Astra's Light setting is `low`"） | Chat：作为 "GPT-6 Pro" 用于 Pro $100/$200、Business、Enterprise；Work/Codex：Plus（限量）/Pro/Business/Ent/Edu（Ent/Edu 默认关）；API；Ultrafast（Pro $500、API） | [A1][A8][A9][D1][G1][H1][B1] |
| GPT-6.1 Sol | `gpt-6.1-sol` | API 1,050,000 / 922,000；Codex 272,000 / 872,000 | 128,000 | **2026-04-30** | API：low / **medium（默认）** / high / xhigh / max；"The `none` and `minimal` reasoning efforts are not supported"；Codex：+ ultra，Codex 默认 low | **Codex 当前默认/推荐模型**（models.json priority 1）；Work/Codex：Plus/Pro/Business/Ent/Edu（"Free and Go are not included at launch"）；**不在 Chat**；API；Ultrafast（Pro $500、API） | [A2][A8][D1][G1][B4][D7] |
| GPT-6 Sol | `gpt-6-sol` | 同上 | 128,000 | **2026-04-20** | API：none / low / **medium（默认）** / high / xhigh / max；Codex：low…max + ultra，默认 medium | Chat：驱动付费档 GPT-6 Instant–Extra High；Work/Codex：Plus 起；API | [A3][H1][G1] |
| GPT-6 Luna | `gpt-6-luna` | 同上 | 128,000 | **2026-05-18** | API：none / low / **medium（默认）** / high / xhigh / max；Codex：最高到 max（"GPT-6 Luna supports reasoning efforts up to **Max**, but not **Ultra**"），默认 medium | Chat：Free/Go 的 GPT-6（按 10-07 发布说明）；Work/Codex：全部档位（Free/Go 仅桌面 App）；API | [A4][D1][G1][B5][D7] |
| GPT-5.6 Sol | `gpt-5.6-sol`（`gpt-5.6` 别名指向它） | API 1,050,000 / 922,000；Codex 272,000 / 872,000 | 128,000 | **2026-02-16** | API：none / low / medium（默认）/ high / xhigh / max；Codex：+ ultra，默认 low；API 另有 `reasoning.mode: pro` | Chat：付费档可显式选择 "GPT-5.6 Sol"，Pro 档另有 "GPT-5.6 Sol Pro"；Work/Codex 付费档；API | [A5][A9][H1][G1][P1] |
| GPT-5.6 Terra | `gpt-5.6-terra` | 同上 | 128,000 | **2026-02-16** | API：none…max，默认 medium；Codex 默认 medium，+ ultra | Chat 不可选；Work/Codex（Free/Go 桌面限量）；API | [A5][H1][P1][G1] |
| GPT-5.6 Luna | `gpt-5.6-luna` | 同上 | 128,000 | **2026-02-16** | API：none…max，默认 medium；Codex 最高 max，默认 medium | Chat：Free/Go 默认与 Think（帮助中心说法）；**Luna Reserve 兜底模型**；Work/Codex；API | [A5][H1][H12][G1] |
| GPT-5.5 | `gpt-5.5`（快照 `gpt-5.5-2026-04-23`） | API 1,050,000（页内未列最大输入）；Codex 272,000 / 272,000 | 128,000 | **2025-12-01** | API：none / low / medium（默认）/ high / xhigh；Codex：low…xhigh，默认 medium | **2026-10-14 从 ChatGPT / Work / Codex 全档位退役**；API 继续 | [A6][D1][G1] |
| GPT-5.5 Pro | `gpt-5.5-pro`（`gpt-5.5-pro-2026-04-23`） | 1,050,000 | 128,000 | 2025-12-01 | medium / high（默认）/ xhigh | 仅 API（Responses） | [A6] |
| chat-latest | `chat-latest` | 400,000 / 272,000 | 128,000 | **2026-04-20** | 页面未写 | API；"points to the latest Instant model currently used in ChatGPT"，2026-10-07 已更新为 Plus/Pro/Business/Enterprise 当前模型（截止日与 GPT-6 Sol 一致） | [A7][A10] |
| GPT-5.4 / 5.4 mini | `gpt-5.4`（`gpt-5.4-2026-03-05`） | 1,050,000 | 128,000 | 2025-08-31 | none（默认）/ low / medium / high / xhigh | 2026-08-31 起 ChatGPT 登录的 Codex 不可用；API 仍可用 | [A6][D1] |
| GPT-5.3-Codex | `gpt-5.3-codex` | 400,000 / 272,000 | 128,000 | 2025-08-31 | low / medium / high / xhigh | ChatGPT 登录的 Codex 中已弃用；API | [A6][D1] |
| GPT-5.3-Codex-Spark | `gpt-5.3-codex-spark` | 未找到官方资料（API 无此页） | — | — | — | 2026-09-14 退役 | [D1][D7] |
| GPT-5.2 | `gpt-5.2`（`gpt-5.2-2025-12-11`） | 400,000 | 128,000 | 2025-08-31 | none（默认）…xhigh | Codex 中已弃用；API | [A6][D1] |
| Daybreak Blue / Red | `gpt-daybreak-blue-latest`（→`gpt-5.6-sol`）、`gpt-daybreak-red-latest`（→`gpt-5.6-cyber`，400,000/272,000）；API 页称别名已 deprecated | Codex：Blue 272K/872K，Red 372K/372K | 128,000 | 2026-02-16 | Codex：low…max + ultra（Blue 默认 low，Red 默认 medium） | 需 Trusted Access for Cyber 审批；Codex 目录中 `visibility: hide` | [A12][D6][G1] |
| Codex Auto Review | `codex-auto-review` | Codex 272K / 872K | — | — | low…max，默认 medium | Codex 内部审批复核模型（隐藏）；"auto-review safety checks are free and don't count toward your plan's usage limits" | [G1][H11] |

**是否已有 GPT-6 系列**：是。时间线【官方】：GPT-6 Astra 2026-09-03（先限量组织，随后付费档）[B1][H14][A10]；GPT-6 Sol / Luna 2026-09-22 [B3][D7][A10]；GPT-6.1 Sol 2026-09-29 [B4][D7][A10]；GPT-6 进入 Chat（Intelligent UI）2026-10-07，Free/Go 10-08 起 [B5][H14]；Ultrafast：Astra 09-29、6.1 Sol 10-08 [A10][D7]。
**"gpt-6-astra" 核实结论**：存在。官方出处：API 模型页 "Model ID: `gpt-6-astra`" [A1]；Codex 模型页组件 `name="gpt-6-astra"` [D1]；models.json slug `gpt-6-astra` [G1]；发布文 "available in the OpenAI API as gpt-6-astra" [B1]。

#### 2.2 effort / summary / verbosity / speed 参数（Codex）

| 参数 | 取值 | Codex 默认 | 来源 |
|---|---|---|---|
| `model_reasoning_effort` | "such as `low`, `medium`, `high`, `xhigh`, `max`, or `ultra`. Available levels depend on the model and client." UI 名：Light(=low) / Medium / High / Extra High(=xhigh) / Max / Ultra | 按模型（见上表）；**Ultra = "Maximum reasoning with automatic task delegation"（用子代理并行）**，Luna 系与 GPT-5.5 不支持 | [D5][D1][G1] |
| `model_reasoning_summary` | `auto` / `concise` / `detailed` / `none` | models.json 各模型 `default_reasoning_summary: "none"`（支持 summary） | [D5][G1] |
| `model_verbosity` | `low` / `medium` / `high` | 各模型 `default_verbosity: "low"`（Daybreak Red 为 high） | [D5][G1] |
| `service_tier` | "`fast` maps to the request value `priority`" | Fast：Astra/6.1 Sol 标注 "2x speed, increased usage"；6 Sol/6 Luna "1.5x speed"（目录中 `default_service_tier: "priority"`）；5.6/5.5 "1.5x speed" | [D5][D3][G1] |
| `model_context_window` | 数字；被 `max_context_window` 截断 | 默认 272,000；可配到 872,000（源码 `context_window.min(max_context_window)`）；有效上下文按 95% 计（源码默认 `effective_context_window_percent: 95`） | [D5][G1] 源码 model_info.rs |
| API `reasoning.mode` | `standard` / `pro`（GPT-5.6 与 GPT-6） | standard | [A9] |

额度系数【官方】：Fast 消耗 included 额度 **2.5×**、credits **2×**；Ultrafast（Astra / 6.1 Sol）included **8×**、credits **6×**；"GPT-6 Astra Ultrafast generates tokens up to 8x faster than GPT-6 Astra in Standard mode in Codex" [D3][D2][H2]。Credits 费率（每 1M token，Standard）：Astra 250/25/1,250；6.1 Sol 50/2.5/250；6 Sol 50/5/250；6 Luna 2.5/0.25/12.5；5.6 Sol 100/10/500；5.5 125/12.5/750 [D2]（按 Codex for Students "$100 … or 2,500 credits" 推算 1 credit≈$0.04，与 API 价一致，推算值）。

---

### 3. Codex OAuth 渠道的特殊要求

#### 3.1 端点与登录【源码】
- ChatGPT 登录时默认 base URL：`pub const CHATGPT_CODEX_BASE_URL: &str = "https://chatgpt.com/backend-api/codex";`；API Key 登录则 `https://api.openai.com/v1` [G2]。HTTP 请求为 `POST {base}/responses`，`Accept: text/event-stream`（SSE）[源码 codex-api/src/endpoint/responses.rs]。目录中各模型 `prefer_websockets: true`，WebSocket 握手带 `OpenAI-Beta: responses_websockets=2026-02-06` [G6][G1]。
- OAuth：issuer `https://auth.openai.com`，本地回调端口 1455，`CLIENT_ID = "app_EMoamEEZ73f0CkXaXp7hrann"`，scope `"openid profile email offline_access api.connectors.read api.connectors.invoke"`，刷新端点 `https://auth.openai.com/oauth/token` [G11]。
- 官方说明（文档层面）只写到 "sign in with ChatGPT"，不公开后端协议；以下为源码细节。

#### 3.2 请求头【源码】

| 头 | 值 / 说明 | 来源 |
|---|---|---|
| `Authorization` | `Bearer <access_token>` | [G3] |
| `ChatGPT-Account-ID` | 账号/工作区 id（`headers.insert("ChatGPT-Account-ID", …)`） | [G3] |
| `X-OpenAI-Fedramp` | FedRAMP 账号为 `true` | [G3] |
| `originator` | 默认 `codex_cli_rs`（`DEFAULT_ORIGINATOR`）；第一方识别：`codex_cli_rs`、`codex-tui`、`codex_vscode`、以 `Codex ` 开头；Chat 类：`codex_atlas`、`codex_chatgpt_desktop` | [G4] |
| `User-Agent` | `"{originator}/{version} ({os} {os_version}; {arch}) {terminal}"` | [G4] |
| `session-id`、`thread-id` | **连字符写法**；2026-05-08 引入，2026-05-13 删除下划线版 `session_id`（commit "drop underscored id headers"）；`conversation_id` 头已于 2026-01-12 移除 | [G5] 及其 commit 历史 |
| `x-client-request-id` | = thread id | 源码 endpoint/responses.rs |
| `x-openai-subagent` | `review` / `compact` / `memory_consolidation` / `collab_spawn` 等 | [G5] |
| `x-codex-turn-state` | 粘性路由令牌：从响应头取得后，同一 turn 内后续请求回传 | [G6][G9] |
| `x-codex-beta-features`、`x-codex-routing-hint`、`x-codex-installation-id`、`x-codex-turn-metadata`、`x-codex-window-id` | Codex 约定头 | [G6] |
| `x-oai-attestation` | 宿主（如桌面 App）可选提供的证明头 | 源码 core/src/attestation.rs |
| `x-openai-internal-codex-residency: us`、`x-openai-internal-codex-responses-lite: true` | 托管驻留要求 / Responses Lite 模型 | [G2][G6] |

#### 3.3 请求体【源码】
- `ResponsesApiRequest` 字段：`model, stream, service_tier, input, tools, tool_choice, parallel_tool_calls, reasoning, store, stream_options, include, prompt_cache_key, text, client_metadata, access_programs`——**当前版本已没有顶层 `instructions` 字段** [G7]。
- 固定值：`store: false`、`stream: true`、`tool_choice: "auto"`、`include: ["reasoning.encrypted_content"]`（可加 `output.encrypted_content`）[G6]。
- **instructions 位置变化**：commit #51156（2026-10-05，"Move base instructions from the top-level `instructions` field into a prefixed `developer` message in `input`…"），首发于 **rust-v0.162.0**（0.161.0 不含；最新 release rust-v0.162.1 发布于 2026-10-09）[G8]。此前版本把官方系统提示放在顶层 `instructions`。
- 服务端是否强制 `store=false` / `stream=true`：**未找到官方资料**（官方客户端总是这样发）。

#### 3.4 instructions 是否必须是官方提示（后端是否校验）
- 【官方】无公开说明（未找到官方资料）。
- 【社区，未经官方确认】2025 年起多方观察到：ChatGPT 登录走 `chatgpt.com/backend-api/codex/responses` 时，顶层 `instructions` 若不是官方 Codex 默认提示，返回 `400 {"detail":"Instructions are not valid"}`；可行做法是保留官方 instructions，把自定义提示放到 `developer` 消息里。来源：Simon Willison 2025-11 https://simonwillison.net/2025/Nov/9/gpt-5-codex-mini/ ；openai/codex issue #3202 https://github.com/openai/codex/issues/3202 、#4433 https://github.com/openai/codex/issues/4433 ；2026-09-09 社区 PR 仍称 "validado server-side e responde 400 "Instructions are not valid" para prompt arbitrário" https://github.com/jvcss/claudecode_to_api/pull/1 ；2026-10-02 社区 PR 遇到该错误后"切 developer 消息重试" https://github.com/AMA-Link/armadra-agent/pull/74 。同一社区 PR 还提到直连 HTTP 会因数据中心 IP / TLS 指纹被 Cloudflare 拦截。
- 推断（未经官方确认）：官方客户端 0.162.0 起已不发顶层 `instructions`，后端至少接受"无 instructions + developer 消息"这种形状；**若网关仍按旧格式发 `instructions`，校验规则可能仍然存在**，需实测。sub2api 本身的 issue #5138（2026-07-31）显示其 passthrough 会在本地拒绝缺 instructions 的 Codex 请求 https://github.com/Wei-Shaw/sub2api/issues/5138 （第三方项目）。

#### 3.5 系统提示开头原文（models.json `model_messages.instructions_template` 首句，逐字）【源码】[G1][G12]

| 模型 | 开头原文 |
|---|---|
| gpt-6-astra、gpt-6.1-sol、gpt-6-sol、gpt-6-luna | "You are Codex, an agent based on GPT-6. You and the user share one workspace, and your job is to collaborate with them until their intended goal is completely handled." |
| gpt-5.6-sol、gpt-5.6-terra、gpt-5.6-luna、Daybreak Blue/Red、codex-auto-review | "You are Codex, an agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled." |
| gpt-5.5 | "You are Codex, a coding agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled." |
| 旧文件 gpt_5_codex_prompt.md / gpt-5.2-codex_prompt.md（及 5.1-codex-max） | "You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer." |
| 旧文件 gpt_5_1_prompt.md | "You are GPT-5.1 running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI. You are expected to be precise, safe, and helpful." |
| models-manager/prompt.md（通用回退） | "You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI. You are expected to be precise, safe, and helpful." |

要点：GPT-6 各型号共用"based on GPT-6"，**提示里不出现 Astra / Sol / Luna 或知识截止日期**（对 instructions 全文 grep 无 "knowledge"/"cutoff"/"Astra"/"Sol"/"Luna"）[G1]。指令长度：Astra 21,420 字符、6.1 Sol 21,769、6 Sol 18,992、6 Luna 18,037、5.6 系 17,730、5.5 21,459——会让 input token 计数比买家原始 prompt 多出约数千 token（未精确换算）。源码中还有 `<current_time_reminder>It is YYYY-MM-DD HH:MM:SS UTC.</current_time_reminder>` 这类 developer 片段（何时注入未核实），会让模型"知道今天日期"。

#### 3.6 响应头（可用于检测）【源码】[G9][G13]
- **`openai-model`（或 `x-openai-model`）**：服务端实际使用的模型；Codex 读取后发 `ServerModel` 事件，若与请求不同则发 `model/rerouted`。
- 限额：`x-codex-primary-used-percent / -window-minutes / -reset-at`、`x-codex-secondary-*`（主/次窗口，对应 5 小时与每周），`x-codex-credits-has-credits / -unlimited / -balance`，`x-codex-rate-limit-reached-type`，`x-codex-promo-message`；另有按 limit id 分族的同名头（如 `codex_other`）。
- 安全缓冲：`x-codex-safety-buffering-enabled`、`x-codex-safety-buffering-faster-model`。其他：`x-codex-turn-state`、`x-reasoning-included`、`x-request-id`、`X-Models-Etag`；SSE 元数据 `openai_verification_recommendation: trusted_access_for_cyber`。

---

### 4. 安全路由与其他会让回复模型变化的机制

#### 4.1 ChatGPT（Chat）

| 机制 | 现状 / 说法 | 类型 | 来源 |
|---|---|---|---|
| 安全路由（敏感/情绪对话） | 2025-09 起："real-time router to direct sensitive parts of conversations—such as those showing signs of acute distress—to reasoning models"；2025-10-03 起 "When GPT-5 Auto or a non-reasoning model is selected, we'll instead route these conversations to GPT-5 Instant…"，"ChatGPT will continue to tell users which model is active when asked."；**2026 现状**：2026-09-14 "ChatGPT can still switch automatically for safety purposes"，H1 同句。**路由目标模型的现名未公开**（未找到官方资料） | 官方 | [H15][H14][H1] |
| "gpt-5-chat-safety" | 2025-09 媒体/用户观察到 GPT-4o/GPT-5 对话被切到未公开的 `gpt-5-chat-safety`；OpenAI 高管 Nick Turley 称 "Routing happens on a per-message basis…ChatGPT will tell you which model is active when asked"；2026 是否仍叫此名：未找到资料 | 社区/媒体 | https://www.bleepingcomputer.com/news/artificial-intelligence/openai-is-routing-gpt-4o-to-safety-models-when-it-detects-harmful-activities/ ；https://techcrunch.com/2025/09/29/openai-rolls-out-safety-routing-system-parental-controls-on-chatgpt/ ；https://lex-au.github.io/Whitepaper-GPT-5-Safety-Classifiers/ |
| Auto / 自动升推理 | Plus/Pro：2026-09-14 "We're retiring automatic switching from Instant to Thinking (reasoning) for ChatGPT Plus and Pro users globally"；Free/Go：早前已取消自动切换；Business 有 "Higher intelligence"（Instant 下自动加推理，消耗推理额度）；Ent/Edu 有 "automatic-routing controls" | 官方 | [H14][H1][H6][H8] |
| 达限降级 | "If you reach a Thinking limit, ChatGPT may continue with another available model."；**Pro $200：GPT-6 Pro 周限用完 → 自动切 GPT-5.6 Thinking（Medium）**；历史：GPT-5.4 mini 作 GPT-5.4 Thinking 的达限 fallback（2026-03）、GPT-5.3 Instant Mini 作 Instant 的达限 fallback（2026-04，"won't appear in the model picker"）——GPT-6 时代的 Instant 达限 fallback 名称未找到官方资料 | 官方 | [H1][H15][H14] |
| 账号级网络安全风险 | "For accounts assessed as higher risk, we apply a more conservative model-behavior boundary that refuses a broader range of potentially risky cyber assistance."；GPT-5.6："Some higher-risk biological and cybersecurity requests may be refused or require additional checks." | 官方 | [B2][H1] |

#### 4.2 Codex / Work

| 机制 | 说法 | 类型 | 来源 |
|---|---|---|---|
| 网络安全重路由 | 文档："If a safeguard blocks, reroutes, or limits a request, inspect the available client notice and request logs."；App Server 事件 "`model/rerouted` - `{ threadId, turnId, fromModel, toModel, reason }` when the service routes a request to another model."；源码中 `ModelRerouteReason` **只有一个值 `HighRiskCyberActivity`**（TS：`"highRiskCyberActivity"`） | 官方 + 源码 | [D6][D9][G10] |
| 2026-02 先例 | OpenAI Codex 负责人 Alexander Embiricos（X）："When our systems detect elevated cyber misuse risk, requests may be routed from GPT-5.3-Codex to GPT-5.2"；客户端提示原文 "Your account was flagged for potentially high-risk cyber activity and this request was routed to gpt-5.2 as a fallback…"（源码仍保留该文本用于过滤旧会话）；曾有 ~9% 用户被误标 | 官方人员言论（X）+ 社区 issue | https://x.com/embirico/status/2021376881942200801 ；https://github.com/openai/codex/issues/12136 ；源码 core/src/context/legacy_model_mismatch_warning.rs |
| Astra 安全监控（暂停/终止） | "GPT-6 Astra includes safety monitoring in Codex and ChatGPT Work… can pause a task"；CLI 与移动端、ZDR、非美国存储驻留："Full findings and resume aren't available. The task ends."；API 返回 `misalignment_policy_violation`；网络安全滥用返回 `cyber_policy` | 官方 | [D8][H13][A11][B1] |
| 安全缓冲 | 事件 `model/safetyBuffering/updated`（含 `fasterModel`），响应头 `x-codex-safety-buffering-faster-model` | 官方 + 源码 | [D9][G13] |
| 达限后行为 | "If you reach your usage limits during an active turn, the agent will be able to continue working on that turn, subject to fair use limits."；Plus/Pro 可买 credits，或 banked/购买 reset；**Luna Reserve**："gives selected accounts additional usage with GPT-5.6 Luna after regular usage is exhausted… It doesn't restore regular usage or unlock our most capable models"（仅部分个人 Plus/Pro，Codex 与 Work） | 官方 | [D2][H11][H9][H12] |
| 子任务换模型 | 自动审批复核用 `codex-auto-review`；压缩、记忆整理、子代理可另起请求（`x-openai-subagent`）；用量统计里出现其他模型不一定代表前台请求被换 | 源码 + 社区解释 | [G1][G5]；社区 https://community.openai.com/t/1399447 |
| 2025-11 历史 | 达到 5 小时限额 90% 时提示切换到 GPT-5-Codex-Mini（"up to 4x more usage"）——当前文档未再出现 | 官方（历史） | [H15] |

**社区观察（未经官方确认）**：2026-09-19~21 多名用户报告 Codex 请求 `gpt-6-astra` 而 `response.created` 的 model 字段为 `gpt-5.6-luna`（一名 "Pro 20x" 用户称 9 小时内 247/247 条带 model 字段的响应为 Luna，并称 `x-codex-turn-state` 长度 312 字符时对应 Luna、292 字符时正常）；与官方 Luna Reserve 机制是否同一件事未有官方答复。https://community.openai.com/t/1399447 ；https://community.openai.com/t/1399076 ；https://community.openai.com/t/1399403 。另一用户 10-05~06 抓包审计称"未发现标签层面的静默替换，但路由控制全在服务端，客户端看不到实际模型"https://community.openai.com/t/1403646 。

---

### 5. 自我介绍

| 模型 / 场景 | 典型自述 | 依据 | 官方/社区 |
|---|---|---|---|
| GPT-6 Astra / 6.1 Sol / 6 Sol / 6 Luna 在 Codex 中 | "I'm Codex, (an agent) based on GPT-6"（系统提示首句即此；提示不含 Astra/Sol/Luna，**无法靠自述区分四个 GPT-6 型号**） | 系统提示原文 [G1] | 官方源码（自述内容本身为推断） |
| GPT-5.6 Sol/Terra/Luna、Daybreak 在 Codex 中 | "I'm Codex, based on GPT-5"（注意写的是 GPT-5 而非 5.6） | [G1] | 同上 |
| GPT-5.5 在 Codex 中 | "I'm Codex, a coding agent based on GPT-5" | [G1] | 同上 |
| 旧 GPT-5-Codex / 5.2-Codex | "I'm Codex, based on GPT-5…running in the Codex CLI" | [G12] | 同上 |
| GPT-5.1（旧 Codex 提示） | "I'm GPT-5.1 running in the Codex CLI" | [G12] | 同上 |
| ChatGPT（Chat）被问到当前模型 | 官方承诺 "ChatGPT will continue to tell users which model is active when asked"（2025-10）；GPT-6 训练目标之一 "better at recognizing and communicating the limits of its own capabilities" | [H15][B5] | 官方 |
| Codex 路径下做非编程角色 | gpt-5.5 在保留 Codex 基础提示时自称 "I'm **Codex** acting as your solo tabletop RPG Dungeon Master…" | https://github.com/octopollux/machine-violet/pull/679 | 社区 |
| GPT-6 Astra（Codex/Work）被问知识截止 | 用户称 Astra 回答 "June 2024"，并据此认定被降级（官方截止为 2026-04-30） | https://community.openai.com/t/1397173 | 社区 |
| API 无系统提示时 GPT-6 / GPT-5.x 的自述 | 未找到官方资料；未找到可靠社区实测 | — | — |
| 能否说出自己版本 | 官方未承诺模型能准确说出版本号；官方社区帖作者也提醒不要依赖 "the model's own statements about its identity" | https://community.openai.com/t/1399076 | 社区 |

---

### 6. 档位差异与降智报道

**档位差异（官方）**
- **Pro 专属/限定**：Chat 里的 "Pro" 推理档（GPT-6 Pro = Astra 驱动，发布文称 "GPT‑6 Astra Pro"）只给 Pro $100/$200、Business、Enterprise；Plus 只能在 Work/Codex 用 Astra，"Plus includes Astra in Work and Codex, but not GPT-6 Pro in Chat"[H1][H9][B1]。API 侧对应能力是 `reasoning.mode: "pro"`，另有独立的 `gpt-5.5-pro` 等 Pro 型号 [A9][A6]。
- **Codex 中各档位用的是同一组模型 ID，差别在额度与速度档**：Plus 与 Business Standard 的 5 小时估算完全相同；Pro 三档无 5 小时限额、总额度递增；Premium = 5× Standard 且无 5 小时限额；**Ultrafast 只给 Pro $500 和合格的 Ent/Edu**（"Other self-serve plans don't have access to Ultrafast at launch, even with purchased credits"）[D2][D3][H6][H9]。Free/Go 在 Codex 只能用 Luna（及桌面端限量 GPT-5.6 Terra），GPT-6.1 Sol 不含 Free/Go [D2][D1][P1][H1]。
- **Chat 上下文差异**：Instant 27K（Free）→ 54K（Go/Plus/Business）→ 128K（Pro/Enterprise）；Reasoning 256K（Go/Plus/Business/Enterprise）→ 400K（Pro）[P1][P2]。
- 源码里 `available_in_plans` 很宽（Astra 也列了 `free`、`go`），**服务端授权才算数，不能当权益依据**；plan id 中出现 `prolite`、`promax`、`self_serve_business_prolite` 等，与 Pro 100/500 的对应关系**未找到官方资料**；另外 `gpt-6-sol`、`gpt-6-luna` 的列表里没有 `promax` [G1]。
- **服务条款风险**：Pro 帮助页 "Sharing account credentials, reselling access and automatically extracting data are prohibited. We may restrict access for misuse."；Business/Enterprise 条款禁止 "Reselling access or using ChatGPT to power third-party services" [H2][H6][H8]。官方另有"Sign in with ChatGPT"额度共享给合作应用（"limited preview"，需用户授权、可设单应用上限）[H3][H14]。

**降智 / 限速报道**

| 事件 | 内容 | 类型 | 来源 |
|---|---|---|---|
| GPT-6 Sol/Luna 图像编码 bug | 2026-09-25 官方修复 "a bug in image encoding that degraded image understanding in GPT-6 Sol and GPT-6 Luna"（影响 API 与 Codex 的视觉任务，包括 computer use） | 官方 | [A10] |
| Astra 发布延迟补偿 | 9-03、9-04 两次 banked reset，9-07 自动 reset，"covered the broader Astra launch delay" | 官方 | [H9] |
| Pro 200 额度下调 | 2026-09-29 新订 Pro 200 "include a lower usage allowance…to reflect our increasingly efficient models"；老用户旧额度到 10-29 | 官方 | [H2][H14] |
| GPT-5.2 思考时间调整 | 2026-01~02 下调 Standard/Light 思考时间，并误把 Extended 调低，02-04 恢复；"We periodically adjust the default thinking time" | 官方（历史，说明官方会调推理时长） | [H15] |
| Astra 在 Codex 用量消耗过快 | Plus 用户 20 分钟用完 5 小时额度；单任务 11 分 31 秒耗尽 5 小时额度并用掉 17% 周额度 | 社区（GitHub issue） | https://github.com/openai/codex/issues/43201 |
| Astra→5.6 Luna 路由 | 见 §4.2 社区观察 | 社区（官方论坛帖） | https://community.openai.com/t/1399447 、/1399076 、/1399403 |
| "Selected model is at capacity"、严重降智 | Pro 20x 账号长时间报容量不足（96 楼） | 社区 | https://community.openai.com/t/1397724 |
| Astra 降智 / 4× 限额削减 / 9-12 事后复盘 3 个 bug | 第三方博客称官方员工发布含 3 个 bug 的复盘并在 9 月中将 Astra 限额削减约 4×；**原始复盘与官方削减说明均未找到** | 第三方博客，未核实 | https://explainx.ai/blog/what-happened-to-gpt-6-astra-hype-2026 |
| GPT-5.3-Codex→5.2 网络安全误判 | 2026-02-10 约 9% 用户被误标 | 官方人员 X + 社区 | https://x.com/btibor91/status/2021325846787785016 ；https://github.com/openai/codex/issues/12088 |

---

### 7. 对出题有用的要点（可直接转为质检题）

1. **知识截止（模型身份题）**：Astra / 6.1 Sol = 2026-04-30；6 Sol = 2026-04-20；6 Luna = 2026-05-18；5.6 Sol/Terra/Luna = 2026-02-16；5.5 = 2025-12-01；5.4 / 5.3-Codex / 5.2 = 2025-08-31；chat-latest = 2026-04-20 [A1–A7]。可问 2026 年 2~5 月的事实来区分 6 系 vs 5.6 系 vs 5.5：如 GPT-5.4 发布（2026-03-05）、GPT-5.3-Codex 发布（2026-02-05）、GPT-4o 等从 ChatGPT 退役（2026-02-13）、GPT-5.5 发布（API 2026-04-24）、Pro $100 新档（2026-04-09）[H15][H14][A10]。5.5 不应知道 2026-03 以后的事，6 Luna 应比 6 Sol 多知道 2026-04-20~05-18 的事。注意：Codex 环境可能注入当前时间，且模型可调用 web search，出题时应禁用联网并只问"截止前"事实。
2. **自述不能区分 GPT-6 型号**：Codex 提示只写 "based on GPT-6"；GPT-5.6 系写 "based on GPT-5"。若某账号在 Codex 中自称 "based on GPT-5" 而请求的是 GPT-6 型号 → 疑似被换（但官方提示文本可能随版本变化，需定期核对 models.json）[G1]。
3. **硬证据优先**：读取响应头 `openai-model` 与 `response.created` 里的 `model` 字段；与请求不一致 = 服务端改路由（可能是网络安全重路由、Luna Reserve 或其他原因）[G9][G10][H12]。同时记录 `x-codex-primary/secondary-used-percent` 判断账号是否已到 5 小时/周限额（到限额后可能落入 Luna Reserve）。
4. **effort 能力探针**：Astra 与 6.1 Sol 不接受 `none`（API 返回 400），6 Sol/6 Luna/5.6 系接受；6.1 Sol 不接受 `minimal`；`max` 仅 5.6/6 家族有，5.5/5.4/5.2 没有；Codex 的 `ultra` 只给 Astra/6.1 Sol/6 Sol/5.6 Sol/5.6 Terra，Luna 系"up to Max"[A1–A6][A9][D1][G1]。注意经 ChatGPT 后端时这些校验是否与 API 一致**未核实**，需实测后再当判据。
5. **上下文上限**：Codex 默认 272K（有效约 95%），可配到 872K；API 1,050,000 总 / 922,000 输入 / 128,000 输出；GPT-5.5 在 Codex 仅 272K 且不可扩 [G1][A1–A6]。可用"长文末尾藏密钥"测是否被截短；Chat 侧 Free 27K、Plus 54K、Pro 128K（Instant），Reasoning 256K/400K [P1]。
6. **档位权限题**：Free/Go 账号请求 `gpt-6.1-sol`、`gpt-6-astra`、`gpt-6-sol` 应失败（官方不含），只有 `gpt-6-luna`（桌面）可用；Plus 可用 Astra 但额度很小（5–45 条/5h）；Ultrafast 仅 Pro $500 / 合格 Ent/Edu（API 侧请求值为 `service_tier: "ultrafast"`；Codex 公开目录 models.json 里没有 ultrafast 档，ChatGPT 后端的请求写法未核实）；Ent/Edu 新模型默认关 [D2][D1][D3][D4][P1][A2]。
7. **退役日期题**：GPT-5.5 于 **2026-10-14** 退出 ChatGPT/Work/Codex（API 保留）；GPT-5.4/5.4-mini 2026-08-31 退出 ChatGPT 登录的 Codex；GPT-5.3-Codex-Spark 2026-09-14 退役 [D1][D7]。10-14 之后若 ChatGPT 账号仍能跑 `gpt-5.5`，说明不是正常的 ChatGPT OAuth 渠道。
8. **速度题**：Fast 对 Astra/6.1 Sol 标称 2×、对 5.6/5.5 1.5×；Ultrafast 标称最高 8× token 生成速度 [D3][G1]。
9. **安全类题目要避开**：网络安全相关提示可能触发重路由（`highRiskCyberActivity`）、`cyber_policy` 拒绝或 Astra 任务被暂停/终止，会污染质检结果，也可能让卖家账号被标记 [D6][D8][G10][B2]。

---

### 8. 未找到官方资料 / 存疑清单

1. **Free/Go 的 Chat 模型说法冲突**：10-07 发布说明与博客写 "Free and Go use GPT-6 Luna" [H14][B5]；H1（更新于约 10-09）仍写 "Free and Go use GPT-5.6 Luna"、"Think uses GPT-5.6 Luna"。以哪条为准未定，建议实测。
2. **GPT-6 Pro 是否含 Pro $500**：官方句子只列 "Pro $100, Pro $200, Business and Enterprise"[H1][H6][H8]，未提 Pro $500（疑为遗漏）。
3. **Plus 的 Chat Thinking 条数、Pro 各档具体倍数**（Pro 100/200/500 相对 Plus 的 Codex 倍数）：未公布。2026-04 曾写 Pro $100 "up to 10x more Codex usage than Plus (up from 5X…)"，现行倍数未找到；社区常用 "Pro 20x" 说法，未找到官方出处。
4. **GPT-5.4 退役后的替代建议不一致**：开发者文档写换成 `gpt-6-sol` / `gpt-6-luna` [D1][D4]；帮助中心 H11 与 Codex changelog 2026-08 写换成 `gpt-5.6-terra` / `gpt-5.6-luna`（发布于 GPT-6 之前的旧文本）。
5. **Codex 定价页与 Business 帮助页数字差异**：GPT-6.1 Sol 本地消息/5h 一处写 15–160 [D2][H9]，一处与 6 Sol 合并写 15–150 [H6]。
6. **gpt-5.3-chat-latest 页面自相矛盾**："128,000 context window" 却又 "Maximum input tokens: 272,000" [A7]。
7. **GPT-6 Astra 的 API 默认 effort**：模型页未写；仅 Codex 目录默认 low [A1][G1]。
8. **ChatGPT 后端是否校验 instructions、是否强制 store=false / stream=true**：无官方说明，仅社区观察（见 §3.4）；0.162.0 起官方客户端改用 developer 消息后的校验现状未核实。
9. **安全路由在 2026 年的目标模型名**（是否仍有 `gpt-5-chat-safety` 一类专用模型）：未找到官方资料；官方仅说 "can still switch automatically for safety"。
10. **GPT-6 时代 Chat 的 Instant 达限 fallback 模型名**：未找到（历史上为 GPT-5.3 Instant Mini / GPT-5.4 mini）。
11. **Astra→GPT-5.6 Luna 路由报告**与 Luna Reserve 的关系、是否有官方答复：未找到官方说明。
12. **"Astra 限额被削减 4×"、"9-12 官方复盘 3 个 bug"、"GPT-6.1 Astra 取消发布"**：仅第三方博客，未找到原始来源。
13. **API 无系统提示时 GPT-6 / 5.6 的自述**：未找到官方或可靠社区实测。
14. **models.json 中 plan id（`prolite`、`promax` 等）与官方档位的对应**；`max_context_window`（872K）在 ChatGPT 登录下是否真的可用（服务端是否接受）：未核实。
15. **地区价**：只看到新加坡页面（Go 11 / Plus 30 / Pro 起 138，SGD），其他地区未查。
16. **Edu 的 Chat 上下文窗口**：定价页未单列。

## 第 3 章 Google Gemini

> 调研范围：Google Gemini（Gemini App 订阅、Gemini API、Gemini CLI、Gemini Code Assist、Google Antigravity）与 xAI Grok（grok.com 订阅、X Premium、xAI API、Grok Build）。
> 访问日期：所有来源统一记为 **2026-10-11**。
> 抓取说明：WebFetch 无法访问 ai.google.dev / x.ai（域名校验失败），因此改用本机系统代理（127.0.0.1:7890，出口 IP 在新加坡）执行 `curl` 抓原始页面。ai.google.dev 与 docs.x.ai 都提供 `.md` 版本。gemini.google 的订阅页按 IP 显示地区价格，美元价格取自 `/us/` 路径。x.ai/pricing 和 help.x.com 用 ego-browser 只读打开；grok.com 被 Cloudflare 人机验证拦截，未绕过。
> 标注约定：「官方」= 厂商官网、文档或官方 GitHub；「源码观察」= 读开源代码得到的事实；「社区观察，未经官方确认」= 媒体、论坛或第三方站点的说法。

### 0. 2026-10 的关键变化（均以官网为准）

| 变化 | 官方原文（节选） | 来源 |
|---|---|---|
| Gemini App 从 2026-05-17 起不再按"每日 N 条 prompt"限额，改为算力计量：每 5 小时刷新一次，另有每周上限 | "Gemini Apps have compute-based usage limits … Your limit refreshes every 5 hours until you reach your weekly limit." | https://support.google.com/gemini/answer/16275805 |
| 2026-10 起：免费用户默认走 Auto 自动选模型（多数请求落到 Flash-Lite）；AI Plus 失去 Pro | "These changes will start to take effect for users without an AI subscription on October 9th." / "Most prompts will go to Flash-Lite, our fastest model." | https://support.google.com/gemini/answer/17004136 |
| 个人账号用 Gemini CLI 或 Code Assist 时，"Login with Google" 从 2026-06-18 起停止服务（覆盖 Code Assist 个人免费版、AI Pro、AI Ultra） | "Starting June 18, 2026, Gemini Code Assist IDE extensions stopped serving requests for the Gemini Code Assist for individuals, Google AI Pro, and Google AI Ultra tiers. This also applies to usage of Gemini CLI." | https://developers.google.com/gemini-code-assist/docs/deprecations/code-assist-individuals |
| Code Assist Standard / Enterprise 从 2026-10-09 起停售，后续由 Antigravity 承接 | "Starting October 9, 2026, you can no longer purchase new Gemini Code Assist Standard or Enterprise subscriptions." | https://docs.cloud.google.com/gemini/docs/codeassist/sunset |
| 当前 API 主力是 3.8 Flash（GA）；Pro 档仍只有 3.1 Pro（Preview）；Gemini 4 Argon 已发布但尚未开放 | "Gemini 3.8 Flash (`gemini-3.8-flash`) is generally available (GA)"；Argon "is rolling out to a set of trusted cyber defenders through our Fairwind Program" | https://ai.google.dev/gemini-api/docs/latest-model ；https://blog.google/innovation-and-ai/models-and-research/gemini-models/gemini-4-argon/ |
| 官方会把旧 ID 自动路由到新模型：3.5 Flash 转到 3.6 Flash，3.7 Flash 转到 3.8 Flash，3 Pro Preview 转到 3.1 Pro | "All requests to `gemini-3.7-flash` are automatically routed to `gemini-3.8-flash`." | https://ai.google.dev/gemini-api/docs/changelog （2026-10-08 条目） |
| 从 3.6 Flash / 3.5 Flash-Lite 起，temperature / top_p / top_k 被废弃（服务端忽略）；请求以 model 角色预填结尾会返回 400 | "`temperature`, `top_p`, and `top_k` are deprecated and ignored." / "If the last non-empty turn in the request is a `model` turn, the API returns a `400` error." | https://ai.google.dev/gemini-api/docs/whats-new-gemini-3.6 |

### 1. 订阅档位对比

| 档位 | 月价（美国） | Gemini App 可用模型 | 用量限额 | 上下文 | Gemini CLI / Code Assist / Antigravity 可用性与配额 | 来源 |
|---|---|---|---|---|---|---|
| 免费（无 AI 计划） | $0 | 订阅页："Access to 3.5 Flash-Lite"。2026-10-09 起改为 **Auto**：自动选模型，"Most prompts will go to Flash-Lite … Prompts that need deeper reasoning may go to Flash or Pro"；如果关闭自动选择，所有回答都用 Flash-Lite | "Standard limits"，按算力计量，每 5 小时刷新，另有周上限。官方不再公布每日条数 | **32k tokens** | **Gemini CLI / Code Assist 个人版**：2026-06-18 起停止服务。**Antigravity**：与 Plus 同列，"A meaningful quota, refreshed weekly" + "A weekly rate limit"。可用 Gemini 3.8 / 3.7 / 3.6 Flash、3.1 Pro、Claude Sonnet 4.6 和 Opus 4.6（thinking）、GPT-OSS-120b；后三个将于 2026-11-02 下线 | 16275805；17004136；gemini.google/us/subscriptions；antigravity.google/docs/models、/docs/plans |
| Google AI Plus | **$4.99** | 订阅页仍写 "Access to 3.6 Flash / Access to 3.1 Pro"。但 10 月新规是 **Flash-Lite ✓、Flash ✓、Pro ✗**，生效日期以邮件通知为准。存储 400 GB | 标准限额的 **2 倍** | **128k** | **Gemini CLI**：官方文档写 "Tiers not listed above, including Google AI Plus, are not supported"，且个人登录已于 6-18 停止。**Antigravity**：与免费档相同（每周刷新） | 同上；github.com/google-gemini/gemini-cli docs/resources/quota-and-pricing.md |
| Google AI Pro | **$19.99** | "Access to 3.8 Flash / Access to 3.1 Pro"。官方博客："3.8 Flash is available to Google AI Pro and Ultra subscribers across the Gemini app"。限额页仍写 Deep Think 仅限 Ultra（Pro 是否获得 Deep Think 见第 5 节） | 标准限额的 **4 倍** | **1M tokens** | **Gemini CLI / Code Assist**：6-18 停止服务（停之前 CLI 文档写 Pro 为 1,500 次/天）。**Antigravity**："A high, generous quota, refreshed every five hours until the weekly limit is reached"，周上限更高。可用 **Claude Sonnet 5.5 / Opus 5.5（thinking）**，但 "Available on Google AI Pro for non-trial subscriptions only"。基础额度用完后可用购买的 AI credits 继续 | 16275805；gemini.google/us/subscriptions；blog.google 3.8 Flash 文；antigravity.google/docs/models、/docs/plans |
| Google AI Ultra（5x） | **$99.99** | 3.8 Flash、3.1 Pro、**Deep Think**（"Deep think (AI Ultra only) … Deep think requires the Pro model"）、Gemini Spark。Gemini 4 Argon 官方表述为日后 "starting with paid API customers and Google AI Ultra subscribers"，截至 10-11 只对 Fairwind 计划开放 | Pro 限额的 **5 倍** | 1M | **Antigravity**："Ultra 5x: Higher rate limits to agent model"。文档："The highest, most generous quota, refreshed every five hours / The highest weekly rate limits / Access to third-party models"。注意：**Ultra 不提供 Claude 4.6 系列**（文档表格中为 ❌），只提供 Claude 5.5 系列 | gemini.google/us/subscriptions；16345172；antigravity docs |
| Google AI Ultra（20x） | **$199.99** | 同 Ultra 5x，另加 Project Genie（只有 Ultra 有） | Pro 限额的 **20 倍** | 1M | "Ultra 20x: Highest rate limits to agent model in Google Antigravity" | gemini.google/us/subscriptions |
| Workspace 企业版：Code Assist Standard / Enterprise | 已于 2026-10-09 停售；存量合同仍可续约至 2027 | — | CLI 与 agent mode 合并计数：**Standard 1,500、Enterprise 2,000 次/人/天**。代码类请求 6,000 次/天，chat 类 960 次/天，每秒 2 次 | Code Assist 本地代码库感知：1,000,000 token | 企业版 Gemini CLI 不受 6-18 停服影响："your access remains unchanged" | docs.cloud.google.com/gemini/docs/quotas（页面更新于 2026-10-09）；developers.googleblog.com 迁移公告 |

补充（Gemini App 的模型选择与思考档位，均为官方）：
- 思考档位存在两套名称。限额页写 Standard thinking（默认）/ Extended thinking / Deep think（仅 Ultra）。"About Gemini models" 页写可选 Low / Medium / High，且 "If you don't select a thinking level, it will default to Low"。10 月新规：每个模型都可选 low / medium / high effort。来源：16275805、18648722、17004136。
- **自动选模型**："When auto model selection is on, your prompts are automatically routed to the most efficient model and thinking level"。要确认某条回答由哪个模型生成：点开 "Show thinking steps"，拉到底部可以看到模型名。来源：https://support.google.com/gemini/answer/18648722
- **官方降级**："If you have a Google AI plan and reach your usage limit, your chat will continue with Flash-Lite." 来源：18648722、16275805
- Ultra 价格变动见 Gemini App 发布说明 2026-05-19："Google AI Ultra subscriptions now starting at $100 USD/month"。来源：https://gemini.google/release-notes/

### 2. 模型参数

上下文与输出上限取自各模型页（ai.google.dev/gemini-api/docs/models/<id>）；知识截止取自模型卡（deepmind.google/models/model-cards/…）或 Gemini 3 开发指南；thinking 参数取自 thinking 文档（generateContent 版）。

| 模型名 | API 模型 ID（发布与状态） | 输入上限 | 输出上限 | 知识截止 | thinking 参数（取值 / 默认 / 能否关闭） | 可用渠道与档位 | 价格要点（每百万 token，付费层） | 来源 |
|---|---|---|---|---|---|---|---|---|
| Gemini 3.8 Flash | `gemini-3.8-flash`（GA，2026-09-02） | 1,048,576 | 65,536 | **2026-03**，原文："The knowledge cutoff date for Gemini 3.8 Flash is March 2026 – … in others they may experience the model's knowledge is limited to January 2025" | `thinking_level`：low / **medium（默认）** / high。**传 minimal 会报错**（"`minimal` is not supported and returns an error"）。无法关闭思考 | API（有免费层）；Antigravity 所有档；Gemini App 仅 Pro / Ultra；Antigravity Agent 默认模型 | 推广价 $0.75 / $3.75（至 2026-12-31），2027-01-01 起 $1.50 / $7.50 | models/gemini-3.8-flash；latest-model；model card gemini-3-8-flash；pricing |
| Gemini 3.7 Flash | `gemini-3.7-flash`（GA 2026-08-13；2026-10-08 废弃，**请求自动路由到 3.8 Flash**） | 1M | 64K | 2026-03（模型卡原文同上） | （已被路由，按 3.8 Flash 处理） | Antigravity 模型列表仍显示 3.7 Flash | — | deprecations；changelog；model card gemini-3-7-flash |
| Gemini 3.6 Flash | `gemini-3.6-flash`（GA，2026-07-21） | 1,048,576 | 65,536 | 2026-03（模型卡原文同上） | minimal / low / **medium（默认）** / high；无法完全关闭 | API；Gemini App 上线时向全部用户开放，现订阅页列在 Plus 档；Antigravity | 标准价 $1.50 / $7.50。定价页显示至 2026-12-31 推广价 $0.75 / $3.75 | models/gemini-3.6-flash；whats-new-gemini-3.6；pricing |
| Gemini 3.5 Flash | `gemini-3.5-flash`（GA 2026-05-19；2026-10-08 废弃，**请求自动路由到 3.6 Flash**） | 1M | 65k | 未找到官方资料（模型卡未写截止日期） | 默认 effort 由 3 Flash 的 high 改为 **medium**；minimal / low / medium / high | 已被路由 | 原价 $1.50 / $9.00 | generate-content/whats-new-gemini-3.5；changelog |
| Gemini 3.5 Flash-Lite | `gemini-3.5-flash-lite`（GA，2026-07-21） | 1,048,576 | 65,536 | 2026-03（模型卡原文同上） | **minimal（默认）** / low / medium / high | API；Gemini App 免费档（"Access to 3.5 Flash-Lite"），订阅用户限额用尽后也回落到 Flash-Lite | $0.30 / $2.50 | models/gemini-3.5-flash-lite；whats-new-gemini-3.6；model card |
| Gemini 3.1 Pro（Preview） | `gemini-3.1-pro-preview`（2026-02-19）；另有 `gemini-3.1-pro-preview-customtools`；`gemini-3-pro-preview` 自 2026-03-09 起指向 3.1 Pro | 1,048,576 | 65,536 | **2025-01**（Gemini 3 开发指南表格："gemini-3.1-pro-preview … Jan 2025"） | low / medium / **high（默认）**；minimal 不支持；"You cannot disable thinking for Gemini 3.1 Pro." | API **无免费层**；Gemini App 的 "Pro"（Pro / Ultra；Plus 将被移除）；Antigravity 所有档（只有 Low / High 两档，默认 High） | ≤200k：$2 / $12；>200k：$4 / $18 | models/gemini-3.1-pro-preview；gemini-3（已标废弃页）；thinking；pricing；antigravity docs/models |
| Gemini 3.1 Flash-Lite | `gemini-3.1-flash-lite`（GA 2026-05-07；最早 2027-05-07 下线） | 1,048,576 | 65,536 | 2025-01（Gemini 3 指南表格） | **minimal（默认）** / low / medium / high | API | $0.25 / $1.50 | models/gemini-3.1-flash-lite；deprecations |
| Gemini 3 Flash（Preview） | `gemini-3-flash-preview`（2025-12-17） | 1,048,576 | 65,536 | 2025-01 | minimal / low / medium / **high（默认）** | API（有免费层） | $0.50 / $3.00 | models/gemini-3-flash-preview；gemini-3 指南 |
| Gemini 3 Pro Preview | `gemini-3-pro-preview`（2025-11-18；**2026-03-09 关停，现指向 3.1 Pro**） | — | — | 2025-01 | low / high（默认 high） | — | — | changelog；thinking |
| Gemini 2.5 Pro | `gemini-2.5-pro`（2025-06-17） | 1,048,576 | 65,536 | 2025-01 | `thinkingBudget` 取值 **128–32768**；默认 -1（动态）；"N/A: Cannot disable thinking" | 2026-09-18 起**只对过去用过的账号开放**（"limiting access to the 2.5 models to users who have actively used them in the past"） | ≤200k：$1.25 / $10；>200k：$2.50 / $15 | models/gemini-2.5-pro；generate-content/thinking；changelog |
| Gemini 2.5 Flash | `gemini-2.5-flash` | 1,048,576 | 65,536 | 2025-01 | **0–24576**；0 关闭思考；默认 -1（动态） | 同上，限老用户 | $0.30 / $2.50 | 同上 |
| Gemini 2.5 Flash-Lite | `gemini-2.5-flash-lite` | 1,048,576 | 65,536 | 2025-01 | **512–24576**；默认不思考；0 关闭；-1 开启动态 | 同上，限老用户 | $0.10 / $0.40 | 同上 |
| Gemini 4 Argon | **API 模型列表中没有 ID**（2026-09-30 发布，尚未开放） | 未公布 | **1M**（"expanding the model's output token limit to an industry-leading 1M tokens, up from the previous 64K"） | 未公布 | 未公布 | 目前只对 Fairwind 计划开放；之后 "starting with paid API customers and Google AI Ultra subscribers" | 推广价 $2 / $10（缓存输入打 5% 价）；之后 $4 / $20 | blog.google gemini-4-argon |
| Gemini 3 Deep Think（App 功能） | 不是 API 模型 | — | — | — | App 的思考档 "Deep Think"，需先选 Pro 模型 | 只有 Ultra（另有媒体报道 Pro 将加入，见第 5 节） | — | 16345172；release-notes 2026-02-12 |

thinking 相关的补充规则（均为官方）：
- `thinking_level` 和旧参数 `thinking_budget` 不能出现在同一请求里，否则返回 400。Gemini 3 系列仍接受 `thinking_budget` 以保持兼容，但官方提示用于 3 Pro 时 "may result in unexpected performance"。
- `max_output_tokens` 包含思考 token。思考阶段触顶时，返回 `finish_reason: MAX_TOKENS`（Interactions API 中状态为 `incomplete`），已生成的思考 token 照常计费。
- OpenAI 兼容层会把 `reasoning_effort` 映射为 `thinking_level`。来源：gemini-3 指南。
- **存疑**：Interactions API 版 thinking 页的表格写 2.5 系列支持 `thinking_level` low / medium / high；generateContent 版却写 "Gemini 2.5 series models don't support `thinkingLevel`; use `thinkingBudget` instead"。见第 10 节。

### 3. 订阅渠道的特殊要求（端点与系统提示开头原文）

| 渠道 | 认证与端点 | 系统提示开头原文 | 现状与限制 | 来源 |
|---|---|---|---|---|
| **Gemini CLI**（Login with Google） | `https://cloudcode-pa.googleapis.com`，版本 `v1internal`，方法包括 `streamGenerateContent`、`generateContent`、`loadCodeAssist`、`onboardUser`、`retrieveUserQuota`（源码常量 `CODE_ASSIST_ENDPOINT`、`CODE_ASSIST_API_VERSION`）。Code Assist 后端的模型名与 API 不同：例如 `gemini-3-flash`（不带 -preview）；实验开关打开时，`gemini-3.5-flash` 会被改写成 `gemini-3.8-flash` | **Gemini 3 系列（modern prompt）**："You are Gemini CLI, an interactive CLI agent specializing in software engineering tasks. You are currently operating in **Default** mode. Your primary goal is to help users safely and effectively."（非交互模式开头改为 "You are Gemini CLI, an autonomous CLI agent specializing in software engineering tasks."）<br>**Gemini 2.5（legacy prompt）**："You are an interactive CLI agent specializing in software engineering tasks. Your primary goal is to help users safely and efficiently, adhering strictly to the following instructions and utilizing your available tools." | 个人账号（Code Assist 个人版、AI Pro、AI Ultra）**自 2026-06-18 起停止服务**；企业版 Code Assist 和 API key / Vertex 仍可用。默认生成参数：Gemini 3 为 `thinkingLevel: HIGH`、`includeThoughts: true`、temperature 1、topP 0.95、topK 64；2.5 为 `thinkingBudget: 8192`（源码注释 "Cap the thinking at 8192 to prevent run-away thinking loops"）。额度用尽时会触发模型回退，内部工具类调用会静默回退（2.5-flash-lite → 2.5-flash → 2.5-pro） | 源码观察：google-gemini/gemini-cli main@9b6e0265d1（2026-10-09），v0.63.0（2026-10-06）。文件：packages/core/src/prompts/snippets.ts、snippets.legacy.ts、config/models.ts、config/defaultModelConfigs.ts、code_assist/server.ts、docs/cli/model-routing.md |
| **Gemini Code Assist**（IDE 插件） | 与 CLI 共用后端和配额 | 未找到官方资料 | 个人版于 6-18 停服；企业版于 10-09 停售 | 见第 1 节 |
| **Google Antigravity**（2.0 桌面版 / IDE / CLI `agy` / SDK） | 浏览器 OAuth 登录个人 Google 账号；SSH 环境用手动 URL 加验证码；企业版走 GCP SSO / WIF / ADC。也可以用 Gemini API key：在 `~/.gemini/antigravity-cli/settings.json` 设置 `"modelProvider": "gemini"`，再设 `GEMINI_API_KEY`；用 `GOOGLE_GEMINI_BASE_URL` 可指定兼容端点。官方未公布订阅后端的端点。sub2api v0.2.14 源码使用 `https://cloudcode-pa.googleapis.com` 与 `https://daily-cloudcode-pa.googleapis.com` 的 `v1internal:*`（包括 `fetchAvailableModels`、`loadCodeAssist`），属于源码观察 | 客户端闭源，官方未公开。社区泄露版（2025-11，Antigravity 1.x 时期）开头："You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding."（社区观察，未经官方确认：github.com/x1xhlol/system-prompts-and-models-of-ai-tools Google/Antigravity/Fast Prompt.txt）。**sub2api v0.2.14 会在请求里注入同一段身份文本**（`antigravityIdentity`，见 backend/internal/pkg/antigravity/request_transformer.go:194），并把用户 system prompt 里 Claude 身份的开头替换为 "You are an AI agent."（`neutralizeClaudeIdentity`）（源码观察） | **服务条款风险（官方 FAQ 原文）**："Using third-party software, tools, or services to access Antigravity is a violation of our Terms of Service … Such actions can result in suspension or termination of your account." 另有："automating account rotation, is not supported under consumer plans."。配额分两个池："Gemini Models" 与 "Claude and GPT models"，各自有 Weekly Limit 和 Five Hour Limit。官方未公布具体数值（"rate limits correlate with the amount of work done by the agent"）。`/boost` 多 agent 深度推理仅限付费档 | antigravity.google/docs/faq、/docs/plans、/docs/models、/docs/cli/install、/docs/boost；sub2api 源码（本机 ~/project/github-fork/sub2api tag v0.2.14，只读） |
| Antigravity 各模型可选思考档 | — | — | Gemini 3.8 / 3.7 / 3.6 Flash：Low / Medium（默认）/ High（UI 标注 "Fast"）。Gemini 3.1 Pro：Low / High（默认 High）。Claude 显示为 "(Thinking)"。GPT-OSS 120B 显示为 "(Medium)" | antigravity.google/docs/models |

### 4. 自我介绍

| 场景 | 预期自我介绍（关键句） | 依据类型 | 质检注意 |
|---|---|---|---|
| Gemini API 裸调用（无 system prompt） | 未找到官方资料 | — | 模型多半知道自己是 Google 的 Gemini，但未必报得准小版本号。版本号检测不要只靠自述，应结合截止日期和参数行为 |
| Gemini App | 官方未公开系统提示 | — | 页面上可以查看实际所用模型：Show thinking steps 的底部 |
| Gemini CLI | "You are Gemini CLI, an interactive CLI agent …"（Gemini 3）；2.5 版本没有 "Gemini CLI" 字样 | 源码观察（官方开源仓库） | 走 CLI 渠道时，模型可能自称 "Gemini CLI" |
| Antigravity（经 sub2api） | "You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team …" | 社区泄露 + sub2api 源码 | **重要**：经 Antigravity 路由的 **Claude 模型**也会收到这段身份文本，可能自称 Antigravity 或 Google DeepMind 出品。不能据此判定"模型被替换" |
| Gemini 3.8 Flash Cyber | 未找到官方资料 | — | 仅对 Fairwind 计划开放，普通账号不会出现 |

### 5. 档位差异与降智报道

**官方明文的降级或替换机制**（质检时要列入白名单，或单独监控）：
1. Gemini App 自动选模型会同时调整模型和思考档。免费用户从 10-09 起默认 Auto，多数请求落到 Flash-Lite；订阅用户用尽限额后回落到 Flash-Lite；限额用尽时 Deep Think 暂时不可用。来源：17004136、18648722、16345172。
2. API 层的自动路由：3.5 Flash → 3.6 Flash，3.7 Flash → 3.8 Flash，3 Pro Preview → 3.1 Pro。2.5 系列只对老用户开放。来源：changelog、deprecations。
3. Gemini CLI 在配额或服务出错时回退到备用模型；`/model` 不影响子 agent："the `/model` command … does not override the model used by sub-agents"。来源：docs/cli/model.md、model-routing.md。
4. App 的限额随容量动态变化："Limits may change without notice, including due to capacity constraints … limits for users without a Google AI plan may be limited before users with a plan." 来源：16275805。

**媒体或社区报道（社区观察，未经官方确认）**：
- 9to5Google（2026-10-03）报道 AI Pro 将获得 Deep Think，免费档实际只剩 Flash-Lite（报道表格中免费档的 Flash 和 Pro 均为 X）。但官方 17004136 页面写的是免费档 "Auto"（可能路由到 Flash 或 Pro），限额页仍写 "Deep think (AI Ultra only)"。来源：https://9to5google.com/2026/10/03/gemini-model-limits-oct-26/
- Gemini 3.5 Pro 据报被取消（SemiAnalysis、Android Authority；Google 官方未确认）。截至 10-11，Pro 档仍只有 3.1 Pro（2026-02）。来源：https://9to5google.com/2026/09/30/gemini-4-argon-announcement/ ；搜索结果 nokiapoweruser.com、geeky-gadgets.com（仅看到搜索摘要）。
- The Register 称 3.8 Flash 做 agent 任务时 token 和轮次更多，单任务成本约高 40%（仅看到搜索摘要，未打开原文）。官方也写了 "Gemini 3.8 Flash can use more tokens on longer running and complex tasks, by design"（latest-model 页）。来源：https://www.theregister.com/ai-and-ml/2026/09/02/with-gemini-38-flash-google-reminds-everyone-its-still-in-the-race/5294049
- 本次时间有限，没有系统检索 Reddit / HN 上的"降智"帖。

## 第 4 章 xAI Grok

> 公司名变化：xAI 已于 2026-02-02 并入 SpaceX（x.ai/news/xai-joins-spacex）。现在的文档和新闻自称 "SpaceXAI"、"SpaceXSI"，API 域名仍为 api.x.ai。

### 6. 订阅档位对比

| 档位 | 月价（美国） | Grok App 可用模型与功能（官方 x.ai/pricing 对比表） | 限额 | Grok Build / 第三方工具 | 来源 |
|---|---|---|---|---|---|
| Free | $0 | **Grok 4.7 ✓**、Imagine 生图 ✓、视频 ✗、Grok Build ✓、Grok Bot ✗、实时 web 与 X 搜索为 "Limited"、Expert ✗ | 免费层的 Chat 和 Voice 限额单独计算 | Build 所有档都可用（"Grok Build is now available on every plan"，2026-08-19） | x.ai/pricing（ego-browser 只读）；x.ai/news 列表 |
| SuperGrok Lite | **官方页未显示价格**（第三方称 $10/月，存疑） | Grok 4.7 ✓、视频 ✓、Expert ✓、Grok Bot ✗ | 每周用量池（未公布数值） | — | x.ai/pricing 对比表 |
| SuperGrok | **$30** | 卡片原文："Grok 4.7 model / Grok Bot access / Connectors / Higher rate limits across all features / Expert / … Image and video generation" | 自 2026-06 起，所有产品共用一个每周用量池："one shared weekly usage pool … across any Grok product"，用量按产品拆分为 API、Build、Chat、Imagine、Voice。超出后可购买 Extra Usage Credits（最低 $5）或开启 Auto Top Up | **官方允许第三方工具用订阅 OAuth 登录**：OpenCode（"xAI Grok OAuth (SuperGrok Subscription)" 和 "Headless / Remote / VPS" 两种方式）、OpenClaw、Kilo Code、Warp、Hermes Agent | x.ai/pricing；docs.x.ai/grok/faq；x.ai/news/grok-opencode 等 |
| SuperGrok Plus | **$100** | "Everything in SuperGrok, plus: Create 1080p videos / Significantly higher usage across Chat, Imagine, Voice & Build / Lightning-fast replies / Priority access at peak times / Early access to new features" | 更大的周用量池 | 同上 | x.ai/pricing |
| SuperGrok Heavy | **官方页未显示价格**（第三方称 $300/月，存疑） | Grok 4.7 ✓、Grok Bot ✓、Priority support ✓。对比表没有单独列出 "Grok 4 Heavy" 之类的模型 | 最大用量池（未公布数值） | Grok Bot 可以关联 "SuperGrok, SuperGrok Plus, or SuperGrok Heavy" | x.ai/pricing；docs.x.ai（Grok Bot 文档） |
| X Premium（Basic / Premium / Premium+） | Basic $3（年付 $32）；**Premium $8（$84/年）**；**Premium+ $40（$395/年）** | Premium："increased usage limits on Grok"。Premium+："SuperGrok access, Grok Bot"。Basic 没有提到 Grok | 未公布 | Grok Build 早期 beta 面向 "all SuperGrok and X Premium Plus subscribers"（2026-05-25）；OpenCode / OpenClaw 支持 "SuperGrok or X Premium subscription" | help.x.com/en/using-x/x-premium（ego-browser 只读）；x.ai/news/grok-build-cli、grok-opencode |
| Business / Enterprise | 页面只有 "Get Business" 和 "Contact"，未标价 | 只有 Business 和 Enterprise 标 "No training ✓" | 自定义 | — | x.ai/pricing |

Grok Build 的认证与端点（官方 docs.x.ai/build/enterprise）：浏览器 OIDC（`grok login`）或设备码（`grok login --device-auth`），认证域为 `auth.x.ai`；订阅路径的推理代理为 `cli-chat-proxy.grok.com`；API key 路径直连 `api.x.ai`。客户端开源：github.com/xai-org/grok-build（2026-07-15 公告开源）。

### 7. 模型参数

| 模型名 | API 模型 ID / 别名 | 上下文 | 最大输出 | 知识截止 | reasoning 参数 | 渠道 | 价格（<200k / ≥200k，每百万 token：输入 / 缓存 / 输出） | 来源 |
|---|---|---|---|---|---|---|---|---|
| Grok 4.7 | `grok-4.7`（2026-09，"most capable model"） | **500,000** | **"No text output limit"** | **2026-05** | `reasoning.effort`：low / medium / **high（默认）** / xhigh。**"Reasoning cannot be disabled."**。`presencePenalty`、`frequencyPenalty`、`stop` 会报错。Responses API 总是返回 `reasoning.encrypted_content` | xAI API（Responses）；Grok Build 默认模型；Cursor；grok.com 所有档（pricing 对比表）。Grok 4.7 Fast 只在 Cursor 和 Grok Build 提供，价格为 2 倍 | $2 / $0.50 / $6；$4 / $1 / $12。限速 150 RPS、50M TPM | docs.x.ai/developers/grok-4-7；/developers/models/grok-4.7；/reasoning；release-notes |
| Grok 4.6 | `grok-4.6`（2026-08-12） | 500,000 | 无输出上限（release notes） | 未找到官方资料 | low / medium / high（默认）/ xhigh；不可关闭 | API；Bedrock、Foundry、Gemini Enterprise Agent Platform、GitHub Copilot | 同 4.7 | /developers/models/grok-4.6；release-notes |
| Grok 4.5 | `grok-4.5`；别名 `grok-4.5-latest`、`grok-build-latest` | 500,000 | 未找到官方资料 | 未找到官方资料 | **文档冲突**：reasoning 页写 low / medium / high（默认），并称 "requests with "xhigh" are treated as "high""；模型页却列出了 xhigh | API；2026-07-22 起成为 grok.com、X、iOS、Android 的默认模型（"Grok 4.5 now powers Grok wherever you use it"），之后被 4.7 取代 | $2 / $0.30 / $6；$4 / $0.60 / $12 | /developers/models/grok-4.5；x.ai/news/grok-4-5-everywhere |
| Grok 4.3 | `grok-4.3`，别名 `grok-4.3-latest` | **1,000,000** | 未找到官方资料 | 未找到官方资料 | 模型页：**none** / low / medium / high / xhigh，**默认 low**。迁移指南却写 "4 reasoning effort levels (none, low, medium, and high)"，存疑 | API；**已退役 slug 的重定向目标** | $1.25 / $0.20 / $2.50；$2.50 / $0.40 / $5。Batch 打 8 折 | /developers/models/grok-4.3；migration/may-15-retirement |
| Grok 4.20（推理 / 非推理两个变体） | `grok-4.20-0309-reasoning`（别名 `grok-4.20`、`grok-4.20-reasoning` 等十余个）；`grok-4.20-0309-non-reasoning` | 1,000,000 | 未找到官方资料 | 未找到官方资料 | 推理与非推理是两个独立 slug。grok-4.20 及更新模型忽略 `logprobs` 和 `top_logprobs` | API | $1.25 / $0.20 / $2.50 | /developers/models.md；/developers/models/grok-4.20-0309-reasoning |
| Grok 4.20 Multi-agent | `grok-4.20-multi-agent-0309` | 1,000,000 | — | — | `reasoning.effort` 控制的是 **agent 数量**（4 或 16），而不是推理深度 | API | 同 4.20 | /reasoning；/multi-agent |
| Grok Build 0.1 | `grok-build-0.1`；别名 **`grok-code-fast-1`、`grok-code-fast`、`grok-code-fast-1-0825`** | **256,000** | 未找到官方资料 | 未找到官方资料 | Reasoning: Yes（文档未列 effort 取值） | API（public beta，2026-05-29） | $1 / $0.20 / $2；$2 / $0.40 / $4 | /developers/models/grok-build-0.1 |
| 已退役（2026-05-15 12:00 PT） | `grok-4-0709`、`grok-4-fast-(non-)reasoning`、`grok-4-1-fast-(non-)reasoning`、`grok-3`、`grok-code-fast-1` | — | — | — | **重定向规则**：推理类 → `grok-4.3` + low；非推理类和 grok-3 → `grok-4.3` + none；grok-code-fast-1 → `grok-build-0.1`。按 grok-4.3 计价 | — | — | /developers/migration/may-15-retirement |
| Grok 5 | **未发布**：docs.x.ai 模型表和 x.ai/news 列表（截至 2026-09-28）中都没有出现 | — | — | — | — | — | — | 官方页面中未见 |

### 8. 自我介绍与社区观察

| 场景 | 预期自我介绍 | 依据 |
|---|---|---|
| grok.com / X 消费端（Grok 4.1 时期公开的系统提示） | "You are Grok, built by xAI." | 官方 GitHub xai-org/grok-prompts（grok4p1_thinking_system_turn_prompt_v2.j2，仓库最后更新 2025-11-17，可能已不反映 4.5 以后的版本） |
| Grok Build（订阅 CLI） | "You are Grok released by xAI. You are an interactive CLI tool that helps users with software engineering tasks. Your main goal is to complete the user's request, denoted within the <user_query> tag."（非交互模式："an autonomous agent that completes software engineering tasks. There is no human operator in this session."） | 源码观察：xai-org/grok-build templates/prompt.md，`DEFAULT_SYSTEM_PROMPT_LABEL = "Grok"`（部分模板做了 XOR 加密：prompt_encrypted.rs） |
| xAI API 裸调用 | 未找到官方资料（文档未说明默认系统提示） | — |

质检注意：公司已更名为 SpaceXAI，新模型可能自称 "built by xAI" 或 "SpaceXAI"，两者都应判为合格。社区观察：本次没有检索到可靠的 Grok 降智或档位缩水报道。第三方定价站对 Lite 和 Heavy 的价格说法不一致（ai-toolbox.co、krater.ai、aitoolanalysis.com，社区观察，未经官方确认）。

---

### 9. 对出题有用的要点（可直接转成质检题）（Google 与 xAI 共用）

1. **用知识截止区分模型档次**：3.5 Flash-Lite / 3.6 / 3.7 / 3.8 Flash 截止于 2026-03（部分领域仍停在 2025-01）；3.1 Pro、3 Flash、3.1 Flash-Lite、2.5 系列截止于 2025-01；Grok 4.7 截止于 2026-05。可用的官方事件锚点：
   - "SpaceX 收购 xAI"（2026-02-02，x.ai/news/xai-joins-spacex）
   - "Gemini 3.1 Pro 发布"（2026-02-19）
   - "Gemini 3 Pro Preview 发布"（2025-11-18）
   - "Gemma 4 发布"（2026-04-02）
   - "Grok 4.3 成为 xAI 旧模型重定向目标（2026-05-15）"

   预期结果：截止于 2025-01 的模型不应知道这些事件；Flash 3.6 及以后的模型可能知道 2026-02 的事件（官方说法是分领域）；Grok 4.7 可能知道 2026-04 的事件。注意要防搜索：消费端 App 默认会联网。
2. **上下文上限**：
   - Gemini App：免费 32k、Plus 128k、Pro / Ultra 1M。用约 100k token 的大海捞针题可以区分 Plus 和 Pro 及以上。
   - Gemini API：一律 1,048,576 输入 / 65,536 输出。
   - Grok：4.7 / 4.6 / 4.5 为 500k；4.3 和 4.20 为 1M；build-0.1 为 256k。
   - 请求 >200k 时 Grok 和 Gemini 3.1 Pro 都会按长上下文价格计费，可用于核对账单。
3. **参数行为指纹（API 层）**：
   - 向 `gemini-3.8-flash` 传 `thinking_level: minimal` 应返回错误；3.6 Flash 和 3 Flash 接受。
   - 3.1 Pro 不支持 minimal。
   - 3.5 Flash-Lite 默认 minimal，3.6 / 3.8 Flash 默认 medium，3.1 Pro 默认 high。
   - 2.5 Pro 设 `thinkingBudget: 0` 应报错（不能关闭思考）；2.5 Flash 允许。
   - 同时传 `thinking_level` 和 `thinking_budget` 应返回 400。
   - 3.6 起会忽略 temperature；以 model 角色预填结尾的请求返回 400。
   - Grok 4.5 及以上：reasoning 不能关闭，`stop` 和 presence / frequency penalty 会报错；xhigh 只有 4.6 及以上支持；grok-4.3 支持 `none`。
4. **档位权限**：
   - Gemini App：Deep Think 仅 Ultra；Plus 将失去 Pro；3.8 Flash 只给 Pro / Ultra。
   - Antigravity：Claude 5.5 只给 Pro（非试用）、Ultra 和企业版；Claude 4.6 与 GPT-OSS 只给 Free / Plus / Pro（2026-11-02 下线），**Ultra 没有 Claude 4.6**。
   - Grok：Grok Bot 只有 SuperGrok / Plus / Heavy 有；Expert 免费档没有；1080p 视频需要 Plus 及以上。
5. **官方重定向白名单**：
   - Gemini：`gemini-3.5-flash` → 3.6 Flash；`gemini-3.7-flash` → 3.8 Flash；`gemini-3-pro-preview` → 3.1 Pro。
   - xAI：`grok-4-0709`、`grok-3`、`grok-4-fast-*`、`grok-4-1-fast-*` → `grok-4.3`（low 或 none）；`grok-code-fast-1` → `grok-build-0.1`。
   - 卖家声称的"旧模型"实际由新模型应答，属于厂商行为，不应判为卖家造假。
6. **渠道风险**：
   - Gemini CLI 个人订阅登录已停服（2026-06-18）。sub2api 中的 Gemini CLI OAuth 类型账号若是个人 Pro / Ultra，预计无法使用或已失效，需要实测确认。
   - Antigravity 官方 FAQ 明确禁止第三方软件使用其登录，也不支持账号轮换。
   - xAI 则明确允许第三方工具（OpenCode 等）用 SuperGrok 订阅 OAuth 登录。

### 10. 未找到官方资料 / 存疑清单（Google 与 xAI）

1. Gemini App 各档的具体额度数值：官方已改为算力计量，只公布倍数（2x / 4x / 5x / 20x），不再公布每日条数。
2. Gemini App 各档 "Flash" 与 "Pro" 背后的确切版本：订阅页写 Plus 用 "3.6 Flash"、Pro 和 Ultra 用 "3.8 Flash"、Pro 档统一为 "3.1 Pro"；帮助中心则泛称 "Gemini 3 Flash / Pro"。限额页的 Model Access 表写所有档都能用 Pro，与 10 月新规（Plus 没有 Pro）冲突，可能是旧表未更新。
3. AI Pro 是否已获得 Deep Think：只有媒体报道，官方限额页仍写 Ultra only。
4. App 思考档的命名：Standard / Extended / Deep think 与 Low / Medium / High 两套写法并存，默认档（Low 还是 Standard）有歧义。
5. Gemini 3.5 Flash 的知识截止：模型卡未写。3.1 Pro 的截止日期出自已标废弃的 Gemini 3 指南（Jan 2025），模型卡指向 3 Pro 的 PDF 卡，本次未能解析。
6. 2.5 系列是否支持 `thinking_level`：Interactions API 版与 generateContent 版 thinking 文档说法冲突。
7. Antigravity：配额具体数值、订阅后端端点、系统提示原文都未公开（只有社区泄露和 sub2api 源码）。文档说 Ultra "Access to third-party models"，但 Pro（非试用）同样能用 Claude 5.5，两处表述不一致。
8. Gemini CLI 仓库的 quota-and-pricing.md 仍列 1,000 / 1,500 / 2,000 次/天，与 6-18 停服公告冲突，文档已过时。
9. Gemini 4 Argon：截至 10-11 尚未向 Ultra 或 API 开放（只有 X 平台的趋势摘要称已开放，未证实）；没有 API ID，输入上下文和知识截止未公布。
10. SuperGrok Lite 与 SuperGrok Heavy 的价格：x.ai/pricing 对比表只列名称不标价（第三方称 $10 和 $300）；grok.com/supergrok 被 Cloudflare 验证拦截，未读取。
11. Grok 消费端各档实际用的模型模式（Fast / Expert / Heavy 多 agent）以及 Heavy 档是否还有独立的 "Grok 4 Heavy" 模型：官方对比表只写了 "Grok 4.7" 和 "Expert"。
12. grok-4.6 / 4.5 / 4.3 / 4.20 / build-0.1 的知识截止，以及 4.5 以下各模型的最大输出：未找到官方资料。
13. grok-4.5 是否支持 xhigh、grok-4.3 的 effort 档数：文档自相矛盾。
14. X Premium+ 附带的 SuperGrok 额度是否等同于单独订阅的 SuperGrok：官方未说明。各档每周用量池的大小也未公布。
15. Grok 5：官方页面未见，视为未发布。

## 第 5 章 国产 Coding Plan 与 OpenCode Go

> 调研范围：Kimi（月之暗面）/ 智谱 GLM Coding Plan / MiniMax（M Plan，原 Token Plan、Coding Plan）/ OpenCode Go。
> **访问日期：下文所有来源均按任务要求统一记为 2026-10-11**（个别页面实际在 2026-10-12 凌晨访问，内容无差异）。
> 标注约定：未加标注 = 官方页面/官方文档/官方开源仓库原文；「社区观察，未经官方确认」= 论坛、媒体、博客、X、第三方站点；「推断」= 本文根据官方材料推出、官方未直接写明。
> 价格、额度、模型名均以 2026-10 官方页面为准；第三方站点（codingplan.org、coding-plan.org、知乎、博客等）只作线索，冲突处已在文中注明。

### 0. 先看这几条（与常见记忆/第三方说法冲突的地方）

| 事项 | 2026-10 官方现状 | 常见旧说法 / 第三方说法 | 来源 |
|---|---|---|---|
| 域名迁移 | `platform.moonshot.cn` → 跳转 `platform.kimi.com`；`platform.moonshot.ai` → `platform.kimi.ai`；`platform.minimaxi.com` → `platform.minimax.cn`；`www.minimaxi.com` → `www.minimax.cn`（curl 跟随跳转实测） | 旧域名 | curl 实测（2026-10-11） |
| Kimi Code 是否独立订阅 | 官方文档：Kimi Code「随 Kimi 会员订阅一同提供，与 Kimi 会员共享同一套额度」；新会员档 Go 无 coding 额度，Plus 起可用 | 第三方称 2026-07-20 已拆成独立订阅 | https://www.kimi.com/code/docs/kimi-code/membership.html |
| Kimi 主力模型 | K3（`k3`/`k3-256k`）、K2.8 Preview（`kimi-for-coding`）、K2.7 Code HighSpeed（`kimi-for-coding-highspeed`）；开放平台 `kimi-k2.5`、`kimi-k2` 系列、`moonshot-v1` 已下线 | K2 / K2-0905 / K2-Thinking / K2.5 | https://www.kimi.com/code/docs/kimi-code/models.html ；https://platform.kimi.com/docs/models.md |
| 智谱价格 | 国内 Lite/Pro/Max ¥118/¥538/¥1078 每月（积分制，2026-07-30 改版）；旧 V2 ¥49/¥149/¥469 仅老用户 | ¥49/¥149/¥469 | https://bigmodel.cn/glm-coding ；https://docs.bigmodel.cn/cn/coding-plan/notice/usage-revision.md |
| 智谱模型 | 全档位 GLM-5.3 + GLM-5.3-Flash；GLM-5.2/5.1 请求自动转 GLM-5.3，GLM-5-Turbo/GLM-4.7 自动转 GLM-5.3-Flash；**Claude Code 默认映射（Opus/Sonnet/Haiku）全部是 GLM-5.3-Flash** | GLM-4.5/4.6/4.7/5 | https://docs.bigmodel.cn/cn/coding-plan/overview.md ；https://docs.bigmodel.cn/cn/coding-plan/tool/claude.md |
| MiniMax 订阅 | **M Plan（Go/Explore/Build）已上线，Token Plan 停止新购**；三档文本模型均为 **MiniMax-M3.1-Flash-Preview** | Coding Plan Starter/Plus/Max；Token Plan Plus/Max/Ultra；M2/M2.1/M2.5 | https://platform.minimax.cn/docs/m-plan/intro.md ；https://platform.minimax.cn/docs/m-plan/token-plan-notice.md |
| MiniMax 端点 | 文档已改用 `api.minimax.cn`（国内）/ `api.minimax.io`（国际）；旧 `api.minimaxi.com` 仍可连通（无 key 返回 401），但新文档不再出现 | `api.minimaxi.com` | https://platform.minimax.cn/docs/guides/text-generation.md ；curl 实测 |
| OpenCode Go | Go $10/月 + **Go Plus $40/月**；文档列 32 个模型（含 Claude Haiku 5.5、GPT 6 Luna、Grok 4.7 等闭源模型），按模型设月度美元额度 | 仅 $10 一档、只含开源模型 | https://opencode.ai/docs/go/ |

---

### 1. Kimi（月之暗面）— Kimi 会员 / Kimi Code

#### 1.1 档位对比

价格来源说明：国内价格取自 kimi.com 定价页背后的公开商品列表接口 `https://www.kimi.com/apiv2/kimi.gateway.order.v1.GoodsService/ListGoods`（只读请求，中国 IP 返回 `REGION_CN`/CNY）；国际价格取自 `https://www.kimi.com/membership/pricing` 与 `https://www.kimi.com/code/#pricing` 在海外出口 IP 下渲染的美元页面。

| 档位（新套餐） | 价格（国内站 / 国际站） | 可用模型（Kimi Code） | 额度规则 | 速度/并发 | 附加能力 | 来源 |
|---|---|---|---|---|---|---|
| Free（国内商品名 Adagio） | ¥0 / $0 | 无 Kimi Code（定价页对比表「Kimi Code」行仅付费档显示「可调用」） | — | — | — | ListGoods 接口；https://www.kimi.com/membership/pricing |
| Go（仅国内可见） | ¥49/月；¥468/年 / 国际页未见此档 | **无 coding 额度** | — | — | Kimi 会员其他权益 | ListGoods 接口；https://www.kimi.com/code/docs/kimi-code/membership.html |
| Plus | ¥99/月；¥948/年 / $19/月；$180/年（折 $15/月） | `k3`（上下文上限 262,144）、`k3-256k`、`kimi-for-coding`（K2.8 Preview，1,048,576） | 新会员：**取消周额度**，仅保留**每 5 小时滚动窗口**；与 Kimi 会员**共享月总额度**（月额度用尽则 Kimi Code 冻结）；所有设备与 API Key 共享一份额度 | 常规速度；不含高速版 | Kimi Code CLI / Desktop / VS Code、第三方工具（API Key）、加油包；国际页：「更多 Agent 额度」 | https://www.kimi.com/code/docs/kimi-code/models.html ；…/membership.html ；https://www.kimi.com/code/#pricing |
| Pro | ¥199/月；¥1,908/年 / $39/月；$372/年（折 $31/月） | 上述全部 + **`k3` 1M 上下文** + **`kimi-for-coding-highspeed`** | 同上 | 高速版约 5–6 倍输出速度（「6 倍速 3 倍消耗」）；Kimi Code 页称「更高的并发上限」 | 国际页：「2 倍 Agent 额度」 | 同上；https://www.kimi.com/code/docs/third-party-tools/claude-code.html |
| Max | ¥699/月；¥6,708/年 / $99/月；$948/年（折 $79/月） | 同 Pro | 同上 | 同 Pro | 国际页：「5 倍 Agent 额度」「支持百万 Tokens 超长对话容量」 | 同上 |
| Ultra（仅国际页可见） | 国内商品列表未见 / $199/月；$1,908/年（折 $159/月） | 同 Pro | 同上 | 同 Pro | 国际页：「10 倍 Agent 额度」 | https://www.kimi.com/membership/pricing |
| 老套餐 Andante / Moderato / Allegretto / Allegro | 文档称「档位名称更新，定价不变」，未给逐档价格映射 | Andante：仅 `kimi-for-coding`；K3：Moderato 及以上；K3 1M 与高速版：Allegretto 及以上 | **仍有周额度**（自订阅日起每 7 天刷新）+ 5 小时窗口 + 月总额度 | — | — | https://www.kimi.com/code/docs/kimi-code/membership.html ；…/error-reference.html |

补充（官方）：
- 档位门槛对照（新/老）：Kimi Code 使用门槛 Plus / Andante；K3 调用门槛 Plus / Moderato；K3 1M 上下文门槛 Pro / Allegretto；高速版门槛 Pro / Allegretto。来源：https://www.kimi.com/code/docs/kimi-code/membership.html
- **各档 5 小时/月度额度的绝对值官方未公开**（只说「按时更新的使用额度」「充足的额度」「尊享澎湃额度」）。
- 加油包（Extra Usage）：订阅额度用尽后按量扣费，「加油包价格近似于 Kimi 开放平台的官方 API 价格」；单次最低 25 元、每日最多 10 次、累计 3,000 元，余额上限 10,000 元。来源同 membership.html。
- 新旧名称映射（推断）：由门槛对照可推 Moderato↔Plus（¥99）、Allegretto↔Pro（¥199）；Andante 对应 ¥49 价位但新 Go 档已无 coding 额度。官方未给逐档映射表。

#### 1.2 模型参数

| 模型名 | 官方模型 ID | 上下文窗口 | 最大输出 | 知识截止 | 思考模式开关（参数 / 默认） | 可用档位 | 来源 |
|---|---|---|---|---|---|---|---|
| Kimi K3 | Kimi Code：`k3`（Claude Code 环境变量写 `k3[1m]` 才开 1M）；开放平台：`kimi-k3`（Claude Code 写 `kimi-k3[1m]`） | 1,048,576（Kimi Code 中 Plus 只给 262,144） | 开放平台 `max_completion_tokens` 默认 131072、最大 1048576；Kimi Code 文档未写 | 未找到官方资料 | **始终思考**；`reasoning_effort`: `low`/`high`/`max`。**默认值不同**：Kimi Code 默认 `high`；开放平台 Chat API 默认 `max`；Anthropic 兼容 `output_config.effort` 默认 `max`。Kimi Code 中「关闭 thinking 后请求由 K2.8 Preview（无思考）处理」 | Plus+（256K）；Pro+（1M） | https://www.kimi.com/code/docs/kimi-code/models.html ；https://platform.kimi.com/docs/api/models-overview.md ；https://platform.kimi.com/docs/guide/kimi-k3-quickstart.md ；https://platform.kimi.com/docs/api/messages.md |
| Kimi K3 256K | `k3-256k` | 262,144（固定） | 未写 | 未找到官方资料 | 同 K3（默认 high） | Plus+ | 同上 |
| Kimi K2.8 Preview | `kimi-for-coding`（仅 Kimi Code；开放平台模型列表无此模型） | 1,048,576（「全部会员档位均开放最高 1M」） | 未写 | 未找到官方资料 | `low`/`high`/`max`，**默认 `max`**；关闭 thinking → K2.8 Preview 无思考版 | Plus+（老套餐 Andante+） | https://www.kimi.com/code/docs/kimi-code/whats-new.html （2026-09-11 条目） |
| Kimi K2.7 Code HighSpeed | `kimi-for-coding-highspeed`（Kimi Code）；开放平台 `kimi-k2.7-code-highspeed` | 262,144 | 开放平台 `max_tokens` 默认 32768（上限未写） | 未找到官方资料 | **Thinking 强制开启**（开放平台只接受 `{"type":"enabled","keep":"all"}`，传 disabled 报错） | Pro+（老套餐 Allegretto+） | models.html；https://platform.kimi.com/docs/guide/kimi-k2-7-code-quickstart.md |
| Kimi K2.7 Code | 开放平台 `kimi-k2.7-code`（Kimi Code 中 2026-09-11 起 `kimi-for-coding` 已升级为 K2.8 Preview） | 262,144 | 默认 32768 | 未找到官方资料 | 同上（强制开启，Preserved Thinking 始终开启） | 开放平台按量 | https://platform.kimi.com/docs/api/models-overview.md |
| Kimi K2.6 | 开放平台 `kimi-k2.6` | 262,144 | 默认 32768 | 未找到官方资料 | `thinking`: `{"type":"enabled"}`（默认）/`{"type":"disabled"}`/`{"type":"enabled","keep":"all"}`；思考 temperature 固定 1.0、非思考 0.6 | 开放平台按量 | 同上；https://platform.kimi.com/docs/guide/kimi-k2-6-quickstart.md |
| 已下线 | `kimi-k2.5`（2026-08-31）、`moonshot-v1-*`（2026-08-31）、`kimi-k2-0905-preview`/`kimi-k2-0711-preview`/`kimi-k2-turbo-preview`/`kimi-k2-thinking`/`kimi-k2-thinking-turbo`（2026-05-25）、`kimi-latest`（2026-01-28）、`kimi-thinking-preview`（2025-11-11） | — | — | — | — | — | https://platform.kimi.com/docs/models.md |

模型规格补充（官方）：
- K3：总参 2.8T、激活 104B、896 专家选 16、Context Length 1048576、MXFP4 权重/MXFP8 激活（量化感知训练）、文本+图像（Kimi Code 中 `k3` 支持图片+视频，`k3-256k` 仅图片）；Kimi Code 公告 2026-07-16 发布，技术博客称「full model weights will be released by July 27, 2026」，发布时「will use max thinking effort by default」。来源：HF 模型卡 https://huggingface.co/moonshotai/Kimi-K3 （经 hf-mirror.com 镜像读取）；https://www.kimi.com/blog/kimi-k3
- K2.7 Code / K2.6：1T 总参 / 32B 激活，原生 INT4，256K。来源：https://huggingface.co/moonshotai/Kimi-K2.7-Code ，https://huggingface.co/moonshotai/Kimi-K2.6
- 开放平台所有 K3/K2.x 的 `temperature`、`top_p`(0.95)、`n`(1)、penalty(0) 均**不可修改**，传其他值报错；`tool_choice: "required"` 仅 K3 支持，K2.6/K2.7 Code 传入报错。来源：https://platform.kimi.com/docs/api/models-overview.md
- 开放平台价格（参考）：kimi-k3 国内 输入 ¥20 / 缓存命中 ¥2 / 输出 ¥100 每百万 token；国际 $3 / $0.30 / $15。来源：https://platform.kimi.com/docs/pricing/chat.md ，https://platform.kimi.ai/docs/pricing/chat.md

#### 1.3 端点与接入方式

| 用途 | 国内 | 国际 | 说明 | 来源 |
|---|---|---|---|---|
| Kimi Code 订阅（OpenAI 兼容） | `https://api.kimi.com/coding/v1` | `https://api.kimi.ai/coding/v1` | Key 在 Kimi Code 控制台创建；模型 ID 必须写 `k3`/`k3-256k`/`kimi-for-coding`/`kimi-for-coding-highspeed`，写版本名（如 `K3`）会失败 | https://www.kimi.com/code/docs/kimi-code/models.html |
| Kimi Code 订阅（Anthropic 兼容） | `https://api.kimi.com/coding/` | `https://api.kimi.ai/coding/` | Claude Code 用 `ANTHROPIC_API_KEY`；`/status` 显示 Base URL 即生效，「即使模型名称仍显示为 Claude 模型，实际调用的仍是 Kimi Code 的 API」 | https://www.kimi.com/code/docs/third-party-tools/claude-code.html |
| 开放平台按量（OpenAI 兼容） | `https://api.moonshot.cn/v1` | `https://api.moonshot.ai/v1` | 按量付费，Key 在开放平台创建 | https://www.kimi.com/code/docs/kimi-code/faq.html |
| 开放平台按量（Anthropic 兼容） | `https://api.moonshot.cn/anthropic` | `https://api.moonshot.ai/anthropic` | 推荐 Opus/Sonnet/Fable 映射 `kimi-k3[1m]`、Haiku 映射 `kimi-k2.7-code` | https://platform.kimi.com/docs/guide/claude-code-kimi.md ；https://platform.kimi.ai/docs/guide/claude-code-kimi.md |

要点：
- 订阅 Key 与开放平台 Key **不通用**（FAQ：「配置时请注意 Base URL 与 API Key 的匹配是否正确」）。
- **高速版 ID 写错会静默兜底到标准版**：「高速版 ID 必须是 kimi-for-coding-highspeed，填错会兜底到标准版 kimi-for-coding，不报错也不加速。」（models.html）
- 第三方工具 effort 映射（官方）：`null`→模型默认（K3=high，K2.8 Preview=max）；`ultra/max/xhigh`→max；`high/medium`→high；`low/minimum/light`→low；`none`→`thinking.type disabled`；其他未知值→HTTP 400。Claude Code `/effort`：low→low，medium/high→high，xhigh/max→max。
- 用量查询：CLI `/usage`、Kimi Code 控制台。**HTTP 用量接口（如 `/coding/v1/usages`）未在官方文档找到**（仅为 sub2api 源码线索）。
- 客户端限制：文档写明 CLI、VS Code、桌面端及第三方工具的请求都计入额度，未见 User-Agent 白名单条款；「所有登录设备和 API Key 共享同一套配额；超过 30 天未活跃的设备会自动解绑」。
- 官方报错文案（可直接用于档位权限检测，来源 https://www.kimi.com/code/docs/kimi-code/error-reference.html ）：
  - 401 `Your current subscription does not have access to k3.`（低于 Plus/Moderato）
  - 401 `Your current plan supports only k3 up to 256K context. 1M context is available on higher-tier Kimi Code plans.`
  - 401 `Your current subscription does not have access to kimi-for-coding-highspeed.`
  - 403 `You've reached your 5-hour usage limit.` / `You've reached your weekly (7-day) usage limit.`（**仅老会员会出现**）/ `You've reached your monthly usage limit for this billing cycle.` / `You've reached your concurrent request limit.`（风控触发）
  - 429 `We're receiving too many requests at the moment.` / `The engine is currently overloaded`
  - 400 `Your request exceeded model token limit: 262144`；`total message size N exceeds limit 2097152`；`thinking is enabled but reasoning_content is missing`

#### 1.4 自我介绍与降智/限速报道

**官方**
- Kimi Code 官网演示对话（问「介绍一下你自己」）：「我是 Kimi Code，一个运行在你电脑上的 AI 编程助手，由月之暗面（Moonshot AI）开发。」来源：https://www.kimi.com/code/
- Kimi Code CLI 系统提示词开头：`You are ${product_name}, an interactive general AI agent running on a user's computer.` 来源：https://raw.githubusercontent.com/MoonshotAI/kimi-code/main/packages/agent-core-v2/src/app/agentProfileCatalog/system.md
- HF 模型卡示例 system prompt（K2.5/K2.6/K2.7 Code）：`You are Kimi, an AI assistant created by Moonshot AI.`；K2.5 卡注明「The default system prompt might cause confusion… so we remove it」（chat template 内无默认身份）。K3 仓库无独立 chat_template.jinja，未找到默认身份/截止文本。来源：https://huggingface.co/moonshotai/Kimi-K2.7-Code
- 官方限速/波动说明：K2.7 Code 高速版「目前资源有限，高速版模型体验可能偶有波动」（https://platform.kimi.com/docs/guide/kimi-k2-7-code-quickstart.md ）；403 并发限制「由风控策略触发」。
- Moonshot 官方第三方部署校验工具 KVV（Kimi Vendor Verifier）：先做 Pre-Verification（检查 temperature/top_p 等参数约束是否被正确强制），再跑 OCRBench（多模态冒烟）、MMMU Pro（视觉预处理）、AIME2025（长输出压力，专门抓 KV cache bug 和量化退化）、K2VV ToolCall（`tool_call_f1` 与 JSON Schema 准确率）、SWE-Bench（未开源）；旧版 K2VV 对 4,000 条工具调用请求与官方 API 对比，`tool_call_f1` 阈值 K2-thinking ≥73%、K2-0905 ≥80%。来源：https://www.kimi.com/blog/kimi-vendor-verifier.html ；https://github.com/MoonshotAI/K2-Vendor-Verifier
- Anthropic 官方声明（2026-02-23）：Moonshot（Kimi 模型）通过数百个欺诈账号与 Claude 产生「Over 3.4 million exchanges」，后期「attempting to extract and reconstruct Claude's reasoning traces」。来源：https://www.anthropic.com/news/detecting-and-preventing-distillation-attacks

**社区观察，未经官方确认**
- K3 发布后被截图回答「我是Claude，不是Kimi」（英文问 “Hey Kimi, how's it going?”）：https://www.epochtimes.com/gb/26/7/21/n14813986.htm ，https://www.ntdtv.com/b5/2026/07/19/a104116746.html （注意：这两家媒体立场鲜明）；同类转载 https://www.sydneytoday.com/content-1026598034530002 。
- 白宫 OSTP 主任 Kratsios 2026-07-22 指控 Moonshot 蒸馏 Anthropic Fable 构建 K3：https://thehill.com/policy/technology/5984510-white-house-moonshot-ai-anthropic-nvidia/ ，https://seekingalpha.com/news/4616700-kratsios-says-moonshot-built-kimi-k3-through-industrial-distillation-of-anthropics-fable （未找到 Moonshot 完整官方回应）。
- 新套餐额度缩水/偷降额度：linux.do「kimi新套餐额度超级大砍」https://linux.do/t/topic/2924623 、「kimi新套餐千万别入坑」https://linux.do/t/topic/2917596 、「KIMI是不是偷偷降额度了？」https://linux.do/t/topic/2909467 （称老 699 档周额度提前耗尽）、「kimi新的套餐有佬订阅了吗」https://linux.do/t/topic/2897188 （称新套餐月额度约等于 50 个 5 小时窗口）。
- 静默升级风险：kimi-for-coding 2026-09-11 无感切到 K2.8 Preview，第三方测评提示「偶发限流」、建议自建回归测试：https://aiho.net/coding/cli/kimi-code.html
- K3 知识截止「early 2026」仅见 X 用户无出处说法：https://x.com/Elaina43114880/status/2077763931670773841 ；技术报告（arXiv 2607.24653）据第三方解读未披露截止日期。

---

### 2. 智谱 GLM Coding Plan（bigmodel.cn / z.ai）

#### 2.1 档位对比

| 档位 | 价格（国内站 / 国际站） | 可用模型 | 额度规则 | 速度/并发 | 附加能力 | 来源 |
|---|---|---|---|---|---|---|
| Lite | ¥118/月（连续包月）；包季 8 折 ¥94.4/月；包年 7 折 ¥82.6/月 / $18/月；包季 20% off；包年 $12.6/月（30% off） | GLM-5.3、GLM-5.3-Flash（「逐步开放最新旗舰模型及功能」） | 每 5 小时 **2,000 积分**；每周 **10,000 积分** | 并发 Max>Pro>Lite，动态调整；建议同时 1 个项目 | 视觉理解/联网搜索/网页读取/开源仓库（zread）MCP **全档可用**，与模型共享积分；ZCode、AutoClaw 内 150% 额度 | https://bigmodel.cn/glm-coding ；https://z.ai/subscribe ；https://docs.bigmodel.cn/cn/coding-plan/overview.md ；…/usage-notes.md ；…/faq.md |
| Pro | ¥538/月；¥430.4（季）；¥376.6（年） / $80/月；$56/月（年） | 同上（「优先体验最新旗舰模型及功能」） | 每 5 小时 **12,000**；每周 **60,000**（页面：6 倍 Lite） | 「更快生成速度」；建议 1–2 个项目 | 「覆盖多款精选 MCP 工具」 | 同上 |
| Max | ¥1078/月；¥862.4（季）；¥754.6（年） / $168/月；$117.6/月（年） | 同上（「首发接入最新旗舰模型及功能」） | 每 5 小时 **28,000**；每周 **140,000**（14 倍 Lite） | 「高峰期专属资源优先保障」；建议 2+ 个项目 | 同 Pro | 同上 |
| 团队标准席位（国内） | ¥598/席/月（年付 9 折 ¥538.2） | 同上 | 每周 66,000 积分 | — | 超额按量付费、IP 白名单、数据默认不用于训练 | https://bigmodel.cn/glm-coding （团队套餐页签） |
| 团队高级席位（国内） | ¥1198/席/月（年付 ¥1078.2） | 同上 | 每周 155,000 积分 | 首发接入、高峰优先 | 同上 | 同上 |
| 旧 V2 个人版（2026-07-30 前；老用户可续/升） | ¥49 / ¥149 / ¥469（包季 9 折、包年 8 折） | 与新版一致（「仅额度计算方式不同」） | 按 prompt：5 小时约 80/400/1,600 次，每周约 400/2,000/8,000 次；GLM-5.3 非高峰 1 倍/高峰 3 倍，Flash 0.4/1.2 倍 | — | — | https://docs.bigmodel.cn/cn/coding-plan/notice/usage-revision.md |

积分规则（官方，国内国际一致）：
- 模型积分 =（输入 Token × Input 系数 + 缓存命中 Token × Cached 系数 + 输出 Token × Output 系数）/ 10000；GLM-5.3 系数 6.9 / 1.7 / 24；GLM-5.3-Flash（含视觉理解 MCP）2.3 / 0.56 / 8；MCP（联网搜索、网页读取、开源仓库）每次 1.2。
- **非高峰时段模型调用按 50% 抵扣**；高峰 = 周一至周五 14:00–18:00（UTC+8）；改版公告另称「周末全天将按非高峰时段抵扣」。
- 5 小时积分「在请求消耗 5 小时后刷新」；周积分自下单起每 7 天刷新；额度耗尽**不会**扣资源包/余额。
- 官方估算（95% 缓存命中）：Lite 每周 GLM-5.3 约 0.48–0.97 亿 tokens、Flash 约 1.46–2.92 亿。
- 来源：https://docs.bigmodel.cn/cn/coding-plan/overview.md ；https://docs.z.ai/devpack/overview.md （系数表逐项一致）

Lite 能否用最新旗舰：**当前能**（全档 GLM-5.3）。历史上 GLM-5 刚上线时 Lite 只能用 GLM-4.7/4.6、Pro/Max 才有 GLM-5 —— 该说法仅见第三方（https://zhuanlan.zhihu.com/p/2015468530938693485 等），社区观察，未经官方确认。

#### 2.2 模型参数

| 模型名 | 官方模型 ID | 上下文窗口 | 最大输出 | 知识截止 | 思考模式开关（参数 / 默认） | 可用档位 | 来源 |
|---|---|---|---|---|---|---|---|
| GLM-5.3 | `glm-5.3`（Claude Code 写 `glm-5.3[1m]` 才开 1M） | 1M | 128K | 未找到官方资料 | **始终思考**：`thinking.type` 仅 `enabled`；`reasoning_effort` `low`/`high`/`max`，默认 `max`。标准 API 传 `thinking.type: "disabled"` → 「请求将失败」 | 全档 | https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3.md ；https://docs.bigmodel.cn/cn/guide/start/migrate-to-glm-new.md |
| GLM-5.3-Flash（内测代号 **OX Alpha**） | `glm-5.3-flash`（`glm-5.3-flash[1m]`） | 1M | 128K | 未找到官方资料 | `thinking.type` 仅 `enabled`；推荐 `reasoning_effort: max`、`thinking.clear_thinking: false` | 全档；**Claude Code 默认映射**；额度消耗约为 GLM-5.3 的 1/3（「可用额度增加至 3 倍」） | https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash.md ；https://bigmodel.cn/glm-coding （FAQ「OX Alpha 是 GLM-5.3-Flash 在内测阶段使用的内部代号」） |
| GLM-5.3-FlashX | `glm-5.3-flashx` | 1M | 128K | 未找到 | 同 Flash | **套餐暂未开放**（API 按量可用，约 200 tokens/s） | 同上 |
| GLM-5.2 | `glm-5.2`（套餐内自动转 GLM-5.3） | 1M | 128K | 未找到 | `thinking.type` enabled/disabled；`reasoning_effort` | 套餐内被路由到 5.3 | https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2.md ；overview.md |
| GLM-5.1 / GLM-5-Turbo / GLM-4.7 | — | — | — | — | — | 套餐内：GLM-5.1→GLM-5.3；GLM-5-Turbo、GLM-4.7→GLM-5.3-Flash（国际文档仅列 GLM-4.7→Flash） | overview.md；https://docs.z.ai/devpack/overview.md |

模型规格补充（官方）：
- GLM-5.3「使用与 GLM-5.2 相同的基础模型——所有提升均来自后训练」（两者知识截止应相同，推断）；仅文本；HF 许可证 `glm-5.3`（非 MIT）。
- GLM-5.3-Flash：GLM-5 系列首个原生多模态（图片/视频/文件）；总参 320B、激活 18B；「starts from a newly trained base model」，30T-token 多模态预训练语料；线性+稀疏注意力混合；MIT。来源：https://huggingface.co/zai-org/GLM-5.3-Flash
- 发布时间（智谱发布记录）：GLM-4.7 2025-12-22；GLM-4.7-Flash 2026-01-19；GLM-5 2026-02-12；GLM-5-Turbo 2026-03-15；GLM-5.1 2026-04-07；团队版 2026-05-29；GLM-5.2 2026-06-16；**GLM-5.3 2026-08-19**；GLM-5.3-Flash 2026-08-26。来源：https://docs.bigmodel.cn/cn/update/new-releases.md （第三方多称 GLM-5.3 为 08-14 发布，存疑）
- 智谱 HF 模型卡引用 Artificial Analysis 的 TPS：GLM-5.3 115 TPS、Kimi K3 40 TPS、Qwen3.8 Max 47 TPS（第三方测量、经智谱引用）。来源：https://huggingface.co/zai-org/GLM-5.3

**Coding 端点上的 effort 映射（官方，与标准 API 不同）**：

| 工具传入 | 实际档位 | 处理 |
|---|---|---|
| `thinking.type` 未传 / true / enabled / adaptive | max | 默认档 |
| `thinking.type` 为 false / disabled / none / off | low | **继续请求，仍会轻量思考**；「关闭思考配置会转换为 low，不会切换到其他模型」 |
| `reasoning_effort` minimal/light/low | low | 自动转换 |
| `reasoning_effort` medium/high | high | 自动转换 |
| `reasoning_effort` xhigh/max/ultra | max | 自动转换 |
| 其他未知字符串 | max | 回退默认档 |

优先级：显式 Effort > thinking 开关 > 默认 max。Claude Code 用 `thinking.type`、`output_config.effort`；Codex 用 `reasoning.effort`。来源：https://docs.bigmodel.cn/cn/coding-plan/latest-model.md ；https://docs.z.ai/devpack/latest-model.md

#### 2.3 端点与接入方式

| 协议 | 国内（Coding 套餐） | 国际（Coding 套餐） | 普通按量 API（不消耗套餐） | 来源 |
|---|---|---|---|---|
| Anthropic Messages | `https://open.bigmodel.cn/api/anthropic` | `https://api.z.ai/api/anthropic` | — | https://docs.bigmodel.cn/cn/coding-plan/tool/others.md ；https://docs.z.ai/devpack/tool/others.md |
| OpenAI Chat Completions | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://api.z.ai/api/coding/paas/v4` | `https://open.bigmodel.cn/api/paas/v4` / `https://api.z.ai/api/paas/v4` | 同上；faq.md |
| OpenAI Responses（Codex） | `https://open.bigmodel.cn/api/v1` | `https://api.z.ai/api/v1` | — | 同上 |

要点：
- **仅限指定工具**：「在除规定工具外调用 API，不可享用 Coding 套餐的额度」；未满足条件会报「1113 余额不足」或扣账户余额；官网体验中心不能用套餐。自建应用/网站/机器人/SaaS 须用标准 API。来源：faq.md、usage-notes.md
- 支持工具（文档列）：ZCode（1.5 倍用量）、Claude Code、Codex、OpenCode、Kilo、Cline、Droid、Roo、Crush、Goose、Cursor、AutoClaw（1.5 倍）、WorkBuddy、OpenClaw（次级调度：「高负载下 OpenClaw 任务将自动触发包括动态排队、限流等公平使用策略」）、Cherry Studio、Hermes Agent 等。
- Claude Code **默认（服务端）映射**：`ANTHROPIC_DEFAULT_OPUS_MODEL` / `SONNET` / `HAIKU` 全部 = `GLM-5.3-Flash`；官方示例把 Opus/Sonnet 改成 `glm-5.3[1m]`、Haiku 用 `glm-5.3-flash[1m]`，并设 `CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000`。来源：https://docs.bigmodel.cn/cn/coding-plan/tool/claude.md ；https://docs.z.ai/devpack/tool/claude.md
- 团队套餐 Key「与平台其他 API Key 不通用」。国内（bigmodel.cn）与国际（z.ai）账号/Key 是否互通：官方未明确说明（存疑）。
- 用量查询：官方 Claude Code 插件 `glm-plan-usage`（仅个人版）源码调用 `{域名}/api/monitor/usage/quota/limit`、`/api/monitor/usage/model-usage`、`/api/monitor/usage/tool-usage`（**非文档化接口**，来自官方开源插件）。来源：https://docs.bigmodel.cn/cn/coding-plan/extension/usage-query-plugin.md ；https://raw.githubusercontent.com/zai-org/zai-coding-plugins/main/plugins/glm-plan-usage/skills/usage-query-skill/scripts/query-usage.mjs

#### 2.4 自我介绍与降智/限速报道

**官方**
- 未找到智谱关于模型自我介绍措辞的官方说明；GLM-5.3 / 5.3-Flash 的 HF chat template 中无默认身份或知识截止文本（本次核查）。
- 风控与转售：「禁止账号共享或多人共用」；「仅限指定工具使用」；违规「限流、冻结…3 次以上违规可能会被封禁」；永久封禁针对「倒卖、中转、套利等严重违规行为」。来源：https://docs.bigmodel.cn/cn/coding-plan/usage-notes.md （**对本项目寄售模式是直接风险点**）
- 限售：2026-01-21 公告自 1-23 起每日限量发售（约原先 20%），原因是 GLM-4.7 后算力紧张、高峰并发限流与变慢（报道：https://www.ithome.com/0/915/122.htm ）；7-30 改版公告称新积分套餐上线、老套餐停售新购。限售结束日（7-31）仅见第三方。
- 高峰期并发动态调整（Max>Pro>Lite），低峰并发动态提升。

**社区观察，未经官方确认**
- 「感觉最近的glm 5.3降智有点严重」（2026-10-05，回帖称在 CC 之外像 5.3 Flash）：https://linux.do/t/topic/2983953
- 「智谱也开始降智了。」（2026-09-23，高峰约 70 tok/s）：https://linux.do/t/topic/2940496
- 「避雷智谱 GLM Coding Plan，日常429限流」：https://linux.do/t/topic/1918431 ；V2EX「GML 限流也太难顶了」：https://www.v2ex.com/t/1187116
- 第三方平台 GLM-5.2 疑因供应商改 FP4 量化而降智（针对 OpenCode 平台，非智谱自营）：https://www.80aj.com/2026/07/02/glm-quantization-accuracy-loss/
- 未找到 GLM-5.x 自称 Claude 的可靠报告；在 Claude Code 中界面显示 Claude 模型名、实际为 GLM（服务端映射），Claude Code 自带系统提示会让任何后端模型倾向自称 “Claude Code”（推断）。

---

### 3. MiniMax（M Plan；原 Token Plan / Coding Plan）

#### 3.1 档位对比

| 档位 | 价格（国内站 / 国际站） | 可用模型 | 额度规则 | 速度/并发 | 附加能力 | 来源 |
|---|---|---|---|---|---|---|
| Free | ¥0 / $0 | M3.1 Flash Preview「有限可用」（仅 MiniMax Code，**无 API Key**） | — | — | — | https://www.minimax.cn/m-plan |
| **Go** | ¥49/月；¥490/年（「立省2月」） / $22/月；年付「2 months free」 | 文本 M3.1 Flash Preview；图像、音频模型；**无视频** | 基准 1 倍；**5 小时窗口 + 周窗口**（均自首次使用起计时，到期刷新，不结转） | RPM/TPM 速率限制，超限「通常约 1 分钟恢复，高峰期可能动态收紧」 | 联网搜索与多模态理解（MCP：web_search、understand_image）；MiniMax Code 全端；API Key 接入 Claude Code/Codex/Cursor/OpenCode 等 | https://platform.minimax.cn/docs/m-plan/intro.md ；…/usage-rules.md ；…/faq.md ；https://www.minimax.io/m-plan |
| **Explore** | ¥119/月；¥1,190/年 / $55/月 | 同 Go + **H3 视频**（视频仅受周窗口限制） | Go 的 3 倍 | 同上 | 同上 | 同上 |
| **Build** | ¥469/月；¥4,690/年 / $132/月 | 同 Explore | **国内 12 倍 Go；国际 7.5 倍 Go**（两站不同） | 「高峰时段优先通行」「抢先体验新功能」 | 同上 | 同上 |
| 首月优惠 | 月付首月 5 折（¥24.5/¥59.5/¥234.5；$11/$27.5/$66），至 10 月 14 日 | — | — | — | — | https://platform.minimax.cn/docs/m-plan/monthly-offer.md ；https://platform.minimax.io/docs/m-plan/monthly-offer.md |
| 积分包 | ¥30=4,489 / ¥150=22,460 / ¥500=74,900（1,000 积分=¥7） / $5=5,000 / $25=25,000 / $100=100,000（1,000 credits=$1）；有效 1 年 | M3.1 Flash Preview、H3、图像音频、搜索 | 套餐额度优先扣，超出扣积分 | — | 可单独购买 | m-plan/faq.md；https://www.minimax.cn/m-plan |
| 旧 Token Plan Plus / Max / Ultra（**已停售**，老用户开自动续费可保留） | ¥49 / ¥119 / ¥469 / 官方定价页现写 $22 / $55 / $132 | 定价页：「MiniMax 全系模型（M3 / M2.7 / 图像 / 语音）」，H3 等不支持；老用户公告：文本模型现为 M3.1 Flash Preview，「已购的 Token Plan 套餐后续依然可以持续使用最新模型」 | 5 小时窗口 + 周窗口；「Agent 用量」3-4 / 4-5 / 6-7 个 | — | 历史权益「∞ 限额、150% 周额度」升级后不保留 | https://platform.minimax.cn/docs/guides/pricing-token-plan.md ；https://platform.minimax.io/docs/guides/pricing-token-plan.md ；…/m-plan/token-plan-notice.md |

补充：
- M Plan 公告：「套餐订阅价格与文本使用额度保持不变」（相对 Token Plan）；升级路径 Plus→Go/Explore/Build，Max→Explore/Build，Ultra→Build；不支持降级。
- **M Plan 各档绝对额度（token/请求数）官方未公开**，只给相对倍数。社区说法：旧 Token Plan Plus 约 6 亿 M3 tokens/月、Max 约 18 亿、Ultra 约 71 亿（https://blog.csdn.net/qq_21982453/article/details/161584233 等，社区观察）。
- 订阅 Key 调用「已有按量计费价格的 API 端点会按对应按量计费价格扣减套餐内 M Plan 额度」——即额度按目录价折算（M3 等按量模型也可用订阅 Key 调用，推断）。来源：m-plan/faq.md（国内、国际一致）
- 音乐模型 2026-08-20 起停止在 M Plan 中调用。

#### 3.2 模型参数

| 模型名 | 官方模型 ID | 上下文窗口 | 最大输出 | 知识截止 | 思考模式开关（参数 / 默认） | 可用档位 | 来源 |
|---|---|---|---|---|---|---|---|
| MiniMax M3.1 Flash Preview | `MiniMax-M3.1-Flash-Preview`（Claude Code 写 `MiniMax-M3.1-Flash-Preview[1m]`） | 1,000,000 | `max_tokens` 推荐 131072，上限 524288 | 未找到官方资料 | **强制开启、不可关闭**：传 `thinking: {"type":"disabled"}` 或 `effort: "none"` → 400：`model "MiniMax-M3.1-Flash-Preview" requires adaptive thinking; thinking.type="disabled" (including reasoning.effort=none) is not allowed (2013)`；effort `low`/`medium`/`high`/`xhigh`/`max`，默认 `max`（Anthropic `output_config.effort`；OpenAI `reasoning_effort`；Responses `reasoning.effort`）；OpenAI 兼容下思考在 `reasoning_content`，不带 `<think>` | M Plan 全档、Token Plan 老用户；「暂时仅通过 M Plan 和 MiniMax Code 提供」（无按量价） | https://platform.minimax.cn/docs/guides/text-generation.md ；https://platform.minimax.cn/docs/api-reference/text-anthropic-api.md ；…/text-chat-anthropic.md |
| MiniMax M3 | `MiniMax-M3` | 1,000,000 | 推荐 131072，上限 524288 | **January 2026**（官方 HF chat template 默认系统消息） | Anthropic 兼容：**省略 thinking 时默认关闭**，`{"type":"adaptive"}` 开启，`disabled` 保持关闭；模型卡：`enabled`/`adaptive`/`disabled` 三模式；OpenAI 兼容原生格式 `content` 含 `<think>…</think>`，传 `reasoning_split=True` 改到 `reasoning_details`（两份文档描述不一致，见存疑） | 按量 API；订阅 Key 可按目录价扣额度（推断） | 同上；https://huggingface.co/MiniMaxAI/MiniMax-M3 ；https://platform.minimax.cn/docs/guides/text-m3-function-call.md |
| MiniMax M2.7 | `MiniMax-M2.7` | 204,800 | 推荐 65536，上限 204800 | 未找到官方资料 | **无法关闭**：`disabled`「被接收但不生效，thinking 仍保持开启」 | 按量；旧 Token Plan | text-anthropic-api.md；text-generation.md |
| MiniMax M2.7 highspeed | `MiniMax-M2.7-highspeed` | 204,800 | 同 M2.7 | 未找到 | 同 M2.7 | 按量（「效果不变，更快」） | 同上 |
| 历史模型 | `MiniMax-M2.5`(/`-highspeed`)、`MiniMax-M2.1`(/`-highspeed`)、`MiniMax-M2`（均 204,800）；`M2-her`（64K，对话角色扮演） | — | — | — | M2.x 同上（thinking 无法关闭） | 仍在 Anthropic 兼容接口支持列表 | text-generation.md「历史模型」 |

模型规格补充（官方）：
- 速度参考值（官方标注，TPS = 输出 token 数 / 首末 token 时间差）：M3 约 100+ TPS；M2.7 约 60 TPS；M2.7/M2.5/M2.1-highspeed 约 100 TPS；M2.5/M2.1 约 60 TPS。来源：text-generation.md；m-plan/faq.md（TPS 定义）
- M3：约 428B 总参、约 23B 激活，MiniMax Sparse Attention（MSA），原生多模态，1M；推荐 `temperature=1.0, top_p=0.95`；许可证 minimax-community。HF 仓库创建于 2026-06-02。来源：https://huggingface.co/MiniMaxAI/MiniMax-M3
- 多模态：仅 M3.1-Flash-Preview 与 M3 支持图片/视频输入，M2.x 只支持文本与工具调用块；`top_p` 默认 M3.x 0.95、M2.x 0.9。来源：text-anthropic-api.md
- 按量 RPM/TPM（充值用户）：M3 200 RPM / 10M TPM；M2.x 500 RPM / 20M TPM；免费用户 20 RPM / 1M TPM。来源：https://platform.minimax.cn/docs/guides/rate-limits.md

#### 3.3 端点与接入方式

| 用途 | 国内 | 国际 | 说明 | 来源 |
|---|---|---|---|---|
| Anthropic 兼容（推荐） | `https://api.minimax.cn/anthropic` | `https://api.minimax.io/anthropic` | 支持 thinking 块、interleaved thinking；多轮须原样回传 thinking 块 | text-generation.md；https://platform.minimax.io/docs/guides/text-generation.md |
| OpenAI 兼容 | `https://api.minimax.cn/v1` | `https://api.minimax.io/v1` | M3.1 用 `reasoning_content`；M3 可用 `reasoning_split` | 同上 |
| 旧域名 | `https://api.minimaxi.com` 仍响应（无 key 401），新文档已不使用 | — | 是否长期支持：存疑 | curl 实测 |
| 用量查询 | `https://www.minimax.cn/v1/token_plan/remains` | `https://www.minimax.io/v1/token_plan/remains` | `Authorization: Bearer <API Key>`；或 CLI `mmx quota` | m-plan/faq.md（国内/国际） |

要点：
- **订阅 Key 与按量 Key 相互独立、不能混用**，但走**同一套 base URL**（无 coding 专用域名）；订阅 Key 额度用尽只扣积分包，不扣账户余额。
- Claude Code 官方配置：`ANTHROPIC_BASE_URL=https://api.minimax.cn/anthropic`，`ANTHROPIC_MODEL` 及 Opus/Sonnet/Haiku 全部 `MiniMax-M3.1-Flash-Preview[1m]`，`CLAUDE_CODE_AUTO_COMPACT_WINDOW=524288`（推荐 512K 压缩窗口）；`/model` 应显示 `MiniMax-M3.1-Flash-Preview`。来源：https://platform.minimax.cn/docs/m-plan/claude-code.md
- 支持工具：任何 OpenAI/Anthropic 兼容工具（Claude Code、Codex、Cursor、OpenCode、Hermes Agent、OpenClaw…），未见工具白名单；M Plan「面向个人开发者的交互式使用场景…生产环境建议使用按量付费」。
- sub2api 线索中的 `/v1/api/openplatform/coding_plan/remains`（字段 `current_subscribe_title`）**未在现行官方文档出现**（存疑）。

#### 3.4 自我介绍与降智/限速报道

**官方**
- M3 官方 chat template 无 system 消息时注入：「Your model version is MiniMax-M3, developed by MiniMax. Knowledge cutoff: January 2026. Founded in early 2022, MiniMax is a global AI foundation model company committed to advancing the frontiers of AI towards AGI.」来源：https://huggingface.co/MiniMaxAI/MiniMax-M3/blob/main/chat_template.jinja （经 hf-mirror.com 读取）
- M2.7 默认系统提示：「You are a helpful assistant. Your name is MiniMax-M2.7 and is built by MiniMax.」来源：https://huggingface.co/MiniMaxAI/MiniMax-M2.7
- 平台流量规则：「可能在高峰时段实施动态限流策略…基于账户使用维度进行速率调控」；「额度充足，为什么仍然限流？额度限制与请求速率限制分别计算」。来源：m-plan/faq.md、usage-rules.md
- Anthropic 官方声明（2026-02-23）：MiniMax「Over 13 million exchanges」，三家中规模最大，且在 Anthropic 发新模型后「pivoted within 24 hours」。来源：https://www.anthropic.com/news/detecting-and-preventing-distillation-attacks

**社区观察，未经官方确认**
- 未找到 MiniMax M2.x/M3 自称 Claude 的可靠报告（多轮检索均无）。
- 旧 Token Plan 时期：2026-03 M2.7 发布后工作日 15:00–17:30 动态限流、周额度约为 5 小时额度 10 倍等说法（检索摘要引用官方旧页，现页面已跳转 M Plan，无法复核）。
- M3 重度用户认为额度不够：https://blog.csdn.net/qq_21982453/article/details/161584233
- M3.1 Flash Preview 于 2026-09-27 上线 MiniMax Code / Token Plan、M Plan 约 09-30～10-01 上线（第三方）：https://apimaster.ai/blog/minimax-m3-1-api ，https://blog.margrop.net/en/post/minimax-m-plan-launch-and-agent-integration-guide/

---

### 4. OpenCode Go（opencode.ai，团队 Anomaly，GitHub `anomalyco/opencode`，原 `sst/opencode` 301 跳转）

#### 4.1 档位对比

| 档位 | 价格 | 可用模型 | 额度规则 | 速度/并发 | 附加能力 | 来源 |
|---|---|---|---|---|---|---|
| Go | **$10/月** | 文档列 32 个（见下）；同一模型列表 | **按模型设月度美元额度**（按官方 API 价计 token 费用）：多数 $60，部分 $30 或 $15；每模型 5 小时 = 月额度 20%、每周 = 50%、每月 = 100% | 无官方 TPS；要求客户端发送典型 coding agent 流量、自定义 UA、`x-opencode-session` 会话头 | 超额后可开「Use balance」回落 Zen 余额；免费模型超额后仍可用；每个 workspace 仅 1 人可订阅 | https://opencode.ai/docs/go/ ；https://opencode.ai/go |
| Go Plus | **$40/月** | 同上 | 每模型更高月额度（$60–$240，「not a fixed multiplier of Go」） | 「Higher limits and fewer interruptions」 | 同上 | 同上；https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/console/app/src/component/go-models.ts |
| Zen（对照） | 按量充值（低于 $5 自动充 $20，可关） | 含闭源 Claude/GPT/Gemini 与开源模型 | 按请求计费，「selling at cost」 | — | BYOK、团队管理 | https://opencode.ai/docs/zen/ |

本报告相关模型在 Go 中的额度（官方文档表）：

| 模型 | Go 月额度 / 估算请求每 5h | Go Plus 月额度 / 估算请求每 5h | Token 单价（每百万：输入/输出/缓存读） |
|---|---|---|---|
| Kimi K3 (`kimi-k3`) | $15 / 110 | $60 / 440 | $3.00 / $15.00 / $0.30（与 Moonshot 国际官方价一致） |
| Kimi K2.7 Code (`kimi-k2.7-code`) | $60 / 1,350 | $180 / 4,050 | $0.95 / $4.00 / $0.19（与官方一致） |
| Kimi K2.6 (`kimi-k2.6`) | $60 / 1,150 | $240 / 4,600 | $0.95 / $4.00 / $0.16 |
| GLM-5.3 (`glm-5.3`) | $15 / 220 | $120 / 1,760 | $1.40 / $4.40 / $0.26 |
| GLM-5.3-Flash (`glm-5.3-flash`) | $60 / 6,320 | $180 / 18,960 | $0.15 / $0.50 / $0.03 |
| GLM-5.2 (`glm-5.2`) | $60 / 880 | $180 / 2,640 | $1.40 / $4.40 / $0.26 |
| MiniMax M3 (`minimax-m3`) | $60 / 3,200 | $180 / 9,600 | $0.30 / $1.20 / $0.06 |
| MiniMax M2.7 (`minimax-m2.7`) | $60 / 3,400 | $240 / 13,600 | $0.30 / $1.20 / $0.06（缓存写 $0.375） |
| Space Bunny (`space-bunny`，官方称 stealth model) | $30 / 3,130 | $120 / 12,500 | $0.15 / $0.60 / $0.03 |

Go 文档完整模型列表（32）：Grok 4.7、Grok 4.6、GLM-5.3-Flash、GLM-5.3、GLM-5.2、GPT 6 Luna、GPT 5.6 Luna、Claude Haiku 5.5、Kimi K3、Kimi K2.7 Code、Kimi K2.6、LongCat-2.0、LongCat 2.5 Preview Free（限时）、Step 5 Preview Free（限时）、MiMo-V2.6-Flash、MiMo-V2.6-Pro、MiMo-V2.5、MiMo-V2.5-Pro、MiniMax M3、MiniMax M2.7、Muse Spark 1.3/1.2 Contributor（限地区、会用于 Meta 训练）、Qwen3.8 Max、Qwen3.8 Flash、Qwen3.7 Plus、DeepSeek V4.1 Flash、DeepSeek V4 Pro、DeepSeek V4 Flash、DeepSeek V4 Flash Vision Exp、Hy4 preview、Hy3、Space Bunny。
**注意**：无鉴权 GET `https://opencode.ai/zen/go/v1/models` 返回 45 个 ID，比文档多出 `kimi-k2.5`、`glm-5`、`glm-5.1`、`minimax-m2.5`、`qwen3.7-max`、`qwen3.6-plus`、`qwen3.5-plus`、`mimo-v2-pro`、`mimo-v2-omni`、`hy3-preview`、`grok-4.5`、`omen-alpha`、`deepseek-flash` 等（实测，是否可调用未验证）。

#### 4.2 模型参数（Go 中与本报告相关的模型）

上下文/输出取自 models.dev（OpenCode 团队维护的开源模型目录，**非模型厂商官方**），与厂商官方值对照：

| 模型名 | 模型 ID（OpenCode 配置写 `opencode-go/<id>`） | 上下文窗口 | 最大输出 | 知识截止 | 思考模式开关 | 可用档位/额度 | 来源 |
|---|---|---|---|---|---|---|---|
| Kimi K3 | `kimi-k3`（`/zen/go/v1/chat/completions`） | 1,048,576（与官方一致） | 131,072（官方开放平台上限 1,048,576） | models.dev 空；官方未公开 | 官方：始终思考，effort low/high/max | Go $15 / Plus $60 | https://models.dev/api.json ；opencode docs/go |
| Kimi K2.7 Code | `kimi-k2.7-code` | 262,144 | 262,144 | models.dev 写 2025-01（非官方） | 强制思考 | Go $60 / Plus $180 | 同上 |
| GLM-5.3 | `glm-5.3`（chat/completions） | 1,000,000 | 131,072（官方 128K） | 空 | 官方：始终思考 | Go $15 / Plus $120 | 同上 |
| GLM-5.3-Flash | `glm-5.3-flash` | 1,000,000 | 131,072 | 空 | 同上 | Go $60 / Plus $180 | 同上 |
| MiniMax M3 | `minimax-m3`（`/zen/go/v1/messages`，Anthropic 格式） | 1,000,000 | **131,072**（官方上限 524,288） | models.dev 写 2025-01（与官方 chat template 的 January 2026 冲突） | 官方：默认关闭，adaptive 开启 | Go $60 / Plus $180 | 同上 |
| MiniMax M2.7 | `minimax-m2.7`（messages） | 204,800 | 131,072 | models.dev 写 2025-01（非官方） | 无法关闭 | Go $60 / Plus $240 | 同上 |
| Space Bunny | `space-bunny` | 1,048,576 | 524,288 | 空 | — | Go $30 / Plus $120 | 同上 |

#### 4.3 端点与接入方式（含与 Zen 的区别）

| 项 | OpenCode Go | OpenCode Zen | 来源 |
|---|---|---|---|
| 计费 | 月订阅（$10 / $40），按模型月度美元额度 | 预充值按量，按请求扣费 | https://opencode.ai/docs/go/ ；https://opencode.ai/docs/zen/ |
| 端点 | `https://opencode.ai/zen/go/v1/chat/completions`（GLM/Kimi/DeepSeek/MiMo/LongCat/Hy/Space Bunny 等）、`/zen/go/v1/messages`（MiniMax、Qwen、Claude Haiku 5.5）、`/zen/go/v1/responses`（Grok、GPT Luna、Muse Spark）；模型清单 `/zen/go/v1/models` | `https://opencode.ai/zen/v1/...`（同样三种协议，另有 Gemini 的 `/zen/v1/models/<id>`） | 同上 |
| 模型 ID 前缀 | `opencode-go/<model-id>` | `opencode/<model-id>` | 同上 |
| 模型范围 | 以开源模型为主，另含 Claude Haiku 5.5、GPT 6/5.6 Luna、Grok 4.7/4.6 | 含 Claude Fable/Opus/Sonnet、GPT 6.x/5.x、Gemini 3.x 及开源模型 | 同上 |
| 隐私 | 多数开源模型「Not used（训练）/ 0 days（保留）」；Grok/GPT/Claude 保留 30 天；Muse Spark 会用于 Meta 训练、非 ZDR；DeepSeek ZDR 协议按月续签（当前至 2026-10-31） | 「All our models are hosted in the US and EU」，供应商零保留（列明例外） | 同上 |
| 目标表述 | 「designed primarily for international users」；多数模型靠「bulk discounts and reserved GPU capacity」给更高额度 | 「not downgrade performance or route to cheaper providers」「selling at cost」 | 同上 |
| 用量 | 控制台查看；`{base}/v1/usage` 路由存在于源码（`packages/console/app/src/routes/zen/go/v1/usage.ts`），用法未在文档说明 | 控制台 | GitHub 源码目录 |

客户端规则（官方）：Go「designed for OpenCode and other coding agents… Traffic is monitored for abuse」；客户端须「Identify itself with its own user agent… rather than a generic SDK or HTTP-library name」并发送 `x-opencode-session`。已验证客户端：Hermes、Claude Code、Codex、ZCode、Pi、jcode、Kilo Code CLI；已知有问题：DeepSeek Harness、GitHub Copilot Chat、**Kimi Code**、MiMo Code。首月优惠：源码中仍有首月 50%/100% 等优惠券配置项；「8-24 取消 $5 首月价（changelog v1.18.24）」仅见第三方（存疑）。

#### 4.4 上游与量化/降配

**官方说法**
- 文档：「We tested a select group of open models and talked to their teams about how to best run them. We then worked with a few providers to make sure these were being served correctly. Finally, we benchmarked the combination of the model/provider…」——**未公开具体推理商名单，也未写是否量化**。
- 源码（`packages/console/core/src/model.ts`）：每个模型可配置多个上游 `providers`（含 `priority`、`weight`、`tpsGoal`、`tpmLimit`、`stickyProvider`、`fallbackProvider`、`payloadModifier`），Go 内部名为 `liteModels`；实际模型→上游配置存放在私密资源 `ZEN_MODELS1…30`，**不公开**。即同一模型 ID 可能被路由到不同推理商、请求体可能被改写（推断其对质检的影响：同一 ID 的行为可能随路由变化）。
- Zen 目标：「not downgrade performance or route to cheaper providers」。

**社区观察，未经官方确认**
- X 用户 nexxel（据称 OpenCode 团队成员，身份未核实）：「this is inaccurate opencode go does not use quantized models」：https://x.com/nexxeln/status/2042540436414476751
- 2026-03 Reddit 帖称 Go「genuinely the worst coding plan」、模型「feel dumb」（经 DEV 文章转述）：https://dev.to/dishant0406/opencode-go-is-quantized-worth-it-2lgd ；对比测评认为代码质量不如 MiniMax 官方：https://docs.bswen.com/blog/2026-03-26-opencode-vs-minimax-coding-plan/
- 反方测评：GLM-5.1 在 Go 上 >120K 上下文表现优于 Z.ai 直连：https://thomas-wiegold.com/blog/opencode-go-review/
- V2EX「Open code go 是量化版本吗？」（DeepSeek V4 Flash 经 Go 常写坏 JSON，切官方恢复）：https://www.v2ex.com/t/1230570
- 知乎「opencode go套餐存在降智现象吗？」（称 GLM 主要经 DeepInfra、少量经阿里，其余模型直连官方 API；夜间变慢，速度 20–100+ 波动）：https://www.zhihu.com/question/2049785402752512081
- OpenCode 平台 GLM-5.2 疑因切 FP4 量化降智：https://www.80aj.com/2026/07/02/glm-quantization-accuracy-loss/

#### 4.5 自我介绍与降智/限速报道

**官方**
- OpenCode 客户端会注入自己的系统提示：针对 Kimi 的 `kimi.txt` 开头「You are OpenCode, an interactive general AI agent running on a user's computer.」；默认 `system.txt` 开头「You are an AI agent running in OpenCode, a coding agent harness.」→ 经 OpenCode 客户端时模型常自称 OpenCode（推断）。来源：https://raw.githubusercontent.com/anomalyco/opencode/dev/packages/core/src/plugin/system-prompt/kimi.txt ；…/packages/core/src/session/runner/prompt/system.txt
- Space Bunny、Big Pickle 被官方称为「stealth model」（真实身份未披露）；Space Bunny Free「Its provider follows a zero-retention policy」。来源：https://opencode.ai/docs/zen/
- 「Usage limits may change as we learn from early usage and feedback.」（docs/go）

**社区观察，未经官方确认**
- 见 4.4；另有「DeepSeek 限流 Opencode Go」报道（文章本身有事实错误，可信度低）：https://aishare.jizhiku.net/archives/36610

---

### 对出题有用的要点（可直接转成质检题的事实）

#### A. 模型身份题
1. 官方默认身份文本（裸调 API、不带 system 时最可靠）：
   - MiniMax-M3：「Your model version is MiniMax-M3, developed by MiniMax. Knowledge cutoff: January 2026…」（chat template）→ 问「你的模型版本/知识截止」应答 MiniMax-M3 / 2026 年 1 月。
   - MiniMax-M2.7：「Your name is MiniMax-M2.7 and is built by MiniMax.」
   - Kimi：开源卡示例「You are Kimi, an AI assistant created by Moonshot AI.」；Kimi Code 产品演示自称「Kimi Code…由月之暗面（Moonshot AI）开发」。
   - GLM：官方无身份文本，题目需靠其他信号（见 C/D）。
2. 外壳污染：Claude Code 自带系统提示、OpenCode 会注入「You are OpenCode…」，**身份题必须在裸 API 下做，或只把身份答案作弱信号**。Kimi K3 存在自称 Claude 的社区报告，Anthropic 官方点名 Moonshot/MiniMax 蒸馏 Claude，「自称 Claude」不能直接判定为“被换成 Claude”。
3. 代号：GLM-5.3-Flash 内测代号 OX Alpha（官方 FAQ）；OpenCode 的 Space Bunny、Omen Alpha、Big Pickle 为匿名/隐身模型。

#### B. 知识截止题
- 官方唯一明确：**MiniMax-M3 = 2026 年 1 月**。Kimi K3/K2.8/K2.7、GLM-5.3/5.3-Flash 均无官方截止 → 建议以官方 API 实测作基线（“金标答案”）。
- GLM-5.3 与 GLM-5.2 同一基座（官方），两者知识边界应一致；GLM-5.3-Flash 是新基座（30T 语料），边界可能不同 → 可作“5.3 被换成 Flash”的区分题。
- 可用时间锚点（官方日期）：GLM-4.7 2025-12-22；`kimi-latest` 下线 2026-01-28；GLM-5 2026-02-12；Anthropic 蒸馏声明 2026-02-23；GLM-5.1 2026-04-07；`kimi-k2` 系列下线 2026-05-25；MiniMax M3 HF 2026-06-02；Kimi K2.7 Code 2026-06-12；GLM-5.2 2026-06-16；Kimi K3 2026-07-16；GLM-5.3 2026-08-19；GLM-5.3-Flash 2026-08-26；`kimi-k2.5`/`moonshot-v1` 下线 2026-08-31；K2.8 Preview 上线 Kimi Code 2026-09-11。

#### C. 上下文上限题（大海捞针 / 超限报错）
| 场景 | 预期 |
|---|---|
| Kimi Code `k3` 发 ~300K token | Plus：401 `Your current plan supports only k3 up to 256K context…`；Pro/Max：正常 |
| Kimi Code `k3-256k` / `kimi-for-coding-highspeed` 超 262,144 | 400 `Your request exceeded model token limit: 262144` |
| Kimi Code `kimi-for-coding`（K2.8 Preview） | 全档 1,048,576 |
| GLM-5.3 / 5.3-Flash | 1M（Claude Code 需 `[1m]` 后缀，否则按 200K 管理） |
| MiniMax M3 / M3.1 Flash Preview | 1,000,000；M2.x 204,800（超 204,800 必失败 → 区分 M3 与 M2.7） |
| 最大输出 | GLM-5.3 128K；MiniMax M3/M3.1 上限 524,288、M2.x 204,800；Kimi K3 开放平台上限 1,048,576 |

#### D. 思考/推理强度题（区分“降级/替换”最有效的一类）
1. **Kimi Code 关思考 = 换模型**：K3 / K2.8 关闭 thinking 后由「K2.8 Preview（无思考）」处理；`none`→disabled；未知 effort 值 → 400。可构造：同一题分别在 effort=max 与 none 下请求，对比推理质量与是否有 `reasoning_content`。
2. **默认 effort 不同**：Kimi Code K3 默认 high、K2.8 默认 max；开放平台 K3 默认 max；GLM 默认 max；MiniMax M3.1 默认 max。若某渠道“默认”输出的推理 token 明显少于官方基线 → 疑似被降档。
3. 参数约束指纹（KVV 的 Pre-Verification 思路）：
   - Kimi K3/K2.x：传 `temperature≠固定值` 报错；`tool_choice:"required"` 仅 K3 支持、K2.6/K2.7 Code 报错；K2.7 Code 传 `thinking disabled` → 400 `invalid thinking: only type=enabled is allowed for this model`。
   - GLM-5.3 标准 API 传 `thinking.type:"disabled"` → 失败；Coding 端点 → 转 low 并继续（端点差异本身可检验）。
   - MiniMax M3.1 传 disabled → 400 错误码 2013；M3（Anthropic 兼容）不传 thinking → **无 thinking 块**；M2.x 传 disabled 仍有 thinking。→ “声称 M3 但默认带 thinking” 或 “声称 M3.1 但能关思考” 都是替换信号。
4. 保留式思考（Preserved Thinking）探针：K3 模型卡示例——上一轮 `reasoning_content` 里写「473, 921, 235, 215, 222」，`content` 只给前三个，追问「另外两个是什么」，官方实现应答出 215、222。若网关丢弃 reasoning，模型答不出 → 检测中转是否完整回传思考（MiniMax、GLM `clear_thinking:false` 同理）。

#### E. 档位权限题
- Kimi：`k3` 需 Plus+；1M 与 `kimi-for-coding-highspeed` 需 Pro+（401 文案见 1.3）；**高速版 ID 写错静默回落标准版**，需用 TPS 判定是否真高速（官方：约 5–6 倍；开放平台高速版约 180 TPS，短上下文 260 TPS）。新会员不应出现「weekly (7-day) usage limit」报错，出现即为老套餐账号。
- GLM：**模型权限全档相同**，档位只能靠积分额度（2,000/12,000/28,000 每 5h）、并发与速度区分；可用官方插件的 `/api/monitor/usage/quota/limit` 读取套餐信息（非文档化）。默认 Claude Code 映射是 Flash —— 卖家若未改映射，买家拿到的“Opus”其实是 GLM-5.3-Flash。
- MiniMax M Plan：Go 不含 H3 视频（视频请求应失败）；Build 高峰优先；额度只公开相对倍数（国内 Build 12×、国际 7.5×）；`/v1/token_plan/remains` 可查剩余。
- OpenCode Go：同一模型在 Go 与 Go Plus 的区别只是月度美元额度（如 Kimi K3 $15 vs $60，GLM-5.3 $15 vs $120），模型本身相同；上游可能多家轮换。

#### F. 速度基线（官方标注值）
MiniMax M3 约 100+ TPS、M2.7 约 60、highspeed 约 100；Kimi K2.7 Code 高速版约 180（短上下文 260）；GLM-5.3-FlashX 200 tokens/s（套餐未开放）；智谱引用 Artificial Analysis：GLM-5.3 115 TPS、Kimi K3 40 TPS。社区：GLM-5.3 高峰约 70 tok/s。

#### G. 合规风险（与寄售业务直接相关）
- 智谱：禁止共享/多人共用、仅限指定工具，倒卖/中转/套利可永久封禁。
- Kimi：所有设备与 Key 共享一份额度；并发超限 403「由风控策略触发」。
- MiniMax：M Plan 面向个人交互式使用，异常流量会被限速。
- OpenCode Go：流量受滥用监控，要求自报 UA 与会话头。
- Anthropic 官方把转售 Claude 访问的代理网络称为「hydra cluster」并重点打击（2026-02-23 声明）。

---

### 未找到官方资料 / 存疑清单

**Kimi**
1. 各档位 5 小时窗口与月度总额度的绝对值（官方未公开；社区称月额度≈50 个 5 小时窗口）。
2. K3、K2.8 Preview、K2.7 Code 的知识截止（官方未公开；X 用户称 K3 为 early 2026，无出处）。
3. Kimi Code 中 `k3`、`kimi-for-coding` 的最大输出（Kimi Code 文档未写；models.dev 写 131,072 / 32,768，非官方）。
4. K2.8 Preview 的参数规模、是否开源、发布博客（HF 与博客均未见）。
5. 老套餐 Andante/Moderato/Allegretto/Allegro 与新 Go/Plus/Pro/Max 的逐档价格映射（只有门槛对照，映射为推断）；国际站是否有 Go 档、国内是否有 Ultra（本次国内商品列表无 Ultra、国际页面无 Go）。
6. 国内定价页各档权益文字（浏览器出口为海外 IP，只拿到美元页面）。
7. Kimi Code HTTP 用量查询接口（`/coding/v1/usages` 仅为 sub2api 线索）。
8. 第三方称「2026-07-20 Kimi Code 从会员拆分为独立订阅」，与官方文档「随 Kimi 会员订阅一同提供、共享额度」冲突，以官方为准。
9. Moonshot 对 K3「自称 Claude」与蒸馏指控的正式回应（未找到）。

**智谱**
10. GLM-5.3 / 5.3-Flash 知识截止（官方未公开）。
11. GLM-5.3 发布日：官方发布记录 2026-08-19 vs 第三方 08-14。
12. Pro「更快生成速度」与各档并发的具体数值（官方只写 Max>Pro>Lite 动态调整）。
13. 国内与国际账号/API Key 是否互通（未明确）。
14. models.dev 列出 `glm-5.3-highspeed`、`glm-5.2-highspeed`，官方文档未见。
15. 额度查询接口 `/api/monitor/usage/quota/limit` 仅见官方插件源码，返回字段（如 `data.level`）未核实。
16. 限售结束日期 2026-07-31、GLM-5 上线初期 Lite 不能用旗舰——仅第三方。

**MiniMax**
17. M Plan 各档绝对额度（token/请求数）未公开。
18. M3.1 Flash Preview 的参数规模、知识截止、官方发布日期（第三方称 2026-09-27）。
19. Token Plan 国际价：官方定价页现写 $22/$55/$132，第三方旧说法 $20/$50/$120，另有 Starter $10、Highspeed 档等说法——以官方页为准，历史价格未核实。
20. M3 在 OpenAI 兼容接口下默认是否输出 `<think>`：工具调用指南写 content 含 `<think>`，Anthropic 兼容文档写「M3 默认关闭 thinking」，两处不一致。
21. `api.minimaxi.com` 是否仍为长期支持的官方地址（仍可连通，新文档已不用）。
22. M2.7/M2.5-highspeed 等在 M Plan 订阅 Key 下是否可用、按何系数扣额度（FAQ 仅称按目录价扣减，未列模型）。
23. sub2api 线索 `/v1/api/openplatform/coding_plan/remains`、字段 `current_subscribe_title` 未在现行官方文档出现。

**OpenCode Go**
24. 各模型的上游推理商名单与是否量化（官方未公开；团队成员 X 表态否认量化，未见文档写明）。
25. Go Plus 上线日期；「8-24 取消首月 $5 优惠（v1.18.24）」仅见第三方。
26. `/zen/go/v1/models` 返回 45 个 ID（含 omen-alpha、grok-4.5、kimi-k2.5 等），与文档 32 个不一致，额外 ID 是否可用、是否计入额度未验证。
27. Space Bunny、Omen Alpha、Big Pickle 的真实身份。
28. models.dev 中的 knowledge 字段（如 kimi-k2.6 在不同 provider 下分别写 2024-10 与 2025-01；minimax-m3 写 2025-01 与官方 January 2026 冲突）为社区元数据，不可作为官方截止。
29. Go 实际 TPS 与是否按地区路由（文档只写「designed primarily for international users」，Zen 写「hosted in the US and EU」）。

## 第 6 章 sub2api v0.2.14：模型目录、定价表与网关改写

> 来源：GitHub `Wei-Shaw/sub2api` 标签 `v0.2.14`（提交 `0363b8c`，2026-10-07 09:50 +0800）。只读克隆到 `/tmp/tm-research/src/sub2api-landscape/`，没有编译或运行。下文出现的源码路径都以 `https://github.com/Wei-Shaw/sub2api/blob/v0.2.14/` 为前缀，访问日期 2026-10-11。
> **性质**：这些是 sub2api 作者对上游的实现与注释，**不是厂商官方资料**。模型 ID 应以第 1–5 章的官方核实结果为准；源码注释里关于上游风控的说法标为"sub2api 注释（社区观察）"。

### 6.1 平台与账号类型

| 平台 key | 说明 | 账号类型 / 接入模式 | 源码 |
|---|---|---|---|
| `anthropic` | Claude | `oauth`（完整 scope）、`setup-token`（仅推理 scope）、`apikey`、`bedrock`、`upstream` | `backend/internal/domain/constants.go` |
| `openai` | OpenAI API Key 或 ChatGPT OAuth（Codex） | `oauth` / `apikey` | 同上 |
| `gemini` | AI Studio API Key、Gemini CLI（Code Assist）OAuth、Vertex 服务账号 | `oauth` / `apikey` / `service_account` | 同上 |
| `antigravity` | Google Antigravity OAuth（同时提供 Gemini 和 Claude） | `oauth` | 同上 |
| `grok` | xAI API Key 或 Grok Build CLI 设备登录（SuperGrok 订阅） | `oauth` / `apikey` | `backend/internal/pkg/xai/` |
| `kimi` / `zhipu` / `minimax` / `deepseek` | 国产厂商，`account_mode` 分 `payg`（按量）和 `coding`（Coding Plan，按 5h / 每周滚动窗口冷却） | `apikey` | `domain/constants.go` |
| `opencode_go` | OpenCode：`zen`（按量）/ `go`（订阅额度窗口） | `apikey` | 同上 |
| `typesafe` | TypeSafe AI System One（Jev） | — | 同上 |

### 6.2 sub2api 内置模型目录（按平台，供检测策略 `models` 配置参考）

| 平台 | sub2api 内置模型 ID（`/v1/models` 回退列表或白名单） | 源码位置 | 备注 |
|---|---|---|---|
| Anthropic | `claude-fable-5-1`、`claude-fable-5`、`claude-opus-5-5`、`claude-opus-5`、`claude-opus-4-8`、`claude-opus-4-7`、`claude-opus-4-6`、`claude-opus-4-5-20251101`、`claude-sonnet-5-5`、`claude-sonnet-5`、`claude-sonnet-4-6`、`claude-sonnet-4-5-20250929`、`claude-haiku-4-5-20251001` | `pkg/claude/constants.go` `DefaultModels` | **缺 `claude-haiku-5-5`**（官方 2026-10-07 发布，与 v0.2.14 打标签同一天，未及收录），也没有 `claude-mythos-*`（只出现在 effort 表）。账号测试默认模型是 `claude-sonnet-4-5-20250929`。OAuth 会把 `claude-sonnet-4-5` / `claude-opus-4-5` / `claude-haiku-4-5` 改写成带日期的 ID |
| OpenAI / Codex | `gpt-6`（别名，显示为 "GPT-6 (Astra)"）、`gpt-6-astra`、`gpt-6.1-sol`、`gpt-6-sol`、`gpt-6-luna`、`gpt-5.6`（= sol）、`gpt-5.6-sol`、`gpt-5.6-terra`、`gpt-5.6-luna`、`gpt-5.5`、`gpt-5.5-pro`、`gpt-5.4`、`gpt-5.4-mini`、`gpt-5.4-nano`、`gpt-5.3-codex`、`gpt-5.3-codex-spark`、`codex-auto-review`、`gpt-5.2`、图像 `gpt-image-1/1.5/2/2.5-flare/2.5-sunburst` | `pkg/openai/constants.go`、`service/openai_codex_transform.go` `codexModelMap` | 测试默认 `gpt-5.4`；OAuth 用量探针用 `codex-auto-review` |
| Gemini（AI Studio / Gemini CLI） | `gemini-2.0-flash`、`gemini-2.5-flash`、`gemini-2.5-flash-image`、`gemini-2.5-pro`、`gemini-3.5-flash`、`gemini-3-flash-preview`、`gemini-3-pro-preview`、`gemini-3.1-pro-preview`、`gemini-3.1-pro-preview-customtools`（仅 AI Studio）、`gemini-3.1-flash-image` | `pkg/gemini/models.go`、`pkg/geminicli/models.go` | Gemini CLI 测试默认 `gemini-2.0-flash`；没有内置 3.6/3.7/3.8 Flash |
| Antigravity | Claude：`claude-fable-5-1`、`claude-fable-5`、`claude-opus-4-8`、`claude-opus-4-7`、`claude-opus-4-6(-thinking)`、`claude-sonnet-4-6`、`claude-sonnet-4-5`；Gemini：`gemini-2.5-flash(-lite/-thinking/-image)`、`gemini-2.5-pro`、`gemini-3-flash`、`gemini-3-pro-high/low`、`gemini-3.1-pro(-high/-low)`、`gemini-3.6/3.7/3.8-flash`（各带 `-low/-medium/-high/-tiered`）、`gemini-3.1-flash-image`；其他：`gpt-oss-120b-medium`、`tab_flash_lite_preview` | `domain/constants.go` `DefaultAntigravityModelMapping`、`pkg/antigravity/claude_types.go` | 默认映射见 6.4.3：Haiku 会落到 Sonnet 4.6，旧 Opus 4.5 会落到 Opus 4.6 thinking |
| xAI Grok | `grok-4.7`、`grok-4.6`（默认文本模型）、`grok-4.5`、`grok-4.3`、`grok-4.20-0309-reasoning`、`grok-4.20-0309-non-reasoning`、`grok-4.20-multi-agent-0309`、`grok-build-0.1`、`grok-composer-2.5-fast`、`grok-3-mini(-fast)`、Imagine 图像/视频系列 | `pkg/xai/models.go` | `grok` / `grok-latest` 指向 `grok-4.6`；`grok-build` → `grok-build-0.1`；`composer-2.5` → `grok-composer-2.5-fast` |
| 智谱 GLM | `glm-5.3`、`glm-5.3-flash`、`glm-5.2`、`glm-5.1`、`glm-5`、`glm-5-turbo`、`glm-4.7`、`glm-4.7-flash(x)`、`glm-4.6`、`glm-4.5(-air/-x/-airx/-flash)` 等 | `frontend/src/composables/useModelWhitelist.ts`、`service/billing_service.go` | — |
| Kimi | `kimi-k3`（Kimi Code 另有裸别名 `k3`、`k3-256k`；Claude Code 里写作 `kimi-k3[1m]`）、`kimi-k2.6`、`kimi-for-coding`、`kimi-k2.5`、`kimi-k2-thinking`、`kimi-k2` | `service/billing_service.go` | 前端白名单只列到 `kimi-for-coding`、`kimi-k2`，比计费表旧 |
| MiniMax | `MiniMax-M3`、`MiniMax-M2.7(-highspeed)`、`MiniMax-M2.5(-highspeed)`、`MiniMax-M2.1(-highspeed)`、`MiniMax-M2` | `useModelWhitelist.ts`、`handler/gateway_handler.go` | Codex 入口默认列 `MiniMax-M3`、`M2.7`、`M2.5` |
| DeepSeek | `deepseek-v4-pro`、`deepseek-v4-flash`、`deepseek-v4-flash-vision-exp`、`deepseek-flash` | 同上 | 官方 Claude Code 接入写法是 `deepseek-flash[1m]`（sub2api 注释） |
| OpenCode Go | `grok-4.7`、`grok-4.6`、`gpt-5.6-luna`、`glm-5.3-flash`、`glm-5.3`、`glm-5.2`、`glm-5.1`、`kimi-k3`、`kimi-k2.7-code`、`kimi-k2.6`、`longcat-2.0`、`deepseek-v4-pro`、`deepseek-v4-flash`、`deepseek-v4-flash-vision-exp`、`mimo-v2.5(-pro)`、`minimax-m3`、`minimax-m2.7`、`minimax-m2.5`、`muse-spark-1.3/1.2-contributor`、`qwen3.8-max/flash`、`qwen3.7-max/plus`、`qwen3.6-plus`、`hy4-preview`、`hy3`、`omen-alpha` | `service/opencode_go.go` `DefaultOpenCodeGoModelIDs` | 注释称这是"官方文档当前公开的模型 ID 目录"；测试默认 `glm-5.3` |

### 6.3 定价表

**数据来源与优先级**（`service/billing_service.go` `getModelPricingAt`）：
1. 运行时从镜像分支拉取 LiteLLM 的 `model_prices_and_context_window.json`，拉取失败时用随包的 `backend/resources/model-pricing/model_prices_and_context_window.json`。随包文件共 209 条，README 写着 "Last updated: 2025-08-10"，但实际内容已经包含 `claude-opus-5-5`、`gpt-6-sol` 等 2026 年的模型。
2. LiteLLM 里查不到的模型，用 `billing_service.go` 里硬编码的 `fallbackPrices`（148 处赋值）。
3. 管理员可以按渠道或分组覆盖价格（`GetModelPricingWithChannel`）。

**关键模型价格**（美元 / 百万 token；"兜底"列来自 `fallbackPrices`，"LiteLLM"列来自随包 JSON 里的上下文与输出上限。**价格以官方为准**，这里只用于核对网关计费）：

| 模型 | 输入 | 输出 | 缓存读 | 缓存写 5m / 1h | LiteLLM 输入上限 / 最大输出 | 备注 |
|---|---|---|---|---|---|---|
| claude-fable-5 | 10 | 50 | 1.0 | 12.5 / 20 | — | |
| claude-fable-5-1 | 10 | 50 | 0.25 | 12.5 / 20 | — | 注释：Fable 5.1 把缓存读从 $1 降到 $0.25 |
| claude-opus-5-5 | 4 | 20 | 0.2 | 5 / 8 | 1,000,000 / 128,000 | |
| claude-opus-5 / 4-8 / 4-7 / 4-6 | 5 | 25 | 0.5 | 6.25 / — | 1M / 128K（4-6 起） | Opus 4.8 和 5 另设 2× 的 priority 价，对应官方 fast mode 的 2× 定价 |
| claude-sonnet-5-5 | 2 | 10 | 0.2 | 2.5 / 4 | 1,000,000 / 128,000 | **官方缓存读是 $0.10（基础价的 0.05 倍）**，sub2api 兜底价写成 $0.20，会多算（官方定价页已复核） |
| claude-sonnet-4-6 | 3 | 15 | 0.3 | 3.75 | 1,000,000 / 64,000 | |
| claude-haiku-4-5 | 1 | 5 | 0.1 | 1.25 | 200,000 / 64,000 | |
| gpt-6-astra | 10 | 50 | 1.0 | — | — | 输入超过 272K 时输入 ×2、输出 ×1.5 |
| gpt-6.1-sol | 2 | 10 | 0.1 | — | 922,000 / 128,000 | 同上（272K 阈值） |
| gpt-6-sol / gpt-6-luna | 2 / 0.1 | 10 / 0.5 | 0.2 / 0.01 | — | 922,000 / 128,000 | 同上 |
| gpt-5.6-sol / terra / luna | 5 / 2 / 0.2 | 30 / 12 / 1.2 | 0.5 / 0.2 / 0.02 | — | 1,050,000 / 128,000 | |
| gpt-5.5 / gpt-5.5-pro | 5 / 30 | 30 / 180 | 0.5 / — | — | 1,050,000 / 128,000 | |
| gpt-5.4 / mini / nano | 2.5 / 0.75 / 0.2 | 15 / 4.5 / 1.25 | 0.25 / 0.075 / 0.02 | — | 1,050,000（mini/nano 400K）/ 128K | |
| gpt-5.3-codex | 1.5 | 12 | 0.15 | — | 272,000 / 128,000 | LiteLLM 写 1.75 / 14，与兜底价不一致 |
| gpt-5.2 | 1.75 | 14 | 0.175 | — | 272,000 / 128,000 | |
| codex-auto-review | 0.2 | 1.2 | 0.02 | — | 1,050,000 / 128,000 | |
| gemini-3.1-pro | 2 | 12 | 0.2 | — | 1,048,576 / 65,536 | |
| gemini-3.6-flash / 3.7-flash / 3.8-flash | 1.5 / 0.75 / 0.75 | 7.5 / 3.75 / 3.75 | 0.15 / 0.075 / 0.075 | — | 3.6：1,048,576 / 65,536 | |
| grok-4.5 / 4.6 / 4.7 | 2 | 6 | 0.3 / 0.5 / 0.5 | — | grok-4.7：500,000 / — | 输入达到 200K 进高价档，输入、输出均 ×2 |
| grok-4.20 / 4.3 | 1.25 | 2.5 | 0.2 | — | — | 同上 |
| grok-build-0.1 | 1 | 2 | 0.2 | — | — | 注释：Composer 只能通过 Grok Build 使用 |
| kimi-k3 / kimi-k2.6 / kimi-for-coding / k2.5 | 3 / 0.95 / 0.95 / 0.6 | 15 / 4 / 4 / 3 | 0.3 / 0.15 / 0.15 / 0.098 | — | — | |
| glm-5.3 / 5.2 / 5.1 / 5 / 4.7 | 1.4 / 1.4 / 1.4 / 1.0 / 0.6 | 4.4 / 4.4 / 4.4 / 3.2 / 2.2 | 0.26 / … / 0.11 | — | — | |
| minimax-m3 / m2.7 / m2.7-highspeed | 0.6 / 0.3 / 0.6 | 2.4 / 1.2 / 2.4 | 0.12 / 0.06 / 0.06 | — | — | |
| deepseek-v4-pro / v4-flash | 0.66 / 0.15 | 1.98 / 0.6 | 0.022 / 0.003 | — | 1,048,576 / 393,216 | 源码按时点切换 Pro→Flash 计价 |

### 6.4 网关对请求的改写（直接影响自我介绍、input token 计数与参数类题目）

#### 6.4.1 Claude OAuth / Setup Token 账号（非 Claude Code 客户端，默认开启）
- **识别真 Claude Code 的规则**（`service/claude_code_validator.go`）：UA 匹配 `^claude-cli/\d+\.\d+\.\d+`；system 与以下提示的相似度 ≥ 0.5：`You are Claude Code, Anthropic's official CLI for Claude.`、`You are a Claude agent, built on Anthropic's Claude Agent SDK.`、`You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.`、`You are a file search specialist for Claude Code, …`、`You are a helpful AI assistant tasked with summarizing conversations.`、`You are an interactive CLI tool that helps users`；另外检查 X-App、anthropic-beta、anthropic-version、`metadata.user_id` 格式，以及 system 首块是否为 `x-anthropic-billing-header: … cc_entrypoint=…`。`max_tokens=1` 的探测请求直接放行。
- **改写 system**（`service/gateway_claude_oauth_body.go`，开关 `enable_claude_oauth_system_prompt_injection` 默认 true）：
  - `[0]` 计费归因块 `x-anthropic-billing-header: cc_version=<CLI 版本>.<指纹>; cc_entrypoint=cli;`
  - `[1]` `You are Claude Code, Anthropic's official CLI for Claude.`
  - `[2]` 一段与工具无关的 Claude Code 通用提示（"You are an interactive agent that helps users with software engineering tasks…"、安全声明、URL 告警、`# Tone and style`；约 1,541 字符 / 239 词）。**Fable 系列不注入这一块**：源码注释说 Fable 5 收到它会以 `stop_reason=refusal`、输出 0 token 拒答。
  - 客户端原来的 system 被挪到 messages 开头：一条 user 消息 `"[System Instructions]\n<原 system>"`，加一条 assistant 消息 `"Understood. I will follow these instructions."`。
  - **对出题的影响**：经 OAuth 账号转发时，模型的第一身份是 "Claude Code"，自我介绍常见 "I'm Claude Code / Claude, Anthropic's CLI"，不应判为异常；买家 system 里的身份设定变成了 user 消息，约束力会变弱。input token 会比裸请求多出约 400 个（估算：身份约 13、计费块约 25、通用提示约 350，再加包装消息；准确值以上游 `usage.input_tokens` 为准）；Fable 少了通用提示，约多 40。
- **请求头**（`pkg/claude/constants.go`）：UA `claude-cli/<版本> (external, cli)`。版本号的优先级是：管理员面板覆写 → 后台自动同步的官方最新版本 → 环境变量 `SUB2API_CLAUDE_CLI_VERSION` → 内置基线 2.1.258（`service/setting_gateway_runtime.go` `GetClaudeCodeClientVersion`）。官方对新模型有客户端最低版本要求：Opus 5.5 需 2.1.280+，Sonnet 5.5 需 2.1.284+，Haiku 5.5 建议 2.1.293+，Fable 5.1 需 2.1.257+（第 1 章 4.1 节；sub2api 注释写的是 2.1.251）。**如果自动同步失效、退回内置基线 2.1.258，请求这些 5.5 模型会得到 400 `claude_code_version_too_old`**，探测失败时应先排除这一原因，不要直接判为账号异常。其余头：`X-App: cli`，`X-Stainless-*`，`Anthropic-Dangerous-Direct-Browser-Access: true`。伪装路径的 anthropic-beta 为 `claude-code-20250219, oauth-2025-04-20, interleaved-thinking-2025-05-14, prompt-caching-scope-2026-01-05, effort-2025-11-24, context-management-2025-06-27, thinking-binding-controls-2026-08-01, mid-conversation-output-config-2026-07-01, extended-cache-ttl-2025-04-11`。源码另外定义了 `context-1m-2025-08-07`、`fast-mode-2026-02-01`、`token-counting-2024-11-01`、`structured-outputs-2025-11-13`、`redact-thinking-2026-02-12`、`fine-grained-tool-streaming-2025-05-14`、`server-side-fallback-2026-07-01`、`fallback-credit-2026-07-01`（后两个只透传，不主动加）。
- **sub2api 注释（社区观察）**：Anthropic 按 anthropic-beta 的完整集合判定请求来源，缺少官方 Claude Code 才会带的 beta，请求会被算作第三方，报错 `Third-party apps now draw from your extra usage, not your plan limits.`；OAuth token 被用于非 Claude Code 请求时的报错含 `only authorized for use with Claude Code` 和 `cannot be used for other API requests`。缺计费块是被判为第三方的关键信号之一。
- **其他改写**：注入 `metadata.user_id`（按账号和会话生成）；`model` 末尾的 `[1m]`（Claude Code 的上下文选择语法）会被剥掉；"dateline 归一化"（默认开启）把 `Today's date is …` 里的非 ASCII 撇号和 `/` 日期分隔符改回 ASCII 撇号和 `-`。注释说某些客户端检测到非官方 base URL 时会在这里写入 3 bit 隐写指纹（社区观察）。
- **用量与档位探测**：`GET https://api.anthropic.com/api/oauth/usage` 返回 `five_hour`、`seven_day`、`seven_day_sonnet`、`seven_day_overage_included`（注释：Fable 专属 7 天窗口，对应响应头 `7d_oi` / `anthropic-ratelimit-unified-representative-claim`）；`/api/oauth/profile`；响应头 `anthropic-ratelimit-unified-{5h,7d}-{utilization,reset,status}`、`-overage-status`、`-fallback-percentage`。
- **effort 取值表**（`pkg/claude/effort_catalog.go`，可与第 1 章官方资料对照）：Opus 5.5 / Sonnet 5.5 / Opus 5 / Sonnet 5 / Fable 5.x / Mythos 5 / Opus 4.8 / Opus 4.7 为 `low/medium/high/xhigh/max`；Opus 4.6 / Sonnet 4.6 / Mythos Preview 为 `low/medium/high/max`；Opus 4.5 为 `low/medium/high`。

#### 6.4.2 OpenAI Codex OAuth（ChatGPT 订阅）
- **端点**：`https://chatgpt.com/backend-api/codex/responses`。sub2api 注释（社区观察）：上游会校验 `originator` 与 UA 首段是否配套，不配套一律返回 404（issue #3901，2026-07 实测）。官方 originator 集合包括 `codex_cli_rs`、`codex-tui`、`codex_vscode`、`codex_vscode_copilot`、`codex_exec`、`codex_sdk_ts`、`codex_chatgpt_desktop`、`codex_atlas` 等（`pkg/openai/request.go`）。
- **强制改写**（`service/openai_codex_transform.go` `applyCodexOAuthTransformWithOptions`）：`store=false`、`stream=true`；**删除** `max_output_tokens`、`max_completion_tokens`、`temperature`、`top_p`、`frequency_penalty`、`presence_penalty`、`user`、`metadata`、`prompt_cache_retention`、`safety_identifier`、`stream_options`、`truncation`、`stop_sequences`、`chat_template_kwargs`；input 里的 `role:"system"` 文本搬进 `instructions`（上游不接受 system 角色）；带 reasoning 时补 `include:["reasoning.encrypted_content"]`。
- **instructions 为空时**，按模型注入官方 Codex 基础提示（内嵌自 `openai/codex` 仓库 `codex-rs/models-manager/models.json`）：

| 模型 | 注入提示的开头原文 | 规模（估算） |
|---|---|---|
| gpt-6 / gpt-6-astra / gpt-6.1-sol | `You are Codex, an agent based on GPT-6. You and the user share one workspace…` | 约 3,300 词，约 4.5K–5K token |
| gpt-5.5，以及**所有其他模型的回退**：gpt-6-sol、gpt-6-luna、gpt-5.6-sol/terra/luna、5.3/5.4、裸 gpt-5、未知模型 | `You are Codex, a coding agent based on GPT-5. You and the user share one workspace…`（GPT-5.5 的提示） | 约 3,500 词 |
| gpt-5.2 | `You are GPT-5.2 running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI.` | 约 3,500 词 |
| gpt-5.1 | `You are GPT-5.1 running in the Codex CLI, …` | 约 3,900 词 |
| 名字含 codex 的模型 | `You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer.` | 约 1,100 词，约 1.5K token |

  → **对出题的影响**：经 Codex OAuth 账号且买家没给 system 时，模型会自称 "Codex"，并可能自报 "based on GPT-5 / GPT-6"。**注意 sub2api 与官方不一致**：官方 Codex 给 gpt-6-sol / gpt-6-luna 的提示写 "based on GPT-6"，给 GPT-5.6 系列的写 "an agent based on GPT-5"（第 2 章 3.5 节）；sub2api 对 gpt-6-sol / gpt-6-luna 却回退到 GPT-5.5 的提示（"based on GPT-5"）。所以经 sub2api 时，gpt-6-sol / luna 自称 "based on GPT-5" 是网关造成的，不能据此判为换模型。不能把"自称 Codex"判为异常，也不能用"无系统提示时的 input token 数"作基线（会多出约 1.5K–5K token）。`max_output_tokens` 和 `temperature` 被删除，所以"限制输出长度"类题目在这条通道上无效。
- **模型名归一化**（`codexModelMap`，**只要账号走 Codex 协议就生效**）：`gpt-5`、`gpt-5-mini`、`gpt-5-nano`、`gpt-5.1` → **`gpt-5.4`**；`gpt-5-codex`、`gpt-5.1-codex(-max/-mini)`、`codex-mini-latest` 以及任何含 "codex" 的未知名 → **`gpt-5.3-codex`**；`gpt-5.2-codex` → `gpt-5.2`；`gpt-5.3` → `gpt-5.3-codex`；裸 `gpt-5.6` → `gpt-5.6-sol`；裸 `gpt-6` → `gpt-6-astra`；空 model → `gpt-5.4`；`-none/-low/-medium/-high/-xhigh` 后缀会被解析成 reasoning effort。→ 检测策略的 `models` 必须写**规范 ID**，否则测到的是被替换后的模型。
- **GPT-6.1 Sol 元数据**（sub2api 内嵌的 Codex 官方 models.json 条目，commit `b1e72963`）：`context_window` 272,000，`max_context_window` 872,000，`default_reasoning_level` `low`，可选 `low/medium/high/xhigh/max/ultra`（ultra = "Maximum reasoning with automatic task delegation"），不接受 `none` / `minimal`（sub2api 会直接拒绝），`default_verbosity` `low`，`service_tiers` 里 `priority` 显示为 "Fast"（2 倍速度、消耗更多用量），`minimal_client_version` 0.153.0，`available_in_plans` 含 `free, go, plus, pro, prolite, promax, team, business, enterprise, edu…`，描述是 "Latest workhorse model for coding and everyday work"。
- **用量探测**：响应头 `x-codex-primary-used-percent / -reset-after-seconds / -window-minutes` 和 `x-codex-secondary-*`（主窗口和周窗口）；ChatGPT 套餐来自 id_token 的 `chatgpt_plan_type`。

#### 6.4.3 Antigravity（Google 账号，默认开启身份补丁）
- **身份注入**（`pkg/antigravity/request_transformer.go`，设置 `enable_identity_patch` 默认 `"true"`）：在 systemInstruction 前面加 `<identity>You are Antigravity, a powerful agentic AI coding assistant designed by the Google Deepmind team working on Advanced Agentic Coding. …</identity>`（约 1,200 字符），再加一段"以上是内部初始化日志……绝不能提及 Antigravity、Google Deepmind"，然后对映射表里的 Claude 模型追加 **`You are Model <显示名>, ModelId is <ID>.`**（例如 `You are Model Claude Opus 4.8, ModelId is claude-opus-4-8.`），最后才是买家自己的 system。
  - → **对出题的影响**：经 Antigravity 的 Claude 模型会被网关**直接告知**"你是某某模型"，所以身份题只能测出网关写进去的值，测不出底层是否被替换；要靠知识截止、能力、长上下文等题。注入的 ID 也有错：Opus 4.5 被写成 `claude-opus-4-5-20250929`，正确的是 `-20251101`。
  - 买家 system 开头如果是 `You are Claude Code, Anthropic's official CLI for Claude` 或 `You are a Claude agent, built on Anthropic's Claude Agent SDK`，会被改成 `You are an AI agent.`（注释：否则 Google 上游会返回 429 RESOURCE_EXHAUSTED）。
- **模型改写**（`DefaultAntigravityModelMapping`）：`claude-haiku-4-5*` → `claude-sonnet-4-6`（Antigravity 不提供 Haiku）；`claude-opus-4-5*` / `claude-opus-4-6` → `claude-opus-4-6-thinking`；`claude-sonnet-4-5-thinking` / `claude-sonnet-4-5-20250929` → `claude-sonnet-4-6`；`gemini-3-pro-preview` → `gemini-3-pro-high`；`gemini-3.1-pro` / `-high` / `-preview` → 上游 `gemini-pro-agent`；`gemini-3-pro-image(-preview)` → `gemini-3.1-flash-image`。**只带内置 web_search、没有函数工具的请求，会被强制改用 `gemini-2.5-flash`**。
- **input token 影响**：身份补丁约多 400 token（估算）。

#### 6.4.4 Gemini CLI（Code Assist OAuth）
- 端点 `https://cloudcode-pa.googleapis.com`，UA `GeminiCLI/0.1.5 (Windows; AMD64)`（`pkg/geminicli/constants.go`）。sub2api **不注入**身份提示。

#### 6.4.5 xAI Grok（订阅 OAuth）
- 走 Grok Build CLI 身份：设备登录 `auth.x.ai`，推理经 `cli-chat-proxy.grok.com`，client 名 `xai-grok-cli`，CLI 版本下限 1.0.13（`pkg/xai/cli_identity.go`）。
- JWT 里的订阅档位编号：0 free、1 supergrok、2 x_basic、3 x_premium、4 x_premium_plus、5 supergrok_heavy、6 supergrok_lite、7 supergrok_plus（另有 supergrok_pro 字符串）。sub2api 用 grok-4.5 Responses 窗口的额度判断 SuperGrok 和 Heavy：Heavy 阈值为 8,300 次请求或 5,300 万 token（`pkg/xai/subscription_tier.go`）。

#### 6.4.6 国产 Coding Plan（`service/domain_constants.go`、`service/cn_provider_quota_service.go`）

| 厂商 | 按量端点 | Coding Plan 端点 | Anthropic 兼容端点 | 额度查询（可读出档位） |
|---|---|---|---|---|
| Kimi | `https://api.moonshot.cn/v1` | `https://api.kimi.com/coding/v1` | `https://api.moonshot.cn/anthropic`、`https://api.kimi.com/coding` | `https://api.kimi.com/coding/v1/usages`（5h + 每周） |
| 智谱 | `https://open.bigmodel.cn/api/paas/v4` | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://open.bigmodel.cn/api/anthropic` | `{host}/api/monitor/usage/quota/limit`，`data.level` 为套餐等级；团队版要加 `?type=2` 和组织/项目头 |
| MiniMax | `https://api.minimaxi.com/v1`（与 Coding Plan 同域名，靠 Key 区分） | 同左 | `https://api.minimaxi.com/anthropic` | `https://api.minimaxi.com/v1/api/openplatform/coding_plan/remains`（国际站 `api.minimax.io`），`current_subscribe_title` 为套餐名 |
| DeepSeek | `https://api.deepseek.com` | — | `https://api.deepseek.com/anthropic` | — |
| OpenCode | Zen `https://opencode.ai/zen/v1` | Go `https://opencode.ai/zen/go/v1` | `https://opencode.ai/zen`、`https://opencode.ai/zen/go` | `{base}/v1/usage` |

### 6.5 给检测策略 `models` 配置的建议（基于 v0.2.14 行为）

> 本节是标签快照上的配置建议，引用前须对照[当前 HEAD](sub2api-rewrites.md)。只提示管理员在 token-market 检测策略页选择规范 ID，不在迁移中改默认型号、覆盖管理员配置或改 sub2api 代码。额度/档位字段只能通过其现有接口与只读数据取得，缺失时记“依赖 sub2api，后续处理”。
1. **一律写规范 ID**。Codex OAuth 下 `gpt-5`、`gpt-5-mini`、`gpt-5.1`、`gpt-5-codex` 等旧名会被静默替换成 `gpt-5.4` 或 `gpt-5.3-codex`；Antigravity 下 Haiku 和 Opus 4.5 会被替换。要测"卖家账号是否满血"，就要测网关真正发往上游的那个模型，避免把网关映射误判成降智。
2. **自述只解释通道污染，不计分**：Claude OAuth 常自称 "Claude Code"；Codex OAuth 常自称 "Codex（based on GPT-5 / GPT-6）"；Antigravity 身份由网关注入。原“按通道设身份金标/API Key 可计分”建议未采纳，所有通道均不以自述认定实际型号。
3. **token 计数基线按通道分别标定**：Claude OAuth 约多 400 token（Fable 约多 40），Codex OAuth 空 instructions 时约多 1.5K–5K，Antigravity 约多 400，均为规模估算，不是写死的容差。A03 采用同型号、同入口与通道、同批同请求的总输入计数逐项精确相等，并先做两基线 A/A。
4. **参数类题目受网关限制**：Codex OAuth 删除了 `max_output_tokens`、`temperature`、`top_p`；Claude 新模型本身拒绝采样参数（见第 1 章）。推理强度要用 `reasoning.effort` / `output_config.effort` 控制，再用用量里的 reasoning / thinking token 或答题质量来验证。
5. **档位题优先用额度接口和响应头**（Claude `/api/oauth/usage` 与 `anthropic-ratelimit-unified-*`、Codex `x-codex-*`、智谱 `data.level`、MiniMax `current_subscribe_title`、xAI JWT 档位），比靠提示词让模型自报档位可靠得多。模型本身不知道账号档位。
6. **v0.2.14 缺少最新模型**：`claude-haiku-5-5`（2026-10-07 发布）没有进 Anthropic 默认列表，配置时要手动加白名单或映射；内置 Gemini CLI 列表里也没有 3.6/3.7/3.8 Flash。

### 6.6 sub2api v0.2.14 与官方现状的差异（对照第 1–5 章的核实结果）

> 官方现状为 2026-10-11 访问快照，不是 10-12 重新联网核验。表中“手动加白名单/映射、计费以官方价为准”仅指已有配置/API 能完成的管理操作，不授权修改内置目录、定价代码或网关；现有接口做不到的记“依赖 sub2api，后续处理”。本任务不执行真实请求或账号测试。

| 平台 | sub2api v0.2.14 的写法 | 2026-10-11 官方现状 | 对配置的影响 |
|---|---|---|---|
| Anthropic | 默认列表没有 `claude-haiku-5-5`；Sonnet 5.5 缓存读按 $0.20 计 | Haiku 5.5 已于 2026-10-07 发布；Sonnet 5.5 缓存读是 $0.10（第 1 章） | 手动把 `claude-haiku-5-5` 加进白名单；计费以官方价为准 |
| Antigravity | Claude 只有 Fable 5 / 5.1、Opus 4.6–4.8、Sonnet 4.5 / 4.6，**没有 Claude 5.5** | 官方模型页：Free / Plus / Pro 有 Claude Sonnet 4.6、Opus 4.6（thinking）和 GPT-OSS-120b（2026-11-02 下线）；Pro（非试用）和 Ultra 有 Claude Sonnet 5.5、Opus 5.5（thinking）；Ultra 没有 Claude 4.6（第 3 章） | 以官方列表配置 `models`；Opus 5.5 / Sonnet 5.5 要手动加映射。**官方 FAQ 禁止第三方软件使用 Antigravity 登录，也不支持账号轮换** |
| Gemini CLI（Code Assist OAuth） | 仍支持个人 Google 账号 OAuth | 个人版、AI Pro、AI Ultra 的 "Login with Google" 已于 2026-06-18 停止服务，企业版不受影响（第 3 章） | 个人订阅的 Gemini CLI 账号大概率已不可用，需要实测；检测策略不要再为这类账号排期 |
| Gemini API | 内置列表有 `gemini-3.5-flash`、`gemini-3-pro-preview`，没有 3.6 / 3.8 Flash | 官方自动路由：3.5 Flash → 3.6 Flash，3.7 Flash → 3.8 Flash，3 Pro Preview → 3.1 Pro（第 3 章） | 检测时用 `gemini-3.8-flash`、`gemini-3.6-flash`、`gemini-3.1-pro-preview`；旧 ID 由新模型作答属于官方行为，不算卖家换模型 |
| xAI | 默认文本模型 `grok-4.6` | 最新为 `grok-4.7`；`grok-4-0709`、`grok-3`、`grok-4-fast-*`、`grok-4-1-fast-*` 自 2026-05-15 起重定向到 `grok-4.3`，`grok-code-fast-1` 重定向到 `grok-build-0.1`（第 4 章） | 同上，旧 slug 列入官方重定向白名单 |
| Kimi | 计费表有 `kimi-k2.5`、`kimi-k2-thinking`、`kimi-k2` | Kimi Code 当前 ID 只有 `k3`、`k3-256k`、`kimi-for-coding`（2026-09-11 起指向 K2.8 Preview）、`kimi-for-coding-highspeed`；开放平台的 `kimi-k2.5`、`kimi-k2` 系列、`moonshot-v1` 已下线；国际站 Coding 端点为 `https://api.kimi.ai/coding/v1`；`/coding/v1/usages` 不在官方文档里（第 5 章） | 用官方 ID；高速版 ID 写错会被静默降为标准版，检测要看 TPS |
| 智谱 | 列出 glm-4.5 至 glm-5.3 共十余个模型 | Coding Plan 全档只有 `glm-5.3`、`glm-5.3-flash`：5.2 / 5.1 自动转 5.3，5-turbo / 4.7 自动转 5.3-flash；Claude Code 默认把 Opus / Sonnet / Haiku 都映射到 GLM-5.3-Flash；额度接口只出现在官方插件源码里（第 5 章） | `models` 只配这两个；买家请求 "Opus" 时实际可能是 Flash，要按卖家映射核对 |
| MiniMax | 域名 `api.minimaxi.com`；额度接口 `/v1/api/openplatform/coding_plan/remains`；模型到 M3 为止 | 现行文档域名 `api.minimax.cn` / `api.minimax.io`（旧域名仍可连）；Token Plan 停售，换成 M Plan（Go / Explore / Build），文本模型为 `MiniMax-M3.1-Flash-Preview`（思考不能关）；官方额度接口 `https://www.minimax.cn/v1/token_plan/remains`（第 5 章） | 新账号 `models` 配 `MiniMax-M3.1-Flash-Preview`；highspeed 只在按量列表里出现 |
| OpenCode Go | 内置目录 29 个 ID | 文档列 32 个模型（含 Claude Haiku 5.5、GPT 6 Luna 等闭源模型），`/zen/go/v1/models` 实际返回 45 个；另有 Go Plus（$40/月）；每个模型单独设月度美元额度（第 5 章） | 以文档列表为准；同一 ID 可能被路由到不同推理商 |
| OpenAI Codex OAuth | 旧名归一化到 `gpt-5.4` 和 `gpt-5.3-codex`；空 model 与账号测试默认都是 `gpt-5.4`；instructions 为空时写**顶层** `instructions` | 官方：GPT-5.4 / 5.4-mini 自 2026-08-31 起不能在 ChatGPT 登录的 Codex 里用；GPT-5.3-Codex、GPT-5.2 在 Codex 中已弃用；GPT-5.3-Codex-Spark 于 2026-09-14 退役；GPT-5.5 将于 **2026-10-14** 退出 ChatGPT / Work / Codex；当前 Codex 默认模型是 `gpt-6.1-sol`。官方 Codex 客户端从 0.162.0（2026-10-05）起把基础提示从顶层 `instructions` 挪到 input 里的 developer 消息（第 2 章） | 经 OAuth 请求旧名（含被归一化后的 `gpt-5.4`）**很可能被上游拒绝**，需实测；检测策略和账号测试要改用 `gpt-6.1-sol`、`gpt-6-sol`、`gpt-6-luna`、`gpt-6-astra`、`gpt-5.6-*`。社区报告称后端会对非官方的顶层 instructions 返回 400 "Instructions are not valid"，sub2api 填入的是官方提示，一般不受影响 |

## 第 7 章 出题与检测建议（跨厂商）

> 本章是基于第 0–6 章事实的建议。涉及 token-market 现有代码的条目，引用的是本地仓库文件（只读查看，未修改）。

### 7.1 现有探针需要先改的地方（token-market 代码）

| # | 位置 | 问题 | 依据 | 建议 |
|---|---|---|---|---|
| 1 | `internal/quality/protocol.go` 的 `anthropicProtocol.build` | 推理类题目发送 `thinking: {"type":"enabled","budget_tokens":2048}`。按官方文档，Opus 4.7 / 4.8、Opus 5、Opus 5.5、Sonnet 5、Sonnet 5.5、Haiku 5.5、Fable 5 / 5.1 对这种写法**一律返回 400**；只有 Opus 4.6、Sonnet 4.6（已弃用写法）和 4.5 系列、Haiku 4.5 还接受 | 第 1 章 3.2 节 | 改用 `output_config: {"effort": "<level>"}`，thinking 不传或传 `{"type":"adaptive"}`；Haiku 4.5 等旧模型保留 `budget_tokens` 分支 |
| 2 | `internal/quality/verdict.go` 的 `reasoningAmount` | Claude 的推理量按 thinking 文本长度 ÷ 4 估算。Opus 4.7 及之后的所有新模型默认 `display: "omitted"`，thinking 正文是空字符串，结果恒为 0 | 第 1 章 3.2 节 | 优先读响应的 output thinking token 明细；缺明细时用 `usage.output_tokens`（已包含 thinking）减去可见文本 token 估算并标“估算”。摘要长度不能替代推理量，不要求返回思维链 |
| 3 | `geminiProtocol.build` | 发送 `thinkingBudget: 2048`。官方说 Gemini 3 系列为兼容仍接受，但提示用在 3 Pro 上 "may result in unexpected performance"；同一请求里与 `thinking_level` 同时出现会返回 400 | 第 3 章 2 节 | Gemini 3.x 改用 `thinkingLevel`（3.8 Flash 不接受 `minimal`）；2.5 系列继续用 `thinkingBudget` |
| 4 | `responsesProtocol.build` 的 `max_output_tokens` | 经 Codex OAuth 账号时，sub2api 会删除 `max_output_tokens`、`temperature`、`top_p` 等字段，题目里的 `max_tokens` 不生效 | 第 6 章 6.4.2 节 | Codex OAuth 通道的题目不要依赖输出长度上限；判分时容忍更长的输出 |
| 5 | 检测策略的 `models` | 旧模型名会被网关或厂商替换：Codex OAuth 下的 `gpt-5*` → `gpt-5.4`，`*codex*` → `gpt-5.3-codex`（而这两个在 ChatGPT 登录的 Codex 里已不可用）；Antigravity 下 Haiku → Sonnet 4.6；Gemini 3.5 / 3.7 Flash 由官方路由到 3.6 / 3.8 | 第 6 章 6.5、6.6 节 | `models` 只写各厂商当前的规范 ID（见 0.1 表），并把官方重定向写进白名单 |

### 7.2 身份题
- 原“按通道给自述设标准答案”的研究建议**未采纳**（见 0.3 表）。OAuth 通道里的网关或客户端提示会决定自我介绍：Claude 是 "Claude Code"、Codex 是 "Codex（based on GPT-6 / GPT-5）"、Antigravity 是写入的模型名。自述既不能证明版本，也不能证明厂商；API Key 通道同样不计分。
- 能区分版本的身份信息应当来自**协议层**：Claude 响应体的真实 `model` 字段和 `fallback` 块；OpenAI 官方客户端使用 `OpenAI-Model` 响应头，但现有 sub2api 不透传/记录，缺口记“依赖 sub2api，后续处理”。`response.created.model` 若只是回显请求值不能作为替代。Gemini App 的 "Show thinking steps" 底部标注属于消费端资料，不是 token-market 可采字段；Antigravity 缺原始 modelVersion 时无法判定。
- 国产模型"自称 Claude"不能直接判定为"被换成了 Claude"：Anthropic 在 2026-02-23 点名 Moonshot、MiniMax 等通过蒸馏获取 Claude 的输出，K3 也有自称 Claude 的社区报告。

### 7.3 知识截止题

**截止日期分层**（官方值）：

| 截止 | 模型 |
|---|---|
| 2026-06 | Claude Fable 5.1、Opus 5.5、Sonnet 5.5、Haiku 5.5 |
| 2026-05 | Claude Opus 5（2026-05）；Grok 4.7（2026-05）；GPT-6 Luna（2026-05-18） |
| 2026-04 | GPT-6 Astra、GPT-6.1 Sol（2026-04-30）；GPT-6 Sol（2026-04-20） |
| 2026-03 | Gemini 3.8 / 3.7 / 3.6 Flash、3.5 Flash-Lite（官方称部分领域停在 2025-01） |
| 2026-02 | GPT-5.6 Sol / Terra / Luna（2026-02-16） |
| 2026-01 | Claude Fable 5、Sonnet 5、Opus 4.8、Opus 4.7；MiniMax M3 |
| 2025-12 | GPT-5.5（2025-12-01） |
| 2025-08 | GPT-5.4、GPT-5.3-Codex、GPT-5.2（2025-08-31）；Claude Sonnet 4.6（有冲突） |
| 2025-05 | Claude Opus 4.6、Opus 4.5 |
| 2025-02 / 2025-01 | Claude Haiku 4.5（2025-02）；Gemini 3.1 Pro、3 Flash、3.1 Flash-Lite、2.5 系列，Claude Sonnet 4.5（2025-01） |
| 未公开 | Kimi K3 / K2.8 / K2.7 Code、GLM-5.3 / 5.3-Flash、MiniMax M3.1 Flash Preview，以及 Grok 4.6 及更早的型号 → 待运营通过现有 sub2api 网关建立同批基线，平台不直连官方 |

**可用作探针的官方事件**（日期均来自官方页面，见各章）：Claude Opus 4.6 发布（2026-02-05）、Sonnet 4.6（02-17）、Opus 4.7（04-16）、Opus 4.8（05-28）、Fable 5（06-09）、Sonnet 5（06-30）；GPT-5.3-Codex 发布（2026-02-05）、GPT-4o 等退出 ChatGPT（02-13）、GPT-5.4 发布（03-05）、ChatGPT Pro $100 档上线（04-09）、GPT-5.5 API 发布（04-24）；xAI 并入 SpaceX（2026-02-02）、Gemini 3.1 Pro 发布（02-19）、Gemma 4 发布（04-02）；GLM-5 发布（2026-02-12）、GLM-5.1（04-07）、Kimi K2.7 Code（06-12）、GLM-5.2（06-16）。

**注意**：
- 只问截止日期之前的事实，并在多道题上做统计。单道"你知不知道某事件"的题受记忆不稳定影响大；模型对"本公司发布史"的记忆普遍不稳定，这类题只宜作弱信号（第 1 章 7.2 节的调研建议，不是官方说法）。
- 在 claude.ai 和 Claude Code 的系统提示里，模型名、最新模型列表和截止日期都写明了；Codex 可能注入当前时间；消费端 App 默认会联网搜索。这些渠道的截止题会被污染，必须关闭搜索、只在 API 或无注入的通道出题。

### 7.4 上下文长度题
- 各通道的上限不同：Claude API 上 4.6 及之后的模型为 1M，4.5 系列为 200K；Claude Code 基本都是 1M（Opus 4.6 / Sonnet 4.6 需 `[1m]`）；claude.ai 聊天中 Fable 5、Opus 4.6–4.8、Sonnet 4.6 只有 500K。Codex 默认 272K（实际可用约 95%），最多可配到 872K；API 是 1,050,000（输入上限 922,000）。Gemini API 为 1,048,576，Gemini App 的 Free / Plus / Pro 分别为 32K / 128K / 1M。Grok 4.7 为 500K。Kimi Code 的 Plus 档 `k3` 只有 262,144，Pro 起才有 1M。MiniMax M2.x 为 204,800。
- 超限时的特征报错可以直接用来判分：Claude 返回 "prompt is too long"；Kimi 的 Plus 档请求 1M 返回 401 "Your current plan supports only k3 up to 256K context"，超过 262,144 返回 400 "Your request exceeded model token limit: 262144"；MiniMax M2.x 超过 204,800 必然失败。
- 长上下文会触发高价：Claude Haiku 5.5 提示超过 10 万 token、Gemini 3.1 Pro 和 Grok 超过 200K 时（官方）；sub2api 定价表还对 GPT-6 系列在输入超过 272K 时按输入 ×2、输出 ×1.5 计价（官方定价页未核实）。安排长上下文题时要算预算。

### 7.5 推理强度题
- 各模型默认 effort 不同：Claude Opus 5.5 和 Haiku 5.5 默认 medium，其余为 high；Claude Code 中 5.5 系列默认 medium、Opus 4.7 默认 xhigh；Gemini 3.6 / 3.8 Flash 默认 medium，3.1 Pro 默认 high；Grok 4.7 默认 high；Kimi K3 在 Kimi Code 默认 high、在开放平台默认 max；GLM 和 MiniMax M3.1 默认 max。
- 官方说明 effort 的档位按模型分别校准，同名档位在不同模型上不等价。**必须按"模型 × effort"分别建基线**，再比较推理 token 和答题质量。
- 推理量读取字段：OpenAI Responses 用 `output_tokens_details.reasoning_tokens`；Claude 用 `usage.output_tokens`（见 7.1 第 2 条）；Gemini 用 usage 里的思考 token 计数；国产 OpenAI 兼容接口可以看 `reasoning_content` 的长度。
- 参数校验是比推理量更可靠的指纹，但要考虑网关会删字段（见 6.4.2）。例如：Claude 4.7 及之后的模型收到非默认 `temperature` 返回 400；`between_tools` 只有 Sonnet 5.5 接受；GPT-6 Astra 和 6.1 Sol 不接受 `none`；Gemini 3.8 Flash 不接受 `minimal`；Kimi 的 temperature 固定，`tool_choice: "required"` 只有 K3 支持；MiniMax M3.1 传 `disabled` 返回错误码 2013。

### 7.6 官方自己也会换模型，这些情况不能判卖家作弊

| 厂商 | 机制 | 怎么识别 | 详见 |
|---|---|---|---|
| Anthropic | 服务端拒答回退：Opus 5.5 和 Fable 5.x 的 bio 类请求回退到 Opus 5，cyber 类回退到 Opus 4.8；Sonnet 5.5 的 cyber 类回退到 Sonnet 5。订阅渠道（Claude Code、claude.ai）默认开启，回退后整个会话都停在回退模型上 | 响应顶层 `model` 字段、content 里的 `{"type":"fallback"}` 块、`usage.iterations`；同一对话前缀约 1 小时内保持回退 | 1 章 3.5、4.1 节 |
| OpenAI | 网络安全风险重路由（源码里唯一的原因值是 `highRiskCyberActivity`）；Luna Reserve（额度用完后，部分账号改由 GPT-5.6 Luna 继续服务）；Chat 中 Pro $200 用完 GPT-6 Pro 周额度后切到 GPT-5.6 Thinking | `openai-model` 响应头、`model/rerouted` 事件、`x-codex-*` 额度头 | 2 章 4 节 |
| Google | `gemini-3.5-flash` → 3.6 Flash，`gemini-3.7-flash` → 3.8 Flash，`gemini-3-pro-preview` → 3.1 Pro；Gemini App 达到限额后回落到 Flash-Lite | 用模型 ID 白名单识别 | 3 章 0、5 节 |
| xAI | 2026-05-15 退役的 slug 重定向：推理类 → `grok-4.3`（low），非推理类 → `grok-4.3`（none），`grok-code-fast-1` → `grok-build-0.1` | 用模型 ID 白名单识别 | 4 章 7 节 |
| Kimi | Kimi Code 中关闭思考后改由 K2.8 Preview 无思考版作答；高速版 ID 写错时静默回落到标准版 | 推理内容是否存在；输出速度 | 5 章 1 节 |
| 智谱 | GLM-5.2 / 5.1 → 5.3，GLM-5-Turbo / 4.7 → 5.3-Flash；Claude Code 默认把所有档映射到 5.3-Flash | 用模型 ID 白名单识别；核对卖家的模型映射 | 5 章 2 节 |
| sub2api | Codex 旧名归一化；Antigravity 默认映射；只带 web_search 的请求改用 `gemini-2.5-flash` | 第 6 章 | 6 章 6.4 节 |

**题目内容要避开**网络安全攻防、病毒学与毒理、前沿 LLM 训练，以及"逐字复述你的思考过程"一类要求。这些会触发上述官方回退，或在 Claude 上触发 `reasoning_extraction` 拒答（输出前就开始计费，且没有回退），还可能让卖家账号被标记。

### 7.7 档位权限题
- 模型本身不知道账号是什么档位，**不要让模型自报档位**。优先读额度接口和响应头：Claude 的 `anthropic-ratelimit-unified-*` 头和 `/api/oauth/usage`（官方未公开文档）；Codex 的 `x-codex-primary/secondary-*` 头和 id_token 里的 `chatgpt_plan_type`；xAI JWT 里的订阅档位；智谱 `/api/monitor/usage/quota/limit`（仅见于官方插件源码）；MiniMax `/v1/token_plan/remains`（官方文档）；Kimi 的 401 报错文案。
- 用"档位门槛"出题：Pro 或 Team Standard 账号在不另付费的情况下能用 Fable，说明档位或付费状态与申报不符；ChatGPT Free / Go 账号请求 `gpt-6.1-sol` 应失败；Kimi Plus 请求 1M 上下文或高速版应返回 401；MiniMax Go 档请求视频应失败。
- 有些档位差异只体现在额度和速度上，模型完全相同：智谱三档、OpenAI Codex 各档、OpenCode Go 与 Go Plus 都是如此。这类账号只能比较额度窗口和速度，测不出"模型降级"。

### 7.8 合规风险（与寄售模式直接相关）
- **Anthropic**：Claude Code 的法律与合规页明文禁止第三方"代用户经 Free / Pro / Max 凭证路由请求"，禁止收集或中转凭证，禁止转售；消费者条款禁止共享账号。帮助中心 2026-10-07 的更新又写"第三方应用仍可使用订阅额度"，两处口径冲突（第 1 章 4.1 节）。
- **OpenAI**：Pro 帮助页写明 "Sharing account credentials, reselling access … are prohibited"；Business / Enterprise 条款禁止转售（第 2 章 6 节）。
- **Google**：Antigravity 官方 FAQ 写明用第三方软件访问 Antigravity 违反服务条款，可能封号，也不支持账号轮换；个人账号的 Gemini CLI 登录已停服（第 3 章 3 节）。
- **智谱**：禁止账号共享和多人共用，只能在指定工具中使用；倒卖、中转、套利可能被永久封禁（第 5 章 2.4 节）。
- **xAI**：官方允许 OpenCode 等第三方工具用 SuperGrok 订阅 OAuth 登录；是否允许转售未查到官方说法（第 4 章 6 节）。
- **Kimi、MiniMax、OpenCode Go**：都定位为个人交互式使用，有风控和滥用监控（第 5 章）。
