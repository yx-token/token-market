# 质检升级数据契约、预算与校准

日期：2026-10-12。本文记录已实现的数据契约与分阶段边界：阶段二协议底座、阶段三参数化题库、阶段四逐项评估、估价和调度；依据[汇总报告](../quality-inspection-2026-10-12.md)与已确认方案。默认试运行，真实规则校准和正式启用由运营完成。字段名是接口约定，说明与错误信息使用简体中文。所有表均为 token-market 的表；sub2api 数据库只读，所有写入仅调用其现有接口。能力缺口记为“依赖 sub2api，后续处理”。

## 1. 发布与回退边界

1. 迁移只增列、只放宽现有 CHECK，不删表、不删列、不缩窄约束；旧程序 INSERT 仍能成功。新列使用下表安全默认值或允许 NULL，不能把“没有观测”当成零。
2. 不下线现有 22 道内置题，不改其状态、题面、suite；新程序正式模式按旧题原题面识别并跳过，试运行仍用旧题判定。回退旧程序后旧题立即可用。
3. 新题使用 `inspection_authenticity`、`inspection_capability`、`inspection_function`、`inspection_context`、`inspection_output` 等新 suite；仅放宽 suite CHECK，不改变旧版只选 `iq/reasoning/long_context` 的选择规则。迁移后的旧版选择器必须只能选到旧题。
4. 内置题同步只补缺失项；不恢复管理员停用的题，不覆盖编辑内容。旧题的 builtin_key 不作强制回填；新题使用稳定、唯一的非空键。
5. 迁移不改 detection_policies 的 models、suites 或现有管理员设置。推荐规范模型 ID 只在后台提示，不自动替换。
6. settings 新建独立 `inspection` 键，不塞进 `platform`，避免旧程序整包保存 platform 时覆盖它。键不存在或字段缺失时读取下文默认值。新旧应用均不依赖删除此键回退。
7. `summary.suites` 的名称、层级、类型保持不变；新字段均可缺省。旧报告、第三方等待记录、旧版页面继续正常渲染。
8. 阶段二只修协议与误判，不改变旧判定口径；阶段三新题只记录；阶段四默认试运行。`inspection.mode=formal` 与独立调度开关同时启用后，才启用新增保障池准入与 Q 排序；正式模式的等级/门槛仍由现有规划器消费 grade/health 输入，关闭新调度不绕过既有健康暂停。

## 2. 新增列

以下 SQL 类型与安全默认值已由 `0022_inspection_probe.sql` 实现，生产错误来源另由 `0023_inspection_attempt_errors.sql` 增列。除列出的唯一约束与范围校验外，不收紧旧列。旧行不存在新观测时，不回填伪造的观测或 Q。

| 表 | 新增列：类型、默认值 | 含义与约束 |
|---|---|---|
| probe_items | `check_key text NOT NULL DEFAULT ''` | 质检目录键；旧题为空，不参与新质检 |
| probe_items | `request jsonb NOT NULL DEFAULT '{}'` | 下文请求模板；空对象沿用旧 prompt/effort |
| probe_items | `generator jsonb NOT NULL DEFAULT '{}'` | 空对象表示静态题；生成器定义见 §3 |
| probe_items | `modes text[] NOT NULL DEFAULT '{}'` | 字符串数组；旧题空数组走旧选择器，新题明确 full/quick |
| probe_items | `builtin_key text NULL DEFAULT NULL` | 唯一约束允许多个 NULL；新内置题稳定键，不能以题面哈希代替 |
| probe_items | `weight double precision NOT NULL DEFAULT 1` | 非负，仅用于题族预算分配/展示，不直接加权 SPRT 的似然项 |
| probe_results | `check_key text NOT NULL DEFAULT ''`、`instance_idx integer NOT NULL DEFAULT 0`、`step integer NOT NULL DEFAULT 0` | 序号从 0 开始；配对键为 item_id + instance_idx + step，另由 batch/model/目标角色限定范围 |
| probe_results | `instance_hash text NOT NULL DEFAULT ''`、`generator_version text NOT NULL DEFAULT ''` | 预生成实例 SHA-256 与生成器版本；旧记录为空，不与新实例混合统计 |
| probe_results | `input_tokens bigint NULL DEFAULT NULL`、`cache_read_tokens bigint NULL DEFAULT NULL`、`cache_creation_tokens bigint NULL DEFAULT NULL` | 非负观测；NULL 为不可取得，0 为确实返回零；保留已有 output/reasoning 列与旧含义 |
| probe_results | `stop_reason text NOT NULL DEFAULT ''`、`effort text NOT NULL DEFAULT ''` | stop_reason 原始值；旧空值为未知，归一化结束原因放 flags；effort 为显式请求档位 |
| probe_results | `flags text[] NOT NULL DEFAULT '{}'` | 去重字符串数组，分类见 §4，不是任意用户文本 |
| probe_results | `step_state jsonb NOT NULL DEFAULT '{}'` | 多步续跑所需的协议无关 text/tool_calls，限 32KiB；无后续依赖不保存，不能用截断 raw_excerpt 恢复 |
| probe_results | `error_body text NOT NULL DEFAULT ''` | 脱敏后最多 4096 字节 UTF-8，保留错误类型/业务码；不保存凭据、IP、请求题面或完整响应头 |
| probe_runs | `mode text NOT NULL DEFAULT 'legacy'` | legacy/full/quick；旧程序创建的记录继续使用旧批次时限 |
| probe_runs | `execution_snapshot jsonb NOT NULL DEFAULT '{}'` | 每批仅在被测 run 保存实际请求计划、参数/派生 seed、生成器/策略版本与模式/预算快照；基线 run 保持 {}，通过 batch_id 读取被测 run 的同一份，详见 §3 |
| probe_runs | `deadline_at timestamptz NULL DEFAULT NULL` | 新 full/quick 开始时写入独立截止时间；旧 NULL 按旧全局时限，不回填当前时间 |
| quality_verdicts | `quality_factor double precision NULL DEFAULT NULL` | 本次可评估的展示 Q；[0.5,1.05]；门槛或无有效观测为 NULL |
| quality_verdicts | `checks jsonb NOT NULL DEFAULT '{}'`、`gates jsonb NOT NULL DEFAULT '[]'` | §5 同构逐项结果与门槛事件，不另建逐项表 |
| upstreams | `quality_factor double precision NOT NULL DEFAULT 1`、`appraised_at timestamptz NULL DEFAULT NULL` | 最近有效正式 Q 与质检时间；试运行 Q 只存本次 quality_verdicts，不改排序 Q；默认 1 是旧排序兼容值，不能据此显示“已通过质检”；门槛保留上一有效 Q，按健康/准入排除；估价使用最近检测结论，不回退旧有效 Q |
| upstream_quality_state | `pending_grade text NULL DEFAULT NULL`、`pending_grade_count integer NOT NULL DEFAULT 0` | 候选等级与连续有效检测次数；非门槛降级两次确认；不确定/反向结果清空连续计数 |
| upstream_quality_state | `last_failed_checks text[] NOT NULL DEFAULT '{}'` | 上次未通过/门槛项键；用于恢复必测清单，缺测不能清除 |
| vendor_configs | `appraisal_base jsonb NOT NULL DEFAULT '{}'` | tier → 正数倍率与 default 兜底；空配置展示“未配置”，不能回退到 buyback_base |
| detection_policies | `item_parallelism integer NOT NULL DEFAULT 4` | 题目单元并发，范围 1–8；账号和全局上限另行限制 |
| detection_policies | `full_triggers text[] NOT NULL DEFAULT '{onboarding,appeal,admin}'` | 沿用现有 trigger 编码，决定完整质检；scheduled 默认快检，恢复必测可升级 full |
| detection_policies | `token_budgets jsonb NOT NULL DEFAULT '{}'` | 按 full/quick 存每侧输入/输出上限；空对象读取 §7 默认值，不影响旧程序 |
| edge_attempts | `error_class text NOT NULL DEFAULT ''` | 仅用于生产质检来源排除；旧记录空值为未知，不推定卖家失败，不改变计费、结束原因或旧 INSERT |

