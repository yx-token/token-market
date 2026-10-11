# GPT、Claude「降智」检测调研

检索与访问日期：2026-10-10。检索覆盖中英文网页、厂商官方文档与事故复盘、学术论文、作者代码仓库和独立监测项目。本文核对了 32 项原始来源；搜索摘要、中文转载、营销检测站和社交媒体投诉仅用于发现线索，没有用于证明厂商主动降智。

本文是资料调研与实验设计，**没有实际调用 GPT/Claude 执行评测，也没有测量本项目任何账号的质量**。文中的实验结果均归属于原作者及其特定实验条件；建议的题量、频率和阈值属于起步方案，需要校准。

## 1. 结论与检测对象

「降智」可以检测，但应定义为：**固定任务分布、可见配置与运行环境后，某个端点或产品入口的任务表现，相对可复核基线出现持续且具有实际意义的下降。**

需要分别回答四个问题：

1. **行为是否改变？** 检测回答分布、格式、用词、工具调用或 token 使用量的变化。
2. **任务能力是否下降？** 检测正确率、代码测试通过率、长上下文利用和任务完成率的下降。
3. **下降发生在哪里？** 区分模型服务、产品客户端、系统提示词、推理配置、上下文管理、工具环境、账号或网络路径。
4. **具体原因和意图是什么？** 更弱模型、推理预算变化、基础设施故障等需要另外取证。黑盒质量下降本身不能证明模型被替换，更不能证明厂商故意针对账号。

这些判断不是同一件事。行为指纹变了，能力可能提高、下降或不变；模型名称不变，基础设施仍可能改变；API 表现正常，Claude Code 或 ChatGPT 的产品体验仍可能退化。

推荐采用 **低成本变化监测 + 客观能力回归评测 + 受控归因实验**。对于当前 GPT 与 Claude，客观能力评测是核心；单 token 探针、身份指纹、推理 token 和延迟都只能补充证据。

总体证据强度：存在真实回归的结论为高，9/10；针对某个账号、IP 或代理的主动降智机制，目前本次检索没有找到足以支持普遍结论的一手证据。

## 2. 官方确认过哪些真实退化

| 资料 | 已确认的事实 | 对检测的直接启示 |
|---|---|---|
| GPT 行为漂移研究，HDSR 2024 [S01] | 比较 2023 年 3 月与 6 月 GPT-3.5、GPT-4：数学、指令遵循、代码格式等变化明显，各能力变化方向不同。正式版本中 GPT-4 素数/合数分类准确率为 84%→51%。 | 应监测能力分项；不能把一个任务的下降说成总体智能全面下降。这是不同已知版本的比较，不能直接证明同一固定快照被偷偷更换。 |
| Claude 2025-09-17 官方复盘 [S02] | 三类基础设施问题造成间歇性退化：上下文路由错误、输出损坏、XLA/TPU 近似 top-k 编译问题。Sonnet 4 路由错误初期影响约 0.8% 请求，最严重小时为 16%；存在会话粘性路由。 | 单次或短期测试容易漏检；应覆盖真实生产路径、多个时段、独立会话和平台。整体平均可能掩盖少量坏路径。 |
| Claude 2026-04-23 官方复盘 [S03] | Claude Code、Agent SDK、Cowork 的三项变化造成质量下降：默认 effort 从 high 改成 medium、错误地持续清理历史 thinking、系统提示词压缩输出而损害编码质量。该事故中 API 与推理层未受影响；官方称问题已于 4 月 20 日解决。 | 必须分别测试原始 API 与用户实际客户端，并覆盖多轮、长时间空闲后恢复、提示词和 effort 配置。 |
| GPT-4o 2025-05-02 官方复盘 [S04] | 4 月 25 日 ChatGPT 更新造成明显迎合；厂商从 4 月 28 日开始回滚。此前离线评测与小规模用户 A/B 指标总体不错，未覆盖到这类问题。 | 数学、代码分数和点赞率不足以代表质量；需要纠错、抗迎合与错误前提测试。 |

Claude 2025 年复盘明确表示不会根据需求、时间或服务负载主动降低模型质量。这里应把它视为厂商声明，而不是独立证明；同样，官方承认的基础设施故障也不能推导为「代理账号必然被降智」。

## 3. 最相关的论文与方法

### 3.1 直接研究漂移、变化监测、身份与统计

