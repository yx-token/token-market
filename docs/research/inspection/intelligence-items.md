# "智力是否满血"质检题库设计：低成本、可自动判分、有区分度

> 整理日期：2026-10-12。证据访问日期、数字与来源编号沿用原报告；来源编号仅在本报告内有效，不能与另三份报告混用。论文/官方页面的声明是该来源的一手事实，不等于对 token-market 真实账号的实测；【推断】与候选设计均未获真实账号验证。
>
> 本文保留 13 个研究模板、53 道示例题、公开评测数字、原型验算记录与来源登记。原报告提出的“删除 22 道旧题”“40 题快检 / 150 题完整”“10 种判分器”均为研究阶段建议，本期采用已确认方案：保留 22 道旧内置题作为回退资源；8 类 Go 生成器；约 25 / 90 次请求；11 种新判分器。原 40 / 150 题的功效和预算不能直接移用到 25 / 90 次请求。Python 原型可供阶段三本地交叉验证，生产服务不执行生成代码。本次整理没有重跑原型，也没有调用真实模型。
>
> 实施优先级：[确认后的汇总](../quality-inspection-2026-10-12.md)与[数据契约](data-contract.md)高于本文的候选建议。所有业务、迁移、测试和脚本只能在 token-market 一侧实现，通过现有 Admin API、用户 API、网关、只读数据库与配置驱动 sub2api；能力缺口记为“依赖 sub2api，后续处理”，不修改 sub2api 的任何代码。


- **日期**：2026-10-11（部分资料访问跨到 10-12 凌晨）
- **适用项目**：token-market 主动探测，即 `iq`、`reasoning`、`long_context`、`tool_call` 四个套件。
- **前置阅读**：[2026-10-10 降智调研](../model-degradation-gpt-claude-2026-10-10.md)（下称"上一份调研"），本文沿用其中的 S 编号来源。
- **本文范围**：调研、出题设计和本地验算。**没有调用任何被测模型，也没有修改项目代码**。示例题的答案都用本地程序计算，并用第二种方法或独立求解器交叉验证。"预计推理 token"一律是估计值，上线前须用阳性对照实测。
- **原型脚本**：`/tmp/tm-research/intel-work/scripts/`，含斑马谜题生成与独立校验、K&K、代码输出预测、AIME 式数学、寄存器机、日期、字符串、约束判分、长上下文、偷懒检测、人民币大写、功效计算、预算估算。

## 0. 结论速览

1. **区分度来自"需要长链推理才能稳过的题"**。在基线 high 档上通过率 85–95%、压到 low 档或换成小一档模型后明显掉分的题型，按优先级排是：
   - 斑马谜题、骑士与无赖等可生成的约束题。LiveBench zebra 上，gpt-5.1 low/high 为 19.5/80.3。
   - 研究级或参数化的多步数学。ArXivMath 上，Fable 5.1 low/max 为 38.5/91.0。
   - 现学规则加长链状态追踪，即 BBEH 式题，低档接近 0，高档 50–90。
   - hard 档代码，以及多输入、程序级全对的输出预测。LiveCodeBench hard 档上，o4-mini low/high 为 38.4/63.5。
   - IFBench 式非常规约束。Opus 4.5 关/开推理 43→58。
   - 长上下文多针和两跳。MRCR 8 针 @128K 上，gpt-5.5 xhigh/关推理 85.8/28.3。
2. **事实题（SimpleQA 类）主要抓"被换成小模型"，几乎抓不到"压低推理"**。AA-Omniscience 上，Opus/Sonnet/Haiku 5.5 为 66/54/36，Opus 5.5 max/low 为 66/64。
3. **不适合作为新题的题型**：现有 22 道题全部属于这一类（实施仍保留旧题，不下线）；IFEval 式简单约束、C-Eval/CMMLU、GPQA、公开 AIME 原题、CRUXEval 原题风格；BFCL 单轮 AST、Irrelevance；单针大海捞针。
4. **13 个参数化模板**（§4），覆盖：斑马、代码输出预测、AIME 式数学、寄存器机、日期与夏令时、字符串、中英约束写作、长上下文（多跳、聚合、首尾截断、联想针）、工具调用语义、多轮约束、事实与弃权、偷懒与截断、中文专项（大写金额、汉字计数）。
5. **53 道具体示例题**（§5），每道都有完整提示词、标准答案、判分器、区分理由和 token 估计。
6. **研究阶段判分器候选**：原报告列出 10 种（§6），代码里的"……省略"占位符检测和截断、拒答分类也包含在内；确认方案补入 `accept_reject`，本期共 11 种，计数配对在引擎中完成。
7. **统计功效**（§7）：
   - 用通用题想测出 5 个百分点的下降，要 400–800 对，不现实。
   - 用精选的"降档放大器"题，40 题对平均约 24 个百分点的下降有约 0.82 的功效；温和下降（约 12 个百分点）需要约 100 题。
   - 默认多出新题，不做重复采样；在调用次数相同时，两者效果相当。
   - 日常快检 40 题：串行约 25 分钟，被测与基线并发约 13 分钟，合计输出约 270k token。单次对严重降档的功效约 0.5–0.65，定位是"发现大问题、触发完整质检"，并配合推理 token 探针使用。
   - 完整质检 150 题：串行约 106 分钟，并发约 53 分钟，4 道题并行约 13–15 分钟；合计输出约 1.0M token，输入约 1.3M token。
8. **推理强度是否兑现**（§8）：
   - 靠"锚定题 × low/high × 两个账号"比较推理 token。只需 10–20 道题就能发现 high→medium 这类约 26% 的推理量下降；同样的下降如果靠准确率来发现，要上百题。
   - LiveNerf 的实测数据：medium 对比 high，准确率 −4.2 个百分点、token −26%；把 Opus 5.5 换成 Opus 5，准确率 −3.8 个百分点，在 99% 置信度下测不出，token −23%。
   - 小模型有时反而更费 token，所以 token 检验要做双侧。
   - **现有代码有三处必须改**：
     - 对 Claude 5 家族发送 `budget_tokens` 会返回 400；
     - Claude thinking 文本默认为空，读它没有意义；
     - Gemini 3 要改用 `thinkingLevel`。
   - Kimi Code、GLM Coding Plan、Grok、Gemini CLI 都有官方写明的档位映射或换模型规则，**基线账号必须走同一产品通道**，否则会把官方行为误判成账号降智。
9. **量化**：INT8、FP8、W4A16 用准确率基本测不出来；INT4、INT3 会出现"推理 token 膨胀、准确率不升"的特征，对开源权重的 Kimi、GLM、MiniMax 有参考价值。

## 1. 现状问题：题库、判分器、探测协议

### 1.1 现有 22 道题的区分度不足（删除建议未采纳）

> 以下“删除”是原报告对选题区分度的研究建议，不是迁移指令。本期不删除、下线或改写这 22 道旧题；试运行继续由旧题驱动现有判定。正式模式仅由新程序按原题面识别并跳过，程序回退后旧题仍可被旧版 `selectItems` 选中。内置题同步只补缺，不改变管理员设置的状态。

`internal/quality/reports.go` 中的 `defaultItems()` 有 22 道题。按题型逐项看：

| 题型（现有题） | 处理 | 理由 |
|---|---|---|
| 单步算术：37×43、2^20、15% of 240、数位和 45、3x+7=31、三个连续偶数 | 删除 | 前沿模型和小模型都接近满分，GSM8K 一类已饱和。GSM-Symbolic 中 GPT-4o 与 4o-mini 在原题难度上同为 95，加 2 个子句（P2）后才拉开 15.6 分 [R-GSMS] |
| 字母计数 strawberry、反转字符串 | 删除 | 已是公开的"梗题"，可能被专门训练过；上一份调研也指出，单题失败不能代表能力 |
| 三段论 Yes/No、谁最小、行星选择题、素数个数 25 | 删除 | 二选一或四选一的猜对率高，知识题又没有推理区分度 |
| 排序 6 个数、Paris 的 JSON | 删除，或保留 1 题作格式连通性检查 | `json_keys` 只查键是否存在，太弱 |
| reasoning 套件：n! 被 2^10 整除、组合 112、2^100 mod 7、球拍问题、连续偶数 | 删除 | 都是教科书题，强弱模型都会做；球拍问题这类"陷阱"题已经广泛出现在训练数据里 |
| 糖果抽屉原理（gpt-6-astra 专用） | 保留为该模型的专项观察题，不进总分 | 单题结果只代表这道题本身（代码注释已经这样说明） |
| 大海捞针 20k/80k，单针 | 改为 T8 | 单针检索早已饱和，换成多跳、聚合、首尾、联想针，并覆盖多种长度和位置 |

如果只想保留连通性检查（availability），留 2–3 道最简单的题，**不计入智力分**。

### 1.2 判分器缺口

现有 6 种判分器覆盖不了以下需求：多字段精确比较、集合与别名、组合约束、JSON Schema、工具调用语义、弃权识别、完整输出与偷懒检测。§6 保留原报告 10 种候选的规格；实施还包含确认方案的 `accept_reject`，共 11 种，完整字段以[数据契约](data-contract.md)为准。

另外，`numeric` 取"最后一个数字"、`choice` 取"最后一个大写字母"，遇到解释性文字时容易误判。建议所有新题统一要求 `ANSWER:` 标签或固定的行格式。

### 1.3 探测协议缺口

- **只能单轮**：只发一条 user 消息，不支持 system、多轮 messages 和 tools。
- **Claude 参数用的是旧写法**：对 Claude 5 家族发送 `budget_tokens` 会返回 400；读取 thinking 文本长度也不可行（见 §8.1）。
- **Gemini 推理参数需要更新**：仍在用 `thinkingBudget`，没有用 Gemini 3 的 `thinkingLevel`。
- **缺少结束原因和拒答分类**：没有记录结束原因，也没有拒答类别（如 Anthropic 的 `stop_details.category`）。
- **题目是静态 prompt**：上游一旦识别出固定题，可以把考题路由给真模型。已有审计研究描述过这种规避手法 [R-CAI]。题目应改为"模板 + 参数 + seed"，现场生成。

## 2. 哪些题型有区分度：证据汇总

### 2.0 一表看懂

下表用"高 / 中 / 低"概括各题型对三类降智方式的敏感度：被压低推理强度、被换成小一档模型、被截短上下文。括号中是代表性证据；完整数字和来源见 2.1–2.6。

| 题型 | 推理强度被压低 | 被换成小一档模型 | 被截短上下文 | 现状与建议 |
|---|---|---|---|---|
| 竞赛与研究级数学、参数化多步数学 | **高**（AIME 2025：o3-mini low/med/high 48.3/76.7/86.7；ArXivMath 2026：Fable 5.1 low/max 38.5/91.0） | 中–高（AIME 2025：GPT-5/mini/nano 95.0/87.5/85.0） | — | AIME 2026 已饱和（32 个模型中 19 个 ≥95%）；只能用参数化或研究级的新题，难度放在基线 high 档 85–95% |
| 逻辑约束：斑马、K&K、数独 | **高**（LiveBench zebra：gpt-5.1 不开推理/low/med/high 为 8.3/19.5/63.0/80.3；K&K 8 人题非 o1 最佳模型 11%、o1 0.86；Enigmata 中 Claude 3.7 开/关思考 53.2/22.7） | 中–高 | — | 搜索空间是天然的难度旋钮，**首选** |
| 代码：hard 档生成与输出预测 | **高**（LCB hard：o4-mini low/med/high 38.4/52.7/63.5；Aider：gpt-5 high/med/low 88.0/86.7/81.3） | 中（GPT-5 mini ≥ GPT-5；Opus 4 ≈ Sonnet 4） | — | CRUXEval 原题和 LCB easy 档已饱和；要多输入、程序级全对、带异常路径 |
| IFBench 式非常规约束、高密度指令 | **高**（GPT-5 minimal/low/medium/high 45.6/66.6/70.6/73.1；Opus 4.5 关/开推理 43.0/58.0） | 低（同为 high 档时 GPT-5/mini/nano 为 73.1/75.4/67.6） | — | IFEval 已饱和（前沿模型 90–95），应删除 |
| 中文精确指令（SuperCLUE） | 中 | **中–高**（同家族 Pro 对 Flash 差 10–14 分） | — | 中文约束写作比 C-Eval 有用 |
| 多轮约束保持（MultiChallenge） | 中，且**不单调**（Opus 4.6 不开思考 56.0 > thinking-max 37.2） | 中（Opus/Haiku 4.5 为 59.0/50.5） | — | 只改造版本编辑、格式保持、抗迎合三类，程序判分 |
| 工具调用：多轮、记忆、τ 系列 | **高**（τ²-airline：GPT-5.2 effort none→high 为 52.5→83.0；BFCL 记忆，推理开/关 54.0/26.2） | **高**（BFCL 多轮：Opus/Sonnet/Haiku 4.5 为 68.4/61.4/53.6） | — | BFCL 单轮 AST、Live、Irrelevance 没有区分度 |
| 长上下文：多针、两跳、聚合 | **高**（MRCR 8 针 @128K：gpt-5.5 xhigh/low/关推理 85.8/67.0/28.3） | **高**（NoLiMa 4K：GPT-4.1/mini/nano 91.7/58.7/36.7；MRCRv2 @128K：Opus 5 对 Sonnet 5 为 91.3/52.9） | **高**（首尾题，见 T8） | 单针 NIAH 已饱和 |
| 事实性（SimpleQA 类） | **低**（AA-Omniscience 中 Opus 5.5 max/low 准确率 66/64） | **高**（Opus/Sonnet/Haiku 5.5 为 66/54/36；SimpleQA Verified：Opus 5/Sonnet 5.5/Haiku 5.5 为 63.3/44.7/23.9） | — | 题池必须私有、人工核对，禁用联网 |
| 弃权 | 低，且方向不定（推理微调使弃权平均下降 24%） | 低（Llama 3.1 8B/70B/405B 为 0.66/0.64/0.68） | — | 只作辅助信号，用阈值规则计分 |
| 中文选择题（C-Eval/CMMLU） | 低 | 低 | — | 已饱和（前沿模型 90–94），删除 |
| 单步算术、字母计数、教科书陷阱题 | 低 | 低 | — | 删除 |
| 输出偷懒、截断 | 中（Claude Code 复盘：限制输出长度后，一项评测降约 3%） | 中 | — | 不是智力题，单独列为"输出完整性"质检项 |

### 2.1 数学、推理、科学

以下数字均为 MathArena、OpenAI、AA、LiveBench 等原始页面或数据文件中的一手数据 [R-MATH]。

- **AIME / HMMT（MathArena）**
  - AIME 2025，o3-mini low/med/high：准确率 48.3/76.7/86.7，平均输出 token 2.4k/6.2k/11.4k。
  - AIME 2025，o4-mini 三档：61.7/84.2/91.7；GPT-5-nano low/high：47.2/85.0。
  - HMMT Feb 2025 差距更大：o3-mini 28.3/53.3/67.5，o4-mini 47.5/67.5/83.3。
  - 按尺寸：AIME 2025 上 GPT-5/mini/nano(high) 为 95.0/87.5/85.0。
  - **饱和**：AIME 2026 的 32 个模型里有 19 个 ≥95%，Qwen3.5-9B 也有 92.5；MathArena 已把终答题类竞赛整体标为 Deprecated。所以公开竞赛原题只能用来抓"关了推理"或极小模型。
- **研究级新题（MathArena ArXivMath，2026）**
  - Claude Fable 5.1 low/max：6 月题集 38.5/91.0，token 2.1k/117k；5 月题集 51.3/87.5。但在 8 月题集上，low 用了 72.7k token，只比 high 低 8.8 分。**同名档位背后的实际预算会变，阈值要持续校准。**
  - GPT-6 Astra low/max：84.4/94.4，token 1.7k/12.6k。
  - Opus 5.5 (high) 总榜 83.7。
  - 不同家族的档位差差别很大，例如 Astra 在 low 档仍有 84–89，所以阈值必须按模型家族分别标定。
- **OpenAI 官方数字**
  - o3-mini 发布页：AIME 2024 low/med/high 60.0/79.6/87.3，GPQA 70.6/76.8/79.7。
  - GPT-5 开发者页（high 档）：GPT-5/mini/nano 的 HMMT 93.3/87.8/75.6，HLE 24.8/16.7/8.7。
  - 结论：从 low 到 high，在 AIME/HMMT 上差 27–39 分，在 GPQA 上只差 5–10 分。
- **GPQA-Diamond 已饱和**
  - 前沿模型集中在 91.7–96.3。GPT-5.5 low→xhigh 为 91.0→93.5，不开推理 76.8；Fable 5.1 low→max 为 88.1→93.7。
  - 只有 198 道四选一，档位之间的差距在噪声范围内，只能识别"关了推理"。AA 从指数 v4.2 起已移除 GPQA。
- **HLE**
  - 地板效应已经消失。HLE-Diamond（2026-09-22，1000 题）high/max 两档：GPT-6 Astra 59.9/65.8、Opus 5.5 54.6/60.9、Sonnet 5.5 33.1/43.3。
  - AA 的 HLE，low 对最高档：Opus 5.5 48.3/61.4、Sonnet 5.5 36.2/55.0、Haiku 5.5 27.0/44.4。
  - 不适合直接采用：一半是知识题；官方判分要用 LLM；尺寸和档位会混在一起，例如 Sonnet 5.5 max 的 55.0 高于 Opus 5.5 low 的 48.3。
- **BBEH**
  - 调和平均：o3-mini(high) 44.8，Gemini 2.0 Flash 9.8，GPT-4o 6.0。
  - 分差最大的子任务（o3-mini / Flash / GPT-4o）：
    - Temporal Sequences 68.5/0.5/0；
    - Object Properties 56.5/1.5/0；
    - Buggy Tables 59.5/3.5/0.5；
    - Multistep Arithmetic 73/9.5/5.5；
    - Object Counting 90/11/6.5。
  - 同一批任务的 BBH 原题已饱和：不开推理的 Gemini 2.0 Flash 多步算术 99.6、shuffled objects 100。
  - 启示："现学一个新定义的运算 + 长链状态追踪"是最好的放大器，也就是本文的 T4、T5、T6。
- **ZebraLogic**
  - 整格准确率，按搜索空间 Small/Medium/Large/X-Large：

    | 模型 | Small | Medium | Large | X-Large |
    |---|---:|---:|---:|---:|
    | o1 | 97.2 | 92.1 | 78.0 | 42.5 |
    | o1-mini | 87.5 | 76.8 | 39.0 | 12.0 |
    | GPT-4o | 80.0 | 19.6 | 2.5 | 0.5 |
    | Claude 3.5 Sonnet | 84.7 | 28.9 | 4.0 | 1.0 |

  - 隐藏推理 token：o1-mini 约 5.1k，GPT-4o 只有 544。在约 20 个求解冲突以内，每多 1 个冲突大约多用 400 token。
  - 这是 2025 年初的数据。LiveBench 2026 年的数据显示，前沿模型在高档位已把 zebra 做到满分，而低档位仍然很低：gpt-5.4-nano 从不开推理到 xhigh 依次为 0.6/14.7/66.3/88.8/97.4。正好符合"基线稳过、降档明显掉分"的要求。
- **LiveBench**
  - 2026-06 题集里已饱和的类别：AMPS_Hard 97–99、math_comp 91–98、zebra 100。
  - 仍能拉开差距的：integrals_with_game（Opus 4.7 low/xhigh 为 31/85）、logic_with_navigation（52–88）。
  - 不单调的例子：gpt-5.3-codex 的 zebra 在 high/xhigh 档为 68/60；Haiku 5.5 的 integrals 在 xhigh/max 档为 89/67。**不要依赖 xhigh 与 max 之间的差。**
- **安全护栏会造成误报**：Opus 5.5 触发安全护栏时，会透明地改由其他模型作答。LiveNerf 在 Claude Code 订阅通道上观察到：pilot 的 5 个样本中有 3 个部分由 claude-opus-4-8 作答 [R-LNERF]。API 侧的回退机制见 [C-SKILL]。所以题目要避开生物、网络安全等容易触发分类器的主题，否则干净的基线账号也会"变弱"。

### 2.2 effort 干预的直接证据：AA 指数、LiveNerf、厂商曲线、Epoch

**Artificial Analysis Intelligence Index v4.3.2**（2026-09 起）[R-AA]

- **组成**：10 项评测，括号内为权重和"题数 × 重复次数"：
  - AA-Briefcase 15%（91×1）、GDPval-AA 10%（220×1）、AutomationBench-AA 5%（657×1）
  - Terminal-Bench 4.0 10%（66×3）、SciCode 10%（288×3）
  - AA-Omniscience 15%（6000×1）、GDP.pdf 10%（100×5）、AA-LCR 5%（100×3）
  - HLE 10%（2158×1）、CritPt 10%（70×5）

  GPQA、IFBench、τ²、MMLU-Pro、LiveCodeBench、AIME25 已陆续移出。
- **参数**：推理模型温度 0.6，非推理模型温度 0；推理模型的 max output 取厂商允许的上限。
- **判分**：
  - 多选题先用正则抽 `Answer: X`，失败再试 `\boxed{}`，取最后一次匹配。
  - HLE、LCR、Omniscience、GDP.pdf 用 GPT-5.6 Luna(medium) 判定。
  - Terminal-Bench、AutomationBench 用程序判分。
- **effort 处理**：每档 effort 单独作为一个条目上榜。

各档的指数与"跑完整个指数的输出 token 数"：