已核对 migrations/0006_quality.sql，probe_results 只有主键与 run_id 索引，不存在 run/item 唯一约束。断点续跑可对新实例（instance_hash 非空）增加 run_id/item_id/instance_idx/step 的部分唯一索引，旧结果不受约束；被测与基线各自 run_id，跨 run 的配对由 batch/model/角色限定。断点续跑只补未完成实例，已完成结果保持原样；历史表、旧状态值（包括 third_party）不删除。

## 3. 请求与生成器 JSON

`request` 字段（各项可省略，省略时按型号与协议默认，不把 NULL 原样发上游）：

| 字段 | 类型 | 定义 |
|---|---|---|
| system | string | 平台题目系统指令；不得伪装 Claude Code 绕过网关改写 |
| messages | array | `{role, content}`；role 为 user/assistant/tool，content 为协议无关内容块数组 |
| content 块 | object | type 为 text/image/document/tool_use/tool_result；对应 text、attachment_id、id/name/input、tool_use_id/content 字段；文本可带 cache_control 对象供支持的入口缓存；工具块另可带 local_id（boolean）与 thought_signature（string），用于 Gemini 动态历史 |
| tools | array | `{name, description, input_schema}`，input_schema 为 JSON Schema；仅本地确定性虚拟工具，不访问网络/外部服务 |
| attachments | array | `{id, media_type, encoding, data}`，encoding 为 base64；仅平台审核的图片/PDF，限制大小，不接受远程 URL 拉取 |
| effort | string | 型号允许的显式档位；按型号规则转换，不统一假设 low/high 都可用 |
| extra | object | 指纹题专用的原始参数覆盖，限制在已注册白名单；不能覆盖 URL、凭据、model、代理与认证头 |
| expected | object | `{outcome, http_status, error_codes}`；outcome 为 accept/reject，后两项是允许的整数/字符串数组；仅明确预期拒绝时使用 |
| steps | array | 多步请求增量，按账号顺序执行；request 为字段增量，expected 为该步骤的判分器覆盖，use_previous 表示追加真实上一步文本/工具调用，tool_results 为本地工具名称到固定结果的映射；不允许执行模型生成代码 |

```json
{
  "system": "只按题目指定格式给出最终答案。",
  "messages": [{"role": "user", "content": [{"type": "text", "text": "计算给定规则的结果，仅输出 ANSWER: 后的整数。"}]}],
  "tools": [],
  "attachments": [],
  "effort": "high",
  "extra": {},
  "expected": {"outcome": "accept", "http_status": [200], "error_codes": []}
}
```

Chat 协议纯文本消息使用字符串，多个文本块顺序拼接；只有含图片时使用内容块数组，以兼容各 Coding Plan 通道。Codex 通道不发送顶层 `instructions` 或 system 角色，将题目系统指令前置到第一条 user 消息，避免覆盖 sub2api 在 instructions 为空时注入的官方 Codex 提示。社区报告非官方 instructions 可能返回 400；这是风险规避，真实兼容性**待运营校准**。Gemini 3 `thinkingLevel` 的 high/HIGH 大小写也**待运营校准**。

新套件包含 inspection_authenticity（真伪）、inspection_capability（能力）、inspection_context（上下文）、inspection_function（功能）、inspection_output（输出）、inspection_performance（性能与可用观测复用）。

`generator` 定义：template_id（注册表模板键）、version（不可变版本）、parameters（允许参数域，不是自由代码）、instances（full/quick 每模式实例数）。静态题为 `{}`。示例只说明结构，不把该题面或参数当作校准后的难度。

```json
{
  "template_id": "register_machine",
  "version": "1",
  "parameters": {"operations": [30, 60]},
  "instances": {"full": 8, "quick": 4}
}
```

派发前按稳定 item_id、实例序号、步骤顺序生成所有实例；seed 使用 HMAC(平台密钥, batch_id | item_id | instance_idx | generator_version)，分隔与序列化须无歧义，密钥不入库。每批仅在被测 run 保存一份版本、哈希与 execution_snapshot，基线 run 不复制草堆，包含实际参数、派生 seed、静态请求计划和期望答案（不保存平台 HMAC 密钥）；后台改题/参数、密钥轮换或版本升级后，禁止重算已开始批次。快照最多 32MiB；旧计划超限为题目配置错误，不归因卖家。试运行先为旧题保留空间，新题扩样超过容量则停止扩样并告警，不能让旧检测新增失败，详见 §10。Go 同 seed 同哈希；不承诺与 Python 的随机数序列相同。

双侧使用相同的初始题面、参数与静态步骤；instance_hash 是预生成请求计划/题面/期望答案的规范序列化哈希，不包含账号凭据。多步后续请求使用各自真实上轮响应，字节可能不同；这些动态步骤不能直接做 A03“输入计数必须相等”。A03 只对双侧有效输入内容一致、接入配置一致的请求比较。模板升级、难度改变、入口/网关配置改变均切分历史，不能混入近 7 天旧通过率。execution_snapshot 只保存内部探针计划，不复制真实买家对话。多步的 step_state 仅保存下一步所需 text/tool_calls 与完整性标记；超限/不完整则不消费截断状态，保留已完成单元，以该多步单元为边界重跑并计入重试预算。旧 raw_excerpt 仅用于管理员展示，不能替代续跑状态。这两列是为落实确认方案的断点续跑而补充的存储细节，不扩大外部功能范围。

### 3.1 续跑状态结构

读取时按 batch_id 找 trigger != reference 的被测 run；被测和基线均使用它的 execution_snapshot。基线 run 只引用既有 batch_id，不复制长上下文草堆；被测 run 有快照则无需重新生成。旧批次无快照按 legacy 初始化，一旦持久化不得覆盖。

execution_snapshot 保存开始时的 policy（完整检测策略）、inspection_settings（完整 §6 设置对象，含模式、校准覆盖、等级及调度设置）、limits、request_timeout_seconds、scoring 与 thresholds；旧快照缺新增设置对象时沿用兼容读取路径，不能用正在修改的设置覆写已有快照。顶层含 schema_version（整数）、policy_version（既有策略版本）、inspection_mode（trial/formal）、mode（legacy/full/quick）、token_budget（本次每侧输入/输出上限）、instances（实例计划数组）及可选 warnings（简体中文字符串数组，计划生成告警）。每个实例保存 item_id/instance_idx/check_key/generator_version/instance_hash、seed（派生值，字符串）、parameters（实际参数对象）与 steps；step 含 step 序号、request（§3 模板/已生成题面）、expected（判分期望对象）及可选 answer（测试替身的标准响应，真实请求不带此字段）。型号、接入配置与参与角色绑定使用既有 batch/run 信息；内部对象不返回给卖家或市场。旧 {} 初始化后只能读取同一份计划，不能借重启重置题面或预算。