| 论文 | 方法或发现 | 对 GPT / Claude 的适用性与边界 |
|---|---|---|
| **How Is ChatGPT’s Behavior Changing Over Time?**，Chen、Zaharia、Zou；HDSR 2024 [S01] | 固定任务，跨时间/版本比较正确率、格式、指令遵循等。 | 直接相关，但研究对象是 2023 年版本。值得借鉴实验设计，不能套用当年的下降幅度。 |
| **Adding Error Bars to Evals**，Evan Miller；2024 [S12] | 配对差异、题目聚类标准误、重复采样、功效分析。 | 最值得用于落地的统计基础；比直接比较两个百分比更可靠。官方解读同时给出五项具体建议。 |
| **Log Probability Tracking of LLM APIs**；ICLR 2026，arXiv v2 [S14] | 请求很短的输入/输出，统计返回 token 的 logprob，检测细微分布变化；引入 TinyChange。 | 要求端点返回 logprobs。论文实验报告成本可低至对比方法的约 1/1000，但不是对任意 GPT/Claude 服务的普遍保证；不能识别变化原因或质量方向。 |
| **Token-Efficient Change Detection in LLM APIs**；ICML 2026，arXiv v4 [S13] | B3IT 寻找低温下输出不稳定的 Border Inputs，再用短输出检测分布变化。论文实验报告相对既有方法约 30 倍成本优势。 | 比 logprob 方法更适合纯黑盒。最新全文报告 131 个候选端点中的覆盖率约 78%（含额外推理查询策略）；隐藏推理、不能控制温度、找不到边界输入的端点存在限制。**未证明可直接覆盖当前所有 GPT/Claude 模型与订阅入口。** |
| **Behavioral Fingerprints for LLM Endpoint Stability and Identity**；2026-03 预印本 [S15] | 固定提示集、重复回答、回答嵌入、energy distance、置换检验与序贯证据。 | 不要求 logprobs；指纹变化后再启动能力评测。作者实现每份指纹需约 800 次短请求，不能误解成「一条请求即可判断」。受控验证主要基于开放模型；真实跨供应商案例包含 Kimi。 |
| **LLMmap: Fingerprinting for Large Language Models**；USENIX Security 2025 [S16] | 主动选择有辨识度的提示，识别模型版本；论文在 42 个已知版本中报告约 8 次交互、超过 95% 准确率。 | 可辅助发现明显型号不符。准确率只限原实验候选集，不能推导为对任意新 GPT/Claude 的身份认证，也不是权重的密码学证明。 |
| **Fingerprinting Inference Systems of Large Language Models**；2026-05 预印本 [S17] | 推理引擎、attention 后端、硬件造成可观察的数值和输出特征。 | 提醒「同一权重、不同部署」也会产生指纹差异；不能看到差异就判定换了小模型。只核到摘要，对闭源当前模型的推广需另测。 |
| **Anosognosia in LLMs**；2026-10-05 预印本 [S26] | 研究量化模型能否自报计算底座受损，发现自报和泛化有明显限制。 | 支持对模型自述保持谨慎，但只核到摘要，实验不能直接推广到当前 GPT/Claude。询问「你降智了吗」不应成为检测依据。 |

B3IT 的一个容易忽略的区别：**边界输入用于放大微小行为变化；能力题用于测量任务是否做得更差。** 两类题不应混用。论文最新版本还包含推理端点策略，不能简单说它「只支持非推理模型」，但其低成本优势、推理可见性和覆盖率都必须对目标接口重新验证。

### 3.2 用来构建能力题库的研究

| 研究 / 基准 | 检测维度 | 使用建议 |
|---|---|---|
| IFEval，2023 [S18] | 可程序验证的指令遵循。 | 用约束满足率、严格格式、指定字段等；中文题应另校准，不直接沿用英文词数规则。 |
| LiveBench，ICLR 2025 [S21] | 数学、代码、推理、语言、指令与数据分析，客观答案，持续更新。 | 固定版本作为回归集；新题用于另外的能力集。题库更新前后不能直接比较原始总分。 |
| LiveCodeBench，2024 [S22] | 编码、修复、执行结果预测，按时间收集竞赛题。 | 用隐藏测试判分，固定题目时间段、语言、执行环境与输出预算。 |
| GSM-Symbolic，ICLR 版本 [S23] | 同一题模板修改数值、加入干扰条件后，表现存在变化。 | 构建有程序化答案的变体组，检查脆弱性；不把该研究解释为所有模型「没有推理能力」。 |
| RULER，COLM 2024 [S19] | 多针检索、多跳追踪、聚合，不只是一根针。 | 分长度和难度测试有效上下文能力；一次大海捞针成功不代表长上下文能力正常。 |
| Lost in the Middle，TACL 接收版本 [S20] | 关键信息的位置影响上下文利用。 | 同内容放在开头、中间、结尾，比较正确率；历史论文不证明所有当前模型仍有同样幅度的问题。 |
| LLMs Get Lost In Multi-Turn Conversation，2025 [S25] | 多轮中早期错误假设会累积，模型可能难以恢复。 | 比较「同信息一次输入」与「分轮输入」，测约束保持、纠错与最终任务成功；这是会话退化，不能直接当成服务随时间降智。 |
| Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena，NeurIPS 2023 [S24] | 裁判存在位置、冗长与自我偏好等偏差。 | 开放题需盲化、交换 A/B 顺序、固定裁判并由人工校准；客观题优先程序判分。 |

