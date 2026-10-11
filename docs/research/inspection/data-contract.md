# 质检升级数据契约、预算与校准

日期：2026-10-12。本文是后续阶段的实施契约，尚未实现；依据[汇总报告](../quality-inspection-2026-10-12.md)与已确认方案。字段名是接口约定，说明与错误信息使用简体中文。所有表均为 token-market 的表；sub2api 数据库只读，所有写入仅调用其现有接口。能力缺口记为“依赖 sub2api，后续处理”。

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

以下为拟定 SQL 类型与安全默认值，具体迁移编号在对应阶段确定。除列出的唯一约束与范围校验外，不收紧旧列。旧行不存在新观测时，不回填伪造的观测或 Q。

| 表 | 新增列：类型、默认值 | 含义与约束 |
|---|---|---|
| probe_items | `check_key text NOT NULL DEFAULT ''` | 质检目录键；旧题为空，不参与新质检 |
| probe_items | `request jsonb NOT NULL DEFAULT '{}'` | 下文请求模板；空对象沿用旧 prompt/effort |
| probe_items | `generator jsonb NOT NULL DEFAULT '{}'` | 空对象表示静态题；生成器定义见 §3 |
| probe_items | `modes jsonb NOT NULL DEFAULT '[]'` | 字符串数组；旧题空数组走旧选择器，新题明确 full/quick |
| probe_items | `builtin_key text NULL DEFAULT NULL` | 唯一约束允许多个 NULL；新内置题稳定键，不能以题面哈希代替 |
| probe_items | `weight double precision NOT NULL DEFAULT 1` | 非负，仅用于题族预算分配/展示，不直接加权 SPRT 的似然项 |
| probe_results | `check_key text NOT NULL DEFAULT ''`、`instance_idx integer NOT NULL DEFAULT 0`、`step integer NOT NULL DEFAULT 0` | 序号从 0 开始；配对键为 item_id + instance_idx + step，另由 batch/model/目标角色限定范围 |
| probe_results | `instance_hash text NOT NULL DEFAULT ''`、`generator_version text NOT NULL DEFAULT ''` | 预生成实例 SHA-256 与生成器版本；旧记录为空，不与新实例混合统计 |
| probe_results | `input_tokens bigint NULL DEFAULT NULL`、`cache_read_tokens bigint NULL DEFAULT NULL`、`cache_creation_tokens bigint NULL DEFAULT NULL` | 非负观测；NULL 为不可取得，0 为确实返回零；保留已有 output/reasoning 列与旧含义 |
| probe_results | `stop_reason text NOT NULL DEFAULT ''`、`effort text NOT NULL DEFAULT ''` | stop_reason 原始值；旧空值为未知，归一化结束原因放 flags；effort 为显式请求档位 |
| probe_results | `flags jsonb NOT NULL DEFAULT '[]'` | 去重字符串数组，分类见 §4，不是任意用户文本 |
| probe_results | `step_state jsonb NOT NULL DEFAULT '{}'` | 多步续跑所需的协议无关 text/tool_calls，限 32KiB；无后续依赖不保存，不能用截断 raw_excerpt 恢复 |
| probe_results | `error_body text NOT NULL DEFAULT ''` | 脱敏后最多 4096 字节 UTF-8，保留错误类型/业务码；不保存凭据、IP、请求题面或完整响应头 |
| probe_runs | `mode text NOT NULL DEFAULT 'legacy'` | legacy/full/quick；旧程序创建的记录继续使用旧批次时限 |
| probe_runs | `execution_snapshot jsonb NOT NULL DEFAULT '{}'` | 本批实际请求计划、参数/派生 seed、生成器/策略版本与开始时的模式/预算快照，详见 §3；仅内部读取 |
| probe_runs | `deadline_at timestamptz NULL DEFAULT NULL` | 新 full/quick 开始时写入独立截止时间；旧 NULL 按旧全局时限，不回填当前时间 |
| quality_verdicts | `quality_factor double precision NULL DEFAULT NULL` | 本次可评估的展示 Q；[0.5,1.05]；门槛或无有效观测为 NULL |
| quality_verdicts | `checks jsonb NOT NULL DEFAULT '{}'`、`gates jsonb NOT NULL DEFAULT '[]'` | §5 同构逐项结果与门槛事件，不另建逐项表 |
| upstreams | `quality_factor double precision NOT NULL DEFAULT 1`、`appraised_at timestamptz NULL DEFAULT NULL` | 最近有效 Q 与检测时间；默认 1 是旧排序兼容值，不能据此显示“已通过质检”；门槛时保留上一有效 Q，按健康状态排除 |
| upstream_quality_state | `pending_grade text NULL DEFAULT NULL`、`pending_grade_count integer NOT NULL DEFAULT 0` | 候选等级与连续有效检测次数；非门槛降级两次确认；不确定/反向结果清空连续计数 |
| upstream_quality_state | `last_failed_checks jsonb NOT NULL DEFAULT '[]'` | 上次未通过/门槛项键；用于恢复必测清单，缺测不能清除 |
| vendor_configs | `appraisal_base jsonb NOT NULL DEFAULT '{}'` | tier → 正数倍率与 default 兜底；空配置展示“未配置”，不能回退到 buyback_base |
| detection_policies | `item_parallelism integer NOT NULL DEFAULT 4` | 题目单元并发，范围 1–8；账号和全局上限另行限制 |
| detection_policies | `full_triggers jsonb NOT NULL DEFAULT '["onboarding","appeal","admin"]'` | 沿用现有 trigger 编码，决定完整质检；scheduled 默认快检，恢复必测可升级 full |
| detection_policies | `token_budgets jsonb NOT NULL DEFAULT '{}'` | 按 full/quick 存每侧输入/输出上限；空对象读取 §7 默认值，不影响旧程序 |