step_state 为 `{complete:boolean, text:string, tool_calls:array, input_charged:integer, output_charged:integer}`；工具调用为 `{id:string, name:string, arguments:object, local_id?:boolean, thought_signature?:string}`，另可含 retry_after_ms（毫秒）。text/tool_calls 仅在下一步确实依赖时保存；预算累计字段保存当前步骤包括重试的消耗，恢复时与本 run 其他步骤合计，缺 usage 或中断保留预留值，可靠 usage 到达后按实际计数修正。完整响应/工具结果超过上限则 complete=false，不把截断片段设为 complete=true。无中间依赖时只保存 complete 和预算累计字段，不保存 text/tool_calls；旧记录仍允许 {}。写入状态与完成结果在同一事务，恢复前校验实例哈希；不保存推理正文、凭据、真实买家历史或无关响应元数据。thought_signature 只保留不透明签名，不能视为推理正文或推理量。

```json
{
  "complete": true,
  "text": "",
  "tool_calls": [{"id": "本地调用编号", "name": "lookup_schedule", "arguments": {"city": "示例城市"}}]
}
```

新增 execution_snapshot/step_state 只用于落实已确认的实例一致与断点续跑：不新增外部产品流程、逐项结果表或厂商访问接口。配置与模板改动只影响下一次 run，执行、汇总、第三方等待合并、等级确认均使用开始时设置；切到 formal 后不能将旧 trial 批次自动当作正式定级结果。

## 4. 协议、错误与判分输出

ProbeRequest 增加 System/Messages/Tools/Attachments/Effort/Extra；ProbeResult 增加 InputTokens/CacheReadTokens/CacheCreationTokens/StopReason/ToolCalls/ErrorBody。ToolCalls 保存协议无关的 id/name/arguments，匹配工具语义而不是仅检查 JSON 可解析。

- Claude 5.x、Opus 4.7/4.8 按型号表使用 adaptive + output_config.effort；Sonnet/Haiku 4.5 等旧型号用 budget_tokens。Haiku 5.5 在 ≤high 时可 disabled，Sonnet 5.5 API 默认 high 而 Code 默认 medium，不能笼统判断所有 5.x 同规则。功能题 tool_choice=auto；异常参数仅在 A15 指纹测试使用。被 sub2api 本地拦截的规则不能作为官方指纹。
- Gemini 3.x 使用 thinkingLevel，2.5 使用 thinkingBudget；OpenAI 使用 reasoning.effort；GLM 经网关 low/medium→high，xhigh→max，只比较实际可区分档位。
- Gemini 函数调用可不返回 id：内部生成稳定且避开真实编号的关联键，local_id=true；回发 Gemini 时省略合成 id，真实 id 保留。动态历史保留真实 thoughtSignature；functionResponse 带工具 name，response 为 JSON 对象而非字符串，数值精度不丢失。只读核实本地 sub2api 仓库的 HEAD 与 v0.2.14：Gemini 原生网关保留真实签名，缺签名时补 dummy；Antigravity 保留函数调用内容块，仅签名报错且开启修复配置时改 dummy 重试。真实签名与 dummy 的厂商接受行为**待运营校准**，不能据此宣称工具题已通过真实联调。
- 输入计数：Claude 总输入=input+cache_creation+cache_read；OpenAI cached_tokens 已包含在 input_tokens 内，不重复加；Gemini promptTokenCount/cache 字段按其含义单独记录。不同通道的注入差异先排除，不直接跨通道比较。
- 推理量：Claude 优先读 usage 输出明细，缺失时用总输出减可见文本 token 的估算，标记 reasoning_estimated；不能按 thinking 字符数。OpenAI reasoning 是 output 子集；Gemini thoughts 单列；Grok reasoning 不包含在 output 内，输出预算按对应协议加一次。没有可靠观测时记 NULL，不把空 thinking 当零推理。
- 结束原因归一化为 normal/truncated/refusal/error，保留原值。HTTP 200 也可能 refusal，不作为智力错误；无法识别则标 unknown_stop_reason。
- 分类优先识别 platform：网关并发限制（包括本地429）、探测余额不足、claude_code_version_too_old 均属平台；其余429/529为 rate_limited，按 Retry-After/退避重试，不作为主动探针可用失败或智力错误。明确预期的 400 为 expected_reject，非预期 4xx 再区分题目配置/平台/账号。

11 种新判分器：answer_tag、fields_exact、set_exact、lines_alias、constraints、json_schema、tool_calls、abstain、full_output、num_list、accept_reject。统一输出 `{pass: boolean, partial: number|null, flags: string[]}`，partial 为 [0,1] 的辅助展示，SPRT/McNemar 只用二元 pass。计数配对由引擎计算，不另造 token_count_equal 判分器。answer_tag 必须匹配明确 ANSWER 标记，不能猜最后一个数字。constraints 版本 1 的 exact_text 子规则保留旧批次回放语义；版本 2 改为自由写作，不再要求唯一答案或全文匹配。中文七条、英文八条可验证约束同时判分，参考文本与另一份合法文本均须通过，逐项构造仅违反该项的反例并检出。新增 paragraph_shape 子规则含 n、initials、sep、language（段数、首字/首词、分隔符、语言）；ends_sentence 校验完整固定末句。已有判分规则语义不变。

既有 probe_results.error_class 承载主错误分类（含 platform/rate_limited/expected_reject），flags 是可并存的补充状态，不能取代主错误原因。flags 允许 stop_normal/truncated/refusal/stop_error/lazy_placeholder/format_error/rate_limited/platform/expected_reject/reasoning_estimated/unknown_stop_reason/window_unknown/window_available/abnormal_rate_limited/configuration；其中 stop_normal/truncated/refusal/stop_error 分别保存归一化的 normal/truncated/refusal/error，原 stop_reason 仍原样记录。Anthropic model_context_window_exceeded 归 truncated。window_available 是发题前已知有余量，abnormal_rate_limited 还要求限流后复查仍有余量；仅这类有证据的异常限流参加 D04。拒答、截断、限流、平台错误从智力统计剔除，仍保留对应功能/输出/可用项证据。成对样本只要任一侧不合格，两侧一并剔除，SPRT 与 McNemar 使用同一数据清单；正式新题只采用同期有效配对，不使用旧 0.95 默认基线。

生产 edge_attempts.error_class 记录 platform/rate_limited/configuration/network/timeout/http_5xx/unknown 等来源，成功仍为空；保守分类未知 4xx，不凭状态码推定账号错误。D13 只纳入有归属、可计费且有效完成的生产成功，以及来源明确的网络、超时、上游 HTTP 错误，排除平台、配置、限流、未知失败、跳过和买家取消。正文仅透传时暂存最多 64KiB 前缀供分类，随后丢弃，不入库，不改变用量入账与结算口径。