这些辅助论文已核对原始摘要与版本信息；上表没有把其中的旧模型实验结果当成当前模型的性能结论。

## 4. 可以参考的现成监测项目

### LiveNerf：最接近订阅入口「降智」检测的公开设计

[LiveNerf 仓库][S27]及[预注册方案][S28]针对 Claude Code 订阅入口：固定 CLI、固定提示词、固定题目、客观判分、显式 effort、保存日志、用另一个模型作对照。题目先筛选，再用**独立的新样本**确认答对率，降低「筛选出偶尔失败题」造成的选择偏差。

截至本次访问，仓库最近进度写于 2026-10-08：已收集 15/30 天，尚未达到最终判定窗口。其规则要求两个连续 10 天窗口的 99% 区间排除零、变化至少 3 个百分点，并检查对照与运行环境；最早正式判定约为 10 月 24 日。**本次不能据此声称当前 Claude 已被证实降智。**

它的已知干预验证尤其有参考价值：

- 低 effort 相对 high：作者报告输出 token 约减少 62%，准确率约下降 8.3 个百分点。
- medium 相对 high：token 约减少 26%，准确率约下降 4.2 个百分点。
- 用同家族另一个模型替换：这份验证的样本不足以在 99% 水平可靠区分。作者明确承认此限制。
- 基线后的单个 10 天窗口，作者报告实际最小可检测差异约为 6.6 个百分点；**3 点的实际阈值不等于能可靠检测 3 点变化**。

不要直接照搬它的归因和排除规则。其对照模型也可能变；只测一个固定时段覆盖有限；拒答与回退被排除后，主分数衡量的是特定条件下的能力。商业服务还需同时报告拒答、回退和最终任务完成率。

### Marginlab：监测实际客户端体验

[Marginlab Claude Code tracker][S29]每天跑约 50 个 SWE-Bench-Pro 实例，报告日、周、月通过率与区间；会跟随最新 CLI 和模型。这适合监测用户实际产品体验，但模型与客户端都可能改变，因此不能单独隔离底层模型原因。

访问页面标注最近更新为 2026-09-29，显示当前新模型仍在收集基线、暂停降智检测；部分可见图表还展示历史变化数字。报告应以其明确的基线状态为准，不把跨模型的历史下降当作当前模型降智结论。

### B3IT、LT：给能力评测增加触发器

作者均提供代码 [S31][S32]。可先小范围验证目标端点兼容性、正常波动和成本，再用于触发完整能力测试。本次只读了论文与 README，没有安装或运行这些项目。

## 5. 推荐的可执行检测流程

### 5.1 同时维护三种基线

| 基线 | 能回答的问题 | 不能单独回答的问题 |
|---|---|---|
| 同端点的历史基线 | 这个入口是否比自身以前差？ | 题库、配置或客户端是否变了？ |
| 同时段的参考端点/账号 | 当前被测入口是否明显低于参照？ | 厂商全局下降可能两边一起发生，差异不变。 |
| 固定版本的工具和运行环境 | 客户端升级是否引入退化？ | 固定版本无法阻止远端基础设施变化。 |

不要用一个所谓「干净官方账号」代表真值。参考端点也可能波动或路由到异常服务器。至少保留历史基线，条件允许时用多个独立参照。

检测入口应包含：厂商 API、实际订阅客户端或网关、被测账号。每个入口先跑同一套受控单轮题；产品入口另外跑真实多轮和工具任务。

### 5.2 固定并记录可见条件

每个样本记录：run_id、UTC 时间、端点与接入平台、声明/返回的模型 ID、账号匿名 ID、会话 ID、请求 ID、客户端与 SDK 版本、提示词/题库/判分器哈希、完整请求参数、effort、thinking 设置、输入长度、输出预算、工具定义、工具返回、重试/回退、停止原因、输入/输出/token 用量、延迟与得分。

