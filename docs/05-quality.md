# 05 质量检测

检测分为「测智」和「身份与功能核验」两条独立流程。一次触发生成一条运行记录，由 `flows` 标记本次检测范围；可用与性能跟随每次运行计算。测智使用自有题库，身份核验可按厂商选择第三方或自有题库。每个检测项独立配置「用于扣价」，默认全部关闭；开启后在试运行和正式模式下都计入质量系数 Q，影响估价与调度排序。校准只决定自有健康门槛能否确认，等级与保障池最低等级仍只在正式模式下生效。

## 1. 目标

检测区分三类问题：

- 不可用：授权撤销、账号封禁、订阅到期、连接错误或窗口耗尽。
- 身份、功能与能力异常：实际型号不符、权限缺失、上下文或输出被截断、答题能力下降。
- 性能异常：首字延迟高、输出慢、异常限流。

身份一致不能证明答题能力正常。官方账号被路由到较弱模型等现象需要通过测智发现；测智未通过时标疑似、降低并发并复测，不据此暂停账号。检测是抽样，不能保证没有偷换，也不把代理与能力下降的相关性当作因果。

平台固定官方 `base_url`，卖家 CONNECT/SOCKS5 代理转发端到端 TLS，保持证书校验。实现只使用 sub2api 现有接口、网关和只读数据库，不修改 sub2api。协议与证据边界见[汇总调研](research/quality-inspection-2026-10-12.md)及[只读网关对照](research/inspection/sub2api-rewrites.md)。

## 2. 信号来源

| 来源 | 内容 | 成本 | 检测开关 |
|---|---|---|---|
| sub2api 账号状态 | 授权错误、限流、过载、窗口快照 | 无 | 始终读取 |
| sub2api 用量 | 成功请求的延迟、输出、响应型号 | 无 | 始终读取 |
| Edge 观测 | 每跳状态码、故障转移、错误归属 | 无 | 始终读取 |
| 自有主动探测 | 同入口题库请求、可选同期基线对照 | 消耗窗口额度 | 按流程策略 |
| 第三方主动探测 | 检测站通过临时 Edge 令牌核验身份 | 消耗窗口额度，检测站可能按次收费 | 按身份流程策略 |

生产可用率使用 Edge 的可归因请求分母；只记录成功的用量不能充当全部请求。平台、配置、未知来源错误、限流、跳过与买家取消不归为卖家可用失败。响应型号先归一化日期别名，再与同期官方路由比较；自报身份与缺少原始证据的型号提示不能单独证明换模。

输入计数指纹只比较同型号、同入口、同接入配置及相同输入的请求。Claude 总输入包括缓存创建与读取，OpenAI 缓存 token 已包含在输入内。多步题使用各侧真实上轮回答，后续动态输入不要求两侧计数相等。

## 3. 厂商检测策略

策略按厂商与接入方式保存；接入方式级优先，创建时以厂商级为模板。worker 每轮读取最新版本，运行冻结当次策略、流程来源、模型、题目、模式与预算，修改只影响之后入队的运行。

| 配置 | 说明 |
|---|---|
| `enabled` | 检测总开关；上架与管理员检测保留例外 |
| `scheduled_enabled` | 定时总开关，依赖检测总开关 |
| `flows.intelligence` | 测智开关、定时、周期、触发类型、完整检测触发类型、模型及可选期望通过率 |
| `flows.identity` | 身份流程开关、来源 `third_party` / `self`、定时、周期与触发类型 |
| `third_party` | 检测站、模型、协议与身份不符处置；旧 `triggers` 不再控制执行 |
| `daily_budget_usd` | 每上游每日探测预算；上架、管理员、申诉豁免美元预算 |
| `jitter_minutes` | 定时抖动，避免同一时刻集中检测 |
| `default_grade` | 尚无正式检测等级时采用的默认等级 |
| `item_parallelism` | 题目并发上界，默认 4，仍受账号与全局槽限制 |
| `token_budgets` | full / quick 每侧硬预算，空对象沿用质检默认值 |

旧 `suites`、`models`、`interval_minutes`、`full_triggers` 保留返回和存储，前端不再编辑；执行以 `flows` 为准。身份来源为第三方且开启时，必须配置至少一个已启用的检测站。`expected_pass_rate` 可为空，否则须处于 `(0,1]`。