已核对 migrations/0006_quality.sql，probe_results 只有主键与 run_id 索引，不存在 run/item 唯一约束。断点续跑可对新实例（instance_hash 非空）增加 run_id/item_id/instance_idx/step 的部分唯一索引，旧结果不受约束；被测与基线各自 run_id，跨 run 的配对由 batch/model/角色限定。断点续跑只补未完成实例，已完成结果保持原样；历史表、旧状态值（包括 third_party）不删除。

## 3. 请求与生成器 JSON

`request` 字段（各项可省略，省略时按型号与协议默认，不把 NULL 原样发上游）：

| 字段 | 类型 | 定义 |
|---|---|---|
| system | string | 平台题目系统指令；不得伪装 Claude Code 绕过网关改写 |
| messages | array | `{role, content}`；role 为 user/assistant/tool，content 为协议无关内容块数组 |
| content 块 | object | type 为 text/image/document/tool_use/tool_result；对应 text、attachment_id、id/name/input、tool_use_id/content 字段 |
| tools | array | `{name, description, input_schema}`，input_schema 为 JSON Schema；仅本地确定性虚拟工具，不访问网络/外部服务 |
| attachments | array | `{id, media_type, encoding, data}`，encoding 为 base64；仅平台审核的图片/PDF，限制大小，不接受远程 URL 拉取 |
| effort | string | 型号允许的显式档位；按型号规则转换，不统一假设 low/high 都可用 |
| extra | object | 指纹题专用的原始参数覆盖，限制在已注册白名单；不能覆盖 URL、凭据、model、代理与认证头 |
| expected | object | `{outcome, http_status, error_codes}`；outcome 为 accept/reject，后两项是允许的整数/字符串数组；仅明确预期拒绝时使用 |
| steps | array | 多步请求增量，按账号顺序执行；允许引用真实上一步文本/工具调用，不允许执行模型生成代码 |

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

`generator` 定义：template_id（注册表模板键）、version（不可变版本）、parameters（允许参数域，不是自由代码）、instances（full/quick 每模式实例数）。静态题为 `{}`。示例只说明结构，不把该题面或参数当作校准后的难度。