模型、温度、seed、thinking、budget 的支持范围随版本不同；只发送目标模型允许的参数。固定 temperature=0 仍不保证确定性 [S02][S08][S30]。不支持温度或 seed 时，通过固定其他条件和重复采样处理。

错误、拒答、回退与输出截断要单独分类，不能都算成数学推理错误。对商业服务同时计算最终任务成功率，防止「大量失败被排除，剩下的正确率却很好」。

### 5.3 第一版题库

以下题量是工程起点，**不是已经证明达到某种检出率的配置**。

| 套件 | 起步规模 | 客观指标 |
|---|---:|---|
| 多步数学、逻辑、表格计算 | 60–100 题 | 精确答案、数值容差；模板变体与干扰项稳定性。 |
| 编码与修复 | 40–60 题 | 隐藏测试通过率、编译/语法错误率；固定沙箱与依赖。 |
| 指令与结构输出 | 40–60 题 | 指令满足率、JSON Schema、缺字段、额外文字。 |
| 长上下文 | 20–40 个配置 | 多针、多跳、聚合；在上下文上限内测试多长度与位置。 |
| 工具任务与错误恢复 | 20–40 个场景 | 工具选择、参数语义、执行结果、最终状态；不能只看 schema。 |
| 多轮约束保持与纠错 | 20–40 个会话 | 前述约束保持、错误修正、空闲后恢复、任务完成。 |
| 无害问题的拒答、错误前提与抗迎合 | 20–40 题 | 适当回答/拒绝率、是否纠正错误前提、事实一致性。 |

先校准 200–400 道独立题，能力主集每题重复 3–5 次，再按实际成本和方差调整。保留分项结果，避免「简单题变好、关键编码任务变差」被平均分抵消。

选择高区分度题时，用独立样本确认；最终基线另采，不能重用筛题数据。保留一部分普通真实任务，避免整个检测器只代表人为精选难题。IRT 可以帮助标定，但必须先有可信的校准数据；不应一开始就把未经验证的「满血/降智」标签当作真值。

题库分为：固定回归集、轮换的新题集、仅用于人工复核的真实案例。固定集用于时间比较，新题集防止污染；更新版本就建立新基线。

### 5.4 先验证检测器，再监测生产

**A/A 阴性对照**：同模型、同入口、同参数分成两组，检查误报率和区间覆盖，而不只是做一次「没显著」的检查。

**已知干预的阳性对照**：分别降低 effort、减少可用上下文、压缩输出预算、换成已知不同型号。观察哪些套件能发现哪些变化。限制预算导致截断是服务配置退化，不等于内在能力下降，应单独标注。

**隔离实验**：固定模型服务而更换客户端/提示词；固定客户端而更换模型；对照上下文和工具环境。用这些结果建立症状特征，不能凭一条输出长度曲线猜测机制。

阳性对照验证失败时，「没检测到变化」只能说明仪器不敏感，不能证明正常。报告最低可检测差异、误报、漏报、检测延迟和测试成本。

## 6. 如何做统计判定

### 6.1 首选题目级配对差异

对每道题分别计算当前与基线的平均分：

```text
d_i = 当前窗口第 i 题的平均分 − 基线窗口第 i 题的平均分
Δ = 所有题 d_i 的平均值
```

报告 Δ 的百分点数和置信区间。二元单次配对结果可使用 McNemar 检验；重复采样、混合评分及相关题组可使用题目/模板聚类的配对 bootstrap 或合适的配对统计模型。对于粘性路由，考虑会话、运行批次或时间块相关性，不能把同会话的多次回答当作大量独立证据。[S12]

如果参考账号的得分也由有限样本估计，其误差必须进入分析。直接计算「被测分/参考分」再当作无误差观测不够稳健，尤其在参考分很低时。

例子：真实正确率约 80% 时，200 个独立二元样本的单组 95% 正态近似区间半宽约 5.5 个百分点；两组各 200 样本的差异区间可能更宽。相同题目配对可以降低方差，但不会自动消除不确定性。**200 次调用不能普遍保证发现 3–5 点退化。**

### 6.2 显著性、实际阈值和功效分开

预先定义实际重要差异 δ，例如关键套件下降 5 个百分点。可以用两档证据：

- 预警：观察到 Δ ≤ −δ，且区间上界低于 0；存在统计证据及足够大的点估计。
- 更强确认：区间上界也低于 −δ；有证据支持下降至少达到业务阈值。

两者不要混称。连续窗口复现有帮助，但因共享基线和路由相关性，不能简单视为独立检验后相乘。阈值、窗口和复测规则应在看结果前确定。