```json
{
  "enabled": true,
  "scheduled_enabled": true,
  "flows": {
    "intelligence": {
      "enabled": true,
      "scheduled_enabled": true,
      "interval_minutes": 360,
      "triggers": ["onboarding", "scheduled", "recovery", "seller_manual", "admin", "adaptive", "appeal"],
      "full_triggers": ["onboarding", "appeal", "admin"],
      "models": [{"model": "gpt-6-astra", "expected_pass_rate": null}]
    },
    "identity": {
      "enabled": true,
      "source": "third_party",
      "scheduled_enabled": false,
      "interval_minutes": 1440,
      "triggers": ["onboarding", "admin", "appeal"],
      "full_triggers": ["onboarding", "appeal", "admin", "scheduled", "recovery", "seller_manual", "adaptive"]
    }
  }
}
```

质检设置 `settings.inspection` 独立于厂商策略。`mode` 默认 `trial`，所有检测项的 `default_pricing=false`。管理员可分别修改 enabled、pricing、calibrated、calibration_note、系数与阈值；`pricing=null` 恢复默认。调度的 `scheduling.enabled` 默认关闭，只控制正式模式的保障池等级门槛；策略为 `quality_adjusted` 时，倍率 ÷ Q 的排序在任何模式下都生效。

## 4. 流程、套件与选题

| 流程 | 套件与中文名 | 检测项 |
|---|---|---|
| 测智 | `inspection_capability` 测智；`inspection_context` 测智·上下文深度 | D07、D07.honesty、D09、D06 |
| 身份与功能核验（自有） | `inspection_authenticity` 身份核验；`inspection_function` 功能核验；`inspection_output` 输出完整性 | A02、A03、A12、A15、A04、A05、A11.*、D08、D12 |
| 身份与功能核验（第三方） | 第三方身份核验 | third_party；本次不跑上述自有身份题 |
| 可用与性能 | `inspection_performance` 性能；请求与生产汇总 | D13、D04、D10、D05，归属 `common` |

`iq`、`reasoning`、`long_context` 是已停用的旧智力套件，不能新建该套件题目。迁移将有适用型号的 iq / reasoning 题原地转为 `inspection_capability / D07`，其余旧题下线；保留题目 ID 与历史关联。默认种子不再写入旧题，运行不再使用旧选择器、iq 请求兜底、影子题拆分或旧 SPRT 结论。

题目仅在 active、平台适用、模型适用且模式匹配时执行，空的平台或模型集合表示不限。每次测全部适用题，不轮换模型或抽题。D06 的 low 副本只在本次包含测智且该流程为完整检测时生成，静态 D07 题不作为 D06 high 锚点。A04 权限副本按挂售白名单展开；白名单为 NULL 时覆盖策略测智模型，单独跑身份流程也保留这一范围。可用率恢复使用单独载入的内置短请求，不依赖本次是否选中了 A04。

### 4.1 按模型测智

本次模型集合为 `flows.intelligence.models ∩ upstreams.model_whitelist`；白名单为 NULL 时取全部已配置测智模型。每次检测交集中的全部模型，执行实例记录 `RequestedModel`，运行的 `models` 保存全集，旧 `model` 保留主模型。交集为空时测智记 `not_applicable`，说明「挂售模型未配置测智」。

D07、D09、D06 先按模型分别判定，使用该模型配置的期望通过率，未配置时沿用检测项阈值；总项取最差模型，并在管理员指标中保留各模型结果。任一模型未通过会触发测智疑似。基线按模型匹配，不能用另一型号结果替代。

### 4.2 参数化题与协议

题库支持斑马、骑士与无赖、寄存器机、数学、代码输出、字符串与日期、自由写作约束、长上下文等生成器，以及工具、多轮、完整输出、诚实弃权等题型。稳定键同步只补缺，不覆盖编辑或复活已下线题。