```json
{
  "template_id": "register_machine",
  "version": "1",
  "parameters": {"registers": [3, 4], "operations": [30, 60], "difficulty": "候选高档"},
  "instances": {"full": 8, "quick": 4}
}
```

派发前按稳定 item_id、实例序号、步骤顺序生成所有实例；seed 使用 HMAC(平台密钥, batch_id | item_id | instance_idx | generator_version)，分隔与序列化须无歧义，密钥不入库。每批保存版本、哈希与 execution_snapshot，包含实际参数、派生 seed、静态请求计划和期望答案（不保存平台 HMAC 密钥）；后台改题/参数、密钥轮换或版本升级后，禁止重算已开始批次。快照最多 32MiB，超限视为题目配置错误，派发前拒绝，不归因卖家。Go 同 seed 同哈希；不承诺与 Python 的随机数序列相同。

双侧使用相同的初始题面、参数与静态步骤；instance_hash 是预生成请求计划/题面/期望答案的规范序列化哈希，不包含账号凭据。多步后续请求使用各自真实上轮响应，字节可能不同；这些动态步骤不能直接做 A03“输入计数必须相等”。A03 只对双侧有效输入内容一致、接入配置一致的请求比较。模板升级、难度改变、入口/网关配置改变均切分历史，不能混入近 7 天旧通过率。execution_snapshot 只保存内部探针计划，不复制真实买家对话。多步的 step_state 仅保存下一步所需 text/tool_calls 与完整性标记；超限/不完整则不消费截断状态，保留已完成单元，以该多步单元为边界重跑并计入重试预算。旧 raw_excerpt 仅用于管理员展示，不能替代续跑状态。这两列是为落实确认方案的断点续跑而补充的存储细节，不扩大外部功能范围。

### 3.1 续跑状态结构

execution_snapshot 顶层含 schema_version（整数）、policy_version（既有策略版本）、inspection_mode（trial/formal）、mode（full/quick）、token_budget（本次每侧输入/输出上限）、instances（实例计划数组）。每个实例保存 item_id/instance_idx/check_key/generator_version/instance_hash、seed（派生值，字符串）、parameters（实际参数对象）与 steps；step 含 step 序号、request（§3 模板/已生成题面）与 expected（判分期望对象）。型号、接入配置与参与角色绑定使用既有 batch/run 信息；内部对象不返回给卖家或市场。旧 {} 走 legacy，不能当作新计划续跑。

step_state 为 `{complete:boolean, text:string, tool_calls:array}`；工具调用为 `{id:string, name:string, arguments:object}`。仅在下一步确实依赖时保存，完整响应/工具结果超过上限则 complete=false，不把截断片段设为 complete=true。无中间依赖为 {}。写入状态与完成结果在同一事务，恢复前校验实例哈希；不保存推理正文、凭据、真实买家历史或无关响应元数据。

```json
{
  "complete": true,
  "text": "",
  "tool_calls": [{"id": "本地调用编号", "name": "lookup_schedule", "arguments": {"city": "示例城市"}}]
}
```

新增 execution_snapshot/step_state 只用于落实已确认的实例一致与断点续跑：不新增外部产品流程、逐项结果表或厂商访问接口。配置与模板改动只影响下一次 run，既有 run 保持开始时快照；切到 formal 后不能将旧 trial 批次自动当作正式定级结果。

## 4. 协议、错误与判分输出

ProbeRequest 增加 System/Messages/Tools/Attachments/Effort/Extra；ProbeResult 增加 InputTokens/CacheReadTokens/CacheCreationTokens/StopReason/ToolCalls/ErrorBody。ToolCalls 保存协议无关的 id/name/arguments，匹配工具语义而不是仅检查 JSON 可解析。