被测与基线同时发送，题目单元并行，多步账号内有序；每账号信号量的实际容量取 min(策略 item_parallelism、inspection 设置的账号上限、该账号当前下发到 sub2api 的实际并发)，基线同理；item_parallelism 仅为上界，全局上限仍独立。只有持 worker 主锁的进程执行检测，不新增 sub2api 并发控制代码。

## 5. checks、gates 与 summary

`summary.checks` 和 `quality_verdicts.checks` 均为 **check_key → 结果对象**。注册表定义名称、类型、适用条件、默认判法与说明，数据库只保存覆盖值。A11 可使用 A11.thinking/A11.tools/A11.image_pdf/A11.json_schema/A11.multiturn 子键；同子键定义不可在各页自行解释。

| 结果字段 | 类型/单位 | 约定 |
|---|---|---|
| key / name | string | 与对象键相同的稳定目录键 / 简体中文名称 |
| category | string | configuration/authenticity/capability/function/availability/performance/display |
| status | string | pass/warning/fail/inconclusive/not_applicable/not_run；无法判定与未测严格区分 |
| role | string | gate/factor/display；门槛不是 factor=0 |
| factor_group | string/null | capability/function/availability/performance；非扣价项为 null |
| factor | number/null | 本项候选系数；不适用/未测/不确定为 null，聚合时中性 1；group 规则不能改成逐项全相乘 |
| applied | boolean | 是否实际影响本次正式定级；试运行恒 false，健康处置仍由旧逻辑决定 |
| metrics | object | 指标名 → `{value:number|null, unit:string, ci:[number,number]|null, confidence:number|null}`；ratio/fraction、tokens、ms、tokens_per_second、count 等明确单位 |
| samples | object | observed/paired/excluded 整数，单位请求或题目单元须在 evidence 中标明；不可把多步算成多道独立智力题 |
| evidence | object | source 为 paired_probe/production/account_snapshot/third_party/history；window_start/end 为 RFC3339 UTC 或 null，generator_version 为 string/null；sample_unit 为 request/item_unit；code 为机器原因码 |
| reason / advice / impact | string | 简体中文原因、建议、对价格/调度的影响；卖家版本移除阈值、题面与内部定位信息 |
| checked_at | string | RFC3339 UTC |

可用范围、样本下限、校准状态由目录规则控制。检查结果表示“证据支持当前判法”，不宣称等效认证或绝对没有换模。无法判定/不适用的 factor 与缺失指标使用 null，不能写成 pass。当前代码目录包含 D07.honesty 诚实与弃权子键；A08 知识代际暂不放题，A02/A03/A12/A15 均只提示，不启用未经批准的身份重扣系数。

`gates` 为事件数组，每个 `{check_key, code, health, confirmed, occurred_at, reason}`；health 只能 degraded/unavailable。试运行新门槛只展示，旧门槛逻辑继续；正式模式 confirmed=true 才驱动现有暂停/上架不通过。门槛触发时 quality_factor/appraisal.factor 为 null，估价标“暂不估价”。gates 不是长期健康状态的唯一来源，账号既有健康限制始终存在。

新增 summary 字段：inspection_mode（trial/formal）、mode（legacy/full/quick）、checks、quality_factor（number/null）、appraisal、gates、group_factors（四组名称 → number）、proposed_grade/repaired_grade（string）。appraisal 为 `{base:number|null, factor:number|null, multiplier:number|null, tier:string, checked_at:string|null, configured:boolean, provisional:boolean}`；multiplier 是寄售卖家侧倍率，不含抽佣。checked_at 为质检时间，展示读取 appraised_at，改基准不伪造新检测时间；provisional 表示试运行、覆盖不足或证据来自试运行。缺基准时 configured=false、base/multiplier=null。repaired_grade 用 Q=1 和当前历史计算，不能保证 S 或自动恢复，需复测确认。

error_counts 与 reference_error_counts（可省略对象，主错误分类 → 请求数）记录被测与基线的错误来源，供管理员排查。trial 另保留 shadow，结构见 §10；shadow.checks 只是影子样本计数，不是本节正式的 summary.checks。checks/quality_factor/appraisal/gates 已在阶段四计算；示例值仅说明格式，不是实测或已经校准的参数。

完整兼容示例（仅为格式示例，不是真实测量；试运行 grade 仍来自旧逻辑）：

```json
{
  "verdict": "normal",
  "trigger": "scheduled",
  "model": "claude-opus-5-5",
  "checked_at": "2026-10-12T04:00:00Z",
  "baseline": true,
  "grade": "A",
  "tier": "max_20x",
  "suggestions": [],
  "suites": {
    "availability": {"success": 25, "total": 25},
    "latency": {"ttft_p50_ms": 1200, "ttft_p95_ms": 2100, "output_tps": 60},
    "iq": {"relative_score": 0.95, "samples": 20, "confidence": 0.95},
    "reasoning": {"ratio_to_baseline": 0.75}
  },
  "inspection_mode": "trial",
  "mode": "quick",
  "checks": {
    "D06": {
      "key": "D06", "name": "推理强度兑现", "category": "capability",
      "status": "warning", "role": "factor", "factor_group": "capability", "factor": 0.85, "applied": false,
      "metrics": {"reasoning_ratio": {"value": 0.75, "unit": "ratio", "ci": [0.68, 0.79], "confidence": 0.95}},
      "samples": {"observed": 20, "paired": 20, "excluded": 0},
      "evidence": {"source": "paired_probe", "window_start": null, "window_end": null, "generator_version": "1", "sample_unit": "item_unit", "code": "reasoning_ratio_low"},
      "reason": "配对推理量低于同期基线。", "advice": "完成复检与校准后再确认。", "impact": "试运行，不影响价格与调度。",
      "checked_at": "2026-10-12T04:00:00Z"
    }
  },
  "quality_factor": 0.85,
  "appraisal": {"base": 1.5, "factor": 0.85, "multiplier": 1.275, "tier": "max_20x", "checked_at": "2026-10-12T04:00:00Z", "configured": true, "provisional": true},
  "gates": []
}
```

保留原 impact 的 health_before/health_after/buyer_facing_paused/next_check_at。第三方等待态保存 `{"internal": Assessment}`，Assessment 带同样 checks/quality_factor/gates 等字段及本次 InspectionSettings；finishRun 对旧记录缺字段按兼容路径处理，合并保留内部质检字段。管理员逐题明细可含脱敏响应片段，按 item_id + instance_idx + step 配对。卖家/市场使用 PublicChecks：不含题面、答案、基线身份、IP、错误体、精确阈值、置信区间、生成器版本或内部原因码；原因和建议只取目录安全文案，原始证据留在管理员接口。

## 6. inspection 设置键

settings.value 的独立 inspection 键存以下对象，由后台设置接口写入，不在迁移中覆盖运营配置。新字段逐个读取默认值；更新按嵌套对象合并已有值与补丁，未知键保留兼容但不生效，不因部分编辑丢失其余规则。