实例使用 HMAC seed，冻结生成器版本、参数和哈希；同模型被测与基线共用初始实例，多步分别使用各侧真实回答。生产服务不执行模型生成的代码，也不要求模型复述推理过程。判分支持 `answer_tag`、`fields_exact`、`set_exact`、`lines_alias`、`constraints`、`json_schema`、`tool_calls`、`abstain`、`full_output`、`num_list`、`accept_reject` 等，统一保留通过、部分通过与 flags。

请求经与买家相同的网关入口发送；型号、协议、映射与 effort 应一致。Claude、OpenAI、Gemini 按型号能力转换推理参数，用量缺失标未知。OpenAI reasoning 是 output 子集，预算不重复累加；Codex OAuth 删除输出上限，在途请求可能超过预留量，不能承诺单请求费用精确封顶。

## 5. 结论、扣价与等级

### 5.1 检测项判定

有同期基线时按同题、同型号的有效配对计算；拒答、截断、限流或平台错误按证据归属剔除。无基线时使用被测题目结果与模型官方能力，证据来源记 `item_probe`，不伪装为配对结论。

| 检测项 | 主要口径 |
|---|---|
| D07 / D07.honesty / D09 | 配对差异或自身通过率与期望通过率比较；样本不足为 inconclusive |
| D06 | 有基线比较 high 锚定题推理量；完整检测加测 low；无基线使用自身 high/low 比，无法区分厂商默认行为时不确认下降 |
| A04 / A05 | 权限与上下文边界；自有正式门槛必须已校准 |
| A11.* / D08 / D12 | 官方能力支持与功能题观测；不支持为 not_applicable，未登记为 inconclusive |
| A02 / A03 / A12 / A15 | 响应型号、同输入计数、回退拒答、参数规则指纹；按证据判断，不以自报身份确认真伪 |
| D13 | 可归因生产成功率或有效探针；正式门槛采用登记阈值 |
| D04 / D10 / D05 | 异常限流、同模型性能、额度窗口；缺少观测不写成通过 |

模型能力可按平台 + 精确型号、末尾 `*` 前缀或平台默认配置；精确优先于最长前缀，再到默认。未登记不等于不支持。A15 默认关闭，显式开启与扣价不依赖校准，但仍须登记可复核规则；网关本地拒绝不能当作厂商指纹。

### 5.2 总体结论

| 条件 | 结论与处置 |
|---|---|
| 本次目标流程有效且通过，未触发健康门槛 | normal；仅清除实际通过流程的疑似 |
| 测智存在 warning / fail | suspected；记录 intelligence，继续调度、并发减半并退避复测，不产生 D07 暂停门槛 |
| 身份不符且策略选择疑似 | suspected；记录 identity 并复测 |
| 身份不符且策略选择暂停，或自有正式已校准门槛成立 | degraded / unavailable，暂停并安排恢复 |
| 第三方身份不符且策略选择 ignore | 身份按通过处理；没有其他异常时为 normal |
| 试运行至少 2 次有效请求且成功率低于 50% | unavailable |
| 正式 D13 可用门槛成立 | unavailable |
| 官方全局问题 | 标记 vendor_wide，不据此将单个上游标疑似 |
| 上架时任一目标流程没有结论 | 运行失败，上架不通过，可重试 |
| 申诉或恢复的目标流程没有结论 | 运行失败，不当作恢复通过或申诉完成 |

未知、未运行、不适用与失败分别保留状态，不能用另一条流程的正常结论掩盖本次目标流程未完成。旧 SPRT 及 `sprt_degraded` 不再参与结论。

### 5.3 用于扣价

扣价与健康门槛独立。只有 enabled、pricing 开启且有有效 pass / warning / fail 证据的项计入 Q，不要求 `role=factor` 或 calibrated。Factor 未提供时，pass 取 1、warning 取 warning 系数、fail 取 severe 系数；未测、无法判定与不适用不冒充扣价证据。

| 组 | 合成方式 | 默认系数或下限 |
|---|---|---|
| identity | 身份项取最小值 | A02/A03/A12/A15/third_party：warning 0.85、severe 0.70；组下限 0.6 |
| capability | D06、D07、D07.honesty、D09 取最小值 | warning 0.85、severe 0.70 |
| function | 每个功能桶相乘；tools/JSON Schema/多轮合并为一桶 | A04/A05 各自成桶，默认 0.70；D08 独立桶；组下限 0.7 |
| availability | 可用率与异常限流等相乘 | 组下限 0.6 |
| performance | 取最差有效性能系数 | 范围 [0.85,1.05] |