| 模型 | low | medium | high | xhigh | max |
|---|---|---|---|---|---|
| Claude Opus 5.5 | 42.3 / 19.6M | 51.2 / 38.2M | 53.6 / 52.5M | 56.0 / 99.7M | 57.6 / 259.9M |
| Claude Sonnet 5.5 | 35.9 / 22.7M | 40.8 / 31.1M | 46.8 / 52.1M | 51.9 / 105.1M | 56.0 / 415.8M |
| Claude Haiku 5.5 | 29.5 / 32.3M | 34.5 / 53.8M | 37.8 / 97.1M | 41.2 / 180.0M | 43.4 / 435.2M |
| Claude Fable 5.1 | 46.8 / 32.5M | 48.9 / 44.2M | 51.2 / 62.0M | 53.2 / 120.5M | 53.4 / 188.5M |
| GPT-6.1 Sol | 42.1 / 9.0M | 47.8 / 15.3M | 50.2 / 25.4M | 51.0 / 36.0M | 51.8 / 67.2M |

其他模型：GPT-6 Luna 非推理 18.5、low 21.5、max 38.1；Kimi K3 low 30.1、max 43.6；GLM-5.3 low 34.3、max 44.8；Grok 4.7 low 42.2、xhigh 46.4。

- **对 effort 最敏感的子评测**（low → max）：
  - Terminal-Bench 4.0：Opus 5.5 +28.3 分，Sonnet 5.5 +42.9 分，GPT-5.6 Sol 从 1.0% 升到 39.9%。
  - HLE：Kimi K3 +21.9 分，Sonnet 5.5 +18.8 分。
  - CritPt：Sonnet 5.5 +20.0 分。
- **几乎不敏感的子评测**：
  - Omniscience 准确率：Opus 5.5 +2.7 分，GPT-6.1 Sol +3.2 分。
  - SciCode：GPT-6.1 Sol +1.0 分。
  - AA-LCR：GPT-6.1 Sol −1.0 分。
  - GDP.pdf：Opus 5.5 +0.6 分。
- **token 的变化比分数的变化大得多**：Opus 5.5 做 HLE，每题输出从 1,341 涨到 35,832 token；做 Omniscience，每题从 254 涨到 8,011 token，准确率只多 2.7 分。
- **小模型可能更费 token**：Sonnet 5.5 max 的指数只比 Opus 5.5 max 低 1.6，却多用约 60% 的 token。所以"被换成小模型"时推理 token 可能**变多**，token 检验要做**双侧**，见 §8.2。

**LiveNerf**（Claude Code `claude -p`，Max 订阅，测 Opus 5.5）[R-LNERF]

- **设置**：CLI 固定为 2.1.280，effort 显式设为 high，不给工具，精确匹配判分。
- **选题**：
  - 候选 2,336 题，来自 GPQA Diamond、MMLU-Pro、竞赛数学和 AIME，每题先跑 4 次。
  - 97% 的题要么恒对、要么恒错，只保留 4 次中对 1–3 次的 **78 题**。
  - AIME 题被判定为背题：最难的题只用 27–32 个 token 就答对了。
- **干预实验**（1,248 个有效样本）：

  | 对比 | 准确率差 | 输出 token 变化 |
  |---|---|---|
  | medium vs high | −4.2 ± 3.9 分 | −26% |
  | low vs high | −8.3 ± 4.5 分 | −62% |
  | 换成 Opus 5（high） | −3.8 ± 6.3 分 | −23% |

  中位输出 token：high 638、medium 474、low 293、Opus 5 512。同家族换模型在 99% 置信度下检测不出来。
- **最新进度（2026-10-10）**：第 17/30 天，基线 58.4%，最新 57.7%。每个 10 天窗口的实测最小可检测变化为 6.6 分，最早约 10-24 能下结论。**目前不能据此说 Claude 已被证实降智。**
- **注意**：pilot 中 5 个样本里有 3 个，部分内容由 claude-opus-4-8 作答，原因是安全分类器把请求回退给了其他模型，需要按 `modelUsage` 剔除。本平台检测 Claude 订阅通道时，也要记录并剔除这类回退。

**厂商自己公布的 effort 曲线**

- **Anthropic Opus 5.5 发布页**（默认 effort 为 medium）[R-VEND]：
  - Terminal-Bench 4.0：low 38.5%、medium 57.6%、high 64.2%、xhigh 66.4%、max 64.8%。max 反而低于 xhigh。
  - WANDR：low 31.2%、medium 62.8%、max 72.3%。
  - FrontierCode：medium 54.6%、xhigh 51.4%，不单调。
- **Anthropic Opus 4.5 发布页**：medium 档追平 Sonnet 4.5 在 SWE-bench Verified 上的最好成绩，输出 token 少 76%。
- **OpenAI GPT-6 Sol/Luna 发布页**，AutomationBench 从 low 到 max：
  - Sol：21.2% → 33.2%（xhigh）→ 32.0%（max）
  - Luna：1.2% → 20.7%
  - Astra：30.3% → 41.4%
- **Google**：Gemini 3.8 Flash 模型卡只定性说明更高档位会多用 token，没有找到按档位给出的官方分数。

**Epoch AI Benchmarking Hub**（官方数据包）[R-EPOCH]

- FrontierMath Tier 1–3：
  - GPT-5 minimal/low/high：18.2/37.2/55.4%
  - o3-mini low/medium/high：3.9/10.5/18.6%
  - o4-mini 三档：16.1/28.8/36.1%
- OTIS Mock AIME：GPT-5 minimal 46.7% → high 91.4%。
- GPQA Diamond 基本不敏感：o3 low/high 79.8/81.8%，Opus 5 low/max 87.9/93.9%。反例是 Sonnet 5，max 80.3% 低于 xhigh 90.5%。
- 国际象棋谜题：Opus 5 low 20% → max 42%。
- 按尺寸（均为 max 档），Opus/Sonnet/Haiku 5.5：
  - FrontierMath Tier 4：95.0/80.5/46.3%
  - SimpleQA Verified：72.2/46.5/23.8%
  - GPQA：90.6/95.6/89.6%，拉不开差距

**小结**：effort 敏感度从高到低依次是：

1. 长程 agent 任务（Terminal-Bench、AutomationBench、WANDR）：低档掉 20–40 分。
2. 没背过的高难推理（HLE、CritPt、FrontierMath、谜题）：差 10–40 分。
3. 知识召回（Omniscience、SimpleQA）：effort 只带来 2–8 分，但换小一档掉 12–48 分。
4. GPQA、MMLU-Pro、SciCode、AA-LCR、背过的 AIME：几乎不敏感。

单一信号里最灵敏的是**输出 token**：同一题在不同档位下相差 2–30 倍。

### 2.3 代码

- **CRUXEval 原题已饱和，且对 effort 不敏感**。
  - 输出预测：o3-mini low/high 为 97.6/98.1，平均推理 token 为 213 和 1311，token 差 6 倍、准确率只差 0.5 分。GPT-5.2 和 Gemini 3 Pro 都是 99.1。
  - 同样 684 个程序、换 10 个变异的新输入后，GPT-5.2 降到 79.6，"同一程序所有输入全对"只有 56.1%；Gemini 3 Pro 为 97.2%。
  - 预测会抛出哪种异常：GPT-5 Nano / Gemini 3 Pro / GPT-5.2 为 71.9/48.6/14.8。GPT-5.2 低于 Nano，可能与默认 effort 不同有关，所以配对测试必须锁定参数。
  - 一手来源：[R-CRUX]、CodeCrash（arXiv 2504.14119）、arXiv 2604.16320。
- **LiveCodeBench 只有 hard 档能拉开 effort 差距**。
  - 官方 JSON 复算：o4-mini low/medium/high 总分 65.9/74.2/80.2，hard 档 38.4/52.7/63.5，easy 档 98.2/98.2/99.1。
  - 2026-09 vals.ai 自测：Claude Fable 5.1 为 90.5（hard 80.6），Opus 5 为 89.0，Sonnet 5 为 82.4；Opus 4.5 有/无 thinking 为 83.7/75.0（hard 64.0/44.6）；Grok 4.1 Fast 推理/非推理 80.6/42.6；GPT-5/mini/nano(high) 为 85.9/86.6/70.2。
  - 一手来源：[R-LCB]。
- **Aider polyglot**。
  - gpt-5 high/medium/low：正确率 88.0/86.7/81.3，编辑格式合规率 91.6/88.4/86.7。
  - Claude Opus 4 开 32k thinking / 不开：72.0/70.7。
  - Gemini 2.5 Flash 开 24k thinking / 不开：55.1/44.0。
  - 偷懒基准：89 道重构题，靠 AST 节点数比对判定；用正则 `^[+]? *[#].* [.][.][.] ` 统计"……"类偷懒注释。gpt-4-1106 在 SEARCH/REPLACE 格式下只得 20%（12 题写了偷懒注释），改用 unified diff 后得 61%。
  - 一手来源：[R-AIDER]。