- Claude 5.x、Opus 4.7/4.8 按型号表使用 adaptive + output_config.effort；Sonnet/Haiku 4.5 等旧型号用 budget_tokens。Haiku 5.5 在 ≤high 时可 disabled，Sonnet 5.5 API 默认 high 而 Code 默认 medium，不能笼统判断所有 5.x 同规则。功能题 tool_choice=auto；异常参数仅在 A15 指纹测试使用。被 sub2api 本地拦截的规则不能作为官方指纹。
- Gemini 3.x 使用 thinkingLevel，2.5 使用 thinkingBudget；OpenAI 使用 reasoning.effort；GLM 经网关 low/medium→high，xhigh→max，只比较实际可区分档位。
- 输入计数：Claude 总输入=input+cache_creation+cache_read；OpenAI cached_tokens 已包含在 input_tokens 内，不重复加；Gemini promptTokenCount/cache 字段按其含义单独记录。不同通道的注入差异先排除，不直接跨通道比较。
- 推理量：Claude 优先读 usage 输出明细，缺失时用总输出减可见文本 token 的估算，标记 reasoning_estimated；不能按 thinking 字符数。OpenAI reasoning 是 output 子集；Gemini thoughts 单列；Grok reasoning 不包含在 output 内，输出预算按对应协议加一次。没有可靠观测时记 NULL，不把空 thinking 当零推理。
- 结束原因归一化为 normal/truncated/refusal/error，保留原值。HTTP 200 也可能 refusal，不作为智力错误；无法识别则标 unknown_stop_reason。
- 分类优先识别 platform：网关并发限制（包括本地429）、探测余额不足、claude_code_version_too_old 均属平台；其余429/529为 rate_limited，按 Retry-After/退避重试，不作为主动探针可用失败或智力错误。明确预期的 400 为 expected_reject，非预期 4xx 再区分题目配置/平台/账号。

11 种新判分器：answer_tag、fields_exact、set_exact、lines_alias、constraints、json_schema、tool_calls、abstain、full_output、num_list、accept_reject。统一输出 `{pass: boolean, partial: number|null, flags: string[]}`，partial 为 [0,1] 的辅助展示，SPRT/McNemar 只用二元 pass。计数配对由引擎计算，不另造 token_count_equal 判分器。answer_tag 必须匹配明确 ANSWER 标记，不能猜最后一个数字。

既有 error_class 列承载本次主错误分类（含 platform/rate_limited/expected_reject），flags 是可并存的补充状态，不能取代主错误原因。flags 允许 stop_normal/truncated/refusal/stop_error/lazy_placeholder/format_error/rate_limited/platform/expected_reject/reasoning_estimated/unknown_stop_reason；其中 stop_normal/truncated/refusal/stop_error 分别保存归一化的 normal/truncated/refusal/error，原 stop_reason 仍原样记录。拒答、截断、限流、平台错误从智力统计剔除，仍保留对应功能/输出/可用项证据。成对样本只要任一侧不合格，两侧一并剔除，SPRT 与 McNemar 使用同一数据清单。无同批基线且无合格历史时，新题不可用默认 0.95 代替基线；记录“无法判定”，不进入降智或扣价。

被测与基线同时发送，题目单元并行，多步账号内有序；每账号信号量与全局上限独立于策略 item_parallelism。只有持 worker 主锁的进程执行检测，不新增 sub2api 并发控制代码。

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

可用范围、样本下限、校准状态由目录规则控制。检查结果表示“证据支持当前判法”，不宣称等效认证或绝对没有换模。无法判定/不适用使用 null，不能写成 pass。

`gates` 为事件数组，每个 `{check_key, code, health, confirmed, occurred_at, reason}`；health 只能 degraded/unavailable。试运行新门槛只展示，旧门槛逻辑继续；正式模式 confirmed=true 才驱动现有暂停/上架不通过。门槛触发时 quality_factor/appraisal.factor 为 null，估价标“暂不估价”。gates 不是长期健康状态的唯一来源，账号既有健康限制始终存在。