所有组乘积限制在 `[0.5,1.05]`。A01/A06/A08 归 identity，D05 归 availability，默认 warning/severe 为 1；管理员显式改系数并开启扣价后才改变 Q。所有项默认 `pricing=false`。

有效门槛只在正式模式阻断计价；此时所有项 `applied=false`、本次 Q 为空，保留上游原 Q。否则所有模式写入 `COALESCE(Q,1)`，开启扣价且有有效结果的项 `applied=true`。下次通过解除对应扣价与门槛。没有任何启用项开启扣价时估价标试算；开启扣价但证据覆盖不足时也保留 provisional，模式本身不决定试算。

### 5.4 等级与报价

正式模式根据 Q 推出等级：S≥1.02、A≥0.93、B≥0.82、C≥0.65，其余 D。S 另需三十天无门槛、七天可用率≥99.5%；首次上架最高 A。等级采用 ±0.02 滞回，非门槛降级须连续两次有效确认。试运行不改等级与回收报价。

估价为当前厂商 `appraisal_base × Q`，基准调整即时生效，不能伪造检测时间。缺基准为未配置、缺有效检测证据或暂停门槛为暂不估价。倍率 ÷ Q 只改变排序，不修改卖家倍率或实际费用；保障池最低等级要求正式模式且开启质量调度。寄售倍率仍只受厂商区间限制，见[计费](08-billing.md)与[调度](06-scheduling.md)。

## 6. 归因与历史继承

所有模式统一使用 `attributeInspection`，结合同期基线、同题版本、难度、型号、接入方式与档位区分账号、代理、厂商全局和未知。多上游同时异常只提示相关性；基线也下降时不处罚单个上游。缺基线不能宣称已排除外部因素。

历史继承使用单项生效定义哈希，不要求整份设置 JSON 相同：

- 快检中 not_run 项可从同流程、同型号的最近完整检测继承。
- 本次未跑流程可从包含该流程的最近运行继承，时效为 7 天与该流程两倍周期中的较大值。
- 测智历史按当前模型集合筛选，不要求历史模型数组完全相同；当前每个模型均须有有效结果且模型期望通过率未变化。已撤下模型的异常不再扣价，新增或缺证据的模型必须重测。
- 身份历史须主型号和来源相同；A04 还要求权限检测模型范围相同，第三方项还要求检测站型号与处置配置相同。
- 只继承 `deductions_reset_at` 之后的证据，保留原 checked_at，来源标 history，并输出 inherited_at。
- 只比较该检测项的生效定义；其他项设置变化不使其历史失效。本次已有 inconclusive、warning、fail 等结果不被历史替换；恢复失败项须取得本次 pass 或不再适用，继承不能证明修复。

## 7. 入队、排期与恢复

| 触发 | 流程范围 |
|---|---|
| onboarding | 两条已启用流程 |
| admin | 可传 flows，省略为全部已启用流程；保留总开关例外 |
| scheduled | 已启用且开启定时、各自 next 时间到期的流程 |
| seller_manual | 当前异常或有生效扣价的流程 |
| recovery | 失败项所属流程；可用率恢复由测智承载 |
| adaptive | 疑似流程 |
| appeal | 被申诉运行的流程 |
| reference | 仅为本次自有流程建立对照 |

默认入队按流程 triggers 过滤。一次触发只建一条 run，同上游始终串行；queued / running / third_party 都属于 pending。排期优先级为 recovery > adaptive > scheduled，只推进本次实际入队流程的 next 时间。

各流程分别决定 full / quick；任一流程要求完整检测时 run.mode=full，快照仍保存每条流程自己的模式，选题按对应流程模式过滤。定时默认快检。测智疑似复测按 30 分钟、1 小时、2 小时、4 小时、6 小时封顶；失败项含 D09 / D06 时用完整检测。

`suspected_flows` 分别保存测智与身份疑似。只有该流程取得有效通过才移除；测智模型交集为空、不再适用时也清除测智疑似。集合清空且疑似原因来自流程或被动信号时才恢复健康。另一流程的 normal 不会清掉仍未解决的疑似。所有模式写 `last_failed_checks`，暂停恢复要求失败项本次重测通过；沿用连续两次正常恢复的规则，上架检测一次正常即可。