- **"格式漂移"会被误判成降智**。Chen 等人发现，GPT-4"能直接执行"的代码比例从 52.0% 降到 10.0%，主要原因是输出里多了 ``` 代码块围栏 [S01]。所以所有判分器都要先剥掉围栏和首尾空白。

### 2.4 指令遵循、多轮、工具、中文

- **IFEval 已饱和**：GPT-5.2 94.8、Gemini 3 Pro 93.5、Claude 4.5 Opus 90.9（Qwen3.5 模型卡，厂商自报）。IFBench 论文指出 2B 级模型也能拿到 80 分以上 [R-IFB]。
- **IFBench** 有 58 种训练分布外的约束，对推理开关最敏感：
  - Opus 4.5 关/开推理 43.0→58.0；Kimi K2.6 44.3→76.0；GLM-5.1 52.0→76.3。
  - GPT-5 minimal/low/medium/high 为 45.6/66.6/70.6/73.1；Gemini 3 Pro low/high 为 49.7/70.4。
  - Grok 4.3 low/medium/high 为 81.0/83.3/81.3，不单调。
  - 对模型尺寸不敏感：GPT-5.4 nano xhigh 75.9，高于 GPT-5.4 xhigh 的 73.9。
  - 来源：AA 页面内嵌数据 [R-AA]。
- **IFScale（指令密度）**：10 条指令时各模型都 ≥92%。100 条时 gpt-4.1/mini/nano 为 95.4/80.0/51.6。250 条时 o3 medium/high 为 91.8/97.8，Opus 4 关/开思考为 67.9/81.5 [R-IFS]。
- **中文精确指令**（SuperCLUE，规则脚本判分）：
  - 2026-03：Gemini-3.1-Pro 56.76、Claude-Opus-4.6(max) 47.57、GPT-5.4(xhigh) 44.32；海外模型均分 30.68，国内 15.68。
  - 2026-07 同家族大小档：DeepSeek-V4-Pro/Flash 49.52/35.24，Qwen3.8-Max/Flash 43.81/30.48，GLM-5.3/Flash 40.00/30.48。
  - 典型判错样例："第二段第 10 个汉字须为'效'""应写 6 句写成 7 句"[R-SC]。
- **MultiChallenge**：
  - 规模：273 段对话，平均 5 轮，用 LLM 裁判加实例级 rubric（与人工一致率 93%）。
  - 2026-02 榜单：GPT 6.1 Sol 81.95、Claude Haiku 5.5 67.67；gpt-5.1 thinking/instant 为 63.41/51.23；Opus/Haiku 4.5 为 58.97/50.49；Opus 4.6 不开思考 56.02 反而高于 thinking-max 的 37.15。
  - 版本编辑类最难，均值约 38.5 [R-MC]。
- **"Lost in Multi-Turn"**：同一任务把信息分片在多轮里给出，平均下降 39%。强弱模型降幅相近（Sharded/Full 比值：GPT-4.1 61.8%、Llama3.1-8B 62.5%），所以"降幅"本身不能当判据 [S25]。
- **BFCL v4**（2026-04-12）：
  - 单轮 AST：Opus 4.5 88.58 对 Qwen3-4B 87.88，没有区分度。
  - 多轮：Opus/Sonnet/Haiku 4.5 为 68.38/61.37/53.62。
  - 记忆：推理开/关 53.98/26.24。
  - FC 模式与 Prompt 模式差距极大（Opus 4.5 为 77.47 对 33.47），被测与基线必须用同一模式 [R-BFCL]。
- **τ²-bench**：
  - GPT-5.2 effort none→high：airline 52.5→83.0（pass^4 从 22 到 72），telecom 57.2→89.7，retail 只从 75.0 到 81.6。
  - 尺寸：τ²-telecom 上 GPT-5/mini/nano 为 84.8/68.4/36.5。
  - 但 telecom 已接近饱和，Qwen3.5-4B 不开推理也有 87.7 [R-TAU]。
- **C-Eval / CMMLU**：前沿模型 90.5–94.0，已饱和。SuperCLUE 2026-03 总榜：Claude-Opus-4.6(max) 77.02 [R-SC]。

### 2.5 长上下文与事实性

- **RULER**：
  - passkey 和普通 NIAH 几乎全是 100。掉得最快的是聚合和 QA：GPT-4 聚合 99.0→79.7、QA 79.0→59.0；变量追踪只从 100 降到 99.6。Llama3.1-8B 聚合 97.0→36.2。
  - 17 个宣称支持 ≥32K 的模型，约一半在 32K 达标 [R-RULER]。
- **NoLiMa**：
  - 针与问题没有字面重叠，需要一次常识跳转。32K 时 13 个模型中有 11 个跌破自身短上下文基线的一半。
  - 在 4K 就已分档：GPT-4.1/mini/nano 为 91.7/58.7/36.7。
  - Llama3.3-70B 在 32K：字面直问 98.5、一跳 56.2、两跳 25.9 [R-NOLIMA]。
- **MRCR / Context Arena**：
  - 8 针 @128K：gpt-5.5 xhigh/low/关推理为 85.8/67.0/28.3；gpt-5.6 sol/terra/luna 为 92.4/80.2/58.4；claude-opus-5(max) 对 claude-sonnet-5(max) 为 91.3/52.9。
  - 分长度：opus-5 在 8K/32K/64K 为 98.7/99.3/99.9，sonnet-5 为 96.4/87.9/68.0，gpt-5.5 关推理为 66.4/35.0/37.6。
  - 2 针瞎猜也能拿不少分，8 针更好 [R-MRCR]。
- **Fiction.LiveBench**：
  - 推理开/关：claude-sonnet-4 @32k 为 91.7/44.4。
  - 尺寸：gpt-5/mini/nano @120k 为 96.9/62.5/21.9 [R-FLB]。
- **HELMET / Context Rot**：
  - 普通 NIAH 结果两极分化，多数模型要么满分要么接近 0。
  - 针与问题的语义越不像，随长度掉得越快；1 个干扰项就会降分；句子连贯的草堆反而更难 [R-HELMET][R-ROT]。
- **跨方口径差异**：同一个 GraphWalks BFS-256K 任务，GPT-5.5 在 Anthropic 口径下得 73.7，OpenAI 自报 88.1（二手 [R-GW2]）。公开榜单的数字不能直接当本平台的基线，必须自己跑配对对照。
- **SimpleQA 系列**：
  - 原版 correct：GPT-4o/mini 38.2/8.6，o1-preview/mini 42.7/8.1。
  - Verified（Kaggle，2026-10-10）：Gemini 3.1 Pro 77.5、GPT-6 Astra 75.8、GPT-5.6 Sol/Terra/Luna 69.2/41.5/39.1、Claude Opus 5 / Sonnet 5.5 / Haiku 5.5 为 63.3/44.7/23.9 [R-SQA]。
- **Chinese SimpleQA**：o1-preview/mini 67.9/44.1；Qwen2.5 72B/7B/3B 为 50.2/27.9/17.3 [R-CSQA]。
- **AA-Omniscience**（答对 − 答错，不答记 0）：
  - Opus/Sonnet/Haiku 5.5 准确率 66/54/36。
  - Opus 5.5 max/low 为 66/64；GPT-5.6 Sol max/low/关推理 为 59/57/49。
  - 结论：知识题对推理强度不敏感，对模型档位很敏感 [R-AAO]。
- **AbstentionBench**：推理微调使弃权平均下降 24%；规模几乎没有作用 [R-ABST]。

### 2.6 程序化、带验证器的推理题库（模板设计的来源）

- **Reasoning Gym**：100 多个生成器和验证器，难度可调。hard 配置下 o3-mini 63.5、R1 59.5，对比 Claude 3.5 Sonnet 40.3。从 easy 到 hard，o3-mini 在代码类掉 71.9 分 [R-RG]。
- **Enigmata**：o4-mini-high 65.1、GPT-4o 14.2；Claude 3.7 Sonnet 开/关思考 53.2/22.7 [R-ENIG]。
- **K&K**：难度由人数决定。0-shot 直答时，非 o1 的最佳模型在 8 人题只有 11%；o1 在 8 人题 0.86，在 18 人题 0.67 [R-KK]。
- **Sudoku-Bench**：4×4 单次求解 o3-mini-high 73.3、GPT-4.1 13.3；9×9 几乎都是 0。说明难度要按目标模型调，避免两头饱和 [R-SUDOKU]。
- **GSM-Symbolic**：
  - P2（加 2 个子句）：GPT-4o 88.0、GPT-4o-mini 72.4、o1-mini 89.1、o1-preview 94.0。
  - NoOp（加一句无关干扰）：o1-preview 77.4、GPT-4o 63.1、Phi-3-mini 18.0。
  - 注意：NoOp 与"过度思考"纠缠在一起。
  - 加子句是可靠的难度旋钮 [R-GSMS]。
- **Inverse Scaling in Test-Time Compute**：
  - 带干扰的计数、伪特征回归等任务，推理越多越差。
  - 斑马谜题在模型"自行决定推理长度"时，9 个模型全部出现逆缩放；但在"受控预算"下，Claude 和 o3-mini 是正向缩放 [R-INV]。
  - 所以入库前必须在基线上逐档验证单调性；不要用 NoOp 题当降智指标。

## 3. 出题原则

下面这些原则对 §4 的所有模板都适用：

1. **参数化模板 + 程序算答案**。题面由模板和随机参数生成，标准答案由生成器直接算出。每类答案尽量用**两种独立方法**交叉验证，例如暴力枚举对 DP、模拟器对"只读题面文本的重解析器"，两者一致才入库。
2. **每次检测现场生成新实例**。被测与基线拿到完全相同的实例（同一 seed）。题面不入公开代码，生成器的参数域与 seed 私有。这样做可以防止题目泄漏，也能防止上游识别固定题后"把考题路由给真模型"。审计研究里已经出现这种规避手法 [R-CAI]。
3. **只要最终答案，格式固定**（如 `ANSWER: 123`，或指定的几行）。这样可见输出只有几十 token，成本落在推理 token 上，判分也稳定。**不要要求"写出你的思考过程"**：Claude 5 家族对"在正文复现内部推理"的请求设有 `reasoning_extraction` 拒答类别 [C-SKILL]，也会白白增加可见输出。
4. **难度旋钮**用于把题目放到"基线高 effort 稳定做对（≥90%）、低 effort 或小一档模型明显掉分"的区间。入库前用阳性对照标定，见 §7.4。
5. **入库前检查单调性**：同一题在基线账号上必须满足 high ≥ medium ≥ low（准确率）。出现"逆缩放"的题族直接剔除 [R-INV]。
6. **避开会触发安全分类器的主题**，例如生物、网络安全、协助开发竞争性 AI 模型。Claude Sonnet 5.5 有 `cyber`、`bio`、`frontier_llm`、`reasoning_extraction`、`general_harms` 五类拒答 [C-SKILL]。Opus 5.5 触发安全护栏时会透明地改由其他模型作答。LiveNerf 在订阅通道上观察到：pilot 的 5 个样本中有 3 个部分由 claude-opus-4-8 作答 [R-LNERF]。API 侧的 `fallbacks` 机制见 [C-SKILL]。这样一来，干净的基线账号也会"变弱"，造成误报。
7. **同一项检测里，被测与基线必须完全对齐**：同一端点、同一模型 ID、同一 effort、同一 `max_tokens`、同一提示词，工具调用模式（FC 或 Prompt）也要相同。只要有一项不一致，配对比较就失效。BFCL 中 Opus 4.5 的 FC 模式与 Prompt 模式差距可达 44 分 [R-BFCL]。

> 原型脚本在 `/tmp/tm-research/intel-work/scripts/`，§4 中的"生成耗时"都是这些 Python 原型的实测。本文所有示例的答案都用 `python3 -I` 计算并交叉验证过。

## 4. 程序化出题：13 个可直接实现的参数化模板

### 模板总表

| 编号 | 模板 | 主要考察 | 判分器 | 可见输出 | 推理 token 估计（high，待实测） |
|---|---|---|---|---:|---:|
| T1 | 斑马谜题（中/英） | 约束搜索、长链推理 | `fields_exact`（暂可用 regex） | 20–40 | 3k–15k |
| T2 | 代码输出预测（Python/JS） | 程序语义、逐步模拟 | `exact` | 10–40 | 1k–6k |
| T3 | AIME 式多步数学（整数答案） | 计数、数论、概率 | `answer_tag` / `numeric` | 5–15 | 2k–12k |
| T4 | 寄存器机状态追踪 | 长程状态保持 | `fields_exact` + 部分分 | ~20 | 1.5k–6k |
| T5 | 日期时间算术（工作日、夏令时） | 日历规则、多步换算 | `fields_exact` | ~20 | 1k–4k |
| T6 | 字符串与序列变换 | 字符级精确操作 | `exact` | 20–60 | 1k–5k |
| T7 | 可验证指令遵循（中/英） | 多约束同时满足 | `constraints`（新） | 150–400 | 0.3k–2k |
| T8 | 长上下文：多跳、聚合、首尾、联想针 | 有效上下文、截断 | `exact` / `numeric` | ≤20 | 0.5k–3k |
| T9 | 工具调用语义 | 选对工具、参数语义 | `tool_calls`（新） | 30–150 | 0.2k–2k |
| T10 | 多轮约束保持 | 指令保留、记忆、版本编辑、立场 | 规则组合 | 50–300 | 0.2k–2k |
| T11 | 事实性与弃权 | 长尾知识、诚实 | `exact`+别名 / `abstain`（新） | 20–150 | 0.1k–1k |
| T12 | 输出偷懒与截断 | 完整输出、无占位符 | `full_output`（新） | 1.2k–3k | 0.2k–2k |
| T13 | 中文专项（大写金额、汉字计数等） | 中文规则与字符级处理 | `exact` / `set` | 10–60 | 0.5k–3k |

下面逐个说明。

### T1 斑马谜题（ZebraLogic 风格，中英双语）

- **参数范围**
  - 房子数 N ∈ {4,5,6}，属性数 M ∈ {4,5,6}。属性池有姓名、饮品、宠物、颜色、职业、运动。
  - 线索类型 8 种：在 k 号、不在 k 号、同一人、不是同一人、在左边某处、紧挨在左边、相邻、中间隔一座。
  - 直接给位置的线索（"在 k 号"）保留概率设为 0.15，越低越难。
  - 每题问 2 个属性在 1..N 号房子的排列。随机猜对的概率是 1/(N!)²；N=5 时为 1/14400。
- **生成步骤**
  1. 随机生成真解，即每个属性一个排列。
  2. 枚举所有在真解下为真的候选线索，按类型权重抽样并打乱。
  3. 逐条加入线索，直到求解器判定解唯一。
  4. 按随机顺序贪心删除冗余线索，删后仍唯一才删。
  5. 渲染成中文或英文，线索再打乱。
  6. 用独立校验器再验一遍：它**只读渲染后的文本**，用正则解析线索，再用"按格子逐个填"的另一种回溯算法求全部解。这一步同时验证渲染与唯一性。
- **标准答案**：真解中被问属性按房号排列。
- **判分**：新增 `fields_exact`。逐行解析 `Pet: dog, fish, ...`，忽略大小写、空格和中英文逗号差异，所有被问行都对才通过。另记格子正确率，用作 IRT 标定的连续分。现有 `regex` 判分器可以临时替代。
- **难度调节**：用搜索空间 (N!)^M 分档，与 ZebraLogic 的分档一致：
  - 4×4 = 3.3×10⁵，可作对照题。
  - 5×5 = 2.5×10¹⁰，属 X-large。
  - 5×6 = 3.0×10¹²。
  - 6×6 = 1.4×10¹⁷。

  线索越少、"相对位置"类线索越多，题越难。原型生成耗时：5×5 和 5×6 约 1 秒，6×6 约 100 秒，所以 6×6 应离线预生成成池。

  **难度按模型家族分别标定**。ZebraLogic 2025 年初的数据中，X-Large 档对当时所有模型都接近地板；LiveBench 2026 年的数据显示，前沿模型在高档位已把 zebra 做到满分（§2.1）。建议起点如下，然后按基线 high 档 85–95% 的目标上下调整：
  - Haiku、Flash、Luna、Flash-Lite 一档：从 4×4 或 5×4 起；
  - Opus、Sol、Astra、Pro、Fable 一档：从 5×5 或 5×6 起。
- **预计 token**：可见输出 20–40。推理 token 随搜索空间增长，ZebraLogic 论文观察到推理模型的隐藏推理量随难度上升，见 §2。
- **为什么有区分度**：难度随搜索空间单调增大。推理模型与非推理或小模型的差距在大空间上最大，即"复杂度诅咒"，见 §2 证据。
- **注意**：Inverse Scaling 论文观察到两种不同的情况 [R-INV]：
  - 模型自己决定推理长度时，斑马题上"推理越长越容易错"，部分原因是难题本来就会想得更久。
  - 在受控预算下，Claude 和 o3-mini 呈正向缩放。

  本平台显式传 effort 档位，属于后一种情况。但入库前仍须在基线上验证 high ≥ medium ≥ low，同时检查同一档内"长推理答错"的比例是否异常。

### T2 代码输出预测（CRUXEval-O 风格，免沙箱）

- **参数范围**：6 个 Python 程序族加 1 个 JS 陷阱族，常量和输入随机。
  - A 列表流水线：条件插入、切片插入、自定义 key 排序、步长切片。
  - B 字符串状态机：数字变换、元音前插、条件整体反转。
  - C 字典累加：键冲突、截断。
  - D 语义陷阱：可变默认参数、闭包晚绑定、`[[0]*3]*3` 别名、负数整除与取模、银行家舍入。
  - E 数字过程：类 Collatz 循环，带步数上限和采样轨迹。
  - G 记忆化递归：整数拆分计数。
  - JS 族：`sort()` 默认按字典序、`map(parseInt)`、隐式类型转换。
- **生成步骤**
  1. 填参得到程序文本。
  2. 在本地隔离解释器中执行（`python3 -I` / `node`，只运行我们自己生成的代码），取 stdout。
  3. 过滤掉不合格的程序：输出超过 80 字符的；依赖 set 迭代顺序、`hash()`、浮点打印格式、时间或随机数的；执行步数超出范围的。
- **标准答案**：真实执行输出。G 族另用生成函数独立验算。
- **判分**：`exact`，去掉首尾空白和代码块围栏。可在 prompt 里要求"只输出程序打印的内容"。
- **难度调节**：循环次数、分支数、可变状态个数、陷阱语义个数、E 族步数上限、G 族的 (n,k)。
- **为什么有区分度**：答对必须逐步模拟。低推理强度时模型容易"凭模式猜"，在别名、晚绑定、负数取模这些点上出错。成本也低：输出只有一行，不需要沙箱。
- **饱和警告**：CRUXEval 原题风格（3–13 行、单个输入）已经饱和。o3-mini 在 low 和 high 两档分别得 97.6 和 98.1，推理 token 却相差 6 倍；GPT-5.2、Gemini 3 Pro 都达到 99.1 [R-CRUX]。所以 T2 必须做到以下几点：
  - 同一程序给 3–6 组**新输入**，按"程序级全对"计分；
  - 加入会抛异常的输入，要求输出异常类名；
  - 循环与状态步数要足够长；
  - 不使用任何公开题。

  例 C6 就是按这个要求出的。

### T3 AIME 式多步数学（整数答案）

- **六个族**，括号内为参数范围：
  - m1 同余 + 数位和 + 排除倍数的计数（N∈[800,2000]）。
  - m2 求 a^(b^c) 的末三位（指数可达几十万，需要用欧拉定理降幂再用 CRT）。
  - m3 长度 n、恰有 m 个 1、不含 k 个连续 1 的二进制串计数（n∈[14,18]）。
  - m4 带阻塞点的格点路径计数（网格 6–8 × 5–7，阻塞 4–6 个）。
  - m5 条件概率化为最简分数 p/q，回答 p+q。
  - m6 2^a·3^b·5^c 的因子中平方数或立方数的个数。
- **生成步骤**：随机取参，用两种方法分别求答案，一致才收录：
  - m1：暴力枚举 vs 只遍历同余类。
  - m2：`pow` vs 模 8 与模 125 分别降幂再 CRT。
  - m3：DP vs 组合枚举。
  - m4：DP vs 容斥公式。
  - m5：全枚举 vs 补集计数。
  - m6：三重枚举 vs 容斥。
- **判分**：要求最后一行写 `ANSWER: <整数>`。新增 `answer_tag` 抽取器，抽不到时退回 `numeric` 取最后一个数。答案范围大，几乎不可能猜中。
- **难度调节**：增大 N 或参数、增加约束条件、引入需要技巧的结构（CRT、容斥、DP）。可以再加 GSM-Symbolic 式"增加子句"（P1/P2），它是公认可靠的难度旋钮 [R-GSMS]。
- **为什么有区分度**：多步、易在中间步骤出错。低 effort 时常见的错误是漏算、边界错（≤ 和 <）、模运算错。

### T4 寄存器机状态追踪（RULER 变量追踪 / BBEH 多步算术风格）

- **参数范围**：5 个寄存器 A–E，初值 0–30。7 种指令：ADD、SUB、MUL、SWP、IFGT（条件加常数）、IFODD（条件加寄存器）、ROT（循环右移）。指令数 L ∈ {15, 24, 40, 60}，所有运算都取模 100。
- **生成与答案**：模拟器 1 直接执行指令元组。模拟器 2 把渲染后的指令文本重新解析再执行。两者一致才收录。
- **判分**：要求只输出一行 `A=..,B=..,C=..,D=..,E=..`，五个都对才通过；另记对了几个寄存器作为部分分。
- **难度调节**：增大 L、提高条件指令比例、增加 ROT 次数。各类模型的出错率都随 L 线性上升，而推理强度决定曲线的斜率。
- **为什么有区分度**：每一步都依赖前一步，一处出错就全错，偷懒、跳步或注意力不足都会直接暴露。

### T5 日期时间算术（工作日 + 夏令时）

- **参数范围**：起始日期；工作日数 n ∈ [5,15]；1–3 个节假日（可以和周末相邻）；绝对时长 h ∈ [100,400] 小时；可选是否跨夏令时切换；时区。
- **生成与答案**：方法一用 Python `datetime` + `zoneinfo`；方法二手动推算 UTC 偏移，美国夏令时 2026-11-01 02:00 结束。两者交叉验证。
- **防止考成知识题**：题面直接写明夏令时切换时刻，只考推理不考记忆。
- **判分**：要求输出 `DEADLINE=YYYY-MM-DD; CALL=YYYY-MM-DD HH:MM`，两个字段都要对。
- **难度调节**：跨月、跨夏令时、跨年，节假日落在周一，绝对时长与本地挂钟时间的差别。

### T6 字符串与序列变换（BBEH word sorting / dyck 风格）

- **三个子型**：
  - 按打乱的 26 字母表给 12 个词排序，含前缀词，如 kern / kernel。
  - 多步变换流水线：反转 → 元音轮换 → 取奇数位 → 凯撒移位。
  - 括号补全（Dyck）：给出 26 个符号，补齐闭合序列。
- **答案**：每个子型都有两种实现交叉验证，例如 sorted 对插入排序、逐步索引对切片。
- **判分**：`exact`，词之间以单个空格分隔。
- **难度调节**：词数、前缀词个数、流水线步数、括号深度与长度。
- **为什么有区分度**：分词器会把字符级操作搞得很难，必须逐字符核对，低推理强度时最容易出错。

### T7 可验证指令遵循（IFEval/IFBench 风格，中英分开出题）

- **约束库**（原型已实现 16 种判分函数，见 `ifcheck.py`）：
  - 英文：词数区间、关键词恰好 n 次、禁用某些字符（如逗号）、段落数（`***` 分隔）、各段首词（藏头）、以固定句子结尾、每句不超过 k 词、任何词最多重复 k 次、全小写、要点数、JSON 字段与类型。
  - 中文：汉字数区间（不含标点）、禁用某字（如"的"）、某词恰好 n 次、空行分段数、各段首字（藏头）、不得出现 ASCII 字母和数字、固定结尾（如"——完"）。
- **生成步骤**：
  1. 每题随机抽 5–8 个约束，要求同时可满足。
  2. 用"约束构造器 + 人工参考答案"验证可满足性：参考答案必须全部通过，再故意违反一条，确认判分器能检出。
  3. 题面写明所有约束。
- **判分**：新增 `constraints`。严格分是全部通过才算对（prompt 级），宽松分是通过比例（指令级）。两者都记录，判定降智时只用严格分。
- **难度调节**：
  - 约束数：IFScale 类研究显示，指令越多越容易掉分。
  - 约束之间的张力：窄字数区间、关键词次数、禁用字符三者叠加。
  - IFBench 式的 OOD 约束：例如"第 3 个词必须是某词""相邻句子的长度必须交替"。
- **为什么有区分度**：IFEval 已饱和，只能当可用性对照。IFBench 这类没见过的约束组合，在前沿模型之间、以及不同推理强度之间仍有明显差距，见 §2。

### T8 长上下文：多跳、聚合、首尾、联想针（RULER / MRCR / NoLiMa 风格）

- **草堆格式**：结构化员工记录，形如 `[R8585] Name | Dept | Manager | Office | Joined | Badge`。每条约 30–35 token，按目标长度生成记录数：8k 约 250 条，32k 约 950 条，128k 约 3800 条。实际 token 数请用各家 count_tokens 接口校准。
  - 徽章号无放回抽样，保证唯一。
  - 同名、同姓、相近徽章号作干扰。
- **问题类型**
  - **2 跳**：徽章号 → 经理 → 经理的办公室。
  - **3 跳**：经理的经理的徽章号。
  - **聚合**：某部门、某年之前（严格小于）入职的人数，故意放一条恰好在边界年份的记录作陷阱。
  - **首尾**：第一条与最后一条记录的徽章号之和。这一题专门检测"截头或截尾"。
  - **联想针**（NoLiMa 式）：插入一句"某人最喜欢在 Semper Opera House 旁写生"，问"哪位员工去过 Dresden"。题面和针没有字面重合，需要一次常识跳转。
- **深度**：目标记录放在 10% / 50% / 90% 处。
- **答案**：生成器给出一份；另有一个只解析渲染文本的验证器，独立重算多跳、聚合和首尾，结果一致才收录。
- **判分**：多跳、联想针用 `exact`；聚合、首尾用 `numeric`。
- **难度调节**：长度、跳数、深度、干扰相似度、聚合条件数。
- **成本**：输入 token 是主要成本。只在上架检测、申诉和完整质检里跑。快检只放 1–2 道 32k 题，每次随机换类型和深度。

### T9 工具调用语义（BFCL / τ-bench 风格）

- **场景族**
  - 相对日期推算，如"10 月第三个星期五"。
  - 实体规范化：机场名 → IATA，手机号 → E.164。
  - 单位换算：华氏 → 摄氏。
  - 并行调用：一句话包含多个独立请求。
  - 缺必填参数时应该追问，不能编造邮箱。
  - 与工具无关的请求不应调用工具。
  - "先查后办"：先 `find_customer`，不能直接编造 `order_id` 退款。
- **工具集**：每题 3–8 个工具，含名字和功能相近的干扰工具。
- **答案**：期望调用的集合，包括函数名和规范化后的参数。日期与单位由 Python 计算。
- **判分**：新增 `tool_calls`。
  - 解析各家协议原生的工具调用：Anthropic `tool_use`、OpenAI `function_call`、Gemini `functionCall`。
  - 参数先规范化：大小写、数字类型、日期格式、数值容差。
  - 比较时用集合匹配，并行调用不计顺序。
  - "不应调用"类题检查调用为空，且文本里有追问。
- **难度调节**：工具数与干扰度、需要推导的参数个数、并行调用数、是否需要两步。
- **注意**：Claude Opus 5.5 / Sonnet 5.5 / Fable 5.1 不接受强制 `tool_choice`（`any` / `tool`），会返回 400，只能用 `auto` [C-SKILL]。现有探测协议只发单条 user 消息，需要扩展 `tools` 字段，见 §9。

### T10 多轮约束保持（MultiChallenge 风格，程序判分）

- **四类**，对应 MultiChallenge 的四个类别：
  - **指令保留**：第 1 轮设定格式约束，最后一轮用"详细比较一下……"诱导长答。
  - **推断记忆**：第 1 轮提到过敏或禁忌，最后一轮让推荐零食。
  - **版本编辑**：多轮修改同一句话，最后"撤销第 2 次修改、保留第 3 次"。
  - **自洽与抗迎合**：用户坚称一个错误的计算结果。
- **生成**：可以替换话题、约束、禁忌词表、被编辑的文本；助手的中间轮回答预先写好，且都合规。
- **判分**：全部是程序规则，包括行数、前缀、末行、禁用词（先剥离"无坚果""不含花生"这类否定短语再查）、精确文本、最后一个数字。
- **协议要求**：probe 需要支持发送 `messages` 数组。只把历史拼进单条 prompt 会削弱这类题的效果，因为模型看到的不是真实的多轮结构。

### T11 事实性与"不知道就说不知道"

- **四个子型**
  1. **虚构实体**（可程序生成）：随机拼一个人名，配上真实奖项和错误年份。例如问"2019 年菲尔兹奖得主 X"，而菲尔兹奖每四年颁发一次（如 2018、2022 年），2019 年没有颁发。期望模型指出查无此人或前提错误。
  2. **伪前提**（可程序生成）：例如"为什么 4294967297 是质数？"它其实等于 641×6700417。期望模型纠正前提。
  3. **未来或不可知事件**：例如"2027 年诺贝尔物理学奖得主"。
  4. **真实长尾事实**：来自私有、人工双人核对的题池，配别名表。
- **判分**
  - 1–3 用新增的 `abstain`：命中弃权或纠正标记的正则即通过，标记包括"不存在、没有记录、无法确认、并非质数、I don't know、no record、not prime"等。
  - 4 用 `exact` 加别名表，并记录三分类（正确 / 错误 / 弃权）。可以借鉴 SimpleQA 的计分方式。
- **两种问法**：
  - 自然问法：不提示可以拒答，测模型会不会顺着前提编造。
  - 校准问法：提示"不确定就只输出 UNKNOWN"，并写明计分规则：答对 +1，答错扣 t/(1−t)，UNKNOWN 记 0。这是 OpenAI 建议的阈值计分法 [R-ABST]。

  两种问法分开统计。
- **区分什么**：事实题主要区分**模型规模**，所以对"被换成小一档模型"敏感，对推理强度不敏感，证据见 §2。弃权题主要区分模型的诚实性和校准程度。

### T12 输出偷懒与截断

- **子型 1：整文件改写**
  - 生成一个 100–200 行、含 K∈{10,14,20} 个函数的 Python 模块。
  - 要求把某个辅助函数全局改名，同时不能误伤同前缀的其他函数名；并在每个函数体第一行加 `# reviewed`；最后输出**完整文件**。
  - 期望输出由程序变换直接得到。
- **子型 2：长枚举**：输出 1..N 里满足条件的所有整数，N ∈ [300,1000]，期望列表由程序算出。
- **子型 3：长表格**：输出 n=1..120 的若干列确定值。
- **判分**：新增 `full_output`，依次检查：
  - 抽出代码块；
  - `def` 数和 `# reviewed` 数都等于 K；
  - 旧名出现 0 次；
  - 行数不少于期望值减 2；
  - 偷懒占位符正则 0 命中，正则见 §6；
  - 结束原因不是 `max_tokens` / `length`；
  - 严格模式下与期望文本逐行比对。

  长枚举直接和期望列表比对。
- **为什么有区分度**：这类题针对的是"输出被压短、模型偷懒、截断"，而不是智力本身。Anthropic 2026-04 的复盘就记录过"系统提示要求压缩输出 → 编码质量下降"[S03]。

### T13 中文专项

- **人民币大写金额**
  - 依据《支付结算办法》附件《正确填写票据和结算凭证的基本规定》[CN-DAXIE]。
  - 生成器**只生成不触发"可写可不写'零'"条款的金额**：排除万位为 0 且千位非 0、元位为 0 且角位非 0 这两种情形。
  - 接受集合只允许"角"后可选的"整/正"，以及可选的"人民币"前缀。
  - 用两种独立实现交叉验证。
- **汉字计数与定位**：生成一段 150–400 字的中文，问某字出现几次、第 k 个汉字是什么（不计标点）。
- **中文斑马谜题**：即 T1 的中文渲染版。
- **中文约束写作**：即 T7 的中文约束。
- **中文长尾事实**：Chinese SimpleQA 风格，取自私有人工核对题池。
- **判分**：`exact` 或 `set`（接受集合）。
- **为什么需要**：C-Eval、CMMLU 这类中文选择题已经饱和（见 §2），不能再用。中文能力应改测"中文规则 + 字符级精确处理 + 中文语境推理"。

## 5. 分类示例题（完整提示词 + 标准答案 + 判分器 + 区分理由 + 预计 token）

所有示例题都已在本地验算：程序题实际执行，谜题用独立求解器确认解唯一，数学题用两种方法交叉验证，约束写作题用人工参考答案跑通判分器。"推理 token"一栏是估计值，必须用 §7.4 的阳性对照实测后再定阈值。正式使用时，每次检测都应由模板现场生成**新的实例**；本文的具体实例一旦公开就算泄题，只能用作单元测试和判分器回归测试。

### 5.1 多步数学（T3，AIME 式整数答案）

判分器：`answer_tag`，抽取 `ANSWER:` 后的值；暂时也可以用现有 `regex` 实现。所有答案都由两种方法交叉验证。

**例 M1（m2_lastdigits）**

```text
What are the last three digits of 3^(6^3)? (That is, 3 raised to the power 216.) Give the answer as a three-digit string (keep leading zeros) on the last line in the form ANSWER: <ddd>.
```
- 标准答案：`ANSWER: 721`
- 判分器：`answer_tag`（精确整数或字符串）
- 为什么能区分：指数高达 216，必须先降幂（模 8 与模 125 分别用欧拉定理）再 CRT；低推理强度常把降幂指数算错。
- 预计 token：可见 ≈10；推理 high 估计 1.5k–6k

**例 M2（m3_binary）**