如果目标是声明「没有超过 δ 的退化」，需要非劣效/等效检验及足够功效；普通检验 p>0.05 只能写「没有检测到显著下降」。

### 6.3 连续查看结果，需要序贯方法

每天、每小时不断重复 p<0.05 检验会累积误报。可选预先固定的观察窗口及多重比较修正，或经过校准的 SPRT、CUSUM、e-process / anytime-valid 方法。[S14][S15]

SPRT 不是在一个评分比值上直接套公式。它需要明确逐次观测的概率模型、H0/H1、独立性或相关性处理、误报/漏报目标、停止规则，以及基线不确定性。经典简单假设下，似然比的边界与 α、β 有明确关系；若用了复合假设、IRT、未知参考正确率或自适应选题，则必须另验证适用性。

**未越过边界、预算耗尽就是 inconclusive**。同时测试多个模型、账号、能力维度要处理多重比较。对身份指纹与行为探针，统计显著仅说明分布改变，不能直接转换成 degraded。

## 7. GPT 与 Claude 的专门注意事项

| 项目 | GPT | Claude |
|---|---|---|
| 版本控制 | 官方建议固定模型版本，并运行自己的 evals。[S07] | 4.6 及以后的 canonical 无日期 ID 本身就是固定快照；更早模型的一些无日期短名是便利别名。不能一概把无日期 ID 当滚动别名。[S11] |
| 推理设置 | 显式记录 reasoning.effort；支持值与默认值因模型而异。支持的模型还需固定 reasoning.mode、reasoning.context。[S06] | 显式记录 output_config.effort 与 thinking；默认 effort 和是否能关闭 thinking 因型号而异。[S09][S10] |
| 推理量 | 支持时记录 usage.output_tokens_details.reasoning_tokens。它衡量用量，不直接等于能力。[S06] | thinking 文本是摘要，可能为空；真实计费用量和展示长度不同。**不能用 thinking 字符数推导真实推理 token。** [S10] |
| 截断 | max_output_tokens 与 incomplete_details，检查是否预算耗尽。[S06] | max_tokens 同时容纳 thinking 和最终输出；记录 stop_reason，排查预算截断。[S09][S10] |
| 产品路径 | API 与 ChatGPT/Codex 分别建立基线，记录工具、记忆、模式与会话状态。 | API 与 Claude Code/SDK/Cowork 分别建立基线，重点测上下文保留、空闲恢复和 effort。[S03] |
| 辅助指纹 | 某些 Chat Completions 返回 system_fingerprint，但当前文档标记 Deprecated；seed 也被标记 Deprecated，不应把它们作为核心长期方案。[S08] | 模型 ID 固定权重不等于固定全部基础设施，官方明确路由、安全分类器和采样实现可以变化。[S11] |

特别注意：跨代模型的同名 effort 不一定对应相同算力，也不保证同名参数的实际能力相等。比较同一模型的时间漂移，应保持可见配置；比较不同模型，应报告质量、延迟、用量的联合表现。

## 8. 账号、代理、时段如何归因

先区分「有质量下降证据」和「已定位根因」。推荐随机交错、尽量同时运行以下组合，而不只是先后更换一次代理：

| 组合 | 目的 |
|---|---|
| 账号 A × 出口 X | 被测路径。 |
| 账号 A × 出口 Y | 检查同账号是否与出口相关。 |
| 账号 B × 出口 X | 检查同出口是否影响多个账号。 |
| 账号 B × 出口 Y | 对照路径。 |

固定模型、effort、客户端、题目、工具和预算；记录独立会话与实际回退。多个时间窗口复现，再估计账号效应、路径效应及其交互。

解释原则：

- 同出口多个账号变差：与路径相关，仍可能涉及网络、重试、区域路由或共同客户端，不立即判「代理触发官方降智」。
- 同账号多个出口仍变差：支持账号关联，但不能排除订阅配置、会话状态、粘性路由和请求形态差异。
- 多个参考也一起下降：支持共同因素；共同客户端更新、工具环境和判分器同样可能导致它，未必已经定位到厂商。
- 只有特定时段下降：记录时间相关性，不能直接写成官方时段策略。
- 被测和参考都一起下降：实时相对分可能不变，所以必须保留各自历史基线。

对于市场里的卖家信用、报价和暂停调度，证据应先落到**端点质量异常**；账号原因、代理原因、官方全局原因另作置信度标签。单个第三方黑盒身份判断不宜直接作为账号处罚的唯一证据。

## 9. 对 token-market 现有方案的具体建议

对照 `docs/05-quality.md` 后，现有主动探测、实时参考、预算、疑似/确认/无法判定的结构有参考价值。建议调整以下内容；本次仅提出建议，没有修改现有方案或检测代码。