新增 summary 字段：inspection_mode（trial/formal）、mode（legacy/full/quick）、checks、quality_factor（number/null）、appraisal、gates。appraisal 为 `{base:number|null, factor:number|null, multiplier:number|null, tier:string, checked_at:string|null, configured:boolean, provisional:boolean}`；multiplier 是寄售卖家侧倍率，不含抽佣。checked_at 来自 appraised_at，改基准不伪造新检测时间；provisional 表示试运行或覆盖不足。缺基准时 configured=false、base/multiplier=null。

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

保留原 impact 的 health_before/health_after/buyer_facing_paused/next_check_at。第三方等待态保存 `{"internal": Assessment}`，Assessment 带同样 checks/quality_factor/gates 等字段；finishRun 对旧记录缺字段按“未测”处理，合并后不丢内部质检字段。管理员逐题明细可含脱敏响应片段，卖家/市场摘要不含题面、答案、基线身份、IP、错误体或精确阈值。

## 6. inspection 设置键

settings.value 存以下对象；这是字段契约，不是本轮迁移。新字段缺省逐个读取默认值，不能因部分覆盖丢失其余默认规则。管理员编辑应校验注册键与范围，未知键保留兼容但不生效。

```json
{
  "mode": "trial",
  "check_overrides": {},
  "grade_thresholds": {"S": 1.02, "A": 0.93, "B": 0.82, "C": 0.65},
  "grade_hysteresis": 0.02,
  "downgrade_confirmations": 2,
  "s_requirements": {"gate_free_days": 30, "availability_days": 7, "min_availability": 0.995},
  "scheduling": {"enabled": false, "minimum_grade": "B", "strategy": "quality_adjusted"},
  "timeouts": {"full_minutes": 45, "quick_minutes": 20},
  "limits": {"account_parallelism": 4, "global_parallelism": 32},
  "token_budgets": {
    "full": {"input_tokens_per_side": 1300000, "output_tokens_per_side": 640000},
    "quick": {"input_tokens_per_side": 80000, "output_tokens_per_side": 160000}
  }
}
```

- mode：trial=试运行（默认）/formal=正式；管理页面用中文标签。
- check_overrides：目录键 → `{enabled:boolean, coefficients:object, thresholds:object}`。仅允许覆盖该目录注册的参数，缺字段读代码默认值；未校准规则不因 enabled=true 自动变成可扣价。
- grade_thresholds：各等级进入分界，严格递增顺序为 C/B/A/S；hysteresis 为 Q 的绝对差（非百分比），downgrade_confirmations 是连续有效检测次数。
- s_requirements：天数与比例；无足够历史不授予 S。试运行历史只作校准证据，不自动补造正式无门槛历史。
- scheduling：enabled 默认 false；minimum_grade 默认 B；strategy 为新策略名称。即使 enabled=true，trial 下仍不得改变规划器输出。
- timeouts：单位分钟，不能短于单请求时限；新 run 将开始时计算的 deadline 固化，修改设置不影响执行中的截止时间。
- limits：正整数，账号并发不能大于全局；全局上限约束的是实际 HTTP 请求，两侧各占一个槽。多步在账号上顺序执行。
- token_budgets：每侧每次 run 硬上限，detection_policies 中非空模式覆盖优先于此；不能用卖家美元日预算豁免绕过这些上限。