```json
{
  "mode": "trial",
  "check_overrides": {},
  "grade_thresholds": {"S": 1.02, "A": 0.93, "B": 0.82, "C": 0.65},
  "grade_hysteresis": 0.02,
  "downgrade_confirmations": 2,
  "s_requirements": {"gate_free_days": 30, "availability_days": 7, "min_availability": 0.995},
  "scheduling": {"enabled": false, "minimum_grade": "B", "strategy": "quality_adjusted"},
  "timeouts": {"full_minutes": 75, "quick_minutes": 20},
  "limits": {"account_parallelism": 2, "global_parallelism": 32},
  "token_budgets": {
    "full": {"input_tokens_per_side": 1300000, "output_tokens_per_side": 640000},
    "quick": {"input_tokens_per_side": 80000, "output_tokens_per_side": 160000}
  }
}
```

- mode：trial=试运行（默认）/formal=正式；管理页面用中文标签。
- check_overrides：目录键 → `{enabled?:boolean, calibrated:boolean, calibration_note:string, coefficients?:object, thresholds?:object}`。calibrated 默认 false；标 true 必须提供非空 calibration_note（最多 4096 字节），登记型号、入口、网关版本、样本、误报和记录标识，审核质量由运营负责。仅该目录注册的参数参与规则；未知字段保留但不生效，缺字段读代码默认值。enabled=true 不自动授予校准状态；未校准规则不参与 Q、SPRT 新智力判定或确认门槛。A15 在未校准时强制不生效，即使手动设 enabled=true 也不发题。
- grade_thresholds：各等级进入分界，严格递增顺序为 C/B/A/S；hysteresis 为 Q 的绝对差（非百分比），downgrade_confirmations 是连续有效检测次数。
- s_requirements：天数与比例；无足够历史不授予 S。试运行历史只作校准证据，不自动补造正式无门槛历史。
- scheduling：enabled 默认 false；minimum_grade 默认 B；strategy 为新策略名称。即使 enabled=true，trial 下仍不得改变规划器输出。
- timeouts：单位分钟，不能短于单请求时限；新 run 将开始时计算的 deadline 固化，修改设置不影响执行中的截止时间。
- limits：正整数，account_parallelism 默认 2；实际生效值取 min(策略 item_parallelism、设置的账号上限、该账号当前下发的实际并发)，基线同理，suspected 减半后的值必须生效。账号并发不能大于全局；全局上限约束的是实际 HTTP 请求，两侧各占一个槽。多步在账号上顺序执行。
- token_budgets：每侧每次 run 硬上限，detection_policies 中非空模式覆盖优先于此；不能用卖家美元日预算豁免绕过这些上限。