| 当前方案 | 调研后的建议 | 理由 |
|---|---|---|
| 将代理、多用户共享与官方账号风控降智直接联系 | 改为待检验的风险假设；默认结论采用「服务质量下降」 | 没有核到足以证明普遍因果关系的一手证据。 |
| Claude 取 thinking 内容长度判断推理强度 | 改为能力分 + usage + 显式 effort；thinking 摘要长度单独标为展示指标 | 当前官方文档明确摘要与真实推理量不同。 |
| 相对分=被测分/参考分，直接用于 SPRT | 优先题目级配对差异；若保留 SPRT，定义观测似然并纳入参考误差 | 比值不是自动合法的序贯检验观测模型。 |
| 只用同时间参考排除全局波动 | 同时保存历史基线、同时间参考、配置版本 | 两边全局下降时比值可能不变。 |
| 换代理仍差即归为账号；特定时段差即官方策略 | 采用账号×出口交叉实验，保留「归因未确定」 | 时间、会话和客户端等混杂仍存在。 |
| tool_call 只验证参数 schema | 加工具语义、错误恢复、结果与最终状态 | 合法 JSON 可以调用错误工具或给出错误参数。 |
| long_context 只做大海捞针 | 加 RULER 式多针、多跳、聚合、位置变化 | 单针任务覆盖过浅。 |
| 知识截止题放在 iq 主分 | 放入知识/版本行为辅助集 | 新近事实缺失与推理退化不同；知识截止自报也不可靠。 |
| 身份不一致/疑似伪装默认直接降智并归因账号 | 单独报告身份异常，校准第三方假阳性，对照主动能力测试后复核 | 身份识别是候选集内推断；更换部署、未知新型号也可能触发。 |
| 题库轮换，但使用统一趋势分 | 固定回归集与轮换探索集分开，版本变化后建立新基线 | 难度变化会制造假的时间漂移。 |

可考虑把 `degraded` 明确定义为端点或产品能力的确认回归，并增加独立的 `behavior_changed`、`identity_suspected`、`attribution_unknown` 等证据维度，而不是用一个标签同时表示能力、身份、原因和责任。

最低可行落地顺序：先完善记录字段与固定客观题；建立双基线；完成 A/A 与已知 effort 干预验证；加入配对区间与 inconclusive；最后再增加身份指纹、B3IT、IRT 与自动处置。

## 10. 哪些流行检测法不能下结论

- 一道 9.11/9.9、字母计数、乘法、素数题：可以作为失败案例，不能代表总体能力，更不能单次判断账号降智。
- 询问「你是什么型号」「你的 juice 是多少」「你是否被量化」：输出是模型生成文本；本次没有核到把这些自述与实际公共服务配置绑定的有效检测协议。
- 回复更短、思考更短、首字更快：提示词、摘要展示、缓存、问题难度和输出预算都能改变它们。
- 工具列表、知识截止自报、响应中的 model 名称：可以辅助审计接口，但不等于真实能力或权重认证。
- IP 纯净度、PoW 难度：不能在缺少受控能力对照时推导降智。
- 排行榜掉名次或跨版本不同榜单分数：可能是其他模型变好、题库更新、提示词和预算不同。
- 同一系列正在变化的 GPT/Claude 担任唯一裁判：评分器自己漂移会制造假回归。
- 未显著下降：在低功效、小样本、题目饱和时，不意味着没有下降。

## 11. 来源登记与证据边界

全部来源访问于 **2026-10-10**。官方事故复盘是厂商的一手自述，可信但不是独立审计；同行评审的历史实验只支持其具体版本与任务；新预印本与独立项目需要更多复现。主要结论没有使用付费墙二手转述。本次排除了 SEO 检测文章、AI 摘要和重复转载作为关键证据；无法保证整个互联网没有其他更好的研究。

评分是对来源可复核性的定性分级，不是统计概率：官方当前文档/已发表原研究约 9/10；新预印本约 7/10；开放独立项目约 8/10。适用于当前 GPT/Claude 的程度另按正文所述限制判断。