质检目录与初始系数见[汇总 §4](../quality-inspection-2026-10-12.md#4-质量系数等级与价格)。方案未规定的“重扣”具体值、显著智力分档边界、参数指纹行为白名单要在运营校准后登记；本轮不把研究中互相冲突的候选值默认为已批准参数。

## 7. 请求、token 与时长预算

默认上架完整质检，scheduled 每日一次快检；策略可按厂商覆盖。以下是**单型号、单被测账号 + 一个同期基线、适用项全部启用时的起步计划**，不是实测额度保证。两侧总量约为两倍；同批共享基线只按实际请求计费。多步每次 HTTP 都算请求，low 重测亦计入。

| 构成 | 完整每侧请求 | 快检每侧请求 |
|---|---:|---:|
| 斑马 / 骑士无赖 / 寄存器 / 数学 / 代码输出 / 字符串日期（显式 high） | 48 | 20 |
| 中英可验证约束（显式 high） | 6 | 2 |
| high 锚定题中的 10 道加测 low | 10 | 0 |
| 长上下文：8K×3、32K×3、128K×4，覆盖五个子类 | 10 | 0 |
| 功能：工具 2 步、多轮 2 步、缓存 2 步、图片与 PDF 各 1、Schema 1、thinking 1 | 10 | 3（工具 2 + Schema 1） |
| 完整输出 | 2 | 0 |
| 配置/指纹：主型号权限 1、上下文边界 1、已校准参数行为 2 | 4 | 0 |
| **合计** | **约 90** | **约 25** |

high 的 54/22 道中选 10–20 道作为 D06 锚；同请求复用 A02/A03、延迟与 usage 证据，不再额外发请求。A08 的知识提示题可以替换少量普通静态题，不扩大硬预算。A15 未校准时不用，Codex OAuth 不出完整输出题，基线不支持功能时记不适用；跳过不自动补成更大题量。每个上架型号 A04 各发一次，多型号、申诉扩样、恢复失败项追加与厂商不同档位均须显式重新生成预算，不暗中计作 90。

上下文边界题使用略超下一低档上限的输入，例如宣称 1M 的账号先检查略超 256K；通过该点不能认证完整 1M，报告写实际测到的长度。需要验证更大上限时增加独立预算，经运营配置后执行；不能在 128K 多针通过后显示“1M 已核验”。不足窗口时不执行这类请求。

| 模式 | 单侧预计输入 | 单侧预计输出（含推理，合并口径） | 单侧硬上限（输入 / 输出） | 时长估算 / run 截止 |
|---|---:|---:|---:|---|
| 快检 | 30–50K | 80–100K | 80K / 160K | 约 3–7 分钟 / 20 分钟 |
| 完整 | 约 1.0–1.1M（含上下文边界） | 约 300–450K | 1.3M / 640K | 约 15–25 分钟 / 45 分钟 |

估算依据：原调研 40 题快检每侧输入约 53K、可见输出 4K、推理 130K；缩至 25 次请求后仅是成本量级参考。完整输入的十个长上下文合计 647,168 tokens，再加约 257K 的边界题及普通题/网关注入预留；输出沿研究 150 题 500K 推理量与 14K 可见量按题量/档位粗估。原研究 150 题双侧约 1.3M 输入/1.0M 输出、串行 106 分钟，不等于当前 90 次方案。实际 tokenizer、响应推理量、缓存写入、网关注入与限流会改变费用；上表不得作为运营 SLA。

时长假设两侧同步、题目并发 4、全局与账号槽可用，普通题几十秒、长题最高单次 180 秒。阶段三要求真实完整质检 p95 < deadline 的 70%（默认 31.5 分钟），目前**待运营校准**；替身测试只验证调度和时限，不证明真实 p95。

开跑前读只读 extra 或现有 Admin API 被动用量，窗口不足顺延并记录原因；快照未知不能假设窗口全满，应排队等待运营配置的安全额度。active 用量查询会访问官方，本次任务不用。提交请求前预留最大输出/输入预算，完成后按厂商 usage 修正；对 Codex 删除 max_output_tokens 的通道预留保守上限，并在到达预算后停止新请求。网关无法提供可靠单请求硬输出限制时，只能停止后续派发，已在途请求可能超出预留量，记“依赖 sub2api，后续处理”，不能宣称精确费用封顶。

完整/快检硬预算覆盖本期新题；试运行驱动旧判定的 22 道旧题按当前适用配置另计，availability/latency 复用这些结果、不额外重复计数；开跑预留新旧题合计，而不是吞进 90/25。第三方 BazaarLink 最多 86 项属于另一个预算阶段，计入平台探测成本，但不纳入自有请求数；没有 token 配额接口时保留现有频率/超时约束与单独成本展示，不宣称它受自有 token 预算精确约束。

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