手动复测要求厂商检测已开启且最近存在健康异常、失败/证据不足或已生效扣价；无复测必要返回 `RETEST_NOT_NEEDED`，详情 `manual_retest` 给出原因。每日限次，上架与卖家手动复测由卖家承担额度和费用归属，见 §10。

申诉可对暂停结论或已生效且系数小于 1 的扣价项发起。复测正常且没有生效扣价时自动推翻，清除对应流程疑似，以复测 `started_at`（未开始则 `queued_at`）写入 `deductions_reset_at`；旧证据失效，本轮通过结果仍用于估价与后续继承。管理员直接推翻则以当前时间清旧证据，将上游 Q 重置为 1、清空 appraised_at，调度恢复原倍率排序；当前摘要等待新检测，不再展示旧检查与估价证据。两种推翻均保留历史报告；申诉复测不豁免 token、窗口和截止时间。

运行冻结 full / quick 截止时间，重启后保留已完成实例、题面、累计用量与第三方提交；到期失败，不重新延长预算或截止。平台余额不足时顺延，不归为卖家不可用。429/529 归限流，预期 400 归 expected_reject，平台或题目配置错误保留独立分类。

## 8. 卖家测试报告

每次运行生成一条脱敏报告。顶部展示可用与性能，下面分别展示测智和身份与功能核验；身份区块说明来源，测智按模型列状态。继承结果标注「沿用某时间结果」，未测、不适用与无法判定明确区分。只有旧 summary.suites 的历史报告继续回退显示。

运行与报告 summary 增加：

```json
{
  "flows": ["intelligence", "identity"],
  "identity_source": "third_party",
  "models": ["gpt-6-astra", "gpt-6.1-sol"],
  "intelligence": {
    "status": "pass",
    "inherited_at": null,
    "models": [{"model": "gpt-6-astra", "status": "pass", "target_pass_rate": 0.96, "expected_pass_rate": 0.9, "samples": 57}]
  },
  "identity": {"source": "third_party", "status": "pass", "inherited_at": null}
}
```

流程状态为 pass / warning / fail / inconclusive / not_run，测智额外可为 not_applicable。管理员查看完整模型指标；卖家 `intelligence.models[]` 只保留 model、status，不返回通过率、期望通过率或样本指标。卖家 checks 中 applied=true 的项保留 factor，显示生效系数；未生效项不公开候选系数。

卖家不看到题面、答案、响应片段、基线身份、代理 IP、精确阈值、内部证据码或生成器版本。报告保留安全建议、Q、组系数、门槛、归因与影响；市场只展示摘要。旧 summary.suites 与原 impact 字段保留，缺少新增字段时不伪造数据。

## 9. 第三方身份核验

身份来源为第三方时，运行开始立即为已启用的检测站提交，与自有测智并行。内部题为零的纯第三方运行跳过执行计划，直接等待检测站。内部完成而第三方未结束时进入 third_party 状态；内部不可用时撤销第三方检测及临时令牌。数据库唯一索引 `(run_id,provider)` 防止重入重复提交。

worker 生成 `tm-tp-` 临时 Edge 令牌，只路由到目标上游探测分组，冻结截止时间，完成后撤销。检测站通过公网 https Edge 入口访问目标，频率、超时与 `Retry-After` 受后台配置控制。检测模型优先使用检测站显式配置，否则取测智交集第一个，再取白名单第一个；检测站每次只测一个模型。

| 检测站结果 | 处置 |
|---|---|
| clean_match / clean_match_family_only | 身份通过 |
| clean_match_submodel_mismatch | 按 on_submodel_mismatch 暂停、标疑似或 ignore |
| plain_mismatch / spoof_* | 按 on_mismatch 暂停、标疑似或 ignore |
| ambiguous / insufficient_data / failed / timeout | 身份没有结论，不证明恢复；上架时判失败 |

第三方按策略处置，不要求检测项已校准；ignore 折算为 normal，身份项按 pass 处理，不产生疑似或扣价。正式身份门槛仍优先于扣价。第三方来源下不跑自有身份项，也不从测智附带证据推出 A02/A12；这些项为 not_run，身份结论只看 third_party。