| 编号 | 原始来源、日期与版本 | 本文核对位置 / 用途 | 分级 |
|---|---|---|---:|
| S01 | Chen、Zaharia、Zou，How Is ChatGPT’s Behavior Changing Over Time?；HDSR 6.2，2024；arXiv 2307.09009 v3（2023-10-31） | 正式期刊正文 §1、实验概述；2023 年 GPT-3.5/4 行为变化 | 9 |
| S02 | Anthropic，A postmortem of three recent issues，2025-09-17 | Three overlapping issues、Why detection was difficult | 9 |
| S03 | Anthropic，An update on recent Claude Code quality reports，2026-04-23 | 三项变化与 API 未受影响；effort、历史思考清理、系统提示词 | 9 |
| S04 | OpenAI，Expanding on what we missed with sycophancy，2025-05-02 | Why did we not catch this；ChatGPT GPT-4o 更新 | 9 |
| S05 | OpenAI Docs，Evaluation best practices；动态页面，无显示发布日期 | 连续评测、任务分布、人类校准；当前页面还公告 Evals 平台未来退役时间 | 9 |
| S06 | OpenAI Docs，Reasoning models；动态页面，无显示发布日期 | Reasoning effort、mode、成本、reasoning.context | 9 |
| S07 | OpenAI Docs，API Overview；动态页面 | Backwards compatibility、request ID；建议固定模型版本与 eval | 9 |
| S08 | OpenAI Docs，Create chat completion；Python reference，动态页面 | seed 与 system_fingerprint 字段均标记 Deprecated；确定性无保证 | 9 |
| S09 | Claude Platform Docs，Effort；动态页面 | 默认值因型号而异；参数作用于输出、工具、思考；非严格 token 预算 | 9 |
| S10 | Claude Platform Docs，Thinking；动态页面 | Controlling thinking display、Summarized thinking；摘要非原始 CoT | 9 |
| S11 | Claude Platform Docs，Model IDs and versioning；动态页面 | Dateless IDs are pinned snapshots；权重与 serving infrastructure 区别 | 9 |
| S12 | Evan Miller，Adding Error Bars to Evals，2024-11-01，arXiv 2411.00640 v1；官方解读 2024-11-19 | 原始摘要及官方五项建议：聚类、重复、配对、功效 | 9 |
| S13 | Chauvin 等，Token-Efficient Change Detection in LLM APIs；ICML 2026；arXiv 2602.11083 v4，2026-08-03 | 全文 §4、§5.2.2、§5.2.3、附录端点；B3IT、覆盖与限制 | 9 |
| S14 | Chauvin 等，Log Probability Tracking of LLM APIs；ICLR 2026；arXiv 2512.03816 v2，2026-02-27 | 全文 Introduction、Method、Limitations；logprob 变化监测 | 9 |
| S15 | Leshin、Shah、Timmis、Kang，Behavioral Fingerprints for LLM Endpoint Stability and Identity，2026-03-19，arXiv 2603.19022 v1 | 全文 §4–§9；800 次请求、energy distance、序贯监测 | 7 |
| S16 | Pasquini、Kornaropoulos、Ateniese，LLMmap；USENIX Security 2025；arXiv 2407.15847 v4，2025-02-10 | USENIX 原始介绍及 arXiv 摘要；42 个版本的身份识别实验 | 9 |
| S17 | Wimbauer 等，Fingerprinting Inference Systems of Large Language Models，2026-05-28，arXiv 2605.29979 v1 | 原始摘要；推理引擎、硬件指纹；未全面审读全文 | 7 |
| S18 | Zhou 等，Instruction-Following Evaluation for Large Language Models，2023-11-14，arXiv 2311.07911 v1 | 原始摘要；可验证指令与 IFEval | 8 |
| S19 | Hsieh 等，RULER；COLM 2024；arXiv 2404.06654 v3，2024-08-06 | 原始摘要；13 类任务、多针、多跳、聚合 | 9 |
| S20 | Liu 等，Lost in the Middle；TACL 接收版本；arXiv 2307.03172 v3，2023-11-20 | 原始摘要；相关内容的位置与上下文利用 | 9 |
| S21 | White 等，LiveBench；ICLR 2025 Spotlight；arXiv 2406.19314 v2，2025-04-18 | 原始摘要；客观评分、更新题库和污染限制 | 9 |
| S22 | Jain 等，LiveCodeBench，arXiv 2403.07974 v2，2024-06-06 | 原始摘要；按时间收集编码题，执行/修复等维度 | 8 |
| S23 | Mirzadeh 等，GSM-Symbolic；ICLR camera-ready；arXiv 2410.05229 v2，2025-08-27 | 原始摘要；模板实例和干扰条件；作者的假说不当作定论 | 9 |
| S24 | Zheng 等，Judging LLM-as-a-Judge；NeurIPS 2023 Datasets and Benchmarks；arXiv 2306.05685 v4，2023-12-24 | 原始摘要；位置、长度、自我偏好等裁判偏差 | 9 |
| S25 | Laban、Hayashi、Zhou、Neville，LLMs Get Lost In Multi-Turn Conversation，2025-05-09，arXiv 2505.06120 v1 | 原始摘要；六任务与模拟多轮；不外推当前服务退化幅度 | 8 |
| S26 | Izawa、Minegishi、Yamakata，Anosognosia in LLMs，2026-10-05，arXiv 2610.06174 v1 | 原始摘要；量化状态自报与泛化局限，新预印本 | 7 |
| S27 | ninjahawk/livenerf；读取 commit 61616e3ec548816082bcd51d6a6246c49335eafc，2026-10-08 | README、进度、验证、限制；非厂商项目，未运行 | 8 |
| S28 | 同仓库 PREREGISTRATION.md；同一 commit | 实验筛选、独立确认、聚类差异、窗口、MDE、排除规则 | 8 |
| S29 | Marginlab Claude Code tracker；页面标注 2026-09-29 | Methodology、新基线状态；独立项目，页面指标有历史混合限制 | 8 |
| S30 | Horace He / Thinking Machines Lab，Defeating Nondeterminism in LLM Inference，2025-09-10 | Introduction 与数值/批量相关机制；温度零不充分 | 8 |
| S31 | B3IT 作者仓库；commit ad2f69295797a5f03e7b480246080d91087d9007，2026-07-19 | README、两阶段方法、代码布局与 replay；未运行 | 8 |
| S32 | LT 作者仓库；commit f2d11152a242ce3a3e08c879bb45bffdae0ff8a6，2025-12-17 | README、Remote APIs / TinyChange 文件布局；未运行 | 8 |