```text
How many binary strings of length 18 contain exactly 7 ones and never contain 4 consecutive ones? Give only the final integer on the last line in the form ANSWER: <integer>.
```
- 标准答案：`ANSWER: 27456`
- 判分器：`answer_tag`（精确整数或字符串）
- 为什么能区分：需要 DP 或容斥，组合数大，抄近路容易漏掉边界（"恰好 7 个 1" 与 "不含 4 连" 两个约束叠加）。
- 预计 token：可见 ≈10；推理 high 估计 3k–10k

**例 M3（m4_grid）**

```text
A robot walks on the integer grid from (0,0) to (6,7). Each step moves either +1 in x or +1 in y. The points (1,1), (2,1), (2,2), (3,1), (3,2) are blocked and may not be visited. How many different paths are there? Give only the final integer on the last line in the form ANSWER: <integer>.
```
- 标准答案：`ANSWER: 372`
- 判分器：`answer_tag`（精确整数或字符串）
- 为什么能区分：阻塞点成簇，靠近起点，必须逐格 DP 或做有序容斥；草率的做法会重复扣减。
- 预计 token：可见 ≈10；推理 high 估计 2k–8k

**例 M4（m5_dice）**

```text
Three fair six-sided dice are rolled. Given that at least one die shows a 5, what is the probability that the sum of the three dice is at least 12? Write the probability as p/q in lowest terms and report p+q only, on the last line in the form ANSWER: <integer>.
```
- 标准答案：`ANSWER: 20`（概率 = 7/13）
- 判分器：`answer_tag`（精确整数或字符串）
- 为什么能区分：条件概率要先算分母 91 再计数分子，最后约分；任何一步错都过不了。
- 预计 token：可见 ≈10；推理 high 估计 1.5k–5k

**例 M5（m6_divisors）**

```text
How many positive divisors of N = 2^12 · 3^9 · 5^6 are perfect squares or perfect cubes (or both)? Give only the final integer on the last line in the form ANSWER: <integer>.
```
- 标准答案：`ANSWER: 188`
- 判分器：`answer_tag`（精确整数或字符串）
- 为什么能区分：平方与立方的容斥，需要正确处理六次方的重叠。
- 预计 token：可见 ≈10；推理 high 估计 1k–4k

### 5.2 逻辑约束谜题（T1 斑马谜题 + 骑士与无赖）

**例 L1：斑马谜题 5×5（搜索空间 2.5×10¹⁰，17 条线索）**

```text
Five houses stand in a row, numbered 1 to 5 from left to right. Each house has one resident, and the residents differ in every attribute below (each value is used exactly once):
- Name: Alice, Bob, Carol, David, Eve
- Drink: tea, coffee, milk, juice, water
- Pet: cat, dog, bird, fish, rabbit
- Color: red, green, blue, yellow, white
- Job: doctor, teacher, pilot, chef, lawyer
"Left of" means a smaller house number. Clues:
1. The teacher lives directly left of the person who keeps the cat.
2. There is exactly one house between the person who keeps the dog and the teacher.
3. The person in the green house lives directly left of the person in the red house.
4. There is exactly one house between David and the person who keeps the fish.
5. The person in the blue house lives somewhere to the left of the person who drinks water.
6. Carol lives somewhere to the left of the person who keeps the cat.
7. Bob is the person who keeps the rabbit.
8. The person who drinks water lives somewhere to the left of the doctor.
9. The person who drinks tea is not the person who keeps the dog.
10. The person in the white house lives directly left of the chef.
11. There is exactly one house between the person who drinks juice and the chef.
12. The lawyer lives directly left of Eve.
13. Alice lives somewhere to the left of Carol.
14. There is exactly one house between the person who keeps the fish and the doctor.
15. The person who drinks coffee does not live in house 3.
16. David is the person in the red house.
17. There is exactly one house between David and the person who drinks coffee.

Exactly one assignment satisfies all clues. Output exactly 2 lines and nothing else, each listing the values for houses 1 to 5 in order, separated by commas:
Pet: <...>
Job: <...>
```
- 标准答案：
  ```
  Pet: dog, fish, bird, cat, rabbit
  Job: lawyer, chef, teacher, doctor, pilot
  ```
  完整解：1 号 Alice/milk/dog/white/lawyer，2 号 Eve/coffee/fish/blue/chef，3 号 Carol/water/bird/green/teacher，4 号 David/juice/cat/red/doctor，5 号 Bob/tea/rabbit/yellow/pilot。生成器求解器与"读文本重解析"的独立求解器都确认解唯一。
- 判分器：`fields_exact`。暂时可用 regex：`(?is)pet:\s*dog,\s*fish,\s*bird,\s*cat,\s*rabbit\s*\n\s*job:\s*lawyer,\s*chef,\s*teacher,\s*doctor,\s*pilot`
- 为什么能区分：17 条线索里没有一条直接给出房号，只有 1 条否定式的"不住 3 号"，其余都是相对位置或身份等同；必须做多轮假设与回溯。按 ZebraLogic 的分档，这道题属于 X-large，是推理模型和非推理/小模型拉开差距最大的区间（证据见 §2）。
- 预计 token：可见 ≈25；推理 high 估计 3k–12k，low 明显更少，也更容易出错。

**例 L2：斑马谜题 5×6（搜索空间 3.0×10¹²，21 条线索，作高难版）**

```text
Five houses stand in a row, numbered 1 to 5 from left to right. Each house has one resident, and the residents differ in every attribute below (each value is used exactly once):
- Name: Alice, Bob, Carol, David, Eve
- Drink: tea, coffee, milk, juice, water
- Pet: cat, dog, bird, fish, rabbit
- Color: red, green, blue, yellow, white
- Job: doctor, teacher, pilot, chef, lawyer
- Sport: tennis, golf, chess, rowing, boxing
"Left of" means a smaller house number. Clues:
1. There is exactly one house between the person who plays chess and the person who plays boxing.
2. The person who drinks tea lives directly left of the person who keeps the dog.
3. The person who keeps the dog is not the person in the white house.
4. The person who drinks coffee lives directly left of the person in the red house.
5. The person who keeps the bird and the person who plays chess live next to each other.
6. Carol lives directly left of the person in the blue house.
7. The chef and the person who plays rowing live next to each other.
8. The teacher lives somewhere to the left of the person who keeps the fish.
9. The person who keeps the cat lives somewhere to the left of Bob.
10. The person who keeps the dog lives directly left of Bob.
11. There is exactly one house between the person in the yellow house and the chef.
12. The person who drinks coffee lives somewhere to the left of the person who drinks water.
13. The lawyer lives directly left of Eve.
14. The lawyer lives directly left of the person who plays tennis.
15. There is exactly one house between the person who keeps the rabbit and the pilot.
16. There is exactly one house between David and the pilot.
17. The person who drinks milk lives somewhere to the left of Bob.
18. The person who drinks coffee and the person who plays golf live next to each other.
19. Alice and the pilot live next to each other.
20. Carol lives in house 1.
21. Eve and the person in the green house live next to each other.

Exactly one assignment satisfies all clues. Output exactly 2 lines and nothing else, each listing the values for houses 1 to 5 in order, separated by commas:
Drink: <...>
Sport: <...>
```
- 标准答案：
  ```
  Drink: tea, milk, juice, coffee, water
  Sport: boxing, tennis, chess, rowing, golf
  ```
  完整解：1 号 Carol/tea/cat/green/lawyer/boxing，2 号 Eve/milk/dog/blue/teacher/tennis，3 号 Bob/juice/fish/yellow/pilot/chess，4 号 Alice/coffee/bird/white/doctor/rowing，5 号 David/water/rabbit/red/chef/golf。两个求解器都确认解唯一，独立求解器用时约 10 分钟。
- 判分器：`fields_exact`
- 为什么能区分：线索之间是链式依赖，例如 2、10、17 共同约束 Bob 的位置；多一个属性就多一个维度，搜索空间比 L1 大约 120 倍。适合在完整质检中检测"推理强度被压到 low"的情况。
- 预计 token：可见 ≈25；推理 high 估计 5k–20k。

**例 L3：骑士与无赖（8 人，K&K 风格）**

```text
On an island, knights always tell the truth and knaves always lie. Each of the 8 people below is either a knight or a knave, and each makes one statement:
Ava: At least one of Hal and Eli is a knave.
Ben: Eli is a knave.
Cora: At least one of Hal and Ava is a knave.
Dan: Fay and Eli are the same type.
Eli: Exactly 4 of the 8 of us are knights.
Fay: Gus is a knave.
Gus: Ava and Hal are the same type.
Hal: If Eli is a knight, then Dan is a knave.
There is exactly one consistent assignment. Output only the names of all knights in alphabetical order, separated by single spaces.
```
- 标准答案：`Ava Ben Dan Gus Hal`。已逐句人工验算一致；穷举 2⁸ 种情形确认解唯一。
- 判分器：`exact`，也可以按名字集合比较。
- 为什么能区分：K&K 论文中，非 o1 的最佳模型直接作答 8 人题只有 11%，o1 为 0.86 [R-KK]。人数是天然的难度旋钮。含"恰好 k 个骑士"和条件句的陈述需要整体一致性检查。
- 预计 token：可见 ≈10；推理 high 估计 1.5k–6k。

**例 L4：中文骑士与无赖（8 人）**

```text
岛上有两类人：骑士只说真话，无赖只说假话。下面 8 个人每人是骑士或无赖之一，每人说了一句话：
甲：戊和辛不是同一类人。
乙：戊和丙是同一类人。
丙：我们 8 个人里恰好有 1 个骑士。
丁：如果辛是骑士，那么庚是无赖。
戊：乙和庚是同一类人。
己：甲和丙之中至少有一个是无赖。
庚：辛和丙是同一类人。
辛：己和戊都是骑士。
符合条件的身份分配唯一。只输出所有骑士的代号，按甲乙丙丁……的顺序，用一个空格分隔。
```
- 标准答案：`丁 戊 己 辛`。已逐句人工验算一致，穷举确认解唯一。
- 判分器：`exact`，或按集合比较。
- 为什么能区分：同 L3，并且同时考中文条件句的理解，例如"如果……那么……"在前件为假时整句为真。
- 预计 token：可见 ≈10；推理 high 估计 1.5k–6k。

> 斑马谜题的中文版（5×5）放在 §5.9 例 Z1。

### 5.3 代码执行结果预测（T2，免沙箱）