检测站原始回答、响应头和提交者 IP 不保存，摘要只保留核验结论、允许的统计、警告及报告链接。第三方历史准确率不等于本次「未换模」概率。首个提供方为 BazaarLink，接入其他提供方实现 `internal/quality/thirdparty.Provider`。

## 10. 探测成本与预算

定时、恢复、追加、管理员与申诉检测由平台承担，卖家照常获得探测收益。上架（含失败重试）与 seller_manual 由卖家承担，不结算收益，不记平台探测成本；按同上游排队到结束时间窗归属，第三方请求一并计入。重叠检测窗口同样按卖家承担处理，历史用量不回改。

美元日预算只控制是否开始下一次检测；上架、管理员、申诉豁免该预算。所有触发仍受 token、账号窗口、请求时限与运行截止约束。探测用户余额不足时先补足并顺延，不能误判为卖家失败。

| 模式 | 单模型每侧输入 / 输出硬预算 | 独立截止 |
|---|---|---|
| quick | 80K / 160K tokens | 20 分钟 |
| full | 2.0M / 640K tokens | 75 分钟 |

执行预算按本次实际测智模型数等比放大；请求数随适用题、模型、权限副本与恢复样本变化，不承诺固定请求数或真实时长。第三方是独立等待与计费阶段。派发前预留输入与最大输出，用 usage 修正，到预算停止新派发；Codex OAuth 在途输出可能超出预留量。

执行实例上限为 `200 × max(1, 测智模型数)`，执行快照上限为 32 MiB。超限明确判为运行失败，不执行截断后的计划，确保不会将未覆盖的模型记成检测完成。

可用率恢复默认追加 20 个安全短请求，仅发送被测侧，独占账号且每次间隔 3 秒；参数在 inspection.recovery，仍计入本次 token 与截止约束。恢复样本仅继承门槛之后同型号、同单项定义的有效记录，不能用旧生产健康掩盖失败。

窗口快照明确显示余量不足时顺延，未知时正常执行并标记未知；不为检测主动访问真实厂商窗口接口。单请求默认 180 秒，重试等待最多 30 秒，完整与快检截止不能短于单请求时限。默认预算与估算方法见[原数据契约](research/inspection/data-contract.md)，实际成本与时长需运营实测。

## 11. 被动异常检测

被动账号状态、响应型号异常、同行性能偏离与 Edge 故障转移持续监控，不消耗额外探测额度。异常可触发追加检测与疑似并发限制，不能替代目标流程的通过结论。

D13 使用可归因分母；D04 仅统计已确认窗口未耗尽的异常 429；D10 使用同模型同行分位数，同行不足时改用同期配对基线，基线不授予同行奖励。成功用量与主动样本不是随机实验，报告保留证据局限。

## 12. 迁移与校准

`0029_detection_flows.sql` 增加策略 flows、运行 flows / identity_source / models、按流程 next / last 时间、suspected_flows、adaptive_attempts、deductions_reset_at、第三方运行唯一索引及只读的挂售白名单列。测智配置由旧开关、周期、完整触发和 models 复制；身份默认 third_party，开关及触发取原检测站配置，next 时间由旧 next_scheduled_at 回填。旧运行流程保持空数组，不推断历史执行范围。

带型号的旧 iq / reasoning 题原地迁入 D07，其余旧智力题下线，不删除历史题目与结果。旧题下线、流程执行与新结论规则必须一起发布；停用旧题后不能继续靠旧题可用样本或旧 SPRT 决定上架。白名单及模型目录的接入、修改与市场展示见数据模型和相关 API 文档。

默认所有项关闭扣价，自有门槛须独立登记 calibrated / calibration_note；第三方处置不依赖校准。运营仍应做同型号同入口 A/A、受控低档阳性对照与独立留出验证，记录样本、误报、网关版本、成本与时长。样本不足如实记无法判定，不把配置了系数当作完成实测标定。研究依据、题库生成与原始协议约束保留在[调研目录](research/quality-inspection-2026-10-12.md)，当前流程与扣价行为以本文为准。