若随后做工程实现，应重新核对目标模型与平台支持的参数，并在模型、客户端、提示词、上下文管理、工具和题库版本改变时复查。当前 OpenAI 官方 eval 指南还公告 Evals 平台将于 2026-10-31 对已有用户转只读、计划 2026-11-30 关闭 [S05]；这不影响本文的评测方法，但新监测系统应优先选择不依赖该平台生命周期的 runner。

[S01]: https://hdsr.mitpress.mit.edu/pub/y95zitmz/release/2
[S02]: https://www.anthropic.com/engineering/a-postmortem-of-three-recent-issues
[S03]: https://www.anthropic.com/engineering/april-23-postmortem
[S04]: https://openai.com/index/expanding-on-sycophancy/
[S05]: https://developers.openai.com/api/docs/guides/evaluation-best-practices
[S06]: https://developers.openai.com/api/docs/guides/reasoning
[S07]: https://developers.openai.com/api/reference/overview/
[S08]: https://developers.openai.com/api/reference/python/resources/chat/subresources/completions/methods/create/
[S09]: https://platform.claude.com/docs/en/build-with-claude/effort
[S10]: https://platform.claude.com/docs/en/build-with-claude/thinking
[S11]: https://platform.claude.com/docs/en/about-claude/models/model-ids-and-versions
[S12]: https://www.anthropic.com/research/statistical-approach-to-model-evals
[S13]: https://arxiv.org/html/2602.11083v4
[S14]: https://arxiv.org/html/2512.03816v2
[S15]: https://arxiv.org/html/2603.19022
[S16]: https://www.usenix.org/conference/usenixsecurity25/presentation/pasquini
[S17]: https://arxiv.org/abs/2605.29979
[S18]: https://arxiv.org/abs/2311.07911
[S19]: https://arxiv.org/abs/2404.06654
[S20]: https://arxiv.org/abs/2307.03172
[S21]: https://arxiv.org/abs/2406.19314
[S22]: https://arxiv.org/abs/2403.07974
[S23]: https://arxiv.org/abs/2410.05229
[S24]: https://arxiv.org/abs/2306.05685
[S25]: https://arxiv.org/abs/2505.06120
[S26]: https://arxiv.org/abs/2610.06174
[S27]: https://github.com/ninjahawk/livenerf/tree/61616e3ec548816082bcd51d6a6246c49335eafc
[S28]: https://github.com/ninjahawk/livenerf/blob/61616e3ec548816082bcd51d6a6246c49335eafc/PREREGISTRATION.md
[S29]: https://marginlab.ai/trackers/claude-code/
[S30]: https://thinkingmachines.ai/blog/defeating-nondeterminism-in-llm-inference/
[S31]: https://github.com/timothee-chauvin/token-efficient-change-detection-llm-apis/tree/ad2f69295797a5f03e7b480246080d91087d9007
[S32]: https://github.com/timothee-chauvin/track-llm-apis/tree/f2d11152a242ce3a3e08c879bb45bffdae0ff8a6