提示词统一为：`What does the following program print? Output only the exact printed text, with no explanation and no code fences.`，后接代码块。判分器为 `exact`：去掉首尾空白，再去掉可能的 ``` 围栏。

**例 C1（Python，族 A）**

```python
def f(xs):
    out = []
    for i, x in enumerate(xs):
        if x % 4 == 2:
            out.insert(0, x // 3)
        elif i % 2 == 1:
            out.append(x * 2 - i)
        else:
            mid = len(out) // 2
            out[mid:mid] = [x + i]
    out = sorted(out, key=lambda v: (v % 5, -v))
    return out[1:5] + out[::-3][:2]

print(f([35, 18, 15, 16, 27, 6, 26, 24]))
```
- 标准答案：`[41, 31, 6, 17, 29, 17]`
- 为什么能区分：插入位置随列表长度变化（`out[mid:mid] = [...]`），排序 key 是二元组，最后还有负步长切片；三处都必须逐步模拟。
- 预计 token：可见 ≈15；推理 high 估计 1.5k–5k

**例 C2（Python，族 B）**

```python
def g(s):
    res = []
    for i, ch in enumerate(s):
        if ch.isdigit():
            res.append(str((int(ch) * 5 + i) % 10))
        elif ch in "aeiou":
            res.insert(0, ch.upper())
        elif i % 3 == 0:
            res.append(ch * 2)
        else:
            res = res[::-1]
    return "".join(res)

print(g("i3toah28ern3"))
```
- 标准答案：`rr76AO6IE6`
- 为什么能区分：辅音会触发整个列表反转，元音插到队首，数字按下标变换；状态在 12 步里来回翻转，跳步必错。
- 预计 token：可见 ≈10；推理 high 估计 1.5k–5k

**例 C3（Python，族 D）**

```python
def acc(x, bucket=[]):
    bucket.append(x)
    return sum(bucket)

fs = [lambda y: y * i for i in range(5)]
a = [[0] * 3] * 3
a[2][0] = 3
r1 = acc(3)
r2 = acc(9)
print(r1, r2, fs[0](5), sum(map(sum, a)), -7 // 4, -7 % 4, round(1.5))
```
- 标准答案：`3 12 20 9 -2 1 2`
- 为什么能区分：一行里集中了 5 个经典陷阱：可变默认参数累积、闭包晚绑定、`[[0]*3]*3` 共享同一行、负数整除和取模、`round` 的银行家舍入。
- 预计 token：可见 ≈15；推理 high 估计 0.8k–3k

**例 C4（Python，族 G）**

```python
from functools import lru_cache

@lru_cache(maxsize=None)
def w(n, k):
    if n == 0:
        return 1
    if n < 0 or k == 0:
        return 0
    return w(n - k, k) + w(n, k - 1)

print(w(22, 4), w(22, 2))
```
- 标准答案：`136 12`
- 为什么能区分：这是"把 22 拆成不超过 4 的正整数之和"的拆分数，需要识别出来再正确计数，或者老老实实展开递归；答案 136 已用生成函数独立验算。
- 预计 token：可见 ≈8；推理 high 估计 2k–8k

**例 C5（JavaScript 陷阱）**

```javascript
const a = [10, 9, 1, 100, 25].sort();
const b = [3, 20, 100].map(parseInt);
console.log(JSON.stringify(a), JSON.stringify(b), '5' - 3 + '2', typeof null, [1, 2, 3] + [4]);
```
- 标准答案：`[1,10,100,25,9] [3,null,4] 22 object 1,2,34`（已用 node v22 执行验证）
- 为什么能区分：默认 `sort()` 按字符串排序；`map(parseInt)` 会把下标当进制传入，parseInt(20,1) 得 NaN，parseInt(100,2) 得 4；`JSON.stringify` 把 NaN 输出成 null；`'5'-3+'2'` 先做减法再拼接字符串；数组相加会先转成字符串。快速作答时模型常凭直觉写错其中一两项。
- 预计 token：可见 ≈25；推理 high 估计 0.5k–2k

**例 C6（Python，多输入 + 异常路径，按"程序级全对"计分）**

```text
What does the following program print? Output only the exact printed lines, with no explanation and no code fences.
```
```python
def run(cmds):
    st = []
    for tok in cmds.split():
        if tok.isdigit():
            st.append(int(tok))
        elif tok == "+":
            st.append(st.pop() + st.pop())
        elif tok == "-":
            b, a = st.pop(), st.pop()
            st.append(a - b)
        elif tok == "*":
            st.append(st.pop() * st.pop())
        elif tok == "/":
            b, a = st.pop(), st.pop()
            st.append(a // b)
        elif tok == "dup":
            st.append(st[-1])
        elif tok == "swap":
            st[-1], st[-2] = st[-2], st[-1]
        else:
            raise ValueError(tok)
    return st

for prog in ["3 4 swap - dup *", "7 2 3 * - 9 /", "5 1 1 - /", "2 dup dup * * 4 swap -", "6 rot", "1 +"]:
    try:
        print(run(prog))
    except Exception as e:
        print(type(e).__name__)
```
- 标准答案（6 行）：
  ```
  [1]
  [0]
  ZeroDivisionError
  [-4]
  ValueError
  IndexError
  ```
  已执行验证，并逐行手算复核。
- 判分器：`exact`，逐行比较，6 行全对才通过。另记对了几行，作为部分分。
- 为什么能区分：CRUXEval 原题风格（单个短函数、单个输入）已经饱和，o3-mini 在 low 与 high 两档只差 0.5 分 [R-CRUX]。改成同一程序给多组新输入，并加入会抛异常的输入，按"程序级全对"计分之后，前沿模型之间重新出现明显差距（CodeCrash 等工作，见 §2.3）。这一题还包含 swap 之后的减法顺序、整除、空栈 pop、未知指令等陷阱。
- 预计 token：可见约 25；推理 high 估计 1k–4k。

### 5.4 推理放大器补充：状态追踪、日期、字符串（T4/T5/T6）

**例 S1：寄存器机（24 条指令）**

```text
Five registers A, B, C, D, E start with these values: A=19, B=8, C=23, D=11, E=25.
Execute the program below from line 01 to line 24 in order (there are no jumps). All arithmetic is modulo 100: after every operation, replace the result by its remainder modulo 100, in the range 0-99.
Instruction meanings:
  ADD X Y      X <- X + Y
  SUB X Y      X <- X - Y
  MUL X k      X <- X * k
  SWP X Y      exchange the values of X and Y
  IFGT X Y Z k if X > Y (current values) then Z <- Z + k
  IFODD X Y    if X is odd then Y <- Y + X
  ROT          (A,B,C,D,E) <- (E,A,B,C,D)
Program:
01 IFGT E A B 10
02 IFGT B A E 18
03 IFODD D A
04 SUB B D
05 ADD D B
06 ADD E D
07 ADD A B
08 IFODD B E
09 SUB B E
10 IFODD B D
11 ADD D B
12 SUB C E
13 SWP E C
14 SUB C D
15 SUB D E
16 ADD C A
17 MUL D 8
18 MUL E 2
19 IFGT E B A 10
20 IFODD D C
21 MUL E 3
22 SWP C E
23 MUL A 7
24 IFODD C E
Output only one line in exactly this format: A=<a>,B=<b>,C=<c>,D=<d>,E=<e>
```
- 标准答案：`A=59,B=57,C=38,D=72,E=55`（两个独立模拟器结果一致，并已人工逐行复核）
- 判分器：`fields_exact`，五个值都对才通过；部分分为对了几个寄存器。
- 为什么能区分：第 09 行的负数取模、第 13 和 22 行的交换、条件指令是否触发，任意一处错都会连锁影响后面。
- 预计 token：可见 ≈20；推理 high 估计 1.5k–4k（24 条），L=60 时约翻倍


**例 S2：工作日 + 跨夏令时**

```text
Today is Wednesday, 2026-10-14, in New York (America/New_York). A ticket must be resolved by the 9th business day after today (today does not count). Business days are Monday to Friday, excluding public holidays on 2026-10-19 and 2026-10-26. Call that date the DEADLINE.
A follow-up call is scheduled exactly 200 hours of elapsed real time after 09:30 local time on the DEADLINE.
Note: in 2026, US daylight saving time ends on Sunday 2026-11-01 at 02:00 local time, when clocks go back one hour to 01:00.
Output exactly one line: DEADLINE=YYYY-MM-DD; CALL=YYYY-MM-DD HH:MM (New York local time)
```
- 标准答案：`DEADLINE=2026-10-29; CALL=2026-11-06 16:30`
- 验算过程：
  - 跳过 10-19、10-26 两个周一假日，第 9 个工作日是 10-29（周四）。
  - 200 小时等于 8 天 8 小时。按挂钟时间推算是 11-06 17:30。
  - 中途经过夏令时结束、时钟回拨 1 小时，所以实际是 16:30。
  - 用 `zoneinfo` 和手算 UTC 偏移两种方法验证，结果一致。
- 判分器：`fields_exact`，两个字段都要对。
- 为什么能区分：要数清工作日、处理节假日，还要处理"经过时长"与挂钟时间的差别；低推理强度最常见的错误是忘了回拨那一小时。
- 预计 token：可见 ≈20；推理 high 估计 1k–3k。

**例 S3：自定义字母表排序**

```text
Sort the following 12 words using this custom alphabet order (earlier letters come first): vlzsnbgufkixroqywatcepmdjh
Compare words letter by letter; if one word is a prefix of another, the shorter word comes first.
Words: lumen kern zenith quartz tundra vivid orb jovial cipher kernel nectar ember
Output only the sorted words separated by single spaces.
```
- 标准答案：`vivid lumen zenith nectar kern kernel orb quartz tundra cipher ember jovial`。已用 sorted 和逐对比较的插入排序两种方法验证。
- 判分器：`exact`
- 为什么能区分：每个字母都要查新序；还有前缀陷阱 kern / kernel；分词器让字符级操作变难。
- 预计 token：可见 ≈20；推理 high 估计 1k–3k。

**例 S4：变换流水线**

```text
Start with the string: ajswjyqgnntjno
Apply these steps in order:
(1) reverse the string;
(2) replace every vowel by the next vowel in the cycle a->e->i->o->u->a (other letters unchanged);
(3) keep only the characters at odd 0-based positions (positions 1, 3, 5, ...);
(4) shift every letter forward by 3 in the alphabet, wrapping around (x->a, y->b, z->c).
Output only the final string.
```
- 标准答案：`qwqtmvh`。中间结果依次为 `onjtnngqyjwsja` → `unjtnngqyjwsje` → `ntnqjse` → `qwqtmvh`。
- 判分器：`exact`
- 预计 token：可见 ≈5；推理 high 估计 0.8k–2.5k。

**例 S5：括号补全（Dyck）**

```text
Complete the following sequence so that every bracket is properly closed. Output only the missing closing brackets, separated by single spaces.
[ ] { ( ) { < [ [ ] < > ] { [ ( [ ] [ ] { ( ) } { {
```
- 标准答案：`} } ) ] } > } }`。共 8 个，已用栈两次独立扫描验证。
- 判分器：`exact`，比较前规范化空白。
- 预计 token：可见 ≈15；推理 high 估计 0.8k–3k。

### 5.5 可验证指令遵循（T7，中英各两题，另加 1 道 JSON）

这类题没有唯一答案，判分器是一组约束。下面每题都给出一份**人工写的合规参考答案**，它已通过原型判分器，用来证明约束可同时满足；另外故意改坏一处，确认判分器能判为不通过。

**例 I1（英文，7 个约束）**

```text
Write a short travel note about an old river town. Follow ALL of these rules:
1. Exactly 3 paragraphs, separated by a line containing only ***
2. The three paragraphs must begin with the words "Bridges", "Rivers" and "Roads", in that order.
3. Total length between 90 and 130 words.
4. Use the word "lantern" exactly 3 times.
5. Do not use any commas.
6. No sentence may be longer than 18 words.
7. End the whole response with the exact sentence: Safe travels.
Output only the note.
```
- 判分规格：
  ```json
  [{"kind":"paragraphs","n":3,"sep":"***"},
   {"kind":"para_initials","sep":"***","initials":["Bridges","Rivers","Roads"]},
   {"kind":"word_count","min":90,"max":130},
   {"kind":"keyword_count","word":"lantern","n":3},
   {"kind":"forbid_chars","chars":","},
   {"kind":"sentence_max_words","max":18},
   {"kind":"ends_with","text":"Safe travels."}]
  ```
- 参考答案（90 词，全部通过；在其中加一个逗号就会被判不通过）：
  > Bridges in our old town glow at night because every arch holds a small lantern. Visitors stop to watch the light dance on the stones. The city repaints the rails each spring.
  > \*\*\*
  > Rivers carry the reflections far downstream. Fishermen say the lantern light helps them find the narrow channel before dawn. Children count the boats from the bank.
  > \*\*\*
  > Roads leading out of town are quieter now. A single lantern marks the last bus stop near the farms. Pack water and a warm coat if you plan to walk. Safe travels.
- 判分器：`constraints`。严格分要求全部通过；宽松分是通过比例。
- 为什么能区分：单个约束都简单，但词数区间、关键词恰好 3 次、禁用逗号叠在一起，需要边写边核对。低推理强度最常见的失败是关键词次数多一次、手滑写了逗号、词数越界。
- 预计 token：可见 130–180；推理 0.3k–1.5k。

**例 I2（英文，IFBench 式非常规约束）**

```text
Describe a desk lamp in exactly 4 sentences. Rules:
(1) every sentence contains exactly 9 words;
(2) no word may appear more than twice in the whole text (case-insensitive);
(3) use the word "amber" exactly once;
(4) never use the letter q;
(5) do not use digits or hyphens;
(6) plain text only, no lists or headings.
```
- 判分规格：按 `[.!?]` 切句，必须恰好 4 句、每句恰好 9 个词（词指连续字母串）；`max_word_repeat ≤ 2`；`keyword_count("amber") == 1`；`forbid_chars("qQ0123456789-")`。
- 参考答案（4×9 词，全部通过）：
  > This slim lamp bends wherever your work needs light. Its warm amber glow softens long evenings at home. A single touch switches between three brightness levels instantly. The weighted base keeps everything steady on busy tables.
- 为什么能区分："每句恰好 9 个词"必须逐句数，"全文任何词最多 2 次"要全局跟踪，这些都属于 IFBench 所说的训练分布外约束。
- 预计 token：可见 ≈50；推理 0.5k–2k。

**例 I3（英文，JSON 结构）**

```text
Return only a JSON object (no code fences, no other text) describing an imaginary product, with exactly these keys:
"name": a lowercase string with no spaces;
"year": an integer between 2020 and 2030;
"tags": an array of exactly 3 lowercase strings without spaces;
"summary": a string of 12 to 20 words that contains no commas.
No other keys are allowed.
```
- 判分：解析 JSON（不允许出现代码块围栏）；键集合必须恰好等于给定的 4 个；检查 year 的类型与范围、tags 的长度与格式、summary 的词数及不含逗号。这一题需要新的 `json_schema` 判分器，现有的 `json_keys` 只检查键是否存在，太弱。
- 参考答案：`{"name": "tidewatch", "year": 2026, "tags": ["ocean", "sensor", "solar"], "summary": "A floating sensor buoy that measures water temperature and reports readings every hour using solar power"}`（summary 16 词，全部通过）
- 预计 token：可见 ≈60；推理 0.2k–1k。

**例 I4（中文，7 个约束）**

```text
写一段描写小镇四季的短文，必须同时满足以下全部要求：
1. 恰好 4 段，段与段之间空一行；
2. 四段的第一个字依次是“春”“夏”“秋”“冬”；
3. 全文汉字数在 120 到 160 之间（标点不计）；
4. 全文不得出现“的”字；
5. “灯笼”一词恰好出现 2 次；
6. 不得出现任何阿拉伯数字或英文字母；
7. 全文以“——完”结尾。
只输出短文本身。
```
- 判分规格：
  ```json
  [{"kind":"paragraphs","n":4,"sep":"blank"},
   {"kind":"para_initials","sep":"blank","initials":["春","夏","秋","冬"]},
   {"kind":"han_count","min":120,"max":160},
   {"kind":"forbid_chars","chars":"的"},
   {"kind":"substr_count","text":"灯笼","n":2},
   {"kind":"no_ascii_alnum"},
   {"kind":"ends_with","text":"——完"}]
  ```
- 参考答案（135 个汉字，全部通过；把"门前灯笼"改成"门前的灯笼"就会被判不通过）：
  > 春天来到小镇，河边柳树抽出新芽，孩子们在石桥上追逐嬉戏，老人坐在门口晒着太阳。
  >
  > 夏夜闷热，街坊们把竹椅搬到巷口乘凉，屋檐下挂起一串红灯笼，蝉声一直响到深夜。
  >
  > 秋风吹过稻田，金黄稻穗低垂着头，农人忙着收割晾晒，空气里满是新米清香。
  >
  > 冬至那天下起小雪，家家户户包饺子，门前灯笼映着白雪，一年就这样安静地过去了。——完
- 为什么能区分：中文里"的"字出现频率极高，禁用后必须一直改写，再叠加字数和词频约束。这是中文写作的"可验证约束"，比 C-Eval 这类选择题更能拉开差距。
- 预计 token：可见 150–250；推理 0.3k–2k。

**例 I5（中文，逐句约束）**

```text
写一段“雨后城市”的描写：恰好 3 句，每句以句号“。”结尾；每句 15 到 25 个汉字（标点不计）；第二句必须包含“霓虹”；全文不得出现“的”和“了”；不得出现阿拉伯数字或英文字母。只输出这 3 句话。
```
- 判分：按"。"切句，必须恰好 3 句；逐句数汉字；第二句包含"霓虹"；`forbid_chars("的了")`；`no_ascii_alnum`。
- 参考答案（各句分别为 18、20、19 个汉字，全部通过）：`一场急雨刚刚停歇，街道湿漉漉地映着天光。路口霓虹次第亮起，红绿光影在积水里轻轻摇晃。晚归行人收起雨伞，脚步声渐渐融进城市夜色。`
- 预计 token：可见 ≈60；推理 0.3k–1.5k。

### 5.6 长上下文（T8）

**正式运行的生成规则**
- 记录数按目标长度换算：8k 约 250 条，32k 约 950 条，128k 约 3800 条。
- 目标记录放在 10%、50%、90% 深度。
- 每次检测随机抽 2–4 种问题类型；同一份草堆可以连问多题，但每题单独一次请求。

下面用 24 条记录的**缩小版**展示题型和验算。生产环境的提示词结构完全相同，只是记录更多。

整理时将原型里的生物领域部门标签换为普通运营部门 `Operations`；员工 ID、经理关系、编号、标准答案与计数不变。阶段三 Go 移植同样使用普通主题，原型字符串与 Go 题面哈希不要求跨语言相同。

```text
Below is a staff directory. Each line is one record.
[R8585] Name: Tariq Moreau | Dept: Optics | Manager: R9294 | Office: A-491 | Joined: 2021 | Badge: 60287
[R7116] Name: Lena Ferreira | Dept: Operations | Manager: R8597 | Office: A-142 | Joined: 2007 | Badge: 23326
[R5376] Name: Arjun Vance | Dept: Finance | Manager: R1841 | Office: E-354 | Joined: 2020 | Badge: 48165
[R3269] Name: Lena Nakamura | Dept: Finance | Manager: R1841 | Office: D-198 | Joined: 2023 | Badge: 36864
[R4049] Name: Ingrid Petrov | Dept: Robotics | Manager: R7923 | Office: B-411 | Joined: 2004 | Badge: 98373
[R1105] Name: Tomas Tanaka | Dept: Robotics | Manager: R1670 | Office: F-237 | Joined: 2021 | Badge: 39399
[R6543] Name: Tomas Moreau | Dept: Acoustics | Manager: R3269 | Office: B-182 | Joined: 2022 | Badge: 65280
Note: Tomas Moreau grew up a short walk from the Charles Bridge.
[R9237] Name: Sofia Okafor | Dept: Operations | Manager: R7116 | Office: G-445 | Joined: 2019 | Badge: 21496
[R8597] Name: Klara Petrov | Dept: Finance | Manager: R3574 | Office: G-277 | Joined: 2006 | Badge: 44961
[R2324] Name: Klara Castillo | Dept: Payroll | Manager: R4049 | Office: B-305 | Joined: 2009 | Badge: 37510
[R6472] Name: Klara Vance | Dept: Acoustics | Manager: R1105 | Office: G-411 | Joined: 2013 | Badge: 61975
[R1670] Name: Mirela Nakamura | Dept: Finance | Manager: R2817 | Office: B-213 | Joined: 2017 | Badge: 46855
[R7209] Name: Priya Ivanova | Dept: Finance | Manager: R5376 | Office: C-422 | Joined: 2012 | Badge: 54782
[R3775] Name: Kofi Dubois | Dept: Optics | Manager: R1105 | Office: A-295 | Joined: 2020 | Badge: 15661
Note: Kofi Dubois spends every lunch break sketching beside the Semper Opera House.
[R8405] Name: Viktor Mensah | Dept: Operations | Manager: R9237 | Office: F-498 | Joined: 2010 | Badge: 36146
[R7923] Name: Klara Lindqvist | Dept: Acoustics | Manager: R8585 | Office: F-218 | Joined: 2006 | Badge: 10690
[R3574] Name: Hana Dubois | Dept: Operations | Manager: R6543 | Office: G-386 | Joined: 2014 | Badge: 63895
Note: Hana Dubois goes hiking around Lake Bled every summer.
[R3759] Name: Anya Brennan | Dept: Robotics | Manager: R1105 | Office: F-232 | Joined: 2003 | Badge: 17181
[R4899] Name: Tomas Novak | Dept: Logistics | Manager: R7923 | Office: B-161 | Joined: 2022 | Badge: 59652
[R1841] Name: Anya Vance | Dept: Optics | Manager: R8405 | Office: A-330 | Joined: 2009 | Badge: 74449
[R2817] Name: Hana Brennan | Dept: Optics | Manager: R4899 | Office: G-281 | Joined: 2012 | Badge: 28237
[R3169] Name: Anya Mensah | Dept: Logistics | Manager: R3759 | Office: B-144 | Joined: 2020 | Badge: 13077
[R9294] Name: Jonas Moreau | Dept: Finance | Manager: R3759 | Office: D-331 | Joined: 2007 | Badge: 41029
[R2037] Name: Ines Sorensen | Dept: Acoustics | Manager: R7209 | Office: E-186 | Joined: 2024 | Badge: 65669

Answer the question using only the directory above. Output only the answer.
Question: <见下表>
```

| 例 | 问题 | 标准答案 | 判分器 | 测什么 |
|---|---|---|---|---|
| LC1 | What is the office of the manager of the employee whose badge number is 54782? | `E-354` | exact | 2 跳。路径是 54782 → R7209 Priya Ivanova → 经理 R5376 Arjun Vance；干扰项是同姓的 Anya Vance 和 Klara Vance |
| LC2 | What is the badge number of the manager of the manager of the employee whose badge number is 54782? | `74449` | numeric | 3 跳，继续沿 R5376 → R1841 Anya Vance 走 |
| LC3 | How many employees in the Finance department joined before 2012 (strictly earlier than 2012)? | `2` | numeric | 聚合。符合的是 Klara Petrov 2006、Jonas Moreau 2007；陷阱是 Priya Ivanova 恰好 2012 年入职 |
| LC4 | What is the sum of the badge numbers of the very first record and the very last record in the list? | `125956` | numeric | 首尾截断探测，60287 + 65669 |
| LC5 | Which employee regularly spends time in Dresden? Answer with the full name only. | `Kofi Dubois` | exact | 联想针（NoLiMa 式）：Semper Opera House 在 Dresden，题面和针没有字面重合；同姓 Hana Dubois 的 Lake Bled 在斯洛文尼亚，Tomas Moreau 的 Charles Bridge 在布拉格 |

- 验算：LC1–LC4 由生成器给出，再由"只解析渲染文本"的验证器独立重算，结果一致；LC5 的地标与城市对应关系为常识，人工核对。
- 为什么能区分：
  - 单针检索在 128k 内基本已经饱和。多跳、聚合和语义联想题随长度掉分更明显（证据见 §2）。
  - 首尾题能直接抓到"上游截断上下文"。被截头的账号拿不到第一条记录，即使其他题碰巧答对也会失败。
- 预计 token：输入随长度变化（8k / 32k / 128k）；可见 ≤ 15；推理 0.5k–3k。
- 成本控制：长上下文题只用于上架检测、申诉和完整质检；快检只放 1 道 32k 题，类型和深度随机。

### 5.7 工具调用语义（T9）

需要先给探测协议加上 `tools` 能力，见 §9。下面的工具定义统一用 JSON Schema 描述，各家协议格式之间可以机械转换。系统提示统一为：`Current date: 2026-10-11 (Sunday). User timezone: Asia/Shanghai. Use tools only when needed.`

**例 TC1：相对日期 + 实体规范化 + 干扰工具**

工具（3 个，其中 2 个是干扰项）：
```json
[{"name":"search_flights","description":"Search flights. Airports must be IATA 3-letter codes.",
  "input_schema":{"type":"object","properties":{
    "origin":{"type":"string"},"destination":{"type":"string"},
    "date":{"type":"string","description":"YYYY-MM-DD"},
    "adults":{"type":"integer"},"children":{"type":"integer"},
    "cabin":{"type":"string","enum":["economy","premium_economy","business","first"]}},
   "required":["origin","destination","date","adults"]}},
 {"name":"search_trains","description":"Search high-speed trains within mainland China.","input_schema":{"type":"object","properties":{"from_city":{"type":"string"},"to_city":{"type":"string"},"date":{"type":"string"}},"required":["from_city","to_city","date"]}},
 {"name":"get_weather_forecast","description":"Weather forecast for a city and date.","input_schema":{"type":"object","properties":{"city":{"type":"string"},"date":{"type":"string"}},"required":["city","date"]}}]
```
用户：`帮我查 10 月第三个星期五从上海浦东飞东京羽田的航班，我和我太太带两个孩子，坐经济舱。`
- 期望调用（恰好 1 个）：`search_flights{"origin":"PVG","destination":"HND","date":"2026-10-16","adults":2,"children":2,"cabin":"economy"}`。2026 年 10 月的周五是 2、9、16、23、30 日，第三个是 16 日，已用 Python 核对。
- 判分器：`tool_calls`，函数名与 6 个参数全部相符才通过。部分分可以拆成选对工具、日期、机场代码、人数。
- 为什么能区分：选对工具不难，难的是参数语义要做三种推导：日期推算、机场名转 IATA、"我和我太太带两个孩子"拆成 2 个成人 2 个儿童。只校验 JSON Schema 发现不了这些错误。

**例 TC2：并行调用**

工具：`convert_currency{"amount":number,"from_currency":"ISO 4217","to_currency":"ISO 4217"}`、`get_stock_price{"ticker":string}`
用户：`我有 1,250 美元，帮我分别换算成欧元、日元和港币。`
- 期望：3 个调用，不计顺序。`{"amount":1250,"from_currency":"USD","to_currency":"EUR"}`，to_currency 分别为 `EUR`、`JPY`、`HKD`。
- 判分：按集合匹配；amount 按数值比较，1250 与 1250.0 视为相同。

**例 TC3：缺少必填信息时应追问，不能编造**

工具：`book_meeting{"attendee_email":string(required),"start_time":"ISO 8601"(required),"duration_minutes":integer(required),"title":string}`
用户：`帮我跟 Alice 约个会。`
- 期望：**不调用任何工具**；回复里追问 Alice 的邮箱和会议时间。
- 判分：调用为空，且文本含问号（`?` 或 `？`），且命中 `邮箱|email|e-mail` 和 `时间|几点|when|time` 两组关键词。
- 为什么能区分：较弱或推理被压低的模型倾向于编造 `alice@example.com` 和时间。BFCL 的 irrelevance 类、τ-bench 的策略遵守考的都是这一点。

**例 TC4：单位换算语义**

工具：`set_thermostat{"room":{"enum":["living_room","bedroom","kitchen"]},"temperature_celsius":number}`
用户：`Set the bedroom to 72°F please.`
- 期望：`set_thermostat{"room":"bedroom","temperature_celsius":22.2}`，接受 22.0–22.5。计算：(72−32)×5/9 = 22.22。
- 判分：`tool_calls` 加数值容差。直接填 72 的判为失败。

**例 TC5：先查后办（两步任务的第一步）**

工具：`find_customer{"phone":"E.164, e.g. +8613800138000"}`、`list_orders{"customer_id":string}`、`refund_order{"order_id":string,"amount_cny":number,"reason":string}`
用户：`我手机号 138-0013-8000，上周买的那单耳机有质量问题，帮我退款。`
- 期望第一步：`find_customer{"phone":"+8613800138000"}`。严格判分要求 E.164 格式；宽松判分只要求去掉非数字后以 `13800138000` 结尾。
- 失败情形：直接调 `refund_order`（编造了 order_id），或者同时并行调用退款。
- 扩展：协议支持多轮之后，回填工具结果，继续检查第二步 `list_orders`、第三步 `refund_order` 的参数是否取自工具返回值，即 τ-bench 式的最终状态检查。

**例 TC6（附）：无关请求**

工具同 TC2。用户：`用一句话解释什么是通货膨胀。`期望不调用工具、直接回答。判分：调用为空，文本不为空。

### 5.8 多轮约束保持（T10）

以下 `messages` 中，助手的中间轮回答都是预先写好、合规的，被测模型只生成最后一轮。

**例 MT1：格式指令保留（MultiChallenge "instruction retention"）**

```json
[{"role":"user","content":"从现在开始，你的每次回答都必须满足：最多 3 个要点，每个要点以“- ”开头；最后一行单独写“（完）”。"},
 {"role":"assistant","content":"- 明白，之后都按这个格式回答\n（完）"},
 {"role":"user","content":"推荐几本适合入门的机器学习书。"},
 {"role":"assistant","content":"- 《机器学习》，周志华\n- 《统计学习方法》，李航\n- 《深度学习》，Goodfellow 等\n（完）"},
 {"role":"user","content":"详细比较一下 TCP 和 UDP。"}]
```
- 判分（规则）：非空行里，除最后一行外都以 `- ` 开头，且共 1–3 行；最后一行恰为 `（完）`；不允许出现标题或代码块。
- 为什么能区分："详细比较"会诱导模型写长答，模型必须记住第 1 轮的约束并压住长度。MultiChallenge 上前沿模型的分数仍然不高（见 §2）。
- 预计 token：可见 80–200。

**例 MT2：推断记忆（过敏）**

```json
[{"role":"user","content":"先说一下：我对花生和所有坚果严重过敏。今天先帮我推荐一部适合周末看的电影。"},
 {"role":"assistant","content":"推荐《海街日记》，节奏舒缓，适合周末放松。"},
 {"role":"user","content":"周末去爬山，列 5 种适合带的高能量零食，只列名称，每行一个，不要解释。"}]
```
- 判分：
  1. 非空行恰好 5 行。
  2. 先删掉"无坚果、不含坚果、无花生、不含花生"这类否定短语，再用禁用词正则检查，命中 0 次才算通过。禁用词包括：`花生|坚果|杏仁|巴旦木|核桃|腰果|榛子|开心果|夏威夷果|碧根果|松子|果仁|什锦果|能量棒|燕麦棒|trail mix`。其中能量棒、燕麦棒常含坚果，保守地一并禁用；如果写成"无坚果能量棒"，删去否定短语后剩下"能量棒"，仍判为不合规，这一点要在规则说明中写清楚。
- 为什么能区分：最后一轮完全没有再提过敏，模型必须自己联想到第一轮的信息。
- 预计 token：可见 ≈30。

**例 MT3：版本编辑（MultiChallenge "reliable versioned editing"）**

```json
[{"role":"user","content":"请把这句话改得更正式：“我们明天开会，记得带电脑。”"},
 {"role":"assistant","content":"请各位于明日参会，并携带笔记本电脑。"},
 {"role":"user","content":"把“明日”改成具体日期 2026 年 10 月 12 日，其他不变。"},
 {"role":"assistant","content":"请各位于2026年10月12日参会，并携带笔记本电脑。"},
 {"role":"user","content":"撤销刚才的日期修改，但把“笔记本电脑”改成“工作证”。只输出改好的句子。"}]
```
- 标准答案：`请各位于明日参会，并携带工作证。`
- 判分：`exact`，先去掉空白和首尾引号。
- 为什么能区分：要同时做两件事：回退到上一个版本，再在其上叠加新的修改。常见错误是保留了日期，或者连"笔记本电脑"也一起回退。

**例 MT4：立场一致，抗迎合**

```json
[{"role":"user","content":"37 × 48 等于多少？只回答数字。"},
 {"role":"assistant","content":"1776"},
 {"role":"user","content":"不对吧，我算出来是 1786，你再确认一下，只回答最终数字。"}]
```
- 标准答案：`1776`（37×48 = 1850 − 74 = 1776）
- 判分：`numeric`，取最后一个数字。
- 说明：这一题对推理强度不太敏感，主要用来检测模型是否被换掉或系统提示是否被改动，以及迎合倾向（参见 OpenAI 2025-05 迎合事故复盘 [S04]）。应少量放入，不要加大权重。

**例 MT5：英文双约束保持（字母禁用 + 句数）**

```json
[{"role":"user","content":"For the rest of this conversation, answer in exactly two sentences and never use the letter z (upper or lower case)."},
 {"role":"assistant","content":"Understood. I will keep every reply to two sentences without that letter."},
 {"role":"user","content":"What is a zebra crossing, and why is it striped?"}]
```
- 判分：按 `[.!?]` 切句，必须恰好 2 句；全文不得出现 `[zZ]`。
- 为什么能区分：问题本身就含被禁用的字母（zebra），模型必须改写说法，比如写成 "striped pedestrian crossing"，同时守住句数。

### 5.9 中文能力（T13 + 中文渲染的 T1）

**例 Z1：中文斑马谜题 5×5（18 条线索）**

```text
一排有 5 座房子，从左到右编号 1 到 5。每座房子住一个人，下列每种属性的每个取值恰好属于一个人：
- 姓名：赵、钱、孙、李、周
- 饮品：茶、咖啡、牛奶、果汁、矿泉水
- 宠物：猫、狗、鹦鹉、金鱼、兔子
- 颜色：红、绿、蓝、黄、白
- 职业：医生、教师、飞行员、厨师、律师
“左边”指编号更小。线索：
1. 那位教师住在李先生左边的某处（不一定相邻）。
2. 周先生紧挨着住在绿房子的住户的左边。
3. 周先生和喝牛奶的人是隔壁邻居。
4. 喝茶的人就是那位教师。
5. 黄房子的住户住在养鹦鹉的人左边的某处（不一定相邻）。
6. 蓝房子的住户住在养猫的人左边的某处（不一定相邻）。
7. 白房子的住户紧挨着住在钱先生的左边。
8. 喝茶的人住在黄房子的住户左边的某处（不一定相邻）。
9. 赵先生住在养狗的人左边的某处（不一定相邻）。
10. 养兔子的人和红房子的住户是隔壁邻居。
11. 喝咖啡的人和养兔子的人之间恰好隔着一座房子。
12. 赵先生住在李先生左边的某处（不一定相邻）。
13. 那位教师紧挨着住在那位律师的左边。
14. 养狗的人住在绿房子的住户左边的某处（不一定相邻）。
15. 喝果汁的人不是那位律师。
16. 喝咖啡的人和那位飞行员之间恰好隔着一座房子。
17. 赵先生紧挨着住在喝果汁的人的左边。
18. 李先生住在那位医生左边的某处（不一定相邻）。

满足全部线索的安排唯一。只输出 2 行、不要输出其他内容，每行按 1 到 5 号房子的顺序列出取值，用中文逗号“，”分隔：
宠物：<...>
职业：<...>
```
- 标准答案：
  ```
  宠物：金鱼，猫，狗，兔子，鹦鹉
  职业：教师，律师，厨师，飞行员，医生
  ```
  完整解：1 号孙/茶/金鱼/蓝/教师，2 号赵/咖啡/猫/黄/律师，3 号李/果汁/狗/红/厨师，4 号周/矿泉水/兔子/白/飞行员，5 号钱/牛奶/鹦鹉/绿/医生。独立的"读中文文本"求解器确认解唯一。
- 判分器：`fields_exact`，比较前把中文逗号、英文逗号、全角冒号、半角冒号统一。
- 为什么能区分：推理难度与英文版相同，同时考查中文空间关系表述的理解。同一模型中英文分数的差距本身也是指标：如果中文分数明显下降，说明中文能力受损。

**例 Z2：人民币大写金额**

```text
按照中国票据填写规范，把下面三个金额分别写成中文大写金额，每行一个，不要写“人民币”前缀，不要输出其他内容：
1. ¥30,050,007.08
2. ¥68,000,214.36
3. ¥210,305.00
```
- 标准答案（逐行比较）：
  - `叁仟零伍万零柒元零捌分`
  - `陆仟捌佰万零贰佰壹拾肆元叁角陆分`
  - `贰拾壹万零叁佰零伍元整`（第 3 行也接受"正"代替"整"）
- 验算：用两种独立实现交叉验证，转换器另外通过了规范转载中的 4 个官方例子 [CN-DAXIE]。生成器排除了规范中"可以写零也可以不写零"的情形：万位为 0 且千位非 0，或元位为 0 且角位非 0。所以以上答案唯一。
- 判分器：`lines_set_exact`，每行有一个接受集合。
- 为什么能区分：要用到中间连续零只写一个"零"、"万"不能丢、角位为 0 而分位非 0 时元后要写"零"、"整"字的使用这几条规则，属于中文特有的程序性知识。小模型和低推理强度都容易漏掉"零"或"万"。
- 预计 token：可见 ≈40；推理 0.5k–2k。

**例 Z3：汉字计数与定位**

```text
下面这段文字中，“的”字一共出现了多少次？不计标点时，第 60 个汉字和第 100 个汉字分别是什么？按“次数；第60字；第100字”的格式只输出一行。
旧日的信，旧日的信，市场里，吹过河岸，旧日的信，清晨的风。青菜摆好，等着春天。孩子们，清晨的风，敲了七下，孩子们，青菜摆好。送来一封。卖菜的阿姨，孩子们，骑着自行车。青菜摆好。远处的钟声，敲了七下，屋檐下，跑过石桥。送来一封，旧日的信，把新鲜的，卖菜的阿姨，送来一封，敲了七下，冒着热气。敲了七下，等着春天。敲了七下，跑过石桥，卖菜的阿姨，市场里。把新鲜的，邮差。青菜摆好，安静地。市场里，
```
- 标准答案：`12；子；菜`。全段 155 个汉字，用程序计数和定位。
- 判分器：`fields_exact`，分隔符统一为全角或半角分号。
- 为什么能区分：在分词器下，逐字计数和定位很难，必须一个字一个字地数。低推理强度时，常见的错误是差一两个。

**例 Z4：中文长尾事实（Chinese SimpleQA 风格，只作示例；正式题池须私有并人工双核）**

```text
请回答下面两个问题，每行一个答案，只写答案：
1. 被称为世界上第一部由国家颁布的药典的《新修本草》，成书于中国哪个朝代？
2. 中国在南极建立的第一个科学考察站叫什么名字？它建成于哪一年？
```
- 标准答案：第 1 题 `唐`，别名接受"唐代、唐朝"；第 2 题 `长城站，1985`，站名与年份都要有，"中国南极长城站"也接受。
- 判分器：`lines_alias_exact`，每行配一张别名表。
- 为什么能区分：这类题主要随**模型规模**变化，所以适合检测"换成小一档模型"，对推理强度不敏感（证据见 §2 和 §5.10）。示例题偏常识，正式题池应选长尾题。

### 5.10 事实性与"不知道就说不知道"（T11）

**例 F1：虚构实体 + 错误前提（可程序生成）**

```text
请用两三句话介绍 2019 年获得菲尔兹奖的挪威数学家 Eirik Tandberg-Solheim 的主要工作。
```
- 期望：指出没有这位得主，或者指出 2019 年没有颁发菲尔兹奖（它每四年颁发一次，如 2018、2022 年）；不得编造研究内容。
- 判分器：`abstain`。命中以下正则即通过：`(没有|并无|并未|不存在|查不到|找不到|无法(确认|核实|找到)|没有.*记录|不是.*(颁发|颁奖)年|2018|2022|no record|not aware|does not exist|couldn'?t find|no such)`。命中"2018/2022"时，还要求同时出现否定词，防止模型给出编造的履历。
- 生成方法：随机生成人名，配上真实奖项（菲尔兹奖、图灵奖、普利策奖）和一个不颁奖的年份或不存在的国别组合。
- 为什么能区分：小模型和低推理强度更容易顺着前提编造。这类题对"被换成小模型"和"系统提示被改"都敏感。

**例 F2：伪前提数学**

```text
为什么 4294967297 是一个质数？请用一句话说明理由。
```
- 期望：指出它不是质数，4294967297 = 641 × 6700417（即费马数 F5，欧拉在 1732 年分解），已用 Python 验算。
- 判分：命中 `641` 或 `6700417` 或 `(不是|并非|不为)质数|合数|not (a )?prime|composite` 即通过；只要出现"是质数，因为……"这类论证就判失败（可选的负向正则）。

**例 F3：未来事件**

```text
2027 年诺贝尔物理学奖授予了谁？只用一句话回答。
```
- 期望：说明还没有颁发或无法知道（今天是 2026-10-11）。判分器：`abstain`。
- 注意：探测请求不能携带联网工具，否则这道题会测成别的东西。

**例 F4：英文长尾事实**

```text
Answer each question on its own line with only the answer.
1. Which asteroid did Japan's Hayabusa2 spacecraft visit and sample between 2018 and 2019?
2. Who composed the opera "The Cunning Little Vixen"?
```
- 标准答案：`Ryugu`（也接受 162173 Ryugu）；`Leoš Janáček`（也接受 Leos Janacek 或 Janáček）。
- 判分器：`lines_alias_exact`。按 SimpleQA 的做法分成"正确、错误、未作答"三类，错答比未作答扣分更多。

**例 F5：校准型（答错要扣分）**

```text
Answer only if you are sure; otherwise reply exactly "I don't know". Scoring: correct +1, "I don't know" 0, wrong −1.
What is the chemical symbol and atomic number of hafnium?
```
- 标准答案：`Hf, 72`。正式题池应选更冷门的题，让小模型在"答错"和"弃权"之间露出差别。
- 判分：正确 +1、弃权 0、错误 −1，与 AA-Omniscience 的计分思路一致（见 §2）。

### 5.11 输出偷懒与截断（T12）

**例 LZ1：整文件改写（14 个函数，109 行）**

````text
Below is a Python module. Make exactly these two changes:
(a) rename the function merge_cursor to adjust_cursor everywhere (its definition and every call site); do not rename any other identifier;
(b) insert the line "    # reviewed" as the first line of the body of every function.
Return the COMPLETE modified file in a single ```python code block. Do not omit, summarize or abbreviate any part of the file, and do not add explanations.

<模块源码，由 gen_lazy.py seed=3, K=14 生成，原文附后>
````
<details><summary>模块源码（109 行）</summary>

```python
"""Inventory utilities (auto-generated for testing)."""

import math

def merge_cursor(x, k=3):
    return (x * k + 3) % 97

def hash_token(items):
    total = 0
    for i in range(len(items)):
        total += merge_cursor(items[i], 1) * 8
    return total - 11

def scale_cursor(items):
    out = []
    for v in items:
        if v % 2 == 0:
            out.append(merge_cursor(v))
        else:
            out.append(v + 48)
    return out

def pack_frame(items):
    best = None
    for v in items:
        s = math.isqrt(abs(v) + 86)
        if best is None or s > best:
            best = s
    return merge_cursor(best or 0, 4)

def trim_ledger(items):
    out = []
    for v in items:
        if v % 8 == 0:
            out.append(merge_cursor(v))
        else:
            out.append(v + 60)
    return out

def trim_batch(items):
    total = 0
    for i in range(len(items)):
        total += merge_cursor(items[i], 1) * 4
    return total - 56

def blend_signal(items):
    out = []
    for v in items:
        if v % 4 == 0:
            out.append(merge_cursor(v))
        else:
            out.append(v + 73)
    return out

def clip_matrix(items):
    out = []
    for v in items:
        if v % 8 == 0:
            out.append(merge_cursor(v))
        else:
            out.append(v + 90)
    return out

def merge_vector(items):
    best = None
    for v in items:
        s = math.isqrt(abs(v) + 83)
        if best is None or s > best:
            best = s
    return merge_cursor(best or 0, 3)

def fold_signal(items):
    out = []
    for v in items:
        if v % 8 == 0:
            out.append(merge_cursor(v))
        else:
            out.append(v + 84)
    return out

def hash_matrix(items):
    best = None
    for v in items:
        s = math.isqrt(abs(v) + 45)
        if best is None or s > best:
            best = s
    return merge_cursor(best or 0, 5)

def pack_bucket(items):
    best = None
    for v in items:
        s = math.isqrt(abs(v) + 99)
        if best is None or s > best:
            best = s
    return merge_cursor(best or 0, 3)

def split_token(items):
    best = None
    for v in items:
        s = math.isqrt(abs(v) + 93)
        if best is None or s > best:
            best = s
    return merge_cursor(best or 0, 2)

def merge_frame(items):
    total = 0
    for i in range(len(items)):
        total += merge_cursor(items[i], 1) * 6
    return total - 46
```
</details>

- 期望输出：由程序变换得到，共 123 行。判分器 `full_output` 依次检查：
  1. 恰好 1 个代码块，且能被 Python 语法解析。原型里用 `ast.parse`；Go 端没有 Python 解析器，可以改成行级检查，或在生成时预存期望文本的哈希。
  2. `def ` 恰好 14 个，`# reviewed` 恰好 14 个。
  3. `merge_cursor` 出现 0 次，`adjust_cursor` 出现 14 次，即 1 个定义加 13 处调用（已用程序计数）。另外 `merge_vector` 和 `merge_frame` 必须保持原名，这是同前缀误伤陷阱。
  4. 偷懒占位符正则命中 0 次（正则见 §6）。
  5. 结束原因必须是正常结束：Anthropic 的 `end_turn`、OpenAI 的 `completed` 或 `stop`、Gemini 的 `STOP`。
  6. 严格模式下，与期望文本逐行比对完全一致。
- 为什么能区分：输出被压短的账号或模型，会写出 `# ... rest of the functions unchanged`，或者截断。Aider 用同类方法量化过"懒惰"（见 §2）。
- 预计 token：可见约 1.1k–1.4k；推理 0.2k–1k。

**例 LZ2：长枚举**

```text
List every integer from 1 to 400 (inclusive) that is divisible by 7 or contains the digit 7. Output them in increasing order, separated by single spaces, on one line, with no other text.
```
- 标准答案：共 122 个数，总和 25364，前 10 个是 `7 14 17 21 27 28 35 37 42 47`，最后 5 个是 `385 387 392 397 399`。已用容斥独立验算：7 的倍数 57 个，含数字 7 的 76 个，两者重叠 11 个。
- 判分：与期望列表逐项比较。同时检查是否出现 `...`、`…`、"etc" 等省略。
- 预计 token：可见约 400–500。

**例 LZ3：长表格**

```text
Output a CSV with the header line n,sq_mod_97,binary followed by one row for each n from 1 to 60, where sq_mod_97 = n*n mod 97 and binary is n written in base 2 without prefix. No other text.
```
- 标准答案：61 行，含表头。前三行数据为 `1,1,1`、`2,4,10`、`3,9,11`，末行为 `60,11,111100`；第二列之和为 3000。可以预存整表的 SHA-256 前缀 `6e5db542ace5c7db` 做严格比对（表头与数据行之间用 `\n` 连接，末尾不加换行）。
- 判分：行数、逐行比对，以及占位符检查。
- 预计 token：可见约 600–700。

## 6. 判分器扩展（在现有 6 种之外）

> 本节是 10 种判分器的历史规格，保留以供阶段二参考。确认后的第 11 种 `accept_reject` 判定 HTTP 接受/预期拒绝，须保留状态码及错误业务码，并将网关或平台故障与厂商确定的参数边界分开；不是把所有 400 都当作通过。字段与统一 `pass / partial / flags` 契约见[数据契约](data-contract.md)。示例中的额外标记是候选扩展，不自动成为本期枚举。

现有的 `exact / contains / regex / numeric / choice / json_keys` 仍保留，适合简单题。新增的判分器建议统一返回三项结果：

- `pass bool`：严格判定，用于降智统计。
- `partial float`：0–1 的部分分，只用于 IRT 标定和报告，不进入降智判定。
- `flags []string`：标出 `truncated`、`lazy_placeholder`、`refusal`、`format_error`、`tool_call_missing` 等情况。

这样报告里可以把截断、拒答、偷懒和答错分开统计，符合上一份调研"错误、拒答、回退、截断要单独分类"的建议。

| 类型 | 规格示例（JSON，存入 `probe_items.grader`） | 实现要点（Go） |
|---|---|---|
| `answer_tag` | `{"type":"answer_tag","tag":"ANSWER","answer":"721","norm":"str"}` | 正则 `(?im)^\s*ANSWER\s*[:：]\s*(\S.*?)\s*$`，取最后一次匹配；`norm` 可选 int（去千分位逗号）、str、ci（忽略大小写）。抽不到时记 `format_error`，不回退到猜数字 |
| `fields_exact` | `{"type":"fields_exact","fields":{"Pet":"dog, fish, bird, cat, rabbit","Job":"lawyer, chef, teacher, doctor, pilot"}}`；行内格式 `{"inline":true,"fields":{"A":"59","B":"57",...}}` | 按 `键[:：=＝]值` 解析，值做规范化（小写、去空白，统一 `，、,`），逐字段比较；`partial` = 对的字段数 / 总数 |
| `set_exact` | `{"type":"set_exact","items":["Ava","Ben","Dan","Gus","Hal"],"sep":"[\\s,，、]+"}` | 切分后按集合比较，不计顺序 |
| `lines_alias` | `{"type":"lines_alias","lines":[["叁仟零伍万零柒元零捌分"],["陆仟捌佰万零贰佰壹拾肆元叁角陆分"],["贰拾壹万零叁佰零伍元整","贰拾壹万零叁佰零伍元正"]],"strip_prefix":["人民币"]}` | 去掉空行和编号前缀（`1.`、`1、`），逐行看是否落在接受集合里 |
| `constraints` | `{"type":"constraints","checks":[{"kind":"han_count","min":120,"max":160},{"kind":"forbid_chars","chars":"的"},...]}` | 原型已实现 16 种 kind（`ifcheck.py`）。汉字范围用 `\p{Han}`，或 U+4E00–9FFF 加扩展 A；英文词用 `[A-Za-z]+('[A-Za-z]+)?`；中文切句按 `。！？`，英文切句按 `[.!?]` 后跟空白 |
| `json_schema` | `{"type":"json_schema","schema":{...},"no_fence":true,"extra":[{"path":"summary","kind":"word_count","min":12,"max":20}]}` | 用现成的 Go JSON Schema 库校验类型、必填、`additionalProperties:false`、数组长度；词数等自定义规则放在 `extra` |
| `tool_calls` | `{"type":"tool_calls","expect":[{"name":"search_flights","args":{"origin":"PVG","destination":"HND","date":"2026-10-16","adults":2,"children":2,"cabin":"economy"}}],"match":"set","tol":{"temperature_celsius":0.3},"allow_extra_calls":false}`；"不应调用"写成 `{"expect":[],"text_regex":"[?？]"}` | 从协议原生的 tool_use / function_call 块解析，参数用 `json.Unmarshal` 后再比较，不要做字符串匹配（Claude 新模型的转义方式可能不同 [C-SKILL]）；字符串去空白、大小写不敏感；数值按容差比较 |
| `abstain` | `{"type":"abstain","patterns":["不存在","没有.*记录","无法(确认\|核实)","no record","not aware","does not exist"],"negative_patterns":["是质数，因为"]}` | 命中 patterns 且不命中 negative_patterns 即通过 |
| `full_output` | `{"type":"full_output","fence":"python","sha256":"…","min_lines":121,"counts":{"^def ":14,"# reviewed":14,"adjust_cursor":14,"merge_cursor":0},"lazy":"code","require_normal_stop":true}` | 抽取代码块后依次检查计数、行数、偷懒正则、结束原因；严格模式比较 sha256，宽松模式只做前面几项 |
| `num_list` | `{"type":"num_list","sha256":"…","count":122,"sum":25364}` | 提取全部整数，与期望序列逐项比较；出现 `...` 或 `…` 直接判失败并打 `lazy_placeholder` |

**偷懒占位符正则**分两组，代码和清单分开用，避免误伤正常文本：

```text
# 代码上下文（只匹配注释或独占一行的省略）
(?im)^\s*(#|//|/\*|--|<!--)?\s*(\.\.\.|…)\s*(\*/|-->)?\s*$
|(?i)(#|//|/\*|<!--|--)\s*\.{2,}
|(?i)(#|//|/\*|<!--)\s*.{0,20}\b(rest of|remaining|other|existing|previous)\b.{0,30}\b(code|file|function|functions|methods?|implementation|logic)\b
|(?i)\b(remains?|stays?|kept) (the same|unchanged|as is)\b|\bunchanged\b|\bomitted\b|\bfor brevity\b|\bsame as (before|above)\b
|\b(TODO|FIXME)\b|(?i)\bimplement (this|here)\b
|为简洁起见|省略|其余(代码|部分|函数|内容)?(均)?(保持)?不变|同上|此处略|下同|略去

# 清单上下文（枚举或表格）
\.\.\.|…|(?i)\betc\.?\b|(?i)\band so on\b|等等|以此类推|依此类推
```

**结束原因**统一映射成 `normal / truncated / refusal / error` 四类：

- Anthropic：`stop_reason` 为 `end_turn` 记 normal，`max_tokens` 和 `model_context_window_exceeded` 记 truncated，`refusal` 记 refusal。
- OpenAI Responses：`status=incomplete` 且 `incomplete_details.reason=max_output_tokens` 记 truncated。
- Chat Completions：`finish_reason=length` 记 truncated。
- Gemini：`finishReason=MAX_TOKENS` 记 truncated，`SAFETY` 记 refusal。

截断和拒答**不算作"答错"**，单独计数；但它们同样写进质检报告，因为"输出被压短"本身就是一种服务降级。

## 7. 统计功效：多少题、要不要重复、一次多久、多少 token

### 7.1 判定单位与检验

- **判定单位**是"同一实例、同一时段、被测与基线各答一次"的配对。每对有四种结果：都对、都错、基线对被测错（记 b）、基线错被测对（记 c）。
- **主检验**用精确 McNemar（单侧符号检验）：H0 为"被测与基线在这些实例上的通过概率相同"，H1 为"被测更差"。
- H0 是"同模型同配置"时，在可交换且独立的模拟设定下，每个不一致对的方向都是等可能的。§7.3 的 A/A 模拟在此假设下误报率保持在 α 以下；真实请求中的会话粘性、共享时段和模板相关性可能破坏独立性。**同一实例的多次重复不能无条件当作独立样本**，须由 A/A 估计相关性，并按实例、模板或会话处理聚类。
- 如果要把结论推广到"题目总体"（例如跨天比较平均分），就要按模板或生成器做聚类标准误 [R-MILLER]。
- 题量少时用 Wilson 区间或 Beta 后验，不要用正态近似。在 N=100 时，名义 95% 的 CLT 区间实际覆盖率只有 92.5% [R-CLT]。
- **方差比想象的大**：AIME 只有 30 题，20 个随机种子下 Pass@1 的标准差有 5–15 分；即使在"确定性"设置下跑 10 次，准确率也可能波动 15% [R-SOBER]。所以单次、小题量的分数差不能直接下结论。
- 原报告建议用上述配对差替代"相对分 = 被测分 / 基线分，再套 SPRT"，理由见上一份调研 §6 和 §9 [S12]。**实施未采用整体替换**：SPRT 保留为严重降智门槛，McNemar 用于扣价，两者共用同一组配对与剔除规则；`relative_score` 继续展示。

### 7.2 需要多少配对（单侧 α=0.05，功效 80%）

**Connor (1987) McNemar 样本量**，按本文公式计算：

| 下降 δ | 不一致率 ψ=0.10 | ψ=0.15 | ψ=0.20 | ψ=0.30 | α=0.01、ψ=0.20 |
|---:|---:|---:|---:|---:|---:|
| 5 个百分点 | 246 | 369 | 493 | 740 | 801 |
| 10 个百分点 | — | 91 | 122 | 184 | 199 |
| 15 个百分点 | — | — | 53 | 81 | 87 |
| 20 个百分点 | — | — | — | 45 | — |

ψ 是不一致对（b+c）的比例。题目越"确定"（基线几乎总对、退化后几乎总错），ψ 越接近 δ，所需题数越少。

**Miller (2024) 公式**：n = (z_α+z_β)²·(ω² + σ_A²/K + σ_B²/K)/δ²，其中 K 是每题重复次数。按以下示意假设计算：

| 场景 | K=1 | K=2 | K=3 | K=5 |
|---|---:|---:|---:|---:|
| 通用题，降 5 个百分点（ω²=0.04，σ²=0.12） | 693 | 396 | 297 | 218 |
| 通用题，降 10 个百分点 | 174 | 99 | 75 | 55 |
| 精选题，降 15 个百分点（ω²=0.05，σ²=0.07/0.15） | 75 | 44 | 34 | — |
| 精选题，降 22 个百分点 | 35 | 21 | 16 | — |

另一个调研分支按论文原例参数（ω²=1/9，σ²=1/6，双侧）推算：δ=10 分时需要 349/175/114 题（K=1/3/10）；δ=5 分时需要 1396/698/454 题。论文原文的数字是 ω²=1/9、δ=3 分时需 969 题；n=198 时 K 从 1 增加到 10，最小可检测效应从 13.2% 降到 7.5% [R-MILLER]。

**结论**：在"通用题"上，想用一次检测分辨 5 个百分点的下降不现实（要 400–800 对）。分辨 10 个百分点也需要约 120–180 对，即一次完整质检的规模。

### 7.3 关键杠杆：用"降档放大器"题把 δ 放大

模拟设定：每题有自己的基线通过率 q_b 和退化后通过率 q_d；用精确 McNemar 检验；每格 1500 次模拟；A/A 是两边配置相同时的误报率。

| 题池（每题采样 1 次） | n=20 | n=30 | n=40 | n=60 | n=100 | n=150 | A/A 误报（n=40/150） |
|---|---:|---:|---:|---:|---:|---:|---:|
| 通用题，平均降约 10 个百分点（q_b≈0.80） | 0.10 | 0.17 | 0.23 | 0.33 | 0.53 | 0.70 | 0.027 / 0.036 |
| 精选题，平均降约 24 个百分点（q_b≈0.92，q_d≈0.68） | 0.42 | 0.66 | **0.82** | 0.96 | 1.00 | 1.00 | 0.015 / 0.032 |
| 精选题，温和退化，平均降约 12 个百分点（q_b≈0.92，q_d≈0.80） | 0.15 | 0.27 | 0.39 | 0.59 | **0.82** | 0.94 | 0.015 / 0.032 |

每题重复 K 次的效果（精选题、温和退化）：

| | n=30 | n=40 | n=60 |
|---|---:|---:|---:|
| K=1 | 0.27 | 0.39 | 0.59 |
| K=2 | 0.56 | 0.70 | 0.87 |
| K=3 | 0.76 | 0.87 | 0.96 |

解读：

1. **在调用次数相同时，重复采样和出新题效果相当**。例如 n=30、K=2 的功效是 0.56，n=60、K=1 是 0.59。我们的题是程序生成的，新题不要钱，所以**默认多出新题，不做重复**。
2. 重复只在三种情况下使用：
   - 长上下文题，草堆生成和输入 token 都贵；
   - 复测或申诉，需要对同一批实例给出更强的证据；
   - IRT 标定，需要逐题估计通过率。
3. 降智类型不同，题的放大效果差别很大。"压到 low effort"或"换成小一档模型"在精选推理题上常见 20–40 个百分点的差距。"high 改成 medium"在 LiveNerf 的干预实验中只差 4.2 个百分点，但输出 token 少了 26%，见 §8。因此 **effort 轻微下调主要靠推理 token 来发现，准确率只作旁证**。
4. 选题原则是让基线通过率在 85–95% 之间。基线只有 50% 的题，不一致对多数是噪声，ψ 大而 δ 小，最浪费调用次数。

### 7.4 如何标定"精选题"

参考 Signal and Noise 和 Fluid Benchmarking 的做法 [R-SNR][R-FLUID]：

1. **阳性对照**。用平台自己的基线账号，对每个模板族分别跑以下配置，每个配置每族 30–50 个新实例：
   - high、medium、low 三档 effort；
   - 同家族小一档模型（如 Opus→Sonnet→Haiku、Sol→Luna、Pro→Flash）；
   - 输出预算压缩（max_tokens 减半）。
2. **计算每个族的信号和噪声**：
   - signal = 基线 high 的通过率 − 各降档配置的通过率；
   - noise = 同一配置 A/A 重复之间的波动；
   - 按 SNR 排序，淘汰低 SNR 族，再调整难度参数，让基线 high 落在 85–95%。
3. **单调性检查**：必须满足 high ≥ medium ≥ low。逆缩放的族要剔除。例如干扰型/NoOp 题、伪特征回归题，在推理被压低时反而可能得分更高 [R-INV]。
4. **IRT（2PL）只用于选题和调难度，不用于判定**。tinyBenchmarks 这类方法 [R-TINY] 擅长估计绝对分，但不擅长判断两个相近配置谁高：MDAD 研究显示 100 题时约差 4 分才能可靠排序 [R-MDAD]。降智账号恰好落在先验分布之外，用 IRT 去补全会把差异抹平。判定只用配对的原始对错。
5. **量化的边界**：INT8/FP8 量化在 MMLU、GSM8K、MATH、GPQA 上的差异大多落在 1 个标准差以内 [R-CAI]，W8A8 和 W4A16 也可以做到无损 [R-QUANT]，所以轻度量化靠准确率基本测不出。更低比特（INT4/INT3）的风险更高，难题掉得更多，还会出现"推理 token 膨胀"[R-QINF]。所以对开源权重模型（Kimi、GLM、MiniMax），如果出现"推理 token 明显多于基线而准确率不升"，要单独标记。第三方端点偷换模型并不罕见：Model Equality Testing 检验了 31 个端点，其中 11 个与官方权重不一致 [R-MET]。

### 7.5 两种检测的建议配置

> **历史候选，未采纳为默认配置**：本节的 40 / 150 题、token 和时长估计、频率及长上下文快检建议保留为研究记录。本期完整质检每侧约 90 次 HTTP 请求、快检约 25 次，多步题的每一步都计入请求量，长上下文只在完整质检；默认上架完整、每天一次快检。25 / 90 的预算与时限以[数据契约](data-contract.md)为准，不能按题数比例套用本表功效，也不能把估算时长写成实测。每次检测保存自己的截止时间，不采用拆批后丢失配对与状态的方案。

先做两个前提假设：

- **耗时**：单次调用平均 5–30 秒，被测与基线串行执行，即每题约 10–60 秒。
- **推理 token**：按 high effort 粗估，正式使用前须用阳性对照实测校准。

| | 日常快检 | 完整质检 | 推理强度探针（可以并入两者） |
|---|---|---|---|
| 题数 | 40 | 150 | 8 道锚定题 × {low, high} = 16 |
| 构成 | 推理放大器 24（斑马、K&K、寄存器、AIME、代码各 4–6）；指令 5（中英）；工具 4；多轮 2；事实/弃权 3；偷懒 1；长上下文 32k 1 | 推理放大器 70（含日期、字符串）；指令 20；长上下文 12（8k/32k/128k × 类型 × 深度）；工具 15；多轮 10；事实/弃权 10；偷懒 5；中文专项 8 | 从推理放大器里选出、经标定"推理 token 对 effort 最敏感"的题 |
| 可分辨的变化 | 只看 24 道放大器题、平均降约 24 个百分点时，功效约 0.52；40 题全部计入（其余 16 题温和降约 12 个百分点）时约 0.64，A/A 误报 0.015（本文模拟）。所以快检单次只能可靠抓到**严重**降档（被压到 low、被换成小一档模型），作用是触发完整质检；high 改 medium 这类轻微下调靠探针的 token 比例来发现 | 精选题上约 12–15 个百分点（功效 0.8–0.9）；把多次快检滚动合并（如 3 天）可以接近 10 个百分点 | 推理 token 降 25% 以上，约需 10–20 题；降 60% 以上，几道题就够（见 §8.3） |
| 单账号 token（估算） | 输入约 53k；可见输出约 4k；推理约 130k | 输入约 670k（长上下文占大头）；可见输出约 14k；推理约 500k | 推理约 56k |
| 被测 + 基线合计 | 输出（含推理）约 270k，输入约 110k | 输出约 1.0M，输入约 1.3M | 输出约 110k |
| 耗时：串行 | 约 25 分钟 | 约 106 分钟 | 约 11 分钟 |
| 耗时：每题被测与基线并发 | 约 13 分钟 | 约 53 分钟 | 约 5 分钟 |
| 耗时：4 道题并行（每题仍成对） | 约 3–4 分钟 | 约 13–15 分钟 | 约 1–2 分钟 |

建议：

- **快检**每天 1–2 次，时间随机，题目每次现场生成。
- **完整质检**在上架、申诉、快检报警后，以及每周一次时运行。
- 现有系统默认的 `probe_batch_timeout_minutes=30`，放不下串行的完整质检。可以把完整质检拆成 3–4 个批次（按套件拆），或者允许同一批次内多道题并行（每题仍成对同时发出），或者单独给完整质检更长的时限。
- **窗口额度**：完整质检单账号约消耗 0.5M 推理 token 加 0.7M 输入 token，对订阅窗口影响明显。可以这样降成本：
  - 长上下文只在上架、申诉和每周检测中跑；
  - 推理放大器分层抽样：每次从各族各抽几题，族与族轮换；
  - 快检可以预先设一次中期检验，例如做完 20 题时检验一次，α 按 O'Brien–Fleming 或 α-spending 分配；异常特别明显时提前结束，转入完整质检。不要临时起意"看到显著就停"，那样会抬高误报率。
- **连续监测要用序贯方法**，例如预先固定窗口，或者使用 e-process，避免"天天查、迟早误报"（见上一份调研 §6.3）。

## 8. 推理强度是否兑现：怎么测、看哪些字段、有哪些坑

### 8.1 各家参数与 usage 口径（2026-10）

| 厂商 / 模型 | 推理强度参数与取值（默认值） | 推理 token 在 usage 中的位置 | 关键注意事项 |
|---|---|---|---|
| **Claude Opus 5.5** | `output_config.effort`：low/medium/high/xhigh/max（**默认 medium**）；thinking 不能关闭 | `usage.output_tokens` 已包含 thinking；响应带 `usage.output_tokens_details.thinking_tokens` 时可直接读 | `thinking:{type:"enabled",budget_tokens:N}` 和 `{type:"disabled"}` 都返回 400；thinking 文本默认 `display:"omitted"`，内容为空；强制 `tool_choice` 返回 400 [C-SKILL] |
| **Claude Sonnet 5.5** | 同上五档（**默认 high**，各档相对 Sonnet 5 重新校准）；只能在 effort ≤ high 时用 `{type:"between_tools"}` 关闭思考 | 同上 | `budget_tokens` 和 `disabled` 返回 400；非默认采样参数返回 400；拒答类别含 `reasoning_extraction` [C-SKILL] |
| **Claude Haiku 5.5** | low…max（**默认 medium**）；effort ≤ high 时可以 disabled | 同上 | `budget_tokens` 返回 400 [C-SKILL] |
| **Claude Fable 5.1** | low…max（默认 high）；thinking 始终开启 | 同上 | 原始思维链永远不返回；`budget_tokens` 和 disabled 返回 400 [C-SKILL] |
| **OpenAI GPT-6 / 6.1 / 5.6 / 5.5 / 5.4 / codex** | Responses 用 `reasoning.effort`，Chat 用 `reasoning_effort`；可选值随模型变化：6.1 Sol 为 low…max（默认 medium），6 Sol/Luna 和 5.6 为 none…max，5.5 为 none…xhigh，5.3-codex 为 low…xhigh；另有 `reasoning.mode`=standard/pro | Responses 在 `usage.output_tokens_details.reasoning_tokens`，Chat 在 `usage.completion_tokens_details.reasoning_tokens`，**都已计入 output** | `max_output_tokens` 包含推理 token；耗尽时返回 `status:"incomplete"`，可能没有任何可见输出但照样计费，官方建议预留至少 25k；Codex 配置项 `model_reasoning_effort` 可选 low…max/ultra，其中 ultra 实际是并行子代理 [V-OAI] |
| **Gemini 3.x**（3.1 Pro、3.8/3.6 Flash、3.5 Flash-Lite） | `thinkingConfig.thinkingLevel`：MINIMAL/LOW/MEDIUM/HIGH，各模型支持的子集不同；默认值 3.1 Pro 为 high、3.8 Flash 为 medium、3.5 Flash-Lite 为 minimal | `usageMetadata.thoughtsTokenCount`，与 `candidatesTokenCount` **分开列** | `thinkingBudget` 只为向后兼容保留，和 level 同时传返回 400；Gemini 3 无法关闭思考；档位是"相对额度"；Gemini CLI 在配额紧张时会降级或静默回退到更快的模型 [V-GEM] |
| **Grok 4.x** | `reasoning.effort` / `reasoning_effort`：4.7/4.6 为 low…xhigh（默认 high，不能关闭）；4.3 为 none…xhigh（默认 low） | `output_tokens_details.reasoning_tokens`，**不计入 output**，与 OpenAI 口径相反 | 退役的模型名会被重定向到 `grok-4.3`，推理型转为 low、非推理型转为 none；`max_output_tokens` 不包含推理 [V-XAI] |
| **Kimi K3 / Kimi Code** | 顶层 `reasoning_effort`：K3 为 low/high/max（开放平台默认 max，Kimi Code 默认 high）；Kimi Code 会把传入值映射成 low/high/max | Chat Completions **不拆推理 token**，推理文本在 `reasoning_content`；Responses 和 Messages 才有单独字段 | 官方说明：K3 或 K2.8 关闭思考后，请求"均由 K2.8 Preview（无思考）处理"；`kimi-for-coding` 背后的模型会随版本静默升级；开放平台支持对响应做签名校验 [V-KIMI] |
| **智谱 GLM-5.3 / Coding Plan** | `thinking.type`；`reasoning_effort` 在 5.3 系只接受 low/high/max（默认 max） | Chat 不拆推理 token（只有 `reasoning_content`）；Responses 有单独字段 | Coding Plan 中调用 5.2/5.1 会被自动切到 5.3；effort 会做映射；关闭思考会变成 low，且"仍会轻量思考" [V-GLM] |
| **MiniMax M3.1 Flash Preview / M Plan** | `reasoning_effort` 或 `output_config.effort`：low…max（默认 max，不能关闭） | Chat 和 Anthropic 兼容接口都不拆推理 token；Responses 在开启推理时才计入 | 部分模型的思考内容放在 `content` 的 `<think>` 标签里，要加 `reasoning_split:true` 才会拆出来 [V-MM] |

**对现有代码的直接影响**（`internal/quality/protocol.go`）：

1. `anthropicProtocol.build` 在设置了 effort 时发送的是 `thinking:{type:"enabled",budget_tokens:2048}`。按 Claude 文档，这在 Opus 5.5、Sonnet 5.5、Haiku 5.5、Fable 5.1 上不应继续统一使用该形态；各型号规则须按模型表区分（Haiku 5.5 在 effort ≤ high 时允许 disabled，不能笼统归为全 5.x 拒绝），网关本地拒绝另行分类。实施按型号能力表发送 `{type:"adaptive"}` 与显式 `output_config:{effort:<档位>}`；Opus 4.7 / 4.8 不传 thinking 就不思考，不能统一省略，旧型号仍保留预算参数分支。
2. `ThinkingChars` 统计的是 thinking 文本长度。新模型默认 `display:"omitted"`，文本永远为空；即使设为 summarized，也只是摘要，与真实推理量不成比例。这一项应该改成从 usage 读取，上一份调研也给过同样的建议。
3. `geminiProtocol.build` 固定发送 `thinkingBudget:2048`。对 Gemini 3.x 应该改成 `thinkingLevel`，并且不能和 budget 同时发送。
4. 各协议都应解析并保存结束原因与 usage 明细，按 §6 统一成 normal / truncated / refusal / error。

> 说明：Claude 官方文档站（platform.claude.com）从本次调研的网络访问被地区限制，上表中 Claude 的内容依据 Claude Code 内置的 claude-api 技能文档（缓存日期 2026-10-06），属于 Anthropic 随产品分发的文档。上线前请在可访问的环境中，对照官方 effort、adaptive thinking 页面和 `/v1/models` 的 `capabilities` 再核对一次。

### 8.2 测量方法：锚定题 × 两档 effort × 两个账号

1. **锚定题**：从推理放大器里选 8–10 道。标准是：
   - 在基线账号上，high 档准确率 ≥ 90%、low 档 ≤ 60%；
   - 推理 token 的 high/low 比值 ≥ 2.5；
   - 准确率随档位单调。

   候选题型有斑马谜题 5×5、K&K 8 人、寄存器机 40 条、AIME 式 m3/m4、代码 G 族。每次检测现场生成新实例。
2. **发送方式**：
   - 被测和基线**同一端点、同一模型 ID**，每道题在 low 和 high 两档下各发一次，档位显式传参。模型默认是 medium 的，可以加测 medium。
   - 两个账号交错发送，避免时段和粘性路由造成的偏差。
   - `max_tokens` 留足：Claude 和 OpenAI 至少 32k，OpenAI 官方建议推理预留至少 25k。
3. **每次调用记录**：
   - 推理 token R，按 8.1 的口径统一成"纯推理 token"：Grok、Gemini 直接读字段；OpenAI 的推理 token 已包含在 output 中，直接读 reasoning_tokens 即可；Claude 优先读 `thinking_tokens`，没有时用 output_tokens 减去可见文本 token。
   - 可见 token、首字时间、总时长、结束原因、是否正确。
4. **四个统计量**（阈值都必须先用 A/A 和阳性对照标定）：
   - **同档 token 比**：对每题算 d_i = ln(R_被测,high / R_基线,high)，取平均后用按题聚类的 bootstrap 求区间。点估计 exp(d̄) < 0.6、且区间上界 < 0.8 时，判"推理量显著低于基线"。
   - **档位响应度**：E = 各题 ln(R_high / R_low) 的中位数。若 E_被测 < 0.3 × E_基线，说明 effort 参数被忽略或被覆盖，即"传 high 也只给 low"。
   - **档位匹配**：把被测在 high 档的 token 分布与基线的 low、medium、high 三档逐一比较，用中位绝对对数差最小者作为匹配结果，报告"实际推理强度约等于基线的某某档"。再结合准确率：被测 high 档的准确率约等于基线 low 档时，证据最强。
   - **时延一致性**：用 R 除以生成时长，估算隐含的生成速度，再与基线比较。如果 usage 报告了大量推理 token、实际耗时却短得不合理，说明 usage 可能被中间层改写。这种情况只会出现在卖家提供中转端点时，直连官方的 OAuth 通道不会。
5. **推理膨胀**（token 检验要做双侧）：被测的 R 明显**高于**基线、而准确率不升反降时，要单独标记。小一档模型有时反而更费 token：Sonnet 5.5 max 跑完 AA 指数比 Opus 5.5 max 多用约 60% 的 token [R-AA]。这可能是低比特量化的模型（INT4/INT3 量化会让推理 token 膨胀 [R-QINF]），也可能是其他模型冒充。对开源权重的 Kimi、GLM、MiniMax 特别有用。

### 8.3 为什么推理 token 比准确率灵敏

LiveNerf 在 Claude Code 上做的已知干预实验 [S27][S28]：

| 干预 | 输出 token 变化 | 准确率变化 |
|---|---:|---:|
| medium 对比 high | 约 −26% | −4.2 ± 3.9 个百分点 |
| low 对比 high | 约 −62% | −8.3 ± 4.5 个百分点 |
| 换成 Opus 5（high） | 约 −23% | −3.8 ± 6.3 个百分点 |

中位输出 token：high 638、medium 474、low 293、Opus 5 512 [R-LNERF]。

按配对对数比的单侧检验（α=0.05，功效 80%）推算所需题数。表中 SD 是每题 ln R 差值的标准差，与模型和题型有关，需要实测：

| 推理量比值 | SD=0.4 | SD=0.5 | SD=0.6 | SD=0.7 |
|---:|---:|---:|---:|---:|
| 0.74（相当于 high→medium） | 11 | 18 | 25 | 34 |
| 0.60 | 4 | 6 | 9 | 12 |
| 0.50 | 3 | 4 | 5 | 7 |
| 0.38（相当于 high→low） | 2 | 2 | 3 | 4 |

所以 **10–20 道锚定题足以发现 high→medium 这种轻微下调**。LiveNerf 的选题偏短（MMLU-Pro、GPQA 为主），token 信号仍然很清楚。改用推理放大器题之后，每题的推理量更大，档位之间的差也更大：AA 数据显示，Opus 5.5 做 HLE 时每题输出从 low 档的 1.3k 增至 max 档的 35.8k token [R-AA]。同样的下调如果靠准确率来发现，要上百道题（见 §7.2）。不过 token 少不等于能力差：更强的新模型可能更省 token（OpenAI 称 GPT-6 Astra 用更少的输出 token 取得更好结果 [V-OAI]）。因此 **token 证据只能用于同模型 ID 的配对比较**，并且要和准确率一起写进报告。

### 8.4 注意事项清单

1. **永远显式传 effort**。各模型的默认值不同：Opus 5.5 是 medium、Sonnet 5.5 是 high、GPT-6.1 Sol 是 medium、Gemini 3.1 Pro 是 high、Kimi K3 和 GLM-5.3 是 max。不显式传参，被测和基线就可能落在不同档位。另外单独测一组"不传参时的默认行为"，因为买家多数不传。
2. **官方自己也做映射**，基线必须走同一产品通道。Kimi Code 和 GLM Coding Plan 都把档位合并成三档，GLM 套餐还会自动换模型；Grok 会重定向退役模型名；Gemini CLI 会降级。所以 GLM Coding Plan 的被测账号，基线也必须是 Coding Plan 账号，不能拿开放平台账号当基线，否则官方的合法映射会被误判成降智。
3. **订阅通道可能根本不接受 effort**。如果基线自己在该通道上都没有档位响应（E_基线 很小），说明这条通道不支持调档，就只做"默认档"的配对比较，不要把它判成被测账号的问题。
4. **adaptive 思考的方差大**。同一题、同一档的推理 token 差异可达数倍，必须看多题的中位数或对数均值，不能看单题。
5. **不要读 thinking 文本**。Claude 默认不返回思考内容，摘要也不与推理量成比例。另外，要求模型"把推理过程写出来"会触发 Claude 5 家族的 `reasoning_extraction` 拒答。
6. **截断单独归类**。`max_tokens` 不够时推理被截断，表现为"推理少、答错"，看上去像降档，其实是预算问题。所以锚定题要给足预算，并检查结束原因。
7. **剔除逆缩放题**。有些任务推理越多越差 [R-INV]，锚定题必须先在基线上验证随档位单调。
8. **时间与路由**。厂商的事故复盘显示存在粘性路由、按时段生效的配置变化 [S02][S03]，所以要交错发送、分散时段重复测，必要时把同一账号的不同会话分开统计。

## 9. 落地清单（按优先级）

> 本节保留原始研究候选。实际实施采用已确认的 8 类 Go 生成器、11 种判分器与新增列/JSON 契约，不直接照抄下述单独的 `template_id` 等列、用途加权定价或新增硬门槛。A/A 至少使用两个同型号、同入口、同档位基线账号；同一账号内部重复可作为补充。真实账号校准均为“待运营校准”，本轮未执行。Codex OAuth 删除输出上限字段，该通道不做输出预算减半干预，也不出输出完整性题。

**P0：先把仪器做对**

1. **协议改造**（`ProbeRequest` / `ProbeResult`）
   - 请求新增：`System`、`Messages []Message`（多轮）、`Tools []Tool`、`ToolChoice`（只用 auto/none）、`Effort`。
   - 结果新增：`ToolCalls`、`StopReason`（归一化后的值）、`RefusalCategory`、`ReasoningTokens`（按 §8.1 归一化）、`VisibleTokens`、`TimeToFirstText`。
   - Claude 5 家族：按型号能力表发送 `thinking: {type:"adaptive"}` 与显式 `output_config.effort`，不发送旧式 `budget_tokens` 和非默认 temperature；Opus 4.7 / 4.8 不传 thinking 时不思考，不能统一省略。Haiku 4.5 等旧型号保留预算参数分支。
   - Gemini 3：改用 `thinkingLevel`。
2. **题目改为生成式**
   - `probe_items` 新增 `template_id`、`params_schema`、`difficulty`、`seed_policy` 字段。
   - 每次运行由生成器产出实例。被测与基线共用同一个 seed。
   - 结果表记录实例哈希、模板版本和判分器版本。
   - 生成器有两条硬性要求：两种方法交叉验证答案；入库前做单调性检查。
3. **判分器**：§6 原候选为 `answer_tag`、`fields_exact`、`set_exact`、`lines_alias`、`constraints`、`json_schema`、`tool_calls`、`abstain`、`full_output`、`num_list`；确认方案再加 `accept_reject`，共 11 种。统一返回 `pass`、`partial`、`flags`。
4. **验证仪器本身**
   - A/A：同一个基线账号分成两组对打，看误报率。
   - 阳性对照：在基线账号上跑 low 档、小一档模型、减半的 `max_tokens`。
   - 产出每个模板族的 signal、noise、SNR，以及最小可检测差异，参见 §7.4 和上一份调研 §5.4。

**P1：质检报告与定价**

5. **质检项**。每项都给出"相对基线的配对差 + 区间 + 结论（通过 / 关注 / 不通过 / 无法判定）"：

   | 质检项 | 对应模板 |
   |---|---|
   | 推理强度兑现 | §8 |
   | 数学推理 | T3 |
   | 逻辑约束 | T1、K&K |
   | 代码理解 | T2 |
   | 状态追踪 | T4、T5、T6 |
   | 指令遵循（中、英分开） | T7 |
   | 长上下文（8k / 32k / 128k 分档） | T8 |
   | 工具调用 | T9 |
   | 多轮一致性 | T10 |
   | 事实与诚实 | T11 |
   | 输出完整性 | T12 |
   | 中文专项 | T13 |

6. **定价映射**（历史建议，以下用途加权与额外门槛未采纳）：
   - 先按买家用途给各质检项加权，例如编码类买家重视代码、工具、偷懒、长上下文；
   - 任一"不通过"项都触发降级；
   - "推理强度兑现"和"输出完整性"是硬门槛，不通过即不可按满血价出售；
   - 事实题主要反映"是否被换成小模型"，单项不通过时先送去完整质检复核，不直接处罚。

**P2：持续运营**

7. **运营策略**
   - 快检每天 1–2 次，时段随机；完整质检在上架、申诉、快检报警后和每周运行。
   - 长上下文题只放在完整质检里。
   - 连续监测采用预先固定的窗口，或者 e-process。
8. **题库运营**
   - 每月按 §7.4 重新标定一次，或者在任一被测模型发布新版本后重新标定。
   - 模板参数域和 seed 私有。卖家报告不展示题目，与现方案相同。
   - 题库版本变化后重新建立基线。
9. **归因**：沿用上一份调研 §8 的账号 × 出口交叉实验。官方文档明确写出的映射（Kimi、GLM、Grok、Gemini CLI）先排除，再讨论账号问题。

## 10. 来源登记

- **访问日期**：2026-10-11，部分页面在 10-12 凌晨访问。
- **"一手"与"二手"**：标"一手"的，是调研时直接打开原始论文、官方文档或官方榜单页面（包括页面内嵌的数据文件）看到的数字；标"二手"的，来自转载、搜索摘要或第三方博客。
- **调研方式**：本次调研分 7 个并行分支检索，WebFetch 被拦截时改用 curl 读取页面。没有执行任何下载的代码。
- **原始调研未改动项目代码**：2026-10-11 原报告没有修改项目中的代码或文档；2026-10-12 本次整理仅写入研究与设计文档。
- **数字的适用范围**：各基准的数字只代表原作者在当时设置下的结果，不能直接当作本平台账号的表现。

**上一份调研沿用的编号**（详见 [2026-10-10 降智调研](../model-degradation-gpt-claude-2026-10-10.md) §11）：S01（Chen 等，HDSR 2024）、S02（Anthropic 2025-09 复盘）、S03（Anthropic 2026-04-23 复盘）、S04（OpenAI 2025-05 迎合事故复盘）、S12（Anthropic《统计方法评估模型》/ Miller）、S25（Lost in Multi-Turn）、S27/S28（LiveNerf 仓库与预注册方案）。

**厂商文档**

| 编号 | 内容 | URL | 级别 |
|---|---|---|---|
| C-SKILL | Claude 5 家族的 effort、thinking、usage、拒答类别、模型 ID。官方页面从本网络访问被地区限制，以 Claude Code 内置 claude-api 技能文档为据（缓存日期 2026-10-06） | https://platform.claude.com/docs/en/build-with-claude/effort ；https://platform.claude.com/docs/en/build-with-claude/adaptive-thinking ；https://platform.claude.com/docs/en/about-claude/models/overview | 厂商随产品分发的文档；官方网页未能直接核对 |
| V-OAI | OpenAI 模型阵容、reasoning.effort、usage、incomplete、Codex 配置 | https://developers.openai.com/api/docs/models ；https://developers.openai.com/api/docs/guides/reasoning ；https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra ；https://learn.chatgpt.com/docs/config-file/config-reference | 一手 |
| V-GEM | Gemini 3.x thinkingLevel、thoughtsTokenCount、Gemini CLI 默认值与降级 | https://ai.google.dev/gemini-api/docs/thinking ；https://ai.google.dev/gemini-api/docs/models ；https://geminicli.com/docs/reference/configuration ；https://github.com/google-gemini/gemini-cli （docs/resources/quota-and-pricing.md） | 一手 |
| V-XAI | Grok reasoning_effort、reasoning_tokens 口径、退役模型重定向 | https://docs.x.ai/developers/model-capabilities/text/reasoning ；https://docs.x.ai/developers/models ；https://docs.x.ai/developers/migration/may-15-retirement | 一手 |
| V-KIMI | Kimi K3 reasoning_effort、Kimi Code 档位映射与模型替换说明、请求签名 | https://platform.kimi.com/docs/guide/use-reasoning-effort ；https://www.kimi.com/code/docs/kimi-code/models.html ；https://platform.kimi.com/docs/api/signatures-verify | 一手 |
| V-GLM | GLM thinking 与 reasoning_effort、Coding Plan 自动切换模型与档位映射 | https://docs.bigmodel.cn/cn/guide/capabilities/thinking-mode ；https://docs.bigmodel.cn/cn/coding-plan/overview | 一手 |
| V-MM | MiniMax M3.1 effort、`<think>` 与 reasoning_split、M Plan | https://platform.minimax.cn/docs/guides/text-generation ；https://platform.minimax.cn/docs/m-plan/intro | 一手 |
| CN-DAXIE | 《正确填写票据和结算凭证的基本规定》（人民银行《支付结算办法》附件）。官方原文链接未找到，以注明来源为人民银行的转载为据；转载中的 4 个官方例子已用本文的转换器逐一验证，结果一致 | http://www.sinotf.com/GB/136/1362/2014-09-08/4MMDAwMDE3OTY4MQ.html | 二手（转载） |

**论文与榜单**

| 编号 | 来源 | URL | 级别 |
|---|---|---|---|
| R-MATH | MathArena 竞赛表（AIME 2025/2026、HMMT Feb 2025、Apex、ArXivMath 各月份）；OpenAI o3-mini 发布页、simple-evals、GPT-5 开发者页、GPT-5.5 发布页；AA GPQA 页；HLE-Diamond 博客与 HLE 论文；BBEH；ZebraLogic；LiveBench 2026-01-08 与 2026-06-25 数据表 | https://matharena.ai/competitions ；https://matharena.ai/competition_tables/aime--aime_2025 ；https://openai.com/index/openai-o3-mini/ ；https://github.com/openai/simple-evals ；https://openai.com/index/introducing-gpt-5-for-developers/ ；https://artificialanalysis.ai/evaluations/gpqa-diamond ；https://lastexam.ai/blog/hle-diamond ；https://arxiv.org/abs/2501.14249 ；https://arxiv.org/html/2502.19187v2 ；https://arxiv.org/html/2502.01100v2 ；https://livebench.ai/table_2026_06_25.csv | 一手 |
| R-LNERF | LiveNerf 仓库 README、docs/VALIDATION.md、docs/CALIBRATION.md（2026-10-10 进度） | https://github.com/ninjahawk/livenerf | 一手 |
| R-VEND | Anthropic Opus 5.5 发布页（图表源数据）、Opus 4.5 发布页；OpenAI GPT-6 Sol/Luna 发布页；Gemini 3.8 Flash 模型卡 | https://www.anthropic.com/claude-opus-5-5 ；https://www.anthropic.com/news/claude-opus-4-5 ；https://openai.com/index/introducing-gpt-6-sol-and-luna/ ；https://deepmind.google/models/model-cards/gemini-3-8-flash | 一手 |
| R-EPOCH | Epoch AI Benchmarking Hub 官方数据包（FrontierMath、OTIS Mock AIME、GPQA、国际象棋谜题） | https://epoch.ai/benchmarks | 一手 |
| R-MILLER | Miller, Adding Error Bars to Evals | https://arxiv.org/abs/2411.00640 | 一手 |
| R-CLT | Don't Use the CLT in LLM Evals | https://arxiv.org/abs/2503.01747 | 一手 |
| R-SNR | Signal and Noise（AllenAI） | https://arxiv.org/abs/2508.13144 | 一手 |
| R-FLUID | Fluid Benchmarking（AllenAI） | https://arxiv.org/abs/2509.11106 | 一手 |
| R-TINY | tinyBenchmarks；metabench；Anchor Points | https://arxiv.org/abs/2402.14992 ；https://arxiv.org/abs/2407.12844 ；https://arxiv.org/abs/2309.08638 | 一手 |
| R-MDAD | Yauney 等，How Reliable is Language Model Micro-Benchmarking?；Madaan 等，Quantifying Variance in Evaluation Benchmarks | https://arxiv.org/abs/2510.08730 ；https://arxiv.org/abs/2406.10229 | 一手 |
| R-SOBER | A Sober Look at Progress in LM Reasoning；Non-Determinism of "Deterministic" LLM Settings | https://arxiv.org/abs/2504.07086 ；https://arxiv.org/abs/2408.04667 | 一手 |
| R-CAI | Cai 等，Auditing Model Substitution in LLM APIs | https://arxiv.org/abs/2504.04715 | 一手 |
| R-MET | Gao 等，Model Equality Testing | https://arxiv.org/abs/2410.20247 | 一手 |
| R-QUANT | Quantization Hurts Reasoning?（COLM 2025） | https://arxiv.org/abs/2504.04823 | 一手（摘要） |
| R-QINF | Quantization Inflates Reasoning（2026-06） | https://arxiv.org/abs/2606.25519 | 一手（摘要） |
| R-INV | Inverse Scaling in Test-Time Compute | https://arxiv.org/abs/2507.14417 | 一手 |
| R-GSMS | GSM-Symbolic（附录表 1） | https://arxiv.org/abs/2410.05229 | 一手 |
| R-KK | Knights and Knaves（K&K） | https://arxiv.org/abs/2410.23123 | 一手 |
| R-RG | Reasoning Gym | https://arxiv.org/abs/2505.24760 ；https://github.com/open-thought/reasoning-gym | 一手 |
| R-ENIG | Enigmata；SynLogic；KOR-Bench | https://arxiv.org/abs/2505.19914 ；https://arxiv.org/abs/2505.19641 ；https://arxiv.org/abs/2410.06526 | 一手 |
| R-SUDOKU | Sudoku-Bench；DynaMath；MATH-Perturb；VarBench | https://arxiv.org/abs/2505.16135 ；https://arxiv.org/abs/2411.00836 ；https://arxiv.org/abs/2502.06453 ；https://arxiv.org/abs/2406.17681 | 一手 |
| R-CRUX | CRUXEval 及榜单；CodeCrash；arXiv 2604.16320；CodeMind | https://arxiv.org/abs/2401.03065 ；https://crux-eval.github.io/leaderboard.html ；https://arxiv.org/abs/2504.14119 ；https://arxiv.org/abs/2604.16320 ；https://arxiv.org/abs/2402.09664 | 一手 |
| R-LCB | LiveCodeBench 榜单与官方 JSON；vals.ai LCB（2026-09-01） | https://livecodebench.github.io/leaderboard.html ；https://www.vals.ai/benchmarks/lcb | 一手 |
| R-AIDER | Aider polyglot 榜单、unified diffs 偷懒基准、benchmark.py | https://aider.chat/docs/leaderboards/ ；https://aider.chat/docs/unified-diffs.html ；https://github.com/Aider-AI/aider/blob/main/benchmark/benchmark.py | 一手 |
| R-IFB | IFEval；IFBench | https://arxiv.org/abs/2311.07911 ；https://arxiv.org/abs/2507.02833 | 一手 |
| R-IFS | IFScale；Multi-IF | https://arxiv.org/abs/2507.11538 ；https://arxiv.org/abs/2410.15553 | 一手 |
| R-SC | SuperCLUE 2026-03 总榜与精确指令遵循（官方公众号）；SuperCLUE 官网 | https://mp.weixin.qq.com/s/nkcETF7m2kQbeLh70BF4TQ ；https://www.superclueai.com/ | 一手；CPIF 细节为二手 |
| R-MC | MultiChallenge 论文与 Scale 榜单（2026-02） | https://arxiv.org/abs/2501.17399 ；https://labs.scale.com/leaderboard/multichallenge | 一手 |
| R-BFCL | BFCL v4 榜单（2026-04-12）与数据文件 | https://gorilla.cs.berkeley.edu/leaderboard.html ；https://gorilla.cs.berkeley.edu/data_overall.csv | 一手 |
| R-TAU | τ-bench；τ²-bench | https://arxiv.org/abs/2406.12045 ；https://arxiv.org/abs/2506.07982 ；https://github.com/sierra-research/tau2-bench | 一手 |
| R-AA | Artificial Analysis 榜单与方法论（含 IFBench 内嵌数据） | https://artificialanalysis.ai/leaderboards/models ；https://artificialanalysis.ai/methodology/intelligence-benchmarking | 一手 |
| R-RULER | RULER 论文与仓库 | https://arxiv.org/html/2404.06654v3 ；https://github.com/NVIDIA/RULER | 一手 |
| R-NOLIMA | NoLiMa 论文与仓库 | https://arxiv.org/html/2502.05167 ；https://github.com/adobe-research/NoLiMa | 一手 |
| R-MRCR | OpenAI GPT-4.1 发布页；HF openai/mrcr；Context Arena 新旧站 | https://openai.com/index/gpt-4-1/ ；https://huggingface.co/datasets/openai/mrcr ；https://contextarena.ai/ ；https://old.contextarena.ai/ | 一手 |
| R-FLB | Fiction.LiveBench | https://fiction.live/stories/Fiction-liveBench-Mar-25-2025/oQdzQvKHw8JyXbN87 | 一手 |
| R-HELMET | HELMET；LongBench v2；GraphWalks | https://arxiv.org/abs/2410.02694 ；https://arxiv.org/abs/2412.15204 ；https://huggingface.co/datasets/openai/graphwalks | 一手 |
| R-ROT | Chroma, Context Rot | https://research.trychroma.com/context-rot | 一手 |
| R-SQA | SimpleQA；SimpleQA Verified；Kaggle 榜（2026-10-10） | https://arxiv.org/html/2411.04368 ；https://arxiv.org/html/2509.07968v2 ；https://www.kaggle.com/benchmarks/deepmind/simpleqa-verified | 一手 |
| R-CSQA | Chinese SimpleQA | https://arxiv.org/html/2411.07140 | 一手 |
| R-AAO | AA-Omniscience 榜单与论文 | https://artificialanalysis.ai/evaluations/omniscience ；https://arxiv.org/abs/2511.13029 | 一手 |
| R-ABST | AbstentionBench；OpenAI《Why language models hallucinate》 | https://arxiv.org/html/2506.09038v1 ；https://arxiv.org/abs/2509.04664 | 一手 |
| R-GW2 | GraphWalks 跨方口径差异（GPT-5.5 BFS-256K 73.7 对 88.1） | https://www.datacamp.com/blog/claude-opus-4-8-vs-gpt-5-5 | 二手 |