质检目录与初始候选系数见[汇总 §4](../quality-inspection-2026-10-12.md#4-质量系数等级与价格)。后台“质检项”页分别编辑启用、校准记录、系数、阈值；系统设置页单独编辑完整 inspection JSON 及常用开关。代码候选规则不代表真实厂商验收，方案未规定的“重扣”值不默认启用。A15 不增加自由脚本或独立规则列：管理员在题库登记 check_key=A15、限定 models、request.extra 白名单参数、request.expected 接受/拒绝条件与 accept_reject 判分器，再登记对应校准覆盖；至少两个有效同期规则配对才给提示，网关本地拦截不作为厂商指纹。

## 7. 请求、token 与时长预算

默认上架完整质检，scheduled 每日一次快检；策略可按厂商覆盖。以下是**单型号、单被测账号 + 一个同期基线、适用项全部启用时的起步计划**，不是实测额度保证。两侧总量约为两倍；同批共享基线只按实际请求计费。多步每次 HTTP 都算请求，low 重测亦计入。

| 构成 | 完整每侧请求 | 快检每侧请求 |
|---|---:|---:|
| 斑马 / 骑士无赖 / 寄存器 / 数学 / 代码输出 / 字符串日期（显式 high） | 48 | 20 |
| 中英可验证约束（显式 high） | 6 | 2 |
| high 锚定题中的 10 道加测 low | 10 | 0 |
| 长上下文：8K×3、32K×3、128K×4，覆盖五个子类 | 10 | 0 |
| 功能：工具 2 步、多轮 2 步、缓存 2 步、图片与 PDF 各 1、Schema 1、thinking 1 | 10 | 3（工具 2 + Schema 1） |
| 完整输出 1、中文金额 2、诚实弃权 1 | 4 | 0 |
| 配置：主型号权限 1、上下文边界 1 | 2 | 0 |
| **合计** | **约 90** | **约 25** |

当前 31 条新增内置定义由题库基础 25 条（每侧 full 73 / quick 25 次）与补充探针 6 条（缓存两步，其余一步，共 7 次）组成，完整另加 10 道 low 锚定重测，合计 90 次。high 的 54/22 道中取最多 20 道用于 D06 推理量，完整选择 10 道加测 low；同请求复用 A02/A03、延迟与 usage，不重复派发。A08 暂不放事实题，弃权归 D07.honesty。A15 默认关闭、没有预置指纹题，管理员登记后另计预算。Codex OAuth 不出完整输出题，PDF 只用于 anthropic/openai/gemini/antigravity，其他平台按适用范围跳过；基线不支持功能记不适用，跳过不补题。因此 90 是全部适用单型号计数，不是每个平台固定题量。每个上架型号 A04 各发一次，多型号、申诉扩样与厂商不同档位均显式预留，不暗中计作 90。

正式恢复若上次失败清单含 D13/D04，完整计划额外加入 100 个短连通性新请求；默认全部适用时每侧共 190 次，仍受同一 token 预算、200 个实例、32MiB 快照和 run 截止约束，不自动加额度。配置的样本下限超出可容纳范围时明确失败并退避重排，不能以小样本恢复。恢复只消费门槛之后的新样本；D13 的足够本次有效样本不被暂停前的生产失败窗口覆盖，D04 按门槛后且已核实窗口的新样本汇总。成本和真实可完成性均待运营校准。

上下文边界题使用略超下一低档上限的输入，例如宣称 1M 的账号先检查略超 256K；通过该点不能认证完整 1M，报告写实际测到的长度。需要验证更大上限时增加独立预算，经运营配置后执行；不能在 128K 多针通过后显示“1M 已核验”。不足窗口时不执行这类请求。

| 模式 | 单侧预计输入 | 单侧预计输出（含推理，合并口径） | 单侧硬上限（输入 / 输出） | 时长估算 / run 截止 |
|---|---:|---:|---:|---|
| 快检 | 30–50K | 80–100K | 80K / 160K | 约 6–14 分钟 / 20 分钟 |
| 完整 | 约 1.0–1.1M（含上下文边界） | 约 300–450K | 1.3M / 640K | 约 30–50 分钟 / 75 分钟 |

估算依据：原调研 40 题快检每侧输入约 53K、可见输出 4K、推理 130K；缩至 25 次请求后仅是成本量级参考。完整输入的十个长上下文合计 647,168 tokens，再加约 257K 的边界题及普通题/网关注入预留；输出沿研究 150 题 500K 推理量与 14K 可见量按题量/档位粗估。原研究 150 题双侧约 1.3M 输入/1.0M 输出、串行 106 分钟，不等于当前 90 次方案。实际 tokenizer、响应推理量、缓存写入、网关注入与限流会改变费用；上表不得作为运营 SLA。

时长按双侧同步、每账号实际并发 2、策略上界 4、全局槽可用估算；相比原并发 4 的估算约翻倍，suspected 或实际并发 1 时还会变长。普通题几十秒、长题最高单次 180 秒。完整默认截止调整为 75 分钟；阶段三要求真实完整质检 p95 < deadline 的 70%（默认 52.5 分钟），目前**待运营校准**；替身测试只验证调度和时限，不证明真实 p95。

开跑前读只读 extra 或现有 Admin API 被动用量，只有快照明确显示余量不足时才顺延并记录原因；快照未知时照常执行，在结果和报告中标“窗口余量未知”，仍受 token 硬预算、单请求超时与 run 截止时间限制。上架、申诉和管理员检测不得因快照未知而阻塞。active 用量查询会访问官方，本次任务不用。提交请求前预留最大输出/输入预算，完成后按厂商 usage 修正；对 Codex 删除 max_output_tokens 的通道预留保守上限，并在到达预算后停止新请求。网关无法提供可靠单请求硬输出限制时，只能停止后续派发，已在途请求可能超出预留量，记“依赖 sub2api，后续处理”，不能宣称精确费用封顶。

完整/快检硬预算覆盖新题；试运行驱动旧判定的 22 道旧题按当前适用配置另计，availability/latency 复用这些结果，不额外重复计数。旧题单独按兼容软预算执行，只告警；新题硬预算/时限失败保留证据，不推翻已完成旧判定。恢复追加单独计入同一新题硬预算，不吞进常规 90/25。第三方 BazaarLink 最多 86 项属于另一个预算阶段，计入平台探测成本，但不纳入自有请求数；没有 token 配额接口时保留现有频率/超时约束与单独成本展示，不宣称它受自有 token 预算精确约束。

onboarding/admin/appeal 现有美元日预算豁免保留；窗口、安全 token 预留、单请求和 run 时限仍不可豁免。预算跳过或 worker 中断必须重新安排恢复复测；恢复必须覆盖 last_failed_checks 且连续两次有效正常，快检没测到的项不得视为恢复。超过 deadline 保留已完成证据，检测失败不改变健康，按现有上架/申诉/恢复流程返回可重试结果。

## 8. 上线前校准

所有真实账号步骤均为**待运营校准**，本轮没有真实厂商账号或厂商调用；本期不承诺自动化校准工具。

1. 准备至少两个同型号、同接入方式、同档位的健康基线；记录网关版本、映射、system 注入、effort、窗口状态。先在不同时间与新会话做 A/A，检查双侧输入计数逐个相等、基线通过率/推理量方差、参数接受与拒绝一致性，估计误报与置信区间。缓存题比较各账号自身的两步，不要求跨账号缓存相同。
2. 阳性对照：在基线跑 high/medium/low、同家族小一档模型、输出上限减半。仅测试当前通道真正支持的干预；Codex OAuth 删除输出上限，此阳性通道不适用。用无害题，不请求推理复述、不含敏感类别或注入串。
3. 每题族每难度档先生成 30–50 个候选实例，选出 high 通过约 85–95%、降档单调、差异与成本比好的模板；留出独立实例确认，避免用同批挑题再报告乐观功效。参数分布/题面版本改变即重建历史。
4. 预先登记 D06 的配对对数比、异常值/零 token/估算规则，智力 McNemar 的显著性、多项比较和分档边界，以及 SPRT 的同一配对剔除清单。A/A 无显著差异不等于等效，样本不足保持 inconclusive、不扣价。
5. LiveNerf medium 相对 high 的 token −26%、准确率 −4.2±3.9 个百分点，low −62%/−8.3±4.5，旧 Opus −23%/−3.8±6.3 是论文样本结果，不是平台阈值保证。“10–20 题可发现推理下降”依赖方差假设；原 40 精选题检出约 24 个百分点下降功效 0.82，12 个百分点约需 100 题，25 次快检不能沿用这项功效。
6. 试运行期间观察误报率、拒答/限流来源、题族单调性、D13 分母、p95 时长和成本；双基线一致、阳性干预可检出、无意外 4xx、Claude 推理量可采集、阈值备案后，管理员才切 formal。调度 enabled 仍单独启用；开关关闭的规划器输出必须与旧版一致。
7. 厂商换型号/网关配置或版本改变后再做 A/A 与阳性对照。来源页更新只能更新模型表，不能在迁移中改策略或自动授予“满血”标签。

## 9. 阶段二实现边界与校准依赖

- 迁移为 `0022_inspection_probe.sql`。所有新列按 §2 增加；估价、checks、gates 等列仅预留结构，阶段二不写新 Q、不改变等级公式或调度策略。22 道旧内置题及默认检测模型保留。
- 未启用 `inspection_*` 套件的旧流程继续创建 `legacy` run，使用既有单请求/批次设置；启用新套件后，按 `full_triggers` 选择 full/quick。阶段三接入生成器和新题选择器；阶段二遇到未注册的 generator 会在发请求前报配置错误，不执行自由代码。
- 所有模式第一次开始时固定 deadline_at。full/quick 取消后保留完成结果；legacy 维持原有“清空结果后重跑”的行为，但保留同一份题面快照与截止时间，并在内部 run.summary.resume_budget 中累计清除结果之前的预算，不能借重启重置预算。恢复状态不进入卖家报告。
- 步骤先预留输入和合并输出预算；每侧独立计账，可靠 usage 到达后修正实际值，缺失计数/请求中断保留预留。Claude 输入合并缓存创建与读取；OpenAI 缓存不重复加；Gemini/Grok 独列推理只加一次。平台与限流错误的重试也受预算和截止时间约束。单次重试等待最多 30 秒；Retry-After 超过上限直接记录 rate_limited，不再重试本题，原等待信息以 step_state.retry_after_ms（毫秒）保留。legacy 的预留及实际用量超支只记 warning，不中止、不失败；full/quick 仍执行硬上限。
- 校准前普通题输入按完整请求 JSON 的 UTF-8 字节数加网关注入预留量估算，数值偏保守。阶段三生成长题改用 §10 的词元估算与保护余量，实际 usage 仍用于记账。Gemini 3 的思考档位没有独立数值预算，额外预留 65,536 tokens；这些是执行保护参数，不是厂商 tokenizer 或额度保证，需运营用真实基线校准。长题若无法预留则停止新派发，不能自动扩充预算；阶段三影子失败不推翻已完成的旧检测。
- sub2api 的 Codex OAuth 会删去 max_output_tokens，其他协议也可能缺失完整 usage；token-market 能限制派发、累计返回观测并停止后续请求，无法保证一次在途请求的厂商实际消耗绝不超过预留。要取得网关之外的强制输出限额，记为“依赖 sub2api，后续处理”，不修改 sub2api。
- 窗口只读现有账号快照。Claude 5h 到期时间读 accounts.session_window_end，7d/OI 与 Codex 到期时间读 extra；已过期或无效的快照视为未知。未知照常执行并标记 window_unknown／“窗口余量未知”；明确不足时记录顺延原因，基线同样检查。不访问 active usage 接口。
- 新判分器的工具参数用精确 JSON 数值；JSON Schema 只允许本题内引用，禁止加载外部文件或 URL。工具结果来自注册的本地确定性数据，不执行模型生成代码。
- 现有 SPRT、等级和健康处置口径保持不变；只剔除平台/限流/题目配置失败及拒答、截断等无效智力配对。没有任何有效可用观测时定时/追加检测记 inconclusive；上架、申诉、恢复复测按“检测未完成”处理：上架不通过且说明无有效观测、申诉回待处理、恢复退避重排，均不改变健康状态。汇总单独列出平台/限流/配置错误，供管理员排查；不把平台限制当作账号 unavailable。旧题缺基线的既有口径保留，新题的“无合格基线不判定”在题库接入时落实。
- 真实基线无意外 4xx、Claude 推理量大于零、同配置双侧 input_tokens 相等、完整质检 p95 与预算估算，均为**待运营校准**；离线替身仅验证数据流与控制逻辑。

## 10. 阶段三参数化题库与影子边界

修正后斑马与中英自由写作使用生成器版本 2 和新版 builtin_key；输出平台、弃权分类同样使用新版键。同步只补新版缺项，旧键行与管理员编辑保留，新选择器排除已替代的旧键（含低区分度的每周天数题），旧批次仍按冻结快照回放。斑马版本 1 无参数时仍为 4×3。版本 2 覆盖 4–6 房屋、3–6 属性的全部规格与双求解器唯一解；6×6 的 10 个本地 seed 平均约 77ms、最慢约 100ms，保留单次搜索步数上限，无需离线预生成。内置 5×5 的真实区分度待运营校准。

### 10.1 模板与参数域

注册表有以下 8 个模板；斑马与中英自由写作当前为 version="2"，其余为 "1"，旧版本 1 冻结保留。整数参数接受单个整数或 `[下界,上界]` 闭区间；字符串参数为固定枚举值，未设置的题型由 seed 选择。只接受模板白名单参数，不执行自由代码。generator.instances 的 full/quick 值为 0–32；未写 instances 时生成 1 个实例，存在 instances 但缺当前模式时为 0。实际抽到的参数与答案冻结在快照中，generator_version 保存 `模板/版本`。

| 模板键 | 题型与允许参数 |
|---|---|
| zebra | houses：4–6（版本 2 默认 5）；attributes：3–6（版本 2 默认 5）；每个属性的取值与房子一一对应，8 类线索，求解器确认唯一解 |
| knights_knaves | people：4–8（默认 6）；8 类陈述，确认唯一身份配置 |
| register_machine | operations：20–80（默认 40）；固定 5 寄存器、模 100，7 种指令；不支持 registers/difficulty 参数 |
| aime_math | kind：count/lastdigits/binary/grid；count 的 n 800–2000、modulus 6–13、remainder 0–modulus−1、digit_modulus 3–7、excluded_multiple 5–11；lastdigits 的 base 3–99 且与 10 互素、exponent_base 3–9、exponent_power 3–6；binary 的 length 14–18、ones 5–8、run_limit 3–4；grid 的 width 6–8、height 5–7、blocked_count 4–6 |
| code_output | kind：A/B/D/G；cases：3–6（默认 3）；覆盖列表切片、字符串分支、Python 语义陷阱、双重循环，答案由 Go 计算 |
| string_date | kind：transform/sort/date；transform 的 length 14–30、shift 1–25；date 的 year 2024–2028、start_day 1–24（10 月）、business_days 5–15、elapsed_hours 100–400、holiday_count 1–3；题面给出节假日和纽约夏令时规则，sort 使用自定义字母顺序 |
| constraints | lang：zh/en（默认 zh）；版本 2 自由写作，同时满足 7/8 条有张力的汉字数/词数、关键词频次、段落首字/首词、禁用字符、逐句上限、重复上限、小写和结尾约束，不提供候选或唯一答案 |
| long_context | target_tokens：8192/32768/131072（仅固定值，默认 8192）；variant：multi_needle/multi_hop/aggregate/edges/associative（默认 multi_hop） |

独立验算测试读取渲染后的题面，避免只重跑生成器内部算法：逻辑题用另一种搜索/执行法、数学题用枚举与公式、字符串题用独立变换、日期题用系统时区核对、中英约束按题面独立解析、逐项违例与多份合法文本验证、长题解析记录查表。代码预测在 Go 内计算答案；仅测试在有 python3 时以 `python3 -I` 实际执行本项目生成的安全程序交叉验证，缺少解释器时跳过这部分；生产服务不执行生成代码。各模板均验证同 seed 同题面、同哈希。版本 1 的逻辑题另覆盖 1500 个默认 seed，确保有界生成过程不轻易失败；这不是对真实模型难度的标定。

长草堆使用 lexical_subword_v1 估算：按字母子词、数字组与标点计数，不能把字符数标成 token。快照 parameters 保存 target_tokens、estimated_input_tokens、token_estimator、records、target_record；生成到估算长度达到目标。执行预算预留 `estimated_input_tokens × 1.25 + 1024`，可靠返回后以 probe_results.input_tokens（及对应缓存口径）修正。估算器不是厂商 tokenizer，实际测到的长度必须依据 usage 展示；保护余量和 8K/32K/128K 对应的真实输入均**待运营校准**。

### 10.2 起步题量与同步

阶段三修正后的题库基础为 25 个稳定 builtin_key 定义：6 个通用模板、中英约束 2 个、长上下文 10 个、静态题 7 个；阶段四再补六条，当前共 31 条，旧版键行仍保留。仅 `INSERT ... ON CONFLICT(builtin_key) DO NOTHING`，不覆盖管理员编辑、不复活 retired、不变更已有版本；22 道旧题内容与状态保持原样。后台题库支持 check_key/request/generator/modes/weight，按模板和版本过滤，编辑保留旧 IRT 参数与 builtin_key。

| 题库基础内容 | full 每侧请求 | quick 每侧请求 |
|---|---:|---:|
| 六类能力生成器 | 48 | 20 |
| 中英可验证约束 | 6 | 2 |
| 五型长上下文，8K×3/32K×3/128K×4 | 10 | 0 |
| 工具两步、Schema 一步、多轮两步 | 5 | 3 |
| 完整输出、中文金额两题、缺失信息弃权 | 4 | 0 |
| **全部适用时合计** | **73** | **25** |

这是题库基础请求量，旧题另计；阶段四完整请求量见 §7。Codex 通道跳过完整输出题，基础 full 为 72；管理员可按模式/套件调整。弃权挂 D07.honesty，归入智力配对的诚实子类；A08 暂不放题，等待安全可靠的截止期附近事实与同期基线。完整输出适用平台为除 openai 外全部现有平台。

### 10.3 不影响旧判定的执行与汇总

阶段三交付时新题需显式启用且始终为影子；阶段四已有 active 新题的 trial 新建检测默认 quick，上架/申诉/管理员完整，执行时自动附加 inspection_*，不修改数据库中旧策略 suites。没有新题或全部下线时 trial 仍为 legacy；已排队 legacy 兼容旧流程。原 selectItems 的 SQL 与套件白名单保持不变，在新结构上仍只能读旧题。trial 下新题不参与旧 SPRT、可用率、速度、推理比、错误分母、健康、等级或回收报价，只计算新 checks/Q/估价供观察；formal 生效边界见 §11。

trial 先完成驱动现有判定的旧题，再执行影子题；两侧同步、账号实际并发约束与多步顺序保持。旧题预算仅告警，新题预算、窗口、配置或截止失败保留证据和 warning，不能推翻已完成旧结果；影子到期后允许最多 10 秒数据库收尾。进程主动取消仍按已有规则重排。配置/实例生成无效的新题跳过并告警；快照先保留旧题，再将新题扩样限制到总实例数 200 和 32MiB（增量预留 64KiB），超限停止扩样。旧 legacy 不因这个新增实例上限失败；正式必测或恢复样本不能因截断计划而冒充完成。

`summary.shadow` 为 `{applied:false, checks:{质检键:计数对象}}`，同一字段随 Assessment 进入第三方等待态；以下计数单位均为题目单元，多步合并为一个单元：

| 字段 | 定义 |
|---|---|
| planned | 本次冻结计划中的影子单元数；生成时跳过的配置错误只在 warnings 中，不伪造结果 |
| not_run | 尚未取得 HTTP 状态且没有主错误分类的单元数；不能记作通过或失败 |
| observed | 已派发并取得结果/错误的被测单元数，planned = not_run + observed |
| passed | 被测全部步骤通过的单元数 |
| paired | 双侧均合格、可用于智力配对的单元数；trial 只展示，不接入旧判定 |
| reference_passed | paired 中基线通过的单元数 |
| error_counts | 可省略对象，主错误分类 → 被测单元数 |

逐步 usage、flags、结束原因与实例 hash 仍存在 probe_results，不以聚合计数替代。summary.suites 结构不变；阶段四同时写本次 checks/Q/估价，shadow 仍保留 trial 计数兼容，不作为逐项结果对象。

Engine.RegisterInstance 是可选测试回调，在派发前登记协议请求体、实例 hash、步骤、判分期望与标准答案。e2e 显式交给 token-market 自有替身，替身按完整请求哈希作答并可按账号注入错误；不执行题目程序。生产环境不设置回调，标准答案不进入真实请求。

## 11. 阶段四实现与运营边界

### 11.1 目录、评估与等级

代码目录注册配置、真伪、能力、功能、可用与性能项，数据库仅保存覆盖；Inspect 产出 §5 同构 checks 与 gates，四组系数和 Q 由同一聚合器计算。扣价规则同时要求 enabled、独立 calibrated、有效状态与足够观测；不确定、不适用、未测和未校准项不参与系数。trial 也展示已校准观测的 Q，但 applied 恒 false，旧等级/回收报价/健康处置不变；formal 才将新题有效智力配对接入 SPRT，并消费确认门槛和 Q。没有任何已校准有效扣价观测时 Q=null。

能力组取最差；功能组按 thinking、工具（含 Schema/多轮）、图片/PDF、缓存四个功能面乘法并设 0.7 下限；可用组乘法并设 0.6 下限；性能组取最差、限 [0.85,1.05]；Q 限 [0.5,1.05]。A02/A03/A12/A15 当前只提示，不使用方案未批准的重扣系数。A15 默认关闭，不预置未经厂商校准的参数指纹；本地平台/配置拒绝不构成厂商观测。

正式等级按 S/A/B/C 分界 1.02/0.93/0.82/0.65，采用 Q 绝对值 ±0.02 滞回、非门槛降级连续两次有效 Q 确认；无有效 Q 不确认等级。首次上架最高 A；S 需正式无门槛历史和最近生产可用率，不能用历史最高值、试运行历史或本次门槛之前的记录冒充。本次确认门槛计入历史，门槛暂停不等待两次等级确认。修复预期按 Q=1 与同一历史算，不承诺 S，也不自动恢复。

上架、申诉和恢复的无有效观测或零成功且证据不足的批次按“检测未完成”返回可重试流程，不改变健康；定时/追加仍可 inconclusive。有独立可靠的 confirmed 门槛证据时照常处置，不能以其他观测不足覆盖门槛。A04 只对主型号明确权限拒绝确认上架门槛，次型号失败展示；A05 同期基线接受、被测明确上下文拒绝才确认边界门槛。首尾检索失败且输入计数证据不足时为 inconclusive；只有基线通过且计数支持被测静默截断才确认，不以一道答错证明截断。默认边界点约 262,145 估算 tokens，仍不能认证全部 1M。

### 11.2 历史、恢复与估价

quick 的 not_run 项可继承最近 formal/full、同型号且 execution_snapshot.inspection_settings 完全相等、checked_at 七天内、已实际 applied 的逐项结果；保留原时间、source=history。本次已有观测的 inconclusive/warning/fail 不被旧 pass 覆盖。历史是覆盖说明，不是本次派发，也不能作为恢复证据。

暂停期间 last_failed_checks 并集保留。恢复必须本次覆盖并 pass；D13/D04 加测门槛后的新样本，仍受原预算/时限，未完成就退避重排。第一次有效恢复尚未解除健康暂停时仍“暂不估价”，第二次满足恢复条件才清除失败项。旧生产失败窗口不能盖掉足够的本次 D13 有效通过，也不能用门槛之前的 D04 样本确认恢复。

估价读取当前 vendor_configs.appraisal_base 与最近 quality_verdicts.quality_factor，相乘得到卖家侧参考倍率，不含抽佣；档位缺失可用 default，缺配置为“未配置”，无有效 Q、确认门槛或仍暂停则“暂不估价”。不回退 buyback_base、旧有效 Q 或兼容默认 1。trial 或覆盖不足标 provisional；切正式不会把 trial 历史变为正式结果。正式 Q 定级后仍由原 QuoteFor 按 buyback_base × GradeCoefficients 与毛利上限报价，不修改默认等级系数、寄售区间或 validateConsignment。

### 11.3 调度、页面与校准

保障池新增最低等级（默认 B）与无确认门槛条件，以及 quality_adjusted 的 m_s/Q 每 0.05 一档，只在 formal 且 scheduling.enabled=true 时启用。正式保障池只认已保存 upstreams.grade，不用 default_grade 冒充检测等级；专属寄售组仍用原健康约束。trial 和新调度关闭时，相同输入的规划输出与原策略逐字段一致。

报告按目录分组，展示 Q/组系数、等级、影响、安全建议与修复预期；旧 summary.suites 回退保留。卖家发布与详情显示估价/偏离度，市场显示等级徽章、关键项、上次检测和抽样局限声明；实际应用的扣价项可申诉。后台维护目录校准覆盖、独立 inspection 设置、appraisal_base、策略并发/完整触发/token 预算，逐题明细按三元键配对。

离线测试使用 token-market 自有替身，不修改 sub2api，不访问真实厂商；目录、Q/等级/估价、开关兼容和流程可离线验证。真实基线无意外 4xx、同配置输入计数一致、Claude 推理量、Gemini 3 thinkingLevel 大小写、附件/缓存/边界接受性、A/A 误报、阳性区分度、恢复扩样预算与 p95 全部**待运营校准**。运营确认具体型号/入口/网关版本后逐项登记，再分别启用 formal 与调度；不承诺本期实现自动校准或完整 IRT 标定。
